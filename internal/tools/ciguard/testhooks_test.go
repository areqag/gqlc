// The chain that makes .githooks/tests/*.sh block a merge: the ci.yml `test`
// job runs `just test`, `just test` depends on `test-hooks`, and `test-hooks`
// names each suite. What is held here is that each link is named, not that
// each link carries a status: a suite the recipe still names but whose exit
// is discarded (`bash ...-test.sh || true`) passes every assertion below
// (bd gqlc-lisu). Measured (bd gqlc-1ekq): deleting any one of the five lines
// from `test-hooks` on master leaves `just test-hooks` at rc=0.
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

// justRecipe returns a recipe's dependency list and the lines of its body.
//
// just's own `--dump` would need just on PATH, which `go test ./...` does not
// promise; the justfile is in the tree, like the workflows this package's
// other assertions parse. A recipe is a header at column zero and every line
// under it that is indented or blank, up to the next line that is neither.
func justRecipe(t *testing.T, name string) (deps string, body []string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, justfilePath))
	require.NoError(t, err, "read %s", justfilePath)

	header := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `[ \t]*:(.*)$`)
	lines := strings.Split(string(src), "\n")
	start := -1
	for i, ln := range lines {
		if m := header.FindStringSubmatch(ln); m != nil {
			start, deps = i, strings.TrimSpace(m[1])
			break
		}
	}
	require.GreaterOrEqualf(t, start, 0,
		"%s has no recipe %q, so the chain that reaches the shell suites from a "+
			"required CI context is broken at that link", justfilePath, name)

	for _, ln := range lines[start+1:] {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") {
			break
		}
		body = append(body, strings.TrimSpace(ln))
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

	for _, s := range job.Steps {
		if runsTestRecipe.MatchString(s.Run) {
			return
		}
	}
	t.Fatalf("no step in job %q of %s runs `just %s`, so the go tests and the "+
		"shell suites it depends on are not reached by a required context",
		testJob, ciWorkflow, testRecipe)
}
