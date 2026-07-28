package gql

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
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
	s.Equal(graph.LabelSet{"Person"}.Key(), n.KeyLabels)
	s.Equal(graph.LabelSet{"Person"}.Key(), n.CompleteLabels, "no `=>`, so the key label set is inferred and coincides with the complete one")
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
		Source:    graph.LabelSet{"Person"}.Key(),
		KeyLabels: graph.LabelSet{"AUTHORED"}.Key(),
		Target:    graph.LabelSet{"Post"}.Key(),
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
		Source:    graph.LabelSet{"Person"}.Key(),
		KeyLabels: graph.LabelSet{"KNOWS"}.Key(),
		Target:    graph.LabelSet{"Person"}.Key(),
	}
	_, ok := got.Edges[key]
	s.True(ok, "inline filler endpoints resolve to the declared node type")
}

// TestEndpointFillerEmptyBraceAllowed pins that an inline endpoint filler
// spelled with an empty property-type spec `{}` is NOT rejected — the author
// wrote no properties, so there is nothing to silently drop. The guard fires
// only when the property-type list has entries. Keeps a future widening of
// TestEndpointFillerRejectsProperties honest.
func (s *ParserSuite) TestEndpointFillerEmptyBraceAllowed() {
	src := `CREATE PROPERTY GRAPH TYPE T AS {
		(:Person),
		(:Post),
		(:Person) -[:AUTHORED]-> (:Post {})
	}`
	got, err := New().Parse(strings.NewReader(src))
	s.Require().NoError(err)
	key := schema.EdgeKey{
		Source:    graph.LabelSet{"Person"}.Key(),
		KeyLabels: graph.LabelSet{"AUTHORED"}.Key(),
		Target:    graph.LabelSet{"Post"}.Key(),
	}
	_, ok := got.Edges[key]
	s.True(ok, "empty-brace endpoint filler resolves to the label-only spelling")
}

// TestEndpointFillerRejectsProperties pins the h9n.18 semantics: an inline
// endpoint filler that carries a property-type set is rejected, symmetric on
// source and destination. Previously the filler's properties were parsed and
// silently discarded (the model matched the label-only spelling byte for byte,
// with err=nil), which is the silent-wrong-answer shape the h9n corpus epic
// exists to eliminate. Symmetry is not incidental: fillerLabels is called from
// both sides via the same helper, so a source-only or destination-only guard
// would put the two sides in different behaviours with no principled basis.
//
// A label-only inline endpoint (the shape TestEdgeInlineEndpoints already pins)
// stays green — this test does not regress that case; the guard only fires on
// a non-empty property-type list (TestEndpointFillerEmptyBraceAllowed pins the
// empty-brace tolerance).
func (s *ParserSuite) TestEndpointFillerRejectsProperties() {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "destination-side filler carries a property (the bead witness)",
			src: `(:Person),
				(:Post),
				(:Person) -[:AUTHORED]-> (:Post { extra :: STRING })`,
		},
		{
			name: "source-side filler carries a property",
			src: `(:Person),
				(:Post),
				(:Person { extra :: STRING }) -[:AUTHORED]-> (:Post)`,
		},
		{
			name: "both sides carry properties",
			src: `(:Person),
				(:Post),
				(:Person { a :: INT }) -[:AUTHORED]-> (:Post { b :: STRING })`,
		},
		{
			name: "left-pointing arc, source-side (canonical) filler carries a property",
			src: `(:Person),
				(:Post),
				(:Person) <-[:AUTHORED]- (:Post { extra :: STRING })`,
		},
	}
	for _, tt := range cases {
		s.Run(tt.name, func() {
			got, err := New().Parse(strings.NewReader(graphType(tt.src)))
			s.Require().ErrorIs(err, ErrEndpointFillerHasProperties)
			s.Equal(schema.Schema{}, got, "model must be the zero value on error")
		})
	}
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
		Source:    graph.LabelSet{"Post"}.Key(),
		KeyLabels: graph.LabelSet{"WRITTEN_BY"}.Key(),
		Target:    graph.LabelSet{"Person"}.Key(),
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
		Source:    graph.LabelSet{"Person"}.Key(),
		KeyLabels: graph.LabelSet{"AUTHORED"}.Key(),
		Target:    graph.LabelSet{"Post"}.Key(),
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
// a listener that collected the labels and dropped the type name, the alias or the
// properties.
//
// The node and edge counts are not redundant with the comparison. Two spellings
// that both collect nothing are equal, and collecting nothing is the defect this
// bead exists for: the counts are what stop the equality holding vacuously.
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
// an alias. The slots the grammar reads as an alias — the phrase form's CONNECTING
// pair and the pattern form's parenthesised reference — take a bare identifier, so
// an author who writes the node type's name or label there gets a lookup miss for
// a type that is declared on the screen in front of them. Distinguishing that from
// a genuine typo is the whole value of ErrEndpointNotAlias, so the undeclared case
// is here to keep the distinction load-bearing.
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

