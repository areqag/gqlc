package main

import (
	"context"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func testConfig(root string) scanConfig {
	return scanConfig{
		root:   root,
		base:   "origin/master",
		maxAge: 12 * time.Hour,
		now:    time.Now(),
		uid:    os.Getuid(),
	}
}

func scanFixture(t *testing.T, root string, worktrees map[string]bool, oracle worktreeOracle, held map[string]string) []entry {
	t.Helper()
	entries, err := scanRoot(t.Context(), testConfig(root), worktrees, oracle, held)
	if err != nil {
		t.Fatalf("scanRoot: %v", err)
	}
	return entries
}

func TestScanRoot_AbandonedPlainScratchIsReaped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "probe937r3", "run.log"), "output\n")
	ageTree(t, root, time.Now().Add(-48*time.Hour))

	e := findEntry(t, scanFixture(t, root, nil, stubOracle(nil), nil), "probe937r3")
	if !e.reap {
		t.Fatalf("expected reap, got retain: %s", e.reason)
	}
	if e.kind != kindPlain {
		t.Errorf("kind = %q, want %q", e.kind, kindPlain)
	}
	if e.inodes != 2 {
		t.Errorf("inodes = %d, want 2 (the directory and its file)", e.inodes)
	}
}

// The in-use gate, mtime half. A directory's OWN mtime does not move when a file
// inside it is rewritten, so the age has to come from the newest mtime anywhere
// in the tree; reading the top-level one alone reaps a tree an agent wrote to
// ten seconds ago.
func TestScanRoot_RecentWriteDeepInAnOldTreeRetains(t *testing.T) {
	root := t.TempDir()
	dir := mkdir(t, filepath.Join(root, "carmack-mut-7", "nested", "deeper"))
	writeFile(t, filepath.Join(dir, "live.log"), "still writing\n")
	ageTree(t, root, time.Now().Add(-48*time.Hour))
	now := time.Now()
	if err := os.Chtimes(filepath.Join(dir, "live.log"), now, now); err != nil {
		t.Fatal(err)
	}

	e := findEntry(t, scanFixture(t, root, nil, stubOracle(nil), nil), "carmack-mut-7")
	if e.reap {
		t.Fatal("reaped a tree written to seconds ago")
	}
	if !strings.Contains(e.reason, "threshold") {
		t.Errorf("reason = %q, want the age threshold named", e.reason)
	}
}

// The in-use gate, live-process half. mtime alone cannot see this: the tree is
// two days old and a process is sitting in it.
func TestScanRoot_DirectoryHeldByLiveProcessRetained(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "f3uof", "notes.md"), "notes\n")
	ageTree(t, root, time.Now().Add(-48*time.Hour))
	held := map[string]string{"f3uof": "pid 4242 has it as cwd (" + filepath.Join(root, "f3uof") + ")"}

	e := findEntry(t, scanFixture(t, root, nil, stubOracle(nil), held), "f3uof")
	if e.reap {
		t.Fatal("reaped a directory a live process is sitting in")
	}
	if !strings.Contains(e.reason, "in use") {
		t.Errorf("reason = %q, want it to say the entry is in use", e.reason)
	}
}

func TestScanRoot_DirtyWorktreeRetained(t *testing.T) {
	root := t.TempDir()
	wt := mkdir(t, filepath.Join(root, "gqlc-dirty"))
	writeFile(t, filepath.Join(wt, "work.go"), "package work\n")
	ageTree(t, root, time.Now().Add(-72*time.Hour))
	oracle := stubOracle(map[string]wtState{wt: {dirty: true, dirtyDetail: "?? work.go", landed: true}})

	e := findEntry(t, scanFixture(t, root, map[string]bool{wt: true}, oracle, nil), "gqlc-dirty")
	if e.reap {
		t.Fatal("reaped a worktree with uncommitted work in it")
	}
	if e.kind != kindWorktree {
		t.Errorf("kind = %q, want %q", e.kind, kindWorktree)
	}
	if !strings.Contains(e.reason, "uncommitted") {
		t.Errorf("reason = %q, want it to name the uncommitted changes", e.reason)
	}
}

