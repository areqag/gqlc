package gql

import (
	"slices"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/antranig-yeretzian/gqlc/internal/grammar/gql/gen"
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
var typeSpellings = map[string]schema.PropertyType{
	"STRING":    schema.TypeString,
	"CHAR":      schema.TypeString,
	"VARCHAR":   schema.TypeString,
	"BOOL":      schema.TypeBool,
	"BOOLEAN":   schema.TypeBool,
	"DATE":      schema.TypeDate,
	"TIMESTAMP": schema.TypeTimestamp,

	"ZONED DATETIME":              schema.TypeTimestamp,
	"LOCAL DATETIME":              schema.TypeTimestamp,
	"TIMESTAMP WITH TIME ZONE":    schema.TypeTimestamp,
	"TIMESTAMP WITHOUT TIME ZONE": schema.TypeTimestamp,

	"INT":           schema.TypeInt,
	"INTEGER":       schema.TypeInt,
	"SMALLINT":      schema.TypeInt16,
	"SMALL INTEGER": schema.TypeInt16,
	"BIGINT":        schema.TypeInt64,
	"BIG INTEGER":   schema.TypeInt64,
	"INT8":          schema.TypeInt8,
	"INTEGER8":      schema.TypeInt8,
	"INT16":         schema.TypeInt16,
	"INTEGER16":     schema.TypeInt16,
	"INT32":         schema.TypeInt32,
	"INTEGER32":     schema.TypeInt32,
	"INT64":         schema.TypeInt64,
	"INTEGER64":     schema.TypeInt64,
	"INT128":        schema.TypeInt128,
	"INTEGER128":    schema.TypeInt128,
	"INT256":        schema.TypeInt256,
	"INTEGER256":    schema.TypeInt256,

	"UINT":      schema.TypeUint,
	"USMALLINT": schema.TypeUint16,
	"UBIGINT":   schema.TypeUint64,
	"UINT8":     schema.TypeUint8,
	"UINT16":    schema.TypeUint16,
	"UINT32":    schema.TypeUint32,
	"UINT64":    schema.TypeUint64,
	"UINT128":   schema.TypeUint128,
	"UINT256":   schema.TypeUint256,

	"FLOAT":            schema.TypeFloat,
	"REAL":             schema.TypeFloat32,
	"DOUBLE":           schema.TypeFloat64,
	"DOUBLE PRECISION": schema.TypeFloat64,
	"FLOAT16":          schema.TypeFloat16,
	"FLOAT32":          schema.TypeFloat32,
	"FLOAT64":          schema.TypeFloat64,
	"FLOAT128":         schema.TypeFloat128,
	"FLOAT256":         schema.TypeFloat256,

	"DECIMAL": schema.TypeDecimal,
	"DEC":     schema.TypeDecimal,
}

func normaliseType(spelling string) (schema.PropertyType, bool) {
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
