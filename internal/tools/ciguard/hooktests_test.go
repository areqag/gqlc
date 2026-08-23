package ciguard_test

import (
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/liverecipes"
)

// The recipe that runs the hooks' own test suites, the hooks tree, and the one
// directory inside it that suites live in. A suite the recipe's glob reaches
// runs wherever the recipe runs; one outside that directory runs nowhere.
const (
	hookTestsRecipe = "test-hooks"
	hooksDir        = ".githooks"
	hookTestsDir    = ".githooks/tests/"
	// The naming convention for a suite. Enforced below rather than assumed:
	// see hookSuites.
	hookSuiteSuffix = "-test.sh"
	// The pattern the recipe globs, spelled as the recipe spells it. It is the
	// glob-shaped statement of the convention above, and the two are pinned to
	// each other by TestTestHooksGlobsExactlyWhatThisPackageAcceptsAsASuite:
	// the recipe RUNS whatever this matches, and hookSuites REFUSES whatever it
	// does not, so a drift between them is either a suite nobody runs or a file
	// nobody refuses.
	hookTestsGlob = hookTestsDir + "*" + hookSuiteSuffix
)

// hookSuites is every non-dotfile in .githooks/tests/ — this repository's hook
// test suites, under the definition the walk below enforces rather than
// assumes. The dotfile skip at the end of this comment is why that says
// non-dotfile rather than file.
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
// The walk over .githooks/ itself is the other half: it refuses a non-dotfile
// parked beside the hooks it tests whose name carries `test` or ends in
// `.bats` — between them the four spellings above — and it refuses every
// subdirectory but the one suites live in, so a suite cannot hide in a
// directory this walk does not enter. The directory check runs ahead of the
// dotfile skip in both loops, which is what makes that hold for `.hidden` as
// well as for `hidden`; put the skip back in front and
// `.githooks/.hidden/pre-commit-test.sh` goes unseen again (measured, package
// green). That is a rule over names, not a decision procedure: a suite spelled
// outside both, `hooks-spec.sh`, is accepted there, the same hole the dotfile
// skip below has. `.bats` is in the rule because `hooks.bats` carries no
// `test` and the substring alone let it through.
//
// The limit, stated rather than left to be found: a hook test suite is a file
// in .githooks/tests/, and that is a definition this walk enforces, not a fact
// about the whole repository. A suite written under some other top-level
// directory is outside this test's reach. The justfile's comment on the recipe
// says where suites go, which is the only thing holding that case.
//
// Dot-files are skipped in both directories — dot-directories are not, per the
// ordering above — which is a hole and a deliberate one: `.foo-test.sh` in
// .githooks/tests/ and `.hooks.bats` beside the hooks are skipped here rather
// than refused. It is here because an editor swap file would otherwise redden
// the build for whoever has a suite open, and a swap file is a file. Nothing in
// the tree spells a suite that way today.
//
// Only one of those two is a hole in the package.
// TestEveryHookSuiteRunsInTestHooks globs .githooks/tests/ with filepath.Glob,
// whose `*` matches a leading dot where a shell's does not, so `.foo-test.sh`
// reddens there while this walk skips it; `.hooks.bats` is caught by nothing
// (both measured — the first reddens that test, the second leaves the package
// green).
func hookSuites(t *testing.T) []string {
	t.Helper()

	hooks, err := os.ReadDir(filepath.Join(repoRoot, hooksDir))
	require.NoErrorf(t, err, "read %s", hooksDir)
	for _, e := range hooks {
		// Before the dotfile skip, not after: the skip is there for an editor's
		// swap file, and a dot-directory is a whole suite tree.
		if e.IsDir() {
			require.Equalf(t, filepath.Base(hookTestsDir), e.Name(),
				"%s/%s/ is a directory this walk does not enter, so a hook test suite "+
					"inside it would be neither wired into %q nor reported by this test. "+
					"Suites live in %s.", hooksDir, e.Name(), hookTestsRecipe, hookTestsDir)
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		lower := strings.ToLower(e.Name())
		require.Falsef(t, strings.Contains(lower, "test") || strings.HasSuffix(lower, ".bats"),
			"%s/%s looks like a test suite but does not live in %s, and %q reaches no "+
				"suite outside it: the recipe names its files by path. An unwired suite "+
				"is an absent gate (bd gqlc-l45j).",
			hooksDir, e.Name(), hookTestsDir, hookTestsRecipe)
	}

	entries, err := os.ReadDir(filepath.Join(repoRoot, hookTestsDir))
	require.NoErrorf(t, err, "read %s", hookTestsDir)
	var suites []string
	for _, e := range entries {
		// Same ordering, for the same reason: `.githooks/tests/.sub/` would
		// otherwise hold suites this loop never lists.
		require.Falsef(t, e.IsDir(), "%s%s is a directory, and %q runs files",
			hookTestsDir, e.Name(), hookTestsRecipe)
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
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

// expandingLines is the lines of a recipe body that carry `pattern` somewhere
// the shell would expand it — every line carrying it except an `echo`.
//
// The exclusion is not fastidiousness. `test-hooks` prints the pattern in its
// own emptiness refusal, so a Contains over the whole body is satisfied by that
// message with the glob repointed at something matching nothing: measured, the
// assignment changed to `*-tests.sh` and this test passing on the strength of
// the echo alone. A pin a defect cannot fail is reporting on nothing.
//
// It is a rule over one spelling and it says so: a pattern hidden in a `printf`
// or a comment-shaped string would satisfy this, and the property is carried by
// the behavioural rows below rather than here. What this buys is that deleting
// the working glob is not silently covered by the message describing it.
func expandingLines(body, pattern string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, pattern) || strings.HasPrefix(trimmed, "echo ") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// Every hook test suite in this repository has to be reached by the recipe that
// runs them.
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
// The recipe discovers its suites now rather than enumerating them (bd
// gqlc-234l), so this no longer looks for a line per suite; it asks whether the
// pattern the recipe globs reaches each file the directory holds. That change
// is why the append point the four conflicting PRs collided at is gone — but it
// moves the whole of the totality claim onto the pattern, so the pattern is
// pinned twice: here against the tree, and in
// TestTestHooksGlobsExactlyWhatThisPackageAcceptsAsASuite against hookSuites'
// own rule.
//
// RecipeBody strips comments, so a glob commented out of the recipe reads as
// absent rather than as present.
func TestEveryHookTestSuiteIsDiscoveredByTestHooks(t *testing.T) {
	src := readRepoFile(t, justfile)
	body, ok := liverecipes.RecipeBody(src, hookTestsRecipe)
	require.Truef(t, ok, "the %s declares no `%s:` recipe, so nothing runs the hooks' "+
		"own tests", justfile, hookTestsRecipe)

	require.NotEmptyf(t, expandingLines(body, hookTestsGlob),
		"%q does not glob %q anywhere it would expand. It discovers its suites rather "+
			"than listing them, so that pattern is the whole of the wiring: without it "+
			"the recipe reaches no suite at all, and an absent gate is not a failing "+
			"one (bd gqlc-l45j). Body:\n%s",
		hookTestsRecipe, hookTestsGlob, body)

	suites := hookSuites(t)
	require.NotEmptyf(t, suites, "no file in %s, so this test is comparing the recipe "+
		"against an empty set and would pass over a recipe that runs nothing",
		hookTestsDir)

	for _, name := range suites {
		matched, err := path.Match(hookTestsGlob, hookTestsDir+name)
		require.NoErrorf(t, err, "match %q", hookTestsGlob)
		require.Truef(t, matched,
			"%s%s is a suite this package accepts, and the pattern %q the recipe globs "+
				"does not reach it — so it is in the tree, linted, listed, and run by "+
				"nothing (bd gqlc-l45j).", hookTestsDir, name, hookTestsGlob)
	}

	// ...and no line may name an individual suite, which is the property the
	// discovery was adopted FOR.
	//
	// A re-enumeration is the regression that matters here and it is invisible
	// to every other assertion in this file: adding `bash .githooks/tests/foo-test.sh`
	// back beside the glob runs foo-test.sh twice, passes the loop above, passes
	// the behavioural rows below (the stub is idempotent), and reinstates the
	// append point that cost four citizens a hand resolution each (bd
	// gqlc-234l). Refusing the shape is the only thing that sees it.
	//
	// The glob line itself carries the directory too, so it is excluded by
	// spelling rather than by position — a recipe that moved it would otherwise
	// take the whole rule down with it.
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, hookTestsDir) || strings.Contains(line, hookTestsGlob) {
			continue
		}
		t.Errorf("%q spells %q, which names a suite path outside the glob. The recipe "+
			"discovers its suites: an enumeration beside the glob runs that suite "+
			"twice and restores the append point every parallel new suite conflicts "+
			"at (bd gqlc-234l). Delete the line; the file is already run.",
			hookTestsRecipe, strings.TrimSpace(line))
	}
}

// The pattern the recipe globs and the rule hookSuites enforces have to be the
// same rule, because between them they decide whether a file in
// .githooks/tests/ is run, refused, or silently ignored.
//
// Only the third of those is a defect, and it is the one neither side can see
// alone. hookSuites refuses what it does not accept, so nothing can sit there
// unrecognised — but that refusal is worth exactly as much as the claim that
// what it accepts is what the recipe runs, and that claim was free when the
// recipe named each file and is not free now.
//
// The rows are documents taken, not a survey of glob syntax: `accepted` is what
// hookSuites does with the name, and the assertion is that path.Match agrees
// with it on every one.
func TestTestHooksGlobsExactlyWhatThisPackageAcceptsAsASuite(t *testing.T) {
	for _, row := range []struct {
		name     string
		accepted bool
	}{
		{"km-test.sh", true},
		{"a-test.sh", true},
		{"deeply-hyphenated-subject-test.sh", true},
		// hookSuites requires the suffix and refuses everything else, so none
		// of these can reach the tree — and the glob has to agree, or the
		// refusal is what is holding the totality rather than the pattern.
		{"pre_push_test.sh", false},
		{"pre-commit-tests.sh", false},
		{"hooks.bats", false},
		{"test-pre-commit.sh", false},
		{"README.md", false},
		// The bare suffix with no subject. Refused by neither rule: `*` matches
		// the empty string in path.Match as it does in a shell, and hookSuites
		// asks only for the suffix. Recorded so the two are known to agree
		// rather than assumed to.
		{"-test.sh", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			require.Equalf(t, row.accepted, strings.HasSuffix(row.name, hookSuiteSuffix),
				"this row's premise is that hookSuites %s %q",
				map[bool]string{true: "accepts", false: "refuses"}[row.accepted], row.name)

			matched, err := path.Match(hookTestsGlob, hookTestsDir+row.name)
			require.NoErrorf(t, err, "match %q", hookTestsGlob)
			require.Equalf(t, row.accepted, matched,
				"hookSuites and the %q glob disagree about %q: one of them lets a file "+
					"through that the other does not see. A name accepted here and not "+
					"globbed is a suite nothing runs; a name globbed and not accepted is "+
					"a file this package would never have refused.",
				hookTestsRecipe, row.name)
		})
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

// recipeRun is what happened when the recipe under test ran against stubs.
type recipeRun struct {
	status int
	// ran holds the names of the stub suites that executed, sorted.
	ran     []string
	planted []string
	output  string
}

// runTestHooks writes the recipe under test into a throwaway tree, plants
// `planted` as files under .githooks/tests/, and runs it through the real
// `just`.
//
// fail names the stub that exits 1, or "" for none. Every other stub records
// that it ran, which is what separates "the recipe returned non-zero" from "the
// recipe stopped".
//
// The stub names are the harness's, not the repository's. That is the point:
// the recipe discovers its suites, so a name it has never heard of has to run
// on the strength of the file existing and nothing else (bd gqlc-234l). Under
// the enumeration this harness had to read the paths back out of the recipe
// text, because a name the recipe did not spell could not run at all — which is
// the append point, stated as a property of the test.
//
// Behavioural, and not because the carrier is unreadable. Whether a failing
// suite stops the recipe is decided by which of just's two carriers it is
// written in and by what the body sets: this recipe is a `#!/usr/bin/env bash`
// one, which hands its whole body to a single shell and returns only the last
// line's status unless that shell sets errexit. Delete the `set -euo pipefail`
// and every text rule above still passes.
//
// A rule over the recipe text could see that: require the body to spell
// `set -e`. It is refused here because it pins the carrier rather than the
// property — the enumeration this replaced had the property with no `set -e`
// anywhere in it. Running the recipe asks the question directly.
//
// `just` is required rather than skipped over. It is what the CI test job runs
// (asserted below) and what `.githooks/pre-push` runs, so an absent `just` is a
// broken environment rather than a supported one. `go test ./...` reaches this
// package without it, which is exactly the run that would skip past — and a
// skip here would be the fail-open shape this file exists to close.
func runTestHooks(t *testing.T, planted []string, fail string) recipeRun {
	t.Helper()

	justBin, err := exec.LookPath("just")
	require.NoError(t, err, "`just` is not on PATH, and this test runs the %q recipe "+
		"rather than reading it. CI runs these tests through `just`, so this is a broken "+
		"environment and not a case to skip past.", hookTestsRecipe)

	recipe := rawRecipe(t, readRepoFile(t, justfile), hookTestsRecipe)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "justfile"), []byte(recipe), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "ran"), 0o755))
	// Created even when nothing is planted: the empty-discovery row is about a
	// directory with no suite in it, not about a directory that is not there,
	// and the two would otherwise be the same fixture.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.FromSlash(hookTestsDir)), 0o755))

	for _, name := range planted {
		abs := filepath.Join(dir, filepath.FromSlash(hookTestsDir), name)
		// `ran/<name>` rather than an index: the recipe decides the order, so an
		// index assigned here would name a different stub than the one that ran.
		script := "#!/usr/bin/env bash\ntouch " + shQuote(filepath.Join(dir, "ran", name)) + "\n"
		if name == fail {
			script = "#!/usr/bin/env bash\necho 'stub suite reporting a failure' >&2\nexit 1\n"
		}
		require.NoError(t, os.WriteFile(abs, []byte(script), 0o700))
	}

	cmd := exec.CommandContext(t.Context(), justBin, hookTestsRecipe)
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()

	run := recipeRun{planted: planted, output: string(out)}
	var ee *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &ee):
		run.status = ee.ExitCode()
	default:
		t.Fatalf("could not run %q: %v\n%s", hookTestsRecipe, runErr, out)
	}
	for _, name := range planted {
		if _, err := os.Stat(filepath.Join(dir, "ran", name)); err == nil {
			run.ran = append(run.ran, name)
		}
	}
	sort.Strings(run.ran)
	return run
}

