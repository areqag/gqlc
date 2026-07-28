package gql

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
)

// gg21Fixtures names the valid fixtures that declare a key label set explicitly
// with `=>` (optional feature GG21). Every other valid fixture omits it, so GG22
// infers the key label set from the whole label set phrase.
//
// It is a hand-kept list rather than a scan of the sources because the two tests
// below want opposite halves of the same partition, and a scan for "=>" would
// also match the token in a comment — ldbc_social_network.gql has exactly that,
// a header line recording that the syntax was rewritten away when it was
// adapted.
var gg21Fixtures = map[string]bool{
	"key_label_set.gql": true,
}

// TestInferredKeyLabelSetsCoincide pins the GG22 invariant over the fixtures with
// no `=>`: nothing declared a key label set, so one is inferred from the whole
// phrase and identity coincides with what the elements carry.
//
// This is the assertion the pre-gqlc-h9n.8 model could not make, because one
// field played both roles and the coincidence was structural rather than
// asserted. It held over every valid fixture until GG21 landed in gqlc-h9n.9,
// and was narrowed here rather than deleted: inference is still the rule for
// every declaration that does not opt out of it, which is nearly all of them.
func TestInferredKeyLabelSetsCoincide(t *testing.T) {
	for _, f := range parseValidFixtures(t) {
		if gg21Fixtures[f.name] {
			continue
		}
		t.Run(f.name, func(t *testing.T) {
			for _, nt := range f.schema.Nodes {
				require.Equal(t, nt.KeyLabels, nt.CompleteLabels,
					"no `=>`, so the key label set is inferred and coincides with the complete one")
			}
			for _, et := range f.schema.Edges {
				require.Equal(t, et.KeyLabels, et.CompleteLabels,
					"no `=>`, so the key label set is inferred and coincides with the complete one")
			}
		})
	}
}

// TestExplicitKeyLabelSetsDiverge is the other half of that partition, and the
// reason narrowing the test above is not a quiet loss of coverage: under GG21 the
// two label sets are allowed to differ, and this demands that at least one type
// actually does. Without it, "narrowed to the fixtures without a `=>`" would be
// indistinguishable from "narrowed until it passed".
//
// It also pins the direction. The complete label set is the key one plus what the
// declaration implies, so it is always a superset — never the reverse, and never
// unrelated.
func TestExplicitKeyLabelSetsDiverge(t *testing.T) {
	seen := 0
	for _, f := range parseValidFixtures(t) {
		if !gg21Fixtures[f.name] {
			continue
		}
		seen++
		t.Run(f.name, func(t *testing.T) {
			diverged := 0
			for _, nt := range f.schema.Nodes {
				requireCarries(t, nt.CompleteLabels, nt.KeyLabels)
				if nt.CompleteLabels != nt.KeyLabels {
					diverged++
				}
			}
			for _, et := range f.schema.Edges {
				requireCarries(t, et.CompleteLabels, et.KeyLabels)
				if et.CompleteLabels != et.KeyLabels {
					diverged++
				}
			}
			require.NotZero(t, diverged,
				"a GG21 fixture must declare a type whose complete label set exceeds its key one, or it pins nothing")
		})
	}
	require.Equal(t, len(gg21Fixtures), seen, "every fixture named in gg21Fixtures must exist")
}

// TestElementTypesAreKeyedByTheirKeyLabelSet pins the half of the invariant GG21
// does not touch, so it runs over every valid fixture: whatever a type carries,
// it is indexed by its identity alone, and an edge's endpoints name node type
// identities. That is what makes the key label set a key.
func TestElementTypesAreKeyedByTheirKeyLabelSet(t *testing.T) {
	for _, f := range parseValidFixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			require.NotEmpty(t, f.schema.Nodes)

			for id, nt := range f.schema.Nodes {
				require.Equal(t, id, nt.KeyLabels, "node type identity is the map key")
			}
			for id, et := range f.schema.Edges {
				require.Equal(t, id, et.EdgeKey, "edge type identity is the map key")
				_, ok := f.schema.Nodes[et.Source]
				require.True(t, ok, "an edge's source names a declared node type's key label set")
				_, ok = f.schema.Nodes[et.Target]
				require.True(t, ok, "an edge's target names a declared node type's key label set")
			}
		})
	}
}

// requireCarries asserts every label of key appears in complete.
func requireCarries(t *testing.T, complete, key graph.LabelSetKey) {
	t.Helper()
	carried := make(map[string]bool)
	for _, l := range complete.Split() {
		carried[l] = true
	}
	for _, l := range key.Split() {
		require.True(t, carried[l], "complete label set %q must carry key label %q", complete, l)
	}
}

// validFixture is one parsed file from the valid fixture directory, kept beside
// its base name so the tests below can partition on gg21Fixtures.
type validFixture struct {
	name   string
	schema schema.Schema
}

// parseValidFixtures parses every valid fixture once. The three tests each run
// their own subtests over the result rather than taking a callback, which keeps
// a failure reported at the assertion that made it.
func parseValidFixtures(t *testing.T) []validFixture {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(fixtureDir, "valid", "*.gql"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	out := make([]validFixture, 0, len(files))
	for _, path := range files {
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		s, err := New().Parse(bytes.NewReader(src))
		require.NoError(t, err)
		out = append(out, validFixture{name: filepath.Base(path), schema: s})
	}
	return out
}
