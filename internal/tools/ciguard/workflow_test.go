// Package ciguard's assertions live in an external test package. They import
// testify and yaml.v3, and govulncheck discards a package's in-package test
// variant together with everything only it imports — so an in-package test
// here would take the whole package out of the vulnerability scan's call graph
// (bd gqlc-m5rc, enforced by `just vuln-root-residual`). Nothing below needs
// unexported state.
package ciguard_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// repoRoot reaches the workflow files from this package's directory.
const repoRoot = "../../.."

const ciWorkflow = ".github/workflows/ci.yml"

// The job that carries the PR-body gate. It is named, not derived, because
// the name is the thing being pinned: `tidy` is a required status check on
// master, and moving the step into a job that is not one detaches the gate
// from merge without changing a single line of the step (bd gqlc-w4al).
const gateJob = "tidy"

// What the gate step runs. Matched as a substring of the step's `run`.
const gateScript = ".github/scripts/check-pr-closes.py"

// The frozen read the gate used to do. `github.event.pull_request.body` is
// the body as it was when the event fired, which is not the body a reviewer
// or GitHub sees at merge time (bd gqlc-w4al).
const payloadBody = "github.event.pull_request.body"

// ContinueOnError is a yaml.Node rather than a bool, the same modelling as
// actionStep in golangci_cache_test.go and for the same reason: the key takes
// values this decoder cannot give one Go type. `continue-on-error: true` is a
// bool and `continue-on-error: ${{ github.event_name == 'push' }}` is a string,
// so against a bool field the expression form is an unmarshal error — it does
// redden, but as "decode job", naming the decoder rather than the bypass.
// Measured out of tree over absent/true/false/expression/quoted: into a bool
// the expression and quoted forms error and leave the field false; into a Node
// every written form arrives with a non-zero Kind. present and spell read it.
//
// If is a Node for a different reason, and the reason is bd gqlc-ff66. A string
// field is told nothing about whether the key was written: `if:` written with
// no value decodes to the same "" an absent `if:` does, so `require.Empty` over
// a string passes the written form. Kind is what parts them. Both `if:` fields
// here carry the Node so that the distinction stays available to whatever
// asserts on them next — the step's is compared to a condition below, where
// absence and an empty value are refused alike, and the job's is refused on
// having been written at all.
type ciSteps []struct {
	Name            string            `yaml:"name"`
	Run             string            `yaml:"run"`
	Shell           string            `yaml:"shell"`
	If              yaml.Node         `yaml:"if"`
	ContinueOnError yaml.Node         `yaml:"continue-on-error"`
	Env             map[string]string `yaml:"env"`
}

type ciDefaults struct {
	Run struct {
		Shell string `yaml:"shell"`
	} `yaml:"run"`
}

type ciJob struct {
	Permissions     map[string]string `yaml:"permissions"`
	If              yaml.Node         `yaml:"if"`
	ContinueOnError yaml.Node         `yaml:"continue-on-error"`
	Defaults        ciDefaults        `yaml:"defaults"`
	Steps           ciSteps           `yaml:"steps"`
}

// The `if:` the gate step is allowed to carry, and the only one. ci.yml's own
// header explains why a job-level `if:` is a bypass — a skipped job still
// emits a check run, its conclusion is `skipped`, branch protection reads
// that as a pass, and the newest run for a context wins — but nothing
// asserted it, so `if: false` on a required job passed every check here.
// Written out rather than matched loosely: narrowing the condition is how
// this would be disabled without deleting anything, and a narrowing is a
// different string.
const gateStepIf = "github.event_name == 'pull_request'"

// ciDoc parses ci.yml into its top-level mapping node.
//
// A node walk rather than a map[string]any decode, because the top-level key
// is literally `on`, and which YAML schema a parser applies to that token has
// changed under this repo before. Node.Value is the source token, so the
// lookup does not depend on the answer.
//
// ci.yml is the only workflow with assertions here, and unparam refuses a
// parameter every caller passes the same constant to.
func ciDoc(t *testing.T) *yaml.Node {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, ciWorkflow))
	require.NoError(t, err, "read %s", ciWorkflow)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(src, &root), "parse %s", ciWorkflow)
	require.NotEmpty(t, root.Content, "%s parsed to an empty document", ciWorkflow)
	return root.Content[0]
}

