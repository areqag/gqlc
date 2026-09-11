package neo4j

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// renderRecordHelpers emits record_neo4j.go: one carrier alias per
// declared record encoding the batch reaches, plus whichever of the five
// conversion helpers the emission sites call.
//
// encodings is codegen.RecordEncodings' answer, so the file's order is
// the canonical-encoding order both backends share and is byte-stable
// across runs. uses is conversionUses' record half, so a helper is
// emitted exactly where something calls it — an unexported function
// nothing calls fails the emitted package's own lint fence, which is a
// red fixture rather than a dead line.
func renderRecordHelpers(pkg string, encodings []graph.PropertyType, uses map[graph.PropertyType]carrierUse, target driverTarget) []byte {
	var body strings.Builder
	for _, pt := range encodings {
		use := uses[pt]
		alias := codegen.RecordAliasName(pt)
		suffix := codegen.RecordHelperSuffix(pt)
		// Every encoding in the set is reached from some position, so
		// every alias is named by some signature below and none is a
		// declaration nothing uses.
		text, ok := codegen.RecordStructText(pt.Fields(), target.types().Property)
		if !ok {
			// Unreachable: a record some field of which this backend
			// cannot carry is refused at preparation, before any
			// emission walk builds the encoding set. Skipping rather
			// than panicking, because a helper the batch never calls is
			// the only thing lost and generation has no channel here to
			// report a refusal through.
			continue
		}
		fmt.Fprintf(&body, "\n// %s carries %s.\ntype %s = %s\n", alias, pt, alias, text)

		// The plain encode helper stands under all three encode
		// directions: the Ptr wrapper nil-checks and calls it, and the
		// List wrapper calls it per element.
		if use.encode || use.encodePtr || use.list {
			writeRecordEncode(&body, pt, suffix, alias, target.types())
		}
		writeRecordWrappers(&body, pt, suffix, alias, use)
		if use.decode {
			writeRecordDecode(&body, pt, suffix, alias, target.types())
		}
	}

	needFmt, needTime, needDbtype := recordFileImports(encodings, uses, target.types())

	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")
	if needFmt || needTime || needDbtype {
		b.WriteString("import (\n")
		if needFmt {
			b.WriteString("\t\"fmt\"\n")
		}
		if needTime {
			b.WriteString("\t\"time\"\n")
		}
		if (needFmt || needTime) && needDbtype {
			b.WriteString("\n")
		}
		if needDbtype {
			b.WriteString("\t\"" + target.dbtypeImport + "\"\n")
		}
		b.WriteString(")\n")
	}
	b.WriteString(body.String())
	return []byte(b.String())
}

