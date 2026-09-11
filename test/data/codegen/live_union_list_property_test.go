//go:build codegen_live

// The measurement bd gqlc-npus demands BEFORE any neo4j StorableProperty arm
// for LIST<UNION<…>> is written, and the premise tripwire under that arm
// afterwards.
//
// docs/specs/codegen-record-union-carriers.md §7 deferred this width
// unmeasured, so neo4j's StorableProperty admitted a union-valued list by
// saying nothing about it. Whether the server will hold a HETEROGENEOUS ARRAY
// as a stored property is a fact about the server, not about gqlc, and the
// spec forbids guessing it — a guessed arm is an ADR 0035 premise tripwire
// waiting to fire. The fork was stated so that either answer was a result:
//
//   - the server STORES a heterogeneous array. Then no arm is written and
//     LIST<UNION<…>> is storable like any other list.
//   - the server REFUSES. Then the arm refuses with ErrUnstorableProperty
//     naming the width, beside the nested-list arm already there.
//
// It refused, so the arm exists and this file is what holds it up. If a
// future image serves the write, the refusal row below reds and gqlc-npus's
// question reopens with evidence rather than the arm quietly outliving its
// premise.
//
// THE CONTROLS ARE THE POINT, because the outcome is a REFUSAL and a refusal
// is what a broken probe also produces. A connection failure, a bad
// credential or a syntax error all present as "the server said no", and every
// one of them would confirm the expected branch for the wrong reason. Three
// stand behind it, each retiring a different way of being wrong:
//
//   - two HOMOGENEOUS arrays are stored on the same session, one per member
//     family. A server that refused every array property, or that had no
//     working write path at all, would pass the refusal row; these say it
//     does not. Two rather than one because the refusal is about the MIX, so
//     neither member's own family may be the thing being refused.
//   - the identical heterogeneous list is PROJECTED back as a column, so the
//     refusal is known to be about STORAGE rather than about the server's
//     ability to handle a mixed list at all. That asymmetry is the same one
//     the record design turns on, and it is why Property still carries a
//     union while this axis refuses a list of one.
//   - a BARE union value is stored, which is what scopes the arm to the LIST
//     form. Without this row the arm could just as well have been hung on
//     KindUnion at the top of StorableProperty, refusing a width the server
//     demonstrably keeps.
//
// THE NUMERIC PAIR IS MEASURED SEPARATELY AND IS THE SHARP ROW. The server
// ACCEPTS [1, 1.5] and stores it by widening the long to a double, so an
// INT64|FLOAT64 union — wire-distinct, and therefore one gqlc admits — is the
// single heterogeneous array that does not fail loudly. It fails silently
// instead, which is worse: the value read back is no longer the value
// written. The arm refuses the whole width because of this row, so the row
// belongs here rather than in the commit message. Its assertions are on the
// READ-BACK, not on the write, because the write succeeding is the premise
// and the coercion is the finding.
//
// THE WORDING IS ASSERTED IN ITS OWN ROW, deliberately not folded into the
// refusal. The refusal row answers the fork; the wording row answers a much
// smaller question, and merging them would make one red mean either "the
// server changed its behaviour" or "the server changed its prose". The exact
// text is logged whatever happens, so the run carries its own evidence.
//
// The wording is NOT the map-valued probe's constant, and the two must not be
// merged into one shared string. Measured 2026-09-11, the same image answers
// a map property with "Property values can only be of primitive types or
// arrays thereof" and a heterogeneous array with "Neo4j only supports a
// subset of Cypher types for storage as singleton or array properties" —
// different rules, different messages, and a shared constant would have to be
// widened until it asserted nothing.
//
// ONE ARM ON THE NEO4J SIDE, not one per driver major: the rule is the
// store's, and v5 and v6 speak to the same store. The nested-list and
// map-valued files state the same reasoning and pay for the same single
// container. There is no AGE half here at all, and its absence is a
// statement: this bead asks what NEO4J does, because neo4j is the backend
// whose StorableProperty owed an answer. AGE's stays `return true` on its own
// measured footing.
//
// The name is spelled into the justfile recipe. go test's -run is unanchored
// and that alternation is a NAME LIST for that reason; a live test added here
// and not added there runs in no job at all and prints ok, which
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

