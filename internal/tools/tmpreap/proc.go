package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procRef is one live reference into the scan root held by a running process.
type procRef struct {
	pid    string
	via    string // cwd, exe, or fd
	target string
}

// procRefs reads every running process's working directory, executable and open
// file descriptors, and keeps the ones pointing into root.
//
// This is the strong half of the in-use test. mtime alone is weak: an agent can
// sit idle for hours between writes and still be holding a directory it will
// write to next, and reaping that directory is a silent loss.
//
// It refuses to return a set that does not contain this process, because a
// filter that silently matched nothing — a /proc that is not mounted, a kernel
// that does not export these links — would hand the classifier an empty held
// set, which reads exactly like "nothing is in use".
func procRefs(root string) ([]procRef, error) {
	return procRefsIn("/proc", root)
}

// procRefsIn is procRefs over an arbitrary procfs mount point. The parameter
// exists so the sawSelf refusal below can be tested: a real /proc always names
// the test process, which is the one case that must not be trusted blindly.
func procRefsIn(procDir, root string) ([]procRef, error) {
	return procRefsUnder(procDir, []string{root})
}

// procRefsUnder is procRefsIn over several candidate roots at once. The
// -worktrees mode asks about a registry of scattered absolute paths rather than
// one root's children, and walking /proc once per candidate would read a table
// that is moving underneath it — a process that exits between two walks is
// present for one candidate and absent for the next.
func procRefsUnder(procDir string, roots []string) ([]procRef, error) {
	dirents, err := os.ReadDir(procDir)
	if err != nil {
		return nil, fmt.Errorf("read %s to find processes using %s: %w", procDir, strings.Join(roots, ", "), err)
	}
	self := strconv.Itoa(os.Getpid())
	sawSelf := false

	var refs []procRef
	for _, de := range dirents {
		pid := de.Name()
		if _, err := strconv.Atoi(pid); err != nil {
			continue
		}
		if pid == self {
			sawSelf = true
		}
		// Errors are ignored throughout: /proc is racy by construction — a
		// process that exits mid-walk takes its whole directory with it — and
		// another user's process is unreadable, which is the same answer as
		// "holds nothing of ours" for a root we only reap our own entries from.
		for _, link := range []string{"cwd", "exe"} {
			if target, err := os.Readlink(filepath.Join(procDir, pid, link)); err == nil {
				refs = appendIfUnderAny(refs, roots, procRef{pid: pid, via: link, target: target})
			}
		}
		fds, err := os.ReadDir(filepath.Join(procDir, pid, "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if target, err := os.Readlink(filepath.Join(procDir, pid, "fd", fd.Name())); err == nil {
				refs = appendIfUnderAny(refs, roots, procRef{pid: pid, via: "fd", target: target})
			}
		}
	}
	if !sawSelf {
		return nil, fmt.Errorf("%s did not name this process (pid %s), so the in-use scan is reading "+
			"something other than the live process table and would report every entry as idle", procDir, self)
	}
	return refs, nil
}

func appendIfUnderAny(refs []procRef, roots []string, r procRef) []procRef {
	// An fd on a file that has been unlinked reads back with this suffix; the
	// path still names where it lived, which is what the prefix test needs.
	r.target = strings.TrimSuffix(r.target, " (deleted)")
	for _, root := range roots {
		if under(r.target, root) {
			return append(refs, r)
		}
	}
	return refs
}

// heldUnder maps each candidate path to a description of one live process
// referencing it or something inside it.
//
// It counts a reference to the candidate ITSELF, which is where it differs from
// heldTopLevel: an agent whose cwd is /tmp pins no particular child there and is
// rightly dropped, but an agent whose cwd IS a worktree is the exact occupant
// this mode exists to protect. That worktree is a seat's whole working
// directory, not one entry among its siblings (bd gqlc-24wf).
func heldUnder(roots []string, refs []procRef) map[string]string {
	held := make(map[string]string)
	for _, r := range refs {
		for _, root := range roots {
			if _, seen := held[root]; seen || !under(r.target, root) {
				continue
			}
			held[root] = fmt.Sprintf("pid %s has it as %s (%s)", r.pid, r.via, r.target)
		}
	}
	return held
}

// heldTopLevel maps the name of each top-level child of root to a description of
// one live process referencing something inside it. Keyed by name because the
// entries a plan decides over are exactly root's top-level children: a reference
// ten levels down still pins the child it is under.
func heldTopLevel(root string, refs []procRef) map[string]string {
	held := make(map[string]string)
	for _, r := range refs {
		// A reference to the root itself — an agent whose cwd is /tmp — pins no
		// top-level child, and dropping it here is why one such process does not
		// make the whole root read as in use.
		rel, err := filepath.Rel(root, r.target)
		if err != nil || rel == "." {
			continue
		}
		name, _, _ := strings.Cut(rel, string(filepath.Separator))
		if name == "" {
			continue
		}
		if _, seen := held[name]; seen {
			continue
		}
		held[name] = fmt.Sprintf("pid %s has it as %s (%s)", r.pid, r.via, r.target)
	}
	return held
}
