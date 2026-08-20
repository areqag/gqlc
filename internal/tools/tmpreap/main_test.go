package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// scratchWorld builds the shape a shared /tmp is actually in: agent scratch
// directories, worktrees in three states, and the machine's own entries beside
// them. Returns the scan root, the repository the worktrees belong to, and a
// path outside the root for the archive.
func scratchWorld(t *testing.T) (root, repo, archive string) {
	t.Helper()
	isolateGit(t)
	base := t.TempDir()
	repo = newRepo(t, filepath.Join(base, "repo"))
	root = mkdir(t, filepath.Join(base, "scratch"))
	archive = filepath.Join(base, "reaped.tar.gz")

	// Abandoned agent scratch: the larger half of what actually accumulates.
	writeFile(t, filepath.Join(root, "factory", "logs", "run.log"), "an agent's log\n")
	writeFile(t, filepath.Join(root, "probe937r3", "notes.md"), "probe notes\n")

	// Squash-landed, clean: the branch head is on no remote ref and is not an
	// ancestor of master, which is what every merged PR here looks like.
	landed := filepath.Join(root, "gqlc-landed")
	mustGit(t, repo, "worktree", "add", "-q", landed, "-b", "feat/landed")
	writeFile(t, filepath.Join(landed, "b.txt"), "b\n")
	mustGit(t, landed, "add", "b.txt")
	mustGit(t, landed, "commit", "-qm", "add b")
	writeFile(t, filepath.Join(repo, "b.txt"), "b\n")
	mustGit(t, repo, "add", "b.txt")
	mustGit(t, repo, "commit", "-qm", "add b (#1)")

	// Unlanded: real work, nowhere else.
	open := filepath.Join(root, "gqlc-open")
	mustGit(t, repo, "worktree", "add", "-q", open, "-b", "feat/open")
	writeFile(t, filepath.Join(open, "c.txt"), "c\n")
	mustGit(t, open, "add", "c.txt")
	mustGit(t, open, "commit", "-qm", "add c")

	// Dirty: landed content plus an uncommitted file.
	dirty := filepath.Join(root, "gqlc-dirty")
	mustGit(t, repo, "worktree", "add", "-q", dirty, "-b", "feat/dirty")
	writeFile(t, filepath.Join(dirty, "unsaved.md"), "not committed anywhere\n")

	// The machine's.
	writeFile(t, filepath.Join(root, ".X11-unix", "X0"), "")
	writeFile(t, filepath.Join(root, "systemd-private-abc-upower.service-XYZ", "tmp", "x"), "")

	ageTree(t, root, time.Now().Add(-72*time.Hour))

	// Written after the backdating: an agent that is still working.
	writeFile(t, filepath.Join(root, "impl-osuz", "in-progress.md"), "still going\n")
	return root, repo, archive
}

func runTool(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(t.Context(), args, &out, &errOut)
	if errOut.Len() != 0 {
		t.Logf("stderr: %s", errOut.String())
	}
	t.Logf("stdout:\n%s", out.String())
	return out.String(), err
}

func TestRun_ApplyReclaimsOnlyTheProvablyAbandoned(t *testing.T) {
	root, repo, archive := scratchWorld(t)

	out, err := runTool(t,
		"-root", root, "-repo", repo, "-base", "master",
		"-age", "12h", "-archive", archive, "-apply")
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	gone := []string{"factory", "probe937r3", "gqlc-landed"}
	kept := []string{"gqlc-open", "gqlc-dirty", "impl-osuz", ".X11-unix", "systemd-private-abc-upower.service-XYZ"}
	for _, name := range gone {
		if _, statErr := os.Stat(filepath.Join(root, name)); !os.IsNotExist(statErr) {
			t.Errorf("%s survived the reap (err=%v)", name, statErr)
		}
	}
	for _, name := range kept {
		if _, statErr := os.Stat(filepath.Join(root, name)); statErr != nil {
			t.Errorf("%s was deleted: %v", name, statErr)
		}
	}

	// The reaped branch's commits are still reachable.
	if got := mustGit(t, repo, "cat-file", "-t", mustGit(t, repo, "rev-parse", "feat/landed")); got != "commit" {
		t.Errorf("the reaped worktree's branch head is %q, want a commit", got)
	}

	names := archiveNames(t, archive)
	for _, want := range []string{"factory/logs/run.log", "probe937r3/notes.md"} {
		if !slices.Contains(names, want) {
			t.Errorf("%s is not in the archive %v", want, names)
		}
	}
	if !strings.Contains(out, "inodes") {
		t.Error("the report never mentions inodes, which is the currency that ran out")
	}
}

