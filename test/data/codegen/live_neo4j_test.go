//go:build codegen_live

// The neo4j arms: the container helper both driver majors share, and one
// adapter per major. The battery they serve is in live_test.go.

package fixtures_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"

	edgeunionv5 "github.com/areqag/gqlc/test/data/codegen/valid/edge_union_undeclared_relationship_type/golden/neo4j-go-v5"
	edgeunionv6 "github.com/areqag/gqlc/test/data/codegen/valid/edge_union_undeclared_relationship_type/golden/neo4j-go-v6"
	entityedgev5 "github.com/areqag/gqlc/test/data/codegen/valid/entity_edge_projected_one/golden/neo4j-go-v5"
	entityedgev6 "github.com/areqag/gqlc/test/data/codegen/valid/entity_edge_projected_one/golden/neo4j-go-v6"
	entitynodev5 "github.com/areqag/gqlc/test/data/codegen/valid/entity_node_projected_one/golden/neo4j-go-v5"
	entitynodev6 "github.com/areqag/gqlc/test/data/codegen/valid/entity_node_projected_one/golden/neo4j-go-v6"
	listlistv5 "github.com/areqag/gqlc/test/data/codegen/valid/list_list_int/golden/neo4j-go-v5"
	listlistv6 "github.com/areqag/gqlc/test/data/codegen/valid/list_list_int/golden/neo4j-go-v6"
	deeplistv5 "github.com/areqag/gqlc/test/data/codegen/valid/list_list_list_int/golden/neo4j-go-v5"
	deeplistv6 "github.com/areqag/gqlc/test/data/codegen/valid/list_list_list_int/golden/neo4j-go-v6"
	ldtv5 "github.com/areqag/gqlc/test/data/codegen/valid/local_datetime_constructed_column/golden/neo4j-go-v5"
	ldtv6 "github.com/areqag/gqlc/test/data/codegen/valid/local_datetime_constructed_column/golden/neo4j-go-v6"
	manycolmanyv5 "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many/golden/neo4j-go-v5"
	manycolmanyv6 "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many/golden/neo4j-go-v6"
	mixedv5 "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden/neo4j-go-v5"
	mixedv6 "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden/neo4j-go-v6"
	onecolonev5 "github.com/areqag/gqlc/test/data/codegen/valid/one_col_one_param_one/golden/neo4j-go-v5"
	onecolonev6 "github.com/areqag/gqlc/test/data/codegen/valid/one_col_one_param_one/golden/neo4j-go-v6"
	scalarmapv5 "github.com/areqag/gqlc/test/data/codegen/valid/scalar_map/golden/neo4j-go-v5"
	scalarmapv6 "github.com/areqag/gqlc/test/data/codegen/valid/scalar_map/golden/neo4j-go-v6"
	anypropv5 "github.com/areqag/gqlc/test/data/codegen/valid/schema_any_property/golden/neo4j-go-v5"
	anypropv6 "github.com/areqag/gqlc/test/data/codegen/valid/schema_any_property/golden/neo4j-go-v6"
	temporalv5 "github.com/areqag/gqlc/test/data/codegen/valid/temporal_property_roundtrip/golden/neo4j-go-v5"
	temporalv6 "github.com/areqag/gqlc/test/data/codegen/valid/temporal_property_roundtrip/golden/neo4j-go-v6"
	tsv5 "github.com/areqag/gqlc/test/data/codegen/valid/timestamp_property_roundtrip/golden/neo4j-go-v5"
	tsv6 "github.com/areqag/gqlc/test/data/codegen/valid/timestamp_property_roundtrip/golden/neo4j-go-v6"
	zonedv5 "github.com/areqag/gqlc/test/data/codegen/valid/zoned_time_roundtrip/golden/neo4j-go-v5"
	zonedv6 "github.com/areqag/gqlc/test/data/codegen/valid/zoned_time_roundtrip/golden/neo4j-go-v6"
)

const (
	// Pinned by digest, not tag, so an upstream rebuild of neo4j:5-community
	// cannot serve a stale cached PASS from Go's test cache (bd gqlc-9u5).
	// Refresh with: curl -sSL https://hub.docker.com/v2/repositories/library/neo4j/tags/5-community/ | jq -r .digest
	neo4jImage    = "neo4j@sha256:362542416de6c09a971484d1893878016cc3b5cdec166e54b1c824a220ecd6b9"
	neo4jUser     = "neo4j"
	neo4jPassword = "gqlctest1"
	// wipeCypher DETACHes so any prior scenario's leftover edges are removed
	// alongside its nodes; DELETE on an empty graph is a no-op in neo4j 5.
	wipeCypher = "MATCH (n) DETACH DELETE n"
)

// startNeo4jContainer boots one neo4j:5-community container and returns its
// bolt URI. Cleanup is registered on t; the caller does not terminate.
// Testcontainers is driver-version-agnostic, so both neo4j arms share it and
// cannot drift apart on container setup.
func startNeo4jContainer(ctx context.Context, t *testing.T) string {
	t.Helper()
	container, err := tcneo4j.Run(ctx,
		neo4jImage,
		tcneo4j.WithAdminPassword(neo4jPassword),
	)
	require.NoError(t, err, "start neo4j testcontainer")
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate neo4j: %v", err)
		}
	})
	boltURI, err := container.BoltUrl(ctx)
	require.NoError(t, err, "read bolt uri")
	return boltURI
}

// neo4jV5 is the neo4j-go-v5 arm: one container, one driver, and the handles
// built on it. Every scenario sees the single graph the server exposes, so
// isolation is a DETACH-wipe as the scenario opens and scenarios must not
// overlap.
type neo4jV5 struct {
	driver     neo4jv5.DriverWithContext
	one        oneColOneParamOneV5
	mixed      mixedReadWriteBatchV5
	many       manyColManyV5
	nestedList nestedListV5
	deepList   deepNestedListV5
	entityNode entityNodeV5
	entityEdge entityEdgeV5
	anyValue   anyValueColumnsV5
	edgeUnion  edgeUnionV5
	timestamps timestampRoundtripV5
	temporals  temporalRoundtripV5
	builtLDT   localDateTimeColumnV5
	zonedTimes zonedTimeRoundtripV5
	maps       mapColumnV5
}

