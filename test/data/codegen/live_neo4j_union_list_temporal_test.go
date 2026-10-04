//go:build codegen_live

// The live half of bd gqlc-oo5p: a closed union one of whose MEMBERS is a list
// of a temporal carrier, written and read back through generated code on both
// driver majors.
//
// The defect was a decode arm that asserted each driver element to a POINTER
// (`v3.(*Date)`), which compiles and is false for every element Bolt can hand
// back. TestNeo4jGoldensAssertOnlyDriverCarriers holds that over the text;
// this file holds the claim the text cannot, that the value the server stores
// is the value the caller reads.
//
// StorableProperty admits the width, and this file is the measurement behind
// that admission for this shape: the union's VALUE is one homogeneous array
// (or one INT64), so it is not the heterogeneous array
// TestNeo4jRefusesAHeterogeneousArrayStoredProperty measures refused. The
// first row writes it and reads the slot back raw, so a server that refused it
// would red there rather than in a decode.
//
// THE TIMESTAMP LIST IS WRITTEN RAW. Its member carries as []*time.Time and
// the encode hands that to the driver bare, which both majors refuse — v5
// client-side, v6 by packing each element as an empty map the server will
// not store (bd gqlc-gk6q, which reaches a top-level parameter of the same
// width too). The decode is this bead's and is read back like the other four;
// the refusal is pinned by its own row so the fix reds it.
//
// No NULL element is written. A neo4j property array cannot hold one, so the
// nil-element arm of the decode is unreachable from a stored property and is
// held by the emission's unit rows instead.
//
// The container is shared with live_neo4j_union_list_expression_test.go's
// rows (bd gqlc-dkcz), whose header says why they have no test of their own.
//
// NOT PARALLEL, for live_neo4j_uuid_test.go's reason: it boots a container of
// its own, and running beside the parallel set would raise the peak.
//
// The name is spelled into the test-codegen-live-neo4j recipe;
// TestEveryLiveTestIsRunByARecipeThatNamesIt refuses a live test no recipe
// names.

package fixtures_test

import (
	"context"
	"os"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"

	ultv5 "github.com/areqag/gqlc/test/data/codegen/valid/union_list_temporal_member/golden/neo4j-go-v5"
	ultv6 "github.com/areqag/gqlc/test/data/codegen/valid/union_list_temporal_member/golden/neo4j-go-v6"
)

// diaryUnions is one Diary's five union properties, each holding whichever
// member the row chose, in the major's own carrier types.
type diaryUnions struct {
	days, offsets, locals, stamps, spans *any
}

// unionListTemporalArm is one driver major's view of the fixture. lists builds
// the five list members in that major's carrier types, since a Date of one
// golden package is not a Date of the other.
type unionListTemporalArm struct {
	lists   func() diaryUnions
	open    func(ctx context.Context, id int64, u diaryUnions) error
	whole   func(ctx context.Context, id int64) (diaryUnions, error)
	columns func(ctx context.Context) ([]diaryUnions, error)
	byDays  func(ctx context.Context, days *any) ([]int64, error)
	raw     func(ctx context.Context, t *testing.T, cypher string) []map[string]any
}

func TestNeo4jRoundTripsAUnionOfATemporalList(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	boltURI := startNeo4jContainer(ctx, t)

	arms := []struct {
		name string
		open func(ctx context.Context, t *testing.T, boltURI string) unionListTemporalArm
	}{
		{name: "neo4j-go-v5", open: openUnionListTemporalArmV5},
		{name: "neo4j-go-v6", open: openUnionListTemporalArmV6},
	}
	for _, a := range arms {
		t.Run(a.name, func(t *testing.T) {
			runUnionListTemporalRows(ctx, t, a.open(ctx, t, boltURI))
		})
	}
	t.Run("a list expression of nullable union elements", func(t *testing.T) {
		runUnionListExpressionRows(ctx, t, boltURI)
	})
}

// The stamps are written at a non-UTC offset so the read-back is known to
// carry the instant rather than the wall clock; the comparison is on the
// instant, because the driver hands back its own *time.Location.
var unionListStamps = []time.Time{
	time.Date(2024, 2, 29, 23, 30, 0, 0, time.FixedZone("", 2*3600)),
	time.Date(1999, 12, 31, 12, 0, 0, 500, time.UTC),
}

