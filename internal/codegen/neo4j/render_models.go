package neo4j

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
)

// renderModels emits models.go (spec §5.2). C2 emits one exported
// struct per schema NodeType and EdgeType (Phase Z order) plus an
// unexported decode<Name> helper. C5 adds two blocks:
//
//   - Marker methods on each candidate entity struct, one per edgeUnion
//     interface it participates in. Emitted between the struct
//     declaration and the decode helper so a reader following the entity
//     sees shape → sum-membership → decode.
//   - EdgeUnion interface declarations, per-query-column in
//     Input.Queries slice order sub-ordered by column position, with a
//     `//sumtype:decl` comment line above each.
//
// The import set is a template invariant on schema shape:
//
//   - dbtype: unconditional (decode helpers take dbtype.Node /
//     dbtype.Relationship)
//   - fmt iff any decode can fail, which every property can except a
//     nullable one of no declared shape: that arm is a Props lookup
//     whose miss is the schema's null, not an error to report
//   - neo4j iff any non-nullable property of a declared shape is decoded
//     (neo4j.GetProperty[T]); a property typed `any` never reaches it
//
// EdgeUnion emission adds no new import (the interface + marker methods
// live in this package; no cross-package reference emerges). A schema
// with zero entity types emits an empty body — package clause only —
// matching C1's byte-empty models.go (§7 "silently accepted").
func renderModels(pkg string, entities []codegen.Entity, prepared []codegen.Query, target driverTarget) []byte {
	if len(entities) == 0 {
		return []byte(codegen.Header() + `package ` + pkg + `
`)
	}

	// Collect edgeUnion interfaces across every query, preserving
	// Input.Queries slice order sub-ordered by column position.
	// markersByEntity maps entity-struct name -> ordered interface
	// names it satisfies, deduplicated so an entity that appears twice
	// in an EdgeKeys slice (impossible — resolver commits distinct
	// candidates) or across two per-query columns projecting the same
	// interface (impossible — per-query-column naming) still emits one
	// marker per interface participation.
	var unions []*codegen.EdgeUnion
	markersByEntity := make(map[string][]string)
	seenMarker := make(map[string]struct{})
	for _, p := range prepared {
		for _, u := range p.EdgeUnions {
			unions = append(unions, u)
			for _, cand := range u.Candidates {
				key := cand + "\x00" + u.InterfaceName
				if _, dup := seenMarker[key]; dup {
					continue
				}
				seenMarker[key] = struct{}{}
				markersByEntity[cand] = append(markersByEntity[cand], u.InterfaceName)
			}
		}
	}

	// fmt is unconditional once an entity exists: every decode helper
	// opens with the wire-label guard, whose mismatch arm is a
	// fmt.Errorf. It used to gate on a property whose read can fail,
	// which left a zero-property entity's helper importing nothing —
	// that helper now reports a wrong-labelled value like every other.
	anyProp := true
	anyNonNull := false
	anyTime := false
	for _, e := range entities {
		for _, f := range e.Fields {
			// neo4j is emitted for GetProperty, which a property of no
			// declared shape never reaches (ridesADriverCarrier) and a
			// nullable one reads round through the Props map. An import
			// nothing names does not compile.
			if !f.Nullable && ridesADriverCarrier(f.GoType) {
				anyNonNull = true
			}
			// A list property names its leaf type, not its slice
			// type, so an exact "time.Time" match misses
			// LIST<TIMESTAMP> ([]time.Time) and its nestings and
			// emits a struct field plus a decode assertion against
			// an unimported package. goTypeNeedsImports strips the
			// "[]" prefixes to the leaf; the dbtype half of its
			// answer is discarded because this file's dbtype import
			// is unconditional.
			if _, needTime := goTypeNeedsImports(f.GoType); needTime {
				anyTime = true
			}
		}
	}

	var b strings.Builder
	b.WriteString(codegen.Header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")

	// Each checked-narrowing helper is emitted only where something calls
	// it, so narrowFloat32 — the only thing in this file that names math —
	// gates the math import with it.
	narrowsInts, narrowsFloats := narrowsANumericWidth(entities, prepared)

	// Imports: dbtype is unconditional (every helper's argument type);
	// fmt gates on anyProp; math gates on narrowsFloats; time gates on
	// anyTime (TIMESTAMP property); neo4j gates on anyNonNull.
	// Alphabetical: fmt, math, time, then external neo4j / dbtype.
	b.WriteString("import (\n")
	if anyProp {
		b.WriteString("\t\"fmt\"\n")
	}
	if narrowsFloats {
		b.WriteString("\t\"math\"\n")
	}
	if anyTime {
		b.WriteString("\t\"time\"\n")
	}
	if anyProp || narrowsFloats || anyTime {
		b.WriteString("\n")
	}
	if anyNonNull {
		b.WriteString("\t\"" + target.neo4jImport + "\"\n")
	}
	b.WriteString("\t\"" + target.dbtypeImport + "\"\n")
	b.WriteString(")\n\n")

	for i, e := range entities {
		if i > 0 {
			b.WriteString("\n")
		}
		writeEntityStruct(&b, e)
		if markers := markersByEntity[e.Name]; len(markers) > 0 {
			b.WriteString("\n")
			for _, iface := range markers {
				fmt.Fprintf(&b, "func (%s) is%s() {}\n", e.Name, iface)
			}
		}
		b.WriteString("\n")
		writeEntityDecodeHelper(&b, e)
	}

	// The site-named record aliases (spec §2.1), after the entity blocks
	// they are named from and not in record_neo4j.go beside the digest
	// carriers: that file is emitted from the encoding set alone, which
	// has no entity in it to take a name from.
	for _, s := range codegen.RecordSiteAliases(entities) {
		text, ok := codegen.RecordStructText(s.Width.Fields(), typeMap{}.Property)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "\n// %s is the record type of %s property %s.\n", s.Name, s.Entity, s.Property)
		fmt.Fprintf(&b, "type %s = %s\n", s.Name, text)
	}

	writeNarrowHelpers(&b, narrowsInts, narrowsFloats)

	// EdgeUnion interface declarations, appended after entity blocks,
	// one per synthesised per-query-column interface in emission order.
	for _, u := range unions {
		b.WriteString("\n//sumtype:decl\n")
		fmt.Fprintf(&b, "type %s interface{ is%s() }\n", u.InterfaceName, u.InterfaceName)
	}
	return []byte(b.String())
}

