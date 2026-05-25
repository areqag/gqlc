package gql

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/antranig-yeretzian/gqlc/internal/schema"
)

var update = flag.Bool("update", false, "regenerate .golden.json files from parser output")

const fixtureDir = "../../../test/data/schema/gql"

type ParserSuite struct {
	suite.Suite
}

func TestParserSuite(t *testing.T) {
	suite.Run(t, new(ParserSuite))
}

// parseFixture reads a fixture file and parses it. Reading the bytes up front
// avoids holding an open file handle across the parse.
func (s *ParserSuite) parseFixture(path string) (schema.Schema, error) {
	src, err := os.ReadFile(path)
	s.Require().NoError(err)
	return New().Parse(bytes.NewReader(src))
}

// TestValid parses every valid fixture and compares its model against the
// fixture's golden file. Run with -update to regenerate the golden files.
func (s *ParserSuite) TestValid() {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "valid", "*.gql"))
	s.Require().NoError(err)
	s.Require().NotEmpty(files)

	for _, path := range files {
		s.Run(filepath.Base(path), func() {
			got, err := s.parseFixture(path)
			s.Require().NoError(err)

			want, err := json.MarshalIndent(got, "", "  ")
			s.Require().NoError(err)

			goldenPath := path + ".golden.json"
			if *update {
				s.Require().NoError(os.WriteFile(goldenPath, want, 0o644))
				return
			}

			expected, err := os.ReadFile(goldenPath)
			s.Require().NoError(err, "missing golden file; run go test -update")
			s.JSONEq(string(expected), string(want))
		})
	}
}

// TestGraphTypeName covers Schema.Name extraction and the accept-and-ignore of
// the OR REPLACE / IF NOT EXISTS prefixes. A catalog-qualified type-name keeps
// only its simple last component, since the grammar already isolates the parent
// path from the type name.
func (s *ParserSuite) TestGraphTypeName() {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"simple", `CREATE PROPERTY GRAPH TYPE T AS { (:A { x :: INT }) }`, "T"},
		{"catalog path keeps last component", `CREATE PROPERTY GRAPH TYPE store.metrics.M AS { (:A { x :: INT }) }`, "M"},
		{"or replace ignored", `CREATE OR REPLACE PROPERTY GRAPH TYPE T AS { (:A { x :: INT }) }`, "T"},
		{"if not exists ignored", `CREATE PROPERTY GRAPH TYPE IF NOT EXISTS T AS { (:A { x :: INT }) }`, "T"},
	}

	for _, tt := range cases {
		s.Run(tt.name, func() {
			got, err := New().Parse(strings.NewReader(tt.src))
			s.Require().NoError(err)
			s.Equal(tt.want, got.Name)
		})
	}
}

// TestNodeTypeAssembly covers building NodeType from a node type pattern: the
// canonical label-set identity, the optional explicit type name, and the
// normalised properties.
func (s *ParserSuite) TestNodeTypeAssembly() {
	src := `CREATE PROPERTY GRAPH TYPE T AS {
		(:Person { id :: INT NOT NULL, name :: STRING })
	}`

	got, err := New().Parse(strings.NewReader(src))
	s.Require().NoError(err)
	s.Require().Len(got.Nodes, 1)

	n, ok := got.Nodes[schema.LabelSet{"Person"}.Key()]
	s.Require().True(ok, "node keyed by canonical label set")
	s.Equal(schema.LabelSet{"Person"}.Key(), n.Labels)
	s.Empty(n.Name)
	s.Equal(map[string]schema.Property{
		"id":   {Name: "id", Type: schema.TypeInt, Nullable: false},
		"name": {Name: "name", Type: schema.TypeString, Nullable: true},
	}, n.Properties)
}