// neo4jStoredTemporal is both neo4j arms' declaration of what their target
// keeps of a written zoneless temporal: all of it. The driver carries a
// LocalTime and a Duration natively, at nanosecond resolution and with the
// Months, Days and Seconds of a duration held apart, so nothing is
// truncated and nothing is folded. The identity here is that claim, not
// the absence of one — an arm that coarsened a value would have to change
// this to stay green.
type neo4jStoredTemporal struct{}

func (neo4jStoredTemporal) storedLocalTime(v localTimeValue) localTimeValue { return v }

func (neo4jStoredTemporal) storedDuration(v durationValue) durationValue { return v }

// neo4jSeenOnCypher spells the nullable-date seed for both neo4j arms.
// date() is neo4j's constructor for the width, which is what makes this
// outside the dialect intersection backend.seed serves and the reason the
// seed is a method on the arm at all.
func neo4jSeenOnCypher(id int64, on dateValue) string {
	return fmt.Sprintf("MATCH (r:Reading {id: %d}) SET r.seenOn = date('%04d-%02d-%02d')", id, on.Year, on.Month, on.Day)
}

func startNeo4jV5(ctx context.Context, t *testing.T) harness {
	t.Helper()
	boltURI := startNeo4jContainer(ctx, t)

	driver, err := neo4jv5.NewDriverWithContext(boltURI, neo4jv5.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v5 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")

	return &neo4jV5{
		driver:     driver,
		one:        oneColOneParamOneV5{q: onecolonev5.New(driver)},
		mixed:      mixedReadWriteBatchV5{q: mixedv5.New(driver)},
		many:       manyColManyV5{q: manycolmanyv5.New(driver)},
		nestedList: nestedListV5{q: listlistv5.New(driver)},
		deepList:   deepNestedListV5{q: deeplistv5.New(driver)},
		entityNode: entityNodeV5{q: entitynodev5.New(driver)},
		entityEdge: entityEdgeV5{q: entityedgev5.New(driver)},
		anyValue:   anyValueColumnsV5{q: anypropv5.New(driver)},
		edgeUnion:  edgeUnionV5{q: edgeunionv5.New(driver)},
		timestamps: timestampRoundtripV5{q: tsv5.New(driver)},
		temporals:  temporalRoundtripV5{q: temporalv5.New(driver)},
		builtLDT:   localDateTimeColumnV5{q: ldtv5.New(driver)},
		zonedTimes: zonedTimeRoundtripV5{q: zonedv5.New(driver)},
		maps:       mapColumnV5{q: scalarmapv5.New(driver)},
	}
}

func (h *neo4jV5) parallelScenarios() bool { return false }

func (h *neo4jV5) scenario(ctx context.Context, t *testing.T) backend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV5) writeScenario(ctx context.Context, t *testing.T) writeBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV5) edgeUnionScenario(ctx context.Context, t *testing.T) edgeUnionBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV5) temporalScenario(ctx context.Context, t *testing.T) temporalBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV5) localDateTimeColumnScenario(ctx context.Context, t *testing.T) localDateTimeColumnBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV5) mapColumnScenario(ctx context.Context, t *testing.T) mapColumnBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV5) zonedTimeScenario(ctx context.Context, t *testing.T) zonedTimeBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV5) newScenario(ctx context.Context, t *testing.T) neo4jV5Scenario {
	t.Helper()
	s := neo4jV5Scenario{arm: h}
	s.seed(ctx, t, wipeCypher)
	return s
}

// neo4jV5Scenario is one scenario's view of the neo4jV5 arm: the server's
// single graph, wiped as the scenario opened.
type neo4jV5Scenario struct {
	neo4jStoredTemporal
	arm *neo4jV5
}

func (s neo4jV5Scenario) seed(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	session := s.arm.driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close session: %v", err)
		}
	}()
	_, err := neo4jv5.ExecuteWrite(ctx, session, func(tx neo4jv5.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, cypher, nil)
		if err != nil {
			return nil, err
		}
		return result.Consume(ctx)
	})
	require.NoError(t, err, "seed: %s", cypher)
}

func (s neo4jV5Scenario) oneColOneParamOne() oneColOneParamOneQuerier { return s.arm.one }

func (s neo4jV5Scenario) mixedReadWriteBatch() mixedReadWriteBatchQuerier { return s.arm.mixed }

func (s neo4jV5Scenario) timestampRoundtrip() timestampRoundtripQuerier { return s.arm.timestamps }

func (s neo4jV5Scenario) tx() txQuerier { return s.arm.mixed }

// refuseBeginOnBoundHandle drives Begin on a handle WithTx has bound to
// a managed transaction. The driver only hands a ManagedTransaction out
// inside ExecuteWrite's closure, so the call has to happen there and the
// closure carries Begin's error back out — verbatim, nil included.
//
// Without this row neo4j's runtime refusal would be live-unwitnessed:
// deleting the Tx.Queries accessor removed the only other way to reach
// it, and tx.Begin is now a compile error rather than a refusal.
func (s neo4jV5Scenario) refuseBeginOnBoundHandle(ctx context.Context, t *testing.T) error {
	t.Helper()
	session := s.arm.driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the refusal ran in: %v", err)
		}
	}()

	// The refusal is the closure's RESULT, not its error: returned as an
	// error it would be indistinguishable from a driver failure, and
	// ExecuteWrite would retry it.
	refusal, err := neo4jv5.ExecuteWrite(ctx, session, func(mt neo4jv5.ManagedTransaction) (error, error) {
		nested, beginErr := s.arm.mixed.q.WithTx(mt).Begin(ctx)
		if beginErr != nil {
			return beginErr, nil
		}
		// Begin was served where it should have been refused. The row is
		// about to fail on the nil; give the transaction back first so
		// that the failing run does not also strand a connection.
		if rbErr := nested.Rollback(ctx); rbErr != nil {
			t.Logf("rollback the transaction Begin should have refused: %v", rbErr)
		}
		return nil, nil
	})
	require.NoError(t, err, "open the managed transaction the refusal is asked in")
	return refusal
}

