package query_test

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query"
)

// The Type sum is the type vocabulary the resolver reads (Stage 6
// spec §3). Each variant carries a stringer that is the single source of the
// wire tag, mirroring AggregateFunc / UnionKind.

func TestTypeIntString(t *testing.T) {
	// TypeInt is the integer scalar: an integer literal, arithmetic over integer
	// operands. Its wire tag is "int".
	require.Equal(t, "int", query.TypeInt{}.String())
	var _ query.Type = query.TypeInt{}
}

// TestTypeListString pins TypeList's stringer: "list<" + elem.String() + ">"
// so a typed list is recognisable on the wire ("list<int>"), an untyped list
// falls back to "list<unknown>", and a nested list composes ("list<list<int>>").
func TestTypeListString(t *testing.T) {
	require.Equal(t, "list<int>", query.NewTypeList(query.TypeInt{}).String())
	require.Equal(t, "list<string>", query.NewTypeList(query.TypeString{}).String())
	require.Equal(t, "list<unknown>", query.NewTypeList(query.TypeUnknown{}).String())
	// Nested: composition through the element's stringer.
	require.Equal(t, "list<list<int>>",
		query.NewTypeList(query.NewTypeList(query.TypeInt{})).String())

	var _ query.Type = query.NewTypeList(query.TypeInt{})
}

// TestTypeListElement pins the accessor: the constructor is total and the
// element type is retrievable, so the resolver can walk into a typed list.
func TestTypeListElement(t *testing.T) {
	l := query.NewTypeList(query.TypeInt{})
	require.Equal(t, query.TypeInt{}, l.Element())
}

// TestTypeListNilElementInvariant pins the "invariant lives in one place" fix:
// NewTypeList is the sole writer of the element field, so a nil element passed
// in must be normalised to TypeUnknown before the value escapes. String() and
// MarshalJSON() then read the element directly with no fallback of their own —
// there is no other TypeList value that can exist.
func TestTypeListNilElementInvariant(t *testing.T) {
	l := query.NewTypeList(nil)
	require.Equal(t, query.TypeUnknown{}, l.Element())
	require.Equal(t, "list<unknown>", l.String())
	b, err := json.Marshal(l)
	require.NoError(t, err)
	require.JSONEq(t, `"list<unknown>"`, string(b))
}

// TestRefProjectionType pins the Stage-6 accessor: RefProjection carries its
// result type as the fourth exported datum (after variable, property, and the
// Ref shape it already had). A whole-entity node ref types as TypeNode; the
// listener passes the correct type via the widened constructor.
func TestRefProjectionType(t *testing.T) {
	p := query.NewRefProjection(query.Ref{Variable: "n"}, query.TypeNode{})
	require.Equal(t, query.TypeNode{}, p.Type())
}

// --- ExprUse (Stage 6 spec §4) ---

// TestNewExprUse pins the new Use variant: a $param inside a rich expression
// records its enclosing projection's result type and the expression position
// (a projection column vs a predicate). Its own type is inferred by the
// resolver from the enclosing expression.
func TestNewExprUse(t *testing.T) {
	u := query.NewExprUse(query.TypeInt{}, query.ExprInProjection)
	require.Equal(t, query.TypeInt{}, u.EnclosingType())
	require.Equal(t, query.ExprInProjection, u.Position())
	var _ query.Use = u
}

// TestExprPositionString pins the wire tags for ExprPosition. Stage 12 adds
// setValue (SET RHS, a producer position) and deleteTarget (DELETE rich
// target, a consumer position); the four-way split keeps the position axis
// honest across the write set.
func TestExprPositionString(t *testing.T) {
	require.Equal(t, "projection", query.ExprInProjection.String())
	require.Equal(t, "predicate", query.ExprInPredicate.String())
	require.Equal(t, "setValue", query.ExprInSetValue.String())
	require.Equal(t, "deleteTarget", query.ExprInDeleteTarget.String())
}