// TestNodeTypeName covers the explicit nodeTypeName (the NODE TYPE <name> prefix)
// landing on NodeType.Name, distinct from the label set and the local alias.
func (s *ParserSuite) TestNodeTypeName() {
	src := `CREATE PROPERTY GRAPH TYPE T AS { NODE TYPE PersonType (p :Person { id :: INT }) }`

	got, err := New().Parse(strings.NewReader(src))
	s.Require().NoError(err)

	n, ok := got.Nodes[schema.LabelSet{"Person"}.Key()]
	s.Require().True(ok)
	s.Equal("PersonType", n.Name)
}

// TestNodeMultiLabelIdentity covers a node typed by more than one label: the
// canonical key is order- and duplicate-independent.
func (s *ParserSuite) TestNodeMultiLabelIdentity() {
	src := `CREATE PROPERTY GRAPH TYPE T AS { (:Employee&Person { id :: INT }) }`

	got, err := New().Parse(strings.NewReader(src))
	s.Require().NoError(err)

	_, ok := got.Nodes[schema.LabelSet{"Person", "Employee"}.Key()]
	s.True(ok, "keyed by canonical (sorted) label set regardless of source order")
}

// TestEdgeTypeAssembly covers building EdgeType from an alias edge: endpoints
// resolved through the node alias table, the (Source, Label, Target) identity,
// and the normalised edge properties.
func (s *ParserSuite) TestEdgeTypeAssembly() {
	src := `CREATE PROPERTY GRAPH TYPE T AS {
		(a :Person { id :: INT }),
		(b :Post { id :: INT }),
		(a) -[:AUTHORED { publishedAt :: TIMESTAMP }]-> (b)
	}`

	got, err := New().Parse(strings.NewReader(src))
	s.Require().NoError(err)
	s.Require().Len(got.Edges, 1)

	key := schema.EdgeKey{
		Source: schema.LabelSet{"Person"}.Key(),
		Label:  schema.LabelSet{"AUTHORED"}.Key(),
		Target: schema.LabelSet{"Post"}.Key(),
	}
	e, ok := got.Edges[key]
	s.Require().True(ok, "edge keyed by (source, label, target) triple")
	s.Equal(key, e.EdgeKey)
	s.Empty(e.Name)
	s.Equal(map[string]schema.Property{
		"publishedAt": {Name: "publishedAt", Type: schema.TypeTimestamp, Nullable: true},
	}, e.Properties)
}

// TestEdgeInlineEndpoints covers endpoints written as inline node-type fillers:
// the filler's label set is the identity (inline properties ignored), and it
// must match a declared node type.
func (s *ParserSuite) TestEdgeInlineEndpoints() {
	src := `CREATE PROPERTY GRAPH TYPE T AS {
		(:Person { id :: INT }),
		(:Person) -[:KNOWS]-> (:Person)
	}`

	got, err := New().Parse(strings.NewReader(src))
	s.Require().NoError(err)

	key := schema.EdgeKey{
		Source: schema.LabelSet{"Person"}.Key(),
		Label:  schema.LabelSet{"KNOWS"}.Key(),
		Target: schema.LabelSet{"Person"}.Key(),
	}
	_, ok := got.Edges[key]
	s.True(ok, "inline filler endpoints resolve to the declared node type")
}

// TestEdgeLeftPointingCanonicalised covers a left-pointing arc being normalised
// to source->target: `(a) <-[:R]- (b)` is the edge b -> a, so its identity is
// independent of the direction it was written in.
func (s *ParserSuite) TestEdgeLeftPointingCanonicalised() {
	src := `CREATE PROPERTY GRAPH TYPE T AS {
		(a :Person { id :: INT }),
		(b :Post { id :: INT }),
		(a) <-[:WRITTEN_BY]- (b)
	}`

	got, err := New().Parse(strings.NewReader(src))
	s.Require().NoError(err)

	key := schema.EdgeKey{
		Source: schema.LabelSet{"Post"}.Key(),
		Label:  schema.LabelSet{"WRITTEN_BY"}.Key(),
		Target: schema.LabelSet{"Person"}.Key(),
	}
	_, ok := got.Edges[key]
	s.True(ok, "left-pointing arc canonicalised so source is the arrow's tail (Post)")
}

