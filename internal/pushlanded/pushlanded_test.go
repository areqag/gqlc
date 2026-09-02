package pushlanded_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run the shipped `push-landed` recipe over a throwaway
// repository, with `gh` stubbed on PATH, and ask what it CONCLUDES. The
// question is not whether the recipe consults GitHub but whether an absent
// remote head branch is still reported as a failed push, which is the state a
// squash merge leaves behind and the state a citizen is in at session close
// (bd gqlc-97rxk).
//
// WHAT THE STUB IS MODELLED ON. The three payloads below are the real
// `gh pr list --state all --json number,state,mergeCommit` bodies, measured
// against this repository on 2026-09-02:
//
//	merged   [{"mergeCommit":{"oid":"72d104ff…"},"number":2287,"state":"MERGED"}]
//	closed   [{"mergeCommit":null,"number":2217,"state":"CLOSED"}]
//	absent   []
//
// The stub does not emit a canned rendering of them: it takes the `--jq`
// expression out of the argv the recipe actually passed and evaluates it with
// jq over the payload. So the recipe's own expression is what produces every
// string asserted here, and rewriting it wrongly reddens these rows rather
// than leaving a stub that agrees with the old spelling.
//
// WHAT THAT DOES NOT REACH: whether GitHub still answers in that shape.
// TestTheRealGhDegradesOnARepositoryItCannotResolve runs the real binary, but
// only far enough to witness the degrade path — nothing here calls a live
// GitHub API, and a change to gh's JSON field names would pass this file.

const (
	// repoRoot reaches the justfile from this package's directory.
	repoRoot = "../.."

	// recipe is the recipe under test.
	recipe = "push-landed"

	// probeBranch is the branch every row asks about. Slashed, like the branch
	// names this repository's own recipe requires.
	probeBranch = "fix/gqlc-97rxk-probe"

	// failedPushClaim is the sentence that must be reserved for a push that
	// really did fail. Its appearance over a merged branch is the defect.
	failedPushClaim = "The push did fail"

	mergedPayload = `[{"mergeCommit":{"oid":"72d104ff908c8a6fb8d7285cbe53994e0946bec6"},"number":2287,"state":"MERGED"}]`
	mergedNumber  = "2287"
	mergedOID     = "72d104ff908c8a6fb8d7285cbe53994e0946bec6"

	closedPayload = `[{"mergeCommit":null,"number":2217,"state":"CLOSED"}]`
	closedNumber  = "2217"

	emptyPayload = `[]`
)

// world is one throwaway repository, the bare origin behind it, and the PATH
// the recipe will see.
type world struct {
	work   string // the working tree the recipe runs in
	origin string // the bare repository `origin` points at
	bin    string // prepended to PATH; holds the gh stub when there is one
	calls  string // every stub invocation's argv, one per line
	sha    string // the commit probeBranch points at locally
}

// run is what `just push-landed` did.
type run struct {
	status int
	output string
}

func (r run) says(want string) bool { return strings.Contains(r.output, want) }

