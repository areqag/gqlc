//go:build codegen_live

// The neo4j half of the uint64-above-MaxInt64 claim (bd gqlc-lr0v6). The AGE
// half is uint64_param_test.go, and the two are different KINDS of test on
// purpose.
//
// On AGE the refusal is gqlc's own: the emitted agtypeUnsigned raises it
// between cypherStmt and q.db.Query, so a nil DBTX is enough to execute it and
// that half needs no server. Here the refusal belongs to the DRIVER —
// outgoing.packX and packV route reflect.Uint64 to packer.Uint64, whose
// checkOverflowInt sets an OverflowError above math.MaxInt64, and
// bolt5.onPackErr turns that into a fatal connection error rather than a send
// (read at v5.28.4 packstream/packer.go:93-96,260-264 and v6.2.0
// packstream/packer.go:289). gqlc's whole contribution is to BIND THE VALUE
// BARE, because an emitted int64(arg.Hits) reaches that check as an
// already-wrapped negative and disarms it (bd gqlc-tzjqu).
//
// So what is under test here is a behaviour of the driver's send path, and
// nothing short of a real send exercises it. That is why this half was split
// out of gqlc-l65y9 and filed rather than shipped with the AGE half.
//
// TWO ASSERTIONS, NOT ONE. The defect this refusal exists to catch is a STORE
// holding a value the caller never wrote, so an error alone does not say the
// defect is absent — an error with a row behind it IS the defect. Every
// refusal row therefore also reads the graph back and requires it empty.
// Under a reinstated widen the row does not merely stop erroring: it commits
// a :Counter whose hits is -9223372036854775808, and the read-back is what
// prints that number.
//
// THE READ-BACK IS RAW, the write is generated. The subject is gqlc's
// emission; an observation made through that same emission could be wrong in
// agreement with it, and would report an empty graph for a decode that merely
// failed. Every witness below goes through a plain driver session.
//
// AND THE REASON, NOT JUST THE ERROR. require.Error alone is satisfied by a
// typo in the query text, a dead container, or a refusal of some other kind,
// each of which would leave this row green while the overflow guard was gone.
// The rows pin packstream's own words.
//
// BOTH DRIVER MAJORS, ONE CONTAINER. Unlike
// TestNeo4jRefusesANestedListStoredProperty — whose subject is a rule the
// SERVER enforces, so a second driver would measure one fact twice — the
// guard here lives in each driver's own packstream, which v5 and v6 vendor
// separately. They are two facts and they get two arms, at the price of a
// second driver against the one container rather than a second container.
//
// NOT PARALLEL, deliberately. This probe boots a container of its own, and
// the neo4j half already boots four concurrently; go test runs the
// non-parallel tests before the parallel ones, so declining t.Parallel here
// trades one container boot of wall time for a peak unchanged from today's.
// See the justfile's note at test-codegen-live-neo4j for the arithmetic.
//
// The name is spelled into that recipe's -run list and into
// internal/liverecipes.LiveArms; go test's -run is unanchored and the recipe
// is a name list for that reason, and TestEveryLiveTestIsRunByARecipeThatNamesIt
// refuses a live test no half names.

package fixtures_test

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"

	uint64v5 "github.com/areqag/gqlc/test/data/codegen/valid/uint64_parameter/golden/neo4j-go-v5"
	uint64v6 "github.com/areqag/gqlc/test/data/codegen/valid/uint64_parameter/golden/neo4j-go-v6"
)

// packerOverflowRefusal is packstream's own wording for the check under test.
// Identical in v5.28.4 and v6.2.0, and matched as a substring so the sentence
// around it may be reworded — but a row that stops refusing, or that refuses
// for some other reason, cannot pass it.
const packerOverflowRefusal = "Trying to pack uint64 that doesn't fit into int64"

// storedCountersCypher reads back what the store actually holds. Only the
// `hits` property, because that is the one a widen wraps into a value with a
// signed representation: the other three shapes carry the same number and the
// row that moved one of them out of range moved hits nowhere.
const storedCountersCypher = "MATCH (c:Counter) RETURN c.hits AS hits ORDER BY c.id"

// uint64Counter is one set of parameters for the generated RecordCounter,
// spelled in primitives because the two majors declare their own params
// struct and the arms below are what convert.
type uint64Counter struct {
	id     int64
	hits   uint64
	misses *uint64
	runs   []uint64
	spans  *[]uint64
}

// uint64WriteArm is one driver major under test: the generated write, and the
// raw observations that judge it.
//
// Function fields rather than an interface. The two majors share no type —
// each vendors its own driver, session and Record — so an arm is a set of
// closures over whichever major's package its constructor named, and a named
// method set would be one nothing else implements or asserts against.
type uint64WriteArm struct {
	// record binds c through the generated RecordCounter — the emission
	// whose bare uint64 is the whole of gqlc's part in this refusal.
	record func(ctx context.Context, c uint64Counter) error
	// wipe empties the graph so each row reads its own answer.
	wipe func(ctx context.Context, t *testing.T)
	// storedHits is every :Counter the store holds, as the raw signed
	// integer neo4j keeps. Empty means the send never landed. A row whose
	// refusal was disarmed prints the wrapped negative here.
	storedHits func(ctx context.Context, t *testing.T) []int64
}

