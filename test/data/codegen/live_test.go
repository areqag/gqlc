//go:build codegen_live

// Live smoke test for generated repositories against real neo4j drivers
// (gqlc-73h, gqlc-5gc). Opt-in via -tags codegen_live so PR CI stays fast;
// the manual / nightly CI job runs it. Lives in the nested
// test/data/codegen module so testcontainers and its ~50 transitive deps
// stay out of gqlc's root go.mod and the compiler binary.
//
// Two top-level arms — TestLiveSmokeV5 and TestLiveSmokeV6 — exercise the
// same neo4j:5-community image against their respective driver majors
// (v5 keeps the DriverWithContext surface; v6 renamed the interface to
// Driver and the constructor to NewDriver). Both call t.Parallel() so
// go test runs them concurrently: two containers, ~4GB peak, well within
// a standard CI runner. Within each arm the two golden fixtures share
// the arm's container and DETACH-wipe between sub-tests, so container
// startup is amortised across both sub-tests.
package fixtures

import (
	"context"
	"os"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"

	manycolmanyv5 "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many/golden"
	manycolmanyv6 "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many_v6/golden"
	mixedv5 "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden"
	mixedv6 "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch_v6/golden"
)

const (
	neo4jImage    = "neo4j:5-community"
	neo4jPassword = "gqlctest1"
	// wipeCypher DETACHes so any prior sub-test's leftover edges are removed
	// alongside its nodes; DELETE on an empty graph is a no-op in neo4j 5.
	wipeCypher = "MATCH (n) DETACH DELETE n"
)

