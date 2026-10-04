//go:build codegen_live

// The live halves of bd gqlc-3s7q and bd gqlc-3jhv, through the emitted
// package of the union_list_two_widths fixture against the digest-pinned AGE
// image.
//
// gqlc-3s7q: two LIST<ANY<…>> properties of different member sets, and a
// LIST<ANY VALUE> beside them, in one entity. Their wrappers used to be one
// agtypeListOfAny bound to the first width's element decoder, so the others
// decoded through the wrong dispatcher. The rows read each list back and
// assert each element comes back as ITS OWN union's member: an INT32 element
// as int32, not as the other union's int64, and a STRING one at all.
//
// gqlc-3jhv: a NULL element in a list of unions. Every such list a schema can
// spell has nullable elements, and both directions refused the null. The rows
// write one through the generated encoder and read it back through the
// generated decoder; a SEEDED row, written as a Cypher literal, reads one back
// without the encoder in the loop, so a decoder that agreed only with its own
// encoder could not pass.
//
// One row reads its null through union_only_carrier_list's package instead.
// That package has ONE union list, so its wrapper was bound to the right
// decoder even before the gqlc-3s7q fix, and the row isolates the null: in
// the two-widths package before the fix, the lists that reached agtypeValue
// passed a null for the wrong reason.
//
// The bare nullable union PROPERTY is the third question gqlc-3jhv asked: a
// Row stored with no union property reads back nil at both the property and
// the column position, through union_property's emitted package. Nothing in
// the union decoder is reached by it — AGE never hands back a null-valued
// property (TestAGENeverHandsBackANullValuedProperty), and the column arrives
// as SQL NULL — and the row is the measurement that says so.
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

	unionlistage "github.com/areqag/gqlc/test/data/codegen/valid/union_list_two_widths/golden/apache-age-pgx-v5"
	carrierlistage "github.com/areqag/gqlc/test/data/codegen/valid/union_only_carrier_list/golden/apache-age-pgx-v5"
	unionpropage "github.com/areqag/gqlc/test/data/codegen/valid/union_property/golden/apache-age-pgx-v5"
)

// unionListSeed writes the row whose elements are spelled as STORED literals
// rather than through the generated encoder. A DATE is stored as zero-padded
// ISO text, which is what the emitted agtypeDate reads.
const unionListSeed = `
	CREATE (:Ledger {id: 2, dates: ['2024-01-02', null, 7], tags: ['a', null, 1], bag: [null, 'x']})
`

