package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixtures for the -worktrees mode. Every one of them builds its own repository
// inside t.TempDir() and registers worktrees as its siblings, so no row here
// ever asks git about a worktree this package did not create — a reaper
// exercised against the town's real registry is how the work it was written to
// protect gets lost.

// wtWorld returns a repository whose basename is "gqlc", because the seat
// exclusion is derived from the MAIN worktree's basename and a fixture named
// anything else would never exercise it.
func wtWorld(t *testing.T) (repo, base string) {
	t.Helper()
	isolateGit(t)
	base = t.TempDir()
	return newRepo(t, filepath.Join(base, "gqlc")), base
}

// addLanded registers a worktree whose branch touched a path that master then
// gained identically. That is what a squash-merged branch looks like here:
// its commits are on no remote ref and it is not an ancestor of master, so
// only a CONTENT test calls it landed.
func addLanded(t *testing.T, repo, base, name string) string {
	t.Helper()
	path := filepath.Join(base, name)
	mustGit(t, repo, "worktree", "add", "-q", path, "-b", "feat/"+name)
	writeFile(t, filepath.Join(path, name+".txt"), name+"\n")
	mustGit(t, path, "add", name+".txt")
	mustGit(t, path, "commit", "-qm", "add "+name)
	writeFile(t, filepath.Join(repo, name+".txt"), name+"\n")
	mustGit(t, repo, "add", name+".txt")
	mustGit(t, repo, "commit", "-qm", "add "+name+" (#1)")
	ageTree(t, path, time.Now().Add(-72*time.Hour))
	return path
}

func wtOptions(repo string) options {
	return options{repo: repo, base: "master", maxAge: 12 * time.Hour, procDir: "/proc", worktrees: true}
}

// planFor runs the real decision table over a real registry, reaching git
// through the same oracle the command uses.
func planFor(t *testing.T, repo string, held map[string]string) []wtEntry {
	t.Helper()
	regs, err := listWorktreeRegistrations(t.Context(), repo)
	if err != nil {
		t.Fatalf("list registrations of %s: %v", repo, err)
	}
	cfg := wtConfig{
		seatPrefix: filepath.Base(regs[0].path) + "-seat-",
		base:       "master",
		maxAge:     12 * time.Hour,
		now:        time.Now(),
	}
	return planWorktrees(t.Context(), cfg, regs, gitOracle{base: "master"}.state, held)
}

func wtFind(t *testing.T, entries []wtEntry, name string) wtEntry {
	t.Helper()
	for _, e := range entries {
		if filepath.Base(e.path) == name {
			return e
		}
	}
	t.Fatalf("no entry named %q in the plan (%d entries)", name, len(entries))
	return wtEntry{}
}

func runWorktreeMode(t *testing.T, o options) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runWorktrees(t.Context(), o, &out)
	t.Logf("report:\n%s", out.String())
	return out.String(), err
}

// R1 — THE control, and the reason this mode exists. A worktree holding a
// commit whose content is nowhere else must be HELD. It is written as a
// separate row from the REAP below rather than as its negation because the
// failure it guards against is the exists-elsewhere test going VACUOUS: an
// oracle that answered "landed" from the candidate's own ref would reap this
// tree and every other one, and the plan would look entirely reasonable
// (bd gqlc-24wf, the live-worktree deletion).
func TestPlanWorktrees_UnlandedContentIsHeld(t *testing.T) {
	repo, base := wtWorld(t)
	path := filepath.Join(base, "gqlc-unlanded")
	mustGit(t, repo, "worktree", "add", "-q", path, "-b", "feat/unlanded")
	writeFile(t, filepath.Join(path, "only-here.txt"), "exists on no other ref\n")
	mustGit(t, path, "add", "only-here.txt")
	mustGit(t, path, "commit", "-qm", "work that has landed nowhere")
	ageTree(t, path, time.Now().Add(-72*time.Hour))

	e := wtFind(t, planFor(t, repo, nil), "gqlc-unlanded")
	if e.verdict != wtHold {
		t.Fatalf("verdict = %s, want HOLD; reason: %s", e.verdict, e.reason)
	}
	if !strings.Contains(e.reason, "content is not on master") {
		t.Errorf("the reason does not say the content is not on the base ref: %s", e.reason)
	}
	if !strings.Contains(e.reason, "only-here.txt") {
		t.Errorf("the reason does not name the path that has not landed: %s", e.reason)
	}
}

