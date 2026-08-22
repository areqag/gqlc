package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func archiveNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("close archive: %v", err)
		}
	}()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
	slices.Sort(names)
	return names
}

// The archive destination cannot live inside the tree it is insurance against.
func TestArchiveEntries_DestinationInsideRootRefused(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "scratch", "run.log"), "x\n")
	dest := filepath.Join(root, "archive.tar.gz")

	_, err := archiveEntries(dest, root, []entry{{path: filepath.Join(root, "scratch"), kind: kindPlain}}, archiveLimits{maxFileBytes: 1 << 20, maxTotalBytes: 1 << 20})
	if err == nil {
		t.Fatal("an archive written into the scan root was accepted; the reap would delete its own record")
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("the refused archive was created anyway")
	}
}

func TestArchiveEntries_TakesTextAndSkipsBinary(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, "factory")
	writeFile(t, filepath.Join(scratch, "run.log"), "line one\nline two\n")
	writeFile(t, filepath.Join(scratch, "nested", "notes.md"), "# notes\n")
	if err := os.WriteFile(filepath.Join(scratch, "cache.bin"), []byte{0x7f, 0x45, 0x4c, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "reap.tar.gz")

	stats, err := archiveEntries(dest, root, []entry{{path: scratch, kind: kindPlain}}, archiveLimits{maxFileBytes: 1 << 20, maxTotalBytes: 1 << 20})
	if err != nil {
		t.Fatalf("archiveEntries: %v", err)
	}
	if stats.binary.files != 1 {
		t.Errorf("binary = %d, want 1", stats.binary.files)
	}
	got := archiveNames(t, dest)
	want := []string{"factory/nested/notes.md", "factory/run.log"}
	if !slices.Equal(got, want) {
		t.Errorf("archive holds %v, want %v", got, want)
	}
}

// A worktree that reached the plan is clean by construction, so its content is
// in the object store already and archiving the checkout would multiply the
// tarball by every worktree reaped.
func TestArchiveEntries_SkipsWorktrees(t *testing.T) {
	root := t.TempDir()
	wt := filepath.Join(root, "gqlc-landed")
	writeFile(t, filepath.Join(wt, "main.go"), "package main\n")
	dest := filepath.Join(t.TempDir(), "reap.tar.gz")

	stats, err := archiveEntries(dest, root, []entry{{path: wt, kind: kindWorktree}}, archiveLimits{maxFileBytes: 1 << 20, maxTotalBytes: 1 << 20})
	if err != nil {
		t.Fatalf("archiveEntries: %v", err)
	}
	if stats.files != 0 {
		t.Errorf("files = %d, want 0: a clean worktree's content is already committed", stats.files)
	}
}

// Truncation has to be reported, because the caller refuses to delete over an
// incomplete record — silently short insurance is worse than none.
func TestArchiveEntries_TruncationIsReported(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, "big")
	writeFile(t, filepath.Join(scratch, "a.log"), strings.Repeat("a\n", 4096))
	writeFile(t, filepath.Join(scratch, "b.log"), strings.Repeat("b\n", 4096))
	dest := filepath.Join(t.TempDir(), "reap.tar.gz")

	stats, err := archiveEntries(dest, root, []entry{{path: scratch, kind: kindPlain}}, archiveLimits{maxFileBytes: 1 << 20, maxTotalBytes: 100})
	if err != nil {
		t.Fatalf("archiveEntries: %v", err)
	}
	if !stats.truncated {
		t.Fatal("the archive stopped at its total limit without saying so")
	}
}

func TestArchiveEntries_OversizeFileSkippedNotTruncating(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, "mixed")
	writeFile(t, filepath.Join(scratch, "huge.log"), strings.Repeat("h\n", 4096))
	writeFile(t, filepath.Join(scratch, "small.log"), "small\n")
	dest := filepath.Join(t.TempDir(), "reap.tar.gz")

	stats, err := archiveEntries(dest, root, []entry{{path: scratch, kind: kindPlain}}, archiveLimits{maxFileBytes: 64, maxTotalBytes: 1 << 20})
	if err != nil {
		t.Fatalf("archiveEntries: %v", err)
	}
	if stats.large.files != 1 {
		t.Errorf("large = %d, want 1", stats.large.files)
	}
	if stats.truncated {
		t.Error("one oversize file truncated the whole archive")
	}
	if got := archiveNames(t, dest); !slices.Equal(got, []string{"mixed/small.log"}) {
		t.Errorf("archive holds %v, want just the small file", got)
	}
}

// -archive-max-file is the largest size that is TAKEN, not the smallest that is
// dropped. The boundary is the whole content of the flag an operator raises and
// re-runs after reading the report, and off by one it drops a file the report
// just told them raising the limit would keep (verdict-osuz-r2, survivor A10).
func TestArchiveEntries_AFileExactlyAtTheLimitIsTaken(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, "edge")
	const body = "exactly sixteen\n"
	writeFile(t, filepath.Join(scratch, "at.log"), body)
	writeFile(t, filepath.Join(scratch, "over.log"), body+"x")
	dest := filepath.Join(t.TempDir(), "reap.tar.gz")

	stats, err := archiveEntries(dest, root, []entry{{path: scratch, kind: kindPlain}},
		archiveLimits{maxFileBytes: int64(len(body)), maxTotalBytes: 1 << 20})
	if err != nil {
		t.Fatalf("archiveEntries: %v", err)
	}
	if got := archiveNames(t, dest); !slices.Equal(got, []string{"edge/at.log"}) {
		t.Errorf("archive holds %v, want just the file whose size equals the limit", got)
	}
	if stats.large.files != 1 {
		t.Errorf("large = %d, want 1: only the file one byte OVER the limit is a drop", stats.large.files)
	}
}