// shQuote is s as a single-quoted shell word.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// The control for the directions below, and the zero-conflict claim itself:
// with three stubs planted under names that appear nowhere in the justfile, the
// recipe runs all three and succeeds.
//
// That is bd gqlc-234l's fix, measured rather than argued. A citizen adding a
// suite writes one file and edits nothing — so two citizens adding suites in
// parallel touch disjoint paths, where under the enumeration they appended at
// one location and every pair conflicted.
//
// Without this control, a harness that ran nothing at all — a justfile just
// could not parse, a recipe name that no longer exists, stubs written somewhere
// the recipe does not look — would satisfy "non-zero status, no suite ran" and
// report the guard as live while measuring nothing.
func TestTestHooksRunsEverySuiteItDiscovers(t *testing.T) {
	planted := []string{"aaa-test.sh", "mmm-test.sh", "zzz-test.sh"}
	run := runTestHooks(t, planted, "")
	require.Zerof(t, run.status, "%q exited %d with every suite stubbed to succeed:\n%s",
		hookTestsRecipe, run.status, run.output)
	require.Equalf(t, planted, run.ran,
		"%q discovered %v of the %v planted in %s. The recipe globs that directory, "+
			"so a file there that does not run is an absent gate reached by nothing "+
			"(bd gqlc-l45j). Output:\n%s",
		hookTestsRecipe, run.ran, planted, hookTestsDir, run.output)
}

