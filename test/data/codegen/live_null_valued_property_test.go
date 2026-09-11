//go:build codegen_live

// Measurement for bd gqlc-wc5j, which is a QUESTION and not a bug report.
// See the answer recorded at writeShapelessFieldDecode in
// internal/codegen/neo4j/render_models.go.
//
// BOTH BACKENDS ARE HERE because both emissions gate on presence alone, and
// each does so on a claim about its own server that nothing in this repository
// could hold. The neo4j claim is stated at writeShapelessFieldDecode; the AGE
// claim was already written, at agtypeProperty in internal/codegen/age's
// render_models.go — "AGE drops a property whose value is null, so an absent
// key is how a null arrives" — and until this file nothing measured it.

package fixtures_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	neo4jv5 "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"
	"github.com/stretchr/testify/require"
)

// nullValuedPropertyKey is the property every row below tries to make null.
const nullValuedPropertyKey = "p"

// nullWrite is one way a caller can try to put a null on a property.
//
// One table serves both backends so that the two answers are answers to the
// same enumerated question. The parameters are carried twice because the two
// transports spell them differently and neither can be derived from the other:
// Bolt takes a Go map the driver packs, AGE takes one agtype argument whose
// text is JSON. A row with no parameters leaves both zero.
type nullWrite struct {
	name   string
	cypher string
	params map[string]any
	// ageParams is the agtype argument this row's statement travels with,
	// empty meaning the no-parameter "{}".
	ageParams string
}

// nullWrites is every route a null can take onto a property that this
// project's emissions can produce: a literal in the CREATE map, SET on the
// key, SET += and SET = with a map, and each of the last three through a
// bound parameter rather than a literal. Parameters are listed separately
// because the literal forms are folded by the planner and a bound null is
// not — a server could plausibly treat them differently and only one of the
// two is what gqlc emits.
var nullWrites = []nullWrite{
	{
		name:   "a literal null in the CREATE property map",
		cypher: "CREATE (n:NullProbe {id: 1, p: null})",
	},
	{
		name:   "SET the key to a literal null after creating it with a value",
		cypher: "CREATE (n:NullProbe {id: 1, p: 'present'}) SET n.p = null",
	},
	{
		name:   "SET += a map whose value is a literal null",
		cypher: "CREATE (n:NullProbe {id: 1, p: 'present'}) SET n += {p: null}",
	},
	{
		name:   "SET = a map whose value is a literal null",
		cypher: "CREATE (n:NullProbe {id: 1, p: 'present'}) SET n = {id: 1, p: null}",
	},
	{
		name:      "a bound null in the CREATE property map",
		cypher:    "CREATE (n:NullProbe {id: 1, p: $p})",
		params:    map[string]any{"p": nil},
		ageParams: `{"p": null}`,
	},
	{
		name:      "SET the key to a bound null",
		cypher:    "CREATE (n:NullProbe {id: 1, p: 'present'}) SET n.p = $p",
		params:    map[string]any{"p": nil},
		ageParams: `{"p": null}`,
	},
	{
		name:      "SET += a bound map whose value is null",
		cypher:    "CREATE (n:NullProbe {id: 1, p: 'present'}) SET n += $m",
		params:    map[string]any{"m": map[string]any{"p": nil}},
		ageParams: `{"m": {"p": null}}`,
	},
}

