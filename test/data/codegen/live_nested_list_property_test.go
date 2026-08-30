//go:build codegen_live

// The divergence ADR 0035 rests on, measured on both servers rather than
// asserted in the ADR's prose.
//
// The ruling is that the neo4j backend refuses a nested list as a STORED
// PROPERTY at generation time while Apache AGE keeps generating one. That is
// a claim about two servers, and it is the whole reason the corpus now has an
// AGE-only nested_list_property fixture where three neo4j fixtures used to
// carry the shape. If either half stops being true the ruling is wrong, and
// these are the two rows that say so:
//
//   - neo4j refuses the write. Its server answers with "Collections
//     containing collections can not be stored in properties", so gqlc
//     refuses the declaration instead of emitting a struct field no writer
//     could ever fill. If a future image bump serves the write, this row reds
//     and gqlc-v0gk's question reopens with evidence.
//   - AGE serves it, and serves it back. If AGE ever refuses, the
//     nested_list_property fixture's premise died and this row says so rather
//     than the fixture silently generating against a server that would reject
//     every write it describes.
//
// The neo4j row carries a flat-list control, and the control is not a
// courtesy. Without it a server that refused EVERY list property would pass
// the refusal row, and the refusal row is the only thing standing behind a
// rule scoped to NESTING. The AGE row's read-back is the same idea one step
// on: accepting a write proves less than reading the value back proves.
//
// ONE ARM, NOT TWO, on the neo4j side. The rule is the server's, not the
// driver's — v5 and v6 speak to the same store and would be measuring one
// fact twice, at the price of a second container on the job pull requests
// pay for.
//
// WHY ITS OWN FILE. live_age_dialect_test.go is bound by a sweep
// (TestEveryDialectGapCarriesItsWitness) that reads gap witnesses out of the
// bodies of the tests its gap table names, so a probe belonging to no dialect
// gap sitting in there is text a witness could be mistaken for. And
// live_age_ungated_test.go is titled for a measured REFUSAL that no gap acts
// on, which the AGE half here is the opposite of. Both halves are one claim,
// so they are one file rather than a row filed under each backend.
//
// COST. The AGE half is nightly and manual only, like every AGE row. The
// neo4j half rides live-smoke, which pull requests do block on, and it starts
// its own container: three concurrent neo4j containers rather than two, which
// TestLiveSmoke's own header already measures as ~4GB peak and within a
// standard runner. The recipe carries no -count=1, so a pull request that
// does not invalidate the test binary pays nothing at all.
//
// Both names are spelled into the recipes in the justfile. go test's -run is
// unanchored and the recipes' alternations are name lists for that reason; a
// live test added here and not added there runs in no job, which
// TestEveryLiveTestIsRunByARecipeThatNamesIt refuses.

package fixtures_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"
)

const (
	// nestedListPropertyWrite is the shape ADR 0035 refuses at generation
	// time, spelled as a literal so neither server is being asked about
	// gqlc's emission. Both probes send this same text.
	nestedListPropertyWrite = "CREATE (:NestedListProbe {xss: [[1], [2, 3]]})"

	// flatListPropertyWrite is the control: one level shallower, and stored
	// by both servers. It is what makes the refusal a fact about nesting.
	flatListPropertyWrite = "CREATE (:FlatListProbe {xs: [1, 2, 3]})"

	// nestedListRefusal is the substring of neo4j's answer that names the
	// rule rather than the statement, so the row survives a rewording of the
	// surrounding message but not a change of behaviour.
	nestedListRefusal = "Collections containing collections"
)

// TestNeo4jRefusesANestedListStoredProperty is the premise's tripwire on the
// pinned neo4j image.
func TestNeo4jRefusesANestedListStoredProperty(t *testing.T) {
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

	t.Run("a flat list property is stored", func(t *testing.T) {
		require.NoError(t, writeProperty(ctx, t, driver, flatListPropertyWrite),
			"the refusal below is a rule about NESTING, and this row is what says so; "+
				"a server refusing every list property would pass that row for the wrong reason")
	})

	t.Run("a nested list property is refused", func(t *testing.T) {
		err := writeProperty(ctx, t, driver, nestedListPropertyWrite)
		require.Error(t, err,
			"ADR 0035 refuses this declaration at generation time BECAUSE the server "+
				"refuses the write; if the server now serves it, the ruling is what to reconsider")
		require.Contains(t, err.Error(), nestedListRefusal,
			"the refusal must be the storage rule, not a syntax error or a connection failure")
	})
}

// TestAGEStoresANestedListProperty is the other half: the shape neo4j refuses
// is stored and returned intact by AGE, which is why nested_list_property is
// an AGE-only fixture rather than a deleted one.
func TestAGEStoresANestedListProperty(t *testing.T) {
	// The dialect harness is one container, one graph, and the envelope a
	// shipped generated method actually sent, with a slot for the query text.
	// Borrowing it here keeps the probe from composing SQL by hand, so an
	// answer is the construct's and not the harness's. It also handles the
	// GQLC_SKIP_LIVE skip and t.Parallel.
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_nested_list_property")

	_, err := pool.Exec(ctx, substituteQueryText(t, shipped, nestedListPropertyWrite), "{}")
	require.NoError(t, err,
		"AGE must store the shape neo4j refuses; if it does not, the AGE-only "+
			"nested_list_property fixture describes writes this server would reject")

	rows, err := pool.Query(ctx, substituteQueryText(t, shipped, "MATCH (n:NestedListProbe) RETURN n.xss"), "{}")
	require.NoError(t, err, "read the stored nested list back")
	defer rows.Close()

	var stored []string
	for rows.Next() {
		var raw []byte
		require.NoError(t, rows.Scan(&raw), "scan the agtype value")
		stored = append(stored, string(raw))
	}
	require.NoError(t, rows.Err())

	require.Len(t, stored, 1, "the probe wrote exactly one node")
	// Spacing is agtype's to choose and is not what this asserts; the
	// structure and the values are.
	require.Equal(t, "[[1],[2,3]]", strings.ReplaceAll(stored[0], " ", ""),
		"accepting the write proves less than reading the value back unchanged")
}

// writeProperty runs one write in its own auto-commit transaction and returns
// the server's answer to it.
//
// Deliberately NOT ExecuteWrite, which the seeding helpers use: that wraps the
// call in the driver's retry policy, and a probe whose subject is a REFUSAL
// should not be run through machinery whose job is to try refusals again. The
// error can surface either at Run or at Consume depending on when the server
// answers, so both are reported.
func writeProperty(ctx context.Context, t *testing.T, driver neo4jv5.DriverWithContext, cypher string) error {
	t.Helper()
	session := driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the probe ran in: %v", err)
		}
	}()

	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}