// timestampRoundtripV5 binds the TIMESTAMP fixture. The driver carries a
// datetime natively, so the whole of this arm is the identity — which is
// what makes the comparison against the AGE arm worth running.
type timestampRoundtripV5 struct{ q *tsv5.Queries }

func (a timestampRoundtripV5) addEvent(ctx context.Context, id int64, occurredAt time.Time) error {
	return a.q.AddEvent(ctx, tsv5.AddEventParams{Id: id, OccurredAt: occurredAt})
}

func (a timestampRoundtripV5) eventsAfter(ctx context.Context, since time.Time) ([]int64, error) {
	return a.q.EventsAfter(ctx, since)
}

func (a timestampRoundtripV5) eventAt(ctx context.Context, id int64) (time.Time, error) {
	return a.q.EventAt(ctx, id)
}

func (a timestampRoundtripV5) oneEvent(ctx context.Context, id int64) (eventEntity, error) {
	e, err := a.q.OneEvent(ctx, id)
	if err != nil {
		return eventEntity{}, err
	}
	return eventEntity{ID: e.Id, OccurredAt: e.OccurredAt, SeenAt: e.SeenAt}, nil
}

func (s neo4jV5Scenario) temporalRoundtrip() temporalRoundtripQuerier { return s.arm.temporals }

func (s neo4jV5Scenario) seedSeenOn(ctx context.Context, t *testing.T, id int64, on dateValue) {
	t.Helper()
	s.seed(ctx, t, neo4jSeenOnCypher(id, on))
}

func (s neo4jV5Scenario) localDateTimeColumn() localDateTimeColumnQuerier { return s.arm.builtLDT }

func (s neo4jV5Scenario) mapColumn() mapColumnQuerier { return s.arm.maps }

func (s neo4jV5Scenario) zonedTimeRoundtrip() zonedTimeRoundtripQuerier { return s.arm.zonedTimes }

// temporalRoundtripV5 binds the zoneless temporal fixture. Every
// conversion between the battery's shape and the target's is a field
// copy: the generated carriers hold components and so does the battery,
// so an arm that had reached for the driver's dbtype.* on its public
// surface would not compile here — which is the surface property ADR
// 0033 establishes, restated where a live value passes through it.
type temporalRoundtripV5 struct{ q *temporalv5.Queries }

func (a temporalRoundtripV5) addReading(ctx context.Context, id int64, onDate dateValue, atLocal localTimeValue, elapsed durationValue) error {
	return a.q.AddReading(ctx, temporalv5.AddReadingParams{
		Id:      id,
		OnDate:  temporalv5.Date{Year: onDate.Year, Month: onDate.Month, Day: onDate.Day},
		AtLocal: temporalv5.LocalTime{Hour: atLocal.Hour, Minute: atLocal.Minute, Second: atLocal.Second, Nanosecond: atLocal.Nanosecond},
		Elapsed: temporalv5.Duration{Months: elapsed.Months, Days: elapsed.Days, Seconds: elapsed.Seconds, Nanos: elapsed.Nanos},
	})
}

func (a temporalRoundtripV5) readingsFrom(ctx context.Context, from dateValue) ([]int64, error) {
	return a.q.ReadingsFrom(ctx, temporalv5.Date{Year: from.Year, Month: from.Month, Day: from.Day})
}

func (a temporalRoundtripV5) readingsSeenFrom(ctx context.Context, seenFrom *dateValue) ([]int64, error) {
	if seenFrom == nil {
		return a.q.ReadingsSeenFrom(ctx, nil)
	}
	return a.q.ReadingsSeenFrom(ctx, &temporalv5.Date{Year: seenFrom.Year, Month: seenFrom.Month, Day: seenFrom.Day})
}

func (a temporalRoundtripV5) readingDate(ctx context.Context, id int64) (dateValue, error) {
	d, err := a.q.ReadingDate(ctx, id)
	if err != nil {
		return dateValue{}, err
	}
	return dateValue{Year: d.Year, Month: d.Month, Day: d.Day}, nil
}

func (a temporalRoundtripV5) readingLocalTime(ctx context.Context, id int64) (localTimeValue, error) {
	v, err := a.q.ReadingLocalTime(ctx, id)
	if err != nil {
		return localTimeValue{}, err
	}
	return localTimeValue{Hour: v.Hour, Minute: v.Minute, Second: v.Second, Nanosecond: v.Nanosecond}, nil
}

func (a temporalRoundtripV5) readingElapsed(ctx context.Context, id int64) (durationValue, error) {
	v, err := a.q.ReadingElapsed(ctx, id)
	if err != nil {
		return durationValue{}, err
	}
	return durationValue{Months: v.Months, Days: v.Days, Seconds: v.Seconds, Nanos: v.Nanos}, nil
}

func (a temporalRoundtripV5) oneReading(ctx context.Context, id int64) (readingEntity, error) {
	r, err := a.q.OneReading(ctx, id)
	if err != nil {
		return readingEntity{}, err
	}
	out := readingEntity{
		ID:      r.Id,
		OnDate:  dateValue{Year: r.OnDate.Year, Month: r.OnDate.Month, Day: r.OnDate.Day},
		AtLocal: localTimeValue{Hour: r.AtLocal.Hour, Minute: r.AtLocal.Minute, Second: r.AtLocal.Second, Nanosecond: r.AtLocal.Nanosecond},
		Elapsed: durationValue{Months: r.Elapsed.Months, Days: r.Elapsed.Days, Seconds: r.Elapsed.Seconds, Nanos: r.Elapsed.Nanos},
	}
	if r.SeenOn != nil {
		out.SeenOn = &dateValue{Year: r.SeenOn.Year, Month: r.SeenOn.Month, Day: r.SeenOn.Day}
	}
	return out, nil
}