// TestEdgeTypeName covers the explicit edgeTypeName landing on EdgeType.Name.
func (s *ParserSuite) TestEdgeTypeName() {
	src := `CREATE PROPERTY GRAPH TYPE T AS {
		(:Person { id :: INT }),
		(:Post { id :: INT }),
		DIRECTED EDGE TYPE Authorship (:Person) -[:AUTHORED]-> (:Post)
	}`

	got, err := New().Parse(strings.NewReader(src))
	s.Require().NoError(err)

	key := schema.EdgeKey{
		Source: schema.LabelSet{"Person"}.Key(),
		Label:  schema.LabelSet{"AUTHORED"}.Key(),
		Target: schema.LabelSet{"Post"}.Key(),
	}
	e, ok := got.Edges[key]
	s.Require().True(ok)
	s.Equal("Authorship", e.Name)
}

// TestInvalid asserts each invalid fixture produces its paired sentinel. A nil
// wantErr means the fixture is a syntax error (no sentinel), so any non-nil
// error from the syntax error listener satisfies it.
func (s *ParserSuite) TestInvalid() {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "invalid", "*.gql"))
	s.Require().NoError(err)
	s.Require().Len(invalidFixtures, len(files), "every invalid fixture must be mapped to a sentinel")

	for _, path := range files {
		name := filepath.Base(path)
		s.Run(name, func() {
			wantErr, ok := invalidFixtures[name]
			s.Require().True(ok, "unmapped invalid fixture")

			got, parseErr := s.parseFixture(path)
			s.Require().Error(parseErr)
			s.Equal(schema.Schema{}, got, "model must be the zero value on error")
			if wantErr != nil {
				s.Require().ErrorIs(parseErr, wantErr)
			}
		})
	}
}

// invalidFixtures pairs each negative fixture with the sentinel it must produce.
// A nil value means the fixture is a syntax error (no sentinel), satisfied by any
// non-nil error from the syntax error listener.
var invalidFixtures = map[string]error{
	"syntax_error.gql":           nil,
	"label_implication_node.gql": ErrLabelImplication,
	"label_implication_edge.gql": ErrLabelImplication,
	"undirected_edge.gql":        ErrUndirectedEdge,
	"unknown_endpoint.gql":       ErrUnknownEndpoint,
	"unsupported_type.gql":       ErrUnsupportedType,
	"unnamed_node.gql":           ErrUnnamedNodeType,
	"unnamed_edge.gql":           ErrUnnamedEdgeType,
	"duplicate_node.gql":         ErrDuplicateNodeType,
	"duplicate_edge.gql":         ErrDuplicateEdgeType,
	"no_graph_type.gql":          ErrNoGraphType,
	"multiple_graph_types.gql":   ErrMultipleGraphTypes,
	"unsupported_source.gql":     ErrUnsupportedSource,
}

// allSentinels is the canonical list of every Parse sentinel — the single source
// of truth TestSentinelReachability checks against. A new sentinel must be added
// here (and paired with a fixture); a removed one must be dropped.
var allSentinels = []error{
	ErrLabelImplication,
	ErrUndirectedEdge,
	ErrUnknownEndpoint,
	ErrUnsupportedType,
	ErrUnnamedNodeType,
	ErrUnnamedEdgeType,
	ErrDuplicateNodeType,
	ErrDuplicateEdgeType,
	ErrNoGraphType,
	ErrMultipleGraphTypes,
	ErrUnsupportedSource,
}

// TestSentinelReachability is the bidirectional sweep: the set of sentinels the
// negative fixtures cover must equal the canonical sentinel set. It fails if a
// sentinel is declared but no fixture exercises it (orphaned), or if a fixture
// maps to a sentinel missing from the canonical list (stray or renamed).
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
