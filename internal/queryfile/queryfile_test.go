package queryfile

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

var update = flag.Bool("update", false, "regenerate queryfile .golden.json files")

const fixtureDir = "testdata"

// invalidFixtures pairs each negative fixture with the sentinel it must
// produce. Totality against invalid/*.cypher is asserted in TestInvalid so
// a stray fixture or missing map entry fails the suite.
var invalidFixtures = map[string]error{
	"missing_annotation.cypher":     ErrMissingAnnotation,
	"unknown_cardinality.cypher":    ErrUnknownCardinality,
	"invalid_query_name.cypher":     ErrInvalidQueryName,
	"duplicate_query_name.cypher":   ErrDuplicateQueryName,
	"malformed_annotation.cypher":   ErrMalformedAnnotation,
	"text_before_annotation.cypher": ErrTextBeforeAnnotation,
	"no_queries.cypher":             ErrNoQueries,
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

			got, err := New().Parse(bytes.NewReader(src))
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

			got, err := New().Parse(bytes.NewReader(src))
			s.Require().Error(err)
			s.Require().Nil(got, "queries must be nil on error")
			s.Require().ErrorIs(err, wantErr)
		})
	}
}

// TestSentinelReachability is the bidirectional sweep: every allSentinels
// member must have at least one invalid fixture; every mapped sentinel must
// be in allSentinels.
func TestSentinelReachability(t *testing.T) {
	covered := make(map[error]bool)
	for _, sentinel := range invalidFixtures {
		if sentinel != nil {
			covered[sentinel] = true
		}
	}
	canonical := make(map[error]bool, len(allSentinels))
	for _, sentinel := range allSentinels {
		canonical[sentinel] = true
	}
	for _, sentinel := range allSentinels {
		require.True(t, covered[sentinel], "sentinel %q has no negative fixture", sentinel)
	}
	for sentinel := range covered {
		require.True(t, canonical[sentinel], "fixture maps to non-canonical sentinel %q", sentinel)
	}
}

// annotationEnvelope wraps the parse result for stable JSON encoding: an
// object with a "queries" key beats a bare array at the top level for
// readability, and gives the golden a clear anchor for future field
// additions without churning every fixture at once.
type annotationEnvelope struct {
	Queries []AnnotatedQuery `json:"queries"`
}

// Ensure Cardinality serialises as its wire tag so goldens read naturally
// (":one" / ":many" / ":exec" via the enum's String()). This lives in the
// test file rather than production code because JSON encoding is a
// test-only concern for queryfile — the codegen consumer passes
// AnnotatedQuery directly, no wire.
func (c Cardinality) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}
