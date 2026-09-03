package resolver

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// Unit tests for the scope module (spec docs/specs/resolver-branch-scope.md
// §4.1). Tests exist to pin the scope-module's contract independent of
// the golden-pair harness. Each test constructs a scope directly via
// newScope + Ingest, runs one or more phase methods, and asserts on
// Snapshot / Export / read-only predicates.
//
// Tests that name a phase method not yet on the receiver stay skipped
// until that method's step lands (spec §5). The current step wires
// A1/A2/B/C onto scope; the associated tests are enabled below.

// makeTestEdgeBinding is a small helper — constructing a
// query.EdgeBinding for shadow-cascade tests takes five arguments
// every time. The helper hides that ceremony.
func makeTestEdgeBinding(variable string) (query.EdgeBinding, error) {
	src := query.NewInlineEndpoint(graph.LabelSet{"Person"})
	tgt := query.NewInlineEndpoint(graph.LabelSet{"Person"})
	return query.NewEdgeBinding(variable, graph.LabelSet{"KNOWS"}, src, tgt, true)
}

func TestScopeEmptyCarryEmptyPart(t *testing.T) {
	sc := newScope(branchState{})
	sc.Ingest(query.Part{})
	snap := sc.Snapshot()
	require.Empty(t, snap.nodeTypes)
	require.Empty(t, snap.edgeTypes)
	require.Empty(t, snap.edgeCands)
	require.Empty(t, snap.edgeBindings)
	require.Empty(t, snap.nullableBinding)
}

func TestScopeBindNodeShadowCascade(t *testing.T) {
	// Seed the scope with a carried edge binding at variable "r" so we
	// can prove BindNode drops every mirror lane at the same name.
	carriedEdge, err := makeTestEdgeBinding("r")
	require.NoError(t, err)
	carry := branchState{
		exportedEdgeTypes:    map[string]schema.EdgeType{"r": {}},
		exportedEdgeKeys:     map[string]schema.EdgeKey{"r": {}},
		exportedEdgeCands:    map[string][]schema.EdgeKey{"r": nil},
		exportedEdgeBindings: map[string]query.EdgeBinding{"r": carriedEdge},
	}
	sc := newScope(carry)
	sc.Ingest(query.Part{})

	nb, err := query.NewNodeBinding("r", graph.LabelSet{"Person"})
	require.NoError(t, err)
	nt := schema.NodeType{KeyLabels: graph.LabelSet{"Person"}.Key(), CompleteLabels: graph.LabelSet{"Person"}.Key()}
	require.NoError(t, sc.BindNode(nb, nt))

	require.Contains(t, sc.nodeTypes, "r")
	require.NotContains(t, sc.edgeTypes, "r")
	require.NotContains(t, sc.edgeCands, "r")
	require.NotContains(t, sc.edgeKeys, "r")
	require.NotContains(t, sc.edgeCands, "r")
	require.NotContains(t, sc.edgeBindings, "r")
}

func TestScopeBindEdgeShadowCascade(t *testing.T) {
	// BindEdge has two arms, and each needs its own seed: the node shadow
	// (carried nodeTypes) and the closed-edge reset (carried edgeTypes /
	// edgeKeys / edgeCands, which Phase A2/C recomputes for the new
	// binding's endpoints). Seeding only the node lane left the reset arm
	// unpinned — dropping the edgeKeys or edgeCands delete kept the whole
	// tree green.
	carry := branchState{
		exportedNodeTypes: map[string]schema.NodeType{"x": {KeyLabels: graph.LabelSet{"Person"}.Key(), CompleteLabels: graph.LabelSet{"Person"}.Key()}},
		exportedEdgeTypes: map[string]schema.EdgeType{"x": {}},
		exportedEdgeKeys:  map[string]schema.EdgeKey{"x": {}},
		exportedEdgeCands: map[string][]schema.EdgeKey{"x": nil},
	}
	sc := newScope(carry)
	sc.Ingest(query.Part{})

	eb, err := makeTestEdgeBinding("x")
	require.NoError(t, err)
	require.NoError(t, sc.BindEdge(eb))

	require.NotContains(t, sc.nodeTypes, "x")
	require.NotContains(t, sc.edgeTypes, "x")
	require.NotContains(t, sc.edgeKeys, "x")
	require.NotContains(t, sc.edgeCands, "x")
	_, ok := sc.edgeBindings["x"]
	require.True(t, ok)
}

func TestScopeBindCallShadowCascade(t *testing.T) {
	// Belt-and-braces: seed every carried entity lane so each of
	// BindCall's five deletes has something to drop, and assert on the
	// maps rather than through HasNode/HasEdge. HasEdge reads edgeTypes
	// and edgeCands only, so seeding edgeBindings and asserting HasEdge
	// tests nothing about either — the seeded lane and the read lanes did
	// not intersect, and all four edge deletes could be removed together
	// with the whole tree green. Parser-reachability is a separate
	// question; this pins the invariant at the scope layer.
	carriedEdge, err := makeTestEdgeBinding("c")
	require.NoError(t, err)
	carry := branchState{
		exportedNodeTypes:    map[string]schema.NodeType{"c": {KeyLabels: graph.LabelSet{"Person"}.Key(), CompleteLabels: graph.LabelSet{"Person"}.Key()}},
		exportedEdgeTypes:    map[string]schema.EdgeType{"c": {}},
		exportedEdgeKeys:     map[string]schema.EdgeKey{"c": {}},
		exportedEdgeCands:    map[string][]schema.EdgeKey{"c": nil},
		exportedEdgeBindings: map[string]query.EdgeBinding{"c": carriedEdge},
	}
	sc := newScope(carry)
	sc.Ingest(query.Part{})

	cb, err := query.NewCallBinding("c", "test.proc", "value", query.TypeInt{}, false)
	require.NoError(t, err)
	reg, err := procsig.NewRegistry(nil)
	require.NoError(t, err)
	require.NoError(t, sc.BindCall(cb, reg))

	require.NotContains(t, sc.nodeTypes, "c")
	require.NotContains(t, sc.edgeTypes, "c")
	require.NotContains(t, sc.edgeKeys, "c")
	require.NotContains(t, sc.edgeCands, "c")
	require.NotContains(t, sc.edgeBindings, "c")
	require.Contains(t, sc.callTypes, "c")
}

