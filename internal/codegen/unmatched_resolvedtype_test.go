package codegen_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// This file is package codegen_test rather than package codegen, and the
// distinction is the point rather than style. A test inside the package
// can hand an unexported function a value no caller could produce, which
// is how three unreachable branches came to sit in §2 of
// docs/specs/codegen-sentinel-taxonomy.md before gqlc-h4ug. Every value
// below arrives the way a consumer's would: an assembled codegen.Input
// handed to a backend's Generate, exactly the shape gqlc-edze measured
// the original fault with.
//
// The sweep behind TestSentinelTaxonomy drops internal/codegen from its
// corpus, and corpusPackageOf folds `internal/codegen_test` back onto
// `internal/codegen`, so this file pays no fail-site's coverage. It has
// none to pay: every refusal below returns through a fail-site that
// already carries a §2 row and a conformance witness. What is new here
// is that the refusal completes rather than faulting.

// nilEmbeddedNode satisfies resolver.ResolvedType through an embedded
// *ResolvedNode: Go promotes the embedded type's methods, including the
// unexported marker, and the pointer's method set carries ResolvedNode's
// value methods. The zero value therefore satisfies the interface with a
// nil embedded pointer, and String() on it reaches ResolvedNode.String
// through that nil pointer.
//
// It is here because it is the shape the two cheaper guards miss. A
// `t == nil` test does not see it — the interface holds a non-nil
// nilEmbeddedNode — and neither does a reflect check for a nil pointer,
// since its reflect.Kind is Struct.
type nilEmbeddedNode struct{ *resolver.ResolvedNode }

// embeddedNode is the same construction with a value field, so its
// String() returns ResolvedNode's wire tag rather than faulting. It
// witnesses the other half: rendering an unmatched type must not stop
// reporting the tag for the implementations that can supply one.
type embeddedNode struct{ resolver.ResolvedNode }

var (
	_ resolver.ResolvedType = nilEmbeddedNode{}
	_ resolver.ResolvedType = embeddedNode{}
)

// typedNilVariants is every variant's pointer form, nil. Each satisfies
// resolver.ResolvedType — the marker and String take value receivers, so
// a pointer's method set carries both — and matches no `case
// resolver.Resolved*:` arm, so each reaches a default arm. Go emits a
// nil check before a value method reached through a pointer, so String()
// on any of them faults, including the zero-sized ResolvedUnknown whose
// method body dereferences nothing.
//
// Written out rather than derived: a derivation over the eight would
// need a list of the eight to derive from, and this is that list.
func typedNilVariants() map[string]resolver.ResolvedType {
	return map[string]resolver.ResolvedType{
		"ResolvedNode":      (*resolver.ResolvedNode)(nil),
		"ResolvedProperty":  (*resolver.ResolvedProperty)(nil),
		"ResolvedEdge":      (*resolver.ResolvedEdge)(nil),
		"ResolvedEdgeUnion": (*resolver.ResolvedEdgeUnion)(nil),
		"ResolvedScalar":    (*resolver.ResolvedScalar)(nil),
		"ResolvedTemporal":  (*resolver.ResolvedTemporal)(nil),
		"ResolvedList":      (*resolver.ResolvedList)(nil),
		"ResolvedUnknown":   (*resolver.ResolvedUnknown)(nil),
	}
}

// unmatchedSchema declares the one node type the admissible column in
// each probe below resolves against.
func unmatchedSchema() schema.Schema {
	return schema.Schema{
		Name:  "Probe",
		Nodes: map[graph.LabelSetKey]schema.NodeType{"Person": {KeyLabels: "Person", CompleteLabels: "Person"}},
	}
}

// unmatchedColumnQuery wraps one column in an otherwise admissible
// query, so the column's resolved type is the only axis that can refuse.
func unmatchedColumnQuery(col resolver.Column) codegen.NamedQuery {
	return codegen.NamedQuery{
		Name:        "Fetch",
		Cardinality: codegen.CardinalityMany,
		SourceFile:  "probe.cypher",
		SourceText:  "MATCH (n) RETURN n",
		Validated:   resolver.ValidatedQuery{Columns: []resolver.Column{col}},
	}
}

