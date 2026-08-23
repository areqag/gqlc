//go:build codegen_live

// The live witness for the offset sidecar.
//
// bd gqlc-vs9i: the write battery's instant scenario (timestampRoundTrip in
// live_test.go) binds the instant ALONE — gqlc derives no sidecar to bind, by
// design — so every live run of it exercises agtypeZone on the one branch
// where the key is absent. The two branches PR #848 added, the in-range zone
// and the out-of-range refusal, were pinned only by the corpus harness, which
// calls the emitted helpers against hand-built props maps rather than against
// agtype coming back from Apache AGE.
//
// A sidecar is a property like any other, written by whatever query text names
// it. So this file seeds one through the driver and reads it back through the
// generated OneEvent, which is the emitted call site.
//
// It is AGE-only and therefore not a readScenarios row: the sidecar is this
// backend's encoding of a zone, and a Neo4j arm running the same body would be
// asserting a zone on a driver-native temporal that never consulted a sidecar.
//
// NOT YET ENROLLED, and therefore not yet a gate. test-codegen-live-age selects
// its tests by an explicit -run allowlist that does not name TestAGEOffsetSidecar,
// so as of this commit the file compiles, ships, and runs nowhere. Measured, not
// assumed: under that recipe's flags `-v` prints four `=== RUN` lines and none of
// them is this test, while `-list '.*'` under the same tag lists it. bd gqlc-zm6k
// carries the one-line recipe change; gqlc-vs9i stays open until it lands.

package fixtures_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	tsage "github.com/areqag/gqlc/test/data/codegen/valid/timestamp_property_roundtrip/golden/apache-age-pgx-v5"
)

// ageZoneGraph is the graph this file's events live in. One graph, one
// container, one EnsureGraph: the rows differ only in the properties they
// seed, and each reads back the event it seeded by id.
const ageZoneGraph = "gqlc_zone_sidecar"

// ageZoneInstant is the instant every row stores, in UTC. Chosen away from a
// day boundary so a wrong zone cannot coincide with the right wall clock.
var ageZoneInstant = time.Date(2024, time.March, 5, 13, 47, 11, 0, time.UTC)

// ageZoneHarness boots the arm this file drives and returns the generated
// handle plus a seeder that writes through the driver rather than through
// generated code — the sidecar has to arrive from outside the emission,
// because the emission never binds one.
func ageZoneHarness(t *testing.T) (context.Context, *tsage.Queries, func(cypher string)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	pool := openAGEPool(ctx, t, ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase), ageSessionInit)

	q := tsage.New(pool, ageZoneGraph)
	require.NoError(t, q.EnsureGraph(ctx), "ensure graph %s", ageZoneGraph)
	t.Cleanup(func() { require.NoError(t, q.DropGraph(ctx), "drop graph %s", ageZoneGraph) })

	return ctx, q, func(cypher string) {
		t.Helper()
		seedAGEZoneGraph(ctx, t, pool, cypher)
	}
}

// seedAGEZoneGraph runs one CREATE through cypher() directly. The graph name
// is composed into the statement because cypher() resolves that argument at
// parse analysis and takes a constant there; it is this file's own constant.
func seedAGEZoneGraph(ctx context.Context, t *testing.T, pool *pgxpool.Pool, cypher string) {
	t.Helper()
	stmt := "SELECT * FROM ag_catalog.cypher('" + ageZoneGraph + "', $seed$" + cypher + "$seed$) AS (v ag_catalog.agtype)"
	_, err := pool.Exec(ctx, stmt)
	require.NoError(t, err, "seed: %s", cypher)
}

