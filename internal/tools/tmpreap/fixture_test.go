package main

import (
	"context"
	"net"
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

// useScratchRoot points the -apply guard at a fixture directory for the duration
// of one test. The guard's list is two absolute literals and reads nothing from
// the environment, so without this a fixture is scratch only when ambient
// GOTMPDIR/TMPDIR happen to put t.TempDir() under /tmp; any exported value
// moves every fixture, and each -apply row then measures the machine rather
// than the fixture. Measured — with GOTMPDIR set, five rows that pass here fail.
func useScratchRoot(t *testing.T, dir string) {
	t.Helper()
	saved := scratchCandidates
	scratchCandidates = []string{dir}
	t.Cleanup(func() { scratchCandidates = saved })
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
//
// It does not follow symlinks, and that is the whole reason it does not use
// os.Chtimes, which does. Following one backdates the TARGET — often outside
// the fixture entirely — and leaves the link's own mtime at creation time. The
// scan reads a top-level entry's age from the newest mtime in its subtree, so
// one such link keeps the entire tree above the threshold and the fixture is
// retained for a reason having nothing to do with what it was built to test
// (bd gqlc-cp8o, where it cost a run that reaped nothing and reported it as a
// deletion that had not happened).
//
// It shells out because Go's stdlib has no Lchtimes and its syscall package
// wraps neither utimensat(2) nor AT_SYMLINK_NOFOLLOW. x/sys/unix has both and
// cannot be used: an in-package test that imports third-party code takes the
// whole package out of govulncheck's call graph, which `just vuln` refuses (bd
// gqlc-m5rc). touch(1) is already a weaker dependency than the git binary every
// other fixture here needs.
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
	// One invocation for the whole tree: touch sets an inode's own timestamps
	// and never its parent directory's, so the order of the arguments is free.
	touch, err := exec.LookPath("touch")
	if err != nil {
		t.Fatalf("touch is not on PATH, and every fixture here needs it: %v", err)
	}
	args := append([]string{"-h", "-d", when.UTC().Format(time.RFC3339Nano)}, paths...)
	if out, err := exec.CommandContext(t.Context(), touch, args...).CombinedOutput(); err != nil {
		t.Fatalf("backdate %s: %v: %s", path, err, out)
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

// mkSocket leaves a bound unix socket inode at path, the way an exited
// service leaves one in /tmp.
//
// It binds elsewhere and renames, because bind(2) carries the path in
// sun_path, which is 108 bytes: a fixture under t.TempDir() with a long test
// name in it overruns that and bind fails with EINVAL. Binding at the target
// directly therefore turns a long test name into a SKIP, which is a test that
// passes having asserted nothing. The short base is TMPDIR, which testtmp
// redirects to this binary's own private directory, so the rename stays on one
// filesystem.
//
// SetUnlinkOnClose(false) is what makes the inode outlive the listener: a
// UnixListener removes its own socket file on Close by default.
func mkSocket(t *testing.T, path string) {
	t.Helper()
	short, err := os.MkdirTemp("", "s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(short); err != nil {
			t.Errorf("remove %s: %v", short, err)
		}
	})

	bound := filepath.Join(short, "k")
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "unix", bound)
	if err != nil {
		t.Fatalf("bind a unix socket at %s: %v", bound, err)
	}
	ul, ok := l.(*net.UnixListener)
	if !ok {
		t.Fatalf("listening on unix gave %T, not *net.UnixListener", l)
	}
	ul.SetUnlinkOnClose(false)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(bound, path); err != nil {
		t.Fatalf("move the bound socket to %s: %v", path, err)
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