// unmatchedParamQuery wraps one parameter beside one admissible column.
// The column is required: a :many query projecting none is refused by
// the cardinality-shape gate, which runs before the parameter loop.
func unmatchedParamQuery(param resolver.ResolvedParameter) codegen.NamedQuery {
	q := unmatchedColumnQuery(resolver.Column{Name: "n", Type: resolver.ResolvedNode{Labels: "Person"}})
	q.Validated.Parameters = []resolver.ResolvedParameter{param}
	return q
}

// generateUnmatched runs the batch a consumer would hand a backend and
// returns the refusal. It takes no recover of its own: a fault inside
// Generate must surface as this test's own panic, naming the site, which
// is what the pre-fix run of every case below reported.
func generateUnmatched(t *testing.T, q codegen.NamedQuery) error {
	t.Helper()
	files, err := neo4j.New().Generate(codegen.Input{
		Schema:  unmatchedSchema(),
		Queries: []codegen.NamedQuery{q},
	})
	require.Nil(t, files, "a refused Input must emit nothing")
	require.Error(t, err)
	return err
}

// TestUnmatchedResolvedTypeRefusesRatherThanFaulting is gqlc-edze's
// measurement. Three fail-sites end a walk over resolver.ResolvedType by
// naming the value that reached them — Phase A's column switch, Phase
// A's parameter type assertion, and buildListElemPlan's element switch —
// and each names it by calling the value's own String().
//
// That call is into code this package does not own, on a value chosen
// because no arm matched it. The interface's unexported marker seals
// which types may DECLARE it, not which may satisfy it: pointer forms
// and embedders promote it from any package in the module (AGENTS.md,
// "Closed sum types"). So the set reaching these three sites includes
// the nil interface, all eight typed-nil pointer forms, and structs
// whose embedded pointer is nil — and String() on each of those faults.
// Each case below panicked before the fix instead of returning.
//
// It does not claim the set is now safe under every implementation. A
// String() that blocks, or that calls runtime.Goexit, still takes the
// caller with it; what is bounded here is the panic.
func TestUnmatchedResolvedTypeRefusesRatherThanFaulting(t *testing.T) {
	t.Parallel()

	t.Run("column", func(t *testing.T) {
		t.Parallel()

		t.Run("nil-interface", func(t *testing.T) {
			t.Parallel()
			err := generateUnmatched(t, unmatchedColumnQuery(resolver.Column{Name: "n", Type: nil}))
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `out of C6 scope: query "Fetch" column 0 "n" resolved as <nil>`)
		})

		t.Run("nil-embedded-pointer", func(t *testing.T) {
			t.Parallel()
			err := generateUnmatched(t, unmatchedColumnQuery(resolver.Column{Name: "n", Type: nilEmbeddedNode{}}))
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `out of C6 scope: query "Fetch" column 0 "n" resolved as codegen_test.nilEmbeddedNode`)
		})

		for name, typed := range typedNilVariants() {
			t.Run("typed-nil/"+name, func(t *testing.T) {
				t.Parallel()
				err := generateUnmatched(t, unmatchedColumnQuery(resolver.Column{Name: "n", Type: typed}))
				require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
				require.EqualError(t, err, fmt.Sprintf(`out of C6 scope: query "Fetch" column 0 "n" resolved as *resolver.%s`, name))
			})
		}
	})

	t.Run("list-element", func(t *testing.T) {
		t.Parallel()

		t.Run("nil-interface", func(t *testing.T) {
			t.Parallel()
			col := resolver.Column{Name: "xs", Type: resolver.ResolvedList{Element: nil}}
			err := generateUnmatched(t, unmatchedColumnQuery(col))
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `query "Fetch" column 0 "xs": out of C6 scope: list element has unknown resolved type <nil>`)
		})

		t.Run("nil-embedded-pointer", func(t *testing.T) {
			t.Parallel()
			col := resolver.Column{Name: "xs", Type: resolver.ResolvedList{Element: nilEmbeddedNode{}}}
			err := generateUnmatched(t, unmatchedColumnQuery(col))
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `query "Fetch" column 0 "xs": out of C6 scope: list element has unknown resolved type codegen_test.nilEmbeddedNode`)
		})

		for name, typed := range typedNilVariants() {
			t.Run("typed-nil/"+name, func(t *testing.T) {
				t.Parallel()
				col := resolver.Column{Name: "xs", Type: resolver.ResolvedList{Element: typed}}
				err := generateUnmatched(t, unmatchedColumnQuery(col))
				require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
				require.EqualError(t, err, fmt.Sprintf(`query "Fetch" column 0 "xs": out of C6 scope: list element has unknown resolved type *resolver.%s`, name))
			})
		}
	})

	t.Run("parameter", func(t *testing.T) {
		t.Parallel()

		t.Run("nil-interface", func(t *testing.T) {
			t.Parallel()
			err := generateUnmatched(t, unmatchedParamQuery(resolver.ResolvedParameter{Name: "p", Type: nil}))
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `out of C6 scope: query "Fetch" parameter 0 $p resolved as <nil> (non-property parameters are post-v1)`)
		})

		t.Run("nil-embedded-pointer", func(t *testing.T) {
			t.Parallel()
			err := generateUnmatched(t, unmatchedParamQuery(resolver.ResolvedParameter{Name: "p", Type: nilEmbeddedNode{}}))
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `out of C6 scope: query "Fetch" parameter 0 $p resolved as codegen_test.nilEmbeddedNode (non-property parameters are post-v1)`)
		})

		for name, typed := range typedNilVariants() {
			t.Run("typed-nil/"+name, func(t *testing.T) {
				t.Parallel()
				err := generateUnmatched(t, unmatchedParamQuery(resolver.ResolvedParameter{Name: "p", Type: typed}))
				require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
				require.EqualError(t, err, fmt.Sprintf(`out of C6 scope: query "Fetch" parameter 0 $p resolved as *resolver.%s (non-property parameters are post-v1)`, name))
			})
		}
	})
}

