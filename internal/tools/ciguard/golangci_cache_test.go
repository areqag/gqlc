// Assertions holding the CI-side half of bd gqlc-l45j together: the composite
// action that restores the pinned golangci-lint binary from cache, and the
// join between it and the justfile it takes its path and version from.
//
// The action is what stops every linting job re-downloading the binary, which
// is what a GitHub 429 turned into a red required context that never compiled
// the change. Nothing else notices when that join breaks. A cache path that no
// longer matches where the justfile installs restores into a directory nothing
// reads; a version literal that drifts keys the cache to a build
// ensure-golangci rejects. Both leave a green cache step above a full download
// on every job — the exposure back, with no signal that it returned.
package ciguard_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	golangciAction = ".github/actions/setup-golangci/action.yml"
	// The `uses:` value a job writes to pull the action in.
	golangciActionRef = "./.github/actions/setup-golangci"
	// setup-golangci reads the pin with `just --evaluate`, so a job has to pull
	// this in first.
	justActionRef = "./.github/actions/setup-just"
	justfile      = "justfile"
	// The recipe every linter-provisioning path goes through.
	provisionRecipe = "ensure-golangci"
	// The justfile variable holding the pinned version.
	versionVar = "golangci_version"
)

// A step as the assertions below need to see it: `uses` for the action
// reference, `run` for the shell, `with` for the cache inputs, `if` for whether
// the step runs at all.
//
// If is a yaml.Node rather than a string because `if: false` is a YAML bool and
// would fail a string decode — turning a switched-off restore into a decode
// error attributed to the whole job rather than to the step that carries it.
type actionStep struct {
	Name string            `yaml:"name"`
	ID   string            `yaml:"id"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	If   yaml.Node         `yaml:"if"`
	With map[string]string `yaml:"with"`
}

// shellCode is run with shell comments and blank lines removed.
//
// An assertion about what a step runs must not be satisfiable by a line that is
// commented out. Commenting out the version read and restating the pin as a
// literal (`# just --evaluate golangci_version` above `echo
// "version=v2.12.2"`) is the precise defect reading the version off the
// justfile exists to prevent, and it satisfies a Contains over the raw source.
//
// A `#` inside a quoted string is truncated here too. That direction is safe:
// dropping text can only make the assertions below fail, never pass.
func shellCode(run string) string {
	var kept []string
	for _, line := range strings.Split(run, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, rel))
	require.NoErrorf(t, err, "read %s", rel)
	return string(src)
}

// actionSteps decodes the composite action's step list.
func actionSteps(t *testing.T) []actionStep {
	t.Helper()
	var doc struct {
		Runs struct {
			Using string       `yaml:"using"`
			Steps []actionStep `yaml:"steps"`
		} `yaml:"runs"`
	}
	require.NoErrorf(t, yaml.Unmarshal([]byte(readRepoFile(t, golangciAction)), &doc),
		"parse %s", golangciAction)
	require.Equalf(t, "composite", doc.Runs.Using,
		"%s is no longer a composite action, so the jobs that `uses:` it are not "+
			"running the steps below", golangciAction)
	require.NotEmptyf(t, doc.Runs.Steps, "%s declares no steps, so it restores nothing "+
		"and every job re-downloads the linter (bd gqlc-l45j)", golangciAction)
	return doc.Runs.Steps
}

// cacheStep returns the actions/cache step and the path it caches.
func cacheStep(t *testing.T) actionStep {
	t.Helper()
	for _, s := range actionSteps(t) {
		if strings.HasPrefix(s.Uses, "actions/cache@") {
			return s
		}
	}
	t.Fatalf("no step in %s uses actions/cache, so the binary is not cached at all "+
		"and the 429 exposure in bd gqlc-l45j is open on every job", golangciAction)
	return actionStep{}
}

// justInstallPath is where the justfile puts golangci-lint, relative to the
// repo root. Read off the assignment rather than spelled here: this test exists
// to catch the two files disagreeing, and a third copy of the string would just
// move the disagreement.
func justInstallPath(t *testing.T) string {
	t.Helper()
	// golangci := justfile_directory() + "/.bin/golangci-lint"
	re := regexp.MustCompile(`(?m)^golangci\s*:=\s*justfile_directory\(\)\s*\+\s*"/([^"]+)"`)
	m := re.FindStringSubmatch(readRepoFile(t, justfile))
	require.Lenf(t, m, 2, "could not read the golangci-lint install path out of the %s. "+
		"This test cannot compare the cache path against a path it failed to find, and "+
		"passing on a failed read is how a guard goes quiet without going away.", justfile)
	return m[1]
}

// The cache has to name the file the justfile installs. A stale path here does
// not fail anything: actions/cache warns that the path did not exist and the
// job goes green, having downloaded the binary it was supposed to restore.
func TestGolangciBinaryCacheTracksTheJustfileInstallPath(t *testing.T) {
	want := justInstallPath(t)
	require.Equalf(t, want, cacheStep(t).With["path"],
		"%s caches %q but the justfile installs golangci-lint to %q. A cache keyed on "+
			"a path nothing reads restores nothing, warns, and lets every job pay the "+
			"download the cache was added to remove (bd gqlc-l45j).",
		golangciAction, cacheStep(t).With["path"], want)
}

