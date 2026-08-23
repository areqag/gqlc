// A required status context that concludes SKIPPED SATISFIES branch protection
// on this repository. That is measured, not believed: PR #1323 / run
// 32622299816, 2026-08-23, forced `codegen-fence` to conclude SKIPPED with the
// other six required contexts green, and the pull request's mergeStateStatus
// read UNSTABLE — the value for "mergeable, some non-required check is red" —
// rather than BLOCKED. The gate was open with a required check that had never
// run.
//
// A job in this tree can conclude SKIPPED two ways, and this file refuses both
// on every required job:
//
//	`if:`     a job-level condition that evaluates false
//	`needs:`  a parent that fails or is cancelled — a dependent job then does
//	          not fail with it, it is SKIPPED
//
// The second is the one with history. lint, test and codegen-fence carried
// `needs: [actionlint, tidy]` until bd gqlc-yc6x, so a tidy failure printed
// `skipping` against three of the seven required contexts — a word that reads
// to a human like a pass. Removing those edges closed it, and left the rule
// held by a paragraph at the top of ci.yml and by nothing executable. This file
// is bd gqlc-zlqv, which is that paragraph made to fail.
//
// What already existed and why it was not enough:
// TestEveryLintingCIJobRestoresTheCachedBinary in golangci_cache_test.go
// refuses `if: false` on a job it reaches, and its message already names this
// hazard. But it reaches only the jobs that need the linter — `lint` and
// `codegen-fence`. `test`, `tidy`, `actionlint`, `govulncheck` and `live-smoke`
// were outside it, and NOTHING in this tree read `needs:` at all.
package ciguard_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// requiredContext names a status check that branch protection on master
// requires, and the workflow job that produces it. A context's name IS its job
// id here; GitHub derives the check name from the job id unless a `name:` key
// overrides it, and none of these do.
type requiredContext struct {
	job      string
	workflow string
}

// The required contexts, written out.
//
// Read from the live API on 2026-08-23:
//
//	gh api repos/areqag/gqlc/branches/master/protection \
//	    --jq '.required_status_checks.contexts'
//	["lint","test","tidy","actionlint","govulncheck","live-smoke","codegen-fence"]
//
// A literal rather than that call, deliberately. A test that needs a token is a
// test that is skipped — on a fork, in a sandbox, and for any contributor
// without admin scope on this repository, which is everyone but the owner. A
// skipped guard over a merge gate is the shape this whole file exists to
// refuse. The cost is that adding a required context in the GitHub UI does not
// add it here; that is a change made by one person in one place, and this
// comment is where they are told to mirror it.
var requiredContexts = []requiredContext{
	{"lint", ".github/workflows/ci.yml"},
	{"test", ".github/workflows/ci.yml"},
	{"tidy", ".github/workflows/ci.yml"},
	{"actionlint", ".github/workflows/ci.yml"},
	{"codegen-fence", ".github/workflows/ci.yml"},
	{"govulncheck", ".github/workflows/vuln.yml"},
	{"live-smoke", ".github/workflows/codegen-live.yml"},
}

// requiredJob is the two keys that can make a job conclude SKIPPED.
//
// Both are yaml.Node for the reason ciStep.If is one (bd gqlc-ff66): a string
// field cannot part an absent key from one written with no value, because both
// decode to "". Needs additionally takes two shapes in YAML — `needs: tidy` and
// `needs: [tidy]` — which no single scalar Go type accepts; against a string
// the sequence form is an unmarshal error that reddens as "decode job", naming
// the decoder rather than the bypass.
type requiredJob struct {
	If    yaml.Node `yaml:"if"`
	Needs yaml.Node `yaml:"needs"`
}

// workflowDoc parses any workflow file into its top-level mapping node.
//
// A node walk rather than a map decode, for the reason ciDoc gives: the
// top-level key is literally `on`, and which YAML schema a parser applies to
// that token has changed under this repository before.
func workflowDoc(t *testing.T, path string) *yaml.Node {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, path))
	require.NoError(t, err, "read %s", path)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(src, &root), "parse %s", path)
	require.NotEmpty(t, root.Content, "%s parsed to an empty document", path)
	return root.Content[0]
}

