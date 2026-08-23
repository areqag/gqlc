// The chain that makes .githooks/tests/*.sh block a merge: the ci.yml `test`
// job runs `just test`, `just test` depends on `test-hooks`, and `test-hooks`
// globs the suite directory. `test` is a required status check on master.
//
// What is held here is that each link is named, and that the yaml link carries
// no `if:` condition and no `continue-on-error:` — neither on the `test` job
// nor on the step that runs `just test`. An `if:` written so that it decodes to
// the empty string carries no condition and passes here; the empty-decoding
// spellings tried against actionlint, a required context in its own right, were
// each refused by it.
//
// Both keys leave every line of the workflow in place, and neither reads the
// same on a job as on a step, because a job emits a check run of its own and a
// step does not: an `if:` on the job leaves a check run whose conclusion is
// `skipped`, which branch protection reads as a pass, while an `if:` on the
// step leaves the job's check run concluding success with the suites never run.
// The messages below carry the rest, one per key per level.
//
// Those are the keys refused here, not the set of keys that can retire a check
// without deleting a line. A `shell:` on a step, and a `defaults.run.shell` on
// the job or on the workflow, reach that end by a different route; those are
// refused around the PR-body gate in workflow_test.go, where the step runs two
// commands and leans on the runner's `-e` to stop at the first that fails.
//
// What is not held here is that a suite carries its exit status into the
// recipe's status. That end is hooktests_test.go's, behaviourally:
// TestTestHooksStopsAtTheFirstFailingSuite writes the recipe into a throwaway
// tree, plants stub suites under names the justfile has never heard of, fails
// the first one and asserts that nothing after it ran; and
// TestTestHooksFailsWhenItDiscoversNoSuite drives the case discovery added,
// where the glob matches nothing and a loop over nothing exits 0.
//
// The shape rule that used to sit beside them — every suite line a bare
// `bash <suite>` — is gone with the enumeration it read. There is one suite
// invocation now, inside a loop, and the spellings that rule caught (`|| true`,
// `| tee`, a trailing `&`, just's `-` prefix) are caught by the behavioural
// rows instead, which is where they were already caught: measured on the old
// recipe's first suite line, each of those reddened both. What the shape rule
// held alone was `@bash` and `>/dev/null`, neither of which discards a status.
// In its place hooktests_test.go refuses a body line that names an individual
// suite path, which is the append point returning.
//
// The justfile reads below are comment-stripped, so a link that is spelled and
// commented out reads as absent rather than as present. `test: check-hooks
// # test-hooks` is a `test` recipe that does not depend on `test-hooks`, and an
// indented `# .githooks/tests/*-test.sh` is a discovery the recipe does not
// perform; matched against raw bytes, as this file did, both read as the link
// being there (bd gqlc-sgot). Neither passed the package, though, and not through
// one test: the commented-out dependency reddened TestTestHooksIsReachedByARecipeCIRuns,
// the commented-out glob reddens TestEveryHookTestSuiteIsDiscoveredByTestHooks,
// and neither of those two reddened on the other's input. Both read their
// justfile through a comment strip already. So what was wrong here was narrower
// than a hole in CI and is still worth fixing: a pin that cannot fail on the
// link it names reports on nothing, and neither justfile pin below could.
//
// Asserted here rather than inside the suites because a suite that has been
// unwired does not run, so it cannot be the thing that notices.
package ciguard_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/areqag/gqlc/internal/liverecipes"
)

const justfilePath = "justfile"

const (
	// Where the shell suites live, and what a suite file is called.
	hookSuiteDir  = ".githooks/tests"
	hookSuiteGlob = "*-test.sh"
	// The recipe that runs them, the recipe that depends on it, and the
	// ci.yml job that invokes that one. `test` is a required status check
	// on master.
	hookSuiteRecipe = "test-hooks"
	testRecipe      = "test"
	testJob         = "test"
)

// Matches the ci.yml step that runs the whole suite.
var runsTestRecipe = regexp.MustCompile(`(?m)^[ \t]*just ` + testRecipe + `[ \t]*$`)