// writeEntityStruct emits the exported struct declaration for one entity.
// Zero-property entities emit an empty struct declaration (§7 "silently
// accepted"). Doc comment names the source-side axis (labels or edge key).
func writeEntityStruct(b *strings.Builder, e codegen.Entity) {
	if e.Kind == codegen.EntityNode {
		fmt.Fprintf(b, "// %s corresponds to the %s node type.\n", e.Name, e.DocAxis)
	} else {
		fmt.Fprintf(b, "// %s corresponds to the %s.\n", e.Name, e.DocAxis)
	}
	fmt.Fprintf(b, "type %s struct {\n", e.Name)
	for _, f := range e.Fields {
		if f.Nullable {
			fmt.Fprintf(b, "\t%s *%s\n", f.Field, f.GoType)
		} else {
			fmt.Fprintf(b, "\t%s %s\n", f.Field, f.GoType)
		}
	}
	b.WriteString("}\n")
}

// writeEntityDecodeHelper emits the unexported decode<Name> helper for
// one entity. It opens with the wire-label guard (writeLabelGuard) and
// then reads the properties. A property of a declared shape reads
// through the driver: nullable ones by direct Props lookup + type
// assertion (three-way outcome), non-nullable ones through
// neo4j.GetProperty[T] (missing key is a decode error). A property of no
// declared shape reads through the Props map on both arms — see
// writeShapelessFieldDecode.
func writeEntityDecodeHelper(b *strings.Builder, e codegen.Entity) {
	var carrier, arg string
	if e.Kind == codegen.EntityNode {
		carrier = "dbtype.Node"
		arg = "node"
	} else {
		carrier = "dbtype.Relationship"
		arg = "rel"
	}
	fmt.Fprintf(b, "// decode%s decodes a driver %s into a %s struct,\n", e.Name, carrier, e.Name)
	b.WriteString("// enforcing the wire label and the per-property nullability the\n")
	b.WriteString("// schema declares.\n")
	fmt.Fprintf(b, "func decode%s(%s %s) (%s, error) {\n", e.Name, arg, carrier, e.Name)
	writeLabelGuard(b, e, arg)
	fmt.Fprintf(b, "\tvar out %s\n", e.Name)
	// The position counted is the local's, not the field's. Both decode
	// arms bind a value<i> only where the property is required — the
	// nullable ones bind their own generator-fixed locals inside an `if`
	// block and consume no position — so counting fields leaves a gap at
	// every nullable property, and generated code that skips from value1
	// to value3 reads as though a statement was deleted.
	position := 0
	for _, f := range e.Fields {
		writeEntityFieldDecode(b, e, position, f, arg)
		if !f.Nullable {
			position++
		}
	}
	b.WriteString("\treturn out, nil\n")
	b.WriteString("}\n")
}