// loadRequiredJob decodes one required context's job out of its workflow.
//
// The job must be THERE. A required context whose job id no longer exists in
// any workflow is worse than one that skips: GitHub never reports the context
// at all, and a pull request sits at "Expected — Waiting for status to be
// reported" forever. That is a visible stall rather than a silent pass, but it
// is still this list going stale, and it is the failure this lookup names.
func loadRequiredJob(t *testing.T, rc requiredContext) requiredJob {
	t.Helper()
	jobs := childByKey(workflowDoc(t, rc.workflow), "jobs")
	require.NotNilf(t, jobs, "%s has no `jobs:` block", rc.workflow)

	node := childByKey(jobs, rc.job)
	require.NotNilf(t, node, "%s has no %q job, but %q is a required status check on "+
		"master. Either the job was renamed — in which case branch protection is now "+
		"waiting for a context nothing reports, and every pull request hangs — or this "+
		"list is stale (bd gqlc-zlqv).", rc.workflow, rc.job, rc.job)

	var job requiredJob
	require.NoErrorf(t, node.Decode(&job), "decode job %q in %s", rc.job, rc.workflow)
	return job
}

// requireNoRequiredJobIf refuses a job-level `if:` on a required job.
//
// Refused on being WRITTEN, not on evaluating false. A test cannot evaluate
// `${{ … }}`, so "is this condition truthy on the run that matters" is not a
// question it can answer; "is the key there" is, and it has no undecidable
// case. The cost is that a genuinely-wanted condition on a required job is
// refused too — which is correct, because that is exactly the change that would
// need to be argued rather than merged.
//
// It takes require.TestingT so that it can be MEASURED against a job that
// carries one. Over the tree it only ever runs the accepting side.
func requireNoRequiredJobIf(t require.TestingT, rc requiredContext, job requiredJob) {
	require.Falsef(t, present(job.If),
		"job %q in %s carries a job-level `if:`, written as %s. It is a required status "+
			"context: a job that does not run still emits a check run, its conclusion is "+
			"`skipped`, and a skipped required context SATISFIES branch protection on this "+
			"repository — measured on PR #1323 / run 32622299816, where a forced-skipped "+
			"codegen-fence left the pull request UNSTABLE and not BLOCKED. So an `if:` here "+
			"retires the gate without deleting a line of it (bd gqlc-zlqv).",
		rc.job, rc.workflow, spell(job.If))
}

// spellNeeds renders a `needs:` for a failure message.
//
// spell() alone is not enough for this key: a sequence node carries no Value,
// so it renders as the bare tag `!!seq` and the message names neither the
// parents nor how many there are. The list form is the one this rule is about —
// `needs: [actionlint, tidy]` is the shape gqlc-yc6x removed — so a reader who
// is told only `!!seq` has to go and open the file to learn what was refused.
func spellNeeds(n yaml.Node) string {
	if n.Kind != yaml.SequenceNode {
		return spell(n)
	}
	parents := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		parents = append(parents, c.Value)
	}
	return "[" + strings.Join(parents, ", ") + "]"
}

// requireNoRequiredJobNeeds refuses a `needs:` on a required job.
//
// This is the arm nothing in the tree had. A `needs:` is not a failure that
// propagates — a job whose parent fails is SKIPPED, and skipped satisfies the
// gate. The saving it buys is real and small: with the edges in place a green
// pull request waited ~30s for actionlint and tidy before the slow jobs
// started, and a red one stopped early. The price was three of seven required
// contexts satisfiable by a job that did nothing.
func requireNoRequiredJobNeeds(t require.TestingT, rc requiredContext, job requiredJob) {
	require.Falsef(t, present(job.Needs),
		"job %q in %s carries `needs: %s`. It is a required status context, and a job "+
			"whose `needs:` parent fails or is cancelled does not fail with it — it "+
			"concludes SKIPPED, which satisfies branch protection here (PR #1323 / run "+
			"32622299816). The check table then prints `skipping` against this context, "+
			"which reads to a human like a pass. lint, test and codegen-fence carried "+
			"`needs: [actionlint, tidy]` until bd gqlc-yc6x for the runner minutes; do not "+
			"buy them back (bd gqlc-zlqv).",
		rc.job, rc.workflow, spellNeeds(job.Needs))
}

// Every required job, over the real workflows.
func TestNoRequiredJobCanConcludeSkipped(t *testing.T) {
	for _, rc := range requiredContexts {
		t.Run(rc.job, func(t *testing.T) {
			job := loadRequiredJob(t, rc)
			requireNoRequiredJobIf(t, rc, job)
			requireNoRequiredJobNeeds(t, rc, job)
		})
	}
}