// Discovery that discovers nothing has to FAIL.
//
// This is the failure mode the glob introduced and the enumeration could not
// have: an empty match runs zero suites, and a loop over zero suites exits 0.
// On every dashboard that reads exactly like fourteen suites passing — `just
// test` green, the `test` check green, the required context satisfied — with
// not one hook assertion made. A detector that exits 0 is not a gate.
//
// It is reachable without malice: the recipe is run from a working directory
// that is not the repository root, a rename of .githooks/tests/, a `shopt -u
// nullglob` that turns the empty case into a different error, or a pattern
// edited to one nothing matches.
//
// The message is asserted, not just the status. Two things in the recipe exit
// non-zero — the emptiness refusal and a suite failing — and with nothing
// planted there is no suite to fail, so a status alone would go green on a
// recipe that died for any reason at all, `set -u` on an unset variable
// included.
func TestTestHooksFailsWhenItDiscoversNoSuite(t *testing.T) {
	run := runTestHooks(t, nil, "")
	require.NotZerof(t, run.status,
		"%q exited 0 against an empty %s. It ran zero suites and reported success, "+
			"which is indistinguishable from every suite passing (bd gqlc-234l). "+
			"Output:\n%s", hookTestsRecipe, hookTestsDir, run.output)
	require.Containsf(t, run.output, "matched nothing",
		"%q failed against an empty %s, but not with the emptiness refusal — so this "+
			"row is green on whatever else went wrong and would stay green with the "+
			"refusal deleted. Output:\n%s", hookTestsRecipe, hookTestsDir, run.output)
}