// writeLabelGuard emits the check that the driver value the helper was
// handed carries the labels the schema keys this entity on. Without it a
// decoder reads whatever Props it is given: hand decodePerson a Post
// whose property names overlap and it fills a Person and reports no
// error. What arrives is a dbtype.Node the driver built, not a value
// gqlc constructed, so what the resolver asked for does not bound what
// comes back — the decoder is a boundary and validates. AGE's decoders
// have always done this (internal/codegen/age's writeEntityDecoder);
// bd gqlc-2h9w is the ruling that the two backends may not disagree
// about it.
//
// A mismatch is an error naming both the label wanted and what the value
// carried. There is no correct value to return instead, so a silent
// zero would trade one silent failure for another.
//
// The two carriers differ in what they can hold. A node carries a set,
// which may hold labels beyond the key set the schema names — a
// deployment's own "Archived", say — so the check is containment, one
// pass per key label. A relationship carries exactly one type, so the
// check is equality. An entity keyed on more than one label is a node
// in every schema this backend has seen; on the relationship arm it
// emits one equality per key label, which no single Type satisfies, and
// that is the honest reading of a relationship the wire cannot carry.
//
// Locals are positional (has0, has1) or fixed (label), never derived
// from anything the schema names, for the reason writeSliceNarrow gives.
func writeLabelGuard(b *strings.Builder, e codegen.Entity, arg string) {
	labels := e.Labels.Split()
	if e.Kind == codegen.EntityEdge {
		labels = e.EdgeKey.KeyLabels.Split()
	}
	for i, label := range labels {
		if e.Kind == codegen.EntityEdge {
			fmt.Fprintf(b, "\tif %s.Type != %q {\n", arg, label)
			fmt.Fprintf(b, "\t\treturn %s{}, fmt.Errorf(\"decode %s: expected a relationship of type %%q, got %%q\", %q, %s.Type)\n",
				e.Name, e.Name, label, arg)
			b.WriteString("\t}\n")
			continue
		}
		has := fmt.Sprintf("has%d", i)
		fmt.Fprintf(b, "\t%s := false\n", has)
		fmt.Fprintf(b, "\tfor _, label := range %s.Labels {\n", arg)
		fmt.Fprintf(b, "\t\tif label == %q {\n", label)
		fmt.Fprintf(b, "\t\t\t%s = true\n", has)
		b.WriteString("\t\t\tbreak\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
		fmt.Fprintf(b, "\tif !%s {\n", has)
		fmt.Fprintf(b, "\t\treturn %s{}, fmt.Errorf(\"decode %s: expected a node labelled %%q, got labels %%q\", %q, %s.Labels)\n",
			e.Name, e.Name, label, arg)
		b.WriteString("\t}\n")
	}
}

// writeEntityFieldDecode emits the decode of the property at index i.
// Three paths. A property of no declared shape has no carrier to assert
// against and is delegated to writeShapelessFieldDecode. Otherwise —
// nullable path: Props lookup + type assertion against the driver's
// carrier + narrow-convert into a local of the emitted Go type +
// address-of-local into the pointer field. Non-nullable path:
// neo4j.GetProperty[<carrier>] + narrow-convert. The property key is the
// source property name (Property.Name), not the derived field name — the
// driver map is keyed on the schema-side name. Extended at C3 to cover
// DATE (dbtype.Date carrier) and TIMESTAMP (time.Time carrier);
// FLOAT32's nullable arm now narrows correctly (was a latent bug, no
// fixture exercised it before C3).
//
// The local the value lands in is positional. A name derived from the
// property is any identifier the schema author chose, including one this
// scope already holds: a property named out, err, or after the carrier
// argument itself would emit a redeclaration, and generation would still
// exit 0 because the format gate only parses.
func writeEntityFieldDecode(b *strings.Builder, e codegen.Entity, i int, f codegen.EntityField, arg string) {
	if !ridesADriverCarrier(f.GoType) {
		writeShapelessFieldDecode(b, e, i, f, arg)
		return
	}
	carrier := driverCarrier(f.GoType)
	if f.Nullable {
		fmt.Fprintf(b, "\tif v, ok := %s.Props[%q]; ok {\n", arg, f.PropName)
		fmt.Fprintf(b, "\t\ts, ok := v.(%s)\n", carrier)
		b.WriteString("\t\tif !ok {\n")
		fmt.Fprintf(b, "\t\t\treturn %s{}, fmt.Errorf(\"decode %s.%s: property %%q: expected %s, got %%T\", %q, v)\n", e.Name, e.Name, f.Field, carrier, f.PropName)
		b.WriteString("\t\t}\n")
		switch {
		case isSliceType(f.GoType):
			writeSliceNarrow(b, e, f, f.GoType, "s", "narrowed", "\t\t")
			fmt.Fprintf(b, "\t\tout.%s = &narrowed\n", f.Field)
		case isTemporalCarrier(f.GoType):
			fmt.Fprintf(b, "\t\tnarrowed := %s\n", narrowExpr(f.GoType, "s"))
			fmt.Fprintf(b, "\t\tout.%s = &narrowed\n", f.Field)
		case carrier != f.GoType:
			fmt.Fprintf(b, "\t\tnarrowed, err := %s\n", narrowCall(f.GoType, f.Width, "s"))
			fmt.Fprintf(b, "\t\tif err != nil {\n")
			fmt.Fprintf(b, "\t\t\treturn %s{}, fmt.Errorf(\"decode %s.%s: %%w\", err)\n", e.Name, e.Name, f.Field)
			b.WriteString("\t\t}\n")
			fmt.Fprintf(b, "\t\tout.%s = &narrowed\n", f.Field)
		default:
			fmt.Fprintf(b, "\t\tout.%s = &s\n", f.Field)
		}
		b.WriteString("\t}\n")
		return
	}
	value := valueName(i)
	fmt.Fprintf(b, "\t%s, err := neo4j.GetProperty[%s](%s, %q)\n", value, carrier, arg, f.PropName)
	b.WriteString("\tif err != nil {\n")
	fmt.Fprintf(b, "\t\treturn %s{}, fmt.Errorf(\"decode %s.%s: %%w\", err)\n", e.Name, e.Name, f.Field)
	b.WriteString("\t}\n")
	switch {
	case isSliceType(f.GoType):
		narrowed := value + "s"
		writeSliceNarrow(b, e, f, f.GoType, value, narrowed, "\t")
		fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, narrowed)
	case isTemporalCarrier(f.GoType):
		fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, narrowExpr(f.GoType, value))
	case carrier != f.GoType:
		narrowed := value + "n"
		fmt.Fprintf(b, "\t%s, err := %s\n", narrowed, narrowCall(f.GoType, f.Width, value))
		b.WriteString("\tif err != nil {\n")
		fmt.Fprintf(b, "\t\treturn %s{}, fmt.Errorf(\"decode %s.%s: %%w\", err)\n", e.Name, e.Name, f.Field)
		b.WriteString("\t}\n")
		fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, narrowed)
	default:
		fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, value)
	}
}