func (a temporalRoundtripV5) errNoRows() error { return temporalv5.ErrNoRows }

// localDateTimeColumnV5 binds the constructed-LOCALDATETIME fixture.
type localDateTimeColumnV5 struct{ q *ldtv5.Queries }

func (a localDateTimeColumnV5) builtLocalDateTime(ctx context.Context) (localDateTimeValue, error) {
	v, err := a.q.BuiltLocalDateTime(ctx)
	if err != nil {
		return localDateTimeValue{}, err
	}
	return localDateTimeValue{
		Year: v.Year, Month: v.Month, Day: v.Day,
		Hour: v.Hour, Minute: v.Minute, Second: v.Second, Nanosecond: v.Nanosecond,
	}, nil
}

// mapColumnV5 binds the map-column fixture. The method it forwards returns
// the generated map[string]any unchanged, unlike the temporal adapters
// either side: those restate a per-package struct so the battery can read
// components off it, and a map has no per-package shape to restate. So this
// adapter narrows nothing, and what the scenario asserts on is the value the
// emitted read produced.
type mapColumnV5 struct{ q *scalarmapv5.Queries }

func (a mapColumnV5) oneMap(ctx context.Context) (map[string]any, error) { return a.q.OneMap(ctx) }

// zonedTimeRoundtripV5 binds the zoned temporal fixture.
type zonedTimeRoundtripV5 struct{ q *zonedv5.Queries }

func (a zonedTimeRoundtripV5) addSlot(ctx context.Context, id int64, startsAt timeValue) error {
	return a.q.AddSlot(ctx, zonedv5.AddSlotParams{Id: id, StartsAt: zonedTimeV5(startsAt)})
}

func (a zonedTimeRoundtripV5) slotsFrom(ctx context.Context, from timeValue) ([]int64, error) {
	return a.q.SlotsFrom(ctx, zonedTimeV5(from))
}

func (a zonedTimeRoundtripV5) slotStart(ctx context.Context, id int64) (timeValue, error) {
	v, err := a.q.SlotStart(ctx, id)
	if err != nil {
		return timeValue{}, err
	}
	return batteryTimeV5(v), nil
}

func (a zonedTimeRoundtripV5) oneSlot(ctx context.Context, id int64) (slotEntity, error) {
	s, err := a.q.OneSlot(ctx, id)
	if err != nil {
		return slotEntity{}, err
	}
	return slotEntity{ID: s.Id, StartsAt: batteryTimeV5(s.StartsAt)}, nil
}

func (a zonedTimeRoundtripV5) errNoRows() error { return zonedv5.ErrNoRows }

func zonedTimeV5(v timeValue) zonedv5.Time {
	return zonedv5.Time{Hour: v.Hour, Minute: v.Minute, Second: v.Second, Nanosecond: v.Nanosecond, OffsetSeconds: v.OffsetSeconds}
}

func batteryTimeV5(v zonedv5.Time) timeValue {
	return timeValue{Hour: v.Hour, Minute: v.Minute, Second: v.Second, Nanosecond: v.Nanosecond, OffsetSeconds: v.OffsetSeconds}
}

func (s neo4jV5Scenario) manyColMany() manyColManyQuerier { return s.arm.many }

func (s neo4jV5Scenario) nestedList() nestedListQuerier { return s.arm.nestedList }

func (s neo4jV5Scenario) deepNestedList() deepNestedListQuerier { return s.arm.deepList }

func (s neo4jV5Scenario) entityNodeProjectedOne() entityNodeQuerier { return s.arm.entityNode }

func (s neo4jV5Scenario) entityEdgeProjectedOne() entityEdgeQuerier { return s.arm.entityEdge }

func (s neo4jV5Scenario) anyValueColumns() anyValueColumnQuerier { return s.arm.anyValue }

// anyValueColumnsV5 binds the ANY VALUE column fixture, passing both columns
// through untouched for the reason its AGE twin gives.
type anyValueColumnsV5 struct{ q *anypropv5.Queries }

func (a anyValueColumnsV5) eventMarker(ctx context.Context) (any, error) {
	return a.q.EventMarker(ctx)
}

func (a anyValueColumnsV5) eventPayload(ctx context.Context) (*any, error) {
	return a.q.EventPayload(ctx)
}

func (a anyValueColumnsV5) errNoRows() error { return anypropv5.ErrNoRows }

func (s neo4jV5Scenario) edgeUnionUndeclared() edgeUnionQuerier { return s.arm.edgeUnion }

type edgeUnionV5 struct{ q *edgeunionv5.Queries }

// actionOnPost narrows the target's own sealed sum to the battery's shape. The
// type switch is the caller-side half of what the emitted dispatch decided: a
// member reaching here is one the generated code chose an arm for, so a
// member this switch does not know is a surface change and not a wire value.
func (a edgeUnionV5) actionOnPost(ctx context.Context, postID int64) (edgeUnionAction, error) {
	got, err := a.q.ActionOnPost(ctx, postID)
	if err != nil {
		return edgeUnionAction{}, err
	}
	switch v := got.(type) {
	case edgeunionv5.Authored:
		return edgeUnionAction{Kind: "AUTHORED", Since: v.Since}, nil
	case edgeunionv5.Likes:
		return edgeUnionAction{Kind: "LIKES", Rating: v.Rating}, nil
	default:
		return edgeUnionAction{}, fmt.Errorf("battery: %T is not a member this scenario knows", v)
	}
}

func (a edgeUnionV5) errNoRows() error { return edgeunionv5.ErrNoRows }

type entityNodeV5 struct{ q *entitynodev5.Queries }

func (a entityNodeV5) onePerson(ctx context.Context) (personEntity, error) {
	p, err := a.q.OnePerson(ctx)
	if err != nil {
		return personEntity{}, err
	}
	return personEntity{ID: p.Id, MiddleName: p.MiddleName, Name: p.Name}, nil
}

