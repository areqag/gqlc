//go:build codegen_live

// The premise under min/max's generated column type, measured on both servers
// instead of taken from openCypher's prose.
//
// Spec ruling-p9qgu §3.1 lets a certified `min(p.rank)` / `max(p.rank)` keep the
// operand's WIDTH — the result is one of the operand's own values, not a fold —
// and §4 then makes the column NULLABLE regardless of what the schema says about
// the property. Those two halves are decided by different facts, and only the
// first is about the property:
//
//   - the width is the property's, because min SELECTS.
//   - the nullability is the GROUP's, because an aggregation over zero rows
//     still answers one row, and that row's value is null.
//
// So a `rank` declared NOT NULL in the schema still generates `*int32`, and a
// reader who knows only the schema will read that pointer as a mistake. It is
// not: the emptiness that makes it nil is the match's, and no schema constrains
// how many nodes a query matches. ADR 0042 states that in prose; this file is
// the part of it that could be wrong, and it is the part no golden can settle —
// a golden records what gqlc EMITS, and the question here is what the servers
// DO.
//
// Each arm runs the same three claims, in an order where each one makes the next
// meaningful:
//
//  1. every matched node carries the property. Without this row a null below
//     would be explained by an absent property, which is a different fact with a
//     different remedy (it would make the column nullable for a reason the
//     schema DOES constrain).
//  2. min and max over the non-empty match answer the extremes, non-null. This
//     is the control, and it is not a courtesy: a server that answered null to
//     every aggregate would pass claim 3 on its own, and so would a container
//     that was simply dead.
//  3. the same aggregates under a predicate nothing satisfies answer exactly one
//     row, and that row is null. Exactly one, because zero rows would be a
//     different server behaviour with the same green pointer — gqlc's one-row
//     methods report ErrNoRows for that, so the column type would never be
//     exercised and this file would witness nothing about it.
//
// The predicate does the emptying rather than an unmatched LABEL, and that is
// deliberate on AGE's account: AGE short-circuits a MATCH on a label with no
// nodes and never evaluates the projection at all (measured in
// live_age_dialect_test.go's namespace rows). A label with nodes and a WHERE
// that excludes them reaches the aggregate on both servers, so the two arms
// send the SAME query text and a divergence is the server's rather than the
// probe's.
//
// ONE NEO4J ARM, NOT TWO. The claim is the server's, not the driver's: v5 and v6
// speak to the same store and would measure one fact twice at the price of a
// second container on the job pull requests pay for.
//
// COST. The neo4j half rides the recipe that starts its own container, which
// carries no -count=1, so a pull request that does not invalidate the test binary
// pays nothing for it. The AGE half boots its own container every time, which is
// the charge bd gqlc-zase measured and accepted.
//
// Both names are spelled into the justfile recipes and into
// internal/liverecipes.LiveArms. go test's -run is unanchored and those
// alternations are NAME LISTS for that reason; a live test added here and not
// added there runs in no job at all and prints ok, which
// TestEveryLiveTestIsRunByARecipeThatNamesIt refuses.

package fixtures_test

import (
	"context"
	"os"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"
)

// The probe's graph, and the six statements both arms send verbatim. They are
// shared constants rather than a per-arm spelling precisely so that a
// disagreement between the two arms is a disagreement between the two servers.
const (
	emptyGroupSeed = "CREATE (:EmptyGroupProbe {rank: 2}) CREATE (:EmptyGroupProbe {rank: 7})"

	// The census. Two columns would need two record declarations on the AGE
	// side, so every statement here answers exactly one.
	emptyGroupNodes  = "MATCH (n:EmptyGroupProbe) RETURN count(n)"
	emptyGroupRanked = "MATCH (n:EmptyGroupProbe) RETURN count(n.rank)"

	// The control: a match that binds rows.
	emptyGroupMin = "MATCH (n:EmptyGroupProbe) RETURN min(n.rank)"
	emptyGroupMax = "MATCH (n:EmptyGroupProbe) RETURN max(n.rank)"

	// The claim: the same aggregates over a group the predicate empties. `rank`
	// is on every node and is a positive integer on both, so the filter removes
	// every row without the server having to reason about a missing property.
	emptyGroupMinOfNone = "MATCH (n:EmptyGroupProbe) WHERE n.rank < 0 RETURN min(n.rank)"
	emptyGroupMaxOfNone = "MATCH (n:EmptyGroupProbe) WHERE n.rank < 0 RETURN max(n.rank)"
)