// TestNeo4jRefusesAUint64ParameterAboveMaxInt64 is the live half of
// bd gqlc-lr0v6.
func TestNeo4jRefusesAUint64ParameterAboveMaxInt64(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	boltURI := startNeo4jContainer(ctx, t)

	arms := []struct {
		name string
		open func(ctx context.Context, t *testing.T, boltURI string) uint64WriteArm
	}{
		{name: "neo4j-go-v5", open: openUint64WriteArmV5},
		{name: "neo4j-go-v6", open: openUint64WriteArmV6},
	}

	for _, a := range arms {
		t.Run(a.name, func(t *testing.T) {
			runUint64OverflowRows(ctx, t, a.open(ctx, t, boltURI))
		})
	}
}

// runUint64OverflowRows is the battery both majors run.
//
// The four refusal rows are the four bind shapes the fixture declares, and on
// this backend they are four paths through the DRIVER rather than four
// emissions: gqlc binds every one of them bare, so packX's reflect.Uint64 arm
// answers for hits, its reflect.Ptr arm indirects to the same entry point for
// misses, its slice default walks runs through packV, and spans reaches it
// through both. A driver that stopped walking one of those would store a
// wrapped negative under exactly one parameter, which is what a row per shape
// is for.
//
// Rows share one graph and each opens by wiping it, so they must not be
// parallel.
func runUint64OverflowRows(ctx context.Context, t *testing.T, arm uint64WriteArm) {
	t.Helper()

	inRange := uint64Counter{
		id:     1,
		hits:   1,
		misses: ptr[uint64](2),
		runs:   []uint64{3, 4},
		spans:  ptr([]uint64{5, 6}),
	}

	refusals := []struct {
		name string
		// counter is inRange with exactly one field moved out of range, so
		// a row that fails names the shape that let the value through.
		counter uint64Counter
	}{
		{
			name:    "$hits, bound bare",
			counter: withCounter(inRange, func(c *uint64Counter) { c.hits = aboveMaxInt64 }),
		},
		{
			// The pointed-to value, not the pointer: packX's reflect.Ptr
			// arm indirects and re-enters on the pointee, so this row is
			// what says the entry point it re-enters is the guarded one.
			name:    "$misses, bound through the nullable pointer",
			counter: withCounter(inRange, func(c *uint64Counter) { c.misses = ptr(aboveMaxInt64) }),
		},
		{
			// Element 1 and not element 0, so the guard is reached off the
			// failing element rather than off the head of the list.
			name:    "$runs, bound through the list",
			counter: withCounter(inRange, func(c *uint64Counter) { c.runs = []uint64{3, aboveMaxInt64} }),
		},
		{
			name:    "$spans, bound through the pointer to list",
			counter: withCounter(inRange, func(c *uint64Counter) { c.spans = ptr([]uint64{5, 6, aboveMaxInt64}) }),
		},
	}

	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			arm.wipe(ctx, t)

			err := arm.record(ctx, tc.counter)
			require.Error(t, err,
				"the driver accepted a uint64 above MaxInt64 and the write landed; gqlc binds this parameter bare precisely so packer.Uint64 can refuse it")
			require.ErrorContains(t, err, packerOverflowRefusal,
				"the send failed for some other reason, so nothing here witnesses the overflow guard: %v", err)

			require.Empty(t, arm.storedHits(ctx, t),
				"the send failed AND the store holds a :Counter, which is the defect this refusal exists to prevent rather than the absence of it; "+
					"a hits of -9223372036854775808 here is an emitted int64(arg.Hits) wrapping the value before the driver could see it (bd gqlc-tzjqu)")
		})
	}

	// The boundary. Every row above is satisfied by a driver that refused
	// EVERY uint64, and by a container that was simply dead. This one says
	// the refusal is scoped to values the signed carrier cannot hold:
	// MaxInt64 is the largest it can, and it must cross, land, and read back
	// as itself.
	t.Run("MaxInt64 in every shape crosses and is stored", func(t *testing.T) {
		arm.wipe(ctx, t)

		err := arm.record(ctx, uint64Counter{
			id:     1,
			hits:   math.MaxInt64,
			misses: ptr[uint64](math.MaxInt64),
			runs:   []uint64{math.MaxInt64},
			spans:  ptr([]uint64{math.MaxInt64}),
		})
		require.NoError(t, err,
			"the largest value the Bolt integer carries must still be sent; a refusal here is the guard's boundary moved, not the guard working")

		require.Equal(t, []int64{math.MaxInt64}, arm.storedHits(ctx, t),
			"accepting the write proves less than reading the value back proves: the store must hold exactly what was bound")
	})
}

