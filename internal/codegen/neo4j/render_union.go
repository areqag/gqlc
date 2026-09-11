package neo4j

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// renderUnionHelpers emits union_neo4j.go: whichever of the four
// conversion helpers the emission sites call, per distinct closed-union
// encoding the batch reaches.
//
// encodings is codegen.UnionEncodings' answer, so the file's order is the
// canonical-encoding order both backends share and is byte-stable across
// runs. uses is conversionUses' union half, so a helper is emitted exactly
// where something calls it — an unexported function nothing calls fails
// the emitted package's own lint fence, which is a red fixture rather than
// a dead line.
//
// No carrier alias, which is the one thing the record file has that this
// one does not: a union carries as `any` (spec §4), a predeclared name
// every site already spells.
func renderUnionHelpers(pkg string, encodings []graph.PropertyType, uses map[graph.PropertyType]carrierUse, target driverTarget) []byte {
	var body strings.Builder
	for _, pt := range encodings {
		use := uses[pt]
		suffix := codegen.UnionHelperSuffix(pt)
		members, ok := codegen.UnionMembers(pt, typeMap{}.Property)
		if !ok {
			// Unreachable: a union a member of which this backend cannot
			// carry is refused at preparation, before any emission walk
			// builds the encoding set. Skipping rather than panicking,
			// because a helper the batch never calls is the only thing
			// lost and generation has no channel here to report through.
			continue
		}
		// The plain encode helper stands under both other encode
		// directions: the Ptr wrapper nil-checks and calls it, and the
		// List wrapper calls it per element.
		if use.encode || use.encodePtr || use.list {
			writeUnionEncode(&body, pt, suffix, members)
		}
		if use.encodePtr {
			fmt.Fprintf(&body, `
// encode%[1]sPtr binds a nullable %[2]s parameter: a nil pointer is the
// Cypher null the schema's nullability declared, and is the call site's
// answer rather than the member set's — no member matches nil, so
// encode%[1]s would refuse it.
func encode%[1]sPtr(v *%[3]s) (any, error) {
	if v == nil {
		return nil, nil
	}
	return encode%[1]s(*v)
}
`, suffix, pt, codegen.UnionCarrierText)
		}
		if use.list {
			fmt.Fprintf(&body, `
// encode%[1]sList validates a list of %[2]s parameters element by
// element. A nil element binds the Cypher null the schema's element
// nullability declared; whether that nullability was declared is the
// schema's, and a list whose elements are NOT NULL reaches the same
// nil having lost the value the caller meant to send — so it is
// admitted here and refused by the server's own constraint rather than
// by a check this helper has no declaration to read.
func encode%[1]sList(v []%[3]s) ([]any, error) {
	out := make([]any, len(v))
	for i := range v {
		if v[i] == nil {
			continue
		}
		elem, err := encode%[1]s(v[i])
		if err != nil {
			return nil, fmt.Errorf(%[4]s, %[5]s, i, err)
		}
		out[i] = elem
	}
	return out, nil
}
`, suffix, pt, codegen.UnionCarrierText,
				strconv.Quote("encode %s element %d: %w"), strconv.Quote(string(pt)))
		}
		if use.listPtr {
			fmt.Fprintf(&body, `
// encode%[1]sListPtr binds a nullable list of %[2]s: a nil pointer is the
// Cypher null the schema's nullability declared, not an empty list.
func encode%[1]sListPtr(v *[]%[3]s) (any, error) {
	if v == nil {
		return nil, nil
	}
	return encode%[1]sList(*v)
}
`, suffix, pt, codegen.UnionCarrierText)
		}
		if use.decode {
			writeUnionDecode(&body, pt, suffix, members)
		}
	}

	needTime, needDbtype := unionFileImports(encodings, uses)

	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"fmt\"\n")
	if needTime {
		b.WriteString("\t\"time\"\n")
	}
	if needDbtype {
		b.WriteString("\n\t\"" + target.dbtypeImport + "\"\n")
	}
	b.WriteString(")\n")
	b.WriteString(body.String())
	return []byte(b.String())
}

// writeUnionEncode emits encode<Suffix>: the bind-time validation of an
// `any` against the declared member set, plus the widening each matched
// member owes before the driver packs it.
//
// A type switch over the MEMBER CARRIERS rather than over the wire
// families, because this side has the caller's own Go value: a caller
// holding an int32 for an INT32 member is matched by that member and by
// no other. The arms are pairwise distinct Go types because the admission
// rule already made their wire families pairwise distinct and a carrier
// folds onto a family (spec §4) — two members sharing a Go type would
// have shared a family and refused the union at preparation.
//
// Each matched member is bound by paramBindExpr, the SAME renderer that
// binds a top-level parameter, because a union member IS in the parameter
// position once its type is known. Two renderers would be two chances to
// disagree about, say, whether a DATE member is fromDate or a bare value.
//
// A member's NOT NULL has no effect here and that is a recorded decision
// rather than an omission (spec §4): whether nil is legal is the CALL
// SITE's question — a nullable position nil-checks through the Ptr
// wrapper, and a non-nullable one lets the refusal below report it.
func writeUnionEncode(b *strings.Builder, pt graph.PropertyType, suffix string, members []codegen.UnionMemberPlan) {
	fmt.Fprintf(b, `
// encode%s validates a value against the member set %s declares, and
// widens the matched member to the carrier this driver packs. A value
// outside the set is refused by name, which is what declaring the
// members buys over ADR 0020's open union.
func encode%s(v %s) (any, error) {
`, suffix, pt, suffix, codegen.UnionCarrierText)
	b.WriteString("\tswitch t := v.(type) {\n")
	for _, m := range members {
		fmt.Fprintf(b, "\tcase %s:\n", m.GoType)
		bind := paramBindExpr(codegen.Param{GoType: m.GoType, Width: m.Width}, "t")
		fmt.Fprintf(b, "\t\treturn %s, nil\n", bind)
	}
	b.WriteString("\t}\n")
	format, args := unionFail(pt, "encode", 0, "no member carries %T")
	fmt.Fprintf(b, "\treturn nil, fmt.Errorf(%s, %s, v)\n}\n", format, args)
}

