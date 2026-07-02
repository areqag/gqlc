// Package schema holds the parsed, in-memory model of a GQL graph type — "the
// model" (CONTEXT.md) that queries are resolved against — and the Parser
// interface that produces it.
package schema

import "io"

// Parser parses GQL graph-schema source into the model.
type Parser interface {
	// Parse reads a graph schema source and parses it into the domain
	// model of GQL for a graph schema. Returns an error when the schema
	// is invalid.
	Parse(r io.Reader) (Schema, error)
}
