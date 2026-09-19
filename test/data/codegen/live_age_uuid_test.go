//go:build codegen_live

// The Apache AGE half of the UUID width (bd gqlc-ytf9, ADR 0047). The neo4j
// half is TestNeo4jStoresAndRoundTripsAUUID and the rows here are its rows,
// asked of the other store: that a UUID written through generated code is
// STORED, as its RFC 9562 text in an agtype string, and ROUND-TRIPS through
// each read position as the standard library's uuid.UUID.
//
// ONE THING IS UNDER TEST HERE THAT THE NEO4J HALF HAS NO COUNTERPART FOR.
// This backend emits NO UUID ENCODER. A parameter crosses through the JSON
// encoder every argument does, and the claim is that uuid.UUID marshals
// itself as the text agtypeUUID reads — bare, behind a pointer, as a list
// element, behind a nil, and inside the `any` a union carries. That is a
// claim about encoding/json and the uuid package rather than about anything
// gqlc emitted, so nothing short of a write that lands and is read back RAW
// holds it. The storage row is that write.
//
// THE STORAGE WITNESS IS RAW, the write is generated, for the reason the
// neo4j half gives: a store observed only through the emission under test
// could be wrong in agreement with it.
//
// A NIL LIST ELEMENT IS A ROW HERE and could not be one on neo4j, whose
// property arrays hold no null. agtype's lists do, so trail carries one and
// agtypeNullableElem's nil arm is reached by a value that came back from the
// server.
//
// VERSION 7 THROUGHOUT, minted by uuid.NewV7, and the ordering row says the
// stored text keeps its creation order under this store's string comparison.
//
// The name is spelled into the test-codegen-live-age recipe in the justfile
// and declared ArmAGE in internal/liverecipes.

package fixtures_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	uuidage "github.com/areqag/gqlc/test/data/codegen/valid/uuid_property/golden/apache-age-pgx-v5"
)

const ageUUIDGraph = "gqlc_uuid_property"

// ageUUIDSpanMicros is one second in the stored DURATION encoding, for the
// accounts the raw seeds below create. Account.Span is non-nullable, so the
// entity decode refuses a vertex without it.
const ageUUIDSpanMicros = "1000000"

