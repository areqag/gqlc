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
	// text. Total over the closed enum.
	Temporal(k resolver.Temporal) string

	// Scalar maps a resolved scalar-expression kind to its Go type text.
	// Total over the closed enum.
	Scalar(k resolver.Scalar) string
}
