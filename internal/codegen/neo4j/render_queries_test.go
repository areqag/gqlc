package neo4j_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/queryfile"
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
// Every row is a list of depth 1, which is the only depth the helper is
// reached at: a parameter's Go type is its property's, and ADR 0035
// refuses a nested declared list as a stored property. LIST<BYTES> does
// emit [][]byte, but "byte" is no temporal carrier, so ParamBindExpr
// hands that back unconverted — the [][]string rows above are the
// standing witness for that arm.
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
		{"nullable date list", "[]Date", true, "fromDateListPtr(arg)"},
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

// TestACardinalityNamingNoMemberEmitsAMethodWithNoBody is the neo4j half of
// what gqlc-f5dkc witnessed on the AGE side, and it is written against the
// dispatch this bead replaced: an if/else chain whose final `else` answered
// for CardinalityMany and, identically, for every member added after it.
//
// `exhaustive` cannot see an if/else, so the AGE switch's linter cover had no
// counterpart here — one backend was checked and the other only looked it.
// That is the asymmetry gqlc-h08nx is about, and this test is the part of the
// claim that does not depend on a linter running.
//
// The input is assembled rather than parsed, because the same two closed walls
// stand in front of this switch as in front of AGE's: queryfile's cardinality
// annotation yields the three members or is refused, and codegen's phaseAAdmit
// admits those three by name and routes every other value through
// ErrInvalidCardinality (prepare.go:675). neo4j's generate calls
// codegen.Prepare (generate.go:52) before it renders, so a Cardinality naming
// no member cannot reach writeMethod through Generate at all. What is pinned
// here is a backstop behind that gate, not a diagnostic a real input meets.
//
// The observable is two halves, for the reason the AGE test records: a method
// with no body compiles fine unless it declares results, so "no body" only
// means "does not compile" alongside "declares results". Both are asserted.
//
// The named-member row is the control, and it is CardinalityOne rather than
// CardinalityExec deliberately — :one renders the same results-plus-error
// signature shape, so the two rows differ only in whether a body was written.
func TestACardinalityNamingNoMemberEmitsAMethodWithNoBody(t *testing.T) {
	for _, tt := range []struct {
		name        string
		cardinality queryfile.Cardinality
		wantBody    bool
	}{
		{"a cardinality naming no member", queryfile.Cardinality(7), false},
		{"a named member, for contrast", queryfile.CardinalityOne, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := codegen.Query{
				NamedQuery: codegen.NamedQuery{
					Name:        "Q",
					SourceText:  "MATCH (p:Person) RETURN p.name",
					Cardinality: tt.cardinality,
				},
				MethodName: "Q",
				Bare:       "q",
				RowFields: []codegen.Row{
					{ColumnName: "name", Field: "Name", GoType: "string", Kind: codegen.ColumnProperty},
				},
			}
			var b strings.Builder
			neo4j.WriteMethod(&b, p)
			fn := methodDecl(t, "package p\n\n"+b.String(), "Q")

			require.NotNil(t, fn.Type.Results,
				"the emitted method declares no results, so an empty body would compile "+
					"and the claim's whole mechanism is absent")
			require.NotEmpty(t, fn.Type.Results.List,
				"the emitted method's result list is empty, so an empty body would compile")

			if !tt.wantBody {
				require.Empty(t, fn.Body.List,
					"the emission wrote a body for a Cardinality naming no member, so the "+
						"dispatch answered for it — which is what the if/else chain's final "+
						"`else` did, silently giving it the :many body")
				return
			}
			require.NotEmpty(t, fn.Body.List,
				"a named member emitted no body either, so the row above is measuring the "+
					"renderer being broken rather than the unnamed member being unanswered")
		})
	}
}

// methodDecl returns the named method declaration from one rendered method,
// wrapped in a package clause so it parses. It fails rather than returning
// nil, so a caller's assertions are never read against a method the emission
// did not write.
func methodDecl(t *testing.T, src, name string) *ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "rendered.go", src, 0)
	require.NoError(t, err, "the emission is not parseable Go:\n%s", src)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv != nil && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("the emission declares no method %s:\n%s", name, src)
	return nil
}
