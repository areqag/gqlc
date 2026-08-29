//go:build codegen_live

// The live witness for TIME WITH TIME ZONE on Apache AGE.
//
// It is AGE-only, and not a zonedTimeScenarios row, for a reason that is
// this backend's encoding and not an omission. The shared scenario
// (zonedTimeRoundTrip in live_test.go) asserts that a gqlc round trip
// returns the writer's OFFSET as well as the clock reading. On the neo4j
// arms it does, because the driver carries a zoned temporal. Here it
// cannot: ADR 0005 keeps the author's `$name` placeholders verbatim and
// binds one value per placeholder, so a CREATE naming `startsAt: $startsAt`
// has nowhere for gqlc to put a second property, and the emission
// therefore writes no <field>Offset sidecar — for TIME or for TIMESTAMP.
// Grep any apache-age golden: "Offset" occurs on the decode path only.
//
// So the sidecar is a read affordance. It is there so a zone some other
// writer put in the graph survives a read, which is what bd gqlc-vs9i
// established for TIMESTAMP in live_age_zone_test.go, and what the second
// body below does for TIME by seeding through the driver.
//
// What a gqlc round trip DOES preserve is the instant, and that is the
// claim the normalisation exists to make good: the stored count is the
// clock reading minus its offset, so storage order is instant order and
// an author's ORDER BY is answered by agtype's own integer ordering with
// nothing for gqlc to rewrite. The third body drives that through
// generated code with a pair whose local clock order is the reverse of
// their instant order.
//
// Enrolled by name in test-codegen-live-age's -run alternation, because
// that recipe selects by an explicit allowlist and a live test it does
// not name runs in no job at all. TestEveryLiveTestIsRunByARecipeThatNamesIt
// (internal/liverecipes) is what refuses the unenrolled state.

package fixtures_test

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	zonedage "github.com/areqag/gqlc/test/data/codegen/valid/zoned_time_roundtrip/golden/apache-age-pgx-v5"
)

// ageZonedTimeGraphs numbers the graphs this file's bodies run in. Each
// takes one of its own because SlotsFrom matches every Slot in the graph,
// so bodies sharing one would read each other's rows and the ordering
// assertion would be over a population it did not write.
var ageZonedTimeGraphs atomic.Int64

// ageZonedTimeScenario mints a graph on the one container and returns the
// generated handle bound to it, plus a seeder that writes through the
// driver rather than through generated code — the sidecar has to arrive
// from outside the emission, because the emission never binds one.
func ageZonedTimeScenario(ctx context.Context, t *testing.T, dsn string) (*zonedage.Queries, func(cypher string)) {
	t.Helper()
	graph := fmt.Sprintf("gqlc_zoned_time_%d", ageZonedTimeGraphs.Add(1))
	pool := openAGEPool(ctx, t, dsn, ageSessionInit)

	q := zonedage.New(pool, graph)
	require.NoError(t, q.EnsureGraph(ctx), "ensure graph %s", graph)
	t.Cleanup(func() { require.NoError(t, q.DropGraph(ctx), "drop graph %s", graph) })

	return q, func(cypher string) {
		t.Helper()
		// cypher() resolves its graph argument at parse analysis and takes
		// a constant there, so the name is composed into the statement. It
		// is this file's own, formatted from a counter.
		stmt := "SELECT * FROM ag_catalog.cypher('" + graph + "', $seed$" + cypher + "$seed$) AS (v ag_catalog.agtype)"
		_, err := pool.Exec(ctx, stmt)
		require.NoError(t, err, "seed: %s", cypher)
	}
}

