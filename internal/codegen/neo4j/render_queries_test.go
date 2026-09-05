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
	"github.com/areqag/gqlc/internal/graph"
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

// TestNarrowsANumericWidthIgnoresRecords holds the gate that decides
// whether the emitted package declares narrowInt and narrowFloat32 at
// all, against the one shape that looks like a narrowing and is not.
//
// A declared record carries WIDER than it is spelled — struct{...} on
// the emitted surface, map[string]any on the wire — which is the exact
// test every arm of this gate uses to recognise an integer that needs
// checking. But a record's narrowing is its own emitted helper, so a
// schema whose only wide width is a record must be handed neither
// numeric helper. This is not a cosmetic over-emission: an unexported
// function nothing calls fails the emitted package's own lint fence, so
// the wrong answer reds a fixture rather than adding a dead line.
//
// The controls are the whole test. A gate that had stopped answering
// yes to anything would pass the record rows alone, so int32 and
// float32 are asserted beside them, and each of the two helpers is
// asserted separately because they are gated separately — the `math`
// import rides on the float one.
func TestNarrowsANumericWidthIgnoresRecords(t *testing.T) {
	rec := graph.RecordOf([]graph.RecordField{{Name: "zip", Type: graph.TypeInt32, NotNull: true}})
	recText, ok := neo4j.TypeMap{}.Property(rec)
	require.True(t, ok, "this driver carries a record, or the rows below are about an unrepresentable width")

	field := func(goType string, width graph.PropertyType) []codegen.Entity {
		return []codegen.Entity{{Name: "Blob", Fields: []codegen.EntityField{
			{PropName: "p", Field: "P", GoType: goType, Width: width},
		}}}
	}

	tests := []struct {
		name     string
		entities []codegen.Entity
		ints     bool
		floats   bool
	}{{
		// The row this test exists for. The record's own field is an
		// INT32, and a record does NOT narrow it through narrowInt at
		// the property site — the emitted decode helper for the record
		// does, and it is emitted with its own gate.
		name:     "a record does not demand either numeric helper",
		entities: field(recText, rec),
	}, {
		name:     "a list of records does not either",
		entities: field("[]"+recText, graph.ListOf(rec, false)),
	}, {
		name:     "an integer width still demands narrowInt",
		entities: field("int32", graph.TypeInt32),
		ints:     true,
	}, {
		name:     "float32 still demands narrowFloat32 and not narrowInt",
		entities: field("float32", graph.TypeFloat32),
		floats:   true,
	}, {
		// A width already equal to its own carrier has never demanded
		// either, and it is here as the negative control that is NOT a
		// record: it proves the two record rows above are not simply
		// riding the "nothing to narrow" answer everything gets.
		name:     "a width that is its own carrier demands neither",
		entities: field("string", graph.TypeString),
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ints, floats := neo4j.NarrowsANumericWidth(tt.entities, nil)
			require.Equal(t, tt.ints, ints, "narrowInt")
			require.Equal(t, tt.floats, floats, "narrowFloat32")
		})
	}
}

// TestParamBindExprRecords pins the bind expression for a declared
// record in all four shapes a parameter can take it in, and two of the
// four were WRONG before this test existed.
//
// The reason is the one TestParamBindExprSlices already states, now with
// a second kind making it true. packV's reflect.Struct arm and packX's
// reflect.Ptr->Struct arm both hand off to packStruct, whose cases are
// dbtype.Point2D/3D, time.Time and the five dbtype temporals and whose
// default raises UnsupportedTypeError. The anonymous struct a declared
// record carries as is not among them. So a record owes a conversion in
// every position the driver would otherwise reach it as a struct —
// exactly the position gqlc's neutral temporal carriers are in, which is
// why the helper set mirrors from<X>/from<X>Ptr/from<X>List/from<X>ListPtr
// one for one.
//
// What the driver DOES take is map[string]any: packX's reflect.Map arm
// dispatches it to packMap, which packs each value back through packX,
// so a record encoded to a map nests to any depth with no further help.
// Both directions read off v5.28.4 and v6.2.0.
//
// The two rows that were broken:
//
//   - the NULLABLE record, because paramBindExpr's nullable arm passes
//     every non-temporal pointer through bare, and *struct{...} reaches
//     packStruct one indirection later;
//   - the LIST of records, because sliceParamBindExpr returns bare for
//     any leaf that is not a temporal carrier, and packV walks the slice
//     into packStruct element by element.
//
// Neither failed at generation. Both emitted a package that compiled and
// then refused the write at run time, which is the shape of defect this
// bead's spec §1 constraint 3 forbids.
func TestParamBindExprRecords(t *testing.T) {
	width := graph.RecordOf([]graph.RecordField{
		{Name: "city", Type: graph.TypeString},
		{Name: "zip", Type: graph.TypeInt32},
	})
	suffix := codegen.RecordHelperSuffix(width)
	// Pinned as a literal, and the literal is CROSS-CHECKED rather than
	// copied out of a run: the canonical encoding is
	// "RECORD<city STRING,zip INT32>", and an independent
	// sha256 of those bytes gives 77890bd035f2... whose first four bytes
	// are this suffix. A pin lifted from whatever the code printed would
	// agree with any hash the code happened to compute; this one fails if
	// either the digest or the canonical encoding moves, and every golden
	// naming these helpers is downstream of it.
	require.Equal(t, "Record77890bd0", suffix)

	structText, ok := neo4j.TypeMap{}.Property(width)
	require.True(t, ok)
	listWidth := graph.ListOf(width, false)
	listText, ok := neo4j.TypeMap{}.Property(listWidth)
	require.True(t, ok)

	tests := []struct {
		name     string
		goType   string
		width    graph.PropertyType
		nullable bool
		want     string
	}{
		{"record", structText, width, false, "encode" + suffix + "(arg)"},
		{"nullable record", structText, width, true, "encode" + suffix + "Ptr(arg)"},
		{"record list", listText, listWidth, false, "encode" + suffix + "List(arg)"},
		{"nullable record list", listText, listWidth, true, "encode" + suffix + "ListPtr(arg)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := codegen.Param{RawName: "p", Field: "P", GoType: tt.goType, Width: tt.width, Nullable: tt.nullable}
			require.Equal(t, tt.want, neo4j.ParamBindExpr(f, "arg"))
		})
	}
}

