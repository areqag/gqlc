//go:build codegen_live

// The live half of bd gqlc-2omj, through the emitted packages of the
// list_expression_unknown_element and list_expression_record_field_element
// fixtures against the digest-pinned AGE image.
//
// A bare column the resolver types as unknown — an index into a list
// property, a field of a record property — used to be refused by this
// backend, while neo4j served it as `any` and AGE served the same unknown as
// a list element. It is now an `any` column read through agtypeValue. The rows
// read each one back from what the server stores, seeded with a Cypher literal
// rather than through the generated encoder.
//
// The name is spelled into the justfile recipe and internal/liverecipes, which
// TestEveryLiveTestIsRunByARecipeThatNamesIt holds.

package fixtures_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	recordfieldage "github.com/areqag/gqlc/test/data/codegen/valid/list_expression_record_field_element/golden/apache-age-pgx-v5"
	unknownelemage "github.com/areqag/gqlc/test/data/codegen/valid/list_expression_unknown_element/golden/apache-age-pgx-v5"
)

// TestAGEServesAnUnknownColumnAsAny reads bare unknown-typed columns back from
// a live AGE server.
func TestAGEServesAnUnknownColumnAsAny(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	pool := openAGEPool(ctx, t, ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase), ageSessionInit)

	ownGraph := func(t *testing.T, name, seed string, q interface {
		EnsureGraph(context.Context) error
		DropGraph(context.Context) error
	},
	) {
		t.Helper()
		require.NoError(t, q.EnsureGraph(ctx), "ensure graph %s", name)
		t.Cleanup(func() { require.NoError(t, q.DropGraph(ctx), "drop graph %s", name) })
		_, err := pool.Exec(ctx, "SELECT * FROM ag_catalog.cypher('"+name+"', $seed$"+seed+"$seed$) AS (v ag_catalog.agtype)")
		require.NoError(t, err, "seed graph %s", name)
	}

	// The element of a LIST<DATE> is unknown once indexed, so it reaches the
	// caller in agtypeValue's vocabulary: the ISO text AGE stores, not a Date.
	t.Run("an index into a list property reads back as agtype's value", func(t *testing.T) {
		const graph = "gqlc_unknown_column_index"
		q := unknownelemage.New(pool, graph)
		ownGraph(t, graph, `CREATE (:Account {id: 1, dates: ['2024-01-02', '2024-03-04']})`, q)

		got, err := q.AccountBareUnknown(ctx)
		require.NoError(t, err)
		require.Equal(t, []any{"2024-01-02"}, got)
	})

	t.Run("a field of a record property reads back as agtype's value", func(t *testing.T) {
		const graph = "gqlc_unknown_column_field"
		q := recordfieldage.New(pool, graph)
		ownGraph(t, graph, `CREATE (:Account {id: 1, ul: {u: 'x', n: 7}}), (:Account {id: 2, ul: {u: 9, n: 7}})`, q)

		got, err := q.AccountBareRecordField(ctx)
		require.NoError(t, err)
		require.Equal(t, []any{"x", int64(9)}, got)
	})

	// The unknown column carries no nullability, and the row field is
	// planned non-nullable on every target, so a null is refused here as
	// neo4j refuses it. Pinned so a change to that is deliberate.
	t.Run("a null unknown column is refused as non-nullable", func(t *testing.T) {
		const graph = "gqlc_unknown_column_null"
		q := unknownelemage.New(pool, graph)
		ownGraph(t, graph, `CREATE (:Account {id: 1})`, q)

		_, err := q.AccountBareUnknown(ctx)
		require.ErrorContains(t, err, `column "dated" is non-nullable but arrived null`)
	})
}
