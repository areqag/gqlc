package gql

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// property lowers a propertyType context into a schema.Property: name, the
// normalised value type, and nullability. It returns ErrUnsupportedType for a
// grammar-valid value type outside the families this model maps (ADR 0002),
// or one of that error's five family leaves where ADR 0019 has named a reason.
func property(ctx gen.IPropertyTypeContext, ts *antlr.CommonTokenStream) (schema.Property, error) {
	name, err := identifierName(ctx.PropertyName().Identifier())
	if err != nil {
		return schema.Property{}, err
	}
	vt := ctx.PropertyValueType().ValueType()
	pt, notNull, err := resolveValueType(vt, ts)
	if err != nil {
		return schema.Property{}, err
	}
	return schema.Property{
		Name:     name,
		Type:     pt,
		Nullable: !notNull,
	}, nil
}

// resolveValueType maps a valueType parse-tree node to its normalised
// PropertyType and the outer NOT NULL qualifier. It handles the parameterised
// alternatives — lists, records and closed dynamic unions — before delegating
// to declineValueType / normaliseType so that:
//
//   - they are correctly parameterised, and their outer notNull? is read from
//     the direct child rather than by scanning the subtree (hasNotNull would
//     attribute an element's, field's or member's qualifier to the property);
//   - declined families return their named sentinel;
//   - scalar spellings resolve via the existing table.
func resolveValueType(vt gen.IValueTypeContext, ts *antlr.CommonTokenStream) (graph.PropertyType, bool, error) {
	switch lt := vt.(type) {
	case *gen.ListValueTypeAlt1Context:
		// LIST<elemType> (notNull?)
		return resolveListType(lt.ValueType(), lt.NotNull() != nil, ts)
	case *gen.ListValueTypeAlt2Context:
		// elemType LIST (notNull?)  /  elemType ARRAY (notNull?)
		return resolveListType(lt.ValueType(), lt.NotNull() != nil, ts)
	case *gen.ListValueTypeAlt3Context:
		// LIST (notNull?)  /  ARRAY (notNull?)  — bare, no element type
		return graph.TypeList, lt.NotNull() != nil, nil
	case *gen.RecordTypeLabelContext:
		return resolveRecordType(lt.RecordType(), ts)
	case *gen.ClosedDynamicUnionTypeAtl1Context:
		// ANY VALUE? <A | B | …>
		return resolveUnionMembers(lt.AllValueType(), ts)
	case *gen.ClosedDynamicUnionTypeAtl2Context:
		// A | B, left-associative, so A | B | C arrives as ((A | B) | C) and
		// UnionOf's flattening is what makes the three members one set.
		return resolveUnionMembers(lt.AllValueType(), ts)
	}

	if err := declineValueType(vt); err != nil {
		return "", false, err
	}
	pt, ok := normaliseType(spelling(vt, ts))
	if !ok {
		return "", false, ErrUnsupportedType
	}
	return pt, hasNotNull(vt), nil
}

// resolveListType lowers a parameterised list's element type and pairs it with
// the list's own NOT NULL, which the caller read from the list node's direct
// accessor rather than from the element's.
func resolveListType(elem gen.IValueTypeContext, notNull bool, ts *antlr.CommonTokenStream) (graph.PropertyType, bool, error) {
	elemType, elemNotNull, err := resolveValueType(elem, ts)
	if err != nil {
		return "", false, err
	}
	return graph.ListOf(elemType, elemNotNull), notNull, nil
}