// The glob's limit, recorded as a row rather than left to be discovered: a file
// in .githooks/tests/ that does not carry the suffix is not run.
//
// This is the hole hookSuites exists to close, and stating it here is what
// makes the pair legible. The recipe cannot refuse `pre_push_test.sh` — a shell
// glob skips what it does not match, silently, which is precisely the objection
// recorded against globbing on bd gqlc-234l. What answers it is that such a
// file cannot reach the tree: hookSuites REFUSES it, and
// TestTestHooksGlobsExactlyWhatThisPackageAcceptsAsASuite pins the two rules to
// each other. Delete either and this row becomes the description of a live
// defect instead of a division of labour.
//
// The dotfile is the second name, and it is a deliberate hole in both: an
// editor swap file beside a suite must not redden the build.
func TestTestHooksSkipsWhatItsPatternDoesNotMatch(t *testing.T) {
	planted := []string{"aaa-test.sh", "pre_push_test.sh", ".swp-test.sh"}
	run := runTestHooks(t, planted, "")
	require.Zerof(t, run.status, "%q exited %d:\n%s", hookTestsRecipe, run.status, run.output)
	require.Equalf(t, []string{"aaa-test.sh"}, run.ran,
		"%q ran %v. Only the name matching %q may run here: the other two are the "+
			"glob's blind spot, and what keeps that from being a hole is hookSuites "+
			"refusing the first and the swap-file hole being deliberate for the "+
			"second. Output:\n%s",
		hookTestsRecipe, run.ran, hookTestsGlob, run.output)
}

