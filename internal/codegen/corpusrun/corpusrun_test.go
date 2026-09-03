package corpusrun_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen/corpusrun"
)

// The fixture is a corpus driver in miniature: top-level tests whose
// assertions are plain in-body range loops, over the shapes the two
// real fixtures actually use — a literal ranged over directly, a
// literal bound to a name first, a map literal, a nested loop, a loop
// over a value only the run knows, and a name the body binds twice.
const censusFixture = `package corpus

var fileTable = []int{1, 2, 3}

func TestDirectLiteral(t *testing.T) {
	for _, want := range []string{"a", "b"} {
		_ = want
	}
}

func TestBoundLiteral(t *testing.T) {
	accepts := map[string]bool{"x": true, "y": false, "z": true}
	for raw, want := range accepts {
		_, _ = raw, want
	}
}

func TestFileLevelTable(t *testing.T) {
	for _, n := range fileTable {
		_ = n
	}
}

func TestNestedAndInt(t *testing.T) {
	for _, outer := range []int{1, 2} {
		for i := range 4 {
			_, _ = outer, i
		}
	}
}

func TestDynamic(t *testing.T) {
	got := load()
	for _, row := range got {
		_ = row
	}
}

func TestReboundIsDynamic(t *testing.T) {
	cases := []int{1, 2, 3}
	cases = []int{4}
	for _, c := range cases {
		_ = c
	}
}

func TestNoLoop(t *testing.T) {
	_ = 1
}

func helperNotATest() {
	for _, x := range []int{9, 9, 9, 9, 9, 9, 9} {
		_ = x
	}
}
`

func TestTablesCensusesEachShape(t *testing.T) {
	t.Parallel()

	got, err := corpusrun.Tables("corpus_test.go", censusFixture)
	require.NoError(t, err)
	require.Equal(t, map[string]corpusrun.Table{
		"TestDirectLiteral":    {Rows: 2},
		"TestBoundLiteral":     {Rows: 3},
		"TestFileLevelTable":   {Rows: 3},
		"TestNestedAndInt":     {Rows: 6},
		"TestDynamic":          {Dynamic: 1},
		"TestReboundIsDynamic": {Dynamic: 1},
	}, got)
}

// The one thing a top-level name census and a subtest census between
// them cannot see. Emptying a table leaves the test declared, leaves it
// passing, and emits no subtest event either way; only the table census
// moves (bd gqlc-eum1).
func TestTablesSeesAnEmptiedTable(t *testing.T) {
	t.Parallel()

	full, err := corpusrun.Tables("corpus_test.go", censusFixture)
	require.NoError(t, err)

	emptied := `package corpus

func TestDirectLiteral(t *testing.T) {
	for _, want := range []string{} {
		_ = want
	}
}
`
	got, err := corpusrun.Tables("corpus_test.go", emptied)
	require.NoError(t, err)
	require.Equal(t, corpusrun.Table{Rows: 0}, got["TestDirectLiteral"])
	require.NotEqual(t, full["TestDirectLiteral"], got["TestDirectLiteral"],
		"emptying a table left the census where it was, which is the whole defect this guards")
}

// A case commented out is a case the test no longer runs. A census that
// greps source bytes counts it anyway, because the bytes are still
// there; comments are not AST nodes, so this one does not.
func TestTablesDoesNotCountACommentedOutCase(t *testing.T) {
	t.Parallel()

	got, err := corpusrun.Tables("corpus_test.go", `package corpus

func TestDirectLiteral(t *testing.T) {
	for _, want := range []string{
		"a",
		// "b",
	} {
		_ = want
	}
}
`)
	require.NoError(t, err)
	require.Equal(t, corpusrun.Table{Rows: 1}, got["TestDirectLiteral"])
}

// A table swapped for a call that returns one is not the table
// shrinking to nothing, and reading it as that would be a Rows-only
// census telling the reader something false. It moves to Dynamic, which
// is declared separately and so is also a red run.
func TestTablesSeparatesAnUncountableRangeFromAnEmptyOne(t *testing.T) {
	t.Parallel()

	got, err := corpusrun.Tables("corpus_test.go", `package corpus

func TestDirectLiteral(t *testing.T) {
	for _, want := range cases() {
		_ = want
	}
}
`)
	require.NoError(t, err)
	require.Equal(t, corpusrun.Table{Dynamic: 1}, got["TestDirectLiteral"])
}