// unionListStampsCypher stores unionListStamps on Diary 1 through a raw
// session; see the header for why the generated write cannot.
const unionListStampsCypher = "MATCH (d:Diary {id: 1}) SET d.stamps = " +
	"[datetime('2024-02-29T23:30:00+02:00'), datetime('1999-12-31T12:00:00.0000005Z')]"

func runUnionListTemporalRows(ctx context.Context, t *testing.T, arm unionListTemporalArm) {
	t.Helper()
	wipe := func(t *testing.T) {
		t.Helper()
		arm.raw(ctx, t, wipeCypher)
	}
	// openLists writes Diary 1 holding every list member and answers what
	// it wrote: four through the generated write, the stamps raw.
	openLists := func(t *testing.T) diaryUnions {
		t.Helper()
		want := arm.lists()
		generated := want
		generated.stamps = nil
		require.NoError(t, arm.open(ctx, 1, generated),
			"the server refused the write, so StorableProperty's admission of a union whose member is a list is wrong")
		arm.raw(ctx, t, unionListStampsCypher)
		return want
	}

	t.Run("a union holding a temporal list is stored, as a list of that temporal", func(t *testing.T) {
		wipe(t)
		openLists(t)

		rows := arm.raw(ctx, t, "MATCH (d:Diary {id: 1}) RETURN "+
			"[x IN d.days | valueType(x)] AS days, [x IN d.offsets | valueType(x)] AS offsets, "+
			"[x IN d.locals | valueType(x)] AS locals, [x IN d.stamps | valueType(x)] AS stamps, "+
			"[x IN d.spans | valueType(x)] AS spans")
		require.Len(t, rows, 1, "the write reported success and the store holds no such node")
		require.Equal(t, map[string]any{
			"days":    []any{"DATE NOT NULL", "DATE NOT NULL"},
			"offsets": []any{"ZONED TIME NOT NULL", "ZONED TIME NOT NULL"},
			"locals":  []any{"LOCAL TIME NOT NULL", "LOCAL TIME NOT NULL"},
			"stamps":  []any{"ZONED DATETIME NOT NULL", "ZONED DATETIME NOT NULL"},
			"spans":   []any{"DURATION NOT NULL", "DURATION NOT NULL"},
		}, rows[0], "each slot must hold the temporal it was declared as, not its text or a map")
	})

	t.Run("each temporal list reads back equal through the entity and the columns", func(t *testing.T) {
		wipe(t)
		want := openLists(t)

		whole, err := arm.whole(ctx, 1)
		require.NoError(t, err, "the whole-entity decode refused a value it wrote")
		requireDiaryUnionsEqual(t, want, whole, "the whole-entity decode")

		cols, err := arm.columns(ctx)
		require.NoError(t, err, "the column decode refused a value it wrote")
		require.Len(t, cols, 1)
		requireDiaryUnionsEqual(t, want, cols[0], "the column decode")
	})

	t.Run("the INT64 member still reads back as itself", func(t *testing.T) {
		wipe(t)
		var seven any = int64(7)
		want := diaryUnions{days: &seven, offsets: &seven, locals: &seven, stamps: &seven, spans: &seven}
		require.NoError(t, arm.open(ctx, 1, want))

		whole, err := arm.whole(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, want, whole, "the decode dispatches on the wire shape, so an int64 must not be walked as a list")
	})

	t.Run("a temporal list parameter matches the node that holds it and no other", func(t *testing.T) {
		wipe(t)
		held := openLists(t)
		var seven any = int64(7)
		require.NoError(t, arm.open(ctx, 2, diaryUnions{days: &seven}))

		got, err := arm.byDays(ctx, held.days)
		require.NoError(t, err)
		require.Equal(t, []int64{1}, got, "the list member binds as the dates it holds")

		// The control: a match on everything satisfies the row above.
		none, err := arm.byDays(ctx, unionListOtherDays(arm))
		require.NoError(t, err)
		require.Empty(t, none, "a date list no node holds must match no node")
	})

	t.Run("a TIMESTAMP list member is still refused at the write", func(t *testing.T) {
		wipe(t)
		err := arm.open(ctx, 1, diaryUnions{stamps: arm.lists().stamps})
		require.Error(t, err,
			"bd gqlc-gk6q is fixed: write the stamps through the generated write in openLists, delete "+
				"unionListStampsCypher and this row, and close that bead")
	})
}

