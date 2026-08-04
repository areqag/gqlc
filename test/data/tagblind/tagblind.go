//go:build tagblind

// Package tagblind is a fixture for `just vuln`'s build-tag derivation.
//
// Every Go file in this directory is behind a build constraint, which is the
// one directory shape `go list ./...` does not match at all: no package, no
// error, and no IgnoredGoFiles either. A tag derivation that enumerates through
// the wildcard therefore derives none of the tags such a directory carries, and
// the postcondition asserting IgnoredGoFiles is empty never sees the files that
// were dropped — the scan runs over less and exits 0 (bd gqlc-pig9).
//
// So the derivation walks the filesystem, and asserts that every directory
// holding a Go file was matched by `go list` under the derived tags. This
// directory is the only live exercise of either. Deleting it, or adding an
// unconstrained .go file beside this one, takes both guards out of service —
// which is why `just vuln` asserts this directory's shape before it scans.
package tagblind

// Tag is the build constraint this file carries. It exists so the package holds
// a symbol; nothing imports it.
const Tag = "tagblind"