// TestNeo4jNeverHandsBackANullValuedProperty is the neo4j half of gqlc-wc5j's
// question, and it is the tripwire under an ANSWER rather than the gate under
// a defect.
//
// THE QUESTION. writeShapelessFieldDecode's non-nullable arm gates on the
// Props map holding the key and then assigns whatever is under it:
//
//	value0, ok := node.Props["badge"]
//	if !ok {
//	        return Ev{}, fmt.Errorf("decode Ev.Badge: could not find any property named %s", "badge")
//	}
//	out.Badge = value0
//
// There is no nil check, so a key PRESENT with a nil value would deliver nil
// out of a property declared ANY VALUE NOT NULL, beside a nil error. `any` is
// the one emitted width inhabited by nil, which is why this shape exists here
// and nowhere else — bd gqlc-tez0 closed the identical hole one axis down, in
// the COLUMN path, where a null genuinely does arrive.
//
// THE ANSWER, and why this file holds no new gate. On this server a property
// has no null state: every route below either removes the key or refuses the
// statement, so the `!ok` refusal above is already the whole of the answer and
// the missing nil check has no input to check. A gate added for it would be
// dead code, and dead code in a decoder is worse than none — it reads as
// evidence that the state occurs.
//
// THIS ROW IS WHAT MAKES THAT SAFE TO RELY ON. The claim is the SERVER's, not
// gqlc's, so nothing in this repository can hold it and nothing about a future
// image bump would announce it. If neo4j ever stores a null-valued property,
// this row goes red naming the route that produced one, and the comment in
// render_models.go stops being true in the same breath. That is the answer
// gqlc-wc5j's acceptance asks for on the (b) branch: a comment naming what
// makes the state unreachable, plus the live row that would go red if it ever
// became reachable.
//
// The observation is the NODE, not a column: it is Props that the decoder
// reads, and a null column is a different question that tez0 already answered.
//
// ONE ARM. The rule is the server's, so v5 and v6 would measure one fact
// twice at the price of a second driver. Not parallel: it boots a container
// of its own and the neo4j half already boots four concurrently, so go test
// runs it before them and the peak is unchanged.
func TestNeo4jNeverHandsBackANullValuedProperty(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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

	// The control first. Every row below passes on a server that stored no
	// property at all, or that lost this one; this is what says the key can
	// be present and carry a value, so an absent key in the rows means the
	// null removed it.
	t.Run("the control: a property with a value is present and carries it", func(t *testing.T) {
		node := writeAndReadNullProbe(ctx, t, driver, nullWrite{
			cypher: "CREATE (n:NullProbe {id: 1, p: 'present'})",
		})
		value, ok := node.Props[nullValuedPropertyKey]
		require.True(t, ok,
			"the probe cannot tell absence from a server that stores nothing unless a written property is readable")
		require.Equal(t, "present", value)
	})

	for _, w := range nullWrites {
		t.Run(w.name, func(t *testing.T) {
			node := writeAndReadNullProbe(ctx, t, driver, w)

			value, present := node.Props[nullValuedPropertyKey]
			t.Logf("route %q left the key present=%v value=%#v", w.name, present, value)

			require.False(t, present && value == nil,
				"this server now hands back a node whose Props holds %q with a nil value. "+
					"writeShapelessFieldDecode's non-nullable arm gates on presence alone and would deliver that nil "+
					"out of a property declared ANY VALUE NOT NULL, beside a nil error — the shape bd gqlc-tez0 closed "+
					"one axis down. The comment there says this state is unreachable; it no longer is, and the lane "+
					"owes the same refusal in the same words (bd gqlc-wc5j)",
				nullValuedPropertyKey)
		})
	}
}

// writeAndReadNullProbe wipes the graph, runs one write, and returns the node
// it left behind.
//
// A write REFUSED by the server is an answer to the question and not a
// failure of the probe: it is one more route by which a null does not become
// a stored property. It is reported rather than swallowed, and the row then
// reads an absent node — so this returns the zero node and the caller's
// presence check is false, which is the correct verdict for a statement that
// wrote nothing.
func writeAndReadNullProbe(ctx context.Context, t *testing.T, driver neo4jv5.DriverWithContext, w nullWrite) dbtype.Node {
	t.Helper()

	_, err := runNullProbeCypher(ctx, t, driver, wipeCypher, nil)
	require.NoError(t, err, "wipe the graph before the row writes its own answer")

	if _, err := runNullProbeCypher(ctx, t, driver, w.cypher, w.params); err != nil {
		t.Logf("the server refused this route outright, which is itself a way a null does not become a property: %v", err)
		return dbtype.Node{}
	}

	records, err := runNullProbeCypher(ctx, t, driver, "MATCH (n:NullProbe) RETURN n", nil)
	require.NoError(t, err, "read the probe node back")
	if len(records) == 0 {
		return dbtype.Node{}
	}
	require.Len(t, records, 1, "each row writes exactly one probe node")

	node, _, err := neo4jv5.GetRecordValue[dbtype.Node](records[0], "n")
	require.NoError(t, err, "the probe projects a whole node, which is what the entity decoder reads")
	return node
}

// runNullProbeCypher runs one statement in its own auto-commit transaction.
//
// Not ExecuteWrite: a probe whose subject includes a possible REFUSAL should
// not run through machinery whose job is to try refusals again. The error can
// surface at Run or at Collect depending on when the server answers, so both
// are returned.
func runNullProbeCypher(ctx context.Context, t *testing.T, driver neo4jv5.DriverWithContext, cypher string, params map[string]any) ([]*neo4jv5.Record, error) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jv5.SessionConfig{AccessMode: neo4jv5.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Logf("close the session the probe ran in: %v", err)
		}
	}()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return result.Collect(ctx)
}

