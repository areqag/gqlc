// Assertions holding the CI-side half of bd gqlc-l45j together: the composite
// action that restores the pinned golangci-lint binary from cache, and the
// join between it and the justfile it takes its path and version from.
//
// The action is what stops every linting job re-downloading the binary, which
// is what a GitHub 429 turned into a red required context that never compiled
// the change. A broken join reddens nothing: a cache path that no longer
// matches where the justfile installs restores into a directory nothing reads,
// and a version literal that drifts keys the cache to a build ensure-golangci
// rejects. Both leave a green cache step above a full download on every linting
// job — the exposure back, with no signal that it returned. These assertions
// are what turns either one red.
package ciguard_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/areqag/gqlc/internal/liverecipes"
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

// A step as the assertions below need to see it.
//
// The field list is the step schema, not the subset this file happened to need
// first. An unmodelled key is an unasserted key: `if` was added after a
// switched-off restore turned out to be unassertable, and the next round found
// `continue-on-error` doing the same damage through the field beside it. The
// schema for a composite action's `runs.steps[*]` documents ten keys — run,
// shell, uses, with, name, id, if, env, continue-on-error, working-directory
// (docs.github.com "Metadata syntax for GitHub Actions", read 2026-08-17) — and
// a job's step adds timeout-minutes. All ten are here; timeout-minutes is not,
// because a timeout makes a step fail and nothing here is guarding against a
// step that fails.
//
// If, ContinueOnError and Env are yaml.Node rather than typed values because
// each has more than one legal type — `if: false` is a YAML bool,
// `continue-on-error: ${{ ... }}` is a string, an env value can be a number —
// and a decode error would attribute a switched-off restore to the whole
// document rather than to the step carrying it. present() below reads them.
type actionStep struct {
	Name             string            `yaml:"name"`
	ID               string            `yaml:"id"`
	Uses             string            `yaml:"uses"`
	Run              string            `yaml:"run"`
	Shell            string            `yaml:"shell"`
	WorkingDirectory string            `yaml:"working-directory"`
	If               yaml.Node         `yaml:"if"`
	ContinueOnError  yaml.Node         `yaml:"continue-on-error"`
	Env              yaml.Node         `yaml:"env"`
	With             map[string]string `yaml:"with"`
}

// present reports whether a YAML key was written at all. An absent key decodes
// to the zero Node, which has no Kind; any value at all — including `false`,
// including an empty mapping — has one.
//
// Emptiness of Value is the wrong test: `continue-on-error: true` and
// `if: false` both carry a Value, but so does nothing, and a mapping's Value is
// empty however much it contains.
func present(n yaml.Node) bool { return n.Kind != 0 }

// spell renders a node for a failure message.
func spell(n yaml.Node) string {
	if n.Value != "" {
		return n.Value
	}
	return n.Tag
}

// uncommented is src with every line's comment dropped and blank lines removed:
// the text a scan for a live command may look in. Two strips, because the two
// artefacts read here spell a comment by different rules and one function
// modelling both models neither (bd gqlc-snzq F6).
//
// An assertion about what a step runs must not be satisfiable by a line that is
// commented out. Commenting out the version read and restating the pin as a
// literal (`# just --evaluate golangci_version` above `echo
// "version=v2.12.2"`) is the precise defect reading the version off the
// justfile exists to prevent, and it satisfies a Contains over the raw source.
// The same move retires a recipe rather than a pin, and both spellings were
// measured on this branch: with the scans reading raw text, `# just test` in
// the ci.yml test step and `test: check-hooks # test-hooks` in the justfile
// each left the scan reading it reporting a recipe that nothing ran, while
// taking the hook suites out of CI.
//
// WHAT WAS WRONG WITH ONE CUT AT THE FIRST `#`. Neither language starts a
// comment at every `#`. A `#` inside a quoted string is data to bash and data
// to just, and the naive cut truncated there, so the strip dropped text the
// artefact runs and every reader downstream saw a line shorter than the one
// executing. That is the direction that MAKES matches as well as breaking them:
// `version=$(just --evaluate golangci_version)"#x" || true` reads as an
// assignment-only command once truncated, and
// TestGolangciVersionReadIsAnAssignmentUnderErrexit passed on it (measured).
// The refusal that catches that line regardless is
// TestGolangciVersionReadFailsTheStepInsteadOfEmptyingTheKey, which runs the
// block instead of reading it — the quoted-`#` hole is narrowed here, not
// closed here.
//
// WHERE THE TWO RULES PART COMPANY, which is why there are two functions. sh
// starts a comment at a `#` that begins a WORD; `-X main.p=a#b` unquoted keeps
// its hash. just does not: it reads a `#` mid-token on a recipe header as a
// comment, and `test: check-hooks test-hooks#x` dumps as `test: check-hooks
// test-hooks` (measured with `just --dump`). So the justfile strip is
// quote-aware and not word-aware, and the shell strip is both — which is
// liverecipes.StripComment, whose own doc comment states the shapes it still
// does not model (backslash escapes, command substitution, heredocs,
// here-strings).
//
// The direction each reader fails in is unchanged by the fix and is still
// worth naming. recipeDeps' header regexp is line-anchored and truncation
// removes only a suffix, so it cannot move or introduce the first `:`: a recipe
// header is lost, never gained. pinStep is a substring scan, and dropping text
// only ever refuses. TestStripsAgreeWithTheLanguagesTheyModel below is where
// the two rules are held to their artefacts.

