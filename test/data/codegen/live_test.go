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
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
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

// nestedListQuerier is one arm's list_list_int handle: a LIST<LIST<INT64>>
// column, which is the shape that makes the emitted decoder nest one element
// loop inside another.
//
// A column and not a property, because on neo4j there is no other route. The
// server refuses a nested list as a stored property value outright — "Collections
// containing collections can not be stored in properties" — so a seeded
// nested-list property is unavailable on this arm at any depth, and the fixture's
// list-of-list literal is evaluated by the server and packed into a real Bolt
// LIST of LISTs like any other value (bd gqlc-nrao, gqlc-v0gk).
type nestedListQuerier interface {
	nestedList(ctx context.Context) ([][]int64, error)
}

// nullListElemQuerier is one arm's certified_list_element handle for the
// column whose ELEMENTS the schema declares nullable: LIST<INT64> built from a
// nullable property, which emits [][]*int64.
//
// This is the one row where a real server sends a NULL inside a list. Every
// other list this battery reads is declared NOT NULL at the element position,
// so before bd gqlc-dxhwp the emitted decoder asserted elem.(int64) on a nil
// interface — an assertion that is false for every nil, whatever the declared
// width — and the whole read failed on the first absent element. A signature
// the store could not fill is what the goldens pinned; this is what says the
// one it can fill is the one that ships.
type nullListElemQuerier interface {
	nullablePair(ctx context.Context) ([][]*int64, error)
}

// nullOuterElemQuerier is one arm's nested_list_element_projection handle:
// three nested-list columns that differ in exactly one nullability qualifier
// each, so the two stars a nested list can take are read independently.
//
// The OUTER star is what nothing else here witnesses. list_list_int is nested
// with elements NOT NULL and certified_list_element is nullable but flat, so
// before this row no live server had ever been asked to answer [null, null]
// into a []*[]*string — the shape that reaches the nil arm and the address-of
// in neo4j's walkListElemBody ColumnList case, and prepare.go's
// `Nullable: elemNullable` on the same arm (bd gqlc-mxvp).
//
// labelss is the control on the other side: its outer element is NOT NULL, so
// an emitter that starred every nested element unconditionally would satisfy
// the two rows above and fail to compile here. That half is held by the
// checked-in goldens rather than by this interface; what the method adds is
// that the unstarred outer element still decodes a real server's answer.
type nullOuterElemQuerier interface {
	tagsPair(ctx context.Context) ([][]*[]*string, error)
	ranksPair(ctx context.Context) ([][]*[]int16, error)
	labelsPair(ctx context.Context) ([][][]*string, error)
}