// shellQuote wraps s for a single-quoted bash literal.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// hermeticEnv is the environment every child gets: the caller's, minus every
// variable that could redirect git or point gh at a real repository, plus
// extra.
//
// GIT_DIR is the reason for the git half — a hook-launched `go test` exports
// it, and every git command below would then run against THIS repository
// rather than the throwaway one. GH_* and GITHUB_* are dropped so that the
// real-gh row cannot be handed a repository to resolve by a CI runner's
// environment.
func hermeticEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch {
		case key == "PATH",
			strings.HasPrefix(key, "GIT_"),
			strings.HasPrefix(key, "GH_"),
			strings.HasPrefix(key, "GITHUB_"):
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = hermeticEnv(
		"PATH="+os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=probe", "GIT_AUTHOR_EMAIL=probe@invalid",
		"GIT_COMMITTER_NAME=probe", "GIT_COMMITTER_EMAIL=probe@invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newWorld lays out a repository holding one commit on probeBranch, with a
// bare origin that does NOT have the branch. That is the state under test;
// rows wanting the branch on origin push it themselves.
func newWorld(t *testing.T) *world {
	t.Helper()
	base := t.TempDir()
	w := &world{
		work:   filepath.Join(base, "work"),
		origin: filepath.Join(base, "origin.git"),
		bin:    filepath.Join(base, "bin"),
		calls:  filepath.Join(base, "gh-calls"),
	}
	for _, dir := range []string{w.work, w.bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	git(t, base, "init", "--quiet", "--bare", w.origin)
	git(t, w.work, "init", "--quiet", "--initial-branch", "master", ".")
	if err := os.WriteFile(filepath.Join(w.work, "file"), []byte("one\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	git(t, w.work, "add", "file")
	git(t, w.work, "commit", "--quiet", "--no-verify", "-m", "one")
	git(t, w.work, "checkout", "--quiet", "-b", probeBranch)
	git(t, w.work, "remote", "add", "origin", w.origin)
	w.sha = git(t, w.work, "rev-parse", "--verify", probeBranch)
	return w
}

// stubGh writes a `gh` onto the world's PATH. It records its argv, then either
// fails with failure on stderr, or evaluates the recipe's own --jq expression
// over payload.
func (w *world) stubGh(t *testing.T, payload, failure string) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatalf("`jq` is not on PATH, and the gh stub evaluates the recipe's own "+
			"--jq expression rather than emitting a canned rendering of it, so this "+
			"is a broken environment and not a case to skip past: %v", err)
	}
	script := strings.NewReplacer(
		"@CALLS@", shellQuote(w.calls),
		"@FAILURE@", shellQuote(failure),
		"@PAYLOAD@", shellQuote(payload),
	).Replace(`#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >> @CALLS@
failure=@FAILURE@
if [ -n "$failure" ]; then
    printf '%s\n' "$failure" >&2
    exit 1
fi
expr=""
while [ "$#" -gt 0 ]; do
    if [ "$1" = "--jq" ]; then
        expr="${2:-}"
        break
    fi
    shift
done
if [ -z "$expr" ]; then
    printf 'stub gh: argv carries no --jq expression\n' >&2
    exit 90
fi
printf '%s' @PAYLOAD@ | jq -r "$expr"
`)
	path := filepath.Join(w.bin, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write gh stub: %v", err)
	}
}

// ghArgv is every argv the stub was handed, one per line.
func (w *world) ghArgv(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(w.calls)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatalf("read the gh stub's call log: %v", err)
	}
	return string(body)
}

// run executes the shipped recipe against this world. pathTail is what follows
// the world's own bin directory on PATH: the caller's PATH for the rows that
// want the surrounding toolchain, or a directory holding only git for the row
// that wants gh genuinely absent.
func (w *world) run(t *testing.T, pathTail string) run {
	t.Helper()
	justBin, err := exec.LookPath("just")
	if err != nil {
		t.Fatalf("`just` is not on PATH, and this test runs the %q recipe rather than "+
			"reading it. CI runs these tests through `just`, so this is a broken "+
			"environment and not a case to skip past: %v", recipe, err)
	}
	// Absolute, because --working-directory is where just resolves a relative
	// --justfile from: a relative path here reads as missing from inside the
	// throwaway tree, and every row below then fails for that instead of for
	// what it asks.
	justfilePath, err := filepath.Abs(filepath.Join(repoRoot, "justfile"))
	if err != nil {
		t.Fatalf("resolve the justfile: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), justBin,
		"--justfile", justfilePath,
		"--working-directory", w.work,
		recipe, probeBranch)
	cmd.Dir = w.work
	cmd.Env = hermeticEnv(
		"PATH="+w.bin+string(os.PathListSeparator)+pathTail,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, runErr := cmd.CombinedOutput()

	got := run{output: string(out)}
	var exit *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exit):
		got.status = exit.ExitCode()
	default:
		t.Fatalf("could not run %q: %v\n%s", recipe, runErr, out)
	}
	return got
}

// TestAMergedPRIsNotAFailedPush is the defect. Three branches in this state on
// 2026-09-02 were each told their push had failed and to retry it.
func TestAMergedPRIsNotAFailedPush(t *testing.T) {
	w := newWorld(t)
	w.stubGh(t, mergedPayload, "")

	got := w.run(t, os.Getenv("PATH"))

	if got.status != 0 {
		t.Errorf("exit %d over a merged PR; the work is on master and the recipe is "+
			"the last thing asked before a session is abandoned:\n%s", got.status, got.output)
	}
	if got.says(failedPushClaim) {
		t.Errorf("the report claims %q over a branch whose PR merged:\n%s",
			failedPushClaim, got.output)
	}
	// The number and the merge commit are what make the report checkable by
	// the reader; a verdict alone cannot be taken to the PR page or to
	// `git show`.
	for _, want := range []string{mergedNumber, mergedOID} {
		if !got.says(want) {
			t.Errorf("the report names no %s:\n%s", want, got.output)
		}
	}
}

// TestAPRThatNeverMergedIsStillNotAFailedPush holds the other half of the same
// evidence. A PR cannot exist from a head that never reached origin, so the
// push did not fail — but nothing is on master either, so this is not a pass.
func TestAPRThatNeverMergedIsStillNotAFailedPush(t *testing.T) {
	w := newWorld(t)
	w.stubGh(t, closedPayload, "")

	got := w.run(t, os.Getenv("PATH"))

	if got.status == 0 {
		t.Errorf("exit 0 over a PR that closed without merging; nothing of this "+
			"branch is on master:\n%s", got.output)
	}
	if got.says(failedPushClaim) {
		t.Errorf("the report claims %q over a head GitHub has a PR for:\n%s",
			failedPushClaim, got.output)
	}
	if !got.says(closedNumber) {
		t.Errorf("the report names no %s, so the reader cannot go look:\n%s",
			closedNumber, got.output)
	}
}

// TestNoPRFromThatHeadIsAFailedPush is the control that keeps the fix from
// turning every absence into a pass. Without it, a recipe that simply stopped
// reporting failed pushes would satisfy the two rows above.
func TestNoPRFromThatHeadIsAFailedPush(t *testing.T) {
	w := newWorld(t)
	w.stubGh(t, emptyPayload, "")

	got := w.run(t, os.Getenv("PATH"))

	if got.status == 0 {
		t.Errorf("exit 0 over a branch origin has never seen and GitHub has no PR "+
			"for:\n%s", got.output)
	}
	if !got.says(failedPushClaim) {
		t.Errorf("the report does not say the push failed, which is what this state "+
			"is:\n%s", got.output)
	}
}

// TestGitHubIsNotAskedWhenTheBranchIsOnOrigin pins the cost. The pre-push hook
// advertises this recipe, so it runs where a network round trip is least
// wanted, and the state it was written for (bd gqlc-ehgg) is answerable
// without one.
func TestGitHubIsNotAskedWhenTheBranchIsOnOrigin(t *testing.T) {
	w := newWorld(t)
	w.stubGh(t, emptyPayload, "")
	git(t, w.work, "push", "--quiet", "--no-verify", "origin", probeBranch)

	got := w.run(t, os.Getenv("PATH"))

	if got.status != 0 {
		t.Errorf("exit %d over a branch on origin at the same commit:\n%s", got.status, got.output)
	}
	if !got.says("LANDED") {
		t.Errorf("the report does not say the push landed:\n%s", got.output)
	}
	if argv := w.ghArgv(t); argv != "" {
		t.Errorf("gh was called %d time(s) for a question git ls-remote had already "+
			"answered:\n%s", strings.Count(argv, "\n"), argv)
	}
}

// TestADivergedRemoteIsUnchanged holds the third original state.
func TestADivergedRemoteIsUnchanged(t *testing.T) {
	w := newWorld(t)
	w.stubGh(t, emptyPayload, "")
	git(t, w.work, "push", "--quiet", "--no-verify", "origin", probeBranch)
	if err := os.WriteFile(filepath.Join(w.work, "file"), []byte("two\n"), 0o644); err != nil {
		t.Fatalf("rewrite fixture file: %v", err)
	}
	git(t, w.work, "commit", "--quiet", "--no-verify", "-am", "two")

	got := w.run(t, os.Getenv("PATH"))

	if got.status == 0 {
		t.Errorf("exit 0 over a remote holding a different commit:\n%s", got.output)
	}
	if !got.says("DIVERGED") {
		t.Errorf("the report does not name the divergence:\n%s", got.output)
	}
}

// TestAnAbsentGhDegradesInsteadOfAsserting. Consulting GitHub costs a
// dependency the recipe did not have, so its absence has to cost the answer
// rather than the run — and must not be reported as the answer.
func TestAnAbsentGhDegradesInsteadOfAsserting(t *testing.T) {
	w := newWorld(t)
	// No stub, and a PATH holding only what the recipe's own commands need.
	// A dedicated directory rather than the caller's PATH, so `gh` is absent
	// in the way `command -v` sees.
	bare := filepath.Join(t.TempDir(), "bare-bin")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", bare, err)
	}
	for _, tool := range []string{"bash", "env", "git", "awk", "sed", "cat"} {
		found, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s is not on PATH, so this row cannot build a PATH that holds "+
				"everything but gh: %v", tool, err)
		}
		if err := os.Symlink(found, filepath.Join(bare, tool)); err != nil {
			t.Fatalf("link %s: %v", tool, err)
		}
	}
	if _, err := exec.LookPath("gh"); err == nil {
		if _, err := os.Stat(filepath.Join(bare, "gh")); err == nil {
			t.Fatal("the bare PATH holds a gh, so this row measures nothing")
		}
	}

	got := w.run(t, bare)

	if got.status == 0 {
		t.Errorf("exit 0 without having established anything:\n%s", got.output)
	}
	if got.says(failedPushClaim) {
		t.Errorf("the report claims %q on a question it could not ask:\n%s",
			failedPushClaim, got.output)
	}
	if !got.says("gh") {
		t.Errorf("the report does not name what it was missing, so the reader cannot "+
			"repair it:\n%s", got.output)
	}
}

