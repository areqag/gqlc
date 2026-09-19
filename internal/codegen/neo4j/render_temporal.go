package neo4j

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// carrierUse records which conversion directions one carrier needs in a
// given batch. Emitting only the used ones keeps dead unexported
// functions out of the generated package.
//
// ONE type for both carrier kinds this backend converts — the neutral
// temporals and the declared records — because the directions are the
// same five and are decided by the same three facts: which position the
// carrier was reached through, whether it was nullable there, and
// whether a list level sat above it. Two structurally identical types
// would be two places for those rules to be written and one place for
// them to disagree.
type carrierUse struct {
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
	// listElem and listElemPtr are the same two bits one level in, for a
	// list whose ELEMENTS are nullable ([]*X, *[]*X). They are separate
	// bits rather than a flag on list because the two helpers have
	// different parameter types and neither can stand for the other: a
	// batch binding both shapes owes both bodies.
	listElem, listElemPtr bool
}

// conversionUses walks the prepared batch ONCE and answers, for both
// carrier kinds, which directions the emission sites will reach for:
// neutral carriers keyed by name, records keyed by the canonical
// encoding their helper suffix is derived from.
//
// The first map holds EVERY neutral carrier (ADR 0033) — the five
// temporal names and the UUID one — and not only the temporal five, so
// the split by bridge file is the render layer's and not this walk's.
// renderTemporalConversions reads it through codegen.TemporalCarriers
// and renderUUIDConversions through codegen.UUIDCarrier, and each
// therefore skips the other's entries. A walk that filtered here would
// have to be run twice over the same positions to answer both.
//
// Decode positions are entity properties and row columns (models.go and
// the per-source files); encode positions are query parameters. A
// parameter is the one position whose nullability changes the helper,
// because a nil pointer has to become a Cypher null rather than a
// zero-valued carrier.
//
// One walk rather than two, because the two answers are read off the
// SAME three facts at the SAME positions — a second walk would be a
// second copy of the nullable/list direction rules, and the emitted
// package is where a disagreement between them would surface: as a name
// no declaration answers, which `go build` of generated code reports
// with no line in the schema to point at.
//
// Both directions descend into a DECLARED record rather than stopping at
// its own carrier text. A record's carrier is an anonymous struct, so
// the leafType the carrier side marks on hands back the whole struct
// and no carrier inside it is ever named — while the record's emitted
// helper pair calls those carriers' conversions by name.
func conversionUses(prepared codegen.Prepared, tm typeMap) (map[string]carrierUse, map[graph.PropertyType]carrierUse, map[graph.PropertyType]carrierUse) {
	w := conversionUseWalk{
		tm:      tm,
		neutral: make(map[string]carrierUse),
		records: make(map[graph.PropertyType]carrierUse),
		unions:  make(map[graph.PropertyType]carrierUse),
	}
	for _, e := range prepared.Entities {
		for _, f := range e.Fields {
			w.markDecode(f.GoType, f.Width)
		}
	}
	for _, q := range prepared.Queries {
		for _, f := range q.RowFields {
			w.markDecode(f.GoType, f.Width)
			for elem := f.ListElem; elem != nil; elem = elem.Nested {
				w.markDecode(elem.GoType, elem.Width)
			}
		}
		for _, f := range q.ParamFields {
			w.markEncode(f.GoType, f.Width, f.Nullable)
		}
	}
	return w.neutral, w.records, w.unions
}

// conversionUseWalk accumulates conversionUses' three answers as the
// walk marks each position.
type conversionUseWalk struct {
	tm      typeMap
	neutral map[string]carrierUse
	records map[graph.PropertyType]carrierUse
	unions  map[graph.PropertyType]carrierUse
}

func (w *conversionUseWalk) markCarrier(goType string, set func(*carrierUse)) {
	name := leafType(goType)
	if !isNeutralCarrier(name) {
		return
	}
	use := w.neutral[name]
	set(&use)
	w.neutral[name] = use
}

func (w *conversionUseWalk) markRecord(encoding graph.PropertyType, set func(*carrierUse)) {
	use := w.records[encoding]
	set(&use)
	w.records[encoding] = use
}