// writeShapelessFieldDecode emits the read of a property of no declared
// shape — ANY VALUE, whose emitted Go type is `any`. It is the one width
// that takes neither of the driver's two entry points, so it goes
// through the Props map directly (see ridesADriverCarrier).
//
// Absence is the schema's null on the nullable arm and a decode failure
// on the non-nullable one, worded the way neo4j.GetProperty words its
// own miss so that a caller cannot tell which arm reported it.
func writeShapelessFieldDecode(b *strings.Builder, e codegen.Entity, i int, f codegen.EntityField, arg string) {
	if f.Nullable {
		fmt.Fprintf(b, "\tif v, ok := %s.Props[%q]; ok {\n", arg, f.PropName)
		fmt.Fprintf(b, "\t\tout.%s = &v\n", f.Field)
		b.WriteString("\t}\n")
		return
	}
	value := valueName(i)
	fmt.Fprintf(b, "\t%s, ok := %s.Props[%q]\n", value, arg, f.PropName)
	b.WriteString("\tif !ok {\n")
	fmt.Fprintf(b, "\t\treturn %s{}, fmt.Errorf(\"decode %s.%s: could not find any property named %%s\", %q)\n", e.Name, e.Name, f.Field, f.PropName)
	b.WriteString("\t}\n")
	fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, value)
}

