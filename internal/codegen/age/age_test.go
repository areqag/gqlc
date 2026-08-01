package age_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
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
// the Querier interfaces, and an empty models file.
func (s *EmissionSuite) TestFileSet() {
	paths := make([]string, 0, len(s.files))
	for p := range s.files {
		paths = append(paths, p)
	}
	s.Require().ElementsMatch([]string{"db.go", "graph.go", "models.go", "querier.go"}, paths)
	s.Require().Equal(codegen.Header()+"package "+skeletonPackage+"\n", s.files["models.go"])
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

// TestRejectsQueriesItCannotServe pins the capability gate. C0 emits no
// query methods, so generating for a batch that carries queries would
// hand the author a Querier that silently omits them; the error names
// every dropped query so they can see which.
func (s *EmissionSuite) TestRejectsQueriesItCannotServe() {
	query := func(name string) codegen.NamedQuery {
		return codegen.NamedQuery{
			Name:        name,
			Cardinality: codegen.CardinalityExec,
			SourceFile:  "q.cypher",
			SourceText:  "MATCH (n) DELETE n",
			Validated:   resolver.ValidatedQuery{Statement: resolver.StatementWrite},
		}
	}
	cases := []struct {
		name      string
		queries   []codegen.NamedQuery
		wantSub   string
		wantError bool
	}{
		{
			name:      "no queries generates",
			queries:   nil,
			wantError: false,
		},
		{
			name:      "one query is rejected and named",
			queries:   []codegen.NamedQuery{query("Wipe")},
			wantSub:   "1 query would be dropped: Wipe",
			wantError: true,
		},
		{
			name:      "every query is named, in batch order",
			queries:   []codegen.NamedQuery{query("Wipe"), query("Purge"), query("Reset")},
			wantSub:   "3 queries would be dropped: Wipe, Purge, Reset",
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

// ageIdentifiers are the extension-owned names that must never appear
// unqualified. cypher( has no emission until the read path lands; it is
// swept now so the fence is already standing when it does.
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
// schema in the corpus, and every golden file committed for this target.
// The second is what makes the cypher( needle bite as later stages enrol
// query-bearing fixtures — the first cannot, because a batch carrying
// queries has no C0 emission at all.
//
// Comments are blanked before the sweep. Only an identifier the server
// parses can misresolve, and generated prose has to be free to name the
// types it is explaining.
func (s *EmissionSuite) TestEveryAgeIdentifierIsSchemaQualified() {
	schemas, err := filepath.Glob(filepath.Join(corpusRoot, "valid", "*", "schema.gql"))
	s.Require().NoError(err)
	s.Require().Greater(len(schemas), 10, "corpus glob found too few schemas")

	swept := 0
	for _, path := range schemas {
		files, err := age.New().Generate(s.inputFrom(path))
		// A schema whose widths this backend cannot carry is rejected by
		// the type table, so there is nothing to sweep.
		if err != nil {
			continue
		}
		for _, f := range files {
			s.assertQualified(filepath.Base(filepath.Dir(path))+"/"+f.Path, string(f.Contents))
		}
		swept++
	}
	s.Require().Greater(swept, 10, "too few corpus schemas produced emission to sweep")

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
	body = s.blankComments(label, body)
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
			for i := fset.Position(c.Pos()).Offset; i < fset.Position(c.End()).Offset; i++ {
				if b[i] != '\n' {
					b[i] = ' '
				}
			}
		}
	}
	return string(b)
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
	s.Require().Contains(graph, "q.db.Exec(ctx, stmt, q.graph)")
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
		`fmt.Errorf("gqlc: ensure graph %q (the name bound at New): %w", q.graph, err)`)
}

// TestLifecycleRejectsANameTheCastWouldShorten pins the one graph-name
// constraint this backend does own. $1::name is a cast this emission
// chose, and PostgreSQL's name type holds NAMEDATALEN-1 = 63 bytes and
// drops the rest without raising. Two handles whose names differ only
// past byte 63 then address one graph: the second EnsureGraph finds it
// already there and reports nothing, and either handle's DropGraph
// cascades away the other's labels and data. The limit is a static
// property of the type, so unlike AGE's validity grammar it is knowable
// here and cannot drift underneath us.
func (s *EmissionSuite) TestLifecycleRejectsANameTheCastWouldShorten() {
	graph := s.files["graph.go"]
	s.Require().Contains(graph, "const maxGraphNameBytes = 63")
	s.Require().Contains(graph, "if len(graph) > maxGraphNameBytes {")

	// Ahead of the statement in both helpers: a guard on one of them
	// still leaves the other free to alias.
	for _, name := range []string{"EnsureGraph", "DropGraph"} {
		s.Require().Contains(graph,
			"func (q *Queries) "+name+"(ctx context.Context) error {\n\tif err := checkGraphName(q.graph); err != nil {\n\t\treturn err\n\t}\n\tconst stmt = ",
			"%s must reject an over-length name before it reaches the ::name cast", name)
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
