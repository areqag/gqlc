// Every gate `just lint` is made of is applied by ONE dependency edge in the
// justfile's `lint:` header, and deleting an edge is silent: the recipe still
// exists, `just lint` still exits 0, and the check table still says lint.
//
// That is the shape bd gqlc-eo46 named for a single statement inside a recipe,
// one level up. Two of the edges here are new tonight and neither had anything
// holding it:
//
//	lint-python                        bd gqlc-tqi4 — the ONLY linter that reads
//	                                   any Python in this tree, including the PR
//	                                   gate that decides what a PR may close
//	check-golangci-formatters-report   bd gqlc-sh4j — the only thing asserting
//	                                   that `golangci-lint run` still reddens on
//	                                   gofumpt and gci, which is the only
//	                                   server-side formatter enforcement there is
//
// Read off `just --dump`, not off the file's bytes, so a commented-out edge is
// not an edge — and so a header spelled across a line continuation is read the
// way just reads it rather than the way a regex would.
package ciguard_test

import (
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// The edges `lint` must carry, each with what is lost when it goes.
//
// Not an equality against the whole list: adding a gate to lint should not have
// to edit this test, and a list that has to be edited to add a gate is one
// somebody edits to remove a gate. What is refused is an edge going MISSING.
var lintDependencies = map[string]string{
	"ensure-golangci": "the pinned linter is no longer provisioned, so `just lint` runs " +
		"whatever golangci-lint happens to be in .bin/, or fails to find one",
	"lint-hooks": "shellcheck no longer reads .githooks, kingdom/bin or .github/scripts " +
		"(the recipe is named three times, with three directories)",
	"lint-python": "NOTHING lints any Python in this tree — not ruff, not flake8, not " +
		"pylint, not mypy, not py_compile. That was the state until bd gqlc-tqi4, and " +
		"what it left uncovered is .github/scripts/check-pr-closes.py, the gate deciding " +
		"what a pull request may claim to close, run from the required `tidy` context",
	"lint-just": "shellcheck no longer reads this justfile's own recipe bodies",
	"check-golangci-formatters-report": "nothing asserts that `golangci-lint run` still " +
		"reports the `formatters:` block as issues. `just fmt-check` is not a required " +
		"status context and no workflow calls it, so that report is the whole of gofumpt " +
		"and gci enforcement on the server (bd gqlc-sh4j)",
	"check-golangci-build-tags": ".golangci.yml may name build tags that constrain no " +
		"file, pre-accepting whatever is added under that spelling next (bd gqlc-oxne)",
}

// justDump is `just --dump --dump-format json` over this repository's justfile.
type justDump struct {
	Recipes map[string]struct {
		Dependencies []struct {
			Recipe string `json:"recipe"`
		} `json:"dependencies"`
	} `json:"recipes"`
}

func TestLintAppliesEveryGateItIsMadeOf(t *testing.T) {
	justBin, err := exec.LookPath("just")
	require.NoError(t, err, "`just` is not on PATH, and this reads the justfile through it")

	cmd := exec.CommandContext(t.Context(), justBin,
		"--justfile", justfilePath, "--dump", "--dump-format", "json")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	require.NoErrorf(t, err, "just --dump over %s", justfilePath)

	var dump justDump
	require.NoError(t, json.Unmarshal(out, &dump), "decode just --dump")

	lint, ok := dump.Recipes["lint"]
	require.Truef(t, ok, "%s has no `lint` recipe, but `just lint` is what the required "+
		"lint context runs", justfilePath)

	present := map[string]bool{}
	for _, d := range lint.Dependencies {
		present[d.Recipe] = true
	}
	require.NotEmpty(t, present, "`lint` has no dependencies at all, so every gate it is "+
		"supposed to apply has gone at once")

	for dep, cost := range lintDependencies {
		require.Truef(t, present[dep],
			"`lint` no longer depends on %q. Deleting that edge is silent — the recipe is "+
				"still in the file, `just lint` still exits 0, and the required lint "+
				"context still reports green. What is lost: %s.", dep, cost)
	}
}
