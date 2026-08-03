//go:build codegen_live

// Live smoke battery for the generated repositories: every scenario runs
// against every backend arm, driving a real container (gqlc-73h, gqlc-5gc,
// gqlc-35yu.8). Opt-in via -tags codegen_live so PR CI stays fast; the manual
// / nightly CI job runs it. Lives in the nested test/data/codegen module so
// testcontainers and its ~50 transitive deps stay out of gqlc's root go.mod
// and the compiler binary.
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

// personEntity is an entity_node_projected_one row and actedInEntity an
// entity_edge_projected_one row. Every target emits its own Person and
// ACTEDIN struct into its own package; these are the shapes the battery
// reads fields from, and they are the same fields whichever arm produced
// them — that the Go surface does not vary by backend is the property the
// entity scenarios are here to hold.
type personEntity struct {
	ID         int64
	MiddleName *string
	Name       string
}

type actedInEntity struct {
	Since int64
}

// edgeUnionAction is an edge_union_undeclared_relationship_type row flattened.
// The column's Go type is a sealed interface each target emits into its own
// package, with its own AUTHORED and LIKES members, so an adapter narrows the
// arriving member to this shape and the scenario asserts on it.
//
// Kind is the candidate the emitted dispatch chose, and the property carried
// alongside it belongs to that candidate and to no other: AUTHORED declares
// since and LIKES declares rating, so a dispatch that ran the wrong arm is
// visible in the value rather than hidden behind two structurally equal ones.
type edgeUnionAction struct {
	Kind   string
	Since  int64
	Rating int64
}

// oneColOneParamOneQuerier is one arm's one_col_one_param_one handle.
// errNoRows and errMultipleResults report the sentinels of the package these
// methods are generated into: each generated package declares its own
// errors.New values, so errors.Is only holds against the pair that arrived
// with the handle.
type oneColOneParamOneQuerier interface {
	personName(ctx context.Context, id int64) (string, error)
	errNoRows() error
	errMultipleResults() error
}

// manyColManyQuerier is one arm's many_col_many handle. The generated Params
// and Row types are package-local to each target, so they stop here.
type manyColManyQuerier interface {
	peopleByAgeAndLocale(ctx context.Context, minAge int64, locale string) ([]person, error)
}

// entityNodeQuerier is one arm's entity_node_projected_one handle and
// entityEdgeQuerier its entity_edge_projected_one one. Each declares
// errNoRows for the same reason the scalar handle does: the sentinel
// belongs to the package the method was generated into.
type entityNodeQuerier interface {
	onePerson(ctx context.Context) (personEntity, error)
	errNoRows() error
}

type entityEdgeQuerier interface {
	oneActedIn(ctx context.Context) (actedInEntity, error)
	errNoRows() error
}

// mixedReadWriteBatchQuerier is one arm's mixed_read_write_batch handle.
type mixedReadWriteBatchQuerier interface {
	getPersonName(ctx context.Context, id int64) (string, error)
	removePerson(ctx context.Context, id int64) error
	errNoRows() error
}

