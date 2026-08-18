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

// corpusTests names the tests the assembled corpus module has to run and
// pass.
//
// It is written down here rather than censused out of
// testdata/corpus_test.go.txt, because that fixture is the artefact
// under check: a name the fixture stops declaring is a name a census of
// it stops naming, so both sides of the comparison lose it at once.
// Measured against such a census — the fixture's top-level `func Test…`
// declarations — commenting a test out and deleting it each took it out
// of the census and out of the child run together and left the
// comparison green. Held against this list both fail, because this list
// does not move when the fixture does.
//
// A fixture emptied to its package clause is a child run that reports
// success: `go test` prints `[no tests to run]` and exits 0 over a
// _test.go declaring no tests, so the child's exit status says nothing
// about it. Requiring this exact set catches such a run, because the
// pass set it produces is empty and this list is not.
//
// "This list is not" is a condition, not a property of the list.
// Emptying the fixture and this literal together leaves both sides of
// the comparison empty, and two empty sets match. The census this list
// replaces carried a require.NotEmpty against that silence; the test
// below requires this list non-empty before it compares.
//
// This list is a declaration rather than a gate. A test can leave the
// fixture and its name leave this literal in one commit's edit; both
// sides of the comparison below then name the same tests, and a run
// with one name and its test removed together came back green.
// Removing the last name is where that stops: it leaves this literal
// empty, which is what the non-empty requirement below refuses. What
// the edit costs the remover either way is writing the removal down
// in a file the child module is not given.
var corpusTests = []string{
	"TestAgtypeString",
	"TestAgtypeBool",
	"TestAgtypeInt64",
	"TestAgtypeFloat64",
	"TestDecodeVertex",
	"TestDecodeVertexReadsNullableProperties",
	"TestDecodeVertexStepsOverStructuredProperties",
	"TestDecodeZeroPropertyVertex",
	"TestAgtypeListOfString",
	"TestAgtypeListRefusesAnElementOfTheWrongScalar",
	"TestAgtypeNestedList",
	"TestAgtypeListOfNarrowElementWidth",
	"TestDecodeVertexWithListProperties",
	"TestAgtypeValue",
	"TestDecodeVertexWithAnyProperty",
	"TestDecodeEdge",
	"TestEntityDecodersRefuseTheOtherAnnotation",
	"TestDecodeVertexRefusesMisshapenText",
	"TestAgtypeArgs",
	"TestCypherStmtComposesOneStatement",
	"TestCypherStmtRefusesAnOverlongName",
	"TestAgtypeInstantCountsMicrosecondsFromTheEpoch",
	"TestAgtypeMicrosEncodesTheInstantAndNotTheWallClock",
	"TestAgtypeNullableMicrosCarriesAbsence",
	"TestAgtypeZoneReadsAnOffsetInSeconds",
	"TestAgtypeZoneRefusesAnOffsetOutsideADay",
	"TestDecodeVertexReadsTheOffsetSidecarBesideItsInstant",
	"TestEmittedMethodBindsAnInstantAsMicroseconds",
	"TestBoundGraphCountsBytes",
}

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
	s.Require().NotEmpty(corpusTests,
		"corpusTests names no test, so the set comparison below is satisfied by a child run that ran none")
	// The comparison is by multiset, so a name written twice differs from
	// the same name passing once. Which way the two differ is in the lists
	// testify prints above this message, so naming the ways here would only
	// be a shorter list than the one already on screen.
	s.Require().ElementsMatch(corpusTests, passed,
		"the corpus module's passing tests are not what corpusTests names, entry for entry and "+
			"counting repeats:\n%s", log)
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
