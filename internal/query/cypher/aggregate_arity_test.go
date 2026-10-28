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
// other error. bd gqlc-jjot2.12 made the count exact: too many arguments is the
// same InvalidNumberOfArguments, and min(a, b) used to be typed from a alone.

// TestAggregateWithWrongArgumentCountIsRefused covers every aggregate name
// aggregateFunc recognises at zero arguments and at one too many, the two
// percentile functions at one, and the positions an aggregate call reaches:
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
		"count, two args":           "MATCH (n) RETURN count(n, 1)",
		"count(DISTINCT), two args": "MATCH (n) RETURN count(DISTINCT n, n)",
		"sum, two args":             "MATCH (n) RETURN sum(n.x, n.y)",
		"avg, two args":             "MATCH (n) RETURN avg(n.x, n.y)",
		"min, two args":             "MATCH (n) RETURN min(n.x, n.y)",
		"max, two args":             "MATCH (n) RETURN max(n.x, n.y)",
		"collect, two args":         "MATCH (n) RETURN collect(n.x, n.y)",
		"stDev, three args":         "MATCH (n) RETURN stDev(n.x, n.y, n.z)",
		"stDevP, two args":          "MATCH (n) RETURN stDevP(n.x, n.y)",
		"percentileCont, three":     "MATCH (n) RETURN percentileCont(n.x, 0.5, 1)",
		"percentileDisc, three":     "MATCH (n) RETURN percentileDisc(n.x, 0.5, 1)",
		"too many, rich expression": "MATCH (n) RETURN min(n.x, n.y) + 1",
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
// namespaced or non-aggregate function that happens to take no arguments.
func TestAggregateWithRightArgumentCountParses(t *testing.T) {
	for name, src := range map[string]string{
		"count(*)":                 "MATCH (n) RETURN count(*)",
		"count(n)":                 "MATCH (n) RETURN count(n)",
		"count(DISTINCT n)":        "MATCH (n) RETURN count(DISTINCT n)",
		"sum":                      "MATCH (n) RETURN sum(n.x)",
		"avg":                      "MATCH (n) RETURN avg(n.x)",
		"min":                      "MATCH (n) RETURN min(n.x)",
		"max":                      "MATCH (n) RETURN max(n.x)",
		"collect":                  "MATCH (n) RETURN collect(n)",
		"stDev":                    "MATCH (n) RETURN stDev(n.x)",
		"stDevP":                   "MATCH (n) RETURN stDevP(n.x)",
		"percentileCont":           "MATCH (n) RETURN percentileCont(n.x, 0.5)",
		"percentileDisc":           "MATCH (n) RETURN percentileDisc(n.x, 0.5)",
		"inside a rich expression": "MATCH (n) RETURN count(n) + 1",
		"a zero-arg non-aggregate": "RETURN rand()",
		"a namespaced count()":     "RETURN my.count()",
		"a zero-arg temporal":      "RETURN date()",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := cypher.New().Parse(strings.NewReader(src))
			require.NoError(t, err, "query: %q", src)
		})
	}
}

// --- nested aggregates (bd gqlc-jjot2.12) ---
//
// An aggregate among another aggregate's arguments is SyntaxError:
// NestedAggregation in openCypher. Before gqlc-jjot2.12 the outer call entered
// the model and the inner one stayed an invisible rich sub-expression.

// TestNestedAggregateIsRefused covers an inner aggregate by name, the count(*)
// atom, an inner aggregate buried in an expression, and the positions the
// sweep reaches.
func TestNestedAggregateIsRefused(t *testing.T) {
	for name, src := range map[string]string{
		"count(count(n))":             "MATCH (n) RETURN count(count(n))",
		"count(count(*)), Return6 14": "RETURN count(count(*))",
		"sum(count(*))":               "MATCH (n) RETURN sum(count(*))",
		"inside an expression":        "MATCH (n) RETURN collect(n.x + max(n.y))",
		"inside a list literal":       "MATCH (n) RETURN collect([min(n.x)])",
		"at WITH position":            "MATCH (n) WITH avg(sum(n.x)) AS a RETURN a",
		"inside a rich expression":    "MATCH (n) RETURN 1 + max(min(n.x))",
		"inside an EXISTS subquery":   "MATCH (n) WHERE EXISTS { MATCH (m) RETURN count(sum(m.x)) } RETURN n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := cypher.New().Parse(strings.NewReader(src))
			require.ErrorIs(t, err, cypher.ErrNestedAggregate, "query: %q", src)
			require.Equal(t, query.Query{}, got, "model must be the zero value on error")
		})
	}
}