// edgeUnionQuerier is one arm's edge_union_undeclared_relationship_type
// handle. The label a candidate carries is narrowed away by the adapter, so
// what the scenario sees is which arm of the emitted dispatch ran.
type edgeUnionQuerier interface {
	actionOnPost(ctx context.Context, postID int64) (edgeUnionAction, error)
	errNoRows() error
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

// writeHarness is an arm whose target emits :exec methods, so the write
// scenarios have a handle to run against.
type writeHarness interface {
	harness
	writeScenario(ctx context.Context, t *testing.T) writeBackend
}

// edgeUnionHarness is an arm whose target emits an edge-union dispatch. Not
// every arm does: this fixture's candidates carry distinct labels, and a
// pattern naming several relationship types is a relationship-type
// alternation, which Apache AGE's parser refuses — so that backend refuses
// the column at generation instead of emitting a dispatch behind a statement
// no author could send. TestAGERefusesRelationshipTypeAlternation measures the
// refusal that this split rests on. (A multi-candidate column whose candidates
// repeat a label needs no alternation and is refused by the shared admission
// every backend runs, so it never reaches this split on any arm.)
type edgeUnionHarness interface {
	harness
	edgeUnionScenario(ctx context.Context, t *testing.T) edgeUnionBackend
}

// backend is one scenario's isolated view of an arm: a graph no other
// scenario observes, and the generated handles bound to it.
//
// seed writes through the driver, never through generated code, so seeded
// data is independent of the surface under test. Its cypher stays inside the
// openCypher dialect intersection so one string serves every arm.
type backend interface {
	seed(ctx context.Context, t *testing.T, cypher string)
	oneColOneParamOne() oneColOneParamOneQuerier
	manyColMany() manyColManyQuerier
	entityNodeProjectedOne() entityNodeQuerier
	entityEdgeProjectedOne() entityEdgeQuerier
}

// writeBackend is a scenario's view of a writeHarness.
type writeBackend interface {
	backend
	mixedReadWriteBatch() mixedReadWriteBatchQuerier
}

// edgeUnionBackend is a scenario's view of an edgeUnionHarness.
type edgeUnionBackend interface {
	backend
	edgeUnionUndeclared() edgeUnionQuerier
}

// arms are the backends the battery runs against. Each adapter owns its
// container, its connection, and its isolation strategy.
//
// writes records that the arm's target emits the write fixture, and edgeUnions
// that it emits an edge-union dispatch. TestLiveSmoke holds each harness to its
// column, so an arm that stops satisfying writeHarness or edgeUnionHarness
// fails the battery rather than dropping those scenarios unremarked — and an
// arm that starts satisfying one fails it too, which is what would happen if
// Apache AGE gained the alternation and the backend's refusal were lifted.
var arms = []struct {
	name       string
	start      func(ctx context.Context, t *testing.T) harness
	writes     bool
	edgeUnions bool
}{
	{name: "neo4j-go-v5", start: startNeo4jV5, writes: true, edgeUnions: true},
	{name: "neo4j-go-v6", start: startNeo4jV6, writes: true, edgeUnions: true},
	{name: "apache-age-pgx-v5", start: startAGE, writes: true},
}

// readScenarios are the battery every arm runs. Each body is written once
// against backend. A body must not call t.Helper(): its own frame is where
// the assertions live, so marking it a helper attributes every failure to the
// loop in TestLiveSmoke instead of to the line that failed.
var readScenarios = []struct {
	name string
	run  func(ctx context.Context, t *testing.T, b backend)
}{
	{name: "one_col_one_param_one: one + sentinels", run: oneAndSentinels},
	{name: "many_col_many: many + params", run: manyWithParams},
	{name: "entity_node_projected_one: whole vertex", run: nodeEntityRead},
	{name: "entity_edge_projected_one: whole edge", run: edgeEntityRead},
}

// writeScenarios are the battery an arm runs once its target emits :exec
// methods, written against writeBackend under the same rules.
var writeScenarios = []struct {
	name string
	run  func(ctx context.Context, t *testing.T, b writeBackend)
}{
	{name: "mixed_read_write_batch: exec + re-read", run: execWrite},
}

// edgeUnionScenarios are the battery an arm runs once its target emits an
// edge-union dispatch, written against edgeUnionBackend under the same rules.
var edgeUnionScenarios = []struct {
	name string
	run  func(ctx context.Context, t *testing.T, b edgeUnionBackend)
}{
	{name: "edge_union_undeclared_relationship_type: label dispatch", run: edgeUnionDispatch},
}

// TestLiveSmoke runs every scenario against every arm. Arms call t.Parallel()
// so their container boots overlap: three containers, ~4GB peak, well within
// a standard CI runner. Scenarios share their arm's container, amortising the
// startup, and run concurrently or not as that arm's isolation allows.
//
// Skips when GQLC_SKIP_LIVE is set so a developer without docker can still
// run `go test -tags codegen_live ./...` without a hard failure.
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("GQLC_SKIP_LIVE") != "" {
		t.Skip("GQLC_SKIP_LIVE set; skipping live backend containers")
	}
	t.Parallel()
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
			for _, sc := range readScenarios {
				t.Run(sc.name, func(t *testing.T) {
					if parallelScenarios {
						t.Parallel()
					}
					sc.run(ctx, t, h.scenario(ctx, t))
				})
			}

			eh, servesEdgeUnions := h.(edgeUnionHarness)
			require.Equal(t, arm.edgeUnions, servesEdgeUnions,
				"the arm's edge-union capability must match the arms table; a target that gained or lost the dispatch updates both")
			if servesEdgeUnions {
				for _, sc := range edgeUnionScenarios {
					t.Run(sc.name, func(t *testing.T) {
						if parallelScenarios {
							t.Parallel()
						}
						sc.run(ctx, t, eh.edgeUnionScenario(ctx, t))
					})
				}
			}

			wh, servesWrites := h.(writeHarness)
			require.Equal(t, arm.writes, servesWrites,
				"the arm's write capability must match the arms table; a target that gained or lost :exec emission updates both")
			if !servesWrites {
				return
			}
			for _, sc := range writeScenarios {
				t.Run(sc.name, func(t *testing.T) {
					if parallelScenarios {
						t.Parallel()
					}
					sc.run(ctx, t, wh.writeScenario(ctx, t))
				})
			}
		})
	}
}

