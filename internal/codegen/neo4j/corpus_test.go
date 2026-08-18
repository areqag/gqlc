package neo4j_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

const (
	// corpusPackage is the name the emission is asked for, so the emitted
	// decoders and the hand-written driver share a package clause.
	corpusPackage = "neo4jcorpus"
	// corpusSchema declares the entity shapes the driver decodes into. It
	// is this package's own schema rather than a corpus fixture because
	// the driver names the structs and the decode helpers the emission
	// derives from it, so what it declares is fixed by what the driver
	// exercises.
	corpusSchema = "corpus_schema.gql"
	// corpusQueries projects those shapes as query columns. A column
	// decode is a second emission of the same narrowing rule, reached
	// through the driver's record rather than through the Props map, so
	// nothing the entity decoders prove carries over to it. Naming the
	// record rather than neo4j.GetRecordValue is deliberate: an ANY VALUE
	// column and an edge-union column both read record.Get, so
	// GetRecordValue does not name every column read here.
	corpusQueries = "corpus_queries.cypher"
	// driverModule is the module path the v5 emission imports, and so the
	// path the stub has to claim.
	driverModule = "github.com/neo4j/neo4j-go-driver/v5"
)

// corpusModule resolves the driver import through a directory
// replacement, so the run needs neither the network nor a populated
// module cache: a module replaced by a filesystem path is neither
// downloaded nor summed, which is what lets GOPROXY=off through.
const corpusModule = "module " + corpusPackage + `

go 1.26.2

require ` + driverModule + ` v5.28.4

replace ` + driverModule + ` => ./driverstub
`

// stubModule lets the stub claim the driver's own path. It carries no
// requirements of its own, so the whole run is dependency-free.
const stubModule = "module " + driverModule + "\n\ngo 1.26.2\n"

// TestEmittedDecodersRunOnDriverValues runs the emitted entity decoders
// and query methods against the Go values a Bolt driver can actually put
// in a Props map and a Record. None of it can be established by reading
// the emission: an assertion on the source says a decoder was written,
// not that a list holding a null survives it, that an empty list stays
// distinguishable from an absent one, or that a property of no declared
// shape hands the caller back the value the graph held.
//
// Both decode paths are run because they are two emissions of one
// narrowing rule: an entity property reads through the driver value the
// decode helper was handed and a query column through the driver's
// record, and a null that survives one has been seen to fail the other.
//
// The bytes under test come from Generate rather than from the golden
// tree, so regenerating goldens cannot make a decode bug agree with
// itself. The driver's own package is stubbed (testdata/driverstub_*)
// because neo4j.ResultWithContext carries unexported methods and so
// cannot be implemented outside it; the stub copies neo4j.PropertyValue,
// neo4j.GetProperty, neo4j.RecordValue and neo4j.GetRecordValue
// verbatim, and `just test-codegen-fence` compiles the same emitted
// shapes against the real driver, so what the stub is trusted for is
// behaviour rather than API surface.
func TestEmittedDecodersRunOnDriverValues(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("testdata", corpusSchema))
	require.NoError(t, err)
	sch, err := gql.New().Parse(bytes.NewReader(src))
	require.NoError(t, err)
	in := codegen.Input{Schema: sch, Queries: corpusNamedQueries(t, sch)}

	emit := func(v neo4j.DriverVersion) map[string]string {
		files, err := neo4j.New(neo4j.WithPackageName(corpusPackage), neo4j.WithDriverVersion(v)).Generate(in)
		require.NoError(t, err)
		out := make(map[string]string, len(files))
		for _, f := range files {
			out[f.Path] = string(f.Contents)
		}
		return out
	}
	// Running v5 is running both targets, and this is what says so: the
	// two emissions differ in the driver module path and in the name v6
	// gave the driver handle, and in nothing else, so a decode arm
	// exercised below is the same arm on v6.
	v5 := emit(neo4j.DriverV5)
	require.Equal(t, driverAgnostic(v5), driverAgnostic(emit(neo4j.DriverV6)),
		"the driver targets no longer emit the same decoders modulo the driver names, so a v5 run no longer covers v6")

	driver := corpusFile(t, "corpus_test.go.txt")
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "driverstub", "neo4j", "dbtype"), 0o755))
	sources := map[string]string{
		"go.mod":                              corpusModule,
		"corpus_test.go":                      driver,
		filepath.Join("driverstub", "go.mod"): stubModule,
		filepath.Join("driverstub", "neo4j", "graph.go"):            corpusFile(t, "driverstub_neo4j.go.txt"),
		filepath.Join("driverstub", "neo4j", "dbtype", "dbtype.go"): corpusFile(t, "driverstub_dbtype.go.txt"),
	}
	for path, body := range v5 {
		sources[path] = body
	}
	for name, body := range sources {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}

	passed, log, err := runCorpus(t, dir)
	require.NoError(t, err, "the emitted decoders do not satisfy the driver-value corpus:\n%s", log)
	require.ElementsMatch(t, declaredTests(t, driver), passed,
		"the corpus module did not run every test its template declares:\n%s", log)
}

