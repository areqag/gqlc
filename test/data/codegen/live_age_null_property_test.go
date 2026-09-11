//go:build codegen_live

// The measurement bd gqlc-3ohpo refused to let anyone settle from folklore:
// two helpers in every emitted AGE package answer the same question two ways.
// agtypeRecordField treats an absent key and an explicit agtype `null` as one
// thing — no value here; agtypeProperty and agtypeNullableProperty key on
// absence alone. That divergence was written down as deliberate-but-unmeasured,
// with the note that widening the property helpers to match would be answering
// an unmeasured question by assumption, in the direction that silently accepts
// more.
//
// WHAT THIS FILE HOLDS, AND WHAT IT DOES NOT. The PROPERTY half is measured
// next door: live_null_valued_property_test.go (bd gqlc-wc5j) walks every route
// a null can take onto a property on both backends and reddens if any of them
// ever leaves one stored. That is the whole of agtypeProperty's premise and it
// is not repeated here. This file holds the half gqlc-wc5j had no reason to
// ask: whether the RECORD FIELD position differs, and why.
//
// THE ANSWER is that the two positions have different REACHABILITY, so the two
// helpers are each right and the divergence is not an inconsistency:
//
//   - AT A VERTEX PROPERTY an explicit null is unreachable. Every route drops
//     the key at write time (measured next door), and the two routes that would
//     carry a whole caller-built map into a property slot — a parameter as a
//     CREATE property map, and SET n += $param — are REFUSED outright, which is
//     the row below. So absence really is the only shape a null property
//     arrives in, and there is no third door this file has not tried.
//   - AT A RECORD FIELD it is reachable, through the one door the property slot
//     does not have: a record is stored as ONE map value, and a map bound as a
//     query PARAMETER keeps its explicit null members, survives storage, and
//     comes back with them. So agtypeRecordField's null arm is load-bearing
//     rather than defensive.
//
// WHY THE PARAMETER ROW IS THE ONE THAT MATTERS. Every dropping row, here and
// next door, could be explained by "agtype cannot hold a null inside an object
// at all", which would make agtypeRecordField's arm dead code. "a map arriving
// as a parameter keeps its explicit null" refutes that with the bytes: the same
// object, arriving by parameter rather than by map literal, keeps the member,
// through storage and back. What drops nulls is Cypher's MAP LITERAL
// CONSTRUCTOR, not the storage format — and the record decoder reads a whole
// stored map, a position the constructor was never in the path of. The
// projection row above it is what pins the constructor as the agent: it drops
// the member on the way OUT too, where no storage is involved at all.
//
// The last row is the contrast that stops this being read as a fact about
// agtype objects: a stored LIST keeps its null elements, because a list has no
// key to drop. That is agtypeNullableElem's premise, and it would be dead code
// too if the dropping were the format's.
//
// Measured against apache/age 1.7.0 on PostgreSQL 18.1, the digest-pinned image
// the rest of this battery runs on. The raw bytes are logged on every row, so
// the run carries its own evidence rather than this comment.
//
// The name is spelled into the justfile recipe and into
// internal/liverecipes.LiveArms. go test's -run is unanchored and that
// alternation is a NAME LIST for that reason; a live test added here and not
// added there runs in no job at all and prints ok, which
// TestEveryLiveTestIsRunByARecipeThatNamesIt refuses.

