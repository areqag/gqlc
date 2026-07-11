// Package queryfile is the annotation front end that lowers a raw query file
// into the [AnnotatedQuery] slice codegen consumes. A query file is a text
// file whose queries are individually named and typed by sqlc-style line-
// comment annotations ("// name: Ident :one|:many|:exec"); this package
// walks those annotations, slices the bodies, and validates each row —
// nothing more. The generator (internal/codegen) reads AnnotatedQueries as
// data; it never re-parses a query file.
package queryfile

import "io"

// Parser lowers a query file's raw bytes into the annotated queries it
// declares. The concrete producer is the value returned by [New]; consumers
// accept the interface so a future alternative front end (e.g. an inline-
// annotation variant, a JSON query manifest) can substitute without churn.
type Parser interface {
	Parse(r io.Reader) ([]AnnotatedQuery, error)
}

// parser is the concrete Parser. Zero configuration; the grammar is fixed
// (§4.1 of docs/specs/codegen-stage-c0.md).
type parser struct{}

// Compile-time assertion: parser satisfies Parser. Catches a signature typo
// before any test runs.
var _ Parser = parser{}

// New returns the queryfile parser. No compile-time inputs — the parser is a
// pure text-to-model step.
func New() Parser {
	return parser{}
}

// Parse walks the reader line by line, emits one [AnnotatedQuery] per
// annotation, and returns the full slice. Short-circuits on the first error
// (§2.3): a partial slice is never returned alongside an error.
func (parser) Parse(r io.Reader) ([]AnnotatedQuery, error) {
	return parse(r)
}
