//go:build codegen_live

// The measurement bd gqlc-3ohpo refused to let anyone settle from folklore:
// can an explicit agtype `null` arrive at a map key this backend's decoders
// read, and does the answer differ between a VERTEX PROPERTY and a RECORD
// FIELD?
//
// It matters because two helpers in every emitted AGE package answer the same
// question two ways. agtypeRecordField treats an absent key and an explicit
// null as one thing; agtypeProperty and agtypeNullableProperty key on absence
// alone. That divergence was written down as deliberate-but-unmeasured, with
// the note that widening the property helpers to match would be answering an
// unmeasured question by assumption, in the direction that silently accepts
// more.
//
// The answer is that the two positions have different REACHABILITY, so the
// two helpers are each right and the divergence is not an inconsistency:
//
//   - AT A VERTEX PROPERTY an explicit null is unreachable. Every route this
//     server offers drops the key at write time, and the two routes that
//     would carry a whole map of nulls into a property slot — a parameter as
//     a CREATE property map, and SET n += $param — are REFUSED outright. So
//     absence really is the only shape a null property arrives in.
//   - AT A RECORD FIELD it is reachable, through the one door the property
//     slot does not have: a map bound as a query PARAMETER keeps its explicit
//     null members, survives storage, and comes back with them. So
//     agtypeRecordField's null arm is load-bearing rather than defensive.
//
// WHY THE REFUSAL ROW IS NOT PADDING. The property claim is a universal —
// "no route delivers one" — and a universal is only as good as the
// enumeration under it. The first two rows walk the routes that work and find
// them all dropping; "the two routes that would carry a map of nulls into a
// property slot are refused" is what closes the two that would not have, and
// it is the row that reddens first if a later AGE release starts accepting a
// parameterised property map. On that day the property helpers' premise is
// gone and this file says so, rather than the emitted decoder quietly
// reporting a stored null as an absent key.
//
// WHY THE PARAMETER ROW IS THE INTERESTING ONE. Every dropping row could be
// explained by "agtype cannot hold a null inside an object at all", which
// would make agtypeRecordField's arm dead code. "a map arriving as a
// parameter keeps its explicit null" refutes that with the bytes: the same
// object, arriving by parameter rather than by map literal, keeps the member,
// through storage and back. What drops nulls is Cypher's MAP LITERAL
// CONSTRUCTOR, not the storage format — and the record decoder reads a whole
// stored map, a position the constructor was never in the path of.
//
// The last row is the contrast that stops this being read as a fact about
// agtype: a stored LIST keeps its null elements, because a list has no key to
// drop. That is agtypeNullableElem's premise, and it would be dead code too
// if the dropping were the format's.
//
// Measured against apache/age 1.7.0 on PostgreSQL 18.1, the digest-pinned
// image the rest of this battery runs on. The raw bytes are logged on every
// row, so the run carries its own evidence rather than this comment.
//
// The name is spelled into the justfile recipe and into
// internal/liverecipes.LiveArms. go test's -run is unanchored and that
// alternation is a NAME LIST for that reason; a live test added here and not
// added there runs in no job at all and prints ok, which
// TestEveryLiveTestIsRunByARecipeThatNamesIt refuses.

package fixtures_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// nullProbeGraph is this file's graph, named after the file so a parallel
// battery member cannot collide with it.
const nullProbeGraph = "gqlc_null_property"

