package age

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// The annotation AGE suffixes onto every entity it renders. A vertex and
// an edge are the same object otherwise, so this is the only thing on the
// wire that tells them apart.
const (
	vertexAnnotation = "::vertex"
	edgeAnnotation   = "::edge"
)

// The Go type texts this backend's temporals are emitted as, and the
// tokens the render layer dispatches each encoding on — the type table
// is the only thing that decides which resolved types reach them (ADR
// 0025).
//
// goInstant is the standard library's, which is already neutral.
// The rest name the carriers temporal.go declares in the generated
// package itself (ADR 0033), so they are unqualified and cost no import.
const (
	goInstant   = "time.Time"
	goDate      = "Date"
	goLocalTime = "LocalTime"
	goTime      = "Time"
	goDuration  = "Duration"
)

// offsetSidecar names the property one field's UTC offset in seconds
// rides in, with ok=false for a field whose stored value carries no
// zone. It is the one answer to both halves of the question — whether a
// field derives a sidecar at all, and what that sidecar is called — so
// the decode that reads the property and the gate that refuses a
// collision on it are reading the same name for the same set of fields,
// and a width that gains a zone is taught to both in one edit.
//
// A prepared field carries the Go type it is emitted as and not the
// width that was declared, so the carrier text is what the answer is
// keyed on. time.Time is TIMESTAMP's and Time is TIME's — the two
// admitted widths whose value keeps an offset beside it (ADR 0025, and
// the encoding table on gqlc-35yu.11 which gives both the same sidecar).
//
// What the two do with the offset differs and this does not care: the
// instant is stored as an absolute count and re-zoned by moving its
// Location, whereas a TIME is stored UTC-normalised and re-zoned by
// adding the offset back into the clock reading. Both need the same
// property beside them under the same name, which is the whole of what
// this answers.
//
// Flat rather than a member of a map so the instant stays the property
// itself and an author's ORDER BY over it needs no rewriting.
//
// No sidecar is derived on the write side: gqlc adds no parameter to the
// author's CREATE (ADR 0005), so an instant written and read back
// through one schema round-trips in UTC. What the read finds is whatever
// else wrote the graph, and a second gqlc package is one of those
// things: the name is derived, not declared, so a schema is free to
// declare it as a property of its own and bind a write of it.
// rejectOffsetSidecarCollisions refuses the schema that declares it
// beside the instant it derives from — where one key would have two
// readers — and not the schemas that declare it anywhere else.
func offsetSidecar(f codegen.EntityField) (string, bool) {
	if !carriesZone(f.GoType) {
		return "", false
	}
	return f.PropName + "Offset", true
}

// carriesZone reports whether one emitted carrier keeps a UTC offset
// beside its value, and so needs the sidecar property. It is the single
// answer to that question: offsetSidecar derives a name from it, and
// typeMap.Property refuses a list of one of these because a list has one
// name for all of its elements and so has nowhere to put the second and
// later offsets. Those two are the same rule read in opposite
// directions, and splitting them is how a width gets admitted into a list
// it has no room to be decoded out of.
func carriesZone(goType string) bool {
	return goType == goInstant || goType == goTime
}

// helpers records which agtype encode / decode helpers a batch reaches
// for. Each is emitted only when something calls it.
//
// Only one direction of that is gated. A helper referenced but not
// declared fails to compile, which TestGoldenBuild and
// TestEmittedHelpersAreClosedOverWhatTheyCall both catch. A helper
// declared but not referenced is caught by nothing: .golangci.yml sets
// generated: lax, so the linter skips the emitted goldens outright. The
// unreferenced direction is dead weight in the output rather than a
// defect in it, which is why it is recorded here and not gated.
//
// agtypeEntity, agtypeObject, agtypeSpan and agtypeString are not among
// them. A graph type's element type list is one-or-more (GQL.g4
// elementTypeList), so every schema declares at least one entity, every
// entity decoder splits the wire value and reads its label, and those
// four are in every emission this package can produce. A field standing
// for a condition that cannot be false would report nothing.
type helpers struct {
	args    bool // agtypeArgs — some query binds at least one parameter
	boolean bool
	integer bool
	float   bool

	// The checked narrowings, each emitted only where a width narrower
	// than its carrier decodes. They are separate marks rather than one,
	// because an emission that declares only integer widths would
	// otherwise carry agtypeFloat32 and the math import it alone names,
	// and an unexported function nothing calls fails the golden lint
	// fence (bd gqlc-awtb).
	intAs       bool // agtypeIntAs — an integer width narrower than int64 decodes
	narrowFloat bool // agtypeFloat32 — a FLOAT32 decodes

	list     bool // agtypeList — something decodes an agtype list
	value    bool // agtypeValue / agtypeMap — something decodes a value of no declared shape
	prop     bool // agtypeProperty — some entity declares a non-nullable property
	nullProp bool // agtypeNullableProperty — some entity declares a nullable property

	instant    bool // agtypeInstant — some column or property decodes an encoded instant
	zone       bool // agtypeZone — an instant property, so the offset sidecar is beside it
	micros     bool // agtypeMicros — some query binds a non-nullable instant parameter
	nullMicros bool // agtypeNullableMicros — some query binds a nullable one

	// agtypeOffset — the sidecar read the two zoned widths share. Marked
	// by either of them, since it is the one place the bound lives.
	offset bool

	date      bool // agtypeDate — something decodes a stored DATE
	localTime bool // agtypeLocalTime — something decodes a stored LOCAL TIME
	zonedTime bool // agtypeTime / agtypeTimeAt — something decodes a stored TIME
	timeZone  bool // agtypeTimeZone — one of those is an entity property

	// agtypeWrapDay — the day-interval correction both directions of the
	// TIME encoding take. Marked by decode and by encode independently,
	// because a query binding a TIME parameter reaches it with nothing
	// decoding one, and a helper called but not declared does not compile.
	wrapDay  bool
	duration bool // agtypeDuration — something decodes a stored DURATION

	// The encode direction of the same widths. Each is one helper
	// whatever nullability or nesting the parameter has, because the two
	// combinators below carry those: an encoder that can fail has no
	// expression form inside the args map literal, so the emission binds
	// its result to a local either way and there is nothing for a
	// per-nullability variant to save.
	dateText        bool // agtypeDateText — some query binds a DATE parameter
	localTimeMicros bool // agtypeLocalTimeMicros — some query binds a LOCAL TIME parameter
	timeMicros      bool // agtypeTimeMicros — some query binds a TIME parameter
	durationMicros  bool // agtypeDurationMicros — some query binds a DURATION parameter

	encNullable bool // agtypeEncodedNullable — one of those parameters is nullable
	encList     bool // agtypeEncodedList — one of those parameters is a list

	// lists holds every Go slice type the batch decodes into, each of
	// which takes a named wrapper around the generic walk. A nested list
	// registers its element type too, because the outer wrapper's element
	// decoder is the inner wrapper.
	lists []string
}

