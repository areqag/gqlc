package cypher_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
)

// --- the ref-valued-leaf certificate, parser side (gqlc-t0bk, spec
// docs/specs/model-change-f45qn-ref-valued-leaves.md §4 and §7) ---
//
// The certificate is what lets the resolver fill a rich projection's committed
// TypeUnknown leaf from the schema. It asserts three things (§2): the leaf
// values are exactly the projection's Refs, the Refs are exhaustive, and the
// committed type's list spine agrees with the value's actual structure. The
// mint predicate is the only thing standing behind all three, so this table
// pins both sides of every line it draws — a REFUSED row is worth nothing
// without the accepted row one character away from it.

// TestCertificateMintPredicate walks the shapes the mint predicate must
// separate. The two mint sites are read through the same table because they
// answer the same question about the same argument grammar; what differs is
// which projection variant carries the answer.
func TestCertificateMintPredicate(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want bool
	}{
		// --- ExprProjection: uniform ref-tree list literals ---
		{
			// The bead's own shape. Both elements are bare property
			// lookups at one depth, so list<unknown>'s single leaf holds
			// nothing but ref values.
			name: "a flat list of property lookups mints",
			src:  "MATCH (p:Person) RETURN [p.id, p.age] AS xs",
			want: true,
		},
		{
			name: "a list of bare variables mints",
			src:  "MATCH (p:Person) RETURN [p, p] AS xs",
			want: true,
		},
		{
			name: "a single-element list mints",
			src:  "MATCH (p:Person) RETURN [p.id] AS xs",
			want: true,
		},
		{
			// Depth is not the stopping rule. The spine is already
			// committed in the type as list<list<unknown>>, so the bit
			// adds nothing and a cap would be defended by nothing.
			name: "a uniformly nested list mints at depth two",
			src:  "MATCH (p:Person) RETURN [[p.id], [p.age]] AS xs",
			want: true,
		},
		{
			// Ragged LENGTHS are fine — the committed type has one leaf
			// per level regardless of how many values sit at each. Only
			// ragged DEPTH breaks clause 3.
			name: "nested lists of different lengths mint",
			src:  "MATCH (p:Person) RETURN [[p.id, p.age], [p.id]] AS xs",
			want: true,
		},
		{
			// §2 clause 3's falsifier, and the reason the predicate
			// returns a depth rather than a bool. This commits
			// list<unknown> — ONE leaf for two value shapes — so a fill
			// would type p.age as a list and [p.id]'s contents as a
			// scalar. Both confidently wrong.
			name: "mixed depth does not mint",
			src:  "MATCH (p:Person) RETURN [[p.id], p.age] AS xs",
			want: false,
		},
		{
			// The falsifier for inferring from the flat ref set alone:
			// this carries the same one ref as [p.id], which mints.
			name: "a scalar literal beside a ref does not mint",
			src:  "MATCH (p:Person) RETURN [p.id, 3] AS xs",
			want: false,
		},
		{
			name: "a parameter beside a ref does not mint",
			src:  "MATCH (p:Person) RETURN [p.id, $x] AS xs",
			want: false,
		},
		{
			// A fold is not a lookup. The refs are p.id and p.age either
			// way, which is exactly why the certificate exists.
			name: "arithmetic over refs does not mint",
			src:  "MATCH (p:Person) RETURN [p.id + p.age] AS xs",
			want: false,
		},
		{
			name: "a function call over a ref does not mint",
			src:  "MATCH (p:Person) RETURN [size(p.tags)] AS xs",
			want: false,
		},
		{
			// An accessor that is non-nil holds nothing here. There are no
			// leaves to certify, and list<unknown> must stay unknown.
			name: "an empty list does not mint",
			src:  "MATCH (p:Person) RETURN [] AS xs",
			want: false,
		},
		{
			// TypeMap carries no value types, so there is no leaf to fill
			// even though every value is a ref (§9 non-goal).
			name: "a map literal of refs does not mint",
			src:  "MATCH (p:Person) RETURN {a: p.id, b: p.age} AS m",
			want: false,
		},
		{
			// The WITH-position projection mints on the same predicate,
			// and the carried alias inherits its type downstream for free.
			name: "a WITH-position list literal mints",
			src:  "MATCH (p:Person) WITH [p.id, p.age] AS xs RETURN xs",
			want: true,
		},

		// --- AggregateProjection: collect alone ---
		{
			// collect(T) = list<T> puts the operand's values at the leaf
			// verbatim, which is the certificate's claim exactly.
			name: "collect over a property lookup mints",
			src:  "MATCH (p:Person) RETURN collect(p.id) AS xs",
			want: true,
		},
		{
			name: "DISTINCT does not block minting",
			src:  "MATCH (p:Person) RETURN collect(DISTINCT p.id) AS xs",
			want: true,
		},
		{
			name: "collect over a certified list literal mints",
			src:  "MATCH (p:Person) RETURN collect([p.id, p.age]) AS xs",
			want: true,
		},
		{
			name: "collect over a rich operand does not mint",
			src:  "MATCH (p:Person) RETURN collect(size(p.tags)) AS xs",
			want: false,
		},
		{
			// The arity half of the predicate, which nothing else in the
			// pipeline defends: the grammar accepts a multi-argument
			// aggregate and the resolver checks arity for procedures only,
			// so this query reaches the mint site intact. Its refs are
			// p.id and p.age at one depth — indistinguishable from
			// collect([p.id, p.age]), which mints — and collect(x, y) has
			// no defined leaf for a fill to land on.
			name: "collect over two arguments does not mint",
			src:  "MATCH (p:Person) RETURN collect(p.id, p.age) AS xs",
			want: false,
		},
		{
			// sum/min/max commit the result of a FOLD: the leaf holds a
			// value no ref ever held, so clause 1 is false however
			// ref-shaped the operand is. Each is pinned against the
			// collect row above, which differs only in the function name.
			name: "sum over a ref does not mint",
			src:  "MATCH (p:Person) RETURN sum(p.id) AS s",
			want: false,
		},
		{
			name: "min over a ref does not mint",
			src:  "MATCH (p:Person) RETURN min(p.id) AS s",
			want: false,
		},
		{
			name: "max over a ref does not mint",
			src:  "MATCH (p:Person) RETURN max(p.id) AS s",
			want: false,
		},
		{
			// avg is engine-dependent — its unknown may mean "the server
			// decides", which no schema lookup is entitled to overwrite.
			name: "avg over a ref does not mint",
			src:  "MATCH (p:Person) RETURN avg(p.id) AS s",
			want: false,
		},
		{
			// count's result is TypeInt with no unknown leaf, and it is
			// not collect, so the bit stays false on both counts.
			name: "count over a ref does not mint",
			src:  "MATCH (p:Person) RETURN count(p.id) AS s",
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, leavesAreRefs(t, tc.src))
		})
	}
}

// leavesAreRefs parses src and reports the certificate on the LAST part's sole
// return item. The last part is what a WITH-position row needs: `WITH [...] AS
// xs RETURN xs` mints on the WITH part, and reading the first part would make
// that row pass for the wrong reason. A single return item is required so a
// row cannot silently read a neighbour's projection.
func leavesAreRefs(t *testing.T, src string) bool {
	t.Helper()

	q, err := cypher.New().Parse(strings.NewReader(src))
	require.NoError(t, err, "the row's query must parse for its answer to mean anything")
	require.Len(t, q.Branches, 1)
	parts := q.Branches[0].Parts
	require.NotEmpty(t, parts)

	// The WITH-position row's answer lives on the part that mints, not the
	// part that projects the carried alias.
	for i := len(parts) - 1; i >= 0; i-- {
		require.Len(t, parts[i].Returns, 1, "part %d projects more than one item", i)
		switch p := parts[i].Returns[0].Value.(type) {
		case query.ExprProjection:
			return p.LeavesAreRefs()
		case query.AggregateProjection:
			return p.LeavesAreRefs()
		}
	}
	t.Fatalf("no rich projection in %q; the row cannot answer the question it asks", src)
	return false
}
