package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// refuseNonScratchRoot decides whether -root may be deleted from. It answers for
// the deleting mode only; see run.
//
// The hole it closes: `just tmp-reap` takes the root as a POSITIONAL argument,
// so `just tmp-reap apply ~` is one typo away from running the decision table
// over $HOME — where Downloads, Pictures and Videos are uid-owned, non-dot,
// hold no .git and are older than the age threshold, which is every gate this
// tool has (bd gqlc-osuz, round 1). Nothing anywhere else constrains the root:
// readPressure statfs's whatever it is handed.
//
// It is NOT the mount-point test (st_dev(root) != st_dev(parent)) proposed in
// that review, because measurement on the machine this tool was written for
// shows it wrong in both directions:
//
//	/tmp        dev 49     parent 64769   accepts   (correct)
//	/home       dev 64770  parent 64769   ACCEPTS   (144 GiB of user data)
//	/run        dev 26     parent 64769   ACCEPTS   (live session state)
//	/var/tmp    dev 64769  parent 64769   REFUSES   (a designated scratch dir)
//
// Being a separate filesystem is not what makes a directory disposable, and a
// host whose /tmp is a plain directory on / — common, and the default on
// several distributions — would have its primary use case refused. What makes a
// directory disposable is that the host designates it for temporary files.
func refuseNonScratchRoot(root string) error {
	// Home first, and independently of the list below, because the reported
	// accident is specifically ~ and because $HOME is the one directory whose
	// loss is not recoverable from anywhere. It is a second clause and not a
	// special case of the first: a host that names ~/tmp in TMPDIR would
	// otherwise licence a reap of a path inside the home directory.
	if home := resolvedHome(); home != "" && (under(root, home) || under(home, root)) {
		return fmt.Errorf("-apply refused: %s is on the same path chain as the home directory %s, "+
			"and everything this tool reaps it reaps recursively; re-run without -apply to see the plan", root, home)
	}
	dirs := scratchDirs()
	if slices.ContainsFunc(dirs, func(dir string) bool { return under(root, dir) }) {
		return nil
	}
	where := strings.Join(dirs, ", ")
	if where == "" {
		where = "none of them resolvable on this host"
	}
	return fmt.Errorf("-apply refused: %s is not under a directory this host designates for temporary "+
		"files (%s), so it is not scratch; re-run without -apply to see the plan", root, where)
}

// scratchDirs is where this host puts temporary files. os.TempDir is $TMPDIR
// when set and /tmp otherwise; /tmp and /var/tmp are listed unconditionally
// because a TMPDIR pointing elsewhere does not stop /tmp being scratch, and
// -root defaults to /tmp.
//
// Every candidate is resolved, because run resolves -root before comparing and
// an unresolved candidate would never match on a host whose /tmp is a symlink.
// A candidate that does not resolve is dropped rather than compared literally:
// the failure direction of this whole function is refusal.
func scratchDirs() []string {
	var out []string
	for _, dir := range []string{os.TempDir(), "/tmp", "/var/tmp"} {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		if !slices.Contains(out, resolved) {
			out = append(out, resolved)
		}
	}
	return out
}

// resolvedHome returns the user's home directory, or "" when there is none to
// compare against. os.UserHomeDir reads $HOME on unix.
func resolvedHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		return resolved
	}
	return home
}

// under reports whether path is dir or is inside it. The separator is part of
// the prefix on purpose: without it /tmpfoo reads as being under /tmp, which
// would hand -apply a sibling that merely shares a name prefix. It is appended
// only when dir does not already end in one, so that "/" is every path's
// ancestor rather than nothing's.
func under(path, dir string) bool {
	if path == dir {
		return true
	}
	if !strings.HasSuffix(dir, string(filepath.Separator)) {
		dir += string(filepath.Separator)
	}
	return strings.HasPrefix(path, dir)
}