// R2 — the positive control. Without it every row above passes on a table that
// never reaps anything at all.
func TestPlanWorktrees_LandedCleanAndIdleIsReaped(t *testing.T) {
	repo, base := wtWorld(t)
	addLanded(t, repo, base, "gqlc-landed")

	e := wtFind(t, planFor(t, repo, nil), "gqlc-landed")
	if e.verdict != wtReap {
		t.Fatalf("verdict = %s, want REAP; reason: %s", e.verdict, e.reason)
	}
	for _, want := range []string{"no live process inside", "clean", "content on master"} {
		if !strings.Contains(e.reason, want) {
			t.Errorf("the reason does not say %q, so a reader cannot check it: %s", want, e.reason)
		}
	}
}

// R3 — untracked ALONE counts as dirty. A file nobody has added is the shape
// an agent's in-progress work most often takes, and it is the shape git's own
// `worktree remove` also refuses.
func TestPlanWorktrees_AnUntrackedFileAloneHoldsIt(t *testing.T) {
	repo, base := wtWorld(t)
	path := addLanded(t, repo, base, "gqlc-dirty")
	writeFile(t, filepath.Join(path, "unsaved.md"), "not committed anywhere\n")

	e := wtFind(t, planFor(t, repo, nil), "gqlc-dirty")
	if e.verdict != wtHold {
		t.Fatalf("verdict = %s, want HOLD; reason: %s", e.verdict, e.reason)
	}
	if !strings.Contains(e.reason, "unsaved.md") {
		t.Errorf("the reason does not name the untracked file: %s", e.reason)
	}
}

// R4 — this caller's own fail-closed clause. procRefsUnder refuses a process
// table that does not name the caller, but a fail-closed SEAM does not save a
// caller that ignores its error: both caller-side mutations on the scratch
// path measured SURVIVED once (bd gqlc-2459). An empty held set is bit for bit
// the value meaning "no worktree is occupied".
func TestRunWorktrees_UnreadableProcessTableRefusesToPlan(t *testing.T) {
	repo, base := wtWorld(t)
	addLanded(t, repo, base, "gqlc-landed")
	o := wtOptions(repo)
	o.procDir = filepath.Join(t.TempDir(), "no-such-procfs")

	out, err := runWorktreeMode(t, o)
	if err == nil {
		t.Fatal("the mode planned over an unreadable process table instead of refusing")
	}
	if !strings.Contains(err.Error(), o.procDir) {
		t.Errorf("the error does not name the process table it could not read: %v", err)
	}
	// Not merely "an error": the mutation's signature is a plan that PRINTS,
	// with every idle worktree marked reapable on no occupancy evidence.
	if strings.Contains(out, "REAP") {
		t.Errorf("a plan was reported despite the refusal:\n%s", out)
	}
}

// R5 — the occupant. This is the half the sweep that caused gqlc-24wf never
// asked: the tree is clean, landed and old, and a process is standing in it.
func TestPlanWorktrees_ALiveProcessInsideHoldsIt(t *testing.T) {
	repo, base := wtWorld(t)
	path := addLanded(t, repo, base, "gqlc-occupied")

	refs, err := procRefsUnder(fakeProcHolding(t, path), []string{path})
	if err != nil {
		t.Fatalf("scan the fixture process table: %v", err)
	}
	held := heldUnder([]string{path}, refs)
	if len(held) == 0 {
		t.Fatal("the fixture never produced a held set, so this row would pass on any table")
	}

	e := wtFind(t, planFor(t, repo, held), "gqlc-occupied")
	if e.verdict != wtHold {
		t.Fatalf("verdict = %s, want HOLD; reason: %s", e.verdict, e.reason)
	}
	if !strings.Contains(e.reason, "in use") {
		t.Errorf("the reason does not say it is in use: %s", e.reason)
	}
}

