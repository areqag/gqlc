package age

import (
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
	"github.com/areqag/gqlc/internal/resolver"
)

// typeMap is the backend's Go-type table (spec §5.1) the shared phases
// read. Stateless: every entry is a pure function of the resolved type.
type typeMap struct{}

// Property maps a resolved property type to its native Go emission (spec
// §5.1). Returns (typeText, ok): ok=false routes the caller to
// ErrUnrepresentableWidth naming the width. Callers append a leading '*'
// for nullable columns and parameters at emission time.
//
// agtype's scalar vocabulary is null, boolean, integer (int64), float
// (float64), string, list and map. Narrower integer and float widths ride
// the wider carrier and narrow through a Go conversion, the same
// arrangement every backend uses. BYTES has no agtype scalar at all.
// The eight oversized numeric widths have no faithful carrier anywhere
// and are permanently out (§9).
//
// LIST and ANY are admitted. agtype's own vocabulary has a list and a
// map alongside the scalars, so both widths have something on the wire
// to decode from, and both emit the text every other backend emits for
// them — the Go a caller writes against does not vary by backend. A list
// rides its element's carrier, at whatever nesting depth, so it is
// admitted exactly when its element width is; ANY is Go's any (ADR
// 0020), decoded through the agtype value vocabulary rather than through
// one declared width.
//
// TIMESTAMP rides the integer scalar as microseconds since the Unix
// epoch — an encoding this package owns and the emitted helpers are the
// whole of. Microseconds because the gate on an encoding here is
// ORDERING, not round-tripping: a stored integer is compared by agtype's
// own integer ordering, so an author's ORDER BY n.at and WHERE n.at >
// $since are answered correctly with no rewriting of their query text
// (ADR 0005). The ISO-string alternative round-trips just as well and
// sorts by database collation, which is chronological only by accident
// of alphabet. Confirmed live against AGE 1.7.0, negatives and year 9999
// included (spike gqlc-35yu.5 §1d ENC1, §1f).
//
// The zone rides a flat <f>Offset sidecar in offset-seconds, so the sort
// key stays the property itself; the nested {t,o} form sorts by jsonb
// key order and would have forced gqlc to rewrite ORDER BY to reach .t
// (§1e). The carrier is time.Time, which is what neo4j spells TIMESTAMP
// with too, so the declared surface does not vary by backend.
//
// DATE, LOCAL TIME and DURATION ride the neutral carriers temporal.go
// declares — Date, LocalTime and Duration (ADR 0033). That is what
// admits them here: the obstacle was never the encoding, which has been
// settled on gqlc-35yu.11 since the spike, but the carrier. Until PR
// #1481 the neo4j targets spelled these three with a neo4j driver type,
// and a package reaching Postgres through pgx cannot declare one without
// making the surface a caller writes against vary by backend. ADR 0033
// took that spelling off every backend rather than adding a second one
// here, so what the surface names is the neutral carrier the generated
// package declares itself — on neo4j as on AGE.
//
// DATE is the zero-padded ISO 'YYYY-MM-DD' string, the one temporal
// spelling whose lexical order is its chronological order — across
// [0001-01-01, 9999-12-31] and nowhere else, which is the whole of where
// the encoding is defined. Outside it the width stops being fixed and
// the ordering goes with it: year 10000 needs a fifth digit and sorts
// under 2024 because '1' < '2', and a proleptic year before 1 CE needs a
// sign, which sorts under every digit and so files the whole era at the
// front, ascending. The emitted encoder and decoder both range-check to
// that window rather than store or read a string the database will
// silently mis-sort; the fixed width is a precondition of the encoding,
// not a property of the type.
//
// LOCAL TIME is microseconds since midnight in the integer scalar, in
// [0, 86_400e6): the same argument as TIMESTAMP, one width down, and the
// count is non-negative and fixed-range so agtype's integer ordering is
// chronological order within the day with nothing for gqlc to rewrite.
//
// DURATION is total microseconds in the integer scalar. ADR 0002
// collapsed the (YEAR TO MONTH) and (DAY TO SECOND) qualifiers onto one
// carrier, so whether a value counts months is not knowable at generate
// time: the emitted encoder refuses a Duration whose Months is non-zero
// at run time, naming the field, because no fixed count of microseconds
// is faithful to a month. Decode fills Seconds and Nanos alone.
//
// TIME is microseconds since midnight in the integer scalar too, but
// UTC-NORMALISED — the clock reading minus its offset, wrapped back into
// [0, 86_400e6) — with the offset itself in the flat <f>Offset sidecar
// named after the property, the same arrangement TIMESTAMP's uses.
// Normalising is what makes the stored count comparable across offsets:
// storage order is instant order, so agtype's integer ordering answers an
// author's ORDER BY without gqlc rewriting it, which a raw local reading
// would not. Decode adds the offset back, wrapping the other way, and
// fills OffsetSeconds so the reading comes back in the zone it was
// written in.
//
// A zoned width is admitted as a property and refused inside a
// CONTAINER, at every depth: the offset rides a sidecar named after the
// property, and a container gives its contents no property names of
// their own to hang a sidecar on — a list has one name for all of its
// elements, and a record field is not a property. carriesZone is the one
// answer to which widths those are, and the same predicate offsetSidecar
// derives a name from. The widths admitted above carry no zone, so they
// ride a container on the ordinary rule.
//
// The rule was written for list elements and generalised to any
// container position when records began to emit (spec §2, the ruling for
// gqlc-x9tg7); the reason never was about lists in particular.
//
// The two '*' positions belong to different owners. The WHOLE-VALUE star
// is the caller's: a nullable column, field or parameter gets its
// leading '*' at emission time. The ELEMENT star is this table's own,
// applied by the list arm from the width's ElemNotNull qualifier, so
// every position carrying a list — Row, EntityField, Param and the
// nested text under each — reads one answer rather than four
// re-derivations of it. A nullable column of LIST<INT64> is therefore
// `*[]*int64`: one star from each owner.
//
// EXEMPT FROM gocyclo, NOT FROM gocognit. gocyclo counts this 33 because it
// increments once per `case` and the table has one per property width;
// gocognit counts it 15, charging the `switch` once and the nesting nothing,
// and gocognit is the one describing what a reader faces here — a flat table
// with two container guards in front of it.
//
// Splitting it to satisfy the count is not available, and that is a fact
// about this method rather than a preference. Two guards read it by
// STRUCTURE, and both red LOUDLY rather than quietly:
//
//   - typescan.PropertyArms skips any decl whose `fn.Recv == nil` and takes
//     the method name as an argument, so a table moved to a plain function is
//     invisible to it. What that does not do is pass vacuously —
//     types_test.go asserts `require.NotEmpty(t, arms, ...)` before ranging
//     over them, precisely so a walk that read nothing cannot hold the table
//     to nothing.
//   - render_queries_test.go's typeTableGoTypes reads this method's RETURN
//     statements out of the package directory, and returnedGoType REFUSES a
//     return whose shape it cannot read rather than skipping it — so a return
//     of the form `t.someHelper(pt)` reds it too.
//
// Both were measured failing on 2026-09-10 when exactly that split was
// attempted.
//
//nolint:gocyclo // flat per-width dispatch table; gocognit scores it 15 and still gates it
func (t typeMap) Property(pt graph.PropertyType) (string, bool) {
	if pt.Kind() == graph.KindList {
		elemTy, ok := t.Property(pt.Elem())
		if !ok || carriesZone(elemTy) {
			return "", false
		}
		// An element the schema permits to be NULL carries the star, the
		// same rule every other nullable position obeys. `any` is the one
		// exemption: it already carries null as nil, so a star would add a
		// second spelling of the same absence.
		//
		// AFTER the zoned refusal above, never before: carriesZone is
		// exact equality on the carrier text, so starring first turns
		// "Time" into "*Time", matches nothing, and admits a width whose
		// zone has no sidecar to ride in.
		//
		// Folded into the element text rather than written as
		// `"[]*" + elemTy` so this method keeps exactly one list return of
		// the form `"[]" + elem` — the only shape render_queries_test.go's
		// type-table walk can read, and it refuses what it cannot read
		// rather than skipping it.
		if !pt.ElemNotNull() && elemTy != "any" {
			elemTy = "*" + elemTy
		}
		return "[]" + elemTy, true
	}
	if pt.Kind() == graph.KindRecord {
		if pt == graph.TypeAnyRecord {
			// Fields undeclared, so there is no struct to build: the
			// record whose contents are unconstrained maps to Go's
			// unconstrained string-keyed product, exactly as ANY maps
			// to any and LIST<ANY> to []any (spec §3).
			return "map[string]any", true
		}
		return codegen.RecordStructText(pt.Fields(), func(fieldTy graph.PropertyType) (string, bool) {
			text, ok := t.Property(fieldTy)
			if !ok || carriesZone(text) {
				return "", false
			}
			return text, true
		})
	}
	if pt.Kind() == graph.KindUnion {
		// The carrier is `any` and the admission rule decides (spec §4).
		// The member carrier is this table's own wrapped in the SAME zoned
		// refusal the record arm above and the list arm before it apply: a
		// union is a container position, and the offset sidecar is named
		// after the property, which a member has no name of its own inside.
		// So UNION<TIMESTAMP|STRING> is refused here for the reason
		// LIST<TIMESTAMP> is, before the family question is ever asked.
		return codegen.UnionCarrier(pt, func(memberTy graph.PropertyType) (string, bool) {
			text, ok := t.Property(memberTy)
			if !ok || carriesZone(text) {
				return "", false
			}
			return text, true
		}, wireFamily)
	}
	switch pt {
	case graph.TypeString:
		return "string", true
	case graph.TypeBool:
		return "bool", true
	case graph.TypeInt:
		return "int", true
	case graph.TypeInt8:
		return "int8", true
	case graph.TypeInt16:
		return "int16", true
	case graph.TypeInt32:
		return "int32", true
	case graph.TypeInt64:
		return "int64", true
	case graph.TypeUint:
		return "uint", true
	case graph.TypeUint8:
		return "uint8", true
	case graph.TypeUint16:
		return "uint16", true
	case graph.TypeUint32:
		return "uint32", true
	case graph.TypeUint64:
		return "uint64", true
	case graph.TypeFloat, graph.TypeFloat64:
		return "float64", true
	case graph.TypeFloat32:
		return "float32", true
	case graph.TypeAnyPropertyValue:
		return "any", true
	case graph.TypeList:
		// LIST<ANY> spelled out, so the Kind() guard above intercepts it
		// and this arm is unreachable. Listed so the exhaustive linter
		// sees the full constant set, and answering "[]any" keeps it
		// agreeing with the arm that does the work.
		return "[]any", true
	case graph.TypeTimestamp:
		return "time.Time", true
	case graph.TypeDate:
		return "Date", true
	case graph.TypeLocalTime:
		return "LocalTime", true
	case graph.TypeTime:
		return "Time", true
	case graph.TypeDuration:
		return "Duration", true
	case graph.TypeAnyRecord:
		// RECORD<ANY> spelled out, so the Kind() guard above intercepts
		// it and this arm is unreachable. Listed so the exhaustive
		// linter sees the full constant set, and answering
		// "map[string]any" keeps it agreeing with the arm that does the
		// work — the arrangement graph.TypeList already has.
		return "map[string]any", true
	case graph.TypeUUID:
		// agtype's value vocabulary is boolean / integer / float /
		// string / list / map and nothing else, so there is no shape a
		// 128-bit identifier comes back from the server as itself in. A
		// string carrier would round-trip the SPELLING and drop the
		// declared type, which is the silent widening §5.1 exists to
		// refuse.
		//
		// The refusal NAMES this backend, which it did not until
		// stage 2 of bd gqlc-eg4b: neo4j-go-v6 carries UUID as
		// dbtype.UUID, so this is AGE's answer rather than the
		// declaration's obstacle and an author reading it has somewhere
		// to go. The name follows from UUID's absence from
		// uncarriedEverywhere rather than from anything written here.
		return "", false
	case graph.TypeBytes,
		graph.TypeInt128, graph.TypeInt256,
		graph.TypeUint128, graph.TypeUint256,
		graph.TypeFloat16, graph.TypeFloat128, graph.TypeFloat256,
		graph.TypeDecimal:
		return "", false
	}
	// PropertyType is an open string type, so a width internal/graph gains
	// without a row above arrives here rather than failing to compile.
	// Rejecting it routes the caller to ErrUnrepresentableWidth naming the
	// width: generation fails loudly instead of emitting a field no
	// decoder can fill.
	return "", false
}