// writeRecordWrappers emits whichever of the three encode wrappers the
// batch calls: the nullable one, the list one, and the nullable-list one.
// They are together because they share one question the plain encoder
// does not raise — whether this encoding's encode can FAIL, which it can
// exactly when a field of it reaches a declared union, and which changes
// each wrapper's return arity rather than its body.
//
// Split out of renderRecordHelpers because that fallibility doubles every
// arm: six emissions behind four conditions is over the cognitive gate
// this repository sets, and the split is along the seam the doubling
// introduced rather than an arbitrary one.
func writeRecordWrappers(body *strings.Builder, pt graph.PropertyType, suffix, alias string, use carrierUse) {
	fallible := recordEncodeIsFallible(pt)
	if use.encodePtr {
		if fallible {
			fmt.Fprintf(body, `
// encode%[1]sPtr binds a nullable %[2]s parameter: a nil pointer is the
// Cypher null the schema's nullability declared, not a zero-valued map.
func encode%[1]sPtr(v *%[2]s) (any, error) {
	if v == nil {
		return nil, nil
	}
	return encode%[1]s(*v)
}
`, suffix, alias)
		} else {
			fmt.Fprintf(body, `
// encode%[1]sPtr binds a nullable %[2]s parameter: a nil pointer is the
// Cypher null the schema's nullability declared, not a zero-valued map.
func encode%[1]sPtr(v *%[2]s) any {
	if v == nil {
		return nil
	}
	return encode%[1]s(*v)
}
`, suffix, alias)
		}
	}
	if use.list {
		if fallible {
			fmt.Fprintf(body, `
// encode%[1]sList widens a list of %[2]s parameters element by element.
// The driver marshals no gqlc struct, so each element becomes its map
// before the list reaches the wire.
func encode%[1]sList(v []%[2]s) ([]any, error) {
	out := make([]any, len(v))
	for i := range v {
		elem, err := encode%[1]s(v[i])
		if err != nil {
			return nil, fmt.Errorf(%[3]s, %[4]s, i, err)
		}
		out[i] = elem
	}
	return out, nil
}
`, suffix, alias, strconv.Quote("encode %s element %d: %w"), strconv.Quote(string(pt)))
		} else {
			fmt.Fprintf(body, `
// encode%[1]sList widens a list of %[2]s parameters element by element.
// The driver marshals no gqlc struct, so each element becomes its map
// before the list reaches the wire.
func encode%[1]sList(v []%[2]s) []any {
	out := make([]any, len(v))
	for i := range v {
		out[i] = encode%[1]s(v[i])
	}
	return out
}
`, suffix, alias)
		}
	}
	if use.listPtr {
		if fallible {
			fmt.Fprintf(body, `
// encode%[1]sListPtr binds a nullable list of %[2]s: a nil pointer is the
// Cypher null the schema's nullability declared, not an empty list.
func encode%[1]sListPtr(v *[]%[2]s) (any, error) {
	if v == nil {
		return nil, nil
	}
	return encode%[1]sList(*v)
}
`, suffix, alias)
		} else {
			fmt.Fprintf(body, `
// encode%[1]sListPtr binds a nullable list of %[2]s: a nil pointer is the
// Cypher null the schema's nullability declared, not an empty list.
func encode%[1]sListPtr(v *[]%[2]s) any {
	if v == nil {
		return nil
	}
	return encode%[1]sList(*v)
}
`, suffix, alias)
		}
	}
}

// writeRecordEncode emits encode<Suffix>: the field-by-field build of the
// Cypher map one declared record binds as.
//
// A map rather than the struct itself, because the driver marshals no
// struct of gqlc's: packX hands a reflect.Struct — and a *struct, and
// each element of a slice of them — to packStruct, whose cases are the
// driver's own point and temporal types and whose default raises
// UnsupportedTypeError. packX's map[string]any arm reaches packMap, which
// recurses each value back through packX, so a record nests to any depth
// unaided (measured against v5.28.4 and v6.2.0 outgoing.go).
//
// Each field is bound by paramBindExpr, the SAME renderer that binds a
// top-level parameter, because a record field is in the parameter
// position: it is the value side of a map the driver packs, with the same
// nullability question and the same per-carrier answers. Two renderers
// would be two chances to disagree about, say, whether a nullable DATE
// field is fromDatePtr or a bare pointer.
//
// The keys are the DECLARED field names, unmangled, because the map is
// the wire shape a Cypher expression indexes by the name the schema
// wrote. The Go side of the same field is the mangle, and RecordFields is
// what keeps the two in step.
func writeRecordEncode(b *strings.Builder, pt graph.PropertyType, suffix, alias string, tm typeMap) {
	plan, ok := codegen.RecordFields(pt.Fields(), tm.Property)
	if !ok {
		return
	}
	fallible := recordEncodeIsFallible(pt)
	fmt.Fprintf(b, "\n// encode%s builds the Cypher map a %s binds as.\n", suffix, alias)
	if fallible {
		fmt.Fprintf(b, "func encode%s(v %s) (map[string]any, error) {\n", suffix, alias)
	} else {
		fmt.Fprintf(b, "func encode%s(v %s) map[string]any {\n", suffix, alias)
	}
	if len(plan) == 0 {
		// RECORD<> carries struct{}, so the parameter is named but never
		// read. An empty composite literal keeps the signature uniform
		// with every other encode helper. A record with no fields reaches
		// no union, so the fallible form is unreachable here.
		b.WriteString("\treturn map[string]any{}\n}\n")
		return
	}
	binds := make([]string, len(plan))
	for i, f := range plan {
		param := codegen.Param{GoType: f.GoType, Nullable: f.Nullable, Width: f.Width}
		binds[i] = paramBindExpr(param, "v."+f.Field)
		if !paramBindIsFallible(param) {
			continue
		}
		// A field whose bind validates against a union's member set is
		// hoisted for writeParamPrelude's reason one level out: the map is
		// a composite literal and has no room for the error check that
		// refusal exists to raise.
		local := fmt.Sprintf("f%d", i)
		fmt.Fprintf(b, "\t%s, err := %s\n", local, binds[i])
		format, args := recordFail(pt, f.Key, 0, "%w")
		fmt.Fprintf(b, "\tif err != nil {\n\t\treturn nil, fmt.Errorf(%s, %s, err)\n\t}\n", format, args)
		binds[i] = local
	}
	b.WriteString("\treturn map[string]any{\n")
	for i, f := range plan {
		fmt.Fprintf(b, "\t\t%q: %s,\n", f.Key, binds[i])
	}
	if fallible {
		b.WriteString("\t}, nil\n}\n")
		return
	}
	b.WriteString("\t}\n}\n")
}