// childByKey returns the value node under key, or nil.
func childByKey(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// A body edit has to fire the workflow at all. The default activity set for
// `pull_request` is [opened, synchronize, reopened]; a PR body is not part of
// any of them, so before this list existed the gate could pass at open and
// then have its `Closes #N` line edited out with nothing re-evaluating it.
//
// The three defaults are pinned alongside `edited` because writing a `types:`
// list replaces the default set rather than extending it: adding `edited`
// while dropping `synchronize` would silence CI on every push.
func TestCIRunsOnPullRequestBodyEdits(t *testing.T) {
	on := childByKey(ciDoc(t), "on")
	require.NotNil(t, on, "%s has no `on:` block", ciWorkflow)

	pr := childByKey(on, "pull_request")
	require.NotNil(t, pr, "%s does not trigger on pull_request at all", ciWorkflow)

	var trigger struct {
		Types []string `yaml:"types"`
	}
	require.NoError(t, pr.Decode(&trigger), "decode on.pull_request")

	for _, want := range []string{"opened", "synchronize", "reopened", "edited"} {
		require.Containsf(t, trigger.Types, want,
			"on.pull_request.types in %s does not list %q (it lists %v). Writing any "+
				"types: list replaces GitHub's default set, so every activity this "+
				"workflow must see has to be named. %q is the one that carries the "+
				"PR-body gate: without it a PR passes the gate at open and can then "+
				"have its Closes line edited out with nothing re-running (bd gqlc-w4al).",
			ciWorkflow, want, trigger.Types, "edited")
	}
}

// gateStep locates the PR-body check inside the required job.
func gateStep(t *testing.T) (ciJob, int) {
	t.Helper()
	jobs := childByKey(ciDoc(t), "jobs")
	require.NotNil(t, jobs, "%s has no jobs", ciWorkflow)

	node := childByKey(jobs, gateJob)
	require.NotNilf(t, node, "%s has no %q job. That job id is a required status "+
		"check on master, so the PR-body gate has to live in it to block a merge.",
		ciWorkflow, gateJob)

	var job ciJob
	require.NoError(t, node.Decode(&job), "decode job %q", gateJob)

	for i, s := range job.Steps {
		if strings.Contains(s.Run, gateScript) {
			return job, i
		}
	}
	t.Fatalf("no step in job %q of %s runs %s, so nothing is checking that a PR "+
		"body carries its Closes #N (bd gqlc-nyo, bd gqlc-w4al)",
		gateJob, ciWorkflow, gateScript)
	return job, -1
}

// The step must exist, in the required job, and must actually invoke a
// checker that is on disk. A `run:` naming a script that was moved or
// deleted is a step that fails loudly, which is fine; a job id that is not a
// required context is a step that fails silently, which is not.
func TestPRBodyGateRunsInARequiredJob(t *testing.T) {
	job, i := gateStep(t)
	require.NotEmpty(t, job.Steps[i].Name, "the PR-body gate step is unnamed, so its "+
		"failure in a CI log cannot be told from any other step in %q", gateJob)

	_, err := os.Stat(filepath.Join(repoRoot, gateScript))
	require.NoError(t, err, "job %q runs %s, which is not in the tree", gateJob, gateScript)
}

// The gate must read the live body, not the event payload.
//
// This is the half that survives a re-run. `edited` gets a run started when
// the body changes; it does not stop anyone re-running an older run, and the
// newest check run for a context is the one branch protection reads. With a
// payload read, re-running the run that opened the PR replays the body the PR
// had at open — so strip the Closes line, re-run the opening run, and a fresh
// green verdict lands over a body that no longer carries it.
//
// Asserted over the decoded step rather than the file's text: this workflow's
// comments name the expression they are warning about, and a byte-level
// search would read its own warning as the defect.
func TestPRBodyGateDoesNotReadTheFrozenPayloadBody(t *testing.T) {
	job, _ := gateStep(t)
	for _, s := range job.Steps {
		for k, v := range s.Env {
			require.NotContainsf(t, v, payloadBody,
				"step %q sets %s from %s. That is the body as of the event that "+
					"started the run, so re-running an older run re-asserts a body "+
					"the PR may no longer have. Fetch it over the API instead "+
					"(bd gqlc-w4al).", s.Name, k, payloadBody)
		}
		require.NotContainsf(t, s.Run, payloadBody,
			"step %q interpolates %s into its shell", s.Name, payloadBody)
	}
}

// The gate refuses by exiting non-zero, so anything that decouples the step's
// status from the job's turns it into a detector that reports and does not
// block. Two things do: continue-on-error, and a shell without `-e`.
//
// The shell matters here more than it usually would. The step fetches the body
// and then checks it as two commands; the runner's default `bash -e {0}` is
// what stops the second running over an empty file when the first fails.
// Measured on this step: with `bash -e` a failed fetch exits 1; under plain
// `bash` the same failure exits 0 whenever the branch name carries no bead id,
// because an empty body then reads as "this PR is about no bead". A
// `defaults: run: shell: bash` anywhere above the step would spell that
// difference in a file the step does not mention.
func TestPRBodyGateFailsTheJobItRunsIn(t *testing.T) {
	doc := ciDoc(t)
	job, i := gateStep(t)
	step := job.Steps[i]

	// Refused written, not refused true. A test cannot evaluate `${{ … }}`, so
	// "is the value truthy" is not a question this can answer at all; "is the
	// key there" is. It costs a harmless `continue-on-error: false` its place
	// and buys a rule with no undecidable case in it.
	require.Falsef(t, present(step.ContinueOnError),
		"the PR-body gate sets `continue-on-error: %s`, so a refusal no longer fails %q "+
			"and the check that reddens the merge is reporting into a log nobody reads",
		spell(step.ContinueOnError), gateJob)
	require.Falsef(t, present(job.ContinueOnError),
		"job %q sets `continue-on-error: %s`, so no step in it can fail the merge",
		gateJob, spell(job.ContinueOnError))

	requireNoJobIf(t, job)
	require.Equalf(t, gateStepIf, step.If.Value,
		"the PR-body gate step's `if:` is %q, not %q. The step is skipped on anything "+
			"that condition excludes and the job still passes, so narrowing it is how "+
			"this gate gets turned off quietly.", spell(step.If), gateStepIf)

	require.Empty(t, step.Shell,
		"the PR-body gate overrides its shell to %q. The runner's default is "+
			"`bash -e {0}`, and `-e` is what makes a failed body fetch abort the step "+
			"instead of handing the checker an empty file (bd gqlc-w4al).", step.Shell)
	require.Empty(t, job.Defaults.Run.Shell,
		"job %q sets defaults.run.shell to %q, which silently rewrites the PR-body "+
			"gate's shell", gateJob, job.Defaults.Run.Shell)

	if d := childByKey(doc, "defaults"); d != nil {
		var wf ciDefaults
		require.NoError(t, d.Decode(&wf), "decode workflow-level defaults")
		require.Empty(t, wf.Run.Shell,
			"%s sets a workflow-level defaults.run.shell of %q, which rewrites the "+
				"PR-body gate's shell from a block that does not mention it",
			ciWorkflow, wf.Run.Shell)
	}
}

// requireNoJobIf refuses a job-level `if:` on the gate job.
//
// It takes require.TestingT rather than *testing.T so that it can be run
// against a job that carries one. ci.yml does not, so calling it over the file
// exercises only the side that passes.
func requireNoJobIf(t require.TestingT, job ciJob) {
	require.Falsef(t, present(job.If),
		"job %q carries a job-level `if:` (%s). A job that does not run still emits a "+
			"check run, with conclusion `skipped`, which branch protection reads as a "+
			"pass — so an `if:` here retires the gate without deleting a line of it. "+
			"Refused on being written, for the same reason continue-on-error above is: "+
			"a test cannot evaluate the condition, and it does not have to.",
		gateJob, spell(job.If))
}

// recordingT collects what a require reported instead of failing the test, so
// that requireNoJobIf can be measured rather than only run. Same shape as the
// recorder in internal/schema/gql: FailNow must not return, and it panics
// rather than calling runtime.Goexit because Goexit needs a goroutine of its
// own, where an assertion is indistinguishable — to a reader and to testifylint
// — from reporting a failure to a test that has already finished.
type recordingT struct{ msgs []string }

// failedNow is the panic value, distinct so that a panic from anywhere else is
// re-raised rather than read as a refusal.
type failedNow struct{}

func (r *recordingT) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recordingT) FailNow() { panic(failedNow{}) }

