// Package gatesparity_test holds one guard: ci.yml's tidy job and the tidy arms
// of the justfile's `gates` recipe are the same checks, except where this file
// says otherwise and why.
//
// `gates` exists so that a red tidy job is found before the push rather than
// one CI round trip after it, which is only true of the steps it mirrors. The
// mirror was a hand-kept fact on both sides. Deleting the tidy step that runs
// `just test-gates-justfile-format` left every check green, and so did
// deleting `test-setup-go-assertion`'s; in the other direction a step added to
// tidy with no arm is a round trip `gates` silently stopped saving. The list
// of mirrored steps in the recipe's head comment was what stood in for a
// check, and it went stale three times — a count twice, then the list (bd
// gqlc-07di). The comment now points here instead of restating it.
//
// TIDY ONLY. The other jobs pair with `gates` contexts too, and nothing here
// reads them: each of lint, test, codegen-fence and actionlint would need its
// own account of which steps provision and which grade, and the live-smoke
// jobs live in another workflow behind an event filter.
//
// WHAT IT DOES NOT REACH. A `uses:` step is taken to provision and not to
// grade, so a composite action that grew a check would pass unseen. Where
// notArms names an arm that answers for a step under another spelling, that
// the arm really does the step's work is argued in the entry, not measured.
// And of a step's attributes it reads the three in weakening below and no
// others: `env:`, `shell:`, a job-level `if:` or `defaults:` can each change
// what a step grades, and none is looked at.
//
// It is a test rather than a recipe so that `just test` — a required context —
// carries it; a separate gate arm would be one more edge to drop in silence.
// An external test package because it reads ci.yml through a third-party YAML
// package, and govulncheck discards an in-package test variant together with
// what only it imports (`just vuln-root-residual`, bd gqlc-m5rc).
package gatesparity_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	repoRoot     = "../../.."
	justfilePath = "justfile"
	ciPath       = ".github/workflows/ci.yml"
	tidyJob      = "tidy"
	gatesRecipe  = "gates"
	// tidyContext is what a gates arm passes `run` to say it stands for a tidy
	// step.
	tidyContext = "tidy"
	thisFile    = "internal/tools/gatesparity/parity_test.go"
)

// step is one `run:` step of a workflow job. The last three are read only to
// be refused; `continue-on-error` is a boolean or an expression, so it is
// taken as whatever YAML made of it.
type step struct {
	Name            string `yaml:"name"`
	Run             string `yaml:"run"`
	If              string `yaml:"if"`
	ContinueOnError any    `yaml:"continue-on-error"`
	WorkingDir      string `yaml:"working-directory"`
}

// weakening is the attributes under which a step that spells an arm's command
// is still not the gate the arm claims parity with: it can be skipped, which
// satisfies a required context; it can fail without failing the job; or it
// runs the command somewhere the arm does not. A step in notArms may carry
// them, because its entry is where the difference is argued.
func (s step) weakening() []string {
	var found []string
	if s.If != "" {
		found = append(found, "if")
	}
	if s.ContinueOnError != nil {
		found = append(found, "continue-on-error")
	}
	if s.WorkingDir != "" {
		found = append(found, "working-directory")
	}
	return found
}

// justLineRe is a script line that invokes just.
var justLineRe = regexp.MustCompile(`(?m)^\s*(just\s+\S.*?)\s*$`)

// armFor is what a complaint offers as the arm for a step nothing covers. A
// one-line step is its own command. A script is not: flattened, it reads as a
// command nobody wrote, so the just invocations in it are offered instead.
func (s step) armFor() string {
	if !strings.Contains(strings.TrimSpace(s.Run), "\n") {
		return fmt.Sprintf("add `run %s %s` to the %s recipe", tidyContext, s.command(), gatesRecipe)
	}
	var invoked []string
	for _, m := range justLineRe.FindAllStringSubmatch(s.Run, -1) {
		invoked = append(invoked, "`"+strings.Join(strings.Fields(m[1]), " ")+"`")
	}
	if len(invoked) == 0 {
		return fmt.Sprintf("give the %s recipe an arm that runs what the step's script runs, which invokes no just recipe",
			gatesRecipe)
	}
	return fmt.Sprintf("give the %s recipe a `run %s ...` arm for what the step's script runs, which invokes %s",
		gatesRecipe, tidyContext, strings.Join(invoked, " and "))
}

// command is the step's script with its whitespace collapsed, which is what is
// set against an arm's command.
func (s step) command() string { return strings.Join(strings.Fields(s.Run), " ") }

