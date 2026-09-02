package corpusrun_test

import (
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