// The three tests below pin the resolvedCovers delete in BindNodeCands, BindEdge
// and BindCall — the three siblings of the plural arm that
// TestPhaseBsPluralCommitLeavesNoResolvedCoversMark pins in resolver_test.go.
//
// Each seeds the lane by hand, because newScope deliberately does not seed
// resolvedCovers from the carry (scope.go says so and gives the precision
// argument), and every in-Part writer of the lane writes s.nodeTypes in the same
// breath. So no query reaches a scope holding a mark these methods could find.
// Seeding is what makes the state constructible, exactly as the plural arm's
// test seeds a nodeTable; keylabelset_test.go uses the same technique for state
// the corpus cannot express.
//
// These do not claim the state arises today. They claim each method clears it
// when it is there, which is the whole reason the three deletes were written:
// the lane must not outlive the `resolved` entry it qualifies under any seeding
// regime, and seeding newScope from the carry — the alternative scope.go names
// and declines — is what would make them live. Three tests rather than one, and
// three separate scopes, so a dropped delete names the method that dropped it.

func TestScopeBindNodeCandsClearsASeededResolvedCoversMark(t *testing.T) {
	np, err := query.NewNodeBinding("p", graph.LabelSet{})
	require.NoError(t, err)
	person := graph.LabelSet{"Person"}.Key()
	employee := graph.LabelSet{"Employee"}.Key()

	sc := newScope(branchState{})
	sc.Ingest(query.Part{})
	// Seeded WITHOUT a nodeTypes entry. With one, BindNodeCands refuses the
	// re-bind outright ("carried as singular node type, re-bound as plural") and
	// never reaches the delete — which is the unreachability its comment states.
	sc.resolvedCovers["p"] = struct{}{}

	require.NoError(t, sc.BindNodeCands(np, []schema.NodeType{
		{KeyLabels: person, CompleteLabels: person},
		{KeyLabels: employee, CompleteLabels: employee},
	}))

	// Tripwire: without it the assertion below passes for a p that never entered
	// the plural lane, so the arm under test never ran.
	require.Contains(t, sc.nodeCands, "p", "p must enter the plural lane, or the arm under test did not run")
	require.NotContains(t, sc.nodeTypes, "p", "the plural arm writes no singular entry for a mark to qualify")

	require.NotContains(t, sc.resolvedCovers, "p",
		"a mark qualifying a `resolved` entry BindNodeCands did not write must not survive it")
}

func TestScopeBindEdgeClearsASeededResolvedCoversMark(t *testing.T) {
	// The node-shadow arm: an edge binding at a name the node lanes already hold.
	// TestScopeBindEdgeShadowCascade seeds those lanes through the carry and pins
	// the nodeTypes/nodeCands deletes; the carry cannot reach resolvedCovers, so
	// that test says nothing about this third delete and stays green without it.
	carry := branchState{
		exportedNodeTypes: map[string]schema.NodeType{"x": {KeyLabels: graph.LabelSet{"Person"}.Key(), CompleteLabels: graph.LabelSet{"Person"}.Key()}},
	}
	sc := newScope(carry)
	sc.Ingest(query.Part{})
	sc.resolvedCovers["x"] = struct{}{}

	eb, err := makeTestEdgeBinding("x")
	require.NoError(t, err)
	require.NoError(t, sc.BindEdge(eb))

	require.Contains(t, sc.edgeBindings, "x", "the edge must bind, or the shadow cascade under test did not run")
	require.NotContains(t, sc.nodeTypes, "x", "the shadowed singular entry goes")

	require.NotContains(t, sc.resolvedCovers, "x",
		"a mark qualifying a `resolved` entry an edge shadow removed must not outlive it")
}

func TestScopeBindCallClearsASeededResolvedCoversMark(t *testing.T) {
	carry := branchState{
		exportedNodeTypes: map[string]schema.NodeType{"c": {KeyLabels: graph.LabelSet{"Person"}.Key(), CompleteLabels: graph.LabelSet{"Person"}.Key()}},
	}
	sc := newScope(carry)
	sc.Ingest(query.Part{})
	sc.resolvedCovers["c"] = struct{}{}

	cb, err := query.NewCallBinding("c", "test.proc", "value", query.TypeInt{}, false)
	require.NoError(t, err)
	reg, err := procsig.NewRegistry(nil)
	require.NoError(t, err)
	require.NoError(t, sc.BindCall(cb, reg))

	require.Contains(t, sc.callTypes, "c", "the call must bind, or the shadow cascade under test did not run")
	require.NotContains(t, sc.nodeTypes, "c", "the shadowed singular entry goes")

	require.NotContains(t, sc.resolvedCovers, "c",
		"a mark qualifying a `resolved` entry a CALL shadow removed must not outlive it")
}

// The two tests below drive BindCall's parser-drift tripwires — the registry
// miss and the arity mismatch. No query reaches either: cypher/call.go's
// collectCall fails first with ErrUnknownProcedure and ErrProcedureArity, which
// is the parser-authoritative trust posture BindCall's own comment states. They
// are drivable here anyway because BindCall takes the registry as an argument,
// so a scope-level caller can hand it a registry no parser would have passed.
//
// Asserted on the MESSAGE, which is unusual in this package and deliberate.
// scope.go returns these two as plain non-sentinel errors precisely so a drift
// bug cannot pollute ErrCallArgAssignability's fixture semantics — so there is
// no errors.Is to lean on, and the wording is the whole of the contract.
//
// Both are guarded by `len(args) > 0`, so both carry a tripwire that the args
// slice is non-empty: without it a binding that never entered the guarded block
// would report "no error" and pass the control half while testing nothing. Each
// also carries a CONTROL that the same registry admits a call it should, so a
// Lookup wired to miss unconditionally cannot pass the refusing half for the
// wrong reason.

