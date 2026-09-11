//go:build codegen_live

// A temporal list crossing OUTWARD as a query parameter, against a real
// Apache AGE server, at depth 2 and at depth 1.
//
// The defect this exists for was invisible in the way a live row exists to
// catch (bd gqlc-jc8mc). The encoder recorded one list depth per carrier and
// stripped exactly one level, so a depth-2 temporal parameter matched no leaf
// arm and crossed through plain json.Marshal as its Go struct while the stored
// property held ISO strings. The emitted package compiled, every golden was
// well-formed, no test was red — the equality predicate simply never matched.
// A query that silently returns nothing.
//
// PR #2130 fixed it and witnessed it at the level of WIRE BYTES: the age corpus
// asserts the encoded parameter text and the conformance goldens assert the
// emitted expression. Neither asserts that a server ACCEPTS those bytes and
// answers the row, and the gap is the whole failure mode — bytes that are
// well-formed and mean nothing to the server are exactly what the pre-fix tree
// produced.
//
// THREE ROWS, and the two beside the first are not courtesies:
//
//   - depth 2 matches the row it names, and only that row. Two Rosters are
//     seeded with different dates, so the row that comes back is selected
//     rather than merely returned.
//   - depth 2 that names no row matches nothing. Without it a predicate that
//     matched EVERYTHING would pass the first row for the wrong reason, and
//     "matches everything" is a live possibility here: agtype compares values
//     of unlike type by a total order rather than refusing, so a parameter
//     that arrives as the wrong shape does not fail loudly.
//   - depth 1 matches, as the control. It is the same idea as the flat-list
//     control in live_nested_list_property_test.go: without it a server that
//     matched NO list parameter at all would red the depth-2 row, and the
//     reading would be "the depth-2 encoding is broken" when the truth was
//     "this server matches no list". The control is what scopes the claim to
//     DEPTH.
//
// The depth-1 fixture is temporal_list_param, whose $days is LIST<DATE> and
// whose own header records that its depth-2 parameter had to be removed:
// ADR 0035 has neo4j refuse a nested list as a stored property, so the depth-2
// shape is constructible on AGE alone. That is why the two halves come from two
// fixtures rather than one, and why this file is AGE-only.
//
// WHY ITS OWN FILE, on the precedent live_nested_list_property_test.go writes
// down. It cannot be a readScenarios row: that battery runs against every arm
// through the backend interface and nested_temporal_list_property is generated
// for apache-age-pgx-v5 alone. live_age_dialect_test.go is swept by
// TestEveryDialectGapCarriesItsWitness, which reads gap witnesses out of the
// bodies of the tests its gap table names, so a probe belonging to no dialect
// gap must not sit there. live_age_ungated_test.go is titled for a measured
// REFUSAL, and this is the opposite claim.
//
// COST. AGE rows are nightly and manual only by ADR 0010, so this does not
// become a PR gate and no pull request pays for the container. It catches the
// regression on master overnight, which is the design rather than an oversight.
//
// The name is spelled into the test-codegen-live-age recipe in the justfile and
// declared ArmAGE in internal/liverecipes. go test's -run is unanchored and the
// recipe's alternation is a name list for that reason; a live test added here
// and not added there runs in no job, which
// TestEveryLiveTestIsRunByARecipeThatNamesIt refuses.

package fixtures_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nestedtemporalage "github.com/areqag/gqlc/test/data/codegen/valid/nested_temporal_list_property/golden/apache-age-pgx-v5"
	temporalparamage "github.com/areqag/gqlc/test/data/codegen/valid/temporal_list_param/golden/apache-age-pgx-v5"
)