// recipeHeader is the pattern for one named recipe's header line: the name at
// column zero, optionally prefixed with just's `@` quiet marker and optionally
// followed by a parameter list, then the `:` that opens the dependency list.
//
// The parameter group and the `@` are there because their absence made an
// unmodelled SPELLING of an existing recipe read as an ABSENT recipe (bd
// gqlc-75xi). `^name[ \t]*:` — what this was — matches `test:` and not
// `test flag="":` or `@test:`, both of which just accepts and runs; the caller
// then fails with "justfile has no recipe test", which sends a reader looking
// for a deleted link that is in front of them. A gate that reddens on a
// spelling it does not model trains its readers to route around it, and that is
// worse than the missing coverage, because the two other readers of this same
// file model parameters already: rawRecipe in hooktests_test.go and recipeDeps
// in golangci_cache_test.go. This one was the outlier.
//
// The dependency group refuses a leading `=` so that an ASSIGNMENT named for a
// recipe — `test := "x"` — is not read as a recipe whose dependency list is
// `= "x"`. recipeDeps carries the same clause for the same reason; without it
// the two readers disagree about what a header is.
//
// Limits, stated rather than left to be met: a parameter default containing a
// `:` (`p=":"`) is not matched, because the parameter group refuses a colon so
// that the FIRST colon on the line is the one that opens the dependency list;
// and just's `[attribute]` lines sit above the header, so they are neither
// matched nor needed. No recipe in this justfile spells either today.
func recipeHeader(name string) *regexp.Regexp {
	return regexp.MustCompile(`^@?` + regexp.QuoteMeta(name) + `([ \t][^:\n]*)?[ \t]*:([^=\n].*|)$`)
}

// parseRecipe returns a recipe's dependency list and the lines of its body,
// comments stripped from both, so that what is returned is what just runs
// rather than what the justfile spells. ok is false when src declares no such
// recipe.
//
// A recipe is a header at column zero and every line under it that is indented
// or blank, up to the next line that is neither. Those boundaries are decided
// on the raw line and the strip is applied only to what is kept: indentation is
// a property of the text, so stripping first would let a comment at column zero
// stop ending a recipe. A body line that is nothing but a comment runs nothing
// and is dropped rather than returned empty.
//
// liverecipes.StripComment rather than a cut at the first `#`. It cuts at a `#`
// that begins a word outside quotes, so a `#` inside a quoted argument
// survives, and that is the whole of what is claimed for it here: its own doc
// comment disclaims equivalence with sh, naming backslash escapes, command
// substitution, heredocs and here-strings as shapes it does not model and each
// as cutting where sh would not.
//
// The quoted half is exercised rather than hypothetical — the header search
// runs the strip over every line above the recipe it is looking for, and lines
// in that stretch do come out differently under the two rules, `"${#names[@]}"`
// and `"#!"*` shapes that the naive cut truncates mid-line. Measured over the
// justfile as it stands, neither of the two recipes read here comes out with a
// different dependency list or body for it, so what the choice buys at these
// call sites today is nothing; it is made because gqlc-snzq F6 records the
// naive cut truncating a neighbouring surface, and because a recipe line that
// grows a quoted `#` should not have to notice.
//
// Not `just --dump`, though the justfile is what just parses and this is a
// regex. The behavioural question — whether a failing suite stops the recipe —
// is asked by running the real `just` in hooktests_test.go, which requires it
// on PATH rather than skipping past its absence; these three are text pins and
// buy no accuracy for a subprocess. `just --dump` also prints `error:` and
// exits non-zero on a parse failure, so a justfile edit that breaks parsing
// would redden them for a reason unrelated to the wiring, which is the fake
// RED this repository screens mutations for.
func parseRecipe(src, name string) (deps string, body []string, ok bool) {
	header := recipeHeader(name)
	lines := strings.Split(src, "\n")
	start := -1
	for i, ln := range lines {
		if m := header.FindStringSubmatch(liverecipes.StripComment(ln)); m != nil {
			start, deps = i, strings.TrimSpace(m[2])
			break
		}
	}
	if start < 0 {
		return "", nil, false
	}

	for _, raw := range lines[start+1:] {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if !strings.HasPrefix(raw, " ") && !strings.HasPrefix(raw, "\t") {
			break
		}
		if ln := strings.TrimSpace(liverecipes.StripComment(raw)); ln != "" {
			body = append(body, ln)
		}
	}
	return deps, body, true
}