// recordEncodeIsFallible reports whether this record's emitted encode
// helper answers (value, error) rather than a value.
//
// Exactly the records that reach a closed union, through their own fields
// or through a list or a nested record under one. A union validates
// against its declared member set at BIND time (spec §4), so a record
// standing above one cannot promise to build its map; every other record
// encode is a field-by-field copy with nothing to refuse, and a helper
// declared to return an error it can never raise is plumbing every caller
// is made to check.
//
// A separate function rather than the expression inlined at the four
// sites that ask it — the body, the two wrappers and the caller's
// fallibility test — so the four cannot drift on which records they
// consider fallible. A drift there is a signature mismatch in the
// EMITTED package, which `go build` reports with no line in the schema
// to point at.
func recordEncodeIsFallible(pt graph.PropertyType) bool {
	return codegen.ReachesUnion(pt)
}

// writeRecordDecode emits decode<Suffix>: the field-by-field check of a
// driver map into the record's carrier.
//
// The driver hands a Cypher map back as map[string]any and has no
// narrower carrier to offer, so the declared shape is this package's to
// build — the same relationship a LIST has to []any. It is a boundary, so
// it validates rather than assuming: a value of the wrong dynamic type
// fails the decode naming both what was wanted and what arrived, exactly
// as an entity property does.
//
// A MISSING key is the field's own null. On a nullable field that is the
// nil the schema declared; on a NOT NULL one it is a decode error, which
// is the same three-way outcome writeEntityFieldDecode gives a property.
// An explicitly null value reads the same as an absent key, because a
// Cypher map spells "no value here" both ways and a decoder that told
// them apart would be reporting the shape of the writer rather than the
// value.
func writeRecordDecode(b *strings.Builder, pt graph.PropertyType, suffix, alias string, tm typeMap) {
	plan, ok := codegen.RecordFields(pt.Fields(), tm.Property)
	if !ok {
		return
	}
	fmt.Fprintf(b, "\n// decode%s checks a driver map into a %s.\n", suffix, alias)
	fmt.Fprintf(b, "func decode%s(v map[string]any) (%s, error) {\n", suffix, alias)
	fmt.Fprintf(b, "\tvar out %s\n", alias)
	if len(plan) == 0 {
		// RECORD<> declares no fields, so there is nothing to read out
		// of the map and the argument is named but never used.
		b.WriteString("\treturn out, nil\n}\n")
		return
	}
	// One counter across the whole helper rather than a per-field or
	// per-depth one, so no two locals in any of the nested scopes can
	// share a name. Positional for the reason writeSliceNarrow's are: a
	// name derived from a field is any identifier the schema author
	// chose, including `out`, `v` or `err`.
	n := 0
	next := func() string { n++; return fmt.Sprintf("v%d", n) }
	for _, f := range plan {
		raw := next()
		if f.Nullable {
			fmt.Fprintf(b, "\tif %s, ok := v[%q]; ok && %s != nil {\n", raw, f.Key, raw)
			got := writeRecordValueDecode(b, pt, f.Key, 0, f.GoType, f.Width, raw, "\t\t", next)
			fmt.Fprintf(b, "\t\tout.%s = &%s\n", f.Field, got)
			b.WriteString("\t}\n")
			continue
		}
		fmt.Fprintf(b, "\t%s, ok := v[%q]\n", raw, f.Key)
		b.WriteString("\tif !ok {\n")
		format, args := recordFail(pt, f.Key, 0, "no such field")
		fmt.Fprintf(b, "\t\treturn out, fmt.Errorf(%s, %s)\n", format, args)
		b.WriteString("\t}\n")
		got := writeRecordValueDecode(b, pt, f.Key, 0, f.GoType, f.Width, raw, "\t", next)
		fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, got)
	}
	b.WriteString("\treturn out, nil\n}\n")
}

