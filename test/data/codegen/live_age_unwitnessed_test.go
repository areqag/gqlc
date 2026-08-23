//go:build codegen_live

// A measurement instrument, not a gate.
//
// internal/codegen/age/dialect.go refuses a construct only on a probe and the
// answer a live session gave it. bd gqlc-osf1 records four openCypher
// constructors that the gap table's prose calls suspect and that NOBODY HAS
// EVER RUN — time(), localtime(), point() and the namespaced
// duration.between() — plus one thing the table records about the five
// refusals it does carry but never measured: the SQLSTATE they come back
// under.
//
// Suspicion is not evidence, and the whole design of that table is that a
// refusal costs a witness. So the four are not in the table, and this file is
// how they stop being suspicions: it runs each against the pinned image and
// PRINTS what the server said. It asserts almost nothing on purpose — the
// point is to obtain an answer, not to pin one that was guessed. Whichever way
// each turns out, the answer is then written into the gap table (refused) or
// into its served list (accepted), where the existing sweep holds it.
//
// Run it, read the log, and act on it:
//
//	gh workflow run codegen-live.yml --ref <branch>
//	gh run view <id> --log | grep 'osf1:'
//
// Every line it prints is prefixed `osf1:` for exactly that grep.
//
// It is in its own file rather than folded into live_age_dialect_test.go
// because that file's sweep counterpart (TestEveryDialectGapCarriesItsWitness)
// reads gap witnesses out of the named test's body, and a non-asserting probe
// sitting in there would be text a witness could be mistaken for.

package fixtures_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// TestAGEAnswersTheConstructorsNobodyRan prints the pinned image's answer to
// each of bd gqlc-osf1's unmeasured questions.
//
// The four constructors are run for their answer, not against an expectation.
// The five already-refused calls are re-run for their SQLSTATE alone, which is
// the one thing the spike (gqlc-35yu.5) did not record: it captured the
// server's MESSAGE, so TestAGERefusesTheFunctionsItDoesNotDefine asserts the
// message and nothing else, unlike the alternation gap which pins 42601.
// PostgreSQL's undefined_function is 42883 and AGE plausibly reports through
// the same channel — plausibly being the reason this runs.
//
// The only assertion is that the server answered at all: a row that neither
// returned rows nor produced a *pgconn.PgError measured the harness rather
// than the dialect, and a log line saying so would be read as a dialect fact.
func TestAGEAnswersTheConstructorsNobodyRan(t *testing.T) {
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_dialect_unwitnessed")

	for _, tc := range []struct {
		name string
		text string
		// why records what this row is here to settle, so a reader of the
		// log knows what turns on the answer.
		why string
	}{
		{
			name: "time",
			text: "RETURN time()",
			why:  "osf1 (1): suspected undefined; never run",
		},
		{
			name: "localtime",
			text: "RETURN localtime()",
			why: "osf1 (2): load-bearing. invalid/unrepresentable_temporal_localtime_column and " +
				"TestTemporalProjectionNamesThisBackend rest on localtime() reaching the CARRIER refusal " +
				"(codegen.ErrUnrepresentableTemporal). If AGE does not define it, the dialect gate answers " +
				"both ahead of that and both have to move",
		},
		{
			name: "point",
			text: "RETURN point({x: 1, y: 2})",
			why: "osf1 (3): not a temporal, so a refusal would be a THIRD gap rather than a name added " +
				"to the second — the present message says AGE 'defines no temporal constructor at all'",
		},
		{
			name: "duration.between",
			// Literal arguments rather than a MATCH: a pattern that finds
			// nothing returns no rows and no error, which would print
			// ACCEPTED without the function having been resolved at all.
			text: "RETURN duration.between(null, null)",
			why: "osf1 (4): namespaced, so a different name from duration (Cypher.g4 oC_FunctionName is " +
				"oC_Namespace oC_SymbolicName). cypher.UnqualifiedFunctionCalls drops namespaced calls by " +
				"design, so refusing it needs a second scanner as well as a witness",
		},

		// The five the table already refuses, re-run for their SQLSTATE.
		{name: "datetime", text: "RETURN datetime()", why: "osf1 (5): SQLSTATE of an existing refusal"},
		{name: "date", text: "RETURN date()", why: "osf1 (5): SQLSTATE of an existing refusal"},
		{name: "localdatetime", text: "RETURN localdatetime()", why: "osf1 (5): SQLSTATE of an existing refusal"},
		{name: "duration", text: "RETURN duration({days:1})", why: "osf1 (5): SQLSTATE of an existing refusal"},
		{name: "toTimestamp", text: "RETURN toTimestamp('2024-01-01')", why: "osf1 (5): SQLSTATE of an existing refusal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stmt := substituteQueryText(t, shipped, tc.text)
			rows, err := pool.Query(ctx, stmt, "{}")
			if err == nil {
				// pgx defers the server round trip on some paths, so the
				// rows have to be drained before ACCEPTED is a fact.
				var values []string
				for rows.Next() {
					var raw []byte
					require.NoError(t, rows.Scan(&raw), "scan value")
					values = append(values, string(raw))
				}
				drainErr := rows.Err()
				rows.Close()
				if drainErr == nil {
					t.Logf("osf1: %-16s ACCEPTED  text=%q values=%v  [%s]", tc.name, tc.text, values, tc.why)
					return
				}
				err = drainErr
			}

			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr,
				"osf1: %s answered with something that is not the server's error, so this row measured the harness and not the dialect: %v",
				tc.name, err)
			t.Logf("osf1: %-16s REFUSED   text=%q sqlstate=%s message=%q  [%s]",
				tc.name, tc.text, pgErr.Code, pgErr.Message, tc.why)
		})
	}
}
