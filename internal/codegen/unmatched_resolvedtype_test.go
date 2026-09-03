package codegen_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
)

// This file is package codegen_test rather than package codegen, and the
// distinction is the point rather than style. A test inside the package
// can hand an unexported function a value no caller could produce, which
// is how three unreachable branches came to sit in §2 of
// docs/specs/codegen-sentinel-taxonomy.md before gqlc-h4ug. Every value
// below arrives the way a consumer's would: an assembled codegen.Input
// handed to a backend's Generate, which is the shape gqlc-edze measured
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

// nilEmbeddedIface satisfies resolver.ResolvedType by embedding the
// interface rather than a variant. Go promotes an embedded interface's
// whole method set, unexported marker included, so the zero value
// satisfies resolver.ResolvedType from any package while naming no
// variant at all, and String() on it dispatches through the nil embedded
// interface and faults.
//
// It is here because it is none of the three shapes gqlc-edze first
// named. The outer interface holds a non-nil nilEmbeddedIface, so it is
// not the nil interface; its reflect.Kind is Struct, so it is not a
// typed-nil pointer form; and its embedded field is not a pointer to a
// variant. It is the witness for the claim that the faulting set is not
// closed by an enumeration — it comes from the promotion machinery that
// opens the sum, not from an implementation misbehaving.
type nilEmbeddedIface struct{ resolver.ResolvedType }

// embeddedNode is the same construction with a value field, so its
// String() returns ResolvedNode's wire tag rather than faulting. It
// witnesses the other half: rendering an unmatched type must not stop
// reporting the tag for the implementations that can supply one.
type embeddedNode struct{ resolver.ResolvedNode }