func (a entityNodeV5) errNoRows() error { return entitynodev5.ErrNoRows }

type entityEdgeV5 struct{ q *entityedgev5.Queries }

func (a entityEdgeV5) oneActedIn(ctx context.Context) (actedInEntity, error) {
	r, err := a.q.OneActedIn(ctx)
	if err != nil {
		return actedInEntity{}, err
	}
	return actedInEntity{Since: r.Since}, nil
}

func (a entityEdgeV5) errNoRows() error { return entityedgev5.ErrNoRows }

type oneColOneParamOneV5 struct{ q *onecolonev5.Queries }

func (a oneColOneParamOneV5) personName(ctx context.Context, id int64) (string, error) {
	return a.q.PersonName(ctx, id)
}

func (a oneColOneParamOneV5) errNoRows() error { return onecolonev5.ErrNoRows }

func (a oneColOneParamOneV5) errMultipleResults() error { return onecolonev5.ErrMultipleResults }

type mixedReadWriteBatchV5 struct{ q *mixedv5.Queries }

func (a mixedReadWriteBatchV5) getPersonName(ctx context.Context, id int64) (string, error) {
	return a.q.GetPersonName(ctx, id)
}

func (a mixedReadWriteBatchV5) removePerson(ctx context.Context, id int64) error {
	return a.q.RemovePerson(ctx, id)
}

func (a mixedReadWriteBatchV5) errNoRows() error { return mixedv5.ErrNoRows }

func (a mixedReadWriteBatchV5) errTxDone() error { return mixedv5.ErrTxDone }

func (a mixedReadWriteBatchV5) begin(ctx context.Context) (liveTx, error) {
	tx, err := a.q.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return mixedTxV5{tx: tx}, nil
}

// mixedTxV5 is the v5 arm's generated *Tx. Every read and write on it is
// a query method promoted from the embedded core, so the handle under
// test is the transaction itself.
type mixedTxV5 struct{ tx *mixedv5.Tx }

func (a mixedTxV5) commit(ctx context.Context) error { return a.tx.Commit(ctx) }

func (a mixedTxV5) rollback(ctx context.Context) error { return a.tx.Rollback(ctx) }

func (a mixedTxV5) removePerson(ctx context.Context, id int64) error {
	return a.tx.RemovePerson(ctx, id)
}

func (a mixedTxV5) getPersonName(ctx context.Context, id int64) (string, error) {
	return a.tx.GetPersonName(ctx, id)
}

// nestedListV5 binds the list_list_int fixture. The generated method already
// returns the battery's shape, so unlike the entity adapters there is nothing
// to convert — which is the point: [][]int64 is what the emitted decoder
// built out of the driver's []any of []any.
type nestedListV5 struct{ q *listlistv5.Queries }

func (a nestedListV5) nestedList(ctx context.Context) ([][]int64, error) {
	return a.q.NestedList(ctx)
}

// deepNestedListV5 binds the list_list_list_int fixture, the one whose
// emission carries depth-suffixed decoder locals.
type deepNestedListV5 struct{ q *deeplistv5.Queries }

func (a deepNestedListV5) deepNestedList(ctx context.Context) ([][][]int64, error) {
	return a.q.DeepNestedList(ctx)
}

type manyColManyV5 struct{ q *manycolmanyv5.Queries }

func (a manyColManyV5) peopleByAgeAndLocale(ctx context.Context, minAge int64, locale string) ([]person, error) {
	rows, err := a.q.PeopleByAgeAndLocale(ctx, manycolmanyv5.PeopleByAgeAndLocaleParams{
		MinAge: minAge,
		Locale: locale,
	})
	if err != nil {
		return nil, err
	}
	// Nil and empty are different :many answers; a scenario pins which one.
	if rows == nil {
		return nil, nil
	}
	out := make([]person, 0, len(rows))
	for _, row := range rows {
		out = append(out, person{Name: row.Name, Age: row.Age})
	}
	return out, nil
}

// neo4jV6 is the neo4j-go-v6 arm on the same image, isolated the same way as
// neo4jV5.
type neo4jV6 struct {
	driver     neo4jv6.Driver
	one        oneColOneParamOneV6
	mixed      mixedReadWriteBatchV6
	many       manyColManyV6
	nestedList nestedListV6
	deepList   deepNestedListV6
	entityNode entityNodeV6
	entityEdge entityEdgeV6
	anyValue   anyValueColumnsV6
	edgeUnion  edgeUnionV6
	timestamps timestampRoundtripV6
	temporals  temporalRoundtripV6
	builtLDT   localDateTimeColumnV6
	zonedTimes zonedTimeRoundtripV6
	maps       mapColumnV6
}

func startNeo4jV6(ctx context.Context, t *testing.T) harness {
	t.Helper()
	boltURI := startNeo4jContainer(ctx, t)

	driver, err := neo4jv6.NewDriver(boltURI, neo4jv6.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v6 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")

	return &neo4jV6{
		driver:     driver,
		one:        oneColOneParamOneV6{q: onecolonev6.New(driver)},
		mixed:      mixedReadWriteBatchV6{q: mixedv6.New(driver)},
		many:       manyColManyV6{q: manycolmanyv6.New(driver)},
		nestedList: nestedListV6{q: listlistv6.New(driver)},
		deepList:   deepNestedListV6{q: deeplistv6.New(driver)},
		entityNode: entityNodeV6{q: entitynodev6.New(driver)},
		entityEdge: entityEdgeV6{q: entityedgev6.New(driver)},
		anyValue:   anyValueColumnsV6{q: anypropv6.New(driver)},
		edgeUnion:  edgeUnionV6{q: edgeunionv6.New(driver)},
		timestamps: timestampRoundtripV6{q: tsv6.New(driver)},
		temporals:  temporalRoundtripV6{q: temporalv6.New(driver)},
		builtLDT:   localDateTimeColumnV6{q: ldtv6.New(driver)},
		zonedTimes: zonedTimeRoundtripV6{q: zonedv6.New(driver)},
		maps:       mapColumnV6{q: scalarmapv6.New(driver)},
	}
}

func (h *neo4jV6) parallelScenarios() bool { return false }

func (h *neo4jV6) scenario(ctx context.Context, t *testing.T) backend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV6) writeScenario(ctx context.Context, t *testing.T) writeBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV6) edgeUnionScenario(ctx context.Context, t *testing.T) edgeUnionBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV6) temporalScenario(ctx context.Context, t *testing.T) temporalBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV6) localDateTimeColumnScenario(ctx context.Context, t *testing.T) localDateTimeColumnBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV6) mapColumnScenario(ctx context.Context, t *testing.T) mapColumnBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV6) zonedTimeScenario(ctx context.Context, t *testing.T) zonedTimeBackend {
	t.Helper()
	return h.newScenario(ctx, t)
}

