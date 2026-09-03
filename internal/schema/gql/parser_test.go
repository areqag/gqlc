package gql_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
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
	"github.com/areqag/gqlc/internal/schema/gql"
	"github.com/areqag/gqlc/internal/schema/gql/isobnf"
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
	return gql.New().Parse(bytes.NewReader(src))
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
//
// ADR 0018 records both as decisions rather than as what the code happens to do:
// the modifiers are conditions on a catalogue a static generator does not have,
// and the truncation is safe only while Schema.Name is a package label and not a
// lookup key — which COPY OF (gqlc-h9n.1) would change.
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
			got, err := gql.New().Parse(strings.NewReader(tt.src))
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

	got, err := gql.New().Parse(strings.NewReader(src))
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

	got, err := gql.New().Parse(strings.NewReader(src))
	s.Require().NoError(err)

	n, ok := got.Nodes[graph.LabelSet{"Person"}.Key()]
	s.Require().True(ok)
	s.Equal("PersonType", n.Name)
}

// TestNodeMultiLabelIdentity covers a node typed by more than one label: the
// canonical key is order- and duplicate-independent.
func (s *ParserSuite) TestNodeMultiLabelIdentity() {
	src := `CREATE PROPERTY GRAPH TYPE T AS { (:Employee&Person { id :: INT }) }`

	got, err := gql.New().Parse(strings.NewReader(src))
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

	got, err := gql.New().Parse(strings.NewReader(src))
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

	got, err := gql.New().Parse(strings.NewReader(src))
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
	got, err := gql.New().Parse(strings.NewReader(src))
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
			got, err := gql.New().Parse(strings.NewReader(graphType(tt.src)))
			s.Require().ErrorIs(err, gql.ErrEndpointFillerHasProperties)
			s.Equal(schema.Schema{}, got, "model must be the zero value on error")
		})
	}
}

