package neo4j

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// temporalUse records which conversion directions one neutral carrier
// needs in a given batch. Emitting only the used ones keeps dead
// unexported functions out of the generated package.
type temporalUse struct {
	decode    bool // to<X>: a driver value read into the neutral carrier
	encode    bool // from<X>: a carrier bound as a non-nullable parameter
	encodePtr bool // from<X>Ptr: a carrier bound as a nullable parameter
	// list records that the carrier is bound as a list parameter. A bool
	// rather than a depth: a parameter's Go type is its property's, and
	// ADR 0035 refuses a nested DECLARED list as a stored property, so a
	// list whose leaf is a temporal carrier is depth 1 (ruled on bd
	// gqlc-a2g2v). Two slice prefixes are not themselves impossible —
	// LIST<BYTES> emits [][]byte, nesting inside the element rather than
	// in the declared list — but such a leaf is not a carrier and marks
	// nothing here.
	list bool
	// listPtr records that a list parameter of this carrier is ALSO bound
	// nullable somewhere in the batch, which owes the from<X>ListPtr
	// wrapper beside the plain helper.
	listPtr bool
}

// temporalUses walks the prepared batch and answers, per carrier, which
// directions the emission sites will reach for. Decode positions are
// entity properties and row columns (models.go and the per-source
// files); encode positions are query parameters. A parameter is the one
// position whose nullability changes the helper, because a nil pointer
// has to become a Cypher null rather than a zero-valued carrier.
func temporalUses(prepared codegen.Prepared) map[string]temporalUse {
	uses := make(map[string]temporalUse)
	mark := func(goType string, set func(*temporalUse)) {
		name := leafType(goType)
		if !isTemporalCarrier(name) {
			return
		}
		use := uses[name]
		set(&use)
		uses[name] = use
	}
	for _, e := range prepared.Entities {
		for _, f := range e.Fields {
			mark(f.GoType, func(u *temporalUse) { u.decode = true })
		}
	}
	for _, q := range prepared.Queries {
		for _, f := range q.RowFields {
			mark(f.GoType, func(u *temporalUse) { u.decode = true })
			for elem := f.ListElem; elem != nil; elem = elem.Nested {
				mark(elem.GoType, func(u *temporalUse) { u.decode = true })
			}
		}
		for _, f := range q.ParamFields {
			// A list parameter converts per element, so what it needs is
			// the plain from<X> at the leaf plus the list helper that
			// calls it — never from<X>Ptr, whose nil-to-Cypher-null job
			// belongs to the list helper instead.
			if isSliceType(f.GoType) {
				mark(f.GoType, func(u *temporalUse) {
					u.encode = true
					u.list = true
					u.listPtr = u.listPtr || f.Nullable
				})
				continue
			}
			if f.Nullable {
				mark(f.GoType, func(u *temporalUse) { u.encodePtr = true })
				continue
			}
			mark(f.GoType, func(u *temporalUse) { u.encode = true })
		}
	}
	return uses
}

// isTemporalCarrier reports whether a Go type text is exactly one of the
// neutral carrier names. Exact, never a prefix or substring test: "Date"
// is inside "LocalDateTime" and inside entity names a schema chose.
func isTemporalCarrier(goType string) bool {
	for _, name := range codegen.TemporalCarriers {
		if goType == name {
			return true
		}
	}
	return false
}

// leafType strips the slice prefixes off an emitted Go type text,
// yielding the element type the decode sites narrow one at a time.
func leafType(goType string) string {
	for {
		elem := strings.TrimPrefix(goType, "[]")
		if elem == goType {
			return goType
		}
		goType = elem
	}
}

// temporalListHelper names the from<X>List helper for one carrier.
func temporalListHelper(leaf string) string {
	return "from" + leaf + "List"
}

