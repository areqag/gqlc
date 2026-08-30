package vulnguard_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The pin is derived but never reaches the go command, which is the shape an
// `export` quietly not taking would produce.
//
// This is the assertion's whole reason for existing. The unplaced-stdlib
// grading below it cannot stand in: it only notices a dead pin on a machine
// whose default Go is one govulncheck cannot place, and on every other machine
// — CI included — a scan under the wrong toolchain places a version fine and
// the recipe runs to the end green. Measured out of tree on the town's box,
// where the default Go IS unplaceable: dropping the export alone is caught
// here, and dropping it with this assertion blinded falls through to the
// grading instead (bd gqlc-irvs).
func TestVulnRefusesAPinThatDidNotReachTheGoCommand(t *testing.T) {
	run := runVuln(t, edit{
		old: `    GOTOOLCHAIN="go$(./.github/scripts/go-toolchain-version.sh go.mod)"`,
		new: `    GOTOOLCHAIN="go9.9.9"`,
	})
	requireRefusal(t, run, "but the go")
}

// The control for the row above: unedited, the derivation reads the throwaway
// tree's own `go` directive and the stub reports that same version back, so the
// assertion passes rather than being absent.
//
// Without it the row above is satisfied by a recipe that refuses every run —
// including one whose pin never works at all — and a guard that always fires is
// not distinguishable from a broken recipe.
func TestVulnScansUnderTheToolchainGoModNames(t *testing.T) {
	run := runVuln(t)
	require.Zerof(t, run.status, "`%s` exited %d over an unedited throwaway tree. Output:\n%s",
		vulnRecipe, run.status, run.output)
	require.Containsf(t, run.output, "vuln: scanning under go"+fixtureGoDirective,
		"`%s` did not report scanning under the toolchain the throwaway tree's go.mod names, "+
			"so the pin is not being derived from go.mod at all and the row above would refuse "+
			"for a reason it does not name (bd gqlc-irvs). Output:\n%s", vulnRecipe, run.output)
}

// The pin FOLLOWS go.mod, rather than agreeing with it once.
//
// The row above cannot establish this on its own: a recipe pinning the
// fixture's version as a literal satisfies it exactly as well as one deriving
// it, because the two spellings are the same string. Moving the directive
// separates them — a literal pin then disagrees with what the stub reports and
// the assertion refuses, which is a failure this test reads as the derivation
// being gone.
func TestVulnFollowsGoModWhenTheDirectiveMoves(t *testing.T) {
	run := runVulnOver(t, func(t *testing.T, dir string) {
		t.Helper()
		gomod := filepath.Join(dir, "go.mod")
		src, err := os.ReadFile(gomod)
		require.NoError(t, err)
		moved := strings.Replace(string(src),
			"go "+fixtureGoDirective, "go "+movedGoDirective, 1)
		require.NotEqualf(t, string(src), moved,
			"the throwaway tree's go.mod does not carry `go %s`, so this test moved nothing "+
				"and the run below would witness the unedited case twice", fixtureGoDirective)
		require.NoError(t, os.WriteFile(gomod, []byte(moved), 0o600))
	})

	require.Zerof(t, run.status, "`%s` exited %d after the throwaway tree's `go` directive moved "+
		"to %s. A pin that is a literal rather than a derivation disagrees with the toolchain "+
		"the go command then reports, and the recipe's own assertion refuses (bd gqlc-irvs). "+
		"Output:\n%s", vulnRecipe, run.status, movedGoDirective, run.output)
	require.Containsf(t, run.output, "vuln: scanning under go"+movedGoDirective,
		"`%s` ran to the end but did not report scanning under go%s, the version the tree's "+
			"go.mod now names (bd gqlc-irvs). Output:\n%s",
		vulnRecipe, movedGoDirective, run.output)
}
