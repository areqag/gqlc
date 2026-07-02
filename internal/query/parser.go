// Package query holds the curated, dialect-agnostic query model (ADR 0003)
// that concrete parsers lower query source into, and the Parser interface
// those parsers implement.
package query

import "io"

// Parser lowers query source into the query model.
type Parser interface {
	// Parse lowers one query's source into the query model. The result is a
	// parsed query (syntactically well-formed), not a validated one: it is
	// schema-agnostic, and checking it against a schema.Schema is a separate
	// stage. Exactly one query per call. Returns an error when the source is not
	// a query this parser supports.
	Parse(r io.Reader) (Query, error)
}