// driverAgnostic collapses an emission's two driver-major-specific
// texts — the module path, and the handle interface v6 renamed
// DriverWithContext back to Driver — onto a common spelling, so that
// comparing two emissions asks about the decode arms rather than about
// the names driverTarget already owns.
func driverAgnostic(files map[string]string) map[string]string {
	out := make(map[string]string, len(files))
	for path, body := range files {
		body = strings.ReplaceAll(body, "neo4j-go-driver/v6", "neo4j-go-driver/v5")
		out[path] = strings.ReplaceAll(body, "neo4j.DriverWithContext", "neo4j.Driver")
	}
	return out
}

// corpusNamedQueries walks corpus_queries.cypher through the front end —
// queryfile split, cypher parse, resolve — so the emission under test is
// driven by a real Validated shape rather than a hand-built one. Nothing
// short of the real front end decides which ColumnKind a projection
// commits to, and the ColumnKind is what picks the decode arm.
func corpusNamedQueries(t *testing.T, sch schema.Schema) []codegen.NamedQuery {
	t.Helper()

	registry, err := procsig.NewRegistry(nil)
	require.NoError(t, err)
	res := resolver.New(sch, resolver.WithRegistry(registry))

	parsed, err := queryfile.New().Parse(strings.NewReader(corpusFile(t, corpusQueries)))
	require.NoError(t, err)
	out := make([]codegen.NamedQuery, 0, len(parsed))
	for _, aq := range parsed {
		q, err := cypher.New(cypher.WithRegistry(registry)).Parse(strings.NewReader(aq.Text))
		require.NoError(t, err, "parsing %s", aq.Name)
		validated, err := res.Resolve(q)
		require.NoError(t, err, "resolving %s", aq.Name)
		out = append(out, codegen.NamedQuery{
			Name:        aq.Name,
			Cardinality: aq.Cardinality,
			SourceFile:  corpusQueries,
			SourceText:  aq.Text,
			Validated:   validated,
		})
	}
	return out
}

// runCorpus runs the assembled corpus module's own tests, reporting the
// top-level tests that passed alongside the run's output as a reader
// sees it.
//
// The pass set is read from `go test -json`, whose Test field the testing
// framework fills in, rather than from "--- PASS" lines, which a subtest
// name or anything a test writes to stdout can also spell.
func runCorpus(t *testing.T, dir string) (passed []string, log string, err error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "test", "-json", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOPROXY=off")
	out, err := cmd.CombinedOutput()

	var text strings.Builder
	for line := range strings.Lines(string(out)) {
		var event struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
			Output string `json:"Output"`
		}
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &event) != nil {
			text.WriteString(line)
			continue
		}
		text.WriteString(event.Output)
		if event.Action == "pass" && event.Test != "" && !strings.Contains(event.Test, "/") {
			passed = append(passed, event.Test)
		}
	}
	return passed, text.String(), err
}

// declaredTests names the top-level tests a corpus template declares.
// Requiring that every one of them passed is what separates a corpus the
// emission satisfies from a corpus that is not there: `go test` exits 0
// on a _test.go file declaring no tests, so a harness that reads only the
// child's exit status reports success on an emptied template.
func declaredTests(t *testing.T, src string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "corpus_test.go", src, parser.SkipObjectResolution)
	require.NoError(t, err)

	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && isTestName(fn.Name.Name) {
			names = append(names, fn.Name.Name)
		}
	}
	require.NotEmpty(t, names, "the corpus template declares no tests, so a child run of it proves nothing")
	return names
}

// isTestName reports whether `go test` runs a function of this name,
// following testing's own rule: Test, then anything that does not begin
// with a lower-case letter. TestMain is the entry point, not a test.
func isTestName(name string) bool {
	rest, ok := strings.CutPrefix(name, "Test")
	if !ok || name == "TestMain" {
		return false
	}
	if rest == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(first)
}

// corpusFile reads one of the hand-written sources the corpus module is
// assembled from. They carry a .txt suffix so nothing in this repo
// compiles them in place — they are only ever built against an emission.
func corpusFile(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(body)
}
