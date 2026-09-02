//go:build codegen_live

// The dialect facts the Apache AGE backend's text gate is built on.
//
// internal/codegen/age/dialect.go refuses a query on its TEXT, and every
// construct it refuses has a probe here. That is not a documentation habit, it
// is the mechanism: the gap table records each probe and the answer the server
// gave, and a sweep in that package (TestEveryDialectGapCarriesItsWitness)
// requires every probe to appear in the BODY of the test the gap names, and
// every answer to appear in what one of that test's ASSERTIONS reads. The AGE
// live recipes must run it — spelling a probe in some other live test is not a
// re-measurement and does not satisfy the sweep, and neither is a wantMessage
// column that survives the assertion reading it. A construct added to the
// refusal list with nothing added here reddens that sweep. The
// refusals may therefore lag what AGE cannot do, and may not run ahead of it.
//
// The gaps:
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
// AGE defines none of the temporal constructors openCypher spells, and every
// one of them is measured below on each live run. agtype has no temporal value
// type and no cast to or from one, and of the 348 functions in ag_catalog then
// in the pinned image exactly one has a temporal name — reached from cypher as
// timestamp(), returning epoch milliseconds as an integer. Nothing re-measures
// that sweep, so it is provenance for the encoding rather than a closed set;
// what is closed here, by measurement, is the set of names a query can spell.
// So a query calling datetime() is a package that compiles and fails on every
// call, and the only place to answer is generate time.
//
// AGE does not define point() either, which is the third gap rather than a name
// in the second: 42883 is the same code the undefined temporal names come back
// under, but the second gap's message is a claim about TEMPORAL constructors and
// its remedy is to compute the value in Go or generate against a neo4j target,
// neither of which answers a spatial call.
//
// Every refusal here is a claim about a server this repo pins by digest, so they are
// measured here on every live run instead of asserted in a comment. If AGE ever
// learns the alternation or grows a constructor, the test for it goes red and
// the refusal is what should be reconsidered (gqlc-35yu.14, gqlc-35yu.12).
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

// undefinedFunctionSQLSTATE is PostgreSQL's undefined_function. Measured
// against the pinned image on 2026-08-29 for all seven refused names below
// (bd gqlc-osf1, workflow run 33268424367) rather than assumed from the class
// name: AGE resolves a cypher call through Postgres function resolution, so it
// answers on the same channel a missing SQL function does, and a caller cannot
// tell the two apart.
//
// It is not the code every refusal in this dialect carries. The one namespaced
// call this project has measured, duration.between(), answers 3F000 —
// invalid_schema_name — because Postgres reads the openCypher namespace as a
// schema qualifier and fails on the qualifier before looking for a function at
// all. That answer names no function, which is why it cannot join this gap (bd
// gqlc-dy40s).
//
// point() answers under it too, measured on the same run, and the spatial gap's
// message quotes the code back to the author — so this one constant is read by
// two witnesses (bd gqlc-l8e2n).
const undefinedFunctionSQLSTATE = "42883"

// invalidSchemaNameSQLSTATE is PostgreSQL's invalid_schema_name, and the whole
// argument of the namespace gap is that this is the code a namespaced call
// answers under.
//
// It is asserted and not merely quoted because the CLASS is the claim. The gap
// refuses a NAMESPACE rather than a full name, and what licenses that is where
// the failure happens: Postgres resolves the schema qualifier before it looks
// for any function, so the refusal belongs to the qualifier and every function
// under it is refused by the same mechanism. The day this image gains a
// `duration` schema, resolution reaches the function and the code changes —
// which is exactly the day the per-namespace claim needs re-arguing, so the
// witness has to red then rather than pass on the message alone.
const invalidSchemaNameSQLSTATE = "3F000"

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
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_dialect_alternation")

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

