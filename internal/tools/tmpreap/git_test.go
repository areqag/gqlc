package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// squashLanded builds the shape this repo actually produces: a branch in a
// worktree whose content was merged to master by SQUASH, so the branch head is
// not an ancestor of master and its commits are on no remote ref. Returns the
// repo and the worktree path.
func squashLanded(t *testing.T) (repo, wt string) {
	t.Helper()
	isolateGit(t)
	base := t.TempDir()
	repo = newRepo(t, filepath.Join(base, "repo"))
	wt = filepath.Join(base, "gqlc-feature")
	mustGit(t, repo, "worktree", "add", "-q", wt, "-b", "feat/x")
	writeFile(t, filepath.Join(wt, "b.txt"), "b\n")
	mustGit(t, wt, "add", "b.txt")
	mustGit(t, wt, "commit", "-qm", "add b")
	// The squash: same content, a different commit, straight onto master.
	writeFile(t, filepath.Join(repo, "b.txt"), "b\n")
	mustGit(t, repo, "add", "b.txt")
	mustGit(t, repo, "commit", "-qm", "add b (#1)")
	return repo, wt
}

// The claim the whole worktree arm rests on: ancestry says stranded, content
// says landed, and content is right. Measured on five month-old branches in the
// live repo before this was written (bd gqlc-osuz).
func TestGitOracle_SquashMergedWorktreeIsLanded(t *testing.T) {
	repo, wt := squashLanded(t)

	if _, err := git(t.Context(), repo, "merge-base", "--is-ancestor", "feat/x", "master"); err == nil {
		t.Fatal("fixture is wrong: ancestry considers the squashed branch merged, so this test proves nothing")
	}

	st, err := gitOracle{base: "master"}.state(t.Context(), wt)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if st.dirty {
		t.Errorf("clean worktree reported dirty: %s", st.dirtyDetail)
	}
	if !st.landed {
		t.Fatalf("squash-merged branch reported unlanded: %s", st.landedDetail)
	}
}

func TestGitOracle_UnlandedAdditionIsNotLanded(t *testing.T) {
	isolateGit(t)
	base := t.TempDir()
	repo := newRepo(t, filepath.Join(base, "repo"))
	wt := filepath.Join(base, "gqlc-open")
	mustGit(t, repo, "worktree", "add", "-q", wt, "-b", "feat/y")
	writeFile(t, filepath.Join(wt, "c.txt"), "c\n")
	mustGit(t, wt, "add", "c.txt")
	mustGit(t, wt, "commit", "-qm", "add c")

	st, err := gitOracle{base: "master"}.state(t.Context(), wt)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if st.landed {
		t.Fatalf("a branch whose only file is absent from master was called landed: %s", st.landedDetail)
	}
	if !strings.Contains(st.landedDetail, "c.txt") {
		t.Errorf("landedDetail = %q, want it to name the unlanded path", st.landedDetail)
	}
}

// The direction a "files present on master" test gets wrong: the branch's work
// is a DELETION, and every file it touches is present on master precisely
// because the work has not landed.
func TestGitOracle_UnlandedDeletionIsNotLanded(t *testing.T) {
	isolateGit(t)
	base := t.TempDir()
	repo := newRepo(t, filepath.Join(base, "repo"))
	wt := filepath.Join(base, "gqlc-delete")
	mustGit(t, repo, "worktree", "add", "-q", wt, "-b", "feat/z")
	mustGit(t, wt, "rm", "-q", "a.txt")
	mustGit(t, wt, "commit", "-qm", "drop a")

	st, err := gitOracle{base: "master"}.state(t.Context(), wt)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if st.landed {
		t.Fatalf("an unlanded deletion was called landed: %s", st.landedDetail)
	}
}

func TestGitOracle_UntrackedFileIsDirty(t *testing.T) {
	repo, wt := squashLanded(t)
	_ = repo
	writeFile(t, filepath.Join(wt, "scratch-notes.md"), "an agent's unsaved work\n")

	st, err := gitOracle{base: "master"}.state(t.Context(), wt)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if !st.dirty {
		t.Fatal("a worktree holding an untracked file was reported clean")
	}
	if !strings.Contains(st.dirtyDetail, "scratch-notes.md") {
		t.Errorf("dirtyDetail = %q, want it to name the untracked file", st.dirtyDetail)
	}
}

