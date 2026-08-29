//go:build codegen_live

// Live smoke battery for the generated repositories: every scenario runs
// against every backend arm, driving a real container (gqlc-73h, gqlc-5gc,
// gqlc-35yu.8). Opt-in via -tags codegen_live so PR CI stays fast; the manual
// / nightly CI job runs it. Lives in the nested test/data/codegen module so
// testcontainers and its ~50 transitive deps stay out of gqlc's root go.mod
// and the compiler binary.
//
// The battery must stay an external test package. govulncheck builds its
// package graph keyed by PkgPath and skips any package whose PkgPath is already
// present, without descending into that package's imports
// (PackageGraph.AddPackages, x/vuln internal/vulncheck/packages.go). An
// in-package test variant p [p.test] carries PkgPath p, the same key as the
// plain package, which is added first — so the variant and every dependency
// only it imports drop out of the scan with no diagnostic. That is not
// conditional on there being any non-test source to lose to: a directory of
// nothing but in-package _test.go files still yields an empty plain entry that
// takes the key. An external test package survives because PkgPath p_test
// collides with nothing. `just test-codegen-fence` enforces the packaging and
// separately asserts that testcontainers-go is still in the closure govulncheck
// loads (bd gqlc-rohp).
//
// The same defect is still open in the root module — 34 in-package test files
// there, so a called vulnerability reachable only from one of them exits 0.
// `just vuln` prints the current number; bd gqlc-m5rc closes it.

package fixtures_test

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

// txQuerier is one arm's mixed_read_write_batch handle, bound to no
// transaction, plus what the Tx scenarios branch on. Every arm's target
// emits the Tx block, so this rides the write fixture rather than
// splitting a capability off the arms table.
type txQuerier interface {
	begin(ctx context.Context) (liveTx, error)
	getPersonName(ctx context.Context, id int64) (string, error)
	errNoRows() error
	errTxDone() error
}

// liveTx is one arm's generated *Tx behind an interface. Every arm wraps
// its own rather than satisfying begin directly: Begin returns a concrete
// *Tx and Go has no covariant returns.
//
// removePerson and getPersonName run through the handle Tx.Queries hands
// out, so what they see is the transaction's own view and not the graph's
// — the distinction txReadsOwnUncommitted exists to read.
type liveTx interface {
	commit(ctx context.Context) error
	rollback(ctx context.Context) error
	removePerson(ctx context.Context, id int64) error
	getPersonName(ctx context.Context, id int64) (string, error)
	// beginNested calls Begin on the handle this transaction hands out
	// and returns Begin's own error verbatim, nil included. A nil is the
	// refusal not firing, which is the failure the scenario looks for, so
	// it must not be dressed up as an error. t is for the cleanup on that
	// path only: an arm whose Begin was served gives the transaction back
	// before returning, and has nowhere else to report a failure to.
	beginNested(ctx context.Context, t *testing.T) error
}

// edgeUnionQuerier is one arm's edge_union_undeclared_relationship_type
// handle. The label a candidate carries is narrowed away by the adapter, so
// what the scenario sees is which arm of the emitted dispatch ran.
type edgeUnionQuerier interface {
	actionOnPost(ctx context.Context, postID int64) (edgeUnionAction, error)
	errNoRows() error
}

// timestampRoundtripQuerier is one arm's timestamp_property_roundtrip
// handle: an instant out through a bound parameter, and back through a
// projected column, a whole vertex, and a range predicate.
//
// Every method takes and returns time.Time on every arm, which is the
// point of the fixture. The Apache AGE arm stores the property as an
// integer count of microseconds because agtype has no temporal value,
// and no part of that reaches this interface.
type timestampRoundtripQuerier interface {
	addEvent(ctx context.Context, id int64, occurredAt time.Time) error
	eventsAfter(ctx context.Context, since time.Time) ([]int64, error)
	eventAt(ctx context.Context, id int64) (time.Time, error)
	oneEvent(ctx context.Context, id int64) (eventEntity, error)
}