// temporalListParamSeed writes the two Rosters and the two Slots every row
// below reads.
//
// The literals are spelled in the STORED encoding rather than built from the
// generated encoder, which is the point: the encoder is what is under test, so
// a seed that called it would agree with itself no matter what it produced.
// A DATE is stored as zero-padded ISO text and a DURATION as a count of
// microseconds — both documented on the decoders in the emitted models.go —
// and 86400000000 is one day.
//
// Two of each label, differing only in the parameterised property, so a row
// that comes back was selected by the predicate rather than being the only
// thing in the graph.
const temporalListParamSeed = `
	CREATE (:Roster {id: 1, dates: [['2024-01-02', '2024-03-04'], ['2025-05-06']]})
	CREATE (:Roster {id: 2, dates: [['1999-12-31']]})
	CREATE (:Slot {id: 10, tags: ['a', 'b'], ranks: [1, 2], days: ['2024-01-02', '2024-03-04'], spans: [86400000000]})
	CREATE (:Slot {id: 11, tags: ['a', 'b'], ranks: [1, 2], days: ['1999-12-31'], spans: [86400000000]})
`

// TestAGEMatchesATemporalListParameter binds a temporal list as a query
// parameter and asserts the server answers with the row it names.
func TestAGEMatchesATemporalListParameter(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	const graph = "gqlc_temporal_list_param"
	endpoint := startAGEContainer(ctx, t)
	createAGEAppRole(ctx, t, endpoint)
	pool := openAGEPool(ctx, t, ageDSN(endpoint, ageAppRole, ageAppPassword, ageDatabase), ageSessionInit)

	nested := nestedtemporalage.New(pool, graph)
	flat := temporalparamage.New(pool, graph)

	// Created through one package's helper and dropped through the other's:
	// each fixture emits its own lifecycle pair and both handles have to
	// reach the same graph for either to see the seed.
	require.NoError(t, nested.EnsureGraph(ctx), "ensure graph %s", graph)
	t.Cleanup(func() { require.NoError(t, flat.DropGraph(ctx), "drop graph %s", graph) })

	stmt := "SELECT * FROM ag_catalog.cypher('" + graph + "', $seed$" + temporalListParamSeed + "$seed$) AS (v ag_catalog.agtype)"
	_, err := pool.Exec(ctx, stmt)
	require.NoError(t, err, "seed the graph")

	jan2 := nestedtemporalage.Date{Year: 2024, Month: 1, Day: 2}
	mar4 := nestedtemporalage.Date{Year: 2024, Month: 3, Day: 4}
	may6 := nestedtemporalage.Date{Year: 2025, Month: 5, Day: 6}

	t.Run("a depth-2 temporal list parameter matches the row it names", func(t *testing.T) {
		ids, err := nested.RostersOn(ctx, [][]nestedtemporalage.Date{{jan2, mar4}, {may6}})
		require.NoError(t, err, "bind a depth-2 list of DATE as a parameter")
		require.Equal(t, []int64{1}, ids,
			"the encoder must send a depth-2 temporal list the server can compare against the "+
				"stored property; before PR #2130 it stripped one list level and this matched nothing")
	})

	t.Run("a depth-2 temporal list parameter that names no row matches nothing", func(t *testing.T) {
		ids, err := nested.RostersOn(ctx, [][]nestedtemporalage.Date{{jan2}, {may6}})
		require.NoError(t, err, "bind a depth-2 list that no Roster holds")
		require.Empty(t, ids,
			"a predicate that matched everything would pass the row above for the wrong reason")
	})

	t.Run("a depth-1 temporal list parameter matches, as the control", func(t *testing.T) {
		spans := []temporalparamage.Duration{{Days: 1}}
		ids, err := flat.SlotsMatching(ctx, temporalparamage.SlotsMatchingParams{
			Tags:  []string{"a", "b"},
			Ranks: []int32{1, 2},
			Days:  []temporalparamage.Date{{Year: 2024, Month: 1, Day: 2}, {Year: 2024, Month: 3, Day: 4}},
			Spans: &spans,
		})
		require.NoError(t, err, "bind a depth-1 list of DATE as a parameter")
		require.Equal(t, []int64{10}, ids,
			"this server matches list parameters at all, which is what scopes the rows above to DEPTH; "+
				"without it a server matching no list would red them and be read as a depth bug")
	})
}
