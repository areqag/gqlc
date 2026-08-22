package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The in-use gate's strong half, against a real process. A directory nothing has
// written to for days is exactly the shape mtime calls abandoned; a process
// sitting in it is the reason mtime is not enough.
func TestProcRefs_FindsALiveProcessWorkingDirectory(t *testing.T) {
	sleep := hasBinary(t, "sleep")
	root := t.TempDir()
	busy := mkdir(t, filepath.Join(root, "an-agent-is-here"))

	cmd := exec.CommandContext(t.Context(), sleep, "60")
	cmd.Dir = busy
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("kill: %v", err)
		}
		// Reaped so the process does not linger as a zombie holding the fixture.
		if _, err := cmd.Process.Wait(); err != nil {
			t.Errorf("wait: %v", err)
		}
	})

	refs, err := procRefs(root)
	if err != nil {
		t.Fatalf("procRefs: %v", err)
	}
	held := heldTopLevel(root, refs)
	who, ok := held["an-agent-is-here"]
	if !ok {
		t.Fatalf("the directory a live pid %d has as its cwd was not reported held; refs=%v", cmd.Process.Pid, refs)
	}
	if !strings.Contains(who, strconv.Itoa(cmd.Process.Pid)) {
		t.Errorf("held description %q does not name pid %d", who, cmd.Process.Pid)
	}
}

// An open file descriptor is a hold too, and it is the one an idle agent that
// has a log open leaves behind.
func TestProcRefs_FindsAnOpenFileDescriptor(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "still-writing", "run.log")
	writeFile(t, logPath, "")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	refs, err := procRefs(root)
	if err != nil {
		t.Fatalf("procRefs: %v", err)
	}
	if _, ok := heldTopLevel(root, refs)["still-writing"]; !ok {
		t.Fatalf("a directory holding a file this process has open was not reported held; refs=%v", refs)
	}
}

// The scan must not report the whole machine as in use.
func TestProcRefs_IgnoresReferencesOutsideRoot(t *testing.T) {
	root := mkdir(t, filepath.Join(t.TempDir(), "root"))
	outside := t.TempDir()
	f, err := os.Create(filepath.Join(outside, "elsewhere.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	refs, err := procRefs(root)
	if err != nil {
		t.Fatalf("procRefs: %v", err)
	}
	for _, r := range refs {
		if !strings.HasPrefix(r.target, root+string(filepath.Separator)) && r.target != root {
			t.Errorf("ref outside the scan root leaked in: %+v", r)
		}
	}
}

// The boundary the in-use filter turns on. A prefix test on the string alone
// would call a process sitting in /scratch-backup a reference into /scratch,
// and pin an entry no one is using.
func TestAppendIfUnder_SiblingSharingANamePrefixIsNotUnderRoot(t *testing.T) {
	for _, target := range []string{"/scratch-backup/foo", "/scratchfoo", "/scratch2"} {
		if refs := appendIfUnder(nil, "/scratch", procRef{pid: "1", via: "cwd", target: target}); len(refs) != 0 {
			t.Errorf("%s was taken as a reference into /scratch: %v", target, refs)
		}
	}
	for _, target := range []string{"/scratch", "/scratch/foo", "/scratch/foo/deep/run.log"} {
		if refs := appendIfUnder(nil, "/scratch", procRef{pid: "1", via: "cwd", target: target}); len(refs) != 1 {
			t.Errorf("%s was dropped, but it is under /scratch", target)
		}
	}
}

// A process whose cwd is the scan root itself pins no top-level child; treating
// it as one would make a single such agent retain everything.
func TestHeldTopLevel_ReferenceToTheRootItselfPinsNothing(t *testing.T) {
	held := heldTopLevel("/scratch", []procRef{
		{pid: "1", via: "cwd", target: "/scratch"},
		{pid: "2", via: "cwd", target: "/scratch/foo"},
	})
	if len(held) != 1 || held["foo"] == "" {
		t.Fatalf("held = %v, want just foo", held)
	}
}

// A reference ten levels down pins the top-level child it is under, because that
// child is what a plan decides over.
func TestHeldTopLevel_MapsADeepReferenceToItsTopLevelChild(t *testing.T) {
	held := heldTopLevel("/scratch", []procRef{
		{pid: "7", via: "fd", target: "/scratch/factory/logs/2026/08/run.log"},
	})
	if _, ok := held["factory"]; !ok {
		t.Fatalf("a reference under factory/ did not pin factory; got %v", held)
	}
}

// An unlinked-but-open file still pins where it lived.
func TestAppendIfUnder_StripsTheDeletedSuffix(t *testing.T) {
	refs := appendIfUnder(nil, "/scratch", procRef{pid: "3", via: "fd", target: "/scratch/gone/run.log (deleted)"})
	if len(refs) != 1 {
		t.Fatalf("a deleted-but-open fd under the root was dropped: %v", refs)
	}
	if refs[0].target != "/scratch/gone/run.log" {
		t.Errorf("target = %q, want the suffix stripped", refs[0].target)
	}
}

// An empty in-use set and an unreadable process table are the same value, and
// the second one would report every entry as idle. Here the procfs mount holds
// one plausible-looking process that is not this one.
func TestProcRefsIn_ProcTableWithoutThisProcessRefused(t *testing.T) {
	fake := t.TempDir()
	other := strconv.Itoa(os.Getpid() + 1)
	mkdir(t, filepath.Join(fake, other, "fd"))
	mkdir(t, filepath.Join(fake, "not-a-pid"))

	_, err := procRefsIn(fake, t.TempDir())
	if err == nil {
		t.Fatal("a process table that does not name this process produced an in-use set")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Errorf("error does not name the pid it looked for: %v", err)
	}
}

// The same table WITH this process is accepted, so the refusal above is about
// the missing self and not about the fixture being synthetic.
func TestProcRefsIn_ProcTableNamingThisProcessAccepted(t *testing.T) {
	fake := t.TempDir()
	mkdir(t, filepath.Join(fake, strconv.Itoa(os.Getpid()), "fd"))

	if _, err := procRefsIn(fake, t.TempDir()); err != nil {
		t.Fatalf("procRefsIn: %v", err)
	}
}