// justUncommented models just's comment rule: a `#` outside a single- or
// double-quoted string opens a comment, wherever in the line it falls.
func justUncommented(src string) string {
	return dropBlank(src, func(line string) string {
		var quote rune
		for i, r := range line {
			switch {
			case quote != 0:
				if r == quote {
					quote = 0
				}
			case r == '\'' || r == '"':
				quote = r
			case r == '#':
				return line[:i]
			}
		}
		return line
	})
}

// shellUncommented models sh's comment rule, via the reader the recipe-parsing
// package already carries, so a third rule is not invented here.
func shellUncommented(src string) string {
	return dropBlank(src, liverecipes.StripComment)
}

// dropBlank applies strip to every line and drops the lines it empties, so that
// a comment-only line cannot leave a blank one a `(?m)^\s*$` would match.
func dropBlank(src string, strip func(string) string) string {
	var kept []string
	for _, line := range strings.Split(src, "\n") {
		line = strip(line)
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
// Matched over uncommented rather than the raw `run`, so a commented-out
// evaluation reads as absent.
func pinStep(t *testing.T) actionStep {
	t.Helper()
	for _, s := range actionSteps(t) {
		if strings.Contains(shellUncommented(s.Run), "just --evaluate "+versionVar) {
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

// runPinStep executes the pin step's own `run` block with a stub `just` first
// on PATH, and returns its exit status and whatever it wrote to $GITHUB_OUTPUT.
//
// The three assertions above are about shape: that an evaluation exists under
// an id, and that the key interpolates that id. None of them joins the value
// the step evaluates to the value it emits. Keeping the evaluation exactly as
// written and emitting a literal beside it — `version=v2.11.0` — satisfies all
// three, and is verbatim the defect the action's own comment credits the
// derivation with preventing. Running the block is what closes that: the stub
// prints a version no literal would be spelled with, and the output has to
// carry it.
//
// Run under plain `bash <script>` with no flags, deliberately. The runner gives
// `shell: bash` the flags `--noprofile --norc -eo pipefail`, and running with
// those would make this test pass over a step that had dropped its own
// `set -euo pipefail` — which is a thing the action file argues from and this
// repository does not control. With no flags, the only errexit in play is the
// one the step sets itself.
//
// justStub is the body of the fake `just`. onlyPath, when set, replaces PATH
// entirely, which is how the "just is not installed" case is put.
func runPinStep(t *testing.T, justStub, onlyPath string) (status int, output string) {
	t.Helper()
	step := pinStep(t)
	require.NotEmptyf(t, step.Run, "no step in %s evaluates `just --evaluate %s`, so "+
		"there is no shell here to run", golangciAction, versionVar)

	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	if justStub != "" {
		require.NoError(t, os.WriteFile(filepath.Join(bin, "just"), []byte(justStub), 0o755))
	}

	script := filepath.Join(dir, "step.sh")
	require.NoError(t, os.WriteFile(script, []byte(step.Run), 0o600))
	out := filepath.Join(dir, "github_output")
	require.NoError(t, os.WriteFile(out, nil, 0o600))

	path := bin + string(os.PathListSeparator) + os.Getenv("PATH")
	if onlyPath != "" {
		path = onlyPath
	}
	cmd := exec.CommandContext(t.Context(), "bash", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+path, "GITHUB_OUTPUT="+out)
	combined, runErr := cmd.CombinedOutput()

	written, readErr := os.ReadFile(out)
	require.NoError(t, readErr, "read the step's $GITHUB_OUTPUT")

	var ee *exec.ExitError
	switch {
	case runErr == nil:
		status = 0
	case errors.As(runErr, &ee):
		status = ee.ExitCode()
	default:
		t.Fatalf("could not run the pin step's shell: %v\n%s", runErr, combined)
	}
	t.Logf("pin step exited %d\n%s", status, combined)
	return status, string(written)
}

// A version the justfile would never carry, so nothing but a live read of the
// stub can produce it.
const stubbedVersion = "v0.0.0-stubbed-by-ciguard"

const justPrintsStubbedVersion = `#!/usr/bin/env bash
printf '%s\n'
`

// The step has to emit the version it evaluated, not a version.
//
// This is also the control for the refusals below: a harness whose stub `just`
// was never reached, or whose $GITHUB_OUTPUT the step never wrote to, would
// satisfy "non-zero status and an empty output" while measuring nothing at all.
// Here the same harness has to produce a specific string.
func TestGolangciVersionStepEmitsTheVersionItEvaluated(t *testing.T) {
	status, output := runPinStep(t,
		strings.Replace(justPrintsStubbedVersion, "%s", stubbedVersion, 1), "")
	require.Zerof(t, status, "the pin step in %s exited %d with `just --evaluate` "+
		"succeeding", golangciAction, status)
	require.Equalf(t, "version="+stubbedVersion+"\n", output,
		"the pin step evaluated the justfile's %s and then wrote %q. The key it feeds "+
			"is not derived from the pin: a literal here is a second place to bump, and "+
			"a stale one keys the cache to a build ensure-golangci rejects — a green "+
			"cache step above a full download on every linting job (bd gqlc-l45j).",
		versionVar, output)
}

// A failed version read has to fail the step, not empty the key.
//
// `set -e` acts on the exit status of the simple command. In `echo
// "version=$(just --evaluate ...)"` that status is echo's, so the step exits 0
// with an empty output and the cache key collapses to `golangci-bin-<os>-`:
// green step, no restore, full download on every job — the exposure this action
// closes, silently reopened.
//
// Asserted by running the step, because every spelling that reopens it is a
// different string and the same defect: the read moved back into an argument,
// the errexit dropped, a `set +e` above it, a `|| true` after it.
func TestGolangciVersionReadFailsTheStepInsteadOfEmptyingTheKey(t *testing.T) {
	cases := map[string]struct{ stub, path string }{
		// The renamed-variable case: just runs and refuses.
		"the read fails": {stub: "#!/usr/bin/env bash\n" +
			"echo 'error: Justfile does not contain variable' >&2\nexit 1\n"},
		// The job that forgot setup-just: `just` is not there at all.
		"just is not installed": {path: "/nonexistent"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			status, output := runPinStep(t, c.stub, c.path)
			require.NotZerof(t, status,
				"the pin step in %s exited 0 after the version read failed (%s). The next "+
					"line writes whatever the read left behind, and an empty version keys "+
					"the cache to `golangci-bin-<os>-`: a green step, no restore, and the "+
					"download this action removes back on every linting job (bd gqlc-l45j).",
				golangciAction, name)
			require.Emptyf(t, output,
				"the pin step wrote %q to $GITHUB_OUTPUT after the version read failed (%s). "+
					"An emptied version is a cache key that matches nothing and warns about "+
					"nothing.", output, name)
		})
	}
}

// The spelling that makes the two behavioural refusals above hold, pinned so a
// failure names the cause rather than only the symptom.
//
// A simple command with no command word takes its exit status from the last
// command substitution performed in it, so a bare assignment propagates the
// read's failure; in `echo "version=$(just --evaluate ...)"` the status is
// echo's and the failure is discarded. Two other spellings propagate too — a
// bare `$(cmd)` as a command, and `cmd | ...` under pipefail — but neither
// leaves the value in a variable, so neither is a candidate here.
//
// Witnessed out of tree, both under `bash -eo pipefail -c`:
//
//	echo "v=$(just --evaluate absent)" >> f      # rc 0, f == "v="
//	v="$(just --evaluate absent)"; echo "$v" >>f # rc 1, f untouched
//
// The first assertion is end-anchored, and that is one direction uncommented is
// unsafe in: a `#` inside a quoted string is truncated, so text after it stops
// being seen and a line that would not have matched to the end now does. See
// the note there for the line that gets past this, and for the behavioural test
// that refuses it. It is not the only one — the note on justInvocations has a
// second, where the strip joined two lines into a match until the separator
// class stopped it.
func TestGolangciVersionReadIsAnAssignmentUnderErrexit(t *testing.T) {
	code := shellUncommented(pinStep(t).Run)

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

// Nothing in the action may switch a step off or swallow its failure.
//
// The step-level half of what the job-level assertions further down do. A
// composite action's steps take `if` and `continue-on-error` of their own, and
// either one on the pin step voids the two refusals above from inside the file
// they are asserted against: the read still fails, and the action still
// succeeds.
//
// Of the ten keys the composite-step schema permits, this refuses four —
// `if`, `continue-on-error`, `env`, `working-directory` — and pins `shell` on
// any step that runs one. `uses`, `run`, `with`, `id` and `name` are the
// action's shape and are asserted by the tests above. `env` is refused because
// the action takes no configuration: anything set there is either inert or a
// redefinition of a variable the runner provides, and the second is not
// something a cache-restore action should be doing. `working-directory` is
// refused for the same reason rather than because it disarms anything — moving
// the read to a directory with no justfile above it fails the step, which is
// the direction that is safe.
func TestGolangciActionStepsCannotBeSwitchedOffOrSwallowed(t *testing.T) {
	for _, s := range actionSteps(t) {
		what := s.Name
		if what == "" {
			what = s.Uses + s.Run
		}
		require.Falsef(t, present(s.If), "step %q in %s carries `if: %s`. A skipped step "+
			"restores nothing and reads nothing, and reports green either way.",
			what, golangciAction, spell(s.If))
		require.Falsef(t, present(s.ContinueOnError), "step %q in %s carries "+
			"`continue-on-error: %s`, so its failure is not the action's failure. The "+
			"version read is allowed to fail precisely so the job stops; swallowed here, "+
			"it stops nothing and the job downloads the linter itself (bd gqlc-l45j).",
			what, golangciAction, spell(s.ContinueOnError))
		require.Falsef(t, present(s.Env), "step %q in %s carries an `env:` block. This "+
			"action takes no configuration, so anything set there is either inert or a "+
			"redefinition of a runner-provided variable — $GITHUB_OUTPUT among them.",
			what, golangciAction)
		require.Emptyf(t, s.WorkingDirectory, "step %q in %s sets working-directory to "+
			"%q. Every path this action touches is the repository root's.",
			what, golangciAction, s.WorkingDirectory)
		if s.Run != "" {
			require.Equalf(t, "bash", s.Shell, "step %q in %s runs a shell block under "+
				"`shell: %s`. The block is bash — it spells `set -euo pipefail` on its "+
				"first line — and under any other interpreter that line is a syntax error "+
				"or a no-op rather than the errexit the refusals above argue from.",
				what, golangciAction, s.Shell)
		}
	}
}

// recipeDeps is the justfile's recipe dependency graph: each recipe name
// against the names in its dependency list.
//
// A recipe header is a name, optional parameters, `:`, then the dependency
// list. Recipe bodies are indented, so an unindented `name...:` is the only
// thing this can match.
//
// Read through uncommented, because just takes a `#` on a header line as a
// comment: `test: check-hooks # test-hooks` dumps as `test: check-hooks` and
// runs check-hooks only (measured with `just --dump`). Off the raw line the
// commented-out name reads as an edge, which is how the reachability check
// below passed over a `test-hooks` that `just test` no longer ran.
func recipeDeps(t *testing.T) map[string][]string {
	t.Helper()
	header := regexp.MustCompile(`(?m)^([a-zA-Z0-9_-]+)([^:\n]*):([^=\n].*|)$`)
	deps := map[string][]string{}
	for _, m := range header.FindAllStringSubmatch(justUncommented(readRepoFile(t, justfile)), -1) {
		deps[m[1]] = strings.Fields(m[3])
	}
	return deps
}

// recipesReaching is target, plus every recipe that reaches it through a
// dependency list, transitively.
func recipesReaching(deps map[string][]string, target string) map[string]bool {
	reaches := map[string]bool{target: true}
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
	return reaches
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
	deps := recipeDeps(t)
	require.Containsf(t, deps, provisionRecipe,
		"the recipe-header scan did not find %s in the %s, so the dependency walk below "+
			"starts from nothing and would report that no job needs the linter",
		provisionRecipe, justfile)

	reaches := recipesReaching(deps, provisionRecipe)
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

// justInvocations is every recipe name a step's shell block invokes `just`
// with.
//
// Delegated to liverecipes.JustRecipes rather than scanned for here, because
// the scan that was here could not tell a COMMAND from an ARGUMENT: it matched
// `just` anywhere in the text and took the next word as a recipe name, so
// `command -v just test` on one line read as an invocation of `test` and a job
// running no recipe at all satisfied the reachability check (bd gqlc-snzq F1,
// measured on this branch as ci.yml md5 96f4eeaa). JustRecipes splits the line
// into commands at `&&`, `||`, `;` and `|` and asks whether the FIRST field of
// a command is `just`, which is the question the scan was standing in for.
// TestJustInvocationsReadsCommandPositionNotEveryWord is the row.
//
// Comments are dropped by JustRecipes' own per-line strip, so a commented-out
// invocation reads as absent rather than as present: `run: |` with `# just
// test` above `echo skipped` takes the hook suites out of CI, and the raw scan
// reported the step healthy (measured — ci.yml md5 83c139ca to f8a82f6e,
// actionlint rc 0, TestTestHooksIsReachedByARecipeCIRuns green). Per line and
// after the split, which is what the previous separator class was carrying by
// hand: a strip that joined the text first fused a `just` at the end of one
// line with the next line's first word, and `command -v just  # retired` over
// `test -n x` came back as `just test` (ci.yml md5 83c139ca to 1e4fbc40). A
// line-scoped reader cannot make that match at all, so the carriage-return and
// form-feed cases the old character class was chosen to exclude (md5 bca84c62,
// 764788b7, f822b0a5) are decided by Fields' own whitespace splitting instead.
//
// What this returns that the old scan did not: every non-flag word after
// `just`, not just the first, because `just a b` runs both a and b. So
// `just lint-hooks .github/scripts` yields the argument as well as the recipe.
// Both callers ask membership questions over the result, and a name no recipe
// answers to is inert in each.
//
// Every ci.yml md5 named above is a rewrite of the `test` job's step, and
// TestCITestJobRunsTheTestRecipe reddens on each of them — it pins that step's
// shape rather than scanning for a recipe name, so it holds where this scan
// does not. It holds for that one job. This scan is what covers the others,
// which is why the greens above are named per test and not per package.
func justInvocations(run string) []string {
	return liverecipes.JustRecipes(run)
}

// The scan that finds recipe invocations has to read command position, not
// every word on the line (bd gqlc-snzq F1).
//
// The rows below are the two halves of that: a line that RUNS a recipe, and a
// line that merely NAMES one as an argument to something else. Under the
// regexp this replaced the second row returned `test`, so a `test` job whose
// only mention of the recipe was `command -v just test` satisfied
// TestEveryHookTestSuiteIsDiscoveredByTestHooks' sibling reachability check
// while running nothing.
func TestJustInvocationsReadsCommandPositionNotEveryWord(t *testing.T) {
	for _, row := range []struct {
		name string
		run  string
		want []string
	}{
		{"a bare invocation", "just test\n", []string{"test"}},
		{"after an operator", "cd x && just lint\n", []string{"lint"}},
		{
			"with an argument", "just lint-hooks .github/scripts\n",
			[]string{"lint-hooks", ".github/scripts"},
		},
		{
			"flags are not recipes", "just --evaluate golangci_version\n",
			[]string{"golangci_version"},
		},
		// The defect. `just` is an argument to `command`, so nothing is run.
		{"named as an argument", "command -v just test\n", nil},
		{"printed rather than run", "echo just test\n", nil},
		{"commented out", "# just test\necho skipped\n", nil},
		// The line-fusing case the old joined-text strip made: with the comment
		// dropped, `just` ended one line and `test` began the next.
		{
			"a comment between just and the next line",
			"command -v just  # retired\ntest -n x\n", nil,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			require.Equalf(t, row.want, justInvocations(row.run),
				"read of %q", row.run)
		})
	}
}

// Every ci.yml job that runs a recipe needing golangci-lint must pull the
// action in, or that job downloads the binary itself on every run — which is
// the request that returned 429 and killed a required context in setup.
//
// "Pull the action in" is four things, because there are four ways to reference
// it and get nothing: reference it after the prerequisite that puts just on
// PATH, reference it without an `if` that can switch it off, reference it
// without a `continue-on-error` that discards what it reports, and do all of
// that in a job that can itself fail.
//
// The keys ruled out rather than asserted, so the next reader does not have to
// re-derive them.
//
// A `uses:` step takes eight of a job step's eleven keys. actionlint — itself a
// required context here — refuses the other three on one, naming the permitted
// set: continue-on-error, env, id, if, name, timeout-minutes, uses, with
// (measured 2026-08-17 against actionlint's syntax-check on a minimal workflow;
// `shell:`, `working-directory:` and `run:` each exit 1). Of those eight,
// `uses`, `if`, `continue-on-error` and `env` are asserted below;
// `timeout-minutes` makes the step fail, and nothing here guards against
// failing; `with` is inert, since the action declares no inputs; `id` and
// `name` are cosmetic.
//
// Of a job's keys: `strategy.fail-fast` decides whether sibling matrix jobs are
// cancelled, not what this job concludes; a job-level `defaults.run.shell`
// applies to `run:` steps and is in any case overridden by the explicit
// `shell:` each of the action's own run steps carries, which
// TestGolangciActionStepsCannotBeSwitchedOffOrSwallowed pins; `concurrency`,
// `needs` and `timeout-minutes` can stop the job, which stops the download with
// it; `container`, `runs-on`, `services`, `environment`, `permissions`,
// `outputs`, `env` and `name` change where or how the steps run, not whether a
// failure counts.
func TestEveryLintingCIJobRestoresTheCachedBinary(t *testing.T) {
	needsLinter := linterRecipes(t)

	jobs := childByKey(ciDoc(t), "jobs")
	require.NotNilf(t, jobs, "%s has no jobs", ciWorkflow)

	// cachedAt/justAt are step positions, not booleans, because setup-golangci
	// evaluates `just --evaluate` and so has to run after setup-just has put
	// just on PATH. The three nodes are the keys that decide whether the
	// reference is a restore that can fail the job.
	type jobFacts struct {
		recipes    []string
		cachedAt   int
		cachedIf   yaml.Node
		cachedSwal yaml.Node
		cachedEnv  yaml.Node
		jobIf      yaml.Node
		jobSwal    yaml.Node
		justAt     int
	}
	found := map[string]jobFacts{}

	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name := jobs.Content[i].Value
		var job struct {
			If              yaml.Node    `yaml:"if"`
			ContinueOnError yaml.Node    `yaml:"continue-on-error"`
			Steps           []actionStep `yaml:"steps"`
		}
		require.NoErrorf(t, jobs.Content[i+1].Decode(&job), "decode job %q", name)

		facts := jobFacts{cachedAt: -1, justAt: -1, jobIf: job.If, jobSwal: job.ContinueOnError}
		for at, s := range job.Steps {
			switch s.Uses {
			case golangciActionRef:
				facts.cachedAt = at
				facts.cachedIf = s.If
				facts.cachedSwal = s.ContinueOnError
				facts.cachedEnv = s.Env
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
		require.Falsef(t, present(facts.cachedIf),
			"job %q gates `uses: %s` behind `if: %s`. A skipped restore restores nothing: "+
				"the job runs %v, downloads the linter itself, and shows a green step "+
				"where the cache was supposed to be.",
			name, golangciActionRef, spell(facts.cachedIf), facts.recipes)

		// ...and neither is a reference whose failure is discarded. The action's
		// version read is written to fail the step when it cannot read the pin;
		// swallowed here, that failure stops nothing, the job runs on, and
		// ensure-golangci downloads the binary — green, and back where it started.
		require.Falsef(t, present(facts.cachedSwal),
			"job %q sets `continue-on-error: %s` on `uses: %s`. The step still runs and "+
				"still reports, and its failure no longer fails anything: the job goes on "+
				"to run %v and downloads the linter itself (bd gqlc-l45j).",
			name, spell(facts.cachedSwal), golangciActionRef, facts.recipes)
		require.Falsef(t, present(facts.cachedEnv),
			"job %q sets an `env:` block on `uses: %s`. The action takes no inputs and "+
				"reads no configuration; what an env block there can reach is the "+
				"runner's own variables, $GITHUB_OUTPUT among them, which is where the "+
				"version read puts the cache key.", name, golangciActionRef)

		// The job has to be able to fail at all. A job-level continue-on-error
		// reports success whatever its steps did; a job-level `if` reports the
		// required context as skipped, which satisfies branch protection and
		// witnesses nothing.
		require.Falsef(t, present(facts.jobSwal),
			"job %q sets `continue-on-error: %s`, so no step in it can fail the merge — "+
				"including the restore this test is about.", name, spell(facts.jobSwal))
		require.Falsef(t, present(facts.jobIf),
			"job %q is gated behind `if: %s`. It is a required context: skipped, it "+
				"reports as satisfied without having linted or restored anything.",
			name, spell(facts.jobIf))

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

// The two comment strips have to model the two comment rules (bd gqlc-snzq F6).
//
// One function cutting at the first `#` of any kind stood in for both, and it
// was wrong for both in the same direction: it truncated inside a quoted
// string, where neither language starts a comment, so every reader downstream
// saw a line shorter than the one the artefact runs.
//
// `justWant` and `shellWant` are what each strip must leave. The rows where
// they DIFFER are the reason the function was split — a `#` that does not begin
// a word is a comment to just and is not one to sh — and the rows where a naive
// cut would differ from BOTH are the defect.
//
// The two header rows' premises are measured against the real `just`, not
// asserted: a quoted `#` in a default parameter parses (`just --dump` exits 0
// on `lint-hooks dir=".#githooks": dep`), and a bare `#` mid-token on a header
// does not (`dep#x:` is refused with "expected '*', ':', '$', identifier, or
// '+', but found comment"). Both measured against just 1.58.0 on this branch.
func TestStripsAgreeWithTheLanguagesTheyModel(t *testing.T) {
	for _, row := range []struct {
		name                 string
		line                 string
		justWant, shellWant  string
		naiveCutWouldTruncat bool
	}{
		{"no hash at all", "just test", "just test", "just test", false},
		{"a whole-line comment", "# just test", "", "", false},
		// The trailing space is the comment's own; neither strip trims, because
		// every reader downstream is a regexp or a Fields split.
		{
			"a trailing comment", "test: check-hooks # test-hooks",
			"test: check-hooks ", "test: check-hooks ", false,
		},
		// The divergence. just reads this as a comment mid-token; sh does not,
		// because the `#` does not begin a word.
		{
			"a hash inside a bare word", "test: check-hooks test-hooks#x",
			"test: check-hooks test-hooks", "test: check-hooks test-hooks#x", false,
		},
		// The defect: neither language cuts here, and the naive cut did.
		{
			"a hash inside a double-quoted string", `lint-hooks dir=".#githooks": dep`,
			`lint-hooks dir=".#githooks": dep`, `lint-hooks dir=".#githooks": dep`, true,
		},
		{
			"a hash inside a single-quoted string", `echo 'a#b' && just test`,
			`echo 'a#b' && just test`, `echo 'a#b' && just test`, true,
		},
		{
			"a comment after a quoted hash", `echo "a#b" # gone`,
			`echo "a#b" `, `echo "a#b" `, true,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			naive := row.line
			if i := strings.IndexByte(naive, '#'); i >= 0 {
				naive = naive[:i]
			}
			require.Equalf(t, row.naiveCutWouldTruncat, naive != row.justWant && naive != row.shellWant,
				"this row's premise is that the naive cut at the first `#` %s both "+
					"languages on %q; it leaves %q",
				map[bool]string{true: "disagrees with", false: "agrees with at least one of"}[row.naiveCutWouldTruncat],
				row.line, naive)

			require.Equalf(t, row.justWant, justUncommented(row.line),
				"the justfile strip on %q. A `#` outside quotes opens a comment to just "+
					"wherever it falls, and one inside quotes never does.", row.line)
			require.Equalf(t, row.shellWant, shellUncommented(row.line),
				"the shell strip on %q. sh starts a comment at a `#` that begins a word, "+
					"outside quotes.", row.line)
		})
	}
}
