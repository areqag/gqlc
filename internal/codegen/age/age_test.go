package age_test

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/schema/gql"
)

const (
	// skeletonSchema is the golden corpus's minimal valid input: one node
	// type and no queries.
	skeletonSchema = "../../../test/data/codegen/valid/skeleton/schema.gql"
	// skeletonPackage is the name Schema.Name derivation produces for
	// that schema's `CREATE PROPERTY GRAPH TYPE Skeleton`.
	skeletonPackage = "skeleton"
)

// EmissionSuite pins this backend's C0 emission contract: the file set,
// the construction options, and the four properties the generated text
// must hold for AGE to work at all — every AGE identifier schema-
// qualified, the search_path canary ordered ahead of first use, the
// graph lifecycle off the Querier interfaces, and pgx named by major
// only. The fixture-driven golden corpus lives in
// internal/codegen/conformance.
type EmissionSuite struct {
	suite.Suite

	in    codegen.Input
	files map[string]string
}

func TestEmissionSuite(t *testing.T) {
	suite.Run(t, new(EmissionSuite))
}

func (s *EmissionSuite) SetupSuite() {
	src, err := os.ReadFile(skeletonSchema)
	s.Require().NoError(err)
	sch, err := gql.New().Parse(bytes.NewReader(src))
	s.Require().NoError(err)
	s.in = codegen.Input{Schema: sch}

	files, err := age.New().Generate(s.in)
	s.Require().NoError(err)
	s.files = make(map[string]string, len(files))
	for _, f := range files {
		s.files[f.Path] = string(f.Contents)
	}
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

// TestEveryAgeIdentifierIsSchemaQualified sweeps the emitted bytes for
// AGE's extension-owned identifiers and requires each to carry the
// ag_catalog. prefix. An unqualified one compiles and then resolves
// against whatever the caller's search_path happens to hold.
func (s *EmissionSuite) TestEveryAgeIdentifierIsSchemaQualified() {
	const qualifier = "ag_catalog."
	for _, ident := range []string{"agtype", "cypher(", "create_graph", "drop_graph", "ag_graph"} {
		for path, body := range s.files {
			for off := 0; ; {
				i := strings.Index(body[off:], ident)
				if i < 0 {
					break
				}
				at := off + i
				s.Require().GreaterOrEqual(at, len(qualifier),
					"%s: %q at offset %d is unqualified", path, ident, at)
				s.Require().Equal(qualifier, body[at-len(qualifier):at],
					"%s: %q at offset %d is unqualified", path, ident, at)
				off = at + len(ident)
			}
		}
	}
}

// TestSessionInitOrdersLoadSearchPathCanary pins the AfterConnect
// contract. AGE emits bare operators resolved through search_path, so
// loading the extension and qualifying every call site is necessary but
// not sufficient: the canary exercises operator resolution and fails the
// hook, which keeps the misconfigured connection out of the pool instead
// of surfacing at the first WHERE clause.
func (s *EmissionSuite) TestSessionInitOrdersLoadSearchPathCanary() {
	graph := s.files["graph.go"]
	s.Require().Contains(graph, "func SessionInit(ctx context.Context, conn *pgx.Conn) error {")

	load := strings.Index(graph, "LOAD 'age'")
	path := strings.Index(graph, "set_config('search_path'")
	canary := strings.Index(graph, "::ag_catalog.agtype")
	s.Require().Positive(load, "graph.go does not LOAD the extension")
	s.Require().Positive(path, "graph.go does not set search_path")
	s.Require().Positive(canary, "graph.go does not exercise an operator")
	s.Require().Less(load, path, "search_path is set before the extension loads")
	s.Require().Less(path, canary, "the canary runs before search_path is set")

	// The canary is only a gate if its failure and its false result both
	// abort the hook.
	tail := graph[canary:]
	s.Require().Contains(tail, "if err != nil {")
	s.Require().Regexp(`if !\w+ \{\n\t\treturn `, tail)
}

// TestGraphLifecycleIsOffTheQuerierInterfaces pins the exclusion: the
// lifecycle helpers are methods on *Queries but are not query methods,
// so listing them on Querier would make the interface a moving target
// across backends.
func (s *EmissionSuite) TestGraphLifecycleIsOffTheQuerierInterfaces() {
	graph := s.files["graph.go"]
	querier := s.files["querier.go"]
	for _, name := range []string{"EnsureGraph", "DropGraph"} {
		s.Require().Contains(graph, "func (q *Queries) "+name+"(ctx context.Context, name string) error {")
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
