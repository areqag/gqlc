package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
)

// TestEntityKindString pins the lowercase names of the two kinds. It is the
// only caller of String in the module — `go build ./...` is clean without the
// method (measured on bd gqlc-r79zi) — so the wire spellings it shares with
// query.BindingKind.String, which is what the JSON discriminator actually
// derives from, are held here and matched there by construction alone.
func TestEntityKindString(t *testing.T) {
	require.Equal(t, "node", graph.Node.String())
	require.Equal(t, "edge", graph.Edge.String())
}
