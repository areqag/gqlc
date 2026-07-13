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
	nt := schema.NodeType{Labels: graph.LabelSet{"Person"}.Key()}
	require.NoError(t, sc.BindNode(nb, nt))

	require.True(t, sc.HasNode("r"))
	require.False(t, sc.HasEdge("r"))
	require.NotContains(t, sc.edgeTypes, "r")
	require.NotContains(t, sc.edgeKeys, "r")
	require.NotContains(t, sc.edgeCands, "r")
	require.NotContains(t, sc.edgeBindings, "r")
}

func TestScopeBindEdgeShadowCascade(t *testing.T) {
	// Seed the scope with a carried node binding at variable "x" so
	// BindEdge's node/edgeClosed shadow arm has something to erase.
	carry := branchState{
		exportedNodeTypes: map[string]schema.NodeType{"x": {Labels: graph.LabelSet{"Person"}.Key()}},
	}
	sc := newScope(carry)
	sc.Ingest(query.Part{})

	eb, err := makeTestEdgeBinding("x")
	require.NoError(t, err)
	require.NoError(t, sc.BindEdge(eb))

	require.False(t, sc.HasNode("x"))
	require.NotContains(t, sc.nodeTypes, "x")
	_, ok := sc.edgeBindings["x"]
	require.True(t, ok)
}

func TestScopeBindCallShadowCascade(t *testing.T) {
	// Belt-and-braces: seed carried node + edge state so BindCall's
	// shadow cascade drops both when a call binding lands at the same
	// name. Parser-reachability is a separate question; this pins the
	// invariant at the scope layer.
	carriedEdge, err := makeTestEdgeBinding("c")
	require.NoError(t, err)
	carry := branchState{
		exportedNodeTypes:    map[string]schema.NodeType{"c": {Labels: graph.LabelSet{"Person"}.Key()}},
		exportedEdgeBindings: map[string]query.EdgeBinding{"c": carriedEdge},
	}
	sc := newScope(carry)
	sc.Ingest(query.Part{})

	cb, err := query.NewCallBinding("c", "test.proc", "value", query.TypeInt{}, false)
	require.NoError(t, err)
	reg, err := procsig.NewRegistry(nil)
	require.NoError(t, err)
	require.NoError(t, sc.BindCall(cb, reg))

	require.False(t, sc.HasNode("c"))
	require.False(t, sc.HasEdge("c"))
	require.True(t, sc.HasCall("c"))
}

func TestScopeSnapshotNarrowing(t *testing.T) {
	// Snapshot exposes only the five witness lanes. callTypes,
	// carriedResolvedTypes, carriedGroups, and the ingested Part are
	// not observable through partScope. §2.3 invariant #3.
	carry := branchState{
		exportedCallTypes:     map[string]callBindingSlot{"c": {procedure: "test.proc"}},
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
	require.True(t, sc.HasCall("c"))
	require.Equal(t, ResolvedUnknown{}, sc.carriedResolvedTypes["alias"])
	require.Equal(t, 7, sc.carriedGroups["opt"])
}

func TestScopeCloseEdgesWritesOnlyEdgeLanes(t *testing.T) {
	// Set up a tiny schema with one node type and one edge type.
	nodeLabels := graph.LabelSet{"Person"}.Key()
	edgeLabel := graph.LabelSet{"KNOWS"}.Key()
	sch := schema.Schema{
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			nodeLabels: {Labels: nodeLabels},
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			{Source: nodeLabels, Label: edgeLabel, Target: nodeLabels}: {},
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
	require.NoError(t, sc.BindNode(na, schema.NodeType{Labels: nodeLabels}))
	require.NoError(t, sc.BindNode(nb, schema.NodeType{Labels: nodeLabels}))
	require.NoError(t, sc.BindEdge(eb))

	// Snapshot BEFORE CloseEdges to prove only the edge lanes change.
	before := sc.Snapshot()
	require.NoError(t, sc.CloseEdges(sch))
	require.NoError(t, sc.CloseEdgesDeferred(sch))
	after := sc.Snapshot()

	require.Equal(t, before.nodeTypes, after.nodeTypes)
	require.Equal(t, before.nullableBinding, after.nullableBinding)
	// The edge closed: edgeTypes / edgeKeys populated at "r".
	require.Contains(t, sc.edgeTypes, "r")
	require.Contains(t, sc.edgeKeys, "r")
}

func TestScopeIngestSingleShot(t *testing.T) {
	sc := newScope(branchState{})
	sc.Ingest(query.Part{})
	require.Panics(t, func() { sc.Ingest(query.Part{}) })
}

// Skipped: these pin methods that land in steps 3-6 of §5. Enabling
// their assertions before the corresponding step would fail to
// compile against the current stub-free scope surface.

func TestScopeCarryForwardRoundTrip(t *testing.T) {
	t.Skip("blocked on step 5 (Export)")
}

func TestScopeDemoteNullability5xg(t *testing.T) {
	t.Skip("blocked on step 3 (Demote)")
}

func TestScopeDemoteNullabilityAy9CrossPart(t *testing.T) {
	t.Skip("blocked on step 3 (Demote)")
}

func TestScopeDemoteNullabilityEdgeFixedPointTwoRounds(t *testing.T) {
	t.Skip("blocked on step 3 (Demote)")
}

func TestScopeExportWildcardVsExplicit(t *testing.T) {
	t.Skip("blocked on step 5 (Export)")
}