// TestExprUseMarshalJSON pins the wire encoding: kind=expr with the enclosing
// type and position emitted alongside.
func TestExprUseMarshalJSON(t *testing.T) {
	out, err := json.Marshal(query.NewExprUse(query.TypeInt{}, query.ExprInProjection))
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"expr","enclosingType":"int","position":"projection"}`,
		string(out))
}

// TestNewPropertyUse pins the pre-existing constructor's field wiring —
// the widened constructor delegates to NewPropertyUseAt(ref, 0, 0), so the
// zero-value Part and Branch must round-trip through the accessors. New at
// fvo per §5.1: PropertyUse had no dedicated constructor unit test at branch
// base (only ExprUse did); the widening must not leave the zero side of a Use
// variant it touches uncovered.
func TestNewPropertyUse(t *testing.T) {
	u := query.NewPropertyUse(query.Ref{Variable: "a", Property: "title"})
	require.Equal(t, query.Ref{Variable: "a", Property: "title"}, u.Ref())
	require.Equal(t, 0, u.Part())
	require.Equal(t, 0, u.Branch())
	var _ query.Use = u
}

// TestPropertyUseMarshalJSON pins the wire encoding for the zero-value
// attribution coordinate — the "part" and "branch" keys are ABSENT under
// omit-when-zero. New at fvo per §5.1: same rationale as TestNewPropertyUse;
// PropertyUse had no MarshalJSON test at branch base.
func TestPropertyUseMarshalJSON(t *testing.T) {
	out, err := json.Marshal(query.NewPropertyUse(query.Ref{Variable: "a", Property: "title"}))
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"property","variable":"a","property":"title"}`,
		string(out))
}

// TestNewClauseSlotUse pins the pre-existing constructor's field wiring —
// the widened constructor delegates to NewClauseSlotUseAt(slot, 0, 0). New at
// fvo per §5.1: ClauseSlotUse had no dedicated constructor unit test at
// branch base.
func TestNewClauseSlotUse(t *testing.T) {
	u := query.NewClauseSlotUse(query.ClauseSlotSkip)
	require.Equal(t, query.ClauseSlotSkip, u.Slot())
	require.Equal(t, 0, u.Part())
	require.Equal(t, 0, u.Branch())
	var _ query.Use = u
}

// TestClauseSlotUseMarshalJSON pins the wire encoding for the zero-value
// attribution coordinate — the "part" and "branch" keys are ABSENT under
// omit-when-zero. New at fvo per §5.1: same rationale as
// TestNewClauseSlotUse.
func TestClauseSlotUseMarshalJSON(t *testing.T) {
	out, err := json.Marshal(query.NewClauseSlotUse(query.ClauseSlotSkip))
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"clause-slot","slot":"skip"}`,
		string(out))
}

// TestNewPropertyUseAt pins the widened Use variant per ADR 0008 amendments
// 2026-07-06 (Part) and 2026-07-12 (Branch): both attribution axes carry
// through the constructor, the accessors, and the wire shape as
// omit-when-zero keys (wire convention: additive axes emit
// omit-when-zero-value).
func TestNewPropertyUseAt(t *testing.T) {
	u := query.NewPropertyUseAt(query.Ref{Variable: "a", Property: "title"}, 1, 2)
	require.Equal(t, query.Ref{Variable: "a", Property: "title"}, u.Ref())
	require.Equal(t, 1, u.Part())
	require.Equal(t, 2, u.Branch())

	out, err := json.Marshal(u)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"property","variable":"a","property":"title","part":1,"branch":2}`,
		string(out))
}

// TestNewExprUseAt pins the widened ExprUse variant per ADR 0008 amendments
// 2026-07-06 and 2026-07-12. Same convention as TestNewPropertyUseAt.
func TestNewExprUseAt(t *testing.T) {
	u := query.NewExprUseAt(query.TypeBool{}, query.ExprInPredicate, 2, 1)
	require.Equal(t, query.TypeBool{}, u.EnclosingType())
	require.Equal(t, query.ExprInPredicate, u.Position())
	require.Equal(t, 2, u.Part())
	require.Equal(t, 1, u.Branch())

	out, err := json.Marshal(u)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"expr","enclosingType":"bool","position":"predicate","part":2,"branch":1}`,
		string(out))
}

// TestNewClauseSlotUseAt pins the widened ClauseSlotUse variant per ADR 0008
// amendments 2026-07-06 and 2026-07-12. Same convention.
func TestNewClauseSlotUseAt(t *testing.T) {
	u := query.NewClauseSlotUseAt(query.ClauseSlotSkip, 3, 1)
	require.Equal(t, query.ClauseSlotSkip, u.Slot())
	require.Equal(t, 3, u.Part())
	require.Equal(t, 1, u.Branch())

	out, err := json.Marshal(u)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"clause-slot","slot":"skip","part":3,"branch":1}`,
		string(out))
}

// TestNewExprProjection pins the new Stage-6 variant: ExprProjection is a rich
// scalar-expression projection carrying its result type and the []Ref every
// binding the expression touches. It joins the Projection sum through the
// same sealed interface.
func TestNewExprProjection(t *testing.T) {
	refs := []query.Ref{{Variable: "n", Property: "num"}}
	p := query.NewExprProjection(refs, query.TypeInt{})
	require.Equal(t, refs, p.Refs())
	require.Equal(t, query.TypeInt{}, p.Type())
	var _ query.Projection = p
}

