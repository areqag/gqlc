package cypher_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/grammar/nesting"
	"github.com/areqag/gqlc/internal/query/cypher"
)

// nested returns a query whose one expression nests depth of opener, around
// inner, each closed by closer.
func nested(opener, inner, closer string, depth int) string {
	return "RETURN " + strings.Repeat(opener, depth) + inner + strings.Repeat(closer, depth) + " AS x"
}

// TestNestingDepthBound pins nesting.MaxDepth from both sides for each
// bracket Cypher nests: a valid query exactly at the limit parses, and the
// same query one deeper is refused with ErrNestingTooDeep (bd gqlc-jjot2.31).
func TestNestingDepthBound(t *testing.T) {
	for _, b := range []struct{ name, open, inner, close string }{
		{"parenthesis", "(", "1", ")"},
		{"list", "[", "", "]"},
		{"map", "{a: ", "1", "}"},
	} {
		t.Run(b.name+"/at the limit", func(t *testing.T) {
			_, err := cypher.New().Parse(strings.NewReader(nested(b.open, b.inner, b.close, nesting.MaxDepth)))
			require.NoError(t, err)
		})
		t.Run(b.name+"/past the limit", func(t *testing.T) {
			q, err := cypher.New().Parse(strings.NewReader(nested(b.open, b.inner, b.close, nesting.MaxDepth+1)))
			require.ErrorIs(t, err, cypher.ErrNestingTooDeep)
			require.Zero(t, q)
		})
	}
}

// TestNestingLeadingClosersBuyNoDepth pins the floor at depth 0: closers
// before any opener must not let the openers after them nest deeper.
func TestNestingLeadingClosersBuyNoDepth(t *testing.T) {
	src := strings.Repeat(")", 10) + strings.Repeat("(", nesting.MaxDepth+1)
	_, err := cypher.New().Parse(strings.NewReader(src))
	require.ErrorIs(t, err, cypher.ErrNestingTooDeep)
}