// testAGEZonedTimeRoundTripsTheInstant writes readings at three offsets
// through the emitted AddSlot and reads each back both ways the emission
// offers — a projected column and a whole vertex.
//
// What must come back is the UTC-NORMALISED reading with OffsetSeconds
// zero, and saying so is the point rather than a concession. A round trip
// that returned the reading as written would be satisfied by an encoding
// that stored the local clock and never normalised at all, which is the
// defect the ordering body below exists to catch; asserting the UTC
// reading here means this body fails first, at the value, without waiting
// for a comparison to answer wrongly.
//
// The three rows wrap in both directions and in neither: 09:30+05:45 is
// 03:45Z, 08:00-08:00 crosses midnight forward to 16:00Z, and 00:30+02:00
// crosses it backward to 22:30Z the day before.
func testAGEZonedTimeRoundTripsTheInstant(ctx context.Context, t *testing.T, q *zonedage.Queries, _ func(string)) { //nolint:thelper // a scenario body owns its failure frame
	_, err := q.SlotStart(ctx, 1)
	require.ErrorIs(t, err, zonedage.ErrNoRows, "an empty graph must return ErrNoRows")

	for _, tc := range []struct {
		name    string
		id      int64
		written zonedage.Time
		wantUTC zonedage.Time
	}{
		{
			name:    "an offset east of UTC subtracts within the day",
			id:      1,
			written: zonedage.Time{Hour: 9, Minute: 30, OffsetSeconds: 5*3600 + 45*60},
			wantUTC: zonedage.Time{Hour: 3, Minute: 45},
		},
		{
			name:    "an offset west of UTC crosses midnight forward",
			id:      2,
			written: zonedage.Time{Hour: 8, Nanosecond: 250000000, OffsetSeconds: -8 * 3600},
			wantUTC: zonedage.Time{Hour: 16, Nanosecond: 250000000},
		},
		{
			name:    "an offset east of a reading near midnight crosses it backward",
			id:      3,
			written: zonedage.Time{Minute: 30, OffsetSeconds: 2 * 3600},
			wantUTC: zonedage.Time{Hour: 22, Minute: 30},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, q.AddSlot(ctx, zonedage.AddSlotParams{Id: tc.id, StartsAt: tc.written}),
				"write slot %d", tc.id)

			column, err := q.SlotStart(ctx, tc.id)
			require.NoError(t, err, "read slot %d as a column", tc.id)
			require.Equal(t, tc.wantUTC, column,
				"a projected TIME has no sidecar to read, so it must arrive UTC-normalised")

			entity, err := q.OneSlot(ctx, tc.id)
			require.NoError(t, err, "read slot %d as a vertex", tc.id)
			require.Equal(t, zonedage.Slot{Id: tc.id, StartsAt: tc.wantUTC}, entity,
				"a vertex gqlc wrote carries no sidecar either, so its TIME must read the same as the column")
		})
	}
}

// testAGEZonedTimeReadsTheOffsetSidecar is the read affordance, seeded
// through the driver because the emission writes no sidecar.
//
// Every row stores the SAME count, 22:30Z, and differs only in the offset
// beside it. What each asserts is therefore the whole reading, which an
// implementation that read the sidecar and did not shift the clock by it
// would fail — an assertion on OffsetSeconds alone would not.
//
// The bound is a day either way, EXCLUSIVE, so the rows come in pairs
// around it: 86399 is admitted and 86400 refused, and the same negative.
// A guard widened or narrowed by one second reddens exactly one row.
func testAGEZonedTimeReadsTheOffsetSidecar(ctx context.Context, t *testing.T, q *zonedage.Queries, seed func(string)) { //nolint:thelper // a scenario body owns its failure frame
	const stored = 81000000000 // 22:30 UTC

	for _, tc := range []struct {
		name string
		id   int64
		// props is what goes inside the CREATE beside id, so a row can
		// omit the sidecar entirely rather than write a sentinel for it.
		props string
		// want is the whole reading the decode must build, when wantErr
		// is empty.
		want zonedage.Time
		// wantErr is the substring the decode error must contain. A row
		// naming one asserts the refusal, not merely that something failed.
		wantErr string
	}{
		{
			name:  "no sidecar leaves the reading in UTC",
			id:    1,
			props: fmt.Sprintf("startsAt: %d", stored),
			want:  zonedage.Time{Hour: 22, Minute: 30},
		},
		{
			name:  "a positive sidecar carries the reading over midnight",
			id:    2,
			props: fmt.Sprintf("startsAt: %d, startsAtOffset: 7200", stored),
			want:  zonedage.Time{Minute: 30, OffsetSeconds: 7200},
		},
		{
			name:  "a negative sidecar is read with its sign",
			id:    3,
			props: fmt.Sprintf("startsAt: %d, startsAtOffset: -18000", stored),
			want:  zonedage.Time{Hour: 17, Minute: 30, OffsetSeconds: -18000},
		},
		{
			name:  "a zero sidecar is a written UTC, not an absent one",
			id:    4,
			props: fmt.Sprintf("startsAt: %d, startsAtOffset: 0", stored),
			want:  zonedage.Time{Hour: 22, Minute: 30},
		},
		{
			// An offset that is not a whole number of minutes, so a helper
			// reading the sidecar in minutes cannot land on this reading.
			name:  "an offset counted in seconds",
			id:    5,
			props: fmt.Sprintf("startsAt: %d, startsAtOffset: 12600", stored),
			want:  zonedage.Time{Hour: 2, OffsetSeconds: 12600},
		},
		{
			name:  "one second inside the bound is admitted",
			id:    6,
			props: fmt.Sprintf("startsAt: %d, startsAtOffset: 86399", stored),
			want:  zonedage.Time{Hour: 22, Minute: 29, Second: 59, OffsetSeconds: 86399},
		},
		{
			name:  "one second inside the bound is admitted going the other way",
			id:    7,
			props: fmt.Sprintf("startsAt: %d, startsAtOffset: -86399", stored),
			want:  zonedage.Time{Hour: 22, Minute: 30, Second: 1, OffsetSeconds: -86399},
		},
		{
			// The refusal names the PROPERTY, which is the only thing that
			// tells an author which of a vertex's readings is at fault.
			name:    "a full day ahead is refused, naming the property",
			id:      8,
			props:   fmt.Sprintf("startsAt: %d, startsAtOffset: 86400", stored),
			wantErr: `offset "startsAtOffset" is 86400 seconds, which is not within a day of UTC`,
		},
		{
			name:    "a full day behind is refused too",
			id:      9,
			props:   fmt.Sprintf("startsAt: %d, startsAtOffset: -86400", stored),
			wantErr: `offset "startsAtOffset" is -86400 seconds, which is not within a day of UTC`,
		},
		{
			name:    "a sidecar that is not an integer is refused",
			id:      10,
			props:   fmt.Sprintf("startsAt: %d, startsAtOffset: 'noon'", stored),
			wantErr: `offset "startsAtOffset"`,
		},
		{
			// The count is bounded too, and by a graph gqlc did not write:
			// 86400000000 is midnight tomorrow, which is a reading of the
			// day after the one the property holds.
			name:    "a count outside the day is refused",
			id:      11,
			props:   "startsAt: 86400000000",
			wantErr: "outside the [0, 86400000000) interval a clock reading occupies",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seed(fmt.Sprintf("CREATE (s:Slot {id: %d, %s})", tc.id, tc.props))

			got, err := q.OneSlot(ctx, tc.id)
			if tc.wantErr != "" {
				require.Error(t, err, "the property must not be accepted")
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got.StartsAt,
				"the sidecar did not put the reading back in its own zone")
		})
	}
}

