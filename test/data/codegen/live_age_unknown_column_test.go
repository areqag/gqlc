//go:build codegen_live

// The live half of bd gqlc-2omj, through the emitted packages of the
// list_expression_unknown_element, list_expression_record_field_element and
// unknown_entity_value fixtures against the digest-pinned AGE image.
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
	unknownentityage "github.com/areqag/gqlc/test/data/codegen/valid/unknown_entity_value/golden/apache-age-pgx-v5"
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

	// head(collect(p)) and startNode(k) are typed unknown and hold a whole
	// vertex, which AGE sends annotated ::vertex. As a column and as a list
	// element alike it reads back as the map the object carries; before
	// agtypeValue read the annotation, both failed every row.
	t.Run("an unknown holding a vertex reads back as its map, as a column and as an element", func(t *testing.T) {
		const graph = "gqlc_unknown_column_entity"
		q := unknownentityage.New(pool, graph)
		ownGraph(t, graph, `CREATE (:Person {id: 1})-[:KNOWS {since: 2019}]->(:Person {id: 2})`, q)

		requirePerson := func(t *testing.T, got any, id int64) {
			t.Helper()
			m, ok := got.(map[string]any)
			require.Truef(t, ok, "want map[string]any, got %#v", got)
			require.Equal(t, "Person", m["label"])
			require.IsType(t, int64(0), m["id"], "the graphid")
			require.Equal(t, map[string]any{"id": id}, m["properties"])
		}

		first, err := q.FirstPerson(ctx)
		require.NoError(t, err)
		requirePerson(t, first, 1)

		knows, err := q.FirstKnows(ctx)
		require.NoError(t, err)
		k, ok := knows.(map[string]any)
		require.Truef(t, ok, "want map[string]any, got %#v", knows)
		require.Equal(t, "KNOWS", k["label"])
		require.Equal(t, map[string]any{"since": int64(2019)}, k["properties"])
		require.IsType(t, int64(0), k["start_id"])
		require.IsType(t, int64(0), k["end_id"])

		origins, err := q.KnowsStarts(ctx)
		require.NoError(t, err)
		require.Len(t, origins, 1)
		require.Len(t, origins[0], 1)
		requirePerson(t, origins[0][0], 1)
	})

	// The unknown column says nothing about nullability (bd gqlc-14u0l): a
	// missing property and an out-of-range index are nulls the graph holds.
	t.Run("a null unknown column reads back as nil", func(t *testing.T) {
		const graph = "gqlc_unknown_column_null"
		q := unknownelemage.New(pool, graph)
		ownGraph(t, graph, `CREATE (:Account {id: 1}), (:Account {id: 2, dates: []})`, q)

		got, err := q.AccountBareUnknown(ctx)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Nil(t, got[0], "id 1 has no dates property")
		require.Nil(t, got[1], "id 2 has an empty dates list, so index 0 is out of range")
	})
}
