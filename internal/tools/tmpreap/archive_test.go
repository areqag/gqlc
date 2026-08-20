package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
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
	if stats.binary != 1 {
		t.Errorf("binary = %d, want 1", stats.binary)
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
	if stats.large != 1 {
		t.Errorf("large = %d, want 1", stats.large)
	}
	if stats.truncated {
		t.Error("one oversize file truncated the whole archive")
	}
	if got := archiveNames(t, dest); !slices.Equal(got, []string{"mixed/small.log"}) {
		t.Errorf("archive holds %v, want just the small file", got)
	}
}

func TestIsText(t *testing.T) {
	if !isText([]byte("plain log output\n")) {
		t.Error("text was classified binary")
	}
	if isText([]byte{'M', 'Z', 0x00, 0x90}) {
		t.Error("a NUL-bearing file was classified text")
	}
}
