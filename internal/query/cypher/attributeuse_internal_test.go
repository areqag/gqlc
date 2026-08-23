package cypher

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/query"
)

// foreignUse inhabits query.Use without being one of the three variants
// attributeUse dispatches on. It is what bead gqlc-vt45 (from PR #916) says an
// unexported marker with value receivers does NOT prevent: the marker seals
// DECLARATION of the interface, not INHABITATION of it — embedding the
// interface satisfies isUse/Part/Branch from any package in the module and
// matches no `case query.PropertyUse:` arm. So control really does reach
// attributeUse's default.
type foreignUse struct{ query.Use }

// TestAttributeUseStampsEveryKnownVariant is the direction that must keep
// working: each variant attributeUse knows is returned with the caller's
// (part, branch) written onto it, whatever coordinate it arrived carrying. The
// arrival coordinates below are deliberately non-zero and different from the
// stamped ones, so a default-arm pass-through cannot pass this by accident.
func TestAttributeUseStampsEveryKnownVariant(t *testing.T) {
	const part, branch = 3, 7

	for _, tc := range []struct {
		name string
		in   query.Use
	}{
		{"PropertyUse", query.NewPropertyUseAt(query.Ref{Variable: "a", Property: "age"}, 1, 2)},
		{"ExprUse", query.NewExprUseAt(query.TypeInt{}, query.ExprInProjection, 1, 2)},
		{"ClauseSlotUse", query.NewClauseSlotUseAt(query.ClauseSlotLimit, 1, 2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEqual(t, part, tc.in.Part(), "the input must arrive with a DIFFERENT coordinate, or pass-through would pass")
			got := attributeUse(tc.in, part, branch)
			require.IsType(t, tc.in, got, "attributeUse is sum-preserving: the variant must not change")
			require.Equal(t, part, got.Part())
			require.Equal(t, branch, got.Branch())
		})
	}
}

// TestAttributeUseRefusesAForeignUse is the other direction, and the one bead
// gqlc-vt45 site 1 is about. The default arm used to return its argument
// UNCHANGED, so a Use variant this switch does not know passed through carrying
// whatever (branch, part) it already had — reachable and silent, and the
// coordinate is what the resolver keys parameter-type unification on, so the
// wrong value is worse here than no value.
//
// It is spelled as a panic rather than an error because attributeUse has no
// error channel and its two call sites (addParameterUse, and
// addParameterUseUnsuppressed) have none either. The limit that makes a panic
// affordable is not that the arm is unreachable — inhabitation is open, which is
// the whole point above — but that every Use reaching either call site is
// constructed inside this package, so reaching the arm takes a change to this
// package's own code and not a query. Loud at test time beats a silently
// mis-attributed parameter in a shipped column.
func TestAttributeUseRefusesAForeignUse(t *testing.T) {
	require.PanicsWithValue(t,
		"cypher bug: attributeUse cannot stamp a coordinate onto query.Use variant cypher.foreignUse",
		func() { attributeUse(foreignUse{}, 3, 7) },
		"a Use this switch does not know must be refused loudly, not returned with its old coordinate")
}