// eventEntity is a timestamp_property_roundtrip vertex. Each target
// emits its own Event struct; this is the shape the scenario reads.
type eventEntity struct {
	ID         int64
	OccurredAt time.Time
	SeenAt     *time.Time
}

// The battery's own spelling of the four neutral temporal carriers
// (ADR 0033). Each target emits its own Date / LocalTime /
// LocalDateTime / Duration into its own package, so — as with person
// and personEntity — the shape has to be restated here for the
// scenarios to read components off it. Restating it is itself the
// check that the shape does not vary by arm: an arm whose carrier
// gained, lost or renamed a component stops compiling against its own
// adapter.
//
// A component count is what these hold and a wire encoding is not. The
// bolt packer sends a date as epoch-days and a local time as
// nanoseconds since midnight, and no part of that reaches here.
type dateValue struct {
	Year, Month, Day int
}

type localTimeValue struct {
	Hour, Minute, Second, Nanosecond int
}

type localDateTimeValue struct {
	Year, Month, Day                 int
	Hour, Minute, Second, Nanosecond int
}

type durationValue struct {
	Months, Days, Seconds int64
	Nanos                 int
}

// timeValue is the zoned width, carrying the offset the writer chose
// beside the clock reading. East-positive, matching both the wire and
// time.Time.Zone.
type timeValue struct {
	Hour, Minute, Second, Nanosecond int
	OffsetSeconds                    int
}

// readingEntity is a temporal_property_roundtrip vertex and slotEntity
// a zoned_time_roundtrip one.
type readingEntity struct {
	ID      int64
	OnDate  dateValue
	AtLocal localTimeValue
	Elapsed durationValue
	SeenOn  *dateValue
}

type slotEntity struct {
	ID       int64
	StartsAt timeValue
}

// temporalRoundtripQuerier is one arm's temporal_property_roundtrip
// handle: the three zoneless property widths out through bound
// parameters and back through projected columns and a whole vertex,
// plus the constructed LOCALDATETIME column — the only way a batch can
// reach that carrier, since the width has no property spelling.
//
// readingsSeenFrom takes a pointer because its parameter compares
// against a nullable property. A nil there must bind Cypher null and
// not a zero Date, and the scenario holds it to that.
type temporalRoundtripQuerier interface {
	addReading(ctx context.Context, id int64, onDate dateValue, atLocal localTimeValue, elapsed durationValue) error
	readingsFrom(ctx context.Context, from dateValue) ([]int64, error)
	readingsSeenFrom(ctx context.Context, seenFrom *dateValue) ([]int64, error)
	readingDate(ctx context.Context, id int64) (dateValue, error)
	readingLocalTime(ctx context.Context, id int64) (localTimeValue, error)
	readingElapsed(ctx context.Context, id int64) (durationValue, error)
	oneReading(ctx context.Context, id int64) (readingEntity, error)
	builtLocalDateTime(ctx context.Context) (localDateTimeValue, error)
	errNoRows() error
}

