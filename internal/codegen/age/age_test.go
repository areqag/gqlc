package age_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

const (
	// corpusRoot is the golden corpus. The suite reads its schemas rather
	// than inlining fixtures so the emission sweeps run over every input
	// the corpus actually carries.
	corpusRoot = "../../../test/data/codegen"
	// skeletonPackage is the name Schema.Name derivation produces for
	// valid/skeleton's `CREATE PROPERTY GRAPH TYPE Skeleton`.
	skeletonPackage = "skeleton"
	// ageTarget is this backend's registry key, and so the name of its
	// golden subtree under each enrolled fixture.
	ageTarget = "apache-age-pgx-v5"

	// personLabel is the one node type valid/skeleton declares. Phase Z
	// names its entity struct after it, so the label and the struct name
	// are the same text here.
	personLabel = "Person"

	// wantCanary is an independent copy of the operator-resolution probe
	// SessionInit runs. Verified against apache/age 1.7.0: with
	// ag_catalog off the search_path, `+` on two agtype operands has no
	// candidate operator whatever the literals are, so resolution fails.
	// Equality alone lacks that property — agtype casts implicitly to
	// boolean, so `=` falls back to pg_catalog's boolean equality and a
	// misconfigured connection can pass. This is the test's own copy of
	// the text, so an edit to the emission has to be an edit here too.
	wantCanary = `"SELECT '1'::ag_catalog.agtype + '1'::ag_catalog.agtype = '2'::ag_catalog.agtype"`

	// wantSearchPath is the other statement SessionInit runs, copied on
	// the same terms.
	wantSearchPath = `"SELECT set_config('search_path', concat_ws(', ', 'ag_catalog', nullif(current_setting('search_path'), '')), false)"`
)

// wantAborts is SessionInit's three failure exits, each with the branch
// that reaches it and the statement it guards. The hook exists so that a
// connection failing any of them is discarded instead of pooled, so an
// exit that returns nil hands out a connection whose AGE probe never
// succeeded and moves the failure to some later query with no context
// left to diagnose it.
//
// The text is pinned, not the shape: `if err != nil { return … }`
// matches `return nil` as readily as it matches the error, so a shape
// assertion passes on precisely the mutation that matters. These bytes
// come from Generate, not from the golden tree, so regenerating goldens
// cannot quiet them.
var wantAborts = []string{
	`	if _, err := conn.Exec(ctx, ` + wantSearchPath + `); err != nil {
		return fmt.Errorf("gqlc: put ag_catalog on the search_path: %w", err)
	}
`,
	`	err := conn.QueryRow(ctx, ` + wantCanary + `).Scan(&ok)
	if err != nil {
		return fmt.Errorf("gqlc: AGE operator canary: %w", err)
	}
	if !ok {
		return errors.New("gqlc: AGE operator canary returned false")
	}
	return nil
}
`,
}

// EmissionSuite pins this backend's C0 emission contract: the file set,
// the construction options, the batch-rejection path, and the properties
// the generated text must hold for AGE to work at all — every AGE
// identifier schema-qualified, the search_path canary ordered ahead of
// first use, the graph bound to the handle, and pgx named by major only.
// The fixture-driven golden corpus lives in internal/codegen/conformance.
type EmissionSuite struct {
	suite.Suite

	in    codegen.Input
	files map[string]string
}

func TestEmissionSuite(t *testing.T) {
	suite.Run(t, new(EmissionSuite))
}

func (s *EmissionSuite) SetupSuite() {
	s.in = s.inputFrom(filepath.Join(corpusRoot, "valid", "skeleton", "schema.gql"))
	files, err := age.New().Generate(s.in)
	s.Require().NoError(err)
	s.files = make(map[string]string, len(files))
	for _, f := range files {
		s.files[f.Path] = string(f.Contents)
	}
}

// inputFrom parses a corpus schema into a query-free batch.
func (s *EmissionSuite) inputFrom(path string) codegen.Input {
	src, err := os.ReadFile(path)
	s.Require().NoError(err)
	sch, err := gql.New().Parse(bytes.NewReader(src))
	s.Require().NoError(err, "schema %s", path)
	return codegen.Input{Schema: sch}
}

// inputFromText is inputFrom over a schema written in the test, for the
// sweeps whose rows differ by one property width apiece.
func (s *EmissionSuite) inputFromText(src string) codegen.Input {
	sch, err := gql.New().Parse(strings.NewReader(src))
	s.Require().NoError(err, "schema %s", src)
	return codegen.Input{Schema: sch}
}

// TestFileSet pins the C0 file set: the pgx handle, the graph lifecycle,
// the Querier interfaces, and the models file. The models file carries
// the schema's entity surface whether or not a query in the batch
// projects one — the entity table is the schema's, not the batch's, so
// the same schema emits the same structs for every backend.
func (s *EmissionSuite) TestFileSet() {
	paths := make([]string, 0, len(s.files))
	for p := range s.files {
		paths = append(paths, p)
	}
	s.Require().ElementsMatch([]string{"db.go", "graph.go", "models.go", "querier.go"}, paths)
	s.Require().Contains(s.files["models.go"], "type "+personLabel+" struct {")
	s.Require().Contains(s.files["models.go"], "func decode"+personLabel+"(raw []byte) ("+personLabel+", error) {")
}

// TestWithPackageName pins the CLI-1 §3.4 widening: a configured name
// replaces the Schema.Name derivation across every emitted file; the
// empty string keeps the derivation; a value outside the packageIdent
// grammar is codegen.ErrInvalidPackageName naming the configured string.
func (s *EmissionSuite) TestWithPackageName() {
	s.Run("configured name wins", func() {
		files, err := age.New(age.WithPackageName("configuredpkg")).Generate(s.in)
		s.Require().NoError(err)
		s.assertPackage(files, "configuredpkg")
	})
	s.Run("empty keeps derivation", func() {
		files, err := age.New(age.WithPackageName("")).Generate(s.in)
		s.Require().NoError(err)
		s.assertPackage(files, skeletonPackage)
	})
	s.Run("grammar violation names the configured string", func() {
		files, err := age.New(age.WithPackageName("Not_OK")).Generate(s.in)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, codegen.ErrInvalidPackageName)
		s.Require().ErrorContains(err, `configured package "Not_OK"`)
	})
}

// scalarColumn is a resolved column of the shape the read path decodes:
// a schema property of scalar width.
func scalarColumn(name string, pt graph.PropertyType) resolver.Column {
	return resolver.Column{Name: name, Type: resolver.ResolvedProperty{Type: pt}}
}

// readQuery is a batch entry this backend serves — a scalar read whose
// columns and parameters all land on the decode arms the read path
// emits. Every rejection case below is this shape with one axis moved.
func readQuery(name string, cols ...resolver.Column) codegen.NamedQuery {
	return codegen.NamedQuery{
		Name:        name,
		Cardinality: codegen.CardinalityMany,
		SourceFile:  "q.cypher",
		SourceText:  "MATCH (p:Person) RETURN p.name\n",
		Validated:   resolver.ValidatedQuery{Columns: cols},
	}
}

// execQuery is the other batch entry this backend serves — a write that
// projects nothing. Shared admission holds :exec and a column list
// mutually exclusive, so the zero-column shape is the only one an
// emission ever sees under this cardinality.
func execQuery(name string) codegen.NamedQuery {
	return codegen.NamedQuery{
		Name:        name,
		Cardinality: codegen.CardinalityExec,
		SourceFile:  "q.cypher",
		SourceText:  "MATCH (p:Person) DELETE p\n",
		Validated:   resolver.ValidatedQuery{Statement: resolver.StatementWrite},
	}
}

// servedQuery binds a parameter and projects one column of each agtype
// scalar width, so a batch holding it reaches every helper the emission
// gates on demand and every decode arm the read path can take.
var servedQuery = func() codegen.NamedQuery {
	q := readQuery("Names",
		scalarColumn("p.name", graph.TypeString),
		scalarColumn("p.age", graph.TypeInt),
		scalarColumn("p.height", graph.TypeFloat),
		scalarColumn("p.active", graph.TypeBool),
	)
	q.Validated.Parameters = []resolver.ResolvedParameter{
		{Name: "id", Type: resolver.ResolvedProperty{Type: graph.TypeInt}},
	}
	return q
}()

// instantParamQuery binds an instant in both nullabilities and projects
// nothing, so its emission is the parameter-encoding path alone. It
// carries its own source file: emission groups by source basename, and
// the corpus module compiles this file and no other query file.
var instantParamQuery = codegen.NamedQuery{
	Name:        "WriteEvent",
	Cardinality: codegen.CardinalityExec,
	SourceFile:  temporalSource,
	SourceText:  "CREATE (e:Event {occurredAt: $occurredAt, seenAt: $seenAt})\n",
	Validated: resolver.ValidatedQuery{
		Statement: resolver.StatementWrite,
		Parameters: []resolver.ResolvedParameter{
			{Name: "occurredAt", Type: resolver.ResolvedProperty{Type: graph.TypeTimestamp}},
			{Name: "seenAt", Type: resolver.ResolvedProperty{Type: graph.TypeTimestamp, Nullable: true}},
		},
	},
}

// carrierParamQuery binds the three widths that ride the neutral
// carriers, DATE in both nullabilities, and projects nothing. It shares
// instantParamQuery's source file so that both land in the one query file
// the corpus module compiles.
//
// It is a second query rather than four more parameters on that one
// because these encode fallibly and the instant does not: what it puts
// into the emission is the encoder-error path, which has no expression
// form inside the args map and so is emitted as statements ahead of it.
var carrierParamQuery = codegen.NamedQuery{
	Name:        "WriteSpan",
	Cardinality: codegen.CardinalityExec,
	SourceFile:  temporalSource,
	SourceText:  "CREATE (s:Span {startsOn: $startsOn, endsOn: $endsOn, opensAt: $opensAt, lasts: $lasts})\n",
	Validated: resolver.ValidatedQuery{
		Statement: resolver.StatementWrite,
		Parameters: []resolver.ResolvedParameter{
			{Name: "startsOn", Type: resolver.ResolvedProperty{Type: graph.TypeDate}},
			{Name: "endsOn", Type: resolver.ResolvedProperty{Type: graph.TypeDate, Nullable: true}},
			{Name: "opensAt", Type: resolver.ResolvedProperty{Type: graph.TypeLocalTime}},
			{Name: "lasts", Type: resolver.ResolvedProperty{Type: graph.TypeDuration}},
		},
	},
}

// zonedParamQuery binds the second zoned width in both nullabilities and
// projects nothing. It shares the source file above for the same reason
// carrierParamQuery does.
//
// TIME needs its own query rather than a parameter on either of theirs:
// it encodes fallibly, as the neutral carriers do and the instant does
// not, and it is the one parameter width whose encoder does day-wrapping
// arithmetic, which nothing on those two queries reaches.
var zonedParamQuery = codegen.NamedQuery{
	Name:        "WriteMeeting",
	Cardinality: codegen.CardinalityExec,
	SourceFile:  temporalSource,
	SourceText:  "CREATE (m:Meeting {startsAt: $startsAt, endsAt: $endsAt})\n",
	Validated: resolver.ValidatedQuery{
		Statement: resolver.StatementWrite,
		Parameters: []resolver.ResolvedParameter{
			{Name: "startsAt", Type: resolver.ResolvedProperty{Type: graph.TypeTime}},
			{Name: "endsAt", Type: resolver.ResolvedProperty{Type: graph.TypeTime, Nullable: true}},
		},
	},
}

// listCarrierParamQuery binds a list of each carrier the three fallible
// encoders serve, plus a nullable list, and projects nothing. It shares
// instantParamQuery's source file for the same reason carrierParamQuery
// does.
//
// It is a third query rather than four more parameters on that one
// because a list is where the encoders COMPOSE: fallibleParamEncoder
// wraps agtypeEncodedList around the leaf encoder, and agtypeEncodedNullable
// around that again for a nullable list, so these four parameters are the
// only shapes that reach either combinator. Without them the emission
// never sets encList, agtypeEncodedList is never written into the module
// under test, and nothing in the tree compiles or runs it (bd gqlc-t0dp).
var listCarrierParamQuery = codegen.NamedQuery{
	Name:        "WriteSpans",
	Cardinality: codegen.CardinalityExec,
	SourceFile:  temporalSource,
	SourceText:  "CREATE (s:Spans {startsOn: $startsOn, opensAt: $opensAt, lasts: $lasts, mayLast: $mayLast})\n",
	Validated: resolver.ValidatedQuery{
		Statement: resolver.StatementWrite,
		Parameters: []resolver.ResolvedParameter{
			{Name: "startsOn", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.TypeDate, true)}},
			{Name: "opensAt", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.TypeLocalTime, true)}},
			{Name: "lasts", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.TypeDuration, true)}},
			{Name: "mayLast", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.TypeDuration, true), Nullable: true}},
		},
	},
}

// nestedListCarrierParamQuery binds DEPTH-2 lists of non-zoned carriers,
// nullable and not, which is the shape ADR 0036 admits and the encoder does
// not yet compose for.
//
// It is a separate query from listCarrierParamQuery because depth 2 does not
// fail more than depth 1, it fails DIFFERENTLY. At depth 1 the single
// strings.CutPrefix in fallibleParamEncoder leaves a leaf the switch matches,
// so the parameter is encoded. At depth 2 it leaves "[]civil.Date", which
// matches no arm, and the resulting ("", false) is read by the call site as
// "this parameter crosses raw" — plain json.Marshal over carriers that define
// no MarshalJSON. So the defect is not a wrong encoding but NO encoding, and
// no query at depth 1 can reach it (bd gqlc-vhvz7, bug gqlc-jc8mc).
//
// Two carriers rather than one: Date encodes to a string and Duration to an
// int64, so a composition that hard-coded either side of encodedParamText
// would pass on the other.
var nestedListCarrierParamQuery = codegen.NamedQuery{
	Name:        "WriteSchedules",
	Cardinality: codegen.CardinalityExec,
	SourceFile:  temporalSource,
	SourceText:  "CREATE (s:Schedules {runsOn: $runsOn, mayRunOn: $mayRunOn, windows: $windows})\n",
	Validated: resolver.ValidatedQuery{
		Statement: resolver.StatementWrite,
		Parameters: []resolver.ResolvedParameter{
			{Name: "runsOn", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.ListOf(graph.TypeDate, true), true)}},
			{Name: "mayRunOn", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.ListOf(graph.TypeDate, true), true), Nullable: true}},
			{Name: "windows", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.ListOf(graph.TypeDuration, true), true)}},
		},
	},
}

// corpusEdgeKey is the one edge type testdata/corpus_schema.gql declares.
var corpusEdgeKey = schema.EdgeKey{Source: personLabel, KeyLabels: "ACTED_IN", Target: personLabel}

// edgeUnionOn is the resolved shape of an edge binding the resolver
// could not narrow to one candidate: one candidate per label, between
// one pair of endpoints. Its candidates are not corpus edge types: the
// gate under test runs ahead of Prepare, so what it refuses it refuses
// without consulting the schema.
func edgeUnionOn(labels ...graph.LabelSetKey) resolver.ResolvedEdgeUnion {
	keys := make([]schema.EdgeKey, len(labels))
	for i, l := range labels {
		keys[i] = schema.EdgeKey{Source: personLabel, KeyLabels: l, Target: "Post"}
	}
	return resolver.ResolvedEdgeUnion{EdgeKeys: keys}
}

