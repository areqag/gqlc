package resolver

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// Every declared type in this fixture has a key label set that differs from its
// complete label set. It reads as NODE TYPE (:Engineer => :Person), NODE TYPE
// (:Manager => :Person), plus a KNOWS edge between the two — a legal graph type
// since gqlc-h9n.9 implemented GG21, and one no fixture file could express before
// it, when `=>` was rejected at parse time and every built key label set was
// therefore inferred (GG22) equal to its complete one.
//
// It stays built in Go rather than parsed now that it could be parsed, because
// these tests state a resolver contract: which of the two label sets each
// consumer reads. Routing the setup through the GQL parser would let a parser
// change quietly gut the assertions, and a unit test of resolver behaviour should
// not need a schema file to say what it means.
func divergentSchema() schema.Schema {
	engineer := schema.NodeType{
		KeyLabels:      graph.LabelSet{"Engineer"}.Key(),
		CompleteLabels: graph.LabelSet{"Engineer", "Person"}.Key(),
	}
	manager := schema.NodeType{
		KeyLabels:      graph.LabelSet{"Manager"}.Key(),
		CompleteLabels: graph.LabelSet{"Manager", "Person"}.Key(),
	}
	knows := schema.EdgeKey{
		Source:    engineer.KeyLabels,
		KeyLabels: graph.LabelSet{"KNOWS"}.Key(),
		Target:    manager.KeyLabels,
	}
	return schema.Schema{
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			engineer.KeyLabels: engineer,
			manager.KeyLabels:  manager,
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			knows: {EdgeKey: knows, CompleteLabels: knows.KeyLabels},
		},
	}
}

// An implied label is one the element carries, so a query naming it must
// resolve. Point satisfaction at identity instead and this fails with
// ErrUnknownLabel, because no declared type is identified by "Person".
func TestSatisfactionReadsCompleteLabels(t *testing.T) {
	s := divergentSchema()

	nts, err := resolveNodeLabels(graph.LabelSet{"Engineer", "Person"}, s)
	require.NoError(t, err)
	require.Len(t, nts, 1)
	require.Equal(t, graph.LabelSetKey("Engineer"), nts[0].KeyLabels)

	require.True(t, labelDeclared("Person", s), "an implied label is declared")
	require.Empty(t, undeclaredLabels(graph.LabelSet{"Person"}, s))
}

// A resolved type is named by its identity, never by what it carries. The
// round-trip is the assertion: the key label set indexes Schema.Nodes, the
// complete one does not.
func TestResolvedNodeTypeIsNamedByItsKeyLabelSet(t *testing.T) {
	s := divergentSchema()

	nts, err := resolveNodeLabels(graph.LabelSet{"Manager"}, s)
	require.NoError(t, err)
	require.Len(t, nts, 1)
	nt := nts[0]

	back, ok := s.Nodes[nt.KeyLabels]
	require.True(t, ok, "the key label set indexes Schema.Nodes")
	require.Equal(t, nt, back)

	_, ok = s.Nodes[nt.CompleteLabels]
	require.False(t, ok, "the complete label set is not an identity")
}

// A label implied by two declarations is satisfied by both. Under ADR 0022
// (option B), resolveNodeLabels returns the plural set rather than erroring.
// The ErrAmbiguousLabel moves to the projection phase (whole-entity RETURN p).
func TestImpliedLabelSharedByTwoTypesReturnsPluralSlice(t *testing.T) {
	s := divergentSchema()

	nts, err := resolveNodeLabels(graph.LabelSet{"Person"}, s)
	require.NoError(t, err)
	require.Len(t, nts, 2)
	// Both Engineer and Manager must appear.
	keys := make(map[graph.LabelSetKey]struct{})
	for _, nt := range nts {
		keys[nt.KeyLabels] = struct{}{}
	}
	require.Contains(t, keys, graph.LabelSetKey("Engineer"))
	require.Contains(t, keys, graph.LabelSetKey("Manager"))
}

// EdgeKey.Source and .Target hold node type identities, so endpoint keying must
// read the key label set. Feed it the complete one and edgeCandidates finds
// nothing, because no declared edge is anchored at "Engineer&Person".
func TestEdgeEndpointKeyingUsesTheKeyLabelSet(t *testing.T) {
	s := divergentSchema()

	nts, err := resolveNodeLabels(graph.LabelSet{"Engineer"}, s)
	require.NoError(t, err)
	require.Len(t, nts, 1)
	src := nts[0]

	ntst, err := resolveNodeLabels(graph.LabelSet{"Manager"}, s)
	require.NoError(t, err)
	require.Len(t, ntst, 1)
	tgt := ntst[0]

	epSrc, err := query.NewVarEndpoint("a")
	require.NoError(t, err)
	resolved := map[string]schema.NodeType{"a": src}
	nodeCands := map[string][]schema.NodeType{}
	got, ok := endpointLabels(epSrc, resolved, nodeCands)
	require.True(t, ok)
	require.Len(t, got, 1)
	require.Equal(t, src.KeyLabels, got[0])
	require.NotEqual(t, src.CompleteLabels, got[0])

	eb, err := makeTestEdgeBinding("r")
	require.NoError(t, err)

	cands := edgeCandidates(eb, []graph.LabelSetKey{src.KeyLabels}, []graph.LabelSetKey{tgt.KeyLabels}, s)
	require.Len(t, cands, 1)
	require.Equal(t, graph.LabelSetKey("Engineer"), cands[0].Source)

	require.Empty(t, edgeCandidates(eb, []graph.LabelSetKey{src.CompleteLabels}, []graph.LabelSetKey{tgt.CompleteLabels}, s),
		"complete label sets do not key the edge table")
}
