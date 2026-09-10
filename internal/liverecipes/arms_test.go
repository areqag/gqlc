package liverecipes_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/liverecipes"
)

// TestEveryLiveTestRunsInItsDeclaredArm is the per-half binding the union
// guard cannot state: the registry declares which half each live test belongs in,
// and this checks the two halves' -run lists against it in both directions.
//
// A test moved to the half without its container, or dropped from the half
// that has it while the other half still claims it, keeps the union the
// existing guard reads unchanged — so neither direction here may go through
// the union. Each half is read on its own, and a mismatch names the test.
func TestEveryLiveTestRunsInItsDeclaredArm(t *testing.T) {
	split, complaints, err := liverecipes.Read(repoRoot)
	require.NoError(t, err)
	require.Empty(t, complaints, "the artefacts this reads have to be the ones CI runs")
	require.NotEmpty(t, split.Declared, "a live battery this reads as empty is one nothing below can be false of")

	var unregistered []string
	for _, name := range split.Declared {
		if _, ok := liverecipes.LiveArms[name]; !ok {
			unregistered = append(unregistered, name)
		}
	}
	require.Empty(t, unregistered,
		"a live test with no registry row has no declared arm: add it to liverecipes.LiveArms")

	var stale []string
	for name := range liverecipes.LiveArms {
		if !slices.Contains(split.Declared, name) {
			stale = append(stale, name)
		}
	}
	slices.Sort(stale)
	require.Empty(t, stale,
		"a registry row no live test declares is an allowlist entry for a test that does not exist: delete it with the test")

	for _, half := range []struct {
		recipe string
		arm    liverecipes.Arm
	}{
		{liverecipes.Neo4jRecipe, liverecipes.ArmNeo4j},
		{liverecipes.AgeRecipe, liverecipes.ArmAGE},
	} {
		t.Run(half.recipe, func(t *testing.T) {
			require.True(t,
				slices.ContainsFunc(split.CI, func(inv liverecipes.Invocation) bool { return inv.Recipe == half.recipe }),
				"no CI invocation runs under %s, so every test registered %s reads as unrun",
				half.recipe, half.arm)

			var mismatched []string
			for _, name := range split.Declared {
				registered, ok := liverecipes.LiveArms[name]
				if !ok {
					continue
				}
				runs := slices.ContainsFunc(split.CI, func(inv liverecipes.Invocation) bool {
					return inv.Recipe == half.recipe && inv.Claims(name)
				})
				want := registered == half.arm || registered == liverecipes.ArmAll
				switch {
				case want && !runs:
					mismatched = append(mismatched, fmt.Sprintf(
						"%s is registered %s and runs in no %s invocation: it runs against no container that has what it needs",
						name, registered, half.recipe))
				case !want && runs:
					mismatched = append(mismatched, fmt.Sprintf(
						"%s runs under %s but is registered %s: it runs against a container it was not written for",
						name, half.recipe, registered))
				}
			}
			require.Empty(t, mismatched,
				"a half whose -run list disagrees with the registry runs tests where they do not belong")
		})
	}
}
