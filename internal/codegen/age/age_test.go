package age_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
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
	node := moved("Whole", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "p", Type: resolver.ResolvedNode{Labels: graph.LabelSetKey(personLabel)},
		}}
	})
	temporal := moved("When", func(q *codegen.NamedQuery) {
		q.Validated.Columns = []resolver.Column{{
			Name: "t", Type: resolver.ResolvedTemporal{Kind: resolver.TemporalDate},
		}}
	})
	list := moved("Tags", func(q *codegen.NamedQuery) {
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

	cases := []struct {
		name      string
		queries   []codegen.NamedQuery
		wantSub   string
		wantError bool
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
			name:      "an exec binding a list parameter is dropped",
			queries:   []codegen.NamedQuery{execListParam},
			wantSub:   `1 query would be dropped: Batch (parameter $ids is a list)`,
			wantError: true,
		},
		{
			name:    "a whole-entity column generates",
			queries: []codegen.NamedQuery{node},
		},
		{
			name:      "a temporal column is dropped",
			queries:   []codegen.NamedQuery{temporal},
			wantSub:   `1 query would be dropped: When (column "t" projects temporal(date))`,
			wantError: true,
		},
		{
			name:      "a list column is dropped",
			queries:   []codegen.NamedQuery{list},
			wantSub:   `1 query would be dropped: Tags (column "t" projects list)`,
			wantError: true,
		},
		{
			name:      "a map column is dropped",
			queries:   []codegen.NamedQuery{mapCol},
			wantSub:   `1 query would be dropped: Bag (column "m" projects scalar(map))`,
			wantError: true,
		},
		{
			name:      "an unresolved column is dropped",
			queries:   []codegen.NamedQuery{unknown},
			wantSub:   `1 query would be dropped: Opaque (column "u" projects unknown)`,
			wantError: true,
		},
		{
			name:      "a list parameter is dropped",
			queries:   []codegen.NamedQuery{listParam},
			wantSub:   `1 query would be dropped: Batch (parameter $ids is a list)`,
			wantError: true,
		},
		{
			name:      "every dropped query is named, in batch order",
			queries:   []codegen.NamedQuery{execListParam, temporal, list},
			wantSub:   "3 queries would be dropped: Batch (parameter $ids is a list), When (column \"t\" projects temporal(date)), Tags (column \"t\" projects list)",
			wantError: true,
		},
		{
			name:      "a served query alongside a dropped one still fails",
			queries:   []codegen.NamedQuery{served, temporal},
			wantSub:   `1 query would be dropped: When (column "t" projects temporal(date))`,
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
			s.Require().ErrorIs(err, age.ErrUnsupportedQuery)
			s.Require().ErrorContains(err, tc.wantSub)
		})
	}
}

// TestRejectsMultiLabelSchema pins the schema gate. AGE stamps exactly
// one label on a vertex or an edge and its parser has no syntax for a
// second, so an entity the schema keys on two labels names a shape no
// graph this backend can address ever holds. Rejecting the schema is
// what stops the emission producing a struct and a decoder whose label
// check nothing could ever satisfy.
//
// The gate is on the schema's whole entity table rather than on the
// columns a batch projects, which is why a query-free batch fails too.
func (s *EmissionSuite) TestRejectsMultiLabelSchema() {
	in := s.inputFrom(filepath.Join(corpusRoot, "valid", "entity_multi_label_named", "schema.gql"))
	files, err := age.New().Generate(in)
	s.Require().Error(err)
	s.Require().Nil(files, "a rejected schema must not return a partial file set")
	s.Require().ErrorIs(err, age.ErrUnsupportedSchema)
	s.Require().ErrorContains(err, "1 type cannot be represented: PersonEmployee (Employee&Person)")
}

// ageIdentifiers are the extension-owned names that must never appear
// unqualified.
var ageIdentifiers = []string{"agtype", "cypher(", "create_graph", "drop_graph", "ag_graph"}

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
			for _, f := range files {
				s.assertQualified(fixture+"/"+f.Path, string(f.Contents))
			}
			swept++
		}
	}
	s.Require().Greater(swept, 20, "too few corpus schemas produced emission to sweep")

	goldens, err := filepath.Glob(filepath.Join(corpusRoot, "valid", "*", "golden", ageTarget, "*.go"))
	s.Require().NoError(err)
	s.Require().NotEmpty(goldens, "no golden files committed for target %s", ageTarget)
	for _, path := range goldens {
		body, err := os.ReadFile(path)
		s.Require().NoError(err)
		s.assertQualified(path, string(body))
	}
}