// TestUnmatchedResolvedTypeKeepsTheWireTagWhereThereIsOne is the other
// half of the fix, and the half a fallback to the dynamic type name
// alone would lose. §2 of docs/specs/codegen-sentinel-taxonomy.md pins
// these messages as contract, and the conformance suite asserts them:
// an implementation whose String() answers must still be named by its
// answer, not by its Go type.
//
// So the render prefers the value's own tag and falls back only where
// asking for it faults. The two cases here are the pointer form and the
// value embedder — the same two constructions the faulting cases above
// use, differing only in carrying something to dispatch on.
func TestUnmatchedResolvedTypeKeepsTheWireTagWhereThereIsOne(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		typ  resolver.ResolvedType
	}{
		{name: "pointer-form", typ: &resolver.ResolvedNode{Labels: "Person"}},
		{name: "value-embedder", typ: embeddedNode{resolver.ResolvedNode{Labels: "Person"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generateUnmatched(t, unmatchedColumnQuery(resolver.Column{Name: "n", Type: tc.typ}))
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `out of C6 scope: query "Fetch" column 0 "n" resolved as node`)

			listCol := resolver.Column{Name: "xs", Type: resolver.ResolvedList{Element: tc.typ}}
			listErr := generateUnmatched(t, unmatchedColumnQuery(listCol))
			require.ErrorIs(t, listErr, codegen.ErrOutOfC6Scope)
			require.EqualError(t, listErr, `query "Fetch" column 0 "xs": out of C6 scope: list element has unknown resolved type node`)
		})
	}
}
