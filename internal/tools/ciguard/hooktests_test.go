package ciguard_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/liverecipes"
)

// The recipe that runs the hooks' own test suites, the hooks tree, and the one
// directory inside it that suites live in. A suite named by the recipe runs
// wherever the recipe runs; one not named there runs nowhere.
const (
	hookTestsRecipe = "test-hooks"
	hooksDir        = ".githooks"
	hookTestsDir    = ".githooks/tests/"
	// The naming convention for a suite. Enforced below rather than assumed:
	// see hookSuites.
	hookSuiteSuffix = "-test.sh"
)

// hookSuites is every file in .githooks/tests/ — this repository's hook test
// suites, under the definition the walk below enforces rather than assumes.
//
// The classifier is the directory, not a glob over it. A glob is a classifier
// that silently skips what it does not recognise: against `*-test.sh`, the
// names `pre-commit-tests.sh`, `pre_push_test.sh`, `hooks.bats` and
// `test-pre-commit.sh` all read as "not a suite", so an unwired one of those
// satisfies a loop over the matches and the totality claimed by the test name
// below is false — measured, all four, on this branch.
//
// So the convention is enforced instead of assumed: a file in .githooks/tests/
// that is not spelled hookSuiteSuffix is refused rather than skipped. That is
// the fail-closed half. A guard may only assume a convention it also enforces.
//
// The walk over .githooks/ itself is the other half: it refuses a suite-looking
// file parked beside the hooks it tests, and it pins the tree's one
// subdirectory by name, so a suite cannot hide in a directory this walk does
// not enter.
//
// The limit, stated rather than left to be found: a hook test suite is a file
// in .githooks/tests/, and that is a definition this walk enforces, not a fact
// about the whole repository. A suite written under some other top-level
// directory is outside this test's reach. The justfile's comment on the recipe
// says where suites go, which is the only thing holding that case.
//
// Dotfiles are skipped in both directories, which is a hole and a deliberate
// one: `.foo-test.sh` would be skipped rather than refused. It is here because
// an editor swap file would otherwise redden the build for whoever has a suite
// open. Nothing in the tree spells a suite that way today.
func hookSuites(t *testing.T) []string {
	t.Helper()

	hooks, err := os.ReadDir(filepath.Join(repoRoot, hooksDir))
	require.NoErrorf(t, err, "read %s", hooksDir)
	for _, e := range hooks {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() {
			require.Equalf(t, filepath.Base(hookTestsDir), e.Name(),
				"%s/%s/ is a directory this walk does not enter, so a hook test suite "+
					"inside it would be neither wired into %q nor reported by this test. "+
					"Suites live in %s.", hooksDir, e.Name(), hookTestsRecipe, hookTestsDir)
			continue
		}
		require.NotContainsf(t, strings.ToLower(e.Name()), "test",
			"%s/%s looks like a test suite but does not live in %s, and %q reaches no "+
				"suite outside it: the recipe names its files by path. An unwired suite "+
				"is an absent gate (bd gqlc-l45j).",
			hooksDir, e.Name(), hookTestsDir, hookTestsRecipe)
	}

	entries, err := os.ReadDir(filepath.Join(repoRoot, hookTestsDir))
	require.NoErrorf(t, err, "read %s", hookTestsDir)
	var suites []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		require.Falsef(t, e.IsDir(), "%s%s is a directory, and %q runs files",
			hookTestsDir, e.Name(), hookTestsRecipe)
		require.Truef(t, strings.HasSuffix(e.Name(), hookSuiteSuffix),
			"%s%s does not end in %q. Rename it: the wiring check below reads this "+
				"directory, and a suite it cannot recognise is one it would skip rather "+
				"than refuse — which is how an unwired suite passes a test whose name "+
				"says every.", hookTestsDir, e.Name(), hookSuiteSuffix)
		suites = append(suites, e.Name())
	}
	sort.Strings(suites)
	return suites
}