// justRecipe is parseRecipe over the repository's own justfile.
func justRecipe(t *testing.T, name string) (deps string, body []string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, justfilePath))
	require.NoError(t, err, "read %s", justfilePath)

	deps, body, ok := parseRecipe(string(src), name)
	require.Truef(t, ok,
		"%s has no recipe %q, so the chain that reaches the shell suites from a "+
			"required CI context is broken at that link", justfilePath, name)
	return deps, body
}

// justDeclaresRecipe asks the real `just` whether src declares a recipe called
// name. `just --show` answers for a [private] recipe where `--summary` does
// not, and it distinguishes an assignment from a recipe of the same name.
//
// This is the premise oracle for the table below, and it is here because the
// defect being fixed is a MODEL that disagreed with just. A table of spellings
// I believe just accepts, checked only against my own reader, would ratify the
// same mistake in the other direction: a row asserting parseRecipe finds
// `@test:` is worth nothing unless just agrees that `@test:` declares `test`.
func justDeclaresRecipe(t *testing.T, src, name string) bool {
	t.Helper()
	justBin, err := exec.LookPath("just")
	require.NoError(t, err, "`just` is not on PATH, and the rows below are premised on "+
		"what just accepts rather than on what this reader believes")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "justfile"), []byte(src), 0o600))
	cmd := exec.CommandContext(t.Context(), justBin, "--show", name)
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return true
	}
	var ee *exec.ExitError
	require.ErrorAsf(t, runErr, &ee, "could not run `just --show %s` over:\n%s\n%s",
		name, src, out)
	// A parse error is neither answer, and taking it as "no recipe" would let a
	// row whose fixture does not parse stand in for a spelling just refuses.
	require.NotContainsf(t, string(out), "error: Expected",
		"the fixture for %q does not parse, so this row measures a syntax error "+
			"rather than a spelling:\n%s\n%s", name, src, out)
	return false
}

// parseRecipe has to find a recipe under every spelling just accepts for it,
// and under none that just does not (bd gqlc-75xi).
//
// The failure this closes is a FAKE RED, which is the mirror image of the usual
// one here: not a gate that wrongly passes, but a gate that wrongly fails. The
// old header pattern `^name[ \t]*:` refused `test flag="":` and `@test:`, so
// adding a parameter to the `test` recipe — a change that keeps every link in
// the CI chain intact — made TestTestHooksIsADependencyOfTest report that the
// justfile has no `test` recipe at all.
//
// `want` is what parseRecipe must say, and `justSays` is asserted to equal it
// first: every row's premise is checked against the real `just` before the
// reader is asked, so a row cannot pass by the two being wrong together.
func TestParseRecipeAgreesWithJustAboutWhatDeclaresARecipe(t *testing.T) {
	const body = "    echo ran\n"
	// Declared so that a fixture naming a dependency still loads: `just --show`
	// resolves the whole file, and an undeclared dependency is a load error
	// rather than an answer about the header.
	const dep = "dep-a:\n" + body + "dep-b:\n" + body
	for _, row := range []struct {
		name  string
		src   string
		found bool
		deps  string
	}{
		{"bare header", "test:\n" + body, true, ""},
		{"header with dependencies", dep + "test: dep-a dep-b\n" + body, true, "dep-a dep-b"},
		{"quiet recipe", "@test:\n" + body, true, ""},
		{"one parameter", "test flag:\n" + body, true, ""},
		{"parameter with a default", `test flag="":` + "\n" + body, true, ""},
		{"parameters and dependencies", dep + `test flag="": dep-a` + "\n" + body, true, "dep-a"},
		{"attribute above the header", "[private]\ntest:\n" + body, true, ""},
		// The assignment is the one row in the other direction, and it is the
		// clause a looser pattern loses: `^test[ \t]*:(.*)$` matches
		// `test := "x"` and reads `= "x"` as the dependency list, so
		// TestTestHooksIsADependencyOfTest would then be asking whether a
		// variable depends on test-hooks.
		{"an assignment of the same name", `test := "x"` + "\nother:\n" + body, false, ""},
		{"a longer recipe name", "test-hooks:\n" + body, false, ""},
		{"an indented header", "other:\n    test:\n", false, ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			require.Equalf(t, row.found, justDeclaresRecipe(t, row.src, testRecipe),
				"this row's premise is that just %s a recipe %q in:\n%s",
				map[bool]string{true: "declares", false: "does not declare"}[row.found],
				testRecipe, row.src)

			deps, _, ok := parseRecipe(row.src, testRecipe)
			require.Equalf(t, row.found, ok,
				"just and parseRecipe disagree about whether %q declares a recipe %q. "+
					"Reading a recipe just runs as absent is a fake RED that reports a "+
					"broken CI chain over a spelling; reading one just does not run as "+
					"present is a link asserted against nothing.",
				row.src, testRecipe)
			require.Equalf(t, row.deps, deps, "dependency list read from %q", row.src)
		})
	}
}