// testAGEStoredTimesOrderByInstant is the claim the normalisation exists
// for, and the only one here that the DATABASE answers rather than the
// emitted helpers: the ORDER BY and the range predicate are the author's
// own query text, run as written, so what sorts the rows is agtype's
// integer ordering over the stored counts and nothing gqlc does.
//
// 23:00+02:00 is 21:00Z and 22:30+00:00 is 22:30Z, so the reading whose
// clock reads LATER is the earlier instant and must come back first.
// Stored unnormalised the two would sort the other way round, and no
// assertion on a single decoded value can see it.
//
// The cutoff is expressed in a third offset for the same reason: 23:00
// +01:00 is 22:00Z, which excludes the first slot, while its bare clock
// reading of 23:00 is not less than the cutoff's bare 23:00 — so a
// comparison that had lost the offsets keeps the row the instants drop.
func testAGEStoredTimesOrderByInstant(ctx context.Context, t *testing.T, q *zonedage.Queries, _ func(string)) { //nolint:thelper // a scenario body owns its failure frame
	laterClock := zonedage.Time{Hour: 23, OffsetSeconds: 2 * 3600}
	earlierClock := zonedage.Time{Hour: 22, Minute: 30}

	require.NoError(t, q.AddSlot(ctx, zonedage.AddSlotParams{Id: 1, StartsAt: laterClock}))
	require.NoError(t, q.AddSlot(ctx, zonedage.AddSlotParams{Id: 2, StartsAt: earlierClock}))

	all, err := q.SlotsFrom(ctx, zonedage.Time{})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, all,
		"ORDER BY over a stored zoned time must answer by the instant, not by the clock reading")

	after, err := q.SlotsFrom(ctx, zonedage.Time{Hour: 23, OffsetSeconds: 3600})
	require.NoError(t, err)
	require.Equal(t, []int64{2}, after,
		"a range predicate over a stored zoned time must compare instants")
}

// TestAGEZonedTime is the entry point the three bodies above share, so the
// witness costs one container rather than three. Each body runs in a graph
// of its own, because SlotsFrom matches every Slot in the graph it is
// pointed at.
func TestAGEZonedTime(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	dsn := ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase)

	for _, sc := range []struct {
		name string
		run  func(context.Context, *testing.T, *zonedage.Queries, func(string))
	}{
		{name: "a gqlc round trip preserves the instant", run: testAGEZonedTimeRoundTripsTheInstant},
		{name: "the sidecar beside a stored reading", run: testAGEZonedTimeReadsTheOffsetSidecar},
		{name: "stored readings sort by instant", run: testAGEStoredTimesOrderByInstant},
	} {
		t.Run(sc.name, func(t *testing.T) {
			q, seed := ageZonedTimeScenario(ctx, t, dsn)
			sc.run(ctx, t, q, seed)
		})
	}
}
