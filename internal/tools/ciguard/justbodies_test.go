// The justfile's own recipe bodies are shell, and something must read them.
//
// A shebang recipe body is a bash script under `set -euo pipefail`, several of
// them 60+ lines and carrying this repository's gate logic, and until
// `lint-just` landed no linter read a line of it. What is held here is the two
// halves that make it a gate rather than a report (bd gqlc-wprl): that `lint`
// reaches it, and that it FAILS on a finding — plus the two ways its coverage
// can quietly shrink to nothing, an empty selection and a body that stops being
// enrolled.
package ciguard_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// shebangRecipes asks just which recipes carry a shell shebang on their first
// body line. It is the same question lint-just asks, put to the same oracle:
// an expectation derived from a reader of my own would ratify whatever that
// reader gets wrong, in both places at once.
func shebangRecipes(t *testing.T, dir string) []string {
	t.Helper()
	justBin, err := exec.LookPath("just")
	require.NoError(t, err, "`just` is not on PATH, and lint-just's selection is just's own parse")

	cmd := exec.CommandContext(t.Context(), justBin, "--dump", "--dump-format", "json")
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoErrorf(t, err, "just --dump in %s", dir)

	var dump struct {
		Recipes map[string]struct {
			Body [][]any `json:"body"`
		} `json:"recipes"`
	}
	require.NoError(t, json.Unmarshal(out, &dump), "just's json dump changed shape")

	shebang := regexp.MustCompile(`^#!.*(bash|sh)$`)
	var names []string
	for name, r := range dump.Recipes {
		if len(r.Body) == 0 {
			continue
		}
		var first strings.Builder
		for _, frag := range r.Body[0] {
			if s, ok := frag.(string); ok {
				first.WriteString(s)
			} else {
				first.WriteString("INTERP")
			}
		}
		if shebang.MatchString(first.String()) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// runLintJust runs this repository's own lint-just over a throwaway justfile
// holding src. The recipe under test is therefore the one that ships, not a
// copy: a copy would go stale against the shipped one without anything saying
// so, which is the failure mode the whole package exists to refuse.
func runLintJust(t *testing.T, src string) (combined string, exitCode int) {
	t.Helper()
	justBin, err := exec.LookPath("just")
	require.NoError(t, err, "`just` is not on PATH")

	dir := t.TempDir()
	target := filepath.Join(dir, "justfile")
	require.NoError(t, os.WriteFile(target, []byte(src), 0o600))

	cmd := exec.CommandContext(t.Context(), justBin, "lint-just", target)
	cmd.Dir = repoRoot
	out, runErr := cmd.CombinedOutput()
	combined = string(out)

	// A justfile that stopped parsing also prints `error:` and exits non-zero.
	// Naming that here keeps a row from reading as the refusal it is about.
	require.NotContainsf(t, combined, "error: Expected",
		"the justfile copy did not parse, so this row measures a syntax error rather "+
			"than lint-just's verdict:\n%s", combined)

	if runErr == nil {
		return combined, 0
	}
	var ee *exec.ExitError
	require.ErrorAsf(t, runErr, &ee, "could not run `just lint-just`:\n%s", combined)
	return combined, ee.ExitCode()
}

func repoJustfile(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, justfilePath))
	require.NoError(t, err, "read %s", justfilePath)
	return string(src)
}

// The link that makes this a gate a developer meets before CI does.
func TestLintReachesTheJustfileBodyLinter(t *testing.T) {
	t.Parallel()

	deps, _ := justRecipe(t, "lint")
	require.Containsf(t, strings.Fields(deps), "lint-just",
		"`lint` does not depend on lint-just, so the justfile's own shell is linted "+
			"by nothing a developer or CI runs; its dependencies are %q (bd gqlc-wprl)", deps)
}

// A detector that exits 0 is not a gate, so the finding case is run for real.
func TestLintJustFailsOnAFindingInARecipeBody(t *testing.T) {
	t.Parallel()

	src := repoJustfile(t)

	// Planted inside lint-hooks' body, on the line that prints the script list:
	// a set-but-unused variable is SC2034, a warning, which is the band this
	// gate enforces. Control first — the unmutated tree must be clean, or the
	// mutant proves nothing.
	anchor := "    echo \"shellcheck {{shellcheck_version}} over ${#scripts[@]} shell script(s) under $dir:\""
	require.Containsf(t, src, anchor,
		"the anchor this row plants a finding at is gone from %s", justfilePath)

	clean, code := runLintJust(t, src)
	require.Zerof(t, code, "lint-just is not clean at HEAD, so a planted finding below "+
		"would be indistinguishable from the tree's own:\n%s", clean)

	planted := strings.Replace(src, anchor,
		"    unused_var_planted_by_a_ciguard_row=1\n"+anchor, 1)
	out, code := runLintJust(t, planted)
	require.NotZerof(t, code,
		"lint-just reported success over a recipe body carrying an SC2034, so it is a "+
			"report and not a gate:\n%s", out)
	require.Containsf(t, out, "SC2034",
		"lint-just failed over the planted body without naming the finding, so the "+
			"failure could be anything:\n%s", out)
}

// The other half: coverage that shrinks to nothing must be loud.
func TestLintJustRefusesAnEmptySelection(t *testing.T) {
	t.Parallel()

	// A justfile with recipes but no shebang body. Selecting nothing from it and
	// exiting 0 reads exactly like every body being clean.
	const noShebang = "alpha:\n    echo a\n\nbeta:\n    echo b\n"

	out, code := runLintJust(t, noShebang)
	require.NotZerof(t, code,
		"a justfile with no shebang recipe was linted clean rather than refused, so "+
			"a tree that lost every shebang would pass this gate silently:\n%s", out)
	require.Containsf(t, strings.ToLower(out), "ran shellcheck over nothing",
		"the refusal does not say the gate saw no bodies, so it is indistinguishable "+
			"from any other failure:\n%s", out)
}

// Coverage is asserted body by body rather than by a count, because a count
// holds only the size of the set and this is a claim about its membership: a
// recipe that loses its enrolment while another gains one leaves the total
// unmoved.
func TestLintJustEnrolsEveryShebangRecipeBody(t *testing.T) {
	t.Parallel()

	want := shebangRecipes(t, repoRoot)
	require.NotEmpty(t, want, "this tree declares no shebang recipe at all, so the "+
		"comparison below would hold over an empty set")

	out, code := runLintJust(t, repoJustfile(t))
	require.Zerof(t, code, "lint-just is not clean at HEAD:\n%s", out)

	var got []string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "  ") && !strings.HasPrefix(strings.TrimSpace(ln), "shellcheck") {
			if name := strings.TrimSpace(ln); name != "" {
				got = append(got, name)
			}
		}
	}
	sort.Strings(got)
	require.Equalf(t, want, got,
		"lint-just's printed enrolment is not the set of shebang recipes just reports, "+
			"so a body is under the gate in one account and outside it in the other:\n%s", out)
}
