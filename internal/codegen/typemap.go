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

	// StorableProperty reports whether the backend's store accepts a
	// property of this width as a stored property value. Distinct from
	// Property, which is the CARRIER question: a width may have a
	// faithful Go carrier the store still refuses to hold, and neo4j's
	// nested list is exactly that — [][]int16 carries it, and the same
	// backend emits a working recursive decode for one as a query value,
	// while the server refuses it as a stored property (ADR 0035). false
	// routes to ErrUnstorableProperty naming the entity, the property
	// and the width.
	//
	// Asked at Phase Z only. A query column and a query parameter are
	// read and bound, never stored, so a storage rule has nothing to say
	// about them, and asking there would refuse values the backend
	// serves.
	//
	// Required rather than an optional interface a backend may omit: an
	// optional one defaults silently to storable, so a backend that
	// never implemented it would be indistinguishable from one whose
	// store holds everything. Required, the compiler names every
	// implementation that still owes an answer.
	StorableProperty(pt graph.PropertyType) bool

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