// resolveRecordType lowers a recordType (GQL.g4:1977-1980) to its encoded
// PropertyType and the record's own NOT NULL. The three shapes are distinct
// types, not three spellings of one: a braceless RECORD says its fields are
// undeclared, `RECORD { }` says there are none, and a field list says which.
//
// The notNull comes from the recordType's direct accessor, so a field's
// qualifier is never read as the property's.
func resolveRecordType(rt gen.IRecordTypeContext, ts *antlr.CommonTokenStream) (graph.PropertyType, bool, error) {
	notNull := rt.NotNull() != nil

	spec := rt.FieldTypesSpecification()
	if spec == nil {
		return graph.TypeAnyRecord, notNull, nil
	}
	// FieldTypeList() is nil for `{ }` — the accessor being non-nil for a
	// populated list says nothing about the empty one.
	list := spec.FieldTypeList()
	if list == nil {
		return graph.RecordOf(nil), notNull, nil
	}

	declared := list.AllFieldType()
	fields := make([]graph.RecordField, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, ft := range declared {
		// Decoded, exactly as property() takes a property name. The decode is
		// upstream of the seen map deliberately: the duplicate test is over names,
		// and `city` and city are one name, so reading the source bytes here
		// admitted the pair as two fields and RecordOf encoded both.
		//
		// RecordOf then quotes the decoded name for the encoding. That is the
		// second, independent quoting layer (graph/propertytype.go
		// quoteFieldName), and it is what lets a name that legitimately contains a
		// comma or an angle bracket survive the encoding — see
		// TestRecordDelimitedFieldNameQuoted.
		name, err := identifierName(ft.FieldName().Identifier())
		if err != nil {
			return "", false, err
		}
		if seen[name] {
			return "", false, fmt.Errorf("%w: %q", ErrDuplicateFieldName, name)
		}
		seen[name] = true

		fieldType, fieldNotNull, err := resolveValueType(ft.ValueType(), ts)
		if err != nil {
			return "", false, err
		}
		fields = append(fields, graph.RecordField{Name: name, Type: fieldType, NotNull: fieldNotNull})
	}
	return graph.RecordOf(fields), notNull, nil
}

// resolveUnionMembers lowers the members of either closed dynamic union
// alternative. Neither admits a notNull of its own (GQL.g4:1731-1732), so the
// property is always nullable here and a trailing NOT NULL belongs to the last
// member — the grammar's reading rather than the charitable one (ADR 0039).
func resolveUnionMembers(vts []gen.IValueTypeContext, ts *antlr.CommonTokenStream) (graph.PropertyType, bool, error) {
	members := make([]graph.UnionMember, 0, len(vts))
	for _, vt := range vts {
		memberType, memberNotNull, err := resolveValueType(vt, ts)
		if err != nil {
			return "", false, err
		}
		members = append(members, graph.UnionMember{Type: memberType, NotNull: memberNotNull})
	}
	return graph.UnionOf(members), false, nil
}

// declineValueType names the family of a value type gqlc does not model, or nil
// to let normaliseType try the spelling. Deciding on the parse tree rather than
// the spelling is what makes the family knowable at all: `(:X)` and `NOTHING`
// share no text with each other or with anything in typeSpellings, so a lookup
// miss can say only that the lookup missed.
//
// It reads the outermost context and does not descend. One declined family still
// nests a value type inside itself — `BINDING TABLE { id :: STRING }` carries a
// predefinedType for its field — so a subtree search would report the field's
// family in place of the property's.
//
// Descent happens in resolveValueType instead, and only through a construct that
// resolves: a record's field and a union's member are resolved recursively, so
// `RECORD { p :: PATH }` reports ErrPathValueType. That is not the case this
// paragraph warns about — the record itself has a type now, and the only thing
// left to report is the field's.
//
// A value type this does not name falls through to the bare ErrUnsupportedType
// class. The parameterised alternatives are dispatched before this is called
// (see resolveValueType), so they never reach here.
func declineValueType(vt gen.IValueTypeContext) error {
	switch t := vt.(type) {
	case *gen.PathValueTypeLabelContext:
		return ErrPathValueType
	case *gen.PredefinedTypeLabelContext:
		// ISO files references and the immaterial types under <predefined type>,
		// alongside BOOL and STRING, rather than under <constructed value type> —
		// so reaching them means going through this alternative, not past it.
		switch pt := t.PredefinedType(); {
		case pt.ReferenceValueType() != nil:
			return ErrReferenceValueType
		case pt.ImmaterialValueType() != nil:
			return ErrImmaterialValueType
		}
	}
	return nil
}

// spelling returns the value type's default-channel tokens joined by single
// spaces. Joining keeps word boundaries GetText() loses ("ZONED DATETIME" ->
// "ZONEDDATETIME"); reading tokens rather than the source text between them
// drops what the parser never saw. A comment is a hidden-channel token, so the
// source text would carry `INT /* c */ (10)` into the lookup, and a closing
// paren needs no space before NOT NULL, so `INT(10)NOT NULL` would miss the
// " NOT NULL" trim. canonicalSpelling collapses the spaces this inserts around
// a parenthetical.
func spelling(vt antlr.ParserRuleContext, ts *antlr.CommonTokenStream) string {
	// Indexed rather than through ts.GetTokens, which stops one short of the
	// interval's inclusive Stop in antlr4-go v4.
	iv := vt.GetSourceInterval()
	words := make([]string, 0, iv.Length())
	for i := iv.Start; i <= iv.Stop; i++ {
		if tok := ts.Get(i); tok.GetChannel() == antlr.TokenDefaultChannel {
			words = append(words, tok.GetText())
		}
	}
	return strings.Join(words, " ")
}

// hasNotNull reports whether the value type carries a NOT NULL qualifier. The
// NotNull rule hangs off many distinct leaf type contexts, so it is detected by
// presence anywhere in the subtree rather than via a single accessor.
func hasNotNull(t antlr.Tree) bool {
	if _, ok := t.(*gen.NotNullContext); ok {
		return true
	}
	return slices.ContainsFunc(t.GetChildren(), hasNotNull)
}

// typeSpellings maps a canonicalised GQL value-type spelling to its normalised
// PropertyType. Only grammar-reachable spellings appear: NUMERIC, CHARACTER
// and bare DATETIME/LOCALDATETIME are listed in some references but the GQL
// grammar does not accept them.
//
// The parenthesised binary-width spellings (INT(p), INTEGER(p), SIGNED
// INTEGER(p), UINT(p), UNSIGNED INTEGER(p)) have no rows. normaliseType rounds
// them onto binaryWidths from the type their unqualified head resolves to, so
// the five heads reach the rule through their bare rows here and none of them
// can be left out of it. That is the failure an explicit row per width once
// had: every SIGNED prefix row carried a suffix width and every parenthesised
// row carried no prefix, so SIGNED INTEGER(8) matched no row and resolved to
// TypeInt with its eight declared bits gone.
//
// SIGNED and UNSIGNED prefixes on verboseBinaryExactNumericType (GQL.g4:1825
// for SIGNED?, :1838 for UNSIGNED) are represented as one alias row per verbose
// spelling. Alias rows over a pre-lookup normalisation because
// alternative-additions to the grammar are grep-visible here — the fact that
// SIGNED SMALL INTEGER maps to TypeInt16 lands in the same table an author or
// reviewer reads to find any other spelling. The corresponding
// TestPropertyVerboseIntegerSignedness rows go red if any alias row is deleted.
//
// FLOAT(p) is not folded even though its bare form is here. Its parenthetical
// is (LEFT_PAREN precision (COMMA scale)? RIGHT_PAREN) at GQL.g4:1871 — a
// scale-bearing shape byte-identical to DECIMAL(p,s) at :1854, not to the
// scale-free binary integer parentheticals. Nothing in the grammar establishes
// that FLOAT's precision counts bits, so FLOAT(16) falls through to the
// truncated-spelling fallback and lands on TypeFloat. That is gqlc's settled
// decision (ADR 0049), not one pending an ISO citation: the precision is
// dropped, and gqlc does not decide whether it counts bits or digits.
//
// DURATION is present only as the bare key: the grammar (GQL.g4:1914) makes
// the `(YEAR TO MONTH)` / `(DAY TO SECOND)` qualifier mandatory, so
// `DURATION(YEAR TO MONTH)` and `DURATION(DAY TO SECOND)` both reach this
// table via truncateParenthetical and share a single row. Bare `DURATION`
// cannot appear in a well-formed source (pinned by
// TestPropertyBareDurationRejectedAtParse); if a future grammar edit made the
// qualifier optional, the fallback would silently accept it and that test
// would tell us.
//
// The length/character/decimal-digit parenthetical (VARCHAR(255), DECIMAL(p,s),
// STRING(10)) is not a spelling of a type the model already has — it is a
// qualifier ADR 0002 elects to drop, and it does so via the truncated-spelling
// fallback in normaliseType. The duration qualifier drops through the same
// fallback for a different reason (see ADR 0002 §Consequences and the
// TypeDuration paragraph above). A trailing NOT NULL is stripped before
// lookup.
var typeSpellings = map[string]graph.PropertyType{
	"STRING":    graph.TypeString,
	"CHAR":      graph.TypeString,
	"VARCHAR":   graph.TypeString,
	"BYTES":     graph.TypeBytes,
	"BINARY":    graph.TypeBytes,
	"VARBINARY": graph.TypeBytes,
	"BOOL":      graph.TypeBool,
	"BOOLEAN":   graph.TypeBool,
	"DATE":      graph.TypeDate,
	"TIMESTAMP": graph.TypeTimestamp,

	"ZONED TIME":          graph.TypeTime,
	"TIME WITH TIME ZONE": graph.TypeTime,

	"LOCAL TIME":             graph.TypeLocalTime,
	"TIME WITHOUT TIME ZONE": graph.TypeLocalTime,

	"ZONED DATETIME":              graph.TypeTimestamp,
	"LOCAL DATETIME":              graph.TypeTimestamp,
	"TIMESTAMP WITH TIME ZONE":    graph.TypeTimestamp,
	"TIMESTAMP WITHOUT TIME ZONE": graph.TypeTimestamp,

	"DURATION": graph.TypeDuration,

	"INT":                  graph.TypeInt,
	"INTEGER":              graph.TypeInt,
	"SIGNED INTEGER":       graph.TypeInt,
	"SMALLINT":             graph.TypeInt16,
	"SMALL INTEGER":        graph.TypeInt16,
	"SIGNED SMALL INTEGER": graph.TypeInt16,
	"BIGINT":               graph.TypeInt64,
	"BIG INTEGER":          graph.TypeInt64,
	"SIGNED BIG INTEGER":   graph.TypeInt64,
	"INT8":                 graph.TypeInt8,
	"INTEGER8":             graph.TypeInt8,
	"SIGNED INTEGER8":      graph.TypeInt8,
	"INT16":                graph.TypeInt16,
	"INTEGER16":            graph.TypeInt16,
	"SIGNED INTEGER16":     graph.TypeInt16,
	"INT32":                graph.TypeInt32,
	"INTEGER32":            graph.TypeInt32,
	"SIGNED INTEGER32":     graph.TypeInt32,
	"INT64":                graph.TypeInt64,
	"INTEGER64":            graph.TypeInt64,
	"SIGNED INTEGER64":     graph.TypeInt64,
	"INT128":               graph.TypeInt128,
	"INTEGER128":           graph.TypeInt128,
	"SIGNED INTEGER128":    graph.TypeInt128,
	"INT256":               graph.TypeInt256,
	"INTEGER256":           graph.TypeInt256,
	"SIGNED INTEGER256":    graph.TypeInt256,

	"UINT":                   graph.TypeUint,
	"UNSIGNED INTEGER":       graph.TypeUint,
	"USMALLINT":              graph.TypeUint16,
	"UNSIGNED SMALL INTEGER": graph.TypeUint16,
	"UBIGINT":                graph.TypeUint64,
	"UNSIGNED BIG INTEGER":   graph.TypeUint64,
	"UINT8":                  graph.TypeUint8,
	"UNSIGNED INTEGER8":      graph.TypeUint8,
	"UINT16":                 graph.TypeUint16,
	"UNSIGNED INTEGER16":     graph.TypeUint16,
	"UINT32":                 graph.TypeUint32,
	"UNSIGNED INTEGER32":     graph.TypeUint32,
	"UINT64":                 graph.TypeUint64,
	"UNSIGNED INTEGER64":     graph.TypeUint64,
	"UINT128":                graph.TypeUint128,
	"UNSIGNED INTEGER128":    graph.TypeUint128,
	"UINT256":                graph.TypeUint256,
	"UNSIGNED INTEGER256":    graph.TypeUint256,

	"FLOAT":            graph.TypeFloat,
	"REAL":             graph.TypeFloat32,
	"DOUBLE":           graph.TypeFloat64,
	"DOUBLE PRECISION": graph.TypeFloat64,
	"FLOAT16":          graph.TypeFloat16,
	"FLOAT32":          graph.TypeFloat32,
	"FLOAT64":          graph.TypeFloat64,
	"FLOAT128":         graph.TypeFloat128,
	"FLOAT256":         graph.TypeFloat256,

	"DECIMAL": graph.TypeDecimal,
	"DEC":     graph.TypeDecimal,

	// The one gqlc-owned spelling in this table. ISO 39075 names no UUID
	// value type, so there is no standard spelling to alias and no
	// parenthesised or verbose form to fold — the grammar admits the bare
	// keyword and nothing else (GQL.g4 uuidType), so this row is the whole
	// of it and a second row here would be a spelling the parser refuses.
	"UUID": graph.TypeUUID,

	// Open dynamic union types: ANY VALUE, bare ANY (VALUE elided), and the two
	// spellings of the narrower ANY? PROPERTY VALUE. All map to TypeAnyPropertyValue
	// and emit Go's any. (ADR 0020)
	"ANY VALUE":          graph.TypeAnyPropertyValue,
	"ANY":                graph.TypeAnyPropertyValue,
	"PROPERTY VALUE":     graph.TypeAnyPropertyValue,
	"ANY PROPERTY VALUE": graph.TypeAnyPropertyValue,
}

// binaryWidths is the lattice a parenthesised binary width rounds up onto,
// keyed by the type the spelling's unqualified head resolves to. Only the two
// machine words key it, so INT, INTEGER and SIGNED INTEGER share the signed
// lattice, UINT and UNSIGNED INTEGER the unsigned one, and no other
// parenthetical (FLOAT(p), DECIMAL(p,s), STRING(n)) reaches it.
var binaryWidths = map[graph.PropertyType][]struct {
	bits uint64
	pt   graph.PropertyType
}{
	graph.TypeInt: {
		{8, graph.TypeInt8},
		{16, graph.TypeInt16},
		{32, graph.TypeInt32},
		{64, graph.TypeInt64},
		{128, graph.TypeInt128},
		{256, graph.TypeInt256},
	},
	graph.TypeUint: {
		{8, graph.TypeUint8},
		{16, graph.TypeUint16},
		{32, graph.TypeUint32},
		{64, graph.TypeUint64},
		{128, graph.TypeUint128},
		{256, graph.TypeUint256},
	},
}

// normaliseType looks up the full canonical spelling, then the truncated one.
// On the truncated lookup a binary-integer head rounds its parenthesised width
// up to the narrowest width constant that holds it, reading p as a count of
// bits: INT(8) is TypeInt8, INT(7) is TypeInt8 too, INT(10) is TypeInt16
// (ADR 0050). A width wider than 256 takes the widest constant, which both
// codegen targets refuse: nothing fits, and the machine word would narrow it
// silently. Zero bits is not a width and keeps the unqualified machine word.
//
// Every other parenthetical is dropped by the truncated lookup, which is what
// keeps length/character/decimal-digit qualifiers (STRING(10), DECIMAL(10,2))
// and FLOAT(p) resolving to the bare type per ADR 0002 and ADR 0049.
func normaliseType(spelling string) (graph.PropertyType, bool) {
	full := canonicalSpelling(spelling)
	if pt, ok := typeSpellings[full]; ok {
		return pt, true
	}
	head := truncateParenthetical(full)
	pt, ok := typeSpellings[head]
	if !ok {
		return "", false
	}
	if bits, ok := parenthesisedWidth(full[len(head):]); ok {
		if widths := binaryWidths[pt]; len(widths) > 0 {
			for _, w := range widths {
				if bits <= w.bits {
					return w.pt, true
				}
			}
			return widths[len(widths)-1].pt, true
		}
	}
	return pt, true
}

// parenthesisedWidth reads the precision out of a canonical "(p)" suffix. The
// grammar's UNSIGNED_DECIMAL_INTEGER admits a leading zero and an underscore
// between digits, so INT(08) and INT(1_6) are read as the numbers they spell.
// Zero is reported as no width at all: no lattice step is a width of zero bits.
func parenthesisedWidth(paren string) (uint64, bool) {
	inner, ok := strings.CutPrefix(paren, "(")
	if !ok {
		return 0, false
	}
	inner, ok = strings.CutSuffix(inner, ")")
	if !ok {
		return 0, false
	}
	// ParseUint saturates at MaxUint64 on ErrRange, which is still wider than
	// the lattice, so an overflowing width takes the widest constant too.
	bits, err := strconv.ParseUint(strings.ReplaceAll(inner, "_", ""), 10, 64)
	if (err != nil && !errors.Is(err, strconv.ErrRange)) || bits == 0 {
		return 0, false
	}
	return bits, true
}

// canonicalSpelling reduces a raw value-type spelling to its lookup key:
// uppercased, whitespace collapsed to single spaces (including inside a
// parenthetical, so `INT ( 8 )` and `INT(8)` share a key), and a trailing
// NOT NULL removed. The parenthetical itself is preserved — the width-bearing
// forms are direct typeSpellings rows; normaliseType strips it on the fallback
// lookup for qualifier forms.
func canonicalSpelling(s string) string {
	s = strings.ToUpper(s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSuffix(s, " NOT NULL")
	return collapseParenthetical(s)
}

// collapseParenthetical removes the space before `(` and the spaces adjacent
// to `(`, `)` and `,` inside the first parenthetical, so `INT ( 8 )`,
// `INT (8)` and `FLOAT(10, 2)` share a lookup key with their unspaced
// spellings. Only the width forms cash that in, through parenthesisedWidth: no
// typeSpellings row holds a comma, so a comma-bearing key misses the full
// lookup either way and resolves down truncateParenthetical, which discards the
// parenthetical this rewrote.
// The comma clause is kept for the row gqlc-5md would add, and
// TestCanonicalSpellingCommaSpacingIrrelevant is the only thing that can see it
// until then — gqlc-825 has the proof.
func collapseParenthetical(s string) string {
	open := strings.IndexByte(s, '(')
	if open < 0 {
		return s
	}
	end := strings.IndexByte(s[open:], ')')
	if end < 0 {
		return s
	}
	end += open
	head := strings.TrimRight(s[:open], " ")
	inner := s[open+1 : end]
	inner = strings.ReplaceAll(inner, " ,", ",")
	inner = strings.ReplaceAll(inner, ", ", ",")
	inner = strings.TrimSpace(inner)
	return head + "(" + inner + ")" + s[end+1:]
}

func truncateParenthetical(s string) string {
	if i := strings.IndexByte(s, '('); i >= 0 {
		return strings.TrimRight(s[:i], " ")
	}
	return s
}