// deepNestedListQuerier is one arm's list_list_list_int handle: a
// LIST<LIST<LIST<INT64>>> column, one level deeper than nestedListQuerier.
//
// The extra level is the whole point. At depth 2 the emitter's inner element
// loop shadows the outer accumulator harmlessly; only at depth 3 does an
// unsuffixed local make the emission assign an accumulator to itself, so this
// is the shallowest column that has the decoder's local-naming rule as its
// subject (bd gqlc-415l).
type deepNestedListQuerier interface {
	deepNestedList(ctx context.Context) ([][][]int64, error)
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

// anyValueColumnQuerier is one arm's schema_any_property handle, narrowed to
// the two columns whose declared width is ANY VALUE: one NOT NULL, one not.
// A width of no declared shape is the one place where the emitted Go type
// cannot carry the schema's nullability — every other width arrives as T or
// *T, and `any` is already inhabited by nil — so the promise survives only as
// a gate the emitter writes, and only a live null can say whether it did.
type anyValueColumnQuerier interface {
	eventMarker(ctx context.Context) (any, error)
	eventPayload(ctx context.Context) (*any, error)
	errNoRows() error
}

// unionColumns is one Row of the union_property fixture as the battery reads
// it: the three declared-union properties, each a nullable `any`.
//
// The struct is the battery's own rather than either target's, because the
// two targets name it differently (RowColumnsRow off the column lane, Row off
// the vertex lane) and the claim is the same claim in both places.
type unionColumns struct {
	Pick *any
	Also *any
	Flag *any
}

// unionColumnQuerier is one arm's union_property handle: a closed union
// arriving off the wire, which nothing in this repository had ever EXECUTED
// (bd gqlc-2c0c).
//
// Every emitted decoder is unexported — `decodeUnion<suffix>` — and is called
// only after the driver has answered, so no compiled test in the tree can
// reach one. The direction was pinned by golden text alone, and PR #2851
// measured what that is worth: its mutation row M4 deleted the emitted
// decoders' empty-value guard and SURVIVED, because a golden that regenerates
// alongside the emitter agrees with whatever the emitter now says.
//
// BOTH lanes are here because the emitted code has two callers for one helper
// and they hand it different bytes. rowColumns comes off the projected-column
// lane (record.Get on neo4j, rows.Scan on Apache AGE), rowWhole off the
// vertex-property lane in models.go. A guard written into one and not the
// other is invisible to a probe that only drives one of them.
type unionColumnQuerier interface {
	rowColumns(ctx context.Context) ([]unionColumns, error)
	rowWhole(ctx context.Context) (unionColumns, error)
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
// removePerson and getPersonName are the query methods promoted onto the
// generated *Tx from its embedded core, so what they see is the
// transaction's own view and not the graph's — the distinction
// txReadsOwnUncommitted exists to read.
type liveTx interface {
	commit(ctx context.Context) error
	rollback(ctx context.Context) error
	removePerson(ctx context.Context, id int64) error
	getPersonName(ctx context.Context, id int64) (string, error)
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
// parameters and back through projected columns and a whole vertex.
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
	errNoRows() error
}

// localDateTimeColumnQuerier is one arm's
// local_datetime_constructed_column handle. LOCALDATETIME has no
// property spelling, so a constructed column is the only way a batch
// reaches that carrier — and a constructed temporal is what Apache AGE
// refuses permanently, which is why this is a handle of its own rather
// than a method on temporalRoundtripQuerier.
type localDateTimeColumnQuerier interface {
	builtLocalDateTime(ctx context.Context) (localDateTimeValue, error)
}

// mapColumnQuerier is one arm's scalar_map handle: a column whose value is
// a whole map rather than a scalar, a list of scalars, or an entity.
//
// The emitted read is neo4j.GetRecordValue[map[string]any], and until this
// handle existed that line was held by the golden comparison and by the
// compiler and by nothing else — pinned as text, and as something that
// builds. What neither can see is the value the server actually sends: the
// read is a type ASSERTION on an `any` the driver hydrated, so an arrival
// that is not a map[string]any fails it at run time with the golden
// unchanged and the package still compiling (bd gqlc-y6mo).
//
// The member's own Go type is the second half and is the reason the
// scenario asserts a value rather than a length. `any` is inhabited by
// every carriage the driver could have chosen, so nothing in the emitted
// method distinguishes an int64 member from a float64 one — a caller type-
// switching on the member is what has to be right, and the column's Go type
// tells it nothing at all.
type mapColumnQuerier interface {
	oneMap(ctx context.Context) (map[string]any, error)
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

// savepointHarness is an arm whose driver serves a nested Begin with a
// savepoint instead of refusing it. Only such an arm can be left holding
// one, so only such an arm is asked to prove it is not.
type savepointHarness interface {
	harness
	savepointScenario(ctx context.Context, t *testing.T) savepointBackend
}

// localDateTimeColumnHarness is an arm whose target admits a constructed
// temporal column. Apache AGE refuses every temporal constructor
// openCypher spells, and every one is re-measured on the pinned image on
// each live run: the six bare names by
// TestAGERefusesTheFunctionsItDoesNotDefine, duration.between by
// TestAGERefusesTheNamespaceItHasNoSchemaFor. That is closure over the
// names a query can spell, which is all this column needs, and not over
// AGE's function catalogue, which nothing here counts. The backend
// refuses such a column at generation, so this column stays false for
// that arm permanently rather than until some later width lands.
type localDateTimeColumnHarness interface {
	harness
	localDateTimeColumnScenario(ctx context.Context, t *testing.T) localDateTimeColumnBackend
}

// mapColumnHarness is an arm whose target emits a map-valued column at all.
// Apache AGE does not: it refuses the kind at generation time —
//
//	unsupported query: the Apache AGE backend serves scalar and entity
//	columns, so 1 query would be dropped: OneMap (column "m" projects
//	scalar(map))
//
// — which internal/codegen/age/age_test.go pins at four sites, so that arm
// has no OneMap to bind and this stays false for it. Lifting the refusal is
// an ADR 0025 question about what agtype can carry, not a corpus enrolment
// (bd gqlc-faa1).
type mapColumnHarness interface {
	harness
	mapColumnScenario(ctx context.Context, t *testing.T) mapColumnBackend
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
	nestedList() nestedListQuerier
	nullListElem() nullListElemQuerier
	nullOuterElem() nullOuterElemQuerier
	deepNestedList() deepNestedListQuerier
	entityNodeProjectedOne() entityNodeQuerier
	entityEdgeProjectedOne() entityEdgeQuerier
	anyValueColumns() anyValueColumnQuerier
	unionColumns() unionColumnQuerier
}

// writeBackend is a scenario's view of a writeHarness.
type writeBackend interface {
	backend
	mixedReadWriteBatch() mixedReadWriteBatchQuerier
	timestampRoundtrip() timestampRoundtripQuerier
	tx() txQuerier

	// refuseBeginOnBoundHandle binds a generated handle to a driver
	// transaction through WithTx and calls the generated Begin on it,
	// returning Begin's own error verbatim, nil included. A nil is the
	// refusal not firing, which is the failure the scenario looks for, so
	// it must not be dressed up as an error.
	//
	// It lives on the backend and not on liveTx because with the querier
	// embedded there is no longer any transaction-bound handle reachable
	// from a *Tx — the arm has to reach the driver itself, and the
	// backend is what owns the pool or driver.
	refuseBeginOnBoundHandle(ctx context.Context, t *testing.T) error
}

// edgeUnionBackend is a scenario's view of an edgeUnionHarness.
type edgeUnionBackend interface {
	backend
	edgeUnionUndeclared() edgeUnionQuerier
}

// temporalBackend is a scenario's view of a temporalHarness and
// zonedTimeBackend of a zonedTimeHarness.
//
// The three extra methods are where the arms are allowed to differ, and
// each one is a claim this battery makes about its arm rather than a
// question it asks the generated code.
//
// seedSeenOn writes the nullable date without going through the surface
// under test, the way backend.seed does for every other scenario. It
// takes components instead of cypher because a date literal is outside
// the dialect intersection backend.seed's contract rests on: neo4j
// spells one date('YYYY-MM-DD') and Apache AGE, having no such function,
// stores the same value as the ISO string its encoding rides on.
//
// storedLocalTime and storedDuration answer what the arm's target keeps
// of a written value. They are the arm's declaration of its own
// resolution and normalisation — neo4j the identity, AGE the
// microsecond truncation and the collapse of days into seconds that ADR
// 0033's encodings settle — so a target that silently coarsened beyond
// what its arm declares fails here. They read nothing from the emitted
// code, so they cannot agree with a defect in it.
type temporalBackend interface {
	backend
	temporalRoundtrip() temporalRoundtripQuerier
	seedSeenOn(ctx context.Context, t *testing.T, id int64, on dateValue)
	storedLocalTime(v localTimeValue) localTimeValue
	storedDuration(v durationValue) durationValue
}

type zonedTimeBackend interface {
	backend
	zonedTimeRoundtrip() zonedTimeRoundtripQuerier
}

// savepointBackend is a scenario's view of a savepointHarness: the two
// nested Begins its driver can be given, each reporting whether the
// transaction it ran in was left holding a savepoint afterwards.
//
// Each call opens a driver transaction of its own and neither shares one
// with the other, because on at least one arm the probe that finds no
// savepoint is itself an error and aborts the transaction it asked in.
type savepointBackend interface {
	// refuseNestedBegin calls the GENERATED Begin on a handle bound to a
	// driver transaction and returns the probe's answer alongside that
	// Begin's error verbatim, nil included.
	refuseNestedBegin(ctx context.Context, t *testing.T) (savepoint bool, refusal error)

	// serveNestedBegin opens a savepoint through the DRIVER's own nested
	// Begin and returns the same probe's answer. The probe names a
	// savepoint the driver chooses the name of, so this is what holds the
	// name: without it a driver that renamed its savepoints would make
	// refuseNestedBegin report "none" for a transaction holding one.
	serveNestedBegin(ctx context.Context, t *testing.T) (savepoint bool)
}

// localDateTimeColumnBackend is a scenario's view of a
// localDateTimeColumnHarness.
type localDateTimeColumnBackend interface {
	backend
	localDateTimeColumn() localDateTimeColumnQuerier
}

// mapColumnBackend is a scenario's view of a mapColumnHarness.
type mapColumnBackend interface {
	backend
	mapColumn() mapColumnQuerier
}

// arms are the backends the battery runs against. Each adapter owns its
// container, its connection, and its isolation strategy.
//
// writes records that the arm's target emits the write fixture, edgeUnions
// that it emits an edge-union dispatch, temporals that it admits the zoneless
// temporal widths, zonedTime that it admits TIME WITH TIME ZONE, and
// savepoints that its driver serves a nested Begin with one,
// constructedTemporal that it admits a column the server builds by calling a
// temporal constructor, and mapColumns that it admits a column whose value is
// a whole map.
// TestLiveSmoke holds each harness to its column, so an arm that stops
// satisfying writeHarness, edgeUnionHarness, temporalHarness,
// zonedTimeHarness, savepointHarness, localDateTimeColumnHarness or
// mapColumnHarness fails the
// battery rather than dropping those scenarios unremarked — and an arm that
// starts satisfying one fails it too, which is what would happen if Apache AGE
// gained the alternation and the backend's refusal were lifted, or if it
// gained a temporal constructor.
//
// Apache AGE holds temporals without constructedTemporal, and that pair is
// permanent rather than a stage. gqlc-mv3r gave the three zoneless widths an
// agtype encoding the backend owns, which needs nothing of the server but
// string and integer comparison; a constructed column needs a function AGE
// does not define, and no encoding gqlc chooses can supply one.
var arms = []struct {
	name                string
	start               func(ctx context.Context, t *testing.T) harness
	writes              bool
	edgeUnions          bool
	temporals           bool
	zonedTime           bool
	savepoints          bool
	constructedTemporal bool
	mapColumns          bool
}{
	{name: "neo4j-go-v5", start: startNeo4jV5, writes: true, edgeUnions: true, temporals: true, zonedTime: true, constructedTemporal: true, mapColumns: true},
	{name: "neo4j-go-v6", start: startNeo4jV6, writes: true, edgeUnions: true, temporals: true, zonedTime: true, constructedTemporal: true, mapColumns: true},
	{name: "apache-age-pgx-v5", start: startAGE, writes: true, temporals: true, savepoints: true},
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
	{name: "list_list_int: nested list off the wire", run: nestedListDecode},
	{name: "certified_list_element: a NULL inside a list", run: nullListElemDecode},
	{name: "nested_list_element_projection: a NULL as a nested list's OUTER element", run: nullOuterElemDecode},
	{name: "list_list_list_int: thrice-nested list off the wire", run: deepNestedListDecode},
	{name: "entity_node_projected_one: whole vertex", run: nodeEntityRead},
	{name: "entity_edge_projected_one: whole edge", run: edgeEntityRead},
	{name: "schema_any_property: ANY VALUE columns agree on null", run: anyValueColumnsAgreeOnNull},
	{name: "union_property: a closed union dispatches on the wire family it arrived as", run: unionColumnDecode},
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
	{name: "tx: Begin on a transaction-bound handle is refused", run: txBeginIsRefusedOnATransactionBoundHandle},
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

// savepointScenarios are the battery an arm runs once its driver serves a
// nested Begin with a savepoint, written against savepointBackend under the
// same rules.
var savepointScenarios = []struct {
	name string
	run  func(ctx context.Context, t *testing.T, b savepointBackend)
}{
	{name: "tx: the refused nested Begin opens no savepoint", run: txRefusedNestedBeginOpensNoSavepoint},
}

var localDateTimeColumnScenarios = []struct {
	name string
	run  func(ctx context.Context, t *testing.T, b localDateTimeColumnBackend)
}{
	{name: "local_datetime_constructed_column: components arrive whole", run: constructedLocalDateTime},
}

var mapColumnScenarios = []struct {
	name string
	run  func(ctx context.Context, t *testing.T, b mapColumnBackend)
}{
	{name: "scalar_map: a map column arrives as a map, member types and all", run: mapColumnArrives},
}

// edgeUnionScenarios are the battery an arm runs once its target emits an
// edge-union dispatch, written against edgeUnionBackend under the same rules.
var edgeUnionScenarios = []struct {
	name string
	run  func(ctx context.Context, t *testing.T, b edgeUnionBackend)
}{
	{name: "edge_union_undeclared_relationship_type: label dispatch", run: edgeUnionDispatch},
}

// scenarioTables declares how large each battery is. The sizes are written
// down here rather than measured off the tables, because a number taken from
// the table cannot notice the table shrinking — it shrinks with it.
//
// A count is a size and not a membership: renaming a scenario, or swapping
// one for another, leaves the count where it was. What it refuses is a
// battery that lost a row or gained one without anybody saying so, which is
// the failure the batteries themselves cannot report. A scenario that is gone
// runs zero times while every capability assertion around it still passes, so
// the arm boots its container and reports ok having witnessed one thing less
// than it did yesterday.
//
// why is the sentence a red row should leave behind: it says what stops being
// witnessed, which is the part a count alone cannot tell you.
var scenarioTables = []struct {
	name string
	got  int
	want int
	why  string
}{
	{
		name: "readScenarios", got: len(readScenarios), want: 10,
		why: "the battery every arm runs; a lost row is a read contract no target is checked against",
	},
	{
		name: "writeScenarios", got: len(writeScenarios), want: 8,
		why: "the only live witness for the :exec path and for the transaction contract, most of which a method set cannot express",
	},
	{
		name: "temporalScenarios", got: len(temporalScenarios), want: 1,
		why: "the only live witness for the ADR 0033 zoneless temporal round trip",
	},
	{
		name: "zonedTimeScenarios", got: len(zonedTimeScenarios), want: 1,
		why: "the only live witness for TIME WITH TIME ZONE keeping its offset",
	},
	{
		name: "savepointScenarios", got: len(savepointScenarios), want: 1,
		why: "the only live witness that a refused nested Begin leaves no savepoint behind; until bd gqlc-8jfj this table carried no emptiness guard at all, so losing all of it was silent",
	},
	{
		name: "localDateTimeColumnScenarios", got: len(localDateTimeColumnScenarios), want: 1,
		why: "the only live witness for a LOCALDATETIME the server constructs; every other zoneless width is written by the client first, so without this row EVERY zoneless width is asserted against a value our own encode produced, and an encode and a decode wrong in agreement would pass all of them",
	},
	{
		name: "mapColumnScenarios", got: len(mapColumnScenarios), want: 1,
		why: "the only live witness that a map column's emitted GetRecordValue[map[string]any] is a read a server's answer actually satisfies; the golden holds the line as text and the compiler holds it as a build, and neither can see what arrives",
	},
	{
		name: "edgeUnionScenarios", got: len(edgeUnionScenarios), want: 1,
		why: "the only live witness for the emitted edge-union label dispatch",
	},
}

// TestEveryBatteryIsTheDeclaredSize holds each battery to its declared
// size.
//
// It deliberately does NOT honour GQLC_SKIP_LIVE. Every other test here skips
// without a container, which is how this module is usually run, so a battery
// could be emptied and nothing in the module would say a word. This guard
// needs no container, so it runs whenever the package is tested at all.
func TestEveryBatteryIsTheDeclaredSize(t *testing.T) {
	for _, tc := range scenarioTables {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.got,
				"%s holds %d scenarios and this file declares %d. If the change was meant, move the number in the same commit and say so in the message; %s",
				tc.name, tc.got, tc.want, tc.why)
		})
	}
}

// TestEveryBatteryIsNamedInScenarioTables holds scenarioTables to the
// batteries that actually exist in this file.
//
// scenarioTables is itself a written-down list, so it has the failure it was
// added to catch: a battery it does not name is a battery nothing counts, and
// nothing about adding one would say so. That is not hypothetical — a new
// battery arrives with each capability the targets grow.
//
// The names are read off the parse rather than off a registry the tables
// would have to join, because a registry is something an author can forget in
// exactly the same way. Nothing has to be remembered here: declaring
// `var somethingScenarios = ...` is what enrols it, and this test is what
// tells you so.
//
// Where it stops: it reads THIS file only, which is where every battery lives
// today and where the loop that runs them lives. A battery declared in a
// sibling file of this package would not be seen, and would run unheld.
func TestEveryBatteryIsNamedInScenarioTables(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "live_test.go", nil, 0)
	require.NoError(t, err, "the battery has to be able to read its own source to know what to hold")

	var found []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				if strings.HasSuffix(name.Name, "Scenarios") {
					found = append(found, name.Name)
				}
			}
		}
	}
	require.NotEmpty(t, found,
		"no battery was found in the parse at all, so this test would hold scenarioTables to nothing")

	var declared []string
	for _, tc := range scenarioTables {
		declared = append(declared, tc.name)
	}
	require.ElementsMatch(t, found, declared,
		"scenarioTables must name every battery in this file and no others. A battery declared here and missing from scenarioTables runs unheld, which is the gap bd gqlc-8jfj closed one level down.")
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
			// The batteries are held to a size by scenarioTables, not to
			// non-emptiness here. The per-table NotEmpty guards that used to sit
			// in this loop said only that a battery had not been emptied
			// ENTIRELY, so deleting one row of eight read green (bd gqlc-8jfj);
			// and they sat inside this test, which skips without a container, so
			// in the way this module is usually run they said nothing at all.
			// Emptying a table is still a red under the counts, which is the
			// claim bd gqlc-wu5y asked for.
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

			lh, servesConstructedTemporal := h.(localDateTimeColumnHarness)
			require.Equal(t, arm.constructedTemporal, servesConstructedTemporal,
				"the arm's constructed-temporal capability must match the arms table; a target that gained or lost a temporal constructor updates both")
			if servesConstructedTemporal {
				for _, sc := range localDateTimeColumnScenarios {
					t.Run(sc.name, func(t *testing.T) {
						if parallelScenarios {
							t.Parallel()
						}
						sc.run(ctx, t, lh.localDateTimeColumnScenario(ctx, t))
					})
				}
			}

			mch, servesMapColumns := h.(mapColumnHarness)
			require.Equal(t, arm.mapColumns, servesMapColumns,
				"the arm's map-column capability must match the arms table; a target that gained or lost a map-valued column updates both")
			if servesMapColumns {
				for _, sc := range mapColumnScenarios {
					t.Run(sc.name, func(t *testing.T) {
						if parallelScenarios {
							t.Parallel()
						}
						sc.run(ctx, t, mch.mapColumnScenario(ctx, t))
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

			sph, servesSavepoints := h.(savepointHarness)
			require.Equal(t, arm.savepoints, servesSavepoints,
				"the arm's savepoint capability must match the arms table; a driver that gained or lost the savepoint-backed nested Begin updates both")
			if servesSavepoints {
				for _, sc := range savepointScenarios {
					t.Run(sc.name, func(t *testing.T) {
						if parallelScenarios {
							t.Parallel()
						}
						sc.run(ctx, t, sph.savepointScenario(ctx, t))
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

// nestedListDecode drives the nested-list contract: a LIST<LIST<INT64>> the
// server sends is decoded by the loop the emitter nests inside another loop.
//
// What this adds to the coverage the shape already has, since it is not
// obvious: the emitted decoder was covered on golden text, on compilation
// against the real driver, and by TestEmittedDecodersRunOnDriverValues, which
// runs it over hand-built values like []any{[]any{int64(7)}, []any{}}. That
// battery drives a stubbed driver, so those values are what we BELIEVE a Bolt
// server hands back for a nested list. Nothing until this row had asked a
// server. A driver that delivered the inner lists as anything but []any, or
// their elements as anything but int64, would satisfy every one of those
// checks and fail here — the emitted assertions are elem.([]any) and
// elem1.(int64) (bd gqlc-nrao).
//
// The inner lists are deliberately ragged, one element then two, so a decoder
// that carried one accumulator across the outer iterations rather than making
// a fresh one per element returns three values in the first row.
//
// The two backends reach this through different emissions: neo4j nests the
// element loops, and Apache AGE calls a single agtypeListOfListOfInt64 helper
// over the raw agtype bytes. One row witnesses both.
//
// No seed, and the graph it runs against is irrelevant: the fixture's query
// is a bare RETURN of a list-of-list literal with no MATCH, so the server
// evaluates and packs it whatever the graph holds. That is not a weakening —
// see nestedListQuerier for why a stored nested-list property is not
// available on neo4j to seed instead.
func nestedListDecode(ctx context.Context, t *testing.T, b backend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	got, err := b.nestedList().nestedList(ctx)
	require.NoError(t, err)
	require.Equal(t, [][]int64{{1}, {2, 3}}, got,
		"a nested list off the wire must decode element for element, with each inner list its own length")
}

// nullListElemDecode is the live witness for bd gqlc-dxhwp: a list arriving
// off a real server with a NULL inside it decodes to a nil pointer at that
// index rather than failing the whole read.
//
// The seed writes two people and OMITS score on one of them, which is how a
// property becomes absent on both servers — neither has a way to store a
// typed null in a property slot, and a SET to null deletes the property. So
// `[p.score, p.score]` evaluates to a two-element list of nulls for that
// person and to a list of the stored value for the other.
//
// BOTH ROWS ARE THE ASSERTION, not one with the other for company. A decoder
// that dropped null elements, or that returned an empty slice on meeting one,
// would still satisfy a null-only row; and a decoder that had not changed at
// all still satisfies the non-null row, since that is the shape it has always
// read. Only the pair pins that the same list carries both.
//
// Unordered, because the fixture's query has no ORDER BY and MATCH promises
// no order. The claim is about what the two rows contain, and imposing an
// order here would be asserting something the query does not offer.
func nullListElemDecode(ctx context.Context, t *testing.T, b backend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	b.seed(ctx, t, `
		CREATE (:Person {id: 1, age: 30, score: 7, rank: 1})
		CREATE (:Person {id: 2, age: 40, rank: 2})
	`)

	got, err := b.nullListElem().nullablePair(ctx)
	require.NoError(t, err,
		"a NULL element must not fail the read; before gqlc-dxhwp the emitted decoder "+
			"asserted elem.(int64) on a nil interface and returned an error here")

	seven := int64(7)
	require.ElementsMatch(t, [][]*int64{{&seven, &seven}, {nil, nil}}, got,
		"an absent element is a nil pointer at its own index, and a present one is unaffected")
}

// nullOuterElemDecode is the live witness for bd gqlc-mxvp: a nested list
// whose OUTER element arrives NULL decodes to a nil pointer at that index,
// rather than being type-asserted to []any and failing the whole read.
//
// WHY THE OUTER ELEMENT AND NOT THE INNER ONE. The two neighbouring rows above
// each hold one half of this shape and neither holds both — list_list_int is
// nested with its elements NOT NULL, certified_list_element is nullable but
// flat — so the combination reached no live server until this row. It is the
// combination that matters: `e.Nullable` on neo4j's walkListElemBody
// ColumnList arm is what emits the nil arm and the address-of, and every other
// nested-list golden in the corpus has it false, so all three sites could be
// deleted with the whole battery green (measured on bd gqlc-dxhwp). Two of the
// three are held by the goldens' ability to COMPILE, since an unstarred
// innerAcc cannot be appended to a []*[]*string. The nil arm is the one a
// compiler cannot see: deleting it emits code that builds and then meets a
// nil at run time, which is what this row is here to be red for.
//
// THE SEED IS THE ASSERTION'S OTHER HALF. grid 2 OMITS tags and ranks, which
// is how a property becomes absent on both servers, so `[g.tags, g.tags]`
// evaluates to a two-element list of nulls for it and to the stored list for
// grid 1. A fixture whose tags were all present would pass this row with the
// nil arm deleted, which is exactly the vacuous pass the bead names.
//
// labelss is declared NOT NULL at the outer position, so it is seeded on both
// grids and both its rows are pointer-free. It is the control that says the
// two starred columns are starred because of their qualifier and not because
// the emitter stars every nested element: without it, an emitter that did
// would satisfy tagss and rankss and be witnessed nowhere.
//
// Unordered, for the reason nullListElemDecode gives: MATCH promises no order
// and the query has no ORDER BY, so what is asserted is what the two rows
// contain.
func nullOuterElemDecode(ctx context.Context, t *testing.T, b backend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	b.seed(ctx, t, `
		CREATE (:Grid {id: 1, tags: ['a', 'b'], ranks: [1, 2], labels: ['x', 'y']})
		CREATE (:Grid {id: 2, labels: ['z']})
	`)

	q := b.nullOuterElem()

	t.Run("a nullable outer element arrives nil", func(t *testing.T) {
		got, err := q.tagsPair(ctx)
		require.NoError(t, err,
			"a NULL at the OUTER position of a nested list must not fail the read; without "+
				"walkListElemBody's nil arm the emitted decoder runs elem.([]any) on a nil any")

		a, bb := "a", "b"
		present := []*string{&a, &bb}
		require.ElementsMatch(t, [][]*[]*string{{&present, &present}, {nil, nil}}, got,
			"an absent outer element is a nil *[]*string at its own index, and a present one "+
				"still carries every inner element")
	})

	t.Run("a nullable outer element over a NOT NULL inner width arrives nil", func(t *testing.T) {
		got, err := q.ranksPair(ctx)
		require.NoError(t, err, "the outer star is decided by the outer qualifier alone")

		present := []int16{1, 2}
		require.ElementsMatch(t, [][]*[]int16{{&present, &present}, {nil, nil}}, got,
			"rankss differs from tagss in the INNER qualifier only, so a decoder that "+
				"reached its nil arm by way of the inner one would part company here")
	})

	t.Run("a NOT NULL outer element carries no pointer", func(t *testing.T) {
		got, err := q.labelsPair(ctx)
		require.NoError(t, err, "the control column is seeded on both grids and has no null to meet")

		x, y, z := "x", "y", "z"
		require.ElementsMatch(t, [][][]*string{{{&x, &y}, {&x, &y}}, {{&z}, {&z}}}, got,
			"labelss is NOT NULL at the outer position, so the emitter must NOT star it; "+
				"one that starred every nested element unconditionally would pass the two rows above")
	})
}

// deepNestedListDecode drives the same contract one level deeper, at the depth
// where the emitter's local-naming rule starts to bite.
//
// What the extra level is for: the decoder suffixes its per-level locals by
// nesting depth (elemLocal in internal/codegen/neo4j). At depth 2 dropping that
// suffix shadows harmlessly and every existing row stays green; at depth 3 it
// emits innerAcc = append(innerAcc, innerAcc), which does not compile. So the
// witness for the naming rule itself is the checked-in golden for this fixture,
// not this row — remove the suffixing and the goldens stop building.
//
// What this row adds on top of that is the half a compile cannot answer: that
// the three-level decoder, once it does compile, reads a real server's value
// correctly rather than merely plausibly. Nothing else asks a server for a
// list nested this deep.
//
// The literal is deliberately irregular — the first outer element holds one
// inner list, the second holds two of different lengths — so an accumulator
// reused across iterations at either level shows up as a wrong shape rather
// than a wrong count. No seed, for the reason nestedListDecode gives.
func deepNestedListDecode(ctx context.Context, t *testing.T, b backend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	got, err := b.deepNestedList().deepNestedList(ctx)
	require.NoError(t, err)
	require.Equal(t, [][][]int64{{{1}}, {{2, 3}, {4}}}, got,
		"a thrice-nested list off the wire must decode level for level, with each list its own length")
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

// anyValueColumnsAgreeOnNull drives the ANY VALUE column contract on the null
// path, where the two backends were measured to disagree (bd gqlc-tez0).
//
// The disagreement was one-sided and its shape is worth stating, because it is
// what this scenario is pointed at. Every other column lane on neo4j — scalar,
// list, entity, edge-union — fails the row when a column the schema declared
// NOT NULL arrives null, and Apache AGE does it for every column kind it
// serves. The record.Get lane that ANY VALUE rides emitted no such gate, so a
// null came back as a nil `any` beside a nil error: a caller reading a field
// its schema promised was present got Go's zero for "absent" with nothing
// distinguishing it from a value the graph actually holds.
//
// A declaration test cannot see this and never could. Both arms declare
// `EventMarker(ctx) (any, error)`, identically, whichever way they behave —
// TestBackendInvariantSurface compares declarations and is green across the
// divergence. Only a live null separates them.
//
// The nullable half is here as the other side of the same claim rather than
// because it was ever suspected: an ANY VALUE column that MAY be null must
// still come back as a nil pointer and not an error, so a gate added to the
// non-nullable arm cannot pay for itself by refusing the nullable one too.
func anyValueColumnsAgreeOnNull(ctx context.Context, t *testing.T, b backend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.anyValueColumns()

	// A vertex carrying neither ANY VALUE property. Both columns project, and
	// both project null: absence and an explicit null are the same vertex on
	// both backends, which nodeEntityRead measures directly.
	b.seed(ctx, t, "CREATE (:Event {id: 1})")

	_, err := q.eventMarker(ctx)
	require.Error(t, err,
		"a column declared ANY VALUE NOT NULL that arrives null must fail the row; `any` is inhabited by nil, so a caller cannot tell the schema's promise was broken from a value the graph holds")
	// The reason and not merely the verdict: the row is present and it is the
	// value that is null, so an arm that errored because it found nothing at
	// all has not witnessed this claim. Both backends spell the refusal the
	// same way, and that identical spelling IS the agreement being asserted.
	require.NotErrorIs(t, err, q.errNoRows(),
		"the vertex was seeded, so a no-rows error means the scenario stopped testing what it names")
	require.ErrorContains(t, err, "non-nullable but arrived null")
	require.ErrorContains(t, err, "marker")

	payload, err := q.eventPayload(ctx)
	require.NoError(t, err,
		"a nullable ANY VALUE column arriving null is the schema's own case and must not be an error")
	require.Nil(t, payload,
		"a nullable ANY VALUE column arriving null must be a nil pointer, not a pointer to a nil")

	// Both columns carrying values. Without this the scenario would pass on an
	// arm that refused every ANY VALUE column outright, which is the shape a
	// gate written one line too wide would produce.
	b.seed(ctx, t, "MATCH (e:Event {id: 1}) SET e.marker = 'here', e.payload = 'also here'")

	marker, err := q.eventMarker(ctx)
	require.NoError(t, err)
	require.Equal(t, "here", marker,
		"a value the graph holds must reach the caller through the same lane the gate guards")

	payload, err = q.eventPayload(ctx)
	require.NoError(t, err)
	require.NotNil(t, payload)
	require.Equal(t, "also here", *payload)
}

// unionColumnDecode drives the closed-union DECODE direction, which until
// this scenario nothing in the repository executed (bd gqlc-2c0c).
//
// The gap was not a missing assertion, it was a missing CALLER. `decodeUnion…`
// is unexported in every emitted package and runs only once a driver has
// answered, so the conformance corpus could only ever compare its text to a
// golden — and the golden is regenerated from the same emitter, so the two
// agree by construction. PR #2851 put a number on that: its mutation row M4
// deleted the emitted decoders' empty-value guard and the whole tree stayed
// green.
//
// WHAT EACH STEP IS FOR, since a union decoder can be wrong in four
// independent ways and one arrival witnesses none of the others:
//
//   - the narrowing. An INT32 member arrives on both backends inside a
//     64-bit carrier — neo4j hands the caller an int64, AGE a decimal text —
//     so int32 is a width nothing but the generated decoder can produce.
//     `require.IsType` on that is the load-bearing line in this scenario: a
//     decoder that never ran leaves the driver's own int64 in the `any`.
//   - the DISPATCH, which is the thing a single arrival cannot show. The same
//     emitted helper is handed a second wire family and must answer with the
//     other member. One helper, two arrivals, two widths.
//   - the two refusals, which are different defects and are asserted apart.
//     A value inside a member's family but outside its declared width is a
//     NARROWING failure; a value in no member's family at all is a
//     MEMBERSHIP failure. An assertion that merely demanded "an error"
//     could not tell an emitter that had collapsed one into the other.
//   - the null, which is the negative control. No member of a closed union
//     carries nil, so the decoder REFUSES nil by construction; a nullable
//     union column arriving null therefore proves the emitted nil-guard kept
//     it away from the decoder. Without this row a decoder wired to run
//     unconditionally would look identical on every row above.
//
// Both callers of the helper are driven, in BOTH directions. rowColumns is the
// projected-column lane and rowWhole the vertex-property lane in models.go;
// they hand the same helper different bytes, and a guard present in one is not
// a guard in the other. Until bd gqlc-6jyp only the arrival direction went
// through the vertex lane and both refusals went through the column lane alone,
// which is this scenario's own premise applied one level short: an emitter that
// dropped the vertex lane's error check — the `if err != nil` beside each
// `decode Row.<F>: property %q` wrap — would have hidden every refusal behind a
// zero value and left the whole battery green.
//
// The wordings asserted are the INTERSECTION of what the two backends spell,
// which is why they are substrings rather than whole messages: neo4j reports a
// Go carrier it dispatched on and AGE the agtype text it parsed. The shared
// spine — the union's own name, the column's name, and the narrowing
// sentence — is identical on both, and that is what is held here. The vertex
// lane narrows that intersection further: neo4j names the property a second
// time (`decode Row.Pick: property "pick":`) where AGE wraps as `decode
// Row.Pick:` alone, so `decode Row.Pick` is the whole of what both spell.
func unionColumnDecode(ctx context.Context, t *testing.T, b backend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	q := b.unionColumns()

	// One Row carrying one member of each declared union: an INT32 for pick,
	// a STRING for also, a BOOL for flag.
	b.seed(ctx, t, "CREATE (:Row {id: 1, pick: 7, also: 'seven', flag: true})")

	rows, err := q.rowColumns(ctx)
	require.NoError(t, err, "a union column carrying a declared member must decode, not refuse")
	require.Len(t, rows, 1,
		"the scenario seeded exactly one Row; a different count means it is reading a graph another scenario wrote")

	requireUnionMember(t, rows[0].Pick, int32(7), "pick")
	requireUnionMember(t, rows[0].Also, "seven", "also")
	requireUnionMember(t, rows[0].Flag, true, "flag")

	// The other caller of the same helpers: the vertex-property lane.
	whole, err := q.rowWhole(ctx)
	require.NoError(t, err, "the same widths must decode off the vertex-property lane")
	requireUnionMember(t, whole.Pick, int32(7), "Row.Pick")
	requireUnionMember(t, whole.Also, "seven", "Row.Also")
	requireUnionMember(t, whole.Flag, true, "Row.Flag")

	// The dispatch. pick and flag arrive as the OTHER member of their own
	// union, through the helper that just answered with the first.
	b.seed(ctx, t, "MATCH (r:Row {id: 1}) SET r.pick = 'now a string', r.flag = 1.5")

	rows, err = q.rowColumns(ctx)
	require.NoError(t, err, "the second member of a union must decode through the same emitted helper as the first")
	require.Len(t, rows, 1)
	requireUnionMember(t, rows[0].Pick, "now a string", "pick")
	requireUnionMember(t, rows[0].Flag, 1.5, "flag")
	requireUnionMember(t, rows[0].Also, "seven", "also")

	whole, err = q.rowWhole(ctx)
	require.NoError(t, err)
	requireUnionMember(t, whole.Pick, "now a string", "Row.Pick")
	requireUnionMember(t, whole.Flag, 1.5, "Row.Flag")

	// The narrowing refusal: 2^32 is an integer the wire carries happily and
	// the declared INT32 member cannot hold. A decoder that skipped the
	// narrow would hand back a silently truncated 0 with no error at all.
	b.seed(ctx, t, "MATCH (r:Row {id: 1}) SET r.pick = 4294967296")

	_, err = q.rowColumns(ctx)
	require.Error(t, err,
		"a value inside a member's wire family but outside its declared width must fail the row; "+
			"the alternative is a silent truncation, which is the defect narrowing exists to prevent")
	require.ErrorContains(t, err, `decode column "pick"`,
		"the refusal must name the column it came from, or a caller cannot tell which of three union columns refused")
	require.ErrorContains(t, err, "UNION<INT32|STRING>",
		"and the union whose member set it was held against")
	require.ErrorContains(t, err, "does not fit the declared int32 width",
		"and it must be the NARROWING sentence: this value is a member of the set, so a membership refusal here would mean the decoder never reached the narrow")

	// The same refusal through the OTHER lane, on the same stored value. The
	// vertex lane reaches the helper through a different emitted line — a
	// per-property call site in models.go rather than a per-column one in
	// queries.cypher.go — and its error check is emitted separately, so the row
	// above says nothing about whether this one propagates at all.
	_, err = q.rowWhole(ctx)
	require.Error(t, err,
		"a value outside the declared width must fail the row off the vertex-property lane too; "+
			"the property call site swallowing the helper's error would hand back a nil Pick beside a nil error, "+
			"which is the shape a legitimately absent property has")
	// This is where the no-rows distinction can actually fire: rowWhole is a
	// :one, so an arm whose seed never landed answers ErrNoRows, and a bare
	// require.Error above would be satisfied by it while nothing under test ran.
	require.NotErrorIs(t, err, q.errNoRows(),
		"the vertex was seeded, so a no-rows error means this half stopped testing what it names")
	require.ErrorContains(t, err, "decode Row.Pick",
		"the refusal must name the FIELD it came from; also and flag hold decodable values here, so a refusal naming either is the wrong property having failed")
	require.ErrorContains(t, err, "UNION<INT32|STRING>",
		"and the union whose member set it was held against")
	require.ErrorContains(t, err, "does not fit the declared int32 width",
		"and the narrowing sentence, for the reason the column lane's row above gives")

	// The membership refusal, which is a different defect and is asserted
	// apart from the one above. 2.5 is in no member's family at all.
	b.seed(ctx, t, "MATCH (r:Row {id: 1}) SET r.pick = 2.5")

	_, err = q.rowColumns(ctx)
	require.Error(t, err,
		"a value outside the declared member set must fail the row; a closed union that accepted it would be ADR 0020's open union wearing a member list")
	require.ErrorContains(t, err, `decode column "pick"`)
	require.ErrorContains(t, err, "UNION<INT32|STRING>")
	require.NotContains(t, err.Error(), "does not fit the declared",
		"a value in no member's family must not be reported as a narrowing failure: the two refusals answer different questions, "+
			"and a test that cannot tell them apart witnesses neither")

	// And the membership refusal through the vertex lane. Both refusals are
	// driven through both lanes rather than one each: the two are separate
	// emitted paths through the helper on each lane, and a lane that collapsed
	// them into one another is the defect the column lane's pair already
	// refuses to accept.
	_, err = q.rowWhole(ctx)
	require.Error(t, err,
		"a value in no member's family must fail the row off the vertex-property lane too")
	require.ErrorContains(t, err, "decode Row.Pick")
	require.ErrorContains(t, err, "UNION<INT32|STRING>")
	require.NotContains(t, err.Error(), "does not fit the declared",
		"and it must still be the MEMBERSHIP refusal on this lane: a lane that reported it as a narrowing failure would be answering the other question")

	// The negative control. No member of a closed union carries nil, so the
	// decoder refuses nil by construction and a nil here is the emitted
	// nil-guard having kept the column away from it. Without this row a
	// decoder called unconditionally would look identical above.
	b.seed(ctx, t, "MATCH (r:Row {id: 1}) SET r.pick = null, r.also = null, r.flag = null")

	rows, err = q.rowColumns(ctx)
	require.NoError(t, err,
		"a nullable union column arriving null is the schema's own case; reaching the decoder with it would refuse the row, since no member carries nil")
	require.Len(t, rows, 1)
	require.Nil(t, rows[0].Pick, "a null union column must be a nil pointer, not a pointer to a decoded nil")
	require.Nil(t, rows[0].Also)
	require.Nil(t, rows[0].Flag)

	whole, err = q.rowWhole(ctx)
	require.NoError(t, err, "and the same on the vertex-property lane, where the guard is a different line of emitted code")
	require.Nil(t, whole.Pick)
	require.Nil(t, whole.Also)
	require.Nil(t, whole.Flag)
}

// requireUnionMember holds one decoded union column to the member the graph
// holds, at that member's DECLARED width.
//
// The width is the assertion. Both backends carry an INT32 member inside
// something wider — an int64 on neo4j's wire, decimal text on AGE's — so
// int32 is a shape only the generated decoder can produce, and a decoder that
// never ran leaves the carrier behind instead. require.Equal would catch that
// too, since int32(7) and int64(7) are not equal to testify; IsType is here
// so the failure says which of the two happened.
func requireUnionMember(t *testing.T, got *any, want any, column string) {
	t.Helper()
	require.NotNil(t, got,
		"column %q holds a value, so the emitted nil-guard must not have skipped its decode", column)
	require.IsType(t, want, *got,
		"column %q must arrive as its member's DECLARED width; the driver's own carrier reaching the caller unchanged is the decoder not having run", column)
	require.Equal(t, want, *got, "column %q", column)
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

// txBeginIsRefusedOnATransactionBoundHandle holds the nesting refusal.
// Neo4j cannot nest, and a surface that nests on the other backend is the
// portability failure the Tx object exists to remove, so AGE refuses too
// rather than opening a savepoint.
//
// The generated *Tx cannot reach this refusal at all: with the querier
// embedded, tx.Begin does not compile, and TestTxMethodSet is what
// witnesses that. What remains reachable — and so still needs a live
// witness — is the handle bound to a caller-owned transaction by WithTx,
// whose boundness is a dynamic-type fact the compiler cannot see.
func txBeginIsRefusedOnATransactionBoundHandle(ctx context.Context, t *testing.T, b writeBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	err := b.refuseBeginOnBoundHandle(ctx, t)
	require.Error(t, err, "Begin on a handle already bound to a transaction must be refused, not served")
	require.ErrorContains(t, err, txNestedRefusal,
		"the refusal must name itself; another failure of Begin would satisfy the line above while the refusal never fired")
}

// txRefusedNestedBeginOpensNoSavepoint holds the one thing about the
// refusal the row above cannot see: where it stands relative to the
// driver call it guards.
//
// Begin's refusal sits above the b.Begin that would open a savepoint.
// Below it, the function opens one, abandons it, and returns the same
// error to the same caller — so the row above stays green over a
// transaction left holding a savepoint nobody will ever release. Only a
// probe of the transaction itself can tell the two placements apart.
func txRefusedNestedBeginOpensNoSavepoint(ctx context.Context, t *testing.T, b savepointBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	savepoint, refusal := b.refuseNestedBegin(ctx, t)
	require.ErrorContains(t, refusal, txNestedRefusal,
		"Begin on a transaction-bound handle must be refused by name; this row is about what that refusal did on its way out")
	require.False(t, savepoint,
		"the refusal must precede the driver's Begin: placed after it, it hands back the same error over a transaction now holding an abandoned savepoint")

	// Without this the row above passes on a probe that can find nothing.
	require.True(t, b.serveNestedBegin(ctx, t),
		"control: the probe must find the savepoint the driver's own nested Begin opens, or its silence above witnesses nothing")
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
// Three more dates are here for the AGE arm, whose encoding is a
// zero-padded ISO string ordered by the server's string comparison
// (ADR 0033). That order is the calendar's only inside [0001, 9999] and
// only while every component is padded, so the two ends of the window
// are rows, and a pair of adjacent days across a year boundary is a
// third: 2023-12-31 and 2024-01-01 differ in every component, so a
// padding or component defect that survives the round trip still puts
// them the wrong way round under ORDER BY.
//
// The durations stay inside DAY TO SECOND — the schema's declared
// precision — and carry nanoseconds, because the bolt encoding sends
// seconds and nanoseconds apart and a conversion that flattened them
// would lose the sub-second half. They sit either side of zero because
// AGE stores a signed count of microseconds, where the sign is the one
// place its decode has to divide the way neo4j's components read: a
// duration backwards borrows from Seconds, and a division truncating
// toward zero answers with a negative Nanos no neo4j value takes.
//
// Sub-microsecond components are deliberate too. AGE's encodings hold
// microseconds and truncate below that silently, so reading 9ns back as
// 0 is the arm's declared resolution being witnessed rather than a value
// going missing — see temporalBackend.storedLocalTime.
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
	{
		id:      4,
		onDate:  dateValue{Year: 1, Month: 1, Day: 1},
		atLocal: localTimeValue{Hour: 12, Minute: 0, Second: 0, Nanosecond: 0},
		elapsed: durationValue{Seconds: -2, Nanos: 500000000},
	},
	{
		id:      5,
		onDate:  dateValue{Year: 9999, Month: 12, Day: 31},
		atLocal: localTimeValue{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999000},
		elapsed: durationValue{},
	},
	{
		id:      6,
		onDate:  dateValue{Year: 2023, Month: 12, Day: 31},
		atLocal: localTimeValue{Hour: 1, Minute: 2, Second: 3, Nanosecond: 4000},
		elapsed: durationValue{Seconds: -90},
	},
	{
		id:      7,
		onDate:  dateValue{Year: 2024, Month: 1, Day: 1},
		atLocal: localTimeValue{Hour: 0, Minute: 0, Second: 0, Nanosecond: 1000},
		elapsed: durationValue{Days: 1, Seconds: 1, Nanos: 1000},
	},
}

// temporalReadingsInDateOrder is temporalReadings by onDate, which is
// the order every ORDER BY in the scenario answers in and no arm's
// insertion order.
var temporalReadingsInDateOrder = []int64{4, 2, 6, 7, 1, 3, 5}

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
		wantLocal := b.storedLocalTime(r.atLocal)
		wantElapsed := b.storedDuration(r.elapsed)

		gotDate, err := q.readingDate(ctx, r.id)
		require.NoError(t, err, "read reading %d date", r.id)
		require.Equal(t, r.onDate, gotDate, "reading %d: the date must survive the encoding component for component", r.id)

		gotLocal, err := q.readingLocalTime(ctx, r.id)
		require.NoError(t, err, "read reading %d local time", r.id)
		require.Equal(t, wantLocal, gotLocal, "reading %d: the local time must survive to the resolution this arm declares", r.id)

		gotElapsed, err := q.readingElapsed(ctx, r.id)
		require.NoError(t, err, "read reading %d duration", r.id)
		require.Equal(t, wantElapsed, gotElapsed, "reading %d: the duration must survive as this arm declares it stores one", r.id)

		entity, err := q.oneReading(ctx, r.id)
		require.NoError(t, err, "read reading %d as a vertex", r.id)
		require.Equal(t, readingEntity{ID: r.id, OnDate: r.onDate, AtLocal: wantLocal, Elapsed: wantElapsed}, entity,
			"reading %d: a whole vertex must carry the same components its columns do, and an unwritten nullable date must be absent rather than a zero Date", r.id)
	}

	// The whole set, ordered by the author's ORDER BY. In date order, so
	// the pre-epoch reading leads and the id order it was written in does
	// not survive. The cutoff is the first date the encoding admits, so
	// this probe also holds that lower bound bindable and inclusive.
	all, err := q.readingsFrom(ctx, dateValue{Year: 1, Month: 1, Day: 1})
	require.NoError(t, err)
	require.Equal(t, temporalReadingsInDateOrder, all, "ORDER BY over the stored date must answer in date order")

	// The bound date is the cutoff and the comparison is inclusive, so the
	// reading written on exactly this date stays in. A conversion that
	// built its driver value anywhere but midnight would put the stored
	// value on either side of this boundary depending on the runner's
	// clock.
	from, err := q.readingsFrom(ctx, dateValue{Year: 2024, Month: 2, Day: 29})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3, 5}, from, "a range predicate over the stored date must be inclusive and in date order")

	// Adjacent days across a year boundary. Every component differs, so a
	// stored form that ordered on anything but the whole date — the day
	// alone, an unpadded year, the text a locale would render — keeps
	// 2023-12-31 on the wrong side of this cutoff or answers the two the
	// wrong way round.
	acrossYear, err := q.readingsFrom(ctx, dateValue{Year: 2024, Month: 1, Day: 1})
	require.NoError(t, err)
	require.Equal(t, []int64{7, 1, 3, 5}, acrossYear,
		"a cutoff on January 1st must exclude the December 31st before it")

	// A nullable date parameter. Nothing has written seenOn yet, so the
	// predicate has nothing to match either way; what it holds is that a
	// nil binds Cypher null, under which `>=` is null and no row is
	// returned. A nil that bound a zero Date would return every row.
	none, err := q.readingsSeenFrom(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, none, "a nil nullable date parameter must bind null, not a zero Date")

	b.seedSeenOn(ctx, t, 1, dateValue{Year: 2024, Month: 3, Day: 1})
	b.seedSeenOn(ctx, t, 3, dateValue{Year: 2023, Month: 1, Day: 31})

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
}

// constructedLocalDateTime reads the fourth neutral carrier. LOCALDATETIME
// has no property spelling in either target, so it is reached through a
// column the server builds by calling a temporal constructor. The literal
// is in the query text, so the components are known exactly and a
// conversion that dropped one is visible here and nowhere else in the
// battery.
func constructedLocalDateTime(ctx context.Context, t *testing.T, b localDateTimeColumnBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	built, err := b.localDateTimeColumn().builtLocalDateTime(ctx)
	require.NoError(t, err)
	require.Equal(t, localDateTimeValue{Year: 2024, Month: 3, Day: 5, Hour: 6, Minute: 7, Second: 8, Nanosecond: 9}, built,
		"a constructed LOCALDATETIME column must arrive component for component")
}

// mapColumnArrives drives the one column kind whose emitted read is a type
// assertion the server can falsify.
//
// scalar_map's query is `RETURN {a: 1} AS m`, so the map is built by the
// server out of a literal and nothing here writes or seeds. What the
// scenario is for is the two halves of GetRecordValue[map[string]any]:
// that the driver hands back a map[string]any at all, and that the member
// inside it is the int64 the fixture's `1` implies.
//
// The member type is pinned by require.Equal and not by a length or a key
// set, deliberately: Equal is reflect.DeepEqual here, so an arrival of
// float64(1) under the same key fails this row. Nothing else in the
// battery would notice — the column's Go type is map[string]any, `any` is
// inhabited by both, and the golden reads the same either way.
//
// FALSIFIER, and it is not the one bd gqlc-y6mo asked for. The bead names
// GetRecordValue[map[string]string] as the mutation this row must red on,
// and that mutant does not exist: GetRecordValue's parameter is
// constrained to neo4j.RecordValue, a union of the driver's own carriers,
// and map[string]string is not among them (v5 record.go:25, v6
// record.go:25). The compiler refuses it before any test runs, on both
// majors, which is a red for the wrong reason and not one this row can
// take credit for. The mutation actually run was
// GetRecordValue[[]any] — inside the union, so it compiles, and wrong, so
// the assertion in the emitted method fails and this row reds where the
// golden and the build alone stay green.
func mapColumnArrives(ctx context.Context, t *testing.T, b mapColumnBackend) { //nolint:thelper // a scenario body owns its failure frame; see the scenarios table
	got, err := b.mapColumn().oneMap(ctx)
	require.NoError(t, err,
		"the emitted read asserts the arriving value is a map[string]any; an error here is the server "+
			"sending a carriage that assertion does not hold for")
	require.Equal(t, map[string]any{"a": int64(1)}, got,
		"a map column must arrive whole and with the member types the emitted map[string]any leaves "+
			"entirely to the driver; an int64 member arriving as float64 passes every other gate")
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
