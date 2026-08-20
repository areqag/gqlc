package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A home directory that is not inside any temporary directory, which is what
// every real one is. t.TempDir() would sit under /tmp and make every scratch
// row on this table read as a home row.
const fixtureHome = "/home/fixture-nobody"

// The table is the guard. Each REFUSE row is a directory somebody could type
// after `just tmp-reap apply`, and /home and /run are the two the mount-point
// test proposed for this would have ACCEPTED on the machine it was proposed
// for: both are their own mounts (dev 64770 and 26 against / at 64769).
func TestRefuseNonScratchRoot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", fixtureHome)
	t.Setenv("TMPDIR", tmp)

	for _, tc := range []struct {
		name   string
		root   string
		refuse bool
	}{
		{"the designated temporary directory", tmp, false},
		{"below the designated temporary directory", filepath.Join(tmp, "agent", "scratch"), false},
		{"/tmp, whatever TMPDIR says", "/tmp", false},
		{"below /tmp", "/tmp/factory", false},
		{"/var/tmp", "/var/tmp", false},

		{"the home directory itself", fixtureHome, true},
		{"below the home directory", filepath.Join(fixtureHome, "Downloads"), true},
		{"a parent of the home directory", "/home", true},
		{"the filesystem root", "/", true},
		{"/run, a tmpfs holding live session state", "/run", true},
		{"/dev/shm, a tmpfs", "/dev/shm", true},
		{"a data mount", "/srv/data", true},
		{"a sibling sharing the /tmp name prefix", "/tmpfoo", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := refuseNonScratchRoot(tc.root)
			if tc.refuse && err == nil {
				t.Errorf("-apply was allowed over %s", tc.root)
			}
			if !tc.refuse && err != nil {
				t.Errorf("-apply was refused over the scratch directory %s: %v", tc.root, err)
			}
		})
	}
}

// The home clause is not a special case of the temporary-directory clause: a
// host that points TMPDIR inside the home directory must not thereby licence a
// reap of a path under it.
func TestRefuseNonScratchRoot_HomeBeatsATmpdirInsideIt(t *testing.T) {
	home := t.TempDir()
	inside := filepath.Join(home, "tmp")
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", inside)

	err := refuseNonScratchRoot(filepath.Join(inside, "scratch"))
	if err == nil {
		t.Fatal("-apply was allowed under a TMPDIR inside the home directory")
	}
	if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("the refusal cites the wrong clause: %v", err)
	}
}

// The home clause looks up the chain as well as down. A container or a CI image
// that puts $HOME inside the scratch filesystem makes the scratch filesystem
// undeletable, not the home directory deletable — and this is the only case
// where the ancestor half decides anything the temporary-directory clause does
// not already refuse.
func TestRefuseNonScratchRoot_AHomeInsideScratchProtectsTheScratchRoot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("HOME", filepath.Join(tmp, "home"))

	err := refuseNonScratchRoot(tmp)
	if err == nil {
		t.Fatal("-apply was allowed over a temporary directory that contains the home directory")
	}
	if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("the refusal cites the wrong clause: %v", err)
	}
}

// A refusal that does not say what would have been acceptable sends the reader
// to the source.
func TestRefuseNonScratchRoot_MessageNamesTheAlternative(t *testing.T) {
	t.Setenv("HOME", fixtureHome)
	err := refuseNonScratchRoot("/srv/data")
	if err == nil {
		t.Fatal("/srv/data was accepted")
	}
	if !strings.Contains(err.Error(), "/tmp") || !strings.Contains(err.Error(), "without -apply") {
		t.Errorf("the refusal names neither a directory that would work nor the read-only mode: %v", err)
	}
}

// The containment test decides both clauses, and the case that matters is the
// one a plain HasPrefix gets wrong.
func TestUnder(t *testing.T) {
	for _, tc := range []struct {
		path, dir string
		want      bool
	}{
		{"/tmp", "/tmp", true},
		{"/tmp/factory", "/tmp", true},
		{"/tmp/a/b/c", "/tmp", true},
		{"/tmpfoo", "/tmp", false},
		{"/tmp-backup/x", "/tmp", false},
		{"/var/tmp", "/tmp", false},
		{"/home/someone", "/", true},
		{"/", "/", true},
	} {
		if got := under(tc.path, tc.dir); got != tc.want {
			t.Errorf("under(%q, %q) = %v, want %v", tc.path, tc.dir, got, tc.want)
		}
	}
}

// scratchDirs is what the guard compares against, so an empty or unresolved one
// would accept nothing — or, mutated the other way, everything.
func TestScratchDirs_ResolvesAndDeduplicates(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp")
	dirs := scratchDirs()
	if len(dirs) == 0 {
		t.Fatal("no temporary directory resolved on this host, so -apply can never run")
	}
	seen := map[string]bool{}
	for _, d := range dirs {
		if seen[d] {
			t.Errorf("%s is listed twice; the refusal message repeats itself", d)
		}
		seen[d] = true
		if d != filepath.Clean(d) {
			t.Errorf("%s is not a resolved, cleaned path, so it can never match a resolved -root", d)
		}
	}
	if !seen["/tmp"] {
		t.Errorf("/tmp is not a scratch directory according to %v, and it is this tool's default -root", dirs)
	}
}