// withCounter copies c and applies edit, so each row states only the field it
// moves out of range. The pointer and slice fields are replaced rather than
// written through, so the shallow copy shares nothing with the original.
func withCounter(c uint64Counter, edit func(*uint64Counter)) uint64Counter {
	edit(&c)
	return c
}

// uint64WriteArmV5 drives the neo4j-go-v5 golden.
type uint64WriteArmV5 struct {
	driver neo4jv5.DriverWithContext
	q      *uint64v5.Queries
}

func openUint64WriteArmV5(ctx context.Context, t *testing.T, boltURI string) uint64WriteArm {
	t.Helper()
	driver, err := neo4jv5.NewDriverWithContext(boltURI, neo4jv5.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v5 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	a := uint64WriteArmV5{driver: driver, q: uint64v5.New(driver)}
	return uint64WriteArm{record: a.record, wipe: a.wipe, storedHits: a.storedHits}
}

func (a uint64WriteArmV5) record(ctx context.Context, c uint64Counter) error {
	return a.q.RecordCounter(ctx, uint64v5.RecordCounterParams{
		Id:     c.id,
		Hits:   c.hits,
		Misses: c.misses,
		Runs:   c.runs,
		Spans:  c.spans,
	})
}

func (a uint64WriteArmV5) wipe(ctx context.Context, t *testing.T) {
	t.Helper()
	_, err := a.runV5(ctx, t, wipeCypher)
	require.NoError(t, err, "wipe the graph before the row reads its own answer")
}

func (a uint64WriteArmV5) storedHits(ctx context.Context, t *testing.T) []int64 {
	t.Helper()
	records, err := a.runV5(ctx, t, storedCountersCypher)
	require.NoError(t, err, "read back what the store holds")
	out := make([]int64, 0, len(records))
	for _, record := range records {
		hits, _, err := neo4jv5.GetRecordValue[int64](record, "hits")
		require.NoError(t, err, "a stored hits must be a Bolt integer")
		out = append(out, hits)
	}
	return out
}

// runV5 runs one statement in its own auto-commit transaction.
//
// Deliberately NOT ExecuteWrite, which the generated code under test uses:
// that wraps the call in the driver's retry policy, and the observations here
// must not be retried into agreement with anything.
func (a uint64WriteArmV5) runV5(ctx context.Context, t *testing.T, cypher string) ([]*neo4jv5.Record, error) {
	t.Helper()
	session := a.driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the witness ran in: %v", err)
		}
	}()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		return nil, err
	}
	return result.Collect(ctx)
}

// uint64WriteArmV6 drives the neo4j-go-v6 golden. Its own packstream, so its
// own row.
type uint64WriteArmV6 struct {
	driver neo4jv6.Driver
	q      *uint64v6.Queries
}

func openUint64WriteArmV6(ctx context.Context, t *testing.T, boltURI string) uint64WriteArm {
	t.Helper()
	driver, err := neo4jv6.NewDriver(boltURI, neo4jv6.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v6 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	a := uint64WriteArmV6{driver: driver, q: uint64v6.New(driver)}
	return uint64WriteArm{record: a.record, wipe: a.wipe, storedHits: a.storedHits}
}

func (a uint64WriteArmV6) record(ctx context.Context, c uint64Counter) error {
	return a.q.RecordCounter(ctx, uint64v6.RecordCounterParams{
		Id:     c.id,
		Hits:   c.hits,
		Misses: c.misses,
		Runs:   c.runs,
		Spans:  c.spans,
	})
}

func (a uint64WriteArmV6) wipe(ctx context.Context, t *testing.T) {
	t.Helper()
	_, err := a.runV6(ctx, t, wipeCypher)
	require.NoError(t, err, "wipe the graph before the row reads its own answer")
}

func (a uint64WriteArmV6) storedHits(ctx context.Context, t *testing.T) []int64 {
	t.Helper()
	records, err := a.runV6(ctx, t, storedCountersCypher)
	require.NoError(t, err, "read back what the store holds")
	out := make([]int64, 0, len(records))
	for _, record := range records {
		hits, _, err := neo4jv6.GetRecordValue[int64](record, "hits")
		require.NoError(t, err, "a stored hits must be a Bolt integer")
		out = append(out, hits)
	}
	return out
}

// runV6 is runV5's v6 spelling; see the note there on why it is auto-commit.
func (a uint64WriteArmV6) runV6(ctx context.Context, t *testing.T, cypher string) ([]*neo4jv6.Record, error) {
	t.Helper()
	session := a.driver.NewSession(ctx, neo4jv6.SessionConfig{AccessMode: neo4jv6.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the witness ran in: %v", err)
		}
	}()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		return nil, err
	}
	return result.Collect(ctx)
}
