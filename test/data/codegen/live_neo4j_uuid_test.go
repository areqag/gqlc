//go:build codegen_live

// The live half of the UUID width (bd gqlc-ybk2, ADR 0047), and the two
// claims that bead was filed to measure: that a UUID can be STORED in a
// property slot at all, and that one ROUND-TRIPS through generated code.
//
// Both were unmeasurable while the width rode the driver's dbtype.UUID. That
// is a Bolt 6.1 value the v6 packer and hydrator refuse client-side on an
// older connection, and the image this repository pins speaks Bolt 5.x, so a
// live arm could only ever have measured the client's gate. Carried as the
// standard library's uuid.UUID over its RFC 9562 text, the width is a STRING
// to the server, and every row below runs against the pinned image as it is.
//
// THE STORAGE WITNESS IS RAW, the write is generated. The subject is gqlc's
// emission; a store observed only through that same emission could be wrong
// in agreement with it — a decode that parsed whatever came back would call a
// byte array a round trip. The rows that say WHAT the store holds read it
// through a plain driver session and compare it with uuid.UUID.String().
//
// BOTH DRIVER MAJORS, ONE CONTAINER. The server-side facts here are one fact
// however many drivers ask, but the emission is two goldens and each packs
// through its own vendored packstream, so each gets an arm — at the price of
// a second driver against the one container rather than a second container.
//
// VERSION 7 THROUGHOUT, minted by uuid.NewV7. Nothing in the carrier is
// specific to a version; what version 7 adds is that the text form sorts in
// creation order, and the ordering row is what says the stored form keeps
// that.
//
// NOT PARALLEL, for the reason
// TestNeo4jRefusesAUint64ParameterAboveMaxInt64 gives: it boots a container
// of its own and the neo4j half already boots four concurrently. Rows share
// one graph and each opens by wiping it.
//
// The name is spelled into the test-codegen-live-neo4j recipe's -run list and
// into internal/liverecipes.LiveArms.

package fixtures_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"

	uuidv5 "github.com/areqag/gqlc/test/data/codegen/valid/uuid_property/golden/neo4j-go-v5"
	uuidv6 "github.com/areqag/gqlc/test/data/codegen/valid/uuid_property/golden/neo4j-go-v6"
)

// notAUUIDRefusal is the emitted toUUID's own wording. Matched as a substring
// so the sentence around it may be reworded — but a read that fails for some
// other reason, or does not fail, cannot pass it.
const notAUUIDRefusal = "is not a UUID"

// uuidAccount is one :Account in primitives. The two goldens each declare
// their own Account and OpenAccountParams, but UUID in both is an ALIAS of
// uuid.UUID, so the UUID-typed fields cross between them and this struct with
// no conversion — which is itself the claim ADR 0047 makes about the carrier,
// held here by the compiler.
type uuidAccount struct {
	id     int64
	ref    uuid.UUID
	prior  *uuid.UUID
	trail  *[]*uuid.UUID
	chain  *[]uuid.UUID
	either *any
}

// uuidArm is one driver major under test: the generated surface, and the raw
// session that judges it. Function fields rather than an interface, for the
// reason uint64WriteArm gives — the majors share no driver type.
type uuidArm struct {
	open    func(ctx context.Context, a uuidAccount) error
	whole   func(ctx context.Context, id int64) (uuidAccount, error)
	columns func(ctx context.Context) ([]uuidAccount, error)
	ref     func(ctx context.Context, id int64) (uuid.UUID, error)
	byRef   func(ctx context.Context, ref uuid.UUID) ([]int64, error)
	byPrior func(ctx context.Context, prior *uuid.UUID) ([]int64, error)
	byTrail func(ctx context.Context, trail *[]*uuid.UUID) ([]int64, error)
	byChain func(ctx context.Context, chain *[]uuid.UUID) ([]int64, error)
	// raw runs one statement in its own auto-commit transaction and hands
	// each record back as the driver's own map. Deliberately not the
	// ExecuteRead/ExecuteWrite the generated code uses: the observations
	// here must not be retried into agreement with anything.
	raw func(ctx context.Context, t *testing.T, cypher string, params map[string]any) []map[string]any
}

