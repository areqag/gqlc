package codegen

import (
	"fmt"
	"strings"
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
//   - fmt iff any property is decoded (decode-error wrapping)
//   - neo4j iff any non-nullable property is decoded (neo4j.GetProperty[T])
//
// EdgeUnion emission adds no new import (the interface + marker methods
// live in this package; no cross-package reference emerges). A schema
// with zero entity types emits an empty body — package clause only —
// matching C1's byte-empty models.go (§7 "silently accepted").
func renderModels(pkg string, entities []preparedEntity, prepared []preparedQuery, target driverTarget) []byte {
	if len(entities) == 0 {
		return []byte(header() + `package ` + pkg + `
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
	var unions []*preparedEdgeUnion
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

	anyProp := false
	anyNonNull := false
	anyTime := false
	for _, e := range entities {
		if e.AnyProp {
			anyProp = true
		}
		if e.AnyNonNull {
			anyNonNull = true
		}
		if e.AnyTime {
			anyTime = true
		}
	}

	var b strings.Builder
	b.WriteString(header())
	b.WriteString("package ")
	b.WriteString(pkg)
	b.WriteString("\n\n")

	// Imports: dbtype is unconditional (every helper's argument type);
	// fmt gates on anyProp; time gates on anyTime (TIMESTAMP property);
	// neo4j gates on anyNonNull. Alphabetical: fmt, time, then external
	// neo4j / dbtype.
	b.WriteString("import (\n")
	if anyProp {
		b.WriteString("\t\"fmt\"\n")
	}
	if anyTime {
		b.WriteString("\t\"time\"\n")
	}
	if anyProp || anyTime {
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
func writeEntityStruct(b *strings.Builder, e preparedEntity) {
	if e.Kind == entityNode {
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
// one entity. Nullable properties go through direct Props lookup + type
// assertion (three-way outcome); non-nullable properties go through
// neo4j.GetProperty[T] (missing key is a decode error).
func writeEntityDecodeHelper(b *strings.Builder, e preparedEntity) {
	var carrier, arg string
	if e.Kind == entityNode {
		carrier = "dbtype.Node"
		arg = "node"
	} else {
		carrier = "dbtype.Relationship"
		arg = "rel"
	}
	fmt.Fprintf(b, "// decode%s decodes a driver %s into a %s struct,\n", e.Name, carrier, e.Name)
	b.WriteString("// enforcing per-property nullability against the schema.\n")
	fmt.Fprintf(b, "func decode%s(%s %s) (%s, error) {\n", e.Name, arg, carrier, e.Name)
	fmt.Fprintf(b, "\tvar out %s\n", e.Name)
	for _, f := range e.Fields {
		writeEntityFieldDecode(b, e, f, arg)
	}
	b.WriteString("\treturn out, nil\n")
	b.WriteString("}\n")
}

// writeEntityFieldDecode emits one field's decode block. Nullable path:
// Props lookup + type assertion against the driver's carrier + narrow-
// convert into a local of the emitted Go type + address-of-local into
// the pointer field. Non-nullable path: neo4j.GetProperty[<carrier>] +
// narrow-convert. The property key is the source property name
// (Property.Name), not the derived field name — the driver map is
// keyed on the schema-side name. Extended at C3 to cover DATE
// (dbtype.Date carrier) and TIMESTAMP (time.Time carrier); FLOAT32's
// nullable arm now narrows correctly (was a latent bug, no fixture
// exercised it before C3).
func writeEntityFieldDecode(b *strings.Builder, e preparedEntity, f preparedEntityField, arg string) {
	carrier := driverCarrier(f.GoType)
	if f.Nullable {
		fmt.Fprintf(b, "\tif v, ok := %s.Props[%q]; ok {\n", arg, f.PropName)
		fmt.Fprintf(b, "\t\ts, ok := v.(%s)\n", carrier)
		b.WriteString("\t\tif !ok {\n")
		fmt.Fprintf(b, "\t\t\treturn %s{}, fmt.Errorf(\"decode %s.%s: property %%q: expected %s, got %%T\", %q, v)\n", e.Name, e.Name, f.Field, carrier, f.PropName)
		b.WriteString("\t\t}\n")
		if carrier != f.GoType {
			fmt.Fprintf(b, "\t\tnarrowed := %s(s)\n", f.GoType)
			fmt.Fprintf(b, "\t\tout.%s = &narrowed\n", f.Field)
		} else {
			fmt.Fprintf(b, "\t\tout.%s = &s\n", f.Field)
		}
		b.WriteString("\t}\n")
		return
	}
	local := lowerFirstRune(f.Field)
	fmt.Fprintf(b, "\t%s, err := neo4j.GetProperty[%s](%s, %q)\n", local, carrier, arg, f.PropName)
	b.WriteString("\tif err != nil {\n")
	fmt.Fprintf(b, "\t\treturn %s{}, fmt.Errorf(\"decode %s.%s: %%w\", err)\n", e.Name, e.Name, f.Field)
	b.WriteString("\t}\n")
	if carrier != f.GoType {
		fmt.Fprintf(b, "\tout.%s = %s(%s)\n", f.Field, f.GoType, local)
	} else {
		fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, local)
	}
}
