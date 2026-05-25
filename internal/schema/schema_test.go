package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/antranig-yeretzian/gqlc/internal/schema"
)

// TestMarshalJSONIsDeterministic guards the property that makes Schema safe to
// serialize: marshaling the same model twice yields byte-identical output, so a
// regenerated artifact (a .golden.json today, generated code later) never diffs
// from Go's randomized map iteration order alone. The fixture deliberately uses
// out-of-order map entries so a naive marshal would flap.
func TestMarshalJSONIsDeterministic(t *testing.T) {
	s := schema.Schema{
		Name: "G",
		Nodes: map[schema.LabelSetKey]schema.NodeType{
			"B": {Labels: "B", Properties: map[string]schema.Property{
				"y": {Name: "y", Type: schema.TypeInt, Nullable: true},
				"x": {Name: "x", Type: schema.TypeString},
			}},
			"A": {Labels: "A"},
		},
		Edges: map[schema.EdgeKey]schema.EdgeType{
			{Source: "B", Label: "R", Target: "A"}: {EdgeKey: schema.EdgeKey{Source: "B", Label: "R", Target: "A"}},
			{Source: "A", Label: "R", Target: "B"}: {EdgeKey: schema.EdgeKey{Source: "A", Label: "R", Target: "B"}},
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