// TestEdgeKindArcConsistency pins the two halves of gqlc-h9n.3: the decision
// that an explicit edgeKind must not contradict the arc/connector direction, and
// the ordering that fires the mismatch sentinel *before* the accepted-subset
// ErrUndirectedEdge rejection when both would apply. The matrix covers both edge
// forms — the pattern form (where edgeKind is optional) and the phrase form
// (where edgeKind is mandatory, GQL.g4:1558) — because EnterEdgeTypePattern and
// EnterEdgeTypePhrase collect independently and each has to read the kind on
// its own; dropping the check from either leaves that form silently reinterpreting.
//
// The bare-kind rows are what stops a lazier fix passing: they pin that a
// no-kind pattern still resolves, i.e. the mismatch is a kind-vs-arc check and
// not just "any UNDIRECTED token forbids a directed arc". The UNDIRECTED+~ and
// DIRECTED+-> agreement rows pin that consistent kinds are not swept up as
// mismatches. The DIRECTED+~ row pins the ordering: without the mismatch firing
// first, that spelling reports ErrUndirectedEdge and names the wrong mistake.
func TestEdgeKindArcConsistency(t *testing.T) {
	// Pattern form. edgeTypeName is required whenever any of the prefix is
	// present (GQL.g4:1553), so kind-with-name is the only pattern-form spelling
	// that reaches an edge-kind token; the bare-arc rows have no prefix at all.
	patternCases := []struct {
		name string
		src  string
		want error // nil = expect success
	}{
		{
			// Bare arc, no kind. The common case; regressing this would break
			// almost every existing corpus file.
			name: "bare right arrow with no kind",
			src:  `(a) -[:E]-> (b)`,
			want: nil,
		},
		{
			// Left arrow with no kind. Canonicalisation invariant from PR #422.
			name: "bare left arrow with no kind",
			src:  `(a) <-[:E]- (b)`,
			want: nil,
		},
		{
			// Bare undirected arc still hits the accepted-subset rejection.
			name: "bare undirected arc with no kind stays ErrUndirectedEdge",
			src:  `(a) ~[:E]~ (b)`,
			want: ErrUndirectedEdge,
		},
		{
			// Kind agrees with arc, redundant but consistent — accept.
			name: "DIRECTED with right arrow is redundant and accepted",
			src:  `DIRECTED EDGE TYPE E (a) -[:E]-> (b)`,
			want: nil,
		},
		{
			// Same, left arrow: canonicalisation still runs.
			name: "DIRECTED with left arrow is redundant and accepted",
			src:  `DIRECTED EDGE TYPE E (a) <-[:E]- (b)`,
			want: nil,
		},
		{
			// Contradiction *and* accepted-subset rejection both apply here. The
			// mismatch must win — reordering the checks in the listener would
			// send this to ErrUndirectedEdge, which names the wrong mistake.
			name: "DIRECTED with undirected arc is the mismatch, not the undirected-arc gap",
			src:  `DIRECTED EDGE TYPE E (a) ~[:E]~ (b)`,
			want: ErrEdgeKindArcMismatch,
		},
		{
			// The headline defect from the bead — silent reinterpretation today.
			name: "UNDIRECTED with right arrow is a mismatch",
			src:  `UNDIRECTED EDGE TYPE E (a) -[:E]-> (b)`,
			want: ErrEdgeKindArcMismatch,
		},
		{
			// Left arrow, same mismatch — canonicalisation must not launder it.
			name: "UNDIRECTED with left arrow is a mismatch",
			src:  `UNDIRECTED EDGE TYPE E (a) <-[:E]- (b)`,
			want: ErrEdgeKindArcMismatch,
		},
		{
			// Kind agrees with arc (both say undirected), so *not* a mismatch —
			// falls through to the accepted-subset rejection.
			name: "UNDIRECTED with undirected arc is not a mismatch, stays ErrUndirectedEdge",
			src:  `UNDIRECTED EDGE TYPE E (a) ~[:E]~ (b)`,
			want: ErrUndirectedEdge,
		},
	}

	for _, tc := range patternCases {
		t.Run("pattern/"+tc.name, func(t *testing.T) {
			src := `CREATE PROPERTY GRAPH TYPE T AS {
				(a :A { id :: INT }),
				(b :B { id :: INT }),
				` + tc.src + `
			}`
			got, err := New().Parse(strings.NewReader(src))
			if tc.want == nil {
				require.NoError(t, err)
				require.Len(t, got.Edges, 1, "consistent kind/arc must yield exactly one edge")
				return
			}
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, schema.Schema{}, got, "model must be the zero value on error")
		})
	}

	// Phrase form. edgeKind is mandatory here, so every phrase-form spelling
	// carries a kind and can trip the mismatch. The `TO` connector is the same
	// alternative as `->` (connectorPointingRight) but a separate token — worth
	// pinning that the mismatch fires against it too, since the listener reads
	// EndpointPairDirected, not the connector token.
	phraseCases := []struct {
		name string
		src  string
		want error
	}{
		{"DIRECTED CONNECTING right arrow accepted", `DIRECTED EDGE :E CONNECTING (a -> b)`, nil},
		{"DIRECTED CONNECTING left arrow accepted", `DIRECTED EDGE :E CONNECTING (a <- b)`, nil},
		{"DIRECTED CONNECTING TO accepted", `DIRECTED EDGE :E CONNECTING (a TO b)`, nil},
		{
			"DIRECTED CONNECTING ~ is the mismatch, not ErrUndirectedEdge",
			`DIRECTED EDGE :E CONNECTING (a ~ b)`, ErrEdgeKindArcMismatch,
		},
		{
			"UNDIRECTED CONNECTING right arrow is a mismatch",
			`UNDIRECTED EDGE :E CONNECTING (a -> b)`, ErrEdgeKindArcMismatch,
		},
		{
			"UNDIRECTED CONNECTING left arrow is a mismatch",
			`UNDIRECTED EDGE :E CONNECTING (a <- b)`, ErrEdgeKindArcMismatch,
		},
		{
			"UNDIRECTED CONNECTING TO is a mismatch",
			`UNDIRECTED EDGE :E CONNECTING (a TO b)`, ErrEdgeKindArcMismatch,
		},
		{
			"UNDIRECTED CONNECTING ~ is not a mismatch, stays ErrUndirectedEdge",
			`UNDIRECTED EDGE :E CONNECTING (a ~ b)`, ErrUndirectedEdge,
		},
	}

	for _, tc := range phraseCases {
		t.Run("phrase/"+tc.name, func(t *testing.T) {
			src := `CREATE PROPERTY GRAPH TYPE T AS {
				(a :A { id :: INT }),
				(b :B { id :: INT }),
				` + tc.src + `
			}`
			got, err := New().Parse(strings.NewReader(src))
			if tc.want == nil {
				require.NoError(t, err)
				require.Len(t, got.Edges, 1, "consistent kind/connector must yield exactly one edge")
				return
			}
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, schema.Schema{}, got, "model must be the zero value on error")
		})
	}
}