// writeUnionDecode emits decode<Suffix>: the dispatch of a driver value
// onto the one member its WIRE SHAPE belongs to, narrowed to that
// member's declared width.
//
// A type switch over the driver carriers, which is the whole point of the
// admission rule: decode has the wire shape and nothing else to dispatch
// on, so the members were required to be pairwise distinct there before
// this helper could be emitted at all. The narrowing is what the member
// list buys — an INT32 member comes back int32, not the driver's widened
// int64 (spec §4).
//
// A value of no member's shape is refused rather than handed over as
// `any`. That includes nil: no member's carrier is nil, so a null
// arriving where the call site did not nil-check is a decode failure
// naming the union, which is the same three-way outcome an entity
// property already gives.
func writeUnionDecode(b *strings.Builder, pt graph.PropertyType, suffix string, members []codegen.UnionMemberPlan) {
	fmt.Fprintf(b, `
// decode%s dispatches a driver value onto the member of %s whose wire
// shape it arrived as, and narrows it to that member's declared width.
func decode%s(v %s) (any, error) {
`, suffix, pt, suffix, codegen.UnionCarrierText)
	site := decodeSite{
		zero: "nil",
		fail: func(depth int, tail string) (format, args string) {
			return unionFail(pt, "decode", depth, tail)
		},
	}
	// One counter across the whole helper rather than a per-arm one, so no
	// two locals in any of the nested scopes can share a name. Positional
	// for the reason writeRecordDecode's are, and starting past `t` so the
	// switch guard cannot be shadowed.
	n := 0
	next := func() string { n++; return fmt.Sprintf("v%d", n) }
	b.WriteString("\tswitch t := v.(type) {\n")
	for _, m := range members {
		carrier := driverCarrier(m.GoType)
		fmt.Fprintf(b, "\tcase %s:\n", carrier)
		got := writeCarrierNarrow(b, site, 0, m.GoType, m.Width, carrier, "t", "\t\t", next)
		fmt.Fprintf(b, "\t\treturn %s, nil\n", got)
	}
	b.WriteString("\t}\n")
	format, args := unionFail(pt, "decode", 0, "no member carries %T")
	fmt.Fprintf(b, "\treturn nil, fmt.Errorf(%s, %s, v)\n}\n", format, args)
}

// unionFail renders one union conversion failure as the arguments of a
// call: a Go string literal holding the format, then the literal it
// interpolates. tail is the generator-owned wording of what went wrong
// and may carry its own verbs, whose values the caller appends.
//
// The union's canonical encoding is an ARGUMENT rather than text pasted
// into the format, and that is the whole point of this function, for
// recordFail's reason one level out: a union encoding can hold author
// text — a RECORD member carries its field names — so a name holding a
// quote would close the literal early and emit a file that does not
// parse, and one holding a '%' would emit a verb the call has no argument
// for, which `go vet` of the generated package fails.
//
// depth is how many list levels were entered to reach the value, so a
// failure inside a member's list still names the union it belongs to.
func unionFail(pt graph.PropertyType, direction string, depth int, tail string) (format, args string) {
	subject := "%s" + strings.Repeat(" element", depth)
	return strconv.Quote(direction + " " + subject + ": " + tail), strconv.Quote(string(pt))
}

// unionFileImports answers which imports beyond fmt the emitted union
// file names, derived from the member plans rather than grepped out of
// the rendered text: a record member's field name is author text and can
// contain "time." or "dbtype.", so a substring test over the body would
// gate an import on a schema's spelling. An import nothing names does not
// compile.
//
//   - fmt is unconditional and so is not answered here: every emitted
//     helper in this file reports a refusal through it, and the file is
//     not emitted at all when no helper is.
//   - time: time.Time is TIMESTAMP's carrier, and it is named as a case
//     type in whichever direction is emitted.
//   - dbtype: the neutral temporal carriers dispatch on their dbtype
//     counterparts, which only the decode direction does — the encode
//     direction names from<X>, whose own file holds the dbtype mention.
func unionFileImports(encodings []graph.PropertyType, uses map[graph.PropertyType]carrierUse) (needTime, needDbtype bool) {
	for _, pt := range encodings {
		use := uses[pt]
		members, ok := codegen.UnionMembers(pt, typeMap{}.Property)
		if !ok {
			continue
		}
		for _, m := range members {
			leaf := leafType(m.GoType)
			if leaf == "time.Time" {
				needTime = true
			}
			if use.decode && isTemporalCarrier(leaf) {
				needDbtype = true
			}
		}
	}
	return needTime, needDbtype
}
