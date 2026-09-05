//go:build codegen_live

// The measurement the RECORD carrier design refuses to let anyone assert from
// folklore: what each server actually does with a MAP-VALUED STORED PROPERTY.
//
// docs/specs/codegen-record-union-carriers.md §5 names a fork rather than a
// rule. Its neo4j StorableProperty arm — false for every record width — rests
// on the belief that the server will not keep a map in a property, and that
// belief had never been measured in this repository. §7 makes the measurement
// a precondition of writing the arm. These two rows are it, on the same
// footing as ADR 0035's nested-list pair next door:
//
//   - neo4j refuses the write. Then the fork takes its expected branch: the
//     declaration is refused at generation time, and §6's four positions
//     collapse to zero on that backend because nothing else in the language
//     derives a record width (§6's derivation root). This file becomes the
//     premise tripwire under that refusal, exactly as
//     TestNeo4jRefusesANestedListStoredProperty is under ADR 0035's.
//   - neo4j SERVES the write. Then §5's other branch is taken, no
//     StorableProperty arm is written, and the divergence ADR mirrors ADR
//     0035 with the two backends' roles reversed. A RED here is that finding,
//     not a defect to route around.
//
// THE TWO CONTROLS ARE THE POINT OF THE NEO4J ROW, because the outcome this
// design expects is a REFUSAL, and a refusal is what a broken probe also
// produces. A connection failure, a bad credential or a syntax error all
// present as "the server said no", and every one of them would confirm the
// expected branch of the fork for the wrong reason.
//
//   - a scalar property on the same label is stored, so the write path,
//     the session and the credentials are known good before the refusal is
//     read as a fact about maps;
//   - the same map value is PROJECTED back as a column, so the refusal is
//     known to be about STORAGE rather than about the server's ability to
//     handle a map at all. That row is also the live half of the asymmetry
//     §6 turns on, at the one position that survives the refusal.
//
// THE WORDING IS ASSERTED IN ITS OWN ROW, and deliberately not folded into
// the refusal row. The refusal row answers the fork; the wording row answers
// a much smaller question, and merging them would make one red mean either
// "the server changed its behaviour" or "the server changed its prose". The
// exact text is logged whatever happens, so the run itself carries the
// evidence rather than this comment.
//
// ONE ARM ON THE NEO4J SIDE, not one per driver major: the rule is the
// store's, and v5 and v6 speak to the same store. ADR 0035's file states the
// same reasoning and pays for the same single container.
//
// Both names are spelled into the justfile recipes. go test's -run is
// unanchored and those alternations are NAME LISTS for that reason; a live
// test added here and not added there runs in no job at all and prints ok,
// which TestEveryLiveTestIsRunByARecipeThatNamesIt refuses.

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
	// mapPropertyWrite is the shape §5 expects neo4j to refuse, spelled as a
	// literal so neither server is being asked about gqlc's emission. The
	// field names are the record encoding's, but nothing here depends on
	// that — this is a question about a map arriving at a property slot.
	mapPropertyWrite = "CREATE (:RecordPropertyProbe {addr: {city: 'Yerevan', zip: 1}})"

	// scalarPropertyWrite is the write-path control: same label, same
	// session, a width nobody disputes.
	scalarPropertyWrite = "CREATE (:RecordPropertyProbe {city: 'Yerevan'})"

	// mapValueProjection is the storage-versus-value control. The server
	// composes and returns the identical map; only the property slot is in
	// question.
	mapValueProjection = "RETURN {city: 'Yerevan', zip: 1} AS addr"

	// mapPropertyRefusal is the fragment of neo4j's answer that names the
	// rule rather than the statement. UNMEASURED FROM THE SEAT THAT WROTE IT
	// — docker.service is disabled host-wide (bd gqlc-p9g2i), so the first
	// real run of this file is on the live-smoke runner and this string is a
	// prediction until then. Its row says so in its own failure message.
	mapPropertyRefusal = "can only be of primitive types"
)

