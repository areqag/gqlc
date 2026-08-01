//go:build codegen_live

// Live smoke battery for the generated repositories: every scenario runs
// against every backend arm, driving a real container (gqlc-73h, gqlc-5gc).
// Opt-in via -tags codegen_live so PR CI stays fast; the manual / nightly CI
// job runs it. Lives in the nested test/data/codegen module so testcontainers
// and its ~50 transitive deps stay out of gqlc's root go.mod and the compiler
// binary.
package fixtures

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// person is a many_col_many row. Every target emits its own
// PeopleByAgeAndLocaleRow into its own package; this is the shape the battery
// reads columns from.
type person struct {
	Name string
	Age  int64
}

// mixedReadWriteBatchQuerier is one arm's mixed_read_write_batch handle.
// errNoRows and errMultipleResults report the sentinels of the package these
// methods are generated into: each generated package declares its own
// errors.New values, so errors.Is only holds against the pair that arrived
// with the handle.
type mixedReadWriteBatchQuerier interface {
	getPersonName(ctx context.Context, id int64) (string, error)
	removePerson(ctx context.Context, id int64) error
	errNoRows() error
	errMultipleResults() error
}

// manyColManyQuerier is one arm's many_col_many handle. The generated Params
// and Row types are package-local to each target, so they stop here.
type manyColManyQuerier interface {
	peopleByAgeAndLocale(ctx context.Context, minAge int64, locale string) ([]person, error)
}

// harness is one arm for the length of the battery: a running container and a
// connection to it. Handing out scenarios is its whole surface, so a querier
// is unobtainable outside the isolation it belongs to.
//
// parallelScenarios reports whether the arm's isolation admits concurrent
// scenarios; an arm whose scenarios share one graph reports false. scenario
// establishes one scenario's isolation, binds the generated handles to it,
// and registers any teardown on t.
type harness interface {
	parallelScenarios() bool
	scenario(ctx context.Context, t *testing.T) backend
}

// backend is one scenario's isolated view of an arm: a graph no other
// scenario observes, and the generated handles bound to it.
//
// seed writes through the driver, never through generated code, so seeded
// data is independent of the surface under test. Its cypher stays inside the
// openCypher dialect intersection so one string serves every arm.
type backend interface {
	seed(ctx context.Context, t *testing.T, cypher string)
	mixedReadWriteBatch() mixedReadWriteBatchQuerier
	manyColMany() manyColManyQuerier
}

// arms are the backends the battery runs against. Each adapter owns its
// container, its connection, and its isolation strategy.
var arms = []struct {
	name  string
	start func(ctx context.Context, t *testing.T) harness
}{
	{name: "neo4j-go-v5", start: startNeo4jV5},
	{name: "neo4j-go-v6", start: startNeo4jV6},
}

// scenarios are the battery. Each body is written once against backend and
// runs against every arm. A body must not call t.Helper(): its own frame is
// where the assertions live, so marking it a helper attributes every failure
// to the loop in TestLiveSmoke instead of to the line that failed.
var scenarios = []struct {
	name string
	run  func(ctx context.Context, t *testing.T, b backend)
}{
	{name: "mixed_read_write_batch: one + exec", run: oneAndExec},
	{name: "many_col_many: many + params", run: manyWithParams},
}

// TestLiveSmoke runs every scenario against every arm. Arms call t.Parallel()
// so their container boots overlap: two containers, ~4GB peak, well within a
// standard CI runner. Scenarios share their arm's container, amortising the
// ~15s startup, and run concurrently or not as that arm's isolation allows.
//
// Skips when GQLC_SKIP_LIVE is set so a developer without docker can still
// run `go test -tags codegen_live ./...` without a hard failure.
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	for _, arm := range arms {
		t.Run(arm.name, func(t *testing.T) {
			t.Parallel()
			// One timeout per arm keeps a stuck container from hanging the
			// whole test binary indefinitely. Neo4j 5-community typically
			// starts in <15s; 120s covers a cold image pull on a slow runner.
			// Cleanups run last-registered-first, so cancelling here rather
			// than on return leaves the container and driver teardown the arm
			// is about to register a live context to close over.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			t.Cleanup(cancel)

			h := arm.start(ctx, t)
			parallelScenarios := h.parallelScenarios()
			for _, sc := range scenarios {
				t.Run(sc.name, func(t *testing.T) {
					if parallelScenarios {
						t.Parallel()
					}
					sc.run(ctx, t, h.scenario(ctx, t))
				})
			}
		})
	}
}

// oneAndExec drives the :one contract end to end — both sentinels, a
// single-row read, and a :exec write observed by a re-read.
func oneAndExec(ctx context.Context, t *testing.T, b backend) {
	q := b.mixedReadWriteBatch()

	// errors.Is (via require.ErrorIs) confirms the sentinel is
	// identity-matchable so callers can branch generically.
	_, err := q.getPersonName(ctx, 1)
	require.ErrorIs(t, err, q.errNoRows(), "empty graph must return ErrNoRows")

	b.seed(ctx, t, "CREATE (:Person {id: 1, name: 'Alice'})")

	name, err := q.getPersonName(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "Alice", name)

	// Two rows for the same id triggers ErrMultipleResults.
	b.seed(ctx, t, "CREATE (:Person {id: 1, name: 'AliceTwin'})")
	_, err = q.getPersonName(ctx, 1)
	require.ErrorIs(t, err, q.errMultipleResults(), "two matching rows must return ErrMultipleResults")

	// :exec write path: delete both rows, then re-query and confirm
	// ErrNoRows — proves the :exec method actually mutated the graph.
	require.NoError(t, q.removePerson(ctx, 1))
	_, err = q.getPersonName(ctx, 1)
	require.ErrorIs(t, err, q.errNoRows(), "after :exec delete, :one must see empty result")
}

// manyWithParams drives the :many contract — parameter binding narrows the
// result set, and an empty result is (empty, nil) rather than a sentinel.
func manyWithParams(ctx context.Context, t *testing.T, b backend) {
	q := b.manyColMany()

	// Two locales, three ages: only Alice satisfies age > 25 AND locale = 'en'.
	b.seed(ctx, t, `
		CREATE (:Person {name: 'Alice', age: 30, locale: 'en'})
		CREATE (:Person {name: 'Bob',   age: 20, locale: 'en'})
		CREATE (:Person {name: 'Cara',  age: 40, locale: 'fr'})
	`)

	rows, err := q.peopleByAgeAndLocale(ctx, 25, "en")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "Alice", rows[0].Name)
	require.Equal(t, int64(30), rows[0].Age)

	// Empty result set on :many is (empty slice, nil error) — distinct
	// from :one's ErrNoRows contract.
	rows, err = q.peopleByAgeAndLocale(ctx, 100, "en")
	require.NoError(t, err)
	require.NotNil(t, rows, "empty :many result must be an empty slice, not nil")
	require.Empty(t, rows)
}