// label is how a step is named in notArms and in a complaint: its `name:`, or
// its command where it has none.
func (s step) label() string {
	if s.Name != "" {
		return s.Name
	}
	return s.command()
}

// arm is one `run <context> <command>` line of the gates recipe.
type arm struct{ Context, Command string }

// notArm accounts for one tidy step that no tidy arm spells.
type notArm struct {
	// Step is the step's label.
	Step string
	// Arm is the gates arm that answers for the step under another spelling,
	// or empty when nothing in gates does.
	Arm string
	// Says is what the recipe's NOT-covered summary has to name when Arm is
	// empty, so that the run tells its reader what this table tells its own.
	// Empty for a step that grades nothing.
	Says string
	// Why is the reason, and is what a reviewer of a new entry reads.
	Why string
}

// notArms is every tidy step that is not a tidy arm word for word. An entry is
// a claim about the step, so it is refused once the step is gone or once an
// arm spells it.
var notArms = []notArm{
	{
		Step: "just --fmt --check --unstable",
		Arm:  "just check-justfile-format",
		Why: "the recipe runs this command and adds the remedy; gates runs it only under the " +
			"just CI pins and says SKIPPED otherwise (bd gqlc-i2lw)",
	},
	{
		Step: "bd export monotonicity vs base",
		Arm:  "just bd-export-monotonic-local",
		Why: "the step hands bd-export-monotonic the PR's base SHA from the event payload; the " +
			"arm derives the merge-base with origin/master and runs the same recipe",
	},
	{
		Step: "just lint-hooks .github/scripts",
		Arm:  "just lint",
		Why:  "`lint` depends on `lint-hooks .github/scripts`, so the lint arm has already run it",
	},
	{
		Step: "install the pinned bd",
		Why: "provisions and grades nothing. The arm for the rows it serves runs against the bd " +
			"this host deploys, on purpose (bd gqlc-kip5)",
	},
	{
		Step: "check PR body carries Closes #N for beads with GH mirror",
		Says: "check-pr-closes.py",
		Why: "the step fetches the PR's body from the pulls API by PR number and hands " +
			"check-pr-closes.py that file; before the PR there is no number and no body",
	},
	{
		Step: "PR commits carry a plausible author and no AI-attribution trailer",
		Says: "check-pr-authors.sh",
		Why: "check-pr-authors.sh reads GitHub's own listing of the PR's commits, fetched by PR " +
			"number; .githooks/commit-msg is the half that runs before one exists",
	},
	{
		Step: "the nightly cron is still firing",
		Says: "check-cron-freshness.sh",
		Why: "check-cron-freshness.sh asks the Actions API when codegen-live.yml last ran on " +
			"schedule. It reads nothing in the tree, so no edit here can move its answer",
	},
}

