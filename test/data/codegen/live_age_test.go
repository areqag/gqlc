//go:build codegen_live

// The Apache AGE arm: the container helper, the least-privilege role every
// pool in this package logs in as, and the adapter that binds the generated
// handles to a graph of their own. The battery it serves is in live_test.go.

package fixtures

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	entityedgeage "github.com/areqag/gqlc/test/data/codegen/valid/entity_edge_projected_one/golden/apache-age-pgx-v5"
	entitynodeage "github.com/areqag/gqlc/test/data/codegen/valid/entity_node_projected_one/golden/apache-age-pgx-v5"
	manycolmanyage "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many/golden/apache-age-pgx-v5"
	onecoloneage "github.com/areqag/gqlc/test/data/codegen/valid/one_col_one_param_one/golden/apache-age-pgx-v5"
)

const (
	// Pinned by digest, not tag, so an upstream rebuild of apache/age:latest
	// cannot serve a stale cached PASS from Go's test cache (bd gqlc-9u5).
	// Refresh with: docker buildx imagetools inspect apache/age:latest
	ageImage = "apache/age@sha256:4241e2d8bb86a6b2ea44e9ad06c73856e12b209de295124603a599dd7feb70eb"

	// ageSuperuser owns the cluster and creates the roles and databases the
	// tests need. Nothing under test connects as it.
	ageSuperuser     = "postgres"
	ageSuperPassword = "gqlctest1"
	ageDatabase      = "postgres"

	// ageAppRole is what a generated client is expected to be deployed as: a
	// plain LOGIN role with no SUPERUSER, holding only the privileges listed
	// in ageAppGrants. Every pool the battery drives logs in as it, so a
	// statement the emitted code gains that needs more than those privileges
	// fails the arm instead of passing on the superuser's blanket rights (bd
	// gqlc-35yu.6: a superuser-only LOAD reached review because the harness
	// connected as postgres).
	ageAppRole     = "gqlc_app"
	ageAppPassword = "gqlctest2"
)

// ageAppGrants is the privilege set a generated AGE client needs, one grant
// per emitted requirement: every statement names ag_catalog, so the schema
// must be usable; EnsureGraph and DropGraph read ag_graph to decide whether
// their work is already done; create_graph builds a schema, which is a
// privilege on the database. AGE grants none of these to PUBLIC.
var ageAppGrants = []string{
	"GRANT USAGE ON SCHEMA ag_catalog TO " + ageAppRole,
	"GRANT SELECT ON ag_catalog.ag_graph TO " + ageAppRole,
	"GRANT CREATE ON DATABASE " + ageDatabase + " TO " + ageAppRole,
}

// ageSessionInit is the emitted pgxpool.AfterConnect hook the pools in this
// package run. Every AGE target emits its own copy into its own package;
// TestAGESessionInit holds those copies to issuing the same statements, which
// is what lets one pool serve handles from several of them.
var ageSessionInit = onecoloneage.SessionInit

// startAGEContainer boots one apache/age container and returns its host:port.
// Cleanup is registered on t; the caller does not terminate.
//
// The image's own command is left alone: it preloads the extension library,
// which nothing here relies on — SessionInit's canary is an AGE C function
// and loads the library itself (bd gqlc-35yu.6).
func startAGEContainer(ctx context.Context, t *testing.T) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        ageImage,
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     ageSuperuser,
				"POSTGRES_PASSWORD": ageSuperPassword,
				"POSTGRES_DB":       ageDatabase,
			},
			WaitingFor: wait.ForAll(
				// The entrypoint starts the server once to run the image's
				// init scripts and again to serve; the first announcement
				// belongs to a server that only listens on a unix socket.
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
				wait.ForListeningPort("5432/tcp"),
			),
		},
		Started: true,
	})
	require.NoError(t, err, "start apache/age testcontainer")
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate apache/age: %v", err)
		}
	})
	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err, "read postgres endpoint")
	return endpoint
}

// ageDSN addresses one database on the container as one role.
func ageDSN(endpoint, role, password, database string) string {
	return fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", role, password, endpoint, database)
}

// ageExecAsSuperuser runs bootstrap DDL the app role is deliberately not
// entitled to. The connection closes before anything under test opens.
func ageExecAsSuperuser(ctx context.Context, t *testing.T, endpoint, database string, stmts ...string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, ageDSN(endpoint, ageSuperuser, ageSuperPassword, database))
	require.NoError(t, err, "connect as %s to %s", ageSuperuser, database)
	defer func() {
		if err := conn.Close(ctx); err != nil {
			t.Logf("close bootstrap connection: %v", err)
		}
	}()
	for _, stmt := range stmts {
		_, err := conn.Exec(ctx, stmt)
		require.NoError(t, err, "bootstrap: %s", stmt)
	}
}