var twoCandidateEdgeUnion = edgeUnionOn("AUTHORED", "LIKES")

// sharedLabelSchema declares the edge types sharedLabelEdgeUnion and
// mixedLabelEdgeUnion name. A union carrying a duplicate label is
// refused inside Prepare, which resolves candidates against the entity
// index, so unlike edgeUnionOn's shapes these have to be ones the schema
// actually declares.
const sharedLabelSchema = "shared_label_schema.gql"

// sharedLabelEdgeUnion is the other resolved edge-union shape: two
// candidates carrying ONE label. The pattern that produces it names a
// single relationship type whose source endpoint the schema satisfies
// more than one way (ADR 0022) — `(p)-[r:FOUNDED]->(c:Company)`, with no
// '|' anywhere. Apache AGE parses that statement, so it is not this
// backend's to refuse.
var sharedLabelEdgeUnion = resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{
	{Source: personLabel, KeyLabels: "FOUNDED", Target: "Company"},
	{Source: "Investor", KeyLabels: "FOUNDED", Target: "Company"},
}}

// mixedLabelEdgeUnion carries both shapes at once: three candidates
// under two labels, one of which repeats. `(p)-[r:FOUNDED|BACKED]->(c)`
// over a source endpoint the schema satisfies twice reaches it. The
// column gate stands aside on the repeated label, because that is an
// obstacle no server-side parser is party to and no rewrite of the
// alternation removes — which is what lets the portable refusal answer.
// The rows below carry readQuery's own text, which spells no '|', so
// what they measure is the column gate alone; a real batch whose text
// did spell one is answered a step later by rejectDialectGaps.
var mixedLabelEdgeUnion = resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{
	{Source: personLabel, KeyLabels: "FOUNDED", Target: "Company"},
	{Source: personLabel, KeyLabels: "BACKED", Target: "Company"},
	{Source: "Investor", KeyLabels: "FOUNDED", Target: "Company"},
}}

// wantEdgeUnionReason is this test's own copy of the reason the gate
// gives for an edge-union column, so a change to the emission's wording
// has to be a change here too. names is the prose label list, written
// out per case rather than derived, so a gate that stopped reading the
// column's own candidates cannot satisfy two cases at once.
//
// It claims nothing about the author's query text. The candidates are
// the relationship types the SCHEMA declares for the pattern, and a
// pattern may name types it does not declare — the resolver drops those
// (internal/resolver, edgeCandidates), so the label list is a subset of
// what the author wrote and quoting it back as their alternation was the
// defect that put this here.
func wantEdgeUnionReason(names string) string {
	return "binds more than one relationship type — " + names + ", the candidates the schema " +
		"declares for its pattern — which openCypher spells only as an alternation, and Apache " +
		`AGE 1.7.0's parser has no "|" in a relationship pattern: it answers one with ` +
		`"syntax error at or near \"|\"" (SQLSTATE 42601)`
}

