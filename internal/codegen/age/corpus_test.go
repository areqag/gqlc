package age_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
)

// corpusPackage is the name the emission is asked for, so the extracted
// declarations and the hand-written driver share a package clause.
const corpusPackage = "agecorpus"

// corpusSchema declares the entity shapes the driver decodes into. It is
// this package's own schema rather than a corpus fixture because the
// driver names the structs and the decode helpers the emission derives
// from it, so what it declares is fixed by what the driver exercises.
const corpusSchema = "corpus_schema.gql"

// corpusModule is dependency-free by construction: the driver exercises
// declarations that import only the standard library, so the module
// resolves with the proxy off and the run needs no network.
const corpusModule = "module " + corpusPackage + "\n\ngo 1.26.2\n"

// temporalSource is the source file the instant-binding query is
// attributed to, so its emission lands in a file of its own. Grouping is
// by source basename, and this file's methods are the only ones the
// corpus module compiles: a method over a projected column would drag in
// the pgx row surface this module has no dependency on.
const temporalSource = "temporal.cypher"

// graphStub stands in for the emitted Queries, which carries a pgx
// handle this module has no dependency on. The field name and the method
// bodies under test are the emitted ones; only the surrounding struct
// and the handle it holds are written here.
//
// The handle records the one argument a query binds instead of sending
// it. Everything an instant parameter is encoded through is on that
// path and nowhere else, so running the emitted method is what says the
// count crossing the wire is microseconds and not a formatted string.
const graphStub = "package " + corpusPackage + `

import (
	"context"
	"fmt"
	"strings"
)

type Queries struct {
	db    *recordingDB
	graph string
}

type recordingDB struct{ args string }

func (d *recordingDB) Exec(ctx context.Context, sql string, args ...any) (int, error) {
	d.args = args[0].(string)
	return 0, nil
}

`

// TestEmittedHelpersDecodeTheAgtypeCorpus runs the emitted agtype
// helpers, the emitted entity decoders, the emitted graph-name check,
// the emitted statement composer and the emitted parameter encoding
// against captured agtype text and against the names and query texts
// that make composition hard. All are functions of their arguments and
// none can be exercised by reading the emission: an assertion on the
// source says the helper was written, not that the value it produces
// from `1.5::numeric` is 1.5, that a vertex whose string property
// carries a brace splits at the right byte, that an instant crosses as a
// count of microseconds rather than of millis, nor that a name carrying
// a quote arrives as one SQL literal.
//
// The bytes under test come from Generate rather than from the golden
// tree, so regenerating goldens cannot make a decode, encode or
// composition bug agree with itself.
func (s *EmissionSuite) TestEmittedHelpersDecodeTheAgtypeCorpus() {
	in := s.inputFrom(filepath.Join("testdata", corpusSchema))
	in.Queries = []codegen.NamedQuery{servedQuery, instantParamQuery}
	emitted, err := age.New(age.WithPackageName(corpusPackage)).Generate(in)
	s.Require().NoError(err)
	files := make(map[string]string, len(emitted))
	for _, f := range emitted {
		files[f.Path] = string(f.Contents)
	}

	driver, err := os.ReadFile(filepath.Join("testdata", "corpus_test.go.txt"))
	s.Require().NoError(err)

	dir := s.T().TempDir()
	for name, body := range map[string]string{
		"go.mod":         corpusModule,
		"models.go":      files["models.go"],
		"boundgraph.go":  graphStub + s.declarations(files["db.go"], "maxGraphNameBytes", "boundGraph", "cypherStmt"),
		"writeevent.go":  files[temporalSource+".go"],
		"corpus_test.go": string(driver),
	} {
		s.Require().NoError(os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}

	passed, log, err := s.runCorpus(dir)
	s.Require().NoError(err, "the emitted helpers do not satisfy the captured corpus:\n%s", log)
	s.Require().ElementsMatch(s.declaredTests(string(driver)), passed,
		"the corpus module did not run every test its template declares:\n%s", log)
}

// runCorpus runs the assembled corpus module's own tests, reporting the
// top-level tests that passed alongside the run's output as a reader
// sees it.
//
// The pass set is read from `go test -json`, whose Test field the testing
// framework fills in, rather than from "--- PASS" lines, which a subtest
// name or anything a test writes to stdout can also spell.
func (s *EmissionSuite) runCorpus(dir string) (passed []string, log string, err error) {
	cmd := exec.CommandContext(s.T().Context(), "go", "test", "-json", "-count=1", ".")
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
func (s *EmissionSuite) declaredTests(src string) []string {
	file, err := parser.ParseFile(token.NewFileSet(), "corpus_test.go", src, parser.SkipObjectResolution)
	s.Require().NoError(err)

	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && isTestName(fn.Name.Name) {
			names = append(names, fn.Name.Name)
		}
	}
	s.Require().NotEmpty(names, "the corpus template declares no tests, so a child run of it proves nothing")
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

// declarations prints the named top-level declarations of an emitted
// file, so what the driver compiles is the emitted bytes and not a copy
// of them kept in step by hand.
func (s *EmissionSuite) declarations(body string, names ...string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "db.go", body, parser.SkipObjectResolution)
	s.Require().NoError(err)

	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	var b strings.Builder
	found := 0
	for _, decl := range f.Decls {
		if !wanted[declName(decl)] {
			continue
		}
		s.Require().NoError(printer.Fprint(&b, fset, decl))
		b.WriteString("\n\n")
		found++
	}
	s.Require().Equal(len(names), found, "emitted db.go does not declare all of %v", names)
	return b.String()
}

// declName is the single name a declaration introduces, or the empty
// string for every shape that introduces some other number of them.
func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Name.Name
	case *ast.GenDecl:
		if len(d.Specs) != 1 {
			return ""
		}
		if spec, ok := d.Specs[0].(*ast.ValueSpec); ok && len(spec.Names) == 1 {
			return spec.Names[0].Name
		}
	}
	return ""
}
