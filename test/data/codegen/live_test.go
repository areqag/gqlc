//go:build codegen_live

// Live smoke test for generated repositories against a real neo4j v5 driver
// (gqlc-73h). Opt-in via -tags codegen_live so PR CI stays fast; the manual /
// nightly CI job runs it. Lives in the nested test/data/codegen module so
// testcontainers and its ~50 transitive deps stay out of gqlc's root go.mod
// and the compiler binary.
//
// The test drives two golden fixtures — mixed_read_write_batch (:one + :exec)
// and many_col_many (:many with params) — through the same neo4j container.
// Between sub-tests the graph is wiped, so container startup cost pays for
// both.
package fixtures

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"

	manycolmany "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many/golden"
	mixed "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden"
)

const (
	neo4jImage    = "neo4j:5-community"
	neo4jPassword = "gqlctest1"
	// wipeCypher DETACHes so any prior sub-test's leftover edges are removed
	// alongside its nodes; DELETE on an empty graph is a no-op in neo4j 5.
	wipeCypher = "MATCH (n) DETACH DELETE n"
)

// TestLiveSmoke starts one neo4j container and runs the generated
// repositories' :one, :many, and :exec paths against it. A single container
// amortises the ~15s startup across every sub-test. Skips when GQLC_SKIP_LIVE
// is set so a developer without docker can still run
// `go test -tags codegen_live ./...` without a hard failure.
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping neo4j testcontainer")
	}
	// A single top-level timeout keeps a stuck container from hanging the
	// whole test binary indefinitely. Neo4j 5-community typically starts in
	// <15s; 120s covers a cold image pull on a slow runner.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

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

	driver, err := neo4j.NewDriverWithContext(boltURI, neo4j.BasicAuth("neo4j", neo4jPassword, ""))
	require.NoError(t, err, "construct neo4j driver")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify neo4j connectivity")

	// Sub-test bodies are inlined so require-failures point at the assertion,
	// not at a helper wrapper. Each sub-test wipes the graph on entry.

	t.Run("mixed_read_write_batch: one + exec", func(t *testing.T) {
		seed(ctx, t, driver, wipeCypher)
		q := mixed.New(driver)

		// ErrNoRows on empty graph — errors.Is (via require.ErrorIs) confirms
		// the sentinel is identity-matchable so callers can branch generically.
		_, err := q.GetPersonName(ctx, 1)
		require.ErrorIs(t, err, mixed.ErrNoRows, "empty graph must return ErrNoRows")

		seed(ctx, t, driver, "CREATE (:Person {id: 1, name: 'Alice'})")

		name, err := q.GetPersonName(ctx, 1)
		require.NoError(t, err)
		require.Equal(t, "Alice", name)

		// Two rows for the same id triggers ErrMultipleResults.
		seed(ctx, t, driver, "CREATE (:Person {id: 1, name: 'AliceTwin'})")
		_, err = q.GetPersonName(ctx, 1)
		require.ErrorIs(t, err, mixed.ErrMultipleResults, "two matching rows must return ErrMultipleResults")

		// :exec write path: delete both rows, then re-query and confirm
		// ErrNoRows — proves the :exec method actually mutated the graph.
		require.NoError(t, q.RemovePerson(ctx, 1))
		_, err = q.GetPersonName(ctx, 1)
		require.ErrorIs(t, err, mixed.ErrNoRows, "after :exec delete, :one must see empty result")
	})

	t.Run("many_col_many: many + params", func(t *testing.T) {
		seed(ctx, t, driver, wipeCypher)
		q := manycolmany.New(driver)

		// Two locales, three ages: only Alice satisfies age > 25 AND locale = 'en'.
		seed(ctx, t, driver, `
			CREATE (:Person {name: 'Alice', age: 30, locale: 'en'})
			CREATE (:Person {name: 'Bob',   age: 20, locale: 'en'})
			CREATE (:Person {name: 'Cara',  age: 40, locale: 'fr'})
		`)

		rows, err := q.PeopleByAgeAndLocale(ctx, manycolmany.PeopleByAgeAndLocaleParams{
			MinAge: 25,
			Locale: "en",
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "Alice", rows[0].Name)
		require.Equal(t, int64(30), rows[0].Age)

		// Empty result set on :many is (empty slice, nil error) — distinct
		// from :one's ErrNoRows contract.
		rows, err = q.PeopleByAgeAndLocale(ctx, manycolmany.PeopleByAgeAndLocaleParams{
			MinAge: 100,
			Locale: "en",
		})
		require.NoError(t, err)
		require.Empty(t, rows)
	})
}

// seed executes the given cypher against a fresh session in write mode.
// Uses the driver directly (not the generated code) so the seed path is
// independent of the surface under test — a bug in a generated method
// cannot mask itself by shaping the seed. Multi-CREATE statements are
// idiomatic Cypher; the test relies on that shape.
func seed(ctx context.Context, t *testing.T, driver neo4j.DriverWithContext, cypher string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close session: %v", err)
		}
	}()
	_, err := neo4j.ExecuteWrite(ctx, session, func(tx neo4j.ManagedTransaction) (any, error) {
		result, err := tx.Run(ctx, cypher, nil)
		if err != nil {
			return nil, err
		}
		return result.Consume(ctx)
	})
	require.NoError(t, err, "seed: %s", cypher)
}
