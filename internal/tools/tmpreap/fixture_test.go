package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every fixture in this package is built inside t.TempDir(). Nothing here reads
// or writes the real /tmp beyond that, and no test calls the delete path over a
// root it did not create — a reaper exercised against the live scratch
// filesystem is how the work it was written to protect gets lost.

// isolateGit strips the ambient git environment. These tests run under `just
// test`, which runs from a pre-push hook, and GIT_DIR and friends leak in from
// there: without this a fixture's `git status` reports on the outer repository.
func isolateGit(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_") {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatalf("unset %s: %v", name, err)
			}
		}
	}
	// A developer's ~/.gitconfig can set init.defaultBranch, commit.gpgsign or
	// core.hooksPath, all of which change what these fixtures do.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "fixture")
	t.Setenv("GIT_AUTHOR_EMAIL", "fixture@invalid")
	t.Setenv("GIT_COMMITTER_NAME", "fixture")
	t.Setenv("GIT_COMMITTER_EMAIL", "fixture@invalid")
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(t.Context(), dir, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return out
}

// newRepo makes a one-commit repository on master.
func newRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "init", "-q", "-b", "master")
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	mustGit(t, dir, "add", "a.txt")
	mustGit(t, dir, "commit", "-qm", "init")
	return dir
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// ageTree backdates every inode under path. mtimes drive the age gate, and a
// fixture built a moment ago is inside every threshold worth testing.
func ageTree(t *testing.T, path string, when time.Time) {
	t.Helper()
	var paths []string
	if err := filepath.Walk(path, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Deepest first: setting a directory's mtime and then writing inside it
	// would move the directory's mtime forward again.
	for i := len(paths) - 1; i >= 0; i-- {
		if err := os.Chtimes(paths[i], when, when); err != nil {
			t.Fatal(err)
		}
	}
}

// stubOracle answers for a fixed set of paths, so the decision table can be
// tested without a repository behind it.
func stubOracle(states map[string]wtState) worktreeOracle {
	return func(_ context.Context, path string) (wtState, error) {
		st, ok := states[path]
		if !ok {
			return wtState{landed: true}, nil
		}
		return st, nil
	}
}

func findEntry(t *testing.T, entries []entry, name string) entry {
	t.Helper()
	for _, e := range entries {
		if filepath.Base(e.path) == name {
			return e
		}
	}
	t.Fatalf("no entry named %q in plan (%d entries)", name, len(entries))
	return entry{}
}

func hasBinary(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not on PATH: %v", name, err)
	}
	return path
}