// readSteps is the `run:` steps of one job in a workflow. A job that is absent
// or runs nothing is an error: every rule below is vacuously true of it.
func readSteps(workflow []byte, job string) ([]step, error) {
	var parsed struct {
		Jobs map[string]struct {
			Steps []step `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflow, &parsed); err != nil {
		return nil, fmt.Errorf("read the workflow: %w", err)
	}
	found, ok := parsed.Jobs[job]
	if !ok {
		return nil, fmt.Errorf("the workflow has no job named %q", job)
	}
	var steps []step
	for _, s := range found.Steps {
		if s.command() != "" {
			steps = append(steps, s)
		}
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("job %q has no `run:` step", job)
	}
	return steps, nil
}

var (
	// armRe is an arm as the recipe spells one: either runner, a context that
	// may be single-quoted, and the command.
	armRe = regexp.MustCompile(`^(?:run|run_under_pinned_just)\s+('[^']*'|[^\s']+)\s+(\S.*)$`)
	// armishRe is a line that calls a runner at all. One that armRe cannot
	// read is refused rather than skipped, since a skipped arm is one this
	// file would report as missing from a recipe that has it.
	armishRe = regexp.MustCompile(`^(?:run|run_under_pinned_just)\s`)
)

// readArms is every arm in the gates recipe's body, given as just dumps it:
// one string per line.
func readArms(body []string) ([]arm, error) {
	var arms []arm
	for _, raw := range body {
		line := strings.TrimSpace(raw)
		if !armishRe.MatchString(line) {
			continue
		}
		m := armRe.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("the %s recipe calls a runner in a shape this reader cannot read: %q",
				gatesRecipe, raw)
		}
		arms = append(arms, arm{
			Context: strings.Trim(m[1], "'"),
			Command: strings.Join(strings.Fields(m[2]), " "),
		})
	}
	if len(arms) == 0 {
		return nil, fmt.Errorf("the %s recipe has no `run <context> <command>` line", gatesRecipe)
	}
	return arms, nil
}

// compare is every way the job's steps, the recipe's arms and the table
// disagree. body is the recipe again, for the summary it prints.
func compare(steps []step, arms []arm, table []notArm, body []string) []string {
	var complaints []string
	tidyArms := map[string]bool{}
	anyArm := map[string]bool{}
	for _, a := range arms {
		anyArm[a.Command] = true
		if a.Context == tidyContext {
			tidyArms[a.Command] = true
		}
	}
	if len(tidyArms) == 0 {
		complaints = append(complaints, fmt.Sprintf(
			"the %s recipe has no arm under the %q context", gatesRecipe, tidyContext))
	}
	var summary []string
	for _, line := range body {
		if strings.HasPrefix(strings.TrimSpace(line), "echo ") {
			summary = append(summary, line)
		}
	}

	entries := map[string]notArm{}
	for _, e := range table {
		entries[e.Step] = e
	}
	answered := map[string]bool{}
	seen := map[string]bool{}
	for _, s := range steps {
		seen[s.label()] = true
		entry, listed := entries[s.label()]
		if tidyArms[s.command()] {
			answered[s.command()] = true
		}
		switch {
		case tidyArms[s.command()] && listed:
			complaints = append(complaints, fmt.Sprintf(
				"notArms lists tidy step %q, and the %s recipe runs `%s` as a tidy arm: the entry is stale, delete it from %s",
				s.label(), gatesRecipe, s.command(), thisFile))
		case tidyArms[s.command()]:
			for _, attr := range s.weakening() {
				complaints = append(complaints, fmt.Sprintf(
					"%s's %s job runs step %q under `%s:`, and the %s recipe claims that step as the arm `%s`. "+
						"A step that can be skipped, can fail without failing the job, or runs somewhere else is not the gate the arm mirrors. "+
						"Either remove `%s:` from the step, or, if it is deliberate, teach %s why the two are still one check",
					ciPath, tidyJob, s.label(), attr, gatesRecipe, s.command(), attr, thisFile))
			}
		case !listed:
			complaints = append(complaints, fmt.Sprintf(
				"%s's %s job runs step %q and `just %s` has no arm for it, so its red is found one CI round trip late. "+
					"If it reads only the tree, %s; "+
					"if it cannot run before the PR exists, add it to notArms in %s with the reason",
				ciPath, tidyJob, s.label(), gatesRecipe, s.armFor(), thisFile))
		case entry.Arm != "" && !anyArm[entry.Arm]:
			complaints = append(complaints, fmt.Sprintf(
				"notArms says the arm `%s` answers for tidy step %q, and the %s recipe has no such arm",
				entry.Arm, s.label(), gatesRecipe))
		case entry.Arm != "":
			answered[entry.Arm] = true
		case entry.Says != "" && !strings.Contains(strings.Join(summary, "\n"), entry.Says):
			complaints = append(complaints, fmt.Sprintf(
				"nothing in `just %s` runs tidy step %q, and the recipe's NOT-covered summary does not name %s",
				gatesRecipe, s.label(), entry.Says))
		}
	}
	for _, e := range table {
		if !seen[e.Step] {
			complaints = append(complaints, fmt.Sprintf(
				"notArms lists %q, which is no step of %s's %s job: the entry is stale, delete it from %s",
				e.Step, ciPath, tidyJob, thisFile))
		}
	}
	for _, a := range arms {
		if a.Context == tidyContext && !answered[a.Command] {
			complaints = append(complaints, fmt.Sprintf(
				"the %s recipe runs `%s` for the %q context and no step of that job runs it, so a red here blocks nothing. "+
					"Either add the step to %s's %s job, or collect the arm under the context whose job does run it",
				gatesRecipe, a.Command, tidyContext, ciPath, tidyJob))
		}
	}
	return complaints
}

// gatesBody asks just for the recipe's body rather than reading it out of the
// file, as internal/tools/ghorphan's justfile test does.
func gatesBody(t *testing.T) []string {
	t.Helper()
	// just's reads are a child's and are not in the test log, so without this
	// one a justfile edit would replay a cached pass.
	if _, err := os.ReadFile(repoRoot + "/" + justfilePath); err != nil {
		t.Fatal(err)
	}
	justBin, err := exec.LookPath("just")
	if err != nil {
		t.Fatalf("`just` is not on PATH: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), justBin,
		"--justfile", justfilePath, "--unstable", "--dump", "--dump-format", "json")
	cmd.Dir = repoRoot
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("`just --dump` on the justfile: %v\n%s", err, errb.String())
	}
	var dumped struct {
		Recipes map[string]struct {
			Body [][]json.RawMessage `json:"body"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(out, &dumped); err != nil {
		t.Fatalf("read `just --dump --dump-format json`: %v", err)
	}
	recipe, ok := dumped.Recipes[gatesRecipe]
	if !ok {
		t.Fatalf("just reports no recipe named %s", gatesRecipe)
	}
	lines := make([]string, 0, len(recipe.Body))
	for _, fragments := range recipe.Body {
		var line strings.Builder
		for _, f := range fragments {
			var text string
			if err := json.Unmarshal(f, &text); err != nil {
				t.Fatalf("the %s recipe interpolates (%s), and this reader does not evaluate just expressions",
					gatesRecipe, f)
			}
			line.WriteString(text)
		}
		lines = append(lines, line.String())
	}
	return lines
}