func (r *recordingT) Helper() {}

// jobIfRefusal is what requireNoJobIf reported about job, and "" when it
// accepted it.
func jobIfRefusal(t *testing.T, job ciJob) string {
	t.Helper()
	rec := &recordingT{}
	func() {
		defer func() {
			r := recover()
			if _, ok := r.(failedNow); r != nil && !ok {
				panic(r)
			}
		}()
		requireNoJobIf(rec, job)
	}()
	return strings.Join(rec.msgs, "\n")
}

// The refusal has to reach an `if:` written with no value, and that is the
// thing a string field cannot be asked. Decoded into a string, a written-empty
// `if:` and an absent one are both "", so `require.Empty` over one passes the
// written form — which is what this test did until bd gqlc-ff66. Kind is what
// parts them, and `value` below is what a string field would have seen: the
// rows that share a value and differ in `refused` are that conflation.
//
// This also holds present() to the distinction it is named for. Every key
// present() is asked about elsewhere in this package is absent in the tree
// today, so a present() rewritten to read Value answers those the same way and
// says nothing; here it would answer differently.
//
// The rows are documents taken, not a survey of YAML. There are forms that
// decode to an empty value which are not written below, and nothing here claims
// a verdict for them: what is asserted is the verdict beside each source.
func TestGateJobIfIsRefusedOnBeingWrittenNotOnItsValue(t *testing.T) {
	for _, row := range []struct {
		name    string
		src     string
		refused bool
		value   string
	}{
		{"no if: key at all", "permissions:\n  contents: read\n", false, ""},
		{"if: with nothing after it", "if:\n", true, ""},
		{"if: with an empty string", "if: \"\"\n", true, ""},
		{"if: carrying a condition", "if: " + gateStepIf + "\n", true, gateStepIf},
	} {
		t.Run(row.name, func(t *testing.T) {
			var job ciJob
			require.NoErrorf(t, yaml.Unmarshal([]byte(row.src), &job), "decode %q", row.src)
			require.Equalf(t, row.value, job.If.Value,
				"this row's premise is that a string `If` would have read %q from %q",
				row.value, row.src)

			refusal := jobIfRefusal(t, job)
			if !row.refused {
				require.Emptyf(t, refusal, "%q writes no `if:` and was refused anyway", row.src)
				return
			}
			require.NotEmptyf(t, refusal,
				"%q writes an `if:` on job %q and it was accepted, so the gate can be "+
					"retired by a key this test reads as absent", row.src, gateJob)
			require.Containsf(t, refusal, gateJob,
				"the refusal does not name the job it is about: %s", refusal)
		})
	}
}

