package queryfile_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/queryfile"
)

var update = flag.Bool("update", false, "regenerate queryfile .golden.json files")

const fixtureDir = "testdata"

// invalidFixtures pairs each negative fixture with the sentinel it must
// produce. Totality against invalid/*.cypher is asserted in TestInvalid so
// a stray fixture or missing map entry fails the suite.
var invalidFixtures = map[string]error{
	"missing_annotation.cypher":     queryfile.ErrMissingAnnotation,
	"unknown_cardinality.cypher":    queryfile.ErrUnknownCardinality,
	"invalid_query_name.cypher":     queryfile.ErrInvalidQueryName,
	"duplicate_query_name.cypher":   queryfile.ErrDuplicateQueryName,
	"malformed_annotation.cypher":   queryfile.ErrMalformedAnnotation,
	"text_before_annotation.cypher": queryfile.ErrTextBeforeAnnotation,
	"no_queries.cypher":             queryfile.ErrNoQueries,
}

type QueryfileSuite struct {
	suite.Suite
}

func TestQueryfileSuite(t *testing.T) {
	suite.Run(t, new(QueryfileSuite))
}

// TestValid walks valid/*.cypher: parses each, then either writes the golden
// (-update) or JSON-encodes the parse result and compares byte-for-byte
// against the stored golden.
func (s *QueryfileSuite) TestValid() {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "valid", "*.cypher"))
	s.Require().NoError(err)
	s.Require().NotEmpty(files)

	for _, path := range files {
		name := filepath.Base(path)
		s.Run(name, func() {
			src, err := os.ReadFile(path)
			s.Require().NoError(err)

			got, err := queryfile.New().Parse(bytes.NewReader(src))
			s.Require().NoError(err)

			// Serialised as JSON with the Text field's embedded newlines
			// preserved. Byte-equality on the encoded bytes catches both
			// field drift and text-preservation regressions in one check.
			encoded, err := json.MarshalIndent(annotationEnvelope{Queries: got}, "", "  ")
			s.Require().NoError(err)
			encoded = append(encoded, '\n')

			goldenPath := path + ".golden.json"
			if *update {
				s.Require().NoError(os.WriteFile(goldenPath, encoded, 0o644))
				return
			}
			want, err := os.ReadFile(goldenPath)
			s.Require().NoError(err, "missing golden file; run go test -update")
			s.Require().True(bytes.Equal(want, encoded), "golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s",
				name, want, encoded)
		})
	}
}

// TestInvalid walks invalid/*.cypher: parses each, and asserts (a) the
// returned slice is nil and (b) the error is the mapped sentinel via
// errors.Is. Map totality asserted at the top of the test.
func (s *QueryfileSuite) TestInvalid() {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "invalid", "*.cypher"))
	s.Require().NoError(err)
	s.Require().NotEmpty(files)
	s.Require().Len(invalidFixtures, len(files),
		"invalidFixtures must be total against invalid/*.cypher")

	for _, path := range files {
		name := filepath.Base(path)
		s.Run(name, func() {
			wantErr, ok := invalidFixtures[name]
			s.Require().True(ok, "invalid fixture %q missing from invalidFixtures", name)

			src, err := os.ReadFile(path)
			s.Require().NoError(err)

			got, err := queryfile.New().Parse(bytes.NewReader(src))
			s.Require().Error(err)
			s.Require().Nil(got, "queries must be nil on error")
			s.Require().ErrorIs(err, wantErr)
		})
	}
}

// TestSentinelReachability is the bidirectional sweep: every AllSentinels
// member must have at least one invalid fixture; every mapped sentinel must
// be in AllSentinels.
//
// It reads the canonical set through the exported accessor rather than the
// package's `allSentinels` var, because this file moved out of package
// queryfile (bd gqlc-m5rc). AllSentinels copies that var, so the mutation
// recorded below still reaches this test.
//
// Both directions are quantified over a set, so on empty inputs each holds
// vacuously and the sweep reconciles nothing against nothing. Measured
// (bd gqlc-v1w8): gut invalidFixtures to `map[string]error{}` AND the
// package's allSentinels to `[]error{}` and this test passed, rc=0, no
// "[no tests to run]" — it was green exactly when what it guards had
// vanished.
//
// The census guard below closes that, and it is deliberately ONE guard and not
// two. The canonical list going empty on its own is already caught: the second
// direction then finds a covered sentinel that is not canonical. Adding a
// matching guard on AllSentinels would make BOTH read non-load-bearing under
// mutation, because either alone still kills the row. The composition is:
// census empty -> this guard; canonical empty -> the covered/canonical
// direction; both empty -> this guard.
func TestSentinelReachability(t *testing.T) {
	covered := make(map[error]bool)
	for _, sentinel := range invalidFixtures {
		if sentinel != nil {
			covered[sentinel] = true
		}
	}
	require.NotEmpty(t, covered,
		"no invalid fixture maps to a sentinel, so both directions below hold vacuously and this sweep reconciles nothing against nothing")
	sentinels := queryfile.AllSentinels()
	canonical := make(map[error]bool, len(sentinels))
	for _, sentinel := range sentinels {
		canonical[sentinel] = true
	}
	for _, sentinel := range sentinels {
		require.True(t, covered[sentinel], "sentinel %q has no negative fixture", sentinel)
	}
	for sentinel := range covered {
		require.True(t, canonical[sentinel], "fixture maps to non-canonical sentinel %q", sentinel)
	}
}