func TestTablesRefusesASourceItCannotParse(t *testing.T) {
	t.Parallel()

	_, err := corpusrun.Tables("corpus_test.go", "package corpus\n\nfunc TestBroken(t *testing.T) {")
	require.Error(t, err)
}

// Check is what the backends call, and each of its three comparisons
// has to be the one that fails.
func TestCheckHoldsEachDeclarationSeparately(t *testing.T) {
	t.Parallel()

	fixture := `package corpus

func TestOne(t *testing.T) {
	for _, want := range []string{"a", "b"} {
		_ = want
	}
}
`
	run := corpusrun.Report{
		Passed:   []string{"TestOne", "TestTwo"},
		Subtests: map[string]int{"TestTwo": 3},
	}
	good := corpusrun.Declared{
		Tests:    []string{"TestOne", "TestTwo"},
		Subtests: map[string]int{"TestTwo": 3},
		Tables:   map[string]corpusrun.Table{"TestOne": {Rows: 2}},
	}
	require.NoError(t, good.Check(run, "corpus_test.go", fixture))

	for name, mutate := range map[string]func(d *corpusrun.Declared){
		"an empty declaration": func(d *corpusrun.Declared) { d.Tests = nil },
		"a name the run does not report": func(d *corpusrun.Declared) {
			d.Tests = append(d.Tests, "TestThree")
		},
		"a name declared twice": func(d *corpusrun.Declared) {
			d.Tests = append(d.Tests, "TestOne")
		},
		"a subtest count that fell": func(d *corpusrun.Declared) {
			d.Subtests = map[string]int{"TestTwo": 2}
		},
		"a subtest key only one side holds": func(d *corpusrun.Declared) {
			d.Subtests = map[string]int{}
		},
		"a table count that fell": func(d *corpusrun.Declared) {
			d.Tables = map[string]corpusrun.Table{"TestOne": {Rows: 1}}
		},
		"a table key only one side holds": func(d *corpusrun.Declared) {
			d.Tables = map[string]corpusrun.Table{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := corpusrun.Declared{
				Tests:    append([]string(nil), good.Tests...),
				Subtests: map[string]int{"TestTwo": 3},
				Tables:   map[string]corpusrun.Table{"TestOne": {Rows: 2}},
			}
			mutate(&d)
			require.Error(t, d.Check(run, "corpus_test.go", fixture))
		})
	}
}

// A CI reader gets the message and nothing else. It has to say that
// order is not compared, because the lists it prints are sorted and two
// sorted lists side by side are exactly what invites reading them index
// by index.
func TestTestsCensusMessageDisclaimsOrderAndDisclosesItsSort(t *testing.T) {
	t.Parallel()

	d := corpusrun.Declared{Tests: []string{"TestB", "TestA"}}
	err := d.Check(corpusrun.Report{Passed: []string{"TestB"}}, "corpus_test.go", "package corpus\n")
	require.Error(t, err)
	require.Contains(t, err.Error(), "order not compared")
	require.Contains(t, err.Error(), "sorted for reading")
	// The disclosure has to be true of the bytes beside it: declared in
	// the order TestB, TestA, printed sorted.
	require.Contains(t, err.Error(), "declared: [TestA TestB]")
}

// TestRunPinsANonUTCZoneInTheChild is the witness that Run's TZ pin took.
//
// The pin exists so that a decoder consulting time.Local where it should
// not can be killed by a fixture; under UTC no fixture can kill one,
// because time.Local IS UTC and the wrong code and the right code agree
// on every input (corpusrun.ChildZone). But Go resolves time.Local from
// TZ at startup and silently falls back to UTC when the zone will not
// load — a host with no tzdata puts every corpus back in the state the
// pin repairs, with nothing red to say so.
//
// So this asks the CHILD, which is the only process whose time.Local is
// the one the corpora run under, and it asks for the offset rather than
// the zone's name: a name is what was requested, an offset is what was
// resolved. It also runs with TZ=UTC exported into the parent, so a
// green here is not the ambient zone leaking through — it is Run's own
// entry winning over the inherited one.
func TestRunPinsANonUTCZoneInTheChild(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	write("go.mod", "module zoneprobe\n\ngo 1.26.2\n")
	write("zone_test.go", `package zoneprobe

import (
	"testing"
	"time"
)

func TestChildZoneIsNotUTC(t *testing.T) {
	_, offset := time.Now().In(time.Local).Zone()
	if offset == 0 {
		t.Fatalf("child time.Local has a zero UTC offset (%s); the TZ pin did not resolve", time.Local)
	}
	if want := -5 * 60 * 60; offset != want {
		t.Fatalf("child time.Local offset = %d, want %d", offset, want)
	}
}
`)

	t.Setenv("TZ", "UTC")
	report, err := corpusrun.Run(t.Context(), dir)
	require.NoError(t, err, "child run failed:\n%s", report.Log)
	require.Equal(t, []string{"TestChildZoneIsNotUTC"}, report.Passed, "child run:\n%s", report.Log)
}

