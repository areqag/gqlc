package ciguard_test

import (
	"errors"
	"fmt"
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

// The recipe that runs the hooks' own test suites, the hooks tree, and the
// directories inside the repository that suites live in. A suite one of the
// recipe's globs reaches runs wherever the recipe runs; one outside them runs
// nowhere.
const (
	hookTestsRecipe = "test-hooks"
	hooksDir        = ".githooks"
	hookTestsDir    = ".githooks/tests/"
	// The second suite home. .github/scripts/tests/ holds the suite covering
	// the two PR gates that live beside it, and it is there deliberately: the
	// scripts are not hooks. It was outside every rule in this file until bd
	// gqlc-snzq F4, and outside `test-hooks` until bd gqlc-xqf6 — CI ran it
	// from a step of its own, so `just test` and .githooks/pre-push did not.
	scriptTestsDir = ".github/scripts/tests/"
	// The naming convention for a suite. Enforced below rather than assumed:
	// see hookSuites.
	hookSuiteSuffix = "-test.sh"
)

// suiteDirs is every directory a runnable suite may live in, and hookTestsGlobs
// is the same statement as the patterns the recipe globs, spelled as the recipe
// spells them.
//
// The two are pinned to each other and to the name rule by
// TestTestHooksGlobsExactlyWhatThisPackageAcceptsAsASuite: the recipe RUNS
// whatever these match, hookSuites REFUSES whatever they do not, and
// TestNoSuiteLivesWhereNothingSweepsIt refuses a suite outside the directories
// entirely. A drift between them is either a suite nobody runs or a file nobody
// refuses.
var (
	suiteDirs      = []string{hookTestsDir, scriptTestsDir}
	hookTestsGlobs = []string{hookTestsDir + "*" + hookSuiteSuffix, scriptTestsDir + "*" + hookSuiteSuffix}
)

// globsReach reports whether any pattern the recipe globs REACHES the
// repo-relative path of a suite, under the shell's rule and not path.Match's.
//
// The leading-dot clause is the difference between the two, and it is not
// cosmetic: path.Match's `*` matches a leading dot and a shell's does not, so
// `.githooks/tests/.foo-test.sh` reads as globbed to Go and is not run by the
// recipe. Without the clause the walk would ACCEPT that name as a suite (it
// carries the suffix) and the recipe would skip it — a suite in a suite
// directory that runs nowhere, which is the whole class this file refuses.
// acceptedInSuiteDir refuses it in the same breath, so the two agree.
func globsReach(t *testing.T, suitePath string) bool {
	t.Helper()
	if strings.HasPrefix(path.Base(suitePath), ".") {
		return false
	}
	for _, glob := range hookTestsGlobs {
		matched, err := path.Match(glob, suitePath)
		require.NoErrorf(t, err, "match %q", glob)
		if matched {
			return true
		}
	}
	return false
}

// acceptedInSuiteDir is the rule surveyHooks applies to a file already sitting
// in a suite directory: the suffix, and not a name the recipe's glob cannot
// reach. Named so that the agreement test asks the walk's own question rather
// than restating it.
func acceptedInSuiteDir(name string) bool {
	return !strings.HasPrefix(name, ".") && strings.HasSuffix(name, hookSuiteSuffix)
}

// namesASuiteDir and spellsAGlob part a recipe body line that ENUMERATES a
// suite from one that globs a directory. The glob lines carry their directory
// too, so they are excluded by spelling rather than by position — a recipe that
// moved one would otherwise take the whole rule down with it.
func namesASuiteDir(line string) bool {
	for _, dir := range suiteDirs {
		if strings.Contains(line, dir) {
			return true
		}
	}
	return false
}

func spellsAGlob(line string) bool {
	for _, glob := range hookTestsGlobs {
		if strings.Contains(line, glob) {
			return true
		}
	}
	return false
}

// suiteShaped reports whether a file name is one a reader would take for a
// shell test suite.
//
// A rule over names, and it says so: it is what decides whether a file parked
// outside a suite directory is refused, and no rule over names is a decision
// procedure for "is this a test suite". What it is held to is that it accepts
// every spelling anyone here has actually reached for.
//
// The `spec` term and the extension-less arm are bd gqlc-snzq F3 and its
// neighbours: `hooks-spec.sh` carries no `test` and was accepted by nothing, so
// a suite could sit beside the hooks under that name and run nowhere. `.bats`
// is here because `hooks.bats` carries no `test` either. The extension-less arm
// is for the hooks tree, whose own files carry no extension — `run-tests` beside
// `pre-commit` is the shape.
//
// The limit: a suite named for its subject alone, `pre-commit-checks.sh`, is
// accepted by no term here and is refused by nothing. That is the residue the
// name rule cannot reach, and what stands behind it is the recipe running
// everything in the suite directories, so a file that IS in one of them runs
// whatever it is called.
func suiteShaped(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".bats") {
		return true
	}
	if !strings.Contains(lower, "test") && !strings.Contains(lower, "spec") {
		return false
	}
	return strings.HasSuffix(lower, ".sh") || !strings.Contains(lower, ".")
}