// Reading the body over the API needs the scope for it, and a job-level
// permissions block replaces the workflow-level one outright rather than
// merging with it — so contents: read has to be restated beside it or
// actions/checkout loses its token.
func TestPRBodyGateJobCanReadPullRequests(t *testing.T) {
	job, _ := gateStep(t)
	require.Equal(t, "read", job.Permissions["pull-requests"],
		"job %q does not grant pull-requests: read, so the API fetch of the PR "+
			"body has no scope. The fetch fails closed, which reddens every PR "+
			"rather than passing them — but it reddens them for a reason no log "+
			"line explains.", gateJob)
	require.Equal(t, "read", job.Permissions["contents"],
		"job %q sets job-level permissions without restating contents: read. A "+
			"job-level block replaces the workflow-level one, it does not extend "+
			"it, so actions/checkout is left without a token.", gateJob)
}

// runGateStep executes the gate step's own shell exactly as the runner would:
// `bash -e <file>`, with a stub `gh` first on PATH.
//
// It runs the shell rather than reading it because the property under test is
// what the step does when the fetch fails, and every spelling that would break
// that — `|| true`, `|| :`, a `set +e`, piping the fetch into anything — is a
// different string and the same defect. Asserting on the text would pin one of
// them; asserting on the exit status pins the behaviour. It also cannot be
// satisfied by a comment: what runs is the decoded scalar.
//
// The working directory is a fabricated checkout carrying the real checker and
// a fixture export, not the repository itself. The step's paths are relative,
// so pointing it at a fixture is enough — and a test that read the tree's own
// .beads/issues.jsonl would assert against a file bd rewrites, i.e. it would
// start failing the day a bead it names is archived.
//
// ghStub is the body of the fake `gh`. status is what the step exited with.
func runGateStep(t *testing.T, ghStub, branch string) (status int, output string) {
	t.Helper()
	job, i := gateStep(t)

	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(ghStub), 0o755))

	root := filepath.Join(dir, "checkout")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".beads"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "scripts"), 0o755))
	checker, err := os.ReadFile(filepath.Join(repoRoot, gateScript))
	require.NoError(t, err, "read %s", gateScript)
	require.NoError(t, os.WriteFile(filepath.Join(root, gateScript), checker, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".beads", "issues.jsonl"),
		[]byte(fixtureExport), 0o600))

	script := filepath.Join(dir, "step.sh")
	require.NoError(t, os.WriteFile(script, []byte(job.Steps[i].Run), 0o600))

	cmd := exec.CommandContext(t.Context(), "bash", "-e", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RUNNER_TEMP="+dir,
		"GITHUB_REPOSITORY=areqag/gqlc",
		"PR_NUMBER=1",
		"BRANCH="+branch,
	)
	out, runErr := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case runErr == nil:
		return 0, string(out)
	case errors.As(runErr, &ee):
		return ee.ExitCode(), string(out)
	default:
		t.Fatalf("could not run the gate step's shell: %v\n%s", runErr, out)
		return -1, ""
	}
}