// TestAGEDropsANullPropertyAndKeepsANullRecordField is the measurement bd
// gqlc-3ohpo asks for, in the order the file comment sets out.
func TestAGEDropsANullPropertyAndKeepsANullRecordField(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	pool := openAGEPool(ctx, t, ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase), ageSessionInit)

	execSQL(ctx, t, pool, `SELECT * FROM ag_catalog.create_graph('`+nullProbeGraph+`')`)
	t.Cleanup(func() {
		execSQL(ctx, t, pool, `SELECT * FROM ag_catalog.drop_graph('`+nullProbeGraph+`', true)`)
	})

	t.Run("a property written null is dropped, and its neighbour is not", func(t *testing.T) {
		// The neighbour is the write-path control. Without it a vertex
		// arriving with an empty property map would read as "the null was
		// dropped" when what happened is that nothing was stored at all.
		cypherExec(ctx, t, pool, `CREATE (:Lit {present: 'yes', absent: null})`)

		got := cypherOne(ctx, t, pool, `MATCH (n:Lit) RETURN n`)
		require.Contains(t, got, `"present": "yes"`,
			"the neighbouring property must survive, or this row is measuring a failed write")
		require.NotContains(t, got, "absent",
			"a property written null must not reach storage as a key at all; if it does, "+
				"agtypeProperty's absence-only reading is wrong and the emitted decoder will "+
				"report a stored null as a missing property")
	})

	t.Run("every route to a null property drops the key", func(t *testing.T) {
		// Five spellings of the same intent, because agtypeProperty's premise
		// is a universal over routes and one literal does not establish it.
		// The `keep` member on each is that row's own control, so a row that
		// stored nothing at all cannot read as a row whose null was dropped.
		//
		// The parameter row is the one that could not have been predicted
		// from the others: a parameter carries a value the query text never
		// spells, which is exactly how row 6 below gets a null INTO a stored
		// map. It drops here and survives there, and the difference is the
		// property slot.
		for _, route := range []struct {
			name, label, write, params string
		}{
			{"a null literal", "R1", `CREATE (:R1 {keep: 1, drop: null})`, "{}"},
			{"a null from a function", "R2", `CREATE (:R2 {keep: 1, drop: head([])})`, "{}"},
			{"a null from a failed coercion", "R3", `CREATE (:R3 {keep: 1, drop: toInteger('abc')})`, "{}"},
			{"SET to null after the write", "R4", `CREATE (n:R4 {keep: 1, drop: 2}) SET n.drop = null`, "{}"},
			{"a null bound as a scalar parameter", "R5", `CREATE (:R5 {keep: 1, drop: $v})`, `{"v": null}`},
		} {
			t.Run(route.name, func(t *testing.T) {
				cypherExecParams(ctx, t, pool, route.write, route.params)
				got := cypherOne(ctx, t, pool, `MATCH (n:`+route.label+`) RETURN properties(n)`)
				require.Equal(t, `{"keep": 1}`, got,
					"the control member must survive and the null member must not reach storage as a key")
			})
		}
	})

	t.Run("the two routes that would carry a map of nulls into a property slot are refused", func(t *testing.T) {
		// This is what makes the enumeration above exhaustive rather than a
		// sample: these are the only spellings that put a whole caller-built
		// map at a property slot without passing through the map literal
		// constructor, and the server declines both. The day either is
		// accepted, agtypeProperty's premise needs re-measuring.
		for _, refused := range []struct {
			name, text string
		}{
			{"a parameter as a CREATE property map", `CREATE (z:R6 $props) RETURN z`},
			{"SET += a parameter map", `CREATE (n:R7 {keep: 1}) SET n += $props RETURN n`},
		} {
			t.Run(refused.name, func(t *testing.T) {
				_, err := pool.Exec(ctx, cypherStatement(refused.text), `{"props": {"a": null, "b": 2}}`)
				require.Error(t, err,
					"if this server has started accepting %s, a stored explicit null can reach a "+
						"vertex property and agtypeProperty / agtypeNullableProperty must be widened "+
						"to read it as absence (bd gqlc-3ohpo)", refused.name)
				t.Logf("%s is refused: %v", refused.name, err)
			})
		}
	})

	t.Run("a projected map literal drops its null member", func(t *testing.T) {
		// The projection path, which is the one the record work had reason to
		// doubt: neo4j projects maps it will not store, so "storage drops it"
		// would not have settled the question on its own.
		got := cypherOne(ctx, t, pool, `RETURN {a: 1, b: null}`)
		require.Equal(t, `{"a": 1}`, got,
			"the map literal constructor drops a null member on the way out as well as in")

		nested := cypherOne(ctx, t, pool, `RETURN {outer: {a: null, b: 2}}`)
		require.Equal(t, `{"outer": {"b": 2}}`, nested,
			"and at depth, so a record-shaped literal loses it too")
	})

	t.Run("a map arriving as a parameter keeps its explicit null, stored and read back", func(t *testing.T) {
		// The row that makes agtypeRecordField's null arm reachable, and the
		// one that says what the rows above are really about: the dropping is
		// the MAP LITERAL CONSTRUCTOR's, not the storage format's.
		projected := cypherOneParams(ctx, t, pool, `RETURN $m`, `{"m": {"a": null, "b": 2}}`)
		require.Equal(t, `{"a": null, "b": 2}`, projected,
			"a map that never passes through the literal constructor keeps its null member")

		cypherExecParams(ctx, t, pool, `CREATE (:R8 {addr: $m})`, `{"m": {"a": null, "b": 2}}`)
		stored := cypherOne(ctx, t, pool, `MATCH (n:R8) RETURN n.addr`)
		require.Equal(t, `{"a": null, "b": 2}`, stored,
			"and keeps it through storage, which is the exact shape agtypeRecordField reads: "+
				"an explicit null at a record member, in a map the decoder receives whole")
	})

	t.Run("a list element written null is kept, unlike a map member", func(t *testing.T) {
		// The contrast that stops the finding being read as "agtype cannot
		// hold a null anywhere". It is also the premise of agtypeNullableElem,
		// which decodeFunc's starred arms call and which would be dead code if
		// a stored list dropped its nulls the way a stored map does.
		cypherExec(ctx, t, pool, `CREATE (:R9 {xs: [1, null, 2]})`)
		got := cypherOne(ctx, t, pool, `MATCH (n:R9) RETURN n.xs`)
		require.Equal(t, `[1, null, 2]`, got,
			"a list keeps its null elements, positionally — a list has no key to drop")
	})
}

