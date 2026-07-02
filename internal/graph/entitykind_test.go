package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
)

// TestEntityKindString pins the lowercase names the query model's JSON
// discriminator derives from.
func TestEntityKindString(t *testing.T) {
	require.Equal(t, "node", graph.Node.String())
	require.Equal(t, "edge", graph.Edge.String())
}
