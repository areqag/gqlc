// `check-golangci-build-tags` carries a sixty-line witness proving that
// `refuse_stale`'s BODY refuses a stale entry, and nothing at all proving that
// body is APPLIED to the real sets. The application is one line, the last
// statement of the recipe:
//
//	refuse_stale "${derived}" "${configured}" || exit 1
//
// Delete it and a genuinely stale `.golangci.yml` run.build-tags entry passes in
// silence. Measured first-party, and re-measured on this branch against the
// fixture below:
//
//	A  pristine recipe + a stale entry   rc=1, names the term
//	B  that line deleted + same entry    rc=0, prints nothing
//	C  pristine recipe + clean sets      rc=0, prints nothing
//
// B is byte-identical in observable behaviour to C. The gate reports clean over
// a tree it never checked.
//
// Why the recipe cannot close this itself, and why this file is Go: any counter
// or assertion added around that line is itself deletable, and the regress does
// not terminate inside the script. The close has to be out of band. This is bd
// gqlc-eo46.
//
// The rows run the REAL recipe out of the REAL justfile, in a throwaway tree —
// the same move golangci_staleness_test.go makes for `ensure-golangci`, and for
// the same reason: what is asserted is what a citizen SEES, and a text pin over
// the recipe body would be satisfied by a line that never executes.
package ciguard_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The last statement of check-golangci-build-tags: the one line that applies
// the refusal to the sets the tree really has. Written out here because
// deleting it is the mutation this file exists to kill, and a mutation nothing
// can find is one nothing tests.
const staleApplication = `    refuse_stale "${derived}" "${configured}" || exit 1`

// The stale term the rows inject. Distinct from the recipe's own `zzstaleprobe`
// so that a row and the recipe's internal witness cannot be confused for one
// another in the output.
const injectedStaleTag = "zzstalereal"

// buildTagFixture is a throwaway tree the recipe can run in.
//
// What it needs, and no more: the justfile itself, a .gitignore and a git
// repository (sweep-discovery-probes, which this recipe depends on, asks
// `git check-ignore` where a rule came from), test/data for the probe glob, and
// a `go` on PATH that answers the three `modscope` queries the recipe makes.
//
// Stubbing `go` rather than copying the module is what keeps this cheap and
// hermetic. The recipe reaches modscope through exactly one helper —
// `scope() { go run ./internal/tools/modscope "$@"; }` — so the two tag sets
// are inputs to the recipe, and a fixture that supplies them can put the recipe
// in states no real tree can reach. A stale entry is one of those states: on
// the real tree, derived is a subset of configured by construction, so nothing
// in this repository can witness the refusal firing.
type buildTagFixture struct {
	dir string
}