func TestRun_DryRunDeletesNothing(t *testing.T) {
	root, repo, _ := scratchWorld(t)

	out, err := runTool(t, "-root", root, "-repo", repo, "-base", "master", "-age", "12h")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, name := range []string{"factory", "probe937r3", "gqlc-landed", "gqlc-open", "gqlc-dirty", "impl-osuz"} {
		if _, statErr := os.Stat(filepath.Join(root, name)); statErr != nil {
			t.Errorf("%s was deleted by a run without -apply: %v", name, statErr)
		}
	}
	if !strings.Contains(out, "nothing was deleted") {
		t.Error("a dry run did not say that nothing was deleted")
	}
	if !strings.Contains(out, "REAP") || !strings.Contains(out, "RETAIN") {
		t.Error("the plan reports neither what it would reap nor what it would keep")
	}
}

// The report has to name what is holding the filesystem, not just count it:
// "621 entries" sends nobody anywhere.
func TestRun_ReportNamesTheBiggestHolders(t *testing.T) {
	root, repo, _ := scratchWorld(t)
	writeFile(t, filepath.Join(root, "factory", "logs", "big.log"), strings.Repeat("x\n", 200_000))
	ageTree(t, filepath.Join(root, "factory"), time.Now().Add(-72*time.Hour))

	out, err := runTool(t, "-root", root, "-repo", repo, "-base", "master", "-age", "12h")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, filepath.Join(root, "factory")) {
		t.Error("the biggest reapable entry is not named in the report")
	}
	if !strings.Contains(out, filepath.Join(root, "gqlc-dirty")) {
		t.Error("a retained worktree is not named in the report, so nobody can act on it")
	}
}

func TestRun_CheckReportsPressureAndTouchesNothing(t *testing.T) {
	root, _, _ := scratchWorld(t)

	out, err := runTool(t, "-root", root, "-check")
	if err != nil {
		t.Fatalf("run -check: %v", err)
	}
	if !strings.Contains(out, "bytes") || !strings.Contains(out, "inodes") {
		t.Errorf("the check reports one currency, not both: %s", out)
	}
	if _, statErr := os.Stat(filepath.Join(root, "factory")); statErr != nil {
		t.Errorf("-check deleted something: %v", statErr)
	}
}

// A fail threshold below the warn threshold makes the warning unreachable, which
// would retire the early signal without any output saying so.
func TestParseOptions_WarnAboveFailRefused(t *testing.T) {
	var errOut bytes.Buffer
	if _, err := parseOptions([]string{"-warn", "97", "-fail", "90"}, &errOut); err == nil {
		t.Fatal("a warn threshold above the fail threshold was accepted")
	}
}

func TestRun_UnknownRootIsAnError(t *testing.T) {
	if _, err := runTool(t, "-root", filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("a root that does not exist produced a report")
	}
}

func TestDefaultArchivePath_IsOutsideTheScanRoot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/home/someone/.local/state")
	got := defaultArchivePath(time.Date(2026, 8, 19, 23, 4, 5, 0, time.UTC))
	if err := destOutsideRoot(got, "/tmp"); err != nil {
		t.Fatalf("the default archive path is inside /tmp: %v", err)
	}
	if !strings.HasSuffix(got, "20260819T230405Z.tar.gz") {
		t.Errorf("archive path = %q, want it stamped with the run time", got)
	}
}

// An archive that hit its total limit is a short record of a long deletion. The
// reap has to stop rather than delete over it, and it has to say why.
func TestRun_TruncatedArchiveDeletesNothing(t *testing.T) {
	root, repo, archive := scratchWorld(t)

	out, err := runTool(t,
		"-root", root, "-repo", repo, "-base", "master", "-age", "12h",
		// Small enough that the first file already exhausts it, so the second
		// reapable entry is left unread and the archive is short by construction.
		"-archive", archive, "-archive-max-total", "8", "-apply")
	if err == nil {
		t.Fatal("a reap ran over an archive that stopped short of recording it")
	}
	if !strings.Contains(err.Error(), "nothing was deleted") {
		t.Errorf("the error does not say the deletion was abandoned: %v", err)
	}
	for _, name := range []string{"factory", "probe937r3", "gqlc-landed"} {
		if _, statErr := os.Stat(filepath.Join(root, name)); statErr != nil {
			t.Errorf("%s was deleted despite the incomplete archive: %v", name, statErr)
		}
	}
	if strings.Contains(out, "reclaimed") {
		t.Error("the report claims it reclaimed space after refusing to delete")
	}
}
