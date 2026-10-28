package cypher_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
)

// --- hop-range integer literals (bd gqlc-jjot2.16) ---
//
// oC_RangeLiteral's bounds are oC_IntegerLiterals, which the grammar spells
// three ways: decimal, HexInteger (0x…) and OctalInteger (0o…). Until
// gqlc-jjot2.16 the bounds were read with strconv.Atoi, which refused the two
// prefixed forms as "invalid syntax" — a valid query refused — and answered a
// literal past int64 with a plain error rather than a sentinel.

// TestHopRangeReadsEveryIntegerSpelling pins each spelling's value, so a fix
// that merely stopped refusing the prefixed forms (reading them as 0, say)
// fails here rather than passing.
func TestHopRangeReadsEveryIntegerSpelling(t *testing.T) {
	for name, tc := range map[string]struct {
		query    string
		min, max *int
	}{
		"decimal fixed":      {"MATCH ()-[r*3]-() RETURN r", ptr(3), ptr(3)},
		"hex fixed":          {"MATCH ()-[r*0x2]-() RETURN r", ptr(2), ptr(2)},
		"hex, upper digits":  {"MATCH ()-[r*0x1F]-() RETURN r", ptr(31), ptr(31)},
		"octal fixed":        {"MATCH ()-[r*0o7]-() RETURN r", ptr(7), ptr(7)},
		"octal past seven":   {"MATCH ()-[r*0o10]-() RETURN r", ptr(8), ptr(8)},
		"zero":               {"MATCH ()-[r*0]-() RETURN r", ptr(0), ptr(0)},
		"hex lower, open":    {"MATCH ()-[r*0x2..]-() RETURN r", ptr(2), nil},
		"octal upper only":   {"MATCH ()-[r*..0o11]-() RETURN r", nil, ptr(9)},
		"mixed spellings":    {"MATCH ()-[r*0o1..0xA]-() RETURN r", ptr(1), ptr(10)},
		"int64 max, decimal": {"MATCH ()-[r*9223372036854775807]-() RETURN r", ptr(9223372036854775807), ptr(9223372036854775807)},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := cypher.New().Parse(strings.NewReader(tc.query))
			require.NoError(t, err, "query: %q", tc.query)
			hops := edgeHops(t, got, "r")
			require.Equal(t, tc.min, hops.Min(), "lower bound")
			require.Equal(t, tc.max, hops.Max(), "upper bound")
		})
	}
}

// TestHopRangeOutOfRangeIsRefused covers a bound past int64 in every position
// and spelling a bound takes; each must be the sentinel, not a plain error.
func TestHopRangeOutOfRangeIsRefused(t *testing.T) {
	for name, src := range map[string]string{
		"the fuzz reproducer": "MATCH()-[*9227000000000000000]-()RETURN 0",
		"int64 max plus one":  "MATCH ()-[*9223372036854775808]-() RETURN 0",
		"lower bound":         "MATCH ()-[*99999999999999999999..]-() RETURN 0",
		"upper bound":         "MATCH ()-[*1..99999999999999999999]-() RETURN 0",
		"hex":                 "MATCH ()-[*0x10000000000000000]-() RETURN 0",
		"octal":               "MATCH ()-[*0o7777777777777777777777]-() RETURN 0",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := cypher.New().Parse(strings.NewReader(src))
			require.ErrorIs(t, err, cypher.ErrIntegerOverflow, "query: %q", src)
			require.Equal(t, query.Query{}, got, "model must be the zero value on error")
		})
	}
}

func ptr(n int) *int { return &n }

// edgeHops returns the hop range of the named edge binding in the query's
// first part.
func edgeHops(t *testing.T, q query.Query, variable string) query.EdgeHops {
	t.Helper()
	for _, b := range q.Branches[0].Parts[0].Bindings {
		if eb, ok := b.(query.EdgeBinding); ok && eb.Variable() == variable {
			require.NotNil(t, eb.Hops(), "edge %q carries no hop range", variable)
			return *eb.Hops()
		}
	}
	t.Fatalf("no edge binding named %q in parsed model", variable)
	return query.EdgeHops{}
}
