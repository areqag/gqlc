package ciguard_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/liverecipes"
)

// The recipe that runs the hooks' own test suites, and the directory they live
// in. `just test` depends on test-hooks, and `test` is a required context, so a
// suite named there runs on every PR — and one not named there runs nowhere.
const (
	hookTestsRecipe = "test-hooks"
	hookTestsGlob   = ".githooks/tests/*-test.sh"
)

// Every hook test suite in the tree has to be named by the recipe that runs
// them.
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
func TestEveryHookTestSuiteIsWiredIntoTestHooks(t *testing.T) {
	src := readRepoFile(t, justfile)
	body, ok := liverecipes.RecipeBody(src, hookTestsRecipe)
	require.Truef(t, ok, "the %s declares no `%s:` recipe, so nothing runs the hooks' "+
		"own tests", justfile, hookTestsRecipe)

	suites, err := filepath.Glob(filepath.Join(repoRoot, hookTestsGlob))
	require.NoError(t, err, "glob %s", hookTestsGlob)
	require.NotEmptyf(t, suites, "no file matches %s, so this test is comparing the "+
		"recipe against an empty set and would pass over a recipe that runs nothing",
		hookTestsGlob)

	for _, abs := range suites {
		rel := filepath.ToSlash(filepath.Join(".githooks", "tests", filepath.Base(abs)))
		require.Containsf(t, body, rel,
			"%s exists but %q does not run it. An unwired suite is an absent gate: it "+
				"is still linted and still listed, and not one of its cases executes.",
			rel, hookTestsRecipe)
	}

	// ...and the recipe must not name a suite that is gone, which would fail the
	// whole of `just test` for a reason unrelated to any change.
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "bash" {
			continue
		}
		require.FileExistsf(t, filepath.Join(repoRoot, fields[1]),
			"%s runs %q, which is not in the tree", hookTestsRecipe, fields[1])
	}
}
