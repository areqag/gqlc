package main

import (
	"bytes"
	"fmt"
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
	// -apply is refused outside a designated temporary directory and inside the
	// home directory (refuseNonScratchRoot), and t.TempDir() answers neither
	// question the same way twice: `just test` sets GOTMPDIR to .bin/gotmp
	// inside the repository, which puts every fixture under $HOME and outside
	// every scratch directory, while a bare `go test` puts it under /tmp.
	// Declaring both here makes the apply fixtures deterministic under either,
	// and stops these tests depending on the developer's real home directory.
	useScratchRoot(t, base)
	t.Setenv("HOME", mkdir(t, filepath.Join(base, "home")))
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
	// Everything here is small text, so the unrecoverable disclosure must stay
	// quiet: a warning printed on every run is one nobody reads on the run that
	// matters.
	if strings.Contains(out, "unrecoverable") {
		t.Errorf("a reap that archived everything still warned about unrecoverable files:\n%s", out)
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

// The per-file limit decides what survives a reap, so its default is a safety
// decision and not a tuning knob. 8 MiB, which this shipped with, is below the
// largest text file on the filesystem it was written for (24.2 MiB) and only
// 26% above the largest agent .output log there (6.6 MiB, mid-session). The
// assertion is a floor rather than the exact number: what has to hold is that
// an agent log fits.
func TestParseOptions_ArchiveMaxFileFitsAnAgentLog(t *testing.T) {
	var errOut bytes.Buffer
	o, err := parseOptions(nil, &errOut)
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if o.archiveL.maxFileBytes < 32<<20 {
		t.Errorf("-archive-max-file defaults to %s; an agent log above it is deleted with no copy",
			humanBytes(o.archiveL.maxFileBytes))
	}
	if o.archiveL.maxFileBytes > o.archiveL.maxTotalBytes {
		t.Errorf("-archive-max-file (%s) is above -archive-max-total (%s), so the per-file limit can never bind",
			humanBytes(o.archiveL.maxFileBytes), humanBytes(o.archiveL.maxTotalBytes))
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

// The archive drops two classes of file — text over -archive-max-file, and
// anything binary — and deletes both regardless. Round 1 counted them into
// archiveStats and printed neither, so a 12 MiB plain-text agent log left
// "archived 1 text file(s)" and 750 B behind it (bd gqlc-osuz). A count only
// the tests read is silence.
func TestRun_ApplyNamesWhatItCouldNotArchive(t *testing.T) {
	root, repo, archive := scratchWorld(t)
	oversize := filepath.Join(root, "factory", "logs", "agent.log")
	writeFile(t, oversize, strings.Repeat("an agent log line\n", 4096))
	if err := os.WriteFile(filepath.Join(root, "factory", "cache.o"), []byte{0x7f, 'E', 'L', 'F', 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	ageTree(t, filepath.Join(root, "factory"), time.Now().Add(-72*time.Hour))

	out, err := runTool(t,
		"-root", root, "-repo", repo, "-base", "master", "-age", "12h",
		"-archive", archive, "-archive-max-file", "1024", "-apply")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "unrecoverable") {
		t.Errorf("the run deleted files it did not archive without calling them unrecoverable:\n%s", out)
	}
	if !strings.Contains(out, oversize) {
		t.Errorf("the oversize TEXT file %s is not named, so nobody can tell which log was lost:\n%s", oversize, out)
	}
	if !strings.Contains(out, "1 text file(s)") || !strings.Contains(out, "over -archive-max-file") {
		t.Errorf("the oversize text file is not counted as one:\n%s", out)
	}
	binary := filepath.Join(root, "factory", "cache.o")
	if !strings.Contains(out, "1 binary file(s)") {
		t.Errorf("the binary file that was deleted without a copy is not counted:\n%s", out)
	}
	// A count says a build artefact went; only a path says whether it was a
	// build artefact. The head heuristic reads 8 KiB, so a transcript with one
	// stray NUL in it is reported as binary "by design" — and while binary drops
	// carried no paths, nobody could tell that from the report (verdict-osuz-r2,
	// blocking 2).
	if !strings.Contains(out, binary) {
		t.Errorf("the binary file %s is counted but not named, so the operator cannot tell what went:\n%s", binary, out)
	}
	// The premise: it really was deleted, and really is not in the tarball.
	if _, statErr := os.Stat(oversize); !os.IsNotExist(statErr) {
		t.Errorf("the fixture never exercised the case — %s survived (err=%v)", oversize, statErr)
	}
	if names := archiveNames(t, archive); slices.ContainsFunc(names, func(n string) bool {
		return strings.HasSuffix(n, "agent.log") || strings.HasSuffix(n, "cache.o")
	}) {
		t.Errorf("the fixture never exercised the case — the archive holds %v", names)
	}
}

// A file that cannot be READ is deleted all the same: os.RemoveAll needs write
// permission on the directory, not read permission on the file. Through round 2
// such a file was archived by nothing, counted by nothing and named by nothing —
// the third drop category, and the one the branch was REVISEd for in its first
// two forms (verdict-osuz-r2, blocking 1).
//
// EACCES is not the racy case the code's comment claimed it was. Another uid's
// file inside a directory this uid owns is the ordinary state of a shared /tmp.
func TestRun_ApplyNamesTheFilesItCouldNotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file, so the fixture cannot construct the case")
	}
	root, repo, archive := scratchWorld(t)
	unreadable := filepath.Join(root, "factory", "logs", "handoff.md")
	writeFile(t, unreadable, "notes nobody will ever see again\n")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	ageTree(t, filepath.Join(root, "factory"), time.Now().Add(-72*time.Hour))

	out, err := runTool(t,
		"-root", root, "-repo", repo, "-base", "master", "-age", "12h",
		"-archive", archive, "-apply")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, unreadable) {
		t.Errorf("%s was deleted with no copy, no count and no name:\n%s", unreadable, out)
	}
	if !strings.Contains(out, "unrecoverable") {
		t.Errorf("the unreadable file is not reported under the unrecoverable heading:\n%s", out)
	}
	// The premise: it really was deleted, and really is not in the tarball.
	if _, statErr := os.Stat(unreadable); !os.IsNotExist(statErr) {
		t.Errorf("the fixture never exercised the case — %s survived (err=%v)", unreadable, statErr)
	}
	if names := archiveNames(t, archive); slices.ContainsFunc(names, func(n string) bool {
		return strings.HasSuffix(n, "handoff.md")
	}) {
		t.Errorf("the fixture never exercised the case — the archive holds %v", names)
	}
}

// The mirror of the row above, and the reason the report cannot simply count
// every failure: a file that vanished between the walk and the read is not a
// loss, because there is nothing left to delete. Reporting it would put a
// permanent scary line on a run that lost nothing, and a warning printed on
// every clean run is one nobody reads on the run that matters.
func TestRun_ApplyIsSilentAboutAFileThatVanished(t *testing.T) {
	root, repo, archive := scratchWorld(t)
	ageTree(t, filepath.Join(root, "factory"), time.Now().Add(-72*time.Hour))

	out, err := runTool(t,
		"-root", root, "-repo", repo, "-base", "master", "-age", "12h",
		"-archive", archive, "-apply")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "NOT archived") {
		t.Errorf("a reap that lost nothing printed the unrecoverable heading anyway:\n%s", out)
	}
}

// The report is the whole promise: these files are gone the moment the next
// loop runs. Three properties, one per surviving mutation of round 2 — the count
// is of ALL of them and not of the named ones, the cap on the names discloses
// its own remainder (M10), and a category with nothing in it prints no line and
// no heading (M8, M9).
func TestReportUnarchived_CountsInFullNamesToTheCapAndDisclosesTheRest(t *testing.T) {
	large := drop{files: pathsListed + 3, bytes: 900}
	for i := range pathsListed {
		large.paths = append(large.paths, fmt.Sprintf("/tmp/agent/run%d.log", i))
	}
	var out bytes.Buffer
	reportUnarchived(&printer{w: &out},
		archiveStats{large: large, unreadable: drop{files: 1, bytes: 7, paths: []string{"/tmp/agent/handoff.md"}}},
		archiveLimits{maxFileBytes: 1024, maxTotalBytes: 1 << 20})
	got := out.String()

	for _, want := range []string{
		"NOT archived",
		fmt.Sprintf("%d text file(s)", pathsListed+3),
		"/tmp/agent/run0.log",
		fmt.Sprintf("/tmp/agent/run%d.log", pathsListed-1),
		"... and 3 more",
		"1 unreadable file(s)",
		"/tmp/agent/handoff.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report does not say %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "binary") {
		t.Errorf("a category with nothing in it printed a line, so a clean reap carries a warning nobody will read:\n%s", got)
	}
}

// The heading belongs to the categories, not to the run. A reap that lost
// nothing must print neither.
func TestReportUnarchived_SaysNothingWhenNothingWasDropped(t *testing.T) {
	var out bytes.Buffer
	reportUnarchived(&printer{w: &out}, archiveStats{files: 3, bytes: 99}, archiveLimits{maxFileBytes: 1024, maxTotalBytes: 1 << 20})
	if out.Len() != 0 {
		t.Errorf("a reap that archived everything still warned about unrecoverable files:\n%s", out.String())
	}
}

// -apply is the mode that deletes, and `just tmp-reap apply <root>` takes the
// root positionally, so the guard has to be in the tool and not in the recipe.
func TestRun_ApplyInsideTheUsersHomeRefused(t *testing.T) {
	root, repo, archive := scratchWorld(t)
	// root is <base>/scratch, so this puts the scan root inside the home
	// directory exactly the way `just tmp-reap apply ~` would.
	t.Setenv("HOME", filepath.Dir(root))

	out, err := runTool(t,
		"-root", root, "-repo", repo, "-base", "master", "-age", "12h",
		"-archive", archive, "-apply")
	if err == nil {
		t.Fatal("-apply ran over a root inside the home directory")
	}
	if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("the refusal does not say the root was inside the home directory: %v", err)
	}
	for _, name := range []string{"factory", "probe937r3", "gqlc-landed"} {
		if _, statErr := os.Stat(filepath.Join(root, name)); statErr != nil {
			t.Errorf("%s was deleted despite the refusal: %v", name, statErr)
		}
	}
	if strings.Contains(out, "reclaimed") {
		t.Error("the report claims it reclaimed space after refusing to run")
	}
}

// The refusal is scoped to -apply. `just tmp-report` and the `check-tmp` gate
// are pointed at arbitrary directories on purpose — including $HOME, and
// including a hosted runner's root disk — and they are read-only. A guard that
// also refused those would retire the half of this tool that pays.
func TestRun_DryRunIsNotConstrainedByTheApplyGuard(t *testing.T) {
	root, repo, _ := scratchWorld(t)
	t.Setenv("HOME", filepath.Dir(root))

	if err := refuseNonScratchRoot(root); err == nil {
		t.Fatal("the fixture never exercised the case — -apply would have been allowed over this root")
	}
	if _, err := runTool(t, "-root", root, "-repo", repo, "-base", "master", "-age", "12h"); err != nil {
		t.Fatalf("a read-only report was refused by the -apply guard: %v", err)
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
