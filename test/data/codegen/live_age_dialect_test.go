//go:build codegen_live

// The one dialect fact the Apache AGE backend's capability gate is built on.
//
// An edge column whose candidates carry DISTINCT labels is reachable in
// openCypher only through a relationship-type alternation — oC_RelationshipTypes
// admits a second type after '|' and nowhere else — and AGE's parser has no '|'
// in a relationship detail. The backend refuses such a column at generation
// (internal/codegen/age/errors.go, edgeUnionReason) rather than emit a label
// dispatch behind a statement the server will not parse. Candidates sharing a
// label are a different failure, refused for a reason no parser is party to by
// the shared admission this gate stands aside for; edgeUnionReason says which
// is which and why.
//
// That refusal is a claim about a server this repo pins by digest, so it is
// measured here on every live run instead of asserted in a comment. If AGE ever
// learns the alternation, this test goes red and the refusal is what should be
// reconsidered (gqlc-35yu.14).
//
// A live run is nightly on master and manual, not per-PR (.github/workflows/
// codegen-live.yml), so the measurement lags a change by up to a day. That is
// the right cadence for what this asserts. The subject is the pinned image,
// which no PR in this repo can alter except by editing the digest — and a PR
// that does is a PR about the image, where running the battery by hand is the
// obvious move. Nothing else a PR can touch changes AGE's parser. The risk the
// nightly covers is therefore a digest bump landing unmeasured, and it covers
// it within one cycle, with the refusal message naming SQLSTATE 42601 so that
// the failure reads as a dialect change rather than as flake. Per-PR would buy
// earlier notice of a fact that changes only when someone edits one line, at
// the price of a container start on every PR — which is the trade gqlc-35yu.8
// already made for this job.

package fixtures_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	entityedgeage "github.com/areqag/gqlc/test/data/codegen/valid/entity_edge_projected_one/golden/apache-age-pgx-v5"
)

// gqlcDelimiter is the dollar-quote tag every emitted AGE query method opens
// and closes its query text with. Naming it here is how this test splits a
// captured statement into the envelope the emission builds and the text the
// author wrote.
const gqlcDelimiter = "$gqlc$"

// alternationSQLSTATE is PostgreSQL's syntax_error. AGE's parser reports
// through the same channel as the server's own, so a refusal at parse time is
// indistinguishable to a caller from a malformed SQL statement — which is why a
// generated package built on an alternation could not recover from one.
const alternationSQLSTATE = "42601"

// TestAGERefusesRelationshipTypeAlternation measures, against the pinned AGE
// image, that the pattern an edge-union column requires does not parse — and
// that the patterns AGE does parse in its place carry no union.
//
// No statement here is composed by hand. A shipped generated method runs first
// and its SQL is captured off the wire; every probe is that same envelope — the
// E-quoted graph name, the dollar-quoted text, the bound argument, the record
// declaration — with only the author's query text substituted. A refusal is
// therefore the pattern's and not the harness's.
func TestAGERefusesRelationshipTypeAlternation(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	dsn := ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase)

	pool := openAGEPool(ctx, t, dsn, ageSessionInit)
	rec := &statementRecorder{}
	traced := openTracedAGEPool(ctx, t, dsn, rec)

	const graph = "gqlc_dialect"
	q := entityedgeage.New(pool, graph)
	require.NoError(t, q.EnsureGraph(ctx), "ensure graph %s", graph)
	t.Cleanup(func() { require.NoError(t, q.DropGraph(ctx), "drop graph %s", graph) })

	// Capture the envelope from a shipped method before anything is seeded:
	// the graph is empty, so the method reports no rows, and the statement it
	// issued to find that out is the one this test goes on to carry its probes
	// in.
	_, err := entityedgeage.New(traced, graph).OneActedIn(ctx)
	require.ErrorIs(t, err, entityedgeage.ErrNoRows, "an empty graph has no ACTED_IN edge")
	shipped := lastDollarQuotedStatement(t, rec)
	require.Contains(t, shipped, "MATCH (:Person)-[r:ACTED_IN]->(:Movie) RETURN r",
		"the captured statement must be the one carrying the fixture's query text")

	// Two edge types between the same endpoints. Only ACTED_IN is in the
	// fixture's schema; DIRECTED is here so a pattern naming both has two
	// labels to find, which is the state an edge-union column describes.
	seed := substituteQueryText(t, shipped,
		"CREATE (p:Person {id: 1}) CREATE (m:Movie {id: 2}) "+
			"CREATE (p)-[:ACTED_IN {since: 2019}]->(m) CREATE (p)-[:DIRECTED]->(m)")
	_, err = pool.Exec(ctx, seed, "{}")
	require.NoError(t, err, "seed the graph through the same envelope")

	for _, tc := range []struct {
		name string
		// text replaces the author's query text inside the shipped envelope.
		text string
		// sqlstate is the code the server must answer with; empty means the
		// statement must parse and return rows.
		sqlstate string
		// wantLabels is what the returned edges' labels must be when the
		// statement parses.
		wantLabels []string
	}{
		{
			name:     "relationship-type alternation",
			text:     "MATCH (:Person)-[r:ACTED_IN|DIRECTED]->(:Movie) RETURN r",
			sqlstate: alternationSQLSTATE,
		},
		{
			// The pre-openCypher-9 spelling of the same thing. Refusing one
			// and admitting the other would leave a shape the gate misses.
			name:     "legacy alternation with a repeated colon",
			text:     "MATCH (:Person)-[r:ACTED_IN|:DIRECTED]->(:Movie) RETURN r",
			sqlstate: alternationSQLSTATE,
		},
		{
			// The variable-length form the _list edge-union fixtures reach the
			// same leaf through.
			name:     "alternation under a variable-length pattern",
			text:     "MATCH (:Person)-[r:ACTED_IN|DIRECTED*]->(:Movie) RETURN r",
			sqlstate: alternationSQLSTATE,
		},
		{
			// The two patterns that do parse, and neither yields a union: gqlc
			// refuses an untyped edge at R0, and a single-typed one binds one
			// candidate. They are here for what they prove about the wire —
			// every edge carries its label, spelled as the schema spells it —
			// so what fails above is the statement and not the premise the
			// dispatch would have decoded on.
			name:       "untyped relationship",
			text:       "MATCH (:Person)-[r]->(:Movie) RETURN r",
			wantLabels: []string{"ACTED_IN", "DIRECTED"},
		},
		{
			name:       "single relationship type",
			text:       "MATCH (:Person)-[r:DIRECTED]->(:Movie) RETURN r",
			wantLabels: []string{"DIRECTED"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stmt := substituteQueryText(t, shipped, tc.text)
			// A refusal reaches the caller from Query itself: pgx's default
			// exec mode parses on the round trip Query makes, and a statement
			// AGE refuses can never have been cached. That is measured, not
			// assumed — asserting here rather than after the first Next is
			// what goes red if it stops holding.
			rows, err := pool.Query(ctx, stmt, "{}")
			if tc.sqlstate != "" {
				require.Error(t, err, "AGE must refuse this pattern")
				var pgErr *pgconn.PgError
				require.ErrorAs(t, err, &pgErr, "the refusal must be the server's")
				require.Equal(t, tc.sqlstate, pgErr.Code)
				require.Contains(t, pgErr.Message, `syntax error at or near "|"`,
					"the alternation's pipe is what the parser must name")
				return
			}
			require.NoError(t, err, "AGE must parse this pattern")
			defer rows.Close()

			var labels []string
			for rows.Next() {
				var raw []byte
				require.NoError(t, rows.Scan(&raw), "scan edge")
				labels = append(labels, edgeLabelOf(t, raw))
			}
			require.NoError(t, rows.Err())
			require.ElementsMatch(t, tc.wantLabels, labels,
				"every edge must carry its label spelled as the schema spells it")
		})
	}
}