// TestAGERoundTripsANullElementInAUnionList writes and reads back lists of
// closed unions with a null element in them.
//
// Each subtest works in a graph of its own and puts there every row it
// reads, so `-run` can pick out any one of them (bd gqlc-5hey).
func TestAGERoundTripsANullElementInAUnionList(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	pool := openAGEPool(ctx, t, ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase), ageSessionInit)

	// ownGraph creates the named graph through one generated package, drops
	// it when the subtest ends, and runs the seed, if any, in it.
	ownGraph := func(t *testing.T, name, seed string, q interface {
		EnsureGraph(context.Context) error
		DropGraph(context.Context) error
	},
	) {
		t.Helper()
		require.NoError(t, q.EnsureGraph(ctx), "ensure graph %s", name)
		t.Cleanup(func() { require.NoError(t, q.DropGraph(ctx), "drop graph %s", name) })
		if seed == "" {
			return
		}
		_, err := pool.Exec(ctx, "SELECT * FROM ag_catalog.cypher('"+name+"', $seed$"+seed+"$seed$) AS (v ag_catalog.agtype)")
		require.NoError(t, err, "seed graph %s", name)
	}

	jan2 := unionlistage.Date{Year: 2024, Month: 1, Day: 2}
	wantDates := []any{jan2, nil, int64(7)}
	wantTags := []any{"a", nil, int32(1)}

	// encodeLedger writes Ledger 3, whose union lists hold a null element,
	// through the generated encoder.
	encodeLedger := func(t *testing.T, lists *unionlistage.Queries) {
		t.Helper()
		require.NoError(t, lists.CreateLedger(ctx, unionlistage.CreateLedgerParams{
			Id:    3,
			Dates: &[]any{jan2, nil, int64(7)},
			Tags:  &[]any{"a", nil, int32(1)},
		}), "bind union lists holding a null element")
	}

	t.Run("a seeded null element reads back as nil, each list through its own union", func(t *testing.T) {
		const graph = "gqlc_union_list_seeded"
		lists := unionlistage.New(pool, graph)
		ownGraph(t, graph, unionListSeed, lists)

		got, err := lists.LedgerWhole(ctx, 2)
		require.NoError(t, err, "read a Ledger whose union lists hold a null element")
		require.NotNil(t, got.Dates)
		require.NotNil(t, got.Tags)
		require.Equal(t, wantDates, *got.Dates)
		require.Equal(t, wantTags, *got.Tags,
			"1 is an INT32 member of tags and must come back int32; int64 is the OTHER union's integer member")
		require.NotNil(t, got.Bag)
		require.Equal(t, []any{nil, "x"}, *got.Bag)
	})

	t.Run("a null element written through the encoder reads back as nil", func(t *testing.T) {
		const graph = "gqlc_union_list_encoded"
		lists := unionlistage.New(pool, graph)
		ownGraph(t, graph, "", lists)
		encodeLedger(t, lists)

		got, err := lists.LedgerWhole(ctx, 3)
		require.NoError(t, err)
		require.NotNil(t, got.Dates)
		require.NotNil(t, got.Tags)
		require.Equal(t, wantDates, *got.Dates)
		require.Equal(t, wantTags, *got.Tags)
		require.Nil(t, got.Bag, "bag was not bound, and an absent property reads back nil")
	})

	t.Run("the column position reads the same lists", func(t *testing.T) {
		const graph = "gqlc_union_list_columns"
		lists := unionlistage.New(pool, graph)
		ownGraph(t, graph, unionListSeed, lists)
		encodeLedger(t, lists)

		rows, err := lists.LedgerLists(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 2, "one seeded Ledger and one encoded")
		for _, row := range rows {
			require.NotNil(t, row.Dates)
			require.NotNil(t, row.Tags)
			require.Equal(t, wantDates, *row.Dates)
			require.Equal(t, wantTags, *row.Tags)
		}
	})

	t.Run("a list parameter holding a null matches the stored list", func(t *testing.T) {
		const graph = "gqlc_union_list_param"
		lists := unionlistage.New(pool, graph)
		ownGraph(t, graph, unionListSeed, lists)
		encodeLedger(t, lists)

		ids, err := lists.LedgersByTags(ctx, &[]any{"a", nil, int32(1)})
		require.NoError(t, err)
		require.ElementsMatch(t, []int64{2, 3}, ids)

		ids, err = lists.LedgersByTags(ctx, &[]any{"a", int32(1)})
		require.NoError(t, err)
		require.Empty(t, ids, "a predicate that matched everything would pass the row above for the wrong reason")
	})

	t.Run("a seeded null element reads back as nil through a lone union list", func(t *testing.T) {
		const graph = "gqlc_union_list_single"
		carrier := carrierlistage.New(pool, graph)
		ownGraph(t, graph, "CREATE (:Ledger {id: 1, entries: ['2024-01-02', null, 7]})", carrier)

		got, err := carrier.LedgerWhole(ctx, 1)
		require.NoError(t, err, "read a Ledger whose union list holds a null element")
		require.NotNil(t, got.Entries)
		require.Equal(t, []any{carrierlistage.Date{Year: 2024, Month: 1, Day: 2}, nil, int64(7)}, *got.Entries)
	})

	t.Run("a bare nullable union property that was never stored reads back nil", func(t *testing.T) {
		const graph = "gqlc_union_list_property"
		props := unionpropage.New(pool, graph)
		ownGraph(t, graph, "CREATE (:Row {id: 1})", props)

		got, err := props.RowWhole(ctx)
		require.NoError(t, err)
		require.Nil(t, got.Pick)
		require.Nil(t, got.Also)
		require.Nil(t, got.Flag)

		rows, err := props.RowColumns(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Nil(t, rows[0].Pick)
		require.Nil(t, rows[0].Also)
		require.Nil(t, rows[0].Flag)
	})
}