func (w *conversionUseWalk) markUnion(encoding graph.PropertyType, set func(*carrierUse)) {
	use := w.unions[encoding]
	set(&use)
	w.unions[encoding] = use
}

// setDecodeUse is the mark every decode position applies.
func setDecodeUse(u *carrierUse) { u.decode = true }

// markDecode marks a decode position. Decode needs no nullability, in
// either kind: to<X> and decode<Suffix> are the same call either way,
// because a missing key is the field's own null rather than a different
// conversion.
func (w *conversionUseWalk) markDecode(goType string, width graph.PropertyType) {
	if members, ok := unionLeafMembers(goType, width, w.tm); ok {
		w.markUnion(leafWidth(width), setDecodeUse)
		for _, m := range members {
			w.markDecode(m.GoType, m.Width)
		}
		return
	}
	if fields, ok := recordLeafFields(goType, width, w.tm); ok {
		w.markRecord(leafWidth(width), setDecodeUse)
		for _, f := range fields {
			w.markDecode(f.GoType, f.Width)
		}
		return
	}
	w.markCarrier(goType, setDecodeUse)
}

// markEncode marks a parameter position. A record's encode body spells
// each field by the PARAMETER rules — it is paramBindExpr that renders
// them — so the marks a field owes are the marks the same shape would
// owe as a parameter. The outer nullability does not reach the fields:
// encode<X>Ptr nil-checks and then calls encode<X>, which builds every
// field the same way.
func (w *conversionUseWalk) markEncode(goType string, width graph.PropertyType, nullable bool) {
	set := encodeDirection(goType, width, nullable)
	if members, ok := unionLeafMembers(goType, width, w.tm); ok {
		w.markUnion(leafWidth(width), set)
		// A member is marked as a NON-nullable, NON-list parameter
		// whatever the position above it was: encode<Suffix>'s arms
		// each hold a value already known to be that member, and the
		// outer nullability was spent by the Ptr wrapper one call
		// earlier — exactly the rule the record branch states.
		for _, m := range members {
			w.markEncode(m.GoType, m.Width, false)
		}
		return
	}
	if fields, ok := recordLeafFields(goType, width, w.tm); ok {
		w.markRecord(leafWidth(width), set)
		for _, f := range fields {
			w.markEncode(f.GoType, f.Width, f.Nullable)
		}
		return
	}
	w.markCarrier(goType, set)
}

// encodeDirection answers which encode helper ONE parameter position
// reaches for, as the mutation conversionUses applies to whichever
// carrier's flags that position lands on.
//
// A function of the position alone rather than a closure over the walk,
// because the answer depends on nothing the walk accumulates: the shape
// and the nullability are the whole question. That is what lets the same
// answer be applied to a temporal carrier, a record encoding and a union
// encoding without the walk restating the rules once per kind.
func encodeDirection(goType string, width graph.PropertyType, nullable bool) func(*carrierUse) {
	return func(u *carrierUse) {
		switch {
		case walksElements(goType, width):
			// A list parameter converts per element, so what it needs is
			// the plain helper at the leaf plus the list helper that calls
			// it — never the Ptr form, whose nil-to-Cypher-null job belongs
			// to the list helper.
			u.encode = true
			// A nullable ELEMENT owes a DIFFERENT list helper, not a flag
			// on this one: the two take incompatible parameter types ([]X
			// and []*X), so marking both would emit a helper nothing calls.
			if listElemIsNullable(goType) {
				u.listElem = true
				u.listElemPtr = u.listElemPtr || nullable
				return
			}
			u.list = true
			u.listPtr = u.listPtr || nullable
		case nullable:
			u.encodePtr = true
		default:
			u.encode = true
		}
	}
}

// unionLeafMembers answers the member plan of the DECLARED closed union at
// the leaf of a carrier, if there is one.
//
// The leaf rather than the carrier itself, for recordLeafFields' reason: a
// LIST<UNION<…>> reaches its members' conversions too, because the emitted
// encode<Suffix>List calls encode<Suffix> per element and that helper's
// arms call each member's own conversion by name. A test on the outer
// width alone would see KindList and descend into nothing.
//
// ok=false covers two negatives on purpose — not a union, and a union some
// member of which this backend cannot carry. The second cannot arrive
// here, because a refused union fails preparation before any emission walk
// runs; it is folded in rather than distinguished so a caller has one
// question to ask and no unreachable arm to write.
func unionLeafMembers(goType string, width graph.PropertyType, tm typeMap) ([]codegen.UnionMemberPlan, bool) {
	leaf, elem := leafType(goType), leafWidth(width)
	if !codegen.IsDeclaredUnion(leaf, elem) {
		return nil, false
	}
	return codegen.UnionMembers(elem, tm.Property)
}