// Every hook test suite in this repository has to be named by the recipe that
// runs them, and named in a way that lets a failure through.
//
// This is the wiring, not the content. A suite that is never invoked is not a
// failing gate, it is an absent one: the file stays in the tree, shellcheck
// still lints it, `lint-hooks` still lists it in CI output, and nothing runs a
// single case. That is how the fail-open arm in .githooks/pre-commit survived
// (bd gqlc-l45j) — there was no pre-commit suite at all, and nothing said so.
//
// It lives in ciguard rather than beside the hooks because a shell assertion
// about which shell suites run would have the same hole it is checking for.
//
// RecipeBody strips comments, so a suite commented out of the recipe reads as
// absent rather than as present.
func TestEveryHookTestSuiteIsNamedByTestHooks(t *testing.T) {
	src := readRepoFile(t, justfile)
	body, ok := liverecipes.RecipeBody(src, hookTestsRecipe)
	require.Truef(t, ok, "the %s declares no `%s:` recipe, so nothing runs the hooks' "+
		"own tests", justfile, hookTestsRecipe)

	suites := hookSuites(t)
	require.NotEmptyf(t, suites, "no file in %s, so this test is comparing the recipe "+
		"against an empty set and would pass over a recipe that runs nothing",
		hookTestsDir)

	for _, name := range suites {
		rel := hookTestsDir + name
		require.Containsf(t, body, rel,
			"%s exists but %q does not run it. An unwired suite is an absent gate: it "+
				"is still linted and still listed, and not one of its cases executes.",
			rel, hookTestsRecipe)
	}

	// ...and every line that names a suite has to name a file in the tree and
	// run it under a bare `bash <suite>`.
	//
	// Selected by the directory rather than by `bash`, because the shape of the
	// command is the thing being asserted. just's `-` prefix discards a line's
	// exit status: `-bash .githooks/tests/tool-gate-test.sh` keeps the suite
	// named here, keeps it wired into `just test`, keeps it running on every
	// PR — and throws away every failure it reports. The loop above cannot see
	// that, because the path is still in the body.
	//
	// This is a spelling rule and it does not carry the property on its own —
	// TestTestHooksStopsAtTheFirstFailingSuite does, by running the recipe. It
	// is kept because it names the defect in the failure message, and because a
	// redirection that swallows a suite's output leaves the behavioural test
	// green while making the CI log useless.
	//
	// A named file that is gone is the other direction: it fails the whole of
	// `just test` for a reason unrelated to any change.
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, hookTestsDir) {
			continue
		}
		fields := strings.Fields(line)
		require.Lenf(t, fields, 2, "%s runs %q, which is not a bare `bash <suite>`. "+
			"Anything else — a prefix, a redirection, a `||` — can decide the line's "+
			"exit status independently of what the suite found.", hookTestsRecipe, line)
		require.Equalf(t, "bash", fields[0], "%s runs %q. `%s` is not `bash`; just's `-` "+
			"prefix in particular ignores the line's exit status, so the suite runs on "+
			"every PR and reports failures nothing reads.",
			hookTestsRecipe, line, fields[0])
		require.FileExistsf(t, filepath.Join(repoRoot, fields[1]),
			"%s runs %q, which is not in the tree", hookTestsRecipe, fields[1])
	}
}

// rawRecipe is one justfile recipe exactly as the file spells it, header line
// included and comments intact.
//
// liverecipes.RecipeBody is the wrong instrument for the question below. It
// drops the header, and its comment stripping blanks a `#!/usr/bin/env bash`
// first line — which is the one token deciding whether a failing line aborts
// the recipe or is merely one statement in a script that carries on. Every
// assertion built on RecipeBody is blind to that token by construction.
func rawRecipe(t *testing.T, src, name string) string {
	t.Helper()
	header := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `(\s[^:\n]*)?:`)
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if !header.MatchString(line) {
			continue
		}
		out := []string{line}
		for _, next := range lines[i+1:] {
			indented := strings.HasPrefix(next, " ") || strings.HasPrefix(next, "\t")
			if next != "" && !indented {
				break
			}
			out = append(out, next)
		}
		return strings.Join(out, "\n") + "\n"
	}
	t.Fatalf("the %s declares no `%s` recipe", justfile, name)
	return ""
}