func TestEveryTidyStepIsAGatesArmOrSaysWhyNot(t *testing.T) {
	raw, err := os.ReadFile(repoRoot + "/" + ciPath)
	if err != nil {
		t.Fatal(err)
	}
	steps, err := readSteps(raw, tidyJob)
	if err != nil {
		t.Fatalf("%s: %v", ciPath, err)
	}
	body := gatesBody(t)
	arms, err := readArms(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, complaint := range compare(steps, arms, notArms, body) {
		t.Error(complaint)
	}
}

// TestCompare drives each refusal over input built for it: the real tree
// exercises none of them while it is healthy.
func TestCompare(t *testing.T) {
	healthySteps := []step{
		{Run: "just tidy-check"},
		{Name: "rows", Run: "just rows"},
		{Run: "just --fmt --check"},
		{Name: "needs a PR", Run: "gh api pulls\ncheck.py body"},
	}
	healthyArms := []arm{
		{"lint", "just lint"},
		{"tidy", "just tidy-check"},
		{"tidy", "just rows"},
		{"tidy", "just check-format"},
	}
	healthyTable := []notArm{
		{Step: "just --fmt --check", Arm: "just check-format"},
		{Step: "needs a PR", Says: "check.py"},
	}
	healthyBody := []string{`    echo "NOT covered: check.py"`}

	for _, tc := range []struct {
		name  string
		steps []step
		arms  []arm
		table []notArm
		body  []string
		want  string // a substring of the one complaint, or empty for none
	}{
		{name: "healthy", steps: healthySteps, arms: healthyArms, table: healthyTable, body: healthyBody},
		{
			name:  "a step with no arm and no entry",
			steps: append([]step{{Name: "new check", Run: "just new-check"}}, healthySteps...),
			arms:  healthyArms, table: healthyTable, body: healthyBody,
			want: `runs step "new check" and`,
		},
		{
			name:  "a script with no arm is offered its just line, not itself flattened",
			steps: append([]step{{Name: "sneaky", Run: "echo hi\njust sneaky-check  docs\n"}}, healthySteps...),
			arms:  healthyArms, table: healthyTable, body: healthyBody,
			want: "arm for what the step's script runs, which invokes `just sneaky-check docs`; if",
		},
		{
			name:  "a script with no arm and no just line",
			steps: append([]step{{Name: "sneaky", Run: "echo hi\n./check.sh\n"}}, healthySteps...),
			arms:  healthyArms, table: healthyTable, body: healthyBody,
			want: "which invokes no just recipe; if",
		},
		{
			name:  "an arm's step behind if",
			steps: append([]step{{Run: "just tidy-check", If: "false"}}, healthySteps[1:]...),
			arms:  healthyArms, table: healthyTable, body: healthyBody,
			want: "runs step \"just tidy-check\" under `if:`",
		},
		{
			name:  "an arm's step allowed to fail",
			steps: append([]step{{Run: "just tidy-check", ContinueOnError: true}}, healthySteps[1:]...),
			arms:  healthyArms, table: healthyTable, body: healthyBody,
			want: "under `continue-on-error:`",
		},
		{
			name:  "an arm's step run somewhere else",
			steps: append([]step{{Run: "just tidy-check", WorkingDir: "docs"}}, healthySteps[1:]...),
			arms:  healthyArms, table: healthyTable, body: healthyBody,
			want: "under `working-directory:`",
		},
		{
			name: "a listed step may carry them",
			steps: append([]step{{Name: "needs a PR", Run: "gh api pulls\ncheck.py body", If: "github.event_name == 'pull_request'"}},
				healthySteps[:3]...),
			arms: healthyArms, table: healthyTable, body: healthyBody,
		},
		{
			name: "a tidy arm with no step", steps: healthySteps,
			arms:  append([]arm{{"tidy", "just orphan"}}, healthyArms...),
			table: healthyTable, body: healthyBody,
			want: "runs `just orphan` for the",
		},
		{
			name: "an entry whose step is gone", steps: healthySteps, arms: healthyArms,
			table: append([]notArm{{Step: "removed last year", Says: "check.py"}}, healthyTable...),
			body:  healthyBody,
			want:  `lists "removed last year", which is no step`,
		},
		{
			name: "an entry for a step an arm now spells", steps: healthySteps, arms: healthyArms,
			table: append([]notArm{{Step: "rows", Says: "check.py"}}, healthyTable...),
			body:  healthyBody,
			want:  `lists tidy step "rows", and`,
		},
		{
			name: "an entry naming an arm the recipe lacks", steps: healthySteps,
			arms:  healthyArms[:3],
			table: healthyTable, body: healthyBody,
			want: "the arm `just check-format` answers for",
		},
		{
			name: "an uncovered step the summary does not name", steps: healthySteps, arms: healthyArms,
			table: healthyTable,
			body:  []string{`    # check.py is named in a comment only`, `    echo "NOT covered: nothing"`},
			want:  "summary does not name check.py",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := compare(tc.steps, tc.arms, tc.table, tc.body)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("complaints over healthy input: %q", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], tc.want) {
				t.Fatalf("got %q, want one complaint containing %q", got, tc.want)
			}
		})
	}

	t.Run("no tidy arm at all", func(t *testing.T) {
		got := compare([]step{{Run: "just lint"}}, []arm{{"lint", "just lint"}},
			[]notArm{{Step: "just lint", Arm: "just lint"}}, nil)
		if len(got) != 1 || !strings.Contains(got[0], "has no arm under the") {
			t.Fatalf("got %q, want the one complaint that no arm is a tidy arm", got)
		}
	})
}