// editorScratch reports whether a name is an editor's leavings rather than a
// file anybody wrote on purpose.
//
// This replaces a blanket dotfile skip, which was bd gqlc-snzq F2: every rule
// in this file began by skipping any name starting with `.`, so
// `.githooks/.hooks.bats` was accepted by nothing and could be added and
// silently never run. The skip exists so that a swap file beside a suite does
// not redden the build for whoever has that suite open, and that is a claim
// about vim's and emacs's spellings, not about the leading dot — so it is
// spelled as those and the dot is no longer a free pass.
func editorScratch(name string) bool {
	base := strings.ToLower(name)
	for _, suffix := range []string{".swp", ".swo", ".swn", ".swx", ".tmp", "~", ".orig", ".rej", ".bak"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	// emacs writes `#file#` for an auto-save and `.#file` for a lock.
	return strings.HasPrefix(base, ".#") || (strings.HasPrefix(base, "#") && strings.HasSuffix(base, "#"))
}

// isDir reports whether entry e of dir is a directory, FOLLOWING a symlink.
//
// os.ReadDir's DirEntry answers about the link and not about its target, so
// IsDir() is false for a symlink to a directory and the entry falls through to
// the name rule instead of being treated as a directory (bd gqlc-snzq F7). The
// walk's whole directory clause — refuse every subdirectory but the ones suites
// live in, so a suite cannot hide in a directory nothing enters — is defeated by
// `ln -s somewhere .githooks/x` without it.
//
// A broken symlink is not a directory and is not followed; os.Stat fails and
// the entry is read as a file, which is the direction that refuses.
func isDir(dir string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	info, err := os.Stat(filepath.Join(dir, e.Name()))
	return err == nil && info.IsDir()
}

// surveyHooks reads the hooks tree the way the walk below defines it: it returns
// every suite it found under the suite directories, and one complaint per file
// or directory that breaks a rule.
//
// Separated from the assertion so the rules can be driven over a fixture tree
// rather than only over this repository, which is what lets the symlink, the
// dotfile and the `spec` name have rows of their own. Over this repository the
// complaint list has to be empty.
func surveyHooks(root string) (suites []string, complaints []string) {
	complain := func(format string, args ...any) {
		complaints = append(complaints, fmt.Sprintf(format, args...))
	}

	hooksPath := filepath.Join(root, hooksDir)
	hooks, err := os.ReadDir(hooksPath)
	if err != nil {
		complain("read %s: %v", hooksDir, err)
		return nil, complaints
	}
	for _, e := range hooks {
		// Before the scratch-file skip, not after: the skip is there for an
		// editor's swap file, and a dot-directory is a whole suite tree.
		if isDir(hooksPath, e) {
			if e.Name() != filepath.Base(hookTestsDir) {
				complain("%s/%s/ is a directory this walk does not enter, so a hook test "+
					"suite inside it would be neither wired into %q nor reported by this "+
					"test. Suites live in %v.", hooksDir, e.Name(), hookTestsRecipe, suiteDirs)
			}
			continue
		}
		if editorScratch(e.Name()) {
			continue
		}
		if suiteShaped(e.Name()) {
			complain("%s/%s looks like a test suite but does not live in one of %v, and "+
				"%q reaches no suite outside them: the recipe globs those directories. "+
				"An unwired suite is an absent gate (bd gqlc-l45j).",
				hooksDir, e.Name(), suiteDirs, hookTestsRecipe)
		}
	}

	for _, dir := range suiteDirs {
		dirPath := filepath.Join(root, filepath.FromSlash(dir))
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			complain("read %s: %v", dir, err)
			continue
		}
		for _, e := range entries {
			// Same ordering, for the same reason: `<dir>/.sub/` would otherwise
			// hold suites this loop never lists.
			if isDir(dirPath, e) {
				complain("%s%s is a directory, and %q runs files", dir, e.Name(), hookTestsRecipe)
				continue
			}
			if editorScratch(e.Name()) {
				continue
			}
			if strings.HasPrefix(e.Name(), ".") {
				complain("%s%s begins with a dot, and a shell glob's `*` does not match "+
					"one — so it sits in a suite directory and %q reaches it with nothing. "+
					"Rename it, or if it is an editor's leavings teach editorScratch about "+
					"the spelling.", dir, e.Name(), hookTestsRecipe)
				continue
			}
			if !acceptedInSuiteDir(e.Name()) {
				complain("%s%s does not end in %q. Rename it: the wiring check below reads "+
					"this directory, and a suite it cannot recognise is one it would skip "+
					"rather than refuse — which is how an unwired suite passes a test whose "+
					"name says every.", dir, e.Name(), hookSuiteSuffix)
				continue
			}
			suites = append(suites, dir+e.Name())
		}
	}
	sort.Strings(suites)
	return suites, complaints
}