func (s *EmissionSuite) assertQualified(label, body string) {
	body = s.keepServerText(label, body)
	hay := strings.ToLower(body)
	for _, ident := range ageIdentifiers {
		for off := 0; ; {
			i := strings.Index(hay[off:], ident)
			if i < 0 {
				break
			}
			at := off + i
			s.Require().Truef(qualifier.MatchString(body[:at]),
				"%s: %q at offset %d is not schema-qualified: %q", label, ident, at, window(body, at))
			off = at + len(ident)
		}
	}
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

// keepServerText reduces an emitted file to the string literals that
// reach a parser: literals stay where they are, so a lookbehind still
// reads the bytes preceding a needle inside its own literal and cannot
// run off into the declaration around it, and everything else — syntax,
// comments, and the literals building error text — becomes blanks.
//
// The two exclusions are the two ways a name can appear without being a
// name. A Go declaration called after an agtype helper is an identifier
// no parser but Go's sees, and an error explaining that a value is not
// an agtype string has to be free to say so.
func (s *EmissionSuite) keepServerText(label, body string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, label, body, parser.SkipObjectResolution)
	s.Require().NoError(err, "emitted file %s does not parse", label)
	offset := func(p token.Pos) int { return fset.Position(p).Offset }

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
		if !ok || lit.Kind != token.STRING {
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
	s.Require().Contains(db, "return &Queries{db: db, graph: graph}")
	// WithTx must carry the binding across, or the transactional handle
	// would silently address a different graph.
	s.Require().Contains(db, "return &Queries{db: tx, graph: q.graph}")

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
	s.Require().Contains(db, "func New(db DBTX, graph string) *Queries {\n\treturn &Queries{db: db, graph: graph}\n}")
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
// readable from one accessor, that accessor holds the check, and the
// bearers take what it returns.
func (s *EmissionSuite) TestTheGraphNameReachesTheServerThroughOneCheck() {
	files := s.emitReadBatch()

	db := files["db.go"]
	s.Require().Contains(db, "const maxGraphNameBytes = 63")
	s.Require().Contains(db,
		"func (q *Queries) boundGraph() (string, error) {\n\tif len(q.graph) > maxGraphNameBytes {")

	var readers, bearers []string
	for path, body := range files {
		readers = append(readers, s.functionsSelecting(path, body, "q", "graph")...)
		bearers = append(bearers, s.functionsCalling(path, body, "q", "boundGraph")...)
	}
	s.Require().ElementsMatch([]string{"WithTx", "boundGraph"}, readers,
		"the graph field is readable outside the accessor that checks it")
	s.Require().ElementsMatch(nameBearers, bearers,
		"a function carrying the name to the server does not take it from boundGraph")
}

// emitReadBatch generates the skeleton schema carrying one served query,
// so the sweeps above see the read path's files as well as the lifecycle
// ones. Keyed by path, as SetupSuite keys the query-free emission.
func (s *EmissionSuite) emitReadBatch(opts ...age.Option) map[string]string {
	in := s.in
	in.Queries = []codegen.NamedQuery{servedQuery}
	files, err := age.New(opts...).Generate(in)
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

// TestNoEmittedNameTakesAQueryParameterName pins the one identifier an
// emitted method's signature does not choose. The single-parameter form
// names its argument after the parameter the author wrote, so anything
// the method resolves under that name resolves to the caller's value
// instead. Declaring a name is not what puts it at risk — resolving it
// is, and a body resolves the package-level query-text const without
// ever declaring it.
//
// Neither half is reliably a compile error. A shadowed body local
// against a STRING property lets the composition assign the SQL text
// over the argument; a shadowed query-text const is worse, because the
// argument silently becomes the statement — $<bare>QueryText makes
// Method(ctx, "MATCH (n) DETACH DELETE n") run that text, with no
// concatenation anywhere to find.
//
// The candidate names are every identifier the emission mentions, read
// off the syntax tree rather than listed here, so a name the emitter
// starts using later is covered without anyone remembering to add it.
// Each is fed back as the parameter's own name and the emission has to
// come out intact.
func (s *EmissionSuite) TestNoEmittedNameTakesAQueryParameterName() {
	candidates := s.identsOf(s.emitParamBatch("id"))
	s.Require().NotEmpty(candidates, "the emission mentions no identifiers to check")

	for _, name := range candidates {
		s.Run(name, func() {
			body := s.emitParamBatch(name)

			// The surface cannot move to buy the emission room. Only
			// names the mangle leaves alone can be compared directly:
			// it splits on underscores and capitalises, so those are
			// the ones a query text can put in scope verbatim.
			if !strings.Contains(name, "_") && name == codegen.LowerFirstRune(name) {
				for _, local := range s.paramLocalsOf(body) {
					s.Require().Equal(name, local,
						"the signature must still name its argument after the query's parameter")
				}
			}
			s.Require().NotContains(s.bodyLocalsOf(body), name,
				"a body local shadows the caller's argument")
			s.requireNothingDeclaredIsCaptured(body)
		})
	}
}

// requireNothingDeclaredIsCaptured holds every package-level name the
// emitted file declares to being resolvable from some method that needs
// it. Capture has one signature and this is it: the const stays in the
// file, the body still reads a name spelled the same way, and that name
// now resolves to the parameter — leaving the declaration referenced by
// nobody. Asserting reachability rather than comparing name lists is
// what keeps this closed, because it never has to know what the emitter
// chose to call anything.
//
// Names the file does not declare are out of scope here and tracked by
// gqlc-ni66: fmt, agtypeArgs and the rest arrive from an import or
// another file of the package, and capturing one is a compile failure of
// the generated package — a string has no method Errorf and cannot be
// called. The query-text const is the only silent one, because it is
// string-typed and lands where a string is expected.
func (s *EmissionSuite) requireNothingDeclaredIsCaptured(body string) {
	file := s.parseEmission(body)

	var free []map[string]bool
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			free = append(free, freeIdents(fn))
		}
	}

	for _, name := range packageDecls(file) {
		reachable := false
		for _, f := range free {
			if f[name] {
				reachable = true
				break
			}
		}
		s.Require().True(reachable,
			"package-level %q is declared but no method resolves it: the caller's argument captured it", name)
	}
}

// freeIdents names the identifiers a function resolves outside itself —
// every identifier it mentions, less every name it binds. Flat rather
// than block-scoped, which errs towards calling a name bound: an
// emission that captures one therefore fails rather than slips through.
func freeIdents(fn *ast.FuncDecl) map[string]bool {
	bound := make(map[string]bool)
	for _, l := range []*ast.FieldList{fn.Recv, fn.Type.Params, fn.Type.Results} {
		if l == nil {
			continue
		}
		for _, f := range l.List {
			for _, n := range f.Names {
				bound[n.Name] = true
			}
		}
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		for _, id := range declaredIdents(n) {
			bound[id.Name] = true
		}
		return true
	})

	free := make(map[string]bool)
	for _, name := range referencedIdents(fn) {
		if !bound[name] {
			free[name] = true
		}
	}
	return free
}

