package schema

import "io"

type Parser interface {
	// Parse reads a graph schema source and parses it into the domain
	// model of GQL for a graph schema. Returns an error when the schema
	// is invalid.
	Parse(r io.Reader) (Schema, error)
}
