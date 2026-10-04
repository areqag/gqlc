//go:build codegen_live

// The live half of bd gqlc-dkcz: a list EXPRESSION of nullable closed-union
// elements, `RETURN [a.either] AS xs`, read back through generated code on
// both driver majors, an Account with no `either` among the rows.
//
// The defect was an element walk with no nil arm, so the NULL element reached
// the union's decode helper, which carries no member for nil, and the whole
// column failed. TestUnionListExpressionElementAdmitsNull holds the arm over
// the text; this holds the claim the text cannot, that the server really does
// hand a missing property inside a list expression back as a nil element, and
// that the caller reads it as nil rather than as a pointer to one.
//
// A query-built list is the only way to reach this element on neo4j: a stored
// LIST<ANY<…>> is refused (StorableProperty), and a list expression is not
// stored, so no NULL-element storage claim is made here.
//
// It runs inside TestNeo4jRoundTripsAUnionOfATemporalList, on that test's
// container, rather than as a top-level test of its own: a fourth serial boot
// in the PR-blocking half would cost ~20s of wall time for three rows.

package fixtures_test

import (
	"context"
	"testing"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"

	ulev5 "github.com/areqag/gqlc/test/data/codegen/valid/union_list_expression_element/golden/neo4j-go-v5"
	ulev6 "github.com/areqag/gqlc/test/data/codegen/valid/union_list_expression_element/golden/neo4j-go-v6"
)

// unionListExpressionSeed writes one Account per member and one holding
// neither, in the order AccountEithers reads them back.
const unionListExpressionSeed = "CREATE (:Account {id: 1, either: date('2024-02-29')}), " +
	"(:Account {id: 2, either: 7}), (:Account {id: 3})"

// unionListExpressionArm is one driver major's view of the fixture. date is
// that major's carrier for 2024-02-29, since a Date of one golden package is
// not a Date of the other.
type unionListExpressionArm struct {
	date    any
	eithers func(ctx context.Context) ([][]any, error)
	raw     func(ctx context.Context, t *testing.T, cypher string) []map[string]any
}

func runUnionListExpressionRows(ctx context.Context, t *testing.T, boltURI string) {
	t.Helper()
	arms := []struct {
		name string
		open func(ctx context.Context, t *testing.T, boltURI string) unionListExpressionArm
	}{
		{name: "neo4j-go-v5", open: openUnionListExpressionArmV5},
		{name: "neo4j-go-v6", open: openUnionListExpressionArmV6},
	}
	for _, a := range arms {
		t.Run(a.name, func(t *testing.T) {
			arm := a.open(ctx, t, boltURI)

			t.Run("the server hands a missing property back as a nil element", func(t *testing.T) {
				arm.raw(ctx, t, wipeCypher)
				arm.raw(ctx, t, unionListExpressionSeed)

				rows := arm.raw(ctx, t, "MATCH (a:Account {id: 3}) RETURN [a.either] AS xs")
				require.Len(t, rows, 1, "the seed reported success and the store holds no such node")
				require.Equal(t, []any{nil}, rows[0]["xs"],
					"the premise: the row below reads a NULL element only if the server sends one")
			})

			t.Run("a NULL union element reads back as nil beside each member", func(t *testing.T) {
				arm.raw(ctx, t, wipeCypher)
				arm.raw(ctx, t, unionListExpressionSeed)

				got, err := arm.eithers(ctx)
				require.NoError(t, err, "the column decode refused an element the schema declares nullable")
				require.Equal(t, [][]any{{arm.date}, {int64(7)}, {nil}}, got,
					"each element must read back as its member, and the missing one as a bare nil")
			})
		})
	}
}

func openUnionListExpressionArmV5(ctx context.Context, t *testing.T, boltURI string) unionListExpressionArm {
	t.Helper()
	driver, err := neo4jv5.NewDriverWithContext(boltURI, neo4jv5.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v5 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	raw := unionListTemporalArmV5{driver: driver}
	return unionListExpressionArm{
		date:    ulev5.Date{Year: 2024, Month: 2, Day: 29},
		eithers: ulev5.New(driver).AccountEithers,
		raw:     raw.raw,
	}
}

func openUnionListExpressionArmV6(ctx context.Context, t *testing.T, boltURI string) unionListExpressionArm {
	t.Helper()
	driver, err := neo4jv6.NewDriver(boltURI, neo4jv6.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v6 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	raw := unionListTemporalArmV6{driver: driver}
	return unionListExpressionArm{
		date:    ulev6.Date{Year: 2024, Month: 2, Day: 29},
		eithers: ulev6.New(driver).AccountEithers,
		raw:     raw.raw,
	}
}