// pinStep returns the step that evaluates the justfile's version variable.
// Matched over shellCode rather than the raw `run`, so a commented-out
// evaluation reads as absent.
func pinStep(t *testing.T) actionStep {
	t.Helper()
	for _, s := range actionSteps(t) {
		if strings.Contains(shellCode(s.Run), "just --evaluate "+versionVar) {
			return s
		}
	}
	return actionStep{}
}

// The cache key has to move when the pin moves, and it has to move by reading
// the justfile rather than by someone remembering to edit two files.
func TestGolangciBinaryCacheKeyIsDerivedFromTheJustfilePin(t *testing.T) {
	pin := pinStep(t)
	require.NotEmptyf(t, pin.ID,
		"no step in %s evaluates `just --evaluate %s` under an `id:`, so the cache key "+
			"below cannot be reading the pinned version off the justfile. Either the "+
			"version is restated in this file — a second place to bump, and a stale one "+
			"keys the cache to a build ensure-golangci rejects — or the key no longer "+
			"varies with the pin at all. A commented-out evaluation counts as absent "+
			"here: it leaves the key on whatever literal the live line carries.",
		golangciAction, versionVar)

	key := cacheStep(t).With["key"]
	require.Containsf(t, key, "steps."+pin.ID+".outputs.version",
		"the cache key %q does not interpolate the version read by step %q, so bumping "+
			"%s in the justfile leaves the key pointing at the previous binary",
		key, pin.ID, versionVar)

	// The variable the step evaluates must still be the one the justfile declares.
	require.Regexpf(t, `(?m)^`+versionVar+`\s*:=`, readRepoFile(t, justfile),
		"the justfile no longer declares %s, which %s evaluates", versionVar, golangciAction)
}

// A failed version read has to fail the step, not empty the key.
//
// `set -e` acts on the exit status of the simple command. In `echo
// "version=$(just --evaluate ...)"` that status is echo's, so the step exits 0
// with an empty output and the cache key collapses to `golangci-bin-<os>-`:
// green step, no restore, full download on every job — the exposure this action
// closes, silently reopened. Only an assignment-only command inherits the
// command substitution's status, which is why the read is on its own line.
//
// Witnessed out of tree against this branch's justfile:
//
//	echo "v=$(just --evaluate absent)" >> f   # rc 0, f == "v="
//	v="$(just --evaluate absent)"             # rc 1, f untouched
func TestGolangciVersionReadFailsTheStepInsteadOfEmptyingTheKey(t *testing.T) {
	code := shellCode(pinStep(t).Run)

	require.Regexpf(t, `(?m)^\s*[A-Za-z_][A-Za-z0-9_]*="?\$\(just --evaluate `+versionVar+`\)"?\s*$`,
		code,
		"the step in %s reads %s somewhere other than in an assignment-only command:\n%s\n"+
			"Interpolated into another command's arguments, a failed read exits 0 and the "+
			"key becomes `golangci-bin-<os>-` — a green cache step above a full download "+
			"on every linting job (bd gqlc-l45j).",
		golangciAction, versionVar, code)

	// The step carries its own errexit rather than relying on the default flags
	// the runner gives `shell: bash`. The assignment above only aborts under
	// errexit, and the comment in the action file argues from this line.
	require.Regexpf(t, `(?m)^\s*set\s+-[a-z]*e[a-z]*(\s|$)`, code,
		"the step in %s no longer sets errexit, so the version read below it can fail "+
			"and leave the following line to write an empty version. It ran under the "+
			"runner's default `shell: bash` flags, which are not stated in this repo and "+
			"are not this file's to promise.", golangciAction)
}

// linterRecipes is every justfile recipe that reaches ensure-golangci through
// its dependency list, transitively.
//
// Derived rather than listed because the property is about recipes nobody has
// written yet: a job added next month that runs `just fmt-check` needs the
// action as much as `lint` does, and a hardcoded list would pass while it
// downloads on every run.
func linterRecipes(t *testing.T) map[string]bool {
	t.Helper()
	// A recipe header: name, optional parameters, `:`, then the dependency list.
	// Recipe bodies are indented, so an unindented `name...:` is the only thing
	// this can match.
	header := regexp.MustCompile(`(?m)^([a-zA-Z0-9_-]+)([^:\n]*):([^=\n].*|)$`)

	deps := map[string][]string{}
	for _, m := range header.FindAllStringSubmatch(readRepoFile(t, justfile), -1) {
		deps[m[1]] = strings.Fields(m[3])
	}
	require.Containsf(t, deps, provisionRecipe,
		"the recipe-header scan did not find %s in the %s, so the dependency walk below "+
			"starts from nothing and would report that no job needs the linter",
		provisionRecipe, justfile)

	reaches := map[string]bool{provisionRecipe: true}
	// Fixed point: the graph is tiny and this is O(recipes^2) at worst.
	for changed := true; changed; {
		changed = false
		for name, ds := range deps {
			if reaches[name] {
				continue
			}
			for _, d := range ds {
				if reaches[d] {
					reaches[name] = true
					changed = true
					break
				}
			}
		}
	}
	delete(reaches, provisionRecipe)

	// Positive controls, per recipe rather than on the size of the set: an
	// emptiness check alone passes on a walk that found one of four.
	for _, want := range []string{"lint", "lint-new", "fmt", "fmt-check", "test-codegen-fence"} {
		require.Truef(t, reaches[want],
			"the dependency walk over the %s does not have %q reaching %s, but it does. "+
				"The walk is broken, so the job assertions built on it are vacuous.",
			justfile, want, provisionRecipe)
	}
	return reaches
}

