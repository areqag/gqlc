// Package age emits typed Go repository packages against Apache AGE on
// PostgreSQL, driven by pgx v5 — the sqlc analogue for the graph side
// (ADR 0010). C0 emits a compiling-but-empty package: the Queries handle
// with New / WithTx over a pgx DBTX seam, the session initialiser and
// graph lifecycle helpers, and the three Querier interfaces.
//
// The shared admission and naming phases live in internal/codegen; this
// package supplies AGE's Go-type table to them and owns every text
// fragment the generated files carry.
package age

import "github.com/areqag/gqlc/internal/codegen"

// Compile-time assertion: *Codegen satisfies the seam consumers accept.
// Catches a signature typo on Generate before any test runs.
var _ codegen.Generator = (*Codegen)(nil)

// pgxModule is the pgx import root every generated file names. The path
// carries the major only: a consuming module resolves its own minor.
const pgxModule = "github.com/jackc/pgx/v5"

// Codegen is the Apache AGE generator. The schema and queries arrive on
// Input, not New; construction-time knobs arrive through the Option
// surface. The zero value derives the package identifier from
// Schema.Name.
type Codegen struct {
	packageName string
}

// Option configures a Codegen at construction time.
type Option func(*Codegen)

// WithPackageName overrides the Schema.Name-derived package identifier
// with an explicitly configured one. The empty string — the zero value —
// keeps the derivation.
func WithPackageName(name string) Option {
	return func(c *Codegen) { c.packageName = name }
}

// New returns a Codegen with the given options applied.
func New(opts ...Option) *Codegen {
	c := &Codegen{}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Generate emits the generated-package file set for a batch. Pure,
// deterministic, short-circuits on the first error (§2.3). Returns
// (nil, err) on failure — never a partial slice.
func (c *Codegen) Generate(in codegen.Input) ([]codegen.File, error) {
	return generate(in, c.packageName)
}