// A seat's cwd is the worktree ITSELF, not a child of it. heldTopLevel drops a
// reference to the root it is passed — correctly, for a scan root — so this
// mode needs its own mapping, and this row is what distinguishes them.
func TestHeldUnder_AReferenceToTheCandidateItselfCounts(t *testing.T) {
	held := heldUnder([]string{"/w/one", "/w/two"}, []procRef{
		{pid: "5", via: "cwd", target: "/w/one"},
		{pid: "6", via: "fd", target: "/w/two/deep/notes.md"},
	})
	if held["/w/one"] == "" {
		t.Error("a process whose cwd IS the worktree did not pin it")
	}
	if held["/w/two"] == "" {
		t.Error("a deep reference did not pin the worktree it is under")
	}
}

// R6 — the permanent seats. Reapable on every other gate, and excluded before
// any of them is asked. Deliberate over-exclusion: see decideWorktree.
func TestPlanWorktrees_ASeatWorktreeIsExcludedThoughOtherwiseReapable(t *testing.T) {
	repo, base := wtWorld(t)
	addLanded(t, repo, base, "gqlc-seat-astghik")

	e := wtFind(t, planFor(t, repo, nil), "gqlc-seat-astghik")
	if e.verdict != wtExcluded {
		t.Fatalf("verdict = %s, want EXCLUDED; reason: %s", e.verdict, e.reason)
	}
	if !strings.Contains(e.reason, "seat worktree") {
		t.Errorf("the reason does not say it is a seat: %s", e.reason)
	}
	// The control for the exclusion: the same shape without the prefix reaps,
	// so this row is measuring the NAME and not some other gate the fixture
	// happens to trip.
	addLanded(t, repo, base, "gqlc-ordinary")
	if got := wtFind(t, planFor(t, repo, nil), "gqlc-ordinary"); got.verdict != wtReap {
		t.Fatalf("the un-prefixed control was %s, not REAP, so the row above proves nothing; reason: %s",
			got.verdict, got.reason)
	}
}

// R7 — the main checkout. git refuses to remove it in any case; this is the
// first gate so that nothing below is ever asked about it.
func TestPlanWorktrees_TheMainCheckoutIsExcluded(t *testing.T) {
	repo, _ := wtWorld(t)
	e := wtFind(t, planFor(t, repo, nil), filepath.Base(repo))
	if e.verdict != wtExcluded {
		t.Fatalf("verdict = %s, want EXCLUDED; reason: %s", e.verdict, e.reason)
	}
	if !strings.Contains(e.reason, "main checkout") {
		t.Errorf("the reason does not say it is the main checkout: %s", e.reason)
	}
}

