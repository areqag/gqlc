package graph

import (
	"slices"
	"strings"
)

// PropertyType is the normalised value type of a property; numeric types carry
// their bit width (ADR 0002). Parameterised types encode their parameters in
// the string: "LIST<ELEM>", "RECORD<name enc,...>" and "UNION<enc|...>", where
// an inner "enc NOT NULL" carries that element's, field's or member's own
// qualifier. Kind, Elem, ElemNotNull, Fields and Members provide structured
// access to those encodings.
//
// Every parameterised value MUST be built by ListOf, RecordOf or UnionOf.
// Those constructors canonicalise, and canonicalisation is what makes string
// equality mean type equality: the resolver unifies a query reference across
// labels by comparing property types with ==, so two spellings of one record
// or union type that produced two strings would refuse a valid query. A string
// assembled by hand skips that and is a defect, not a shortcut.
type PropertyType string

// PropertyTypeKind distinguishes scalar property types from the parameterised
// families. The zero value is KindScalar so uninitialized values name a scalar.
type PropertyTypeKind int

// KindScalar is the zero value; the others name a parameterised encoding.
const (
	KindScalar PropertyTypeKind = iota
	KindList
	KindRecord
	KindUnion
)

// Kind names the family pt encodes. It is the discriminator that callers must
// read BEFORE switching on the scalar constant table, so that "is a record"
// and "is a scalar this backend cannot represent" cannot collapse into one
// answer — which is the whole reason the encoding is safe to extend.
//
// The tests are on the prefix up to and including "<", which no scalar
// constant contains, so a constant can never be read as a parameterised type.
func (pt PropertyType) Kind() PropertyTypeKind {
	switch {
	case strings.HasPrefix(string(pt), "LIST<"):
		return KindList
	case strings.HasPrefix(string(pt), "RECORD<"):
		return KindRecord
	case strings.HasPrefix(string(pt), "UNION<"):
		return KindUnion
	}
	return KindScalar
}

// Elem returns the element type of a list PropertyType. The result is
// undefined (returns "") when Kind() != KindList.
func (pt PropertyType) Elem() PropertyType {
	s := strings.TrimPrefix(string(pt), "LIST<")
	s = strings.TrimSuffix(s, ">")
	s = strings.TrimSuffix(s, " NOT NULL")
	return PropertyType(s)
}

// ElemNotNull reports whether the element type of a list PropertyType carries
// a NOT NULL qualifier. Returns false when Kind() != KindList.
func (pt PropertyType) ElemNotNull() bool {
	s := strings.TrimPrefix(string(pt), "LIST<")
	return strings.HasSuffix(s, " NOT NULL>")
}

// ListOf constructs a list PropertyType whose element type is elem and whose
// element carries a NOT NULL qualifier when elemNotNull is true. Use TypeList
// for a bare LIST / ARRAY with no declared element type.
func ListOf(elem PropertyType, elemNotNull bool) PropertyType {
	if elemNotNull {
		return PropertyType("LIST<" + string(elem) + " NOT NULL>")
	}
	return PropertyType("LIST<" + string(elem) + ">")
}

// RecordField is one named field of a record type. Name is the field's
// identifier as the author wrote it, unquoted — RecordOf applies whatever
// quoting the encoding needs and Fields removes it again.
type RecordField struct {
	Name    string
	Type    PropertyType
	NotNull bool
}

// UnionMember is one member of a closed dynamic union.
type UnionMember struct {
	Type    PropertyType
	NotNull bool
}

// RecordOf constructs a record PropertyType. Fields are sorted by name, which
// is what makes `RECORD { a :: INT, b :: STRING }` and its reverse spelling one
// type rather than two that never unify. Zero fields encode as "RECORD<>",
// which is a different type from TypeAnyRecord: one is a record with no
// fields, the other a record whose fields are undeclared.
//
// Duplicate names are not rejected here; the parser refuses them at the point
// where it can name the offending source, so nothing that reaches this can
// carry one.
func RecordOf(fields []RecordField) PropertyType {
	if len(fields) == 0 {
		return "RECORD<>"
	}
	sorted := slices.Clone(fields)
	slices.SortFunc(sorted, func(a, b RecordField) int { return strings.Compare(a.Name, b.Name) })

	encoded := make([]string, len(sorted))
	for i, f := range sorted {
		encoded[i] = quoteFieldName(f.Name) + " " + withNotNull(f.Type, f.NotNull)
	}
	return PropertyType("RECORD<" + strings.Join(encoded, ",") + ">")
}