// importsTime reports whether models.go names the time package, which
// is the whole of what it decides.
//
// It is not "the batch carries a temporal": LOCAL TIME and DURATION both
// ride the integer scalar in either direction and their helpers do
// arithmetic on an int64, so a batch carrying nothing but those two
// spells no time at all. DATE is on the list because both directions of
// its encoding go through the calendar — time.Parse to read the ISO
// string, time.Date to reject a day the calendar does not have.
//
// zone is not a disjunct. It is marked on an entity field whose Go type
// is the instant, which is the same condition that marks instant, so a
// batch reaching the sidecar read has already answered true here.
func (h helpers) importsTime() bool {
	return h.instant || h.micros || h.nullMicros || h.date || h.dateText
}

// forEntities marks the helpers an entity emission reaches beyond the
// four every emission carries.
func (h *helpers) forEntities(entities []wiredEntity) {
	for _, e := range entities {
		for _, f := range e.Fields {
			h.need(f.GoType)
			// The offset sidecar is a second property of the same
			// vertex, so only an entity decode has it in hand. Which
			// re-zoning helper reads it depends on the carrier: an
			// instant moves its Location, a TIME adds the offset back
			// into the clock reading.
			if _, ok := offsetSidecar(f); ok {
				h.offset = true
				if f.GoType == goTime {
					h.timeZone = true
				} else {
					h.zone = true
				}
			}
			if f.Nullable {
				h.nullProp = true
			} else {
				h.prop = true
			}
		}
	}
}

// forParams marks the helpers one query's bound parameters encode
// through. Only a temporal needs one: every other emitted Go type is
// already a shape the JSON encoder writes as the agtype scalar it rides,
// and a carrier left alone would cross as the JSON object its fields
// spell rather than as the scalar the encoding stores.
//
// Nullability and list nesting are marked on the two combinators rather
// than on a variant of each encoder, and the leaf carrier is what the
// encoder itself is chosen by, so a []Date and a *Date mark the same
// agtypeDateText.
func (h *helpers) forParams(params []codegen.Param) {
	for _, p := range params {
		leaf, list := p.GoType, false
		if elem, ok := strings.CutPrefix(leaf, "[]"); ok {
			leaf, list = elem, true
		}
		if leaf == goInstant {
			// The instant predates the combinators and keeps its own
			// pair: its encode cannot fail, so it stays an expression
			// inside the args map and needs no local bound to it.
			if p.Nullable {
				h.nullMicros = true
			} else {
				h.micros = true
			}
			continue
		}
		fallible := true
		switch leaf {
		case goDate:
			h.dateText = true
		case goLocalTime:
			h.localTimeMicros = true
		case goTime:
			h.timeMicros = true
			h.wrapDay = true
		case goDuration:
			h.durationMicros = true
		default:
			fallible = false
		}
		if !fallible {
			continue
		}
		if p.Nullable {
			h.encNullable = true
		}
		if list {
			h.encList = true
		}
	}
}

// need marks the helper one emitted Go type decodes through. Narrow
// integer and float widths ride the wide carrier, so they mark their
// carrier's helper AND the checked narrowing built on it — the
// narrowing is where the declared width is enforced, and it is a helper
// of its own rather than a conversion at the call site so that no
// emission site can reach the width without the check (ADR 0037).
// A slice marks the generic list walk plus a named wrapper
// of its own, and recurses so the element's helper is marked too; a Go
// type of no declared shape marks the agtype value vocabulary.
func (h *helpers) need(goType string) {
	if elem, ok := strings.CutPrefix(goType, "[]"); ok {
		h.list = true
		if !slices.Contains(h.lists, goType) {
			h.lists = append(h.lists, goType)
		}
		h.need(elem)
		return
	}
	if goType == "any" || goType == "map[string]any" {
		h.needValue()
		return
	}
	switch agtypeCarrier(goType) {
	case "bool":
		h.boolean = true
	case "int64":
		h.integer = true
		if goType != "int64" {
			h.intAs = true
		}
	case "float64":
		h.float = true
		if goType != "float64" {
			h.narrowFloat = true
		}
	case goInstant:
		// The instant rides the integer scalar, so its helper is built
		// on the integer one.
		h.instant = true
		h.integer = true
	case goDate:
		// The date rides the string scalar, which every emission
		// declares, so there is no second helper to mark here.
		h.date = true
	case goLocalTime:
		h.localTime = true
		h.integer = true
	case goTime:
		h.zonedTime = true
		h.wrapDay = true
		h.integer = true
	case goDuration:
		h.duration = true
		h.integer = true
	}
}

// needValue marks agtypeValue and the helpers it dispatches into. A value
// of no declared shape is read through agtype's own vocabulary, so every
// arm of that vocabulary has to be in the file whatever the rest of the
// batch declares.
func (h *helpers) needValue() {
	h.value = true
	h.integer, h.float, h.list = true, true, true
}

// listHelpers is the batch's named list wrappers, shallowest first and
// then by name. The order the batch reaches them is already a function
// of the schema, so this is not what makes the emission deterministic:
// it is what makes it readable, since a wrapper's element decoder is the
// wrapper one level in and reading inner before outer follows the decode.
func (h helpers) listHelpers() []string {
	out := slices.Clone(h.lists)
	slices.SortFunc(out, func(a, b string) int {
		if d := cmp.Compare(listDepth(a), listDepth(b)); d != 0 {
			return d
		}
		return cmp.Compare(a, b)
	})
	return out
}

// listDepth counts a Go slice type's nesting.
func listDepth(goType string) int {
	depth := 0
	for {
		elem, ok := strings.CutPrefix(goType, "[]")
		if !ok {
			return depth
		}
		depth, goType = depth+1, elem
	}
}

// listHelperName is the wrapper emitted for one Go slice type:
// agtypeListOf per level of nesting, then the element type exported.
// Every type that reaches here came out of the property table, whose
// leaves are Go identifiers.
func listHelperName(goType string) string {
	name := "agtype"
	for {
		elem, ok := strings.CutPrefix(goType, "[]")
		if !ok {
			break
		}
		name, goType = name+"ListOf", elem
	}
	return name + strings.ToUpper(goType[:1]) + goType[1:]
}

// agtypeCarrier picks the decode helper's return type for a Go type the
// emission wants to produce. Integer widths widen to int64 and float
// widths to float64, matching agtype's own two numeric scalars; the
// caller narrows with a Go conversion.
func agtypeCarrier(goType string) string {
	switch goType {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64":
		return "int64"
	case "float32", "float64":
		return "float64"
	default:
		return goType
	}
}

