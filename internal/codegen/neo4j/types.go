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
// parameters at emission time. FLOAT32 returns "float32" (the
// carrier-widens-on-encode / narrow-on-decode contract is enforced at
// the emission sites, spec §5.5 / §5.7).
//
// DATE / TIME / LOCAL TIME / DURATION return the gqlc-owned neutral
// carriers "Date" / "Time" / "LocalTime" / "Duration", declared in the
// generated package's own temporal.go and bridged to dbtype by the
// unexported helpers in temporal_neo4j.go (ADR 0033). They name no
// package, because they are declared alongside the code that uses them.
// TIMESTAMP stays "time.Time": the stdlib type is already neutral and
// models an instant without residue. DURATION collapses its
// (YEAR TO MONTH) vs (DAY TO SECOND) qualifier onto a single Duration
// carrying Months / Days / Seconds / Nanos (see ADR 0002 Consequences).
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
		return "Date", true
	case graph.TypeTime:
		return "Time", true
	case graph.TypeLocalTime:
		return "LocalTime", true
	case graph.TypeTimestamp:
		return "time.Time", true
	case graph.TypeDuration:
		return "Duration", true
	case graph.TypeAnyPropertyValue:
		return "any", true
	case graph.TypeList:
		// Intercepted by the Kind() guard above; unreachable here.
		// Listed so the exhaustive linter sees the full constant set.
		return "[]any", true
	case graph.TypeAnyRecord:
		// Refused by prepare.go's kind walk before any table is asked
		// (ErrUnimplementedTypeKind, ADR 0039), so this is unreachable —
		// listed for the exhaustive linter, as graph.TypeList is. The
		// false is fail-closed rather than an answer: a record has no
		// emission on any backend, so no width claim here would be true.
		return "", false
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

// StorableProperty refuses a list whose element is itself a list, and
// admits everything else.
//
// This is the storage axis, not the carrier axis: Property answers
// "[][]int16" for LIST<LIST<INT16>> and is right to, and this backend
// emits a working recursive decode for a nested list arriving as a
// QUERY VALUE. What refuses it is the server, which stores a property
// value only if it is a scalar or a flat list of scalars and answers a
// nested write with "Collections containing collections can not be
// stored in properties" — measured against the pinned image by
// TestNeo4jRefusesANestedListStoredProperty (ADR 0035, bd gqlc-v0gk).
// Emitting for such a property would hand the author a struct field no
// write could ever fill, so it fails at generation, where it can name
// the property.
//
// Elem() strips the NOT NULL suffix, so LIST<LIST<INT16> NOT NULL> is
// caught the same as LIST<LIST<FLOAT32>>; a depth-3 list is caught at
// its outer level, its element being a list; LIST<LIST<ANY VALUE>> is
// caught for the same reason. LIST<ANY VALUE> is ADMITTED and can carry
// a nested list at runtime, which no static check can see — that write
// fails at the server as it does today (ADR 0035 names the limit).
func (typeMap) StorableProperty(pt graph.PropertyType) bool {
	if pt.Kind() != graph.KindList {
		return true
	}
	return pt.Elem().Kind() != graph.KindList
}

// Temporal maps a resolver Temporal kind to the Go type text C3 emits
// (spec §5.1 column-shape table). Returns (typeText, ok): ok=false
// routes the caller to ErrUnrepresentableTemporal naming the kind.
// Every kind of the enum has a carrier — the four neutral ones plus
// time.Time for the zoned datetime (ADR 0033) — so every arm answers
// ok=true and this backend never takes that channel.
func (typeMap) Temporal(k resolver.Temporal) (string, bool) {
	switch k {
	case resolver.TemporalDate:
		return "Date", true
	case resolver.TemporalTime:
		return "Time", true
	case resolver.TemporalLocalTime:
		return "LocalTime", true
	case resolver.TemporalDateTime:
		return "time.Time", true
	case resolver.TemporalLocalDateTime:
		return "LocalDateTime", true
	case resolver.TemporalDuration:
		return "Duration", true
	}
	// Only a value converted in from outside resolver.Temporal's
	// vocabulary reaches here; refusing beats guessing a carrier for a
	// kind the resolver never named.
	return "", false
}

// Scalar maps a resolver Scalar kind to the Go type text C3 emits (spec
// §5.1 column-shape table). Bool / Int / Float / String bridge to the
// driver's native carriers; Null → any (the openCypher null literal is
// legal-but-pointless projection); Map → map[string]any. Every arm is the
// Go shape of a value the driver's record vocabulary already carries.
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
	// Only a value converted in from outside resolver.Scalar's vocabulary
	// reaches here; projecting it undecoded beats guessing a Go type for
	// a kind the resolver never named.
	return "any"
}

// driverCarrier picks the neo4j.GetRecordValue[T] type for a Go type
// the emission wants to produce. Integer widths widen to int64; float
// widths widen to float64; string / bool pass through. The caller
// narrows via a Go conversion.
//
// A slice widens to []any, which is the only shape a Bolt driver has for
// one. neo4j.PropertyValue admits []byte and []any and no other slice,
// GetProperty's own doc says "any property array value other than byte
// array is typed as []any", and the hydrator builds exactly that:
// `func (h *hydrator) array() []any`. So the element widths the schema
// declared are not on the wire to be asserted — a LIST<STRING> arrives
// as []any holding strings, and narrowing it is per element rather than
// whole. []byte is the exception because BYTES is the one width the
// driver does hand back as a Go slice of its own.
func driverCarrier(goType string) string {
	if isSliceType(goType) {
		return "[]any"
	}
	switch goType {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "int64"
	case "float32", "float64":
		return "float64"
	case "Date", "Time", "LocalTime", "LocalDateTime", "Duration":
		// The neutral temporal carriers (ADR 0033). The driver still
		// speaks dbtype on both wires, so the carrier is the dbtype
		// counterpart — but unlike every other arm here the two are not
		// conversion-compatible, and narrowExpr / widenExpr route them
		// through the emitted to<X> / from<X> helpers instead of a Go
		// conversion.
		return "dbtype." + goType
	default:
		return goType
	}
}