// unionListOtherDays is the days member with its last element dropped: the
// same carrier type, so it reaches the list arm of the encode, and a value no
// node holds.
func unionListOtherDays(arm unionListTemporalArm) *any {
	var other any
	switch days := (*arm.lists().days).(type) {
	case []*ultv5.Date:
		other = days[:1]
	case []*ultv6.Date:
		other = days[:1]
	}
	return &other
}

// requireDiaryUnionsEqual compares two Diaries' unions, holding the stamps to
// the instant they name: the driver hydrates its own *time.Location, which
// reflect.DeepEqual would read as a difference.
func requireDiaryUnionsEqual(t *testing.T, want, got diaryUnions, msgAndArgs ...any) {
	t.Helper()
	require.Equal(t, *want.days, *got.days, msgAndArgs...)
	require.Equal(t, *want.offsets, *got.offsets, msgAndArgs...)
	require.Equal(t, *want.locals, *got.locals, msgAndArgs...)
	require.Equal(t, *want.spans, *got.spans, msgAndArgs...)

	wantStamps, ok := (*want.stamps).([]*time.Time)
	require.True(t, ok, "the premise: the written stamps are a list of *time.Time")
	gotStamps, ok := (*got.stamps).([]*time.Time)
	require.True(t, ok, "the stamps must come back as the LIST<TIMESTAMP> member, got %T", *got.stamps)
	require.Len(t, gotStamps, len(wantStamps), msgAndArgs...)
	for i := range wantStamps {
		require.NotNil(t, gotStamps[i], msgAndArgs...)
		require.True(t, wantStamps[i].Equal(*gotStamps[i]),
			"stamp %d: wrote %s, read %s", i, wantStamps[i], *gotStamps[i])
	}
}

func unionListStampPtrs() []*time.Time {
	out := make([]*time.Time, len(unionListStamps))
	for i := range unionListStamps {
		out[i] = ptr(unionListStamps[i])
	}
	return out
}

func anyPtr(v any) *any { return &v }

type unionListTemporalArmV5 struct {
	driver neo4jv5.DriverWithContext
	q      *ultv5.Queries
}

func openUnionListTemporalArmV5(ctx context.Context, t *testing.T, boltURI string) unionListTemporalArm {
	t.Helper()
	driver, err := neo4jv5.NewDriverWithContext(boltURI, neo4jv5.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v5 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	a := unionListTemporalArmV5{driver: driver, q: ultv5.New(driver)}
	return unionListTemporalArm{
		lists: func() diaryUnions {
			return diaryUnions{
				days:    anyPtr([]*ultv5.Date{{Year: 2024, Month: 2, Day: 29}, {Year: 1999, Month: 12, Day: 31}}),
				offsets: anyPtr([]*ultv5.Time{{Hour: 9, Minute: 15, OffsetSeconds: -5 * 3600}, {Hour: 23, Second: 59, Nanosecond: 7, OffsetSeconds: 3600}}),
				locals:  anyPtr([]*ultv5.LocalTime{{Hour: 6, Minute: 30}, {Hour: 18, Second: 1, Nanosecond: 999}}),
				stamps:  anyPtr(unionListStampPtrs()),
				spans:   anyPtr([]*ultv5.Duration{{Days: 1, Seconds: 30}, {Seconds: 5, Nanos: 250}}),
			}
		},
		open:    a.open,
		whole:   a.whole,
		columns: a.columns,
		byDays:  a.q.DiariesByDays,
		raw:     a.raw,
	}
}

func (a unionListTemporalArmV5) open(ctx context.Context, id int64, u diaryUnions) error {
	return a.q.OpenDiary(ctx, ultv5.OpenDiaryParams{
		Id: id, Days: u.days, Offsets: u.offsets, Locals: u.locals, Stamps: u.stamps, Spans: u.spans,
	})
}

func (a unionListTemporalArmV5) whole(ctx context.Context, id int64) (diaryUnions, error) {
	got, err := a.q.DiaryWhole(ctx, id)
	if err != nil {
		return diaryUnions{}, err
	}
	return diaryUnions{days: got.Days, offsets: got.Offsets, locals: got.Locals, stamps: got.Stamps, spans: got.Spans}, nil
}

func (a unionListTemporalArmV5) columns(ctx context.Context) ([]diaryUnions, error) {
	rows, err := a.q.DiaryColumns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]diaryUnions, 0, len(rows))
	for _, r := range rows {
		out = append(out, diaryUnions{days: r.Days, offsets: r.Offsets, locals: r.Locals, stamps: r.Stamps, spans: r.Spans})
	}
	return out, nil
}

