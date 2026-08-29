//go:build codegen_live

// The measured answers that no gap acts on yet.
//
// internal/codegen/age/dialect.go refuses a construct only on a probe and the
// answer a live session gave it, and every such pair lives in
// live_age_dialect_test.go under the sweep that binds it to its gap. The two
// rows here have the witness and not the gap: the pinned image refuses both,
// measured 2026-08-29 on workflow run 33268424367 (bd gqlc-osf1), and neither
// can join a gap that exists today.
//
//   - point() is refused, but it is not a temporal, and the undefined-function
//     gap's message scopes itself to temporal constructors. Refusing it there
//     would print an answer about temporals over a name that is not one. It
//     needs a third gap with its own sentinel and message: bd gqlc-l8e2n.
//   - duration.between() is refused under a different error CLASS — 3F000
//     invalid_schema_name, not 42883 undefined_function — because Postgres
//     reads the openCypher namespace as a schema qualifier and fails on the
//     qualifier before looking for a function. The answer therefore names no
//     function at all, which is exactly what
//     TestEveryRefusedFunctionNameIsNamedByItsProbeAnswer requires of a row in
//     that gap. It cannot join it even with a namespaced scanner: bd
//     gqlc-dy40s.
//
// So this file is the standing evidence those two beads are built on, and the
// tripwire if the pinned image's answer moves before either is taken. Whoever
// takes one of them moves its row into live_age_dialect_test.go alongside the
// gap it earns, and deletes it from here.
//
// It is in its own file rather than folded into live_age_dialect_test.go
// because that file's sweep counterpart (TestEveryDialectGapCarriesItsWitness)
// reads gap witnesses out of the named test's body, and a probe belonging to no
// gap sitting in there is text a witness could be mistaken for.
//
// It prints nothing unless test-codegen-live-age's -run alternation names it.
// That allowlist is a name list rather than a pattern, and
// TestEveryLiveTestIsRunByARecipeThatNamesIt (internal/liverecipes) refuses a
// live test missing from it.

package fixtures_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// TestAGEAnswersTheConstructsNoGapRefuses holds the pinned image to the answers
// bd gqlc-l8e2n and bd gqlc-dy40s were filed on.
//
// Both the code and the message are asserted, and the two are what separate
// these rows from each other and from the undefined-function gap: point()
// answers on the same channel a missing SQL function does, duration.between()
// on the channel a missing schema does, and it is that difference — not the
// fact of refusal — that decides which gap each one can ever belong to.
func TestAGEAnswersTheConstructsNoGapRefuses(t *testing.T) {
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_dialect_unwitnessed")

	for _, tc := range []struct {
		name string
		text string
		// wantSQLSTATE and wantMessage are the server's own answer, read off
		// run 33268424367. Asserted per row: one shared expectation would
		// pass both while measuring the difference away.
		wantSQLSTATE string
		wantMessage  string
	}{
		{
			name:         "point",
			text:         "RETURN point({x: 1, y: 2})",
			wantSQLSTATE: "42883",
			wantMessage:  "function point does not exist",
		},
		{
			name: "duration.between",
			// Literal arguments rather than a MATCH: a pattern that finds
			// nothing returns no rows and no error, which would read as
			// accepted without the function having been resolved at all.
			text:         "RETURN duration.between(null, null)",
			wantSQLSTATE: "3F000",
			wantMessage:  `schema "duration" does not exist`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stmt := substituteQueryText(t, shipped, tc.text)
			rows, err := pool.Query(ctx, stmt, "{}")
			if err == nil {
				// pgx defers the server round trip on some paths, so the
				// rows have to be drained before acceptance is a fact.
				for rows.Next() {
					var raw []byte
					require.NoError(t, rows.Scan(&raw), "scan value")
				}
				err = rows.Err()
				rows.Close()
			}

			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr,
				"%s answered with something that is not the server's error, so this row measured the harness and not the dialect: %v",
				tc.name, err)
			require.Equal(t, tc.wantSQLSTATE, pgErr.Code,
				"the error CLASS is what decides which gap this construct can belong to")
			require.Contains(t, pgErr.Message, tc.wantMessage,
				"the refusal has to name what the server could not find")
		})
	}
}
