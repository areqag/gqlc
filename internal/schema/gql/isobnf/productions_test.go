package isobnf_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/schema/gql/isobnf"
)

// TestDDLClosureSortedAndUnique pins what extract_ddl_closure.py produces. The
// value of a vendored list is that a re-vendor shows up as a readable diff; two
// authors regenerating with different orderings would make that diff useless.
func TestDDLClosureSortedAndUnique(t *testing.T) {
	require.True(t, sort.StringsAreSorted(isobnf.DDLClosure), "isobnf.DDLClosure is not sorted")

	seen := make(map[string]struct{}, len(isobnf.DDLClosure))
	for _, p := range isobnf.DDLClosure {
		_, dup := seen[p]
		require.False(t, dup, "isobnf.DDLClosure has duplicate %q", p)
		seen[p] = struct{}{}
	}
}

// TestSnapshotCardinality pins the counts from the 2026-07-26 fetch. These move
// only when ISO publishes a new edition, and moving them is the deliberate act
// SOURCE.md's drift-check section describes — not something a refactor does.
func TestSnapshotCardinality(t *testing.T) {
	require.Len(t, isobnf.DDLClosure, 200, "DDL closure size changed; re-read SOURCE.md before updating")
	require.Equal(t, 814, isobnf.TotalProductions, "artefact production count changed; ISO published a new edition?")
	require.Len(t, isobnf.SourceSHA256, 64, "SourceSHA256 is not a sha256 hex digest")
}

// TestRootsAreInTheClosure guards the failure mode where a regeneration silently
// produces a closure from the wrong roots: every root is trivially reachable from
// itself, so their absence means the roots were renamed or the walk broke.
func TestRootsAreInTheClosure(t *testing.T) {
	for _, root := range []string{
		"create graph type statement",
		"drop graph type statement",
		"nested graph type specification",
	} {
		require.Contains(t, isobnf.DDLClosure, root, "root production missing from closure")
	}
}

// TestGraphExpressionIsAFrontierNotAMember pins the one subtle thing about the
// closure: <graph expression> is cut, and a cut production is itself in the list
// (DDL really does reference it, via `LIKE <graph expression>`) while its subtree
// is not. Asserting only that the name is present would pass just as well if the
// cut stopped applying, so the load-bearing half is the absence of the 538
// query-language productions reachable through it.
func TestGraphExpressionIsAFrontierNotAMember(t *testing.T) {
	require.Contains(t, isobnf.DDLClosure, "graph expression",
		"the frontier production itself belongs in the closure")

	for _, beyond := range []string{
		"aggregate function",
		"all shortest path search",
		"ambient linear query statement",
	} {
		require.NotContains(t, isobnf.DDLClosure, beyond,
			"the <graph expression> cut stopped applying; the denominator has grown into the query language, see SOURCE.md")
	}
}
