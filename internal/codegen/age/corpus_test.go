package age_test

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/codegen/corpusrun"
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

// graphStub stands in for the emitted core and root handle, which carry
// a pgx handle this module has no dependency on. The field names and the
// method bodies under test are the emitted ones; only the surrounding
// structs and the handle they hold are written here.
//
// The embed is not decoration: the helpers below are emitted on *queries
// and the driver calls them on a *Queries, so the corpus compiles only
// if promotion reaches them.
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

type queries struct {
	db    *recordingDB
	graph string
}

type Queries struct {
	queries
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
// testdata/corpus_test.go.txt, for the reason corpusrun.Declared gives
// about all three of these literals. Measured against such a census —
// the fixture's top-level `func Test…` declarations — commenting a test
// out and deleting it each took it out of the census and out of the
// child run together and left the comparison green. Held against this
// list both fail, because this list does not move when the fixture does.
var corpusTests = []string{
	"TestAgtypeString",
	"TestAgtypeBool",
	"TestAgtypeInt64",
	"TestAgtypeFloat64",
	"TestDecodeVertex",
	"TestDecodeVertexReadsNullableProperties",
	"TestDecodeVertexRefusesAValueTheDeclaredWidthCannotHold",
	"TestDecodeVertexAcceptsWhatTheDeclaredWidthHolds",
	"TestDecodeVertexStepsOverStructuredProperties",
	"TestDecodeZeroPropertyVertex",
	"TestAgtypeListOfString",
	"TestAgtypeListRefusesAnElementOfTheWrongScalar",
	"TestAgtypeListRefusesAnOutOfRangeElement",
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
	"TestAgtypeInstantRefusesACountOutsideTheCalendar",
	"TestAgtypeMicrosEncodesTheInstantAndNotTheWallClock",
	"TestAgtypeNullableMicrosCarriesAbsence",
	"TestAgtypeZoneReadsAnOffsetInSeconds",
	"TestAgtypeZoneRefusesAnOffsetOutsideADay",
	"TestDecodeVertexReadsTheOffsetSidecarBesideItsInstant",
	"TestEmittedMethodBindsAnInstantAsMicroseconds",
	"TestAgtypeDateReadsTheStoredText",
	"TestAgtypeDateRefusesTextThatWouldNotSortChronologically",
	"TestAgtypeDateTextRefusesADayOffTheCalendar",
	"TestAgtypeLocalTimeCountsMicrosecondsFromMidnight",
	"TestAgtypeLocalTimeRefusesACountOutsideTheDay",
	"TestAgtypeLocalTimeMicrosRefusesAReadingOffTheClock",
	"TestAgtypeTimeMicrosNormalisesTheReadingAndWrapsTheDay",
	"TestAgtypeTimeRefusesACountOutsideTheDay",
	"TestAgtypeTimeMicrosRefusesAReadingOffTheClock",
	"TestStoredTimesSortByTheirInstantAndNotTheirLocalClock",
	"TestAgtypeTimeZoneRebuildsTheReadingItWasWrittenAt",
	"TestDecodeVertexReadsTheOffsetSidecarBesideItsTime",
	"TestEmittedMethodBindsATimeAsNormalisedMicroseconds",
	"TestAgtypeDurationCountsTotalMicroseconds",
	"TestAgtypeDurationMicrosFoldsDaysAndRefusesMonths",
	"TestEmittedMethodBindsTheCarriersAsTheirScalars",
	"TestEmittedMethodBindsListsOfCarriers",
	"TestEmittedMethodBindsNestedListsOfCarriers",
	"TestBoundGraphCountsBytes",
}

// corpusSubtests names, per top-level test, how many subtest passes the
// corpus module's run has to report under it.
//
// corpusTests is a census of top-level names, and a top-level test passes
// and carries its name whether or not anything ran inside it. Measured on
// this fixture: with TestAgtypeString's accepts table emptied to
// `map[string]string{}` and nothing else touched, the child run reported 51
// top-level passes and 11 subtest passes — this key's 11 gone, the other
// two keys' 4 and 7 still there — and the set comparison below stayed
// green. Held against these counts it goes red, naming the key:
// "TestAgtypeString: declared 11, ran no entry".
//
// Re-taken 2026-09-02 (bd gqlc-wuyu). The verdict is the one gqlc-mlf4
// recorded; the two figures beside it were not re-taken with it and had
// gone stale. That measurement read 29 top-level passes and 0 subtest
// passes, on a fixture that has since grown to 51 tests and gained the two
// DecodeVertex keys below, which is where the 0 went.
//
// A count is a size, not a membership. Renaming a case, or swapping one
// case for another, leaves the count where it was; what a count refuses is
// a tree that shrank or grew.
//
// A top-level test with no subtests is absent from this map rather than
// written down as zero, so the keys carry the distinction the guard needs:
// a tree that goes silent drops a key this file still holds, and a tree
// that appears adds one this file does not.
var corpusSubtests = map[string]int{
	"TestAgtypeString": 11,
	"TestDecodeVertexRefusesAValueTheDeclaredWidthCannotHold": 4,
	"TestDecodeVertexAcceptsWhatTheDeclaredWidthHolds":        7,
}

// corpusTables counts, per top-level test, the in-body range tables the
// fixture declares, as corpusrun.Tables censuses them.
//
// This is the third census because the first two cannot see the fixture's
// commonest shape. corpusTests names top-level tests and a top-level test
// passes whether or not anything ran inside it; corpusSubtests counts
// subtest passes and this fixture has three t.Run call sites among 51
// top-level tests, so for the other 48 a test whose table goes empty has
// no key on either side, which is equality. Measured: emptying the
// five-entry table in TestAgtypeListRefusesAnElementOfTheWrongScalar
// reddens this census and nothing else in the run moved (bd gqlc-eum1,
// the half gqlc-mlf4 left open; re-taken 2026-09-02 under gqlc-wuyu,
// where "nothing else moved" is witnessed by Check's order — Tables is
// compared last, so reaching it means Tests and Subtests both agreed).
// The count of t.Run sites was one in 46 tests when this was written.
//
// Every test here that ranges over anything has an entry, and a test that
// ranges over nothing has none — see corpusrun.Table for what Rows and
// Dynamic each refuse. When a deliberate fixture edit moves a number, the
// failure prints the fixture's current census as a Go literal to paste.
var corpusTables = map[string]corpusrun.Table{
	"TestAgtypeArgs":                   {Rows: 7},
	"TestAgtypeBool":                   {Rows: 17},
	"TestAgtypeDateReadsTheStoredText": {Rows: 5},
	"TestAgtypeDateRefusesTextThatWouldNotSortChronologically": {Rows: 14},
	"TestAgtypeDateTextRefusesADayOffTheCalendar":              {Rows: 10},
	"TestAgtypeDurationCountsTotalMicroseconds":                {Rows: 16},
	"TestAgtypeDurationMicrosFoldsDaysAndRefusesMonths":        {Rows: 9},
	"TestAgtypeFloat64": {Rows: 21},
	"TestAgtypeInstantCountsMicrosecondsFromTheEpoch":  {Rows: 11},
	"TestAgtypeInstantRefusesACountOutsideTheCalendar": {Rows: 6},
	"TestAgtypeInt64":                                         {Rows: 27},
	"TestAgtypeListOfString":                                  {Rows: 15},
	"TestAgtypeListRefusesAnElementOfTheWrongScalar":          {Rows: 14},
	"TestDecodeVertexRefusesAValueTheDeclaredWidthCannotHold": {Rows: 4},
	"TestDecodeVertexAcceptsWhatTheDeclaredWidthHolds":        {Rows: 7},
	"TestAgtypeLocalTimeCountsMicrosecondsFromMidnight":       {Rows: 11},
	"TestAgtypeLocalTimeMicrosRefusesAReadingOffTheClock":     {Rows: 9},
	"TestAgtypeLocalTimeRefusesACountOutsideTheDay":           {Rows: 5},
	"TestAgtypeMicrosEncodesTheInstantAndNotTheWallClock":     {Rows: 8},
	"TestAgtypeNestedList":                                    {Rows: 5},
	"TestAgtypeNullableMicrosCarriesAbsence":                  {Rows: 4},
	"TestAgtypeString":                                        {Rows: 26},
	"TestAgtypeTimeMicrosNormalisesTheReadingAndWrapsTheDay":  {Rows: 9},
	"TestAgtypeTimeMicrosRefusesAReadingOffTheClock":          {Rows: 15},
	"TestAgtypeTimeRefusesACountOutsideTheDay":                {Rows: 12},
	"TestAgtypeTimeZoneRebuildsTheReadingItWasWrittenAt":      {Rows: 8},
	"TestAgtypeValue":                                         {Rows: 21},
	"TestAgtypeZoneReadsAnOffsetInSeconds":                    {Rows: 8},
	"TestAgtypeZoneRefusesAnOffsetOutsideADay":                {Rows: 8},
	"TestBoundGraphCountsBytes":                               {Rows: 7},
	"TestCypherStmtComposesOneStatement":                      {Rows: 6},
	"TestDecodeVertexRefusesMisshapenText":                    {Rows: 16},
	"TestEmittedMethodBindsATimeAsNormalisedMicroseconds":     {Rows: 4},
	"TestEmittedMethodBindsAnInstantAsMicroseconds":           {Rows: 4},
	"TestEmittedMethodBindsListsOfCarriers":                   {Rows: 7},
	"TestEmittedMethodBindsNestedListsOfCarriers":             {Rows: 4},
	"TestEmittedMethodBindsTheCarriersAsTheirScalars":         {Rows: 6},
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
	in.Queries = []codegen.NamedQuery{servedQuery, instantParamQuery, carrierParamQuery, zonedParamQuery, listCarrierParamQuery, nestedListCarrierParamQuery}
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
		"go.mod":    corpusModule,
		"models.go": files["models.go"],
		// The neutral carriers the three widths decode into. Emitted
		// rather than written here for the same reason models.go is: a
		// driver holding its own copy of Date would compile against a
		// shape the emission had stopped producing.
		"temporal.go":    files["temporal.go"],
		"boundgraph.go":  graphStub + s.declarations(files["db.go"], "maxGraphNameBytes", "boundGraph", "cypherStmt"),
		"writeevent.go":  files[temporalSource+".go"],
		"corpus_test.go": string(driver),
	} {
		s.Require().NoError(os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}

	report, err := corpusrun.Run(s.T().Context(), dir)
	s.Require().NoError(err, "the emitted helpers do not satisfy the captured corpus:\n%s", report.Log)
	declared := corpusrun.Declared{Tests: corpusTests, Subtests: corpusSubtests, Tables: corpusTables}
	s.Require().NoError(declared.Check(report, "corpus_test.go", string(driver)), report.Log)
	s.requireEveryAgtypeHelperRan(dir, report)
}

// requireEveryAgtypeHelperRan holds the corpus template against the
// emission it drives: every agtype* helper the assembled module declares
// has to be entered by the child run.
//
// The three censuses beside it ask what the DRIVER declares — its test
// names, its subtest counts, its table row counts — so all three are
// satisfied by a driver that is internally consistent and reaches only
// half the emission. A helper the emission gains and the driver never
// calls is declared by nobody, and every one of them stays green (bd
// gqlc-5et5).
//
// Entry comes from the child's own coverage profile rather than from
// call sites in the driver's syntax. A call site is not a run: it can sit
// in a table row nothing selects. It is also the wrong question — several
// emitted helpers are reached only through an emitted decoder, and a
// census demanding a direct call would report those unexercised while
// their behaviour ran on every row.
//
// What this does NOT claim is that the helper's behaviour was checked.
// Coverage witnesses entry; an assertion is what witnesses a verdict, and
// the corpus driver is where those live.
func (s *EmissionSuite) requireEveryAgtypeHelperRan(dir string, report corpusrun.Report) {
	s.Require().NotEmpty(report.Cover,
		"the child run produced no coverage profile, so this census would pass having measured nothing")

	entered, err := corpusrun.Entered(dir, report.Cover)
	s.Require().NoError(err)

	helpers, err := corpusrun.Functions(dir, "agtype")
	s.Require().NoError(err)
	s.Require().NotEmpty(helpers,
		"the assembled module declares no agtype* helper at all, so this census has nothing to hold")

	var unreached []string
	for _, name := range helpers {
		if !entered[name] {
			unreached = append(unreached, name)
		}
	}
	s.Require().Empty(unreached,
		"these emitted agtype* helpers are compiled by the corpus module and no test in the run enters "+
			"them, so the corpus is green on an emission it does not exercise; each one needs a corpus "+
			"test that reaches it")
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
