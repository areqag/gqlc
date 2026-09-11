package backends_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// TestGenerationStillRefusesAUnionBeforeTheTable is a tripwire, not a
// claim gqlc wants to keep true.
//
// Both backends now carry a closed-union arm, and they disagree about
// UNION<DATE|STRING>: neo4j admits it because the driver hands back a
// dbtype.Date, AGE refuses it because agtype has no date scalar and the
// value is ISO text indistinguishable from a STRING. That disagreement is
// spec §8's third falsifier, and it is what makes AGE's refusal contingent
// and so obliges the message to name the backend (ADR 0035).
//
// This package cannot assert it. Both type maps are test-only exports —
// `TypeMap = typeMap` in each backend's export_test.go — so the tables are
// reachable only from their own package's tests, which is where each half
// of the cell is pinned (TestTypeMapAdmitsAWireDistinctUnion on neo4j,
// TestTypeMapRefusesAUnionWhoseMembersShareAWireFamily on AGE). The only
// cross-backend view this composition root has is GENERATION, and nothing
// generates a union yet: ErrUnimplementedTypeKind is asked first, at every
// position, and refuses every union on every backend (ADR 0039's ordering
// rule, and spec §7's between-stages state).
//
// So the ADR 0035 half of the falsifier — the backend NAME in the refusal
// text — is owed and not yet payable, and this row exists because of how
// that debt would otherwise be settled: silently. The attribution sweep
// beside this file skips any width no backend accepts, so adding a union
// to declaredWidths today contributes no assertion at all, and the cell
// would read green while testing nothing.
//
// Measured rather than assumed: with the walk's KindUnion arm returning
// false, generation succeeds for UNION<BOOL|INT64> on both backends and
// TestAContingentRefusalNamesItsBackend reports UNION<DATE|STRING> among
// the widths dividing the roster, with the name assertion passing. The
// carrier is `any`, which both backends already emit for ANY, so the
// emission largely arrives free; what does NOT arrive free is spec §4's
// bind-time member validation and decode dispatch, which is why the walk
// stays until those are built.
//
// This row fails the moment generation stops refusing unions, and its
// message says what to do then.
func TestGenerationStillRefusesAUnionBeforeTheTable(t *testing.T) {
	reg, err := backends.Registry()
	require.NoError(t, err)

	keys := reg.Keys()
	require.NotEmpty(t, keys, "an empty roster asserts nothing below")

	// Admitted by the neo4j carrier table, so this is the width most
	// likely to start generating first, and the one the sweep would skip.
	pt := graph.UnionOf([]graph.UnionMember{{Type: graph.TypeDate}, {Type: graph.TypeString}})
	require.Equal(t, graph.KindUnion, pt.Kind())

	for _, key := range keys {
		newGen, ok := reg.Lookup(key)
		require.True(t, ok, "registry key %q reported by Keys does not resolve through Lookup", key)
		_, err := newGen("widths").Generate(codegen.Input{Schema: schemaWithPayload(pt)})
		require.ErrorIs(t, err, codegen.ErrUnimplementedTypeKind,
			"%s now generates %s. That is the intended end state, and this tripwire is how you are told the "+
				"attribution sweep no longer covers it: add the union encodings to declaredWidths so "+
				"TestAContingentRefusalNamesItsBackend holds the ADR 0035 half of spec §8's falsifier, then "+
				"delete this test", key, pt)
	}
}