// TestUnnestedAggregatesParse is the control: aggregates side by side, an
// aggregate over a non-aggregate call, and an aggregate inside an EXISTS
// subquery in an aggregate's argument — the subquery is its own query — are
// not nested aggregation.
func TestUnnestedAggregatesParse(t *testing.T) {
	for name, src := range map[string]string{
		"side by side":                    "MATCH (n) RETURN count(n) + sum(n.x)",
		"over a scalar call":              "MATCH (n) RETURN collect(toString(n.x))",
		"aggregate under a scalar call":   "MATCH (n) RETURN toString(sum(n.x))",
		"aggregate in an EXISTS argument": "MATCH (n) RETURN count(EXISTS { MATCH (n)-->(m) WITH count(m) AS c WHERE c > 1 RETURN c })",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := cypher.New().Parse(strings.NewReader(src))
			require.NoError(t, err, "query: %q", src)
		})
	}
}

// --- scalar function call arity (bd gqlc-jjot2.12) ---
//
// The built-in scalar functions have fixed argument ranges too, and the
// grammar admits any count for them as it does for aggregates. Before
// gqlc-jjot2.12, size() and substring('a') parsed; the typer guards the
// length, so nothing panicked, but the server refused each one at compile
// time.

// TestScalarFunctionWithWrongArgumentCountIsRefused covers the bead's shapes,
// both sides of a ranged arity, and the positions a call reaches.
func TestScalarFunctionWithWrongArgumentCountIsRefused(t *testing.T) {
	for name, src := range map[string]string{
		"size()":                    "RETURN size()",
		"head()":                    "RETURN head()",
		"toString()":                "RETURN toString()",
		"abs()":                     "RETURN abs()",
		"substring, one arg":        "RETURN substring('a')",
		"substring, four args":      "RETURN substring('a', 1, 2, 3)",
		"size, two args":            "RETURN size([1], [2])",
		"atan2, one arg":            "RETURN atan2(1)",
		"rand, one arg":             "RETURN rand(1)",
		"coalesce()":                "RETURN coalesce()",
		"date, two args":            "RETURN date('2020-01-01', 'x')",
		"upper-case name":           "RETURN SIZE()",
		"in WHERE":                  "MATCH (n) WHERE size() > 1 RETURN n",
		"under an aggregate":        "MATCH (n) RETURN collect(toString())",
		"inside an EXISTS subquery": "MATCH (n) WHERE EXISTS { MATCH (m) RETURN abs() } RETURN n",
		"in a SET":                  "MATCH (n) SET n.x = toUpper() RETURN n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := cypher.New().Parse(strings.NewReader(src))
			require.ErrorIs(t, err, cypher.ErrFunctionArity, "query: %q", src)
			require.Equal(t, query.Query{}, got, "model must be the zero value on error")
		})
	}
}

// TestScalarFunctionWithRightArgumentCountParses is the control: each end of a
// ranged arity, a variadic call, a zero-argument function, and names the table
// does not know — a user's namespaced function and an unknown bare name — are
// accepted at any count.
func TestScalarFunctionWithRightArgumentCountParses(t *testing.T) {
	for name, src := range map[string]string{
		"substring, two args":    "RETURN substring('abc', 1)",
		"substring, three args":  "RETURN substring('abc', 1, 1)",
		"round, three args":      "RETURN round(1.25, 1, 'HALF_UP')",
		"coalesce, many args":    "RETURN coalesce(null, null, 1, 2)",
		"date()":                 "RETURN date()",
		"date, one arg":          "RETURN date('2020-01-01')",
		"pi()":                   "RETURN pi()",
		"a namespaced size":      "RETURN my.size()",
		"an unknown bare name":   "RETURN myFunction(1, 2, 3)",
		"range, two args":        "RETURN range(1, 3)",
		"a delimited known name": "RETURN `size`([1])",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := cypher.New().Parse(strings.NewReader(src))
			require.NoError(t, err, "query: %q", src)
		})
	}
}
