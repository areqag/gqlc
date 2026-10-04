//go:build codegen_live

// bd gqlc-nvb4: a TIMESTAMP placed inside a parameter whose carrier is any
// or []any (ANY VALUE, LIST<ANY VALUE>, ADR 0020's open union), written and
// matched through the generated code of fixture any_param_timestamp on both
// driver majors.
//
// The declared-width binds go through fromTimestamp (bd gqlc-m3ax), but an
// ANY carrier has no declared width to dispatch on, so its value reached the
// driver as the caller built it. Both majors send a time.Time whose zone is
// not named "Offset" as a zone id, and the server refuses one that is not
// IANA: time.Now() in time.Local is refused as `Illegal zone identifier:
// "Local"`. A *time.Time inside an ANY value is refused before that, as
// gqlc-gk6q measured for a list element: v5 raises `Usage of type
// '*time.Time' is not supported` and v6 sends an empty map.
//
// The rows share the container of TestNeo4jRoundTripsANullableElementTemporalList
// rather than booting one of their own.

package fixtures_test

import (
	"context"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"

	aptv5 "github.com/areqag/gqlc/test/data/codegen/valid/any_param_timestamp/golden/neo4j-go-v5"
	aptv6 "github.com/areqag/gqlc/test/data/codegen/valid/any_param_timestamp/golden/neo4j-go-v6"
)

// anySlot is one :Slot of the fixture. Every ANY carrier is spelled the same
// on both goldens, so one struct serves both majors.
type anySlot struct {
	id      int64
	payload *any
	marker  any
	bag     *[]any
	loose   []any
}

// anyParamTimestampArm is one driver major's view of the fixture.
type anyParamTimestampArm struct {
	add       func(ctx context.Context, s anySlot) error
	byPayload func(ctx context.Context, v *any) ([]int64, error)
	byMarker  func(ctx context.Context, v any) ([]int64, error)
	byBag     func(ctx context.Context, v *[]any) ([]int64, error)
	byLoose   func(ctx context.Context, v []any) ([]int64, error)
	raw       func(ctx context.Context, t *testing.T, cypher string) []map[string]any
}

