package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// TestMarshalJSONIsDeterministic guards the property that makes Schema safe to
// serialise: marshalling the same model twice yields byte-identical output, so a
// regenerated artefact (a .golden.json today, generated code later) never diffs
// from Go's randomised map iteration order alone. The fixture deliberately uses
// out-of-order map entries so a naive marshal would flap.
func TestMarshalJSONIsDeterministic(t *testing.T) {
	s := schema.Schema{
		Name: "G",
		Nodes: map[graph.LabelSetKey]schema.NodeType{
			"B": {KeyLabels: "B", CompleteLabels: "B", Properties: map[string]schema.Property{
				"y": {Name: "y", Type: graph.TypeInt, Nullable: true},
				"x": {Name: "x", Type: graph.TypeString},
			}},
			"A": {KeyLabels: "A", CompleteLabels: "A"},
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			{Source: "B", KeyLabels: "R", Target: "A"}: {EdgeKey: schema.EdgeKey{Source: "B", KeyLabels: "R", Target: "A"}},
			{Source: "A", KeyLabels: "R", Target: "B"}: {EdgeKey: schema.EdgeKey{Source: "A", KeyLabels: "R", Target: "B"}},
		},
	}

	first, err := json.MarshalIndent(s, "", "  ")
	require.NoError(t, err)
	for range 8 {
		next, err := json.MarshalIndent(s, "", "  ")
		require.NoError(t, err)
		require.Equal(t, string(first), string(next))
	}
}