// TestAGERefusesTheFunctionsItDoesNotDefine measures, against the pinned AGE
// image, the second gap the text gate reads: the openCypher temporal
// constructors AGE has no definition for, and the one temporal call it does
// serve.
//
// Every refused row here is a probe recorded in internal/codegen/age/
// dialect.go, and the refused NAME SET is derived from these very texts — the
// gate parses them for the functions they call and refuses exactly those. So
// this is not a test that happens to agree with the gate; it is where the
// gate's contents come from. Deleting a row deletes a refusal.
//
// The served rows are the bound. `timestamp()` is the one call that works, and
// a gate refusing everything temporal-looking would refuse it; `p.datetime` is
// the false positive a scan for the name would take. Generated code runs the
// author's text verbatim (ADR 0005), so a wrong refusal leaves the author with
// no way round it at all — which makes the served half the more important one.
//
// Both the SQLSTATE and the message are asserted. The spike that recorded
// these answers (gqlc-35yu.5) captured the message alone, and the code went
// unpinned for want of a container; run 33268424367 read it off the pinned
// image for every row here (bd gqlc-osf1), so it is now measured rather than
// assumed. The message is what the refusal quotes back to the author and the
// code is what a caller branches on, and neither implies the other.
func TestAGERefusesTheFunctionsItDoesNotDefine(t *testing.T) {
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_dialect_functions")

	for _, tc := range []struct {
		name string
		// text replaces the author's query text inside the shipped envelope.
		text string
		// wantMessage is the substring the server's error must carry. Empty
		// means the statement must parse. Per row rather than per test,
		// because each name is its own answer and one shared string would
		// pass every row while measuring one.
		wantMessage string
		// wantInteger requires the single value returned to be an integer,
		// which is the claim the refusal message makes about timestamp() when
		// it tells the author what AGE has instead.
		wantInteger bool
	}{
		{
			name:        "datetime",
			text:        "RETURN datetime()",
			wantMessage: "function datetime does not exist",
		},
		{
			name:        "time",
			text:        "RETURN time()",
			wantMessage: "function time does not exist",
		},
		{
			name:        "localtime",
			text:        "RETURN localtime()",
			wantMessage: "function localtime does not exist",
		},
		{
			name:        "date",
			text:        "RETURN date()",
			wantMessage: "function date does not exist",
		},
		{
			name:        "localdatetime",
			text:        "RETURN localdatetime()",
			wantMessage: "function localdatetime does not exist",
		},
		{
			// Called with a map argument, because a constructor refused for
			// its ARGUMENTS would be a different fact from one refused for
			// its name. Spelled without the space after the colon because
			// that is the byte sequence the spike ran (gqlc-35yu.5); the
			// probe is the statement that was measured, not a tidied copy
			// of it.
			name:        "duration",
			text:        "RETURN duration({days:1})",
			wantMessage: "function duration does not exist",
		},
		{
			name:        "toTimestamp",
			text:        "RETURN toTimestamp('2024-01-01')",
			wantMessage: "function toTimestamp does not exist",
		},
		{
			name:        "the one temporal function AGE does define",
			text:        "RETURN timestamp()",
			wantInteger: true,
		},
		{
			// A property lookup spells the name and calls nothing. The gate
			// reads the grammar rather than scanning for `datetime(` for
			// exactly this shape, and the server agreeing is what says the
			// grammar is the right thing to have read.
			name: "a property named like a constructor",
			text: "MATCH (p:Person) RETURN p.datetime",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stmt := substituteQueryText(t, shipped, tc.text)
			rows, err := pool.Query(ctx, stmt, "{}")
			if tc.wantMessage != "" {
				require.Error(t, err, "AGE must refuse this call")
				var pgErr *pgconn.PgError
				require.ErrorAs(t, err, &pgErr, "the refusal must be the server's")
				require.Equal(t, undefinedFunctionSQLSTATE, pgErr.Code,
					"a missing function must answer undefined_function, not a parse error")
				require.Contains(t, pgErr.Message, tc.wantMessage,
					"the missing function is what the server must name")
				return
			}
			require.NoError(t, err, "AGE must accept this call")
			defer rows.Close()

			var values []string
			for rows.Next() {
				var raw []byte
				require.NoError(t, rows.Scan(&raw), "scan value")
				values = append(values, string(raw))
			}
			require.NoError(t, rows.Err())
			if !tc.wantInteger {
				return
			}
			require.Len(t, values, 1, "a bare RETURN yields one row")
			require.Regexp(t, `^-?[0-9]+$`, values[0],
				"timestamp() must answer a bare integer — the refusal tells the author it is epoch millis")
		})
	}
}