// recordFail renders one decode failure as the arguments of a call:
// a Go string literal holding the format, then the two literals it
// interpolates. tail is the generator-owned wording of what went wrong
// and may carry its own verbs, whose values the caller appends.
//
// The two author-derived halves — the record's canonical encoding and
// the declared field name — are ARGUMENTS rather than text pasted into
// the format, and that is the whole point of this function. Pasted in,
// a name holding a quote would close the literal early and emit a file
// that does not parse, and a name holding a '%' would emit a format
// verb the call has no argument for, which `go vet` of the generated
// package fails. Both are author text and neither is validated
// anywhere: recordFieldLegality polices the Go MANGLE of a field name,
// not its spelling.
//
// depth is how many list levels were entered to reach the value, so the
// wording says which declared field failed even inside a list — the
// index is not named, because the loop variable is a positional local
// whose spelling is this file's business rather than the reader's.
func recordFail(pt graph.PropertyType, key string, depth int, tail string) (format, args string) {
	subject := "field %q" + strings.Repeat(" element", depth)
	return strconv.Quote("decode %s: " + subject + ": " + tail),
		strconv.Quote(string(pt)) + ", " + strconv.Quote(key)
}

// writeRecordValueDecode emits the statements that turn one driver value
// — src, an expression of static type any — into a value of goType, and
// answers the name of the local it bound the result to. Every arm binds
// one, so the caller assigns or takes the address of a name it did not
// have to predict.
//
// Recursive through list levels, because a record field may be declared
// LIST<...> and the driver hands every array back as []any whatever the
// elements are. A nested record is NOT recursed into here: it is its own
// entry in the encoding set with its own emitted helper, which narrowCall
// names — inlining it would emit the same body once per reference.
//
// key and depth are what the emitted failures are worded from — see
// recordFail — and are carried down the list levels unchanged apart from
// the depth, so an element that fails still names the declared field it
// belongs to.
func writeRecordValueDecode(b *strings.Builder, pt graph.PropertyType, key string, depth int, goType string, width graph.PropertyType, src, indent string, next func() string) string {
	site := decodeSite{
		zero: "out",
		fail: func(depth int, tail string) (format, args string) {
			return recordFail(pt, key, depth, tail)
		},
	}
	return writeValueDecode(b, site, depth, goType, width, src, indent, next)
}

// decodeSite is the two things a value-decode walk differs on between its
// callers: what a failing return hands back BESIDE the error, and how a
// failure is worded at a given list depth.
//
// Two fields rather than two walks. The record helper returns
// (RecordAlias, error) and words its failures around a declared field
// name; the union helper returns (any, error) and words its failures
// around a declared union. Everything between those two facts — the
// assertion to the driver carrier, the per-element descent, the checked
// narrowing and its error plumbing — is one rule, and a second copy of it
// would be a second chance for the two to disagree about, say, whether a
// nullable list element is asserted before or after its nil check.
type decodeSite struct {
	zero string
	fail func(depth int, tail string) (format, args string)
}