// suitePathsIn is the suite paths a recipe's text names, in the order it names
// them, whatever else each line carries.
func suitePathsIn(recipe string) []string {
	var out []string
	for _, line := range strings.Split(recipe, "\n") {
		for _, f := range strings.Fields(liverecipes.StripComment(line)) {
			if strings.Contains(f, hookTestsDir) {
				out = append(out, f[strings.Index(f, hookTestsDir):])
			}
		}
	}
	return out
}

// recipeRun is what happened when the recipe under test ran against stubs.
type recipeRun struct {
	status int
	// ran holds the indices of the stub suites that executed.
	ran    []int
	suites []string
	output string
}

// runTestHooks writes the recipe under test into a throwaway justfile, stubs
// every suite it names, and runs it through the real `just`.
//
// fail is the index of the suite whose stub exits 1, or -1 for none. Every
// other stub records that it ran, which is what separates "the recipe returned
// non-zero" from "the recipe stopped".
//
// Behavioural because no spelling rule reaches the property. Whether a failing
// suite stops the recipe is decided by which of just's two carriers the recipe
// is written in, and both carriers spell the suite lines identically: converted
// to a `#!/usr/bin/env bash` recipe with its lines untouched, this recipe still
// satisfies every `bash <suite>` rule above while discarding every failure but
// the last. That is not a hypothetical carrier — `lint-hooks` in this same
// justfile is a shebang recipe.
//
// `just` is required rather than skipped over. It is what the CI test job runs
// (asserted below) and what `.githooks/pre-push` runs, so an absent `just` is a
// broken environment rather than a supported one. `go test ./...` reaches this
// package without it, which is exactly the run that would skip past — and a
// skip here would be the fail-open shape this file exists to close.
func runTestHooks(t *testing.T, fail int) recipeRun {
	t.Helper()

	justBin, err := exec.LookPath("just")
	require.NoError(t, err, "`just` is not on PATH, and this test runs the %q recipe "+
		"rather than reading it. CI runs these tests through `just`, so this is a broken "+
		"environment and not a case to skip past.", hookTestsRecipe)

	recipe := rawRecipe(t, readRepoFile(t, justfile), hookTestsRecipe)
	suites := suitePathsIn(recipe)
	require.GreaterOrEqualf(t, len(suites), 2, "%q names %d suites; this test needs at "+
		"least two to tell an abort from a last-line status", hookTestsRecipe, len(suites))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "justfile"), []byte(recipe), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "ran"), 0o755))
	for i, rel := range suites {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		script := "#!/usr/bin/env bash\ntouch ran/" + strconv.Itoa(i) + "\n"
		if i == fail {
			script = "#!/usr/bin/env bash\necho 'stub suite reporting a failure' >&2\nexit 1\n"
		}
		require.NoError(t, os.WriteFile(abs, []byte(script), 0o700))
	}

	cmd := exec.CommandContext(t.Context(), justBin, hookTestsRecipe)
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()

	run := recipeRun{suites: suites, output: string(out)}
	var ee *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &ee):
		run.status = ee.ExitCode()
	default:
		t.Fatalf("could not run %q: %v\n%s", hookTestsRecipe, runErr, out)
	}
	for i := range suites {
		if _, err := os.Stat(filepath.Join(dir, "ran", strconv.Itoa(i))); err == nil {
			run.ran = append(run.ran, i)
		}
	}
	return run
}

// The control for the two directions below: with every stub passing, the
// recipe runs all of them and succeeds.
//
// Without it, a harness that ran nothing at all — a justfile just could not
// parse, a recipe name that no longer exists, stubs written somewhere the
// recipe does not look — would satisfy "non-zero status, no suite ran" and
// report the guard as live while measuring nothing.
func TestTestHooksRunsEverySuiteItNames(t *testing.T) {
	run := runTestHooks(t, -1)
	require.Zerof(t, run.status, "%q exited %d with every suite stubbed to succeed:\n%s",
		hookTestsRecipe, run.status, run.output)
	require.Lenf(t, run.ran, len(run.suites),
		"%q names %d suites but only %v ran. A suite the recipe names and does not "+
			"reach is an absent gate (bd gqlc-l45j). Output:\n%s",
		hookTestsRecipe, len(run.suites), run.ran, run.output)
}

