//go:build codegen_live

// The neo4j half of bd gqlc-14u0l, through the emitted package of the
// list_expression_unknown_element fixture against the digest-pinned image. The
// AGE half is TestAGEServesAnUnknownColumnAsAny.
//
// A column the resolver types as unknown says nothing about nullability, and
// a.dates[0] is null over a node with no dates or an empty list. The row reads
// both back through the generated method and requires nil where the graph
// holds null, beside a value where it holds one, so a method that answered nil
// to everything cannot pass it.
//
// One driver major: the null gate is written by one codegen path for v5 and
// v6, and the subject is that path, not the driver.
//
// The second row is bd gqlc-gem1p's: RETURN null AS n, a column that holds
// nothing but null and was planned non-nullable the same way.
//
// The name is spelled into the justfile recipe and internal/liverecipes, which
// TestEveryLiveTestIsRunByARecipeThatNamesIt holds.

package fixtures_test

import (
	"context"
	"os"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"

	unknownelemv5 "github.com/areqag/gqlc/test/data/codegen/valid/list_expression_unknown_element/golden/neo4j-go-v5"
	scalarnullv5 "github.com/areqag/gqlc/test/data/codegen/valid/scalar_null/golden/neo4j-go-v5"
)

// TestNeo4jPassesANullAnyColumnAsNil reads a null through an
// unknown-typed column and a literal-null column from a live neo4j server.
func TestNeo4jPassesANullAnyColumnAsNil(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	boltURI := startNeo4jContainer(ctx, t)
	driver, err := neo4jv5.NewDriverWithContext(boltURI, neo4jv5.BasicAuth(neo4jUser, neo4jPassword, ""))
	require.NoError(t, err, "construct the driver the probe runs in")
	t.Cleanup(func() {
		if err := driver.Close(ctx); err != nil {
			t.Logf("close the probe driver: %v", err)
		}
	})
	require.NoError(t, driver.VerifyConnectivity(ctx), "verify connectivity")

	t.Run("an index past what the graph holds reads back as nil", func(t *testing.T) {
		require.NoError(t, writeProperty(ctx, t, driver,
			"CREATE (:Account {id: 1, dates: [date('2024-01-02')]}), (:Account {id: 2}), (:Account {id: 3, dates: []})"),
			"seed the probe nodes")

		got, err := unknownelemv5.New(driver).AccountBareUnknown(ctx)
		require.NoError(t, err, "a null in an unknown column is a value the graph holds, not a broken NOT NULL")
		require.Len(t, got, 3)
		require.NotNil(t, got[0], "id 1 holds a date at index 0")
		require.Nil(t, got[1], "id 2 has no dates property")
		require.Nil(t, got[2], "id 3 has an empty dates list, so index 0 is out of range")
	})

	// The same planning arm's other untyped column (bd gqlc-gem1p): a literal
	// null can only ever arrive null, so a null gate on it fails every call.
	t.Run("a literal null column reads back as nil", func(t *testing.T) {
		got, err := scalarnullv5.New(driver).OneNull(ctx)
		require.NoError(t, err, "RETURN null AS n holds nothing but null")
		require.Nil(t, got)
	})
}
