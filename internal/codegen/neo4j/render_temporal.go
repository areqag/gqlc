package neo4j

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
)

// temporalUse records which conversion directions one neutral carrier
// needs in a given batch. Emitting only the used ones keeps dead
// unexported functions out of the generated package.
type temporalUse struct {
	decode    bool // to<X>: a driver value read into the neutral carrier
	encode    bool // from<X>: a carrier bound as a non-nullable parameter
	encodePtr bool // from<X>Ptr: a carrier bound as a nullable parameter
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

// narrowExpr renders the expression that turns a value of the driver
// carrier into the emitted Go type, given the local the driver value
// landed in. Widths the driver over-carries (int64 → int8, float64 →
// float32) narrow by a Go conversion; the neutral temporal carriers are
// not conversion-compatible with their dbtype counterparts — they are
// different shapes, not different spellings — so they route through the
// emitted to<X> helper instead.
//
// Callers reach here only when driverCarrier(goType) != goType; a type
// that is its own carrier is assigned bare at the call site.
func narrowExpr(goType, src string) string {
	if isTemporalCarrier(goType) {
		return fmt.Sprintf("to%s(%s)", goType, src)
	}
	return fmt.Sprintf("%s(%s)", goType, src)
}

// widenExpr renders the parameter-binding expression for a non-nullable
// value of the emitted Go type. The mirror of narrowExpr: narrow
// integers and float32 widen by a Go conversion into the carrier the
// driver marshals; neutral temporals route through from<X>.
func widenExpr(goType, access string) string {
	if isTemporalCarrier(goType) {
		return fmt.Sprintf("from%s(%s)", goType, access)
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
	}
	return []byte(b.String())
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