const (
	// heterogeneousArrayWrite is the shape the arm refuses at generation
	// time, spelled as a literal so the server is not being asked about
	// gqlc's emission. BOOL and INT64 are two distinct wire families on this
	// driver — bool and int64 — which is what makes this the LIST<UNION<…>>
	// question rather than a question about one family.
	heterogeneousArrayWrite = "CREATE (:UnionListProbe {xs: [true, 1]})"

	// stringIntArrayWrite is the SIBLING PAIR. The arm is scoped to the
	// width, not to one member pairing, so a second family crossing is
	// measured rather than inferred from the first.
	stringIntArrayWrite = "CREATE (:UnionListProbe {ss: ['a', 1]})"

	// boolArrayWrite and intArrayWrite are the write-path controls: same
	// label, same session, one per member family of the row above. They are
	// what make the refusal a fact about the MIX.
	boolArrayWrite = "CREATE (:UnionListProbe {bs: [true, false]})"
	intArrayWrite  = "CREATE (:UnionListProbe {is: [1, 2]})"

	// bareUnionWrite is the scoping control. A union VALUE is a primitive
	// and is stored; only the array form is refused. This row is why the arm
	// sits in StorableProperty's list branch rather than at its top.
	bareUnionWrite = "CREATE (:UnionListProbe {u: true})"

	// numericArrayWrite is the one heterogeneous array the server accepts,
	// and numericArrayRead is what it accepts it as. The read-back is the
	// assertion; see the header.
	numericArrayWrite = "CREATE (:UnionListProbe {ns: [1, 1.5]})"
	numericArrayRead  = "MATCH (n:UnionListProbe) WHERE n.ns IS NOT NULL RETURN [x IN n.ns | valueType(x)]"

	// heterogeneousValueProjection is the storage-versus-value control. The
	// server composes and returns the identical mixed list; only the
	// property slot is in question.
	heterogeneousValueProjection = "RETURN [true, 1] AS xs"

	// heterogeneousArrayRefusal is the fragment of neo4j's answer that names
	// the rule rather than the statement. MEASURED 2026-09-11 against the
	// pinned image, Neo4j Kernel 5.26.28 community, which answered:
	//
	//	Neo4j only supports a subset of Cypher types for storage as
	//	singleton or array properties. Please refer to section
	//	cypher/syntax/values of the manual for more details.
	heterogeneousArrayRefusal = "only supports a subset of Cypher types for storage"

	// neo4jComponents reads the server's own version. gqlc-npus requires it
	// recorded because the answer is version-contingent, and a version taken
	// off the image tag is a claim about a download rather than about the
	// process that answered. It is logged rather than asserted: pinning it
	// would red on every image bump for a reason that is not this file's.
	neo4jComponents = "CALL dbms.components() YIELD name, versions, edition " +
		"RETURN name + ' ' + versions[0] + ' ' + edition"
)

