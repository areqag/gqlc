// Package codegen emits typed Go repository packages from a schema plus a
// batch of resolved, annotated queries — the sqlc analogue for the graph
// side (ADR 0010). C0 emits a compiling-but-empty package (Queries handle
// with New / WithTx, the three Querier interfaces, an empty models file);
// C1..C5 add per-query methods, entity structs, and the write path.
//
// Consumers accept the [Generator] seam and construct via [New], which
// returns a *Codegen. Generate is pure: same Input in, byte-identical
// []File out (§2.3). All I/O is the caller's — Generate never touches
// disk.
package codegen

// Generator emits a generated package from a schema plus a batch of named
// queries. The concrete producer is *Codegen; consumers accept the
// interface so an alternative target (a future TypeScript emitter, a
// second-driver Go emitter) can substitute without importing this
// package's target-specific types.
type Generator interface {
	Generate(in Input) ([]File, error)
}

// Compile-time assertion: *Codegen satisfies Generator. Catches a
// signature typo on Generate before any test runs.
var _ Generator = (*Codegen)(nil)

// Codegen is the concrete generator. C0 has no compile-time inputs; the
// schema and queries arrive on Input, not New. Later stages may add
// knobs — a version-stamp override for goldens, a target-driver
// selection — through the Option surface.
type Codegen struct{}

// Option configures a Codegen at construction time. The surface is empty
// at C0; later stages add knobs without churning the constructor.
type Option func(*Codegen)

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
func (c *Codegen) Generate(in Input) ([]File, error) {
	return generate(in)
}
