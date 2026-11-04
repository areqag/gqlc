package gql_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// prefixRunContexts are places a run of prefix operators was measured slow (bd
// gqlc-jjot2.32), each with an operator a GQL.g4 rule recurses on there. op is
// repeated whole, so a run of n repetitions is n*ops tokens.
var prefixRunContexts = []struct {
	name, open, op, close string
	ops                   int
}{
	{"list", "RETURN [", "+", "1]", 1},
	{"paren", "RETURN (", "+", "1)", 1},
	{"property map", "MATCH (n {a: ", "+", "1}) RETURN n", 1},
	{"where", "MATCH (n) WHERE ", "NOT ", "TRUE RETURN n", 1},
	{"label", "MATCH (n:", "!", "A) RETURN n", 1},
	// '--' is a comment, so a run of minus signs alternates with plus.
	{"alternating signs", "RETURN (", "+-", "1)", 2},
	// A comment is on the hidden channel and does not break the run.
	{"commented", "RETURN [", "+ /* c */ ", "1]", 1},
}

// TestPrefixRunBound pins gql.MaxPrefixRun from both sides in each context: a
// run one past it is refused with ErrPrefixRunTooLong, and a run at it reaches
// the parser, which refuses the query for being no schema.
func TestPrefixRunBound(t *testing.T) {
	for _, c := range prefixRunContexts {
		at := c.open + strings.Repeat(c.op, gql.MaxPrefixRun/c.ops) + c.close
		past := c.open + strings.Repeat(c.op, gql.MaxPrefixRun/c.ops+1) + c.close
		t.Run(c.name+"/at the limit", func(t *testing.T) {
			_, err := gql.New().Parse(strings.NewReader(at))
			require.Error(t, err, "a query is no schema")
			require.NotErrorIs(t, err, gql.ErrPrefixRunTooLong)
		})
		t.Run(c.name+"/past the limit", func(t *testing.T) {
			s, err := gql.New().Parse(strings.NewReader(past))
			require.ErrorIs(t, err, gql.ErrPrefixRunTooLong)
			require.Equal(t, schema.Schema{}, s)
		})
	}
}

// TestPrefixRunNamesTheOperatorPastTheLimit pins the position the refusal
// reports: the operator that first makes the run too long, by ANTLR's 1-based
// line and 0-based column.
func TestPrefixRunNamesTheOperatorPastTheLimit(t *testing.T) {
	src := "RETURN\n  " + strings.Repeat("+", gql.MaxPrefixRun+5) + "1"
	_, err := gql.New().Parse(strings.NewReader(src))
	require.ErrorIs(t, err, gql.ErrPrefixRunTooLong)
	require.ErrorContains(t, err, "operator at 2:34 extends a run past 32")
}

// TestPrefixRunOperandBreaksTheRun pins that only an unbroken run counts: the
// same operators with an operand between each are as fast as any expression,
// so thousands of them are no refusal.
func TestPrefixRunOperandBreaksTheRun(t *testing.T) {
	src := "RETURN [" + strings.Repeat("+1", 4*gql.MaxPrefixRun) + "]"
	_, err := gql.New().Parse(strings.NewReader(src))
	require.Error(t, err, "a query is no schema")
	require.NotErrorIs(t, err, gql.ErrPrefixRunTooLong)
}

// TestLoadRefusesPrefixRunTooLong pins the Loader's path through the same
// check: it shares parse with Parse, but a Loader is what the pipeline calls.
func TestLoadRefusesPrefixRunTooLong(t *testing.T) {
	fsys := fstest.MapFS{"a.gql": file("RETURN [" + strings.Repeat("+", gql.MaxPrefixRun+1) + "1]")}
	_, err := gql.NewLoader(fsys).Load("a.gql")
	require.ErrorIs(t, err, gql.ErrPrefixRunTooLong)
}