// TestNeo4jMinOverAnEmptyGroupIsNull is the neo4j half of ruling-p9qgu §7's
// obligation.
func TestNeo4jMinOverAnEmptyGroupIsNull(t *testing.T) {
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

	require.NoError(t, writeProperty(ctx, t, driver, emptyGroupSeed), "seed the probe nodes")

	t.Run("every matched node carries the property", func(t *testing.T) {
		require.Equal(t, int64(2), neo4jAggregate(ctx, t, driver, emptyGroupNodes))
		require.Equal(t, int64(2), neo4jAggregate(ctx, t, driver, emptyGroupRanked),
			"the null below has to be the GROUP's emptiness and not a missing property; "+
				"this row is what tells those two apart")
	})

	t.Run("min and max over a non-empty group answer the extremes", func(t *testing.T) {
		require.Equal(t, int64(2), neo4jAggregate(ctx, t, driver, emptyGroupMin),
			"a server that answered null to every aggregate would pass the row below for the wrong reason")
		require.Equal(t, int64(7), neo4jAggregate(ctx, t, driver, emptyGroupMax))
	})

	t.Run("min and max over an empty group answer one row, holding null", func(t *testing.T) {
		require.Nil(t, neo4jAggregate(ctx, t, driver, emptyGroupMinOfNone),
			"this is the whole reason a certified min column is a POINTER even where the "+
				"property is NOT NULL (ruling-p9qgu §4, ADR 0042); if this server has started "+
				"answering a value here, that nullability is over-cautious rather than wrong, "+
				"and the ADR is what to revisit")
		require.Nil(t, neo4jAggregate(ctx, t, driver, emptyGroupMaxOfNone))
	})
}

// TestAGEMinOverAnEmptyGroupIsNull is the other half. Same six statements, same
// three claims, on the server that reaches Go through pgx and agtype rather than
// through Bolt.
func TestAGEMinOverAnEmptyGroupIsNull(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	pool := agtypeProbePool(ctx, t)
	const graph = "gqlc_empty_group"
	_, err := pool.Exec(ctx, "SELECT create_graph("+pgQuote(graph)+")")
	require.NoError(t, err, "create graph %s", graph)

	// agtypeOneRaw declares one agtype column, so the seed goes through the same
	// shape with a statement that projects nothing; AGE answers a CREATE with no
	// rows, which is why it is Exec'd here rather than probed.
	_, err = pool.Exec(ctx,
		"SELECT * FROM cypher("+pgQuote(graph)+", $gqlcprobe$ "+emptyGroupSeed+" $gqlcprobe$) AS (v agtype)")
	require.NoError(t, err, "seed the probe nodes")

	t.Run("every matched node carries the property", func(t *testing.T) {
		require.Equal(t, "2", agtypeOneValue(ctx, t, pool, graph, emptyGroupNodes))
		require.Equal(t, "2", agtypeOneValue(ctx, t, pool, graph, emptyGroupRanked),
			"the null below has to be the GROUP's emptiness and not a missing property; "+
				"this row is what tells those two apart")
	})

	t.Run("min and max over a non-empty group answer the extremes", func(t *testing.T) {
		require.Equal(t, "2", agtypeOneValue(ctx, t, pool, graph, emptyGroupMin),
			"a server that answered null to every aggregate would pass the row below for the wrong reason")
		require.Equal(t, "7", agtypeOneValue(ctx, t, pool, graph, emptyGroupMax))
	})

	t.Run("min and max over an empty group answer one row, holding null", func(t *testing.T) {
		// agtypeOneRaw requires exactly one row, so the other way this could go
		// — AGE serving no row at all for an aggregate over nothing — fails here
		// naming the statement rather than passing as a nil that never arrived.
		// The distinction matters: a generated one-row method reports ErrNoRows
		// for an empty result set and never reaches the column's type.
		for _, cypher := range []string{emptyGroupMinOfNone, emptyGroupMaxOfNone} {
			raw, isNull := agtypeOneRaw(ctx, t, pool, graph, cypher)
			require.True(t, isNull,
				"%s answered the text %q rather than SQL NULL. A certified min column is a "+
					"POINTER even where the property is NOT NULL (ruling-p9qgu §4, ADR 0042) "+
					"because this row is null; a value here makes that nullability "+
					"over-cautious rather than wrong, and the ADR is what to revisit",
				cypher, string(raw))
		}
	})
}

// neo4jAggregate runs a read expected to answer exactly one row of exactly one
// column and returns the driver's value for it — nil for a null.
//
// Undecoded on purpose: what this file asserts is the value the SERVER put on
// the wire, and running it through anything that maps null to a zero value would
// make the null rows unfalsifiable.
func neo4jAggregate(ctx context.Context, t *testing.T, driver neo4jv5.DriverWithContext, cypher string) any {
	t.Helper()

	session := driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeRead})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the probe ran in: %v", err)
		}
	}()

	result, err := session.Run(ctx, cypher, nil)
	require.NoError(t, err, "run %s", cypher)
	records, err := result.Collect(ctx)
	require.NoError(t, err, "collect %s", cypher)

	// One row, not "at least one": an aggregate with no grouping key answering
	// zero rows is the behaviour that would take the null rows out of a
	// generated method's reach entirely.
	require.Len(t, records, 1, "%s answered %d rows", cypher, len(records))
	require.Len(t, records[0].Values, 1, "%s answered %d columns", cypher, len(records[0].Values))

	t.Logf("%s => %#v", cypher, records[0].Values[0])
	return records[0].Values[0]
}