// TestNeo4jStoresAndRoundTripsAUUID is the live half of bd gqlc-ybk2.
func TestNeo4jStoresAndRoundTripsAUUID(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	boltURI := startNeo4jContainer(ctx, t)

	arms := []struct {
		name string
		open func(ctx context.Context, t *testing.T, boltURI string) uuidArm
	}{
		{name: "neo4j-go-v5", open: openUUIDArmV5},
		{name: "neo4j-go-v6", open: openUUIDArmV6},
	}

	for _, a := range arms {
		t.Run(a.name, func(t *testing.T) {
			runUUIDRows(ctx, t, a.open(ctx, t, boltURI))
		})
	}
}

// fullUUIDAccount is an account with every UUID position filled, each by its
// own version 7 value so a decode that crossed two positions cannot pass. The
// union's member is handed back beside it, already typed, so a row comparing
// it has no assertion to make on the `any` that carries it.
//
// trail's elements are all non-nil though its element type is nullable. That
// is the server's rule and not this test's economy: a stored property array
// cannot hold a null, so a nil element is a value the write could never land.
func fullUUIDAccount(id int64) (uuidAccount, uuid.UUID) {
	member := uuid.NewV7()
	var either any = member
	return uuidAccount{
		id:     id,
		ref:    uuid.NewV7(),
		prior:  ptr(uuid.NewV7()),
		trail:  ptr([]*uuid.UUID{ptr(uuid.NewV7()), ptr(uuid.NewV7())}),
		chain:  ptr([]uuid.UUID{uuid.NewV7(), uuid.NewV7()}),
		either: &either,
	}, member
}