// TestAGERefusesTheSpatialConstructor measures, against the pinned AGE image,
// the third gap the text gate reads: the one spatial constructor this project
// has run.
//
// It is a gap of its own and not a row in the one above, and the reason is the
// message rather than the server. AGE answers point() in the SAME error class
// as the temporal names — 42883, undefined_function — but the temporal gap
// tells the author to compute the value in Go and bind it, or to generate
// against a neo4j target, and neither of those answers a spatial call. So the
// two carry different sentinels and different prose, and this test is where the
// spatial one's evidence lives.
//
// The SQLSTATE is asserted here, unlike the temporal witness, because this
// gap's refusal quotes it: it was measured for this probe on workflow run
// 33268424367 (bd gqlc-l8e2n) rather than inferred from the temporal rows, and
// a code quoted to an author is a claim about the server that has to be
// re-measured like any other.
//
// The served row is a property lookup and there is no served CALL beside it.
// The one spatial name this project has run is refused, so the accepting half
// of this gap's bound is `p.point` alone — which is also the false positive a
// scan for `point(` would take, and the reason the gate reads the grammar.
func TestAGERefusesTheSpatialConstructor(t *testing.T) {
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_dialect_spatial")

	for _, tc := range []struct {
		name string
		// text replaces the author's query text inside the shipped envelope.
		text string
		// wantMessage is the substring the server's error must carry. Empty
		// means the statement must parse.
		wantMessage string
	}{
		{
			name:        "point",
			text:        "RETURN point({x: 1, y: 2})",
			wantMessage: "function point does not exist",
		},
		{
			// A property lookup spells the name and calls nothing.
			name: "a property named like the constructor",
			text: "MATCH (p:Person) RETURN p.point",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stmt := substituteQueryText(t, shipped, tc.text)
			rows, err := pool.Query(ctx, stmt, "{}")
			if tc.wantMessage != "" {
				require.Error(t, err, "AGE must refuse this call")
				var pgErr *pgconn.PgError
				require.ErrorAs(t, err, &pgErr, "the refusal must be the server's")
				require.Equal(t, undefinedFunctionSQLSTATE, pgErr.Code,
					"the refusal quotes this code back to the author")
				require.Contains(t, pgErr.Message, tc.wantMessage,
					"the missing function is what the server must name")
				return
			}
			require.NoError(t, err, "AGE must accept this call")
			defer rows.Close()

			for rows.Next() {
				var raw []byte
				require.NoError(t, rows.Scan(&raw), "scan value")
			}
			require.NoError(t, rows.Err())
		})
	}
}

// TestAGERefusesTheNamespaceItHasNoSchemaFor measures, against the pinned AGE
// image, the fourth gap the text gate reads — and the only one whose refusal
// names no function.
//
// A namespaced call reaches PostgreSQL as a schema-qualified name, and
// PostgreSQL resolves the qualifier before it looks for a function. So the
// server answers `schema "duration" does not exist` under SQLSTATE 3F000, and
// there is nothing in that sentence for the temporal gap's name-witness guard
// to match a function name against. Hence a gap of its own, catalogued by
// namespace.
//
// Two rows carry the per-namespace claim rather than one. The catalogue holds
// `duration` on the strength of duration.between, and the gate then refuses
// duration.inSeconds too — a name no probe ever ran. That widening is the
// ruling's one inferential step, so inSeconds is measured here beside it: same
// class, same message, a different function under the same absent namespace.
// Without that row the gate would refuse a spelling nothing had witnessed.
//
// The mixed-case row is not politeness either. The gate lowercases the
// namespace before matching because internal/codegen/age/shape.go lowercases
// it before typing a temporal, so a case-sensitive gate would let
// Duration.Between through to a refusal that names a column instead of the
// text. The server's own answer quotes the author's case back —
// `schema "Duration"` — which is why the gate's catalogue folds case and the
// unit guard compares case-insensitively.
//
// The served rows are the half that bounds the false positive, and this gap
// has a served CALL where the spatial gap has none: ag_catalog.age_timestamp()
// is namespaced AND resolves, so the gate is shown to refuse the measured
// namespace rather than the call shape. Both served rows read the seeded
// Person: an empty graph short-circuits a MATCH on an unknown label and never
// evaluates what follows, so a served row over a label with no nodes would
// pass without measuring anything.
//
// Not measured here, deliberately: com.example.between. A multi-part namespace
// does not reach schema resolution at all — AGE's own cypher parser refuses it
// as 42601 invalid indirection syntax — so it is neither a refusal of this
// class nor a text the server serves, and it belongs in the unit suite as the
// find-silence pin it is (internal/query/cypher).
func TestAGERefusesTheNamespaceItHasNoSchemaFor(t *testing.T) {
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_dialect_namespace")

	for _, tc := range []struct {
		name string
		// text replaces the author's query text inside the shipped envelope.
		text string
		// wantMessage is the substring the server's error must carry. Empty
		// means the statement must parse.
		wantMessage string
	}{
		{
			// The probe the catalogue is derived from. Null arguments
			// because the NAME is what is being measured: the qualifier
			// fails before an argument could be evaluated.
			name:        "duration.between",
			text:        "RETURN duration.between(null, null)",
			wantMessage: `schema "duration" does not exist`,
		},
		{
			// The widening's own witness: a function no probe ran, under
			// the namespace a probe did.
			name:        "another function under the same namespace",
			text:        "RETURN duration.inSeconds(null, null)",
			wantMessage: `schema "duration" does not exist`,
		},
		{
			// The server quotes the namespace as written, so this row also
			// records that the answer is NOT lowercased — the reason the
			// gate folds case on its own side.
			name:        "the namespace in the author's case",
			text:        "RETURN Duration.Between(null, null)",
			wantMessage: `schema "Duration" does not exist`,
		},
		{
			// The shape only a text gate can catch. A constructor in a
			// predicate reaches no column at all, because the query model
			// drops predicate structure (ADR 0003).
			name:        "a namespaced call in a predicate",
			text:        "MATCH (p:Person) WHERE p.d < duration.between(p.a, p.b) RETURN p.id",
			wantMessage: `schema "duration" does not exist`,
		},
		{
			// A qualified call that resolves. The served row this gap has
			// and the spatial gap does not.
			name: "a namespaced call AGE does define",
			text: "RETURN ag_catalog.age_timestamp()",
		},
		{
			// A property lookup spells the namespace and calls nothing —
			// the false positive a scan for `duration.` would take.
			name: "a property named like the namespace",
			text: "MATCH (p:Person) RETURN p.duration",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stmt := substituteQueryText(t, shipped, tc.text)
			rows, err := pool.Query(ctx, stmt, "{}")
			if tc.wantMessage != "" {
				require.Error(t, err, "AGE must refuse this call")
				var pgErr *pgconn.PgError
				require.ErrorAs(t, err, &pgErr, "the refusal must be the server's")
				require.Equal(t, invalidSchemaNameSQLSTATE, pgErr.Code,
					"the refusal is the QUALIFIER's, which is what licenses refusing per namespace")
				require.Contains(t, pgErr.Message, tc.wantMessage,
					"the namespace is what the server must name, and it names no function")
				require.NotContains(t, pgErr.Message, "function",
					"an answer naming a function would belong to the temporal gap's catalogue, "+
						"which is derived from answers that name one")
				return
			}
			require.NoError(t, err, "AGE must accept this call")
			defer rows.Close()

			var seen int
			for rows.Next() {
				var raw []byte
				require.NoError(t, rows.Scan(&raw), "scan value")
				seen++
			}
			require.NoError(t, rows.Err())
			require.Equal(t, 1, seen,
				"a served row that matched nothing measured nothing: AGE short-circuits a MATCH "+
					"on a label with no nodes and never evaluates the call under test")
		})
	}
}

