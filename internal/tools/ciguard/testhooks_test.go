// The chain that makes .githooks/tests/*.sh block a merge: the ci.yml `test`
// job runs `just test`, `just test` depends on `test-hooks`, and `test-hooks`
// names each suite. `test` is a required status check on master.
//
// What is held here is that each link is named, and that the yaml link carries
// neither an `if:` nor a `continue-on-error:` — neither on the `test` job nor
// on the step that runs `just test`. Both keys leave every line of the workflow
// in place, and neither reads the same on a job as on a step, because a job
// emits a check run of its own and a step does not: an `if:` on the job leaves
// a check run whose conclusion is `skipped`, which branch protection reads as a
// pass, while an `if:` on the step leaves the job's check run concluding
// success with the suites never run. The messages below carry the rest, one per
// key per level.
//
// Those are the keys refused here, not the set of keys that can retire a check
// without deleting a line. A `shell:` on a step, and a `defaults.run.shell` on
// the job or on the workflow, reach that end by a different route; those are
// refused around the PR-body gate in workflow_test.go, where the step runs two
// commands and leans on the runner's `-e` to stop at the first that fails.
//
// What is not held here is that a suite line carries its exit status into the
// recipe's status. That end is hooktests_test.go's, in two tests:
// TestEveryHookTestSuiteIsNamedByTestHooks refuses a suite line that is not a
// bare `bash <suite>`, and TestTestHooksStopsAtTheFirstFailingSuite writes the
// recipe into a throwaway justfile, stubs the suites it names and runs the
// real `just`. Measured on the recipe's first suite line: `|| true`, `|| :`,
// `| tee`, a trailing `&` and just's `-` prefix redden both of them; `@bash`
// and `>/dev/null` redden the shape rule alone, and neither of those two
// discards a status; rewriting `test-hooks` as a `#!/usr/bin/env bash` recipe
// reddens the behavioural one alone, the shape rule being blind to a token its
// comment strip removes. That is the set that was measured, not the set that
// exists — `just` accepts spellings no one has tried here.
//
// The justfile reads below are comment-stripped, so a link that is spelled and
// commented out reads as absent rather than as present. `test: check-hooks
// # test-hooks` is a `test` recipe that does not depend on `test-hooks`, and an
// indented `# bash .githooks/tests/foo-test.sh` is a suite the recipe does not
// run; matched against raw bytes, as this file did, both read as the link being
// there (bd gqlc-sgot). Neither passed the package, though, and not through one
// test: the commented-out dependency reddened TestTestHooksIsReachedByARecipeCIRuns,
// the commented-out suite line reddened TestEveryHookTestSuiteIsNamedByTestHooks,
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
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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

// justRecipe returns a recipe's dependency list and the lines of its body,
// comments stripped from both, so that what is returned is what just runs
// rather than what the justfile spells.
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
func justRecipe(t *testing.T, name string) (deps string, body []string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, justfilePath))
	require.NoError(t, err, "read %s", justfilePath)

	header := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `[ \t]*:(.*)$`)
	lines := strings.Split(string(src), "\n")
	start := -1
	for i, ln := range lines {
		if m := header.FindStringSubmatch(liverecipes.StripComment(ln)); m != nil {
			start, deps = i, strings.TrimSpace(m[1])
			break
		}
	}
	require.GreaterOrEqualf(t, start, 0,
		"%s has no recipe %q, so the chain that reaches the shell suites from a "+
			"required CI context is broken at that link", justfilePath, name)

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
	return deps, body
}

// A suite file that no recipe names is a suite that never runs. It is the
// wiring rather than the suite that goes missing: the file stays in the tree,
// green, and its assertions stop being made.
func TestEveryHookSuiteRunsInTestHooks(t *testing.T) {
	_, body := justRecipe(t, hookSuiteRecipe)
	require.NotEmptyf(t, body, "the %q recipe in %s has an empty body",
		hookSuiteRecipe, justfilePath)

	suites, err := filepath.Glob(filepath.Join(repoRoot, hookSuiteDir, hookSuiteGlob))
	require.NoError(t, err, "glob %s/%s", hookSuiteDir, hookSuiteGlob)
	require.NotEmptyf(t, suites, "no %s files under %s, so this assertion is "+
		"holding nothing: either the suites moved or the glob is wrong",
		hookSuiteGlob, hookSuiteDir)

	ran := strings.Join(body, "\n")
	for _, s := range suites {
		rel := hookSuiteDir + "/" + filepath.Base(s)
		require.Containsf(t, ran, rel,
			"%s is in the tree but the %q recipe does not run it. Nothing else "+
				"does, so its rows are not asserted by any CI context. %q runs:\n%s",
			rel, hookSuiteRecipe, hookSuiteRecipe, ran)
	}
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

	require.Emptyf(t, job.If,
		"job %q carries a job-level `if:` (%q). A skipped job still emits a check run "+
			"with conclusion `skipped`, and branch protection reads that as a pass, so "+
			"an `if:` here retires the shell suites without deleting a line of them.",
		testJob, job.If)

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

	for _, s := range job.Steps {
		if runsTestRecipe.MatchString(s.Run) {
			require.Emptyf(t, s.If,
				"the `just %s` step in job %q carries an `if:` (%q). A skipped step "+
					"leaves the job green, so the context reports SUCCESS with the "+
					"suites never run — worse than a skipped job, which at least "+
					"reports `skipped`.", testRecipe, testJob, s.If)
			require.Falsef(t, present(s.ContinueOnError),
				"the `just %s` step in job %q sets `continue-on-error: %s`, so the "+
					"step's failure is not the job's. The suites run, the recipe "+
					"returns non-zero, and %q reports SUCCESS with the verdict "+
					"discarded.",
				testRecipe, testJob, spell(s.ContinueOnError), testJob)
			return
		}
	}
	t.Fatalf("no step in job %q of %s runs `just %s`, so the go tests and the "+
		"shell suites it depends on are not reached by a required context",
		testJob, ciWorkflow, testRecipe)
}
