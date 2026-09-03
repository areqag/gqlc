package neo4j

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// recordAliasName is the unexported type alias one declared record's
// carrier is spelled as inside the emitted package.
//
// An ALIAS, never a definition. The struct text is what every model
// field, row field and parameter of that record is declared with, and a
// defined type would be a DIFFERENT Go type from all of them — the
// helpers would not accept the values the rest of the package holds. The
// alias exists only so the helper signatures and the `var out` line do
// not carry a fifth and sixth copy of a multi-line struct text.
//
// Derived from RecordHelperSuffix rather than hashing again, so the
// carrier and its two helpers cannot come from different digests: the
// suffix is "Record"+hex, and lowering its first byte is what makes the
// name unexported.
func recordAliasName(pt graph.PropertyType) string {
	suffix := codegen.RecordHelperSuffix(pt)
	return strings.ToLower(suffix[:1]) + suffix[1:]
}

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
		alias := recordAliasName(pt)
		suffix := codegen.RecordHelperSuffix(pt)
		// Every encoding in the set is reached from some position, so
		// every alias is named by some signature below and none is a
		// declaration nothing uses.
		text, ok := codegen.RecordStructText(pt.Fields(), typeMap{}.Property)
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
			writeRecordEncode(&body, pt, suffix, alias)
		}
		if use.encodePtr {
			fmt.Fprintf(&body, `
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
		if use.list {
			fmt.Fprintf(&body, `
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
		if use.listPtr {
			fmt.Fprintf(&body, `
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
		if use.decode {
			writeRecordDecode(&body, pt, suffix, alias)
		}
	}

	needFmt, needTime, needDbtype := recordFileImports(encodings, uses)

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
func writeRecordEncode(b *strings.Builder, pt graph.PropertyType, suffix, alias string) {
	plan, ok := codegen.RecordFields(pt.Fields(), typeMap{}.Property)
	if !ok {
		return
	}
	fmt.Fprintf(b, "\n// encode%s builds the Cypher map a %s binds as.\n", suffix, alias)
	fmt.Fprintf(b, "func encode%s(v %s) map[string]any {\n", suffix, alias)
	if len(plan) == 0 {
		// RECORD<> carries struct{}, so the parameter is named but never
		// read. An empty composite literal keeps the signature uniform
		// with every other encode helper.
		b.WriteString("\treturn map[string]any{}\n}\n")
		return
	}
	b.WriteString("\treturn map[string]any{\n")
	for _, f := range plan {
		bind := paramBindExpr(codegen.Param{
			GoType:   f.GoType,
			Nullable: f.Nullable,
			Width:    f.Width,
		}, "v."+f.Field)
		fmt.Fprintf(b, "\t\t%q: %s,\n", f.Key, bind)
	}
	b.WriteString("\t}\n}\n")
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
func writeRecordDecode(b *strings.Builder, pt graph.PropertyType, suffix, alias string) {
	plan, ok := codegen.RecordFields(pt.Fields(), typeMap{}.Property)
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
	format, args := recordFail(pt, key, depth, "expected "+carrier+", got %T")
	fmt.Fprintf(b, "%s\treturn out, fmt.Errorf(%s, %s, %s)\n", indent, format, args, src)
	fmt.Fprintf(b, "%s}\n", indent)

	switch {
	case isSliceType(goType):
		acc, idx, elem := next(), next(), next()
		fmt.Fprintf(b, "%s%s := make(%s, len(%s))\n", indent, acc, goType, held)
		fmt.Fprintf(b, "%sfor %s, %s := range %s {\n", indent, idx, elem, held)
		got := writeRecordValueDecode(b, pt, key, depth+1, strings.TrimPrefix(goType, "[]"), width.Elem(), elem, indent+"\t", next)
		fmt.Fprintf(b, "%s\t%s[%s] = %s\n", indent, acc, idx, got)
		fmt.Fprintf(b, "%s}\n", indent)
		return acc
	case isTemporalCarrier(goType):
		out := next()
		fmt.Fprintf(b, "%s%s := %s\n", indent, out, narrowExpr(goType, held))
		return out
	case carrier != goType:
		out := next()
		fmt.Fprintf(b, "%s%s, err := %s\n", indent, out, narrowCall(goType, width, held))
		fmt.Fprintf(b, "%sif err != nil {\n", indent)
		failFormat, failArgs := recordFail(pt, key, depth, "%w")
		fmt.Fprintf(b, "%s\treturn out, fmt.Errorf(%s, %s, err)\n", indent, failFormat, failArgs)
		fmt.Fprintf(b, "%s}\n", indent)
		return out
	}
	return held
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
//   - dbtype: the neutral temporal carriers assert against their dbtype
//     counterparts, which only the decode direction does — the encode
//     direction names from<X>, and dbtype appears inside that helper's
//     own file.
func recordFileImports(encodings []graph.PropertyType, uses map[graph.PropertyType]carrierUse) (needFmt, needTime, needDbtype bool) {
	for _, pt := range encodings {
		use := uses[pt]
		plan, ok := codegen.RecordFields(pt.Fields(), typeMap{}.Property)
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
			if use.decode && isTemporalCarrier(leaf) {
				needDbtype = true
			}
		}
	}
	return needFmt, needTime, needDbtype
}