package fixtures_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// TestAGEKeepsAnExplicitNullAtARecordFieldButNotAtAProperty is the measurement
// bd gqlc-3ohpo asks for, in the order the file comment sets out.
func TestAGEKeepsAnExplicitNullAtARecordFieldButNotAtAProperty(t *testing.T) {
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_record_field_null")

	t.Run("the two routes that would carry a caller-built map into a property slot are refused", func(t *testing.T) {
		// This is what makes the enumeration next door exhaustive rather than a
		// sample. Its table walks the routes that WORK and finds each dropping
		// the key; these two are the only spellings that would put a whole
		// caller-built map at a property slot without passing through the map
		// literal constructor, and the server declines both. Asserted rather
		// than logged, because on the day either is accepted the property
		// helpers' premise is gone and this row is what says so.
		for _, refused := range []struct {
			name, text string
		}{
			{"a parameter as a CREATE property map", `CREATE (z:R6 $props) RETURN z`},
			{"SET += a parameter map", `CREATE (n:R7 {keep: 1}) SET n += $props RETURN n`},
		} {
			t.Run(refused.name, func(t *testing.T) {
				_, err := pool.Exec(ctx, substituteQueryText(t, shipped, refused.text), `{"props": {"a": null, "b": 2}}`)
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
		// would not have settled the question on its own. It is also what names
		// the CONSTRUCTOR as the thing that drops, since nothing is stored here.
		got := recordNullProbeOne(ctx, t, pool, shipped, `RETURN {a: 1, b: null}`, "{}")
		require.JSONEq(t, `{"a": 1}`, got,
			"the map literal constructor drops a null member on the way out as well as in")

		nested := recordNullProbeOne(ctx, t, pool, shipped, `RETURN {outer: {a: null, b: 2}}`, "{}")
		require.JSONEq(t, `{"outer": {"b": 2}}`, nested,
			"and at depth, so a record-shaped literal loses it too")
	})

	t.Run("a map arriving as a parameter keeps its explicit null, stored and read back", func(t *testing.T) {
		// The row that makes agtypeRecordField's null arm reachable, and the
		// one that says what every dropping row is really about: the dropping
		// is the MAP LITERAL CONSTRUCTOR's, not the storage format's.
		projected := recordNullProbeOne(ctx, t, pool, shipped, `RETURN $m`, `{"m": {"a": null, "b": 2}}`)
		require.JSONEq(t, `{"a": null, "b": 2}`, projected,
			"a map that never passes through the literal constructor keeps its null member")

		recordNullProbeExec(ctx, t, pool, shipped, `CREATE (:R8 {addr: $m})`, `{"m": {"a": null, "b": 2}}`)
		stored := recordNullProbeOne(ctx, t, pool, shipped, `MATCH (n:R8) RETURN n.addr`, "{}")
		require.JSONEq(t, `{"a": null, "b": 2}`, stored,
			"and keeps it through storage, which is the exact shape agtypeRecordField reads: "+
				"an explicit null at a record member, in a map the decoder receives whole")
	})

	t.Run("a list element written null is kept, unlike a map member", func(t *testing.T) {
		// The contrast that stops the finding being read as "agtype cannot hold
		// a null anywhere". It is also the premise of agtypeNullableElem, which
		// decodeFunc's starred arms call and which would be dead code if a
		// stored list dropped its nulls the way a stored map does.
		recordNullProbeExec(ctx, t, pool, shipped, `CREATE (:R9 {xs: [1, null, 2]})`, "{}")
		got := recordNullProbeOne(ctx, t, pool, shipped, `MATCH (n:R9) RETURN n.xs`, "{}")
		require.JSONEq(t, `[1, null, 2]`, got,
			"a list keeps its null elements, positionally — a list has no key to drop")
	})
}

// recordNullProbeExec runs one write through the emission's own envelope and
// fails the test on refusal.
func recordNullProbeExec(ctx context.Context, t *testing.T, pool *pgxpool.Pool, shipped, text, params string) {
	t.Helper()
	_, err := pool.Exec(ctx, substituteQueryText(t, shipped, text), params)
	require.NoError(t, err, "run %s with %s", text, params)
}

// recordNullProbeOne runs a read expected to answer exactly one row of one
// column and returns its raw agtype bytes as text. The whole point of this file
// is what the server put on the wire, so nothing is decoded on the way.
//
// The callers compare that text with require.JSONEq rather than Equal. Every
// claim here is about which MEMBERS a value has — a dropped key, or a null one
// that survived — and JSONEq states that without also pinning agtype's spacing,
// which is the server's rendering choice and not a thing this file means to
// freeze. It still separates the two shapes that matter: an absent key and a
// key holding null unmarshal differently, so a drop cannot pass as a survival.
func recordNullProbeOne(ctx context.Context, t *testing.T, pool *pgxpool.Pool, shipped, text, params string) string {
	t.Helper()

	rows, err := pool.Query(ctx, substituteQueryText(t, shipped, text), params)
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