// UnionOf constructs a closed dynamic union PropertyType, reducing its members
// to a canonical set: nested unions are flattened, a bare ANY absorbs the whole
// union, exact duplicates collapse, members sort by their encoded spelling, and
// a lone unqualified member is not a union at all but that member's own type.
//
// A lone NOT NULL member does NOT collapse: the union is the only place the
// qualifier could live, so collapsing would drop it.
//
// These reductions are recorded gqlc decisions, not readings of ISO 39075 —
// the semantics volume is not among the freely published artefacts (the
// arbiter is bd gqlc-lir). See the ADR.
func UnionOf(members []UnionMember) PropertyType {
	flat := make([]UnionMember, 0, len(members))
	for _, m := range members {
		// Only an unqualified nested union flattens. A NOT NULL one has
		// nowhere to put its qualifier once its members are spliced in, and
		// dropping it silently would be worse than leaving a union nested.
		// The parser cannot produce that shape — neither closed-union
		// alternative admits an outer NOT NULL (GQL.g4:1731-1732) — so this
		// arm exists for direct callers of the constructor.
		if m.Type.Kind() == KindUnion && !m.NotNull {
			flat = append(flat, m.Type.Members()...)
			continue
		}
		flat = append(flat, m)
	}

	for _, m := range flat {
		if m.Type == TypeAnyPropertyValue && !m.NotNull {
			return TypeAnyPropertyValue
		}
	}

	slices.SortFunc(flat, func(a, b UnionMember) int {
		return strings.Compare(withNotNull(a.Type, a.NotNull), withNotNull(b.Type, b.NotNull))
	})
	flat = slices.Compact(flat)

	if len(flat) == 1 && !flat[0].NotNull {
		return flat[0].Type
	}

	encoded := make([]string, len(flat))
	for i, m := range flat {
		encoded[i] = withNotNull(m.Type, m.NotNull)
	}
	return PropertyType("UNION<" + strings.Join(encoded, "|") + ">")
}

// Fields returns the declared fields of a record PropertyType, in canonical
// order. It is nil for anything else, including TypeAnyRecord and the
// zero-field "RECORD<>" — both of which ARE records, so a caller must not read
// a nil result as "not a record". Ask Kind for that.
func (pt PropertyType) Fields() []RecordField {
	if pt.Kind() != KindRecord {
		return nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(string(pt), "RECORD<"), ">")
	if inner == "" || inner == "ANY" {
		return nil
	}
	parts := splitTopLevel(inner, ',')
	fields := make([]RecordField, 0, len(parts))
	for _, part := range parts {
		name, rest := splitFieldName(part)
		enc, notNull := splitNotNull(rest)
		fields = append(fields, RecordField{Name: name, Type: PropertyType(enc), NotNull: notNull})
	}
	return fields
}

// Members returns the members of a union PropertyType, in canonical order, and
// nil for anything else.
func (pt PropertyType) Members() []UnionMember {
	if pt.Kind() != KindUnion {
		return nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(string(pt), "UNION<"), ">")
	if inner == "" {
		return nil
	}
	parts := splitTopLevel(inner, '|')
	members := make([]UnionMember, 0, len(parts))
	for _, part := range parts {
		enc, notNull := splitNotNull(part)
		members = append(members, UnionMember{Type: PropertyType(enc), NotNull: notNull})
	}
	return members
}

// withNotNull appends the qualifier in the one spelling the encoding uses.
func withNotNull(pt PropertyType, notNull bool) string {
	if notNull {
		return string(pt) + " NOT NULL"
	}
	return string(pt)
}

// splitNotNull separates an encoded type from a trailing NOT NULL qualifier.
//
// The suffix test cannot misfire on a qualifier belonging to something nested:
// every parameterised encoding ends in ">", so a nested qualifier is never
// last, and no scalar constant contains a space. That is a property of the
// constant table, and TestScalarConstantsCarryNoSpace holds it — without it
// this would be an accident rather than a rule.
func splitNotNull(s string) (string, bool) {
	if enc, ok := strings.CutSuffix(s, " NOT NULL"); ok {
		return enc, true
	}
	return s, false
}

