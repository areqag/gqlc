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

	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/schema"
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

	n, ok := got.Nodes[graph.LabelSet{"Person"}.Key()]
	s.Require().True(ok, "node keyed by canonical label set")
	s.Equal(graph.LabelSet{"Person"}.Key(), n.Labels)
	s.Empty(n.Name)
	s.Equal(map[string]schema.Property{
		"id":   {Name: "id", Type: graph.TypeInt, Nullable: false},
		"name": {Name: "name", Type: graph.TypeString, Nullable: true},
	}, n.Properties)
}

// TestNodeTypeName covers the explicit nodeTypeName (the NODE TYPE <name> prefix)
// landing on NodeType.Name, distinct from the label set and the local alias.
func (s *ParserSuite) TestNodeTypeName() {
	src := `CREATE PROPERTY GRAPH TYPE T AS { NODE TYPE PersonType (p :Person { id :: INT }) }`

	got, err := New().Parse(strings.NewReader(src))
	s.Require().NoError(err)

	n, ok := got.Nodes[graph.LabelSet{"Person"}.Key()]
	s.Require().True(ok)
	s.Equal("PersonType", n.Name)
}

// TestNodeMultiLabelIdentity covers a node typed by more than one label: the
// canonical key is order- and duplicate-independent.
func (s *ParserSuite) TestNodeMultiLabelIdentity() {
	src := `CREATE PROPERTY GRAPH TYPE T AS { (:Employee&Person { id :: INT }) }`

	got, err := New().Parse(strings.NewReader(src))
	s.Require().NoError(err)

	_, ok := got.Nodes[graph.LabelSet{"Person", "Employee"}.Key()]
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
		Source: graph.LabelSet{"Person"}.Key(),
		Label:  graph.LabelSet{"AUTHORED"}.Key(),
		Target: graph.LabelSet{"Post"}.Key(),
	}
	e, ok := got.Edges[key]
	s.Require().True(ok, "edge keyed by (source, label, target) triple")
	s.Equal(key, e.EdgeKey)
	s.Empty(e.Name)
	s.Equal(map[string]schema.Property{
		"publishedAt": {Name: "publishedAt", Type: graph.TypeTimestamp, Nullable: true},
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
		Source: graph.LabelSet{"Person"}.Key(),
		Label:  graph.LabelSet{"KNOWS"}.Key(),
		Target: graph.LabelSet{"Person"}.Key(),
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
		Source: graph.LabelSet{"Post"}.Key(),
		Label:  graph.LabelSet{"WRITTEN_BY"}.Key(),
		Target: graph.LabelSet{"Person"}.Key(),
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
		Source: graph.LabelSet{"Person"}.Key(),
		Label:  graph.LabelSet{"AUTHORED"}.Key(),
		Target: graph.LabelSet{"Post"}.Key(),
	}
	e, ok := got.Edges[key]
	s.Require().True(ok)
	s.Equal("Authorship", e.Name)
}

// graphType wraps an element type list in the smallest graph type that carries
// it, so a case below reads as the declarations it is about.
func graphType(body string) string {
	return "CREATE PROPERTY GRAPH TYPE T AS {" + body + "}"
}