var (
	_ resolver.ResolvedType = nilEmbeddedNode{}
	_ resolver.ResolvedType = nilEmbeddedIface{}
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
// This is a witness list, not a guard, and nothing here holds it to the
// variants internal/resolver declares — that derivation exists, in
// TestResolvedTypeSumIsNotClosed/declared_variants, which walks the
// package's isResolvedType declarations and reddens when the set moves.
// A ninth variant would leave this list short and nothing in this
// package would say so. Neither would shrinking it: every entry supplies
// its own subtest in each of the three groups below and nothing counts
// them, so dropping entries — up to emptying the map, which deletes most
// of the leaves in this file — leaves every package here green. Review
// measured that. What each surviving entry still pins is which pointer
// form it names, since the subtest asserts the rendered name: swapping an
// entry's value for a different variant's pointer reddens.
//
// No length assertion is written here, deliberately. Eight is a fact
// about internal/resolver, and a second copy of it in this file is the
// two-carriers-one-number shape that has already cost this branch a
// round; the guard against the set moving belongs where the set is
// declared, which is where it is. A ninth would also need no edit here to
// be handled: ResolvedTypeName enumerates no variant, so the pointer form
// of a ninth renders by the same route these eight do.
//
// That derivation is not reachable with `go test -overlay`, which is a
// trap worth naming: parsePackageSources reads the package directory
// with os.ReadDir and go/parser at run time, so a ninth variant added
// through a build overlay is invisible to it and declared_variants
// passes clean. To check the gate is live, mutate sealedsum_test.go's
// inhabitants map instead — dropping an entry reddens declared_variants.
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
		Cardinality: queryfile.CardinalityMany,
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
// measurement. Five fail-sites end a walk over resolver.ResolvedType by
// naming the value that reached them, and each named it by calling that
// value's own String() before this fix. Three of the five are reachable
// from outside the package — Phase A's column switch, Phase A's
// parameter type assertion, and buildListElemPlan's element switch — and
// those three are what the groups below measure, one group each. The
// other two are the sites tagged //gqlc:unreachable param-type-invariant
// and //gqlc:unreachable column-type-invariant, which §3 of
// docs/specs/codegen-sentinel-taxonomy.md argues Phase A shadows and
// TestUnreachedBranchesAreUnreached measures as uncovered. Reverting
// either of those two to a direct String() call leaves every package
// here green: that survival is the §3 shadow holding, not a gap in this
// file, and this file measures neither site. ResolvedTypeName's own doc
// comment carries the same split beside the grep that re-derives it.
//
// That call is into code this package does not own, on a value chosen
// because no arm matched it. The interface's unexported marker seals
// which types may DECLARE it, not which may satisfy it: a pointer form
// carries the marker, and an embedder can promote it, from any package
// in the module (AGENTS.md, "Closed sum types"). Can, not does — a
// struct embedding two variants at equal depth promotes neither's
// methods, so struct{resolver.ResolvedNode; resolver.ResolvedEdge}
// satisfies the interface in neither its value nor its pointer form and
// reaches no fail-site here. So the set reaching those three reachable
// sites includes the nil interface, all eight typed-nil pointer forms,
// structs whose embedded pointer to a variant is nil, and structs
// embedding resolver.ResolvedType itself — and String() on each of those
// faults. Each case below panicked before the fix instead of returning.
//
// Those four shapes are what this measures, not what the set holds.
// Whether a value faults is a fact about the String() it ends up
// dispatching, and the interface is satisfiable by types codegen never
// sees, so no enumeration here closes the set: (*embeddedNode)(nil)
// faults and is none of the four, because the promoted value method must
// dereference the pointer to reach its receiver. Composing is not itself
// what faults, though — embeddedNode held by value answers, which is the
// value-embedder case in
// TestUnmatchedResolvedTypeKeepsTheWireTagWhereThereIsOne below. The fix
// does not depend on the set being closed, since ResolvedTypeName
// enumerates no shape.
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

		t.Run("nil-embedded-interface", func(t *testing.T) {
			t.Parallel()
			err := generateUnmatched(t, unmatchedColumnQuery(resolver.Column{Name: "n", Type: nilEmbeddedIface{}}))
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `out of C6 scope: query "Fetch" column 0 "n" resolved as codegen_test.nilEmbeddedIface`)
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

		t.Run("nil-embedded-interface", func(t *testing.T) {
			t.Parallel()
			col := resolver.Column{Name: "xs", Type: resolver.ResolvedList{Element: nilEmbeddedIface{}}}
			err := generateUnmatched(t, unmatchedColumnQuery(col))
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `query "Fetch" column 0 "xs": out of C6 scope: list element has unknown resolved type codegen_test.nilEmbeddedIface`)
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

		t.Run("nil-embedded-interface", func(t *testing.T) {
			t.Parallel()
			err := generateUnmatched(t, unmatchedParamQuery(resolver.ResolvedParameter{Name: "p", Type: nilEmbeddedIface{}}))
			require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
			require.EqualError(t, err, `out of C6 scope: query "Fetch" parameter 0 $p resolved as codegen_test.nilEmbeddedIface (non-property parameters are post-v1)`)
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

	// The case names are held rather than merely ranged over, because
	// value-embedder is cited by name as the witness for "composing is not
	// itself what faults" in prose that no compiler checks: in
	// ResolvedTypeName's doc comment, in the doc on
	// TestUnmatchedResolvedTypeRefusesRatherThanFaulting above, and in §2's
	// column-unknown-variant row in docs/specs/codegen-sentinel-taxonomy.md.
	// Review measured that deleting the case leaves every package green and
	// all three citations pointing at nothing. This is what reddens instead.
	//
	// The shapes are held beside the names because a name is not what those
	// citations are about. Both cases assert the same `resolved as node`, so
	// giving value-embedder a plain *resolver.ResolvedNode leaves the suite
	// green under an unchanged name, and the three citations would then point
	// at a case that embeds nothing — the review that added the name pin
	// measured that too.
	names := make([]string, 0, len(cases))
	shapes := make(map[string]resolver.ResolvedType, len(cases))
	for _, tc := range cases {
		names = append(names, tc.name)
		shapes[tc.name] = tc.typ
	}
	require.Equal(t, []string{"pointer-form", "value-embedder"}, names)
	require.IsType(t, &resolver.ResolvedNode{}, shapes["pointer-form"])
	require.IsType(t, embeddedNode{}, shapes["value-embedder"])

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

// panicNilString shadows the promoted String() with one that panics with
// a nil value, which the module's own code never does. It exists to
// reach the one case recover()'s return value cannot distinguish.
type panicNilString struct{ resolver.ResolvedNode }

func (panicNilString) String() string { panic(nil) }

var _ resolver.ResolvedType = panicNilString{}

// TestUnmatchedResolvedTypeNamesATypeThatPanickedWithNil pins the one
// fault a `recover() != nil` guard reads as a success. Go 1.21 made
// panic(nil) raise a *runtime.PanicNilError, so recover() returns
// non-nil for it by default — but that conversion is a GODEBUG setting,
// and under GODEBUG=panicnil=1 recover() returns nil for a call that
// very much did fault. A helper whose whole job is to always produce a
// name would then produce the empty string: `resolved as ` with nothing
// after it.
//
// So the fallback is decided by whether a name came back, not by
// recover()'s value. Measured: with the guard written as
// `if recover() != nil`, this case renders `resolved as ` and reddens.
//
// t.Setenv is why this test is not parallel. Go re-reads GODEBUG on
// os.Setenv, and top-level parallel tests are paused while a sequential
// one runs, so no other case observes the change.
func TestUnmatchedResolvedTypeNamesATypeThatPanickedWithNil(t *testing.T) {
	t.Setenv("GODEBUG", "panicnil=1")

	err := generateUnmatched(t, unmatchedColumnQuery(resolver.Column{Name: "n", Type: panicNilString{}}))
	require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
	require.EqualError(t, err, `out of C6 scope: query "Fetch" column 0 "n" resolved as codegen_test.panicNilString`)
}

// emptyNameString shadows the promoted String() with one that returns the
// empty string. It is the sibling of panicNilString above: each is named
// after what its String() does, and each reaches a case the other cannot.
type emptyNameString struct{ resolver.ResolvedNode }

func (emptyNameString) String() string { return "" }

var _ resolver.ResolvedType = emptyNameString{}

// TestUnmatchedResolvedTypeNamesATypeWhoseStringIsEmpty pins the case every
// guard above lets through. All of them bound a FAULT; this String() does
// not fault, so it is answered, and an answer is taken at its word.
//
// The distinction that matters is between the two ways of producing no name.
// A fault announces itself and the recover renders %T instead. An empty
// answer announces nothing: it satisfies the interface, returns normally,
// and leaves a refusal ending mid-sentence —
//
//	out of C6 scope: query "Fetch" column 0 "n" resolved as
//
// which is the measurement on this bead, and reads to whoever hits it as a
// truncated log line rather than as a type that declined to name itself.
// So a name that is empty is treated as no name and falls back with the
// faults, on the ground that the helper's contract is to always produce one.
//
// Nothing here says the empty string is the only unhelpful answer. A
// String() returning " " or "???" is passed through, and deliberately: this
// package cannot referee whether another package's tag is meaningful, only
// whether it said anything at all.
func TestUnmatchedResolvedTypeNamesATypeWhoseStringIsEmpty(t *testing.T) {
	t.Parallel()

	err := generateUnmatched(t, unmatchedColumnQuery(resolver.Column{Name: "n", Type: emptyNameString{}}))
	require.ErrorIs(t, err, codegen.ErrOutOfC6Scope)
	require.EqualError(t, err, `out of C6 scope: query "Fetch" column 0 "n" resolved as codegen_test.emptyNameString`)

	// The same value through the list-element fail-site, which renders by a
	// different call site in prepare.go. One helper serves both, so a repair
	// confined to the column arm would leave this one ending mid-sentence.
	listCol := resolver.Column{Name: "xs", Type: resolver.ResolvedList{Element: emptyNameString{}}}
	listErr := generateUnmatched(t, unmatchedColumnQuery(listCol))
	require.ErrorIs(t, listErr, codegen.ErrOutOfC6Scope)
	require.EqualError(t, listErr, `query "Fetch" column 0 "xs": out of C6 scope: list element has unknown resolved type codegen_test.emptyNameString`)
}