// recordLeafFields answers the field plan of the DECLARED record at the
// leaf of a carrier, if there is one.
//
// The leaf rather than the carrier itself, because a LIST<RECORD<...>>
// reaches its fields' conversions too: the emitted encode<X>List calls
// encode<X> per element, which calls each field's conversion by name. A
// test on the outer width alone would see KindList and descend into
// nothing.
//
// ok=false covers three different negatives on purpose — not a record,
// RECORD<ANY> (no declared fields, so nothing hides inside it), and a
// record some field of which this backend cannot carry. The third cannot
// arrive here, because a refused record fails preparation before any
// emission walk runs; it is folded in rather than distinguished so that
// a caller has one question to ask and no unreachable arm to write.
func recordLeafFields(goType string, width graph.PropertyType, tm typeMap) ([]codegen.RecordFieldPlan, bool) {
	leaf, elem := leafType(goType), leafWidth(width)
	if !codegen.IsDeclaredRecord(leaf, elem) {
		return nil, false
	}
	return codegen.RecordFields(elem.Fields(), tm.Property)
}

// isNeutralCarrier reports whether a Go type text is exactly one of the
// driver-free carrier names the generated package declares for itself —
// the five temporal ones (ADR 0033) or the UUID one (ADR 0047). Exact,
// never a prefix or substring test: "Date" is inside "LocalDateTime" and
// inside entity names a schema chose.
//
// One predicate over both families because the sites that ask it are
// asking what the two share: the emitted type is not what the driver
// packs, so a BIND reaches the wire through an emitted from<X> — bare,
// Ptr or List — and conversionUses marks a direction on it. A *UUID
// passed through as the pointer it is would reach the packer as a
// pointer to a Go array, which both majors refuse with an
// UnsupportedTypeError rather than pack (renderUUIDConversions cites
// the arms).
//
// The DECODE direction is where the families part, and
// isTemporalCarrier below is the question those sites ask instead.
//
// Which FILE the helpers land in is a separate question, answered by the
// two render functions splitting on codegen.TemporalCarriers versus
// codegen.UUIDCarrier — so a batch naming one family emits that family's
// bridge alone.
func isNeutralCarrier(goType string) bool {
	return goType == codegen.UUIDCarrier || isTemporalCarrier(goType)
}

// isTemporalCarrier reports whether a Go type text is exactly one of the
// five neutral temporal carriers, which is what a DECODE site asks.
//
// A temporal decode is a shape change that cannot fail — a dbtype.Date
// holds exactly what a Date holds — so it is narrowExpr's, and the site
// asserts a dbtype value and owes the dbtype import. A UUID decode is
// neither: it arrives as a string and is PARSED, which fails on text
// that is not a UUID, so it rides narrowCall's checked lane with the
// numeric widths and names no driver package.
func isTemporalCarrier(goType string) bool {
	for _, name := range codegen.TemporalCarriers {
		if goType == name {
			return true
		}
	}
	return false
}

// leafType strips the slice prefixes AND the element-nullability stars
// off an emitted Go type text, yielding the element type the decode
// sites narrow one at a time.
//
// Both, alternating, because they alternate in the text: a nullable
// column of LIST<LIST<INT32>> is `*[]*[]*int32` and its leaf is `int32`.
// Stopping at a star would hand narrowsANumericWidth a leaf of
// `*[]*int32`, which is neither its own driver carrier nor a temporal
// carrier nor "float32", so the gate would claim narrowInt is called
// where it is not — and an unexported helper nothing calls fails the
// emitted package's own lint fence, reddening the fixture rather than
// merely emitting a dead line.
func leafType(goType string) string {
	for {
		elem := strings.TrimPrefix(strings.TrimPrefix(goType, "*"), "[]")
		if elem == goType {
			return goType
		}
		goType = elem
	}
}

