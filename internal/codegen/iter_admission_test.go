package codegen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// iterAdmissionInput builds the smallest batch carrying one :iter query of
// the given statement kind and column shape. The schema is one node type
// with one STRING property, which is enough for Phase Z and is not what
// any row here is about.
func iterAdmissionInput(stmt resolver.StatementKind, cols []resolver.Column, text string) codegen.Input {
	return codegen.Input{
		Schema: schema.Schema{
			Name: "IterAdmission",
			Nodes: map[graph.LabelSetKey]schema.NodeType{
				"Person": {KeyLabels: "Person", CompleteLabels: "Person"},
			},
		},
		Queries: []codegen.NamedQuery{{
			Name:        "Stream",
			Cardinality: queryfile.CardinalityIter,
			SourceFile:  "iter.cypher",
			SourceText:  text,
			Validated:   resolver.ValidatedQuery{Statement: stmt, Columns: cols},
		}},
	}
}

// TestIterAdmission pins both of the axes admitQueryAxes reads for :iter
// AND the order it reads them in.
//
// It is a unit test rather than a conformance fixture because one of its
// three rows is not expressible as one. The corpus reaches Phase A through
// the real parser, and a zero-column READ has no openCypher spelling: a
// read query must end in RETURN, which projects at least one column, and
// the only other zero-column read shape — a CALL with no YIELD — needs a
// procsig registry the conformance harness does not wire (it builds
// procsig.NewRegistry(nil), so every CALL fails at parse with
// ErrUnknownProcedure before cardinality is ever consulted). Four
// candidate spellings were measured against the harness on 2026-09-11:
// `MATCH (p:Person) RETURN *`, `MATCH (p:Person) WITH p.name AS n
// RETURN *` and `MATCH (p:Person) WHERE p.name = $name RETURN *` all
// admitted cleanly with columns, and `CALL db.ping()` died at
// ErrUnknownProcedure.
//
// That makes the ruling's named `invalid/iter_zero_column_read` fixture
// unbuildable, and this test is what stands in for it: the arm it guards
// is unreachable from user input TODAY and would stop being the moment a
// registry reaches the corpus, which is exactly the shape a guard is for.
func TestIterAdmission(t *testing.T) {
	t.Run("a write is refused for writing", func(t *testing.T) {
		cols := []resolver.Column{{Name: "name", Type: resolver.ResolvedProperty{}}}
		_, err := codegen.Prepare(
			iterAdmissionInput(resolver.StatementWrite, cols, "CREATE (p:Person {name: $name}) RETURN p.name"),
			stubTypeMap{}, "iteradmission")
		require.ErrorIs(t, err, codegen.ErrIterOnWrite)
	})

	t.Run("a zero-column read is refused for its shape", func(t *testing.T) {
		_, err := codegen.Prepare(
			iterAdmissionInput(resolver.StatementRead, nil, "CALL some.proc()"),
			stubTypeMap{}, "iteradmission")
		require.ErrorIs(t, err, codegen.ErrCardinalityShapeMismatch)
	})

	// The ordering claim, and the reason this row is not redundant with the
	// two above: a zero-column :iter write satisfies BOTH gates, so which
	// one answers is a choice rather than a consequence. Refusing it for
	// writing sends the author to the one edit that fixes it — adding a
	// RETURN to a streamed write leaves it refused, where re-annotating
	// :many admits it outright. Swapping the two gates in prepare.go turns
	// this row red and leaves the other two green.
	t.Run("a zero-column write is refused for writing, not for its shape", func(t *testing.T) {
		_, err := codegen.Prepare(
			iterAdmissionInput(resolver.StatementWrite, nil, "MATCH (p:Person) DELETE p"),
			stubTypeMap{}, "iteradmission")
		require.ErrorIs(t, err, codegen.ErrIterOnWrite)
		require.NotErrorIs(t, err, codegen.ErrCardinalityShapeMismatch)
	})
}
