package liverecipes_test

import (
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/liverecipes"
)

// repoRoot reaches the three artefacts this package reads from its own
// directory: the justfile, the workflows, and the codegen module's sources.
const repoRoot = "../.."

// TestEveryLiveTestIsRunByARecipeThatNamesIt is the binding itself, over the
// repository's own files.
//
// It is in the root module and under no build tag, so `just test` runs it on
// every pull request. Putting it in the codegen module behind codegen_live
// would run it only in the jobs it audits, and a job that certifies its own
// selection certifies nothing.
func TestEveryLiveTestIsRunByARecipeThatNamesIt(t *testing.T) {
	split, complaints, err := liverecipes.Read(repoRoot)
	require.NoError(t, err)
	require.Empty(t, complaints, "the artefacts this reads have to be the ones CI runs")
	require.NotEmpty(t, split.Declared, "a live battery this reads as empty is one nothing below can be false of")
	require.NotEmpty(t, split.CI, "a CI half this reads as absent is one nothing below can be false of")
	require.Empty(t, split.Complaints(),
		"a live test the recipes do not name runs in no job, and the job that was meant to gate it goes green")
}

func TestDeclaredTestsReadsCodeAndNotCommentary(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		src  string
		want []string
	}{
		{
			name: "a live test file declares its tests",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\nfunc TestTwo(t *testing.T) {}\n",
			want: []string{"TestOne", "TestTwo"},
		},
		{
			name: "a commented-out declaration is not a declaration",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\n// func TestGhost(t *testing.T) {}\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a string literal spelling a declaration is not one",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nvar s = \"func TestGhost(t *testing.T) {}\"\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a method is not a test go test can select",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\ntype s struct{}\n\nfunc (s) TestMethod(t *testing.T) {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "TestMain runs the selection rather than being selected",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestMain(m *testing.M) {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a lower-case suffix is not a name go test runs",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc Testing(t *testing.T) {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "an exported helper taking a *testing.T is not a test go test can select",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc Setup(t *testing.T) {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a helper taking no *testing.T is not a test",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestHelper(a, b int) {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a function taking nothing is not a test",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestNoArgs() {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a function taking a second parameter beside the *testing.T is not a test",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestExtra(t *testing.T, n int) {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a benchmark's parameter is not a test's",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestBench(b *testing.B) {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a T from another package is not testing's",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport (\n\t\"testing\"\n\n\t\"example.com/harness\"\n)\n\nfunc TestHarness(t *harness.T) {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "an unqualified T is not testing's",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\ntype T struct{}\n\nfunc TestLocal(t *T) {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a testing.T taken by value is not the parameter go test passes",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestValue(t testing.T) {}\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a declaration with no body declares no test",
			path: "live_x_test.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestAsm(t *testing.T)\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			// go/build rejects this file outright, so no build compiles it and
			// reading it as excluded matches every build that exists.
			name: "a build constraint this reader cannot parse excludes the file",
			path: "live_x_test.go",
			src:  "//go:build codegen_live &&\n\npackage p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n",
			want: nil,
		},
		{
			// A //go:build below the package clause is a comment, not a
			// constraint, so the file is in every build.
			name: "a build constraint below the package clause excludes nothing",
			path: "live_x_test.go",
			src:  "package p\n\n//go:build !codegen_live\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a file the live tag excludes declares no live test",
			path: "live_x_test.go",
			src:  "//go:build !codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n",
			want: nil,
		},
		{
			name: "an unconstrained test file is compiled by the live build too",
			path: "plain_test.go",
			src:  "package p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n",
			want: []string{"TestOne"},
		},
		{
			name: "a file that is not a test file declares no test",
			path: "helpers.go",
			src:  "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n",
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := liverecipes.DeclaredTests(token.NewFileSet(), tc.path, []byte(tc.src))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestDeclaredTestsRefusesSourceItCannotParse(t *testing.T) {
	_, err := liverecipes.DeclaredTests(token.NewFileSet(), "live_x_test.go", []byte("//go:build codegen_live\n\npackage p\n\nfunc TestOne(t *testing.T) {\n"))
	require.Error(t, err, "a file this reader cannot parse declares an unknown set, not an empty one")
}

// The four top-level tests the live battery declares today and the two halves
// that run them, so a row saying "sound" is the shape CI invokes rather than a
// simplification of it. The rows that follow are each one edit away from these.
const (
	smoke   = "TestLiveSmoke"
	session = "TestAGESessionInit"
	alt     = "TestAGERefusesRelationshipTypeAlternation"
	defines = "TestAGERefusesTheFunctionsItDoesNotDefine"

	ageRun = smoke + "|" + session + "|" + alt + "|" + defines
)

func inv(recipe string, fields ...string) liverecipes.Invocation {
	return liverecipes.Invocation{
		Recipe: recipe,
		Fields: append([]string{"go", "test", "-tags", liverecipes.LiveBuildTag}, fields...),
	}
}

func neo4jHalf() liverecipes.Invocation {
	return inv("test-codegen-live-neo4j", "-run", smoke, "-skip", smoke+"/apache-age", "./...")
}

func ageHalf() liverecipes.Invocation {
	return inv("test-codegen-live-age", "-run", ageRun, "-skip", smoke+"/neo4j", "./...")
}

// TestComplaintsNameTheTestAndNotACount drives Split.Complaints over synthetic
// splits, one per way the two artefacts can disagree.
//
// want is the substrings the complaints have to carry, one per complaint, and
// each is a test name or a -run alternative: a rule that reported "3 tests are
// unclaimed" would pass a membership check by size and leave the reader to find
// which. The count assertion is over want and not a literal, so a row that
// starts producing a second complaint fails rather than matching on the first.
func TestComplaintsNameTheTestAndNotACount(t *testing.T) {
	declared := []string{alt, defines, session, smoke}

	for _, tc := range []struct {
		name  string
		split liverecipes.Split
		want  []string
	}{
		{
			name:  "the two halves CI runs today cover the battery between them",
			split: liverecipes.Split{Declared: declared, CI: []liverecipes.Invocation{neo4jHalf(), ageHalf()}},
		},
		{
			name: "a live test neither half names is named",
			split: liverecipes.Split{
				Declared: append(slices.Clone(declared), "TestAGERefusesTemporalLiterals"),
				CI:       []liverecipes.Invocation{neo4jHalf(), ageHalf()},
			},
			want: []string{"TestAGERefusesTemporalLiterals"},
		},
		{
			// The other direction: the test was renamed and the allowlist kept
			// the old spelling, so the half runs one test fewer and says so
			// nowhere.
			name: "an allowlist entry no test declares is named",
			split: liverecipes.Split{
				Declared: []string{alt, defines, smoke},
				CI:       []liverecipes.Invocation{neo4jHalf(), ageHalf()},
			},
			want: []string{session},
		},
		{
			// -run is a regexp to go test, so an allowlist naming the prefix
			// silently claims every test under it. Reading it as a name list is
			// what makes the new test a complaint rather than covered.
			name: "a prefix allowlist does not claim the test that extends it",
			split: liverecipes.Split{
				Declared: []string{smoke, smoke + "Federation"},
				CI:       []liverecipes.Invocation{inv("half", "-run", smoke, "./...")},
			},
			want: []string{smoke + "Federation"},
		},
		{
			name: "a -skip matching the test whole unruns it",
			split: liverecipes.Split{
				Declared: []string{smoke},
				CI:       []liverecipes.Invocation{inv("half", "-run", smoke, "-skip", smoke, "./...")},
			},
			want: []string{smoke},
		},
		{
			name: "a -skip narrowed to a subtest leaves the test running",
			split: liverecipes.Split{
				Declared: []string{smoke},
				CI:       []liverecipes.Invocation{inv("half", "-run", smoke, "-skip", smoke+"/neo4j", "./...")},
			},
		},
		{
			// go test honours the last -run; this reads both, so a name only
			// the last one carries is a complaint and not a silence.
			name: "a -run written twice claims only what both spellings name",
			split: liverecipes.Split{
				Declared: []string{smoke, session},
				CI:       []liverecipes.Invocation{inv("half", "-run", smoke, "-run", smoke+"|"+session, "./...")},
			},
			want: []string{session},
		},
		{
			// The same rule over the local half: a recipe no workflow reaches
			// is read for the names it selects as well as the ones it misses.
			name: "an allowlist entry no test declares is named in a recipe no workflow reaches",
			split: liverecipes.Split{
				Declared: []string{smoke},
				CI:       []liverecipes.Invocation{inv("half", "-run", smoke, "./...")},
				Local:    []liverecipes.Invocation{inv("whole", "-run", smoke+"|"+smoke+"Federation", "./...")},
			},
			want: []string{smoke + "Federation"},
		},
		{
			name: "a recipe no workflow reaches must run the whole battery",
			split: liverecipes.Split{
				Declared: []string{smoke, session},
				CI:       []liverecipes.Invocation{inv("half", "-run", smoke+"|"+session, "./...")},
				Local:    []liverecipes.Invocation{inv("whole", "-run", smoke, "./...")},
			},
			want: []string{session},
		},
		{
			name: "a recipe no workflow reaches passes by carrying no -run",
			split: liverecipes.Split{
				Declared: []string{smoke, session},
				CI:       []liverecipes.Invocation{inv("half", "-run", smoke+"|"+session, "./...")},
				Local:    []liverecipes.Invocation{inv("whole", "./...")},
			},
		},
		{
			// The vacuity guards. Both rules below are written over one of
			// these collections, so an empty one makes its rule pass by
			// iterating nothing — the defect this package exists to refuse,
			// one level up.
			name:  "no declared test is a comparison against nothing",
			split: liverecipes.Split{CI: []liverecipes.Invocation{inv("half", "./...")}},
			want:  []string{"no top-level live test was found"},
		},
		{
			name:  "no CI invocation is a battery nothing is required to run",
			split: liverecipes.Split{Declared: []string{smoke}},
			want:  []string{"no workflow reaches a live `go test`", smoke},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			complaints := tc.split.Complaints()
			require.Len(t, complaints, len(tc.want))
			for _, want := range tc.want {
				require.True(t,
					slices.ContainsFunc(complaints, func(c string) bool { return strings.Contains(c, want) }),
					"no complaint names %s: %v", want, complaints)
			}
		})
	}
}

func TestWorkflowRecipesReadsEveryJobsSteps(t *testing.T) {
	src := []byte(`
jobs:
  neo4j:
    steps:
      - uses: actions/checkout@v4
      - run: just test-codegen-live-neo4j
  age:
    steps:
      - run: |
          just --list
          just -q test-codegen-live-age
`)
	names, err := liverecipes.WorkflowRecipes(src)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"test-codegen-live-neo4j", "test-codegen-live-age"}, names,
		"a recipe reached from any job is reached by CI, and a flag is not a recipe name")
}
