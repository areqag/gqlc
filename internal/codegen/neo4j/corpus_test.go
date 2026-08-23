package neo4j_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

// corpusTests names the tests the assembled corpus module has to run and
// pass.
//
// It is written down here, in the parent, rather than censused out of
// testdata/corpus_test.go.txt, because that fixture is the artefact
// under check: a name the fixture stops declaring is a name a census of
// it stops naming, so both sides of the comparison lose it at once.
// Measured against such a census — the fixture's top-level `func Test…`
// declarations — these three edits each took a test out of the census
// and out of the child run together and left the comparison green:
// commenting the function out, deleting it, and renaming it to something
// that is not a test name at all. Held against this list each of the
// three fails, because this list does not move when the fixture does.
//
// Not every way of silencing a test was invisible to that census, and
// nothing here claims it was. A t.Skip in a test's body and a TestMain
// that never calls m.Run() leave the declaration standing while taking
// the test out of the run, so the census still named it and the old
// comparison went red on both. Renaming TestFoo to Testfoo never reaches
// either check: the vet pass `go test` runs over the child module
// refuses to build it, "malformed name".
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
	"TestBinCarriesNullElements",
	"TestBinNullabilityAndShape",
	"TestListyNarrowsEachElement",
	"TestListyEmptyIsNotAbsent",
	"TestListyRefusesAMisTypedElement",
	"TestAnythingReadsWhatTheGraphHolds",
	"TestAnythingSeparatesAbsentFromNull",
	"TestAnythingMissingPropertyReadsLikeTheDrivers",
	"TestScalarNarrows",
	"TestEdgyNarrowsOverARelationship",
	"TestEdgyRefusesWhatTheSchemaDidNotDeclare",
	"TestEdgyKeepsAbsentApartFromEmptyOverARelationship",
	"TestBinColumnsCarryNullElements",
	"TestListyColumnsNarrowEachElement",
	"TestListColumnRefusalsCarryTheDriversError",
	"TestNestColumnsAccumulateAtEveryDepth",
	"TestAnythingColumnsReadWhatTheGraphHolds",
	"TestAnythingColumnsCarryTheirNull",
	"TestEntityColumnsDecodeWholeEntities",
	"TestEntityColumnRefusalsCarryTheDriversError",
	"TestEdgeUnionColumnDispatchesOnTheWireLabel",
	"TestDecodersRefuseAValueOfAnotherType",
	"TestNodeDecoderAcceptsLabelsTheSchemaDoesNotDeclare",
	"TestNodeDecoderRefusesAnUnlabelledNode",
}

// corpusSubtests names, per top-level test, how many subtest passes the
// corpus module's run has to report under it.
//
// corpusTests is a census of top-level names, and a top-level test passes
// and carries its name whether or not anything ran inside it. Measured on
// this fixture: with TestListyRefusesAMisTypedElement's table emptied to
// `range []string{}` and nothing else touched, the child run reported 21
// top-level passes and 0 subtest passes, and the set comparison below
// stayed green (bd gqlc-mlf4). Held against these counts it goes red.
//
// A count is a size, not a membership. Renaming a case, or swapping one
// case for another, leaves the count where it was; what a count refuses is
// a tree that shrank or grew.
//
// A top-level test with no subtests is absent from this map rather than
// written down as zero, so the keys carry the distinction the guard needs:
// a tree that goes silent drops a key this file still holds, and a tree
// that appears adds one this file does not. Either way the two maps are
// unequal. Taking a tree out without a red run costs the same edit taking
// a name out of corpusTests costs — writing it down here, in a file the
// child module is not given.
var corpusSubtests = map[string]int{
	"TestListyRefusesAMisTypedElement": 4,
}

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

	passed, subtests, log, err := runCorpus(t, dir)
	require.NoError(t, err, "the emitted decoders do not satisfy the driver-value corpus:\n%s", log)
	require.NotEmpty(t, corpusTests,
		"corpusTests names no test, so the set comparison below is satisfied by a child run that ran none")
	// The comparison is by multiset, so a name written twice differs from
	// the same name passing once. Which way the two differ is in the lists
	// testify prints above this message, so naming the ways here would only
	// be a shorter list than the one already on screen.
	require.ElementsMatch(t, corpusTests, passed,
		"the corpus module's passing tests are not what corpusTests names, entry for entry and "+
			"counting repeats:\n%s", log)
	// Compared whole rather than per key, because the two disagreements
	// this has to catch are a key whose count fell and a key only one side
	// holds, and map equality is both. Testify prints the differing keys.
	require.Equal(t, corpusSubtests, subtests,
		"the corpus module's subtest passes are not what corpusSubtests declares, "+
			"top-level test by top-level test:\n%s", log)
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
// top-level tests that passed, how many subtest passes each top-level test
// carried, and the run's output as a reader sees it.
//
// Both sets are read from `go test -json`, whose Test field the testing
// framework fills in, rather than from "--- PASS" lines, which a subtest
// name or anything a test writes to stdout can also spell.
//
// A subtest is counted under its top-level test rather than under the
// parent that ran it, so a tree nested two deep reports as one entry.
func runCorpus(t *testing.T, dir string) (passed []string, subtests map[string]int, log string, err error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "go", "test", "-json", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOPROXY=off")
	out, err := cmd.CombinedOutput()

	var text strings.Builder
	subtests = make(map[string]int)
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
		if event.Action != "pass" || event.Test == "" {
			continue
		}
		if parent, _, isSubtest := strings.Cut(event.Test, "/"); isSubtest {
			subtests[parent]++
		} else {
			passed = append(passed, event.Test)
		}
	}
	return passed, subtests, text.String(), err
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
