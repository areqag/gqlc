//go:build codegen_live

// The top-level-parameter half of bd gqlc-gk6q: a LIST<TIMESTAMP> whose
// ELEMENTS are nullable, carried as []*time.Time, written and read back
// through the generated code of fixture nullable_timestamp_list_element on
// both driver majors.
//
// The defect was the bind: the list was handed to the driver bare, and
// neither major packs a *time.Time inside a list. packV's pointer arm hands a
// pointer-to-struct to packStruct, whose cases name time.Time and not
// *time.Time; v5.28.4 then raises `Usage of type '*time.Time' is not
// supported`, and v6.2.0 falls through to mapping.StructAsMap and sends an
// empty map. The union-member half of the same bead is in
// live_neo4j_union_list_temporal_test.go, and the two share that file's
// container and top-level test.
//
// The NULL-element row is the one that tells the fix from the defect on v6.
// A neo4j property array cannot hold a null, so the server refuses that write
// whatever the bind did — but it refuses an empty map in different words. The
// row asserts the null's wording, so it passes only if the nil element reached
// the server as a Cypher null.

package fixtures_test

import (
	"context"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"

	ntlv5 "github.com/areqag/gqlc/test/data/codegen/valid/nullable_timestamp_list_element/golden/neo4j-go-v5"
	ntlv6 "github.com/areqag/gqlc/test/data/codegen/valid/nullable_timestamp_list_element/golden/neo4j-go-v6"
)

// nullInArrayRefusal is the server's wording for a stored array holding a
// null, measured against the pinned image. Neither defect produces it: v5
// never sends the write, and v6's empty map is refused as `Property values can
// only be of primitive types or arrays thereof. Encountered: Map{}.`
const nullInArrayRefusal = "Collections containing null values can not be stored in properties"

// timestampEntry is one :Entry of the fixture. Every TIMESTAMP-list field is
// spelled in the standard library's types on both goldens, so one struct
// serves both majors.
type timestampEntry struct {
	id     int64
	stamps []*time.Time
	maybe  *[]*time.Time
	fixed  []time.Time
}

// nullableTimestampListArm is one driver major's view of the fixture.
type nullableTimestampListArm struct {
	open     func(ctx context.Context, e timestampEntry) error
	read     func(ctx context.Context, id int64) (timestampEntry, error)
	matching func(ctx context.Context, stamps []*time.Time, fixed []time.Time) ([]int64, error)
	raw      func(ctx context.Context, t *testing.T, cypher string) []map[string]any
}

// The first stamp sits at a non-UTC offset so the read-back is known to carry
// the instant; the comparison is on the instant, because the driver hands back
// its own *time.Location.
//
// The zone is NAMED "Offset", which is both majors' packStruct test for the
// offset form; any other name is sent as a zone id. Measured against the
// pinned image: an unnamed time.FixedZone is refused as `Illegal epoch
// adjustment`, and a non-IANA name as `Illegal zone identifier`, on both
// majors and for a bare time.Time as much as for a list element (bd
// gqlc-m3ax).
var nullableTimestampListStamps = []time.Time{
	time.Date(2024, 2, 29, 23, 30, 0, 0, time.FixedZone("Offset", 2*3600)),
	time.Date(1999, 12, 31, 12, 0, 0, 500, time.UTC),
}

