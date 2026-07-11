// Package fixtures is the module-root anchor for the codegen golden corpus.
// Nothing is exported: the file exists only so `go build ./...` inside this
// nested module has a top-level package to walk from. Every real fixture is
// a sub-package under valid/<name>/golden/ or invalid/<name>/. The nested
// go.mod keeps this file out of gqlc's own ./... walks.
package fixtures
