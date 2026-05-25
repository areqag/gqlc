package gql

import (
	"slices"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen"
	"github.com/antranig-yeretzian/gqlc/internal/graph"
	"github.com/antranig-yeretzian/gqlc/internal/schema"
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
// PropertyType. Only grammar-reachable spellings appear: e.g. SIGNED, UNSIGNED,
// NUMERIC, CHARACTER, bare DATETIME/LOCALDATETIME are listed in some references
// but the GQL grammar does not accept them, so they cannot occur. Length and
// precision qualifiers and a trailing NOT NULL are stripped before lookup.
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

	"UINT":      graph.TypeUint,
	"USMALLINT": graph.TypeUint16,
	"UBIGINT":   graph.TypeUint64,
	"UINT8":     graph.TypeUint8,
	"UINT16":    graph.TypeUint16,
	"UINT32":    graph.TypeUint32,
	"UINT64":    graph.TypeUint64,
	"UINT128":   graph.TypeUint128,
	"UINT256":   graph.TypeUint256,

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
}

func normaliseType(spelling string) (graph.PropertyType, bool) {
	pt, ok := typeSpellings[canonicalSpelling(spelling)]
	return pt, ok
}

// canonicalSpelling reduces a raw value-type spelling to its lookup key:
// uppercased, with the length/precision parenthetical and a trailing NOT NULL
// removed, and internal whitespace collapsed to single spaces.
func canonicalSpelling(s string) string {
	s = strings.ToUpper(s)
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSuffix(s, " NOT NULL")
}