// TestNeo4jRefusesAMapValuedStoredProperty is the measurement §7 demands
// before any neo4j StorableProperty arm is written, and the tripwire under
// that arm afterwards.
func TestNeo4jRefusesAMapValuedStoredProperty(t *testing.T) {
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

	t.Run("a scalar property is stored", func(t *testing.T) {
		require.NoError(t, writeProperty(ctx, t, driver, scalarPropertyWrite),
			"the refusal below is only a fact about MAPS if an ordinary property write "+
				"succeeds on this same session; without this row a bad credential or an "+
				"unreachable server would confirm the expected fork for the wrong reason")
	})

	t.Run("the same map is served as a projected column", func(t *testing.T) {
		value, err := readSingleValue(ctx, t, driver, mapValueProjection)
		require.NoError(t, err,
			"the refusal below must be about STORAGE, not about the server handling a map at all")
		require.IsType(t, map[string]any{}, value,
			"the projection must come back as a map, or this control witnesses nothing")
	})

	// The row that answers the fork. Its failure is a FINDING — spec §5's
	// accepts branch — and the run's own log line carries what the server said.
	var refusal error
	t.Run("a map-valued stored property is refused", func(t *testing.T) {
		refusal = writeProperty(ctx, t, driver, mapPropertyWrite)
		if refusal != nil {
			t.Logf("the server's answer, verbatim: %v", refusal)
		}
		require.Error(t, refusal,
			"THIS RED IS THE MEASUREMENT, not a bug: the pinned neo4j image STORES a map "+
				"property, so spec §5's fork takes its other branch — no StorableProperty "+
				"arm is written, record properties are legal on neo4j, and the divergence "+
				"ADR mirrors ADR 0035 with the backends' roles reversed")
	})

	t.Run("and the refusal names the storage rule", func(t *testing.T) {
		require.Error(t, refusal, "the row above already reported the finding; nothing to word-check")
		require.Contains(t, refusal.Error(), mapPropertyRefusal,
			"the refusal must be the storage rule rather than a syntax error or a dropped "+
				"connection. If the row ABOVE passed and only this one reds, the server still "+
				"refuses and it is this file's predicted wording that is wrong — take the real "+
				"text from the verbatim log line above and correct the constant, do not widen "+
				"this row into one that any error satisfies")
	})
}

// TestAGEStoresARecordProperty is the other half of §7's measurement, and the
// live evidence behind AGE's `StorableProperty` staying `return true`.
//
// It is the ONLY thing in this repository that measures what AGE does with a
// record-shaped property on the wire. internal/codegen/age's corpus driver
// decodes record agtext, but those texts are hand-constructed rather than
// captured off a server, and both files say so: they witness gqlc's decoder
// given a map, never the store's willingness to keep one.
//
// The read-back is not a courtesy either. Accepting a write proves less than
// returning the value proves, and a store that flattened the map or coerced
// it to a string would accept happily.
func TestAGEStoresARecordProperty(t *testing.T) {
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_record_property")

	_, err := pool.Exec(ctx, substituteQueryText(t, shipped, mapPropertyWrite), "{}")
	require.NoError(t, err,
		"AGE must store the shape §5 expects neo4j to refuse; if it does not, the "+
			"AGE-only record fixtures describe writes this server would reject and "+
			"the design's storage ruling is wrong on BOTH backends rather than one")

	rows, err := pool.Query(ctx, substituteQueryText(t, shipped, "MATCH (n:RecordPropertyProbe) RETURN n.addr"), "{}")
	require.NoError(t, err, "read the stored record back")
	defer rows.Close()

	var stored []string
	for rows.Next() {
		var raw []byte
		require.NoError(t, rows.Scan(&raw), "scan the agtype value")
		stored = append(stored, string(raw))
	}
	require.NoError(t, rows.Err())

	require.Len(t, stored, 1, "the probe wrote exactly one node")
	// Spacing and key order are agtype's to choose and are not what this
	// asserts; that the two fields survived as a nested object is.
	flat := strings.ReplaceAll(stored[0], " ", "")
	require.Contains(t, flat, `"city":"Yerevan"`,
		"the string field must come back as a field, not as a flattened or stringified map")
	require.Contains(t, flat, `"zip":1`,
		"and the numeric field beside it, so the row witnesses a nested OBJECT rather than one surviving key")
}

// readSingleValue runs one read and returns the sole column of its sole row.
//
// Separate from writeProperty because that one deliberately reports the
// server's refusal as a value; this one is a control whose whole purpose is to
// succeed, so it is read in a read-mode session and its shape is returned for
// the caller to assert on.
func readSingleValue(ctx context.Context, t *testing.T, driver neo4jv5.DriverWithContext, cypher string) (any, error) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeRead})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the control ran in: %v", err)
		}
	}()

	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		return nil, err
	}
	record, err := result.Single(ctx)
	if err != nil {
		return nil, err
	}
	return record.Values[0], nil
}
