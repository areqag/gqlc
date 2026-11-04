package gql_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// pathRunContexts are places a path can run long, each spelling a source whose
// one path carries exactly n separators (bd gqlc-jjot2.36). The catalog
// references are where the parse was measured slow; the property chain, the
// divisions and the climb are paths the scan counts the same way.
var pathRunContexts = []struct {
	name string
	src  func(n int) string
}{
	{"COPY OF absolute path", func(n int) string {
		return "CREATE PROPERTY GRAPH TYPE g COPY OF /" + strings.Repeat("a/", n-1) + "b"
	}},
	{"COPY OF object names", func(n int) string {
		return "CREATE PROPERTY GRAPH TYPE g COPY OF " + strings.Repeat("a.", n) + "b"
	}},
	{"COPY OF schema then object names", func(n int) string {
		return "CREATE PROPERTY GRAPH TYPE g COPY OF CURRENT_SCHEMA/" + strings.Repeat("a.", n-1) + "b"
	}},
	{"graph type name path", func(n int) string {
		return "CREATE PROPERTY GRAPH TYPE /" + strings.Repeat("a/", n-1) + "g AS { (:A) }"
	}},
	{"USE graph path", func(n int) string {
		return "USE /" + strings.Repeat("a/", n-1) + "g MATCH (n) RETURN n"
	}},
	{"property chain", func(n int) string {
		return "MATCH (n) RETURN n" + strings.Repeat(".a", n)
	}},
	{"divisions", func(n int) string {
		return "RETURN 1" + strings.Repeat("/1", n)
	}},
	// Each '..' of a relative climb is a separator, as each '/' between them
	// is; a run of bare '..' isolates the token from the '/'.
	{"climb", func(n int) string {
		return "CREATE PROPERTY GRAPH TYPE g COPY OF " + strings.Repeat(".. ", n) + "b"
	}},
	// A comment is on the hidden channel and breaks no path.
	{"commented", func(n int) string {
		return "CREATE PROPERTY GRAPH TYPE g COPY OF /" + strings.Repeat("a /* c */ /", n-1) + "b"
	}},
}

// TestPathRunBound pins gql.MaxPathRun from both sides in each context: a path
// one separator past it is refused with ErrPathTooLong, and one at it reaches
// the parser, which refuses or resolves it on its own terms.
func TestPathRunBound(t *testing.T) {
	for _, c := range pathRunContexts {
		t.Run(c.name+"/at the limit", func(t *testing.T) {
			_, err := gql.New().Parse(strings.NewReader(c.src(gql.MaxPathRun)))
			require.NotErrorIs(t, err, gql.ErrPathTooLong)
		})
		t.Run(c.name+"/past the limit", func(t *testing.T) {
			s, err := gql.New().Parse(strings.NewReader(c.src(gql.MaxPathRun + 1)))
			require.ErrorIs(t, err, gql.ErrPathTooLong)
			require.Equal(t, schema.Schema{}, s)
		})
	}
}

// TestPathRunGraphTypeAtTheLimitResolves pins that the bound refuses nothing a
// real schema at the limit spells: a graph type named by a path of exactly
// MaxPathRun separators parses to its node type.
func TestPathRunGraphTypeAtTheLimitResolves(t *testing.T) {
	src := "CREATE PROPERTY GRAPH TYPE /" + strings.Repeat("a/", gql.MaxPathRun-1) + "g AS { (:A) }"
	s, err := gql.New().Parse(strings.NewReader(src))
	require.NoError(t, err)
	require.Len(t, s.Nodes, 1)
}

// TestPathRunNamesTheSeparatorPastTheLimit pins the position the refusal
// reports: the separator that first makes the path too long, by ANTLR's
// 1-based line and 0-based column.
func TestPathRunNamesTheSeparatorPastTheLimit(t *testing.T) {
	src := "CREATE PROPERTY GRAPH TYPE g\n  COPY OF /" + strings.Repeat("a/", gql.MaxPathRun+5) + "b"
	_, err := gql.New().Parse(strings.NewReader(src))
	require.ErrorIs(t, err, gql.ErrPathTooLong)
	require.ErrorContains(t, err, "separator at 2:74 takes a path past 32")
}

// TestPathRunTwoNamesEndThePath pins that a path ends where two non-separators
// meet: thousands of short paths side by side are no refusal.
func TestPathRunTwoNamesEndThePath(t *testing.T) {
	src := "MATCH (n) RETURN " + strings.Repeat("n.a AS x ", 4*gql.MaxPathRun)
	_, err := gql.New().Parse(strings.NewReader(src))
	require.Error(t, err, "a query is no schema")
	require.NotErrorIs(t, err, gql.ErrPathTooLong)

	// Exactly two names between separators end the path; three would not
	// tell a two-name boundary from a longer one.
	src = "MATCH (n) RETURN n" + strings.Repeat(".a x", 4*gql.MaxPathRun)
	_, err = gql.New().Parse(strings.NewReader(src))
	require.Error(t, err, "a query is no schema")
	require.NotErrorIs(t, err, gql.ErrPathTooLong)
}

// TestLoadRefusesPathTooLong pins the Loader's path through the same check: it
// shares parse with Parse, but a Loader is what the pipeline calls.
func TestLoadRefusesPathTooLong(t *testing.T) {
	fsys := fstest.MapFS{"a.gql": file("CREATE PROPERTY GRAPH TYPE g COPY OF /" + strings.Repeat("a/", gql.MaxPathRun) + "b")}
	_, err := gql.NewLoader(fsys).Load("a.gql")
	require.ErrorIs(t, err, gql.ErrPathTooLong)
}
