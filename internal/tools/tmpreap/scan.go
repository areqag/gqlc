package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// kind is what a top-level child of the scan root is. It decides which safety
// questions get asked of it, and it is reported, because "528 plain scratch
// directories and 93 worktrees" is a different remediation from either alone.
type kind string

const (
	kindPlain       kind = "plain"        // ordinary agent scratch: no git repository anywhere inside
	kindWorktree    kind = "worktree"     // a worktree the tracked repo has registered
	kindForeignRepo kind = "foreign-repo" // holds a .git this tool cannot reason about
	kindSystem      kind = "system"       // the machine's, not any agent's
	kindIrregular   kind = "irregular"    // socket, symlink, fifo, device, or unstattable
)

// allKinds fixes the order kinds are reported in.
var allKinds = []kind{kindPlain, kindWorktree, kindForeignRepo, kindSystem, kindIrregular}

// entry is one top-level child of the scan root: what it is, what it costs, and
// the decision taken over it. reason is filled on both branches — an entry that
// is reaped says why it qualified, so a plan can be read without re-deriving it.
type entry struct {
	path   string
	kind   kind
	bytes  int64
	inodes int64
	newest time.Time
	reap   bool
	reason string
}

type scanConfig struct {
	root   string
	base   string
	maxAge time.Duration
	now    time.Time
	uid    int
}

// scanRoot decides over every top-level child of cfg.root.
//
// It refuses an empty root rather than returning an empty plan: every caller of
// this function compares its output against something, and a comparison handed
// an empty measurement reports success over nothing.
func scanRoot(ctx context.Context, cfg scanConfig, worktrees map[string]bool, oracle worktreeOracle, held map[string]string) ([]entry, error) {
	dirents, err := os.ReadDir(cfg.root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", cfg.root, err)
	}
	if len(dirents) == 0 {
		return nil, fmt.Errorf("%s holds no entries at all, so this scan measured nothing — check -root", cfg.root)
	}
	entries := make([]entry, 0, len(dirents))
	for _, de := range dirents {
		entries = append(entries, classify(ctx, cfg, de, worktrees, oracle, held))
	}
	return entries, nil
}

// classify runs one entry through every gate. Order is deliberate: the gates
// that keep the machine working come before the ones that cost a filesystem
// walk or a git invocation, and every gate's failure direction is RETAIN.
func classify(ctx context.Context, cfg scanConfig, de os.DirEntry, worktrees map[string]bool, oracle worktreeOracle, held map[string]string) entry {
	e := entry{path: filepath.Join(cfg.root, de.Name()), kind: kindPlain}

	info, err := de.Info()
	if err != nil {
		e.kind = kindIrregular
		e.reason = "cannot be stat'd: " + err.Error()
		return e
	}
	if reason := systemReason(de.Name(), info, cfg.uid); reason != "" {
		e.kind = kindSystem
		e.reason = reason
		return e
	}
	if reason := irregularReason(info); reason != "" {
		e.kind = kindIrregular
		e.reason = reason
		return e
	}

	u := measure(e.path, info)
	e.bytes, e.inodes, e.newest = u.bytes, u.inodes, u.newest
	if u.unreadable != "" {
		e.reason = "not fully readable (" + u.unreadable + "); a tree this cannot read is one it cannot archive"
		return e
	}

	switch {
	case worktrees[e.path]:
		e.kind = kindWorktree
		st, err := oracle(ctx, e.path)
		switch {
		case err != nil:
			e.reason = "git could not answer for it: " + err.Error()
			return e
		case st.dirty:
			e.reason = "uncommitted or untracked changes: " + st.dirtyDetail
			return e
		case !st.landed:
			e.reason = "content is not on " + cfg.base + ": " + st.landedDetail
			return e
		}
	case u.nestedGit != "":
		// Defence in depth for the worktree arm above. A worktree whose
		// registration the repo has lost, or one belonging to a repo this
		// invocation was not pointed at, reaches here instead — and holding a
		// .git is enough to refuse it without knowing whose it is.
		e.kind = kindForeignRepo
		e.reason = "holds a git repository this invocation does not track: " + u.nestedGit
		return e
	}

	if who, ok := held[de.Name()]; ok {
		e.reason = "in use: " + who
		return e
	}
	// mtime is the weak half of the in-use test on its own — an agent can idle
	// for hours between writes — which is why the /proc check above runs first
	// and why this one reads the NEWEST mtime anywhere in the tree rather than
	// the directory's own, which does not move when a file three levels down is
	// rewritten.
	if age := cfg.now.Sub(u.newest); age < cfg.maxAge {
		e.reason = fmt.Sprintf("modified %s ago, inside the %s threshold", age.Round(time.Second), cfg.maxAge)
		return e
	}

	e.reap = true
	e.reason = fmt.Sprintf("untouched for %s, no live process inside", cfg.now.Sub(u.newest).Round(time.Minute))
	if e.kind == kindWorktree {
		e.reason += ", clean, and its content is already on " + cfg.base
	}
	return e
}