// The two oversize cases are different losses: a build artefact nobody wanted a
// copy of, and the agent log this archive exists for. Counting them together
// would report the 24 MiB text file on the live /tmp as one of 47 oversize
// files, 46 of which are binaries nobody would look for.
func TestArchiveEntries_OversizeBinaryIsNotCountedAsLostText(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, "mixed")
	writeFile(t, filepath.Join(scratch, "agent.log"), strings.Repeat("log line\n", 512))
	if err := os.WriteFile(filepath.Join(scratch, "cache.o"),
		append([]byte{0x7f, 'E', 'L', 'F', 0x00}, make([]byte, 4096)...), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "reap.tar.gz")

	stats, err := archiveEntries(dest, root, []entry{{path: scratch, kind: kindPlain}},
		archiveLimits{maxFileBytes: 64, maxTotalBytes: 1 << 20})
	if err != nil {
		t.Fatalf("archiveEntries: %v", err)
	}
	if stats.large.files != 1 {
		t.Errorf("large = %d, want 1: only the oversize TEXT file is a recoverable artefact lost", stats.large.files)
	}
	if stats.binary.files != 1 {
		t.Errorf("binary = %d, want 1: the oversize binary is still a file deleted without a copy", stats.binary.files)
	}
	if want := int64(512 * len("log line\n")); stats.large.bytes != want {
		t.Errorf("largeBytes = %d, want %d", stats.large.bytes, want)
	}
	if stats.binary.bytes != 4101 {
		t.Errorf("binaryBytes = %d, want 4101", stats.binary.bytes)
	}
	if got := stats.large.paths; len(got) != 1 || !strings.HasSuffix(got[0], "agent.log") {
		t.Errorf("largePaths = %v, want the oversize text file named", got)
	}
}

// A count says a log was lost; a path says which. The list is capped, and the
// cap has to be visible to the caller or it reports "5" for any number above 5.
func TestArchiveEntries_NamesTheFirstOversizeFilesAndCountsTheRest(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, "logs")
	const n = pathsListed + 3
	for i := range n {
		writeFile(t, filepath.Join(scratch, fmt.Sprintf("run%d.log", i)), strings.Repeat("x\n", 64))
	}
	dest := filepath.Join(t.TempDir(), "reap.tar.gz")

	stats, err := archiveEntries(dest, root, []entry{{path: scratch, kind: kindPlain}},
		archiveLimits{maxFileBytes: 16, maxTotalBytes: 1 << 20})
	if err != nil {
		t.Fatalf("archiveEntries: %v", err)
	}
	if stats.large.files != n {
		t.Errorf("large = %d, want %d: the count is of all of them, not of the named ones", stats.large.files, n)
	}
	if len(stats.large.paths) != pathsListed {
		t.Errorf("largePaths holds %d paths, want the list capped at %d", len(stats.large.paths), pathsListed)
	}
}

// The only silent verdict is the one with nothing left to delete. EACCES is not
// a race — another uid's file inside a directory this uid owns is the ordinary
// state of a shared /tmp — and through round 2 every read failure was folded
// into one silent category on the strength of the racy reading alone
// (verdict-osuz-r2, blocking 1).
func TestClassifyFailure_OnlyAVanishedFileIsSilent(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want takeVerdict
	}{
		{"removed between the walk and the read", fs.ErrNotExist, takeVanished},
		{"another uid's file in a directory this uid owns", fs.ErrPermission, takeUnreadable},
		{"a read that failed for any other reason", io.ErrUnexpectedEOF, takeUnreadable},
	} {
		if got := classifyFailure(tc.err); got != tc.want {
			t.Errorf("classifyFailure(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A file whose head cannot be read is classified by neither heuristic: claiming
// it as text would promise a copy that is not in the tarball, and writing it off
// as binary would file a lost transcript under "by design". Nothing reddened
// when the open failure returned text (verdict-osuz-r2, survivor A9).
func TestClassifyHead_AnUnreadableHeadIsNotClaimedEitherWay(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file, so the fixture cannot construct the case")
	}
	path := filepath.Join(t.TempDir(), "oversize.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("readable text\n", 1024)), 0o000); err != nil {
		t.Fatal(err)
	}
	if got := classifyHead(path); got != takeUnreadable {
		t.Errorf("classifyHead of a file that cannot be opened = %q, want %q", got, takeUnreadable)
	}
}

// The head read is the other way an oversize file goes unclassified, and it is
// the one no fixture can build out of a real file: the open succeeds and the
// read fails. A zero-byte head holds no NUL, so the "yielded nothing" arm is
// the only thing standing between a failed read and the verdict "text, and a
// copy is in the tarball".
func TestClassifyReader_AReadThatYieldedNothingIsAFailureNotText(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    io.Reader
		want takeVerdict
	}{
		{"a read that failed having produced nothing", failingReader{err: io.ErrUnexpectedEOF}, takeUnreadable},
		{"a file truncated away under the walk", failingReader{err: fs.ErrNotExist}, takeVanished},
		{"a short read that produced text", strings.NewReader("half a log line"), takeLarge},
		{"a short read that produced a NUL", strings.NewReader("MZ\x00\x90"), takeBinary},
	} {
		if got := classifyReader(tc.r); got != tc.want {
			t.Errorf("classifyReader(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestIsText(t *testing.T) {
	if !isText([]byte("plain log output\n")) {
		t.Error("text was classified binary")
	}
	if isText([]byte{'M', 'Z', 0x00, 0x90}) {
		t.Error("a NUL-bearing file was classified text")
	}
}