// narrowExpr renders the expression that turns a value of the driver
// carrier into the emitted Go type, given the local the driver value
// landed in. The neutral temporal carriers are not conversion-compatible
// with their dbtype counterparts — they are different shapes, not
// different spellings — so they route through the emitted to<X> helper.
//
// Callers reach here only when driverCarrier(goType) != goType; a type
// that is its own carrier is assigned bare at the call site.
//
// EVERY REMAINING CALLER IS TEMPORAL. The numeric widths the driver
// over-carries (int64 → int8, float64 → float32) used to narrow here by
// a bare Go conversion, which wraps silently on a value the declared
// width cannot hold; they now go through narrowCall below, which fails
// the decode instead (ADR 0037, bd gqlc-awtb).
func narrowExpr(goType, src string) string {
	if isTemporalCarrier(goType) {
		return fmt.Sprintf("to%s(%s)", goType, src)
	}
	return fmt.Sprintf("%s(%s)", goType, src)
}

// narrowCall renders the CHECKED narrowing of a driver carrier down to a
// numeric width the schema declared, as a call answering (value, error).
// The caller emits the error plumbing, because what a failed decode
// returns and how it is worded differ per site.
//
// The numeric widths and the declared records reach here, and they are
// one lane at every call site because they answer the same shape: the
// driver hands back something wider than the schema declared, and the
// check can fail. A temporal carrier is a shape change rather than a
// range question — a dbtype.Date holds exactly what a Date holds — so
// narrowExpr keeps those and they have no failure to report.
//
// The record arm is why this takes a width at all. Its helper is named
// from the canonical encoding (codegen.RecordHelperSuffix), and goType
// is the anonymous struct text, which does not run backwards into a
// PropertyType — so the name cannot be derived from the argument every
// other arm here uses. width is the one the prepared surface carries
// beside the carrier it was derived from (spec §6).
//
// RECORD<ANY> is KindRecord too and must not reach the record arm: it
// would name a helper codegen.RecordEncodings never emits. The kind
// alone does not exclude it, so the carrier text is asked as well —
// RECORD<ANY> carries as map[string]any, not as a struct. Every call
// site also guards on driverCarrier(goType) != goType, which excludes it
// a second time; the belt here is what stops a new call site from having
// to know that.
func narrowCall(goType string, width graph.PropertyType, src string) string {
	if width.Kind() == graph.KindRecord && isRecordStruct(goType) {
		return fmt.Sprintf("decode%s(%s)", codegen.RecordHelperSuffix(width), src)
	}
	if goType == "float32" {
		return fmt.Sprintf("narrowFloat32(%s)", src)
	}
	return fmt.Sprintf("narrowInt[%s](%s)", goType, src)
}

// narrowsANumericWidth reports, separately for each helper, whether this
// emission holds a site that calls it.
//
// Separately, because they are gated separately: a schema that narrows
// only integers must not be handed narrowFloat32, and the `math` import
// rides on the float helper alone. An unexported function nothing calls
// fails the emitted package's own lint fence, so an over-broad gate
// reds the fixture rather than merely emitting a dead line.
func narrowsANumericWidth(entities []codegen.Entity, prepared []codegen.Query) (ints, floats bool) {
	visit := func(goType string) {
		leaf := leafType(goType)
		if leaf == driverCarrier(leaf) || isTemporalCarrier(leaf) {
			return
		}
		if isRecordStruct(leaf) {
			// A record also carries wider than it is declared, so it
			// reaches this far — but its narrowing is its own emitted
			// helper, not narrowInt. Without this arm every schema
			// declaring a record would be handed narrowInt with no
			// caller, which the emitted package's lint fence fails.
			return
		}
		if leaf == "float32" {
			floats = true
			return
		}
		ints = true
	}
	for _, e := range entities {
		for _, f := range e.Fields {
			visit(f.GoType)
		}
	}
	for _, p := range prepared {
		for _, f := range p.RowFields {
			visit(f.GoType)
			for elem := f.ListElem; elem != nil; elem = elem.Nested {
				visit(elem.GoType)
			}
		}
	}
	return ints, floats
}