// TestEndpointFillerMixedRejectionsReportTheSource pins which end is named when
// BOTH ends of an arc are defective and the two defects have different sentinels.
// The endpoints are read source-then-destination and the read short-circuits, so
// the source's rejection is the one reported.
//
// The case above stops one sentinel short of seeing this: it puts a defect on both
// ends, but the same defect, so the answer is that sentinel whichever end is read
// first. With one end defective the short-circuit reports that end either way
// round too. The order is observable only across the two sentinels fillerLabels
// can return, which is what these cases pair up.
//
// The left-pointing rows are the point. `<-` reports the RIGHTMOST written
// endpoint as the source, so "source first" and "leftmost first" disagree there,
// and the rule is by role: the same end is named whichever direction the author
// wrote the arc in, so respelling an edge does not move its diagnostic. That is
// the property being pinned — not the source-then-destination read order, which is
// merely how it is currently obtained.
//
// Both sentinels are asserted, presence and absence. Asserting only the winner
// would still pass if the loser were also joined in, which is a different
// behaviour with the same ErrorIs result.
func (s *ParserSuite) TestEndpointFillerMixedRejectionsReportTheSource() {
	cases := []struct {
		name    string
		src     string
		want    error
		notWant error
	}{
		{
			name: "right arc, source implies labels and destination carries properties",
			src: `(:Person),
				(:Post),
				(:Person => :Employee) -[:AUTHORED]-> (:Post { extra :: STRING })`,
			want:    gql.ErrEndpointFillerImpliesLabels,
			notWant: gql.ErrEndpointFillerHasProperties,
		},
		{
			name: "right arc, source carries properties and destination implies labels",
			src: `(:Person),
				(:Post),
				(:Person { extra :: STRING }) -[:AUTHORED]-> (:Post => :Article)`,
			want:    gql.ErrEndpointFillerHasProperties,
			notWant: gql.ErrEndpointFillerImpliesLabels,
		},
		{
			name: "left arc, source is written on the right and still wins",
			src: `(:Post { extra :: STRING }) <-[:AUTHORED]- (:Person => :Employee),
				(:Person),
				(:Post)`,
			want:    gql.ErrEndpointFillerImpliesLabels,
			notWant: gql.ErrEndpointFillerHasProperties,
		},
		{
			name: "left arc, the other pairing of the same two defects",
			src: `(:Post => :Article) <-[:AUTHORED]- (:Person { extra :: STRING }),
				(:Person),
				(:Post)`,
			want:    gql.ErrEndpointFillerHasProperties,
			notWant: gql.ErrEndpointFillerImpliesLabels,
		},
	}
	for _, tt := range cases {
		s.Run(tt.name, func() {
			got, err := gql.New().Parse(strings.NewReader(graphType(tt.src)))
			s.Require().ErrorIs(err, tt.want)
			s.Require().NotErrorIs(err, tt.notWant,
				"the destination's rejection must not be reported alongside the source's")
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

	got, err := gql.New().Parse(strings.NewReader(src))
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

	got, err := gql.New().Parse(strings.NewReader(src))
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
			pattern, err := gql.New().Parse(strings.NewReader(graphType(tt.pattern)))
			s.Require().NoError(err)
			s.Require().Len(pattern.Nodes, tt.nodes)
			s.Require().Len(pattern.Edges, tt.edges)

			phrase, err := gql.New().Parse(strings.NewReader(graphType(tt.phrase)))
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
			want: gql.ErrEndpointNotAlias,
		},
		{
			name: "phrase endpoint names the node type name rather than its label",
			src: `NODE TYPE PersonType :Person { id :: INT },
				DIRECTED EDGE :KNOWS CONNECTING (PersonType TO PersonType)`,
			want: gql.ErrEndpointNotAlias,
		},
		{
			name: "phrase endpoint names a node type aliased under another name",
			src: `NODE TYPE :Person { id :: INT } AS p,
				DIRECTED EDGE :KNOWS CONNECTING (Person TO Person)`,
			want: gql.ErrEndpointNotAlias,
		},
		{
			name: "pattern endpoint names a node type that binds no alias",
			src: `(:Person { id :: INT }),
				(Person) -[:KNOWS]-> (Person)`,
			want: gql.ErrEndpointNotAlias,
		},
		{
			// idx.declared carries the complete label set, not just the key
			// one, so an implied label is the same mistake as a key label.
			name: "pattern endpoint names a label the node type only implies",
			src: `(:Engineer => :Staff { level :: INT }),
				(Staff) -[:KNOWS]-> (Staff)`,
			want: gql.ErrEndpointNotAlias,
		},
		{
			name: "phrase endpoint names nothing declared",
			src: `NODE TYPE :Person { id :: INT } AS p,
				DIRECTED EDGE :KNOWS CONNECTING (p TO Ghost)`,
			want: gql.ErrUnknownEndpoint,
		},
		{
			// The target arm and the source arm forward their failures
			// independently, so a target-only case leaves the source one
			// free to stop propagating and take the schema with it.
			name: "phrase endpoint names nothing declared on the source side",
			src: `NODE TYPE :Person { id :: INT } AS p,
				DIRECTED EDGE :KNOWS CONNECTING (Ghost TO p)`,
			want: gql.ErrUnknownEndpoint,
		},
	}

	for _, tt := range cases {
		s.Run(tt.name, func() {
			got, err := gql.New().Parse(strings.NewReader(graphType(tt.src)))
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
			want: gql.ErrUndirectedEdge,
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
			want: gql.ErrEdgeKindArcMismatch,
		},
		{
			// The headline defect from the bead — silent reinterpretation today.
			name: "UNDIRECTED with right arrow is a mismatch",
			src:  `UNDIRECTED EDGE TYPE E (a) -[:E]-> (b)`,
			want: gql.ErrEdgeKindArcMismatch,
		},
		{
			// Left arrow, same mismatch — canonicalisation must not launder it.
			name: "UNDIRECTED with left arrow is a mismatch",
			src:  `UNDIRECTED EDGE TYPE E (a) <-[:E]- (b)`,
			want: gql.ErrEdgeKindArcMismatch,
		},
		{
			// Kind agrees with arc (both say undirected), so *not* a mismatch —
			// falls through to the accepted-subset rejection.
			name: "UNDIRECTED with undirected arc is not a mismatch, stays ErrUndirectedEdge",
			src:  `UNDIRECTED EDGE TYPE E (a) ~[:E]~ (b)`,
			want: gql.ErrUndirectedEdge,
		},
	}

	for _, tc := range patternCases {
		t.Run("pattern/"+tc.name, func(t *testing.T) {
			src := `CREATE PROPERTY GRAPH TYPE T AS {
				(a :A { id :: INT }),
				(b :B { id :: INT }),
				` + tc.src + `
			}`
			got, err := gql.New().Parse(strings.NewReader(src))
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
			`DIRECTED EDGE :E CONNECTING (a ~ b)`, gql.ErrEdgeKindArcMismatch,
		},
		{
			"UNDIRECTED CONNECTING right arrow is a mismatch",
			`UNDIRECTED EDGE :E CONNECTING (a -> b)`, gql.ErrEdgeKindArcMismatch,
		},
		{
			"UNDIRECTED CONNECTING left arrow is a mismatch",
			`UNDIRECTED EDGE :E CONNECTING (a <- b)`, gql.ErrEdgeKindArcMismatch,
		},
		{
			"UNDIRECTED CONNECTING TO is a mismatch",
			`UNDIRECTED EDGE :E CONNECTING (a TO b)`, gql.ErrEdgeKindArcMismatch,
		},
		{
			"UNDIRECTED CONNECTING ~ is not a mismatch, stays ErrUndirectedEdge",
			`UNDIRECTED EDGE :E CONNECTING (a ~ b)`, gql.ErrUndirectedEdge,
		},
	}

	for _, tc := range phraseCases {
		t.Run("phrase/"+tc.name, func(t *testing.T) {
			src := `CREATE PROPERTY GRAPH TYPE T AS {
				(a :A { id :: INT }),
				(b :B { id :: INT }),
				` + tc.src + `
			}`
			got, err := gql.New().Parse(strings.NewReader(src))
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

// TestConnectorToResolvesPointingRight pins which way `CONNECTING (a TO b)` runs,
// and the reason it needs pinning is that the grammar does not say. `TO` is an
// alternative of both connectorPointingRight and connectorUndirected
// (GQL.g4:1659-1667), so the text is genuinely ambiguous; what settles it is that
// endpointPair lists endpointPairDirected before endpointPairUndirected
// (GQL.g4:1637-1640) and ANTLR takes the first alternative that matches. The
// direction gqlc reports is therefore a property of rule order in a vendored
// community grammar (internal/grammar/gql/SOURCE.md), not of anything ISO states
// here, and reordering those two alternatives — or regenerating from an upstream
// that has — would flip every `TO` in every schema silently.
//
// Asserting the source and target rather than "no error" is the point: a flip
// makes `DIRECTED ... (a TO b)` a kind/connector contradiction, but it would also
// make a *kindless* `TO` an undirected edge, and the two endpoints are what a
// generated repository is built from.
func TestConnectorToResolvesPointingRight(t *testing.T) {
	src := `CREATE PROPERTY GRAPH TYPE T AS {
		(a :A { id :: INT }),
		(b :B { id :: INT }),
		DIRECTED EDGE TYPE E LABEL E CONNECTING (a TO b)
	}`
	got, err := gql.New().Parse(strings.NewReader(src))
	require.NoError(t, err)
	require.Len(t, got.Edges, 1)

	arrow := `CREATE PROPERTY GRAPH TYPE T AS {
		(a :A { id :: INT }),
		(b :B { id :: INT }),
		DIRECTED EDGE TYPE E LABEL E CONNECTING (a -> b)
	}`
	viaArrow, err := gql.New().Parse(strings.NewReader(arrow))
	require.NoError(t, err)
	require.Equal(t, viaArrow, got, "TO and -> are the same alternative and must resolve identically")

	for key := range got.Edges {
		require.Equal(t, graph.LabelSetKey("A"), key.Source, "TO must read left-to-right: source is the first endpoint")
		require.Equal(t, graph.LabelSetKey("B"), key.Target, "TO must read left-to-right: target is the second endpoint")
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
	like, likeErr := gql.New().Parse(strings.NewReader(`CREATE PROPERTY GRAPH TYPE T LIKE SomeGraph`))
	require.ErrorIs(t, likeErr, gql.ErrLikeGraphSource)
	require.NotErrorIs(t, likeErr, gql.ErrCopyOfSource)
	require.Equal(t, schema.Schema{}, like, "model must be the zero value on error")

	copied, copyErr := gql.New().Parse(strings.NewReader(`CREATE PROPERTY GRAPH TYPE T COPY OF SomeType`))
	require.ErrorIs(t, copyErr, gql.ErrCopyOfSource)
	require.NotErrorIs(t, copyErr, gql.ErrLikeGraphSource)
	require.Equal(t, schema.Schema{}, copied, "model must be the zero value on error")
}

// TestGraphTypeSourceErrorsWrapTheClass pins the relationship that lets
// ErrUnsupportedSource stay out of allSentinels: it is the class the leaves wrap,
// not an error any file produces, so the sweep would report it orphaned. What
// makes that safe is exactly this — a caller asking only "was the graph type
// source rejected" keeps matching every rejection, so the split widened the
// public surface rather than narrowing it.
func TestGraphTypeSourceErrorsWrapTheClass(t *testing.T) {
	for _, leaf := range []error{gql.ErrLikeGraphSource, gql.ErrCopyOfSource} {
		require.ErrorIs(t, leaf, gql.ErrUnsupportedSource)
	}
}

// valueTypeFamilies pairs each declined family's sentinel with the ISO production
// it is named after and a spelling that reaches it. One row per sentinel, not per
// spelling: the corpus already covers every alternative, and what this table is
// for is the claim errors.go makes about the *names*.
var valueTypeFamilies = []struct {
	sentinel   error
	production string
	spelling   string
}{
	{gql.ErrPathValueType, "path value type", "PATH"},
	{gql.ErrReferenceValueType, "reference value type", "ANY NODE"},
	{gql.ErrImmaterialValueType, "immaterial value type", "NOTHING"},
}

// TestValueTypeFamiliesAreIsoProductions is what entitles errors.go to say the
// taxonomy is ISO's rather than one gqlc invented to suit its internals. The
// claim is cheap to make and would rot silently: a sentinel renamed to describe
// a Go-side concern would read just as plausibly, and only this notices that the
// name has stopped matching a production the standard actually has.
//
// It checks the DDL closure specifically, not the whole 814-production list, so
// a family named after a production only reachable from a query would fail here.
func TestValueTypeFamiliesAreIsoProductions(t *testing.T) {
	for _, family := range valueTypeFamilies {
		require.Contains(t, isobnf.DDLClosure, family.production,
			"%v is named after %q, which is not an ISO production reachable from CREATE GRAPH TYPE", family.sentinel, family.production)
	}
}

// TestValueTypeFamilyErrorsWrapTheClass keeps the surviving leaves matching a
// caller that asks only whether the property type was rejected. That caller is
// the reason ADR 0019 could split the sentinel at all: the split widened the
// public surface rather than narrowing it, so no existing errors.Is stopped
// working. It is also what let gqlc-h9n.33 delete two of the five leaves without
// breaking such a caller — the class it asks about outlived them.
func TestValueTypeFamilyErrorsWrapTheClass(t *testing.T) {
	for _, family := range valueTypeFamilies {
		require.ErrorIs(t, family.sentinel, gql.ErrUnsupportedType)
	}
}

// TestValueTypeFamilyDeclines pins the family each spelling lands in. The corpus
// pins the same thing file by file; this exists because the corpus entries are
// data and would follow a wrong classifier — if declineValueType started
// answering ErrReferenceValueType for NOTHING, updating the corpus entry to
// match would be the obvious fix and the register would stay green.
//
// The spellings are deliberately the *bare* ones, and one of the three still
// nests a value type: `BINDING TABLE { id :: STRING }` carries a predefinedType
// for its field, so a classifier that descended into a DECLINED family would
// answer for the field instead. The corpus catches that; this pins the shape it
// relies on. Descent into a RESOLVING family is a different thing and is now
// deliberate — see TestDeclinedFamilyInsideRecordOrUnionSurfacesItsOwnSentinel.
func TestValueTypeFamilyDeclines(t *testing.T) {
	for _, family := range valueTypeFamilies {
		t.Run(family.spelling, func(t *testing.T) {
			src := fmt.Sprintf(`CREATE PROPERTY GRAPH TYPE T AS {
				(:N { p :: %s })
			}`, family.spelling)
			_, err := gql.New().Parse(strings.NewReader(src))
			require.ErrorIs(t, err, family.sentinel)
		})
	}
}

// TestListPropertyResolves pins that all five LIST/ARRAY spellings are accepted
// after gqlc-h9n.5. The four corpus entries that previously pinned ErrUnsupportedType
// for these now have outcome:resolves; this unit pin catches a regression where
// the parser rejects a list type that the corpus files do not happen to exercise.
func TestListPropertyResolves(t *testing.T) {
	for _, src := range []string{
		`CREATE PROPERTY GRAPH TYPE T AS { (:N { tags :: LIST<INT> }) }`,
		`CREATE PROPERTY GRAPH TYPE T AS { (:N { tags :: INT LIST }) }`,
		`CREATE PROPERTY GRAPH TYPE T AS { (:N { tags :: LIST }) }`,
	} {
		_, err := gql.New().Parse(strings.NewReader(src))
		require.NoError(t, err)
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
			l := gql.NewListener(ts)
			lex.RemoveErrorListeners()
			lex.AddErrorListener(l)
			p.RemoveErrorListeners()
			p.AddErrorListener(l)

			// The walk errors on the outer property's unsupported value type;
			// pinning that here means a future bead lifting the type rejection
			// has to update this test rather than silently uncover the leak.
			require.ErrorIs(t, gql.ListenerWalk(l, p.GqlProgram()), gql.ErrUnsupportedType)

			require.Empty(t, l.RawNodes(), "nested-body nodes must not be collected as elements of the outer graph type")
			require.Empty(t, l.RawEdges(), "nested-body edges must not be collected as elements of the outer graph type")
		})
	}
}

// The positive control for the three listener accessors the test above reads
// through. Every assertion up there is an emptiness or a NoError, so an accessor
// that returned the zero value unconditionally would satisfy all of them and the
// nested-body guard would be pinned by nothing. Measured: without the rows
// below, mutating Err, RawNodes and RawEdges each to `return nil` SURVIVES the
// whole package (bd gqlc-m5rc).
func TestListenerAccessorsReportWhatTheyName(t *testing.T) {
	walk := func(t *testing.T, src string) (*gql.Listener, error) {
		t.Helper()
		lex := gen.NewGQLLexer(antlr.NewInputStream(src))
		ts := antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel)
		p := gen.NewGQLParser(ts)
		l := gql.NewListener(ts)
		lex.RemoveErrorListeners()
		lex.AddErrorListener(l)
		p.RemoveErrorListeners()
		p.AddErrorListener(l)
		return l, gql.ListenerWalk(l, p.GqlProgram())
	}

	t.Run("a clean walk collects the outer graph type's own elements", func(t *testing.T) {
		l, err := walk(t, `CREATE PROPERTY GRAPH TYPE T AS {
			(:A), (:B), (:A)-[:R]->(:B)
		}`)
		require.NoError(t, err)
		require.NoError(t, l.Err())
		require.Len(t, l.RawNodes(), 2)
		require.Len(t, l.RawEdges(), 1)
	})

	t.Run("a syntax error reaches Err", func(t *testing.T) {
		l, err := walk(t, `CREATE PROPERTY GRAPH TYPE T AS { (:A`)
		require.Error(t, err)
		require.Error(t, l.Err())
	})
}

// TestSyntaxErrorNamesTheOffendingToken pins the branch of listener.SyntaxError
// that decides no outcome and exists only to sharpen the message. Nothing read
// it: TestInvalid maps syntax_error.gql to nil, which it documents as "any
// non-nil error from the syntax error listener satisfies it", and
// TestPropertyBareDurationRejectedAtParse asserts the "syntax error at " prefix
// both branches share. Deleting the branch left every test green.
//
// ErrEndpointNotAlias is the same kind of branch — it changes what a rejection
// is called and never whether it is one — and removing that one is caught. This
// makes the two consistent.
//
// Both cases are here because they fail differently. `fish` is the first token
// of the input, where line:column alone would nearly do; `}` is reported four
// tokens past the construct that is actually wrong, which is where naming the
// token earns its keep.
func TestSyntaxErrorNamesTheOffendingToken(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join(fixtureDir, "invalid", "syntax_error.gql"))
	require.NoError(t, err)

	for _, tc := range []struct {
		name  string
		src   string
		token string
	}{
		{name: "invalid/syntax_error.gql", src: string(fixture), token: "fish"},
		{name: "bare DURATION", src: `CREATE PROPERTY GRAPH TYPE T AS { (:A { p :: DURATION }) }`, token: "}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gql.New().Parse(strings.NewReader(tc.src))
			require.Error(t, err)
			require.Contains(t, err.Error(), fmt.Sprintf("near %q", tc.token),
				"a syntax error must name the offending token, not only its line and column; got: %v", err)
		})
	}
}

// TestLexerErrorsAreNotDropped pins the lexer half of Parse's error plumbing.
// Parse attaches the listener to the lexer as well as the parser, and deleting
// the lexer half left the whole suite green: every other bad character in
// invalid/ sits inside a construct, so the parser objects independently and
// losing the lexer listener only changes the message.
//
// Position is what separates the two halves. Here the graph type is complete
// before the offending token, so the token stream the parser sees ends at EOF
// with or without it — the parser has nothing to object to, and dropping the
// lexer listener makes this input parse cleanly to one node type. Silently
// accepting trailing garbage is the failure; a worse message is not.
//
// The without-the-token parse is the assertion that carries the claim. It is
// what shows the rejection is the lexer's doing rather than the parser's,
// without pinning ANTLR's wording — if a grammar change ever made `#` mean
// something, this would fail here rather than quietly stop testing the lexer.
func TestLexerErrorsAreNotDropped(t *testing.T) {
	const clean = `CREATE PROPERTY GRAPH TYPE T AS { (:A { p :: INT }) }`

	got, err := gql.New().Parse(strings.NewReader(clean))
	require.NoError(t, err)
	require.Len(t, got.Nodes, 1, "the source without the offending token must be valid")

	fixture, err := os.ReadFile(filepath.Join(fixtureDir, "invalid", "lexer_error.gql"))
	require.NoError(t, err)

	_, err = gql.New().Parse(strings.NewReader(string(fixture)))
	require.Error(t, err, "a token the lexer cannot recognise must be rejected even where the parser is content")
	require.Contains(t, err.Error(), "syntax error at ")
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
	"lexer_error.gql":          nil,
	"undirected_edge.gql":      gql.ErrUndirectedEdge,
	"unknown_endpoint.gql":     gql.ErrUnknownEndpoint,
	"endpoint_not_alias.gql":   gql.ErrEndpointNotAlias,
	"path_value_type.gql":      gql.ErrPathValueType,
	"unnamed_node.gql":         gql.ErrUnnamedNodeType,
	"unnamed_edge.gql":         gql.ErrUnnamedEdgeType,
	"duplicate_node.gql":       gql.ErrDuplicateNodeType,
	"duplicate_edge.gql":       gql.ErrDuplicateEdgeType,
	"no_graph_type.gql":        gql.ErrNoGraphType,
	"multiple_graph_types.gql": gql.ErrMultipleGraphTypes,
	"like_graph_source.gql":    gql.ErrLikeGraphSource,

	// An invalid/ fixture rather than a corpus entry, unlike its sibling
	// ErrEndpointFillerImpliesLabels. The corpus half is argued below for sentinels
	// that are temporary by design, so the pin disappears together with the decline;
	// this rejection is structural — an endpoint filler names a node type, and a
	// property there has no consumer at any point — so there is no commit that would
	// flip it to resolves and leave the fixture behind. It also spares the entry an
	// Annex D claim about a construct nobody has researched, which isValidFeature
	// cannot check and which the manifest records being got wrong three times.
	"endpoint_filler_properties.gql": gql.ErrEndpointFillerHasProperties,
	"multi_label_edge.gql":           gql.ErrMultiLabelEdgeType,

	// The four identifier declines, in invalid/ rather than the corpus for the
	// reason argued above: each is a judgment about a SPELLING — an @ prefix, an
	// empty decode, an escape denoting no character, a label carrying the key
	// separator — and none of them is a construct that becomes supported later
	// and takes its fixture with it. gqlc-yd4ba could make the ampersand one
	// representable, and its fixture would then move to a valid one in the same
	// commit; that is a rewrite of this row, not an orphan left behind.
	"no_escape_identifier.gql": gql.ErrNoEscapeIdentifier,
	"empty_identifier.gql":     gql.ErrEmptyIdentifier,
	"identifier_escape.gql":    gql.ErrIdentifierEscape,
	"ampersand_in_label.gql":   gql.ErrAmpersandInLabel,
}

// allSentinels is the canonical list of every Parse sentinel — the single source
// of truth TestSentinelReachability checks against. A new sentinel must be added
// here (and pinned by a file); a removed one must be dropped.
//
// It lists leaves. ErrUnsupportedSource and ErrUnsupportedType are absent
// because they are classes the leaves wrap, and nothing produces them bare —
// see errors.go, TestGraphTypeSourceErrorsWrapTheClass, and sentinelsWithoutAFile.
//
// Keyed by the identifier rather than a bare slice, because the value alone cannot
// say what it is called and TestSentinelRegistryIsComplete has to compare these
// against the names errors.go declares. It also makes this test's failures name the
// sentinel instead of quoting its whole message.
var allSentinels = map[string]error{
	"ErrUndirectedEdge":              gql.ErrUndirectedEdge,
	"ErrEdgeKindArcMismatch":         gql.ErrEdgeKindArcMismatch,
	"ErrUnknownEndpoint":             gql.ErrUnknownEndpoint,
	"ErrEndpointNotAlias":            gql.ErrEndpointNotAlias,
	"ErrEndpointFillerHasProperties": gql.ErrEndpointFillerHasProperties,
	"ErrEndpointFillerImpliesLabels": gql.ErrEndpointFillerImpliesLabels,
	"ErrImpliedLabelIsKeyLabel":      gql.ErrImpliedLabelIsKeyLabel,
	"ErrUnnamedNodeType":             gql.ErrUnnamedNodeType,
	"ErrUnnamedEdgeType":             gql.ErrUnnamedEdgeType,
	"ErrMultiLabelEdgeType":          gql.ErrMultiLabelEdgeType,
	"ErrDuplicateNodeType":           gql.ErrDuplicateNodeType,
	"ErrDuplicateEdgeType":           gql.ErrDuplicateEdgeType,
	"ErrDuplicatePropertyName":       gql.ErrDuplicatePropertyName,
	"ErrDuplicateFieldName":          gql.ErrDuplicateFieldName,
	"ErrNoGraphType":                 gql.ErrNoGraphType,
	"ErrMultipleGraphTypes":          gql.ErrMultipleGraphTypes,
	"ErrLikeGraphSource":             gql.ErrLikeGraphSource,
	"ErrReferenceParameter":          gql.ErrReferenceParameter,
	"ErrHomeSchemaReference":         gql.ErrHomeSchemaReference,
	"ErrObjectParentReference":       gql.ErrObjectParentReference,
	"ErrDelimitedReferenceSegment":   gql.ErrDelimitedReferenceSegment,
	"ErrReferenceOutsideCatalogue":   gql.ErrReferenceOutsideCatalogue,
	"ErrDanglingReference":           gql.ErrDanglingReference,
	"ErrReferenceCycle":              gql.ErrReferenceCycle,
	"ErrReferenceNameMismatch":       gql.ErrReferenceNameMismatch,
	"ErrPathValueType":               gql.ErrPathValueType,
	"ErrReferenceValueType":          gql.ErrReferenceValueType,
	"ErrImmaterialValueType":         gql.ErrImmaterialValueType,
	"ErrNoEscapeIdentifier":          gql.ErrNoEscapeIdentifier,
	"ErrEmptyIdentifier":             gql.ErrEmptyIdentifier,
	"ErrIdentifierEscape":            gql.ErrIdentifierEscape,
	"ErrAmpersandInLabel":            gql.ErrAmpersandInLabel,
}

// sentinelsWithoutAFile are the package's error values allSentinels deliberately
// omits, each with the reason no file can pin it. A register rather than a silent
// subtraction: an omission with nowhere to write the reason is how
// ErrEndpointFillerHasProperties went missing for as long as it did.
var sentinelsWithoutAFile = map[string]string{
	"ErrUnsupportedSource": "a class the graph-type-source leaves wrap rather than a leaf; nothing produces it bare, so no file could pin it without first making one that does. TestGraphTypeSourceErrorsWrapTheClass is the pin it gets instead",
	"ErrCopyOfSource":      "reported by Parse alone, and every corpus file is read through the Loader now — which resolves a reference or names the reason it could not, and reports this never. So no fixture can pin it: a file that did would have to be parsed with no catalogue behind it, which is a property of the call and not of the file. TestParseRefusesEveryReachableReference in loader_test.go is the pin it gets instead, sweeping every reachable spelling rather than one",
	"ErrUnsupportedType":   "a class the value-type families wrap; after gqlc-h9n.5 nothing produces it bare (LIST/ARRAY was the only bare source, and it now resolves). It wrapped five families until gqlc-h9n.33 retired the two carrying a \"yet\", and wraps three now. Same posture as ErrUnsupportedSource",
}

// isSentinel reports whether err is one of the parser's sentinels. allSentinels is
// keyed by name, so membership is a lookup over the values; require.Contains against
// the map would ask whether an error is a name and pass for nothing.
//
// The reversed check (errors.Is(sentinel, err)) recognises class errors such as
// ErrUnsupportedType: nothing produces them bare, so they are not in allSentinels,
// but they are valid sentinels for declined carriage groups that span multiple
// specific families. A declined group whose sentinel is a class passes this check
// because some leaf in allSentinels wraps it.
func isSentinel(err error) bool {
	for _, sentinel := range allSentinels {
		if errors.Is(err, sentinel) || errors.Is(sentinel, err) {
			return true
		}
	}
	return false
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

	for name, sentinel := range allSentinels {
		require.True(t, covered[sentinel],
			"sentinel %s has no negative fixture or unsupported corpus entry", name)
	}
	for sentinel := range covered {
		require.True(t, canonical[sentinel], "file pins non-canonical sentinel %q", sentinel)
	}
}

// TestSentinelRegistryIsComplete checks the registry against the errors the package
// declares, which is the half TestSentinelReachability cannot do. That sweep is
// bidirectional between allSentinels and the files, so it is exact about everything
// except its own left-hand side: a sentinel missing from allSentinels is missing from
// both sides at once and the sweep closes over the smaller set, reporting a clean pass.
//
// That is not hypothetical. ErrEndpointFillerHasProperties was declared, returned by
// fillerLabels, and absent here, so nothing ever asked for a file pinning it — the
// behaviour's only cover was TestEndpointFillerRejectsProperties, a direct unit test
// the sweep does not count and whose deletion would have been silent. The doc above
// already said "a new sentinel must be added here"; this is that sentence with a
// mechanism behind it.
//
// errors.go is read as source rather than through reflection because a package's
// variables are not enumerable at run time, and the alternative — pinning how many
// there are — passes for any wrong twenty-one.
func TestSentinelRegistryIsComplete(t *testing.T) {
	declared := declaredSentinels(t)

	for _, name := range declared {
		_, pinned := allSentinels[name]
		_, excused := sentinelsWithoutAFile[name]
		require.True(t, pinned || excused,
			"errors.go declares %s, which is in neither allSentinels nor sentinelsWithoutAFile, so nothing asks for a file pinning it", name)
		require.False(t, pinned && excused,
			"%s is both pinned and excused from being pinned", name)
	}

	known := make(map[string]bool, len(declared))
	for _, name := range declared {
		known[name] = true
	}
	// The other direction is compiler-enforced for allSentinels, whose entries are the
	// values themselves, but not for sentinelsWithoutAFile, whose keys are strings — so
	// an excuse can outlive the error it excuses.
	for name := range sentinelsWithoutAFile {
		require.True(t, known[name], "sentinelsWithoutAFile excuses %s, which errors.go no longer declares", name)
	}
}

// declaredSentinels returns the names of the package-level error variables errors.go
// declares. Every sentinel lives there; one declared beside the code that returns it
// would be invisible here, which is why errors.go is named rather than the package
// walked — a second file to scan is a decision to take deliberately, not one to
// absorb silently.
func declaredSentinels(t *testing.T) []string {
	t.Helper()

	file, err := goparser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	require.NoError(t, err)

	var names []string
	for _, decl := range file.Decls {
		decl, ok := decl.(*ast.GenDecl)
		if !ok || decl.Tok != token.VAR {
			continue
		}
		for _, spec := range decl.Specs {
			values, ok := spec.(*ast.ValueSpec)
			require.True(t, ok, "a var declaration in errors.go holds a %T, which this scan does not read", spec)
			for _, ident := range values.Names {
				if strings.HasPrefix(ident.Name, "Err") {
					names = append(names, ident.Name)
				}
			}
		}
	}
	require.NotEmpty(t, names, "errors.go declares no sentinel, so the scan is broken and every check below is vacuous")
	return names
}

// TestRepeatedPropertyNameIsRejected pins ADR 0030: a property name declared
// twice in one propertyTypeList is rejected, not silently resolved to the last
// declaration. The rows exercise both collectors — node and edge fillers reach
// listener.properties through separate hand-written paths (nodeContent and
// edgeContent), so a guard added to one and not the other leaves the whole
// diagnostic missing on the other half with the corpus still green.
//
// The name is asserted as well as the sentinel: the entire point of the
// rejection is that an author who left a stale declaration behind is told
// WHICH property collided, and a bare sentinel would let the message drop the
// name with every errors.Is row still passing.
func TestRepeatedPropertyNameIsRejected(t *testing.T) {
	cases := map[string]struct {
		src  string
		name string
	}{
		"node type": {
			src: `CREATE PROPERTY GRAPH TYPE T AS {
				(:Person { id :: INT NOT NULL, id :: STRING })
			}`,
			name: "id",
		},
		"edge type": {
			src: `CREATE PROPERTY GRAPH TYPE T AS {
				(:Person),
				(:Company),
				(:Person)-[:WORKS_AT { since :: INT NOT NULL, since :: STRING }]->(:Company)
			}`,
			name: "since",
		},
		"repeat is not adjacent": {
			src: `CREATE PROPERTY GRAPH TYPE T AS {
				(:Person { id :: INT, name :: STRING, id :: STRING })
			}`,
			name: "id",
		},
	}

	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			got, err := gql.New().Parse(strings.NewReader(tc.src))
			require.ErrorIs(t, err, gql.ErrDuplicatePropertyName)
			require.Contains(t, err.Error(), tc.name,
				"the diagnostic must name the property that collided; that is what the rejection is for")
			require.Equal(t, schema.Schema{}, got, "model must be the zero value on error")
		})
	}
}

// TestDistinctPropertyNamesStillResolve is the ALLOW half of the guard above.
// Without it, a guard that rejected every property types specification with
// more than one entry — or every one at all — would pass every row of
// TestRepeatedPropertyNameIsRejected.
func TestDistinctPropertyNamesStillResolve(t *testing.T) {
	const src = `CREATE PROPERTY GRAPH TYPE T AS {
		(:Person { id :: INT NOT NULL, name :: STRING })
	}`

	got, err := gql.New().Parse(strings.NewReader(src))
	require.NoError(t, err)
	require.Len(t, got.Nodes, 1)
	for _, nt := range got.Nodes {
		require.Len(t, nt.Properties, 2)
	}
}