// TestPhraseFormEquivalence pins the phrase form against the pattern form: the
// same graph type spelled either way resolves to the same model. Comparing whole
// models is what makes it worth writing — a spot check on the label set would pass
// a listener that dropped the type name, the alias or the properties, which is the
// failure the phrase form shipped with.
//
// The node and edge counts are not redundant with the comparison. Two spellings
// that both collect nothing are equal, and that is precisely the defect: the
// counts are what stop the equality holding vacuously.
func (s *ParserSuite) TestPhraseFormEquivalence() {
	cases := []struct {
		name    string
		pattern string
		phrase  string
		nodes   int
		edges   int
	}{
		{
			name:    "named node with alias and properties",
			pattern: `NODE TYPE PersonType (p :Person { id :: INT NOT NULL, name :: STRING })`,
			phrase:  `NODE TYPE PersonType :Person { id :: INT NOT NULL, name :: STRING } AS p`,
			nodes:   1,
		},
		{
			name:    "node with neither name nor alias",
			pattern: `(:Person { id :: INT })`,
			phrase:  `NODE :Person { id :: INT }`,
			nodes:   1,
		},
		{
			name:    "multi-label node",
			pattern: `(:Employee&Person { id :: INT })`,
			phrase:  `NODE TYPE :Employee&Person { id :: INT }`,
			nodes:   1,
		},
		{
			name:    "node declared by LABEL keyword and no properties",
			pattern: `NODE TYPE PersonType (LABEL Person)`,
			phrase:  `NODE TYPE PersonType LABEL Person`,
			nodes:   1,
		},
		{
			name: "edge with a name, properties and aliased endpoints",
			pattern: `(a :Person { id :: INT }), (b :Post { id :: INT }),
				DIRECTED EDGE TYPE Wrote (a) -[:WROTE { since :: TIMESTAMP }]-> (b)`,
			phrase: `NODE TYPE :Person { id :: INT } AS a, NODE TYPE :Post { id :: INT } AS b,
				DIRECTED EDGE TYPE Wrote :WROTE { since :: TIMESTAMP } CONNECTING (a -> b)`,
			nodes: 2,
			edges: 1,
		},
		{
			// TO and -> are both connectorPointingRight, so this is the same edge as
			// the case above spelled with the other connector. It also pins that a
			// bare TO reaches the directed endpoint pair at all: endpointPair lists
			// endpointPairDirected first and connectorUndirected shares the token, so
			// were prediction to go the other way this would be ErrUndirectedEdge.
			pattern: `(a :Person { id :: INT }), (b :Post { id :: INT }),
				(a) -[:WROTE]-> (b)`,
			phrase: `NODE :Person { id :: INT } AS a, NODE :Post { id :: INT } AS b,
				DIRECTED EDGE :WROTE CONNECTING (a TO b)`,
			name:  "TO connector is the arrow",
			nodes: 2,
			edges: 1,
		},
		{
			// An alias may be spelled the same as the label it is bound to, which is
			// how the phrase form is usually written, and the alias table has to win:
			// `Person` here is the alias, not the node type it names.
			name: "alias shadowing its own label still resolves",
			pattern: `(Person :Person { id :: INT }),
				(Person) -[:KNOWS]-> (Person)`,
			phrase: `NODE TYPE :Person { id :: INT } AS Person,
				DIRECTED EDGE :KNOWS CONNECTING (Person TO Person)`,
			nodes: 1,
			edges: 1,
		},
		{
			// Both spellings name their ends by role, so both are the edge Post ->
			// Person and neither needs the listener to swap anything.
			name: "left-pointing endpoints canonicalise to source->target",
			pattern: `(a :Person { id :: INT }), (b :Post { id :: INT }),
				(a) <-[:CITED]- (b)`,
			phrase: `NODE :Person { id :: INT } AS a, NODE :Post { id :: INT } AS b,
				DIRECTED EDGE :CITED CONNECTING (a <- b)`,
			nodes: 2,
			edges: 1,
		},
	}

	for _, tt := range cases {
		s.Run(tt.name, func() {
			pattern, err := New().Parse(strings.NewReader(graphType(tt.pattern)))
			s.Require().NoError(err)
			s.Require().Len(pattern.Nodes, tt.nodes)
			s.Require().Len(pattern.Edges, tt.edges)

			phrase, err := New().Parse(strings.NewReader(graphType(tt.phrase)))
			s.Require().NoError(err)

			s.Require().Equal(pattern, phrase)
		})
	}
}

