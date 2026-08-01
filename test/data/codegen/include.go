// Package fixtures is the module-root anchor for the codegen golden corpus.
// Nothing is exported: the file exists only so `go build ./...` inside this
// nested module has a top-level package to walk from. Every generated
// package is a sub-package under valid/<name>/golden/<target>/, one per
// emission target the fixture is enrolled in; invalid/<name>/ holds inputs
// that never reach emission and so contains no Go package at all. The
// nested go.mod keeps this file out of gqlc's own ./... walks.
package fixtures