func runNullableTimestampListRows(ctx context.Context, t *testing.T, arm nullableTimestampListArm) {
	t.Helper()
	wipe := func(t *testing.T) {
		t.Helper()
		arm.raw(ctx, t, wipeCypher)
	}
	full := func() timestampEntry {
		stamps := make([]*time.Time, len(nullableTimestampListStamps))
		for i := range nullableTimestampListStamps {
			stamps[i] = ptr(nullableTimestampListStamps[i])
		}
		maybe := []*time.Time{ptr(nullableTimestampListStamps[1])}
		return timestampEntry{
			id:     1,
			stamps: stamps,
			maybe:  &maybe,
			fixed:  nullableTimestampListStamps,
		}
	}

	t.Run("a list of nullable TIMESTAMPs is stored as zoned datetimes", func(t *testing.T) {
		wipe(t)
		require.NoError(t, arm.open(ctx, full()),
			"the generated write of a []*time.Time parameter was refused")

		rows := arm.raw(ctx, t, "MATCH (e:Entry {id: 1}) RETURN "+
			"[x IN e.stamps | valueType(x)] AS stamps, [x IN e.maybe | valueType(x)] AS maybe, "+
			"[x IN e.fixed | valueType(x)] AS fixed")
		require.Len(t, rows, 1, "the write reported success and the store holds no such node")
		require.Equal(t, map[string]any{
			"stamps": []any{"ZONED DATETIME NOT NULL", "ZONED DATETIME NOT NULL"},
			"maybe":  []any{"ZONED DATETIME NOT NULL"},
			"fixed":  []any{"ZONED DATETIME NOT NULL", "ZONED DATETIME NOT NULL"},
		}, rows[0], "each element must be stored as the instant it was, not as a map")
	})

	t.Run("each list reads back as the instants written", func(t *testing.T) {
		wipe(t)
		want := full()
		require.NoError(t, arm.open(ctx, want))

		got, err := arm.read(ctx, 1)
		require.NoError(t, err, "the column decode refused a value it wrote")
		requireStampPtrsEqual(t, want.stamps, got.stamps, "stamps")
		require.NotNil(t, got.maybe, "maybe was written and read back as null")
		requireStampPtrsEqual(t, *want.maybe, *got.maybe, "maybe")
		require.Len(t, got.fixed, len(want.fixed))
		for i := range want.fixed {
			require.True(t, want.fixed[i].Equal(got.fixed[i]), "fixed %d: wrote %s, read %s", i, want.fixed[i], got.fixed[i])
		}
	})

	t.Run("a nil list binds null, not an empty list", func(t *testing.T) {
		wipe(t)
		e := full()
		e.maybe = nil
		require.NoError(t, arm.open(ctx, e))

		got, err := arm.read(ctx, 1)
		require.NoError(t, err)
		require.Nil(t, got.maybe, "a nil *[]*time.Time must leave the property absent")
	})

	t.Run("a nullable TIMESTAMP list parameter matches the node holding it and no other", func(t *testing.T) {
		wipe(t)
		e := full()
		require.NoError(t, arm.open(ctx, e))

		got, err := arm.matching(ctx, e.stamps, e.fixed)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, got, "the list binds as the instants it holds")

		// The control: a match on everything satisfies the row above.
		none, err := arm.matching(ctx, e.stamps[:1], e.fixed)
		require.NoError(t, err)
		require.Empty(t, none, "a stamp list no node holds must match no node")
	})

	t.Run("a nil element reaches the server as null", func(t *testing.T) {
		wipe(t)
		e := full()
		e.stamps = append(e.stamps, nil)
		err := arm.open(ctx, e)
		require.Error(t, err, "the server stored an array holding a null, which the premise says it refuses")
		require.ErrorContains(t, err, nullInArrayRefusal,
			"the nil element did not arrive as a Cypher null")
	})
}

// requireStampPtrsEqual compares two lists of nullable stamps by instant.
func requireStampPtrsEqual(t *testing.T, want, got []*time.Time, name string) {
	t.Helper()
	require.Len(t, got, len(want), name)
	for i := range want {
		require.NotNil(t, got[i], "%s %d", name, i)
		require.True(t, want[i].Equal(*got[i]), "%s %d: wrote %s, read %s", name, i, *want[i], *got[i])
	}
}

func openNullableTimestampListArmV5(ctx context.Context, t *testing.T, boltURI string) nullableTimestampListArm {
	t.Helper()
	driver, err := neo4jv5.NewDriverWithContext(boltURI, neo4jv5.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v5 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	q := ntlv5.New(driver)
	raw := unionListTemporalArmV5{driver: driver}
	return nullableTimestampListArm{
		open: func(ctx context.Context, e timestampEntry) error {
			return q.OpenEntry(ctx, ntlv5.OpenEntryParams{Id: e.id, Stamps: e.stamps, Maybe: e.maybe, Fixed: e.fixed})
		},
		read: func(ctx context.Context, id int64) (timestampEntry, error) {
			r, err := q.EntryStamps(ctx, id)
			return timestampEntry{id: id, stamps: r.Stamps, maybe: r.Maybe, fixed: r.Fixed}, err
		},
		matching: func(ctx context.Context, stamps []*time.Time, fixed []time.Time) ([]int64, error) {
			return q.EntriesMatching(ctx, ntlv5.EntriesMatchingParams{Stamps: stamps, Fixed: fixed})
		},
		raw: raw.raw,
	}
}

func openNullableTimestampListArmV6(ctx context.Context, t *testing.T, boltURI string) nullableTimestampListArm {
	t.Helper()
	driver, err := neo4jv6.NewDriver(boltURI, neo4jv6.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v6 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	q := ntlv6.New(driver)
	raw := unionListTemporalArmV6{driver: driver}
	return nullableTimestampListArm{
		open: func(ctx context.Context, e timestampEntry) error {
			return q.OpenEntry(ctx, ntlv6.OpenEntryParams{Id: e.id, Stamps: e.stamps, Maybe: e.maybe, Fixed: e.fixed})
		},
		read: func(ctx context.Context, id int64) (timestampEntry, error) {
			r, err := q.EntryStamps(ctx, id)
			return timestampEntry{id: id, stamps: r.Stamps, maybe: r.Maybe, fixed: r.Fixed}, err
		},
		matching: func(ctx context.Context, stamps []*time.Time, fixed []time.Time) ([]int64, error) {
			return q.EntriesMatching(ctx, ntlv6.EntriesMatchingParams{Stamps: stamps, Fixed: fixed})
		},
		raw: raw.raw,
	}
}
