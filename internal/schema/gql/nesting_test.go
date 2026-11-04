package gql_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/grammar/nesting"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// recordSchema declares one property whose value type nests records deep
// enough that its innermost brace sits at depth: the graph type's brace, the
// node's parenthesis and its property brace are the first three.
func recordSchema(depth int) string {
	k := depth - 3
	return "CREATE PROPERTY GRAPH TYPE T AS { (:A { p :: " +
		strings.Repeat("RECORD { a :: ", k) + "INT" + strings.Repeat(" }", k) + " }) }"
}

// TestNestingDepthBound pins nesting.MaxDepth from both sides (bd
// gqlc-jjot2.31). A real schema exactly at the limit resolves; for each bracket
// kind, and for a compound token that opens one, a source one past the limit
// is refused with ErrNestingTooDeep and one at it is not.
func TestNestingDepthBound(t *testing.T) {
	t.Run("schema at the limit", func(t *testing.T) {
		_, err := gql.New().Parse(strings.NewReader(recordSchema(nesting.MaxDepth)))
		require.NoError(t, err)
	})
	t.Run("schema past the limit", func(t *testing.T) {
		s, err := gql.New().Parse(strings.NewReader(recordSchema(nesting.MaxDepth + 1)))
		require.ErrorIs(t, err, gql.ErrNestingTooDeep)
		require.Equal(t, schema.Schema{}, s)
	})
	for _, open := range []string{"(", "[", "{", "-[", "<-[", "~[", "<~["} {
		t.Run(open+"/at the limit", func(t *testing.T) {
			_, err := gql.New().Parse(strings.NewReader(strings.Repeat(open, nesting.MaxDepth)))
			require.Error(t, err, "a run of openers is no schema")
			require.NotErrorIs(t, err, gql.ErrNestingTooDeep)
		})
		t.Run(open+"/past the limit", func(t *testing.T) {
			_, err := gql.New().Parse(strings.NewReader(strings.Repeat(open, nesting.MaxDepth+1)))
			require.ErrorIs(t, err, gql.ErrNestingTooDeep)
		})
	}
	// A compound closer closes: ']-' after each '[' keeps the depth at one.
	t.Run("compound closer", func(t *testing.T) {
		_, err := gql.New().Parse(strings.NewReader(strings.Repeat("[ ]-", 2*nesting.MaxDepth)))
		require.NotErrorIs(t, err, gql.ErrNestingTooDeep)
	})
}

// TestNestingLeadingClosersBuyNoDepth pins the floor at depth 0: closers
// before any opener must not let the openers after them nest deeper.
func TestNestingLeadingClosersBuyNoDepth(t *testing.T) {
	src := strings.Repeat("}", 10) + strings.Repeat("{", nesting.MaxDepth+1)
	_, err := gql.New().Parse(strings.NewReader(src))
	require.ErrorIs(t, err, gql.ErrNestingTooDeep)
}

// TestLoadRefusesNestingTooDeep pins the Loader's path through the same
// check: it shares parse with Parse, but a Loader is what the pipeline calls.
func TestLoadRefusesNestingTooDeep(t *testing.T) {
	fsys := fstest.MapFS{"a.gql": file(strings.Repeat("{", nesting.MaxDepth+1))}
	_, err := gql.NewLoader(fsys).Load("a.gql")
	require.ErrorIs(t, err, gql.ErrNestingTooDeep)
}
