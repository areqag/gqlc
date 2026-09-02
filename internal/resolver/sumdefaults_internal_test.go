package resolver

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// foreignType and foreignEffect inhabit their query sums without being any
// declared variant. That is what bead gqlc-vt45 (from PR #916) is about: an
// unexported marker with value receivers seals DECLARATION of the interface,
// not INHABITATION of it, so embedding the interface satisfies it from any
// package in the module and matches no `case query.TypeInt:` arm. Both defaults
// below are therefore live branches, not dead code.
type foreignType struct{ query.Type }

type foreignEffect struct{ query.Effect }

// TestResolveTypeDefaultPanicsOnAForeignType pins resolveType's chosen answer
// for the arm.
//
// The panic is kept rather than turned into an error, and the reason is not
// that the arm is unreachable. Only parser-constructed Types reach resolveType
// today, so reaching it takes a change inside this module rather than a query —
// but the arm is reachable in the language sense, and the three arms just above
// it (TypeNode, TypeEdge, TypePath) already panic for exactly that reading of
// "resolver bug". Returning an ErrOutOfR0Scope here instead would put an
// in-module programming mistake into the same channel as a user writing an
// unsupported query, and the resolver's callers surface that channel to the
// user as a diagnostic about their Cypher.
func TestResolveTypeDefaultPanicsOnAForeignType(t *testing.T) {
	var got ResolvedType
	var err error
	require.PanicsWithValue(t,
		"resolver bug: resolveType reached unhandled query.Type resolver.foreignType",
		func() { got, err = resolveType(foreignType{}) },
		"a Type variant the mapper does not know is a resolver bug, not a query the user can write")
	require.Nil(t, got, "the panic unwinds before the assignment, so nothing was returned")
	require.NoError(t, err)
}

// TestResolveTypeMapsADeclaredVariant is the other direction: the default is
// not swallowing a variant the switch is supposed to handle.
func TestResolveTypeMapsADeclaredVariant(t *testing.T) {
	got, err := resolveType(query.TypeInt{})
	require.NoError(t, err)
	require.Equal(t, ResolvedScalar{Kind: ScalarInt}, got)
}

// TestValidateEffectDefaultRefusesAForeignEffect pins the other half of the
// bead: validateEffect HAS an error channel, so its default refuses through it
// instead of panicking. The two defaults answer differently on purpose —
// validateEffect is reached from Resolve with an Effect the parser built, and a
// variant it does not know is a gap in R6's coverage of the model rather than a
// corrupted value, so refusing the query is the honest answer.
//
// sc is nil because the default arm returns before reading it; a variant that
// started dereferencing sc first would fail this row rather than pass it.
func TestValidateEffectDefaultRefusesAForeignEffect(t *testing.T) {
	err := validateEffect(nil, foreignEffect{}, schema.Schema{})
	require.ErrorIs(t, err, ErrOutOfR0Scope)
	require.Contains(t, err.Error(), "unknown Effect variant")
}

// TestValidateEffectDispatchesADeclaredVariant keeps the row above from being
// the only thing the default is measured against: a declared variant must not
// reach it.
func TestValidateEffectDispatchesADeclaredVariant(t *testing.T) {
	sc := newScope(branchState{})
	err := validateEffect(sc, query.NewCreateEffect(nil), schema.Schema{})
	require.NotErrorIs(t, err, ErrOutOfR0Scope,
		"a declared Effect variant must be dispatched, not fall through to the tripwire")
}

// TestListElementEntityProjectionsAreRefusedAtR0 pins the two refusals that
// decide, from here, whether a whole arm of the neo4j emitter is live code.
//
// resolveType is one of three places that construct a ResolvedList. The other
// two cannot originate an entity element: scope.go's variable-length edge
// binding builds ResolvedEdge or ResolvedEdgeUnion and never a ResolvedNode,
// and unify only PROPAGATES an element both of its arguments already carry.
// So these two lines are the whole of the question, and today they answer no.
//
// WHAT RIDES ON THE NODES ROW. internal/codegen's buildListElemPlan maps a
// ResolvedList whose Element is a ResolvedNode to a ListElem of kind
// codegen.ColumnNode, and internal/codegen/neo4j's walkListElemBody has an arm
// for that kind which emits a dbtype.Node assertion and a decode<Entity> call.
// Because no query reaches these lines with a node element, that arm is unrun.
// Measured 2026-09-02 with -coverpkg over ./internal/codegen/...: of the
// eighteen coverage blocks inside that switch, the ColumnNode case is the only
// case with no covered block at all, while the ColumnEdge and ColumnEdgeUnion
// cases beside it are covered by the variable-length-edge corpus queries. Two
// further blocks read zero and are deliberately not claimed here — a
// temporal-carrier branch of the ColumnProperty arm and the non-narrowing
// branch of ColumnTemporal — because both sit inside arms that do run.
//
// It is unrun and it is not deletable, which is why this pin exists instead of
// a deletion. Measured the same day by deleting the arm: `go build ./...`
// still succeeds and `go test -count=1 ./internal/codegen/...` is entirely
// green — no test in this repository defends it — but golangci-lint's
// exhaustive fails with "missing cases in switch of type codegen.ColumnKind:
// codegen.ColumnNode", because that switch carries no default. Satisfying the
// linter after a deletion means adding one, and .golangci.yml sets
// default-signifies-exhaustive, so a default would switch the exhaustive check
// off for that switch permanently — trading a dead arm for the dead guard that
// makes every future ColumnKind land there. So the arm stays.
//
// What was missing was any signal on the other side. If the day comes that R5
// or later admits a list-of-nodes projection, the emitter arm stops being
// hypothetical and needs a corpus query covering it the way the edge arms are
// covered — and nothing said so. This test is that signal: it fails when the
// refusal is lifted, and its message names the arm to go and cover.
//
// The edges row is the control and not a second claim. Both refusals sit in
// one type switch and return the same sentinel, so a single row could be
// satisfied by a mutant that refuses every list alike; two rows with distinct
// messages cannot. Lifting the edges refusal has no such consequence for the
// emitter — codegen.ColumnEdge is reachable through the edge-binding route
// above — so only the nodes row carries the warning.
func TestListElementEntityProjectionsAreRefusedAtR0(t *testing.T) {
	for _, tt := range []struct {
		name string
		elem query.Type
		want string
		why  string
	}{
		{
			"nodes",
			query.TypeNode{},
			"list-of-nodes projection",
			"lifting this refusal makes internal/codegen/neo4j walkListElemBody's codegen.ColumnNode arm reachable: " +
				"cover it with a corpus query the way VarLengthEdgeColumn covers the edge arm, then change this row",
		},
		{
			"edges",
			query.TypeEdge{},
			"list-of-edges projection",
			"the control: a refusal that does not distinguish its two element kinds is not pinned by the row above",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveType(query.NewTypeList(tt.elem))
			require.ErrorIs(t, err, ErrOutOfR0Scope, tt.why)
			require.Contains(t, err.Error(), tt.want, tt.why)
			require.Nil(t, got, "a refusal returns no resolved type")
		})
	}
}