// writeValueDecode emits the statements that turn one driver value — src,
// an expression of static type any — into a value of goType, and answers
// the name of the local it bound the result to. Every arm binds one, so
// the caller assigns or takes the address of a name it did not have to
// predict.
//
// Recursive through list levels, because the driver hands every array
// back as []any whatever the elements are. A nested DECLARED record and a
// nested DECLARED union are NOT recursed into here: each is its own entry
// in its own encoding set with its own emitted helper, which this names —
// inlining either would emit the same body once per reference.
func writeValueDecode(b *strings.Builder, site decodeSite, depth int, goType string, width graph.PropertyType, src, indent string, next func() string) string {
	if codegen.IsDeclaredUnion(goType, width) {
		// A closed union carries as `any`, so it would otherwise take the
		// shapeless arm below and be handed over undispatched. Asked
		// FIRST, and on the pair rather than on the text, because the
		// text it shares with ANY VALUE is the one arm that must keep
		// assigning bare.
		out := next()
		fmt.Fprintf(b, "%s%s, err := decode%s(%s)\n", indent, out, codegen.UnionHelperSuffix(width), src)
		writeValueDecodeFail(b, site, depth, indent)
		return out
	}
	if !ridesADriverCarrier(goType) {
		// ANY VALUE has no carrier to assert against: `x.(any)` is false
		// for exactly the null that width exists to hold. The driver
		// value already IS the `any` the caller is handed.
		out := next()
		fmt.Fprintf(b, "%s%s := %s\n", indent, out, src)
		return out
	}
	carrier := driverCarrier(goType)
	held := next()
	fmt.Fprintf(b, "%s%s, ok := %s.(%s)\n", indent, held, src, carrier)
	fmt.Fprintf(b, "%sif !ok {\n", indent)
	format, args := site.fail(depth, "expected "+carrier+", got %T")
	fmt.Fprintf(b, "%s\treturn %s, fmt.Errorf(%s, %s, %s)\n", indent, site.zero, format, args, src)
	fmt.Fprintf(b, "%s}\n", indent)
	return writeCarrierNarrow(b, site, depth, goType, width, carrier, held, indent, next)
}

// writeCarrierNarrow emits the narrowing of a value ALREADY held at its
// driver carrier down to the declared Go type, and answers the local it
// bound. Split from writeValueDecode because the union decode reaches it
// with the assertion already made: its type switch IS the assertion, and
// re-asserting inside an arm would emit a check the arm just proved.
func writeCarrierNarrow(b *strings.Builder, site decodeSite, depth int, goType string, width graph.PropertyType, carrier, held, indent string, next func() string) string {
	switch {
	case walksElements(goType, width):
		acc, idx, elem := next(), next(), next()
		fmt.Fprintf(b, "%s%s := make(%s, len(%s))\n", indent, acc, goType, held)
		fmt.Fprintf(b, "%sfor %s, %s := range %s {\n", indent, idx, elem, held)
		if unionElementIsNullable(goType, width) {
			// The one element shape whose null cannot be left to the
			// value walk: a union's decode dispatches on the wire shape,
			// and a nil element belongs to no member. On every other
			// element type the null is either impossible or already
			// carried by a star this walk asserts through.
			fmt.Fprintf(b, "%s\tif %s == nil {\n%s\t\tcontinue\n%s\t}\n", indent, elem, indent, indent)
		}
		got := writeValueDecode(b, site, depth+1, strings.TrimPrefix(goType, "[]"), width.Elem(), elem, indent+"\t", next)
		fmt.Fprintf(b, "%s\t%s[%s] = %s\n", indent, acc, idx, got)
		fmt.Fprintf(b, "%s}\n", indent)
		return acc
	case isNeutralCarrier(goType):
		out := next()
		fmt.Fprintf(b, "%s%s := %s\n", indent, out, narrowExpr(goType, held))
		return out
	case carrier != goType:
		out := next()
		fmt.Fprintf(b, "%s%s, err := %s\n", indent, out, narrowCall(goType, width, held))
		writeValueDecodeFail(b, site, depth, indent)
		return out
	}
	return held
}