// TestParamBindExprLeavesTheUndeclaredRecordBare is the control the four
// rows above need. RECORD<ANY> is KindRecord too, and a bind that routed
// on the kind alone would name a helper codegen.RecordEncodings
// deliberately emits nothing for — a package that does not compile.
//
// It binds bare and is right to: its carrier IS map[string]any, which is
// the shape packX's reflect.Map arm already takes. LIST<RECORD<ANY>>
// likewise: []map[string]any walks element by element into that same arm.
func TestParamBindExprLeavesTheUndeclaredRecordBare(t *testing.T) {
	tests := []struct {
		name     string
		goType   string
		width    graph.PropertyType
		nullable bool
	}{
		{"undeclared record", "map[string]any", graph.TypeAnyRecord, false},
		{"nullable undeclared record", "map[string]any", graph.TypeAnyRecord, true},
		{"list of undeclared records", "[]map[string]any", graph.ListOf(graph.TypeAnyRecord, false), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := codegen.Param{RawName: "p", Field: "P", GoType: tt.goType, Width: tt.width, Nullable: tt.nullable}
			require.Equal(t, "arg", neo4j.ParamBindExpr(f, "arg"))
		})
	}
}

// TestIsDeclaredRecordNeedsBothHalves screens the predicate every record
// arm guards on, one falsified half per row.
//
// The predicate is codegen's and shared by both backends. It is screened
// from here rather than from beside its declaration because the carrier
// the control row is asked about is THIS backend's: that row runs
// typeMap.Property, so a table that stopped answering a declared record
// with a struct text fails here rather than leaving a green row about a
// text no emission produces. The age package holds the same row over its
// own table.
//
// It is asked directly rather than through a call site because ONE of
// the two halves is not reachable from any schema. typeMap.Property
// produces a `struct`-prefixed carrier only from
// codegen.RecordStructText, which it calls only for KindRecord, so no
// input can present a struct text beside a non-record width — a mutant
// dropping the kind half survives every emission-level test in this
// package, measured. That does not make the half decorative: it is what
// stops a future call site that pairs a carrier with the wrong width
// from deriving a helper name off a non-record, which would name a
// declaration codegen.RecordEncodings never emits. Since the hazard is a
// caller's, the guard is screened at the predicate, where the mismatched
// pair can be supplied.
//
// The text half is reachable and its falsifier is RECORD<ANY>, which is
// KindRecord and carries as map[string]any.
func TestIsDeclaredRecordNeedsBothHalves(t *testing.T) {
	declared := graph.RecordOf([]graph.RecordField{{Name: "city", Type: graph.TypeString}})
	structText, ok := neo4j.TypeMap{}.Property(declared)
	require.True(t, ok)

	require.True(t, codegen.IsDeclaredRecord(structText, declared),
		"the control: a declared record paired with its own carrier is the whole point")

	require.False(t, codegen.IsDeclaredRecord("map[string]any", graph.TypeAnyRecord),
		"RECORD<ANY> is KindRecord, so the kind alone would admit it — and RecordEncodings emits it no helper to name")

	require.False(t, codegen.IsDeclaredRecord(structText, graph.TypeString),
		"a struct text beside a non-record width is a caller's mistake; naming a helper off it would invent a declaration")
}