// startContainer boots one neo4j:5-community container and returns its
// bolt URI. Cleanup is registered on t; the caller does not terminate.
// Shared between the v5 and v6 arms because testcontainers is driver-
// version-agnostic — a single helper avoids drift between the two
// arms' container setup.
func startContainer(ctx context.Context, t *testing.T) string {
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

// TestLiveSmokeV5 runs the generated repositories' :one, :many, and
// :exec paths against the v5 driver. A single container amortises the
// ~15s startup across every sub-test. Skips when GQLC_SKIP_LIVE is set
// so a developer without docker can still run
// `go test -tags codegen_live ./...` without a hard failure.
func TestLiveSmokeV5(t *testing.T) {
	t.Parallel()
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping neo4j testcontainer")
	}
	// A single top-level timeout keeps a stuck container from hanging the
	// whole test binary indefinitely. Neo4j 5-community typically starts in
	// <15s; 120s covers a cold image pull on a slow runner.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	boltURI := startContainer(ctx, t)

	driver, err := neo4jv5.NewDriverWithContext(boltURI, neo4jv5.BasicAuth("neo4j", neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v5 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")

	// Sub-test bodies are inlined so require-failures point at the assertion,
	// not at a helper wrapper. Each sub-test wipes the graph on entry.

	t.Run("mixed_read_write_batch: one + exec", func(t *testing.T) {
		seedV5(ctx, t, driver, wipeCypher)
		q := mixedv5.New(driver)

		// ErrNoRows on empty graph — errors.Is (via require.ErrorIs) confirms
		// the sentinel is identity-matchable so callers can branch generically.
		_, err := q.GetPersonName(ctx, 1)
		require.ErrorIs(t, err, mixedv5.ErrNoRows, "empty graph must return ErrNoRows")

		seedV5(ctx, t, driver, "CREATE (:Person {id: 1, name: 'Alice'})")

		name, err := q.GetPersonName(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, "Alice", name)

		// Two rows for the same id triggers ErrMultipleResults.
		seedV5(ctx, t, driver, "CREATE (:Person {id: 1, name: 'AliceTwin'})")
		_, err = q.GetPersonName(ctx, 1)
		require.ErrorIs(t, err, mixedv5.ErrMultipleResults, "two matching rows must return ErrMultipleResults")

		// :exec write path: delete both rows, then re-query and confirm
		// ErrNoRows — proves the :exec method actually mutated the graph.
		require.NoError(t, q.RemovePerson(ctx, 1))
		_, err = q.GetPersonName(ctx, 1)
		require.ErrorIs(t, err, mixedv5.ErrNoRows, "after :exec delete, :one must see empty result")
	})

	t.Run("many_col_many: many + params", func(t *testing.T) {
		seedV5(ctx, t, driver, wipeCypher)
		q := manycolmanyv5.New(driver)

		// Two locales, three ages: only Alice satisfies age > 25 AND locale = 'en'.
		seedV5(ctx, t, driver, `
			CREATE (:Person {name: 'Alice', age: 30, locale: 'en'})
			CREATE (:Person {name: 'Bob',   age: 20, locale: 'en'})
			CREATE (:Person {name: 'Cara',  age: 40, locale: 'fr'})
		`)

		rows, err := q.PeopleByAgeAndLocale(ctx, manycolmanyv5.PeopleByAgeAndLocaleParams{
			MinAge: 25,
			Locale: "en",
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "Alice", rows[0].Name)
		require.Equal(t, int64(30), rows[0].Age)

		// Empty result set on :many is (empty slice, nil error) — distinct
		// from :one's ErrNoRows contract.
		rows, err = q.PeopleByAgeAndLocale(ctx, manycolmanyv5.PeopleByAgeAndLocaleParams{
			MinAge: 100,
			Locale: "en",
		})
		require.NoError(t, err)
		require.Empty(t, rows)
	})
}

// TestLiveSmokeV6 mirrors V5 against the v6 driver. Its own container
// runs in parallel with V5's (t.Parallel() on both arms), so the two
// container boots overlap.
func TestLiveSmokeV6(t *testing.T) {
	t.Parallel()
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping neo4j testcontainer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	boltURI := startContainer(ctx, t)

	driver, err := neo4jv6.NewDriver(boltURI, neo4jv6.BasicAuth("neo4j", neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j v6 driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")

	t.Run("mixed_read_write_batch: one + exec", func(t *testing.T) {
		seedV6(ctx, t, driver, wipeCypher)
		q := mixedv6.New(driver)

		_, err := q.GetPersonName(ctx, 1)
		require.ErrorIs(t, err, mixedv6.ErrNoRows, "empty graph must return ErrNoRows")

		seedV6(ctx, t, driver, "CREATE (:Person {id: 1, name: 'Alice'})")

		name, err := q.GetPersonName(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, "Alice", name)

		seedV6(ctx, t, driver, "CREATE (:Person {id: 1, name: 'AliceTwin'})")
		_, err = q.GetPersonName(ctx, 1)
		require.ErrorIs(t, err, mixedv6.ErrMultipleResults, "two matching rows must return ErrMultipleResults")

		require.NoError(t, q.RemovePerson(ctx, 1))
		_, err = q.GetPersonName(ctx, 1)
		require.ErrorIs(t, err, mixedv6.ErrNoRows, "after :exec delete, :one must see empty result")
	})

	t.Run("many_col_many: many + params", func(t *testing.T) {
		seedV6(ctx, t, driver, wipeCypher)
		q := manycolmanyv6.New(driver)

		seedV6(ctx, t, driver, `
			CREATE (:Person {name: 'Alice', age: 30, locale: 'en'})
			CREATE (:Person {name: 'Bob',   age: 20, locale: 'en'})
			CREATE (:Person {name: 'Cara',  age: 40, locale: 'fr'})
		`)

		rows, err := q.PeopleByAgeAndLocale(ctx, manycolmanyv6.PeopleByAgeAndLocaleParams{
			MinAge: 25,
			Locale: "en",
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "Alice", rows[0].Name)
		require.Equal(t, int64(30), rows[0].Age)

		rows, err = q.PeopleByAgeAndLocale(ctx, manycolmanyv6.PeopleByAgeAndLocaleParams{
			MinAge: 100,
			Locale: "en",
		})
		require.NoError(t, err)
		require.Empty(t, rows)
	})
}

// seedV5 executes the given cypher against a fresh v5 session in write
// mode. Uses the driver directly (not the generated code) so the seed
// path is independent of the surface under test — a bug in a generated
// method cannot mask itself by shaping the seed. Multi-CREATE statements
// are idiomatic Cypher; the test relies on that shape. Kept separate
// from seedV6 because neo4j v5's DriverWithContext and v6's Driver are
// distinct types from distinct packages — parameterising would need a
// generic constraint that both interfaces satisfy, which isn't cheaper
// than two forks of the same 15-line helper.
func seedV5(ctx context.Context, t *testing.T, driver neo4jv5.DriverWithContext, cypher string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeWrite})
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

// seedV6 is seedV5 against the v6 driver. See seedV5's doc for the
// rationale on keeping the two helpers separate.
func seedV6(ctx context.Context, t *testing.T, driver neo4jv6.Driver, cypher string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jv6.SessionConfig{AccessMode: neo4jv6.AccessModeWrite})
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
