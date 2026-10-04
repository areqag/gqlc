//go:build codegen_live

// The live half of bd gqlc-k1dg, through the emitted package of the
// list_expression_declared_elements fixture against the digest-pinned AGE
// image.
//
// A list EXPRESSION column — a list literal or collect() — carries no width of
// its own, so its element decoder used to be named from the Go text alone. A
// closed union's text is `any`, and the column read each element through
// agtypeValue: a DATE member came back as its ISO string and an INT32 member
// as int64. A record element failed generation outright. The rows read each
// column back and assert each element as its declared member's Go type.
//
// The graph is seeded with a Cypher literal rather than through the generated
// encoder, so the decoder is measured against what the server stores and not
// against its own encoder.
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

	listexprage "github.com/areqag/gqlc/test/data/codegen/valid/list_expression_declared_elements/golden/apache-age-pgx-v5"
)

const listExpressionSeed = `
	CREATE (:Account {id: 1, either: '2024-01-02', narrow: 5, n32: 7,
		home: {zip_code: 12345, note: 'x'}, moves: [{zip_code: 1}],
		dates: ['2024-01-02', 7], nest: [[{zip_code: 2}]]})
`

// TestAGEDecodesAListExpressionElementByItsWidth reads list expression columns
// of union and record elements back from a live AGE server.
func TestAGEDecodesAListExpressionElementByItsWidth(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	const graph = "gqlc_list_expression"
	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	pool := openAGEPool(ctx, t, ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase), ageSessionInit)

	q := listexprage.New(pool, graph)
	require.NoError(t, q.EnsureGraph(ctx), "ensure graph %s", graph)
	t.Cleanup(func() { require.NoError(t, q.DropGraph(ctx), "drop graph %s", graph) })

	stmt := "SELECT * FROM ag_catalog.cypher('" + graph + "', $seed$" + listExpressionSeed + "$seed$) AS (v ag_catalog.agtype)"
	_, err := pool.Exec(ctx, stmt)
	require.NoError(t, err, "seed the graph")

	jan2 := listexprage.Date{Year: 2024, Month: 1, Day: 2}
	note := "x"
	seven := int32(7)

	t.Run("a list literal reads each union element as its member", func(t *testing.T) {
		rows, err := q.AccountLiterals(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, []any{jan2}, rows[0].Eithers, "a DATE member must come back a Date, not its ISO string")
		require.Equal(t, []any{int32(5)}, rows[0].Narrows, "an INT32 member must come back int32, not agtypeValue's int64")
		require.Equal(t, []*int32{&seven}, rows[0].N32s)
		require.Len(t, rows[0].Homes, 1)
		require.NotNil(t, rows[0].Homes[0])
		require.Equal(t, int32(12345), rows[0].Homes[0].ZipCode)
		require.Equal(t, &note, rows[0].Homes[0].Note)
	})

	t.Run("collect reads each union element as its member", func(t *testing.T) {
		rows, err := q.AccountCollected(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, []any{jan2}, rows[0].Eithers)
		require.Len(t, rows[0].Homes, 1)
		require.NotNil(t, rows[0].Homes[0])
		require.Equal(t, int32(12345), rows[0].Homes[0].ZipCode)
	})

	t.Run("a list literal of lists reads its inner union and record elements", func(t *testing.T) {
		rows, err := q.AccountNested(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Len(t, rows[0].Dates, 1)
		require.NotNil(t, rows[0].Dates[0])
		require.Equal(t, []any{jan2, int64(7)}, *rows[0].Dates[0])
		require.Len(t, rows[0].Moves, 1)
		require.NotNil(t, rows[0].Moves[0])
		require.Len(t, *rows[0].Moves[0], 1)
		require.Equal(t, int32(1), (*rows[0].Moves[0])[0].ZipCode)
		require.NotNil(t, rows[0].Nest)
		require.Len(t, *rows[0].Nest, 1)
		require.Len(t, *(*rows[0].Nest)[0], 1)
		require.Equal(t, int32(2), (*(*rows[0].Nest)[0])[0].ZipCode)
	})

	// In a graph of its own, so the rows above keep reading one Account.
	// An Account stored with no either makes [a.either] a list holding one
	// null, which the nullable union wrapper passes through as nil and the
	// constructor-only NOT NULL one would refuse.
	t.Run("a null union element in a list literal reads back as nil", func(t *testing.T) {
		const bare = "gqlc_list_expression_null"
		qn := listexprage.New(pool, bare)
		require.NoError(t, qn.EnsureGraph(ctx), "ensure graph %s", bare)
		t.Cleanup(func() { require.NoError(t, qn.DropGraph(ctx), "drop graph %s", bare) })
		_, err := pool.Exec(ctx, "SELECT * FROM ag_catalog.cypher('"+bare+
			"', $seed$ CREATE (:Account {id: 2}) $seed$) AS (v ag_catalog.agtype)")
		require.NoError(t, err, "seed the graph")

		rows, err := qn.AccountLiterals(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, []any{nil}, rows[0].Eithers)
		require.Equal(t, []any{nil}, rows[0].Narrows)
	})
}