// oneAndSentinels drives the scalar :one contract — both sentinels and a
// single-row read.
func oneAndSentinels(ctx context.Context, t *testing.T, b backend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.oneColOneParamOne()

	// errors.Is (via require.ErrorIs) confirms the sentinel is
	// identity-matchable so callers can branch generically.
	_, err := q.personName(ctx, 1)
	require.ErrorIs(t, err, q.errNoRows(), "empty graph must return ErrNoRows")

	b.seed(ctx, t, "CREATE (:Person {id: 1, name: 'Alice'})")

	name, err := q.personName(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "Alice", name)

	// Two rows for the same id triggers ErrMultipleResults.
	b.seed(ctx, t, "CREATE (:Person {id: 1, name: 'AliceTwin'})")
	_, err = q.personName(ctx, 1)
	require.ErrorIs(t, err, q.errMultipleResults(), "two matching rows must return ErrMultipleResults")
}

// manyWithParams drives the :many contract — parameter binding narrows the
// result set, and an empty result is (empty, nil) rather than a sentinel.
func manyWithParams(ctx context.Context, t *testing.T, b backend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
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

// nodeEntityRead drives the node-entity contract — a whole vertex arrives
// as the struct the schema names, carrying every property it declares.
//
// The middleName arm holds what a nullable property does on the wire. A
// writer says "no value" with an explicit null, and both stores answer by
// keeping no property at all: neither AGE nor neo4j has a stored null, so
// the write below and a write that omitted the key entirely are the same
// vertex, and the decoder's nullable path is reached by absence in both
// arms. That is asserted here rather than assumed, because it is the
// premise the emitted agtypeNullableProperty rests on.
func nodeEntityRead(ctx context.Context, t *testing.T, b backend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.entityNodeProjectedOne()

	_, err := q.onePerson(ctx)
	require.ErrorIs(t, err, q.errNoRows(), "empty graph must return ErrNoRows")

	b.seed(ctx, t, "CREATE (:Person {id: 7, name: 'Alice', middleName: null})")

	got, err := q.onePerson(ctx)
	require.NoError(t, err)
	require.Equal(t, personEntity{ID: 7, Name: "Alice"}, got,
		"an explicitly null property must decode as nil, not as an error")

	b.seed(ctx, t, "MATCH (p:Person {id: 7}) SET p.middleName = 'Q'")

	middleName := "Q"
	got, err = q.onePerson(ctx)
	require.NoError(t, err)
	require.Equal(t, personEntity{ID: 7, Name: "Alice", MiddleName: &middleName}, got)
}

// edgeEntityRead drives the edge-entity contract — a whole edge arrives as
// its own struct carrying the property declared on the edge, not one
// gathered from either vertex it joins.
func edgeEntityRead(ctx context.Context, t *testing.T, b backend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.entityEdgeProjectedOne()

	_, err := q.oneActedIn(ctx)
	require.ErrorIs(t, err, q.errNoRows(), "empty graph must return ErrNoRows")

	// The vertices carry an id the edge does not, so a decode that read the
	// wrong end of the relationship has nowhere to find 2019.
	b.seed(ctx, t, "CREATE (:Person {id: 1})-[:ACTED_IN {since: 2019}]->(:Movie {id: 2})")

	got, err := q.oneActedIn(ctx)
	require.NoError(t, err)
	require.Equal(t, actedInEntity{Since: 2019}, got)
}

// edgeUnionDispatch drives the edge-union contract — the label off the wire
// chooses which candidate's decoder fills the column, and a label outside the
// candidate set fails the row rather than picking an arm at random.
//
// The default arm is reachable only because the query names a relationship
// type the schema does not declare. gqlc narrows the candidate set to the two
// it does declare and leaves the query text alone (ADR 0005), so the server
// matches FLAGGED edges that the sealed interface has no member for. That is
// schema drift, not a contrivance: it is what an author has the moment their
// graph grows a relationship type ahead of their GQL schema, and it is the
// only shape in which the emitted default arm ever runs.
func edgeUnionDispatch(ctx context.Context, t *testing.T, b edgeUnionBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.edgeUnionUndeclared()

	_, err := q.actionOnPost(ctx, 10)
	require.ErrorIs(t, err, q.errNoRows(), "empty graph must return ErrNoRows")

	// One post per relationship type, so the bound parameter selects which
	// label the single row carries and each call exercises one arm.
	b.seed(ctx, t, `
		CREATE (author:Person {id: 1})
		CREATE (authored:Post {id: 10})
		CREATE (liked:Post {id: 20})
		CREATE (flagged:Post {id: 30})
		CREATE (author)-[:AUTHORED {since: 2019}]->(authored)
		CREATE (author)-[:LIKES {rating: 5}]->(liked)
		CREATE (author)-[:FLAGGED]->(flagged)
	`)

	// Each candidate carries a property the other does not, so an arm that
	// decoded through the wrong candidate cannot produce this value.
	got, err := q.actionOnPost(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, edgeUnionAction{Kind: "AUTHORED", Since: 2019}, got)

	got, err = q.actionOnPost(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, edgeUnionAction{Kind: "LIKES", Rating: 5}, got)

	// The label outside the candidate set. The generated dispatch has no case
	// for it and the sealed interface no member, so the row fails and names
	// what arrived — a nil interface returned without an error would be
	// indistinguishable to a caller from a column that was legitimately absent.
	//
	// Which failure it is matters as much as that it is one: the row EXISTS,
	// so reporting it as ErrNoRows would send the author looking for a post
	// their graph has. The returned value is not asserted — every adapter
	// returns the zero action on every error path, so an assertion on it
	// would hold whatever the dispatch did.
	_, err = q.actionOnPost(ctx, 30)
	require.Error(t, err, "a label outside the candidate set must fail the row")
	require.NotErrorIs(t, err, q.errNoRows(),
		"the row arrived and could not be decoded; that is not an absent row")
	require.ErrorContains(t, err, `unexpected relationship type "FLAGGED"`,
		"the failure must name the label that arrived")
}

// execWrite drives the :exec contract — the write reaches the graph, and the
// bound parameter narrows what it reaches.
func execWrite(ctx context.Context, t *testing.T, b writeBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.mixedReadWriteBatch()

	b.seed(ctx, t, `
		CREATE (:Person {id: 1, name: 'Alice'})
		CREATE (:Person {id: 2, name: 'Bob'})
	`)

	require.NoError(t, q.removePerson(ctx, 1))

	_, err := q.getPersonName(ctx, 1)
	require.ErrorIs(t, err, q.errNoRows(), "after :exec delete, :one must see empty result")

	survivor, err := q.getPersonName(ctx, 2)
	require.NoError(t, err, "the delete must be narrowed by its parameter")
	require.Equal(t, "Bob", survivor)
}