func TestScopeBindCallRefusesAProcedureMissingFromTheRegistry(t *testing.T) {
	// A populated registry, not an empty one: the claim is that the refusal is
	// about this NAME. An empty registry would also be refused by a Lookup that
	// had stopped consulting its argument at all.
	reg, err := procsig.NewRegistry([]procsig.Signature{{
		Name:   "test.present",
		Params: []procsig.Param{{Name: "n", Token: procsig.TokenInteger}},
	}})
	require.NoError(t, err)
	args := []query.CallArg{query.NewCallArg(query.TypeInt{})}

	sc := newScope(branchState{})
	sc.Ingest(query.Part{})
	cb, err := query.NewCallBindingWithArgs("c", "test.absent", "value", query.TypeInt{}, false, args)
	require.NoError(t, err)
	require.NotEmpty(t, cb.Args(), "args must be non-empty, or BindCall's guarded block never runs")

	require.EqualError(t, sc.BindCall(cb, reg),
		`resolver: procedure "test.absent" missing from registry (parser drift)`)
	require.NotContains(t, sc.callTypes, "c", "a refused CALL must not enter the call lane")

	// Control: same registry, same args, a name it does hold.
	ctl := newScope(branchState{})
	ctl.Ingest(query.Part{})
	held, err := query.NewCallBindingWithArgs("c", "test.present", "value", query.TypeInt{}, false, args)
	require.NoError(t, err)
	require.NoError(t, ctl.BindCall(held, reg),
		"the registry must admit a name it holds, or the refusal above is not about the name")
}