// renderModels emits models.go (spec §5.2): one exported struct per
// schema node and edge type with the decoder that fills it, and the
// agtype helpers those and the query methods are built on.
//
// The entity surface is the same surface every other backend emits for
// the same schema — same struct names, same fields, same nullability.
// Only what the decoders read differs, because only the wire differs.
// The sealed edge-union interfaces those backends also emit have no
// counterpart here: this one refuses the column that would name them.
func renderModels(pkg string, entities []wiredEntity, h helpers) []byte {
	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package " + pkg + "\n")

	b.WriteString("\nimport (\n")
	b.WriteString("\t\"bytes\"\n")
	b.WriteString("\t\"encoding/json\"\n")
	b.WriteString("\t\"fmt\"\n")
	// agtypeFloat32 is the only thing here that names math, so the import
	// gates on it alone rather than on any narrowing.
	if h.narrowFloat {
		b.WriteString("\t\"math\"\n")
	}
	if h.integer || h.float {
		b.WriteString("\t\"strconv\"\n\t\"strings\"\n")
	}
	if h.importsTime() {
		b.WriteString("\t\"time\"\n")
	}
	b.WriteString(")\n")

	writeEntities(&b, entities)

	if h.args {
		b.WriteString(`
// agtypeArgs renders a query's bound parameters as the single agtype
// argument cypher() takes. The value crosses as a Go string: agtype's
// OID is assigned per database, so pgx holds no registered codec for it
// and sends a string as an untyped literal, which is the form agtype's
// input function reads. JSON is a subset of agtype's own text syntax, so
// what the encoder produces is text AGE parses.
func agtypeArgs(args map[string]any) (string, error) {
	out, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("gqlc: encode query parameters: %w", err)
	}
	return string(out), nil
}
`)
	}
	b.WriteString(`
// agtypeString decodes an agtype string scalar. AGE renders one as a
// JSON string, escapes included, so the JSON decoder reads it back
// exactly; it also refuses every other agtype scalar, which is what
// stops a mis-shaped projection arriving as a plausible value.
//
// The target is a pointer because that is what separates the two shapes
// the decoder would otherwise agree on: unmarshalling a JSON null into a
// string succeeds and leaves it empty, so an agtype null would reach a
// non-nullable column as "".
func agtypeString(raw []byte) (string, error) {
	var out *string
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("gqlc: %q is not an agtype string: %w", raw, err)
	}
	if out == nil {
		return "", fmt.Errorf("gqlc: %q is not an agtype string", raw)
	}
	return *out, nil
}
`)
	if h.boolean {
		b.WriteString(`
// agtypeBool decodes an agtype boolean scalar. The two spellings are the
// whole vocabulary, so comparing against them refuses the numeric and
// abbreviated forms a general boolean parser accepts and agtype never
// emits.
func agtypeBool(raw []byte) (bool, error) {
	switch string(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("gqlc: %q is not an agtype boolean", raw)
}
`)
	}
	if h.integer {
		b.WriteString(`
// agtypeInt64 decodes an agtype integer scalar, whose range is Go's
// int64. A value carrying the ::numeric annotation is one AGE evaluated
// in arbitrary precision: it is an integer only if it survives the parse
// as one, so a fractional part fails the decode rather than truncating
// quietly.
func agtypeInt64(raw []byte) (int64, error) {
	out, err := strconv.ParseInt(strings.TrimSuffix(string(raw), "::numeric"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("gqlc: %q is not an agtype integer: %w", raw, err)
	}
	return out, nil
}
`)
	}
	if h.intAs {
		b.WriteString(`
// agtypeIntAs decodes an agtype integer into a width narrower than the
// int64 scalar it rides in, refusing a stored value that width cannot
// hold rather than wrapping it.
//
// Two clauses, and both are load-bearing. The round-trip catches every
// width whose range is a strict subset of int64's. It cannot catch
// uint64, where the conversion is a bijection and int64(uint64(-1)) is
// -1 again; there the sign comparison is the whole of the check. A
// UINT64 property's readable set is therefore [0, MaxInt64], since
// agtype's integer scalar is signed 64-bit and a larger value is
// unstorable rather than unreadable.
func agtypeIntAs[T ~int | ~int8 | ~int16 | ~int32 | ~int64 |
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64](raw []byte) (T, error) {
	v, err := agtypeInt64(raw)
	if err != nil {
		return 0, err
	}
	out := T(v)
	if int64(out) != v || (out < T(0)) != (v < 0) {
		return 0, fmt.Errorf("gqlc: value %d does not fit the declared %T width", v, out)
	}
	return out, nil
}
`)
	}
	if h.float {
		b.WriteString(`
// agtypeFloat64 decodes an agtype float scalar. The float parser reads
// it rather than the JSON decoder because agtype's float vocabulary is
// IEEE 754's: NaN, Infinity and -Infinity are values AGE emits and JSON
// has no spelling for. A value carrying the ::numeric annotation is one
// AGE evaluated in arbitrary precision, and lands on the nearest
// float64.
func agtypeFloat64(raw []byte) (float64, error) {
	out, err := strconv.ParseFloat(strings.TrimSuffix(string(raw), "::numeric"), 64)
	if err != nil {
		return 0, fmt.Errorf("gqlc: %q is not an agtype float: %w", raw, err)
	}
	return out, nil
}
`)
	}
	if h.narrowFloat {
		b.WriteString(`
// agtypeFloat32 decodes an agtype float into the narrower width,
// refusing a value that overflows to an infinity the stored value did
// not hold.
//
// Precision loss is NOT refused: FLOAT32 is approximate and every
// in-range float64 rounds to reach it. The test is the invented infinity
// rather than a comparison against math.MaxFloat32, because a float64
// strictly greater than MaxFloat32 can still round DOWN to it — that
// value is representable and a magnitude test would refuse it. An
// infinity or a NaN the store already held passes through unchanged.
func agtypeFloat32(raw []byte) (float32, error) {
	v, err := agtypeFloat64(raw)
	if err != nil {
		return 0, err
	}
	out := float32(v)
	if math.IsInf(float64(out), 0) && !math.IsInf(v, 0) {
		return 0, fmt.Errorf("gqlc: value %g does not fit the declared float32 width", v)
	}
	return out, nil
}
`)
	}
	if h.instant {
		b.WriteString(`
// agtypeInstant decodes an encoded instant: agtype has no temporal
// value, so gqlc stores one as the integer scalar, counting
// microseconds since the Unix epoch. Microseconds are the finest
// resolution that spans the range without overflowing an int64, and the
// count is the whole of the instant — the offset sidecar beside a
// stored property decides only how it prints.
//
// The zone is UTC because that is what the count alone determines.
//
// The count is bounded to the four-digit calendar — year 1 through year
// 9999 inclusive — before the instant is built. gqlc's own encode writes
// the micros, but it writes them to an ordinary agtype integer property
// on a vertex any writer can touch, so the count read back is whatever
// the graph holds, on the same argument that bounds the offset sidecar
// below. Unbounded, an int64's far end is a year near 294247, and that
// is not a date any wire format beside this one spells: RFC 3339 has
// four year digits, and so does every timestamp text a SQL client
// prints. A caller who formats such an instant gets a string no other
// system reads back. The bound is the widest four-digit range rather
// than anything narrower, so every instant this encoding was designed
// to carry — pre-epoch dates included — survives the read.
func agtypeInstant(raw []byte) (time.Time, error) {
	micros, err := agtypeInt64(raw)
	if err != nil {
		return time.Time{}, err
	}
	// Year 1 midnight UTC and the last microsecond of year 9999, as
	// counts. Written as literals because they are the encoding, and a
	// derivation from time.Date would put the bound behind the same
	// calendar arithmetic it is checking.
	if micros < -62135596800000000 || micros > 253402300799999999 {
		return time.Time{}, fmt.Errorf(
			"gqlc: %d microseconds since the epoch is outside the year 1 to year 9999 range this encoding admits", micros)
	}
	return time.UnixMicro(micros).UTC(), nil
}
`)
	}
	if h.date || h.dateText {
		b.WriteString(`
// agtypeDateLayout is the stored form of a DATE, in the reference time
// every Go layout is written in. Zero-padded and four-digit: both halves
// are load-bearing, because they are the whole of why the stored strings
// sort chronologically.
const agtypeDateLayout = "2006-01-02"
`)
	}
	if h.date {
		b.WriteString(`
// agtypeDate decodes a stored DATE. agtype has no temporal value, so
// gqlc stores one as the string scalar in zero-padded ISO YYYY-MM-DD:
// the one temporal spelling whose lexical order is its chronological
// order, so an author's ORDER BY n.dob is answered by the database's own
// string comparison with nothing for gqlc to rewrite.
//
// That ordering holds across [0001-01-01, 9999-12-31] and nowhere else,
// which is why the padding and the bound are read back here rather than
// assumed. A year needing a fifth digit sorts under every four-digit year
// because '1' < '2', and a proleptic year before 1 CE needs a sign, which
// sorts under every digit and files the whole era at the front,
// ascending. gqlc's own encode produces neither — it refuses them — but
// it writes to an ordinary agtype string property on a vertex any writer
// can touch, so what is read back is whatever the graph holds.
//
// The padding is enforced by the layout and not by a check beside it.
// time.Parse reads each field at a fixed width, so '2024-1-2' fails at
// the month, '24-01-02' at the year, and a signed or five-digit year at
// the first byte that is not a digit where one belongs — every spelling
// that would sort in the wrong place is refused before a Date is built.
//
// That leaves the range bounded at one end only, which is measured
// rather than an oversight. A year over 9999 needs a fifth digit and a
// year before 1 CE needs a sign, and the layout refuses both; year 0 is
// the one out-of-range year spelled with four unsigned digits, so it is
// the only one that reaches here, and it is what this bound is for.
func agtypeDate(raw []byte) (Date, error) {
	text, err := agtypeString(raw)
	if err != nil {
		return Date{}, err
	}
	at, err := time.Parse(agtypeDateLayout, text)
	if err != nil {
		return Date{}, fmt.Errorf("gqlc: %q is not a date in zero-padded ISO YYYY-MM-DD form: %w", text, err)
	}
	if at.Year() < 1 {
		return Date{}, fmt.Errorf("gqlc: %q is outside the year 1 to year 9999 range this encoding admits", text)
	}
	return Date{Year: at.Year(), Month: int(at.Month()), Day: at.Day()}, nil
}
`)
	}
	if h.localTime {
		b.WriteString(`
// agtypeLocalTime decodes a stored LOCAL TIME: the integer scalar,
// counting microseconds since midnight. The same argument as the
// instant, one width down — the count is non-negative and fixed-range,
// so agtype's integer ordering is chronological order within the day and
// an author's ORDER BY needs no rewriting.
//
// The count is bounded to [0, 86400000000) before the reading is built,
// because a count outside it names no clock reading at all and gqlc's
// encode is not the only writer of the property. Sub-microsecond
// precision is not in the count: it truncated at encode, which is this
// backend's one policy for every temporal it stores.
func agtypeLocalTime(raw []byte) (LocalTime, error) {
	micros, err := agtypeInt64(raw)
	if err != nil {
		return LocalTime{}, err
	}
	if micros < 0 || micros >= 86400000000 {
		return LocalTime{}, fmt.Errorf(
			"gqlc: %d microseconds since midnight is outside the [0, 86400000000) interval a clock reading occupies", micros)
	}
	return LocalTime{
		Hour:       int(micros / 3600000000),
		Minute:     int(micros / 60000000 % 60),
		Second:     int(micros / 1000000 % 60),
		Nanosecond: int(micros % 1000000 * 1000),
	}, nil
}
`)
	}
	if h.zonedTime {
		b.WriteString(`
// agtypeTime decodes a stored TIME: the integer scalar, counting
// microseconds since midnight UTC-NORMALISED — the reading the writer saw
// minus the offset it was read at, wrapped into the day. Normalising is
// what makes the count comparable across offsets, so agtype's integer
// ordering is instant order and an author's ORDER BY needs no rewriting;
// a raw local reading would sort 23:00+02:00 after 22:30+00:00 when the
// first is the earlier instant.
//
// What comes back here is therefore the UTC reading, with OffsetSeconds
// left zero. Where the value is an entity property, agtypeTimeZone reads
// the sidecar beside it and puts the reading back in its own zone; where
// it is a projected column there is no sidecar to read, and UTC is the
// whole of what the count carries.
//
// The count is bounded to [0, 86400000000) before the reading is built,
// because a count outside it names no clock reading at all and gqlc's
// encode is not the only writer of the property. Sub-microsecond
// precision is not in the count: it truncated at encode, which is this
// backend's one policy for every temporal it stores.
func agtypeTime(raw []byte) (Time, error) {
	micros, err := agtypeInt64(raw)
	if err != nil {
		return Time{}, err
	}
	if micros < 0 || micros >= 86400000000 {
		return Time{}, fmt.Errorf(
			"gqlc: %d microseconds since midnight is outside the [0, 86400000000) interval a clock reading occupies", micros)
	}
	return agtypeTimeAt(micros, 0), nil
}

// agtypeTimeAt builds a clock reading from a microsecond count already
// known to be in [0, 86400000000), recording the offset it is a reading
// at. The component arithmetic is here rather than at its two call sites
// so decode and re-zoning cannot drift apart in it.
func agtypeTimeAt(micros int64, offset int) Time {
	return Time{
		Hour:          int(micros / 3600000000),
		Minute:        int(micros / 60000000 % 60),
		Second:        int(micros / 1000000 % 60),
		Nanosecond:    int(micros % 1000000 * 1000),
		OffsetSeconds: offset,
	}
}

`)
	}
	if h.wrapDay {
		b.WriteString(`
// agtypeWrapDay reduces a microsecond count into the [0, 86400000000)
// interval a clock reading occupies, which shifting a reading by an
// offset can leave it outside in either direction. Both directions of the
// TIME encoding go through it: encode subtracts the offset, decode adds
// it back.
//
// The explicit correction is load-bearing. Go's % follows the sign of its
// left operand, so -1 % 86400000000 is -1 and not the 86399999999 the
// day-before reading actually is: taking the remainder alone leaves a
// negative count that then builds a reading with negative components, or
// stores one no decode will accept.
func agtypeWrapDay(micros int64) int64 {
	micros %= 86400000000
	if micros < 0 {
		micros += 86400000000
	}
	return micros
}
`)
	}
	if h.timeZone {
		b.WriteString(`
// agtypeTimeZone puts a decoded TIME back in the zone it was written in,
// reading the offset-seconds sidecar stored beside it. Where the instant
// re-zones by moving a Location and leaving the moment alone, this one
// rebuilds the clock reading: the stored count is UTC-normalised, so the
// reading the writer saw is that count plus the offset, wrapped back into
// the day.
//
// An absent sidecar leaves the UTC reading as it is, which is the same
// instant with OffsetSeconds zero.
func agtypeTimeZone(props map[string][]byte, key string, t Time) (Time, error) {
	offset, ok, err := agtypeOffset(props, key)
	if err != nil {
		return Time{}, err
	}
	if !ok {
		return t, nil
	}
	micros := int64(t.Hour)*3600000000 + int64(t.Minute)*60000000 +
		int64(t.Second)*1000000 + int64(t.Nanosecond)/1000
	return agtypeTimeAt(agtypeWrapDay(micros+int64(offset)*1000000), offset), nil
}
`)
	}
	if h.duration {
		b.WriteString(`
// agtypeDuration decodes a stored DURATION: the integer scalar, counting
// total microseconds. Signed, so a negative count is a duration
// backwards and orders below every positive one.
//
// What comes back is normalised, and cannot be otherwise: the stored
// count is one number, so the Months and Days a Duration can hold have
// nothing to be filled from and stay zero. A value written as 90 days
// reads back as 7776000 seconds. Months is zero rather than derived
// because no fixed count of microseconds is faithful to a month — which
// is the same reason the encode direction refuses a Duration carrying
// one instead of storing an approximation of it.
//
// The division floors rather than truncating, so Nanos is never
// negative and a duration backwards borrows from Seconds: -1500000
// microseconds is Seconds -2 and Nanos 500000000, not Seconds -1 and
// Nanos -500000000. Go's / and % truncate toward zero and would
// produce the second, which no neo4j value ever takes — the driver
// renders a Duration by borrowing from a negative Seconds against a
// positive Nanos, so a negative Nanos is a shape its own String method
// has no reading for. The two are the same amount of time either way;
// what floor division buys is that the components a caller reads off
// this carrier do not change meaning when the target does.
func agtypeDuration(raw []byte) (Duration, error) {
	micros, err := agtypeInt64(raw)
	if err != nil {
		return Duration{}, err
	}
	seconds := micros / 1000000
	rem := micros % 1000000
	if rem < 0 {
		seconds--
		rem += 1000000
	}
	return Duration{Seconds: seconds, Nanos: int(rem * 1000)}, nil
}
`)
	}
	if h.offset {
		b.WriteString(`
// agtypeOffset reads the offset-seconds sidecar stored beside a zoned
// value, with ok=false where the graph holds no such property. An absent
// sidecar is not an error: the stored count is complete without it, so
// the property stays readable by anything that never wrote one —
// including this package, which binds the value alone. Where a sidecar
// goes is the author's query text, and gqlc runs that text as written.
//
// Flat rather than a member of a map so the value stays the property
// itself: ORDER BY n.at and WHERE n.at > $since are then answered by
// agtype's integer ordering, with nothing for gqlc to rewrite.
//
// The offset is bounded at a day either way, exclusive, before it is
// taken. gqlc derives no sidecar to bind — a parameter crosses as the
// value alone — so the integer read here is whatever the graph holds: a
// query that names the key binds it like any other property, whether or
// not gqlc generated that query. Unbounded it names a zone no clock
// keeps, and the wall clock the caller then reads is arbitrarily far
// from the value stored beside it. The bound is a day rather than the
// narrower range zone databases populate today, so a graph a future zone
// database would accept is not refused here.
//
// Both zoned widths read their sidecar through this, so the bound is one
// policy and not two agreeing by inspection.
func agtypeOffset(props map[string][]byte, key string) (int, bool, error) {
	raw, ok := props[key]
	if !ok {
		return 0, false, nil
	}
	offset, err := agtypeInt64(raw)
	if err != nil {
		return 0, false, fmt.Errorf("gqlc: offset %q: %w", key, err)
	}
	if offset <= -86400 || offset >= 86400 {
		return 0, false, fmt.Errorf("gqlc: offset %q is %d seconds, which is not within a day of UTC", key, offset)
	}
	return int(offset), true, nil
}
`)
	}
	if h.zone {
		b.WriteString(`
// agtypeZone puts a decoded instant back in the zone it was written in.
// An absent sidecar leaves it in UTC, which is the instant it was stored
// at either way: the count is absolute, so the zone moves the wall clock
// the caller reads and never the moment it names.
func agtypeZone(props map[string][]byte, key string, at time.Time) (time.Time, error) {
	offset, ok, err := agtypeOffset(props, key)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return at, nil
	}
	return at.In(time.FixedZone("", offset)), nil
}
`)
	}
	if h.micros {
		b.WriteString(`
// agtypeMicros encodes an instant into the integer a query binds it as,
// the same count agtypeInstant reads back. The parameter crosses as the
// bare integer rather than as a structure holding the offset too, which
// is what lets a comparison an author writes against the property be
// answered by agtype's own integer ordering.
//
// The zone does not cross with it. A value stored through a parameter
// therefore reads back in UTC, at the same instant it was written; a
// graph that wants the original zone back carries it in the sidecar
// agtypeZone reads, which is a property the query text has to name.
func agtypeMicros(at time.Time) int64 {
	return at.UnixMicro()
}
`)
	}
	if h.nullMicros {
		b.WriteString(`
// agtypeNullableMicros is agtypeMicros over an absent instant, which
// crosses as the agtype null a nullable property holds.
func agtypeNullableMicros(at *time.Time) *int64 {
	if at == nil {
		return nil
	}
	micros := at.UnixMicro()
	return &micros
}
`)
	}
	if h.dateText {
		b.WriteString(`
// agtypeDateText encodes a DATE into the string a query binds it as, the
// same text agtypeDate reads back.
//
// It fails rather than encodes twice over. A year outside
// [0001, 9999] has no zero-padded four-digit spelling, and the string it
// would get instead sorts in the wrong place — which is the one property
// the whole encoding rests on, so storing it would put a value in the
// column that silently breaks every ORDER BY over it. A Year, Month and
// Day naming no day on the calendar fails for the narrower reason that
// agtypeDate would refuse to read it back: time.Date rolls February 30th
// forward to March 2nd, and a round trip that returns a different date
// than it stored is not one.
func agtypeDateText(d Date) (string, error) {
	if d.Year < 1 || d.Year > 9999 {
		return "", fmt.Errorf(
			"gqlc: year %d is outside the year 1 to year 9999 range this encoding admits", d.Year)
	}
	at := time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC)
	if (Date{Year: at.Year(), Month: int(at.Month()), Day: at.Day()}) != d {
		return "", fmt.Errorf(
			"gqlc: year %d month %d day %d is not a date on the calendar", d.Year, d.Month, d.Day)
	}
	return at.Format(agtypeDateLayout), nil
}
`)
	}
	if h.localTimeMicros {
		b.WriteString(`
// agtypeLocalTimeMicros encodes a LOCAL TIME into the integer a query
// binds it as, the same count agtypeLocalTime reads back.
//
// Each component is bounded on its own rather than the sum being bounded
// once. The two are not the same check: an Hour of -1 beside a Minute of
// 60 sums into the interval and would encode as midnight, which is a
// reading the caller never wrote. Bounding the components refuses it and
// makes the [0, 86400000000) interval the decode enforces a consequence
// rather than a second rule.
//
// Sub-microsecond precision truncates, which is this backend's policy for
// every temporal it stores and not a decision taken here.
func agtypeLocalTimeMicros(t LocalTime) (int64, error) {
	if t.Hour < 0 || t.Hour > 23 || t.Minute < 0 || t.Minute > 59 ||
		t.Second < 0 || t.Second > 59 || t.Nanosecond < 0 || t.Nanosecond > 999999999 {
		return 0, fmt.Errorf(
			"gqlc: hour %d minute %d second %d nanosecond %d is not a clock reading",
			t.Hour, t.Minute, t.Second, t.Nanosecond)
	}
	return int64(t.Hour)*3600000000 + int64(t.Minute)*60000000 +
		int64(t.Second)*1000000 + int64(t.Nanosecond)/1000, nil
}
`)
	}
	if h.timeMicros {
		b.WriteString(`
// agtypeTimeMicros encodes a TIME into the integer a query binds it as,
// the same count agtypeTime reads back: the clock reading UTC-NORMALISED,
// which is the reading minus its offset wrapped into the day.
//
// Normalising here is what the stored ordering rests on. Two readings at
// different offsets compare as instants because both counts are on one
// scale; storing the local reading instead would sort 23:00+02:00 after
// 22:30+00:00 when it is the earlier instant by half an hour.
//
// The offset does not cross beside it. gqlc adds no parameter to the
// author's query (ADR 0005), so a value bound here and read back through
// this package returns at the same instant with OffsetSeconds zero — the
// original zone comes back only from a sidecar the author's own query
// text writes. That is the instant's arrangement exactly, one width down.
//
// Each component is bounded on its own, and the offset with the bound
// agtypeOffset applies on the way in, so what is stored is a count some
// clock somewhere actually read. Sub-microsecond precision truncates,
// this backend's policy for every temporal it stores.
func agtypeTimeMicros(t Time) (int64, error) {
	if t.Hour < 0 || t.Hour > 23 || t.Minute < 0 || t.Minute > 59 ||
		t.Second < 0 || t.Second > 59 || t.Nanosecond < 0 || t.Nanosecond > 999999999 {
		return 0, fmt.Errorf(
			"gqlc: hour %d minute %d second %d nanosecond %d is not a clock reading",
			t.Hour, t.Minute, t.Second, t.Nanosecond)
	}
	if t.OffsetSeconds <= -86400 || t.OffsetSeconds >= 86400 {
		return 0, fmt.Errorf(
			"gqlc: offset %d seconds is not within a day of UTC", t.OffsetSeconds)
	}
	local := int64(t.Hour)*3600000000 + int64(t.Minute)*60000000 +
		int64(t.Second)*1000000 + int64(t.Nanosecond)/1000
	return agtypeWrapDay(local - int64(t.OffsetSeconds)*1000000), nil
}
`)
	}
	if h.durationMicros {
		b.WriteString(`
// agtypeDurationMicros encodes a DURATION into the integer a query binds
// it as, the same count agtypeDuration reads back.
//
// A Duration carrying months is refused rather than approximated. A month
// is not a fixed count of microseconds — it is 28, 29, 30 or 31 days
// depending on where it lands — so any count this could return would be
// one the caller did not write, and a comparison against it would be
// answered wrongly rather than refused. ADR 0002 collapsed the
// (YEAR TO MONTH) and (DAY TO SECOND) qualifiers onto one carrier, so
// which of the two a value holds is not knowable until it is in hand:
// this is a run-time refusal because there is no generate-time question
// to ask.
//
// Days are not months. A day is fixed at 86400 seconds in this encoding,
// which carries no zone and so has no daylight-saving transition to fall
// on, and it folds into the count.
//
// Sub-microsecond precision truncates, as everywhere else here.
func agtypeDurationMicros(d Duration) (int64, error) {
	if d.Months != 0 {
		return 0, fmt.Errorf(
			"gqlc: a duration of %d months has no faithful encoding here, because a month is not a fixed count of microseconds", d.Months)
	}
	return (d.Days*86400+d.Seconds)*1000000 + int64(d.Nanos)/1000, nil
}
`)
	}
	if h.encNullable {
		b.WriteString(`
// agtypeEncodedNullable runs a fallible encoder over an absent value,
// which crosses as the agtype null a nullable parameter holds.
func agtypeEncodedNullable[T, E any](in *T, encode func(T) (E, error)) (*E, error) {
	if in == nil {
		return nil, nil
	}
	out, err := encode(*in)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
`)
	}
	if h.encList {
		b.WriteString(`
// agtypeEncodedList runs a fallible encoder over every element of a bound
// list. The index is named on failure: the elements share one parameter
// name, so it is the only thing that says which value was refused.
func agtypeEncodedList[T, E any](in []T, encode func(T) (E, error)) ([]E, error) {
	out := make([]E, len(in))
	for i, v := range in {
		encoded, err := encode(v)
		if err != nil {
			return nil, fmt.Errorf("gqlc: element %d: %w", i, err)
		}
		out[i] = encoded
	}
	return out, nil
}
`)
	}
	b.WriteString(`
// agtypeSpan reports where the value at the front of b ends: the offset
// of the first stop byte outside any nested structure, or len(b) when
// there is none. A string, a nested map and a nested list are each
// stepped over whole, so a delimiter one of them contains ends nothing.
func agtypeSpan(b []byte, stop byte) (int, error) {
	depth := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '"' {
			for i++; i < len(b) && b[i] != '"'; i++ {
				if b[i] == '\\' {
					i++
				}
			}
			if i >= len(b) {
				return 0, fmt.Errorf("gqlc: %q leaves a string unterminated", b)
			}
			continue
		}
		if depth == 0 && b[i] == stop {
			return i, nil
		}
		switch b[i] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth < 0 {
				return 0, fmt.Errorf("gqlc: %q closes a structure it never opened", b)
			}
		}
	}
	if depth != 0 {
		return 0, fmt.Errorf("gqlc: %q leaves a structure open", b)
	}
	return len(b), nil
}
`)
	b.WriteString(`
// agtypeObject splits an agtype map into its members, each key holding
// the undecoded text of its value. A map carries more than the schema
// declares: AGE stores whatever a writer wrote, so a value here may be of
// a shape no helper in this package reads, and only the ones a field asks
// for are ever decoded.
func agtypeObject(raw []byte) (map[string][]byte, error) {
	body := bytes.TrimSpace(raw)
	if len(body) < 2 || body[0] != '{' || body[len(body)-1] != '}' {
		return nil, fmt.Errorf("gqlc: %q is not an agtype map", raw)
	}
	body = bytes.TrimSpace(body[1 : len(body)-1])
	out := make(map[string][]byte)
	for len(body) > 0 {
		end, err := agtypeSpan(body, ',')
		if err != nil {
			return nil, err
		}
		member := bytes.TrimSpace(body[:end])
		body = bytes.TrimSpace(body[min(end+1, len(body)):])

		at, err := agtypeSpan(member, ':')
		if err != nil {
			return nil, err
		}
		if at == len(member) {
			return nil, fmt.Errorf("gqlc: %q is not a key and a value", member)
		}
		key, err := agtypeString(bytes.TrimSpace(member[:at]))
		if err != nil {
			return nil, err
		}
		out[key] = bytes.TrimSpace(member[at+1:])
	}
	return out, nil
}
`)
	b.WriteString(`
// agtypeEntity splits an agtype vertex or edge into the label it carries
// and the undecoded text of each of its properties. A vertex and an edge
// are the same object but for the annotation, so requiring the one the
// caller named is what stands between an edge's decoder and a vertex
// whose label happens to match it.
func agtypeEntity(raw []byte, annotation string) (string, map[string][]byte, error) {
	body, ok := bytes.CutSuffix(bytes.TrimSpace(raw), []byte(annotation))
	if !ok {
		return "", nil, fmt.Errorf("gqlc: %q does not carry the %s annotation", raw, annotation)
	}
	fields, err := agtypeObject(body)
	if err != nil {
		return "", nil, err
	}
	rawLabel, ok := fields["label"]
	if !ok {
		return "", nil, fmt.Errorf("gqlc: %q carries no label", raw)
	}
	label, err := agtypeString(rawLabel)
	if err != nil {
		return "", nil, err
	}
	rawProps, ok := fields["properties"]
	if !ok {
		return "", nil, fmt.Errorf("gqlc: %q carries no properties", raw)
	}
	props, err := agtypeObject(rawProps)
	if err != nil {
		return "", nil, err
	}
	return label, props, nil
}
`)
	if h.list {
		b.WriteString(`
// agtypeList decodes an agtype list, reading each element through the
// decoder the caller supplies. The split steps over a nested string, map
// or list whole, so a comma inside one separates nothing; an element the
// decoder refuses fails the whole list rather than being dropped or
// zeroed, because a slice one element short is not the value the graph
// holds.
//
// The empty list decodes to a slice of length zero and not to nil: AGE
// stores it as a value, and a nil slice is what an absent property
// reaches the caller as.
func agtypeList[T any](raw []byte, decode func([]byte) (T, error)) ([]T, error) {
	body := bytes.TrimSpace(raw)
	if len(body) < 2 || body[0] != '[' || body[len(body)-1] != ']' {
		return nil, fmt.Errorf("gqlc: %q is not an agtype list", raw)
	}
	body = bytes.TrimSpace(body[1 : len(body)-1])
	out := make([]T, 0)
	for len(body) > 0 {
		end, err := agtypeSpan(body, ',')
		if err != nil {
			return nil, err
		}
		elem := bytes.TrimSpace(body[:end])
		body = bytes.TrimSpace(body[min(end+1, len(body)):])
		value, err := decode(elem)
		if err != nil {
			return nil, fmt.Errorf("gqlc: element %d of %q: %w", len(out), raw, err)
		}
		out = append(out, value)
	}
	return out, nil
}
`)
	}
	for _, goType := range h.listHelpers() {
		writeListHelper(&b, goType)
	}
	if h.value {
		b.WriteString(`
// agtypeValue decodes a value of no declared shape through agtype's own
// vocabulary: its string, integer, float, boolean, list and map land on
// Go's string, int64, float64, bool, []any and map[string]any, and the
// null it carries inline lands on nil. The first byte chooses the arm,
// which is enough because agtype's structured values are self-delimiting
// and its scalars share no opening byte.
//
// Text outside that vocabulary is refused rather than carried through as
// a string: a value of unknown shape is still a value, and reading
// something that is not one as a value would put a fabricated Go value
// in a caller's hands.
func agtypeValue(raw []byte) (any, error) {
	body := bytes.TrimSpace(raw)
	if len(body) == 0 {
		return nil, fmt.Errorf("gqlc: %q is not an agtype value", raw)
	}
	switch body[0] {
	case '"':
		out, err := agtypeString(body)
		if err != nil {
			return nil, err
		}
		return out, nil
	case '[':
		out, err := agtypeList(body, agtypeValue)
		if err != nil {
			return nil, err
		}
		return out, nil
	case '{':
		out, err := agtypeMap(body)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	switch string(body) {
	case "null":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	// An integer is tried first and a float second, so a value AGE wrote
	// without a fractional part keeps the width agtype held it at. A value
	// outside int64's range is one AGE evaluated as a float, and reaches
	// the caller as the float it is.
	if out, err := agtypeInt64(body); err == nil {
		return out, nil
	}
	out, err := agtypeFloat64(body)
	if err != nil {
		return nil, fmt.Errorf("gqlc: %q is not an agtype value", raw)
	}
	return out, nil
}

// agtypeMap decodes an agtype map whose members are of no declared
// shape, each read through agtypeValue.
func agtypeMap(raw []byte) (map[string]any, error) {
	members, err := agtypeObject(raw)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(members))
	for key, member := range members {
		value, err := agtypeValue(member)
		if err != nil {
			return nil, fmt.Errorf("gqlc: member %q: %w", key, err)
		}
		out[key] = value
	}
	return out, nil
}
`)
	}
	if h.prop {
		b.WriteString(`
// agtypeProperty reads one property the schema declares NOT NULL out of a
// split entity. AGE drops a property whose value is null, so an absent
// key is how a null arrives, and taking the Go zero for one would report
// absence as a value the graph holds.
func agtypeProperty[T any](props map[string][]byte, key string, decode func([]byte) (T, error)) (T, error) {
	raw, ok := props[key]
	if !ok {
		var zero T
		return zero, fmt.Errorf("gqlc: property %q is absent", key)
	}
	return decode(raw)
}
`)
	}
	if h.nullProp {
		b.WriteString(`
// agtypeNullableProperty reads one nullable property out of a split
// entity, where the absence agtypeProperty refuses is the schema's null
// and reaches the caller as the nil pointer.
func agtypeNullableProperty[T any](props map[string][]byte, key string, decode func([]byte) (T, error)) (*T, error) {
	raw, ok := props[key]
	if !ok {
		return nil, nil
	}
	out, err := decode(raw)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
`)
	}
	return []byte(b.String())
}