// TestRejectsEdgeUnionColumns pins the narrowest of this backend's
// refusals against the column it is one step away from: an edge column
// the resolver narrowed to a single candidate is served, and the same
// column with a second candidate carrying a second label is not.
//
// The refusal is not about decoding. The candidates carry distinct
// labels, so a dispatch on the label would pick correctly — and where
// they do not, this gate stands aside for the shared refusal that
// answers a repeated label on every backend (the last subtests below;
// this gate runs ahead of Prepare, so standing aside is what lets that
// refusal be reached at all). It is about the query
// text: candidates carrying distinct labels can only have come from a
// pattern naming more than one relationship type, and openCypher writes
// that only as an alternation (Cypher.g4 oC_RelationshipTypes admits a
// second type after '|' and nowhere else; a re-bound relationship
// variable's occurrences intersect rather than accumulate). Apache AGE
// 1.7.0 answers `-[r:AUTHORED|LIKES]->` with `ERROR: syntax error at or
// near "|"`. Generated code runs the author's query text verbatim
// (ADR 0005), so every call on such a method would fail at the server.
// Emitting it would hand the author a package that compiles and cannot
// run.
//
// The refused rows carry label sets that differ from each other, because
// the message is built from the column's own candidates and a message
// built from a fixed list would satisfy one row while failing the rest.
// The three-label row is also the only one whose prose list has a serial
// comma in it.
//
// The last three subtests are the boundary in the other direction: a
// union the resolver never emits, and one carrying a duplicate label,
// are both refused for reasons no parser is party to, so this gate
// stands aside and lets the shared, backend-independent refusal answer.
func (s *EmissionSuite) TestRejectsEdgeUnionColumns() {
	in := s.inputFrom(filepath.Join("testdata", corpusSchema))

	s.Run("one candidate is served", func() {
		batch := in
		batch.Queries = []codegen.NamedQuery{readQuery("OneAction", resolver.Column{
			Name: "r", Type: resolver.ResolvedEdge{EdgeKey: corpusEdgeKey},
		})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().NoError(err)
		s.Require().NotEmpty(files)
	})

	for _, tc := range []struct {
		name  string
		union resolver.ResolvedEdgeUnion
		// names is the prose label list the refusal must carry.
		names string
	}{
		{
			name:  "two candidates are refused",
			union: twoCandidateEdgeUnion,
			names: "AUTHORED and LIKES",
		},
		{
			name:  "two candidates under other labels name those",
			union: edgeUnionOn("FOUNDED", "BACKED"),
			names: "FOUNDED and BACKED",
		},
		{
			name:  "three candidates are all named",
			union: edgeUnionOn("AUTHORED", "LIKES", "REPOSTED"),
			names: "AUTHORED, LIKES and REPOSTED",
		},
		{
			// Four is where a formatter correct only to three comes
			// apart: a list joined as "A, B and C and D" satisfies every
			// shorter row. edgeCandidates iterates all of e.Labels(), so
			// the pattern that produces this is `-[r:A|B|C|D]->`.
			name:  "four candidates are all named, with one 'and'",
			union: edgeUnionOn("AUTHORED", "LIKES", "REPOSTED", "FLAGGED"),
			names: "AUTHORED, LIKES, REPOSTED and FLAGGED",
		},
	} {
		s.Run(tc.name, func() {
			batch := in
			batch.Queries = []codegen.NamedQuery{readQuery("TwoActions", resolver.Column{
				Name: "r", Type: tc.union,
			})}
			files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
			s.Require().Error(err)
			s.Require().Nil(files, "a rejected batch must not return a partial file set")
			s.Require().ErrorIs(err, age.ErrUnsupportedQuery)
			s.Require().ErrorContains(err, `TwoActions (column "r" `+wantEdgeUnionReason(tc.names)+`)`)
		})
	}

	s.Run("a single candidate is not this backend's refusal either", func() {
		batch := in
		batch.Queries = []codegen.NamedQuery{readQuery("OneCandidate", resolver.Column{
			Name: "r", Type: resolver.ResolvedEdgeUnion{EdgeKeys: []schema.EdgeKey{corpusEdgeKey}},
		})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, codegen.ErrOutOfC6Scope,
			"a union the resolver never emits is a broken invariant, which shared admission names and this gate cannot")
		s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().ErrorContains(err, "resolved as edgeUnion with only 1 candidate(s)")
	})

	s.Run("candidates sharing a label are not this backend's refusal", func() {
		batch := s.inputFrom(filepath.Join("testdata", sharedLabelSchema))
		batch.Queries = []codegen.NamedQuery{readQuery("Founded", resolver.Column{
			Name: "r", Type: sharedLabelEdgeUnion,
		})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, codegen.ErrUnrepresentableEdgeUnion,
			"a shared-label union is unrepresentable on every backend; this one must not overwrite that answer")
		s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().NotContains(err.Error(), "alternation",
			"the pattern that produces a shared-label union names one relationship type and spells no '|'")
		s.Require().NotContains(err.Error(), "which the Apache AGE backend has no carrier for",
			"standing aside means letting the portable answer through unaltered: this backend's name is "+
				"added to a width or a temporal kind, which are its own type table's answers, and a "+
				"repeated label is not — attributing it here would send the author to change targets")
	})

	s.Run("a duplicate label stands the gate aside even among distinct ones", func() {
		batch := s.inputFrom(filepath.Join("testdata", sharedLabelSchema))
		batch.Queries = []codegen.NamedQuery{readQuery("FoundedOrBacked", resolver.Column{
			Name: "r", Type: mixedLabelEdgeUnion,
		})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, codegen.ErrUnrepresentableEdgeUnion,
			"a repeated label defeats the dispatch on every backend, and rewriting the alternation does not remove it")
		s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().ErrorContains(err, `both carry edge label "FOUNDED"`)
	})
}

// wantAlternationRefusal is this test's own copy of the text-level
// refusal, so a change to the emission's wording has to be a change
// here too. dropped is the "<Name> (<alternations>)" list, written out
// per case rather than derived.
func wantAlternationRefusal(count int, noun, dropped string) string {
	return fmt.Sprintf("relationship type alternation: generated code runs the author's query text "+
		"verbatim (ADR 0005) and Apache AGE 1.7.0's parser has no \"|\" in a relationship pattern, "+
		"so every call on %d %s would answer \"syntax error at or near \\\"|\\\"\" (SQLSTATE 42601) "+
		"— write each relationship type as its own query; each alternation is located line:column "+
		"within its own query's text: %s", count, noun, dropped)
}

// textQuery is a batch entry whose columns and parameters this backend
// serves and whose TEXT carries whatever construct the case is about. It
// is the readQuery shape with one axis moved, and the axis is the one the
// column gate above cannot see.
func textQuery(name, sourceText string, cols ...resolver.Column) codegen.NamedQuery {
	q := readQuery(name, cols...)
	q.SourceText = sourceText
	return q
}

// execTextQuery is the same axis moved on the OTHER served batch entry:
// execQuery's write, with the construct in its text. Shared admission
// holds :exec and a column list mutually exclusive, so what the column
// gate reads here is not merely clean but empty, and a gate keyed on the
// columns or on the statement kind has nothing to look at. The server
// parses the statement all the same.
func execTextQuery(name, sourceText string) codegen.NamedQuery {
	q := execQuery(name)
	q.SourceText = sourceText
	return q
}

// TestRejectsRelationshipTypeAlternation pins the gate that reads the
// query TEXT rather than the resolved columns.
//
// The two are not the same set, and the column gate above is blind to
// most of this one's. Apache AGE 1.7.0's parser refuses '|' in a
// relationship detail whatever surrounds it, and generated code ships
// the author's text verbatim (ADR 0005) — so an alternation the author
// never projects produces no column at all, and one the resolver
// narrowed to a single declared candidate produces a ResolvedEdge the
// column gate serves. Both would emit a package that compiles and whose
// every call is a server-side syntax error.
//
// The refused rows carry different alternations from each other, because
// the message quotes the column's own text and a message built from a
// fixed string would satisfy one row while failing the rest.
//
// One row carries no columns at all. A :exec write projects nothing, so
// it is not that the column list holds nothing this gate would refuse —
// there is no column list. A gate narrowed to queries that project, or
// to reads, keeps every other row here green.
func (s *EmissionSuite) TestRejectsRelationshipTypeAlternation() {
	in := s.inputFrom(filepath.Join("testdata", corpusSchema))

	for _, tc := range []struct {
		name    string
		queries []codegen.NamedQuery
		// count and noun are the refusal's arithmetic; dropped is the
		// per-query list it ends with.
		count   int
		noun    string
		dropped string
	}{
		{
			name: "an alternation the query never projects is refused",
			queries: []codegen.NamedQuery{textQuery("PostIDs",
				"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `PostIDs (":AUTHORED|LIKES" at 1:19)`,
		},
		{
			name: "an alternation narrowed to one declared candidate is refused",
			queries: []codegen.NamedQuery{textQuery("Rels",
				"MATCH (:Person)-[r:AUTHORED|FLAGGED]->(p:Post) RETURN r\n",
				resolver.Column{Name: "r", Type: resolver.ResolvedEdge{EdgeKey: corpusEdgeKey}})},
			count: 1, noun: "query", dropped: `Rels (":AUTHORED|FLAGGED" at 1:19)`,
		},
		{
			name: "an alternation bound by an anonymous edge is refused",
			queries: []codegen.NamedQuery{textQuery("Anon",
				"MATCH (:Person)-[:AUTHORED|LIKES|REPOSTED]->(p:Post) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Anon (":AUTHORED|LIKES|REPOSTED" at 1:18)`,
		},
		{
			// The arity witness, read as the grammar spells it rather
			// than as a set of names. Cypher.g4 §oC_RelationshipTypes
			// admits a repeated name after '|', and AGE 1.7.0 refuses
			// the '|' the repeat needs — while the resolver narrows the
			// candidates to one ResolvedEdge, which the column gate
			// serves. So a gate counting DISTINCT types refuses nothing
			// here and emits a package whose every call is 42601.
			name: "a type the alternation repeats is refused",
			queries: []codegen.NamedQuery{textQuery("Repeat",
				"MATCH (:Person)-[r:LIKES|LIKES]->(p:Post) RETURN r\n",
				resolver.Column{Name: "r", Type: resolver.ResolvedEdge{EdgeKey: corpusEdgeKey}})},
			count: 1, noun: "query", dropped: `Repeat (":LIKES|LIKES" at 1:19)`,
		},
		{
			name: "a type the alternation repeats in the legacy spelling is refused",
			queries: []codegen.NamedQuery{textQuery("RepeatLegacy",
				"MATCH (:Person)-[r:LIKES|:LIKES]->(p:Post) RETURN r\n",
				resolver.Column{Name: "r", Type: resolver.ResolvedEdge{EdgeKey: corpusEdgeKey}})},
			count: 1, noun: "query", dropped: `RepeatLegacy (":LIKES|:LIKES" at 1:19)`,
		},
		{
			// Whitespace inside the alternation is quoted, not dropped:
			// SP is a default-channel token, so the parser's text keeps
			// it and the message prints only characters the author
			// wrote. Nothing else pins the spaced rendering.
			name: "an alternation written with spaces is quoted with them",
			queries: []codegen.NamedQuery{textQuery("Spaced",
				"MATCH (:Person)-[r:AUTHORED | LIKES]->(p:Post) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Spaced (":AUTHORED | LIKES" at 1:19)`,
		},
		{
			name: "a query spelling two alternations names both",
			queries: []codegen.NamedQuery{textQuery("Both",
				"MATCH (:Person)-[r:LIKES|REPOSTED]->(p:Post), (:Person)-[s:AUTHORED|FLAGGED]->(p) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Both (":LIKES|REPOSTED" at 1:19, ":AUTHORED|FLAGGED" at 1:59)`,
		},
		{
			// Two occurrences of ONE spelling, which is the case the
			// quoted text alone cannot answer: the author has two
			// patterns to rewrite and the message can only say so by
			// naming where each is. Nothing but the position
			// distinguishes these two entries, so a scan that dropped
			// the repeat, or one that carried a position it did not
			// vary, reads as a query with a single alternation in it
			// (bd gqlc-rmzg).
			name: "the same alternation written twice is named twice, at different positions",
			queries: []codegen.NamedQuery{textQuery("Twice",
				"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post), (:Person)-[s:AUTHORED|LIKES]->(p) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query",
			dropped: `Twice (":AUTHORED|LIKES" at 1:19, ":AUTHORED|LIKES" at 1:59)`,
		},
		{
			// Two occurrences of one spelling on DIFFERENT lines, where
			// the line alone separates them. Beside the row above, which
			// varies only the column, this is what keeps both halves of
			// the coordinate load-bearing: a position carrying the line
			// and a constant column passes one of these rows and not the
			// other, and so does one carrying the column alone.
			name: "the same alternation on two lines is separated by the line",
			queries: []codegen.NamedQuery{textQuery("TwiceDown",
				"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post)\nMATCH (:Person)-[s:AUTHORED|LIKES]->(p)\nRETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query",
			dropped: `TwiceDown (":AUTHORED|LIKES" at 1:19, ":AUTHORED|LIKES" at 2:19)`,
		},
		{
			// The zero-column row. A :exec write is a query the AGE
			// backend serves and whose whole text still reaches the
			// server, so the alternation in it is refused on the text
			// alone — there is no column here for any reading of the
			// columns to arrive at.
			name: "an alternation in a write that projects nothing is refused",
			queries: []codegen.NamedQuery{execTextQuery("DropActions",
				"MATCH (p:Person)-[r:AUTHORED|LIKES]->(:Post) DELETE r\n")},
			count: 1, noun: "query", dropped: `DropActions (":AUTHORED|LIKES" at 1:20)`,
		},
		{
			name: "every offending query in the batch is named",
			queries: []codegen.NamedQuery{
				servedQuery,
				textQuery("PostIDs",
					"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN p.id\n",
					scalarColumn("p.id", graph.TypeInt)),
				textQuery("Anon",
					"MATCH (:Person)-[:AUTHORED|REPOSTED]->(p:Post) RETURN p.id\n",
					scalarColumn("p.id", graph.TypeInt)),
			},
			count: 2, noun: "queries",
			dropped: `PostIDs (":AUTHORED|LIKES" at 1:19), Anon (":AUTHORED|REPOSTED" at 1:18)`,
		},
	} {
		s.Run(tc.name, func() {
			batch := in
			batch.Queries = tc.queries
			files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
			s.Require().Error(err)
			s.Require().Nil(files, "a rejected batch must not return a partial file set")
			s.Require().ErrorIs(err, age.ErrRelationshipTypeAlternation)
			s.Require().NotErrorIs(err, age.ErrUnsupportedQuery,
				"the two refusals have different reasons and a caller must be able to tell them apart")
			s.Require().EqualError(err, wantAlternationRefusal(tc.count, tc.noun, tc.dropped))
		})
	}

	s.Run("a '|' outside a relationship pattern is served", func() {
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Tags",
			"MATCH (p:Person) RETURN [x IN p.tags | x] AS t\n",
			scalarColumn("t", graph.TypeString))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().NoError(err, "a list comprehension's '|' names no relationship type")
		s.Require().NotEmpty(files)
	})

	s.Run("a column shared admission refuses is answered here, because this runs first", func() {
		// The gate's POSITION, pinned by its observable consequence. This
		// batch trips two refusals at once: its text spells an
		// alternation, and its column carries a repeated label, which
		// codegen.Prepare refuses with ErrUnrepresentableEdgeUnion (the
		// column gate above stands aside on exactly that repeat, which is
		// what lets Prepare be reached at all). Running this gate ahead of
		// Prepare is what makes the alternation the answer, and the order
		// is not a preference: the text has to be rewritten before the
		// column question can be asked of this server at all, so the
		// portable answer would send the author to change a projection
		// that is not yet the obstacle.
		//
		// This is also the whole justification for
		// test/data/codegen/invalid/unrepresentable_edge_union_shared_label
		// carrying no apache-age-pgx-v5 target: a manifest names one
		// expectedError per target, and on this ordering AGE's is not the
		// one the manifest names. Move the gate below Prepare and that
		// enrolment becomes valid again, so the un-enrolment needs the
		// ordering asserted rather than assumed.
		batch := s.inputFrom(filepath.Join("testdata", sharedLabelSchema))
		batch.Queries = []codegen.NamedQuery{textQuery("FoundedOrBacked",
			"MATCH (:Person)-[r:FOUNDED|BACKED]->(c:Company) RETURN r\n",
			resolver.Column{Name: "r", Type: mixedLabelEdgeUnion})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrRelationshipTypeAlternation,
			"this gate runs ahead of Prepare, so a batch tripping both gets the text's answer")
		s.Require().NotErrorIs(err, codegen.ErrUnrepresentableEdgeUnion,
			"shared admission would refuse this column, and reaching it first would send the author to fix a projection the server never gets far enough to read")
		s.Require().EqualError(err,
			wantAlternationRefusal(1, "query", `FoundedOrBacked (":FOUNDED|BACKED" at 1:19)`))
	})

	s.Run("an edge-union column is answered by the column gate, which says more", func() {
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("TwoActions",
			"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN r\n",
			resolver.Column{Name: "r", Type: twoCandidateEdgeUnion})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUnsupportedQuery,
			"the column gate runs first, because naming the candidates the schema declares is more than the text can say")
		s.Require().NotErrorIs(err, age.ErrRelationshipTypeAlternation)
		s.Require().ErrorContains(err, `TwoActions (column "r" `+wantEdgeUnionReason("AUTHORED and LIKES")+`)`)
	})

	s.Run("an unserved column that is not an edge union yields to the text", func() {
		// The other side of the subtest above, and the bound on it. An
		// edge-union column outranks the text because it answers the SAME
		// defect and names the candidates the schema declares. A map
		// column is a DIFFERENT defect: printing it first sends the
		// author to change a projection, regenerate, and only then learn
		// the statement never parsed on this backend — two rounds for one
		// query. So the column gate yields here.
		mapColumn := resolver.Column{
			Name: "bag", Type: resolver.ResolvedScalar{Kind: resolver.ScalarMap},
		}

		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Bagged",
			"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN properties(p) AS bag\n", mapColumn)}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrRelationshipTypeAlternation,
			"the alternation is the obstacle underneath: the statement has to be rewritten before the projection can be put to this server at all")
		s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().EqualError(err, wantAlternationRefusal(1, "query", `Bagged (":AUTHORED|LIKES" at 1:19)`))

		// The discriminator. Without the '|' the very same column is
		// still unserved and the column gate still owns it, so what
		// moved above is the ordering and not the map arm.
		batch.Queries = []codegen.NamedQuery{textQuery("Bagged",
			"MATCH (:Person)-[r:AUTHORED]->(p:Post) RETURN properties(p) AS bag\n", mapColumn)}
		files, err = age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().NotErrorIs(err, age.ErrRelationshipTypeAlternation)
		s.Require().ErrorContains(err, `Bagged (column "bag" projects scalar(map))`)
	})

	s.Run("an unserved parameter yields to the text", func() {
		// The same yield on the OTHER axis unservedReason reads. It
		// answers the columns first and the parameters after, on a
		// separate return, so a query whose every column is served
		// reaches a different decision from the row above and one arm
		// cannot stand in for the other. A width with no carrier is the
		// map column's defect one step along: still an argument to
		// change, still not the reason the statement never parsed.
		//
		// BYTES is the discriminator because it is unserved on BOTH the
		// column and the parameter axis, which is what these precedence
		// subtests need. That is a dependency on the served set, not on
		// BYTES itself: these subtests were written against LIST and
		// stopped discriminating the day LIST became served. If BYTES is
		// served too, pick another width unserved on both axes — do not
		// delete the subtest, because the precedence it pins is the whole
		// point of the gate above it.
		wideParam := resolver.ResolvedParameter{
			Name: "payload", Type: resolver.ResolvedProperty{Type: graph.TypeBytes},
		}
		byBlob := func(sourceText string) codegen.NamedQuery {
			q := textQuery("ByBlob", sourceText, scalarColumn("p.id", graph.TypeInt))
			q.Validated.Parameters = []resolver.ResolvedParameter{wideParam}
			return q
		}

		batch := in
		batch.Queries = []codegen.NamedQuery{byBlob(
			"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) WHERE p.payload = $payload RETURN p.id\n")}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrRelationshipTypeAlternation,
			"the alternation is the obstacle underneath: the statement has to be rewritten before the argument can be put to this server at all")
		s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().EqualError(err, wantAlternationRefusal(1, "query", `ByBlob (":AUTHORED|LIKES" at 1:19)`))

		// The discriminator. Without the '|' the very same parameter is
		// still unserved and the column gate still owns it, so what
		// moved above is the ordering and not the parameter arm.
		batch.Queries = []codegen.NamedQuery{byBlob(
			"MATCH (:Person)-[r:AUTHORED]->(p:Post) WHERE p.payload = $payload RETURN p.id\n")}
		files, err = age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().NotErrorIs(err, age.ErrRelationshipTypeAlternation)
		s.Require().ErrorContains(err, `ByBlob (parameter $payload is property:BYTES)`)
	})

	s.Run("an edge-union column outranks an unserved parameter", func() {
		// The precedence BETWEEN the two axes unservedReason reads, which
		// is what its loop order decides and nothing else pins. The chain
		// is: an edge-union column, then the text, then every other
		// unserved reason. The column is first because it answers the
		// SAME defect the text does and names the candidates the schema
		// declares for the pattern; a parameter answers a different one
		// and yields (the subtest above). So a query carrying both must
		// report the column.
		//
		// Reading the parameters first inverts that. The parameter arm
		// reports edgeUnion=false, so the query yields to the text and
		// the author is handed the alternation quoted back where a list
		// of candidates was available — strictly less about the very same
		// fix, which is the one trade the exception to the yield exists
		// to avoid making.
		wideParam := resolver.ResolvedParameter{
			Name: "payload", Type: resolver.ResolvedProperty{Type: graph.TypeBytes},
		}
		bothAxes := func(sourceText string) codegen.NamedQuery {
			q := textQuery("ActionsByBlob", sourceText,
				resolver.Column{Name: "r", Type: twoCandidateEdgeUnion})
			q.Validated.Parameters = []resolver.ResolvedParameter{wideParam}
			return q
		}
		wantColumn := `ActionsByBlob (column "r" ` + wantEdgeUnionReason("AUTHORED and LIKES") + `)`

		batch := in
		batch.Queries = []codegen.NamedQuery{bothAxes(
			"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) WHERE p.payload = $payload RETURN r\n")}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUnsupportedQuery,
			"the edge-union column outranks both the parameter and the text, so neither can take the answer from it")
		s.Require().NotErrorIs(err, age.ErrRelationshipTypeAlternation)
		s.Require().ErrorContains(err, wantColumn)
		s.Require().NotContains(err.Error(), "parameter $payload")

		// The same precedence with the text out of it. Both axes are
		// still unserved and neither can yield anywhere, so what decides
		// the message is the loop order alone.
		batch.Queries = []codegen.NamedQuery{bothAxes(
			"MATCH (:Person)-[r:AUTHORED]->(p:Post) WHERE p.payload = $payload RETURN r\n")}
		files, err = age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().ErrorContains(err, wantColumn)
		s.Require().NotContains(err.Error(), "parameter $payload",
			"the column is read first, so the parameter is not what this query is told about")
	})

	s.Run("a sibling the column gate still owns is named in the same run", func() {
		// Yielding is per query. A batch holding one query the text gate
		// owns and one the column gate owns must not lose the second: a
		// gate that stood the whole BATCH aside would report only the
		// alternation and hand the author the uncarried width a round
		// later, which is the round-trip this ordering exists to remove.
		batch := in
		batch.Queries = []codegen.NamedQuery{
			textQuery("Blobbed",
				"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) RETURN p.payload AS payload\n",
				resolver.Column{
					Name: "payload",
					Type: resolver.ResolvedProperty{Type: graph.TypeBytes},
				}),
			readQuery("Bag", resolver.Column{
				Name: "m", Type: resolver.ResolvedScalar{Kind: resolver.ScalarMap},
			}),
		}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUnsupportedQuery,
			"the sibling spells no '|', so the column gate still answers it and answers first")
		s.Require().ErrorContains(err, `1 query would be dropped: Bag (column "m" projects scalar(map))`)
		s.Require().NotContains(err.Error(), "Blobbed",
			"the query the text gate owns must not be reported on the column axis")
	})
}

// wantUndefinedFunctionRefusal is this test's own copy of the second
// text-level refusal, on the same terms as wantAlternationRefusal: a
// change to the emission's wording has to be a change here too.
func wantUndefinedFunctionRefusal(count int, noun, dropped string) string {
	return fmt.Sprintf("undefined function: generated code runs the author's query text verbatim "+
		"(ADR 0005) and Apache AGE 1.7.0 defines no temporal constructor this project has "+
		"measured, so every call on %d %s would answer \"function <name> does not exist\" — "+
		"timestamp() is the one that answered, returning epoch milliseconds as an integer, so "+
		"compute the value in Go and bind it as a parameter, or generate against a neo4j "+
		"target; each call is located line:column within its own query's text: %s",
		count, noun, dropped)
}

// TestRejectsUndefinedFunctions pins the second gap the text gate reads,
// and it is the one whose entries have to be earned. AGE 1.7.0 answers
// `RETURN datetime()` with `ERROR: function datetime does not exist`
// (gqlc-35yu.5, measured against the image test/data/codegen pins), so a
// call on one of those names is a package that compiles and whose every
// call fails on the server — the alternation's failure mode with a
// different production behind it.
//
// The refused set is DERIVED from the probe texts a live session was
// measured on (internal/codegen/age/dialect.go), so what this test
// really pins is that the derivation reaches the gate. The rows that
// matter most are the ones asserting a name is NOT refused: a refusal
// list that grows on suspicion, or on a measurement made for a
// different gap, is a guess wearing a test suite.
func (s *EmissionSuite) TestRejectsUndefinedFunctions() {
	in := s.inputFrom(filepath.Join("testdata", corpusSchema))

	for _, tc := range []struct {
		name    string
		queries []codegen.NamedQuery
		count   int
		noun    string
		dropped string
	}{
		{
			// The load-bearing shape. The call is in a predicate, which
			// the query model drops (ADR 0003), so no column, parameter
			// or binding carries it and only the text does.
			name: "a call in a predicate the model drops is refused",
			queries: []codegen.NamedQuery{textQuery("Recent",
				"MATCH (p:Person) WHERE p.at < datetime() RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Recent ("datetime" at 1:31)`,
		},
		{
			name: "a call in a write that projects nothing is refused",
			queries: []codegen.NamedQuery{execTextQuery("Touch",
				"MATCH (p:Person) SET p.seen = datetime()\n")},
			count: 1, noun: "query", dropped: `Touch ("datetime" at 1:31)`,
		},
		{
			// The name is quoted as the author spelled it, because the
			// refusal quotes it back and every character it prints has
			// to be one the author typed. The catalogue is matched
			// case-insensitively, which is what openCypher function
			// resolution is.
			name: "the name is quoted in the author's own case",
			queries: []codegen.NamedQuery{textQuery("Recent",
				"MATCH (p:Person) WHERE p.at < DateTime() RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Recent ("DateTime" at 1:31)`,
		},
		{
			name: "a query calling two undefined functions names both",
			queries: []codegen.NamedQuery{textQuery("Window",
				"MATCH (p:Person) WHERE p.at > date() AND p.d < duration({days: 1}) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Window ("date" at 1:31, "duration" at 1:48)`,
		},
		{
			name: "every offending query in the batch is named",
			queries: []codegen.NamedQuery{
				servedQuery,
				textQuery("Recent",
					"MATCH (p:Person) WHERE p.at < datetime() RETURN p.id\n",
					scalarColumn("p.id", graph.TypeInt)),
				execTextQuery("Touch",
					"MATCH (p:Person) SET p.seen = localdatetime()\n"),
			},
			count: 2, noun: "queries",
			dropped: `Recent ("datetime" at 1:31), Touch ("localdatetime" at 1:31)`,
		},
		{
			// Two calls on ONE name, which is the case the quoted name
			// alone cannot answer: the author has two calls to rewrite
			// and the message can only say so by naming where each is.
			// Nothing but the position distinguishes these two entries,
			// so a scan dropping the repeat, or one carrying a position
			// it did not vary, reads as a query calling the function
			// once (bd gqlc-fpl14).
			name: "the same call written twice is named twice, at different positions",
			queries: []codegen.NamedQuery{textQuery("Twice",
				"MATCH (p:Person) WHERE p.at < datetime() AND p.until > datetime() RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query",
			dropped: `Twice ("datetime" at 1:31, "datetime" at 1:56)`,
		},
		{
			// The same repeat moved onto its own line. Beside the row
			// above, which varies only the column, this keeps both
			// halves of the coordinate load-bearing: a position
			// carrying the line with a constant column passes one of
			// these and not the other, and so does one carrying the
			// column alone.
			name: "the same call on a later line carries that line",
			queries: []codegen.NamedQuery{textQuery("TwiceDown",
				"MATCH (p:Person)\nWHERE p.at < datetime() AND p.until > datetime()\nRETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query",
			dropped: `TwiceDown ("datetime" at 2:14, "datetime" at 2:39)`,
		},
	} {
		s.Run(tc.name, func() {
			batch := in
			batch.Queries = tc.queries
			files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
			s.Require().Error(err)
			s.Require().Nil(files, "a rejected batch must not return a partial file set")
			s.Require().ErrorIs(err, age.ErrUndefinedFunction)
			s.Require().NotErrorIs(err, age.ErrRelationshipTypeAlternation,
				"two gaps with two answers, and a caller must be able to tell them apart")
			s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
			s.Require().EqualError(err, wantUndefinedFunctionRefusal(tc.count, tc.noun, tc.dropped))
		})
	}

	s.Run("the one temporal function AGE does define is served", func() {
		// timestamp() is not an omission from the catalogue, it is a
		// measured PASS: the spike ran it and got epoch millis back
		// (gqlc-35yu.5). A gate refusing every temporal-looking name
		// would refuse the one call that works.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Now",
			"MATCH (p:Person) RETURN timestamp() AS t\n",
			resolver.Column{Name: "t", Type: resolver.ResolvedScalar{Kind: resolver.ScalarInt}})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().NoError(err)
		s.Require().NotEmpty(files)
	})

	s.Run("a name witnessed for a different gap is not refused by this one", func() {
		// The bound on the whole gate, and the reason it is a table and
		// not a list of names someone believed. point() is REFUSED by
		// the pinned image — SQLSTATE 42883, `function point does not
		// exist`, measured 2026-08-29 (bd gqlc-osf1) — and it is still
		// not refused HERE, because this gap's refused set is derived
		// from its own probes and its message scopes itself to temporal
		// constructors. Refusing point() on the strength of a
		// measurement made elsewhere would print an answer about
		// temporals over a name that is not one.
		//
		// This row asserted NoError until the spatial gap landed (bd
		// gqlc-l8e2n). That was true then and is the weaker claim: it
		// cannot tell a name this gap declined from a name no gap takes.
		// Naming the sentinel that DOES answer keeps the property once a
		// third gap exists, and fails on a point() smuggled into
		// undefinedFunctionProbes either way.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Near",
			"MATCH (p:Person) WHERE p.at < point({x: 1, y: 2}) RETURN p.id\n",
			scalarColumn("p.id", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files, "a rejected batch must not return a partial file set")
		s.Require().NotErrorIs(err, age.ErrUndefinedFunction,
			"a witness for another gap does not add a name to this one")
		s.Require().ErrorIs(err, age.ErrUndefinedSpatialFunction)
	})

	// Cypher.g4 §oC_FunctionName is `oC_Namespace oC_SymbolicName`, and a
	// server resolving `duration.between` resolves nothing about
	// `duration`. So a namespaced call is not a name in THIS gap — and
	// since bd gqlc-dy40s it is not unrefused either: it is the fourth
	// gap's, answered per namespace under ErrUndefinedNamespace.
	//
	// Both sentinels are named on the refusing row. Naming only the one
	// that answers would leave the row green if the temporal gap started
	// claiming namespaced calls, which is the confusion the fourth
	// sentinel was minted to prevent.
	s.Run("a namespaced call is refused by the namespace gap, not this one", func() {
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Between",
			"MATCH (p:Person) WHERE duration.between(p.a, p.b) > 0 RETURN p.id\n",
			scalarColumn("p.id", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files, "a rejected batch must not return a partial file set")
		s.Require().NotErrorIs(err, age.ErrUndefinedFunction,
			"the temporal catalogue holds the bare name, and this call does not spell it")
		s.Require().ErrorIs(err, age.ErrUndefinedNamespace)
		s.Require().Contains(err.Error(), `"duration.between"`,
			"the refusal quotes the whole call the author wrote, namespace included")
	})

	// The false positive, and the half of this pair that can still fail.
	// `com.example.datetime()` calls a function under a namespace no
	// probe has measured. Two different guards have to hold for it to
	// generate: the unqualified scan must not report "datetime" out of
	// it — that name IS in the temporal catalogue, so a scan that lost
	// its namespace guard would refuse an author's own user-defined
	// function on the strength of a probe that measured something else —
	// and the namespace gap must not refuse an unprobed namespace.
	//
	// The pinned image refuses this text too, and that is deliberately
	// not gqlc's business: a gap is what a probe witnessed. A false
	// positive is the one failure this gate has no recovery from, since
	// ADR 0005 leaves the author no rewrite, so the gate under-refuses on
	// purpose and this row is where that choice is held.
	s.Run("a call under a namespace no probe measured is not refused", func() {
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Between",
			"MATCH (p:Person) WHERE p.at < com.example.datetime() RETURN p.id\n",
			scalarColumn("p.id", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().NoError(err)
		s.Require().NotEmpty(files)
	})

	// Case folding, which is load-bearing rather than cosmetic.
	// shape.go lowercases a namespace before typing a temporal, so
	// `Duration.Between` reaches a temporal column further down and is
	// answered by the portable carrier refusal instead of by the text —
	// a different sentinel, a different remedy, and a claim about this
	// backend's carriers rather than about the server's schemas. The
	// pinned image folds nothing: it answers `schema "Duration" does not
	// exist`, quoting the author's own case back.
	s.Run("a namespaced call is refused whatever case it is written in", func() {
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Between",
			"MATCH (p:Person) WHERE Duration.Between(p.a, p.b) > 0 RETURN p.id\n",
			scalarColumn("p.id", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files, "a rejected batch must not return a partial file set")
		s.Require().ErrorIs(err, age.ErrUndefinedNamespace)
		s.Require().Contains(err.Error(), `"Duration.Between"`,
			"the refusal quotes the author's own case, which is what they have to find in the file")
	})

	s.Run("a property named like a constructor is not a call", func() {
		// The false positive a scan for `datetime(` would take, and the
		// reason this reads the grammar. A property lookup names no
		// function.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Stamps",
			"MATCH (p:Person) RETURN p.datetime AS t\n",
			scalarColumn("t", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().NoError(err)
		s.Require().NotEmpty(files)
	})

	s.Run("a projected constructor is answered here, ahead of the portable temporal refusal", func() {
		// This gate's POSITION, pinned by its consequence. A projected
		// date() is refused twice over: codegen.Prepare has no carrier
		// for the temporal column (ADR 0025) and this server has no
		// function to produce one. Running ahead of Prepare makes the
		// missing function the answer, and that is the right order for
		// the same reason the alternation's is — the text has to be
		// rewritten before any column question can be put to this
		// server at all, and no projection of date() will ever parse
		// here whatever carrier AGE later grows.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("SeenOn",
			"MATCH (e:Person) RETURN e.id AS id, date() AS seenOn\n",
			scalarColumn("id", graph.TypeInt),
			resolver.Column{Name: "seenOn", Type: resolver.ResolvedTemporal{Kind: resolver.TemporalDate}})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUndefinedFunction)
		s.Require().NotErrorIs(err, codegen.ErrUnrepresentableTemporal,
			"the carrier is not yet the obstacle: the statement never parses")
		s.Require().EqualError(err, wantUndefinedFunctionRefusal(1, "query", `SeenOn ("date" at 1:37)`))
	})

	s.Run("an unserved column yields to this gap as it does to the alternation", func() {
		// rejectUnservedQueries yields to the TEXT, not to one
		// construct in it. A gap added to the table has to inherit the
		// yield or the author is sent to fix a projection behind a
		// statement that never parsed — the round trip the ordering
		// exists to remove.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Bagged",
			"MATCH (p:Person) WHERE p.at < datetime() RETURN properties(p) AS bag\n",
			resolver.Column{Name: "bag", Type: resolver.ResolvedScalar{Kind: resolver.ScalarMap}})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUndefinedFunction)
		s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().EqualError(err, wantUndefinedFunctionRefusal(1, "query", `Bagged ("datetime" at 1:31)`))
	})

	s.Run("the alternation is answered first when a query spells both", func() {
		// Two gaps, one query, one message: the table is ordered and
		// the first entry that fires answers. Which one wins matters
		// less than that the answer is stable and names a real defect —
		// but it must not be a merged message quoting a '|' and a
		// function name under one sentinel, because a caller branching
		// with errors.Is would then get an answer that is true of
		// neither.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Both",
			"MATCH (:Person)-[r:AUTHORED|LIKES]->(p:Post) WHERE p.at < datetime() RETURN p.id\n",
			scalarColumn("p.id", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrRelationshipTypeAlternation)
		s.Require().NotErrorIs(err, age.ErrUndefinedFunction)
		s.Require().EqualError(err, wantAlternationRefusal(1, "query", `Both (":AUTHORED|LIKES" at 1:19)`))
	})
}

// wantSpatialRefusal is this test's own copy of the third text-level
// refusal, on the same terms as the two above: a change to the
// emission's wording has to be a change here too.
func wantSpatialRefusal(count int, noun, dropped string) string {
	return fmt.Sprintf("undefined spatial function: generated code runs the author's query text "+
		"verbatim (ADR 0005) and Apache AGE 1.7.0 does not define the spatial constructor this "+
		"project has measured, so every call on %d %s would answer "+
		"\"function <name> does not exist\" (SQLSTATE 42883) — store the coordinates as "+
		"ordinary properties and compute the geometry in Go; each call is located "+
		"line:column within its own query's text: %s", count, noun, dropped)
}

// TestRejectsTheSpatialConstructor pins the third gap the text gate
// reads. point() is refused by the pinned image — SQLSTATE 42883,
// `function point does not exist`, measured 2026-08-29 on workflow run
// 33268424367 (bd gqlc-l8e2n) — and it is a gap of its own rather than a
// name in the temporal one, because the temporal gap's message is a
// claim about temporal constructors and point is not one. A caller
// branching with errors.Is would otherwise be handed a sentinel
// belonging to a fix it did not ask about.
func (s *EmissionSuite) TestRejectsTheSpatialConstructor() {
	in := s.inputFrom(filepath.Join("testdata", corpusSchema))

	for _, tc := range []struct {
		name    string
		queries []codegen.NamedQuery
		count   int
		noun    string
		dropped string
	}{
		{
			// The load-bearing shape, as for the temporal gap: the call
			// is in a predicate, which the query model drops (ADR 0003),
			// so no column, parameter or binding carries it and the text
			// is the only place it can be read.
			name: "a call in a predicate the model drops is refused",
			queries: []codegen.NamedQuery{textQuery("Near",
				"MATCH (p:Person) WHERE p.loc = point({x: 1, y: 2}) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Near ("point" at 1:32)`,
		},
		{
			name: "a call in a write that projects nothing is refused",
			queries: []codegen.NamedQuery{execTextQuery("Place",
				"MATCH (p:Person) SET p.loc = point({x: 1, y: 2})\n")},
			count: 1, noun: "query", dropped: `Place ("point" at 1:30)`,
		},
		{
			name: "the name is quoted in the author's own case",
			queries: []codegen.NamedQuery{textQuery("Near",
				"MATCH (p:Person) WHERE p.loc = Point({x: 1, y: 2}) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Near ("Point" at 1:32)`,
		},
		{
			name: "every offending query in the batch is named",
			queries: []codegen.NamedQuery{
				servedQuery,
				textQuery("Near",
					"MATCH (p:Person) WHERE p.loc = point({x: 1, y: 2}) RETURN p.id\n",
					scalarColumn("p.id", graph.TypeInt)),
				execTextQuery("Place",
					"MATCH (p:Person) SET p.loc = point({x: 3, y: 4})\n"),
			},
			count: 2, noun: "queries",
			dropped: `Near ("point" at 1:32), Place ("point" at 1:30)`,
		},
		{
			// Two calls on ONE name, which the quoted name alone
			// cannot answer: the author has two calls to rewrite and
			// the message can only say so by naming where each is.
			// Nothing but the column separates these two entries, so a
			// scan dropping the repeat drops one of them. "MATCH " is
			// 6, "(p:Person)" takes 7-16, " WHERE" ends at 22, " p.a"
			// at 26, " =" at 28 and " " at 29, so the first begins at
			// 30; "point({x: 1, y: 2})" is 19 characters ending at 48,
			// " AND" at 52, " p.b" at 56, " =" at 58 and " " at 59, so
			// the second begins at 60.
			name: "the same call written twice is named twice, at different positions",
			queries: []codegen.NamedQuery{textQuery("Twice",
				"MATCH (p:Person) WHERE p.a = point({x: 1, y: 2}) AND p.b = point({x: 3, y: 4}) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query",
			dropped: `Twice ("point" at 1:30, "point" at 1:60)`,
		},
	} {
		s.Run(tc.name, func() {
			batch := in
			batch.Queries = tc.queries
			files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
			s.Require().Error(err)
			s.Require().Nil(files, "a rejected batch must not return a partial file set")
			s.Require().ErrorIs(err, age.ErrUndefinedSpatialFunction)
			s.Require().NotErrorIs(err, age.ErrUndefinedFunction,
				"the temporal gap's remedy does not answer a spatial call, and a caller "+
					"branching with errors.Is must not be handed it")
			s.Require().NotErrorIs(err, age.ErrRelationshipTypeAlternation)
			s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
			s.Require().EqualError(err, wantSpatialRefusal(tc.count, tc.noun, tc.dropped))
		})
	}

	s.Run("a temporal call is not answered by this sentinel", func() {
		// The other direction of the same claim, and the one that can
		// fail on its own: a gate reading both catalogues under one
		// sentinel would satisfy every row above while telling an
		// author calling datetime() to store coordinates as
		// properties.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Recent",
			"MATCH (p:Person) WHERE p.at < datetime() RETURN p.id\n",
			scalarColumn("p.id", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUndefinedFunction)
		s.Require().NotErrorIs(err, age.ErrUndefinedSpatialFunction)
	})

	s.Run("a query spelling both is answered by the temporal gap", func() {
		// Three gaps now, one query, one message. Which one wins
		// matters less than that the answer is stable and names a real
		// defect — but it must not be a merged message quoting a
		// constructor and point() under one sentinel, because a caller
		// branching with errors.Is would then get an answer true of
		// neither.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Both",
			"MATCH (p:Person) WHERE p.at < datetime() AND p.loc = point({x: 1, y: 2}) RETURN p.id\n",
			scalarColumn("p.id", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUndefinedFunction)
		s.Require().NotErrorIs(err, age.ErrUndefinedSpatialFunction)
		s.Require().EqualError(err, wantUndefinedFunctionRefusal(1, "query", `Both ("datetime" at 1:31)`))
	})

	s.Run("an unserved column yields to this gap as it does to the other two", func() {
		// rejectUnservedQueries yields to the TEXT, not to one
		// construct in it, and a gap added to the table has to inherit
		// that yield or the author is sent to fix a projection behind a
		// statement that never parsed.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Bagged",
			"MATCH (p:Person) WHERE p.loc = point({x: 1, y: 2}) RETURN properties(p) AS bag\n",
			resolver.Column{Name: "bag", Type: resolver.ResolvedScalar{Kind: resolver.ScalarMap}})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUndefinedSpatialFunction)
		s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().EqualError(err, wantSpatialRefusal(1, "query", `Bagged ("point" at 1:32)`))
	})

	s.Run("a property named like the constructor is not a call", func() {
		// The false positive a scan for `point(` would take, and the
		// reason this reads the grammar rather than the characters. It
		// is the served bound the gate has no recovery from: ADR 0005
		// runs the author's text verbatim, so a wrong refusal leaves
		// them no rewrite at all.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Spots",
			"MATCH (p:Person) RETURN p.point AS pt\n",
			scalarColumn("pt", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().NoError(err)
		s.Require().NotEmpty(files)
	})
}

// wantNamespaceRefusal is the fourth gap's message, built here so a
// change to the prose has to be made twice — once where the author reads
// it and once where it is asserted.
func wantNamespaceRefusal(count int, noun, dropped string) string {
	return fmt.Sprintf("undefined namespace: generated code runs the author's query text verbatim "+
		"(ADR 0005) and Apache AGE 1.7.0 has no schema for the namespace this project has "+
		"measured, so every call on %d %s would answer \"schema <namespace> does not exist\" "+
		"(SQLSTATE 3F000) — PostgreSQL resolves the namespace as a schema qualifier before it "+
		"looks for any function, so no function under that namespace resolves whatever it is "+
		"called: compute the value in Go and bind it as a parameter, or generate against a "+
		"neo4j target; each call is located line:column within its own query's text: %s",
		count, noun, dropped)
}

// TestRejectsTheUndefinedNamespace pins the fourth gap: a call under a
// namespace the pinned image has no schema for.
//
// It is the one gap here whose refusal names no function. PostgreSQL
// resolves the schema qualifier before it looks for a function, so
// duration.between answers SQLSTATE 3F000 `schema "duration" does not
// exist` — measured on the pinned image, bd gqlc-dy40s — and the
// catalogue is therefore of namespaces. That widening is the gap's one
// step past its probe's letter, and it is witnessed rather than assumed:
// duration.inSeconds, a function no probe ran, answers the same class
// with the same message on the same image
// (TestAGERefusesTheNamespaceItHasNoSchemaFor).
func (s *EmissionSuite) TestRejectsTheUndefinedNamespace() {
	in := s.inputFrom(filepath.Join("testdata", corpusSchema))

	for _, tc := range []struct {
		name    string
		queries []codegen.NamedQuery
		count   int
		noun    string
		dropped string
	}{
		{
			// The load-bearing shape: a predicate reaches no column at
			// all, because the query model drops predicate structure
			// (ADR 0003), so only the text carries this call.
			name: "a call in a predicate the model drops is refused",
			queries: []codegen.NamedQuery{textQuery("Between",
				"MATCH (p:Person) WHERE p.d < duration.between(p.a, p.b) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Between ("duration.between" at 1:30)`,
		},
		{
			name: "a call in a write that projects nothing is refused",
			queries: []codegen.NamedQuery{execTextQuery("Stamp",
				"MATCH (p:Person) SET p.d = duration.between(p.a, p.b)\n")},
			count: 1, noun: "query", dropped: `Stamp ("duration.between" at 1:28)`,
		},
		{
			// The whole call is quoted, namespace included. Quoting the
			// namespace alone would print a string that is nowhere in
			// the author's file.
			name: "the call is quoted whole and in the author's own case",
			queries: []codegen.NamedQuery{textQuery("Between",
				"MATCH (p:Person) WHERE p.d < Duration.Between(p.a, p.b) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Between ("Duration.Between" at 1:30)`,
		},
		{
			// A function no probe ever called, refused on the strength
			// of its namespace. This is the per-namespace catalogue
			// doing the thing a per-full-name one could not.
			name: "another function under the measured namespace is refused too",
			queries: []codegen.NamedQuery{textQuery("Secs",
				"MATCH (p:Person) WHERE p.d < duration.inSeconds(p.a) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query", dropped: `Secs ("duration.inSeconds" at 1:30)`,
		},
		{
			name: "every offending query in the batch is named",
			queries: []codegen.NamedQuery{
				servedQuery,
				textQuery("Between",
					"MATCH (p:Person) WHERE p.d < duration.between(p.a, p.b) RETURN p.id\n",
					scalarColumn("p.id", graph.TypeInt)),
				execTextQuery("Stamp",
					"MATCH (p:Person) SET p.d = duration.between(p.a, p.b)\n"),
			},
			count: 2, noun: "queries",
			dropped: `Between ("duration.between" at 1:30), Stamp ("duration.between" at 1:28)`,
		},
		{
			// Two calls under ONE namespace, on the same terms as the
			// other two gaps: the quoted spelling is identical, so only
			// the column tells the author which call is which. "MATCH "
			// is 6, "(p:Person)" takes 7-16, " WHERE" ends at 22,
			// " p.d" at 26, " <" at 28 and " " at 29, so the first
			// begins at 30; "duration.between" is 16 characters ending
			// at 45, "(p.a, p.b)" at 55, " AND" at 59, " p.e" at 63,
			// " <" at 65 and " " at 66, so the second begins at 67.
			name: "the same call written twice is named twice, at different positions",
			queries: []codegen.NamedQuery{textQuery("Twice",
				"MATCH (p:Person) WHERE p.d < duration.between(p.a, p.b) AND p.e < duration.between(p.x, p.y) RETURN p.id\n",
				scalarColumn("p.id", graph.TypeInt))},
			count: 1, noun: "query",
			dropped: `Twice ("duration.between" at 1:30, "duration.between" at 1:67)`,
		},
	} {
		s.Run(tc.name, func() {
			batch := in
			batch.Queries = tc.queries
			files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
			s.Require().Error(err)
			s.Require().Nil(files, "a rejected batch must not return a partial file set")
			s.Require().ErrorIs(err, age.ErrUndefinedNamespace)
			s.Require().NotErrorIs(err, age.ErrUndefinedFunction,
				"the temporal gap's catalogue holds bare names, and its remedy names a "+
					"missing function rather than a missing schema")
			s.Require().NotErrorIs(err, age.ErrUndefinedSpatialFunction)
			s.Require().NotErrorIs(err, age.ErrRelationshipTypeAlternation)
			s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
			s.Require().EqualError(err, wantNamespaceRefusal(tc.count, tc.noun, tc.dropped))
		})
	}

	// The two ordering pins. The namespace gap answers LAST, so a query
	// spelling an earlier gap's construct as well is told about that one,
	// and the pair is also where the call-shape partition is measured:
	// each spelling is claimed by exactly one gap, so the two scans
	// cannot both take one call.
	s.Run("a query spelling a bare constructor and a namespaced one is answered by the temporal gap", func() {
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Both",
			"MATCH (p:Person) WHERE p.a < duration({days: 1}) AND p.d < duration.between(p.x, p.y) RETURN p.id\n",
			scalarColumn("p.id", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUndefinedFunction)
		s.Require().NotErrorIs(err, age.ErrUndefinedNamespace)
		// The message quotes the bare name ALONE. One token spells both
		// constructs here, so a merged reading would be invisible to the
		// sentinel assertions above and visible only in what is quoted.
		s.Require().EqualError(err, wantUndefinedFunctionRefusal(1, "query", `Both ("duration" at 1:30)`))
	})

	s.Run("a query spelling a spatial constructor and a namespaced call is answered by the spatial gap", func() {
		// The analogous pairing for the gap that answers third. It pairs
		// point() with duration.between() rather than with
		// point.distance(): `point` is not a namespace any probe has
		// measured, so a point()/point.distance() query trips only the
		// spatial gap and would pin no ordering at all. That silence is
		// itself worth having, and it is the row below.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Both",
			"MATCH (p:Person) WHERE p.loc = point({x: 1, y: 2}) AND p.d < duration.between(p.x, p.y) RETURN p.id\n",
			scalarColumn("p.id", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUndefinedSpatialFunction)
		s.Require().NotErrorIs(err, age.ErrUndefinedNamespace)
		s.Require().EqualError(err, wantSpatialRefusal(1, "query", `Both ("point" at 1:32)`))
	})

	s.Run("a namespace no probe measured is served, even where the bare name is refused", func() {
		// point() is refused and `point.distance()` is not, which reads
		// like an inconsistency and is the gate's central rule: a gap is
		// what a probe witnessed. The pinned image refuses this text —
		// `foo.bar(1)` answers 3F000 the same way, measured — and gqlc
		// still generates it, because a false positive is the one
		// failure ADR 0005 leaves the author no rewrite from.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Far",
			"MATCH (p:Person) WHERE p.d < point.distance(p.a, p.b) RETURN p.id\n",
			scalarColumn("p.id", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().NoError(err)
		s.Require().NotEmpty(files)
	})

	s.Run("an unserved column yields to this gap as it does to the other three", func() {
		// rejectUnservedQueries yields to the TEXT, not to one construct
		// in it, so a gap added to the table has to inherit that yield
		// or the author is sent to fix a projection sitting behind a
		// statement that never parsed.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Bagged",
			"MATCH (p:Person) WHERE p.d < duration.between(p.a, p.b) RETURN properties(p) AS bag\n",
			resolver.Column{Name: "bag", Type: resolver.ResolvedScalar{Kind: resolver.ScalarMap}})}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().Error(err)
		s.Require().Nil(files)
		s.Require().ErrorIs(err, age.ErrUndefinedNamespace)
		s.Require().NotErrorIs(err, age.ErrUnsupportedQuery)
		s.Require().EqualError(err, wantNamespaceRefusal(1, "query", `Bagged ("duration.between" at 1:30)`))
	})

	s.Run("a property named like the namespace is not a call", func() {
		// The false positive a scan for `duration.` would take, and the
		// reason this reads the grammar rather than the characters.
		batch := in
		batch.Queries = []codegen.NamedQuery{textQuery("Spans",
			"MATCH (p:Person) RETURN p.duration AS d\n",
			scalarColumn("d", graph.TypeInt))}
		files, err := age.New(age.WithPackageName(corpusPackage)).Generate(batch)
		s.Require().NoError(err)
		s.Require().NotEmpty(files)
	})
}

// TestRejectsQueriesItCannotServe pins the capability gate. The emitted
// arms cover agtype's scalar vocabulary, the vertices and edges built
// out of it, and a statement that writes with or without projecting, so
// a batch carrying a query outside that would hand the author a Querier
// that silently omits the query — or, worse, a method built on a decode
// arm that does not exist. The error names every dropped query and the
// axis that dropped it.
func (s *EmissionSuite) TestRejectsQueriesItCannotServe() {
	served := servedQuery

	moved := func(name string, mutate func(*codegen.NamedQuery)) codegen.NamedQuery {
		q := readQuery(name, scalarColumn("p.name", graph.TypeString))
		mutate(&q)
		return q
	}
	write := moved("Wipe", func(q *codegen.NamedQuery) {
		q.Validated.Statement = resolver.StatementWrite
	})
	execListParam := func() codegen.NamedQuery {
		q := execQuery("Batch")
		q.Validated.Parameters = []resolver.ResolvedParameter{{
			Name: "ids", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.TypeInt, true)},
		}}
		return q
	}()
	execUncarriedParam := func() codegen.NamedQuery {
		q := execQuery("Batch")
		q.Validated.Parameters = []resolver.ResolvedParameter{{
			Name: "for", Type: resolver.ResolvedProperty{Type: graph.TypeBytes},
		}}
		return q
	}()
	node := moved("Whole", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "p", Type: resolver.ResolvedNode{Labels: graph.LabelSetKey(personLabel)},
		}}
	})
	edgeUnion := moved("Action", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{Name: "r", Type: twoCandidateEdgeUnion}}
	})
	temporal := moved("When", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "t", Type: resolver.ResolvedTemporal{Kind: resolver.TemporalDate},
		}}
	})
	// A list expression column is judged by its ELEMENT, not by the
	// fact that it is a list (bd gqlc-p6cb): a list of string scalars
	// generates, and this one is refused for the vertex it holds, which
	// has no element decoder. The whole element table is
	// TestListExpressionColumnIsJudgedByItsElement's.
	list := moved("Tags", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "t", Type: resolver.ResolvedList{Element: resolver.ResolvedNode{Labels: graph.LabelSetKey(personLabel)}},
		}}
	})
	servedList := moved("Handles", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "t", Type: resolver.ResolvedList{Element: resolver.ResolvedScalar{Kind: resolver.ScalarString}},
		}}
	})
	mapCol := moved("Bag", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "m", Type: resolver.ResolvedScalar{Kind: resolver.ScalarMap},
		}}
	})
	unknown := moved("Opaque", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{Name: "u", Type: resolver.ResolvedUnknown{}}}
	})
	listParam := moved("Batch", func(q *codegen.NamedQuery) {
		q.Validated.Parameters = []resolver.ResolvedParameter{{
			Name: "ids", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.TypeInt, true)},
		}}
	})
	listProp := moved("Tagged", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "t", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.ListOf(graph.TypeString, true), true)},
		}}
	})
	anyProp := moved("Payload", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "p", Type: resolver.ResolvedProperty{Type: graph.TypeAnyPropertyValue},
		}}
	})
	uncarriedProp := moved("Stamp", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "t", Type: resolver.ResolvedProperty{Type: graph.ListOf(graph.TypeTimestamp, false)},
		}}
	})
	// The arm of unservedParam a width question does not reach. It is
	// read off the parameter alone, ahead of Prepare, so this gate is the
	// only thing that can answer it here.
	//
	// A parameter that is not a schema property at all: the encode path
	// builds an agtype argument out of a declared width, so a shape with
	// no width is a shape it has nothing to build from. Shared admission
	// is expected to refuse these before emission, which is why the arm
	// reads as defensive — but this gate runs BEFORE Prepare, so a batch
	// handed straight to a backend reaches it, and an arm that answered
	// "" would let such a parameter through to an encode call with no
	// case for it.
	nonPropertyParam := moved("Batch", func(q *codegen.NamedQuery) {
		q.Validated.Parameters = []resolver.ResolvedParameter{{
			Name: "bag", Type: resolver.ResolvedScalar{Kind: resolver.ScalarMap},
		}}
	})
	// A width outside the type table, on both axes. It is the same
	// question the shared width sweep asks of the SCHEMA, asked here of
	// the query — and the two are different questions with different
	// answers, because a column or an argument can carry a width no
	// entity's property does. Answering it here is what makes the refusal
	// name the query rather than send the author to a schema that carries
	// the width legitimately.
	wideColumn := moved("Blob", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "payload", Type: resolver.ResolvedProperty{Type: graph.TypeBytes},
		}}
	})
	wideParam := moved("ByBlob", func(q *codegen.NamedQuery) {
		q.Validated.Parameters = []resolver.ResolvedParameter{{
			Name: "payload", Type: resolver.ResolvedProperty{Type: graph.TypeBytes},
		}}
	})

	// wantSentinel is the error a rejecting row expects, defaulting to
	// this backend's unserved-query sentinel. A temporal column takes a
	// different one: whether a kind has a carrier is the type table's
	// answer, and the shared phase asks it and refuses with
	// codegen.ErrUnrepresentableTemporal naming the kind — a sentinel
	// this gate cannot raise, since it reports one reason per query and
	// not one per column type.
	cases := []struct {
		name         string
		queries      []codegen.NamedQuery
		wantSub      string
		wantSentinel error
		wantError    bool
	}{
		{
			name:    "no queries generates",
			queries: nil,
		},
		{
			name:    "a scalar read generates",
			queries: []codegen.NamedQuery{served},
		},
		{
			name: "a scalar read with a scalar parameter generates",
			queries: []codegen.NamedQuery{moved("ByID", func(q *codegen.NamedQuery) {
				q.Validated.Parameters = []resolver.ResolvedParameter{{
					Name: "id", Type: resolver.ResolvedProperty{Type: graph.TypeInt},
				}}
			})},
		},
		{
			name:    "a write that projects generates",
			queries: []codegen.NamedQuery{write},
		},
		{
			name:    "a write that projects nothing generates",
			queries: []codegen.NamedQuery{execQuery("Purge")},
		},
		{
			name: "an exec binding a scalar parameter generates",
			queries: []codegen.NamedQuery{func() codegen.NamedQuery {
				q := execQuery("PurgeByID")
				q.Validated.Parameters = []resolver.ResolvedParameter{{
					Name: "id", Type: resolver.ResolvedProperty{Type: graph.TypeInt},
				}}
				return q
			}()},
		},
		{
			// The argument object is JSON, whose syntax agtype reads, so a
			// slice crosses as the agtype list AGE substitutes.
			name:    "an exec binding a list parameter generates",
			queries: []codegen.NamedQuery{execListParam},
		},
		{
			name:      "an exec binding an uncarried parameter width is dropped",
			queries:   []codegen.NamedQuery{execUncarriedParam},
			wantSub:   `1 query would be dropped: Batch (parameter $for is property:BYTES)`,
			wantError: true,
		},
		{
			name:    "a whole-entity column generates",
			queries: []codegen.NamedQuery{node},
		},
		{
			name:         "a temporal column is refused by the type table, naming the kind",
			queries:      []codegen.NamedQuery{temporal},
			wantSentinel: codegen.ErrUnrepresentableTemporal,
			wantSub: `unrepresentable temporal kind: query "When" column 0 "t" projects temporal(date), ` +
				`which the Apache AGE backend has no carrier for`,
			wantError: true,
		},
		{
			// The decode path a schema list property rides is the one an
			// expression list rides, so a served element generates here
			// as it does there.
			name:    "a list expression column of a served element generates",
			queries: []codegen.NamedQuery{servedList},
		},
		{
			name:      "a list column whose element has no decoder is dropped, naming the element",
			queries:   []codegen.NamedQuery{list},
			wantSub:   `1 query would be dropped: Tags (column "t" projects a list of node)`,
			wantError: true,
		},
		{
			name:      "a map column is dropped",
			queries:   []codegen.NamedQuery{mapCol},
			wantSub:   `1 query would be dropped: Bag (column "m" projects scalar(map))`,
			wantError: true,
		},
		{
			name:      "an edge-union column is dropped",
			queries:   []codegen.NamedQuery{edgeUnion},
			wantSub:   `1 query would be dropped: Action (column "r" ` + wantEdgeUnionReason("AUTHORED and LIKES") + `)`,
			wantError: true,
		},
		{
			name:      "an unresolved column is dropped",
			queries:   []codegen.NamedQuery{unknown},
			wantSub:   `1 query would be dropped: Opaque (column "u" projects unknown)`,
			wantError: true,
		},
		{
			name:    "a list parameter generates",
			queries: []codegen.NamedQuery{listParam},
		},
		{
			name:      "a parameter that is not a schema property is dropped",
			queries:   []codegen.NamedQuery{nonPropertyParam},
			wantSub:   `1 query would be dropped: Batch (parameter $bag is scalar(map))`,
			wantError: true,
		},
		{
			name:      "a column whose width has no carrier is dropped, naming the query",
			queries:   []codegen.NamedQuery{wideColumn},
			wantSub:   `1 query would be dropped: Blob (column "payload" projects property:BYTES)`,
			wantError: true,
		},
		{
			name:      "a parameter whose width has no carrier is dropped, naming the query",
			queries:   []codegen.NamedQuery{wideParam},
			wantSub:   `1 query would be dropped: ByBlob (parameter $payload is property:BYTES)`,
			wantError: true,
		},
		{
			// A list property is served exactly when its element width is,
			// at whatever depth, and a property of no declared shape is
			// served through agtype's own value vocabulary.
			name:    "a nested list-property column generates",
			queries: []codegen.NamedQuery{listProp},
		},
		{
			name:    "a column of no declared shape generates",
			queries: []codegen.NamedQuery{anyProp},
		},
		{
			// The declared width is what the report names, not the element's:
			// LIST<TIMESTAMP> is the line the author can go and find.
			name:      "a list of an uncarried element width is dropped",
			queries:   []codegen.NamedQuery{uncarriedProp},
			wantSub:   `1 query would be dropped: Stamp (column "t" projects property:LIST<TIMESTAMP>)`,
			wantError: true,
		},
		{
			name:      "every dropped query is named, in batch order",
			queries:   []codegen.NamedQuery{execUncarriedParam, mapCol, list},
			wantSub:   "3 queries would be dropped: Batch (parameter $for is property:BYTES), Bag (column \"m\" projects scalar(map)), Tags (column \"t\" projects a list of node)",
			wantError: true,
		},
		{
			// The gate runs ahead of the shared phases, so it answers
			// first even though the batch also carries a kind the type
			// table refuses: a batch with a query this backend emits no
			// method for is not improved by first being told about a
			// column type, and repairing the column would leave the batch
			// exactly where it was.
			name:      "an unserved query outranks a refused temporal kind",
			queries:   []codegen.NamedQuery{temporal, list},
			wantSub:   `1 query would be dropped: Tags (column "t" projects a list of node)`,
			wantError: true,
		},
		{
			name:         "a served query alongside a refused one still fails",
			queries:      []codegen.NamedQuery{served, temporal},
			wantSentinel: codegen.ErrUnrepresentableTemporal,
			wantSub: `unrepresentable temporal kind: query "When" column 0 "t" projects temporal(date), ` +
				`which the Apache AGE backend has no carrier for`,
			wantError: true,
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			in := s.in
			in.Queries = tc.queries
			files, err := age.New().Generate(in)
			if !tc.wantError {
				s.Require().NoError(err)
				s.Require().NotEmpty(files)
				return
			}
			s.Require().Error(err)
			s.Require().Nil(files, "a rejected batch must not return a partial file set")
			wantSentinel := tc.wantSentinel
			if wantSentinel == nil {
				wantSentinel = age.ErrUnsupportedQuery
			}
			s.Require().ErrorIs(err, wantSentinel)
			s.Require().ErrorContains(err, tc.wantSub)
		})
	}
}