// elemBase strips the element-nullability star off one emitted element
// type, yielding the type the driver value is asserted to. The star is
// this codebase's spelling of "the schema permits this element to be
// NULL" (bd gqlc-dxhwp); no Bolt wire value is a pointer, so every site
// that asks the driver a question asks it about the base.
//
// One star, not a loop: the star belongs to a single element position,
// and the levels beneath it carry their own, stripped by their own
// recursion. leafType is the one that walks all of them at once.
func elemBase(goType string) string {
	return strings.TrimPrefix(goType, "*")
}

// listElemIsNullable reports whether the elements of a list type text
// are themselves nullable.
//
// A parameter's GoType never carries the whole-value star — that one is
// written from Param.Nullable at struct emission — so every star in this
// text belongs to an element. The prefix is stripped once because a
// temporal list is depth 1: ADR 0035 refuses a nested DECLARED list as a
// stored property, and a parameter's type is its property's.
func listElemIsNullable(goType string) bool {
	_, nullable := strings.CutPrefix(strings.TrimPrefix(goType, "[]"), "*")
	return nullable
}

// temporalListHelper names the from<X>List helper for one carrier.
//
// The Nullable token sits where the star sits in the type text — []*Date
// stars the element, so fromNullableDateList reads as "a list of
// nullable Date". That leaves the Ptr SUFFIX its existing meaning, the
// whole value, so *[]*Date composes as fromNullableDateListPtr with each
// position spelled once. It is also the spelling AGE arrived at
// independently for the same question (agtypeListOfNullableDate).
func temporalListHelper(leaf string, elemNullable bool) string {
	if elemNullable {
		return "fromNullable" + leaf + "List"
	}
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
// Which records the arm admits is codegen.IsDeclaredRecord's question, asked in
// the same words at every record arm in this backend so the three cannot
// drift on it. Every call site here also guards on driverCarrier(goType)
// != goType, which excludes RECORD<ANY> a second time; the belt is what
// stops a new call site from having to know that.
func narrowCall(goType string, width graph.PropertyType, src string) string {
	if codegen.IsDeclaredRecord(goType, width) {
		return fmt.Sprintf("decode%s(%s)", codegen.RecordHelperSuffix(width), src)
	}
	if goType == "float32" {
		return fmt.Sprintf("narrowFloat32(%s)", src)
	}
	if goType == codegen.UUIDCarrier {
		// Not a range question but the same shape of answer: the stored
		// string either is a UUID or fails the read (ADR 0047).
		return fmt.Sprintf("to%s(%s)", goType, src)
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
func narrowsANumericWidth(entities []codegen.Entity, prepared []codegen.Query, tm typeMap) (ints, floats bool) {
	var n numericNarrowing
	for _, e := range entities {
		for _, f := range e.Fields {
			n.markWidth(f.GoType, f.Width, tm)
		}
	}
	for _, p := range prepared {
		for _, f := range p.RowFields {
			n.markWidth(f.GoType, f.Width, tm)
			for elem := f.ListElem; elem != nil; elem = elem.Nested {
				n.markWidth(elem.GoType, elem.Width, tm)
			}
		}
	}
	return n.ints, n.floats
}

// numericNarrowing accumulates narrowsANumericWidth's two answers.
type numericNarrowing struct {
	ints, floats bool
}

// markWidth records which helper one position's declared width calls.
func (n *numericNarrowing) markWidth(goType string, width graph.PropertyType, tm typeMap) {
	if members, ok := unionLeafMembers(goType, width, tm); ok {
		// A union's own carrier is `any`, so the leaf test below would
		// stop here and the narrowing its decode arms call would be
		// emitted with no declaration. The members ARE the narrowed
		// widths — that is the whole of what the member list buys
		// (spec §4) — so the descent is not optional.
		for _, m := range members {
			n.markWidth(m.GoType, m.Width, tm)
		}
		return
	}
	leaf := leafType(goType)
	if leaf == driverCarrier(leaf) || isNeutralCarrier(leaf) {
		return
	}
	if codegen.IsRecordStruct(leaf) {
		// A record also carries wider than it is declared, so it
		// reaches this far — but its narrowing is its own emitted
		// helper, not narrowInt. Without this arm every schema
		// declaring a record would be handed narrowInt with no
		// caller, which the emitted package's lint fence fails.
		return
	}
	if leaf == "float32" {
		n.floats = true
		return
	}
	n.ints = true
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
	if isNeutralCarrier(goType) {
		return fmt.Sprintf("from%s(%s)", goType, access)
	}
	if codegen.IsDeclaredRecord(goType, width) {
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
func renderTemporalConversions(pkg string, uses map[string]carrierUse, target driverTarget) []byte {
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
		writeTemporalCarrierBridge(&b, name, use)
	}
	return []byte(b.String())
}

// writeTemporalCarrierBridge emits the conversions one temporal carrier
// is used in, in the fixed order decode, encode, encodePtr, list,
// listElem.
func writeTemporalCarrierBridge(b *strings.Builder, name string, use carrierUse) {
	if use.decode {
		b.WriteString("\n")
		b.WriteString(temporalDecodeBody(name))
	}
	if use.encode || use.encodePtr {
		b.WriteString("\n")
		b.WriteString(temporalEncodeBody(name))
	}
	if use.encodePtr {
		fmt.Fprintf(b, `
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
		b.WriteString(temporalListEncodeBody(name, false))
		if use.listPtr {
			b.WriteString("\n")
			b.WriteString(temporalListEncodePtrBody(name, false))
		}
	}
	if use.listElem {
		b.WriteString("\n")
		b.WriteString(temporalListEncodeBody(name, true))
		if use.listElemPtr {
			b.WriteString("\n")
			b.WriteString(temporalListEncodePtrBody(name, true))
		}
	}
}

// temporalListEncodeBody returns the from<X>List helper for one carrier.
//
// The result is []any rather than a slice of the dbtype counterpart
// because dbtype has no list type to build: []any is the driver's own
// array carrier, the one its hydrator produces on the way back, and the
// one packX packs element by element on the way out.
func temporalListEncodeBody(name string, elemNullable bool) string {
	if elemNullable {
		return fmt.Sprintf(`// %[1]s widens a list of nullable %[2]s parameters element by
// element. A nil element binds the Cypher null the schema's element
// nullability declared: packV packs a nil interface as null, so the
// conversion the other elements owe is simply the one it does not.
func %[1]s(v []*%[2]s) []any {
	out := make([]any, len(v))
	for i := range v {
		if v[i] == nil {
			out[i] = nil
			continue
		}
		out[i] = from%[2]s(*v[i])
	}
	return out
}
`, temporalListHelper(name, true), name)
	}
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
`, temporalListHelper(name, false), name)
}

// temporalListEncodePtrBody returns the nullable wrapper for one
// from<X>List helper. A nil pointer is the schema's declared null; an
// empty non-nil list is an empty array, which is a different value.
//
// elemNullable selects which helper is wrapped, and only that: the
// pointer this body indirects is the WHOLE list's, one position out from
// the element stars, so its own body is the same either way.
func temporalListEncodePtrBody(name string, elemNullable bool) string {
	elem := name
	if elemNullable {
		elem = "*" + name
	}
	return fmt.Sprintf(`// %[1]sPtr binds a nullable list of %[2]s: a nil pointer is the
// Cypher null the schema's nullability declared, not an empty list.
func %[1]sPtr(v *[]%[2]s) any {
	if v == nil {
		return nil
	}
	return %[1]s(*v)
}
`, temporalListHelper(name, elemNullable), elem)
}

// needsTimePackage reports whether any conversion body THIS FILE emits
// names the time package. Duration is the one temporal carrier that does
// not: dbtype.Duration is already a component struct, so both directions
// are a field copy.
//
// Driven off codegen.TemporalCarriers rather than off the map's own
// keys, because the map is every neutral carrier's uses and this
// question is about temporal_neo4j.go alone. Ranging the keys would let
// the UUID entry — whose bodies live in uuid_neo4j.go and name no time
// package — put an unused time import in this file, and an emitted
// package with an unused import does not compile.
func needsTimePackage(uses map[string]carrierUse) bool {
	for _, name := range codegen.TemporalCarriers {
		if name == "Duration" {
			continue
		}
		use := uses[name]
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
