package main

import (
	"os"
	"path/filepath"
	"slices"
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
	t.Setenv("HOME", fixtureHome)
	// Hostile, and inert: the accept list is two literals and no longer reads
	// this. Set here so the table would redden if the environment ever regained
	// a say in it — with TMPDIR on the list, "/" accepted every REFUSE row below
	// except the home ones.
	t.Setenv("TMPDIR", "/")

	for _, tc := range []struct {
		name   string
		root   string
		refuse bool
	}{
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

// $TMPDIR is not an authorisation to delete. The guard was built against a
// one-typo accident (`just tmp-reap apply ~`), and while os.TempDir() was on the
// accept list a one-unset-variable accident walked through it: TMPDIR="${BASE}/"
// with BASE unset is "/", which is an ancestor of every path, so every root off
// the home chain became scratch — including /dev/shm, which row 11 of the table
// above says must be refused (verdict-osuz-r2, blocking 3).
//
// The rows are directories that resolve on any host, because an unresolvable
// candidate is dropped and would witness nothing.
func TestRefuseNonScratchRoot_TheEnvironmentCannotWidenTheScratchList(t *testing.T) {
	t.Setenv("HOME", fixtureHome)
	for _, tc := range []struct{ tmpdir, root string }{
		{"/", "/srv/data"},
		{"/", "/dev/shm"},
		{"/usr", "/usr/local/lib"},
	} {
		t.Run(tc.tmpdir+" -> "+tc.root, func(t *testing.T) {
			t.Setenv("TMPDIR", tc.tmpdir)
			if err := refuseNonScratchRoot(tc.root); err == nil {
				t.Errorf("-apply was allowed over %s because TMPDIR said %s", tc.root, tc.tmpdir)
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

// The home clause compares a RESOLVED home, and the direction that matters is
// over-permission: $HOME is very often a symlink (/home/u -> /data/u on this
// kind of host), and comparing the unresolved spelling against a -root that run
// has already resolved makes the clause miss its own case. Nothing reddened when
// the EvalSymlinks was deleted (verdict-osuz-r2, survivor R14).
func TestRefuseNonScratchRoot_ASymlinkedHomeIsResolvedBeforeComparing(t *testing.T) {
	base := t.TempDir()
	useScratchRoot(t, base)
	target := mkdir(t, filepath.Join(base, "real-home"))
	link := filepath.Join(base, "link-home")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", link)

	// The scratch clause would accept this root, so the home clause is the only
	// thing that can refuse it — which is what makes the row a witness.
	err := refuseNonScratchRoot(filepath.Join(target, "Downloads"))
	if err == nil {
		t.Fatal("-apply was allowed inside the home directory reached by its real path")
	}
	if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("the refusal cites the wrong clause: %v", err)
	}
}

// A candidate that does not resolve is dropped, not compared literally. The
// literal would match a -root that run has resolved only by accident, and the
// accident is in the accepting direction (verdict-osuz-r2, survivor R8).
func TestResolveScratch_DropsWhatDoesNotResolveAndDeduplicates(t *testing.T) {
	base := t.TempDir()
	target := mkdir(t, filepath.Join(base, "real"))
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(base, "absent")

	got := resolveScratch([]string{absent, link, target})
	if slices.Contains(got, absent) {
		t.Errorf("a candidate that does not exist is in the accept list %v, so -apply would compare against a path nothing resolves to", got)
	}
	if !slices.Equal(got, []string{target}) {
		t.Errorf("resolveScratch = %v, want just the resolved directory once", got)
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
