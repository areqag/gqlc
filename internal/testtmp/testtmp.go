// Package testtmp keeps `go test` result caching alive for packages whose
// tests create temporary files.
//
// t.TempDir opens its base directory — os.TempDir(), so normally /tmp — and
// the test binary records that open in its test log. A cached result replays
// only when every recorded path re-hashes identically, and for a directory
// the hash covers its own stat plus the name and stat of every entry. A
// shared /tmp can never satisfy that: the go command parks its go-buildNNN
// work dir there for the whole invocation, sibling test binaries mint
// tempdirs in it mid-run, and each creation bumps the base's wall-clock
// mtime, which the next run (or the next CI runner) does not reproduce.
// Probed: one package with a single t.TempDir test, run twice back-to-back
// on an idle machine, replays from cache with this redirect and re-runs
// without it.
//
// Main gives the binary a private base and deletes it before the go command
// hashes the log. A path that is absent both when the result is saved and
// when it is later checked hashes as the same ENOENT on both sides, so the
// recorded opens carry no volatile state at all. The redirect itself is
// clean too: os.MkdirTemp records only `getenv TMPDIR`, and getenv lines are
// hashed from the go command's OWN environment, not from the Setenv below.
//
// The fence beside this file keeps packages wired, and what it reaches is
// worth stating exactly, because it was once claimed to catch any future
// temp-using package and does not. It reads each package's own test files
// for a direct temp call, and the non-test files of packages those test
// files import for an exported function making one. Past that it is blind:
// a temp acquisition two packages deep, or behind a value rather than a
// call it can name, is unreachable without the call graph and is not
// reported (bd gqlc-nvmz).
package testtmp

import (
	"fmt"
	"os"
	"testing"
)

// Main runs m with TMPDIR pointing at a private directory that is removed
// before the process exits. It does not return.
func Main(m *testing.M) {
	base, err := os.MkdirTemp("", "gqlc-testtmp-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "testtmp: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("TMPDIR", base); err != nil {
		fmt.Fprintf(os.Stderr, "testtmp: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	// A failed removal only costs the replay (the base is present at save
	// time, so the saved hash never matches a later check); the results
	// themselves stay valid, so report it without failing the run.
	if err := os.RemoveAll(base); err != nil {
		fmt.Fprintf(os.Stderr, "testtmp: %v\n", err)
	}
	os.Exit(code)
}