func TestGitOracle_ModifiedTrackedFileIsDirty(t *testing.T) {
	_, wt := squashLanded(t)
	writeFile(t, filepath.Join(wt, "a.txt"), "edited in place\n")

	st, err := gitOracle{base: "master"}.state(t.Context(), wt)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if !st.dirty {
		t.Fatal("a worktree with a modified tracked file was reported clean")
	}
}

// An absent base ref must not read as "everything has landed".
func TestGitOracle_MissingBaseRefIsAnError(t *testing.T) {
	_, wt := squashLanded(t)

	if _, err := (gitOracle{base: "origin/does-not-exist"}).state(t.Context(), wt); err == nil {
		t.Fatal("a base ref that does not exist produced an answer instead of an error")
	}
}

// The bead's claim, verified rather than believed: removing a worktree costs the
// checkout and nothing else.
func TestGitWorktreeRemoveKeepsCommitsAndRef(t *testing.T) {
	repo, wt := squashLanded(t)
	head := mustGit(t, wt, "rev-parse", "HEAD")

	if err := remove(t.Context(), repo, entry{path: wt, kind: kindWorktree}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("checkout still on disk after remove (err=%v)", err)
	}
	if got := mustGit(t, repo, "rev-parse", "feat/x"); got != head {
		t.Errorf("branch ref = %q after remove, want %q", got, head)
	}
	if got := mustGit(t, repo, "cat-file", "-t", head); got != "commit" {
		t.Errorf("commit object type = %q after remove, want \"commit\"", got)
	}
}

// git's own refusal is the second line of defence behind the dirty check, so it
// is pinned: --force is never passed, and this is why.
func TestRemove_GitRefusesADirtyWorktree(t *testing.T) {
	repo, wt := squashLanded(t)
	writeFile(t, filepath.Join(wt, "unsaved.md"), "work\n")

	if err := remove(t.Context(), repo, entry{path: wt, kind: kindWorktree}); err == nil {
		t.Fatal("git removed a dirty worktree, so the classifier's dirty check is the only thing standing between an agent's work and deletion")
	}
	if _, err := os.Stat(filepath.Join(wt, "unsaved.md")); err != nil {
		t.Errorf("the untracked file did not survive the refused removal: %v", err)
	}
}

func TestListWorktrees_IncludesMainAndLinkedResolved(t *testing.T) {
	repo, wt := squashLanded(t)

	set, err := listWorktrees(t.Context(), repo)
	if err != nil {
		t.Fatalf("listWorktrees: %v", err)
	}
	for _, want := range []string{repo, wt} {
		resolved, err := filepath.EvalSymlinks(want)
		if err != nil {
			t.Fatal(err)
		}
		if !set[resolved] {
			t.Errorf("%s missing from the worktree registry %v", resolved, set)
		}
	}
}

// A registry that came back empty would make every worktree under the scan root
// look like plain scratch, which is the one classification that skips both git
// checks.
func TestListWorktrees_NonRepositoryRefused(t *testing.T) {
	isolateGit(t)
	if _, err := listWorktrees(t.Context(), t.TempDir()); err == nil {
		t.Fatal("a directory that is not a repository produced a worktree registry instead of an error")
	}
}

// The registry that would be silently catastrophic: parsed cleanly, and empty.
// Every worktree under the scan root would then classify as plain scratch, which
// is the one kind that skips both the dirty and the landed check.
func TestParseWorktreeList_EmptyRegistryRefused(t *testing.T) {
	if _, err := parseWorktreeList("", "/some/repo"); err == nil {
		t.Fatal("an empty worktree registry was accepted")
	}
	if _, err := parseWorktreeList("HEAD deadbeef\nbranch refs/heads/master\n", "/some/repo"); err == nil {
		t.Fatal("a registry with no worktree line at all was accepted")
	}
	set, err := parseWorktreeList("worktree /some/repo\nHEAD deadbeef\n", "/some/repo")
	if err != nil {
		t.Fatalf("parseWorktreeList: %v", err)
	}
	if !set["/some/repo"] {
		t.Errorf("registry %v does not hold the worktree it was given", set)
	}
}