// TestAGEStoresAndRoundTripsAUUID is the live half of bd gqlc-ytf9.
func TestAGEStoresAndRoundTripsAUUID(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	pool := openAGEPool(ctx, t, ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase), ageSessionInit)

	q := uuidage.New(pool, ageUUIDGraph)
	require.NoError(t, q.EnsureGraph(ctx), "ensure graph %s", ageUUIDGraph)
	t.Cleanup(func() { require.NoError(t, q.DropGraph(ctx), "drop graph %s", ageUUIDGraph) })

	// Rows share one graph and each opens by wiping it, so they are not
	// parallel among themselves.
	wipe := func(t *testing.T) {
		t.Helper()
		ageUUIDExec(ctx, t, pool, "MATCH (n) DETACH DELETE n")
	}
	span := uuidage.Duration{Seconds: 1}

	// full is an account with every UUID position filled, each by its own
	// version 7 value so a decode that crossed two positions cannot pass.
	// The union's member is handed back beside it, already typed.
	full := func(id int64) (uuidage.OpenAccountParams, uuid.UUID) {
		member := uuid.NewV7()
		var either any = member
		return uuidage.OpenAccountParams{
			Id:     id,
			Ref:    uuid.NewV7(),
			Prior:  ptr(uuid.NewV7()),
			Trail:  ptr([]*uuid.UUID{ptr(uuid.NewV7()), nil, ptr(uuid.NewV7())}),
			Chain:  ptr([]uuid.UUID{uuid.NewV7(), uuid.NewV7()}),
			Either: &either,
			Span:   span,
		}, member
	}
	asEntity := func(p uuidage.OpenAccountParams) uuidage.Account {
		return uuidage.Account{Id: p.Id, Ref: p.Ref, Prior: p.Prior, Trail: p.Trail, Chain: p.Chain, Either: p.Either, Span: p.Span}
	}

	t.Run("a UUID is stored in a property slot, as its RFC 9562 text", func(t *testing.T) {
		wipe(t)
		want, member := full(1)
		require.NoError(t, q.OpenAccount(ctx, want),
			"the server refused the write, so a UUID property cannot be stored the way this backend carries it")

		raw := agtypeOneValue(ctx, t, pool, ageUUIDGraph,
			"MATCH (a:Account {id: 1}) RETURN {ref: a.ref, prior: a.prior, trail: a.trail, chain: a.chain, either: a.either}")
		var got map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &got), "the projected map must be JSON-shaped agtype: %s", raw)

		require.Equal(t, want.Ref.String(), got["ref"],
			"the slot must hold the canonical lowercase text as an agtype string; this is encoding/json's answer "+
				"for a bare uuid.UUID, which no gqlc encoder stands in front of")
		require.Equal(t, want.Prior.String(), got["prior"], "the same, behind a pointer")
		require.Equal(t, []any{(*want.Trail)[0].String(), nil, (*want.Trail)[2].String()}, got["trail"],
			"list elements, with the nil element stored as an agtype null and not as the zero UUID's text")
		require.Equal(t, []any{(*want.Chain)[0].String(), (*want.Chain)[1].String()}, got["chain"])
		require.Equal(t, member.String(), got["either"],
			"inside the `any` a union carries, where the encoder sees the dynamic type")
	})

	t.Run("a stored UUID reads back equal through every read position", func(t *testing.T) {
		wipe(t)
		want, _ := full(1)
		require.NoError(t, q.OpenAccount(ctx, want))

		whole, err := q.AccountWhole(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, asEntity(want), whole, "the whole-entity decode, the nil list element included")

		cols, err := q.AccountColumns(ctx)
		require.NoError(t, err)
		require.Equal(t, []uuidage.AccountColumnsRow{{
			Ref: want.Ref, Prior: want.Prior, Trail: want.Trail, Chain: want.Chain, Either: want.Either, Span: want.Span,
		}}, cols, "the column decode")

		ref, err := q.AccountRef(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, want.Ref, ref, "the :one column decode")
	})

	t.Run("a null UUID round-trips as nil and not as the zero UUID", func(t *testing.T) {
		wipe(t)
		want := uuidage.OpenAccountParams{Id: 1, Ref: uuid.NewV7(), Span: span}
		require.NoError(t, q.OpenAccount(ctx, want))

		raw := agtypeOneValue(ctx, t, pool, ageUUIDGraph, "MATCH (a:Account {id: 1}) RETURN a.prior IS NULL")
		require.Equal(t, "true", raw,
			"a nil *UUID must bind null; the zero UUID's text stored here is encoding/json no longer writing a nil pointer as null")

		whole, err := q.AccountWhole(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, asEntity(want), whole)
	})

	t.Run("a union's INT64 member still reads back as itself beside a UUID one", func(t *testing.T) {
		wipe(t)
		var seven any = int64(7)
		want := uuidage.OpenAccountParams{Id: 1, Ref: uuid.NewV7(), Either: &seven, Span: span}
		require.NoError(t, q.OpenAccount(ctx, want))

		whole, err := q.AccountWhole(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, asEntity(want), whole,
			"the union decode dispatches on the wire family, so an integer must not be offered to agtypeUUID")
	})

	t.Run("a UUID parameter matches the vertex that holds it and no other", func(t *testing.T) {
		wipe(t)
		held, _ := full(1)
		other, _ := full(2)
		require.NoError(t, q.OpenAccount(ctx, held))
		require.NoError(t, q.OpenAccount(ctx, other))

		byRef, err := q.AccountByRef(ctx, held.Ref)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, byRef, "the bare parameter")

		byPrior, err := q.AccountByPrior(ctx, held.Prior)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, byPrior, "the nullable parameter")

		byChain, err := q.AccountByChain(ctx, held.Chain)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, byChain, "the list parameter")

		// The bound list holds a null element, and this store matches it
		// against the stored list all the same — measured, because Cypher's
		// null semantics would have let it go either way.
		byTrail, err := q.AccountByTrail(ctx, held.Trail)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, byTrail, "the list of nullable elements, the null one included")

		// The control: every row above is satisfied by a predicate that
		// matched everything.
		none, err := q.AccountByRef(ctx, uuid.NewV7())
		require.NoError(t, err)
		require.Empty(t, none, "a UUID no vertex holds must match no vertex")
	})

	t.Run("a stored string that is not a UUID fails the read and says why", func(t *testing.T) {
		wipe(t)
		ageUUIDExec(ctx, t, pool, "CREATE (:Account {id: 1, ref: 'not-a-uuid', span: "+ageUUIDSpanMicros+"})")

		_, err := q.AccountRef(ctx, 1)
		require.Error(t, err, "the read handed back a UUID for text that is not one")
		require.ErrorContains(t, err, notAUUIDRefusal,
			"the read failed for some other reason, so nothing here witnesses the parse: %v", err)

		_, err = q.AccountWhole(ctx, 1)
		require.ErrorContains(t, err, notAUUIDRefusal, "the entity decode is a second site and owes the same refusal")
	})

	// The limit the neo4j half measures, measured here too: the read is
	// lenient because uuid.Parse is, the match is exact because the store
	// compares strings.
	t.Run("an uppercase stored UUID reads back, and is not matched by parameter", func(t *testing.T) {
		wipe(t)
		want := uuid.NewV7()
		ageUUIDExec(ctx, t, pool,
			"CREATE (:Account {id: 1, ref: '"+strings.ToUpper(want.String())+"', span: "+ageUUIDSpanMicros+"})")

		got, err := q.AccountRef(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, want, got)

		ids, err := q.AccountByRef(ctx, want)
		require.NoError(t, err)
		require.Empty(t, ids,
			"the store matched across case; ADR 0047 records this as a limit of the string form and is now wrong about it")
	})

	t.Run("version 7 UUIDs sort in creation order under ORDER BY", func(t *testing.T) {
		wipe(t)
		minted := make([]uuid.UUID, 5)
		for i := range minted {
			minted[i] = uuid.NewV7()
		}
		// Neither the insertion order nor the ids agree with the mint order,
		// so a store handing rows back in either of those cannot pass.
		ids := []int64{40, 10, 50, 20, 30}
		for _, i := range []int{3, 0, 4, 1, 2} {
			require.NoError(t, q.OpenAccount(ctx, uuidage.OpenAccountParams{Id: ids[i], Ref: minted[i], Span: span}))
		}

		raw := agtypeOneValue(ctx, t, pool, ageUUIDGraph,
			"MATCH (a:Account) WITH a ORDER BY a.ref RETURN collect(a.id)")
		var got []int64
		require.NoError(t, json.Unmarshal([]byte(raw), &got), "the collected ids must be a JSON-shaped agtype list: %s", raw)
		require.Equal(t, ids, got)
	})
}

// ageUUIDExec runs one cypher statement through cypher() directly, for the
// wipes and the seeds the rows must not make through generated code. The
// graph name is composed into the statement because cypher() takes a constant
// there; it is this file's own constant.
func ageUUIDExec(ctx context.Context, t *testing.T, pool *pgxpool.Pool, cypher string) {
	t.Helper()
	stmt := "SELECT * FROM ag_catalog.cypher('" + ageUUIDGraph + "', $seed$" + cypher + "$seed$) AS (v ag_catalog.agtype)"
	_, err := pool.Exec(ctx, stmt)
	require.NoError(t, err, "run: %s", cypher)
}
