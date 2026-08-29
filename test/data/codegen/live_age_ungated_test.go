//go:build codegen_live

// The measured answer that no gap acts on yet.
//
// internal/codegen/age/dialect.go refuses a construct only on a probe and the
// answer a live session gave it, and every such pair lives in
// live_age_dialect_test.go under the sweep that binds it to its gap. The one
// row here has the witness and not the gap: the pinned image refuses it,
// measured 2026-08-29 on workflow run 33268424367 (bd gqlc-osf1), and it can
// join no gap that exists today.
//
//   - duration.between() is refused under a different error CLASS from every
//     name a gap already holds — 3F000 invalid_schema_name, not 42883
//     undefined_function — because Postgres reads the openCypher namespace as a
//     schema qualifier and fails on the qualifier before looking for a
//     function. The answer therefore names no function at all, which is exactly
//     what TestEveryRefusedFunctionNameIsNamedByItsProbeAnswer requires of a
//     row in that gap. It cannot join it even with a namespaced scanner: bd
//     gqlc-dy40s.
//
// So this file is the standing evidence bd gqlc-dy40s is built on, and the
// tripwire if the pinned image's answer moves before it is taken. Whoever takes
// it moves this row into live_age_dialect_test.go alongside the gap it earns
// and deletes it from here — which empties the file, so that bead retires it
// rather than leaving an instrument measuring nothing.
//
// point() was the second row until bd gqlc-l8e2n gave it the third gap it
// needed (PR #1817). That is the protocol above arriving: its row now lives in
// TestAGERefusesTheSpatialConstructor, which asserts the same 42883 and the
// same message and adds the served `p.point` half, so deleting it here lost no
// evidence — and 42883 is still the contrast that makes 3F000 legible, one file
// over. It was NOT deleted at the time, so for a while point() was probed twice
// under two tests disagreeing about whether a gap acts on it, all gates green
// (bd gqlc-yvubd).
//
// Nothing catches that. The sweep runs gap -> witness
// (TestEveryDialectGapCarriesItsWitness); there is no witness -> no-gap
// direction, so a row left here after its gap lands is a claim this file's own
// title contradicts and no gate reads. Filed as bd gqlc-82ffi, and it is live
// against the row below.
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

// TestAGEAnswersTheConstructsNoGapRefuses holds the pinned image to the answer
// bd gqlc-dy40s was filed on.
//
// Both the code and the message are asserted, and the code is what separates
// this row from every gap that exists: a missing SQL function answers on 42883,
// as the temporal names and point() all do, while duration.between() answers on
// the channel a missing SCHEMA does. It is that difference — not the fact of
// refusal — that decides which gap it can ever belong to, so asserting the
// message alone would measure the refusal and lose the reason.
func TestAGEAnswersTheConstructsNoGapRefuses(t *testing.T) {
	ctx, pool, shipped := ageDialectHarness(t, "gqlc_dialect_unwitnessed")

	for _, tc := range []struct {
		name string
		text string
		// wantSQLSTATE and wantMessage are the server's own answer, read off
		// run 33268424367. Both are asserted: the message alone would hold for
		// a refusal on any channel, and the channel is the whole finding.
		wantSQLSTATE string
		wantMessage  string
	}{
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