// writeListHelper emits the named wrapper for one Go slice type: the
// generic walk with this type's element decoder bound in. A named
// wrapper rather than the generic at each call site because a nested
// list's element decoder is the wrapper one level in, and a function
// value is what agtypeList takes.
func writeListHelper(b *strings.Builder, goType string) {
	elem := strings.TrimPrefix(goType, "[]")
	fmt.Fprintf(b, "\n// %s decodes an agtype list of %s elements.\n", listHelperName(goType), elem)
	fmt.Fprintf(b, "func %s(raw []byte) (%s, error) {\n", listHelperName(goType), goType)
	fmt.Fprintf(b, "\treturn agtypeList(raw, %s)\n}\n", decodeFunc(elem))
}

// writeEntities emits the schema's entity surface: one exported struct
// per node and edge type in Phase Z order, followed by the decoder that
// fills it.
//
// No sealed edge-union interface comes out here, and no marker method.
// Those belong to a column binding more than one candidate edge type,
// which this backend refuses ahead of Prepare (edgeUnionReason), so a
// batch reaching emission carries none: a prepared query's EdgeUnions
// is filled only from a ResolvedEdgeUnion column or from a list with
// one at its leaf, and unservedColumn refuses both.
func writeEntities(b *strings.Builder, entities []wiredEntity) {
	for _, e := range entities {
		b.WriteString("\n")
		writeEntityStruct(b, e.Entity)
		b.WriteString("\n")
		writeEntityDecoder(b, e)
	}
}