// justInvocations returns the recipe names a step's shell runs through `just`.
func justInvocations(run string) []string {
	var out []string
	re := regexp.MustCompile(`(?m)\bjust\s+([a-zA-Z0-9_-]+)`)
	for _, m := range re.FindAllStringSubmatch(run, -1) {
		out = append(out, m[1])
	}
	return out
}

// Every ci.yml job that runs a recipe needing golangci-lint must pull the
// action in, or that job downloads the binary itself on every run — which is
// the request that returned 429 and killed a required context in setup.
func TestEveryLintingCIJobRestoresTheCachedBinary(t *testing.T) {
	needsLinter := linterRecipes(t)

	jobs := childByKey(ciDoc(t), "jobs")
	require.NotNilf(t, jobs, "%s has no jobs", ciWorkflow)

	// cachedAt/justAt are step positions, not booleans, because setup-golangci
	// evaluates `just --evaluate` and so has to run after setup-just has put
	// just on PATH. cachedIf holds the step's `if:`, which decides whether the
	// step referenced here runs at all.
	type jobFacts struct {
		recipes  []string
		cachedAt int
		cachedIf string
		justAt   int
	}
	found := map[string]jobFacts{}

	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name := jobs.Content[i].Value
		var job struct {
			Steps []actionStep `yaml:"steps"`
		}
		require.NoErrorf(t, jobs.Content[i+1].Decode(&job), "decode job %q", name)

		facts := jobFacts{cachedAt: -1, justAt: -1}
		for at, s := range job.Steps {
			switch s.Uses {
			case golangciActionRef:
				facts.cachedAt = at
				facts.cachedIf = s.If.Value
			case justActionRef:
				facts.justAt = at
			}
			for _, r := range justInvocations(s.Run) {
				if needsLinter[r] {
					facts.recipes = append(facts.recipes, r)
				}
			}
		}
		if len(facts.recipes) > 0 {
			sort.Strings(facts.recipes)
			found[name] = facts
		}
	}

	// Controls, asserted per job: a single "the map is non-empty" check would
	// still pass if the scan lost one of the two jobs that lint.
	for _, want := range []string{"lint", "codegen-fence"} {
		require.Containsf(t, found, want,
			"the scan of %s did not find job %q running any recipe that needs "+
				"golangci-lint, but it runs one. The scan is broken, so this test is "+
				"asserting nothing about the jobs it did find.", ciWorkflow, want)
	}

	for name, facts := range found {
		require.GreaterOrEqualf(t, facts.cachedAt, 0,
			"job %q runs %v, which provisions golangci-lint, but does not `uses: %s`. "+
				"It will download the binary on every run; that download answered HTTP 429 "+
				"under this repo's own concurrency and killed the context in setup, before "+
				"a line of the change was read (bd gqlc-l45j).",
			name, facts.recipes, golangciActionRef)

		// A reference is not a restore. `if:` on the step leaves the reference in
		// place, reports the step as skipped, and downloads the binary anyway —
		// the same exposure as deleting the step, with none of the visibility.
		require.Emptyf(t, facts.cachedIf,
			"job %q gates `uses: %s` behind `if: %s`. A skipped restore restores nothing: "+
				"the job runs %v, downloads the linter itself, and shows a green step "+
				"where the cache was supposed to be.",
			name, golangciActionRef, facts.cachedIf, facts.recipes)

		// setup-golangci reads the pinned version with `just --evaluate`, so it
		// needs just already on PATH. Out of order it is not a failure to debug:
		// see TestGolangciVersionReadFailsTheStepInsteadOfEmptyingTheKey for the
		// half of this that makes the step fail rather than emit an empty key.
		require.GreaterOrEqualf(t, facts.justAt, 0,
			"job %q pulls in %s but never `uses: %s`, so just is not on PATH when the "+
				"version read runs.", name, golangciActionRef, justActionRef)
		require.Greaterf(t, facts.cachedAt, facts.justAt,
			"job %q runs `uses: %s` at step %d, before its prerequisite `uses: %s` at "+
				"step %d. The version read needs just on PATH.",
			name, golangciActionRef, facts.cachedAt, justActionRef, facts.justAt)
	}
}
