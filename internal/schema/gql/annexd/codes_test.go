package annexd_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/schema/gql/annexd"
)

// TestCodesSortedAndUnique pins the two invariants the regeneration snippet in
// SOURCE.md produces: sorted and de-duplicated. The value of the list is that a
// human can read it; if two authors re-vendored with different sort orders the
// diffs would be uncheckable.
func TestCodesSortedAndUnique(t *testing.T) {
	require.True(t, sort.StringsAreSorted(annexd.Codes), "annexd.Codes is not sorted")

	seen := make(map[string]struct{}, len(annexd.Codes))
	for _, c := range annexd.Codes {
		_, dup := seen[c]
		require.False(t, dup, "annexd.Codes has duplicate %q", c)
		seen[c] = struct{}{}
	}
}

// TestCodesSnapshotCardinality pins the count from the 2026-07-26 fetch
// recorded in SOURCE.md. A silent shrink or grow of Codes is what the drift bead
// gqlc-4jm exists to catch durably; until then, this pin is the fallback so a
// regeneration cannot land unnoticed.
func TestCodesSnapshotCardinality(t *testing.T) {
	require.Len(t, annexd.Codes, 228, "annexd.Codes cardinality has drifted from the 2026-07-26 snapshot; update SOURCE.md if the regeneration was intentional")
}

// TestHasKnownRealCode shows the guard admits real ISO codes: the whole reason
// for closing it around a set was that nothing in this repo could check one.
// The three ids are the ones the earlier fabrications wrote — each is a real
// ISO code, and each named a different construct than the entry that cited it:
// GG02 was cited on LIKE, GE03 was cited on undirected patterns, and the codes
// are real but they name different constructs. So a check that only guards "is
// a real code" cannot catch a real code applied wrongly, but it does close the
// class where the fabricator invents a code that does not exist at all.
func TestHasKnownRealCode(t *testing.T) {
	for _, c := range []string{"GG02", "GE03", "GH02"} {
		require.True(t, annexd.Has(c), "annexd.Has(%q) is false; the vendored snapshot is missing a code the earlier corpus already cited", c)
	}
}

// TestHasRejectsFabricated is the mutation-inversion of TestHasKnownRealCode:
// a plausible-looking id ("GG99") that is not in the ISO snapshot must be
// rejected. Without this the guard would still accept the earlier fabrications
// silently, which is the failure mode this whole package exists to close.
func TestHasRejectsFabricated(t *testing.T) {
	for _, c := range []string{"GG99", "G001", "GX99", "gg02", "GG02 ", ""} {
		require.False(t, annexd.Has(c), "annexd.Has(%q) is true; a value outside the vendored snapshot must be rejected", c)
	}
}