// referencedIdents names every identifier a node mentions in a position
// where scope resolution applies. Selector suffixes and struct-literal
// keys are excluded: those resolve against a type, not against the
// scope the parameter is bound in, so no argument name can capture them.
func referencedIdents(n ast.Node) []string {
	var out []string
	ast.Inspect(n, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.SelectorExpr:
			ast.Inspect(e.X, func(inner ast.Node) bool {
				if id, ok := inner.(*ast.Ident); ok {
					out = append(out, id.Name)
				}
				return true
			})
			return false
		case *ast.KeyValueExpr:
			ast.Inspect(e.Value, func(inner ast.Node) bool {
				if id, ok := inner.(*ast.Ident); ok {
					out = append(out, id.Name)
				}
				return true
			})
			return false
		case *ast.Ident:
			out = append(out, e.Name)
		}
		return true
	})
	return out
}

// packageDecls names what an emitted file declares at package level:
// the query-text consts, and the Params and Row structs. Read off the
// file so a declaration the emitter adds later is held by the same
// assertion.
func packageDecls(file *ast.File) []string {
	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok == token.IMPORT {
			continue
		}
		for _, spec := range gen.Specs {
			switch sp := spec.(type) {
			case *ast.ValueSpec:
				for _, n := range sp.Names {
					if n.Name != "_" {
						out = append(out, n.Name)
					}
				}
			case *ast.TypeSpec:
				out = append(out, sp.Name.Name)
			}
		}
	}
	return out
}