// A failing suite has to stop the recipe, not be reported past.
//
// The first suite is the one made to fail, deliberately. A shebang recipe
// without errexit still returns its last line's status, so failing the last
// suite tells the two carriers apart not at all; failing the first does. The
// second assertion is the one that carries this: nothing after the failure may
// have run.
func TestTestHooksStopsAtTheFirstFailingSuite(t *testing.T) {
	run := runTestHooks(t, 0)
	require.NotZerof(t, run.status,
		"%q exited 0 with its first suite failing. Every failure it reports is being "+
			"discarded — the suites still run, still print, and still gate nothing "+
			"(bd gqlc-l45j). Output:\n%s", hookTestsRecipe, run.output)
	require.Emptyf(t, run.ran,
		"%q ran suites %v after its first one failed, so it is not aborting on a "+
			"failure; whatever status it ends on is the last suite's, not the failing "+
			"one's. Output:\n%s", hookTestsRecipe, run.ran, run.output)
}

// ciJustInvocations is the recipe names each ci.yml job runs through `just`.
func ciJustInvocations(t *testing.T) map[string][]string {
	t.Helper()
	jobs := childByKey(docOf(t), "jobs")
	require.NotNilf(t, jobs, "%s has no jobs", ciWorkflow)

	found := map[string][]string{}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name := jobs.Content[i].Value
		var job struct {
			Steps []actionStep `yaml:"steps"`
		}
		require.NoErrorf(t, jobs.Content[i+1].Decode(&job), "decode job %q", name)
		for _, s := range job.Steps {
			found[name] = append(found[name], justInvocations(s.Run)...)
		}
	}
	return found
}

// ...and the recipe has to be reached by something CI runs.
//
// One line in the tree invokes test-hooks: the dependency list of `test:` in
// the justfile. The other references are prose that runs nothing — a `# Run
// via: just test-hooks` header in six of the suites, and a pointer in
// .githooks/claude-pre-bash — so deleting that single token retires all of its
// suites at once, leaves every assertion above green (they are about what the
// recipe contains, not about whether anyone calls it), and produces no CI
// output to notice. Those headers would still say the suites are run.
//
// Derived through the dependency graph rather than asserting `test: ... test-hooks`
// directly, because the property is that the recipe is reachable from a required
// context, not that it hangs off one particular parent.
func TestTestHooksIsReachedByARecipeCIRuns(t *testing.T) {
	deps := recipeDeps(t)
	require.Containsf(t, deps, hookTestsRecipe,
		"the recipe-header scan did not find %q in the %s, so the walk below starts "+
			"from nothing", hookTestsRecipe, justfile)

	// The walk has to follow edges, not just return its seed: `lint` reaches
	// ensure-golangci only through a dependency list.
	require.Truef(t, recipesReaching(deps, provisionRecipe)["lint"],
		"the dependency walk over the %s does not have `lint` reaching %s, but it "+
			"does. The walk is broken, so this test is asserting nothing.",
		justfile, provisionRecipe)

	ci := ciJustInvocations(t)
	for _, want := range []string{"lint", "actionlint"} {
		require.Containsf(t, flatten(ci), want,
			"the scan of %s found no job running `just %s`, but one does. The scan is "+
				"broken, so the reachability check below is asserting nothing.",
			ciWorkflow, want)
	}

	reaching := recipesReaching(deps, hookTestsRecipe)
	var via []string
	for job, recipes := range ci {
		for _, r := range recipes {
			if reaching[r] {
				via = append(via, job+" runs `just "+r+"`")
			}
		}
	}
	sort.Strings(via)
	require.NotEmptyf(t, via,
		"no job in %s runs a recipe that reaches %q, so none of its suites runs on a "+
			"PR. They are still in the tree, still linted by `lint-hooks`, and still "+
			"listed in its output — and not one case executes (bd gqlc-l45j). The "+
			"recipes that reach it are %v; the recipes CI runs are %v.",
		ciWorkflow, hookTestsRecipe, sortedKeys(reaching), flatten(ci))
}

// flatten is every recipe name in a job-to-recipes map, sorted and deduplicated.
func flatten(byJob map[string][]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, recipes := range byJob {
		for _, r := range recipes {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