// writeNarrowHelpers emits whichever checked-narrowing helpers this
// emission calls. Both answer the same sentence, because to a caller they
// are one rule: a stored value the declared width cannot hold fails the
// read, as a null on a non-nullable column and a value of the wrong
// dynamic type already do (ADR 0037). They are nonetheless emitted
// independently, because a schema narrowing only integers calls only one
// of them and the other would be an unexported function nothing calls.
func writeNarrowHelpers(b *strings.Builder, ints, floats bool) {
	if ints {
		b.WriteString(`
// narrowInt converts a driver's int64 down to the integer width the
// schema declared, refusing a value that width cannot represent.
//
// The round-trip catches every width whose range is a strict subset of
// int64's. uint64 is the one where it does not: the conversion is a
// bijection there, so uint64(-1) round-trips back to -1 unchanged and
// only the sign disagreement gives it away. A uint64 property's readable
// range is [0, MaxInt64] — the wire integer is signed 64-bit — so a
// negative carrier is always a violation rather than a large value.
func narrowInt[T ~int | ~int8 | ~int16 | ~int32 | ~int64 |
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64](v int64) (T, error) {
	out := T(v)
	if int64(out) != v || (out < T(0)) != (v < 0) {
		return 0, fmt.Errorf("value %d does not fit the declared %T width", v, out)
	}
	return out, nil
}
`)
	}
	if floats {
		b.WriteString(`
// narrowFloat32 converts a driver's float64 down to float32, refusing a
// value that overflows to an infinity the carrier did not hold.
//
// Precision loss is NOT refused: FLOAT32 is approximate and every
// in-range float64 rounds to reach it. The test is the invented infinity
// rather than a comparison against math.MaxFloat32, because a float64
// strictly greater than MaxFloat32 can still round DOWN to it — that
// value is representable and a magnitude test would refuse it. An
// infinity or a NaN the store already held passes through unchanged.
func narrowFloat32(v float64) (float32, error) {
	out := float32(v)
	if math.IsInf(float64(out), 0) && !math.IsInf(v, 0) {
		return 0, fmt.Errorf("value %g does not fit the declared float32 width", v)
	}
	return out, nil
}
`)
	}
}

// widenExpr renders the parameter-binding expression for a non-nullable
// value of the emitted Go type. The mirror of narrowExpr: narrow
// integers and float32 widen by a Go conversion into the carrier the
// driver marshals; neutral temporals route through from<X>; a declared
// record routes through its own encode helper.
//
// The record arm is not a conversion and could not be one. map[string]any
// is not convertible from an anonymous struct in Go — the widening is a
// field-by-field build, which is what the helper holds. It takes the
// width for the reason narrowCall does: the helper's name comes from the
// canonical encoding, and the struct text cannot be read back into one.
func widenExpr(goType string, width graph.PropertyType, access string) string {
	if isTemporalCarrier(goType) {
		return fmt.Sprintf("from%s(%s)", goType, access)
	}
	if width.Kind() == graph.KindRecord && isRecordStruct(goType) {
		return fmt.Sprintf("encode%s(%s)", codegen.RecordHelperSuffix(width), access)
	}
	return fmt.Sprintf("%s(%s)", driverCarrier(goType), access)
}