// TestReadersRefuseWhatTheyCannotRead holds the non-vacuity of the real-tree
// test: over zero steps or zero arms compare has nothing to complain about.
func TestReadersRefuseWhatTheyCannotRead(t *testing.T) {
	for name, workflow := range map[string]string{
		"no such job":        "jobs:\n  lint:\n    steps:\n      - run: just lint\n",
		"a job of uses only": "jobs:\n  tidy:\n    steps:\n      - uses: actions/checkout@v4\n",
		"not YAML":           "jobs: [\n",
	} {
		t.Run(name, func(t *testing.T) {
			if steps, err := readSteps([]byte(workflow), tidyJob); err == nil {
				t.Fatalf("read %d step(s) and no error", len(steps))
			}
		})
	}
	steps, err := readSteps([]byte("jobs:\n  tidy:\n    steps:\n      - uses: x\n      - name: n\n        run: |\n          a\n          b\n"), tidyJob)
	if err != nil || len(steps) != 1 || steps[0].label() != "n" || steps[0].command() != "a b" {
		t.Fatalf("got %+v, %v; want the one run step, labelled n, commanding `a b`", steps, err)
	}

	// The attributes reach compare only if the YAML reader fills them in, in
	// both spellings continue-on-error has.
	for _, allowed := range []string{"true", "${{ matrix.experimental }}"} {
		steps, err = readSteps([]byte("jobs:\n  tidy:\n    steps:\n      - run: just x\n        if: always()\n"+
			"        continue-on-error: "+allowed+"\n        working-directory: docs\n"), tidyJob)
		if err != nil || len(steps) != 1 || fmt.Sprint(steps[0].weakening()) != "[if continue-on-error working-directory]" {
			t.Fatalf("continue-on-error: %s: got %+v, %v; want all three attributes read", allowed, steps, err)
		}
	}
	arms, err := readArms([]string{
		"    run() {",
		"    # run tidy just commented-out",
		"    run tidy           just tidy-check",
		"    run_under_pinned_just tidy just check-format",
		"    run 'live-smoke[docker-free]' just test-codegen",
	})
	want := []arm{{"tidy", "just tidy-check"}, {"tidy", "just check-format"}, {"live-smoke[docker-free]", "just test-codegen"}}
	if err != nil || fmt.Sprint(arms) != fmt.Sprint(want) {
		t.Fatalf("got %v, %v; want %v", arms, err, want)
	}
	for name, body := range map[string][]string{
		"no arm":                   {"    echo hello"},
		"a runner with no command": {"    run tidy"},
	} {
		t.Run(name, func(t *testing.T) {
			if arms, err := readArms(body); err == nil {
				t.Fatalf("read %v and no error", arms)
			}
		})
	}
}