// zonedTimeRoundtripQuerier is one arm's zoned_time_roundtrip handle.
type zonedTimeRoundtripQuerier interface {
	addSlot(ctx context.Context, id int64, startsAt timeValue) error
	slotsFrom(ctx context.Context, from timeValue) ([]int64, error)
	slotStart(ctx context.Context, id int64) (timeValue, error)
	oneSlot(ctx context.Context, id int64) (slotEntity, error)
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

// temporalHarness is an arm whose target admits the zoneless temporal
// widths, and zonedTimeHarness one that admits TIME WITH TIME ZONE.
// Two columns and not one because the two go their own way: Apache AGE
// has no agtype temporal value at all today and refuses both, and the
// work that lifts each refusal is separate (gqlc-mv3r for the zoneless
// widths, gqlc-oeqi for the zoned one), so an arm will hold one and not
// the other before it holds both.
type temporalHarness interface {
	harness
	temporalScenario(ctx context.Context, t *testing.T) temporalBackend
}

type zonedTimeHarness interface {
	harness
	zonedTimeScenario(ctx context.Context, t *testing.T) zonedTimeBackend
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
	timestampRoundtrip() timestampRoundtripQuerier
	tx() txQuerier
}

// edgeUnionBackend is a scenario's view of an edgeUnionHarness.
type edgeUnionBackend interface {
	backend
	edgeUnionUndeclared() edgeUnionQuerier
}

// temporalBackend is a scenario's view of a temporalHarness and
// zonedTimeBackend of a zonedTimeHarness.
type temporalBackend interface {
	backend
	temporalRoundtrip() temporalRoundtripQuerier
}

type zonedTimeBackend interface {
	backend
	zonedTimeRoundtrip() zonedTimeRoundtripQuerier
}

// arms are the backends the battery runs against. Each adapter owns its
// container, its connection, and its isolation strategy.
//
// writes records that the arm's target emits the write fixture, edgeUnions
// that it emits an edge-union dispatch, temporals that it admits the zoneless
// temporal widths, and zonedTime that it admits TIME WITH TIME ZONE.
// TestLiveSmoke holds each harness to its column, so an arm that stops
// satisfying writeHarness, edgeUnionHarness, temporalHarness or
// zonedTimeHarness fails the battery rather than dropping those scenarios
// unremarked — and an arm that starts satisfying one fails it too, which is
// what would happen if Apache AGE gained the alternation and the backend's
// refusal were lifted, or if it gained a temporal encoding.
var arms = []struct {
	name       string
	start      func(ctx context.Context, t *testing.T) harness
	writes     bool
	edgeUnions bool
	temporals  bool
	zonedTime  bool
}{
	{name: "neo4j-go-v5", start: startNeo4jV5, writes: true, edgeUnions: true, temporals: true, zonedTime: true},
	{name: "neo4j-go-v6", start: startNeo4jV6, writes: true, edgeUnions: true, temporals: true, zonedTime: true},
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
	{name: "timestamp_property_roundtrip: instant round trip + ordering", run: timestampRoundTrip},
	{name: "tx: commit is visible outside", run: txCommitVisible},
	{name: "tx: rollback leaves the row", run: txRollbackAbsent},
	{name: "tx: reads its own uncommitted write", run: txReadsOwnUncommitted},
	{name: "tx: second Commit is ErrTxDone", run: txDoubleCommitIsRefused},
	{name: "tx: Rollback after Commit is nil", run: txRollbackAfterCommitIsNil},
	{name: "tx: Begin inside a transaction is refused", run: txBeginIsRefusedInsideATransaction},
}

// temporalScenarios and zonedTimeScenarios are the batteries an arm runs once
// its target admits the temporal widths, written against their backends under
// the same rules.
var temporalScenarios = []struct {
	name string
	run  func(ctx context.Context, t *testing.T, b temporalBackend)
}{
	{name: "temporal_property_roundtrip: components round trip + date ordering", run: temporalRoundTrip},
}

var zonedTimeScenarios = []struct {
	name string
	run  func(ctx context.Context, t *testing.T, b zonedTimeBackend)
}{
	{name: "zoned_time_roundtrip: offset preserved + instant ordering", run: zonedTimeRoundTrip},
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

			th, servesTemporals := h.(temporalHarness)
			require.Equal(t, arm.temporals, servesTemporals,
				"the arm's temporal capability must match the arms table; a target that gained or lost the zoneless temporal widths updates both")
			if servesTemporals {
				for _, sc := range temporalScenarios {
					t.Run(sc.name, func(t *testing.T) {
						if parallelScenarios {
							t.Parallel()
						}
						sc.run(ctx, t, th.temporalScenario(ctx, t))
					})
				}
			}

			zh, servesZonedTime := h.(zonedTimeHarness)
			require.Equal(t, arm.zonedTime, servesZonedTime,
				"the arm's zoned-time capability must match the arms table; a target that gained or lost TIME WITH TIME ZONE updates both")
			if servesZonedTime {
				for _, sc := range zonedTimeScenarios {
					t.Run(sc.name, func(t *testing.T) {
						if parallelScenarios {
							t.Parallel()
						}
						sc.run(ctx, t, zh.zonedTimeScenario(ctx, t))
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

// txSeed is what every Tx scenario starts from. Two people rather than
// one, so a write that ignored its parameter and emptied the graph is
// distinguishable from the narrowed delete each scenario asks for.
const txSeed = `
	CREATE (:Person {id: 1, name: 'Alice'})
	CREATE (:Person {id: 2, name: 'Bob'})
`

// txNestedRefusal is the message both backends' Begin returns when the
// handle it is called on is already bound to a transaction. The scenario
// asserts the text and not merely that an error came back, because Begin
// has other ways to fail — a driver that cannot open a session returns an
// error too, and would satisfy a bare require.Error while the refusal
// under test never fired.
const txNestedRefusal = "gqlc: Begin on a transaction-bound Queries"

// openTx begins a transaction and registers a rollback, so a scenario
// that fails an assertion mid-transaction does not also strand the
// connection the Tx holds. Rollback is nil on a finished transaction, so
// this is correct beside a later Commit and needs no guard.
func openTx(ctx context.Context, t *testing.T, q txQuerier) liveTx {
	t.Helper()
	tx, err := q.begin(ctx)
	require.NoError(t, err, "Begin on a handle bound to the driver must open a transaction")
	t.Cleanup(func() {
		if err := tx.rollback(ctx); err != nil {
			t.Logf("rollback open transaction: %v", err)
		}
	})
	return tx
}

// txCommitVisible drives the committed write out past the transaction
// that made it. The read is on the outer handle, which is bound to no
// transaction, so what it sees is the graph rather than the Tx's view.
//
// No scenario here reads the outer handle while a transaction is open:
// the delete holds a write lock on the vertex, and a concurrent read of
// it would block until the transaction finished rather than report
// anything.
func txCommitVisible(ctx context.Context, t *testing.T, b writeBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.tx()
	b.seed(ctx, t, txSeed)

	tx := openTx(ctx, t, q)
	require.NoError(t, tx.removePerson(ctx, 1))
	require.NoError(t, tx.commit(ctx))

	_, err := q.getPersonName(ctx, 1)
	require.ErrorIs(t, err, q.errNoRows(), "a committed delete must be visible to a handle outside the transaction")

	survivor, err := q.getPersonName(ctx, 2)
	require.NoError(t, err, "the commit must carry the transaction's write and no more")
	require.Equal(t, "Bob", survivor)
}

// txRollbackAbsent is the other half: the write reached the transaction,
// and the rollback kept it from reaching the graph.
func txRollbackAbsent(ctx context.Context, t *testing.T, b writeBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.tx()
	b.seed(ctx, t, txSeed)

	tx := openTx(ctx, t, q)
	require.NoError(t, tx.removePerson(ctx, 1))
	require.NoError(t, tx.rollback(ctx))

	name, err := q.getPersonName(ctx, 1)
	require.NoError(t, err, "a rolled-back delete must leave the row where it was")
	require.Equal(t, "Alice", name)
}

// txReadsOwnUncommitted holds the seam Tx.Queries exists for: a handle
// that reads inside the transaction rather than beside it.
//
// The rollback at the end is not cleanup. It is what makes the read above
// evidence of uncommitted state: without it, a Tx.Queries that had quietly
// bound the driver instead of the transaction would answer the same way,
// because the delete would have landed for real.
func txReadsOwnUncommitted(ctx context.Context, t *testing.T, b writeBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.tx()
	b.seed(ctx, t, txSeed)

	tx := openTx(ctx, t, q)
	require.NoError(t, tx.removePerson(ctx, 1))

	_, err := tx.getPersonName(ctx, 1)
	require.ErrorIs(t, err, q.errNoRows(), "a handle from Tx.Queries must read the transaction's own uncommitted delete")

	require.NoError(t, tx.rollback(ctx))
	name, err := q.getPersonName(ctx, 1)
	require.NoError(t, err, "the delete the transaction saw must not have reached the graph")
	require.Equal(t, "Alice", name)
}

// txDoubleCommitIsRefused holds the done flag. The second Commit is
// answered by the generated code, without reaching the driver.
func txDoubleCommitIsRefused(ctx context.Context, t *testing.T, b writeBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.tx()
	b.seed(ctx, t, txSeed)

	tx := openTx(ctx, t, q)
	require.NoError(t, tx.removePerson(ctx, 1))
	require.NoError(t, tx.commit(ctx))

	require.ErrorIs(t, tx.commit(ctx), q.errTxDone(),
		"a second Commit must be refused with ErrTxDone rather than reaching a driver whose transaction is gone")
}

// txRollbackAfterCommitIsNil holds the asymmetry between the two
// finishers: Commit refuses a second call, Rollback answers nil, so a
// deferred Rollback beside a Commit is correct and needs no guard.
//
// The re-read is what separates the nil from a nil that undid the commit.
func txRollbackAfterCommitIsNil(ctx context.Context, t *testing.T, b writeBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.tx()
	b.seed(ctx, t, txSeed)

	tx := openTx(ctx, t, q)
	require.NoError(t, tx.removePerson(ctx, 1))
	require.NoError(t, tx.commit(ctx))

	require.NoError(t, tx.rollback(ctx),
		"Rollback on a finished transaction must return nil, not ErrTxDone")

	_, err := q.getPersonName(ctx, 1)
	require.ErrorIs(t, err, q.errNoRows(), "the late Rollback must not have reached the driver")
}

// txBeginIsRefusedInsideATransaction holds the nesting refusal. Neo4j
// cannot nest, and a surface that nests on the other backend is the
// portability failure the Tx object exists to remove, so AGE refuses too
// rather than opening a savepoint.
func txBeginIsRefusedInsideATransaction(ctx context.Context, t *testing.T, b writeBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.tx()
	b.seed(ctx, t, txSeed)

	tx := openTx(ctx, t, q)

	err := tx.beginNested(ctx, t)
	require.Error(t, err, "Begin on a handle already bound to a transaction must be refused, not served")
	require.ErrorContains(t, err, txNestedRefusal,
		"the refusal must name itself; another failure of Begin would satisfy the line above while the refusal never fired")
}

// eventInstants are the instants the timestamp scenario writes, keyed by
// the id it writes them under. The ids are deliberately not in
// chronological order, so a projection ordered by anything but the
// instant — insertion order, the id, the property's text form — answers
// the ordering probe differently from the correct answer.
//
// The values are the ones an encoding fails on rather than a comfortable
// middle: one before the Unix epoch, so a count that cannot go negative
// breaks; the epoch itself, so a strict `>` against it has a boundary to
// get wrong; one carrying microseconds, so a resolution coarser than the
// encoding claims loses them; and one at the far end of the range, so a
// unit off by a thousand overflows or wraps.
var eventInstants = []struct {
	id int64
	at time.Time
}{
	{id: 1, at: time.Date(2024, 1, 1, 12, 34, 56, 123456000, time.UTC)},
	{id: 2, at: time.Date(1969, 7, 20, 20, 17, 40, 0, time.UTC)},
	{id: 3, at: time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)},
	{id: 4, at: time.Unix(0, 0).UTC()},
}

// timestampRoundTrip drives the TIMESTAMP property contract: an instant
// written through a bound parameter comes back the same instant, from a
// projected column and from inside a whole vertex alike, and a range
// predicate plus an ORDER BY over the stored property answer
// chronologically.
//
// The ordering half is the half that is not implied by the round trip.
// Apache AGE has no temporal value, so gqlc stores the property as a
// count of microseconds; an ISO-8601 text encoding would round-trip
// exactly as well and sort by database collation, which is a different
// order. Nothing but a query the server orders can tell those apart, and
// the query text here is the author's, run verbatim.
func timestampRoundTrip(ctx context.Context, t *testing.T, b writeBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.timestampRoundtrip()

	for _, e := range eventInstants {
		require.NoError(t, q.addEvent(ctx, e.id, e.at), "write event %d", e.id)
	}

	for _, e := range eventInstants {
		got, err := q.eventAt(ctx, e.id)
		require.NoError(t, err, "read event %d", e.id)
		require.True(t, e.at.Equal(got), "event %d: wrote %s, read %s", e.id, e.at, got)
		require.Equal(t, e.at, got.UTC(), "event %d must survive the encoding to the microsecond", e.id)

		entity, err := q.oneEvent(ctx, e.id)
		require.NoError(t, err, "read event %d as a vertex", e.id)
		require.Equal(t, e.id, entity.ID)
		require.True(t, e.at.Equal(entity.OccurredAt),
			"event %d: a whole vertex must carry the same instant its column does", e.id)
		require.Nil(t, entity.SeenAt, "event %d: an unwritten nullable instant is absent, not a zero time", e.id)
	}

	// The whole set, ordered by the author's ORDER BY. Chronological, so
	// the pre-epoch event leads and the id order 1,2,3,4 it was written
	// in does not survive.
	all, err := q.eventsAfter(ctx, time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, []int64{2, 4, 1, 3}, all,
		"ORDER BY over the stored instant must answer chronologically")

	// Strictly greater: the event written at the epoch is the cutoff, so
	// it is excluded along with the one before it.
	after, err := q.eventsAfter(ctx, time.Unix(0, 0).UTC())
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3}, after,
		"a range predicate over the stored instant must be strict and chronological")

	// One microsecond past the 2024 event excludes it and keeps the one
	// after it: the resolution the encoding claims is the resolution the
	// comparison has.
	fine, err := q.eventsAfter(ctx, eventInstants[0].at.Add(time.Microsecond))
	require.NoError(t, err)
	require.Equal(t, []int64{3}, fine, "the comparison must resolve to the microsecond")
}

// temporalReadings are the values the temporal scenario writes, keyed by
// the id it writes them under. As with eventInstants the ids are not in
// date order, so a projection ordered by anything but the stored date
// answers the ordering probe differently from the correct answer.
//
// The dates are chosen where a conversion breaks rather than where it is
// comfortable: one before the Unix epoch, because the bolt packer sends
// a date as a count of epoch-days and a truncating division answers a
// negative count off by one unless the value was built at exact midnight;
// a leap day, because a component conversion that went through a
// day-of-year loses it; and the first of a month, because an off-by-one
// on the month component is invisible on most other days.
//
// The durations stay inside DAY TO SECOND — the schema's declared
// precision — and carry nanoseconds, because the bolt encoding sends
// seconds and nanoseconds apart and a conversion that flattened them
// would lose the sub-second half.
var temporalReadings = []struct {
	id      int64
	onDate  dateValue
	atLocal localTimeValue
	elapsed durationValue
}{
	{
		id:      1,
		onDate:  dateValue{Year: 2024, Month: 2, Day: 29},
		atLocal: localTimeValue{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999},
		elapsed: durationValue{Days: 3, Seconds: 4, Nanos: 500000000},
	},
	{
		id:      2,
		onDate:  dateValue{Year: 1969, Month: 7, Day: 20},
		atLocal: localTimeValue{Hour: 0, Minute: 0, Second: 0, Nanosecond: 0},
		elapsed: durationValue{Days: 0, Seconds: 0, Nanos: 1},
	},
	{
		id:      3,
		onDate:  dateValue{Year: 2025, Month: 12, Day: 1},
		atLocal: localTimeValue{Hour: 6, Minute: 7, Second: 8, Nanosecond: 9},
		elapsed: durationValue{Days: 400, Seconds: 86399, Nanos: 0},
	},
}

// temporalRoundTrip drives the zoneless temporal widths through the
// neutral carriers of ADR 0033: a date, a local time and a duration
// written through bound parameters come back the same components, from
// a projected column and from inside a whole vertex alike, and a range
// predicate plus an ORDER BY over the stored date answer in date order.
//
// This is the witness the unit conversions do not supply. toDate and
// fromDate are emitted into the generated package and read the
// components off a time.Time the driver packs and the server stores; a
// pair that agreed with each other and disagreed with the wire would
// round-trip through themselves perfectly and still answer the ordering
// probe wrong, because the ordering is the server's and the server sees
// only what the packer sent.
func temporalRoundTrip(ctx context.Context, t *testing.T, b temporalBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.temporalRoundtrip()

	_, err := q.readingDate(ctx, 1)
	require.ErrorIs(t, err, q.errNoRows(), "empty graph must return ErrNoRows")

	for _, r := range temporalReadings {
		require.NoError(t, q.addReading(ctx, r.id, r.onDate, r.atLocal, r.elapsed), "write reading %d", r.id)
	}

	for _, r := range temporalReadings {
		gotDate, err := q.readingDate(ctx, r.id)
		require.NoError(t, err, "read reading %d date", r.id)
		require.Equal(t, r.onDate, gotDate, "reading %d: the date must survive the encoding component for component", r.id)

		gotLocal, err := q.readingLocalTime(ctx, r.id)
		require.NoError(t, err, "read reading %d local time", r.id)
		require.Equal(t, r.atLocal, gotLocal, "reading %d: the local time must survive to the nanosecond", r.id)

		gotElapsed, err := q.readingElapsed(ctx, r.id)
		require.NoError(t, err, "read reading %d duration", r.id)
		require.Equal(t, r.elapsed, gotElapsed, "reading %d: the duration's components must survive held apart", r.id)

		entity, err := q.oneReading(ctx, r.id)
		require.NoError(t, err, "read reading %d as a vertex", r.id)
		require.Equal(t, readingEntity{ID: r.id, OnDate: r.onDate, AtLocal: r.atLocal, Elapsed: r.elapsed}, entity,
			"reading %d: a whole vertex must carry the same components its columns do, and an unwritten nullable date must be absent rather than a zero Date", r.id)
	}

	// The whole set, ordered by the author's ORDER BY. In date order, so
	// the pre-epoch reading leads and the id order 1,2,3 it was written
	// in does not survive.
	all, err := q.readingsFrom(ctx, dateValue{Year: 1900, Month: 1, Day: 1})
	require.NoError(t, err)
	require.Equal(t, []int64{2, 1, 3}, all, "ORDER BY over the stored date must answer in date order")

	// The bound date is the cutoff and the comparison is inclusive, so the
	// reading written on exactly this date stays in. A conversion that
	// built its driver value anywhere but midnight would put the stored
	// value on either side of this boundary depending on the runner's
	// clock.
	from, err := q.readingsFrom(ctx, dateValue{Year: 2024, Month: 2, Day: 29})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3}, from, "a range predicate over the stored date must be inclusive and in date order")

	// A nullable date parameter. Nothing has written seenOn yet, so the
	// predicate has nothing to match either way; what it holds is that a
	// nil binds Cypher null, under which `>=` is null and no row is
	// returned. A nil that bound a zero Date would return every row.
	none, err := q.readingsSeenFrom(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, none, "a nil nullable date parameter must bind null, not a zero Date")

	b.seed(ctx, t, "MATCH (r:Reading {id: 1}) SET r.seenOn = date('2024-03-01')")
	b.seed(ctx, t, "MATCH (r:Reading {id: 3}) SET r.seenOn = date('2023-01-31')")

	seen, err := q.readingsSeenFrom(ctx, &dateValue{Year: 2023, Month: 1, Day: 31})
	require.NoError(t, err)
	require.Equal(t, []int64{3, 1}, seen, "a bound nullable date must select and order like a non-nullable one")

	// The nullable property comes back on the whole vertex as a pointer
	// to the components the seed wrote — through the same conversion the
	// non-nullable arm uses, reached by a different emitted path.
	entity, err := q.oneReading(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, &dateValue{Year: 2024, Month: 3, Day: 1}, entity.SeenOn,
		"a written nullable date must arrive as its components, not as nil")

	// LOCALDATETIME has no property spelling, so the carrier is reached
	// through a column the server constructs. The literal is in the query
	// text, so the components are known exactly and a conversion that
	// dropped one is visible here and nowhere else in the battery.
	built, err := q.builtLocalDateTime(ctx)
	require.NoError(t, err)
	require.Equal(t, localDateTimeValue{Year: 2024, Month: 3, Day: 5, Hour: 6, Minute: 7, Second: 8, Nanosecond: 9}, built,
		"a constructed LOCALDATETIME column must arrive component for component")
}

// zonedSlots are the values the zoned-time scenario writes. The offsets
// are far from any runner's likely local zone and far from each other,
// and the clock readings are chosen so that the two orderings disagree:
// by the instant each denotes, slot 1 (03:45Z) precedes slot 2 (16:00Z);
// by the bare clock reading, 08:00 precedes 09:30 and the order reverses.
// A conversion that dropped the offset and built its driver value in the
// process's local zone therefore answers the ordering probe backwards
// rather than merely imprecisely.
var zonedSlots = []struct {
	id       int64
	startsAt timeValue
}{
	{id: 1, startsAt: timeValue{Hour: 9, Minute: 30, Second: 0, Nanosecond: 0, OffsetSeconds: 5*3600 + 45*60}},
	{id: 2, startsAt: timeValue{Hour: 8, Minute: 0, Second: 0, Nanosecond: 250000000, OffsetSeconds: -8 * 3600}},
}

// zonedTimeRoundTrip drives TIME WITH TIME ZONE through the neutral Time
// carrier: the clock reading and the offset the writer chose both come
// back, from a projected column and from inside a whole vertex alike,
// and an ORDER BY over the stored property answers by the instant each
// value denotes rather than by its bare clock reading.
func zonedTimeRoundTrip(ctx context.Context, t *testing.T, b zonedTimeBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.zonedTimeRoundtrip()

	_, err := q.slotStart(ctx, 1)
	require.ErrorIs(t, err, q.errNoRows(), "empty graph must return ErrNoRows")

	for _, s := range zonedSlots {
		require.NoError(t, q.addSlot(ctx, s.id, s.startsAt), "write slot %d", s.id)
	}

	for _, s := range zonedSlots {
		got, err := q.slotStart(ctx, s.id)
		require.NoError(t, err, "read slot %d", s.id)
		require.Equal(t, s.startsAt, got,
			"slot %d: the clock reading and the offset must both survive the encoding", s.id)

		entity, err := q.oneSlot(ctx, s.id)
		require.NoError(t, err, "read slot %d as a vertex", s.id)
		require.Equal(t, slotEntity{ID: s.id, StartsAt: s.startsAt}, entity,
			"slot %d: a whole vertex must carry the same offset its column does", s.id)
	}

	// Midnight UTC precedes both instants, so both rows come back — in
	// the order their offsets put them in, which is the reverse of the
	// order their clock readings alone would.
	all, err := q.slotsFrom(ctx, timeValue{Hour: 0, Minute: 0, Second: 0, Nanosecond: 0, OffsetSeconds: 0})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, all, "ORDER BY over a stored zoned time must answer by the instant, not by the clock reading")

	// A cutoff between the two instants, expressed in a third offset: the
	// earlier slot drops out. Its own clock reading, 09:30, is later than
	// this bound's 06:00 — so a comparison that had lost the offsets would
	// keep it.
	after, err := q.slotsFrom(ctx, timeValue{Hour: 6, Minute: 0, Second: 0, Nanosecond: 0, OffsetSeconds: 2 * 3600})
	require.NoError(t, err)
	require.Equal(t, []int64{2}, after, "a range predicate over a stored zoned time must compare instants")
}
