//go:build !windows

// Package platformtag is a fixture for `just vuln`'s build-tag derivation.
//
// Its one file is guarded by a NEGATED GOOS term, which is the shape that broke
// the derivation this tree used to have (bd gqlc-e7oq): the terms of a
// constraint were read out with the punctuation stripped, so `!windows` yielded
// `windows`, `-tags windows` satisfied `windows`, and this file's own constraint
// went false. A file guarded to build everywhere except Windows was handed a
// flag that built it nowhere the scan looked.
//
// Two properties are asserted before every scan, because a fixture that quietly
// stopped having its shape takes the guard out of service with nothing failing:
// that the derivation emits no `windows`, and that this directory is in the set
// `go list` matched — that is, that the scan actually compiled it. The first
// catches the derivation regressing, the second catches this file losing the
// constraint that makes the first mean anything.
//
// The term is a GOOS rather than a custom tag on purpose. GOOS, GOARCH and go1.N
// terms name facts the toolchain owns, and `-tags` will assert any of them
// anyway; a negated custom tag is dropped too, but for the weaker reason that
// `-tags` can only make a term true.
//
// This package builds on every platform this project's CI runs on. On Windows
// it would not, and `just vuln` would report this directory as unscanned, which
// is the honest answer: code for another platform cannot be scanned from this
// one.
package platformtag

// Constraint is the build constraint this file carries. It exists so the
// package holds a symbol; nothing imports it.
const Constraint = "!windows"