// R8 — the row that runs the REAL actuator at the boundary rather than
// trusting its documentation. The tree is classified REAP and then dirtied
// underneath the plan, which is the classify-to-remove race. git's own dirt
// check is the second wall, and it only holds because --force is never passed.
func TestApplyWorktrees_GitRefusesADirtiedTreeAndTheRunContinues(t *testing.T) {
	repo, base := wtWorld(t)
	raced := addLanded(t, repo, base, "gqlc-raced")
	alsoReapable := addLanded(t, repo, base, "gqlc-clean")

	entries := planFor(t, repo, nil)
	if got := wtFind(t, entries, "gqlc-raced"); got.verdict != wtReap {
		t.Fatalf("the fixture was not classified REAP, so the race is never exercised: %s", got.reason)
	}
	// After the plan, before the removal.
	writeFile(t, filepath.Join(raced, "appeared.md"), "written after classification\n")

	var out bytes.Buffer
	pr := &printer{w: &out}
	if err := applyWorktrees(t.Context(), repo, entries, pr); err != nil {
		t.Fatalf("apply returned an error rather than reporting the refusal and continuing: %v", err)
	}
	t.Logf("report:\n%s", out.String())

	if _, err := os.Stat(filepath.Join(raced, "appeared.md")); err != nil {
		t.Errorf("the dirtied tree was removed anyway: %v", err)
	}
	if !strings.Contains(out.String(), "refused by git, left in place") {
		t.Errorf("the refusal was not reported:\n%s", out.String())
	}
	// Continued: the other reapable tree is gone.
	if _, err := os.Stat(alsoReapable); !os.IsNotExist(err) {
		t.Errorf("the run stopped at the refusal instead of continuing: %s still exists (%v)", alsoReapable, err)
	}
}

