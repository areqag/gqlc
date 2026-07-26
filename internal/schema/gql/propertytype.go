package gql

import (
	"slices"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// property lowers a propertyType context into a schema.Property: name, the
// normalised value type, and nullability. It returns ErrUnsupportedType for a
// grammar-valid value type outside the families this model maps (ADR 0002).
func property(ctx gen.IPropertyTypeContext, ts *antlr.CommonTokenStream) (schema.Property, error) {
	vt := ctx.PropertyValueType().ValueType()

	pt, ok := normaliseType(spelling(vt, ts))
	if !ok {
		return schema.Property{}, ErrUnsupportedType
	}

	return schema.Property{
		Name:     ctx.PropertyName().GetText(),
		Type:     pt,
		Nullable: !hasNotNull(vt),
	}, nil
}

// spelling returns the value type's source text via the token stream so internal
// whitespace is preserved (GetText() collapses it: "ZONED DATETIME" -> "ZONEDDATETIME").
func spelling(vt antlr.ParserRuleContext, ts *antlr.CommonTokenStream) string {
	return ts.GetTextFromInterval(vt.GetSourceInterval())
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
// grammar does not accept them. SIGNED and UNSIGNED prefixes on
// verboseBinaryExactNumericType are grammar-accepted (GQL.g4:1803, :1816) but
// not modelled yet — tracked as gqlc-h9n.4.
//
// The parenthesised binary-width spellings (INT(8), UINT(64), FLOAT(32) …)
// appear as explicit rows because their bit-width identifies a model constant
// (INT(8) == INT8 per GQL.g4:1801). The length/character/decimal-digit
// parenthetical (VARCHAR(255), DECIMAL(p,s), STRING(10)) is not a spelling of
// a type the model already has — it is a qualifier ADR 0002 elects to drop,
// and it does so via the truncated-spelling fallback in normaliseType.
// A trailing NOT NULL is stripped before lookup.
var typeSpellings = map[string]graph.PropertyType{
	"STRING":    graph.TypeString,
	"CHAR":      graph.TypeString,
	"VARCHAR":   graph.TypeString,
	"BOOL":      graph.TypeBool,
	"BOOLEAN":   graph.TypeBool,
	"DATE":      graph.TypeDate,
	"TIMESTAMP": graph.TypeTimestamp,

	"ZONED DATETIME":              graph.TypeTimestamp,
	"LOCAL DATETIME":              graph.TypeTimestamp,
	"TIMESTAMP WITH TIME ZONE":    graph.TypeTimestamp,
	"TIMESTAMP WITHOUT TIME ZONE": graph.TypeTimestamp,

	"INT":           graph.TypeInt,
	"INTEGER":       graph.TypeInt,
	"SMALLINT":      graph.TypeInt16,
	"SMALL INTEGER": graph.TypeInt16,
	"BIGINT":        graph.TypeInt64,
	"BIG INTEGER":   graph.TypeInt64,
	"INT8":          graph.TypeInt8,
	"INTEGER8":      graph.TypeInt8,
	"INT16":         graph.TypeInt16,
	"INTEGER16":     graph.TypeInt16,
	"INT32":         graph.TypeInt32,
	"INTEGER32":     graph.TypeInt32,
	"INT64":         graph.TypeInt64,
	"INTEGER64":     graph.TypeInt64,
	"INT128":        graph.TypeInt128,
	"INTEGER128":    graph.TypeInt128,
	"INT256":        graph.TypeInt256,
	"INTEGER256":    graph.TypeInt256,
	"INT(8)":        graph.TypeInt8,
	"INTEGER(8)":    graph.TypeInt8,
	"INT(16)":       graph.TypeInt16,
	"INTEGER(16)":   graph.TypeInt16,
	"INT(32)":       graph.TypeInt32,
	"INTEGER(32)":   graph.TypeInt32,
	"INT(64)":       graph.TypeInt64,
	"INTEGER(64)":   graph.TypeInt64,
	"INT(128)":      graph.TypeInt128,
	"INTEGER(128)":  graph.TypeInt128,
	"INT(256)":      graph.TypeInt256,
	"INTEGER(256)":  graph.TypeInt256,

	"UINT":      graph.TypeUint,
	"USMALLINT": graph.TypeUint16,
	"UBIGINT":   graph.TypeUint64,
	"UINT8":     graph.TypeUint8,
	"UINT16":    graph.TypeUint16,
	"UINT32":    graph.TypeUint32,
	"UINT64":    graph.TypeUint64,
	"UINT128":   graph.TypeUint128,
	"UINT256":   graph.TypeUint256,
	"UINT(8)":   graph.TypeUint8,
	"UINT(16)":  graph.TypeUint16,
	"UINT(32)":  graph.TypeUint32,
	"UINT(64)":  graph.TypeUint64,
	"UINT(128)": graph.TypeUint128,
	"UINT(256)": graph.TypeUint256,

	"FLOAT":            graph.TypeFloat,
	"REAL":             graph.TypeFloat32,
	"DOUBLE":           graph.TypeFloat64,
	"DOUBLE PRECISION": graph.TypeFloat64,
	"FLOAT16":          graph.TypeFloat16,
	"FLOAT32":          graph.TypeFloat32,
	"FLOAT64":          graph.TypeFloat64,
	"FLOAT128":         graph.TypeFloat128,
	"FLOAT256":         graph.TypeFloat256,
	"FLOAT(16)":        graph.TypeFloat16,
	"FLOAT(32)":        graph.TypeFloat32,
	"FLOAT(64)":        graph.TypeFloat64,
	"FLOAT(128)":       graph.TypeFloat128,
	"FLOAT(256)":       graph.TypeFloat256,

	"DECIMAL": graph.TypeDecimal,
	"DEC":     graph.TypeDecimal,
}

// normaliseType looks up the full canonical spelling first so that a
// parenthesised binary-width form (INT(8)) resolves to the width constant;
// on a miss it falls back to the truncated spelling, which is what keeps
// length/character/decimal-digit qualifiers (STRING(10), DECIMAL(10,2))
// resolving to the bare type per ADR 0002. The fallback is lossy for widths
// with no exact model counterpart — INT(7) lands on TypeInt; gqlc-h9n.25
// tracks whether that should reject instead.
func normaliseType(spelling string) (graph.PropertyType, bool) {
	full := canonicalSpelling(spelling)
	if pt, ok := typeSpellings[full]; ok {
		return pt, true
	}
	pt, ok := typeSpellings[truncateParenthetical(full)]
	return pt, ok
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
// spellings.
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