// enteredFixture is one flat package in miniature: two functions the run
// reaches, one it does not, and a _test.go file whose own functions must
// stay out of both censuses.
const enteredFixture = `package corpus

func agtypeReached() int {
	return 1
}

func agtypeAlsoReached() int {
	return 2
}

func agtypeNeverReached() int {
	return 3
}

func helperOfAnotherFamily() int {
	return 4
}
`

const enteredTestFixture = `package corpus

func agtypeInATestFile() int {
	return 5
}
`

// enteredProfile spells the module's import path in front of each file,
// as `go test -coverprofile` does, and gives agtypeNeverReached a count
// of zero. Line numbers are against enteredFixture.
const enteredProfile = `mode: set
somemodule/emitted.go:3.24,5.2 1 4
somemodule/emitted.go:7.28,9.2 1 1
somemodule/emitted.go:11.30,13.2 1 0
somemodule/emitted.go:15.32,17.2 1 2
`

func writeEnteredFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"emitted.go":      enteredFixture,
		"emitted_test.go": enteredTestFixture,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	return dir
}

// TestEnteredReadsTheProfileRatherThanTheSource holds the two claims the
// AGE helper census rests on: a block with a zero count is not entry, and
// a profile naming files under the child module's import path still
// matches the sources on disk.
func TestEnteredReadsTheProfileRatherThanTheSource(t *testing.T) {
	entered, err := corpusrun.Entered(writeEnteredFixture(t), enteredProfile)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{
		"agtypeReached":         true,
		"agtypeAlsoReached":     true,
		"helperOfAnotherFamily": true,
	}, entered, "a zero-count block is not entry, and a _test.go declaration is not a subject")
}

// TestEnteredReportsNothingForAnEmptyProfile is the shape a missing
// instrument arrives in. It has to be an empty set rather than an error,
// because the caller's own precondition is what distinguishes an
// unmeasured run from an unexercised one.
func TestEnteredReportsNothingForAnEmptyProfile(t *testing.T) {
	entered, err := corpusrun.Entered(writeEnteredFixture(t), "mode: set\n")
	require.NoError(t, err)
	require.Empty(t, entered)
}

// TestEnteredRefusesAProfileItCannotRead fails loudly on a malformed
// line rather than skipping it, since a silently skipped line is a
// function that reads as never entered.
func TestEnteredRefusesAProfileItCannotRead(t *testing.T) {
	for _, tc := range []struct{ line, reason string }{
		{"somemodule/emitted.go:3.24,5.2 1\n", "has 2 fields, want 3"},
		{"somemodule/emitted.go:3.24,5.2 1 not-a-count\n", "unreadable count"},
		{"somemodule/emitted.go:3.24 1 1\n", "has no block range"},
		{"somemodule/emitted.go:3,5.2 1 1\n", `"3" has no line.column separator`},
	} {
		_, err := corpusrun.Entered(writeEnteredFixture(t), "mode: set\n"+tc.line)
		require.ErrorContains(t, err, tc.reason, "profile line %q", tc.line)
	}
}

// TestFunctionsNamesOnlyThePrefixedNonTestDeclarations pins the other
// half of the census. A _test.go declaration carrying the prefix is the
// case that would make the census hold the driver against itself.
func TestFunctionsNamesOnlyThePrefixedNonTestDeclarations(t *testing.T) {
	names, err := corpusrun.Functions(writeEnteredFixture(t), "agtype")
	require.NoError(t, err)
	require.Equal(t, []string{"agtypeAlsoReached", "agtypeNeverReached", "agtypeReached"}, names,
		"sorted, prefixed, and taken from the compiled sources rather than the driver")
}
