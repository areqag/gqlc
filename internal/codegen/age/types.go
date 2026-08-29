package age

import (
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
// settled on gqlc-35yu.11 since the spike, but the carrier. Every other
// backend spells these three with a neo4j driver type, and a package
// reaching Postgres through pgx cannot declare one without making the
// surface a caller writes against vary by backend. It can declare the
// neutral carriers, because the generated package declares them itself.
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
// TIME is the one temporal width still refused for want of a carrier
// here, and it is refused for the sidecar rather than for the count. Its
// value carries an offset, which rides the flat <f>Offset sidecar named
// after the property in the same arrangement TIMESTAMP's does; admitting
// it is gqlc-oeqi.
//
// An instant is admitted as a property and refused as a list element, at
// every depth, and so is anything else carrying a zone: the offset rides
// a sidecar named after the property, and a list has one name for all of
// its elements, so a list of instants has nowhere to put the zone of any
// element but the first. The three widths admitted above carry no zone,
// so they ride a list on the ordinary rule.
func (t typeMap) Property(pt graph.PropertyType) (string, bool) {
	if pt.Kind() == graph.KindList {
		elemTy, ok := t.Property(pt.Elem())
		if !ok || elemTy == goInstant {
			return "", false
		}
		return "[]" + elemTy, true
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
	case graph.TypeDuration:
		return "Duration", true
	case graph.TypeBytes,
		graph.TypeTime,
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

// Temporal maps a resolved temporal-expression kind to the Go type text
// this backend emits. Returns (typeText, ok): ok=false routes the caller
// to ErrUnrepresentableTemporal naming the kind.
//
// Every arm refuses, and the reason is upstream of the encoding table
// gqlc-35yu.11 landed for stored TIMESTAMP properties. A column of this
// shape exists only because the query text called a temporal
// constructor, and AGE 1.7.0 has none: date(), datetime(),
// localdatetime(), duration() and toTimestamp() are all "function does
// not exist", and a sweep of all 348 ag_catalog functions for
// time|date|dur|epoch|local|zone|instant returns exactly one hit,
// age_timestamp, which is an epoch-millis integer and not a temporal
// value (spike gqlc-35yu.5 §1a, live against AGE 1.7.0). So admitting a
// kind here would emit a compiling method whose statement the server
// rejects at run time — the failure mode ADR 0025 created this channel
// to prevent, one step worse.
//
// A carrier is missing too, for five of the six. The Go type the other
// backends spell date, time, local time, local datetime and duration
// with is a neo4j driver type, and a package that talks to Postgres
// through pgx cannot declare it without making the caller-facing surface
// vary by backend. The duration arm is doubly permanent: a calendar
// duration counts months, which no fixed number of microseconds is.
//
// TemporalDateTime is the one kind that would clear the carrier bar —
// its carrier is time.Time on every backend, and the encoding for it is
// settled (epoch-micros, plus a <f>Offset sidecar for the zone; see
// Property). It stays refused on the constructor ground alone.
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
