package cypher_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
)

// --- aggregate call arity (bd gqlc-jjot2.1) ---
//
// The grammar admits an oC_FunctionInvocation with any number of arguments,
// including none, so `RETURN count()` parses. Until gqlc-jjot2.1 the aggregate
// classifier assumed an operand and sliced args[1:] on an empty list, panicking
// the parser on user input; the same call nested in a rich expression did not
// panic but was accepted and typed as if it had an operand. openCypher gives
// every aggregate a fixed arity (count(*) is the zero-operand form of count,
// and a separate grammar alternative), and a missing argument is a
// compile-time SyntaxError: InvalidNumberOfArguments, so each row must be
// refused with ErrAggregateArity — not a panic, not an acceptance, not some
// other error. Too many arguments stays accepted (see the control below).

// TestAggregateWithWrongArgumentCountIsRefused covers every aggregate name
// aggregateFunc recognises at zero arguments, the two percentile functions at
// one, and the positions an aggregate call reaches:
// RETURN, WITH, inside a rich expression, ORDER BY, and inside an EXISTS
// subquery, where the collection walk suppresses ordinary failures.
func TestAggregateWithWrongArgumentCountIsRefused(t *testing.T) {
	for name, src := range map[string]string{
		// The fuzz probe's input, verbatim.
		"count() capitalised, the fuzz reproducer": "RETURN Count()",
		"count()":                   "RETURN count()",
		"count(DISTINCT)":           "RETURN count(DISTINCT)",
		"sum()":                     "RETURN sum()",
		"avg()":                     "RETURN avg()",
		"min()":                     "RETURN min()",
		"max()":                     "RETURN max()",
		"collect()":                 "RETURN collect()",
		"stDev()":                   "RETURN stDev()",
		"stDevP()":                  "RETURN stDevP()",
		"percentileCont()":          "RETURN percentileCont()",
		"percentileDisc()":          "RETURN percentileDisc()",
		"percentileCont, one arg":   "MATCH (n) RETURN percentileCont(n.x)",
		"percentileDisc, one arg":   "MATCH (n) RETURN percentileDisc(n.x)",
		"at WITH position":          "MATCH (n) WITH count() AS c RETURN c",
		"inside a rich expression":  "RETURN count() + 1",
		"as a function argument":    "RETURN toString(sum())",
		"at ORDER BY":               "MATCH (n) RETURN n ORDER BY count()",
		"inside an EXISTS subquery": "MATCH (n) WHERE EXISTS { MATCH (m) RETURN count() } RETURN n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := cypher.New().Parse(strings.NewReader(src))
			require.ErrorIs(t, err, cypher.ErrAggregateArity, "query: %q", src)
			require.Equal(t, query.Query{}, got, "model must be the zero value on error")
		})
	}
}

// TestAggregateWithRightArgumentCountParses is the negative control: the
// refusal must not reach a correctly-applied aggregate, count(*) included, nor a
// namespaced or non-aggregate function that happens to take no arguments. The
// two-argument rows pin the boundary's other side: too many arguments is not
// this refusal's business — certificate_test.go relies on collect(x, y) and
// min(x, y) parsing so that the mint predicate's own arity clause is tested.
func TestAggregateWithRightArgumentCountParses(t *testing.T) {
	for name, src := range map[string]string{
		"count(*)":                   "MATCH (n) RETURN count(*)",
		"count(n)":                   "MATCH (n) RETURN count(n)",
		"count(DISTINCT n)":          "MATCH (n) RETURN count(DISTINCT n)",
		"sum":                        "MATCH (n) RETURN sum(n.x)",
		"avg":                        "MATCH (n) RETURN avg(n.x)",
		"min":                        "MATCH (n) RETURN min(n.x)",
		"max":                        "MATCH (n) RETURN max(n.x)",
		"collect":                    "MATCH (n) RETURN collect(n)",
		"stDev":                      "MATCH (n) RETURN stDev(n.x)",
		"stDevP":                     "MATCH (n) RETURN stDevP(n.x)",
		"percentileCont":             "MATCH (n) RETURN percentileCont(n.x, 0.5)",
		"percentileDisc":             "MATCH (n) RETURN percentileDisc(n.x, 0.5)",
		"inside a rich expression":   "MATCH (n) RETURN count(n) + 1",
		"collect, two args":          "MATCH (n) RETURN collect(n.x, n.y)",
		"percentileCont, three args": "MATCH (n) RETURN percentileCont(n.x, 0.5, 1)",
		"a zero-arg non-aggregate":   "RETURN rand()",
		"a namespaced count()":       "RETURN my.count()",
		"a zero-arg temporal":        "RETURN date()",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := cypher.New().Parse(strings.NewReader(src))
			require.NoError(t, err, "query: %q", src)
		})
	}
}