// A failing suite has to stop the recipe, not be reported past.
//
// The FIRST suite in the order the recipe runs them is the one made to fail,
// deliberately: a shebang recipe without errexit still returns its last line's
// status, so failing the last suite tells the two carriers apart not at all.
// The recipe pins its glob's collation to C, so the first is the
// lexicographically smallest name planted — asserted below rather than assumed,
// since a recipe that ran them in some other order would make this row fail the
// wrong one and the second assertion would go green on a real regression.
//
// The second assertion is the one that carries this: nothing after the failure
// may have run.
func TestTestHooksStopsAtTheFirstFailingSuite(t *testing.T) {
	planted := []string{"aaa-test.sh", "mmm-test.sh", "zzz-test.sh"}

	order := runTestHooks(t, planted, "")
	require.Zerof(t, order.status, "the ordering control exited %d:\n%s",
		order.status, order.output)
	require.Lessf(t, strings.Index(order.output, planted[0]), strings.Index(order.output, planted[1]),
		"%q does not run %q before %q, so failing %q below is not failing the first "+
			"suite and the abort assertion is measuring nothing. Output:\n%s",
		hookTestsRecipe, planted[0], planted[1], planted[0], order.output)

	run := runTestHooks(t, planted, planted[0])
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
	jobs := childByKey(ciDoc(t), "jobs")
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
// Deleting `test-hooks` from the dependency list of `test:` in the justfile
// retires every suite the recipe names at once — from the CI test job and from
// .githooks/pre-push, which both run `just test` — while leaving every
// assertion above green: they are about what the recipe contains, not about
// whether anyone calls it. Nothing above turns red to say so, which is why this
// one is here. What CI would lose is the suites' own output, a signal only to
// someone counting `ok` lines, and their `# Run via: just test-hooks` headers
// would still say they are run.
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