// TestEndpointAliasDiagnostics separates the two ways an endpoint can fail to name
// an alias. Both slots the grammar reads as an alias — the phrase form's
// CONNECTING pair and the pattern form's parenthesised reference — take a bare
// identifier, so an author who writes the node type's name there gets a lookup
// miss for a type that is declared on the screen in front of them. Distinguishing
// that from a genuine typo is the whole value of ErrEndpointNotAlias, so the
// undeclared case is here to keep the distinction load-bearing.
func (s *ParserSuite) TestEndpointAliasDiagnostics() {
	cases := []struct {
		name string
		src  string
		want error
	}{
		{
			name: "phrase endpoint names a node type that binds no alias",
			src: `NODE TYPE Person :Person { id :: INT },
				DIRECTED EDGE :KNOWS CONNECTING (Person TO Person)`,
			want: ErrEndpointNotAlias,
		},
		{
			name: "phrase endpoint names the node type name rather than its label",
			src: `NODE TYPE PersonType :Person { id :: INT },
				DIRECTED EDGE :KNOWS CONNECTING (PersonType TO PersonType)`,
			want: ErrEndpointNotAlias,
		},
		{
			name: "phrase endpoint names a node type aliased under another name",
			src: `NODE TYPE :Person { id :: INT } AS p,
				DIRECTED EDGE :KNOWS CONNECTING (Person TO Person)`,
			want: ErrEndpointNotAlias,
		},
		{
			name: "pattern endpoint names a node type that binds no alias",
			src: `(:Person { id :: INT }),
				(Person) -[:KNOWS]-> (Person)`,
			want: ErrEndpointNotAlias,
		},
		{
			name: "phrase endpoint names nothing declared",
			src: `NODE TYPE :Person { id :: INT } AS p,
				DIRECTED EDGE :KNOWS CONNECTING (p TO Ghost)`,
			want: ErrUnknownEndpoint,
		},
	}

	for _, tt := range cases {
		s.Run(tt.name, func() {
			got, err := New().Parse(strings.NewReader(graphType(tt.src)))
			s.Require().ErrorIs(err, tt.want)
			s.Equal(schema.Schema{}, got, "model must be the zero value on error")
		})
	}
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
	"endpoint_not_alias.gql":     ErrEndpointNotAlias,
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
// here (and pinned by a file); a removed one must be dropped.
var allSentinels = []error{
	ErrLabelImplication,
	ErrUndirectedEdge,
	ErrUnknownEndpoint,
	ErrEndpointNotAlias,
	ErrUnsupportedType,
	ErrUnnamedNodeType,
	ErrUnnamedEdgeType,
	ErrDuplicateNodeType,
	ErrDuplicateEdgeType,
	ErrNoGraphType,
	ErrMultipleGraphTypes,
	ErrUnsupportedSource,
}

// TestSentinelReachability is the bidirectional sweep: the set of sentinels
// pinned by files must equal the canonical sentinel set. It fails if a sentinel
// is declared but no file exercises it (orphaned), or if a file pins a sentinel
// missing from the canonical list (stray or renamed).
//
// Both fixture directories count, because both are registries of the same thing
// and two registries drift. The corpus half also keeps the sweep honest as the
// epic closes: a sentinel here is temporary by design, so when a construct
// becomes supported its corpus entries flip to resolves in the same commit and
// the sentinel's last pin disappears with them. An invalid/ fixture for the same
// construct would instead survive that commit as an orphan failing for an
// unrelated reason.
func TestSentinelReachability(t *testing.T) {
	covered := make(map[error]bool)
	for _, sentinel := range invalidFixtures {
		if sentinel != nil {
			covered[sentinel] = true
		}
	}
	for _, entry := range corpusManifest(t) {
		if entry.outcome == unsupported {
			covered[entry.sentinel] = true
		}
	}

	canonical := make(map[error]bool, len(allSentinels))
	for _, sentinel := range allSentinels {
		canonical[sentinel] = true
	}

	for _, sentinel := range allSentinels {
		require.True(t, covered[sentinel],
			"sentinel %q has no negative fixture or unsupported corpus entry", sentinel)
	}
	for sentinel := range covered {
		require.True(t, canonical[sentinel], "file pins non-canonical sentinel %q", sentinel)
	}
}