// A suite file no recipe reaches is a suite that never runs. It is the wiring
// rather than the suite that goes missing: the file stays in the tree, green,
// and its assertions stop being made.
//
// The recipe discovers its suites by globbing the directory rather than listing
// them (bd gqlc-234l), so the wiring is one pattern and the question here is
// whether that pattern is spelled in the body and reaches everything the
// directory holds. `hookSuiteGlob` is this file's own copy of the convention
// and it is used both to enumerate the tree and to build the pattern, so the
// two cannot disagree here — what holds the pattern against the rule
// hookSuites enforces is
// TestTestHooksGlobsExactlyWhatThisPackageAcceptsAsASuite in hooktests_test.go.
//
// Kept beside hooktests_test.go's near-twin rather than folded into it: this
// one reads the justfile through justRecipe, a different parser with different
// recipe boundaries, and this file's subject is the whole chain from the
// required check to the shell suites. A hole in either parser leaves the other
// speaking.
func TestEveryHookSuiteRunsInTestHooks(t *testing.T) {
	_, body := justRecipe(t, hookSuiteRecipe)
	require.NotEmptyf(t, body, "the %q recipe in %s has an empty body",
		hookSuiteRecipe, justfilePath)

	pattern := hookSuiteDir + "/" + hookSuiteGlob
	ran := strings.Join(body, "\n")
	// Excluding the recipe's own `echo`, which prints the pattern in its
	// emptiness refusal: a Contains over the whole body is satisfied by that
	// message with the working glob repointed at a pattern matching nothing.
	// Measured on `*-tests.sh` — this test passed. expandingLines is where that
	// is argued.
	require.NotEmptyf(t, expandingLines(ran, pattern),
		"the %q recipe in %s does not glob %q anywhere it would expand, and it names "+
			"no suite individually either — so it reaches no suite at all. An absent "+
			"gate is not a failing one. %q runs:\n%s",
		hookSuiteRecipe, justfilePath, pattern, hookSuiteRecipe, ran)

	suites, err := filepath.Glob(filepath.Join(repoRoot, hookSuiteDir, hookSuiteGlob))
	require.NoError(t, err, "glob %s/%s", hookSuiteDir, hookSuiteGlob)
	require.NotEmptyf(t, suites, "no %s files under %s, so this assertion is "+
		"holding nothing: either the suites moved or the glob is wrong",
		hookSuiteGlob, hookSuiteDir)
}

// ...and the recipe that runs them has to be reached by the one CI invokes.
func TestTestHooksIsADependencyOfTest(t *testing.T) {
	deps, _ := justRecipe(t, testRecipe)
	require.Containsf(t, strings.Fields(deps), hookSuiteRecipe,
		"the %q recipe in %s depends on %v, which does not include %q. %q is a "+
			"required status check on master and is the only path from CI to the "+
			"shell suites.",
		testRecipe, justfilePath, strings.Fields(deps), hookSuiteRecipe, testJob)
}