func (h *neo4jV6) newScenario(ctx context.Context, t *testing.T) neo4jV6Scenario {
	t.Helper()
	s := neo4jV6Scenario{arm: h}
	s.seed(ctx, t, wipeCypher)
	return s
}

// neo4jV6Scenario is one scenario's view of the neo4jV6 arm: the server's
// single graph, wiped as the scenario opened.
type neo4jV6Scenario struct {
	neo4jStoredTemporal
	arm *neo4jV6
}

func (s neo4jV6Scenario) seed(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	session := s.arm.driver.NewSession(ctx, neo4jv6.SessionConfig{AccessMode: neo4jv6.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close session: %v", err)
		}
	}()
	_, err := neo4jv6.ExecuteWrite(ctx, session, func(tx neo4jv6.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, cypher, nil)
		if err != nil {
			return nil, err
		}
		return result.Consume(ctx)
	})
	require.NoError(t, err, "seed: %s", cypher)
}

func (s neo4jV6Scenario) oneColOneParamOne() oneColOneParamOneQuerier { return s.arm.one }

func (s neo4jV6Scenario) mixedReadWriteBatch() mixedReadWriteBatchQuerier { return s.arm.mixed }

func (s neo4jV6Scenario) timestampRoundtrip() timestampRoundtripQuerier { return s.arm.timestamps }

func (s neo4jV6Scenario) tx() txQuerier { return s.arm.mixed }

// refuseBeginOnBoundHandle is the v5 method against the v6 module; see
// that one for why the refusal travels as the closure's result.
func (s neo4jV6Scenario) refuseBeginOnBoundHandle(ctx context.Context, t *testing.T) error {
	t.Helper()
	session := s.arm.driver.NewSession(ctx, neo4jv6.SessionConfig{AccessMode: neo4jv6.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the refusal ran in: %v", err)
		}
	}()

	refusal, err := neo4jv6.ExecuteWrite(ctx, session, func(mt neo4jv6.ManagedTransaction) (error, error) {
		nested, beginErr := s.arm.mixed.q.WithTx(mt).Begin(ctx)
		if beginErr != nil {
			return beginErr, nil
		}
		if rbErr := nested.Rollback(ctx); rbErr != nil {
			t.Logf("rollback the transaction Begin should have refused: %v", rbErr)
		}
		return nil, nil
	})
	require.NoError(t, err, "open the managed transaction the refusal is asked in")
	return refusal
}

// timestampRoundtripV6 binds the TIMESTAMP fixture. The driver carries a
// datetime natively, so the whole of this arm is the identity — which is
// what makes the comparison against the AGE arm worth running.
type timestampRoundtripV6 struct{ q *tsv6.Queries }

func (a timestampRoundtripV6) addEvent(ctx context.Context, id int64, occurredAt time.Time) error {
	return a.q.AddEvent(ctx, tsv6.AddEventParams{Id: id, OccurredAt: occurredAt})
}

func (a timestampRoundtripV6) eventsAfter(ctx context.Context, since time.Time) ([]int64, error) {
	return a.q.EventsAfter(ctx, since)
}

func (a timestampRoundtripV6) eventAt(ctx context.Context, id int64) (time.Time, error) {
	return a.q.EventAt(ctx, id)
}

func (a timestampRoundtripV6) oneEvent(ctx context.Context, id int64) (eventEntity, error) {
	e, err := a.q.OneEvent(ctx, id)
	if err != nil {
		return eventEntity{}, err
	}
	return eventEntity{ID: e.Id, OccurredAt: e.OccurredAt, SeenAt: e.SeenAt}, nil
}

func (s neo4jV6Scenario) temporalRoundtrip() temporalRoundtripQuerier { return s.arm.temporals }

func (s neo4jV6Scenario) seedSeenOn(ctx context.Context, t *testing.T, id int64, on dateValue) {
	t.Helper()
	s.seed(ctx, t, neo4jSeenOnCypher(id, on))
}

func (s neo4jV6Scenario) localDateTimeColumn() localDateTimeColumnQuerier { return s.arm.builtLDT }

func (s neo4jV6Scenario) mapColumn() mapColumnQuerier { return s.arm.maps }

func (s neo4jV6Scenario) zonedTimeRoundtrip() zonedTimeRoundtripQuerier { return s.arm.zonedTimes }

// temporalRoundtripV6 binds the zoneless temporal fixture on the v6
// driver, under the same field-copy rule as the v5 adapter.
type temporalRoundtripV6 struct{ q *temporalv6.Queries }

func (a temporalRoundtripV6) addReading(ctx context.Context, id int64, onDate dateValue, atLocal localTimeValue, elapsed durationValue) error {
	return a.q.AddReading(ctx, temporalv6.AddReadingParams{
		Id:      id,
		OnDate:  temporalv6.Date{Year: onDate.Year, Month: onDate.Month, Day: onDate.Day},
		AtLocal: temporalv6.LocalTime{Hour: atLocal.Hour, Minute: atLocal.Minute, Second: atLocal.Second, Nanosecond: atLocal.Nanosecond},
		Elapsed: temporalv6.Duration{Months: elapsed.Months, Days: elapsed.Days, Seconds: elapsed.Seconds, Nanos: elapsed.Nanos},
	})
}