// TestGraphTypeSourceRejectionsAreDistinguishable pins the split gqlc-h9n.12
// made. LIKE and COPY OF are both rejected and shared one sentinel, which meant
// the deviation record carried two justifications against a single error and
// could not say which applied: LIKE takes a graphExpression that reaches session
// state, so no amount of implementation makes it resolvable, while COPY OF names
// a catalogue entry and is resolvable in principle (ADR 0016). Telling "never"
// from "not yet" apart requires the two errors not to be each other.
func TestGraphTypeSourceRejectionsAreDistinguishable(t *testing.T) {
	like, likeErr := New().Parse(strings.NewReader(`CREATE PROPERTY GRAPH TYPE T LIKE SomeGraph`))
	require.ErrorIs(t, likeErr, ErrLikeGraphSource)
	require.NotErrorIs(t, likeErr, ErrCopyOfSource)
	require.Equal(t, schema.Schema{}, like, "model must be the zero value on error")

	copied, copyErr := New().Parse(strings.NewReader(`CREATE PROPERTY GRAPH TYPE T COPY OF SomeType`))
	require.ErrorIs(t, copyErr, ErrCopyOfSource)
	require.NotErrorIs(t, copyErr, ErrLikeGraphSource)
	require.Equal(t, schema.Schema{}, copied, "model must be the zero value on error")
}