// writeValueDecodeFail emits the `if err != nil` that wraps a fallible
// narrowing's error in the site's own wording. One function because the
// three fallible arms — the union dispatch, the checked numeric narrow
// and the nested record decode — all report through %w and differ in
// nothing else.
func writeValueDecodeFail(b *strings.Builder, site decodeSite, depth int, indent string) {
	fmt.Fprintf(b, "%sif err != nil {\n", indent)
	format, args := site.fail(depth, "%w")
	fmt.Fprintf(b, "%s\treturn %s, fmt.Errorf(%s, %s, err)\n", indent, site.zero, format, args)
	fmt.Fprintf(b, "%s}\n", indent)
}

// walksElements reports whether a decode has to narrow a driver []any
// element by element rather than hand it over whole.
//
// isSliceType is the ordinary answer and excludes []any, because an ANY
// element is already the value the caller is handed and asserting it
// would fail on exactly the null that width carries. A LIST of a DECLARED
// UNION is the one []any that is not that case: its elements have a
// member set to be narrowed to, and handing the slice over whole would
// deliver the driver's widened int64 out of an INT32 member — the exact
// thing declaring the member list buys (spec §4).
func walksElements(goType string, width graph.PropertyType) bool {
	return isSliceType(goType) || isUnionList(goType, width)
}

// isUnionList reports whether a carrier text and the width beside it are a
// list whose ELEMENTS are closed unions.
//
// Both halves for IsDeclaredUnion's reason, one container out: `[]any` is
// also LIST<ANY VALUE>'s carrier and LIST<ANY PROPERTY VALUE>'s, and
// neither has a member set — a site reading the text alone would name a
// helper for every ANY-element list in the batch.
func isUnionList(goType string, width graph.PropertyType) bool {
	return goType == "[]"+codegen.UnionCarrierText && width.Kind() == graph.KindList &&
		codegen.IsDeclaredUnion(codegen.UnionCarrierText, width.Elem())
}

// unionElementIsNullable reports whether a walked list's elements are
// closed unions the schema permits to be NULL. Read off the width rather
// than off the text, because a union element carries no star: `any` holds
// its own null and typeMap.Property deliberately does not star it.
func unionElementIsNullable(goType string, width graph.PropertyType) bool {
	return walksElements(goType, width) && !width.ElemNotNull() &&
		codegen.IsDeclaredUnion(codegen.UnionCarrierText, width.Elem())
}

// recordFileImports answers which imports the emitted record file names,
// derived from the plan rather than grepped out of the rendered text: a
// declared field name is author text and can contain "time." or "fmt.",
// so a substring test over the body would gate an import on a schema's
// spelling. An import nothing names does not compile.
//
//   - fmt: every decode helper with at least one field reports a failure
//     through it. An encode-only file names it nowhere, and RECORD<>'s
//     decode has no field to report about.
//   - time: time.Time is the carrier for TIMESTAMP, and it appears in the
//     ALIAS whichever direction is emitted.
//   - dbtype: a neutral carrier (ADR 0033) asserts against its dbtype
//     counterpart, which only the DECODE direction does — the encode
//     direction names from<X>, and dbtype appears inside that helper's
//     own bridge file rather than here. UUID is a neutral carrier on the
//     same terms as the five temporal ones and needs no arm of its own,
//     because it reaches dbtype.UUID through the same emitted pair
//     (isNeutralCarrier records why the conversion-compatible one is
//     bridged anyway).
func recordFileImports(encodings []graph.PropertyType, uses map[graph.PropertyType]carrierUse, tm typeMap) (needFmt, needTime, needDbtype bool) {
	for _, pt := range encodings {
		use := uses[pt]
		plan, ok := codegen.RecordFields(pt.Fields(), tm.Property)
		if !ok {
			continue
		}
		if use.decode && len(plan) > 0 {
			needFmt = true
		}
		for _, f := range plan {
			leaf := leafType(f.GoType)
			if leaf == "time.Time" {
				needTime = true
			}
			if use.decode && isNeutralCarrier(leaf) {
				needDbtype = true
			}
		}
	}
	return needFmt, needTime, needDbtype
}
