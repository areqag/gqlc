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
	// carry part of the enum and not the rest (ADR 0025).
	Temporal(k resolver.Temporal) (goType string, ok bool)

	// Scalar maps a resolved scalar-expression kind to its Go type text.
	// Total over the closed enum: resolver.Scalar's membership (bool /
	// int / float / string / null / map) is the openCypher literal
	// vocabulary, and a store a Cypher query runs against accepts a value
	// of each written into a query, so every backend has an answer. A
	// kind added here with no such value behind it falsifies that ground
	// and needs the rejection channel Temporal carries.
	//
	// Answering does not oblige a backend to emit the column: whether a
	// kind reaches emission is the backend's own gate, and one may answer
	// here and refuse there (ADR 0025).
	Scalar(k resolver.Scalar) string
}