// multiLabelFixture is the corpus fixture whose node type is keyed on
// two labels. It sits in valid/ because the neo4j backends emit for it;
// its manifest leaves this backend out, and the test below is what says
// why.
const multiLabelFixture = "entity_multi_label_named"

// TestRejectsMultiLabelSchema ties the corpus fixture's enrolment to
// this backend's verdict on it. The manifest omitting apache-age-pgx-v5
// is otherwise an unexplained gap a reader would take for an oversight,
// and enrolling it would produce a golden subtree no emission can fill.
//
// Both halves fail on the same edit from opposite sides: enrolling the
// target reddens the enrolment assertion, and serving multi-label
// entities here reddens the refusal. Neither can be satisfied by
// changing only the other (ADR 0027).
func (s *EmissionSuite) TestRejectsMultiLabelSchema() {
	dir := filepath.Join(corpusRoot, "valid", multiLabelFixture)
	m, err := readAgeManifest(dir)
	s.Require().NoError(err)
	s.Require().NotEmpty(m.Targets, "fixture %s enrols no target at all", multiLabelFixture)
	s.Require().NotContains(m.Targets, ageTarget,
		"fixture %s enrols %s, which cannot emit for a multi-label entity", multiLabelFixture, ageTarget)

	files, genErr := age.New().Generate(s.inputFrom(filepath.Join(dir, "schema.gql")))
	s.Require().Error(genErr)
	s.Require().Nil(files, "a refused schema must not return a partial file set")
	s.Require().ErrorIs(genErr, age.ErrUnsupportedSchema)
}