// ...and the CI job named by that required context has to invoke it.
func TestCITestJobRunsTheTestRecipe(t *testing.T) {
	jobs := childByKey(ciDoc(t), "jobs")
	require.NotNilf(t, jobs, "%s has no jobs", ciWorkflow)

	node := childByKey(jobs, testJob)
	require.NotNilf(t, node, "%s has no %q job, which is a required status check "+
		"on master", ciWorkflow, testJob)

	var job ciJob
	require.NoError(t, node.Decode(&job), "decode job %q", testJob)

	requireNoTestJobIf(t, job)

	// The other half of "the job can still fail", and it is not the step-level
	// key spelled one level up. GitHub documents the job-level form against the
	// workflow run — "prevents a workflow run from failing when a job fails" —
	// and it is the step-level form that carries a job past a failing step: the
	// runner applies continue-on-error as each step completes, turning that
	// step's failure into a success that the following steps' default
	// `success()` condition still sees. So a `continue-on-error:` here buys a
	// run recorded as a pass over a `test` job that failed, not a `test` job
	// that runs on past `just test` returning non-zero. What it does to this
	// job's own check run, which is what branch protection reads, is not
	// settled by that documentation and is not measured here — which is reason
	// to refuse the key on a required job, not reason to allow it. Deleting a
	// link is what the assertions here were built to catch; this is the
	// neutralisation they were not (bd gqlc-lisu), and it was measured green
	// against every one of them.
	//
	// Refused written rather than refused true, as present's own comment
	// argues: `continue-on-error: ${{ … }}` is a value no test can evaluate, so
	// "is the key there" is answerable where "is it truthy" is not. The cost is
	// that an explicit `continue-on-error: false` is refused too.
	require.Falsef(t, present(job.ContinueOnError),
		"job %q sets `continue-on-error: %s`. GitHub documents that key on a job as "+
			"preventing a workflow run from failing when the job fails, so `just %s` "+
			"can return non-zero with the run recorded as a pass. Whether the check run "+
			"for %q follows the run or the job is not settled here, and a required "+
			"context is not where to find out.",
		testJob, spell(job.ContinueOnError), testRecipe, testJob)

	s, ok := testRecipeStep(job)
	require.Truef(t, ok, "no step in job %q of %s runs `just %s`, so the go tests and "+
		"the shell suites it depends on are not reached by a required context",
		testJob, ciWorkflow, testRecipe)

	requireNoTestStepIf(t, s)
	require.Falsef(t, present(s.ContinueOnError),
		"the `just %s` step in job %q sets `continue-on-error: %s`, so the "+
			"step's failure is not the job's. The suites run, the recipe "+
			"returns non-zero, and %q reports SUCCESS with the verdict "+
			"discarded.",
		testRecipe, testJob, spell(s.ContinueOnError), testJob)
}

// testRecipeStep is the step in job that runs `just test`, and ok is false when
// no step does.
func testRecipeStep(job ciJob) (step ciStep, ok bool) {
	for _, s := range job.Steps {
		if runsTestRecipe.MatchString(s.Run) {
			return s, true
		}
	}
	return ciStep{}, false
}

// requireNoTestJobIf refuses a job-level `if:` on the `test` job, and
// requireNoTestStepIf refuses one on the step that runs `just test`.
//
// Read for presence rather than for value, as ContinueOnError above is: If is a
// yaml.Node (bd gqlc-ff66), so Kind is what parts an `if:` written with no value
// from an absent one. Both sites read a string field with require.Empty until
// that change, which passed the written-empty form; over a Node require.Empty
// asks a different question — equality with the zero struct — and renders it
// through %q, which has no verb for the *Node field inside it.
//
// Both take require.TestingT so that they can be run against a job that carries
// an `if:`. ci.yml carries neither key, so calling them over the file exercises
// only the side that passes; TestTestJobIfIsRefusedOnBeingWritten drives the
// other side.
func requireNoTestJobIf(t require.TestingT, job ciJob) {
	require.Falsef(t, present(job.If),
		"job %q carries a job-level `if:`, written as %s. A skipped job still emits a "+
			"check run with conclusion `skipped`, and branch protection reads that as a "+
			"pass, so an `if:` here retires the shell suites without deleting a line of "+
			"them.",
		testJob, spell(job.If))
}

