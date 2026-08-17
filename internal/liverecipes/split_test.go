package liverecipes

import (
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
	split, complaints, err := Read(repoRoot)
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
			got, err := DeclaredTests(token.NewFileSet(), tc.path, []byte(tc.src))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestDeclaredTestsRefusesSourceItCannotParse(t *testing.T) {
	_, err := DeclaredTests(token.NewFileSet(), "live_x_test.go", []byte("//go:build codegen_live\n\npackage p\n\nfunc TestOne(t *testing.T) {\n"))
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

func inv(recipe string, fields ...string) Invocation {
	return Invocation{Recipe: recipe, Fields: append([]string{"go", "test", "-tags", LiveBuildTag}, fields...)}
}

func neo4jHalf() Invocation {
	return inv("test-codegen-live-neo4j", "-run", smoke, "-skip", smoke+"/apache-age", "./...")
}

func ageHalf() Invocation {
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
		split Split
		want  []string
	}{
		{
			name:  "the two halves CI runs today cover the battery between them",
			split: Split{Declared: declared, CI: []Invocation{neo4jHalf(), ageHalf()}},
		},
		{
			name: "a live test neither half names is named",
			split: Split{
				Declared: append(slices.Clone(declared), "TestAGERefusesTemporalLiterals"),
				CI:       []Invocation{neo4jHalf(), ageHalf()},
			},
			want: []string{"TestAGERefusesTemporalLiterals"},
		},
		{
			// The other direction: the test was renamed and the allowlist kept
			// the old spelling, so the half runs one test fewer and says so
			// nowhere.
			name: "an allowlist entry no test declares is named",
			split: Split{
				Declared: []string{alt, defines, smoke},
				CI:       []Invocation{neo4jHalf(), ageHalf()},
			},
			want: []string{session},
		},
		{
			// -run is a regexp to go test, so an allowlist naming the prefix
			// silently claims every test under it. Reading it as a name list is
			// what makes the new test a complaint rather than covered.
			name: "a prefix allowlist does not claim the test that extends it",
			split: Split{
				Declared: []string{smoke, smoke + "Federation"},
				CI:       []Invocation{inv("half", "-run", smoke, "./...")},
			},
			want: []string{smoke + "Federation"},
		},
		{
			name: "a -skip matching the test whole unruns it",
			split: Split{
				Declared: []string{smoke},
				CI:       []Invocation{inv("half", "-run", smoke, "-skip", smoke, "./...")},
			},
			want: []string{smoke},
		},
		{
			name: "a -skip narrowed to a subtest leaves the test running",
			split: Split{
				Declared: []string{smoke},
				CI:       []Invocation{inv("half", "-run", smoke, "-skip", smoke+"/neo4j", "./...")},
			},
		},
		{
			// go test honours the last -run; this reads both, so a name only
			// the last one carries is a complaint and not a silence.
			name: "a -run written twice claims only what both spellings name",
			split: Split{
				Declared: []string{smoke, session},
				CI:       []Invocation{inv("half", "-run", smoke, "-run", smoke+"|"+session, "./...")},
			},
			want: []string{session},
		},
		{
			name: "a recipe no workflow reaches must run the whole battery",
			split: Split{
				Declared: []string{smoke, session},
				CI:       []Invocation{inv("half", "-run", smoke+"|"+session, "./...")},
				Local:    []Invocation{inv("whole", "-run", smoke, "./...")},
			},
			want: []string{session},
		},
		{
			name: "a recipe no workflow reaches passes by carrying no -run",
			split: Split{
				Declared: []string{smoke, session},
				CI:       []Invocation{inv("half", "-run", smoke+"|"+session, "./...")},
				Local:    []Invocation{inv("whole", "./...")},
			},
		},
		{
			// The vacuity guards. Both rules below are written over one of
			// these collections, so an empty one makes its rule pass by
			// iterating nothing — the defect this package exists to refuse,
			// one level up.
			name:  "no declared test is a comparison against nothing",
			split: Split{CI: []Invocation{inv("half", "./...")}},
			want:  []string{"no top-level live test was found"},
		},
		{
			name:  "no CI invocation is a battery nothing is required to run",
			split: Split{Declared: []string{smoke}},
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

// TestLiveInvocationsReadsWhatTheShellWouldRun covers the step before the
// rules: a command line this misses is a half the rules never see, and a
// command line it invents is a half they see twice.
func TestLiveInvocationsReadsWhatTheShellWouldRun(t *testing.T) {
	for _, tc := range []struct {
		name           string
		src            string
		wantFields     [][]string
		wantComplaints int
	}{
		{
			name:       "a live recipe body is one invocation",
			src:        "r:\n    cd test/data/codegen && go test -count=1 -tags codegen_live ./...\n",
			wantFields: [][]string{{"go", "test", "-count=1", "-tags", "codegen_live", "./..."}},
		},
		{
			name: "a command line with no -tags builds no live test",
			src:  "r:\n    go test -count=1 ./...\n",
		},
		{
			name: "a -tags without the live tag builds no live test",
			src:  "r:\n    go test -tags integration ./...\n",
		},
		{
			name:       "the live tag beside another is still the live build",
			src:        "r:\n    go test -tags integration,codegen_live ./...\n",
			wantFields: [][]string{{"go", "test", "-tags", "integration,codegen_live", "./..."}},
		},
		{
			// go test honours the last -tags, so a second one without the tag
			// leaves the live files uncompiled.
			name: "a second -tags dropping the live tag is not the live build",
			src:  "r:\n    go test -tags codegen_live -tags integration ./...\n",
		},
		{
			name: "a go test the line only prints is not one it runs",
			src:  "r:\n    echo go test -tags codegen_live ./...\n",
		},
		{
			name: "a commented-out invocation is not one",
			src:  "r:\n    # go test -tags codegen_live ./...\n",
		},
		{
			// The `&&` puts the commented text in command position, so what
			// keeps it out is the comment cut and not the position check.
			name: "a comment spelling a command after an operator is still a comment",
			src:  "r:\n    # disabled && go test -tags codegen_live ./...\n",
		},
		{
			// The direction that matters: a trailing comment's words read as
			// flags would narrow an invocation the shell does not narrow.
			name:       "a trailing comment's words are not the command's arguments",
			src:        "r:\n    go test -tags codegen_live ./...  # -run TestLiveSmoke\n",
			wantFields: [][]string{{"go", "test", "-tags", "codegen_live", "./..."}},
		},
		{
			name:           "a line whose quoting never closed is a complaint",
			src:            "r:\n    go test -tags codegen_live -run 'TestLiveSmoke ./...\n",
			wantComplaints: 1,
		},
		{
			// Nothing here builds the live tag, so the unterminated line is
			// still read: what the open quote swallowed can be the -tags.
			name:           "an unterminated line is a complaint before it is classified",
			src:            "r:\n    go test -run 'TestLiveSmoke ./...\n",
			wantComplaints: 1,
		},
		{
			name: "an unterminated line with no go test on it is not this reader's",
			src:  "r:\n    echo 'unclosed\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, complaints := liveInvocations(tc.src)
			require.Len(t, complaints, tc.wantComplaints)
			var fields [][]string
			for _, one := range found {
				fields = append(fields, one.Fields)
			}
			require.Equal(t, tc.wantFields, fields)
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
	names, err := WorkflowRecipes(src)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"test-codegen-live-neo4j", "test-codegen-live-age"}, names,
		"a recipe reached from any job is reached by CI, and a flag is not a recipe name")
}

// TestReadDeclaredComplainsWhenTwoFilesDeclareTheName holds the shape a
// last-one-wins map would swallow: `-run TestLiveSmoke` would select both
// bodies, and a rule satisfied by one of them says nothing about the other.
func TestReadDeclaredComplainsWhenTwoFilesDeclareTheName(t *testing.T) {
	dir := t.TempDir()
	const src = "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestLiveSmoke(t *testing.T) {}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "live_a_test.go"), []byte(src), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "live_b_test.go"), []byte(src), 0o600))

	names, complaints, err := readDeclared(dir)
	require.NoError(t, err)
	require.Equal(t, []string{"TestLiveSmoke"}, names)
	require.Len(t, complaints, 1)
	require.Contains(t, complaints[0], "TestLiveSmoke")
}

// TestReadWorkflowRecipesComplainsWhenItReadsNoWorkflow is the CI half's
// vacuity guard at its source: a directory that yields no file makes every
// live recipe Local, where the rule is different rather than absent, and the
// disagreement it was pointed at goes unstated.
func TestReadWorkflowRecipesComplainsWhenItReadsNoWorkflow(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("just test-codegen-live-age\n"), 0o600))

	names, complaints, err := readWorkflowRecipes(dir)
	require.NoError(t, err)
	require.Empty(t, names)
	require.Len(t, complaints, 1)
	require.Contains(t, complaints[0], dir)
}

// TestReadAssemblesTheSplitFromDisk drives the three readers together over a
// tree written here, because which half an invocation lands in is decided by
// the join between the workflow's recipe names and the justfile's headers, and
// neither reader alone can be wrong about it.
func TestReadAssemblesTheSplitFromDisk(t *testing.T) {
	justfile := "test-live-half:\n" +
		"    cd test/data/codegen && go test -tags codegen_live -run TestLiveSmoke ./...\n" +
		"\n" +
		"test-live-whole:\n" +
		"    cd test/data/codegen && go test -count=1 -tags codegen_live ./...\n" +
		"\n" +
		"test-unit:\n" +
		"    go test ./...\n"
	workflow := "jobs:\n  live:\n    steps:\n      - run: just test-live-half\n      - run: just test-live-absent\n"
	source := "//go:build codegen_live\n\npackage p\n\nimport \"testing\"\n\nfunc TestLiveSmoke(t *testing.T) {}\n"

	root := t.TempDir()
	for path, content := range map[string]string{
		justfilePath:                                 justfile,
		filepath.Join(workflowDir, "live.yml"):       workflow,
		filepath.Join(liveSourceDir, "live_test.go"): source,
	} {
		full := filepath.Join(root, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}

	split, complaints, err := Read(root)
	require.NoError(t, err)
	require.Empty(t, complaints,
		"a recipe the workflow names and the justfile does not falls through to Local rather than complaining")
	require.Equal(t, []string{"TestLiveSmoke"}, split.Declared,
		"the unit recipe builds no live tag, so its command line is in neither half")
	require.Len(t, split.CI, 1)
	require.Equal(t, "test-live-half", split.CI[0].Recipe)
	require.Len(t, split.Local, 1)
	require.Equal(t, "test-live-whole", split.Local[0].Recipe)
	require.Empty(t, split.Complaints())
}

// TestSubtractLeavesLocalRecipesOnlyCIDoesNotReach keeps two recipes running
// byte-identical commands two invocations: collapsing them would move a live
// recipe no workflow reaches into the half that is not checked for it.
func TestSubtractLeavesLocalRecipesOnlyCIDoesNotReach(t *testing.T) {
	one := inv("a", "./...")
	two := inv("b", "./...")
	require.Len(t, subtract([]Invocation{one, two}, []Invocation{one}), 1)
	require.Empty(t, subtract([]Invocation{one, two}, []Invocation{one, two}))
}