// openTracedAGEPool is openAGEPool with a statement recorder attached, so a
// caller can read back the SQL the generated code sent rather than a copy of it
// written here that could drift from the emission.
func openTracedAGEPool(ctx context.Context, t *testing.T, dsn string, rec *statementRecorder) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err, "parse pool config")
	cfg.ConnConfig.Tracer = rec
	cfg.AfterConnect = ageSessionInit
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err, "construct traced pgx pool")
	t.Cleanup(pool.Close)
	return pool
}

// lastDollarQuotedStatement is the most recent statement the recorder saw that
// carries a query text between the emission's delimiters. SessionInit's two
// statements and the graph lifecycle statements are recorded too, so the query
// method's own statement is found by the delimiter rather than by position.
func lastDollarQuotedStatement(t *testing.T, rec *statementRecorder) string {
	t.Helper()
	stmts := rec.statements()
	for i := len(stmts) - 1; i >= 0; i-- {
		if strings.Count(stmts[i], gqlcDelimiter) == 2 {
			return stmts[i]
		}
	}
	require.FailNow(t, "no dollar-quoted cypher statement was recorded", "saw: %v", stmts)
	return ""
}

// substituteQueryText swaps the author's query text inside a captured
// statement, leaving every other part of the emission's envelope alone.
func substituteQueryText(t *testing.T, shipped, text string) string {
	t.Helper()
	opens := strings.Index(shipped, gqlcDelimiter)
	closes := strings.LastIndex(shipped, gqlcDelimiter)
	require.Greater(t, closes, opens, "the captured statement must open and close the delimiter")
	require.NotContains(t, text, gqlcDelimiter, "a probe must not close the delimiter it travels in")
	return shipped[:opens+len(gqlcDelimiter)] + text + shipped[closes:]
}

// edgeLabelOf reads the label out of an agtype edge value. AGE renders an edge
// as a JSON object suffixed with ::edge and the label is a plain string member,
// so finding it needs no more than the key and the quotes around its value —
// this is a probe, not the emitted decoder.
func edgeLabelOf(t *testing.T, raw []byte) string {
	t.Helper()
	s := string(raw)
	require.True(t, strings.HasSuffix(s, "::edge"), "expected an agtype edge, got %s", s)
	const key = `"label": "`
	i := strings.Index(s, key)
	require.GreaterOrEqual(t, i, 0, "an agtype edge must carry a label: %s", s)
	rest := s[i+len(key):]
	j := strings.Index(rest, `"`)
	require.GreaterOrEqual(t, j, 0, "the label must be a closed string: %s", s)
	return rest[:j]
}
