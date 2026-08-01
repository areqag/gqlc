//go:build codegen_live

package fixtures

import (
	"context"
	"testing"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"

	manycolmanyv5 "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many/golden/neo4j-go-v5"
	manycolmanyv6 "github.com/areqag/gqlc/test/data/codegen/valid/many_col_many/golden/neo4j-go-v6"
	mixedv5 "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden/neo4j-go-v5"
	mixedv6 "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden/neo4j-go-v6"
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

// neo4jV5 runs the battery against the neo4j-go-v5 target. Scenarios share
// the arm's container and are isolated by a DETACH-wipe on entry, which holds
// because the battery runs an arm's scenarios sequentially.
type neo4jV5 struct {
	driver neo4jv5.DriverWithContext
	mixed  mixedReadWriteBatchV5
	many   manyColManyV5
}

func startNeo4jV5(ctx context.Context, t *testing.T) backend {
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
		driver: driver,
		mixed:  mixedReadWriteBatchV5{q: mixedv5.New(driver)},
		many:   manyColManyV5{q: manycolmanyv5.New(driver)},
	}
}

func (b *neo4jV5) isolate(ctx context.Context, t *testing.T) {
	t.Helper()
	b.seed(ctx, t, wipeCypher)
}

func (b *neo4jV5) seed(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	session := b.driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeWrite})
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

func (b *neo4jV5) mixedReadWriteBatch() mixedReadWriteBatchQuerier { return b.mixed }

func (b *neo4jV5) manyColMany() manyColManyQuerier { return b.many }

type mixedReadWriteBatchV5 struct{ q *mixedv5.Queries }

func (a mixedReadWriteBatchV5) getPersonName(ctx context.Context, id int64) (string, error) {
	return a.q.GetPersonName(ctx, id)
}

func (a mixedReadWriteBatchV5) removePerson(ctx context.Context, id int64) error {
	return a.q.RemovePerson(ctx, id)
}

func (a mixedReadWriteBatchV5) errNoRows() error { return mixedv5.ErrNoRows }

func (a mixedReadWriteBatchV5) errMultipleResults() error { return mixedv5.ErrMultipleResults }

type manyColManyV5 struct{ q *manycolmanyv5.Queries }

func (a manyColManyV5) peopleByAgeAndLocale(ctx context.Context, minAge int64, locale string) ([]person, error) {
	rows, err := a.q.PeopleByAgeAndLocale(ctx, manycolmanyv5.PeopleByAgeAndLocaleParams{
		MinAge: minAge,
		Locale: locale,
	})
	if err != nil {
		return nil, err
	}
	out := make([]person, 0, len(rows))
	for _, row := range rows {
		out = append(out, person{Name: row.Name, Age: row.Age})
	}
	return out, nil
}

// neo4jV6 runs the battery against the neo4j-go-v6 target on the same image,
// isolated the same way as neo4jV5. The v6 driver renamed the v5
// DriverWithContext interface to Driver and the NewDriverWithContext
// constructor to NewDriver.
type neo4jV6 struct {
	driver neo4jv6.Driver
	mixed  mixedReadWriteBatchV6
	many   manyColManyV6
}

func startNeo4jV6(ctx context.Context, t *testing.T) backend {
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
		driver: driver,
		mixed:  mixedReadWriteBatchV6{q: mixedv6.New(driver)},
		many:   manyColManyV6{q: manycolmanyv6.New(driver)},
	}
}

func (b *neo4jV6) isolate(ctx context.Context, t *testing.T) {
	t.Helper()
	b.seed(ctx, t, wipeCypher)
}

func (b *neo4jV6) seed(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	session := b.driver.NewSession(ctx, neo4jv6.SessionConfig{AccessMode: neo4jv6.AccessModeWrite})
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

func (b *neo4jV6) mixedReadWriteBatch() mixedReadWriteBatchQuerier { return b.mixed }

func (b *neo4jV6) manyColMany() manyColManyQuerier { return b.many }

type mixedReadWriteBatchV6 struct{ q *mixedv6.Queries }

func (a mixedReadWriteBatchV6) getPersonName(ctx context.Context, id int64) (string, error) {
	return a.q.GetPersonName(ctx, id)
}

func (a mixedReadWriteBatchV6) removePerson(ctx context.Context, id int64) error {
	return a.q.RemovePerson(ctx, id)
}

func (a mixedReadWriteBatchV6) errNoRows() error { return mixedv6.ErrNoRows }

func (a mixedReadWriteBatchV6) errMultipleResults() error { return mixedv6.ErrMultipleResults }

type manyColManyV6 struct{ q *manycolmanyv6.Queries }

func (a manyColManyV6) peopleByAgeAndLocale(ctx context.Context, minAge int64, locale string) ([]person, error) {
	rows, err := a.q.PeopleByAgeAndLocale(ctx, manycolmanyv6.PeopleByAgeAndLocaleParams{
		MinAge: minAge,
		Locale: locale,
	})
	if err != nil {
		return nil, err
	}
	out := make([]person, 0, len(rows))
	for _, row := range rows {
		out = append(out, person{Name: row.Name, Age: row.Age})
	}
	return out, nil
}