// TestAGEEntityDecodeReadsTheOffsetSidecar is bd gqlc-vs9i's missing witness:
// the emitted agtypeZone, reached through the emitted OneEvent, against agtype
// that came back from Apache AGE.
//
// The instant is the same in every row. What differs is the sidecar beside it,
// and what the row asserts is the ZONE the caller reads — never the wall clock
// alone, which an implementation that dropped the sidecar entirely would also
// satisfy, and which the absent-sidecar row already covers.
//
// The bound agtypeZone enforces is a day either way, EXCLUSIVE, so the rows
// come in pairs around it: 86399 is admitted and 86400 is refused, and the
// same on the negative side. A guard widened or narrowed by one second
// reddens exactly one row here.
func testAGEEntityDecodeReadsTheOffsetSidecar(t *testing.T, ctx context.Context, q *tsage.Queries, seed func(string)) { //nolint:thelper // a scenario body owns its failure frame
	micros := ageZoneInstant.UnixMicro()

	for _, tc := range []struct {
		name string
		id   int64
		// props is what goes inside the CREATE beside id, so a row can
		// omit the sidecar entirely rather than write a sentinel for it.
		props string
		// wantOffset is the zone offset in seconds the decoded instant
		// must carry, when wantErr is empty.
		wantOffset int
		// wantErr is the substring the decode error must contain. A row
		// naming one asserts the refusal, not merely that something failed.
		wantErr string
	}{
		{
			// The branch the write battery already reaches, kept here so
			// the three branches are read off one table rather than two
			// files: no sidecar, and the count is complete without one.
			name:       "no sidecar leaves the instant in UTC",
			id:         1,
			props:      fmt.Sprintf("occurredAt: %d", micros),
			wantOffset: 0,
		},
		{
			name:       "a positive sidecar puts the instant back in its zone",
			id:         2,
			props:      fmt.Sprintf("occurredAt: %d, occurredAtOffset: 7200", micros),
			wantOffset: 7200,
		},
		{
			name:       "a negative sidecar is read with its sign",
			id:         3,
			props:      fmt.Sprintf("occurredAt: %d, occurredAtOffset: -18000", micros),
			wantOffset: -18000,
		},
		{
			name:       "a zero sidecar is a written UTC, not an absent one",
			id:         4,
			props:      fmt.Sprintf("occurredAt: %d, occurredAtOffset: 0", micros),
			wantOffset: 0,
		},
		{
			name:       "one second inside the bound is admitted",
			id:         5,
			props:      fmt.Sprintf("occurredAt: %d, occurredAtOffset: 86399", micros),
			wantOffset: 86399,
		},
		{
			name:       "one second inside the bound is admitted going the other way",
			id:         6,
			props:      fmt.Sprintf("occurredAt: %d, occurredAtOffset: -86399", micros),
			wantOffset: -86399,
		},
		{
			// The refusal names the PROPERTY, which is the only thing that
			// tells an author which of a vertex's instants is at fault.
			name:    "a full day ahead is refused, naming the property",
			id:      7,
			props:   fmt.Sprintf("occurredAt: %d, occurredAtOffset: 86400", micros),
			wantErr: `offset "occurredAtOffset" is 86400 seconds, which is not within a day of UTC`,
		},
		{
			name:    "a full day behind is refused too",
			id:      8,
			props:   fmt.Sprintf("occurredAt: %d, occurredAtOffset: -86400", micros),
			wantErr: `offset "occurredAtOffset" is -86400 seconds, which is not within a day of UTC`,
		},
		{
			// Not an integer at all: agtypeZone reads the sidecar through
			// agtypeInt64, so a string there is refused before the bound is
			// consulted, and the error still names the property.
			name:    "a sidecar that is not an integer is refused",
			id:      9,
			props:   fmt.Sprintf("occurredAt: %d, occurredAtOffset: 'noon'", micros),
			wantErr: `offset "occurredAtOffset"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seed(fmt.Sprintf("CREATE (e:Event {id: %d, %s})", tc.id, tc.props))

			got, err := q.OneEvent(ctx, tc.id)
			if tc.wantErr != "" {
				require.Error(t, err, "the sidecar must not be accepted")
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)

			require.True(t, got.OccurredAt.Equal(ageZoneInstant),
				"the instant moved: want %s, got %s", ageZoneInstant, got.OccurredAt)
			_, offset := got.OccurredAt.Zone()
			require.Equal(t, tc.wantOffset, offset,
				"the decoded instant carries the wrong zone: %s", got.OccurredAt)
		})
	}
}

// TestAGENullableInstantReadsItsOwnSidecar covers the second emitted
// agtypeZone call site. A nullable instant is decoded through a different arm
// — agtypeNullableProperty, then a zone applied to the dereferenced value —
// and that arm reads a sidecar named after ITS property. A generator that
// emitted one sidecar name for every instant on a vertex would pass the test
// above and fail here.
func testAGENullableInstantReadsItsOwnSidecar(t *testing.T, ctx context.Context, q *tsage.Queries, seed func(string)) { //nolint:thelper // a scenario body owns its failure frame
	micros := ageZoneInstant.UnixMicro()
	seed(fmt.Sprintf(
		"CREATE (e:Event {id: 100, occurredAt: %d, occurredAtOffset: 3600, seenAt: %d, seenAtOffset: -12600})",
		micros, micros))

	got, err := q.OneEvent(ctx, 100)
	require.NoError(t, err)

	_, occurredOffset := got.OccurredAt.Zone()
	require.Equal(t, 3600, occurredOffset, "occurredAt took the wrong sidecar")

	require.NotNil(t, got.SeenAt, "the nullable instant was seeded and must decode")
	require.True(t, got.SeenAt.Equal(ageZoneInstant),
		"the nullable instant moved: want %s, got %s", ageZoneInstant, *got.SeenAt)
	_, seenOffset := got.SeenAt.Zone()
	require.Equal(t, -12600, seenOffset, "seenAt took the wrong sidecar")
}

// TestAGENullableInstantWithoutItsInstantIgnoresTheSidecar pins the guard the
// nullable arm puts around the zone: agtypeZone is applied only when the
// instant itself decoded to something. A vertex carrying the sidecar and no
// instant is a graph gqlc did not write and has to read without failing —
// there is nothing to put in a zone, so the absent instant survives and the
// stray sidecar is not consulted at all, out-of-range or not.
func testAGENullableInstantWithoutItsInstantIgnoresTheSidecar(t *testing.T, ctx context.Context, q *tsage.Queries, seed func(string)) { //nolint:thelper // a scenario body owns its failure frame
	seed(fmt.Sprintf(
		"CREATE (e:Event {id: 200, occurredAt: %d, seenAtOffset: 99999})",
		ageZoneInstant.UnixMicro()))

	got, err := q.OneEvent(ctx, 200)
	require.NoError(t, err, "a sidecar with no instant beside it must not fail the read")
	require.Nil(t, got.SeenAt, "the absent nullable instant must stay absent")
}

// TestAGEOffsetSidecar is the entry point the three bodies above share, so the
// witness costs one container rather than three. Each body seeds ids of its
// own in the one graph, so they do not observe each other.
func TestAGEOffsetSidecar(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()
	ctx, q, seed := ageZoneHarness(t)

	for _, sc := range []struct {
		name string
		run  func(*testing.T, context.Context, *tsage.Queries, func(string))
	}{
		{name: "the sidecar beside a non-nullable instant", run: testAGEEntityDecodeReadsTheOffsetSidecar},
		{name: "a nullable instant reads its own sidecar", run: testAGENullableInstantReadsItsOwnSidecar},
		{name: "a sidecar with no instant beside it", run: testAGENullableInstantWithoutItsInstantIgnoresTheSidecar},
	} {
		t.Run(sc.name, func(t *testing.T) {
			sc.run(t, ctx, q, seed)
		})
	}
}