func (a temporalRoundtripV6) readingsFrom(ctx context.Context, from dateValue) ([]int64, error) {
	return a.q.ReadingsFrom(ctx, temporalv6.Date{Year: from.Year, Month: from.Month, Day: from.Day})
}

func (a temporalRoundtripV6) readingsSeenFrom(ctx context.Context, seenFrom *dateValue) ([]int64, error) {
	if seenFrom == nil {
		return a.q.ReadingsSeenFrom(ctx, nil)
	}
	return a.q.ReadingsSeenFrom(ctx, &temporalv6.Date{Year: seenFrom.Year, Month: seenFrom.Month, Day: seenFrom.Day})
}

func (a temporalRoundtripV6) readingDate(ctx context.Context, id int64) (dateValue, error) {
	d, err := a.q.ReadingDate(ctx, id)
	if err != nil {
		return dateValue{}, err
	}
	return dateValue{Year: d.Year, Month: d.Month, Day: d.Day}, nil
}

func (a temporalRoundtripV6) readingLocalTime(ctx context.Context, id int64) (localTimeValue, error) {
	v, err := a.q.ReadingLocalTime(ctx, id)
	if err != nil {
		return localTimeValue{}, err
	}
	return localTimeValue{Hour: v.Hour, Minute: v.Minute, Second: v.Second, Nanosecond: v.Nanosecond}, nil
}

func (a temporalRoundtripV6) readingElapsed(ctx context.Context, id int64) (durationValue, error) {
	v, err := a.q.ReadingElapsed(ctx, id)
	if err != nil {
		return durationValue{}, err
	}
	return durationValue{Months: v.Months, Days: v.Days, Seconds: v.Seconds, Nanos: v.Nanos}, nil
}

func (a temporalRoundtripV6) oneReading(ctx context.Context, id int64) (readingEntity, error) {
	r, err := a.q.OneReading(ctx, id)
	if err != nil {
		return readingEntity{}, err
	}
	out := readingEntity{
		ID:      r.Id,
		OnDate:  dateValue{Year: r.OnDate.Year, Month: r.OnDate.Month, Day: r.OnDate.Day},
		AtLocal: localTimeValue{Hour: r.AtLocal.Hour, Minute: r.AtLocal.Minute, Second: r.AtLocal.Second, Nanosecond: r.AtLocal.Nanosecond},
		Elapsed: durationValue{Months: r.Elapsed.Months, Days: r.Elapsed.Days, Seconds: r.Elapsed.Seconds, Nanos: r.Elapsed.Nanos},
	}
	if r.SeenOn != nil {
		out.SeenOn = &dateValue{Year: r.SeenOn.Year, Month: r.SeenOn.Month, Day: r.SeenOn.Day}
	}
	return out, nil
}

func (a temporalRoundtripV6) errNoRows() error { return temporalv6.ErrNoRows }

// localDateTimeColumnV6 binds the constructed-LOCALDATETIME fixture on the
// v6 driver.
type localDateTimeColumnV6 struct{ q *ldtv6.Queries }

func (a localDateTimeColumnV6) builtLocalDateTime(ctx context.Context) (localDateTimeValue, error) {
	v, err := a.q.BuiltLocalDateTime(ctx)
	if err != nil {
		return localDateTimeValue{}, err
	}
	return localDateTimeValue{
		Year: v.Year, Month: v.Month, Day: v.Day,
		Hour: v.Hour, Minute: v.Minute, Second: v.Second, Nanosecond: v.Nanosecond,
	}, nil
}

// mapColumnV6 binds the map-column fixture on the v6 driver, forwarding
// unchanged for the reason the v5 adapter gives.
type mapColumnV6 struct{ q *scalarmapv6.Queries }

func (a mapColumnV6) oneMap(ctx context.Context) (map[string]any, error) { return a.q.OneMap(ctx) }

// zonedTimeRoundtripV6 binds the zoned temporal fixture on the v6 driver.
type zonedTimeRoundtripV6 struct{ q *zonedv6.Queries }

func (a zonedTimeRoundtripV6) addSlot(ctx context.Context, id int64, startsAt timeValue) error {
	return a.q.AddSlot(ctx, zonedv6.AddSlotParams{Id: id, StartsAt: zonedTimeV6(startsAt)})
}

func (a zonedTimeRoundtripV6) slotsFrom(ctx context.Context, from timeValue) ([]int64, error) {
	return a.q.SlotsFrom(ctx, zonedTimeV6(from))
}

func (a zonedTimeRoundtripV6) slotStart(ctx context.Context, id int64) (timeValue, error) {
	v, err := a.q.SlotStart(ctx, id)
	if err != nil {
		return timeValue{}, err
	}
	return batteryTimeV6(v), nil
}

func (a zonedTimeRoundtripV6) oneSlot(ctx context.Context, id int64) (slotEntity, error) {
	s, err := a.q.OneSlot(ctx, id)
	if err != nil {
		return slotEntity{}, err
	}
	return slotEntity{ID: s.Id, StartsAt: batteryTimeV6(s.StartsAt)}, nil
}

func (a zonedTimeRoundtripV6) errNoRows() error { return zonedv6.ErrNoRows }

func zonedTimeV6(v timeValue) zonedv6.Time {
	return zonedv6.Time{Hour: v.Hour, Minute: v.Minute, Second: v.Second, Nanosecond: v.Nanosecond, OffsetSeconds: v.OffsetSeconds}
}

func batteryTimeV6(v zonedv6.Time) timeValue {
	return timeValue{Hour: v.Hour, Minute: v.Minute, Second: v.Second, Nanosecond: v.Nanosecond, OffsetSeconds: v.OffsetSeconds}
}

func (s neo4jV6Scenario) manyColMany() manyColManyQuerier { return s.arm.many }

func (s neo4jV6Scenario) nestedList() nestedListQuerier { return s.arm.nestedList }