// runUUIDRows is the battery both majors run.
func runUUIDRows(ctx context.Context, t *testing.T, arm uuidArm) {
	t.Helper()

	wipe := func(t *testing.T) {
		t.Helper()
		arm.raw(ctx, t, wipeCypher, nil)
	}

	// Claim 2 of the bead: STORAGE. What the property slot holds after a
	// generated write, read through a raw session.
	t.Run("a UUID is stored in a property slot, as its RFC 9562 text", func(t *testing.T) {
		wipe(t)
		want, member := fullUUIDAccount(1)
		require.NoError(t, arm.open(ctx, want),
			"the server refused the write, so a UUID property cannot be stored the way this backend carries it")

		rows := arm.raw(ctx, t,
			"MATCH (a:Account {id: 1}) RETURN a.ref AS ref, a.prior AS prior, a.trail AS trail, a.chain AS chain, a.either AS either", nil)
		require.Len(t, rows, 1, "the write reported success and the store holds no such node")
		got := rows[0]

		require.Equal(t, want.ref.String(), got["ref"],
			"the slot must hold the canonical lowercase text and hold it as a string; any other Go type here is some other wire form")
		require.Equal(t, want.prior.String(), got["prior"])
		require.Equal(t, []any{(*want.trail)[0].String(), (*want.trail)[1].String()}, got["trail"])
		require.Equal(t, []any{(*want.chain)[0].String(), (*want.chain)[1].String()}, got["chain"])
		require.Equal(t, member.String(), got["either"],
			"a union's UUID member is stored as the same text a bare UUID is")
	})

	// Claim 1 of the bead: ROUND TRIP, through each read position the
	// fixture declares — the entity struct, the projected columns, and the
	// bare :one column.
	t.Run("a stored UUID reads back equal through every read position", func(t *testing.T) {
		wipe(t)
		want, _ := fullUUIDAccount(1)
		require.NoError(t, arm.open(ctx, want))

		whole, err := arm.whole(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, want, whole, "the whole-entity decode")

		cols, err := arm.columns(ctx)
		require.NoError(t, err)
		require.Len(t, cols, 1)
		// The columns projection carries no id; everything else must agree.
		cols[0].id = want.id
		require.Equal(t, want, cols[0], "the column decode")

		ref, err := arm.ref(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, want.ref, ref, "the :one column decode")
	})

	t.Run("a null UUID round-trips as nil and not as the zero UUID", func(t *testing.T) {
		wipe(t)
		want := uuidAccount{id: 1, ref: uuid.NewV7()}
		require.NoError(t, arm.open(ctx, want))

		rows := arm.raw(ctx, t, "MATCH (a:Account {id: 1}) RETURN a.prior IS NULL AS absent", nil)
		require.Equal(t, []map[string]any{{"absent": true}}, rows,
			"a nil *UUID must bind the Cypher null; a stored all-zero UUID text here is fromUUIDPtr's nil check gone")

		whole, err := arm.whole(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, want, whole)
	})

	t.Run("a union's INT64 member still reads back as itself beside a UUID one", func(t *testing.T) {
		wipe(t)
		var seven any = int64(7)
		want := uuidAccount{id: 1, ref: uuid.NewV7(), either: &seven}
		require.NoError(t, arm.open(ctx, want))

		whole, err := arm.whole(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, want, whole,
			"the union decode dispatches on the wire shape, so an int64 must not be offered to toUUID")
	})

	t.Run("a UUID parameter matches the node that holds it and no other", func(t *testing.T) {
		wipe(t)
		held, _ := fullUUIDAccount(1)
		other, _ := fullUUIDAccount(2)
		require.NoError(t, arm.open(ctx, held))
		require.NoError(t, arm.open(ctx, other))

		byRef, err := arm.byRef(ctx, held.ref)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, byRef, "the bare parameter, through fromUUID")

		byPrior, err := arm.byPrior(ctx, held.prior)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, byPrior, "the nullable parameter, through fromUUIDPtr")

		byTrail, err := arm.byTrail(ctx, held.trail)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, byTrail, "the list of nullable elements, through fromNullableUUIDListPtr")

		byChain, err := arm.byChain(ctx, held.chain)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, byChain, "the list, through fromUUIDListPtr")

		// The control: every row above is satisfied by a query that matched
		// everything and a LIMIT nobody wrote.
		none, err := arm.byRef(ctx, uuid.NewV7())
		require.NoError(t, err)
		require.Empty(t, none, "a UUID no node holds must match no node")
	})

	t.Run("a stored string that is not a UUID fails the read and says why", func(t *testing.T) {
		wipe(t)
		arm.raw(ctx, t, "CREATE (:Account {id: 1, ref: 'not-a-uuid', span: duration('PT1S')})", nil)

		_, err := arm.ref(ctx, 1)
		require.Error(t, err, "the read handed back a UUID for text that is not one")
		require.ErrorContains(t, err, notAUUIDRefusal,
			"the read failed for some other reason, so nothing here witnesses the parse: %v", err)

		_, err = arm.whole(ctx, 1)
		require.ErrorContains(t, err, notAUUIDRefusal, "the entity decode is a second site and owes the same refusal")
	})

	// A LIMIT, measured rather than left for an author to find. The read is
	// lenient because uuid.Parse is; the MATCH is exact because the server
	// compares strings. A UUID some other writer stored in another spelling
	// is therefore readable and not findable by parameter.
	t.Run("an uppercase stored UUID reads back, and is not matched by parameter", func(t *testing.T) {
		wipe(t)
		want := uuid.NewV7()
		arm.raw(ctx, t, "CREATE (:Account {id: 1, ref: $ref, span: duration('PT1S')})",
			map[string]any{"ref": strings.ToUpper(want.String())})

		got, err := arm.ref(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, want, got)

		ids, err := arm.byRef(ctx, want)
		require.NoError(t, err)
		require.Empty(t, ids,
			"the server matched across case; ADR 0047 records this as a limit of the string wire form and is now wrong about it")
	})

	t.Run("version 7 UUIDs sort in creation order under ORDER BY", func(t *testing.T) {
		wipe(t)
		minted := make([]uuid.UUID, 5)
		for i := range minted {
			minted[i] = uuid.NewV7()
		}
		// Neither the insertion order nor the ids agree with the mint order,
		// so a server handing rows back in either of those cannot pass.
		ids := []int64{40, 10, 50, 20, 30}
		for _, i := range []int{3, 0, 4, 1, 2} {
			require.NoError(t, arm.open(ctx, uuidAccount{id: ids[i], ref: minted[i]}))
		}

		rows := arm.raw(ctx, t, "MATCH (a:Account) RETURN a.id AS id ORDER BY a.ref", nil)
		got := make([]any, 0, len(rows))
		for _, row := range rows {
			got = append(got, row["id"])
		}
		want := make([]any, 0, len(ids))
		for _, id := range ids {
			want = append(want, id)
		}
		require.Equal(t, want, got)
	})
}

// uuidSpanSeconds is the DURATION every account carries. The fixture declares span
// for a reason of the emission's (see its queries.cypher); here it only has to
// be present, since Account.Span is non-nullable and the entity decode refuses
// a node without it.
const uuidSpanSeconds = 1

// uuidArmV5 drives the neo4j-go-v5 golden.
type uuidArmV5 struct {
	driver neo4jv5.DriverWithContext
	q      *uuidv5.Queries
}