// renderTemporalConversions emits temporal_neo4j.go: the unexported
// bridge between the neutral carriers temporal.go declares and the
// dbtype values this driver reads and writes. Entirely unexported, so
// the driver-freedom sweep over the emitted public surface does not see
// it — this file is where the driver is allowed to appear.
//
// The conversions are derived from the bolt codec, not from any
// dbtype constructor: there are none. Both directions are checked
// against neo4j/internal/bolt/outgoing.go (packing) and hydrator.go
// (hydration) at v5.28.4, whose shapes v6 repeats.
//
// Every from<X> builds in a fixed zone — UTC, or the value's own offset
// for zoned Time — never time.Local. time.Date resolves a wall time
// that a DST transition makes ambiguous or non-existent by moving it,
// so a local-zone construction would silently shift the very components
// the carrier exists to hold. A fixed zone has no transitions and
// nothing to resolve.
func renderTemporalConversions(pkg string, uses map[string]temporalUse, target driverTarget) []byte {
	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")

	b.WriteString("import (\n")
	if needsTimePackage(uses) {
		b.WriteString("\t\"time\"\n\n")
	}
	b.WriteString("\t\"" + target.dbtypeImport + "\"\n")
	b.WriteString(")\n")

	for _, name := range codegen.TemporalCarriers {
		use, used := uses[name]
		if !used {
			continue
		}
		if use.decode {
			b.WriteString("\n")
			b.WriteString(temporalDecodeBody(name))
		}
		if use.encode || use.encodePtr {
			b.WriteString("\n")
			b.WriteString(temporalEncodeBody(name))
		}
		if use.encodePtr {
			fmt.Fprintf(&b, `
// from%[1]sPtr binds a nullable %[1]s parameter: a nil pointer is the
// Cypher null the schema's nullability declared, not a zero %[1]s.
func from%[1]sPtr(v *%[1]s) any {
	if v == nil {
		return nil
	}
	return from%[1]s(*v)
}
`, name)
		}
		if use.list {
			b.WriteString("\n")
			b.WriteString(temporalListEncodeBody(name))
			if use.listPtr {
				b.WriteString("\n")
				b.WriteString(temporalListEncodePtrBody(name))
			}
		}
	}
	return []byte(b.String())
}

// temporalListEncodeBody returns the from<X>List helper for one carrier.
//
// The result is []any rather than a slice of the dbtype counterpart
// because dbtype has no list type to build: []any is the driver's own
// array carrier, the one its hydrator produces on the way back, and the
// one packX packs element by element on the way out.
func temporalListEncodeBody(name string) string {
	return fmt.Sprintf(`// %[1]s widens a list of %[2]s parameters element by element. The
// driver marshals no gqlc struct, so each element converts before the
// list reaches the wire.
func %[1]s(v []%[2]s) []any {
	out := make([]any, len(v))
	for i := range v {
		out[i] = from%[2]s(v[i])
	}
	return out
}
`, temporalListHelper(name), name)
}

// temporalListEncodePtrBody returns the nullable wrapper for one
// from<X>List helper. A nil pointer is the schema's declared null; an
// empty non-nil list is an empty array, which is a different value.
func temporalListEncodePtrBody(name string) string {
	return fmt.Sprintf(`// %[1]sPtr binds a nullable list of %[2]s: a nil pointer is the
// Cypher null the schema's nullability declared, not an empty list.
func %[1]sPtr(v *[]%[2]s) any {
	if v == nil {
		return nil
	}
	return %[1]s(*v)
}
`, temporalListHelper(name), name)
}

// needsTimePackage reports whether any emitted conversion body names the
// time package. Duration is the one carrier that does not: dbtype.Duration
// is already a component struct, so both directions are a field copy.
func needsTimePackage(uses map[string]temporalUse) bool {
	for name, use := range uses {
		if name == "Duration" {
			continue
		}
		if use.decode || use.encode || use.encodePtr {
			return true
		}
	}
	return false
}