func TestScanRoot_UnlandedWorktreeRetained(t *testing.T) {
	root := t.TempDir()
	wt := mkdir(t, filepath.Join(root, "gqlc-unlanded"))
	writeFile(t, filepath.Join(wt, "work.go"), "package work\n")
	ageTree(t, root, time.Now().Add(-72*time.Hour))
	oracle := stubOracle(map[string]wtState{wt: {landed: false, landedDetail: "1 of 1 touched path(s) differ"}})

	e := findEntry(t, scanFixture(t, root, map[string]bool{wt: true}, oracle, nil), "gqlc-unlanded")
	if e.reap {
		t.Fatal("reaped a worktree whose content is not on the base ref")
	}
	if !strings.Contains(e.reason, "not on origin/master") {
		t.Errorf("reason = %q, want it to name the base ref", e.reason)
	}
}

func TestScanRoot_CleanLandedWorktreeReaped(t *testing.T) {
	root := t.TempDir()
	wt := mkdir(t, filepath.Join(root, "gqlc-landed"))
	writeFile(t, filepath.Join(wt, ".git"), "gitdir: /elsewhere\n")
	writeFile(t, filepath.Join(wt, "work.go"), "package work\n")
	ageTree(t, root, time.Now().Add(-72*time.Hour))
	oracle := stubOracle(map[string]wtState{wt: {landed: true, landedDetail: "3 touched path(s), all equal on origin/master"}})

	e := findEntry(t, scanFixture(t, root, map[string]bool{wt: true}, oracle, nil), "gqlc-landed")
	if !e.reap {
		t.Fatalf("expected reap of a clean, landed, three-day-old worktree, got: %s", e.reason)
	}
	if e.kind != kindWorktree {
		t.Errorf("kind = %q, want %q", e.kind, kindWorktree)
	}
}

// An oracle that cannot answer must not be read as a clean answer.
func TestScanRoot_WorktreeGitFailureRetains(t *testing.T) {
	root := t.TempDir()
	wt := mkdir(t, filepath.Join(root, "gqlc-broken"))
	writeFile(t, filepath.Join(wt, "work.go"), "package work\n")
	ageTree(t, root, time.Now().Add(-72*time.Hour))
	oracle := func(_ context.Context, _ string) (wtState, error) { return wtState{}, errGit }

	e := findEntry(t, scanFixture(t, root, map[string]bool{wt: true}, oracle, nil), "gqlc-broken")
	if e.reap {
		t.Fatal("reaped a worktree git could not answer for")
	}
}

// Defence in depth behind the worktree arm: a checkout the registry does not
// name still holds a .git, and that alone is enough to refuse it. This is what
// stands between a mis-resolved -root or a lost registration and a deleted
// branch.
func TestScanRoot_UnregisteredRepositoryRetainedAsForeign(t *testing.T) {
	root := t.TempDir()
	repo := mkdir(t, filepath.Join(root, "someones-clone"))
	writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/master\n")
	writeFile(t, filepath.Join(repo, "README.md"), "hi\n")
	ageTree(t, root, time.Now().Add(-72*time.Hour))

	e := findEntry(t, scanFixture(t, root, nil, stubOracle(nil), nil), "someones-clone")
	if e.reap {
		t.Fatal("reaped a directory holding a git repository")
	}
	if e.kind != kindForeignRepo {
		t.Errorf("kind = %q, want %q", e.kind, kindForeignRepo)
	}
}

func TestScanRoot_SystemNamedEntriesRetained(t *testing.T) {
	root := t.TempDir()
	names := []string{
		".X11-unix",
		".ICE-unix",
		".font-unix",
		"systemd-private-19e5c79a-upower.service-dZVnIX",
		"snap-private-tmp",
		"kheaders-7.1.5-arch1-2",
		"hsperfdata_root",
	}
	for _, n := range names {
		writeFile(t, filepath.Join(root, n, "payload"), "x\n")
	}
	ageTree(t, root, time.Now().Add(-30*24*time.Hour))

	entries := scanFixture(t, root, nil, stubOracle(nil), nil)
	for _, n := range names {
		e := findEntry(t, entries, n)
		if e.reap {
			t.Errorf("reaped %s, which belongs to the machine: %s", n, e.reason)
		}
		if e.kind != kindSystem {
			t.Errorf("%s: kind = %q, want %q", n, e.kind, kindSystem)
		}
	}
}