// writeEntityStruct emits one entity's exported struct declaration. A
// nullable property is a pointer field, which is what carries the
// difference between a property the graph holds and one it does not.
func writeEntityStruct(b *strings.Builder, e codegen.Entity) {
	if e.Kind == codegen.EntityNode {
		fmt.Fprintf(b, "// %s corresponds to the %s node type.\n", e.Name, e.DocAxis)
	} else {
		fmt.Fprintf(b, "// %s corresponds to the %s.\n", e.Name, e.DocAxis)
	}
	fmt.Fprintf(b, "type %s struct {\n", e.Name)
	for _, f := range e.Fields {
		b.WriteString("\t" + f.Field + " ")
		if f.Nullable {
			b.WriteString("*")
		}
		b.WriteString(f.GoType + "\n")
	}
	b.WriteString("}\n")
}

// writeEntityDecoder emits one entity's decode<Name> helper. It takes the
// undecoded column text a query method scanned, and holds the label check
// every call site goes through.
func writeEntityDecoder(b *strings.Builder, e wiredEntity) {
	// A zero-property entity reads nothing out of the map, and an
	// unused local does not compile.
	props := "props"
	if len(e.Fields) == 0 {
		props = "_"
	}
	fmt.Fprintf(b, "// decode%s decodes an agtype %s into a %s struct, enforcing\n", e.Name, strings.TrimPrefix(e.annotation, "::"), e.Name)
	b.WriteString("// the label and the per-property nullability the schema declares.\n")
	fmt.Fprintf(b, "func decode%s(raw []byte) (%s, error) {\n", e.Name, e.Name)
	fmt.Fprintf(b, "\tlabel, %s, err := agtypeEntity(raw, %q)\n", props, e.annotation)
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s{}, fmt.Errorf(%q, err)\n\t}\n", e.Name, "decode "+e.Name+": %w")
	fmt.Fprintf(b, "\tif label != %q {\n", e.label)
	fmt.Fprintf(b, "\t\treturn %s{}, fmt.Errorf(%q, %q, label)\n\t}\n",
		e.Name, "decode "+e.Name+": expected label %q, got %q", e.label)
	fmt.Fprintf(b, "\tvar out %s\n", e.Name)
	for i, f := range e.Fields {
		writeEntityFieldDecode(b, e.Entity, i, f)
	}
	b.WriteString("\treturn out, nil\n}\n")
}

