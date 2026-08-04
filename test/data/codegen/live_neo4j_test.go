//go:build codegen_live

// The neo4j arms: the container helper both driver majors share, and one
// adapter per major. The battery they serve is in live_test.go.

package fixtures_test

import (
	"context"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"

	entityedgev5 "github.com/areqag/gqlc/test/data/codegen/valid/entity_edge_projected_one/golden/neo4j-go-v5"
	entityedgev6 "github.com/areqag/gqlc/test/data/codegen/valid/entity_edge_projected_one/golden/neo4j-go-v6"
	entitynodev5 "github.com/areqag/gqlc/test/data/codegen/valid/entity_node_projected_one/golden/neo4j-go-v5"
	entitynodev6 "github.com/areqag/gqlc/test/data/codegen/valid/entity_node_projected_one/golden/neo4j-go-v6"
	manycolmanyv5 "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many/golden/neo4j-go-v5"
	manycolmanyv6 "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many/golden/neo4j-go-v6"
	mixedv5 "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden/neo4j-go-v5"
	mixedv6 "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden/neo4j-go-v6"
	onecolonev5 "github.com/areqag/gqlc/test/data/codegen/valid/one_col_one_param_one/golden/neo4j-go-v5"
	onecolonev6 "github.com/areqag/gqlc/test/data/codegen/valid/one_col_one_param_one/golden/neo4j-go-v6"
	tsv5 "github.com/areqag/gqlc/test/data/codegen/valid/timestamp_property_roundtrip/golden/neo4j-go-v5"
	tsv6 "github.com/areqag/gqlc/test/data/codegen/valid/timestamp_property_roundtrip/golden/neo4j-go-v6"
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
	entityNode entityNodeV5
	entityEdge entityEdgeV5
	timestamps timestampRoundtripV5
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
		entityNode: entityNodeV5{q: entitynodev5.New(driver)},
		entityEdge: entityEdgeV5{q: entityedgev5.New(driver)},
		timestamps: timestampRoundtripV5{q: tsv5.New(driver)},
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

func (h *neo4jV5) newScenario(ctx context.Context, t *testing.T) neo4jV5Scenario {
	t.Helper()
	s := neo4jV5Scenario{arm: h}
	s.seed(ctx, t, wipeCypher)
	return s
}

// neo4jV5Scenario is one scenario's view of the neo4jV5 arm: the server's
// single graph, wiped as the scenario opened.
type neo4jV5Scenario struct{ arm *neo4jV5 }

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

func (s neo4jV5Scenario) manyColMany() manyColManyQuerier { return s.arm.many }

func (s neo4jV5Scenario) entityNodeProjectedOne() entityNodeQuerier { return s.arm.entityNode }

func (s neo4jV5Scenario) entityEdgeProjectedOne() entityEdgeQuerier { return s.arm.entityEdge }

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
	entityNode entityNodeV6
	entityEdge entityEdgeV6
	timestamps timestampRoundtripV6
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
		entityNode: entityNodeV6{q: entitynodev6.New(driver)},
		entityEdge: entityEdgeV6{q: entityedgev6.New(driver)},
		timestamps: timestampRoundtripV6{q: tsv6.New(driver)},
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

func (h *neo4jV6) newScenario(ctx context.Context, t *testing.T) neo4jV6Scenario {
	t.Helper()
	s := neo4jV6Scenario{arm: h}
	s.seed(ctx, t, wipeCypher)
	return s
}

// neo4jV6Scenario is one scenario's view of the neo4jV6 arm: the server's
// single graph, wiped as the scenario opened.
type neo4jV6Scenario struct{ arm *neo4jV6 }

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

func (s neo4jV6Scenario) manyColMany() manyColManyQuerier { return s.arm.many }

func (s neo4jV6Scenario) entityNodeProjectedOne() entityNodeQuerier { return s.arm.entityNode }

func (s neo4jV6Scenario) entityEdgeProjectedOne() entityEdgeQuerier { return s.arm.entityEdge }

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
