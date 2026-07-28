package gql

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuiltTypesHaveInferredKeyLabelSets pins the GG22 invariant across every
// valid fixture: gqlc rejects GG21 (`=>`), so no declaration carries implied
// content, so each type's key label set is inferred from its whole label set
// phrase and coincides with its complete one. Identity is the key label set, so
// it must also be the map key.
//
// This is the assertion the pre-gqlc-h9n.8 model could not make, because one
// field played both roles and the coincidence was structural rather than
// asserted. It is expected to fail the day GG21 lands (gqlc-h9n.9): at that
// point a fixture using `=>` diverges on purpose, and this test must be
// narrowed to the fixtures without one rather than deleted.
func TestBuiltTypesHaveInferredKeyLabelSets(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "valid", "*.gql"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			require.NoError(t, err)
			s, err := New().Parse(bytes.NewReader(src))
			require.NoError(t, err)
			require.NotEmpty(t, s.Nodes)

			for id, nt := range s.Nodes {
				require.Equal(t, id, nt.KeyLabels, "node type identity is the map key")
				require.Equal(t, nt.KeyLabels, nt.CompleteLabels,
					"no `=>`, so the key label set is inferred and coincides with the complete one")
			}
			for id, et := range s.Edges {
				require.Equal(t, id, et.EdgeKey, "edge type identity is the map key")
				require.Equal(t, et.KeyLabels, et.CompleteLabels,
					"no `=>`, so the key label set is inferred and coincides with the complete one")
				_, ok := s.Nodes[et.Source]
				require.True(t, ok, "an edge's source names a declared node type's key label set")
				_, ok = s.Nodes[et.Target]
				require.True(t, ok, "an edge's target names a declared node type's key label set")
			}
		})
	}
}
