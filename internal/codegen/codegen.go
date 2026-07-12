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

// Codegen is the concrete generator. The schema and queries arrive on
// Input, not New; construction-time knobs arrive through the Option
// surface. The zero value emits against neo4j-go-driver v5 and derives
// the package identifier from Schema.Name.
type Codegen struct {
	driverVersion DriverVersion
	packageName   string
}

// Option configures a Codegen at construction time.
type Option func(*Codegen)

// DriverVersion selects the neo4j-go-driver major version the generated
// code imports. The two majors expose a name-identical API surface for
// everything the emission uses, except that v6 renamed
// DriverWithContext back to Driver (keeping the old name as an alias);
// generated v6 code uses the native name.
type DriverVersion int

const (
	// DriverV5 targets github.com/neo4j/neo4j-go-driver/v5 — the zero
	// value and the default.
	DriverV5 DriverVersion = iota
	// DriverV6 targets github.com/neo4j/neo4j-go-driver/v6 (requires
	// Go >= 1.24 in the consuming module).
	DriverV6
)

// WithDriverVersion selects the driver major the generated package is
// emitted against.
func WithDriverVersion(v DriverVersion) Option {
	return func(c *Codegen) { c.driverVersion = v }
}

// WithPackageName overrides the Schema.Name-derived package
// identifier with an explicitly configured one. The empty string —
// the zero value — keeps the derivation.
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
func (c *Codegen) Generate(in Input) ([]File, error) {
	return generate(in, c.driverVersion.target(), c.packageName)
}
