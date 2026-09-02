package neo4j_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
)

// TestParamBindExprSlices pins the driver-binding expression for every
// slice-shaped parameter type §5.1 can produce.
//
// driverCarrier answers a DECODE question — which neo4j.GetRecordValue[T]
// the driver hands a value back in — and for a list that answer is []any,
// because the hydrator builds `func (h *hydrator) array() []any`. Binding
// routed on that answer, so a list parameter emitted `[]any(arg)`, which
// is not a Go conversion at all: `cannot convert arg (variable of type
// []Date) to type []any`.
//
// The encode question has its own answer, read off the driver's packer at
// v5.28.4 and v6.2.0 (neo4j/internal/bolt/outgoing.go). packX's
// reflect.Slice arm ends in a default that walks any slice element by
// element through packV, and packV handles every scalar kind the type map
// emits. So a slice needs no conversion to reach the wire — the widening
// the scalar arms do per value, the packer already does per element.
//
// The one thing packV cannot reach is a struct the driver does not know:
// packStruct's cases are dbtype.Point2D/3D, time.Time and the five dbtype
// temporals, and its default raises UnsupportedTypeError. gqlc's own
// neutral carriers (ADR 0033) are not among them, so a slice whose leaf is
// one of those five is the sole slice shape that still owes a conversion.
func TestParamBindExprSlices(t *testing.T) {
	tests := []struct {
		name     string
		goType   string
		nullable bool
		want     string
	}{
		// Bare: the packer walks these element by element itself.
		{"string list", "[]string", false, "arg"},
		{"narrow int list", "[]int32", false, "arg"},
		{"narrow float list", "[]float32", false, "arg"},
		{"bool list", "[]bool", false, "arg"},
		{"driver-native temporal list", "[]time.Time", false, "arg"},
		{"nested string list", "[][]string", false, "arg"},

		// Already bare before this fix, and must stay so: neither is a
		// list width. []byte is BYTES, which the driver hands back and
		// takes as a Go slice of its own; []any is LIST<ANY VALUE>,
		// already the driver's own array carrier.
		{"bytes", "[]byte", false, "arg"},
		{"any list", "[]any", false, "arg"},

		// Nullable slices bind the pointer through: packX's reflect.Ptr
		// arm indirects to the slice and packs it, and a nil pointer
		// packs as the Cypher null the schema declared.
		{"nullable string list", "[]string", true, "arg"},
		{"nullable nested list", "[][]string", true, "arg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := codegen.Param{RawName: "p", Field: "P", GoType: tt.goType, Nullable: tt.nullable}
			require.Equal(t, tt.want, neo4j.ParamBindExpr(f, "arg"))
		})
	}
}

// TestParamBindExprTemporalLists pins the one slice shape that does owe a
// conversion. dbtype has no constructor for a list, so the widen is per
// element into the driver's own array carrier — []any — mirroring the
// per-element narrow the decode side has had since walkListElemBody.
//
// Bound at the leaf, not at the top: the emitted helper for a depth-2 list
// calls the depth-1 helper, so only the leaf conversion is ever written
// out once per carrier.
func TestParamBindExprTemporalLists(t *testing.T) {
	tests := []struct {
		name     string
		goType   string
		nullable bool
		want     string
	}{
		{"date list", "[]Date", false, "fromDateList(arg)"},
		{"time list", "[]Time", false, "fromTimeList(arg)"},
		{"local time list", "[]LocalTime", false, "fromLocalTimeList(arg)"},
		{"local date time list", "[]LocalDateTime", false, "fromLocalDateTimeList(arg)"},
		{"duration list", "[]Duration", false, "fromDurationList(arg)"},
		{"nested date list", "[][]Date", false, "fromDateList2(arg)"},
		{"nullable date list", "[]Date", true, "fromDateListPtr(arg)"},
		{"nullable nested date list", "[][]Date", true, "fromDateList2Ptr(arg)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := codegen.Param{RawName: "p", Field: "P", GoType: tt.goType, Nullable: tt.nullable}
			require.Equal(t, tt.want, neo4j.ParamBindExpr(f, "arg"))
		})
	}
}

// TestParamBindExprUnsignedWidthsReachTheDriverUnconverted pins the bind
// expression for every integer width, and exists for the two rows that
// change: uint64 and uint.
//
// The driver already refuses a uint64 the signed wire cannot hold.
// packX and packV both route `case reflect.Uint64, reflect.Uint` to
// packer.Uint64, which opens with checkOverflowInt — `if i >
// math.MaxInt64` sets an OverflowError, and outgoing.end hands that to
// onPackErr, which fails the connection rather than sending. Read at
// v5.28.4 (packstream/packer.go:93-96, 260-264; bolt/outgoing.go:377-378,
// 481-482) and identical at v6.2.0.
//
// So the defect was never a missing guard. Emitting int64(arg) performed
// the wrap in OUR code and handed the driver an already-negative int64,
// where a check that only inspects the uint64 entry point cannot see it —
// gqlc disarmed a refusal the driver was offering. The nullable and list
// arms never had the bug precisely BECAUSE they bind bare, so binding
// bare here makes the three arms agree instead of adding a fourth
// mechanism (bd gqlc-tzjqu).
//
// The narrower unsigned widths keep the conversion. Every value of
// uint8, uint16 and uint32 fits int64, so their widen cannot lose one
// and there is nothing for a check to catch; changing them would churn
// goldens to fix nothing.
func TestParamBindExprUnsignedWidthsReachTheDriverUnconverted(t *testing.T) {
	tests := []struct {
		name   string
		goType string
		want   string
	}{
		// The two widths whose range is not a subset of int64's.
		{"uint64", "uint64", "arg"},
		{"uint", "uint", "arg"},

		// Unchanged: each of these always fits the carrier.
		{"uint8", "uint8", "int64(arg)"},
		{"uint16", "uint16", "int64(arg)"},
		{"uint32", "uint32", "int64(arg)"},
		{"int8", "int8", "int64(arg)"},
		{"int16", "int16", "int64(arg)"},
		{"int32", "int32", "int64(arg)"},
		{"int", "int", "int64(arg)"},

		// Its own carrier, so it is assigned bare at the call site.
		{"int64", "int64", "arg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := codegen.Param{RawName: "p", Field: "P", GoType: tt.goType}
			require.Equal(t, tt.want, neo4j.ParamBindExpr(f, "arg"))
		})
	}
}
