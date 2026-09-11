package resolver

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/schema"
)

// TestSelectPartScopeDispatchesOnUseKind pins selectPartScope's two arms
// directly, because the corpus does not.
//
// Provenance: this is the unit row ruling-4w5-semantic-scope-attribution.md §6
// mutation row 5 calls for. That row mutates selectPartScope by deleting its
// query.PropertyUse type assertion, so every Use reads its own Part and the
// range check below applies to all three Use kinds. Run against the whole
// module on the gqlc-dvd1 branch, the mutant SURVIVED: `go test ./...` exited
// 0. Nothing in the corpus — not the WITH-trailing-WHERE fixtures this branch
// adds, not the pre-existing ExprUse or ClauseSlotUse fixtures — observes the
// difference, so the Part-agnostic arm shipped unwitnessed. The row's own text
// names that outcome and asks for this test rather than for a code change.
//
// The three cases are not interchangeable. Case 1 kills the range-check half of
// the mutant, case 2 kills its dispatch half, and case 3 is the control that
// stops the whole test from passing vacuously on a function that returned a
// zero partScope unconditionally.
func TestSelectPartScopeDispatchesOnUseKind(t *testing.T) {
	// A branch of exactly one Part, whose scope is distinguishable from the
	// zero partScope by a non-nil (if empty) map.
	real0 := partScope{nodeTypes: map[string]schema.NodeType{}}
	branchScopes := []partScope{real0}

	t.Run("ClauseSlotUse ignores its Part, including an out-of-range one", func(t *testing.T) {
		// Part 7 is out of range for a one-Part branch. A PropertyUse at that
		// index is model corruption and must error (case 3's sibling); a
		// ClauseSlotUse must not, because the Part axis is not a witness
		// discriminator for it (scope.go §2.3).
		got, err := selectPartScope(query.NewClauseSlotUseAt(query.ClauseSlotLimit, 7, 0), branchScopes)
		require.NoError(t, err, "a Part-agnostic Use must not be range-checked against branchScopes")
		require.Nil(t, got.nodeTypes, "a Part-agnostic Use must get the ZERO partScope")
	})

	t.Run("ExprUse ignores its Part even when the Part is in range", func(t *testing.T) {
		// The out-of-range case alone would still pass if selectPartScope
		// merely clamped. This one fails unless the dispatch is on the Use's
		// TYPE: Part 0 is in range and names real0, and the Part-agnostic arm
		// must still hand back the zero value rather than real0.
		got, err := selectPartScope(query.NewExprUseAt(query.TypeBool{}, query.ExprInPredicate, 0, 0), branchScopes)
		require.NoError(t, err)
		require.Nil(t, got.nodeTypes, "an in-range Part must not make ExprUse read branchScopes")
	})

	t.Run("PropertyUse reads the scope its Part names", func(t *testing.T) {
		// The control. Without it a selectPartScope that returned
		// partScope{}, nil for everything would satisfy both cases above.
		got, err := selectPartScope(query.NewPropertyUseAt(query.Ref{Variable: "a", Property: "id"}, 0, 0), branchScopes)
		require.NoError(t, err)
		require.NotNil(t, got.nodeTypes, "PropertyUse must get the Part-attributed scope, not the zero value")

		_, err = selectPartScope(query.NewPropertyUseAt(query.Ref{Variable: "a", Property: "id"}, 7, 0), branchScopes)
		require.ErrorIs(t, err, ErrOutOfR0Scope, "an out-of-range PropertyUse Part is decoder/model corruption")
	})
}