// wireFamily folds one carrier text onto the equivalence class of declared
// widths agtype delivers as one indistinguishable value (CONTEXT.md, "wire
// family"). It is asked of a closed union's members alone — the one place
// two declared widths have to be told apart by their arrival rather than
// by the declaration that asked for them.
//
// The vocabulary is agtype's own: boolean, integer, float, string, list
// and map. The tags are those words and not the Go carriers, because THIS
// is where the two backends part company and the carrier would hide it: on
// neo4j a DATE arrives as its own dbtype and is distinguishable from a
// STRING, while here it is the zero-padded ISO text the Property comment
// above describes, so DATE and STRING are ONE family and
// UNION<DATE|STRING> is refused on this backend alone.
//
// Derived from agtypeCarrier and then folded onto the scalar each carrier
// actually rides, rather than restated from the Property table, so the
// families cannot drift from the encodings this package emits. The fold is
// decodeFunc's own dispatch read one level further out: agtypeDate reads
// its value through agtypeString, and agtypeLocalTime, agtypeDuration,
// agtypeInstant and agtypeTime all read theirs through agtypeInt64.
//
// goInstant and goTime answer integer for completeness and are never asked
// inside a union: both carry a zone, and the Property arm above refuses a
// zoned member on the container rule before the family question is
// reached.
//
// ANY is WireFamilyIndistinct — it arrives as whatever the writer wrote,
// so it owns no shape and leaves none for a member beside it. A nested
// UNION carries as `any` and lands there for the same reason.
//
// The fallthrough answers WireFamilyIndistinct too, and that direction is
// chosen rather than inherited. A carrier this fold has no row for is one
// whose arrival this package cannot predict, and the tag that collides
// with everything refuses the union it appears in — where a family of its
// own would admit a union whose decode has no arm to dispatch through.
// Fail-closed, in the one arm that exists because the carrier table may
// grow a row before this one does.
//
// A PLAIN FUNCTION and not a typeMap method, deliberately: the census walk
// in render_queries_test.go tells a carrier method from the rest by its
// declared result shape, and a (string, bool) method of this receiver
// would be swept as one — holding family tags to decodeFunc arms, which
// they are not Go types for.
func wireFamily(goType string) string {
	switch {
	case goType == codegen.UnionCarrierText:
		return codegen.WireFamilyIndistinct
	case strings.HasPrefix(goType, "["):
		return "list"
	case goType == goAnyRecord, codegen.IsRecordStruct(goType):
		return "map"
	}
	switch agtypeCarrier(goType) {
	case "bool":
		return "boolean"
	case "int64", goLocalTime, goDuration, goInstant, goTime:
		return "integer"
	case "float64":
		return "float"
	case "string", goDate:
		return "string"
	}
	return codegen.WireFamilyIndistinct
}