// isSliceType reports whether an emitted Go type is a slice this package
// has to walk element by element. Two are excluded, and they are exactly
// the two slice shapes neo4j.PropertyValue admits, so each arrives as
// itself and asserting straight to it is correct:
//
//   - []byte, because BYTES is the one width the driver hands back as a
//     Go slice of its own;
//   - []any, because that is the driver's carrier for every other array,
//     so a property declared LIST<ANY VALUE> (or bare LIST) is already
//     the value the caller is handed.
//
// The []any exclusion is not an optimisation. The walk narrows by
// asserting each element to its carrier, and the carrier of an `any`
// element is `any`: a type assertion on a nil interface value is false
// whatever type it names, so walking a []any would fail the whole decode
// on exactly the null element that width exists to carry — while AGE,
// whose agtypeValue maps null to nil, hands the same graph value back
// intact. Not walking is what keeps the two backends decoding the same
// value.
func isSliceType(goType string) bool {
	return strings.HasPrefix(goType, "[]") && goType != "[]byte" && goType != "[]any"
}

// ridesADriverCarrier reports whether a property's decode reaches the
// driver's constrained generics at all. A property of no declared shape
// does not. `any` is a member of neither neo4j.PropertyValue nor
// neo4j.RecordValue, so neo4j.GetProperty[any] does not compile; and
// `v.(any)` is false for exactly the null such a property is allowed to
// hold. What is left is the Props map itself, whose value is already the
// `any` the caller is handed — which is why this also moves the fmt and
// neo4j import gates.
func ridesADriverCarrier(goType string) bool {
	return driverCarrier(goType) != "any"
}

// writeSliceNarrow emits the walk that turns the driver's []any into the
// slice type the schema declared, binding it to dst.
//
// The driver has no narrower carrier to offer. neo4j.PropertyValue admits
// []byte and []any and nothing else with a slice shape, and the hydrator
// builds every non-byte array as []any whatever the elements turned out
// to be, so a LIST<STRING> property arrives as []any of string and the
// []string the caller reads is this package's to build.
//
// The walk is FLAT, and only a flat list can reach it. Both callers pass a
// declared property's GoType, and Phase Z refuses a nested-list stored
// property before rendering (ErrUnstorableProperty, ADR 0035) because the
// neo4j server will not hold one. A nested list arriving as a QUERY VALUE
// is served and still decoded — by render_queries.go, walking its own
// local family (inner<n>/innerAcc<n>), never through here.
//
// The locals carry a `0` suffix rather than a schema name so a property
// whose name collides with one of them cannot emit a redeclaration. The
// digit is a fixed part of the name, not a depth counter.
func writeSliceNarrow(b *strings.Builder, e codegen.Entity, f codegen.EntityField, sliceType, src, dst, indent string) {
	elem := strings.TrimPrefix(sliceType, "[]")
	fail := fmt.Sprintf("return %s{}, fmt.Errorf(\"decode %s.%s: property %%q", e.Name, e.Name, f.Field)

	fmt.Fprintf(b, "%s%s := make(%s, 0, len(%s))\n", indent, dst, sliceType, src)
	fmt.Fprintf(b, "%sfor i0, elem0 := range %s {\n", indent, src)
	body := indent + "\t"
	carrier := driverCarrier(elem)
	fmt.Fprintf(b, "%sv0, ok := elem0.(%s)\n", body, carrier)
	fmt.Fprintf(b, "%sif !ok {\n", body)
	fmt.Fprintf(b, "%s\t%s element %%d: expected %s, got %%T\", %q, i0, elem0)\n", body, fail, carrier, f.PropName)
	fmt.Fprintf(b, "%s}\n", body)
	switch {
	case isTemporalCarrier(elem):
		fmt.Fprintf(b, "%s%s = append(%s, %s)\n", body, dst, dst, narrowExpr(elem, "v0"))
	case carrier != elem:
		fmt.Fprintf(b, "%sv0n, err := %s\n", body, narrowCall(elem, f.Width.Elem(), "v0"))
		fmt.Fprintf(b, "%sif err != nil {\n", body)
		fmt.Fprintf(b, "%s\t%s element %%d: %%w\", %q, i0, err)\n", body, fail, f.PropName)
		fmt.Fprintf(b, "%s}\n", body)
		fmt.Fprintf(b, "%s%s = append(%s, v0n)\n", body, dst, dst)
	default:
		fmt.Fprintf(b, "%s%s = append(%s, v0)\n", body, dst, dst)
	}
	fmt.Fprintf(b, "%s}\n", indent)
}
