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
	justfile          = "justfile"
	// The recipe every linter-provisioning path goes through.
	provisionRecipe = "ensure-golangci"
	// The justfile variable holding the pinned version.
	versionVar = "golangci_version"
)

// A step as the two assertions below need to see it: `uses` for the action
// reference, `run` for the shell, `with` for the cache inputs.
type actionStep struct {
	Name string            `yaml:"name"`
	ID   string            `yaml:"id"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]string `yaml:"with"`
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

// The cache key has to move when the pin moves, and it has to move by reading
// the justfile rather than by someone remembering to edit two files.
func TestGolangciBinaryCacheKeyIsDerivedFromTheJustfilePin(t *testing.T) {
	steps := actionSteps(t)

	var pin actionStep
	for _, s := range steps {
		if strings.Contains(s.Run, "just --evaluate "+versionVar) {
			pin = s
			break
		}
	}
	require.NotEmptyf(t, pin.ID,
		"no step in %s evaluates `just --evaluate %s` under an `id:`, so the cache key "+
			"below cannot be reading the pinned version off the justfile. Either the "+
			"version is restated in this file — a second place to bump, and a stale one "+
			"keys the cache to a build ensure-golangci rejects — or the key no longer "+
			"varies with the pin at all.", golangciAction, versionVar)

	key := cacheStep(t).With["key"]
	require.Containsf(t, key, "steps."+pin.ID+".outputs.version",
		"the cache key %q does not interpolate the version read by step %q, so bumping "+
			"%s in the justfile leaves the key pointing at the previous binary",
		key, pin.ID, versionVar)

	// The variable the step evaluates must still be the one the justfile declares.
	require.Regexpf(t, `(?m)^`+versionVar+`\s*:=`, readRepoFile(t, justfile),
		"the justfile no longer declares %s, which %s evaluates", versionVar, golangciAction)
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

	type jobFacts struct {
		recipes []string
		cached  bool
	}
	found := map[string]jobFacts{}

	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name := jobs.Content[i].Value
		var job struct {
			Steps []actionStep `yaml:"steps"`
		}
		require.NoErrorf(t, jobs.Content[i+1].Decode(&job), "decode job %q", name)

		facts := jobFacts{}
		for _, s := range job.Steps {
			if s.Uses == golangciActionRef {
				facts.cached = true
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
		require.Truef(t, facts.cached,
			"job %q runs %v, which provisions golangci-lint, but does not `uses: %s`. "+
				"It will download the binary on every run; that download answered HTTP 429 "+
				"under this repo's own concurrency and killed the context in setup, before "+
				"a line of the change was read (bd gqlc-l45j).",
			name, facts.recipes, golangciActionRef)
	}
}
