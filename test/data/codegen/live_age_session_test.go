//go:build codegen_live

// The emitted AGE session init under the deployments it exists to refuse. The
// battery in live_test.go only ever drives sessions it accepts, so what a
// happy-path run establishes is that the hook is harmless, not that it
// discriminates (gqlc-35yu.6, gqlc-35yu.8).

package fixtures

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	manycolmanyage "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many/golden/apache-age-pgx-v5"
	ageskeleton "github.com/areqag/gqlc/test/data/codegen/valid/skeleton/golden/apache-age-pgx-v5"
)

const (
	// ageNoGrantRole is a plain LOGIN role holding none of ageAppGrants: a
	// deployment that got as far as creating the application role and stopped.
	ageNoGrantRole     = "gqlc_nogrant"
	ageNoGrantPassword = "gqlctest3"

	// ageNoExtensionDB is a database in the same cluster with no AGE in it: a
	// deployment pointed at the wrong database, which is indistinguishable from
	// the right one until something asks for ag_catalog.
	ageNoExtensionDB = "gqlc_no_extension"

	// ageNoOperatorDB has AGE installed, granted and reachable, and is missing
	// exactly one thing: the agtype addition operator. It is the narrowest
	// breakage the canary is supposed to notice, and the only fixture here that
	// a probe naming ag_catalog for its casts alone still passes.
	ageNoOperatorDB = "gqlc_no_operator"
)

// ageSessionInits are the SessionInit copies belonging to the targets this arm
// mixes: it binds one of them to pools that hand out handles generated into the
// others, which is sound only while they issue the same statements. That every
// AGE package carries an identical copy is a property of the emitted tree and
// is claimed under bd gqlc-p6mh.
var ageSessionInits = []struct {
	target string
	init   func(context.Context, *pgx.Conn) error
}{
	{target: "many_col_many", init: manycolmanyage.SessionInit},
	{target: "skeleton", init: ageskeleton.SessionInit},
}

// createAGESessionFixtures provisions the broken deployments. Each is a
// separate role or database, so one arm's cluster serves them all concurrently.
func createAGESessionFixtures(ctx context.Context, t *testing.T, endpoint string) {
	t.Helper()
	ageExecAsSuperuser(ctx, t, endpoint, ageDatabase,
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", ageNoGrantRole, ageNoGrantPassword),
		"CREATE DATABASE "+ageNoExtensionDB,
		"CREATE DATABASE "+ageNoOperatorDB,
	)
	ageExecAsSuperuser(ctx, t, endpoint, ageNoOperatorDB,
		"CREATE EXTENSION age",
		// DROP OPERATOR refuses a member of an extension, so the operator is
		// released from age's membership before it can be removed.
		"ALTER EXTENSION age DROP OPERATOR ag_catalog.+ (ag_catalog.agtype, ag_catalog.agtype)",
		"DROP OPERATOR ag_catalog.+ (ag_catalog.agtype, ag_catalog.agtype)",
		"GRANT USAGE ON SCHEMA ag_catalog TO "+ageAppRole,
	)
}

// statementRecorder is a pgx.QueryTracer that keeps the SQL a connection is
// sent, in order. It is how a test asserts on the statements SessionInit
// issues without SessionInit having to report them, so an assertion is against
// the shipped text rather than a copy of it that can drift.
type statementRecorder struct {
	mu  sync.Mutex
	sql []string
}

func (r *statementRecorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sql = append(r.sql, data.SQL)
	return ctx
}

func (r *statementRecorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (r *statementRecorder) statements() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sql...)
}

// refuses acquires one connection and reports the error the pool refused it
// with. An acquired resource keeps pgxpool.Close from returning, and Close is
// registered as a cleanup, so a connection the pool unexpectedly hands out is
// released before the caller's assertion fails and takes that cleanup with it.
func refuses(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err == nil {
		conn.Release()
	}
	return err
}

// captureSessionInit runs init on a healthy connection and returns the
// statements it issued.
func captureSessionInit(ctx context.Context, t *testing.T, dsn string, init func(context.Context, *pgx.Conn) error) []string {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	require.NoError(t, err, "parse connection config")
	rec := &statementRecorder{}
	cfg.Tracer = rec
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err, "connect for capture")
	defer func() {
		if err := conn.Close(ctx); err != nil {
			t.Logf("close capture connection: %v", err)
		}
	}()
	require.NoError(t, init(ctx, conn), "session init on a healthy connection")
	return rec.statements()
}

// weakSessionInit builds an AfterConnect hook that prepares a session exactly
// as the shipped one does — searchPath is the shipped hook's own statement,
// captured from it — and then probes with a comparison of boolean literals.
// Both probes name ag_catalog for their casts; the shipped one additionally
// puts an AGE operator between the literals and the answer. It is the control
// the shipped canary is measured against.
func weakSessionInit(searchPath string) func(context.Context, *pgx.Conn) error {
	return func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, searchPath); err != nil {
			return fmt.Errorf("weak probe: search_path: %w", err)
		}
		var ok bool
		if err := conn.QueryRow(ctx, weakCanary).Scan(&ok); err != nil {
			return fmt.Errorf("weak probe: canary: %w", err)
		}
		if !ok {
			return errors.New("weak probe: canary returned false")
		}
		return nil
	}
}

// weakCanary reaches its answer through agtype's own equality, which
// PostgreSQL resolves without ag_catalog by casting both sides to boolean.
const weakCanary = "SELECT 'true'::ag_catalog.agtype = 'true'::ag_catalog.agtype"

// intEqualityCanary is the same comparison over integer literals. agtype has no
// implicit cast to a numeric type, so with ag_catalog off the search_path the
// cast PostgreSQL falls back to is the one to boolean, and that is what fails.
const intEqualityCanary = "SELECT '1'::ag_catalog.agtype = '1'::ag_catalog.agtype"