// bareFieldName reports whether name can be written without quoting.
func bareFieldName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// quoteFieldName renders a field name for the encoding. GQL's fieldName admits
// a delimited identifier (GQL.g4:2891, :2956-2958), so a legal field name can
// contain ",", "|", "<" and ">" — the encoding's own structure. Left bare,
// such a name forges a field boundary and two unrelated record types collide
// on one string, which, because == is how types unify, silently equates them.
// Quoting uses the lexer's own escape: backtick-delimited, internal backticks
// doubled.
func quoteFieldName(name string) string {
	if bareFieldName(name) {
		return name
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// splitFieldName peels the leading field name off an encoded field, returning
// it unquoted along with the remaining "enc [NOT NULL]".
func splitFieldName(field string) (name, rest string) {
	if !strings.HasPrefix(field, "`") {
		name, rest, _ = strings.Cut(field, " ")
		return name, rest
	}
	var b strings.Builder
	for i := 1; i < len(field); i++ {
		if field[i] != '`' {
			b.WriteByte(field[i])
			continue
		}
		if i+1 < len(field) && field[i+1] == '`' {
			b.WriteByte('`')
			i++
			continue
		}
		return b.String(), strings.TrimPrefix(field[i+1:], " ")
	}
	return b.String(), ""
}

// splitTopLevel splits on sep at nesting depth zero, treating backtick-quoted
// spans as opaque. Both exclusions are load-bearing: a naive split reports
// four fields for RECORD<a RECORD<x INT,y INT>,b STRING> and three members for
// UNION<LIST<UNION<BOOL|DATE>>|STRING>, and every one of them is garbage.
func splitTopLevel(s string, sep byte) []string {
	var (
		parts []string
		start int
		depth int
	)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '`':
			for i++; i < len(s); i++ {
				if s[i] == '`' {
					if i+1 < len(s) && s[i+1] == '`' {
						i++
						continue
					}
					break
				}
			}
		case '<':
			depth++
		case '>':
			depth--
		case sep:
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// The normalised property types: scalars first, then the numeric families,
// each width-preserving variant its own type (ADR 0002).
const (
	TypeString    PropertyType = "STRING"
	TypeBytes     PropertyType = "BYTES"
	TypeBool      PropertyType = "BOOL"
	TypeDate      PropertyType = "DATE"
	TypeTime      PropertyType = "TIME"
	TypeLocalTime PropertyType = "LOCALTIME"
	TypeTimestamp PropertyType = "TIMESTAMP"
	TypeDuration  PropertyType = "DURATION"

	TypeInt    PropertyType = "INT"
	TypeInt8   PropertyType = "INT8"
	TypeInt16  PropertyType = "INT16"
	TypeInt32  PropertyType = "INT32"
	TypeInt64  PropertyType = "INT64"
	TypeInt128 PropertyType = "INT128"
	TypeInt256 PropertyType = "INT256"

	TypeUint    PropertyType = "UINT"
	TypeUint8   PropertyType = "UINT8"
	TypeUint16  PropertyType = "UINT16"
	TypeUint32  PropertyType = "UINT32"
	TypeUint64  PropertyType = "UINT64"
	TypeUint128 PropertyType = "UINT128"
	TypeUint256 PropertyType = "UINT256"

	TypeFloat    PropertyType = "FLOAT"
	TypeFloat16  PropertyType = "FLOAT16"
	TypeFloat32  PropertyType = "FLOAT32"
	TypeFloat64  PropertyType = "FLOAT64"
	TypeFloat128 PropertyType = "FLOAT128"
	TypeFloat256 PropertyType = "FLOAT256"

	TypeDecimal PropertyType = "DECIMAL"

	// TypeAnyPropertyValue is the open dynamic union of storable property value
	// types — ISO GQL's ANY VALUE and ANY? PROPERTY VALUE. Both spellings are the
	// same type: a property whose value the author did not or could not constrain
	// further. Emitted as Go's any. (ADR 0020)
	TypeAnyPropertyValue PropertyType = "ANY"

	// TypeList is the bare list type — ISO GQL's LIST and ARRAY with no declared
	// element type. Equivalent to LIST<ANY VALUE>; emitted as Go's []any.
	TypeList PropertyType = "LIST<ANY>"

	// TypeAnyRecord is the braceless record type — ISO GQL's RECORD and
	// ANY RECORD, whose fields are undeclared. Distinct from RecordOf(nil)
	// ("RECORD<>"), which declares that there are none. "ANY" cannot be
	// mistaken for a field: every encoded field carries a space between its
	// name and its type, and this has none.
	TypeAnyRecord PropertyType = "RECORD<ANY>"
)
