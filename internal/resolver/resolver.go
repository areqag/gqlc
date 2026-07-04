// Package resolver lowers a parsed query.Query into a schema-checked, fully
// typed ValidatedQuery — the first consumer of the frozen query.Query (ADR
// 0008), staged R0..R7 per ADR 0009. R0 handles labelled single-node
// MATCH/RETURN — whole-entity and property refs.
package resolver

import (
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// QueryResolver is the consumer-facing seam for validated-query production.
// The concrete producer is *Resolver; consumers accept the interface so they
// can substitute a fake without importing procsig or schema.
type QueryResolver interface {
	Resolve(q query.Query) (ValidatedQuery, error)
}

// Compile-time assertion: *Resolver satisfies QueryResolver. A signature typo
// on Resolve fails the build before any test runs.
var _ QueryResolver = (*Resolver)(nil)

// Resolver is the concrete resolver: it binds the compile-time inputs (the
// schema and the procedure signature registry) at construction time; Resolve
// is a pure function of its input query given those inputs.
type Resolver struct {
	schema   schema.Schema
	registry procsig.Registry
}

// Option configures a Resolver at construction time.
type Option func(*Resolver)

// WithRegistry supplies the procedure signature registry the resolver consults
// at CALL YIELD (R7). Absent, the zero registry misses on every lookup — a
// CALL against a registry-less resolver raises the R7 unknown-procedure
// sentinel.
func WithRegistry(r procsig.Registry) Option {
	return func(res *Resolver) { res.registry = r }
}

// New binds a resolver to the given schema and options. The returned
// *Resolver is safe for concurrent use: Resolve reads only the compile-time
// inputs and its argument.
func New(s schema.Schema, opts ...Option) *Resolver {
	r := &Resolver{schema: s}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Resolve lowers a parsed query into a validated one. Short-circuits at the
// first error; the zero ValidatedQuery is returned alongside the error, per
// the schema/gql convention.
func (r *Resolver) Resolve(q query.Query) (ValidatedQuery, error) {
	return resolve(q, r.schema, r.registry)
}