func (s neo4jV6Scenario) deepNestedList() deepNestedListQuerier { return s.arm.deepList }

func (s neo4jV6Scenario) entityNodeProjectedOne() entityNodeQuerier { return s.arm.entityNode }

func (s neo4jV6Scenario) entityEdgeProjectedOne() entityEdgeQuerier { return s.arm.entityEdge }

func (s neo4jV6Scenario) anyValueColumns() anyValueColumnQuerier { return s.arm.anyValue }

// anyValueColumnsV6 binds the ANY VALUE column fixture, passing both columns
// through untouched for the reason its AGE twin gives.
type anyValueColumnsV6 struct{ q *anypropv6.Queries }

func (a anyValueColumnsV6) eventMarker(ctx context.Context) (any, error) {
	return a.q.EventMarker(ctx)
}

func (a anyValueColumnsV6) eventPayload(ctx context.Context) (*any, error) {
	return a.q.EventPayload(ctx)
}

func (a anyValueColumnsV6) errNoRows() error { return anypropv6.ErrNoRows }

func (s neo4jV6Scenario) edgeUnionUndeclared() edgeUnionQuerier { return s.arm.edgeUnion }

type edgeUnionV6 struct{ q *edgeunionv6.Queries }

// actionOnPost is edgeUnionV5.actionOnPost against the v6 target's own sum.
func (a edgeUnionV6) actionOnPost(ctx context.Context, postID int64) (edgeUnionAction, error) {
	got, err := a.q.ActionOnPost(ctx, postID)
	if err != nil {
		return edgeUnionAction{}, err
	}
	switch v := got.(type) {
	case edgeunionv6.Authored:
		return edgeUnionAction{Kind: "AUTHORED", Since: v.Since}, nil
	case edgeunionv6.Likes:
		return edgeUnionAction{Kind: "LIKES", Rating: v.Rating}, nil
	default:
		return edgeUnionAction{}, fmt.Errorf("battery: %T is not a member this scenario knows", v)
	}
}

func (a edgeUnionV6) errNoRows() error { return edgeunionv6.ErrNoRows }

type entityNodeV6 struct{ q *entitynodev6.Queries }

func (a entityNodeV6) onePerson(ctx context.Context) (personEntity, error) {
	p, err := a.q.OnePerson(ctx)
	if err != nil {
		return personEntity{}, err
	}
	return personEntity{ID: p.Id, MiddleName: p.MiddleName, Name: p.Name}, nil
}

func (a entityNodeV6) errNoRows() error { return entitynodev6.ErrNoRows }

type entityEdgeV6 struct{ q *entityedgev6.Queries }

func (a entityEdgeV6) oneActedIn(ctx context.Context) (actedInEntity, error) {
	r, err := a.q.OneActedIn(ctx)
	if err != nil {
		return actedInEntity{}, err
	}
	return actedInEntity{Since: r.Since}, nil
}

func (a entityEdgeV6) errNoRows() error { return entityedgev6.ErrNoRows }

type oneColOneParamOneV6 struct{ q *onecolonev6.Queries }

func (a oneColOneParamOneV6) personName(ctx context.Context, id int64) (string, error) {
	return a.q.PersonName(ctx, id)
}

func (a oneColOneParamOneV6) errNoRows() error { return onecolonev6.ErrNoRows }

func (a oneColOneParamOneV6) errMultipleResults() error { return onecolonev6.ErrMultipleResults }

type mixedReadWriteBatchV6 struct{ q *mixedv6.Queries }

func (a mixedReadWriteBatchV6) getPersonName(ctx context.Context, id int64) (string, error) {
	return a.q.GetPersonName(ctx, id)
}

func (a mixedReadWriteBatchV6) removePerson(ctx context.Context, id int64) error {
	return a.q.RemovePerson(ctx, id)
}

func (a mixedReadWriteBatchV6) errNoRows() error { return mixedv6.ErrNoRows }

func (a mixedReadWriteBatchV6) errTxDone() error { return mixedv6.ErrTxDone }

func (a mixedReadWriteBatchV6) begin(ctx context.Context) (liveTx, error) {
	tx, err := a.q.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return mixedTxV6{tx: tx}, nil
}

// mixedTxV6 is mixedTxV5 against the v6 module. The emission the two run
// is byte-identical but for the session type, which the corpus equality
// row holds; this pair is here because each arm imports its own package.
type mixedTxV6 struct{ tx *mixedv6.Tx }

func (a mixedTxV6) commit(ctx context.Context) error { return a.tx.Commit(ctx) }

func (a mixedTxV6) rollback(ctx context.Context) error { return a.tx.Rollback(ctx) }

func (a mixedTxV6) removePerson(ctx context.Context, id int64) error {
	return a.tx.RemovePerson(ctx, id)
}

func (a mixedTxV6) getPersonName(ctx context.Context, id int64) (string, error) {
	return a.tx.GetPersonName(ctx, id)
}

type nestedListV6 struct{ q *listlistv6.Queries }

func (a nestedListV6) nestedList(ctx context.Context) ([][]int64, error) {
	return a.q.NestedList(ctx)
}

type deepNestedListV6 struct{ q *deeplistv6.Queries }

func (a deepNestedListV6) deepNestedList(ctx context.Context) ([][][]int64, error) {
	return a.q.DeepNestedList(ctx)
}

type manyColManyV6 struct{ q *manycolmanyv6.Queries }

func (a manyColManyV6) peopleByAgeAndLocale(ctx context.Context, minAge int64, locale string) ([]person, error) {
	rows, err := a.q.PeopleByAgeAndLocale(ctx, manycolmanyv6.PeopleByAgeAndLocaleParams{
		MinAge: minAge,
		Locale: locale,
	})
	if err != nil {
		return nil, err
	}
	// Nil and empty are different :many answers; a scenario pins which one.
	if rows == nil {
		return nil, nil
	}
	out := make([]person, 0, len(rows))
	for _, row := range rows {
		out = append(out, person{Name: row.Name, Age: row.Age})
	}
	return out, nil
}