// TestNewExprProjectionAllowsNoRefs pins the degenerate case: a rich literal
// expression like RETURN 1 + 2 references no bindings but is still an
// ExprProjection carrying its computed type (here, TypeInt).
func TestNewExprProjectionAllowsNoRefs(t *testing.T) {
	p := query.NewExprProjection(nil, query.TypeInt{})
	require.Empty(t, p.Refs())
	require.Equal(t, query.TypeInt{}, p.Type())
}

// TestExprProjectionMarshalJSON pins the wire encoding: kind=expr, refs array,
// always-emitted type — same posture as FuncProjection with an extra type key.
func TestExprProjectionMarshalJSON(t *testing.T) {
	out, err := json.Marshal(query.NewExprProjection(
		[]query.Ref{{Variable: "a", Property: "n"}}, query.TypeInt{}))
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"expr","refs":[{"variable":"a","property":"n"}],"type":"int"}`,
		string(out))
}

// TestNewExprProjectionWithAggregateTrue pins the widened Stage-6 variant per
// ADR 0008 amendment 2026-07-06: the ContainsAggregate axis carries through
// the constructor, the accessor, and the wire shape as an omit-when-false key
// (wire convention: additive axes emit omit-when-zero-value).
// Complements TestExprProjectionMarshalJSON (which pins the
// containsAggregate=false zero-value default as an ABSENT key — that test
// stays verbatim).
func TestNewExprProjectionWithAggregateTrue(t *testing.T) {
	refs := []query.Ref{{Variable: "n"}}
	p := query.NewExprProjectionWithAggregate(refs, query.TypeInt{}, true)
	require.Equal(t, refs, p.Refs())
	require.Equal(t, query.TypeInt{}, p.Type())
	require.True(t, p.ContainsAggregate())

	out, err := json.Marshal(p)
	require.NoError(t, err)
	require.JSONEq(t,
		`{"kind":"expr","refs":[{"variable":"n","property":""}],"type":"int","containsAggregate":true}`,
		string(out))
}

// TestFuncProjectionType pins the Stage-6 accessor: FuncProjection carries its
// result type. Function identity is below the boundary (ADR 0005), so the
// listener passes TypeUnknown for any function whose return type it cannot
// compute schema-free — which today is every function.
func TestFuncProjectionType(t *testing.T) {
	p := query.NewFuncProjection([]query.Ref{{Variable: "a", Property: "num"}}, query.TypeUnknown{})
	require.Equal(t, query.TypeUnknown{}, p.Type())
}

// TestAggregateProjectionType pins the accessor: AggregateProjection carries
// its Stage-10 per-aggregate result type — sum over an unknown-typed operand
// stays TypeUnknown (property-typed operands are engine-side per ADR 0003).
func TestAggregateProjectionType(t *testing.T) {
	p := query.NewAggregateProjection(query.AggSum, []query.Ref{{Variable: "n", Property: "num"}}, false, query.TypeUnknown{})
	require.Equal(t, query.TypeUnknown{}, p.Type())
}

// TestLiteralProjectionType pins the Stage-6 accessor: LiteralProjection carries
// its scalar literal kind as its only exported datum. A boolean literal types as
// TypeBool; an integer as TypeInt; the null literal as TypeNull; and so on. The
// listener computes the type at classification time from the grammar node.
func TestLiteralProjectionType(t *testing.T) {
	for _, tc := range []struct {
		lit  query.LiteralProjection
		want query.Type
	}{
		{query.NewLiteralProjection(query.TypeBool{}), query.TypeBool{}},
		{query.NewLiteralProjection(query.TypeInt{}), query.TypeInt{}},
		{query.NewLiteralProjection(query.TypeFloat{}), query.TypeFloat{}},
		{query.NewLiteralProjection(query.TypeString{}), query.TypeString{}},
		{query.NewLiteralProjection(query.TypeNull{}), query.TypeNull{}},
	} {
		require.Equal(t, tc.want, tc.lit.Type())
	}
}

// TestTypeMarshalJSON pins the wire encoding: every Type marshals as its
// stringer value, quoted as a JSON string. The stringer is the single source
// so drift is impossible. Stage 7 extends the coverage to the six temporal
// variants.
func TestTypeMarshalJSON(t *testing.T) {
	for _, tc := range []struct {
		t    query.Type
		want string
	}{
		{query.TypeBool{}, `"bool"`},
		{query.TypeInt{}, `"int"`},
		{query.TypeFloat{}, `"float"`},
		{query.TypeString{}, `"string"`},
		{query.TypeNull{}, `"null"`},
		{query.TypeMap{}, `"map"`},
		{query.TypeNode{}, `"node"`},
		{query.TypeEdge{}, `"edge"`},
		{query.TypeUnknown{}, `"unknown"`},
		{query.TypeDate{}, `"date"`},
		{query.TypeTime{}, `"time"`},
		{query.TypeLocalTime{}, `"localtime"`},
		{query.TypeDateTime{}, `"datetime"`},
		{query.TypeLocalDateTime{}, `"localdatetime"`},
		{query.TypeDuration{}, `"duration"`},
		{query.NewTypeList(query.TypeInt{}), `"list<int>"`},
		{query.NewTypeList(query.NewTypeList(query.TypeString{})), `"list<list<string>>"`},
		{query.NewTypeList(query.TypeDate{}), `"list<date>"`},
		{query.NewTypeList(query.TypeDuration{}), `"list<duration>"`},
	} {
		out, err := json.Marshal(tc.t)
		require.NoError(t, err)
		require.JSONEq(t, tc.want, string(out))
	}
}

// scalarAndEntityTypeStrings is the wire tag each variant it names must
// stringify to. It is a package-level var rather than a literal inside the
// test below because a second reader walks it: the membership of this table
// is itself a claim, and TestEveryTypeVariantIsStringPinned holds it against
// the sum.
var scalarAndEntityTypeStrings = []struct {
	t    query.Type
	want string
}{
	{query.TypeBool{}, "bool"},
	{query.TypeInt{}, "int"},
	{query.TypeFloat{}, "float"},
	{query.TypeString{}, "string"},
	{query.TypeNull{}, "null"},
	{query.TypeMap{}, "map"},
	{query.TypeNode{}, "node"},
	{query.TypeEdge{}, "edge"},
	{query.TypeUnknown{}, "unknown"},
	{query.TypeDate{}, "date"},
	{query.TypeTime{}, "time"},
	{query.TypeLocalTime{}, "localtime"},
	{query.TypeDateTime{}, "datetime"},
	{query.TypeLocalDateTime{}, "localdatetime"},
	{query.TypeDuration{}, "duration"},
}

// stringPinnedElsewhere is the Type variants scalarAndEntityTypeStrings does
// NOT carry, each naming the test that carries it instead. An entry is an
// assertion about another test and not a waiver: the guard below requires the
// named test to exist, and requires the variant to be genuinely absent from
// the table, so an exemption cannot outlive either half of its reason.
//
// Both entries are here because their variant is awkward as a row rather than
// because it is unpinned. TypeList's zero value carries no element, so the
// row that would sit in the table is a constructed one and its own test says
// so; TypePath arrived in Stage 8 with its test beside it.
var stringPinnedElsewhere = map[string]string{
	"TypeList": "TestTypeListString",
	"TypePath": "TestTypePathString",
}

// TestScalarAndEntityTypeString pins the lowercase wire name of each variant
// in scalarAndEntityTypeStrings. String is the single source the JSON
// discriminator derives from, so the serialised name can never drift from the
// Go type. Stage 7 extended the table to the temporal variants.
func TestScalarAndEntityTypeString(t *testing.T) {
	require.NotEmpty(t, scalarAndEntityTypeStrings,
		"the table is empty, so this test ranges over nothing and passes having pinned no tag")
	for _, tc := range scalarAndEntityTypeStrings {
		require.Equal(t, tc.want, tc.t.String())
	}
}

// TestEveryTypeVariantIsStringPinned holds the table above to the sum. Until
// bd gqlc-zmyz its membership was a selection nothing checked, so a variant
// added to Type could be left out of it and every test in this package still
// passed — the same class PR #965 closed for TypeList's field declaration.
//
// The variants come off the isType marker through declaredMarkers, which is
// an AST walk and not a grep, so a variant cannot be hidden behind a comment.
// The tabled names come off the ROWS' own Go types rather than off a written
// list, so the two sides cannot drift by an edit to one of them: a row added
// to the table is a name this reader gains with no edit here.
//
// A count would not do. Fifteen tabled variants and fifteen declared ones
// agree on size while disagreeing on membership, and swapping a variant for
// another is exactly the edit a count cannot see, so the comparison is on the
// SETS.
func TestEveryTypeVariantIsStringPinned(t *testing.T) {
	declared := declaredMarkers(t, "isType")
	require.NotEmpty(t, declared,
		"the walk found no isType declaration, so every variant below would read as pinned")

	tabled := map[string]struct{}{}
	for _, tc := range scalarAndEntityTypeStrings {
		// The row's own Go type, so the name this reader compares is the one
		// the compiler resolved and not one written down beside it.
		name := strings.TrimPrefix(fmt.Sprintf("%T", tc.t), "query.")
		tabled[name] = struct{}{}
	}
	require.NotEmpty(t, tabled, "no row yielded a type name, so no variant could fail the sweep below")

	tests := declaredTestFuncs(t)
	require.NotEmpty(t, tests, "the walk found no test function, so every exemption would read as honoured")

	for _, variant := range declared {
		// A marker on a pointer receiver is a variant too, and the name is
		// what this guard compares; the receiver form is TestQuerySumsAreNotClosed's.
		name := strings.TrimPrefix(variant, "*")
		_, inTable := tabled[name]
		pin, exempt := stringPinnedElsewhere[name]

		switch {
		case inTable && exempt:
			t.Errorf("%s is in scalarAndEntityTypeStrings AND exempted to %s. One of the two is stale:\n"+
				"drop the exemption if the table now carries it, or drop the row if %s is where the tag is pinned.",
				name, pin, pin)
		case !inTable && !exempt:
			t.Errorf("%s declares isType but no test in this package pins its String().\n"+
				"Add a row to scalarAndEntityTypeStrings, or — if the variant is awkward as a row, as TypeList is —\n"+
				"pin it in a test of its own and name that test in stringPinnedElsewhere.", name)
		case exempt:
			require.Containsf(t, tests, pin,
				"%s is exempted to %s, which no test in this package declares: the exemption points at a "+
					"test that was renamed or deleted, so the variant's tag is pinned by nothing", name, pin)
		}
	}

	// The other direction, and it is not implied by the loop: an exemption
	// naming a variant the sum does not have is never reached above, so it
	// would sit there indefinitely reading as a live account of one.
	declaredSet := map[string]struct{}{}
	for _, v := range declared {
		declaredSet[strings.TrimPrefix(v, "*")] = struct{}{}
	}
	for name, pin := range stringPinnedElsewhere {
		require.Containsf(t, declaredSet, name,
			"stringPinnedElsewhere exempts %s to %s, but no type in package query declares isType on it, "+
				"so the exemption describes a variant that no longer exists", name, pin)
	}
}

// declaredTestFuncs is the name of every top-level Test function declared in
// this directory, in either package clause.
//
// It is a second walk rather than parseQueryPackage, which filters to package
// `query` on purpose — the markers it reads are unexported, so a method of
// that name in query_test would not satisfy the interface. The tests an
// exemption names live in query_test, which is precisely the half that filter
// drops.
func declaredTestFuncs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", e.Name()), nil, parser.SkipObjectResolution)
		require.NoErrorf(t, err, "parsing %s", e.Name())
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			names = append(names, fn.Name.Name)
		}
	}
	return names
}

// TestTemporalTypesSealed pins that each Stage-7 temporal variant satisfies
// Type in its value form: the private isType() marker is declared on each and
// is reachable from this package, so none of the six was left out of the sum.
// The name is about what the marker seals — declaration, so a foreign package
// cannot declare a new variant. It does not seal inhabitation;
// TestQuerySumsAreNotClosed/Type holds that half.
func TestTemporalTypesSealed(_ *testing.T) {
	var _ query.Type = query.TypeDate{}
	var _ query.Type = query.TypeTime{}
	var _ query.Type = query.TypeLocalTime{}
	var _ query.Type = query.TypeDateTime{}
	var _ query.Type = query.TypeLocalDateTime{}
	var _ query.Type = query.TypeDuration{}
}

// --- Stage 8: TypePath ---

// TestTypePathString pins the Stage-8 TypePath variant's stringer to "path".
// It joins the sum by declaring the same isType() marker as the other
// variants; its wire tag is the single source the JSON encoding derives from.
func TestTypePathString(t *testing.T) {
	require.Equal(t, "path", query.TypePath{}.String())
	var _ query.Type = query.TypePath{}
}

// TestTypePathMarshalJSON pins the wire encoding of TypePath: its stringer
// value quoted as a JSON string, matching every other Type variant's
// marshalType routing.
func TestTypePathMarshalJSON(t *testing.T) {
	out, err := json.Marshal(query.TypePath{})
	require.NoError(t, err)
	require.JSONEq(t, `"path"`, string(out))
}

// TestTypeListOfPath pins that TypeList composes over TypePath: a
// list-of-path types as list<path> on the wire, with no special-casing.
func TestTypeListOfPath(t *testing.T) {
	require.Equal(t, "list<path>", query.NewTypeList(query.TypePath{}).String())
}
