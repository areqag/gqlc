package liverecipes_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/liverecipes"
)

// TestEveryLiveArmRunsOnItsDeclaredTriggers is the guard for the failure a
// conditional CI job cannot signal itself: an arm that stops running is
// reported by GitHub as a skipped check, which satisfies a required context
// and is green. Omission passes there, so the events each arm runs on are
// declared in liverecipes.ArmTriggers and read back off the workflows here.
//
// ONE SUBTEST PER ARM, and set equality inside it rather than a count or a
// union: the halves run on deliberately different event sets, so any rule over
// their total is satisfied by one healthy arm while the other goes dark —
// dropping `pull_request` from live-smoke-age leaves the union unchanged
// because live-smoke still carries it.
func TestEveryLiveArmRunsOnItsDeclaredTriggers(t *testing.T) {
	triggers, complaints, err := liverecipes.ReadTriggers(repoRoot)
	require.NoError(t, err)
	require.Empty(t, complaints, "the workflows this reads have to be ones whose event sets are knowable")
	require.NotEmpty(t, liverecipes.ArmTriggers,
		"an empty registry is one every rule below is vacuously true of")

	for recipe, want := range liverecipes.ArmTriggers {
		t.Run(recipe, func(t *testing.T) {
			got, reached := triggers[recipe]
			require.True(t, reached,
				"no workflow job runs %s, so the arm it is the CI half of runs on no event at all", recipe)
			require.ElementsMatch(t, want, got,
				"%s runs on a different event set than it declares: an event in the registry and not in "+
					"the workflow is an arm that went silent where it is required to run, and one in the "+
					"workflow and not in the registry is an arm running somewhere nobody weighed the cost",
				recipe)
		})
	}
}

// TestEnumeratedEvents drives the `if:` reader over the shapes it has to
// separate, because the real workflow exercises exactly one of them and a
// reader that accepted the others would report an event set it guessed at.
func TestEnumeratedEvents(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expr     string
		want     []string
		readable bool
	}{
		{
			name:     "the positive enumeration decision 0010 requires",
			expr:     "github.event_name == 'schedule' || github.event_name == 'pull_request'",
			want:     []string{"schedule", "pull_request"},
			readable: true,
		},
		{
			name:     "one term",
			expr:     "github.event_name == 'schedule'",
			want:     []string{"schedule"},
			readable: true,
		},
		{
			name:     "wrapped in an expression delimiter",
			expr:     "${{ github.event_name == 'schedule' }}",
			want:     []string{"schedule"},
			readable: true,
		},
		{
			name: "double quotes",
			expr: `github.event_name == "schedule"`,
			want: []string{"schedule"}, readable: true,
		},
		{
			// The form decision 0010 removed. Reading it would mean
			// enumerating the complement, which is GitHub's event list rather
			// than this workflow's.
			name: "a negation",
			expr: "github.event_name != 'pull_request'",
		},
		{
			name: "a conjunction hiding a second operator inside the quoted value",
			expr: "github.event_name == 'schedule' && github.ref == 'refs/heads/master'",
		},
		{
			name: "a disjunct that is not an event equality",
			expr: "github.event_name == 'schedule' || always()",
		},
		{
			name: "a comparison of something other than the event name",
			expr: "github.ref == 'refs/heads/master'",
		},
		{
			name: "an unquoted value",
			expr: "github.event_name == schedule",
		},
		{
			name: "an unclosed expression delimiter",
			expr: "${{ github.event_name == 'schedule'",
		},
		{
			name: "empty",
			expr: "   ",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, readable := liverecipes.EnumeratedEvents(tc.expr)
			require.Equal(t, tc.readable, readable)
			require.Equal(t, tc.want, got)
		})
	}
}

// liveJob is a workflow whose one job runs the AGE half, so that the fixtures
// below differ in the trigger shape alone.
func liveJob(on, jobIf string) []byte {
	step := "      - run: just " + liverecipes.AgeRecipe + "\n"
	body := "jobs:\n  live-smoke-age:\n"
	if jobIf != "" {
		body += "    if: " + jobIf + "\n"
	}
	return []byte(on + body + "    steps:\n" + step)
}

func TestWorkflowTriggers(t *testing.T) {
	t.Run("a job with no if runs on every declared event", func(t *testing.T) {
		byRecipe, complaints, err := liverecipes.WorkflowTriggers(
			liveJob("on:\n  pull_request:\n  schedule:\n    - cron: \"17 4 * * *\"\n", ""))
		require.NoError(t, err)
		require.Empty(t, complaints)
		require.ElementsMatch(t, []string{"pull_request", "schedule"}, byRecipe[liverecipes.AgeRecipe])
	})

	t.Run("an if narrows to the events it enumerates", func(t *testing.T) {
		byRecipe, complaints, err := liverecipes.WorkflowTriggers(liveJob(
			"on:\n  pull_request:\n  merge_group:\n  schedule:\n    - cron: \"17 4 * * *\"\n",
			"github.event_name == 'schedule' || github.event_name == 'pull_request'"))
		require.NoError(t, err)
		require.Empty(t, complaints)
		require.ElementsMatch(t, []string{"pull_request", "schedule"}, byRecipe[liverecipes.AgeRecipe])
	})

	t.Run("an unreadable if is a complaint and not a narrower set", func(t *testing.T) {
		byRecipe, complaints, err := liverecipes.WorkflowTriggers(liveJob(
			"on:\n  pull_request:\n  merge_group:\n", "github.event_name != 'merge_group'"))
		require.NoError(t, err)
		require.Len(t, complaints, 1)
		require.Contains(t, complaints[0], "cannot enumerate")
		require.Empty(t, byRecipe[liverecipes.AgeRecipe])
	})

	t.Run("an enumerated event the workflow never fires is a complaint", func(t *testing.T) {
		byRecipe, complaints, err := liverecipes.WorkflowTriggers(liveJob(
			"on:\n  schedule:\n    - cron: \"17 4 * * *\"\n",
			"github.event_name == 'schedule' || github.event_name == 'pull_request'"))
		require.NoError(t, err)
		require.Len(t, complaints, 1)
		require.Contains(t, complaints[0], "can never be true")
		require.ElementsMatch(t, []string{"schedule"}, byRecipe[liverecipes.AgeRecipe])
	})

	t.Run("a trigger filter over a live arm is a complaint", func(t *testing.T) {
		_, complaints, err := liverecipes.WorkflowTriggers(liveJob(
			"on:\n  pull_request:\n    paths:\n      - internal/codegen/age/**\n", ""))
		require.NoError(t, err)
		require.Len(t, complaints, 1)
		require.Contains(t, complaints[0], "trigger filter")
	})

	t.Run("a job reaching no live arm is not read", func(t *testing.T) {
		byRecipe, complaints, err := liverecipes.WorkflowTriggers([]byte(
			"on:\n  pull_request:\n    paths:\n      - docs/**\n" +
				"jobs:\n  lint:\n    if: github.event_name != 'merge_group'\n    steps:\n      - run: just lint\n"))
		require.NoError(t, err)
		require.Empty(t, complaints,
			"a filter or a condition on a job with no live arm in it is not this guard's business")
		require.Empty(t, byRecipe)
	})
}