// The ownership half of the system gate, which is what covers a system entry
// whose name this tool has never heard of. It cannot be tested by creating a
// root-owned file, so it is tested at the predicate.
func TestSystemReason_ForeignOwnerRefused(t *testing.T) {
	mine := os.Getuid()
	info := fakeInfo{name: "kheaders-next-release", mode: fs.ModeDir | 0o755, uid: uint32(mine + 1)}
	if reason := systemReason("some-unremarkable-name", info, mine); reason == "" {
		t.Fatal("an entry owned by another uid was accepted as a candidate")
	}
	own := fakeInfo{name: "mine", mode: fs.ModeDir | 0o755, uid: uint32(mine)}
	if reason := systemReason("some-unremarkable-name", own, mine); reason != "" {
		t.Fatalf("own directory refused: %s", reason)
	}
}

func TestScanRoot_SocketRetained(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "filler", "x"), "x\n")
	sock := filepath.Join(root, "agent-socket")
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "unix", sock)
	if err != nil {
		t.Skipf("cannot create a unix socket in the fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
	})
	ageTree(t, root, time.Now().Add(-72*time.Hour))

	e := findEntry(t, scanFixture(t, root, nil, stubOracle(nil), nil), "agent-socket")
	if e.reap {
		t.Fatal("reaped a unix socket")
	}
	if e.kind != kindIrregular {
		t.Errorf("kind = %q, want %q", e.kind, kindIrregular)
	}
}

func TestScanRoot_SymlinkRetained(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "filler", "x"), "x\n")
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "pointer")); err != nil {
		t.Fatal(err)
	}
	ageTree(t, root, time.Now().Add(-72*time.Hour))

	e := findEntry(t, scanFixture(t, root, nil, stubOracle(nil), nil), "pointer")
	if e.reap {
		t.Fatal("reaped a symlink, whose target was never measured")
	}
	// Asserted on the kind, not only on reap: os.Chtimes follows the link, so a
	// symlink's own mtime stays recent and the age gate would retain it for a
	// reason that has nothing to do with it being a symlink.
	if e.kind != kindIrregular {
		t.Errorf("kind = %q, want %q", e.kind, kindIrregular)
	}
}

func TestScanRoot_UnreadableSubtreeRetained(t *testing.T) {
	if os.Getuid() == 0 { // root reads everything, so the gate cannot be reached
		t.Skip("running as root")
	}
	root := t.TempDir()
	locked := mkdir(t, filepath.Join(root, "locked", "inner"))
	writeFile(t, filepath.Join(locked, "secret"), "x\n")
	ageTree(t, root, time.Now().Add(-72*time.Hour))
	if err := os.Chmod(filepath.Join(root, "locked", "inner"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(filepath.Join(root, "locked", "inner"), 0o755); err != nil {
			t.Errorf("restore mode: %v", err)
		}
	})

	e := findEntry(t, scanFixture(t, root, nil, stubOracle(nil), nil), "locked")
	if e.reap {
		t.Fatal("reaped a tree it could not fully read, and so could not archive")
	}
}

// A scan that measured nothing must say so rather than report a clean root.
func TestScanRoot_EmptyRootRefused(t *testing.T) {
	_, err := scanRoot(t.Context(), testConfig(t.TempDir()), nil, stubOracle(nil), nil)
	if err == nil {
		t.Fatal("an empty root was reported as a successful scan of nothing")
	}
	if !strings.Contains(err.Error(), "measured nothing") {
		t.Errorf("error = %v, want it to say the scan measured nothing", err)
	}
}

// fakeInfo carries an owner uid, which no file this test process can create
// would.
type fakeInfo struct {
	name string
	mode fs.FileMode
	uid  uint32
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return &syscall.Stat_t{Uid: f.uid} }