// sidecarSchema is one node type carrying an instant and a second
// property of the given width under the name this backend derives for
// the instant's zone.
func sidecarSchema(width string) string {
	return `CREATE PROPERTY GRAPH TYPE Sidecar AS {
    (:Photo {
        takenAt       :: TIMESTAMP NOT NULL,
        takenAtOffset :: ` + width + ` NOT NULL
    })
}`
}

// TestRejectsAnAuthorOwnedOffsetSidecar pins the derived-name gate. A
// TIMESTAMP property's zone is stored in a second property named
// <property>Offset and read out of the same map the declared properties
// come from, so a schema declaring that name gives one key two readers.
//
// The refusal is the whole file set, before any of it is written.
//
// Every row here is a collision the gate must find, and each moves one
// axis the gate could be blind along. The two widths establish that it
// is blind to width, which it has to be: the two are wrong in different
// ways and neither is detectable from the emitted Go — with an integer
// the decoder compiles and hands back an instant re-zoned by the
// author's own value, and with anything else it compiles and no vertex
// of that type ever decodes. The remaining rows move where the
// collision sits: fields arrive map-key sorted and entities arrive as
// nodes and edges together, so a row whose instant is the entity's first
// field, or whose entity is a node, leaves a gate reading only the first
// field or only the node table indistinguishable from the real one.
func (s *EmissionSuite) TestRejectsAnAuthorOwnedOffsetSidecar() {
	cases := []struct {
		name    string
		schema  string
		entity  string
		instant string
	}{
		{
			name:    "the sidecar is an integer, so the decoder re-zones",
			schema:  sidecarSchema("INT64"),
			entity:  "Photo",
			instant: "takenAt",
		},
		{
			name:    "the sidecar is a string, so no vertex decodes",
			schema:  sidecarSchema("STRING"),
			entity:  "Photo",
			instant: "takenAt",
		},
		{
			// The instant sorts after a property that does not collide, so
			// the entity's first field is not the offender.
			name: "the colliding instant is not the entity's first field",
			schema: `CREATE PROPERTY GRAPH TYPE Sidecar AS {
    (:Photo {
        album          :: STRING    NOT NULL,
        zTakenAt       :: TIMESTAMP NOT NULL,
        zTakenAtOffset :: INT64     NOT NULL
    })
}`,
			entity:  "Photo",
			instant: "zTakenAt",
		},
		{
			// AGE stamps properties on an edge as readily as on a vertex,
			// and the emitted edge decoder reads the sidecar out of the
			// same map, so the collision is the same collision.
			name: "the colliding entity is an edge",
			schema: `CREATE PROPERTY GRAPH TYPE Sidecar AS {
    (:Photo { id :: INT64 NOT NULL }),
    (:Photo) -[:SAW {
        takenAt       :: TIMESTAMP NOT NULL,
        takenAtOffset :: INT64     NOT NULL
    }]-> (:Photo)
}`,
			entity:  "Saw",
			instant: "takenAt",
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			files, err := age.New().Generate(s.inputFromText(tc.schema))
			s.Require().ErrorIs(err, codegen.ErrPropertyFieldCollision,
				"the gate did not see the collision on %s.%s", tc.entity, tc.instant)
			s.Require().ErrorContains(err,
				`entity "`+tc.entity+`" declares property "`+tc.instant+`Offset"`)
			s.Require().ErrorContains(err, `property "`+tc.instant+`"`)
			s.Require().Nil(files, "a rejected schema must not return a partial file set")
		})
	}

	// The name is derived from an instant and from nothing else, so the
	// same pair of names with no instant between them is a schema this
	// backend serves.
	s.Run("no instant derives no name", func() {
		files, err := age.New().Generate(s.inputFromText(strings.Replace(sidecarSchema("INT64"), "TIMESTAMP", "STRING   ", 1)))
		s.Require().NoError(err)
		s.Require().NotEmpty(files)
	})
}