// systemNamePrefixes are names under a shared scratch filesystem that belong to
// the machine. Ownership (below) covers every one of them on this host today —
// systemd-private-* and kheaders-* are both root-owned — but the two tests fail
// in different ways, and deleting one of these takes out the desktop session or
// a running unit rather than a scratch directory.
var systemNamePrefixes = []string{
	"systemd-private-",
	"snap-private-tmp",
	"kheaders-",
	"hsperfdata_",
	"pulse-",
	"dbus-",
}

// systemReason reports why an entry is off-limits, or "" when it is a candidate.
func systemReason(name string, info fs.FileInfo, uid int) string {
	if strings.HasPrefix(name, ".") {
		// The desktop session's sockets and locks live in the dot entries of
		// /tmp: .X11-unix, .ICE-unix, .font-unix, .XIM-unix, .X0-lock.
		return "dot entry (the desktop session's sockets and locks are here)"
	}
	for _, p := range systemNamePrefixes {
		if strings.HasPrefix(name, p) {
			return "system name prefix " + strconv.Quote(p)
		}
	}
	if owner, ok := ownerUID(info); ok && owner != uid {
		return fmt.Sprintf("owned by uid %d, not by uid %d", owner, uid)
	}
	return ""
}

func ownerUID(info fs.FileInfo) (int, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}

// irregularReason refuses everything that is neither a directory nor a regular
// file. A socket in /tmp is how a live service is addressed, and a symlink's
// target is somewhere this tool never measured.
func irregularReason(info fs.FileInfo) string {
	m := info.Mode()
	switch {
	case m.IsDir(), m.IsRegular():
		return ""
	case m&fs.ModeSymlink != 0:
		return "symlink (its target was never measured here)"
	case m&fs.ModeSocket != 0:
		return "socket (a live service is addressed through it)"
	default:
		return "neither a regular file nor a directory (mode " + m.String() + ")"
	}
}

// usage is what one walk of an entry found.
type usage struct {
	bytes  int64
	inodes int64
	newest time.Time
	// nestedGit holds the path of the first .git found at any depth, "" if none.
	nestedGit string
	// unreadable holds the first read error, "" if the whole tree was readable.
	unreadable string
}

// measure walks an entry once and answers every question the gates need, so a
// tree of a million files is traversed once rather than once per question.
func measure(path string, info fs.FileInfo) usage {
	if !info.IsDir() {
		return usage{bytes: info.Size(), inodes: 1, newest: info.ModTime()}
	}
	u := usage{}
	walkErr := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if u.unreadable == "" {
				u.unreadable = err.Error()
			}
			return nil
		}
		u.inodes++
		if d.Name() == ".git" && u.nestedGit == "" {
			// Both spellings: a directory in a clone, a file in a worktree.
			u.nestedGit = p
		}
		fi, err := d.Info()
		if err != nil {
			if u.unreadable == "" {
				u.unreadable = err.Error()
			}
			return nil
		}
		if fi.Mode().IsRegular() {
			u.bytes += fi.Size()
		}
		if fi.ModTime().After(u.newest) {
			u.newest = fi.ModTime()
		}
		return nil
	})
	if walkErr != nil && u.unreadable == "" {
		u.unreadable = walkErr.Error()
	}
	return u
}
