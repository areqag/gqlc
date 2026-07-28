package resolver

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// Every declared type in this fixture has a key label set that differs from its
// complete label set. No fixture file can produce one: gqlc rejects GG21 (`=>`)
// at parse time, so the GQL builder always infers a key label set (GG22) equal
// to the complete one. Building the divergence directly is the only way to
// observe which of the two each consumer reads, and without it gqlc-h9n.8's
// split would be a rename indistinguishable from the single-field model it
// replaced.
//
// Reads as: NODE TYPE Engineer (:Engineer => :Person), NODE TYPE Manager
// (:Manager => :Person), plus a KNOWS edge between the two.
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

	nt, err := resolveNodeLabels(graph.LabelSet{"Engineer", "Person"}, s)
	require.NoError(t, err)
	require.Equal(t, graph.LabelSetKey("Engineer"), nt.KeyLabels)

	require.True(t, labelDeclared("Person", s), "an implied label is declared")
	require.Empty(t, undeclaredLabels(graph.LabelSet{"Person"}, s))
}

// A resolved type is named by its identity, never by what it carries. The
// round-trip is the assertion: the key label set indexes Schema.Nodes, the
// complete one does not.
func TestResolvedNodeTypeIsNamedByItsKeyLabelSet(t *testing.T) {
	s := divergentSchema()

	nt, err := resolveNodeLabels(graph.LabelSet{"Manager"}, s)
	require.NoError(t, err)

	back, ok := s.Nodes[nt.KeyLabels]
	require.True(t, ok, "the key label set indexes Schema.Nodes")
	require.Equal(t, nt, back)

	_, ok = s.Nodes[nt.CompleteLabels]
	require.False(t, ok, "the complete label set is not an identity")
}

// A label implied by two declarations is satisfied by both, and the plural case
// is ErrAmbiguousLabel, not ErrUnknownLabel. Under the pre-gqlc-h9n.8 model this
// schema was inexpressible, so the arm could not be reached this way.
func TestImpliedLabelSharedByTwoTypesIsAmbiguous(t *testing.T) {
	s := divergentSchema()

	_, err := resolveNodeLabels(graph.LabelSet{"Person"}, s)
	require.ErrorIs(t, err, ErrAmbiguousLabel)
	require.NotErrorIs(t, err, ErrUnknownLabel)
	require.Contains(t, err.Error(), "Engineer")
	require.Contains(t, err.Error(), "Manager")
}

// EdgeKey.Source and .Target hold node type identities, so endpoint keying must
// read the key label set. Feed it the complete one and edgeCandidates finds
// nothing, because no declared edge is anchored at "Engineer&Person".
func TestEdgeEndpointKeyingUsesTheKeyLabelSet(t *testing.T) {
	s := divergentSchema()

	src, err := resolveNodeLabels(graph.LabelSet{"Engineer"}, s)
	require.NoError(t, err)
	tgt, err := resolveNodeLabels(graph.LabelSet{"Manager"}, s)
	require.NoError(t, err)

	epSrc, err := query.NewVarEndpoint("a")
	require.NoError(t, err)
	resolved := map[string]schema.NodeType{"a": src}
	got, ok := endpointLabels(epSrc, resolved)
	require.True(t, ok)
	require.Equal(t, src.KeyLabels, got)
	require.NotEqual(t, src.CompleteLabels, got)

	eb, err := makeTestEdgeBinding("r")
	require.NoError(t, err)

	cands := edgeCandidates(eb, src.KeyLabels, tgt.KeyLabels, s)
	require.Len(t, cands, 1)
	require.Equal(t, graph.LabelSetKey("Engineer"), cands[0].Source)

	require.Empty(t, edgeCandidates(eb, src.CompleteLabels, tgt.CompleteLabels, s),
		"complete label sets do not key the edge table")
}