func openUUIDArmV5(ctx context.Context, t *testing.T, boltURI string) uuidArm {
	t.Helper()
	driver, err := neo4jv5.NewDriverWithContext(boltURI, neo4jv5.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v5 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	a := uuidArmV5{driver: driver, q: uuidv5.New(driver)}
	return uuidArm{
		open: a.open, whole: a.whole, columns: a.columns, ref: a.q.AccountRef,
		byRef: a.q.AccountByRef, byPrior: a.q.AccountByPrior, byTrail: a.q.AccountByTrail, byChain: a.q.AccountByChain,
		raw: a.raw,
	}
}

func (a uuidArmV5) open(ctx context.Context, acct uuidAccount) error {
	return a.q.OpenAccount(ctx, uuidv5.OpenAccountParams{
		Id: acct.id, Ref: acct.ref, Prior: acct.prior, Trail: acct.trail, Chain: acct.chain, Either: acct.either,
		Span: uuidv5.Duration{Seconds: uuidSpanSeconds},
	})
}

func (a uuidArmV5) whole(ctx context.Context, id int64) (uuidAccount, error) {
	got, err := a.q.AccountWhole(ctx, id)
	if err != nil {
		return uuidAccount{}, err
	}
	return uuidAccount{id: got.Id, ref: got.Ref, prior: got.Prior, trail: got.Trail, chain: got.Chain, either: got.Either}, nil
}

func (a uuidArmV5) columns(ctx context.Context) ([]uuidAccount, error) {
	rows, err := a.q.AccountColumns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]uuidAccount, 0, len(rows))
	for _, r := range rows {
		out = append(out, uuidAccount{ref: r.Ref, prior: r.Prior, trail: r.Trail, chain: r.Chain, either: r.Either})
	}
	return out, nil
}

func (a uuidArmV5) raw(ctx context.Context, t *testing.T, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	session := a.driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the witness ran in: %v", err)
		}
	}()
	result, err := session.Run(ctx, cypher, params)
	require.NoError(t, err, "run the raw statement: %s", cypher)
	records, err := result.Collect(ctx)
	require.NoError(t, err, "collect the raw statement's records: %s", cypher)
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, record.AsMap())
	}
	return out
}

// uuidArmV6 drives the neo4j-go-v6 golden. Its own packstream, so its own arm.
type uuidArmV6 struct {
	driver neo4jv6.Driver
	q      *uuidv6.Queries
}

func openUUIDArmV6(ctx context.Context, t *testing.T, boltURI string) uuidArm {
	t.Helper()
	driver, err := neo4jv6.NewDriver(boltURI, neo4jv6.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v6 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	a := uuidArmV6{driver: driver, q: uuidv6.New(driver)}
	return uuidArm{
		open: a.open, whole: a.whole, columns: a.columns, ref: a.q.AccountRef,
		byRef: a.q.AccountByRef, byPrior: a.q.AccountByPrior, byTrail: a.q.AccountByTrail, byChain: a.q.AccountByChain,
		raw: a.raw,
	}
}

func (a uuidArmV6) open(ctx context.Context, acct uuidAccount) error {
	return a.q.OpenAccount(ctx, uuidv6.OpenAccountParams{
		Id: acct.id, Ref: acct.ref, Prior: acct.prior, Trail: acct.trail, Chain: acct.chain, Either: acct.either,
		Span: uuidv6.Duration{Seconds: uuidSpanSeconds},
	})
}

func (a uuidArmV6) whole(ctx context.Context, id int64) (uuidAccount, error) {
	got, err := a.q.AccountWhole(ctx, id)
	if err != nil {
		return uuidAccount{}, err
	}
	return uuidAccount{id: got.Id, ref: got.Ref, prior: got.Prior, trail: got.Trail, chain: got.Chain, either: got.Either}, nil
}

func (a uuidArmV6) columns(ctx context.Context) ([]uuidAccount, error) {
	rows, err := a.q.AccountColumns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]uuidAccount, 0, len(rows))
	for _, r := range rows {
		out = append(out, uuidAccount{ref: r.Ref, prior: r.Prior, trail: r.Trail, chain: r.Chain, either: r.Either})
	}
	return out, nil
}

func (a uuidArmV6) raw(ctx context.Context, t *testing.T, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	session := a.driver.NewSession(ctx, neo4jv6.SessionConfig{AccessMode: neo4jv6.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the witness ran in: %v", err)
		}
	}()
	result, err := session.Run(ctx, cypher, params)
	require.NoError(t, err, "run the raw statement: %s", cypher)
	records, err := result.Collect(ctx)
	require.NoError(t, err, "collect the raw statement's records: %s", cypher)
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, record.AsMap())
	}
	return out
}