// One bead with a GitHub mirror, which is all these cases need.
const fixtureExport = `{"id":"gqlc-fixture","issue_type":"bug",` +
	`"external_ref":"https://github.com/areqag/gqlc/issues/4242"}
`

const ghFails = `#!/usr/bin/env bash
echo "gh: HTTP 403: Resource not accessible by integration" >&2
exit 1
`

// A body fetch that does not land must fail the step.
//
// This is the case the checker cannot cover on its own: the two things it
// refuses are an unreadable body file and an unreadable export, and a failed
// fetch produces neither — the shell redirect leaves an empty file behind, and
// an empty body is a body. What it means is "this PR mentions no bead", which
// is a pass. The branch here is deliberately one with no bead id in it, so
// nothing downstream can rescue the verdict: if the step does not stop at the
// fetch, the PR goes green with its body never read.
func TestGateStepRefusesWhenTheBodyFetchFails(t *testing.T) {
	status, out := runGateStep(t, ghFails, "fix/a-branch-naming-no-bead")
	require.NotZero(t, status,
		"the gate step exited 0 after its body fetch failed. It reads the fetched "+
			"file next, and an empty body carries no bead and no Closes, which is a "+
			"pass — so this is a PR merging with its body never read (bd gqlc-w4al). "+
			"Output:\n%s", out)
}

// ...and the same failure must still be fatal when the branch does name a
// bead, where the checker would otherwise refuse for the wrong reason and
// report a missing Closes line on a body it never saw.
func TestGateStepRefusesAFailedFetchBeforeReadingTheBody(t *testing.T) {
	status, out := runGateStep(t, ghFails, "fix/gqlc-fixture-thing")
	require.NotZero(t, status, "the gate step exited 0 after a failed fetch:\n%s", out)
	require.NotContains(t, out, "missing 'Closes",
		"the gate step ran the checker over the empty file the failed fetch left "+
			"behind, so it is diagnosing a body it never read:\n%s", out)
}

// The happy path, so the two refusals above are not both just "always red".
func TestGateStepPassesOnABodyThatClosesItsBead(t *testing.T) {
	stub := `#!/usr/bin/env bash
printf 'Bead: gqlc-fixture\n\nCloses #4242\n'
`
	status, out := runGateStep(t, stub, "fix/a-branch-naming-no-bead")
	require.Zero(t, status, "the gate step refused a body that closes its bead:\n%s", out)
}

// ...and a body that does not close it is refused, through the real shell
// rather than through the checker called directly.
func TestGateStepRefusesABodyMissingItsCloses(t *testing.T) {
	stub := `#!/usr/bin/env bash
printf 'Bead: gqlc-fixture\n\nno closing keyword here\n'
`
	status, out := runGateStep(t, stub, "fix/a-branch-naming-no-bead")
	require.NotZero(t, status, "the gate step passed a body with no Closes line:\n%s", out)
	require.Contains(t, out, "missing 'Closes #4242'",
		"the step refused without naming the issue it wanted:\n%s", out)
}