// TestAGENeverHandsBackANullValuedProperty is the AGE half of gqlc-wc5j's
// question, and it differs from the neo4j half in one way worth stating: the
// claim it pins was ALREADY WRITTEN when this file landed.
//
// agtypeProperty, in internal/codegen/age's render_models.go, refuses on an
// absent key and on nothing else, and its doc comment says why that is the
// whole refusal: "AGE drops a property whose value is null, so an absent key
// is how a null arrives". Everything downstream depends on it — agtypeValue
// maps a raw `null` to (nil, nil), so a null that DID survive as a stored
// property would reach a caller as a nil `any` out of a field the schema
// declares NOT NULL, beside a nil error. That is the same shape as the neo4j
// arm above and the same shape bd gqlc-tez0 closed in the COLUMN path.
//
// So the comment was load-bearing and unwitnessed. This row is the witness.
// If AGE ever stores a null-valued property, the sentence at agtypeProperty
// stops being true and this goes red naming the route that did it.
//
// WHY IT CANNOT BE INFERRED FROM THE NEO4J ARM. AGE is a different server and
// agtype is JSON, where null is a member value a map can genuinely hold —
// there is no representational reason a property could not be one, which is
// exactly why the neo4j answer does not carry over and both halves are here.
func TestAGENeverHandsBackANullValuedProperty(t *testing.T) {
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_null_valued_property")

	// The control first, for the same reason the neo4j arm has one: every row
	// below passes against a server that stored no property at all.
	t.Run("the control: a property with a value is present and carries it", func(t *testing.T) {
		props := writeAndReadAGENullProbe(ctx, t, pool, shipped, nullWrite{
			cypher: "CREATE (n:NullProbe {id: 1, p: 'present'})",
		})
		raw, ok := props[nullValuedPropertyKey]
		require.True(t, ok,
			"the probe cannot tell absence from a server that stores nothing unless a written property is readable")
		require.JSONEq(t, `"present"`, string(raw))
	})

	for _, w := range nullWrites {
		t.Run(w.name, func(t *testing.T) {
			props := writeAndReadAGENullProbe(ctx, t, pool, shipped, w)

			raw, present := props[nullValuedPropertyKey]
			// %q, not %s: a json.RawMessage that is ABSENT is nil, and fmt
			// renders a nil one as the text `null` — the exact word whose
			// presence is the finding. Under %s an absent key and a stored
			// null log identically, so the line a reader would take as the
			// evidence cannot tell them apart. Quoting separates them: absent
			// reads "" and a stored null reads "null".
			t.Logf("route %q left the key present=%v raw=%q", w.name, present, string(raw))

			require.False(t, present && string(raw) == "null",
				"this server now stores %q with a null value. agtypeProperty refuses an absent key and "+
					"nothing else, on the stated grounds that AGE drops a null-valued property; that sentence "+
					"is now false, and agtypeValue maps this raw null to (nil, nil), so a field declared NOT "+
					"NULL would reach the caller nil beside a nil error (bd gqlc-wc5j)",
				nullValuedPropertyKey)
		})
	}
}

// writeAndReadAGENullProbe wipes the probe label, runs one write, and returns
// the properties of the vertex it left behind.
//
// As in the neo4j arm, a REFUSED write is an answer and not a failure of the
// probe: it is one more route by which a null does not become a stored
// property. It is logged and read back as an absent vertex, which is the
// correct verdict for a statement that wrote nothing.
func writeAndReadAGENullProbe(ctx context.Context, t *testing.T, pool *pgxpool.Pool, shipped string, w nullWrite) map[string]json.RawMessage {
	t.Helper()

	_, err := pool.Exec(ctx, substituteQueryText(t, shipped, "MATCH (n:NullProbe) DETACH DELETE n"), "{}")
	require.NoError(t, err, "wipe the probe label before the row writes its own answer")

	params := w.ageParams
	if params == "" {
		params = "{}"
	}
	if _, err := pool.Exec(ctx, substituteQueryText(t, shipped, w.cypher), params); err != nil {
		t.Logf("the server refused this route outright, which is itself a way a null does not become a property: %v", err)
		return nil
	}

	rows, err := pool.Query(ctx, substituteQueryText(t, shipped, "MATCH (n:NullProbe) RETURN n"), "{}")
	require.NoError(t, err, "read the probe vertex back")
	defer rows.Close()

	var vertices [][]byte
	for rows.Next() {
		var raw []byte
		require.NoError(t, rows.Scan(&raw), "scan the agtype vertex")
		vertices = append(vertices, raw)
	}
	require.NoError(t, rows.Err())
	if len(vertices) == 0 {
		return nil
	}
	require.Len(t, vertices, 1, "each row writes exactly one probe vertex")
	return ageVertexProperties(t, vertices[0])
}

// ageVertexProperties reads the properties object out of an agtype vertex.
//
// Parsed by hand rather than through the emitted agtypeEntity: what this file
// measures is what the SERVER stored, so reading it back through the decoder
// whose refusal is under question would let a decoder bug hide the very state
// the row exists to find. The members are left as raw JSON because that is the
// width agtypeProperty hands to its decoder, and `null` is exactly the text
// whose presence is the finding.
func ageVertexProperties(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	const suffix = "::vertex"
	body := strings.TrimSpace(string(raw))
	require.True(t, strings.HasSuffix(body, suffix), "expected an agtype vertex, got %s", body)

	var vertex struct {
		Label      string                     `json:"label"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSuffix(body, suffix)), &vertex),
		"an agtype vertex is a JSON object: %s", body)
	require.Equal(t, "NullProbe", vertex.Label, "the probe reads back its own vertex")
	return vertex.Properties
}
