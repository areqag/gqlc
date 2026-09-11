//go:build codegen_live

// The agtype corpus fixture's provenance claim, run against the server it
// names instead of asserted in its header.
//
// internal/codegen/age/testdata/corpus_test.go.txt opens by saying its
// literals "were captured from apache/age 1.7.0 and cross-checked
// byte-for-byte against what pgx v5 scans". Every decode helper the AGE
// backend emits is exercised only against those literals, so the whole
// decode battery is worth exactly what that sentence is worth — and until
// this file landed the sentence was worth nothing mechanical. It was true
// when written and had drifted since: five captures (alice, bob, anyTag,
// slotOK, slotNulls) carried property keys in an order no AGE server
// emits, because a hand edit that adds a property puts it where the author
// is looking rather than where jsonb would sort it. Nothing could see that,
// because the decoders scan by key and pass either way. What it costs is
// the corpus's standing as evidence: a decoder that only worked on
// author-ordered text would have passed the whole battery (bd gqlc-6308).
//
// WHAT IS ASSERTED. Every capture is written into a live graph on the
// pinned image and read back, and the bytes pgx hands over must equal the
// fixture's literal EXACTLY — including the graphid, which is why each
// graph allocates its labels in a fixed order. Byte equality rather than
// structural equality is the point: structural equality is what the
// decoders already give, and it cannot see the key order that drifted.
//
// GRAPHIDS ARE NOT COSMETIC HERE. AGE builds one as (label_id << 48) |
// sequence, label ids are handed out per graph in create_vlabel /
// create_elabel call order, and user labels start at 3 because 1 and 2 are
// AGE's own _ag_label_vertex and _ag_label_edge. So the id in a capture
// pins the label's ordinal and the node's ordinal at once, and reproducing
// it means allocating the same labels in the same order — padding included,
// which is what the AgtypePad labels in the temporals graph are for.
// Dropping the id from the comparison would have been easier and would have
// thrown away the only part of the text that witnesses WHICH write produced
// it.
//
// THE TABLES ARE IN agtype_capture_census_test.go, not here, because that
// file is untagged: a census that has to notice a literal nobody witnessed
// cannot be compiled out of the runs where the omission happens. Every
// literal is in one of its two tables, the live one this test drives or the
// synthetic one, which carries the reason each hand-written literal is
// written rather than measured.
//
// WHY ITS OWN FILE, AND NOT live_age_dialect_test.go: that file is swept by
// TestEveryDialectGapCarriesItsWitness, which reads gap witnesses out of
// the bodies of the tests its gap table names, and a probe belonging to no
// dialect gap sitting in there is text a witness could be mistaken for.
//
// COST. One container, shared by both tests in this file, and three graphs
// on it. The AGE arm is PR-blocking (bd gqlc-ezwae) and carries -count=1,
// so this pays a container boot on every pull request; it is the same
// container shape the arm already boots eleven of. Both test names are
// spelled into the justfile's test-codegen-live-age recipe and into
// liverecipes.LiveArms — go test's -run is unanchored and that recipe's
// alternation is a name list, so a live test added here and not added there
// runs in no job at all.

package fixtures_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// TestAGEAgtypeCaptureMatchesTheServer reproduces every fixture capture on a
// live apache/age 1.7.0 and compares the bytes pgx scans against the
// fixture's literal.
//
// One container for all three graphs. The graphs are sequential rather than
// parallel because each one's ids depend on its own label allocation order
// and on nothing else, so they are already isolated; running them
// concurrently would buy a second or two and cost the reader the guarantee
// that a failing id belongs to the write above it.
func TestAGEAgtypeCaptureMatchesTheServer(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	pool := agtypeProbePool(ctx, t)

	for _, g := range agtypeLiveCaptures {
		_, err := pool.Exec(ctx, fmt.Sprintf("SELECT create_graph(%s)", pgQuote(g.graph)))
		require.NoError(t, err, "create graph %s", g.graph)
		for _, l := range g.labels {
			fn := "create_vlabel"
			if l.edge {
				fn = "create_elabel"
			}
			_, err := pool.Exec(ctx, fmt.Sprintf("SELECT %s(%s, %s)", fn, pgQuote(g.graph), pgQuote(l.name)))
			require.NoError(t, err, "%s(%s, %s)", fn, g.graph, l.name)
		}

		for _, c := range g.captures {
			t.Run(c.constName, func(t *testing.T) {
				got := agtypeOneValue(ctx, t, pool, g.graph, c.cypher)
				require.Equal(t, c.want, got,
					"the fixture's %s literal must be what this server emits, byte for byte; "+
						"a difference here is the corpus describing a wire form apache/age 1.7.0 "+
						"does not produce", c.constName)
			})
		}
	}
}

// agtypeProbePool connects as the superuser. Every other AGE test here logs
// in as the least-privilege gqlc_app role on purpose, because what they
// measure is emitted code that will be deployed as one. This file measures
// the SERVER's rendering of a value and composes DDL no emitted client
// issues — create_vlabel, and graphs it names itself — so the app role
// would be testing this file's own privilege bookkeeping and nothing about
// agtype.
func agtypeProbePool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()
	endpoint := startAGEContainer(ctx, t)
	return openAGEPool(ctx, t, ageDSN(endpoint, ageSuperuser, ageSuperPassword, ageDatabase), ageSessionInit)
}