// TestGraphTypeSourceErrorsWrapTheClass pins the relationship that lets
// ErrUnsupportedSource stay out of allSentinels: it is the class the leaves wrap,
// not an error any file produces, so the sweep would report it orphaned. What
// makes that safe is exactly this — a caller asking only "was the graph type
// source rejected" keeps matching every rejection, so the split widened the
// public surface rather than narrowing it.
func TestGraphTypeSourceErrorsWrapTheClass(t *testing.T) {
	for _, leaf := range []error{ErrLikeGraphSource, ErrCopyOfSource} {
		require.ErrorIs(t, leaf, ErrUnsupportedSource)
	}
}

// TestNestedGraphTypeSpecificationElementsNotCollected pins that an element type
// declared inside a closedGraphReferenceValueType body (GQL.g4:1926, a whole
// graph type nested as a property value type) does not enter the outer graph
// type's rawSchema. Asserting on Parse cannot see the defect today: the outer
// property fails normaliseType with ErrUnsupportedType before resolve() runs, so
// Parse returns (zero schema, err) whether the nested elements leaked into l.raw
// or not — the correct-by-accident shape the bead was filed against. Walking the
// listener directly and reading raw makes the guard visible on its own.
//
// The four spellings (pattern/phrase × node/edge) are here because ANTLR calls a
// separate Enter method for each and the guard has to fire from each of them —
// dropping the check from any one leaks that spelling. The twice-nested case
// distinguishes "depth > 1" from "depth == 2": both variants of the guard reject
// the once-nested case, only "depth > 1" rejects a node three levels deep.
func TestNestedGraphTypeSpecificationElementsNotCollected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// The bead's original witness. Two pattern-form nodes leak from the
			// nested body into the outer graph type when the guard is absent.
			name: "pattern-form nodes in a nested body",
			src: `CREATE PROPERTY GRAPH TYPE T AS {
				(:Outer { g :: PROPERTY GRAPH { (:Inner1), (:Inner2) } })
			}`,
		},
		{
			// The nested body carries an edge as well; the edge collector needs
			// the guard for the same reason the node collector does.
			name: "pattern-form edge in a nested body",
			src: `CREATE PROPERTY GRAPH TYPE T AS {
				(:Outer { g :: GRAPH { (:A), (:B), (:A)-[:R]->(:B) } })
			}`,
		},
		{
			// EnterNodeTypePhrase collects independently of EnterNodeTypePattern
			// and needs the guard on its own; a nested phrase-form node in a
			// pattern-form outer leaks without it.
			name: "phrase-form node in a pattern-form outer's nested body",
			src: `CREATE PROPERTY GRAPH TYPE T AS {
				(:Outer { g :: GRAPH { NODE TYPE Inner :Inner } })
			}`,
		},
		{
			// EnterEdgeTypePhrase, same reason.
			name: "phrase-form edge in a nested body",
			src: `CREATE PROPERTY GRAPH TYPE T AS {
				(:Outer { g :: GRAPH { (a :A), (b :B), DIRECTED EDGE :R CONNECTING (a -> b) } })
			}`,
		},
		{
			// depth 2: the innermost (:C) is under two enclosing nested bodies,
			// so a `depth == 1` guard would let it through where `depth > 1` (the
			// correct rule) blocks it.
			name: "twice-nested node",
			src: `CREATE PROPERTY GRAPH TYPE T AS {
				(:A { p :: GRAPH { (:B { q :: GRAPH { (:C) } }) } })
			}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lex := gen.NewGQLLexer(antlr.NewInputStream(tc.src))
			ts := antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel)
			p := gen.NewGQLParser(ts)
			l := &listener{ts: ts}
			lex.RemoveErrorListeners()
			lex.AddErrorListener(l)
			p.RemoveErrorListeners()
			p.AddErrorListener(l)

			// The walk errors on the outer property's unsupported value type;
			// pinning that here means a future bead lifting the type rejection
			// has to update this test rather than silently uncover the leak.
			require.ErrorIs(t, l.walk(p.GqlProgram()), ErrUnsupportedType)

			require.Empty(t, l.raw.nodes, "nested-body nodes must not be collected as elements of the outer graph type")
			require.Empty(t, l.raw.edges, "nested-body edges must not be collected as elements of the outer graph type")
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
	"syntax_error.gql":         nil,
	"undirected_edge.gql":      ErrUndirectedEdge,
	"unknown_endpoint.gql":     ErrUnknownEndpoint,
	"endpoint_not_alias.gql":   ErrEndpointNotAlias,
	"unsupported_type.gql":     ErrUnsupportedType,
	"unnamed_node.gql":         ErrUnnamedNodeType,
	"unnamed_edge.gql":         ErrUnnamedEdgeType,
	"duplicate_node.gql":       ErrDuplicateNodeType,
	"duplicate_edge.gql":       ErrDuplicateEdgeType,
	"no_graph_type.gql":        ErrNoGraphType,
	"multiple_graph_types.gql": ErrMultipleGraphTypes,
	"like_graph_source.gql":    ErrLikeGraphSource,
}

// allSentinels is the canonical list of every Parse sentinel — the single source
// of truth TestSentinelReachability checks against. A new sentinel must be added
// here (and pinned by a file); a removed one must be dropped.
//
// It lists leaves. ErrUnsupportedSource is absent because it is a class the two
// graph-type-source leaves wrap, and no file produces it bare — see errors.go and
// TestGraphTypeSourceErrorsWrapTheClass, which is the pin it gets instead.
var allSentinels = []error{
	ErrUndirectedEdge,
	ErrEdgeKindArcMismatch,
	ErrUnknownEndpoint,
	ErrEndpointNotAlias,
	ErrEndpointFillerImpliesLabels,
	ErrImpliedLabelIsKeyLabel,
	ErrUnsupportedType,
	ErrUnnamedNodeType,
	ErrUnnamedEdgeType,
	ErrDuplicateNodeType,
	ErrDuplicateEdgeType,
	ErrNoGraphType,
	ErrMultipleGraphTypes,
	ErrLikeGraphSource,
	ErrCopyOfSource,
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