func (a unionListTemporalArmV5) raw(ctx context.Context, t *testing.T, cypher string) []map[string]any {
	t.Helper()
	session := a.driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the witness ran in: %v", err)
		}
	}()
	result, err := session.Run(ctx, cypher, nil)
	require.NoError(t, err, "run the raw statement: %s", cypher)
	records, err := result.Collect(ctx)
	require.NoError(t, err, "collect the raw statement's records: %s", cypher)
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, record.AsMap())
	}
	return out
}

type unionListTemporalArmV6 struct {
	driver neo4jv6.Driver
	q      *ultv6.Queries
}

func openUnionListTemporalArmV6(ctx context.Context, t *testing.T, boltURI string) unionListTemporalArm {
	t.Helper()
	driver, err := neo4jv6.NewDriver(boltURI, neo4jv6.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v6 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")
	a := unionListTemporalArmV6{driver: driver, q: ultv6.New(driver)}
	return unionListTemporalArm{
		lists: func() diaryUnions {
			return diaryUnions{
				days:    anyPtr([]*ultv6.Date{{Year: 2024, Month: 2, Day: 29}, {Year: 1999, Month: 12, Day: 31}}),
				offsets: anyPtr([]*ultv6.Time{{Hour: 9, Minute: 15, OffsetSeconds: -5 * 3600}, {Hour: 23, Second: 59, Nanosecond: 7, OffsetSeconds: 3600}}),
				locals:  anyPtr([]*ultv6.LocalTime{{Hour: 6, Minute: 30}, {Hour: 18, Second: 1, Nanosecond: 999}}),
				stamps:  anyPtr(unionListStampPtrs()),
				spans:   anyPtr([]*ultv6.Duration{{Days: 1, Seconds: 30}, {Seconds: 5, Nanos: 250}}),
			}
		},
		open:    a.open,
		whole:   a.whole,
		columns: a.columns,
		byDays:  a.q.DiariesByDays,
		raw:     a.raw,
	}
}

func (a unionListTemporalArmV6) open(ctx context.Context, id int64, u diaryUnions) error {
	return a.q.OpenDiary(ctx, ultv6.OpenDiaryParams{
		Id: id, Days: u.days, Offsets: u.offsets, Locals: u.locals, Stamps: u.stamps, Spans: u.spans,
	})
}

func (a unionListTemporalArmV6) whole(ctx context.Context, id int64) (diaryUnions, error) {
	got, err := a.q.DiaryWhole(ctx, id)
	if err != nil {
		return diaryUnions{}, err
	}
	return diaryUnions{days: got.Days, offsets: got.Offsets, locals: got.Locals, stamps: got.Stamps, spans: got.Spans}, nil
}

func (a unionListTemporalArmV6) columns(ctx context.Context) ([]diaryUnions, error) {
	rows, err := a.q.DiaryColumns(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]diaryUnions, 0, len(rows))
	for _, r := range rows {
		out = append(out, diaryUnions{days: r.Days, offsets: r.Offsets, locals: r.Locals, stamps: r.Stamps, spans: r.Spans})
	}
	return out, nil
}

func (a unionListTemporalArmV6) raw(ctx context.Context, t *testing.T, cypher string) []map[string]any {
	t.Helper()
	session := a.driver.NewSession(ctx, neo4jv6.SessionConfig{AccessMode: neo4jv6.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the witness ran in: %v", err)
		}
	}()
	result, err := session.Run(ctx, cypher, nil)
	require.NoError(t, err, "run the raw statement: %s", cypher)
	records, err := result.Collect(ctx)
	require.NoError(t, err, "collect the raw statement's records: %s", cypher)
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, record.AsMap())
	}
	return out
}