// agtypeOneValue runs one cypher statement that must return exactly one row
// of exactly one column, and returns the raw bytes.
//
// RawValues rather than Scan into a string: a decoder is what this file is
// evidence FOR, so reading the value through one would make the comparison
// circular. RawValues is also what tells SQL NULL apart from the two-byte
// agtype string "" — nil versus a length-zero slice — which the null test
// below turns on.
func agtypeOneValue(ctx context.Context, t *testing.T, pool *pgxpool.Pool, graph, cypher string) string {
	t.Helper()
	raw, isNull := agtypeOneRaw(ctx, t, pool, graph, cypher)
	require.False(t, isNull, "this probe's value must not be SQL NULL: %s", cypher)
	return string(raw)
}

// agtypeOneRaw is agtypeOneValue without the not-null requirement: it
// reports whether the single column came back as SQL NULL, which is a state
// no agtype text can represent.
func agtypeOneRaw(ctx context.Context, t *testing.T, pool *pgxpool.Pool, graph, cypher string) ([]byte, bool) {
	t.Helper()
	stmt := fmt.Sprintf("SELECT * FROM cypher(%s, $gqlcprobe$ %s $gqlcprobe$) AS (v agtype)", pgQuote(graph), cypher)
	rows, err := pool.Query(ctx, stmt)
	require.NoError(t, err, "run %s", stmt)
	defer rows.Close()

	require.True(t, rows.Next(), "expected one row from %s", cypher)
	vals := rows.RawValues()
	require.Len(t, vals, 1, "expected one column from %s", cypher)
	// RawValues' backing array belongs to the connection and is reused on
	// the next row, so the copy is not tidiness.
	var out []byte
	isNull := vals[0] == nil
	if !isNull {
		out = append(out, vals[0]...)
	}
	require.False(t, rows.Next(), "expected exactly one row from %s", cypher)
	require.NoError(t, rows.Err())
	return out, isNull
}

// pgQuote renders a literal for a statement this file composes. The inputs
// are all constants declared above, so this exists to keep the composition
// honest rather than to defend against an input; it refuses rather than
// escapes, because a quote reaching it means a table above grew a value
// that does not belong in an identifier position.
func pgQuote(s string) string {
	for _, r := range s {
		if r == '\'' || r == '\\' {
			panic("pgQuote: graph and label names here are plain identifiers: " + s)
		}
	}
	return "'" + s + "'"
}

// TestAGEAgtypeNullReachesPgxAsSQLNULL is the durable witness for the claim
// that a null agtype COLUMN arrives as SQL NULL rather than as the four
// bytes "null" (bd gqlc-13ph).
//
// It was measured once through psql, where every value is text and SQL NULL
// and the string "null" are both rendered as something a reader has to
// interpret; that measurement is what gqlc-tez0 struck a wrong second shape
// on. At the pgx layer the two are not ambiguous at all — RawValues holds
// nil for one and a four-byte slice for the other — so this asserts on nil
// and keeps a control that returns real text beside it, because a probe
// that reported NULL for everything would pass every null row on its own.
//
// Three routes reach a null column and they are not the same route: a
// property the writer never set, a property explicitly SET to null, and a
// RETURN of the null literal with no property involved. AGE's storage rule
// collapses the first two — a null property is DROPPED rather than stored —
// and it is worth having both rows precisely because that is a rule that
// could change without the third row noticing.
func TestAGEAgtypeNullReachesPgxAsSQLNULL(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	pool := agtypeProbePool(ctx, t)
	const graph = "agtype_nulls"
	_, err := pool.Exec(ctx, fmt.Sprintf("SELECT create_graph(%s)", pgQuote(graph)))
	require.NoError(t, err, "create graph %s", graph)

	_, err = pool.Exec(ctx, fmt.Sprintf(
		"SELECT * FROM cypher(%s, $gqlcprobe$ CREATE (n:NullProbe {id: 1, present: 'here'}) $gqlcprobe$) AS (v agtype)",
		pgQuote(graph)))
	require.NoError(t, err, "seed the null probe node")

	for _, tc := range []struct {
		name     string
		cypher   string
		wantNull bool
		wantText string
	}{
		{
			name:     "a property the writer never set",
			cypher:   `MATCH (n:NullProbe {id: 1}) RETURN n.absent`,
			wantNull: true,
		},
		{
			name:     "a property SET to null, which AGE stores by dropping",
			cypher:   `MATCH (n:NullProbe {id: 1}) SET n.present = null RETURN n.present`,
			wantNull: true,
		},
		{
			name:     "the null literal returned with no property involved",
			cypher:   `RETURN null`,
			wantNull: true,
		},
		{
			// The control. Without it a connection answering nil to
			// everything would pass all three rows above.
			name:     "a property holding text, which must not read as null",
			cypher:   `MATCH (n:NullProbe {id: 1}) RETURN n.id`,
			wantText: "1",
		},
		{
			// The other control, and the one that says WHY nil is the
			// discriminator: an empty agtype string is two bytes, not zero
			// and not nil, so a probe that tested len() instead of nil
			// would call this null.
			name:     "an empty agtype string, which is two bytes and not null",
			cypher:   `RETURN ''`,
			wantText: `""`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, isNull := agtypeOneRaw(ctx, t, pool, graph, tc.cypher)
			if tc.wantNull {
				require.True(t, isNull,
					"a null agtype column must reach pgx as SQL NULL, not as the text %q", string(raw))
				return
			}
			require.False(t, isNull, "this row's value is not null")
			require.Equal(t, tc.wantText, string(raw))
		})
	}
}