// newBuildTagFixture builds the tree and returns it.
func newBuildTagFixture(t *testing.T) *buildTagFixture {
	t.Helper()
	require.NotEmpty(t, lookPathOrSkip(t, "just"))
	gitBin := lookPathOrSkip(t, "git")

	src, err := os.ReadFile(filepath.Join(repoRoot, justfilePath))
	require.NoError(t, err, "read %s", justfilePath)
	require.Containsf(t, string(src), staleApplication,
		"%s no longer carries the line\n  %s\nwhich is the only place the stale-tag "+
			"refusal is applied to the real tag sets. Either it moved — in which case "+
			"this constant has to move with it — or it is gone, and .golangci.yml can "+
			"name build tags that constrain no file with nothing objecting (bd gqlc-eo46).",
		justfilePath, staleApplication)

	ignore, err := os.ReadFile(filepath.Join(repoRoot, ".gitignore"))
	require.NoError(t, err, "read .gitignore")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "justfile"), src, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), ignore, 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "test", "data"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "stubbin"), 0o750))

	// `go run ./internal/tools/modscope <query>` and nothing else. Anything the
	// recipe asks that is not one of the three queries is an error rather than
	// an empty answer: a stub that shrugs turns a recipe change into a row that
	// still passes.
	stub := `#!/usr/bin/env bash
if [ "$1" != "run" ]; then
    echo "stub go: this fixture only answers 'go run': $*" >&2
    exit 2
fi
shift
shift
case "$1" in
    modules)  printf '.\n' ;;
    tags)     printf '%s\n' "${FIXTURE_DERIVED}" ;;
    declared) printf '%s\n' "${FIXTURE_CONFIGURED}" ;;
    *) echo "stub go: unexpected modscope query: $*" >&2; exit 2 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stubbin", "go"), []byte(stub), 0o700))

	// A repository, because sweep-discovery-probes reads `git check-ignore -v`
	// and requires the matching rule to come from this tree's own .gitignore.
	//
	// GIT_DIR and GIT_WORK_TREE are cleared for the run below rather than
	// trusted to be absent. A `git init <dir>` inherited into a linked
	// worktree's environment re-initialises the PARENT repository, which is a
	// defect this repository has already paid for once; and the git hook that
	// runs `just test` on push exports both.
	initCmd := exec.CommandContext(t.Context(), gitBin, "init", "-q", ".")
	initCmd.Dir = dir
	initCmd.Env = fixtureEnv(dir, "", "")
	out, err := initCmd.CombinedOutput()
	require.NoErrorf(t, err, "git init in the fixture tree: %s", out)

	return &buildTagFixture{dir: dir}
}

// lookPathOrSkip fails rather than skips. A guard over a merge gate that
// quietly does not run on the machine where it matters is the shape this whole
// package refuses.
func lookPathOrSkip(t *testing.T, bin string) string {
	t.Helper()
	p, err := exec.LookPath(bin)
	require.NoErrorf(t, err, "%q is not on PATH, and these rows run the real recipe", bin)
	return p
}

// fixtureEnv is the environment every command in the fixture runs under: the
// caller's, with the two tag sets set and every inherited git redirection
// cleared.
func fixtureEnv(dir, derived, configured string) []string {
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true,
		"FIXTURE_DERIVED": true, "FIXTURE_CONFIGURED": true, "PATH": true,
	}
	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			env = append(env, kv)
		}
	}
	return append(env,
		"PATH="+filepath.Join(dir, "stubbin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FIXTURE_DERIVED="+derived,
		"FIXTURE_CONFIGURED="+configured,
	)
}

// run invokes the real recipe in the fixture with the given tag sets.
func (f *buildTagFixture) run(t *testing.T, derived, configured string) (combined string, exitCode int) {
	t.Helper()
	justBin := lookPathOrSkip(t, "just")

	cmd := exec.CommandContext(t.Context(), justBin, "check-golangci-build-tags")
	cmd.Dir = f.dir
	cmd.Env = fixtureEnv(f.dir, derived, configured)
	out, err := cmd.CombinedOutput()

	var ee *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &ee):
		return string(out), ee.ExitCode()
	default:
		t.Fatalf("running the recipe in the fixture: %v\n%s", err, out)
		return "", -1
	}
}

// deleteStaleApplication rewrites the fixture's justfile with the application
// line removed, and returns how many copies it removed.
func (f *buildTagFixture) deleteStaleApplication(t *testing.T) int {
	t.Helper()
	path := filepath.Join(f.dir, "justfile")
	src, err := os.ReadFile(path)
	require.NoError(t, err, "read the fixture justfile")

	n := strings.Count(string(src), staleApplication+"\n")
	out := strings.ReplaceAll(string(src), staleApplication+"\n", "")
	require.NoError(t, os.WriteFile(path, []byte(out), 0o600))
	return n
}

// The three rows, in one test so that the control and the refusal are measured
// against the same fixture rather than against two trees that might differ.
func TestStaleBuildTagRefusalIsAppliedAndNotJustDefined(t *testing.T) {
	f := newBuildTagFixture(t)

	// C — the sets agree, which is the state this repository is always in.
	out, rc := f.run(t, "mytag", "mytag")
	require.Equalf(t, 0, rc, "the recipe refused a fixture whose two tag sets are equal, "+
		"so the rows below would measure a recipe that refuses everything:\n%s", out)
	require.NotContainsf(t, out, injectedStaleTag,
		"the control run named %s, which no row put in it yet", injectedStaleTag)

	// A — a term configured and constraining nothing. Unreachable on the real
	// tree: derived is a subset of configured there by construction, so this is
	// the only place the refusal's live arm is ever exercised.
	out, rc = f.run(t, "mytag", "mytag\n"+injectedStaleTag)
	require.Equalf(t, 1, rc, "a build tag in the configured set and in no file was ACCEPTED. "+
		"That entry describes nothing and pre-accepts whatever is added under that "+
		"spelling next, and the recipe reported clean over it (bd gqlc-eo46):\n%s", out)
	require.Containsf(t, out, injectedStaleTag,
		"the recipe refused, but did not name %s in what it printed — so a real stale "+
			"entry reddens CI without telling anyone which one:\n%s", injectedStaleTag, out)

	// B — the mutation. This is what the row above is worth: without it, the
	// deletion below is invisible.
	removed := f.deleteStaleApplication(t)
	require.Equalf(t, 1, removed,
		"expected exactly one copy of the application line in the justfile, found %d", removed)

	out, rc = f.run(t, "mytag", "mytag\n"+injectedStaleTag)
	require.Equalf(t, 0, rc,
		"deleting\n  %s\nfrom the recipe no longer changes what it does with a stale tag — "+
			"it still exits %d. Either something else now applies the refusal, in which "+
			"case this row should name that instead and the constant above should point "+
			"at whichever line is now the single point of failure, or the fixture stopped "+
			"reaching the refusal at all and the row above is passing for the wrong "+
			"reason:\n%s", staleApplication, rc, out)
	require.NotContainsf(t, out, injectedStaleTag,
		"with the application deleted the recipe still named %s, so the row above is not "+
			"measuring that line:\n%s", injectedStaleTag, out)
}
