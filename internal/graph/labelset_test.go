package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/graph"
)

// TestAnAmpersandBearingLabelIsNotTheSetItSpells is this bead's falsifier, stated
// as gqlc-yd4ba wrote it: before the quoting landed, LabelSet{"A&B"}.Key() and
// LabelSet{"A","B"}.Key() were the same string, so a one-label set forged the
// identity of a two-label one. The key IS an element type's identity — it indexes
// Schema.Nodes, fills all three fields of schema.EdgeKey, and reaches schema JSON
// and codegen — so the collision decided which type was which.
//
// Both halves are needed. Distinctness alone is satisfied by any respelling; the
// Split assertion is what says the label the author wrote is the label that comes
// back, rather than some mangling that merely happens to be unique.
func TestAnAmpersandBearingLabelIsNotTheSetItSpells(t *testing.T) {
	one := graph.LabelSet{"A&B"}.Key()
	two := graph.LabelSet{"A", "B"}.Key()

	require.NotEqual(t, two, one,
		"the one-label set {A&B} keys identically to the two-label set {A,B}: type-identity forgery")
	require.Equal(t, graph.LabelSet{"A&B"}, one.Split(),
		"the key must decode to the label as written")
	require.Equal(t, graph.LabelSet{"A", "B"}, two.Split())
}

// TestLabelSetKeyRoundTrips is Split stated as Key's inverse over every shape the
// encoding distinguishes: labels that need no quoting, labels quoted for each of
// the three reasons needsQuoting names, and the mixed sets where sort order and
// quoting interact.
//
// The two directions are not the same assertion. Split(Key(s)) says no label is
// lost or altered; Key(Split(k)) says the key has ONE spelling, which is what
// makes it usable as a map key at all — a second encoding of the same set would
// index a second entry in Schema.Nodes.
//
// The {"a","b&"} row additionally pins WHERE the sort happens: raw labels are
// sorted and only then encoded. Encoded-first sorting yields "`b&`&a", because
// "`" (0x60) sorts below "a" (0x61) — a different spelling of the same set, and
// therefore a different identity for the same type.
func TestLabelSetKeyRoundTrips(t *testing.T) {
	for _, tt := range []struct {
		name string
		set  graph.LabelSet
		key  graph.LabelSetKey
		want graph.LabelSet // sorted, compacted
	}{
		{"a bare label", graph.LabelSet{"A"}, "A", graph.LabelSet{"A"}},
		{"two bare labels", graph.LabelSet{"A", "B"}, "A&B", graph.LabelSet{"A", "B"}},
		{"the separator inside a label", graph.LabelSet{"A&B"}, "`A&B`", graph.LabelSet{"A&B"}},
		{"an interior backtick stays literal", graph.LabelSet{"a`b"}, "a`b", graph.LabelSet{"a`b"}},
		{"a leading backtick is quoted and doubled", graph.LabelSet{"`x"}, "```x`", graph.LabelSet{"`x"}},
		{"the empty label", graph.LabelSet{""}, "``", graph.LabelSet{""}},
		{"a quoted label beside a bare one", graph.LabelSet{"A&B", "C"}, "`A&B`&C", graph.LabelSet{"A&B", "C"}},
		{"a label that is only the separator", graph.LabelSet{"&"}, "`&`", graph.LabelSet{"&"}},
		{"raw sort, then quote", graph.LabelSet{"a", "b&"}, "a&`b&`", graph.LabelSet{"a", "b&"}},
		{"a quoted label carrying a literal backtick", graph.LabelSet{"x`y&z"}, "`x``y&z`", graph.LabelSet{"x`y&z"}},
		{"the empty set", graph.LabelSet{}, "", nil},
		{"an unsorted, repeating set", graph.LabelSet{"B", "A", "B"}, "A&B", graph.LabelSet{"A", "B"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.set.Key()
			require.Equal(t, tt.key, key, "canonical key spelling")
			require.Equal(t, tt.want, key.Split(), "Split is Key's inverse")
			require.Equal(t, tt.key, tt.want.Key(), "Key is Split's inverse: the key has one spelling")
		})
	}
}

// TestLabelSetKeysAreDistinct is the collision matrix. Every entry is a DIFFERENT
// set of labels, so every pair of keys must differ; the rows are chosen so that
// each pair collides under some plausible weakening of the encoding — dropping the
// quoting, quoting without doubling, or splitting on a bare "&" regardless of
// quoting.
//
// A round-trip test alone does not reach this: an encoding could be injective on
// each row it is handed and still map two DIFFERENT sets onto one string, because
// the round-trip only ever compares a set against itself.
func TestLabelSetKeysAreDistinct(t *testing.T) {
	sets := []graph.LabelSet{
		{"A&B"},
		{"A", "B"},
		{"A`B"},
		{"A&", "B"},
		{"A", "&B"},
		{"A", "&", "B"},
	}

	seen := make(map[graph.LabelSetKey]graph.LabelSet, len(sets))
	for _, set := range sets {
		key := set.Key()
		prior, dup := seen[key]
		require.False(t, dup, "%v and %v both key as %q", prior, set, key)
		seen[key] = set
	}
	require.Len(t, seen, len(sets))
}