// createAGEAppRole provisions ageAppRole with ageAppGrants and nothing else.
func createAGEAppRole(ctx context.Context, t *testing.T, endpoint string) {
	t.Helper()
	ageExecAsSuperuser(ctx, t, endpoint, ageDatabase,
		append([]string{fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", ageAppRole, ageAppPassword)},
			ageAppGrants...)...)
}

// openAGEPool builds a pool that runs init on every session it opens, which is
// the precondition New documents for the DBTX it takes. Construction is the
// only step asserted here: a session init that refuses a connection surfaces at
// acquisition, and several callers are there to assert exactly that.
func openAGEPool(ctx context.Context, t *testing.T, dsn string, init func(context.Context, *pgx.Conn) error) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err, "parse pool config")
	cfg.AfterConnect = init
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err, "construct pgx pool")
	t.Cleanup(pool.Close)
	return pool
}

// ageArm is the apache-age-pgx-v5 arm: one container, one pool logged in as
// ageAppRole, and a graph per scenario. AGE addresses graphs by name, so
// isolation is a name no other scenario holds rather than a wipe, and
// scenarios run concurrently.
type ageArm struct {
	pool   *pgxpool.Pool
	graphs atomic.Uint64
}

func startAGE(ctx context.Context, t *testing.T) harness {
	t.Helper()
	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	dsn := ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase)
	return &ageArm{pool: openAGEPool(ctx, t, dsn, ageSessionInit)}
}

func (h *ageArm) parallelScenarios() bool { return true }

func (h *ageArm) scenario(ctx context.Context, t *testing.T) backend {
	t.Helper()
	graph := fmt.Sprintf("gqlc_live_%d", h.graphs.Add(1))
	t.Logf("scenario graph: %s", graph)

	s := ageScenario{
		pool:       h.pool,
		graph:      graph,
		one:        oneColOneParamOneAGE{q: onecoloneage.New(h.pool, graph)},
		many:       manyColManyAGE{q: manycolmanyage.New(h.pool, graph)},
		entityNode: entityNodeAGE{q: entitynodeage.New(h.pool, graph)},
		entityEdge: entityEdgeAGE{q: entityedgeage.New(h.pool, graph)},
	}
	// Created through one package's helper and dropped through another's:
	// each target emits its own lifecycle pair, and both handles have to
	// reach the same graph for the scenario to see its own seed.
	require.NoError(t, s.many.q.EnsureGraph(ctx), "ensure graph %s", graph)
	t.Cleanup(func() {
		require.NoError(t, s.one.q.DropGraph(ctx), "drop graph %s", graph)
	})
	return s
}

// ageScenario is one scenario's view of the ageArm: a graph of its own, and
// the generated handles bound to it.
type ageScenario struct {
	pool       *pgxpool.Pool
	graph      string
	one        oneColOneParamOneAGE
	many       manyColManyAGE
	entityNode entityNodeAGE
	entityEdge entityEdgeAGE
}

func (s ageScenario) seed(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	// cypher() resolves its graph argument at parse analysis and takes a
	// constant there, so the name is composed into the statement. It is the
	// harness's own name, formatted from a counter.
	stmt := "SELECT * FROM ag_catalog.cypher('" + s.graph + "', $seed$" + cypher + "$seed$) AS (v ag_catalog.agtype)"
	_, err := s.pool.Exec(ctx, stmt)
	require.NoError(t, err, "seed: %s", cypher)
}

func (s ageScenario) oneColOneParamOne() oneColOneParamOneQuerier { return s.one }

func (s ageScenario) manyColMany() manyColManyQuerier { return s.many }

func (s ageScenario) entityNodeProjectedOne() entityNodeQuerier { return s.entityNode }

func (s ageScenario) entityEdgeProjectedOne() entityEdgeQuerier { return s.entityEdge }

type oneColOneParamOneAGE struct{ q *onecoloneage.Queries }

func (a oneColOneParamOneAGE) personName(ctx context.Context, id int64) (string, error) {
	return a.q.PersonName(ctx, id)
}

func (a oneColOneParamOneAGE) errNoRows() error { return onecoloneage.ErrNoRows }

func (a oneColOneParamOneAGE) errMultipleResults() error { return onecoloneage.ErrMultipleResults }

type entityNodeAGE struct{ q *entitynodeage.Queries }

func (a entityNodeAGE) onePerson(ctx context.Context) (personEntity, error) {
	p, err := a.q.OnePerson(ctx)
	if err != nil {
		return personEntity{}, err
	}
	return personEntity{ID: p.Id, MiddleName: p.MiddleName, Name: p.Name}, nil
}

func (a entityNodeAGE) errNoRows() error { return entitynodeage.ErrNoRows }

type entityEdgeAGE struct{ q *entityedgeage.Queries }

func (a entityEdgeAGE) oneActedIn(ctx context.Context) (actedInEntity, error) {
	r, err := a.q.OneActedIn(ctx)
	if err != nil {
		return actedInEntity{}, err
	}
	return actedInEntity{Since: r.Since}, nil
}

func (a entityEdgeAGE) errNoRows() error { return entityedgeage.ErrNoRows }

type manyColManyAGE struct{ q *manycolmanyage.Queries }

func (a manyColManyAGE) peopleByAgeAndLocale(ctx context.Context, minAge int64, locale string) ([]person, error) {
	rows, err := a.q.PeopleByAgeAndLocale(ctx, manycolmanyage.PeopleByAgeAndLocaleParams{
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