// writeEntityFieldDecode emits the read of the property at index i. A
// width narrower than the agtype scalar it rides in is produced by the
// decoder itself rather than converted here, so the struct carries the
// width the schema declared and a value that width cannot hold fails the
// read instead of wrapping (ADR 0037).
func writeEntityFieldDecode(b *strings.Builder, e codegen.Entity, i int, f codegen.EntityField) {
	value := valueName(i)
	reader := "agtypeProperty"
	if f.Nullable {
		reader = "agtypeNullableProperty"
	}
	fmt.Fprintf(b, "\t%s, err := %s(props, %q, %s)\n", value, reader, f.PropName, decodeFunc(f.GoType))
	fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s{}, fmt.Errorf(%q, err)\n\t}\n",
		e.Name, "decode "+e.Name+"."+f.Field+": %w")
	if sidecar, ok := offsetSidecar(f); ok {
		// Both zoned carriers read the same property; which helper reads
		// it is the carrier's, since an instant re-zones by moving its
		// Location and a TIME by rebuilding its clock reading.
		zoner := "agtypeZone"
		if f.GoType == goTime {
			zoner = "agtypeTimeZone"
		}
		writeZoning(b, e, i, f, sidecar, zoner)
		return
	}
	fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, value)
}

// writeZoning emits the read of one zoned property's offset sidecar and
// the assignment of the zoned value. It runs only inside an entity
// decoder: the sidecar is a second property of the same vertex, which a
// projection of the value alone does not carry.
//
// sidecar is offsetSidecar's answer for f: the key this reads is the key
// the collision gate refuses. zoner is the helper that takes the offset,
// which differs by carrier while the shape of the call does not — both
// take (props, key, value) and answer (value, error), so the nullable
// composition below is written once.
func writeZoning(b *strings.Builder, e codegen.Entity, i int, f codegen.EntityField, sidecar, zoner string) {
	value := valueName(i)
	fail := fmt.Sprintf("\t\treturn %s{}, fmt.Errorf(%q, err)\n", e.Name, "decode "+e.Name+"."+f.Field+": %w")
	if !f.Nullable {
		fmt.Fprintf(b, "\tout.%s, err = %s(props, %q, %s)\n", f.Field, zoner, sidecar, value)
		fmt.Fprintf(b, "\tif err != nil {\n%s\t}\n", fail)
		return
	}
	fmt.Fprintf(b, "\tif %s != nil {\n", value)
	fmt.Fprintf(b, "\t\tzoned, err := %s(props, %q, *%s)\n", zoner, sidecar, value)
	fmt.Fprintf(b, "\t\tif err != nil {\n\t%s\t\t}\n", fail)
	fmt.Fprintf(b, "\t\t%s = &zoned\n\t}\n", value)
	fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, value)
}

// wiredEntity is an entity together with the form it takes on the wire:
// the single label AGE stamps on every vertex and edge, and the
// annotation that says which of the two it is. The two fields are
// unexported and wireEntity is the only thing that fills them, so a
// populated value has been past the single-label check; the zero value
// carries no label to check against and belongs to no entity.
type wiredEntity struct {
	codegen.Entity

	label      string
	annotation string
}

// wireEntity is an entity's wire form, with ok=false for a node or edge
// type declared under any number of labels but one.
func wireEntity(e codegen.Entity) (wiredEntity, bool) {
	labels, annotation := e.EdgeKey.KeyLabels, edgeAnnotation
	if e.Kind == codegen.EntityNode {
		labels, annotation = e.Labels, vertexAnnotation
	}
	label, ok := single(labels)
	if !ok {
		return wiredEntity{}, false
	}
	return wiredEntity{Entity: e, label: label, annotation: annotation}, true
}

// single is the one label a key label set holds, with ok=false for any
// other number.
func single(k graph.LabelSetKey) (string, bool) {
	parts := k.Split()
	if len(parts) != 1 {
		return "", false
	}
	return parts[0], true
}