// TestFreeStandingCommentBlockDoesNotJoinPrecedingQuery is the falsifier for
// bd gqlc-kc5w: a query's Text is handed verbatim to the driver, so prose
// written BETWEEN two named queries must not become part of the first
// query's statement. The neo4j backend happens to tolerate it (// is a Cypher
// line comment); the AGE backend composes Text into a SQL literal and nothing
// establishes it tolerates anything.
//
// The second case pins the deliberate limit of the fix: a comment line that
// abuts the query's last line with no blank separator is the author
// annotating THAT query, and is preserved.
func TestFreeStandingCommentBlockDoesNotJoinPrecedingQuery(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "free-standing block between queries is not appended",
			src: "// name: FindPerson :one\n" +
				"MATCH (p:Person) RETURN p\n" +
				"\n" +
				"// The queries below repeat the ones above one declared\n" +
				"// width over. Prose, not Cypher.\n" +
				"\n" +
				"// name: ListPeople :many\n" +
				"MATCH (p:Person) RETURN p\n",
			want: "MATCH (p:Person) RETURN p",
		},
		{
			name: "free-standing block at end of file is not appended",
			src: "// name: FindPerson :one\n" +
				"MATCH (p:Person) RETURN p\n" +
				"\n" +
				"// Trailing prose after the last query.\n",
			want: "MATCH (p:Person) RETURN p",
		},
		{
			name: "abutting comment is kept as part of the query",
			src: "// name: FindPerson :one\n" +
				"MATCH (p:Person) RETURN p\n" +
				"// this annotates the line above\n" +
				"\n" +
				"// name: ListPeople :many\n" +
				"MATCH (p:Person) RETURN p\n",
			want: "MATCH (p:Person) RETURN p\n// this annotates the line above",
		},
		{
			name: "interior comment between query lines is kept",
			src: "// name: FindPerson :one\n" +
				"MATCH (p:Person)\n" +
				"// filter\n" +
				"WHERE p.age > 30\n" +
				"RETURN p\n" +
				"\n" +
				"// name: ListPeople :many\n" +
				"MATCH (p:Person) RETURN p\n",
			want: "MATCH (p:Person)\n// filter\nWHERE p.age > 30\nRETURN p",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := queryfile.New().Parse(bytes.NewReader([]byte(tc.src)))
			require.NoError(t, err)
			require.NotEmpty(t, got)
			require.Equal(t, tc.want, got[0].Text)
		})
	}
}

// TestCommentOnlyBodyIsNoBody: once a free-standing comment block stops
// attaching to the preceding query, a query whose entire body is such a block
// has no statement to run, and must be reported rather than shipped as an
// empty string.
func TestCommentOnlyBodyIsNoBody(t *testing.T) {
	src := "// name: FindPerson :one\n" +
		"\n" +
		"// nothing but prose here\n" +
		"\n" +
		"// name: ListPeople :many\n" +
		"MATCH (p:Person) RETURN p\n"

	got, err := queryfile.New().Parse(bytes.NewReader([]byte(src)))
	require.Error(t, err)
	require.Nil(t, got)
	require.ErrorIs(t, err, queryfile.ErrMissingAnnotation)
}

// annotationEnvelope wraps the parse result for stable JSON encoding: an
// object with a "queries" key beats a bare array at the top level for
// readability, and gives the golden a clear anchor for future field
// additions without churning every fixture at once.
type annotationEnvelope struct {
	Queries []queryfile.AnnotatedQuery `json:"queries"`
}