// ageIdentifiers are the extension-owned names that must never appear
// unqualified. Each matches on a word boundary: a longer name that
// merely begins with one is a different name, which AGE neither owns nor
// resolves through the search_path — a query parameter called
// $agtypeArgs reaches an emitted map key spelled exactly that.
//
// agtype carries an optional snake_case tail because ag_catalog owns a
// whole family under that stem, not just the type: agtype_build_map,
// agtype_build_list, agtype_access_operator, the agtype_typecast_* arms,
// agtype_in and agtype_out. Every one of them resolves through the
// search_path exactly as the bare type does, and `_` is a word
// character, so a bare \bagtype\b would let the whole family through —
// the boundary never fires between agtype and _build_map. The tail is
// what keeps the false positive out rather than trading it back: AGE's
// C-level names are all snake_case and the trailing \b has to fire, so
// the camelCase agtypeArgs matches neither arm.
//
// A snake_case name the author chose does reach this — $agtype_args is a
// legal parameter name and the args encoder keys its map on the author's
// spelling — which is why keepServerText cuts those keys out of the
// region alongside the query text. The pattern judges spelling; the
// region judges provenance, and this family needs both.
//
// The array type is spelled agtype[] in SQL, which the bare arm matches
// on the boundary before the bracket. Its pg_type-internal name _agtype
// is not matched and does not need to be: no emitter composes a type
// name out of pg_type, so that spelling is unreachable in emitted SQL.
var ageIdentifiers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bagtype(_\w+)?\b`),
	regexp.MustCompile(`(?i)\bcypher\(`),
	regexp.MustCompile(`(?i)\bcreate_graph\b`),
	regexp.MustCompile(`(?i)\bdrop_graph\b`),
	regexp.MustCompile(`(?i)\bag_graph\b`),
}

// qualifier matches both spellings PostgreSQL accepts for the schema
// prefix, case-insensitively — an unquoted identifier folds to lower
// case, and a delimited one does not have to.
var qualifier = regexp.MustCompile(`(?i)(ag_catalog|"ag_catalog")\.$`)

// TestEveryAgeIdentifierIsSchemaQualified sweeps for AGE's
// extension-owned identifiers and requires each to carry an ag_catalog
// qualifier. An unqualified one compiles and then resolves against
// whatever the caller's search_path happens to hold.
//
// The sweep runs over two populations: freshly emitted bytes for every
// schema in the corpus, each emitted twice — once as the corpus holds
// it and once carrying servedQuery, which is what puts the read path's
// statement text in front of the sweep — and every golden file committed
// for this target. The freshly emitted population is the one that bites
// on an edit to the emission, because regenerating goldens rewrites the
// second to match whatever the emission now says.
//
// The sweep runs over the string literals that reach a parser, which is
// where the whole of this package's SQL lives.
func (s *EmissionSuite) TestEveryAgeIdentifierIsSchemaQualified() {
	schemas, err := filepath.Glob(filepath.Join(corpusRoot, "valid", "*", "schema.gql"))
	s.Require().NoError(err)
	s.Require().Greater(len(schemas), 10, "corpus glob found too few schemas")

	swept := 0
	for _, path := range schemas {
		fixture := filepath.Base(filepath.Dir(path))
		in := s.inputFrom(path)
		for _, queries := range [][]codegen.NamedQuery{nil, {servedQuery}} {
			in.Queries = queries
			files, err := age.New().Generate(in)
			// A schema whose widths this backend cannot carry is rejected
			// by the type table, so there is nothing to sweep.
			if err != nil {
				continue
			}
			qualified := 0
			for _, f := range files {
				qualified += s.assertQualified(fixture+"/"+f.Path, string(f.Contents))
			}
			s.Require().Positive(qualified,
				"%s: the sweep found no AGE identifier at all, so it proved nothing about this emission", fixture)
			swept++
		}
	}
	s.Require().Greater(swept, 20, "too few corpus schemas produced emission to sweep")

	goldens, err := filepath.Glob(filepath.Join(corpusRoot, "valid", "*", "golden", ageTarget, "*.go"))
	s.Require().NoError(err)
	s.Require().NotEmpty(goldens, "no golden files committed for target %s", ageTarget)
	qualified := 0
	for _, path := range goldens {
		body, err := os.ReadFile(path)
		s.Require().NoError(err)
		qualified += s.assertQualified(path, string(body))
	}
	s.Require().Positive(qualified, "the sweep found no AGE identifier in the whole golden tree")
}

// sweptWitnesses are qualified occurrences the emission is known to
// place, per file that carries any SQL at all. They are spelled here
// rather than read off the emission, so the region and the bytes it has
// to hold cannot drift together.
var sweptWitnesses = map[string][]string{
	"db.go":       {"ag_catalog.cypher("},
	"graph.go":    {"'1'::ag_catalog.agtype", "ag_catalog.create_graph", "ag_catalog.drop_graph", "ag_catalog.ag_graph"},
	"q.cypher.go": {"ag_catalog.agtype"},
}

// TestTheSweptRegionStillHoldsWhatItPolices is the qualification sweep's
// own guard. The sweep judges a region rather than a file — comments,
// error prose and the author's query text are all cut out of it — and
// every cut is one edit away from cutting everything, at which point the
// sweep passes vacuously and the defect it exists to catch ships. So the
// region is held to still carrying the qualified occurrences the
// emission is known to place, named here by their bytes.
func (s *EmissionSuite) TestTheSweptRegionStillHoldsWhatItPolices() {
	files := s.emitReadBatch()
	for path, witnesses := range sweptWitnesses {
		s.Require().Contains(files, path)
		region := s.keepServerText(path, files[path])
		s.Require().NotEmpty(strings.TrimSpace(region), "%s: the swept region is empty", path)
		for _, w := range witnesses {
			s.Require().Contains(region, w,
				"%s: the swept region no longer holds the emission's own %q", path, w)
		}
		s.Require().Positive(s.assertQualified(path, files[path]),
			"%s: the sweep found no AGE identifier in a file that carries SQL", path)
	}
	// Closing the table the other way keeps it from going stale: a file
	// that acquires SQL has to be named above, or it could be emptied out
	// of the region with nothing to notice.
	for path := range files {
		if _, named := sweptWitnesses[path]; named {
			continue
		}
		s.Require().Zero(s.assertQualified(path, files[path]),
			"%s carries an AGE identifier but names no witness", path)
	}
}

// TestTheSweepJudgesGeneratedIdentifiersOnly holds the qualification
// sweep to text the generator chose. An author's query text is copied
// into the emission verbatim (ADR 0005) and an author's parameter name
// reaches an emitted identifier and an emitted map key, so both can put
// AGE's own spellings in front of the sweep — and neither is the
// generator's to qualify. A guard that reddens on them turns a legal
// batch into a build failure.
func (s *EmissionSuite) TestTheSweepJudgesGeneratedIdentifiersOnly() {
	cases := []struct {
		name  string
		param string
		text  string
	}{
		{
			name:  "a parameter name an AGE identifier prefixes",
			param: "agtypeArgs",
			text:  "MATCH (p:Person) RETURN p.name\n",
		},
		{
			name:  "query text naming an AGE identifier",
			param: "id",
			text:  "MATCH (p:Person) WHERE p.agtype = 'v' RETURN p.name\n",
		},
		{
			name:  "both, as the author who hits this writes it",
			param: "agtypeArgs",
			text:  "MATCH (p:Person) WHERE p.agtype = $agtypeArgs RETURN p.name\n",
		},
		{
			name:  "a snake_case parameter name an AGE function prefixes",
			param: "agtype_args",
			text:  "MATCH (p:Person) WHERE p.id = $agtype_args RETURN p.name\n",
		},
		{
			name:  "a parameter named for the family, spelled as AGE spells it",
			param: "agtype_build_map",
			text:  "MATCH (p:Person) WHERE p.id = $agtype_build_map RETURN p.name\n",
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			q := servedQuery
			q.SourceText = tc.text
			q.Validated.Parameters = []resolver.ResolvedParameter{
				{Name: tc.param, Type: resolver.ResolvedProperty{Type: graph.TypeInt}},
			}
			in := s.in
			in.Queries = []codegen.NamedQuery{q}
			files, err := age.New().Generate(in)
			s.Require().NoError(err)
			qualified := 0
			for _, f := range files {
				qualified += s.assertQualified(f.Path, string(f.Contents))
			}
			s.Require().Positive(qualified, "the sweep found no AGE identifier at all")
		})
	}
}

// agCatalogOwned is every ag_catalog-owned spelling that reaches, or
// could reach, this backend's emitted SQL, written as SQL writes it. The
// emission composes the first group today — the type, the record shape's
// column type, the statement wrapper and the graph lifecycle. The
// agtype_* group it does not, and that is exactly why the group is here:
// each is a function ag_catalog owns and the search_path resolves, so
// the first emitter to reach for one must find the sweep already
// waiting. A guard whose reach is the current emission's reach only ever
// catches what is already caught.
//
// The array type appears as agtype[], which is how SQL spells it. Its
// pg_type-internal name _agtype is deliberately absent: nothing composes
// a type name out of pg_type, so no emitter can produce that spelling.
var agCatalogOwned = []string{
	"agtype",
	"agtype[]",
	"agtype_build_map(",
	"agtype_build_list(",
	"agtype_access_operator(",
	"agtype_typecast_numeric(",
	"agtype_in(",
	"agtype_out(",
	"cypher(",
	"create_graph(",
	"drop_graph(",
	"ag_graph",
}

// TestTheSweepRecognisesEveryAgCatalogOwnedSpelling pins the sweep's
// recall. The sweep matches on a word boundary so that a name merely
// beginning with an AGE identifier is a different name, and `_` is a
// word character — so the boundary that keeps agtypeArgs out also keeps
// agtype_build_map out unless the pattern says otherwise. That failure
// is silent: the sweep reports nothing and passes, and an unqualified
// agtype_build_map ships to resolve against whatever search_path the
// caller happens to hold.
//
// So each spelling is planted twice in a file shaped like an emission —
// once bare, where the sweep has to name it, and once qualified, where
// the sweep has to accept it and count it. Narrowing the agtype pattern
// back to \bagtype\b reddens the bare half of every agtype_* row for the
// reason that matters: the sweep does not see the identifier at all.
func (s *EmissionSuite) TestTheSweepRecognisesEveryAgCatalogOwnedSpelling() {
	for _, name := range agCatalogOwned {
		s.Run(name, func() {
			planted := func(prefix string) string {
				return "package p\n\nconst stmt = \"SELECT " + prefix + name + " FROM t\"\n"
			}

			unqualified, qualified := s.sweep("planted.go", planted(""))
			s.Require().Len(unqualified, 1,
				"the sweep does not recognise the unqualified %q", name)
			s.Require().Zero(qualified)

			unqualified, qualified = s.sweep("planted.go", planted("ag_catalog."))
			s.Require().Empty(unqualified,
				"the sweep rejects %q even qualified", name)
			s.Require().Equal(1, qualified,
				"the sweep does not count the qualified %q", name)
		})
	}
}

// notAgCatalogOwned are spellings the sweep must let through: a name that
// merely begins with, ends with or contains an AGE identifier is a
// different name, which ag_catalog neither owns nor resolves. The first
// three are the emitted helpers themselves — Go declarations no parser
// but Go's sees, and names a query parameter can be spelled after, which
// is how $agtypeArgs turned a legal batch into a build failure.
var notAgCatalogOwned = []string{
	"agtypeArgs",
	"agtypeString",
	"agtypeEntity",
	"myagtype",
	"cypherStmt(",
	"create_graphql(",
}

// TestTheSweepPassesNamesAgCatalogDoesNotOwn is the recall table's
// mirror, and it is what holds the word boundaries in place. Every
// exclusion keepServerText applies is a second line of defence for the
// boundaries, so once the author's parameter names leave the region the
// boundaries can be deleted from the pattern with every emission test
// still green — there is nothing left in the region for them to save.
// Planting the near misses directly leaves the pattern answerable for
// its own precision whatever the region happens to hold.
func (s *EmissionSuite) TestTheSweepPassesNamesAgCatalogDoesNotOwn() {
	for _, name := range notAgCatalogOwned {
		s.Run(name, func() {
			body := "package p\n\nconst stmt = \"SELECT " + name + " FROM t\"\n"
			unqualified, qualified := s.sweep("planted.go", body)
			s.Require().Empty(unqualified,
				"the sweep demands a qualifier on %q, which ag_catalog does not own", name)
			s.Require().Zero(qualified,
				"the sweep counts %q as an AGE identifier it accepted", name)
		})
	}
}

// sweep reports body's swept region: the AGE identifiers in it carrying
// no ag_catalog qualifier, each already written out as the failure a
// caller would print, and the count of those that carried one.
//
// It is separate from assertQualified so that the sweep's own recall can
// be tested — an assertion that stops the test cannot be asked whether
// it fired.
func (s *EmissionSuite) sweep(label, body string) (unqualified []string, qualified int) {
	region := s.keepServerText(label, body)
	for _, ident := range ageIdentifiers {
		for _, at := range ident.FindAllStringIndex(region, -1) {
			if qualifier.MatchString(region[:at[0]]) {
				qualified++
				continue
			}
			unqualified = append(unqualified, fmt.Sprintf("%q at offset %d is not schema-qualified: %q",
				region[at[0]:at[1]], at[0], window(region, at[0])))
		}
	}
	return unqualified, qualified
}

// assertQualified requires every AGE identifier in body's swept region
// to carry an ag_catalog qualifier, and returns how many it accepted. A
// caller that gets zero has swept a region proving nothing.
func (s *EmissionSuite) assertQualified(label, body string) int {
	unqualified, qualified := s.sweep(label, body)
	for _, u := range unqualified {
		s.Require().Fail("unqualified AGE identifier", "%s: %s", label, u)
	}
	return qualified
}

// blankComments overwrites every comment with spaces, preserving byte
// offsets so the qualifier lookbehind and the failure window still
// address the original text.
func (s *EmissionSuite) blankComments(label, body string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, label, body, parser.ParseComments|parser.SkipObjectResolution)
	s.Require().NoError(err, "emitted file %s does not parse", label)
	b := []byte(body)
	for _, group := range f.Comments {
		for _, c := range group.List {
			blank(b, fset.Position(c.Pos()).Offset, fset.Position(c.End()).Offset)
		}
	}
	return string(b)
}

// errorConstructors build a value whose string is read by a person.
var errorConstructors = map[string]bool{"fmt.Errorf": true, "fmt.Sprintf": true, "errors.New": true}

// statementComposer is the emitted function a query method hands its
// text to. Its second argument is that text, and the guards below locate
// the author's bytes through it rather than by the const's name, so the
// emission is free to call the const whatever QueryTextConst makes of it.
const statementComposer = "q.cypherStmt"

// argsEncoder is the emitted function a query method hands its bound
// parameters to. Its only argument is the map literal, whose keys are
// the names the author wrote after the dollar sign, so the guards below
// locate those bytes through the call site on the same terms.
const argsEncoder = "agtypeArgs"

// keepServerText reduces an emitted file to the string literals that
// reach a parser as the generator's own: literals stay where they are,
// so a lookbehind still reads the bytes preceding a needle inside its
// own literal and cannot run off into the declaration around it, and
// everything else — syntax, comments, the literals building error text,
// and the author's query text — becomes blanks.
//
// The exclusions are the ways a name can appear in an emitted file
// without being a name the generator chose. A Go declaration called
// after an agtype helper is an identifier no parser but Go's sees; an
// error explaining that a value is not an agtype string has to be free
// to say so; and a query's text and its parameter names are copied
// through verbatim (ADR 0005), so whatever they spell is the author's
// and not this emission's to qualify.
func (s *EmissionSuite) keepServerText(label, body string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, label, body, parser.SkipObjectResolution)
	s.Require().NoError(err, "emitted file %s does not parse", label)
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	verbatim := verbatimAuthorText(f)

	var prose [][2]int
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok && errorConstructors[types.ExprString(call.Fun)] {
			prose = append(prose, [2]int{offset(call.Pos()), offset(call.End())})
		}
		return true
	})

	src := []byte(body)
	kept := []byte(body)
	blank(kept, 0, len(kept))
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || verbatim[lit] {
			return true
		}
		from, to := offset(lit.Pos()), offset(lit.End())
		for _, p := range prose {
			if from >= p[0] && to <= p[1] {
				return true
			}
		}
		copy(kept[from:to], src[from:to])
		return true
	})
	return string(kept)
}

// verbatimAuthorText identifies the literals the emission copies from
// the author rather than composes. There are two: whatever the statement
// composer is handed as its text argument, whether that is the literal
// itself or the package-level const the emission binds it to; and the
// keys of the map literal the args encoder is handed, which are the
// names the author wrote after the dollar sign so that what AGE
// substitutes is what the author wrote.
//
// The parameter names matter for the same reason the query text does and
// no boundary rule reaches them: a GQL name may hold underscores, so an
// author is free to bind $agtype_args, and that spelling is
// indistinguishable from ag_catalog's own snake_case family by shape
// alone. Provenance is the only thing that separates them.
//
// Resolution is by reference from the call site and stops at the file,
// which is where both the const and its only caller are emitted; a name
// neither composer reads stays in the swept region however it is
// spelled.
func verbatimAuthorText(f *ast.File) map[*ast.BasicLit]bool {
	bound := make(map[string]*ast.BasicLit)
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					bound[name.Name] = lit
				}
			}
		}
	}

	out := make(map[*ast.BasicLit]bool)
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch types.ExprString(call.Fun) {
		case statementComposer:
			if len(call.Args) < 2 {
				return true
			}
			switch arg := call.Args[1].(type) {
			case *ast.Ident:
				if lit := bound[arg.Name]; lit != nil {
					out[lit] = true
				}
			case *ast.BasicLit:
				out[arg] = true
			}
		case argsEncoder:
			if len(call.Args) < 1 {
				return true
			}
			m, ok := call.Args[0].(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, elt := range m.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.BasicLit); ok && key.Kind == token.STRING {
					out[key] = true
				}
			}
		}
		return true
	})
	return out
}

// blank overwrites b[from:to] with spaces, leaving newlines in place so
// byte offsets and line numbers both survive.
func blank(b []byte, from, to int) {
	for i := from; i < to; i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}

// window returns a readable slice around an offset for failure output.
func window(body string, at int) string {
	return body[max(at-24, 0):min(at+24, len(body))]
}

// TestSessionInitOrdersSearchPathThenCanary pins the AfterConnect
// contract. AGE emits bare operators resolved through search_path, so
// qualifying every call site is necessary but not sufficient: the canary
// exercises operator resolution and fails the hook, which keeps the
// misconfigured connection out of the pool instead of surfacing at the
// first WHERE clause.
//
// LOAD stays out of the hook. Evaluating the canary calls an AGE C
// function, which is what makes PostgreSQL load the library and run its
// _PG_init; LOAD 'age' adds only a round trip and a privilege
// requirement. Verified against apache/age 1.7.0: a non-superuser
// running it gets "access to library age is not allowed", and an error
// out of AfterConnect discards the connection — so a hook carrying LOAD
// leaves a least-privilege role with a pool that never yields one.
// Comments are blanked first: the emission explains the absence in prose
// that necessarily names the statement.
func (s *EmissionSuite) TestSessionInitOrdersSearchPathThenCanary() {
	graph := s.files["graph.go"]
	s.Require().Contains(graph, "func SessionInit(ctx context.Context, conn *pgx.Conn) error {")
	s.Require().NotContains(s.blankComments("graph.go", graph), "LOAD ",
		"LOAD is superuser-only and the canary already forces the library load")

	path := strings.Index(graph, "set_config('search_path'")
	canary := strings.Index(graph, wantCanary)
	s.Require().NotEqual(-1, path, "graph.go does not set search_path")
	s.Require().NotEqual(-1, canary, "graph.go does not run the pinned canary statement")
	s.Require().Less(path, canary, "the canary runs before search_path is set")

	for _, abort := range wantAborts {
		s.Require().Contains(graph, abort,
			"SessionInit must abort on this failure, not fall through it")
	}
}

// TestSearchPathSurvivesAnEmptySetting pins the empty-search_path arm: a
// role whose search_path is the empty string would otherwise produce a
// trailing empty list element, which PostgreSQL rejects as invalid list
// syntax — failing SessionInit on exactly the connection the statement
// exists to repair.
func (s *EmissionSuite) TestSearchPathSurvivesAnEmptySetting() {
	s.Require().Contains(s.files["graph.go"], wantSearchPath)
}

// TestNewBindsTheGraph pins the handle contract: the graph is a
// construction argument held on Queries, so no call site can name a
// different one. The lifecycle helpers take only a context.
func (s *EmissionSuite) TestNewBindsTheGraph() {
	db := s.files["db.go"]
	s.Require().Contains(db, "func New(db DBTX, graph string) *Queries {")
	s.Require().Contains(db, "graph string\n}")
	s.Require().Contains(db, "return &Queries{queries: queries{db: db, graph: graph}}")
	// WithTx must carry the binding across, or the transactional handle
	// would silently address a different graph.
	s.Require().Contains(db, "return &Queries{queries: queries{db: tx, graph: q.graph}}")

	graph := s.files["graph.go"]
	for _, name := range []string{"EnsureGraph", "DropGraph"} {
		s.Require().Contains(graph, "func (q *Queries) "+name+"(ctx context.Context) error {")
	}
	s.Require().Contains(graph, "q.db.Exec(ctx, stmt, graph)")
}

// TestGraphNameIsAgesToJudge fences the emission against a client-side
// graph-name check. AGE's grammar is AGE's to change: a copy of it here
// is correct only against the version it was read from, and a client
// that refuses a name the server would take has to be upgraded in step
// with the server to stay correct.
//
// The length axis is characterisable — apache/age 1.7.0 takes a name of
// at least 2 characters and at least 3 bytes, which is why 中中 and ér
// pass while 中 and ab do not — so the argument is not that the rule
// cannot be learnt. It is that knowing it today is not owning it.
//
// New therefore binds and nothing else, and EnsureGraph reports AGE's
// own verdict with the value and its origin attached, since AGE's
// message carries neither.
func (s *EmissionSuite) TestGraphNameIsAgesToJudge() {
	db := s.files["db.go"]
	s.Require().Contains(db, "func New(db DBTX, graph string) *Queries {\n\treturn &Queries{queries: queries{db: db, graph: graph}}\n}")
	s.Require().NotContains(db, "panic(")

	s.Require().Contains(s.files["graph.go"],
		`fmt.Errorf("gqlc: ensure graph %q (the name bound at New): %w", graph, err)`)
}

// nameBearers are the emitted functions that put the bound graph name
// into a statement the server parses.
var nameBearers = []string{"EnsureGraph", "DropGraph", "cypherStmt"}

// TestTheGraphNameReachesTheServerThroughOneCheck pins the one
// graph-name constraint this backend owns, and pins it to a single site.
// PostgreSQL's name type holds NAMEDATALEN-1 = 63 bytes and drops the
// rest without raising, so two handles whose names differ only past byte
// 63 address one graph: the second EnsureGraph finds it already there
// and reports nothing, a read returns the first's rows, and either
// handle's DropGraph cascades away the other's labels and data.
//
// Three functions carry the name to the server and each acquired the
// check separately would be three copies to keep in step; the third
// reaches it on the read path, where a name that has drifted past a
// stale copy of the limit returns another graph's rows. So the field is
// read by one accessor holding the check, plus the propagators that copy
// the binding onto a derived handle without carrying it to the server,
// and the bearers take what the accessor returns.
//
// The name lives on one struct now — the core both handles embed — but
// both receivers are still swept. Tx no longer copies it, yet tx.graph
// stays a legal selector by promotion, so a future method that carried
// it to the server off a *Tx receiver would compile; the tx sweep is
// what says none does.
func (s *EmissionSuite) TestTheGraphNameReachesTheServerThroughOneCheck() {
	files := s.emitReadBatch()

	db := files["db.go"]
	s.Require().Contains(db, "const maxGraphNameBytes = 63")
	s.Require().Contains(db,
		"func (q *queries) boundGraph() (string, error) {\n\tif len(q.graph) > maxGraphNameBytes {")

	var readers, txReaders, bearers []string
	for path, body := range files {
		readers = append(readers, s.functionsSelecting(path, body, "q", "graph")...)
		txReaders = append(txReaders, s.functionsSelecting(path, body, "tx", "graph")...)
		bearers = append(bearers, s.functionsCalling(path, body, "q", "boundGraph")...)
	}
	s.Require().ElementsMatch([]string{"WithTx", "Begin", "boundGraph"}, readers,
		"the graph field is readable outside the accessor that checks it and the propagators that only copy it")
	s.Require().Empty(txReaders,
		"a *Tx method reads the promoted graph name directly instead of taking it from boundGraph")
	s.Require().ElementsMatch(nameBearers, bearers,
		"a function carrying the name to the server does not take it from boundGraph")
}

// emitReadBatch generates the skeleton schema carrying one served query,
// so the sweeps above see the read path's files as well as the lifecycle
// ones. Keyed by path, as SetupSuite keys the query-free emission.
func (s *EmissionSuite) emitReadBatch() map[string]string {
	in := s.in
	in.Queries = []codegen.NamedQuery{servedQuery}
	files, err := age.New().Generate(in)
	s.Require().NoError(err)
	out := make(map[string]string, len(files))
	for _, f := range files {
		out[f.Path] = string(f.Contents)
	}
	return out
}

// functionsSelecting names every emitted function whose body reads the
// field recv.field.
func (s *EmissionSuite) functionsSelecting(label, body, recv, field string) []string {
	return s.functionsWhere(label, body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != field {
			return false
		}
		x, ok := sel.X.(*ast.Ident)
		return ok && x.Name == recv
	})
}

// functionsCalling names every emitted function whose body calls the
// method recv.method.
func (s *EmissionSuite) functionsCalling(label, body, recv, method string) []string {
	return s.functionsWhere(label, body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != method {
			return false
		}
		x, ok := sel.X.(*ast.Ident)
		return ok && x.Name == recv
	})
}

// functionsWhere names every emitted function holding a node match
// accepts, once per function however many it holds.
func (s *EmissionSuite) functionsWhere(label, body string, match func(ast.Node) bool) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, label, body, parser.SkipObjectResolution)
	s.Require().NoError(err, "emitted file %s does not parse", label)
	var found []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if n == nil || !match(n) {
				return true
			}
			if len(found) == 0 || found[len(found)-1] != fn.Name.Name {
				found = append(found, fn.Name.Name)
			}
			return true
		})
	}
	return found
}

// TestNoQueryArgumentReachesTheStatementText pins the property that
// makes a bound parameter a value rather than syntax. AGE takes a query
// argument only as a placeholder — it rejects every composed third
// argument to cypher() — so a statement assembled around a caller's
// value is not merely unsafe here, it is a statement the server refuses.
//
// The fence is structural rather than a search for the shapes that go
// wrong: the statement a query method runs is a name, the arguments it
// hands the composer are names and literals, and the composer is the one
// emitted function that builds a string by concatenation. Nothing a
// caller supplies can travel along any of those.
func (s *EmissionSuite) TestNoQueryArgumentReachesTheStatementText() {
	files := s.emitReadBatch()

	var concatenating []string
	for path, body := range files {
		s.Require().NotContains(body, "fmt.Sprintf", "%s composes text a caller can reach", path)

		for _, call := range s.calls(path, body) {
			var args []ast.Expr
			switch s.callee(call) {
			case "q.db.Query", "q.db.Exec", "conn.Exec", "conn.QueryRow":
				// The statement is the argument after the context.
				args = call.Args[1:2]
			case "q.cypherStmt":
				args = call.Args
			default:
				continue
			}
			for i, arg := range args {
				switch arg.(type) {
				case *ast.Ident, *ast.BasicLit:
				default:
					s.Failf("composed statement argument",
						"%s: %s argument %d is %T, so the text it runs is assembled at the call site",
						path, s.callee(call), i, arg)
				}
			}
		}
		concatenating = append(concatenating, s.functionsWhere(path, body, func(n ast.Node) bool {
			bin, ok := n.(*ast.BinaryExpr)
			if !ok || bin.Op != token.ADD {
				return false
			}
			return isStringLit(bin.X) || isStringLit(bin.Y)
		})...)
	}
	s.Require().ElementsMatch([]string{"cypherStmt"}, concatenating,
		"a function outside the statement composer builds text by concatenation")
}

// isStringLit reports whether e is a string literal, which is as much as
// a syntax tree can say about the type of an operand.
func isStringLit(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// calls returns every call expression in an emitted file.
func (s *EmissionSuite) calls(label, body string) []*ast.CallExpr {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, label, body, parser.SkipObjectResolution)
	s.Require().NoError(err, "emitted file %s does not parse", label)
	var out []*ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			out = append(out, call)
		}
		return true
	})
	return out
}

// callee is the source spelling of what a call invokes.
func (s *EmissionSuite) callee(call *ast.CallExpr) string {
	return types.ExprString(call.Fun)
}

// TestOneMethodsReturnMatchableSentinels pins the two outcomes a :one
// method has beyond its row, and pins them to package-level values: a
// caller distinguishes "no such person" from a transport failure by
// identity, and text carried in a wrapped error is not something
// errors.Is can compare.
//
// They are emitted only for a batch that has a :one query, because an
// exported sentinel no method in the package can return is a promise
// with nothing behind it.
func (s *EmissionSuite) TestOneMethodsReturnMatchableSentinels() {
	sentinels := map[string]string{
		"ErrNoRows":          `var ErrNoRows = errors.New("gqlc: no rows in result set")`,
		"ErrMultipleResults": `var ErrMultipleResults = errors.New("gqlc: multiple rows in :one result set")`,
	}

	one := servedQuery
	one.Name = "Solo"
	one.Cardinality = codegen.CardinalityOne
	in := s.in
	in.Queries = []codegen.NamedQuery{one}
	files, err := age.New().Generate(in)
	s.Require().NoError(err)

	byPath := make(map[string]string, len(files))
	for _, f := range files {
		byPath[f.Path] = string(f.Contents)
	}
	for name, decl := range sentinels {
		s.Require().Contains(byPath["db.go"], decl)
		s.Require().Contains(byPath["q.cypher.go"], "return SoloRow{}, "+name,
			"the :one method never returns %s", name)
	}

	// servedQuery is :many, so its emission is the negative case.
	for name := range sentinels {
		s.Require().NotContains(s.emitReadBatch()["db.go"], name)
	}
}

// TestGraphLifecycleIsOffTheQuerierInterfaces pins the exclusion: the
// lifecycle helpers are declared by this backend alone, so listing them
// on Querier would make the interface a moving target across backends.
func (s *EmissionSuite) TestGraphLifecycleIsOffTheQuerierInterfaces() {
	querier := s.files["querier.go"]
	for _, name := range []string{"EnsureGraph", "DropGraph", "SessionInit"} {
		s.Require().Contains(s.files["graph.go"], name)
		s.Require().NotContains(querier, name)
	}
	s.Require().Contains(querier, "var _ Querier = (*Queries)(nil)")
}

// TestPgxImportsAreMajorOnly pins the import spelling: pgx module paths
// carry the major only, so a consuming module's own pgx minor is never
// contradicted by generated code.
func (s *EmissionSuite) TestPgxImportsAreMajorOnly() {
	pgxPath := regexp.MustCompile(`github\.com/jackc/pgx/[^"\s]*`)
	found := 0
	for path, body := range s.files {
		for _, m := range pgxPath.FindAllString(body, -1) {
			found++
			s.Require().Regexp(`^github\.com/jackc/pgx/v5(/[a-z0-9]+)*$`, m, "file %s", path)
		}
	}
	// found counts matches, so Positive is the assertion: a sweep that
	// matched nothing passes every Regexp above it vacuously.
	s.Require().Positive(found, "no pgx import in the emitted package")
}

// assertPackage checks every emitted file's package clause matches want.
func (s *EmissionSuite) assertPackage(files []codegen.File, want string) {
	for _, f := range files {
		lines := bytes.SplitN(f.Contents, []byte{'\n'}, 4)
		s.Require().GreaterOrEqual(len(lines), 3, "file %s too short for header + package", f.Path)
		// Line 2 is the mandatory blank; line 3 is the package clause.
		s.Require().Equal([]byte("package "+want), lines[2],
			"file %s has wrong package clause: %q", f.Path, lines[2])
	}
}