// temporalDecodeBody returns the to<X> helper for one carrier. Each
// reads its components off the driver value's own location, which is
// what the hydrator built them in: the driver's temporal types are
// time.Time newtypes whose Location is part of the value.
func temporalDecodeBody(name string) string {
	switch name {
	case "Date":
		return `// toDate reads the calendar components off a driver date. The
// hydrator builds it as UTC midnight of the packed epoch-day, so the
// clock the newtype also carries holds nothing to lose.
func toDate(v dbtype.Date) Date {
	year, month, day := time.Time(v).Date()
	return Date{Year: year, Month: int(month), Day: day}
}
`
	case "LocalTime":
		return `// toLocalTime reads the clock components off a driver local time.
func toLocalTime(v dbtype.LocalTime) LocalTime {
	t := time.Time(v)
	hour, minute, second := t.Clock()
	return LocalTime{Hour: hour, Minute: minute, Second: second, Nanosecond: t.Nanosecond()}
}
`
	case "Time":
		return `// toTime reads the clock components and the zone offset off a driver
// zoned time. The offset is east-positive, matching the wire.
func toTime(v dbtype.Time) Time {
	t := time.Time(v)
	hour, minute, second := t.Clock()
	_, offset := t.Zone()
	return Time{Hour: hour, Minute: minute, Second: second, Nanosecond: t.Nanosecond(), OffsetSeconds: offset}
}
`
	case "LocalDateTime":
		return `// toLocalDateTime reads the date and clock components off a driver
// local date-time.
func toLocalDateTime(v dbtype.LocalDateTime) LocalDateTime {
	t := time.Time(v)
	year, month, day := t.Date()
	hour, minute, second := t.Clock()
	return LocalDateTime{
		Year: year, Month: int(month), Day: day,
		Hour: hour, Minute: minute, Second: second, Nanosecond: t.Nanosecond(),
	}
}
`
	case "Duration":
		return `// toDuration copies a driver duration field for field: dbtype.Duration
// already holds the four components apart.
func toDuration(v dbtype.Duration) Duration {
	return Duration{Months: v.Months, Days: v.Days, Seconds: v.Seconds, Nanos: v.Nanos}
}
`
	}
	return ""
}

// temporalEncodeBody returns the from<X> helper for one carrier.
func temporalEncodeBody(name string) string {
	switch name {
	case "Date":
		return `// fromDate builds the driver date the packer turns into epoch-days.
// UTC midnight, so the packer's day division is exact for every date,
// including those before the epoch.
func fromDate(v Date) dbtype.Date {
	return dbtype.Date(time.Date(v.Year, time.Month(v.Month), v.Day, 0, 0, 0, 0, time.UTC))
}
`
	case "LocalTime":
		return `// fromLocalTime builds the driver local time the packer turns into
// nanoseconds since midnight. The date is an arbitrary anchor the
// packer never reads.
func fromLocalTime(v LocalTime) dbtype.LocalTime {
	return dbtype.LocalTime(time.Date(0, time.January, 1, v.Hour, v.Minute, v.Second, v.Nanosecond, time.UTC))
}
`
	case "Time":
		return `// fromTime builds the driver zoned time the packer turns into
// (nanoseconds since local midnight, offset seconds). The zone is
// named "Offset" because that is the name the driver's own hydrator
// gives a fixed-offset zone.
func fromTime(v Time) dbtype.Time {
	zone := time.FixedZone("Offset", v.OffsetSeconds)
	return dbtype.Time(time.Date(0, time.January, 1, v.Hour, v.Minute, v.Second, v.Nanosecond, zone))
}
`
	case "LocalDateTime":
		return `// fromLocalDateTime builds the driver local date-time. The packer
// reads the wall clock and adds the zone offset back, so a UTC
// construction packs the components unshifted.
func fromLocalDateTime(v LocalDateTime) dbtype.LocalDateTime {
	return dbtype.LocalDateTime(time.Date(v.Year, time.Month(v.Month), v.Day, v.Hour, v.Minute, v.Second, v.Nanosecond, time.UTC))
}
`
	case "Duration":
		return `// fromDuration copies a duration field for field.
func fromDuration(v Duration) dbtype.Duration {
	return dbtype.Duration{Months: v.Months, Days: v.Days, Seconds: v.Seconds, Nanos: v.Nanos}
}
`
	}
	return ""
}