func runAnyParamTimestampRows(ctx context.Context, t *testing.T, arm anyParamTimestampArm) {
	t.Helper()
	wipe := func(t *testing.T) {
		t.Helper()
		arm.raw(ctx, t, wipeCypher)
	}
	at := time.Date(2024, 2, 29, 21, 30, 0, 0, time.UTC)
	// zoned holds one stamp for each kind of location m3ax measured
	// refused: time.Local (named "Local"), an unnamed fixed zone and an
	// abbreviated one.
	local := at.In(time.Local)
	unnamed := at.In(time.FixedZone("", 2*3600))
	abbreviated := at.In(time.FixedZone("CEST", -3*3600-1800))
	// now is time.Now() in time.Local, taken once so the row can compare
	// what it wrote.
	now := time.Now()
	zonedSlot := func() anySlot {
		return anySlot{
			id:      1,
			payload: ptr[any](unnamed),
			marker:  now,
			// A *time.Time element, which neither major packs.
			bag:   ptr([]any{ptr(abbreviated), ptr(local)}),
			loose: []any{local, unnamed},
		}
	}

	// One carrier per row, so a refusal names the carrier and the shape
	// that caused it; the other carriers hold nothing temporal.
	for _, c := range []struct {
		name string
		slot anySlot
	}{
		{"any holding time.Now()", anySlot{id: 1, marker: time.Now()}},
		{"any holding a *time.Time", anySlot{id: 1, marker: ptr(local)}},
		{"*any holding an unnamed fixed zone", anySlot{id: 1, marker: int64(0), payload: ptr[any](unnamed)}},
		{"[]any holding time.Local", anySlot{id: 1, marker: int64(0), loose: []any{local}}},
		{"*[]any holding an abbreviated zone", anySlot{id: 1, marker: int64(0), bag: ptr([]any{abbreviated})}},
	} {
		t.Run("binds: "+c.name, func(t *testing.T) {
			wipe(t)
			require.NoError(t, arm.add(ctx, c.slot), "a stamp outside UTC inside an ANY parameter was refused")
		})
	}

	t.Run("a stamp in any zone binds through every ANY carrier, offset kept", func(t *testing.T) {
		wipe(t)
		s := zonedSlot()
		require.NoError(t, arm.add(ctx, s), "a stamp outside UTC inside an ANY parameter was refused")

		rows := arm.raw(ctx, t, "MATCH (s:Slot {id: 1}) RETURN s.payload AS payload, s.marker AS marker, s.bag AS bag, s.loose AS loose")
		require.Len(t, rows, 1, "the write reported success and the store holds no such node")
		row := rows[0]
		requireStoredStamp(t, unnamed, row["payload"], "payload")
		requireStoredStamp(t, now, row["marker"], "marker")
		bag, ok := row["bag"].([]any)
		require.True(t, ok, "bag: stored %T, want a list", row["bag"])
		require.Len(t, bag, 2)
		requireStoredStamp(t, abbreviated, bag[0], "bag 0")
		requireStoredStamp(t, local, bag[1], "bag 1")
		loose, ok := row["loose"].([]any)
		require.True(t, ok, "loose: stored %T, want a list", row["loose"])
		require.Len(t, loose, 2)
		requireStoredStamp(t, local, loose[0], "loose 0")
		requireStoredStamp(t, unnamed, loose[1], "loose 1")
	})

	t.Run("each ANY parameter holding a stamp matches the node holding it and no other", func(t *testing.T) {
		wipe(t)
		s := zonedSlot()
		require.NoError(t, arm.add(ctx, s))
		// The control node holds other instants, in UTC so its own write
		// never depended on the fix.
		other := at.Add(time.Hour)
		require.NoError(t, arm.add(ctx, anySlot{
			id:      2,
			payload: ptr[any](other),
			marker:  other,
			bag:     ptr([]any{other, other}),
			loose:   []any{other, other},
		}))

		for _, c := range []struct {
			name  string
			match func() ([]int64, error)
		}{
			{"payload", func() ([]int64, error) { return arm.byPayload(ctx, s.payload) }},
			{"marker", func() ([]int64, error) { return arm.byMarker(ctx, s.marker) }},
			{"bag", func() ([]int64, error) { return arm.byBag(ctx, s.bag) }},
			{"loose", func() ([]int64, error) { return arm.byLoose(ctx, s.loose) }},
		} {
			got, err := c.match()
			require.NoError(t, err, "%s: a stamp outside UTC inside an ANY parameter was refused", c.name)
			require.Equal(t, []int64{1}, got, "%s: the parameter binds as the instants it holds", c.name)
		}
	})

	// The walk must leave every value that is not a stamp as it was, and a
	// nil pointer carrier must still bind the null its nullability declared.
	t.Run("a value that is not a stamp binds unchanged, and a nil carrier binds null", func(t *testing.T) {
		wipe(t)
		require.NoError(t, arm.add(ctx, anySlot{
			id:     1,
			marker: int64(7),
			loose:  []any{"a", "b"},
		}))
		rows := arm.raw(ctx, t, "MATCH (s:Slot {id: 1}) RETURN s.payload AS payload, s.marker AS marker, s.bag AS bag, s.loose AS loose")
		require.Len(t, rows, 1)
		require.Equal(t, map[string]any{
			"payload": nil,
			"marker":  int64(7),
			"bag":     nil,
			"loose":   []any{"a", "b"},
		}, rows[0])
	})
}

// requireStoredStamp compares a stamp read back raw from the driver with the
// one written, by instant and by offset.
func requireStoredStamp(t *testing.T, want time.Time, got any, name string) {
	t.Helper()
	stamp, ok := got.(time.Time)
	require.True(t, ok, "%s: stored %T (%v), want a zoned datetime", name, got, got)
	requireSameInstantAndOffset(t, want, stamp, name)
}

func openAnyParamTimestampArmV5(ctx context.Context, t *testing.T, boltURI string) anyParamTimestampArm {
	t.Helper()
	driver, err := neo4jv5.NewDriverWithContext(boltURI, neo4jv5.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v5 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	q := aptv5.New(driver)
	raw := unionListTemporalArmV5{driver: driver}
	return anyParamTimestampArm{
		add: func(ctx context.Context, s anySlot) error {
			return q.AddSlot(ctx, aptv5.AddSlotParams{Id: s.id, Payload: s.payload, Marker: s.marker, Bag: s.bag, Loose: s.loose})
		},
		byPayload: q.SlotIdsByPayload,
		byMarker:  q.SlotIdsByMarker,
		byBag:     q.SlotIdsByBag,
		byLoose:   q.SlotIdsByLoose,
		raw:       raw.raw,
	}
}

func openAnyParamTimestampArmV6(ctx context.Context, t *testing.T, boltURI string) anyParamTimestampArm {
	t.Helper()
	driver, err := neo4jv6.NewDriver(boltURI, neo4jv6.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v6 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	q := aptv6.New(driver)
	raw := unionListTemporalArmV6{driver: driver}
	return anyParamTimestampArm{
		add: func(ctx context.Context, s anySlot) error {
			return q.AddSlot(ctx, aptv6.AddSlotParams{Id: s.id, Payload: s.payload, Marker: s.marker, Bag: s.bag, Loose: s.loose})
		},
		byPayload: q.SlotIdsByPayload,
		byMarker:  q.SlotIdsByMarker,
		byBag:     q.SlotIdsByBag,
		byLoose:   q.SlotIdsByLoose,
		raw:       raw.raw,
	}
}