// hookSuites is every hook test suite in this repository, as a repo-relative
// path, under the definition surveyHooks enforces rather than assumes.
//
// The classifier is the directory, not a glob over it. A glob is a classifier
// that silently skips what it does not recognise: against `*-test.sh`, the
// names `pre-commit-tests.sh`, `pre_push_test.sh`, `hooks.bats` and
// `test-pre-commit.sh` all read as "not a suite", so an unwired one of those
// satisfies a loop over the matches and the totality claimed by the test name
// below is false — measured, all four, on this branch.
//
// So the convention is enforced instead of assumed: a file in a suite directory
// that is not spelled hookSuiteSuffix is refused rather than skipped. That is
// the fail-closed half. A guard may only assume a convention it also enforces.
//
// The walk over .githooks/ itself is the second half — a suite-shaped file
// parked beside the hooks it tests is refused, and so is every subdirectory but
// the one suites live in — and TestNoSuiteLivesWhereNothingSweepsIt is the
// third, which asks the same question of the whole tracked tree rather than of
// .githooks/ alone.
func hookSuites(t *testing.T) []string {
	t.Helper()
	suites, complaints := surveyHooks(repoRoot)
	require.Emptyf(t, complaints, "the hooks tree breaks the rules that make a suite "+
		"reachable:\n  %s", strings.Join(complaints, "\n  "))
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

	for _, glob := range hookTestsGlobs {
		require.NotEmptyf(t, expandingLines(body, glob),
			"%q does not glob %q anywhere it would expand. It discovers its suites rather "+
				"than listing them, so those patterns are the whole of the wiring: without "+
				"one of them the recipe reaches no suite in that directory at all, and an "+
				"absent gate is not a failing one (bd gqlc-l45j). Body:\n%s",
			hookTestsRecipe, glob, body)
	}

	suites := hookSuites(t)
	require.NotEmptyf(t, suites, "no file in %v, so this test is comparing the recipe "+
		"against an empty set and would pass over a recipe that runs nothing",
		suiteDirs)

	for _, suite := range suites {
		require.Truef(t, globsReach(t, suite),
			"%s is a suite this package accepts, and none of the patterns %v the recipe "+
				"globs reaches it — so it is in the tree, linted, listed, and run by "+
				"nothing (bd gqlc-l45j).", suite, hookTestsGlobs)
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
		if !namesASuiteDir(line) || spellsAGlob(line) {
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
		// The leading dot, which is the one name where path.Match and a shell
		// part company. Both sides refuse it — the walk by name, the glob by
		// the clause in globsReach — so a dotfile carrying the suffix is a
		// complaint rather than a suite nothing runs.
		{".foo-test.sh", false},
		{".hooks.bats", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			require.Equalf(t, row.accepted, acceptedInSuiteDir(row.name),
				"this row's premise is that hookSuites %s %q",
				map[bool]string{true: "accepts", false: "refuses"}[row.accepted], row.name)

			require.Equalf(t, row.accepted, globsReach(t, hookTestsDir+row.name),
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
// `planted` as files under the suite directories that key it, and runs it
// through the real `just`.
//
// Keyed by directory rather than a flat list because the recipe globs TWO
// directories now (bd gqlc-snzq F4, bd gqlc-xqf6), and a harness that could
// only plant in one of them would report the second swept without ever putting
// a file in it.
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
func runTestHooks(t *testing.T, planted map[string][]string, fail string) recipeRun {
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
	for _, sd := range suiteDirs {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.FromSlash(sd)), 0o755))
	}

	var all []string
	for suiteDir, names := range planted {
		require.Containsf(t, suiteDirs, suiteDir,
			"%q is not a directory the recipe globs, so planting there measures nothing",
			suiteDir)
		for _, name := range names {
			abs := filepath.Join(dir, filepath.FromSlash(suiteDir), name)
			// `ran/<name>` rather than an index: the recipe decides the order, so an
			// index assigned here would name a different stub than the one that ran.
			script := "#!/usr/bin/env bash\ntouch " + shQuote(filepath.Join(dir, "ran", name)) + "\n"
			if name == fail {
				script = "#!/usr/bin/env bash\necho 'stub suite reporting a failure' >&2\nexit 1\n"
			}
			require.NoError(t, os.WriteFile(abs, []byte(script), 0o700))
			all = append(all, name)
		}
	}
	sort.Strings(all)

	cmd := exec.CommandContext(t.Context(), justBin, hookTestsRecipe)
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()

	run := recipeRun{planted: all, output: string(out)}
	var ee *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &ee):
		run.status = ee.ExitCode()
	default:
		t.Fatalf("could not run %q: %v\n%s", hookTestsRecipe, runErr, out)
	}
	for _, name := range all {
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
	// Planted in BOTH suite directories, because a stub in only one of them
	// leaves the other's glob asserted by nothing but its presence in the text.
	planted := map[string][]string{
		hookTestsDir:   {"aaa-test.sh", "mmm-test.sh", "zzz-test.sh"},
		scriptTestsDir: {"sss-test.sh"},
	}
	run := runTestHooks(t, planted, "")
	require.Zerof(t, run.status, "%q exited %d with every suite stubbed to succeed:\n%s",
		hookTestsRecipe, run.status, run.output)
	require.Equalf(t, run.planted, run.ran,
		"%q discovered %v of the %v planted in %v. The recipe globs those directories, "+
			"so a file in one that does not run is an absent gate reached by nothing "+
			"(bd gqlc-l45j). Output:\n%s",
		hookTestsRecipe, run.ran, run.planted, suiteDirs, run.output)
}

// ...and a suite directory that goes silent on its own has to FAIL.
//
// The aggregate shape of this guard is the defect it is written against: one
// emptiness check over the two globs combined fires only when BOTH match
// nothing, so renaming or deleting .github/scripts/tests/ leaves the recipe
// green on the strength of .githooks/tests/ still matching — a set of suites
// nobody runs, and nothing says so.
//
// The directory that goes silent here is the SECOND one, deliberately: the
// first is the one every other row plants in, so a guard that only looked at
// it would still pass every row above.
func TestTestHooksFailsWhenOneSuiteDirectoryGoesSilent(t *testing.T) {
	run := runTestHooks(t, map[string][]string{
		hookTestsDir: {"aaa-test.sh"},
	}, "")
	require.NotZerof(t, run.status,
		"%q exited 0 with %s empty. Every suite in that directory is reached by "+
			"nothing, and the recipe reports success over them. Output:\n%s",
		hookTestsRecipe, scriptTestsDir, run.output)
	require.Containsf(t, run.output, "matched nothing",
		"%q failed with %s empty, but not with the emptiness refusal — so this row is "+
			"green on whatever else went wrong. Output:\n%s",
		hookTestsRecipe, scriptTestsDir, run.output)
	require.Emptyf(t, run.ran,
		"%q ran %v before refusing. The refusal has to come first: a partial run "+
			"followed by an error reads, in a log, like the suites that did run being "+
			"all there were. Output:\n%s", hookTestsRecipe, run.ran, run.output)
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
	planted := map[string][]string{
		hookTestsDir:   {"aaa-test.sh", "pre_push_test.sh", ".swp-test.sh"},
		scriptTestsDir: {"sss-test.sh"},
	}
	run := runTestHooks(t, planted, "")
	require.Zerof(t, run.status, "%q exited %d:\n%s", hookTestsRecipe, run.status, run.output)
	require.Equalf(t, []string{"aaa-test.sh", "sss-test.sh"}, run.ran,
		"%q ran %v. Only the names matching %v may run here: the other two are the "+
			"glob's blind spot, and what keeps that from being a hole is hookSuites "+
			"refusing the first and the swap-file hole being deliberate for the "+
			"second. Output:\n%s",
		hookTestsRecipe, run.ran, hookTestsGlobs, run.output)
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
	first, second := "aaa-test.sh", "mmm-test.sh"
	planted := map[string][]string{
		hookTestsDir:   {first, second, "zzz-test.sh"},
		scriptTestsDir: {"sss-test.sh"},
	}

	order := runTestHooks(t, planted, "")
	require.Zerof(t, order.status, "the ordering control exited %d:\n%s",
		order.status, order.output)
	require.Lessf(t, strings.Index(order.output, first), strings.Index(order.output, second),
		"%q does not run %q before %q, so failing %q below is not failing the first "+
			"suite and the abort assertion is measuring nothing. Output:\n%s",
		hookTestsRecipe, first, second, first, order.output)

	run := runTestHooks(t, planted, first)
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

// plantTree writes a hooks tree under a throwaway root: `files` is
// repo-relative path to contents, and both suite directories are created
// whether or not anything is planted in them, so an absent directory and an
// empty one are not the same fixture.
func plantTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range append([]string{hooksDir}, suiteDirs...) {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755))
	}
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o600))
	}
	return root
}

