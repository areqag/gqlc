package codegen

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
)

// rowBareIdent matches column text of shape "name" — a bare identifier
// projection like RETURN n or RETURN name (spec §4.3 shape 1). Anchored so
// substring matches are impossible.
var rowBareIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// rowPropAccess matches column text of shape "n.name" — a single-dot
// property access projection like RETURN p.name (spec §4.3 shape 2).
// Anchored so substring matches are impossible.
var rowPropAccess = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*$`)

// columnNeedsImports reports whether one prepared row needs dbtype /
// time in the enclosing file's import block. The list arm walks the
// row's carried element type recursively; every other arm delegates to
// a per-kind test on the row's emitted Go type.
func columnNeedsImports(f preparedRow) (needDbtype, needTime bool) {
	switch f.Kind {
	case columnNode, columnEdge, columnEdgeUnion:
		// edgeUnion decode type-asserts dbtype.Relationship (§5.5); the
		// column's emitted Go type is the sealed interface (not a
		// dbtype.* text), so goTypeNeedsImports does not fire and the
		// need is declared here.
		return true, false
	case columnTemporal, columnProperty:
		return goTypeNeedsImports(f.GoType)
	case columnScalar, columnAny:
		return false, false
	case columnList:
		// Walk the element chain for any nested carrier requirement.
		var walk func(resolver.ResolvedType) (bool, bool)
		walk = func(t resolver.ResolvedType) (bool, bool) {
			switch tt := t.(type) {
			case resolver.ResolvedProperty:
				ty, ok := goType(tt.Type)
				if !ok {
					return false, false
				}
				return goTypeNeedsImports(ty)
			case resolver.ResolvedNode, resolver.ResolvedEdge, resolver.ResolvedEdgeUnion:
				return true, false
			case resolver.ResolvedTemporal:
				return goTypeNeedsImports(temporalGoType(tt.Kind))
			case resolver.ResolvedList:
				return walk(tt.Element)
			}
			return false, false
		}
		return walk(f.ListElem)
	}
	return false, false
}

// goTypeNeedsImports reports whether a Go type text names dbtype or
// time. Both are single-string prefix checks; the emitted type text
// never nests dbtype/time except through the list arm (which walks
// element-wise above).
func goTypeNeedsImports(ty string) (bool, bool) {
	needDbtype := strings.HasPrefix(ty, "dbtype.")
	needTime := ty == "time.Time"
	return needDbtype, needTime
}

// driverCarrier picks the neo4j.GetRecordValue[T] type for a Go type
// that C1 wants to emit. Integer widths widen to int64; float widths
// widen to float64; string / bool pass through. The caller narrows via
// a Go conversion.
func driverCarrier(goType string) string {
	switch goType {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "int64"
	case "float32", "float64":
		return "float64"
	default:
		return goType
	}
}

// paramFieldName derives the Params-struct field name for a parameter
// whose annotation was $<raw> (spec §4.2). Splits on '_', capitalises
// the first rune of each non-empty segment, preserves internal case of
// non-ALL-CAPS segments; ALL-CAPS segments stay ALL-CAPS.
func paramFieldName(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	segments := strings.Split(raw, "_")
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		if isAllCaps(seg) {
			b.WriteString(seg)
			continue
		}
		runes := []rune(seg)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	return b.String()
}

// isAllCaps reports whether every letter rune in s is uppercase (and s
// contains at least one letter). ALL-CAPS segments preserve their case
// under §4.2 so acronyms like API / URL / ID keep their form.
func isAllCaps(s string) bool {
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return hasLetter
}

// rowFieldName derives the Row-struct field name for a column whose
// text is one of the two clean shapes (spec §4.3). Returns "", false
// for anything else — the caller emits ErrAliasRequired.
func rowFieldName(colText string) (string, bool) {
	if rowBareIdent.MatchString(colText) {
		return paramFieldName(colText), true
	}
	if rowPropAccess.MatchString(colText) {
		dot := strings.IndexByte(colText, '.')
		return paramFieldName(colText[dot+1:]), true
	}
	return "", false
}

// goType maps a resolved property type to its native Go emission (spec
// §5.1). Returns (typeText, ok): ok=false for the eight unrepresentable
// widths (INT128 / INT256 / UINT128 / UINT256 / FLOAT16 / FLOAT128 /
// FLOAT256 / DECIMAL) — caller routes to ErrUnrepresentableWidth naming
// the width. Callers append a leading '*' for nullable columns and
// parameters at emission time. DATE / TIMESTAMP are in-scope at C3 and
// return "dbtype.Date" / "time.Time"; FLOAT32 returns "float32" (the
// carrier-widens-on-encode / narrow-on-decode contract is enforced at
// the emission sites, spec §5.5 / §5.7).
func goType(pt graph.PropertyType) (string, bool) {
	switch pt {
	case graph.TypeString:
		return "string", true
	case graph.TypeBool:
		return "bool", true
	case graph.TypeInt:
		return "int", true
	case graph.TypeInt8:
		return "int8", true
	case graph.TypeInt16:
		return "int16", true
	case graph.TypeInt32:
		return "int32", true
	case graph.TypeInt64:
		return "int64", true
	case graph.TypeUint:
		return "uint", true
	case graph.TypeUint8:
		return "uint8", true
	case graph.TypeUint16:
		return "uint16", true
	case graph.TypeUint32:
		return "uint32", true
	case graph.TypeUint64:
		return "uint64", true
	case graph.TypeFloat, graph.TypeFloat64:
		return "float64", true
	case graph.TypeFloat32:
		return "float32", true
	case graph.TypeDate:
		return "dbtype.Date", true
	case graph.TypeTimestamp:
		return "time.Time", true
	case graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat16, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal:
		// The eight unrepresentable widths — no faithful Go carrier on
		// neo4j-go-driver (v5 and v6 alike). Permanent, per §9 (spec).
		return "", false
	}
	return "", false
}

// temporalGoType maps a resolver Temporal kind to the Go type text C3
// emits (spec §5.1 column-shape table). Every result is a
// dbtype.<Kind> or time.Time — one dispatch on the closed enum.
func temporalGoType(k resolver.Temporal) string {
	switch k {
	case resolver.TemporalDate:
		return "dbtype.Date"
	case resolver.TemporalTime:
		return "dbtype.Time"
	case resolver.TemporalLocalTime:
		return "dbtype.LocalTime"
	case resolver.TemporalDateTime:
		return "time.Time"
	case resolver.TemporalLocalDateTime:
		return "dbtype.LocalDateTime"
	case resolver.TemporalDuration:
		return "dbtype.Duration"
	}
	// Unreachable: Temporal is a closed enum.
	return "any"
}

// scalarGoType maps a resolver Scalar kind to the Go type text C3
// emits (spec §5.1 column-shape table). Bool / Int / Float / String
// bridge to the driver's native carriers; Null → any (the openCypher
// null literal is legal-but-pointless projection); Map → map[string]any.
func scalarGoType(k resolver.Scalar) string {
	switch k {
	case resolver.ScalarBool:
		return "bool"
	case resolver.ScalarInt:
		return "int64"
	case resolver.ScalarFloat:
		return "float64"
	case resolver.ScalarString:
		return "string"
	case resolver.ScalarNull:
		return "any"
	case resolver.ScalarMap:
		return "map[string]any"
	}
	return "any"
}

// resolvedListGoType derives the Go type text for a ResolvedType leaf
// or nested ResolvedList (spec §2.2, §4.7). Returns (text, err):
// err wraps ErrUnrepresentableWidth for a leaf property width that is
// unrepresentable. A ResolvedList element recurses; every other leaf is
// one dispatch on the ResolvedType sum. C5 widens the sum to admit
// ResolvedEdgeUnion leaves — the derived text is the ambient query's
// synthesised interface name (<queryName><columnField>); the extra
// parameters are inert for every non-edgeUnion arm.
func resolvedListGoType(t resolver.ResolvedType, entities []preparedEntity, entityIndex map[entityLookupKey]int, queryName, columnField string) (string, error) {
	switch tt := t.(type) {
	case resolver.ResolvedProperty:
		ty, ok := goType(tt.Type)
		if !ok {
			return "", fmt.Errorf("%w: list element has unrepresentable property width %s", ErrUnrepresentableWidth, tt.Type)
		}
		return ty, nil
	case resolver.ResolvedNode:
		idx, ok := entityIndex[entityLookupKey{Kind: entityNode, Labels: tt.Labels}]
		if !ok {
			return "", fmt.Errorf("%w: list element references unknown node type %q", ErrOutOfC6Scope, string(tt.Labels))
		}
		return entities[idx].Name, nil
	case resolver.ResolvedEdge:
		idx, ok := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: tt.EdgeKey}]
		if !ok {
			return "", fmt.Errorf("%w: list element references unknown edge type %s -[:%s]-> %s", ErrOutOfC6Scope, string(tt.EdgeKey.Source), string(tt.EdgeKey.Label), string(tt.EdgeKey.Target))
		}
		return entities[idx].Name, nil
	case resolver.ResolvedEdgeUnion:
		// C5 recursion arm: leaf synthesises the same interface name the
		// top-level Row field would emit (§4.7). Phase A already asserted
		// len(EdgeKeys) >= 2 and cache membership for the top-level column;
		// list-of-edgeUnion at Phase A calls into this recursion path only
		// for the validity probe, and Phase B repeats the derivation at
		// emission time — so a schema-cache miss at the leaf indicates a
		// resolver-produced foreign edge and routes through ErrOutOfC6Scope
		// for uniformity with the top-level edgeUnion admission arm.
		if len(tt.EdgeKeys) < 2 {
			return "", fmt.Errorf("%w: list element resolved as edgeUnion with only %d candidate(s) — resolver invariant violated (expected >= 2)", ErrOutOfC6Scope, len(tt.EdgeKeys))
		}
		for _, ek := range tt.EdgeKeys {
			if _, ok := entityIndex[entityLookupKey{Kind: entityEdge, EdgeKey: ek}]; !ok {
				return "", fmt.Errorf("%w: list element edgeUnion candidate %s -[:%s]-> %s not declared by schema", ErrOutOfC6Scope, string(ek.Source), string(ek.Label), string(ek.Target))
			}
		}
		return queryName + columnField, nil
	case resolver.ResolvedTemporal:
		return temporalGoType(tt.Kind), nil
	case resolver.ResolvedScalar:
		return scalarGoType(tt.Kind), nil
	case resolver.ResolvedUnknown:
		return "any", nil
	case resolver.ResolvedList:
		inner, err := resolvedListGoType(tt.Element, entities, entityIndex, queryName, columnField)
		if err != nil {
			return "", err
		}
		return "[]" + inner, nil
	}
	return "", fmt.Errorf("%w: list element has unknown resolved type %s", ErrOutOfC6Scope, t.String())
}

// lowerFirstRune lowercases the first rune of s. Used for the
// package-internal query-text const name (Bare in preparedQuery) and
// for single-parameter argument names.
func lowerFirstRune(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}