// The list must not be able to go silently empty, and must not name one job
// twice — either would make the loop above pass over less than it claims.
func TestTheRequiredContextListIsWhatItSaysItIs(t *testing.T) {
	require.Lenf(t, requiredContexts, 7,
		"master required 7 status contexts when this was written, and the list holds %d. "+
			"If branch protection changed, change the count with it and say so; if the "+
			"list lost an entry, the job it named is no longer guarded and nothing else "+
			"here would have noticed (bd gqlc-zlqv).", len(requiredContexts))

	seen := map[string]string{}
	for _, rc := range requiredContexts {
		prev, dup := seen[rc.job]
		require.Falsef(t, dup, "context %q is listed twice, against %s and %s",
			rc.job, prev, rc.workflow)
		seen[rc.job] = rc.workflow
	}
}

// The two refusals have to actually refuse. Over the tree they only ever run
// their accepting side, and a guard whose passing case is silence is one that
// nothing distinguishes from a deleted one — so each shape that makes a job
// conclude SKIPPED is decoded here and put to the assertion that is supposed to
// stop it.
//
// The rows are documents taken, not a survey of YAML.
func TestBothSkipRoutesAreRefusedOnBeingWritten(t *testing.T) {
	// Any entry; the assertions read the job, and the context only names it in
	// the message.
	rc := requiredContexts[0]

	for _, row := range []struct {
		name        string
		src         string
		refusedIf   bool
		refusedNeed bool
	}{
		{"neither key", "runs-on: ubuntu-latest\n", false, false},
		{"if: with nothing after it", "if:\n", true, false},
		{"if: with an empty string", "if: \"\"\n", true, false},
		{"if: false", "if: false\n", true, false},
		{"if: a condition", "if: github.event_name == 'pull_request'\n", true, false},
		{"needs: a scalar parent", "needs: tidy\n", false, true},
		{"needs: a one-element list", "needs: [tidy]\n", false, true},
		{"needs: the edges gqlc-yc6x removed", "needs: [actionlint, tidy]\n", false, true},
		{"needs: an empty list", "needs: []\n", false, true},
		{"both at once", "if: false\nneeds: [tidy]\n", true, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			var job requiredJob
			require.NoErrorf(t, yaml.Unmarshal([]byte(row.src), &job), "decode %q", row.src)

			ifRefusal := refusalOf(t, func(rt require.TestingT) {
				requireNoRequiredJobIf(rt, rc, job)
			})
			needsRefusal := refusalOf(t, func(rt require.TestingT) {
				requireNoRequiredJobNeeds(rt, rc, job)
			})

			if row.refusedIf {
				require.NotEmptyf(t, ifRefusal,
					"%q writes an `if:` on a required job and it was accepted, so the "+
						"context can be retired by a key this guard reads as absent", row.src)
				require.Containsf(t, ifRefusal, rc.job,
					"the `if:` refusal does not name the job it is about: %s", ifRefusal)
			} else {
				require.Emptyf(t, ifRefusal, "%q writes no `if:` and was refused anyway: %s",
					row.src, ifRefusal)
			}

			if row.refusedNeed {
				require.NotEmptyf(t, needsRefusal,
					"%q writes a `needs:` on a required job and it was accepted. A parent "+
						"that fails skips this job, and skipped satisfies the gate", row.src)
				require.Containsf(t, needsRefusal, rc.job,
					"the `needs:` refusal does not name the job it is about: %s", needsRefusal)
			} else {
				require.Emptyf(t, needsRefusal,
					"%q writes no `needs:` and was refused anyway: %s", row.src, needsRefusal)
			}
		})
	}
}

// ci.yml's header is where this rule is argued, and the argument is load-
// bearing prose: it is what stops the next reader restoring the `needs:` edges
// to save the runner minutes back. A guard with no reachable explanation gets
// deleted as an obstacle.
//
// Read as a substring of the file's own bytes, which is the right read here —
// the claim is about what a human opening ci.yml sees, not about what YAML
// decodes to.
func TestCIHeaderStillArguesWhyTheEdgesAreGone(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, ciWorkflow))
	require.NoError(t, err, "read %s", ciWorkflow)

	require.Containsf(t, string(src), "Do not restore the",
		"%s no longer tells a reader not to restore the `needs:` edges. The guard in "+
			"this file refuses them, but a refusal with no argument beside it is an "+
			"obstacle, and obstacles get removed (bd gqlc-zlqv).", ciWorkflow)
}