// ageDialectHarness is one AGE container, one graph seeded through the
// emission's own envelope, and the envelope itself: the SQL a shipped generated
// method sent, captured off the wire, with a slot where the author's query text
// goes.
//
// No statement either test composes by hand. Every probe travels in that same
// envelope — the E-quoted graph name, the dollar-quoted text, the bound
// argument, the record declaration — with only the query text substituted, so a
// refusal is the construct's and not the harness's. Each caller gets its own
// container and its own graph name, which is what lets both run in parallel.
func ageDialectHarness(t *testing.T, graph string) (context.Context, *pgxpool.Pool, string) {
	t.Helper()
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

	q := entityedgeage.New(pool, graph)
	require.NoError(t, q.EnsureGraph(ctx), "ensure graph %s", graph)
	t.Cleanup(func() { require.NoError(t, q.DropGraph(ctx), "drop graph %s", graph) })

	// Capture the envelope from a shipped method before anything is seeded:
	// the graph is empty, so the method reports no rows, and the statement it
	// issued to find that out is the one the probes go on to travel in.
	_, err := entityedgeage.New(traced, graph).OneActedIn(ctx)
	require.ErrorIs(t, err, entityedgeage.ErrNoRows, "an empty graph has no ACTED_IN edge")
	shipped := lastDollarQuotedStatement(t, rec)
	require.Contains(t, shipped, "MATCH (:Person)-[r:ACTED_IN]->(:Movie) RETURN r",
		"the captured statement must be the one carrying the fixture's query text")

	// Two edge types between the same endpoints. Only ACTED_IN is in the
	// fixture's schema; DIRECTED is here so a pattern naming both has two
	// labels to find, which is the state an edge-union column describes. The
	// Person is what a probe reading a property has to match.
	seed := substituteQueryText(t, shipped,
		"CREATE (p:Person {id: 1}) CREATE (m:Movie {id: 2}) "+
			"CREATE (p)-[:ACTED_IN {since: 2019}]->(m) CREATE (p)-[:DIRECTED]->(m)")
	_, err = pool.Exec(ctx, seed, "{}")
	require.NoError(t, err, "seed the graph through the same envelope")

	return ctx, pool, shipped
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
