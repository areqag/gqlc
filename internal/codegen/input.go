package codegen

import (
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// Input is the batch a Generate call runs over: exactly one schema (v1
// posture: one graph type per generated package) plus the annotated,
// parsed, resolved queries to project. Fields are added as later stages
// need them; the shape stays batch-shaped and caller-lowered.
type Input struct {
	Schema  schema.Schema
	Queries []NamedQuery
}

// NamedQuery is one annotated query lowered by the front end and resolved
// by internal/resolver, in the form Generate consumes. Codegen never
// reaches back into query.Query — every artefact of the resolved surface
// maps to a field on this envelope or on Validated.
type NamedQuery struct {
	// Name is the annotation-declared identifier — must already be a
	// valid exported Go identifier (^[A-Z][A-Za-z0-9]*$). Enforced by
	// the queryfile front end; Generate does not re-validate.
	Name string

	// Cardinality is the author-declared row axis (spec §3.4). Zero
	// value ("unset") is rejected by generate as ErrInvalidCardinality.
	Cardinality Cardinality

	// SourceFile is the query file's basename ("people.cypher"),
	// carried forward as the grouping key for the per-source generated
	// file. C0 emits no per-source files; the field is present for wire
	// stability and used by C1+.
	SourceFile string

	// SourceText is the exact query text between this annotation and
	// the next (or EOF), preserved byte-for-byte per ADR 0005 —
	// generated code executes the verbatim text.
	SourceText string

	// Validated is the resolver's output. C0 does not read any field of
	// it; C1+ derives Params, Row, and method surfaces from it.
	Validated resolver.ValidatedQuery
}

// Cardinality is re-exported as a type alias from internal/queryfile so
// internal/codegen consumers do not import queryfile just to name a
// cardinality. One enum, two package-level identifiers.
type Cardinality = queryfile.Cardinality

// Cardinality constant re-exports, mirroring the alias so callers can
// spell the constants without importing queryfile.
const (
	CardinalityOne  = queryfile.CardinalityOne
	CardinalityMany = queryfile.CardinalityMany
	CardinalityExec = queryfile.CardinalityExec
)

// File is one emitted file: its path relative to the caller's out
// directory, and its complete, gofmt-clean contents. Path is the
// canonical form the caller writes to disk. Generate never touches disk
// (ADR 0010 D4); the caller owns I/O. The returned []File is sorted by
// Path; identical paths in the slice are a bug — the caller can rely on
// the slice being a set keyed by Path.
type File struct {
	Path     string
	Contents []byte
}