func TestScopeBindCallRefusesAnArityMismatch(t *testing.T) {
	reg, err := procsig.NewRegistry([]procsig.Signature{{
		Name: "test.two",
		Params: []procsig.Param{
			{Name: "a", Token: procsig.TokenInteger},
			{Name: "b", Token: procsig.TokenInteger},
		},
	}})
	require.NoError(t, err)
	intArg := func(n int) []query.CallArg {
		out := make([]query.CallArg, n)
		for i := range out {
			out[i] = query.NewCallArg(query.TypeInt{})
		}
		return out
	}

	// Both directions, because the guard is load-bearing differently in each.
	// Under-supply falls through to an assignability loop that would find every
	// arg assignable and admit the call; over-supply would index sig.Params past
	// its end. The second is why this guard is not merely a better message.
	for _, tc := range []struct {
		name string
		args []query.CallArg
		want string
	}{
		{"fewer args than params", intArg(1), `resolver: procedure "test.two" expects 2 arguments, got 1 (parser drift)`},
		{"more args than params", intArg(3), `resolver: procedure "test.two" expects 2 arguments, got 3 (parser drift)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := newScope(branchState{})
			sc.Ingest(query.Part{})
			cb, err := query.NewCallBindingWithArgs("c", "test.two", "value", query.TypeInt{}, false, tc.args)
			require.NoError(t, err)
			require.NotEmpty(t, cb.Args(), "args must be non-empty, or BindCall's guarded block never runs")

			require.EqualError(t, sc.BindCall(cb, reg), tc.want)
			require.NotContains(t, sc.callTypes, "c", "a refused CALL must not enter the call lane")
		})
	}

	// Control: the same registry and procedure at the arity it declares.
	ctl := newScope(branchState{})
	ctl.Ingest(query.Part{})
	matched, err := query.NewCallBindingWithArgs("c", "test.two", "value", query.TypeInt{}, false, intArg(2))
	require.NoError(t, err)
	require.NoError(t, ctl.BindCall(matched, reg),
		"a matching arity must be admitted, or the refusals above are not about the count")
}

func TestScopeSnapshotNarrowing(t *testing.T) {
	// Snapshot exposes only the five witness lanes. callTypes,
	// carriedResolvedTypes, carriedGroups, and the ingested Part are
	// not observable through partScope. §2.3 invariant #3.
	carry := branchState{
		exportedCallTypes:     map[string]callBindingSlot{"c": {resultType: query.TypeUnknown{}}},
		exportedResolvedTypes: map[string]ResolvedType{"alias": ResolvedUnknown{}},
		exportedOptionalGroup: map[string]int{"opt": 7},
	}
	sc := newScope(carry)
	sc.Ingest(query.Part{})

	snap := sc.Snapshot()
	// partScope has exactly five fields (nodeTypes, edgeTypes,
	// edgeCands, edgeBindings, nullableBinding). A test on the field
	// count is the most direct way to prove no lane leaks — anything
	// else would let a widened partScope slip through this invariant
	// silently.
	require.Empty(t, snap.nodeTypes)
	require.Empty(t, snap.edgeTypes)
	require.Empty(t, snap.edgeCands)
	require.Empty(t, snap.edgeBindings)
	require.Empty(t, snap.nullableBinding)
	// callTypes, carried lanes stay on scope, not on partScope.
	require.Contains(t, sc.callTypes, "c")
	require.Equal(t, ResolvedUnknown{}, sc.carriedResolvedTypes["alias"])
	require.Equal(t, 7, sc.carriedGroups["opt"])
}

func TestScopeCloseEdgesWritesOnlyEdgeLanes(t *testing.T) {
	// Set up a tiny schema with one node type and one edge type.
	nodeLabels := graph.LabelSet{"Person"}.Key()
	edgeLabel := graph.LabelSet{"KNOWS"}.Key()
	sch := schema.Schema{
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			nodeLabels: {KeyLabels: nodeLabels, CompleteLabels: nodeLabels},
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			{Source: nodeLabels, KeyLabels: edgeLabel, Target: nodeLabels}: {},
		},
	}

	// Two labelled nodes "a", "b" and one edge "r" between them.
	na, err := query.NewNodeBinding("a", graph.LabelSet{"Person"})
	require.NoError(t, err)
	nb, err := query.NewNodeBinding("b", graph.LabelSet{"Person"})
	require.NoError(t, err)
	epA, err := query.NewVarEndpoint("a")
	require.NoError(t, err)
	epB, err := query.NewVarEndpoint("b")
	require.NoError(t, err)
	eb, err := query.NewEdgeBinding("r", graph.LabelSet{"KNOWS"}, epA, epB, true)
	require.NoError(t, err)

	sc := newScope(branchState{})
	sc.Ingest(query.Part{Bindings: []query.Binding{na, nb, eb}})
	require.NoError(t, sc.BindNode(na, schema.NodeType{KeyLabels: nodeLabels, CompleteLabels: nodeLabels}))
	require.NoError(t, sc.BindNode(nb, schema.NodeType{KeyLabels: nodeLabels, CompleteLabels: nodeLabels}))
	require.NoError(t, sc.BindEdge(eb))

	// Snapshot BEFORE CloseEdges to prove only the edge lanes change.
	before := sc.Snapshot()
	require.NoError(t, sc.CloseEdges(sch))
	after := sc.Snapshot()

	require.Equal(t, before.nodeTypes, after.nodeTypes)
	require.Equal(t, before.nullableBinding, after.nullableBinding)
	// The edge closed: edgeTypes / edgeKeys populated at "r".
	require.Contains(t, sc.edgeTypes, "r")
	require.Contains(t, sc.edgeKeys, "r")
}

// TestScopeNarrowingToASingletonLeavesThePluralLane pins lane exclusivity at
// the one place a binding crosses between the two node lanes: when the edge
// closure leaves exactly one surviving candidate, NarrowPluralEndpoints writes
// nodeTypes AND deletes nodeCands.
//
// Nothing else in the suite states it. Drop the delete and the resolver keeps
// answering correctly for a while, because so much of it asks
// `_, plural := s.nodeCands[v]` first and then resolves by intersection over a
// candidate slice that now has one element — which is the same answer the
// singular path gives. The corpus therefore cannot see the difference, and it
// stays invisible right up until one consumer reads nodeTypes and another reads
// nodeCands for the same variable. The invariant is what makes the two lanes
// interchangeable, so it is asserted as an invariant rather than through a
// query whose answer happens not to move.
//
// The scope is built by hand rather than parsed because the assertion is about
// the tables, not about a column: a resolved query never shows which lane the
// answer came out of.
func TestScopeNarrowingToASingletonLeavesThePluralLane(t *testing.T) {
	person := graph.LabelSet{"Person"}.Key()
	employee := graph.LabelSet{"Employee", "Person"}.Key()
	company := graph.LabelSet{"Company"}.Key()
	worksAt := graph.LabelSet{"WORKS_AT"}.Key()
	sch := schema.Schema{
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			person:   {KeyLabels: person, CompleteLabels: person},
			employee: {KeyLabels: employee, CompleteLabels: employee},
			company:  {KeyLabels: company, CompleteLabels: company},
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			// Declared from the employee type only, so `p` has one survivor.
			{Source: employee, KeyLabels: worksAt, Target: company}: {},
		},
	}

	np, err := query.NewNodeBinding("p", graph.LabelSet{"Person"})
	require.NoError(t, err)
	nc, err := query.NewNodeBinding("c", graph.LabelSet{"Company"})
	require.NoError(t, err)
	epP, err := query.NewVarEndpoint("p")
	require.NoError(t, err)
	epC, err := query.NewVarEndpoint("c")
	require.NoError(t, err)
	eb, err := query.NewEdgeBinding("w", graph.LabelSet{"WORKS_AT"}, epP, epC, true)
	require.NoError(t, err)

	sc := newScope(branchState{})
	sc.Ingest(query.Part{Bindings: []query.Binding{np, nc, eb}})
	require.NoError(t, sc.BindNodeCands(np, []schema.NodeType{
		{KeyLabels: person, CompleteLabels: person},
		{KeyLabels: employee, CompleteLabels: employee},
	}))
	require.NoError(t, sc.BindNode(nc, schema.NodeType{KeyLabels: company, CompleteLabels: company}))
	require.NoError(t, sc.BindEdge(eb))
	require.Contains(t, sc.nodeCands, "p", "p starts in the plural lane")

	require.NoError(t, sc.CloseEdges(sch))

	require.Equal(t, employee, sc.nodeTypes["p"].KeyLabels, "the sole survivor commits")
	require.NotContains(t, sc.nodeCands, "p", "and the plural lane is vacated, so no consumer can still see p as plural")
}

// narrowingFixture builds the schema and bindings the two tests below share: `p`
// touched by one WORKS_AT edge declared from Employee only, so a plural `p` over
// {Person, Employee} has exactly one survivor and the collapse arm fires.
func narrowingFixture(t *testing.T) (schema.Schema, query.NodeBinding, query.NodeBinding, query.EdgeBinding) {
	t.Helper()
	person := graph.LabelSet{"Person"}.Key()
	employee := graph.LabelSet{"Employee", "Person"}.Key()
	company := graph.LabelSet{"Company"}.Key()
	worksAt := graph.LabelSet{"WORKS_AT"}.Key()
	sch := schema.Schema{
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			person:   {KeyLabels: person, CompleteLabels: person},
			employee: {KeyLabels: employee, CompleteLabels: employee},
			company:  {KeyLabels: company, CompleteLabels: company},
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			{Source: employee, KeyLabels: worksAt, Target: company}: {},
		},
	}
	np, err := query.NewNodeBinding("p", graph.LabelSet{"Person"})
	require.NoError(t, err)
	nc, err := query.NewNodeBinding("c", graph.LabelSet{"Company"})
	require.NoError(t, err)
	epP, err := query.NewVarEndpoint("p")
	require.NoError(t, err)
	epC, err := query.NewVarEndpoint("c")
	require.NoError(t, err)
	eb, err := query.NewEdgeBinding("w", graph.LabelSet{"WORKS_AT"}, epP, epC, true)
	require.NoError(t, err)
	return sch, np, nc, eb
}

// TestScopeNarrowingToASingletonMarksTheBindingCovering pins the third write in
// the collapse arm — s.resolvedCovers[v] — which its sibling above does not
// assert: that one is about lane exclusivity, this one about precision. The lane
// qualifies `resolved`, and a collapse earns the mark because v was plural, so
// cands was a satisfying set and the survivor is still a superset of the
// attainable types.
//
// The applier is called DIRECTLY rather than through CloseEdges, because the
// comment this replaces claimed no test could pin the write, and reaching it
// through a query is not what makes that false — reading the lane back off a
// scope is. Nothing here is a query.
//
// It changes no answer today: CloseEdges returns immediately after this call and
// resolvedCovers is not carried, so no reader sits between the write and the end
// of the Part. That is why the guard asserts the table rather than a resolved
// column, and it is what the write is FOR — the moment a reader appears after
// this point, omitting the mark is a precision bug with nothing else to catch it.
func TestScopeNarrowingToASingletonMarksTheBindingCovering(t *testing.T) {
	sch, np, nc, eb := narrowingFixture(t)
	employee := graph.LabelSet{"Employee", "Person"}.Key()
	company := graph.LabelSet{"Company"}.Key()
	person := graph.LabelSet{"Person"}.Key()

	sc := newScope(branchState{})
	sc.Ingest(query.Part{Bindings: []query.Binding{np, nc, eb}})
	require.NoError(t, sc.BindNodeCands(np, []schema.NodeType{
		{KeyLabels: person, CompleteLabels: person},
		{KeyLabels: employee, CompleteLabels: employee},
	}))
	require.NoError(t, sc.BindNode(nc, schema.NodeType{KeyLabels: company, CompleteLabels: company}))
	require.NoError(t, sc.BindEdge(eb))
	require.NotContains(t, sc.resolvedCovers, "p", "the mark must be absent before the call, or the assertion below reads a seed")

	sc.NarrowPluralEndpoints(sch)

	// Tripwire: the arm that writes the mark is the len(narrowed)==1 arm, and the
	// other two write no mark for an honest reason. Without these the assertion
	// below could fail for a run that never collapsed anything.
	require.Equal(t, employee, sc.nodeTypes["p"].KeyLabels, "the collapse arm must run, or nothing under test was reached")
	require.NotContains(t, sc.nodeCands, "p", "and it must vacate the plural lane")

	require.Contains(t, sc.resolvedCovers, "p",
		"a binding the narrowing collapsed to one type is covered by it, and the lane must say so")
}

// TestEndpointNarrowingGivesNoEntryWhenNothingIsPlural pins the premise
// NarrowPluralEndpoints' early return rests on. The return itself is not
// observable — see its comment — but the reason it is safe is a claim about
// endpointNarrowing, and that is ordinary to assert.
//
// The plural row is not decoration. Without it an empty map proves nothing: the
// same emptiness is what an edge skipped for any other reason gives, and the
// singular row alone cannot tell "no plural binding" from "this fixture's edge
// never contributes". The two rows differ in one thing, how `p` is bound.
func TestEndpointNarrowingGivesNoEntryWhenNothingIsPlural(t *testing.T) {
	sch, np, nc, eb := narrowingFixture(t)
	employee := graph.LabelSet{"Employee", "Person"}.Key()
	company := graph.LabelSet{"Company"}.Key()
	person := graph.LabelSet{"Person"}.Key()

	bind := func(t *testing.T, plural bool) *scope {
		t.Helper()
		sc := newScope(branchState{})
		sc.Ingest(query.Part{Bindings: []query.Binding{np, nc, eb}})
		if plural {
			require.NoError(t, sc.BindNodeCands(np, []schema.NodeType{
				{KeyLabels: person, CompleteLabels: person},
				{KeyLabels: employee, CompleteLabels: employee},
			}))
		} else {
			require.NoError(t, sc.BindNode(np, schema.NodeType{KeyLabels: employee, CompleteLabels: employee}))
		}
		require.NoError(t, sc.BindNode(nc, schema.NodeType{KeyLabels: company, CompleteLabels: company}))
		require.NoError(t, sc.BindEdge(eb))
		return sc
	}

	t.Run("plural", func(t *testing.T) {
		sc := bind(t, true)
		require.Contains(t, endpointNarrowing([]query.EdgeBinding{eb}, sc.nodeTable(), sch, sc.writtenBindings()), "p",
			"this edge does contribute, so the singular row's empty map is about pluralness and nothing else")
	})

	t.Run("singular", func(t *testing.T) {
		sc := bind(t, false)
		require.Empty(t, sc.nodeCands, "nothing is plural, which is the state the early return guards")
		require.Empty(t, endpointNarrowing([]query.EdgeBinding{eb}, sc.nodeTable(), sch, sc.writtenBindings()),
			"with no plural binding the rule contributes nothing, so the guarded body could only have written a no-op")
	})
}

func TestScopeIngestSingleShot(t *testing.T) {
	sc := newScope(branchState{})
	sc.Ingest(query.Part{})
	require.Panics(t, func() { sc.Ingest(query.Part{}) })
}

// Below: the five §4.1 tests that landed with the phase methods they
// pin (Demote — step 3; Export — step 5). Enabled here in step 7.

// TestScopeCarryForwardRoundTrip is §4.1 #2 — the deletion test.
// A branchState seeded with a node, an edge, and a nullable entry
// round-trips through newScope → Ingest → ResolveProjections →
// Export on a ReturnsAll=true Part with no local bindings. Dropping
// any of the ten carry lanes from scope + Export makes exactly this
// test fail.
func TestScopeCarryForwardRoundTrip(t *testing.T) {
	nodeKey := graph.LabelSet{"Person"}.Key()
	edgeKey := schema.EdgeKey{Source: nodeKey, KeyLabels: graph.LabelSet{"KNOWS"}.Key(), Target: nodeKey}
	carriedEdge, err := makeTestEdgeBinding("r")
	require.NoError(t, err)
	c1 := branchState{
		exportedNodeTypes:       map[string]schema.NodeType{"a": {KeyLabels: nodeKey, CompleteLabels: nodeKey}},
		exportedEdgeTypes:       map[string]schema.EdgeType{"r": {}},
		exportedEdgeKeys:        map[string]schema.EdgeKey{"r": edgeKey},
		exportedEdgeBindings:    map[string]query.EdgeBinding{"r": carriedEdge},
		exportedNullableBinding: map[string]bool{"a": true, "r": false},
		exportedOrder:           []string{"a", "r"},
		exportedResolvedTypes:   map[string]ResolvedType{},
	}
	// Empty schema is fine — ResolveProjections doesn't consult it for
	// bare-name RefProjections against in-scope bindings.
	sch := schema.Schema{Nodes: map[graph.LabelSetKey]schema.NodeType{nodeKey: {KeyLabels: nodeKey, CompleteLabels: nodeKey}}}
	sc := newScope(c1)
	sc.Ingest(query.Part{ReturnsAll: true})
	sc.SeedLocalNullability() // no local bindings → no-op
	sc.DemoteNullability()    // no local bindings, no groups → no-op
	require.NoError(t, sc.ResolveProjections(sch, false))
	out := sc.Export()

	// Ten carry lanes round-trip: node binding, edge binding lanes,
	// nullable, order. exportedResolvedTypes is populated by
	// ResolveProjections' wildcard expansion — that's the one lane
	// this test allows to differ from c1.
	require.Equal(t, c1.exportedNodeTypes, out.exportedNodeTypes)
	require.Equal(t, c1.exportedEdgeTypes, out.exportedEdgeTypes)
	require.Equal(t, c1.exportedEdgeKeys, out.exportedEdgeKeys)
	require.Equal(t, c1.exportedEdgeBindings, out.exportedEdgeBindings)
	require.Equal(t, c1.exportedNullableBinding, out.exportedNullableBinding)
	require.Equal(t, c1.exportedOrder, out.exportedOrder)
	// exportedResolvedTypes populated at wildcard-expand: one entry
	// per scopeOrder name.
	require.Contains(t, out.exportedResolvedTypes, "a")
	require.Contains(t, out.exportedResolvedTypes, "r")
}

// TestScopeDemoteNullability5xg is §4.1 #4. A single binding whose
// parser-time bare-ref flag is true demotes on Phase D even though
// it declared itself nullable. Re-running SeedLocalNullability
// reasserts the local Nullable() bit (idempotence).
func TestScopeDemoteNullability5xg(t *testing.T) {
	nb, err := query.NewNullableNodeBinding("a", graph.LabelSet{"Person"})
	require.NoError(t, err)
	query.MarkNodeBindingReferencedInRequiredBarePattern(&nb)
	require.True(t, nb.ReferencedInRequiredBarePattern())

	sc := newScope(branchState{})
	sc.Ingest(query.Part{Bindings: []query.Binding{nb}})
	sc.SeedLocalNullability()
	require.True(t, sc.nullableBinding["a"], "SeedLocalNullability seeds a's own Nullable() = true")
	sc.DemoteNullability()
	require.False(t, sc.nullableBinding["a"], "5xg pre-pass demotes bare-ref-flagged binding")

	// Idempotence check: re-seeding restores true, re-demoting drops it back.
	sc.SeedLocalNullability()
	require.True(t, sc.nullableBinding["a"])
	sc.DemoteNullability()
	require.False(t, sc.nullableBinding["a"])
}

// TestScopeDemoteNullability0kq is §4.1 #4b. An OPTIONAL-introduced edge
// binding whose parser-time referencedInRequiredChain flag is true is demoted
// to non-nullable on DemoteNullability even though its own Nullable() bit is
// true. This mirrors the 5xg bare-ref axis on nodes but for edge chain
// re-references (0kq).
func TestScopeDemoteNullability0kq(t *testing.T) {
	epA, err := query.NewVarEndpoint("a")
	require.NoError(t, err)
	epB, err := query.NewVarEndpoint("b")
	require.NoError(t, err)
	eb, err := query.NewNullableEdgeBindingInGroup("r", graph.LabelSet{"R"}, epA, epB, true, 1)
	require.NoError(t, err)
	query.MarkEdgeBindingReferencedInRequiredChain(&eb)
	require.True(t, eb.ReferencedInRequiredChain())
	require.True(t, eb.Nullable())

	sc := newScope(branchState{})
	sc.Ingest(query.Part{Bindings: []query.Binding{eb}})
	sc.SeedLocalNullability()
	require.True(t, sc.nullableBinding["r"], "SeedLocalNullability seeds r's own Nullable() = true")
	sc.DemoteNullability()
	require.False(t, sc.nullableBinding["r"], "0kq pre-pass demotes chain-ref-flagged edge binding")

	// Idempotence: re-seeding restores true, re-demoting drops it back.
	sc.SeedLocalNullability()
	require.True(t, sc.nullableBinding["r"])
	sc.DemoteNullability()
	require.False(t, sc.nullableBinding["r"])
}

// TestScopeDemoteNullabilityAy9CrossPart is §4.1 #5. Part 0
// introduces two OPTIONAL-group siblings a, b at group g > 0, both
// nullable. Part 1 re-MATCHes a in a required clause. Group closure
// via carriedGroups on the receiver demotes b to false — proves
// DemoteNullability reads carriedGroups off the receiver, not a
// parameter (D1 closure).
func TestScopeDemoteNullabilityAy9CrossPart(t *testing.T) {
	g := 7
	// Part 0: OPTIONAL a, b under group g.
	nbA0, err := query.NewNullableNodeBindingInGroup("a", graph.LabelSet{"Person"}, g)
	require.NoError(t, err)
	nbB0, err := query.NewNullableNodeBindingInGroup("b", graph.LabelSet{"Person"}, g)
	require.NoError(t, err)

	part0 := query.Part{
		Bindings:   []query.Binding{nbA0, nbB0},
		ReturnsAll: true,
	}
	sc0 := newScope(branchState{})
	sc0.Ingest(part0)
	nt := schema.NodeType{KeyLabels: graph.LabelSet{"Person"}.Key(), CompleteLabels: graph.LabelSet{"Person"}.Key()}
	require.NoError(t, sc0.BindNode(nbA0, nt))
	require.NoError(t, sc0.BindNode(nbB0, nt))
	sc0.SeedLocalNullability()
	sc0.DemoteNullability()
	sch := schema.Schema{Nodes: map[graph.LabelSetKey]schema.NodeType{nt.KeyLabels: nt}}
	require.NoError(t, sc0.ResolveProjections(sch, false))
	c1 := sc0.Export()
	require.Equal(t, g, c1.exportedOptionalGroup["a"])
	require.Equal(t, g, c1.exportedOptionalGroup["b"])

	// Part 1: required MATCH of a (non-nullable), no fresh group.
	nbA1, err := query.NewNodeBinding("a", graph.LabelSet{"Person"})
	require.NoError(t, err)
	part1 := query.Part{Bindings: []query.Binding{nbA1}}
	sc1 := newScope(c1)
	sc1.Ingest(part1)
	require.NoError(t, sc1.BindNode(nbA1, nt))
	sc1.SeedLocalNullability()
	require.False(t, sc1.nullableBinding["a"], "local re-MATCH overrides carry")
	sc1.DemoteNullability()
	// a's local override is a witness for group g → b (carried in the
	// same group) demotes via carriedGroups on the receiver.
	require.False(t, sc1.nullableBinding["b"], "group closure via carriedGroups demotes sibling b")
}

// TestScopeDemoteNullabilityEdgeFixedPointTwoRounds is §4.1 #6. A
// witness that requires two iterations of the edge fixed-point loop:
// proving a via edge e1 demotes group G, admitting edge e2 to prove c.
func TestScopeDemoteNullabilityEdgeFixedPointTwoRounds(t *testing.T) {
	// Setup: three nullable node bindings a, b, c under one group G.
	// One required edge e1(a, b) (Nullable=false) and one OPTIONAL edge
	// e2(b, c) in group G. Iteration 1: e1's required edge with
	// nullable-endpoint a proves a → group G demotes → b, c demoted.
	// Iteration 2: e2 (now group-demoted) is an effective witness and
	// its endpoints b, c are already false — fixed point converges.
	G := 5
	nodeLabels := graph.LabelSet{"Person"}
	nodeKey := nodeLabels.Key()
	edgeLabel := graph.LabelSet{"KNOWS"}
	edgeKey := schema.EdgeKey{Source: nodeKey, KeyLabels: edgeLabel.Key(), Target: nodeKey}
	sch := schema.Schema{
		Nodes: map[graph.LabelSetKey]schema.NodeType{nodeKey: {KeyLabels: nodeKey, CompleteLabels: nodeKey}},
		Edges: map[schema.EdgeKey]schema.EdgeType{edgeKey: {}},
	}
	nbA, err := query.NewNullableNodeBindingInGroup("a", nodeLabels, G)
	require.NoError(t, err)
	nbB, err := query.NewNullableNodeBindingInGroup("b", nodeLabels, G)
	require.NoError(t, err)
	nbC, err := query.NewNullableNodeBindingInGroup("c", nodeLabels, G)
	require.NoError(t, err)
	epA, err := query.NewVarEndpoint("a")
	require.NoError(t, err)
	epB, err := query.NewVarEndpoint("b")
	require.NoError(t, err)
	epC, err := query.NewVarEndpoint("c")
	require.NoError(t, err)
	// Required edge e1(a, b) — Nullable=false demotes its endpoints.
	e1, err := query.NewEdgeBinding("e1", edgeLabel, epA, epB, true)
	require.NoError(t, err)
	// Optional edge e2(b, c) in group G — demotable only after G is proven.
	e2, err := query.NewNullableEdgeBindingInGroup("e2", edgeLabel, epB, epC, true, G)
	require.NoError(t, err)

	sc := newScope(branchState{})
	sc.Ingest(query.Part{Bindings: []query.Binding{nbA, nbB, nbC, e1, e2}})
	nt := schema.NodeType{KeyLabels: nodeKey, CompleteLabels: nodeKey}
	require.NoError(t, sc.BindNode(nbA, nt))
	require.NoError(t, sc.BindNode(nbB, nt))
	require.NoError(t, sc.BindNode(nbC, nt))
	require.NoError(t, sc.BindEdge(e1))
	require.NoError(t, sc.BindEdge(e2))
	require.NoError(t, sc.CloseEdges(sch))
	sc.SeedLocalNullability()
	sc.DemoteNullability()

	// All three node bindings converge to false via the group + edge
	// fixed-point cascade.
	require.False(t, sc.nullableBinding["a"])
	require.False(t, sc.nullableBinding["b"])
	require.False(t, sc.nullableBinding["c"])
}

// TestScopeExportWildcardVsExplicit is §4.1 #8. ReturnsAll=true
// populates exportedResolvedTypes for every scopeOrder entry;
// explicit WITH v, e.p AS x populates exportedResolvedTypes for both
// v and x, but binding lanes only for v (the alias x lives only in
// exportedResolvedTypes — downstream refs bypass via §4.5.4).
func TestScopeExportWildcardVsExplicit(t *testing.T) {
	nodeLabels := graph.LabelSet{"Person"}
	nodeKey := nodeLabels.Key()
	nt := schema.NodeType{KeyLabels: nodeKey, CompleteLabels: nodeKey, Properties: map[string]schema.Property{"name": {Type: graph.TypeString}}}
	sch := schema.Schema{Nodes: map[graph.LabelSetKey]schema.NodeType{nodeKey: {KeyLabels: nodeKey, CompleteLabels: nodeKey, Properties: nt.Properties}}}

	// Wildcard: WITH * over a single node binding v.
	nbV, err := query.NewNodeBinding("v", nodeLabels)
	require.NoError(t, err)
	scWild := newScope(branchState{})
	scWild.Ingest(query.Part{Bindings: []query.Binding{nbV}, ReturnsAll: true})
	require.NoError(t, scWild.BindNode(nbV, nt))
	scWild.SeedLocalNullability()
	scWild.DemoteNullability()
	require.NoError(t, scWild.ResolveProjections(sch, false))
	outWild := scWild.Export()
	require.Contains(t, outWild.exportedResolvedTypes, "v")
	require.Contains(t, outWild.exportedNodeTypes, "v", "wildcard export populates binding lanes for v")

	// Explicit: WITH v, v.name AS x — v is a bare RefProjection, x is
	// an aliased property projection. Both land in
	// exportedResolvedTypes; only v lands in exportedNodeTypes.
	nbV2, err := query.NewNodeBinding("v", nodeLabels)
	require.NoError(t, err)
	items := []query.ReturnItem{
		{Name: "v", Value: query.NewRefProjection(query.Ref{Variable: "v"}, query.TypeNode{})},
		{Name: "x", Value: query.NewRefProjection(query.Ref{Variable: "v", Property: "name"}, query.TypeString{})},
	}
	scExpl := newScope(branchState{})
	scExpl.Ingest(query.Part{Bindings: []query.Binding{nbV2}, Returns: items})
	require.NoError(t, scExpl.BindNode(nbV2, nt))
	scExpl.SeedLocalNullability()
	scExpl.DemoteNullability()
	require.NoError(t, scExpl.ResolveProjections(sch, false))
	outExpl := scExpl.Export()
	require.Contains(t, outExpl.exportedResolvedTypes, "v")
	require.Contains(t, outExpl.exportedResolvedTypes, "x")
	require.Contains(t, outExpl.exportedNodeTypes, "v", "bare v export populates node binding lane")
	require.NotContains(t, outExpl.exportedNodeTypes, "x", "aliased property projection stays out of binding lanes")

	// Renamed bare binding: WITH v AS w. Export's alias guard has two
	// halves — the projection must be bare AND named by its own variable
	// — and `v.name AS x` above only exercises the first. A rename keeps
	// the binding lanes empty under both names: "w" names a projection
	// rather than a binding, and "v" no longer leaves this Part at all,
	// so downstream refs to w take the §4.5.4 carried-alias bypass.
	// Without this case the alias half could be dropped with the tree
	// green, exporting v's node type under a name nothing carries.
	nbV3, err := query.NewNodeBinding("v", nodeLabels)
	require.NoError(t, err)
	scRenamed := newScope(branchState{})
	scRenamed.Ingest(query.Part{
		Bindings: []query.Binding{nbV3},
		Returns:  []query.ReturnItem{{Name: "w", Value: query.NewRefProjection(query.Ref{Variable: "v"}, query.TypeNode{})}},
	})
	require.NoError(t, scRenamed.BindNode(nbV3, nt))
	scRenamed.SeedLocalNullability()
	scRenamed.DemoteNullability()
	require.NoError(t, scRenamed.ResolveProjections(sch, false))
	outRenamed := scRenamed.Export()
	require.Contains(t, outRenamed.exportedResolvedTypes, "w")
	require.Empty(t, outRenamed.exportedNodeTypes, "a renamed bare projection exports no binding lane, under either name")
}

// TestScopeGroupZeroIsNotAnOptionalGroup pins the one contract three
// separate guards in DemoteNullability and Export each state: group id 0
// means "not in an OPTIONAL group", not "in the group numbered zero".
//
// addMember's `g <= 0` (scope.go:403) and demoteGroup's `g == 0`
// (scope.go:427) mask each other exactly — drop either alone and the
// other still blocks the cascade, so a mutation sweep sees both survive
// and neither looks load-bearing. This test discriminates the pair
// jointly: with both dropped, b's proven non-nullability demotes "group
// 0", which sweeps up every other group-0 name including a.
//
// Export's `g > 0` (scope.go:837) is the third statement of the same
// contract, and downstream it is inert — a zero id in the carry is
// rejected by addMember on arrival. The assertion on exportedOptionalGroup
// pins it directly rather than through that masked path.
//
// The binding shape here is parser-unreachable: cypher/listener.go:159-162
// records the invariant nullable <=> optionalGroup >= 1, and build.go:280-290
// reaches the nullable constructors only when optionalGroup > 0. As with
// the shadow-cascade tests above, parser-reachability is a separate
// question — this pins the invariant at the scope layer, where the
// test-only constructors can express the violation.
func TestScopeGroupZeroIsNotAnOptionalGroup(t *testing.T) {
	nodeLabels := graph.LabelSet{"Person"}
	nodeKey := nodeLabels.Key()
	nt := schema.NodeType{KeyLabels: nodeKey, CompleteLabels: nodeKey}
	sch := schema.Schema{Nodes: map[graph.LabelSetKey]schema.NodeType{nodeKey: nt}}

	// a is nullable and carries no group; b is a plain required binding,
	// also groupless, and so is already proven non-nullable.
	nbA, err := query.NewNullableNodeBinding("a", nodeLabels)
	require.NoError(t, err)
	nbB, err := query.NewNodeBinding("b", nodeLabels)
	require.NoError(t, err)
	require.Zero(t, nbA.OptionalGroup(), "a must carry group 0 for this case to discriminate")
	require.Zero(t, nbB.OptionalGroup(), "b must carry group 0 for this case to discriminate")

	sc := newScope(branchState{})
	sc.Ingest(query.Part{Bindings: []query.Binding{nbA, nbB}, ReturnsAll: true})
	require.NoError(t, sc.BindNode(nbA, nt))
	require.NoError(t, sc.BindNode(nbB, nt))
	sc.SeedLocalNullability()
	sc.DemoteNullability()

	// b being proven says nothing about a: they share no group, because
	// group 0 is not a group.
	require.True(t, sc.nullableBinding["a"], "a shares no OPTIONAL group with b, so b's witness must not demote it")
	require.False(t, sc.nullableBinding["b"])

	require.NoError(t, sc.ResolveProjections(sch, false))
	out := sc.Export()
	require.NotContains(t, out.exportedOptionalGroup, "a", "a zero group id is absence, and must not travel in the carry")
	require.NotContains(t, out.exportedOptionalGroup, "b")
}

// TestCertifiedBareUnknownIsNotFilled pins fillLeaf's underList belt (spec
// model-change-f45qn §5). Neither mint site can produce this projection: both
// certify a list spine, so a certified BARE unknown has no grammar today. It is
// built by hand precisely for that reason — the belt guards a shape nothing
// reaches yet, and without this test deleting it is invisible to the corpus.
//
// The shape is not hypothetical. A future mint site over an engine-dependent
// result (avg) or over a fold would certify exactly this, where the unknown
// means "the server decides" — something no schema lookup is entitled to
// overwrite. Such a projection must fail toward any, never toward a
// confidently wrong concrete.
func TestCertifiedBareUnknownIsNotFilled(t *testing.T) {
	sc := newScope(branchState{})
	sc.Ingest(query.Part{})

	nb, err := query.NewNodeBinding("p", graph.LabelSet{"Person"})
	require.NoError(t, err)
	require.NoError(t, sc.BindNode(nb, schema.NodeType{
		KeyLabels:      graph.LabelSet{"Person"}.Key(),
		CompleteLabels: graph.LabelSet{"Person"}.Key(),
		Properties:     map[string]schema.Property{"id": {Name: "id", Type: graph.PropertyType("INT64")}},
	}))

	refs := []query.Ref{{Variable: "p", Property: "id"}}
	want := ResolvedProperty{Type: graph.PropertyType("INT64")}

	// The control, and it is load-bearing: the SAME refs and the same
	// certificate under a list spine do fill. Without it, a bare unknown
	// staying unknown would also be satisfied by the ref failing to resolve,
	// or by the certificate being ignored outright.
	filled, err := sc.certifiedProjectionType(query.NewTypeList(query.TypeUnknown{}), refs, true, schema.Schema{})
	require.NoError(t, err)
	require.Equal(t, ResolvedList{Element: want}, filled, "a certified unknown UNDER a list fills")

	bare, err := sc.certifiedProjectionType(query.TypeUnknown{}, refs, true, schema.Schema{})
	require.NoError(t, err)
	require.Equal(t, ResolvedUnknown{}, bare, "a certified BARE unknown must stay unknown")
}