// The rules surveyHooks enforces, driven over fixture trees rather than only
// over this repository — because over this repository every rule is satisfied,
// so a rule that had been deleted would look exactly like a rule that holds.
//
// Each row is one file or directory planted beside a healthy baseline, and
// `refused` says whether the walk must complain about it. The rows carrying a
// bd reference are the six measured gaps in bd gqlc-snzq.
func TestTheHooksWalkRefusesWhatWouldRunNowhere(t *testing.T) {
	const stub = "#!/usr/bin/env bash\nexit 0\n"
	baseline := map[string]string{
		hooksDir + "/pre-commit":          stub,
		hooksDir + "/git-env-sandbox.sh":  stub,
		hookTestsDir + "km-test.sh":       stub,
		scriptTestsDir + "gate-a-test.sh": stub,
	}

	for _, row := range []struct {
		name    string
		plant   string
		symlink bool
		refused bool
		says    string
	}{
		{name: "the baseline alone", refused: false},
		// F3. `spec` carries no `test` and no `.bats`, so the old rule admitted
		// a suite-shaped file parked beside the hooks it tests.
		{
			name: "a spec-named file beside the hooks", plant: hooksDir + "/hooks-spec.sh",
			refused: true, says: "looks like a test suite",
		},
		// F2. The old rule skipped every dot-prefixed name before asking, so a
		// suite could be added under one and refused by nothing.
		{
			name: "a dot-prefixed bats suite beside the hooks", plant: hooksDir + "/.hooks.bats",
			refused: true, says: "looks like a test suite",
		},
		{
			name: "a test-named file beside the hooks", plant: hooksDir + "/pre-commit-tests.sh",
			refused: true, says: "looks like a test suite",
		},
		{
			name: "an extension-less test runner beside the hooks", plant: hooksDir + "/run-tests",
			refused: true, says: "looks like a test suite",
		},
		// The deliberate hole, now spelled as the editor's own names rather
		// than as any leading dot: a swap file must not redden the build for
		// whoever has a suite open.
		{
			name: "a vim swap file beside the hooks", plant: hooksDir + "/.pre-commit.swp",
			refused: false,
		},
		{
			name: "an emacs lock file beside the hooks", plant: hooksDir + "/.#pre-commit",
			refused: false,
		},
		{name: "a plain hook", plant: hooksDir + "/post-checkout", refused: false},
		// F7. os.ReadDir's DirEntry answers about the LINK, so IsDir() is false
		// and the entry used to fall through to the name rule — the directory
		// clause, whose whole job is that a suite cannot hide in a directory
		// this walk does not enter, was defeated by a symlink.
		{
			name: "a symlink to a directory beside the hooks", plant: hooksDir + "/vendor",
			symlink: true, refused: true, says: "is a directory this walk does not enter",
		},
		{
			name: "a real directory beside the hooks", plant: hooksDir + "/helpers/x.sh",
			refused: true, says: "is a directory this walk does not enter",
		},
		{
			name:  "a file in a suite directory without the suffix",
			plant: hookTestsDir + "README.md", refused: true, says: "does not end in",
		},
		{
			name:  "a dotfile carrying the suffix in a suite directory",
			plant: hookTestsDir + ".foo-test.sh", refused: true, says: "begins with a dot",
		},
		{
			name:  "a suite in the second suite directory",
			plant: scriptTestsDir + "gate-b-test.sh", refused: false,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			files := map[string]string{}
			for k, v := range baseline {
				files[k] = v
			}
			if row.plant != "" && !row.symlink {
				files[row.plant] = stub
			}
			root := plantTree(t, files)
			if row.symlink {
				target := filepath.Join(root, "elsewhere")
				require.NoError(t, os.MkdirAll(target, 0o755))
				require.NoError(t, os.Symlink(target, filepath.Join(root, filepath.FromSlash(row.plant))))
			}

			_, complaints := surveyHooks(root)
			if !row.refused {
				require.Emptyf(t, complaints, "%q is refused by the walk and must not be",
					row.plant)
				return
			}
			require.NotEmptyf(t, complaints,
				"%q is accepted by the walk. A file the walk does not refuse and the "+
					"recipe does not run is an absent gate (bd gqlc-l45j).", row.plant)
			// The REASON, not just the fact: several rules can complain about a
			// tree, and a row green on the wrong one stays green with its own
			// rule deleted.
			require.Containsf(t, strings.Join(complaints, "\n"), row.says,
				"the walk refused %q for some other reason:\n  %s",
				row.plant, strings.Join(complaints, "\n  "))
		})
	}
}