// identsOf names every identifier an emitted file mentions, declared or
// referenced, deduplicated and ordered. This is the candidate set the
// shadowing sweep draws from, and it is deliberately the widest one the
// syntax tree offers rather than a list of names anyone thought of.
//
// The blank identifier is not a candidate: a query cannot usefully be
// written against it and generation fails on $_ with a gofmt error that
// names the wrong thing, which is gqlc-ni66's.
func (s *EmissionSuite) identsOf(body string) []string {
	file := s.parseEmission(body)
	seen := make(map[string]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name != "_" {
			seen[id.Name] = true
		}
		return true
	})
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// paramLocalsOf names the argument each emitted method takes after ctx.
// The batch binds one parameter per query, so every method carries
// exactly the receiver, ctx and that argument.
func (s *EmissionSuite) paramLocalsOf(body string) []string {
	var out []string
	for _, decl := range s.parseEmission(body).Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		params := fn.Type.Params.List
		s.Require().Len(params, 2, "method %s should take ctx and one argument", fn.Name.Name)
		s.Require().Len(params[1].Names, 1, "method %s should name its argument once", fn.Name.Name)
		out = append(out, params[1].Names[0].Name)
	}
	return out
}

// parseEmission parses an emitted file, which must always parse.
func (s *EmissionSuite) parseEmission(body string) *ast.File {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "q.cypher.go", body, parser.SkipObjectResolution)
	s.Require().NoError(err, "the emitted file does not parse")
	return f
}

// emitParamBatch emits one cypher file whose every query binds a single
// string parameter under the given name. All three cardinalities are in
// it, and the :one query's column is a nullable narrow width, so the
// batch reaches the composition, the cursor walk, the scan, the null
// branch and the narrowing conversion — every place a body declares
// something.
func (s *EmissionSuite) emitParamBatch(param string) string {
	bind := []resolver.ResolvedParameter{
		{Name: param, Type: resolver.ResolvedProperty{Type: graph.TypeString}},
	}

	many := readQuery("Many", scalarColumn("p.name", graph.TypeString))
	many.Validated.Parameters = bind

	one := readQuery("One", resolver.Column{
		Name: "p.height",
		Type: resolver.ResolvedProperty{Type: graph.TypeFloat32, Nullable: true},
	})
	one.Cardinality = codegen.CardinalityOne
	one.Validated.Parameters = bind

	purge := execQuery("Purge")
	purge.Validated.Parameters = bind

	in := s.in
	in.Queries = []codegen.NamedQuery{many, one, purge}
	files, err := age.New().Generate(in)
	s.Require().NoError(err)
	for _, f := range files {
		if f.Path == "q.cypher.go" {
			return string(f.Contents)
		}
	}
	s.Require().Fail("no q.cypher.go in the emission")
	return ""
}

// bodyLocalsOf names every identifier the method bodies in an emitted
// file declare, deduplicated and ordered. The blank identifier is not
// one: it binds nothing and cannot collide.
func (s *EmissionSuite) bodyLocalsOf(body string) []string {
	f := s.parseEmission(body)

	seen := make(map[string]bool)
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			for _, id := range declaredIdents(n) {
				if id.Name != "_" {
					seen[id.Name] = true
				}
			}
			return true
		})
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// declaredIdents returns the identifiers a statement binds. Short
// variable declarations, var declarations and range clauses are the
// whole of what an emitted body uses to introduce a name. Binding is
// only half of what a parameter can capture, though — see
// referencedIdents for the other half.
func declaredIdents(n ast.Node) []*ast.Ident {
	var out []*ast.Ident
	switch stmt := n.(type) {
	case *ast.AssignStmt:
		if stmt.Tok != token.DEFINE {
			return nil
		}
		for _, lhs := range stmt.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				out = append(out, id)
			}
		}
	case *ast.ValueSpec:
		out = append(out, stmt.Names...)
	case *ast.RangeStmt:
		if stmt.Tok != token.DEFINE {
			return nil
		}
		for _, e := range []ast.Expr{stmt.Key, stmt.Value} {
			if id, ok := e.(*ast.Ident); ok {
				out = append(out, id)
			}
		}
	}
	return out
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
