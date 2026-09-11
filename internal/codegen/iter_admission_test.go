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

// TestIterAdmission pins the ORDER in which admitQueryAxes reads the two
// axes that can refuse an :iter query. Both rows below are refused by
// ErrIterOnWrite, whose corpus coverage is paid by
// invalid/iter_on_write; what is measured here is which gate answers,
// which no fixture's expectedError distinguishes once both gates return
// the same sentinel.
//
// The third row this test used to carry — a zero-column :iter READ,
// refused for its shape — now lives in the conformance package, as
// TestAssembledInput's cardinality-iter-zero-column-read case. It was
// the one row whose sentinel had no other witness for the :iter
// disjunct, and a witness in THIS package is one the reachability fence
// cannot read: corpusPackageOf folds `internal/codegen_test` onto
// `internal/codegen`, which corpusPackages then drops. Taxonomy §5.1
// names conformance/assembled_input_test.go as where an assembled-Input
// row goes, and that is the file the fence's coverage sweep reaches
// (bd gqlc-e3ra). These two rows stay because their claim is about
// ordering rather than about reaching a return nothing else reaches.
func TestIterAdmission(t *testing.T) {
	t.Run("a write is refused for writing", func(t *testing.T) {
		cols := []resolver.Column{{Name: "name", Type: resolver.ResolvedProperty{}}}
		_, err := codegen.Prepare(
			iterAdmissionInput(resolver.StatementWrite, cols, "CREATE (p:Person {name: $name}) RETURN p.name"),
			stubTypeMap{}, "iteradmission")
		require.ErrorIs(t, err, codegen.ErrIterOnWrite)
	})

	// The ordering claim, and the reason this row is not redundant with the
	// one above: a zero-column :iter write satisfies BOTH gates, so which
	// one answers is a choice rather than a consequence. Refusing it for
	// writing sends the author to the one edit that fixes it — adding a
	// RETURN to a streamed write leaves it refused, where re-annotating
	// :many admits it outright. Swapping the two gates in prepare.go turns
	// this row red and leaves the row above green; it also leaves the
	// migrated conformance case green, that row being a READ, which is why
	// this row is the only thing holding the order.
	t.Run("a zero-column write is refused for writing, not for its shape", func(t *testing.T) {
		_, err := codegen.Prepare(
			iterAdmissionInput(resolver.StatementWrite, nil, "MATCH (p:Person) DELETE p"),
			stubTypeMap{}, "iteradmission")
		require.ErrorIs(t, err, codegen.ErrIterOnWrite)
		require.NotErrorIs(t, err, codegen.ErrCardinalityShapeMismatch)
	})
}
