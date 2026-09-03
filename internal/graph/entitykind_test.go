package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
)

// TestEntityKindString pins the lowercase names of the two kinds.
//
// It is no longer the module's only caller of String: query.BindingKind.String's
// "node" and "edge" arms call this method, so these two spellings are what the
// JSON discriminator puts on the wire for node and edge bindings (bd
// gqlc-avtrx). Changing either therefore reds internal/query as well as this
// test — which is the point. Until that change it reddened this test alone, and
// the whole of internal/query stayed green over a changed wire tag.
func TestEntityKindString(t *testing.T) {
	require.Equal(t, "node", graph.Node.String())
	require.Equal(t, "edge", graph.Edge.String())
}