// cypherStatement wraps probe text in the ag_catalog.cypher envelope with a
// parameter placeholder. The dollar-quote tag is this file's own, so a probe
// containing $$ would not close it; probes here contain none, and the
// requirement is asserted rather than assumed.
func cypherStatement(text string) string {
	return `SELECT * FROM ag_catalog.cypher('` + nullProbeGraph + `', $nullprobe$ ` + text +
		` $nullprobe$, $1) AS (v ag_catalog.agtype)`
}

// execSQL runs one plain SQL statement and fails the test on refusal.
func execSQL(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err, "run %s", sql)
}

func cypherExec(ctx context.Context, t *testing.T, pool *pgxpool.Pool, text string) {
	t.Helper()
	cypherExecParams(ctx, t, pool, text, "{}")
}

func cypherExecParams(ctx context.Context, t *testing.T, pool *pgxpool.Pool, text, params string) {
	t.Helper()
	require.NotContains(t, text, "$nullprobe$", "a probe must not close the delimiter it travels in")
	_, err := pool.Exec(ctx, cypherStatement(text), params)
	require.NoError(t, err, "run %s with %s", text, params)
}

func cypherOne(ctx context.Context, t *testing.T, pool *pgxpool.Pool, text string) string {
	t.Helper()
	return cypherOneParams(ctx, t, pool, text, "{}")
}

// cypherOneParams runs a read expected to answer exactly one row of one
// column and returns its raw agtype bytes as text. The whole point of this
// file is what the server put on the wire, so nothing is decoded on the way.
func cypherOneParams(ctx context.Context, t *testing.T, pool *pgxpool.Pool, text, params string) string {
	t.Helper()
	require.NotContains(t, text, "$nullprobe$", "a probe must not close the delimiter it travels in")

	rows, err := pool.Query(ctx, cypherStatement(text), params)
	require.NoError(t, err, "run %s with %s", text, params)
	defer rows.Close()

	var out []string
	for rows.Next() {
		var raw []byte
		require.NoError(t, rows.Scan(&raw), "scan the agtype value")
		out = append(out, string(raw))
	}
	require.NoError(t, rows.Err())
	require.Len(t, out, 1, "%s answered %d rows", text, len(out))

	t.Logf("%s => %s", text, out[0])
	return out[0]
}