// StorableProperty admits every width. agtype is a JSON-shaped value and
// nests without limit, so this store holds shapes neo4j's refuses — a
// nested list among them, measured against the pinned image by
// TestAGEStoresANestedListProperty, which is the other half of the
// divergence ADR 0035 records and the reason nested_list_property is an
// AGE-only fixture rather than a deleted one.
//
// The zoned-list refusal stays in Property and does not belong here: a
// list of zoned temporals is refused because the offset sidecar is named
// after the property and a list has one name for all its elements, so
// there is nowhere to put any element's zone but the first. That is a
// carrier problem, and answering it on this axis would say the store
// cannot hold the value, which is not what is wrong with it.
func (typeMap) StorableProperty(graph.PropertyType) bool { return true }

// Temporal maps a resolved temporal-expression kind to the Go type text
// this backend emits. Returns (typeText, ok): ok=false routes the caller
// to ErrUnrepresentableTemporal naming the kind.
//
// Every arm refuses, and the reason is upstream of the encoding table
// gqlc-35yu.11 landed for stored TIMESTAMP properties. A column of this
// shape exists only because the query text called a temporal
// constructor, and no temporal constructor this project has measured is
// defined on AGE 1.7.0: date(), datetime(), localdatetime(), duration()
// and toTimestamp() are all "function does not exist", and a sweep of
// the 348 ag_catalog functions then in the pinned image for
// time|date|dur|epoch|local|zone|instant returns exactly one hit,
// age_timestamp, which is an epoch-millis integer and not a temporal
// value (spike gqlc-35yu.5 §1a, live against AGE 1.7.0). No test
// re-measures the catalogue, so that sweep is provenance rather than a
// closed set. Admitting a kind here would therefore emit a compiling
// method whose statement the server rejects at run time — the failure
// mode ADR 0025 created this channel to prevent, one step worse.
//
// The constructor ground is the whole of it. This comment carried a
// second one until bd gqlc-kjtu7 — that five of the six kinds had no
// carrier a pgx package could declare — and it was true when written,
// the neo4j targets then spelling them dbtype.Date and its siblings. ADR
// 0033 took that spelling off every backend (PR #1481), and temporal.go
// carries the neutral carriers into the generated package of any target
// whose surface names one — AGE among them since PR #1679, which is what
// TestTemporalCarriersAreEmittedExactlyWhenReferenced holds. So every
// kind clears the carrier question today and none of them clears the
// constructor one.
//
// One narrower fact outlives that ground, and it bears on the ENCODING
// rather than on admitting a kind: a calendar duration counts months,
// which no fixed count of microseconds is faithful to. The stored lane
// lives with it by refusing a non-zero Months at run time (see
// Property), so it is a limit on the value rather than a second bar
// here.
//
// TemporalDateTime was once singled out as the only kind clearing the
// carrier bar, its carrier being time.Time on every backend and its
// encoding settled (epoch-micros, plus a <f>Offset sidecar for the zone;
// see Property). Now that every kind clears it, what is left of that is
// the constructor ground DateTime always also rested on.
func (typeMap) Temporal(k resolver.Temporal) (string, bool) {
	switch k {
	case resolver.TemporalDate:
		return "", false
	case resolver.TemporalTime:
		return "", false
	case resolver.TemporalLocalTime:
		return "", false
	case resolver.TemporalDateTime:
		return "", false
	case resolver.TemporalLocalDateTime:
		return "", false
	case resolver.TemporalDuration:
		return "", false
	}
	// Only a value converted in from outside resolver.Temporal's
	// vocabulary reaches here. Refusing stays right if that vocabulary
	// grows in a build this file was not recompiled against.
	return "", false
}

// Scalar maps a resolved scalar-expression kind to the Go type text this
// backend emits (spec §5.1 column-shape table). Null → any: the
// openCypher null literal is a legal-but-pointless projection.
//
// The bool / int / float / string arms are the Go shape of an agtype
// scalar a decode helper reads. The null and map arms are not:
// unservedColumn refuses a column of either kind, so no emitted code
// reaches them. They name what a helper would fill, not a carrier a
// column arrives on (ADR 0025). Both texts have a decodeFunc arm all the
// same — "any" the agtype value vocabulary, "map[string]any" agtypeMap.
func (typeMap) Scalar(k resolver.Scalar) string {
	switch k {
	case resolver.ScalarBool:
		return "bool"
	case resolver.ScalarInt:
		return "int64"
	case resolver.ScalarFloat:
		return "float64"
	case resolver.ScalarString:
		return "string"
	case resolver.ScalarNull:
		return "any"
	case resolver.ScalarMap:
		return "map[string]any"
	}
	// Only a value converted in from outside resolver.Scalar's vocabulary
	// reaches here; projecting it undecoded beats guessing a Go type for
	// a kind the resolver never named.
	return "any"
}
