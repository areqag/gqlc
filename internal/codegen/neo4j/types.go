package neo4j

import (
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
)

// typeMap is the driver's Go-type table (spec §5.1) the shared phases
// read. Stateless: every entry is a pure function of the resolved type.
type typeMap struct{}

// Property maps a resolved property type to its native Go emission (spec
// §5.1). Returns (typeText, ok): ok=false for the eight unrepresentable
// widths (INT128 / INT256 / UINT128 / UINT256 / FLOAT16 / FLOAT128 /
// FLOAT256 / DECIMAL) — caller routes to ErrUnrepresentableWidth naming
// the width. Callers append a leading '*' for nullable columns and
// parameters at emission time. DATE / TIMESTAMP are in-scope at C3 and
// return "dbtype.Date" / "time.Time"; FLOAT32 returns "float32" (the
// carrier-widens-on-encode / narrow-on-decode contract is enforced at
// the emission sites, spec §5.5 / §5.7). BYTES / TIME / LOCAL TIME /
// DURATION add four dbtype-carrying arms; DURATION collapses its
// (YEAR TO MONTH) vs (DAY TO SECOND) qualifier onto a single
// dbtype.Duration, which the driver represents as one struct with
// Months / Days / Seconds / Nanos fields (see ADR 0002 Consequences).
func (t typeMap) Property(pt graph.PropertyType) (string, bool) {
	if pt.Kind() == graph.KindList {
		elemTy, ok := t.Property(pt.Elem())
		if !ok {
			return "", false
		}
		return "[]" + elemTy, true
	}
	switch pt {
	case graph.TypeString:
		return "string", true
	case graph.TypeBytes:
		return "[]byte", true
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
	case graph.TypeTime:
		return "dbtype.Time", true
	case graph.TypeLocalTime:
		return "dbtype.LocalTime", true
	case graph.TypeTimestamp:
		return "time.Time", true
	case graph.TypeDuration:
		return "dbtype.Duration", true
	case graph.TypeAnyPropertyValue:
		return "any", true
	case graph.TypeList:
		// Intercepted by the Kind() guard above; unreachable here.
		// Listed so the exhaustive linter sees the full constant set.
		return "[]any", true
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

// Temporal maps a resolver Temporal kind to the Go type text C3 emits
// (spec §5.1 column-shape table). Every result is a dbtype.<Kind> or
// time.Time — one dispatch on the closed enum.
func (typeMap) Temporal(k resolver.Temporal) string {
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

// Scalar maps a resolver Scalar kind to the Go type text C3 emits (spec
// §5.1 column-shape table). Bool / Int / Float / String bridge to the
// driver's native carriers; Null → any (the openCypher null literal is
// legal-but-pointless projection); Map → map[string]any.
func (typeMap) Scalar(k resolver.Scalar) string {
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

// driverCarrier picks the neo4j.GetRecordValue[T] type for a Go type
// the emission wants to produce. Integer widths widen to int64; float
// widths widen to float64; string / bool pass through. The caller
// narrows via a Go conversion.
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