// TestNeo4jRefusesAHeterogeneousArrayStoredProperty is bd gqlc-npus's
// measurement, and the tripwire under neo4j's LIST<UNION<…>> arm afterwards.
func TestNeo4jRefusesAHeterogeneousArrayStoredProperty(t *testing.T) {
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

	// The version the rest of this run is a fact about. Read from the server
	// rather than from the image tag, and read FIRST so it is in the log even
	// if a row below reds.
	version, err := readSingleValue(ctx, t, driver, neo4jComponents)
	require.NoError(t, err, "read the server's own version")
	t.Logf("measured against: %v", version)

	t.Run("a homogeneous BOOL array is stored", func(t *testing.T) {
		require.NoError(t, writeProperty(ctx, t, driver, boolArrayWrite),
			"the refusal below is only a fact about MIXED arrays if a single-family array "+
				"stores on this same session; without this row an unreachable server or a "+
				"server refusing every array property would confirm the arm for the wrong reason")
	})

	t.Run("a homogeneous INT array is stored", func(t *testing.T) {
		require.NoError(t, writeProperty(ctx, t, driver, intArrayWrite),
			"the OTHER member family of the refused row, on the same footing: neither "+
				"member may be the thing the server is objecting to")
	})

	t.Run("a bare union value is stored", func(t *testing.T) {
		require.NoError(t, writeProperty(ctx, t, driver, bareUnionWrite),
			"this row scopes the arm to the LIST form; if a union VALUE were refused too, "+
				"StorableProperty should refuse KindUnion at its top rather than in its list branch")
	})

	t.Run("the same mixed list is served as a projected column", func(t *testing.T) {
		value, err := readSingleValue(ctx, t, driver, heterogeneousValueProjection)
		require.NoError(t, err,
			"the refusal below must be about STORAGE, not about the server handling a mixed list at all")
		require.IsType(t, []any{}, value,
			"the projection must come back as a list, or this control witnesses nothing")
	})

	// The row that answers the fork. Its failure is a FINDING — the stores
	// branch — and the run's own log line carries what the server said.
	var refusal error
	t.Run("a heterogeneous array stored property is refused", func(t *testing.T) {
		refusal = writeProperty(ctx, t, driver, heterogeneousArrayWrite)
		if refusal != nil {
			t.Logf("the server's answer, verbatim: %v", refusal)
		}
		require.Error(t, refusal,
			"THIS RED IS THE MEASUREMENT, not a bug: this image STORES a heterogeneous array, "+
				"so gqlc-npus's fork takes its other branch — neo4j's LIST<UNION<…>> "+
				"StorableProperty arm must be DELETED and the width is storable like any other list")
	})

	t.Run("and the refusal names the storage rule", func(t *testing.T) {
		require.Error(t, refusal, "the row above already reported the finding; nothing to word-check")
		require.Contains(t, refusal.Error(), heterogeneousArrayRefusal,
			"the refusal must be the storage rule rather than a syntax error or a dropped "+
				"connection. If the row ABOVE passed and only this one reds, the server still "+
				"refuses and it is this file's predicted wording that is wrong — take the real "+
				"text from the verbatim log line above and correct the constant, do not widen "+
				"this row into one that any error satisfies")
	})

	// The sibling pair. Asserted to take the SAME refusal, wording checked in
	// the same breath rather than in a row of its own: this row answers no
	// fork, it only asks whether the rule the server just stated is about
	// mixing families in general or about the one pairing above.
	t.Run("a STRING and INT array is refused for the same reason", func(t *testing.T) {
		err := writeProperty(ctx, t, driver, stringIntArrayWrite)
		if err != nil {
			t.Logf("the server's answer to the string/int array, verbatim: %v", err)
		}
		require.Error(t, err,
			"the arm refuses the WIDTH, so a second family crossing must be refused too; if only "+
				"BOOL|INT64 is refused the rule is narrower than the arm and the arm is over-broad")
		require.Contains(t, err.Error(), heterogeneousArrayRefusal,
			"a different refusal means this row is measuring something other than the storage rule")
	})

	// The sharp row, and the reason the arm refuses the whole width. This one
	// asserts a SUCCESS and then a LOSS, so a server that started refusing
	// numerics would red at the write and a server that stopped coercing
	// would red at the read — and both are findings the arm depends on.
	t.Run("a numeric mixed array is stored, and the INT is widened to FLOAT", func(t *testing.T) {
		require.NoError(t, writeProperty(ctx, t, driver, numericArrayWrite),
			"if the server now REFUSES [1, 1.5], the numeric pair joins the loud refusals and "+
				"the arm's justification simplifies; the arm itself still stands")

		kinds, err := readSingleValue(ctx, t, driver, numericArrayRead)
		require.NoError(t, err, "read the stored numeric array's element types back")
		require.Equal(t, []any{"FLOAT NOT NULL", "FLOAT NOT NULL"}, kinds,
			"the INT64 that went in must come back a FLOAT for the arm's reasoning to hold: this is "+
				"why an INT64|FLOAT64 union list is refused at generation despite the server ACCEPTING "+
				"the write. If both elements come back INTEGER and FLOAT as declared, the server has "+
				"stopped coercing, this width round-trips faithfully, and the arm is over-broad by "+
				"exactly one member pairing")
	})
}