// TestAGhThatCannotAnswerDegradesInsteadOfAsserting is the same refusal for a
// gh that is installed and fails — no auth, no network, a repository it cannot
// resolve.
func TestAGhThatCannotAnswerDegradesInsteadOfAsserting(t *testing.T) {
	const failure = "gh: could not authenticate"
	w := newWorld(t)
	w.stubGh(t, emptyPayload, failure)

	got := w.run(t, os.Getenv("PATH"))

	if got.status == 0 {
		t.Errorf("exit 0 without having established anything:\n%s", got.output)
	}
	if got.says(failedPushClaim) {
		t.Errorf("the report claims %q on a question that errored:\n%s",
			failedPushClaim, got.output)
	}
	if !got.says(failure) {
		t.Errorf("the report drops gh's own reason, which is the only thing that says "+
			"whether this is auth, network or the wrong repository:\n%s", got.output)
	}
}

// TestTheQueryAsksForEveryStateOfThatHead pins the question. `--state all` is
// load-bearing: gh's default is open PRs, and a merged one is not open, so the
// default would answer every row above the way an absent PR does.
func TestTheQueryAsksForEveryStateOfThatHead(t *testing.T) {
	w := newWorld(t)
	w.stubGh(t, mergedPayload, "")

	w.run(t, os.Getenv("PATH"))

	argv := w.ghArgv(t)
	if argv == "" {
		t.Fatal("gh was never called, so there is no query to read")
	}
	for _, want := range []string{"pr list", "--head " + probeBranch, "--state all"} {
		if !strings.Contains(argv, want) {
			t.Errorf("the query carries no %q:\n%s", want, argv)
		}
	}
}

// TestTheRealGhDegradesOnARepositoryItCannotResolve is the one row that runs
// the real binary. It reaches only the degrade path — the throwaway origin is
// a local path, so gh resolves no GitHub host — which is what can be asked
// without a network call or a token. Nothing here witnesses the JSON shape the
// stub above is modelled on; that was measured by hand and is recorded at the
// head of this file.
func TestTheRealGhDegradesOnARepositoryItCannotResolve(t *testing.T) {
	if _, err := exec.LookPath("gh"); err != nil {
		t.Fatalf("`gh` is not on PATH. The recipe under test consults it and CI's "+
			"runner carries it, so this is a broken environment and not a case to "+
			"skip past: %v", err)
	}
	w := newWorld(t)
	// No stub: w.bin is empty, so PATH resolves gh to the real binary.

	got := w.run(t, os.Getenv("PATH"))

	if got.status == 0 {
		t.Errorf("exit 0 on a repository gh cannot resolve:\n%s", got.output)
	}
	if got.says(failedPushClaim) {
		t.Errorf("the report claims %q on a question the real gh refused:\n%s",
			failedPushClaim, got.output)
	}
}