// ...and no suite may live anywhere in the tracked tree but the directories the
// recipe sweeps (bd gqlc-snzq F4).
//
// The walk above is rooted at .githooks/, so a suite written under any other
// top-level directory was outside its reach entirely — and one is: the suite
// covering the two PR gates lives in .github/scripts/tests/, and until bd
// gqlc-xqf6 it was run by a step of ci.yml and by nothing a developer invokes.
// Stating the limit in a comment is what this file did; asking the question of
// the whole tree is what closes it.
//
// Read from `git ls-files` rather than a filesystem walk: the question is about
// the repository, and an untracked scratch file in someone's tree is not the
// repository's problem. The emptiness guard is the fail-closed half — a `git`
// that returns nothing would otherwise report a clean sweep.
func TestNoSuiteLivesWhereNothingSweepsIt(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	require.NoError(t, err, "`git` is not on PATH, and this test asks git which files "+
		"the repository tracks")

	cmd := exec.CommandContext(t.Context(), gitBin, "ls-files", "-z")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	require.NoErrorf(t, err, "git ls-files in %s", repoRoot)

	var tracked []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			tracked = append(tracked, p)
		}
	}
	require.Greaterf(t, len(tracked), 100, "git ls-files returned %d paths, which is "+
		"not this repository — the sweep below would then be clean because it looked "+
		"at nothing", len(tracked))

	var found, misplaced []string
	for _, p := range tracked {
		base := path.Base(p)
		if !suiteShaped(base) || editorScratch(base) {
			continue
		}
		found = append(found, p)
		inASuiteDir := false
		for _, dir := range suiteDirs {
			if strings.HasPrefix(p, dir) && path.Dir(p)+"/" == dir {
				inASuiteDir = true
			}
		}
		if !inASuiteDir {
			misplaced = append(misplaced, p)
		}
	}

	require.NotEmptyf(t, found, "the name rule matched no tracked file at all, so this "+
		"sweep is reporting a clean tree because it recognises nothing as a suite")
	require.Emptyf(t, misplaced,
		"these tracked files are named like test suites and do not live in any of %v, "+
			"which are the only directories %q globs — so nothing a developer or a "+
			"required context runs reaches them (bd gqlc-snzq F4):\n  %s",
		suiteDirs, hookTestsRecipe, strings.Join(misplaced, "\n  "))
}
