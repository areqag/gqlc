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
// against the Go values a Bolt driver can actually put in a Props map.
// None of it can be established by reading the emission: an assertion on
// the source says a decoder was written, not that a list holding a null
// survives it, that an empty list stays distinguishable from an absent
// one, or that a property of no declared shape hands the caller back the
// value the graph held.
//
// The bytes under test come from Generate rather than from the golden
// tree, so regenerating goldens cannot make a decode bug agree with
// itself. The driver's own package is stubbed (testdata/driverstub_*)
// because neo4j.ResultWithContext carries unexported methods and so
// cannot be implemented outside it; the stub copies neo4j.PropertyValue
// and neo4j.GetProperty verbatim, and `just test-codegen-fence` compiles
// the same emitted shapes against the real driver, so what the stub is
// trusted for is behaviour rather than API surface.
func TestEmittedDecodersRunOnDriverValues(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("testdata", corpusSchema))
	require.NoError(t, err)
	sch, err := gql.New().Parse(bytes.NewReader(src))
	require.NoError(t, err)
	in := codegen.Input{Schema: sch}

	models := func(v neo4j.DriverVersion) string {
		files, err := neo4j.New(neo4j.WithPackageName(corpusPackage), neo4j.WithDriverVersion(v)).Generate(in)
		require.NoError(t, err)
		for _, f := range files {
			if f.Path == "models.go" {
				return string(f.Contents)
			}
		}
		require.FailNow(t, "emission has no models.go")
		return ""
	}
	// Running v5 is running both targets, and this is what says so: the
	// two emissions differ in the module path they import and in nothing
	// else, so a decode arm exercised below is the same arm on v6.
	v5 := models(neo4j.DriverV5)
	require.Equal(t, v5, strings.ReplaceAll(models(neo4j.DriverV6), "neo4j-go-driver/v6", "neo4j-go-driver/v5"),
		"the driver targets no longer emit the same decoders modulo the module path, so a v5 run no longer covers v6")

	driver := corpusFile(t, "corpus_test.go.txt")
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "driverstub", "neo4j", "dbtype"), 0o755))
	for name, body := range map[string]string{
		"go.mod":                              corpusModule,
		"models.go":                           v5,
		"corpus_test.go":                      driver,
		filepath.Join("driverstub", "go.mod"): stubModule,
		filepath.Join("driverstub", "neo4j", "graph.go"):            corpusFile(t, "driverstub_neo4j.go.txt"),
		filepath.Join("driverstub", "neo4j", "dbtype", "dbtype.go"): corpusFile(t, "driverstub_dbtype.go.txt"),
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}

	passed, log, err := runCorpus(t, dir)
	require.NoError(t, err, "the emitted decoders do not satisfy the driver-value corpus:\n%s", log)
	require.ElementsMatch(t, declaredTests(t, driver), passed,
		"the corpus module did not run every test its template declares:\n%s", log)
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