func requireNoTestStepIf(t require.TestingT, s ciStep) {
	require.Falsef(t, present(s.If),
		"the `just %s` step in job %q carries an `if:`, written as %s. A "+
			"skipped step leaves the job green, so the context reports "+
			"SUCCESS with the suites never run — worse than a skipped job, "+
			"which at least reports `skipped`.",
		testRecipe, testJob, spell(s.If))
}

// The two refusals above have to reach an `if:` written with no value, and that
// is the case a string field cannot answer. This is the gqlc-ff66 hole in a
// second required context: both sites read If with require.Empty over a string
// until the field was promoted to a yaml.Node, so `if:` on the `test` job or on
// its `just test` step retired the shell suites and passed here.
//
// `value` is what a string field would have seen. The rows that share a value
// and differ in `refused` are that conflation, and they are the rows that go
// green again if If goes back to a string.
//
// `stepKey` says which of the two refusals a row is about: the key is written on
// the job when it is false and on the `just test` step when it is true. Every
// source below carries the step that runs the recipe, so a step row reaches the
// same lookup TestCITestJobRunsTheTestRecipe does.
//
// The rows are documents taken, not a survey of YAML. There are forms that
// decode to an empty value which are not written below, and nothing here claims
// a verdict for them: what is asserted is the verdict beside each source.
func TestTestJobIfIsRefusedOnBeingWritten(t *testing.T) {
	const runsIt = "steps:\n  - run: just " + testRecipe + "\n"
	const cond = "github.event_name == 'push'"

	for _, row := range []struct {
		name    string
		src     string
		stepKey bool
		refused bool
		value   string
	}{
		{"job: no if: key at all", runsIt, false, false, ""},
		{"job: if: with nothing after it", "if:\n" + runsIt, false, true, ""},
		{"job: if: with an empty string", "if: \"\"\n" + runsIt, false, true, ""},
		{"job: if: carrying a condition", "if: " + cond + "\n" + runsIt, false, true, cond},
		{"step: no if: key at all", runsIt, true, false, ""},
		{"step: if: with nothing after it", runsIt + "    if:\n", true, true, ""},
		{"step: if: with an empty string", runsIt + "    if: \"\"\n", true, true, ""},
		{"step: if: carrying a condition", runsIt + "    if: " + cond + "\n", true, true, cond},
	} {
		t.Run(row.name, func(t *testing.T) {
			var job ciJob
			require.NoErrorf(t, yaml.Unmarshal([]byte(row.src), &job), "decode %q", row.src)

			s, ok := testRecipeStep(job)
			require.Truef(t, ok, "%q writes no `just %s` step, so this row reaches "+
				"neither refusal", row.src, testRecipe)

			read, assert := job.If, func(rt require.TestingT) { requireNoTestJobIf(rt, job) }
			if row.stepKey {
				read, assert = s.If, func(rt require.TestingT) { requireNoTestStepIf(rt, s) }
			}
			require.Equalf(t, row.value, read.Value,
				"this row's premise is that a string `If` would have read %q from %q",
				row.value, row.src)

			refusal := refusalOf(t, assert)
			if !row.refused {
				require.Emptyf(t, refusal, "%q writes no `if:` on the key under test and "+
					"was refused anyway", row.src)
				return
			}
			require.NotEmptyf(t, refusal,
				"%q writes an `if:` reaching job %q and it was accepted, so a required "+
					"context can be retired by a key this test reads as absent",
				row.src, testJob)

			// Which level, not which job: testJob and testRecipe are both "test",
			// so a Contains over the job name is satisfied by the `just test` in
			// the step refusal whatever it says. The two markers below appear in
			// one message each. A refusal that names the wrong level sends a
			// maintainer to a key that is not set.
			marker := "carries a job-level `if:`"
			if row.stepKey {
				marker = "step in job"
			}
			require.Containsf(t, refusal, marker,
				"the refusal does not say which key is set: %s", refusal)
		})
	}
}