// TestAGESessionInit drives the emitted SessionInit against deployments it has
// to refuse. Each case is a role or database that a `SELECT 1` connects to
// perfectly well, so nothing short of asking AGE a question distinguishes it
// from a working one.
func TestAGESessionInit(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	createAGESessionFixtures(ctx, t, endpoint)
	healthyDSN := ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase)

	// Everything below asserts against these two strings rather than against
	// copies of them, so a change to what SessionInit issues is a change to
	// what is under test here.
	shipped := captureSessionInit(ctx, t, healthyDSN, ageSessionInit)
	require.Len(t, shipped, 2, "SessionInit prepares the search_path and then probes it")
	searchPath, canary := shipped[0], shipped[1]

	t.Run("a session it cannot serve is refused at pool acquisition", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name     string
			dsn      string
			sqlstate string
		}{
			{
				name:     "role without USAGE on ag_catalog",
				dsn:      ageDSN(endpoint, ageNoGrantRole, ageNoGrantPassword, ageDatabase),
				sqlstate: "42501", // insufficient_privilege
			},
			{
				name:     "database without the age extension",
				dsn:      ageDSN(endpoint, ageAppRole, ageAppPassword, ageNoExtensionDB),
				sqlstate: "3F000", // invalid_schema_name
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				// A connection that skips the hook opens, so what the pool
				// goes on to refuse is the session and not the credentials,
				// the host or the database.
				conn, err := pgx.Connect(ctx, tc.dsn)
				require.NoError(t, err, "connect without session init")
				require.NoError(t, conn.Close(ctx), "close control connection")

				// Constructing the pool succeeds: pgxpool opens nothing until
				// asked, so the refusal has to be observed at acquisition or
				// it is not observed until the first query.
				pool := openAGEPool(ctx, t, tc.dsn, ageSessionInit)
				err = refuses(ctx, pool)
				require.Error(t, err, "acquisition must fail where AGE is unusable")
				require.ErrorContains(t, err, "gqlc: AGE operator canary",
					"the canary is the statement that must have failed")

				var pgErr *pgconn.PgError
				require.ErrorAs(t, err, &pgErr, "the server's error must survive the wrap")
				require.Equal(t, tc.sqlstate, pgErr.Code)

				require.Zero(t, pool.Stat().TotalConns(), "a refused session must not enter the pool")
			})
		}
	})

	t.Run("the shipped canary refuses a session a weaker probe admits", func(t *testing.T) {
		t.Parallel()
		// AGE is installed, granted and on the path in this database; the one
		// thing missing is the operator. Both hooks run the same preparation,
		// so the probe is the only thing they can disagree about.
		dsn := ageDSN(endpoint, ageAppRole, ageAppPassword, ageNoOperatorDB)

		weak := openAGEPool(ctx, t, dsn, weakSessionInit(searchPath))
		conn, err := weak.Acquire(ctx)
		require.NoError(t, err, "a probe needing no AGE operator admits this session")
		conn.Release()

		err = refuses(ctx, openAGEPool(ctx, t, dsn, ageSessionInit))
		require.Error(t, err, "the shipped canary must refuse the session the weaker probe admitted")
		require.ErrorContains(t, err, "gqlc: AGE operator canary")
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "42883", pgErr.Code, "undefined_function: operator resolution is what fails")
	})

	t.Run("off the search_path only the shipped canary fails on operator resolution", func(t *testing.T) {
		t.Parallel()
		// SessionInit's first statement puts ag_catalog on the path
		// unconditionally, so the state the canary guards against is
		// unreachable through SessionInit itself. This is that state, built
		// directly, with the shipped canary replayed on it.
		conn, err := pgx.Connect(ctx, healthyDSN)
		require.NoError(t, err, "connect for replay")
		t.Cleanup(func() {
			if err := conn.Close(ctx); err != nil {
				t.Logf("close replay connection: %v", err)
			}
		})
		_, err = conn.Exec(ctx, "SET search_path = pg_catalog, public")
		require.NoError(t, err, "take ag_catalog off the search_path")
		var path string
		require.NoError(t, conn.QueryRow(ctx, "SELECT current_setting('search_path')").Scan(&path))
		require.NotContains(t, path, "ag_catalog", "the replay must run off the search_path")

		for _, tc := range []struct {
			name string
			// probe answers when the statement resolves; sqlstate is empty in
			// that case and holds the server's code otherwise.
			probe    string
			sqlstate string
			answers  bool
		}{
			{name: "the shipped canary", probe: canary, sqlstate: "42883"},
			{name: "boolean literals under equality", probe: weakCanary, answers: true},
			{name: "integer literals under equality", probe: intEqualityCanary, sqlstate: "22023"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var ok bool
				err := conn.QueryRow(ctx, tc.probe).Scan(&ok)
				if tc.sqlstate == "" {
					require.NoError(t, err, "this probe resolves without ag_catalog on the path")
					require.Equal(t, tc.answers, ok, "and answers, so a hook built on it admits the session")
					return
				}
				require.Error(t, err)
				var pgErr *pgconn.PgError
				require.ErrorAs(t, err, &pgErr)
				require.Equal(t, tc.sqlstate, pgErr.Code)
			})
		}
	})

	t.Run("the targets this arm binds together prepare a session identically", func(t *testing.T) {
		t.Parallel()
		for _, target := range ageSessionInits {
			t.Run(target.target, func(t *testing.T) {
				t.Parallel()
				require.Equal(t, shipped, captureSessionInit(ctx, t, healthyDSN, target.init))
			})
		}
	})
}