// R9 — a registration whose checkout is gone. Nothing is deleted for it; the
// bookkeeping is retired by git's own prune, and afterwards the registry no
// longer names it. os.RemoveAll would leave exactly this registration behind,
// which is how the town accumulated them.
func TestApplyWorktrees_AStaleRegistrationIsPrunedOutOfTheRegistry(t *testing.T) {
	repo, base := wtWorld(t)
	gone := addLanded(t, repo, base, "gqlc-gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	entries := planFor(t, repo, nil)
	e := wtFind(t, entries, "gqlc-gone")
	if e.verdict != wtPrune {
		t.Fatalf("verdict = %s, want PRUNE; reason: %s", e.verdict, e.reason)
	}
	if before := mustGit(t, repo, "worktree", "list", "--porcelain"); !strings.Contains(before, gone) {
		t.Fatalf("the registry did not name the stale worktree before pruning, so this row proves nothing:\n%s", before)
	}

	var out bytes.Buffer
	if err := applyWorktrees(t.Context(), repo, entries, &printer{w: &out}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if after := mustGit(t, repo, "worktree", "list", "--porcelain"); strings.Contains(after, gone) {
		t.Errorf("the stale registration survived the prune:\n%s", after)
	}
}

// R10 — a lock is somebody saying "not this one". Removing it would need
// -f -f, and this tool never overrides a human.
func TestPlanWorktrees_ALockedWorktreeIsHeldAndNeverRemoved(t *testing.T) {
	repo, base := wtWorld(t)
	locked := addLanded(t, repo, base, "gqlc-locked")
	mustGit(t, repo, "worktree", "lock", "--reason", "mid-bisect, do not touch", locked)

	entries := planFor(t, repo, nil)
	e := wtFind(t, entries, "gqlc-locked")
	if e.verdict != wtHold {
		t.Fatalf("verdict = %s, want HOLD; reason: %s", e.verdict, e.reason)
	}
	if !strings.Contains(e.reason, "mid-bisect, do not touch") {
		t.Errorf("the reason does not carry the lock reason the operator gave: %s", e.reason)
	}

	var out bytes.Buffer
	if err := applyWorktrees(t.Context(), repo, entries, &printer{w: &out}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(locked); err != nil {
		t.Errorf("the locked worktree was removed: %v", err)
	}
}

// The porcelain shapes the decision table reads, pinned against the format
// rather than against a recollection of it. `locked` and `prunable` each
// appear both bare and with a reason, and a bare one must still be an
// attribute — reading the reason alone would make an unannotated lock
// indistinguishable from no lock.
func TestParseWorktreeRegistrations_AttributesWithAndWithoutAReason(t *testing.T) {
	regs := parseWorktreeRegistrations(strings.Join([]string{
		"worktree /w/main", "HEAD abc123def456", "branch refs/heads/master", "",
		"worktree /w/detached", "HEAD 0123456789ab", "detached", "",
		"worktree /w/locked-bare", "HEAD aaaa1111bbbb", "detached", "locked", "",
		"worktree /w/locked-why", "HEAD bbbb2222cccc", "detached", "locked mid-bisect", "",
		"worktree /w/prunable", "HEAD cccc3333dddd", "detached", "prunable gitdir file points to non-existent location", "",
	}, "\n"))

	if len(regs) != 5 {
		t.Fatalf("parsed %d registrations, want 5: %+v", len(regs), regs)
	}
	if got := regs[0].at(); got != "master" {
		t.Errorf("main is at %q, want the branch with refs/heads/ stripped", got)
	}
	if got := regs[1].at(); got != "detached at 01234567" {
		t.Errorf("detached worktree is at %q, want the short HEAD", got)
	}
	if !regs[2].isLocked || regs[2].locked != "" {
		t.Errorf("a bare `locked` line did not read as locked-without-a-reason: %+v", regs[2])
	}
	if !regs[3].isLocked || regs[3].locked != "mid-bisect" {
		t.Errorf("an annotated lock lost its reason: %+v", regs[3])
	}
	if !regs[4].isPrunable || !strings.Contains(regs[4].prunable, "non-existent location") {
		t.Errorf("a prunable registration lost its reason: %+v", regs[4])
	}
	if regs[0].isLocked || regs[0].isPrunable {
		t.Errorf("attributes leaked from a later stanza into the first: %+v", regs[0])
	}
}

// An empty registry is refused rather than returned. Real git cannot produce
// one — it always names the main worktree — so this is reachable only through
// parseRegistry, which is why parseRegistry exists. The direction matters: a
// plan over no registrations reports a clean sweep having decided nothing, and
// under -apply it reports success over a repository it never found.
func TestParseRegistry_AnEmptyRegistryIsRefusedRatherThanReturned(t *testing.T) {
	_, err := parseRegistry("", "/some/repo")
	if err == nil {
		t.Fatal("an empty `git worktree list` was accepted; a plan over it would report a clean sweep over nothing")
	}
	if !strings.Contains(err.Error(), "/some/repo") {
		t.Errorf("the refusal does not name the repository it asked: %v", err)
	}

	regs, err := parseRegistry("worktree /some/repo\nHEAD abc123def456\n", "/some/repo")
	if err != nil {
		t.Fatalf("a registry naming one worktree was refused: %v", err)
	}
	if len(regs) != 1 || regs[0].path != "/some/repo" {
		t.Errorf("parseRegistry returned %+v, want the single registration it was given", regs)
	}
}

// The flags this mode does not read are refused rather than ignored. -root is
// the sharpest: someone who writes it has said which population they mean, and
// silently deciding over a different one is the failure this refusal prevents.
func TestParseOptions_WorktreesRefusesTheFlagsItDoesNotRead(t *testing.T) {
	for _, dead := range []string{"-root=/tmp", "-check", "-top=3", "-archive=/tmp/x.tgz", "-apply-above=50"} {
		args := []string{"-worktrees", dead}
		if dead == "-apply-above=50" {
			args = append(args, "-apply")
		}
		_, err := parseOptions(args, &bytes.Buffer{})
		if err == nil {
			t.Errorf("%s was accepted alongside -worktrees, which never reads it", dead)
			continue
		}
		if !strings.Contains(err.Error(), "gates nothing") {
			t.Errorf("%s: the refusal does not say the flag gates nothing: %v", dead, err)
		}
	}
	// The control: the flags the mode DOES read stay accepted, or the loop
	// above would pass on a mode that refuses everything.
	if _, err := parseOptions([]string{"-worktrees", "-repo=/w", "-base=origin/master", "-age=6h", "-apply"}, &bytes.Buffer{}); err != nil {
		t.Errorf("-worktrees refused a flag it does read: %v", err)
	}
}
