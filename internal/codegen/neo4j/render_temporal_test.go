package neo4j_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
)

// paramsOf wraps parameter fields in the smallest Prepared temporalUses reads.
// Entities and RowFields are left empty so every flag the rows below assert can
// only have come from the parameter walk.
func paramsOf(params ...codegen.Param) codegen.Prepared {
	return codegen.Prepared{Queries: []codegen.Query{{ParamFields: params}}}
}

// TestTemporalUsesAccumulatesListPtrRegardlessOfParameterOrder pins the OR in
// render_temporal.go's list-parameter arm as an accumulator rather than an
// assignment.
//
// The distinction is invisible to every committed fixture: it needs one carrier
// bound as both a nullable and a non-nullable list parameter in one batch, and
// temporal_list_param binds days (non-nullable Date) beside spans (nullable
// Duration) — different carriers, so the OR never has two inputs to fold. With
// only one input, `u.listPtr = f.Nullable` and `u.listPtr = u.listPtr ||
// f.Nullable` agree.
//
// The mutant is not equivalent. Under last-wins the nullable-first order reads
// listPtr=false, so from<X>ListPtr is never emitted while paramBindExpr still
// calls it, and the generated package does not compile — a failure that would
// surface as a golden mismatch in some other package, or in a user's build.
// Both orders are asserted because only one of them is wrong: nullable-LAST
// scores listPtr=true under the mutant too, so a single-order row would pass
// over it (bd gqlc-1ddo5).
func TestTemporalUsesAccumulatesListPtrRegardlessOfParameterOrder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params []codegen.Param
	}{
		{
			name: "nullable list parameter first",
			params: []codegen.Param{
				{RawName: "a", Field: "A", GoType: "[]Date", Nullable: true},
				{RawName: "b", Field: "B", GoType: "[]Date", Nullable: false},
			},
		},
		{
			name: "nullable list parameter last",
			params: []codegen.Param{
				{RawName: "a", Field: "A", GoType: "[]Date", Nullable: false},
				{RawName: "b", Field: "B", GoType: "[]Date", Nullable: true},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uses := neo4j.TemporalUses(paramsOf(tc.params...))

			use, ok := uses["Date"]
			require.True(t, ok, "Date reached no emission site at all; uses = %v", uses)
			require.True(t, use.List, "the list helper is not reached, so there is nothing for listPtr to qualify")
			require.True(t, use.ListPtr,
				"listPtr is false, so from<X>ListPtr is not emitted while paramBindExpr still calls it. "+
					"One nullable list parameter in the batch is enough, wherever it sits in the order")
		})
	}
}

// TestTemporalUsesIgnoresNonCarrierParameters pins the other half of the
// parameter walk: leafType strips the slice, and a leaf that is not one of
// codegen.TemporalCarriers reaches no site. []any and []byte are the two that
// matter — both are slices, so both take the list arm and would mark a carrier
// if the arm marked unconditionally (raised while reviewing PR #2287).
//
// Asserted as an empty map rather than by looking up a name: naming a key here
// would only prove that ONE spelling was ignored, and the claim is that neither
// reaches anything.
func TestTemporalUsesIgnoresNonCarrierParameters(t *testing.T) {
	uses := neo4j.TemporalUses(paramsOf(
		codegen.Param{RawName: "a", Field: "A", GoType: "[]any", Nullable: true},
		codegen.Param{RawName: "b", Field: "B", GoType: "[]byte", Nullable: false},
	))

	require.Empty(t, uses, "a non-carrier leaf reached an emission site")
}
