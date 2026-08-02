package codegen

import (
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
)

// TypeMap is a backend's Go-type table (spec §5.1): the mapping from the
// resolved type surface onto the Go type text the generated package
// spells. Every returned string is opaque to the phases — they carry it
// onto the prepared surface verbatim and hand it back to the backend's
// render layer.
type TypeMap interface {
	// Property maps a schema property width to its Go type text. ok is
	// false for a width the backend has no faithful carrier for; the
	// phase routes that to ErrUnrepresentableWidth naming the width.
	Property(pt graph.PropertyType) (goType string, ok bool)

	// Temporal maps a resolved temporal-expression kind to its Go type
	// text. ok is false for a kind the backend has no faithful carrier
	// for; the phase routes that to ErrUnrepresentableTemporal naming
	// the kind. Partial per kind rather than per backend: a store may
	// carry part of the enum and not the rest, so a backend refusing one
	// kind still answers for the others.
	Temporal(k resolver.Temporal) (goType string, ok bool)

	// Scalar maps a resolved scalar-expression kind to its Go type text.
	// Total over the closed enum, on a ground that holds for any backend
	// rather than by coincidence of the two that exist: resolver.Scalar's
	// membership (bool / int / float / string / null / map) is the
	// openCypher literal vocabulary, and a store a Cypher query runs
	// against holds a value of each by being able to accept one written
	// into a query. That is the claim Temporal cannot make — Apache AGE
	// speaks Cypher and still has no temporal value — so a kind added
	// here with no such value behind it falsifies the ground and takes
	// the rejection channel Temporal carries.
	Scalar(k resolver.Scalar) string
}
