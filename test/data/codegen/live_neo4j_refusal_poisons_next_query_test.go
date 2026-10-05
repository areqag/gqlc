//go:build codegen_live

// A pin on a defect in both neo4j driver majors, not on gqlc (bd gqlc-xeyks,
// ADR 0048). When the server refuses a request at decode — Request.Invalid,
// for instance a time.Time sent with a zone id it does not know — the NEXT
// query on that pooled connection fails client-side with `invalid state 4`,
// and the one after it succeeds. Measured on v5.28.4 and v6.2.0 against the
// pinned image, identically for every session shape gqlc emits.
//
// The rows assert the defect is still there. When a driver release fixes it
// they go red, and ADR 0048's caveat is the thing to delete.
//
// The refusal is provoked through a raw session, because gqlc's own binds no
// longer send a zone id the server refuses (bd gqlc-m3ax). The query that
// fails is a generated one. MaxConnectionPoolSize is 1 so the failing query
// is handed the poisoned connection on every run, not only when the pool
// happens to pick it.
//
// The second set of rows is the allow-pin for the mitigation ADR 0048 names:
// the same arm with ConnectionLivenessCheckTimeout = 0 must not fail.
//
// It rides TestNeo4jRoundTripsANullableElementTemporalList's container rather
// than booting one of its own.

package fixtures_test

import (
	"context"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	cfgv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	neo4jv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	cfgv6 "github.com/neo4j/neo4j-go-driver/v6/neo4j/config"
	"github.com/stretchr/testify/require"

	ntlv5 "github.com/areqag/gqlc/test/data/codegen/valid/nullable_timestamp_list_element/golden/neo4j-go-v5"
	ntlv6 "github.com/areqag/gqlc/test/data/codegen/valid/nullable_timestamp_list_element/golden/neo4j-go-v6"
)

// refusedZoneParam is a parameter the server refuses with Request.Invalid:
// the driver sends the zone as the id "CEST", which is no zone id.
var refusedZoneParam = map[string]any{"t": time.Date(2024, 2, 29, 21, 30, 0, 0, time.FixedZone("CEST", 2*3600))}

// poisonedConnectionArm is one major's view: refuse sends the refused
// request through a raw session, and next runs a generated query.
type poisonedConnectionArm struct {
	refuse func(ctx context.Context) error
	next   func(ctx context.Context) error
}

func runRefusalPoisonsNextQueryRows(ctx context.Context, t *testing.T, arm poisonedConnectionArm) {
	t.Helper()
	err := arm.refuse(ctx)
	require.ErrorContains(t, err, "Neo.ClientError.Request.Invalid", "the premise: the server refuses the zone id")

	err = arm.next(ctx)
	require.ErrorContains(t, err, "invalid state 4",
		"the query after a refusal no longer fails with invalid state 4: if the driver defect ADR 0048 records is gone, its caveat is stale")

	require.NoError(t, arm.next(ctx), "the second query after a refusal is handed a fresh connection")
}

// runLivenessCheckMitigatesRows holds the caller-side mitigation ADR 0048
// names: with ConnectionLivenessCheckTimeout = 0 the pool RESETs a connection
// on every borrow, the poisoned one meets EOF there and is discarded, and the
// query after a refusal succeeds. The arm must be opened with that setting.
func runLivenessCheckMitigatesRows(ctx context.Context, t *testing.T, arm poisonedConnectionArm) {
	t.Helper()
	err := arm.refuse(ctx)
	require.ErrorContains(t, err, "Neo.ClientError.Request.Invalid", "the premise: the server refuses the zone id")

	require.NoError(t, arm.next(ctx),
		"with a liveness check on every borrow the query after a refusal must succeed, as ADR 0048 tells callers")
}

func openPoisonedConnectionArmV5(ctx context.Context, t *testing.T, boltURI string, checkEveryBorrow bool) poisonedConnectionArm {
	t.Helper()
	driver, err := neo4jv5.NewDriverWithContext(boltURI, neo4jv5.BasicAuth(neo4jUser, neo4jPassword, ""),
		func(c *cfgv5.Config) {
			c.MaxConnectionPoolSize = 1
			if checkEveryBorrow {
				c.ConnectionLivenessCheckTimeout = 0
			}
		})
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	q := ntlv5.New(driver)
	return poisonedConnectionArm{
		refuse: func(ctx context.Context) error {
			_, err := neo4jv5.ExecuteQuery(ctx, driver, "RETURN $t AS t", refusedZoneParam, neo4jv5.EagerResultTransformer)
			return err
		},
		next: func(ctx context.Context) error {
			_, err := q.EntriesMatching(ctx, ntlv5.EntriesMatchingParams{})
			return err
		},
	}
}

func openPoisonedConnectionArmV6(ctx context.Context, t *testing.T, boltURI string, checkEveryBorrow bool) poisonedConnectionArm {
	t.Helper()
	driver, err := neo4jv6.NewDriver(boltURI, neo4jv6.BasicAuth(neo4jUser, neo4jPassword, ""),
		func(c *cfgv6.Config) {
			c.MaxConnectionPoolSize = 1
			if checkEveryBorrow {
				c.ConnectionLivenessCheckTimeout = 0
			}
		})
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close driver: %v", err)
		}
	})
	q := ntlv6.New(driver)
	return poisonedConnectionArm{
		refuse: func(ctx context.Context) error {
			_, err := neo4jv6.ExecuteQuery(ctx, driver, "RETURN $t AS t", refusedZoneParam, neo4jv6.EagerResultTransformer)
			return err
		},
		next: func(ctx context.Context) error {
			_, err := q.EntriesMatching(ctx, ntlv6.EntriesMatchingParams{})
			return err
		},
	}
}