// TestListElementScalarProjectionStillResolves keeps the two rows above from
// being satisfied by a resolver that refuses every list. It is the same
// posture as TestResolveTypeMapsADeclaredVariant one function up.
func TestListElementScalarProjectionStillResolves(t *testing.T) {
	got, err := resolveType(query.NewTypeList(query.TypeInt{}))
	require.NoError(t, err)
	require.Equal(t, ResolvedList{Element: ResolvedScalar{Kind: ScalarInt}}, got)
}

// TestDescribeColumnTypeRendersANilListElement pins the third posture in this
// file, and it differs from both above on purpose.
//
// describeColumnType's ResolvedList arm recurses on v.Element. A nil Element is
// a nil interface: it matches no case, reaches the default, and calls String()
// on nothing. resolveType answers a value it has no case for by panicking
// (TestResolveTypeDefaultPanicsOnAForeignType) and validateEffect by refusing,
// and neither is available here — describeColumnType is only ever called while
// BUILDING ErrUnionColumnMismatch's message (resolve.go, compareBranchColumns),
// so a fault or a refusal at this point destroys a diagnostic the caller has
// already decided to emit. A refusal that faults is not a refusal; the render
// must total. That is the posture PR #937 settled for the same fault one
// package later, in internal/codegen's resolvedTypeName (bd gqlc-edze).
//
// "<nil>" is the token %T gives a nil interface, which is what resolvedTypeName
// falls back to, so the two packages spell the same value the same way.
// The bare row is what makes the guard's PLACEMENT load-bearing rather than
// incidental. A nil reaches describeColumnType by two routes, not one: nested,
// as a ResolvedList's Element, and bare, as a Column.Type — resolvedTypeEqual's
// nil guard reports nil-versus-populated unequal in both shapes, and
// compareBranchColumns renders whichever pair it was told is unequal. A check
// written inside the ResolvedList arm answers the nested route and still faults
// on the bare one, and would pass the nested row alone.
func TestDescribeColumnTypeRendersANilListElement(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   ResolvedType
		want string
	}{
		{"bare", nil, "<nil>"},
		{"nested in a list", ResolvedList{Element: nil}, "list of <nil>"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.NotPanics(t, func() { describeColumnType(tt.in) },
				"describeColumnType is called to build a fail message, so it may not fault while rendering one")
			require.Equal(t, tt.want, describeColumnType(tt.in))
		})
	}
}

// TestCompareBranchColumnsSurvivesANilListElement is the composed fault path,
// and it is the one that matters: the row above pins the renderer, this one
// pins that the renderer's caller still produces its error.
//
// resolvedTypeEqual guards its own recursion (a == nil || b == nil) and so
// reports the two columns unequal; compareBranchColumns then calls
// describeColumnType on exactly the pair it just called unequal. So the guard
// that makes one function safe is what delivers the other its nil.
//
// The value is constructed directly rather than resolved from a fixture
// because no query originates it. That was read here as the three ResolvedList
// constructions in non-test code all assigning a non-nil Element, with unify
// only PROPAGATING a nil that already exists (bd gqlc-t802); the property is
// wider and is now measured rather than argued, across every arm that can
// produce a column type, by TestNilColumnTypeIsNotConstructible (bd
// gqlc-oltq). The fault is latent, and a pin is what keeps it from being
// re-armed by a future originator.
func TestCompareBranchColumnsSurvivesANilListElement(t *testing.T) {
	branchCols := [][]Column{
		{{Name: "c", Type: ResolvedList{Element: ResolvedProperty{Type: graph.TypeInt}}}},
		{{Name: "c", Type: ResolvedList{Element: nil}}},
	}
	var err error
	require.NotPanics(t, func() { err = compareBranchColumns(branchCols) },
		"a nil element on one branch must reach the mismatch message, not a crash")
	require.ErrorIs(t, err, ErrUnionColumnMismatch)
	require.Contains(t, err.Error(), "list of <nil>",
		"the message must name what the failing branch actually projected")
}
