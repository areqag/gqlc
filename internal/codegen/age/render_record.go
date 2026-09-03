package age

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// This file is the RECORD half of models.go: the carrier alias one
// declared record is spelled as inside the emitted package, the decoder
// that checks an agtype map into it, and the encoder that widens it back
// into the map a bound parameter is rendered from.
//
// Both directions are emitted per ENCODING and not per site. Two schema
// positions declaring the same record — a property here, a parameter
// there — are one canonical encoding (graph.RecordOf sorts the fields),
// so they share one alias and one helper pair, and a value read at one
// position is assignable at the other without a conversion.
//
// The field walk is codegen.RecordFields, the same one that built the
// struct text, so the helper bodies read the same Go field names in the
// same order with the same nullability the struct declared. A second
// walk here would be a second chance to disagree about the mangle, and
// the disagreement emits a helper assigning a field the struct does not
// have.

// recordPlan is one record's emission: its canonical encoding, the
// identifier fragment its helpers are named from, its carrier alias, the
// struct text the alias stands for, its fields, and which of the two
// helpers the batch actually reached.
type recordPlan struct {
	pt     graph.PropertyType
	suffix string
	alias  string
	text   string
	fields []codegen.RecordFieldPlan
	decode bool
	encode bool
}

// recordPlans builds the emission plan for every record encoding the
// batch reaches, in the order helpers marked them.
//
// A record this backend cannot carry is dropped rather than half-built,
// and it cannot reach here: helpers.needRecord marks an encoding only
// after typeMap.Property answered it, and Property answers a record only
// when every field's carrier answered. The guard is the belt on that.
func recordPlans(h helpers) []recordPlan {
	carrier := func(pt graph.PropertyType) (string, bool) { return typeMap{}.Property(pt) }

	out := make([]recordPlan, 0, len(h.records))
	for _, pt := range h.records {
		text, ok := typeMap{}.Property(pt)
		if !ok {
			continue
		}
		fields, ok := codegen.RecordFields(pt.Fields(), carrier)
		if !ok {
			continue
		}
		out = append(out, recordPlan{
			pt:     pt,
			suffix: codegen.RecordHelperSuffix(pt),
			alias:  codegen.RecordAliasName(pt),
			text:   text,
			fields: fields,
			decode: h.recordDecoders[pt],
			encode: h.recordEncoders[pt],
		})
	}
	return out
}

// writeRecordCarriers emits the alias each record's carrier is spelled as
// inside the package.
//
// An ALIAS and not a definition. The struct is the carrier a schema
// position of this width is typed with, and those positions — an entity
// field, a Row field, a Params field — spell the struct text outright,
// because that text is what the shared prepared surface carries. A
// defined type would make the helper signatures below name something not
// assignable to any of them.
func writeRecordCarriers(b *strings.Builder, plans []recordPlan) {
	for _, p := range plans {
		fmt.Fprintf(b, "\n// %s is the carrier for %s.\n", p.alias, p.pt)
		fmt.Fprintf(b, "type %s = %s\n", p.alias, p.text)
	}
}

// writeRecordSiteAliases emits the exported <Entity><Field> alias each
// record-typed entity property is additionally spelled as (spec §2.1).
//
// The ergonomics layer, and severable: every type it names is already
// denoted by the anonymous struct beside it, so an emission without this
// block is complete. What it buys is that a caller declaring a variable
// of a record property's type writes PlaceAddr instead of retyping the
// fields.
//
// An ALIAS for the same reason the digest carrier is one, and the reason
// bites harder here because this name is the one a caller actually
// holds: a defined type would not be assignable to the anonymous
// spelling the Row and Params structs carry, so the value a caller named
// with it could not be passed back to the query that produced it.
//
// Spelled with the struct text rather than as an alias to the digest
// carrier, so the fields are legible at the name a caller reads. Two
// aliases to one underlying type, which is what keeps them assignable.
func writeRecordSiteAliases(b *strings.Builder, sites []codegen.RecordSiteAlias) {
	for _, s := range sites {
		text, ok := typeMap{}.Property(s.Width)
		if !ok {
			continue
		}
		fmt.Fprintf(b, "\n// %s is the record type of %s property %s.\n", s.Name, s.Entity, s.Property)
		fmt.Fprintf(b, "type %s = %s\n", s.Name, text)
	}
}

// writeRecordDecoders emits decode<Suffix> for each record: the
// field-by-field read of a split agtype map.
//
// A MISSING key and an explicit null are the same thing — no value here
// — and that is the rule both backends read a record by. AGE drops a
// null property rather than storing one, so absence is how a null
// arrives from the store; a map a writer built by hand can spell the
// null outright, and a decoder that told those two apart would report a
// difference the schema does not have.
//
// On a NOT NULL field that "no value" is a decode error naming the
// record and the field, and it is raised HERE rather than inside
// agtypeRecordField, because the helper has neither name in hand.
// A record the batch only ever BINDS gets no decoder. Not thrift: its
// decoder would name its fields' decode-side helpers, which a batch that
// never reads one has not marked, so the emission would call what it does
// not declare. The encoder half below is gated for the mirror reason.
func writeRecordDecoders(b *strings.Builder, plans []recordPlan) {
	for _, p := range plans {
		if !p.decode {
			continue
		}
		fmt.Fprintf(b, "\n// decode%s checks an agtype map into a %s.\n", p.suffix, p.alias)
		fmt.Fprintf(b, "func decode%s(raw []byte) (%s, error) {\n", p.suffix, p.alias)
		fmt.Fprintf(b, "\tvar out %s\n", p.alias)
		// `RECORD { }` declares no fields, so nothing reads the split map
		// — and a bound name nothing reads is not Go. The split still
		// happens: the record says its fields are none, not that its
		// value may be any shape, so a text that is not a map is as much
		// a decode failure here as anywhere.
		if len(p.fields) == 0 {
			fmt.Fprintf(b, "\tif _, err := agtypeObject(raw); err != nil {\n\t\treturn out, fmt.Errorf(%q, err)\n\t}\n",
				"decode "+string(p.pt)+": %w")
			b.WriteString("\treturn out, nil\n}\n")
			continue
		}
		fmt.Fprintf(b, "\tfields, err := agtypeObject(raw)\n")
		fmt.Fprintf(b, "\tif err != nil {\n\t\treturn out, fmt.Errorf(%q, err)\n\t}\n",
			"decode "+string(p.pt)+": %w")

		for i, f := range p.fields {
			value := valueName(i)
			fail := fmt.Sprintf("decode %s: field %%q: %%w", p.pt)
			fmt.Fprintf(b, "\t%s, err := agtypeRecordField(fields, %q, %s)\n",
				value, f.Key, decodeFunc(f.GoType, f.Width))
			fmt.Fprintf(b, "\tif err != nil {\n\t\treturn out, fmt.Errorf(%q, %q, err)\n\t}\n",
				fail, f.Key)
			if f.Nullable {
				fmt.Fprintf(b, "\tout.%s = %s\n", f.Field, value)
				continue
			}
			fmt.Fprintf(b, "\tif %s == nil {\n\t\treturn out, fmt.Errorf(%q, %q)\n\t}\n",
				value, fmt.Sprintf("decode %s: field %%q is not null but has no value", p.pt), f.Key)
			fmt.Fprintf(b, "\tout.%s = *%s\n", f.Field, value)
		}
		b.WriteString("\treturn out, nil\n}\n")
	}
}

// writeRecordEncoders emits encode<Suffix> for each record: the widening
// of the carrier into the map its agtype text is rendered from.
//
// A field-by-field build and not a conversion, because there is none to
// make: an anonymous struct is not convertible to map[string]any in Go.
// It is what stops a record parameter from crossing as whatever
// json.Marshal makes of the struct — which is the Go FIELD names, not
// the record field names the schema declared, and a JSON null for a
// nullable field the store spells by absence.
//
// A nullable field with no value writes no key, matching the decode side
// and matching how this backend stores a null property.
//
// Fallible either way. A field whose own encoding can fail makes the
// whole record's encoding fail — a nested record, a DATE the calendar
// does not have, an unsigned width the wire cannot hold — and the ones
// that cannot fail still ride the same signature, because a helper whose
// fallibility moved with its fields would need a different call shape at
// every site that binds it.
func writeRecordEncoders(b *strings.Builder, plans []recordPlan) {
	for _, p := range plans {
		if !p.encode {
			continue
		}
		fmt.Fprintf(b, "\n// encode%s widens a %s into the map its agtype text is rendered from.\n",
			p.suffix, p.alias)
		fmt.Fprintf(b, "func encode%s(v %s) (map[string]any, error) {\n", p.suffix, p.alias)
		fmt.Fprintf(b, "\tout := make(map[string]any, %d)\n", len(p.fields))

		for i, f := range p.fields {
			access := "v." + f.Field
			indent := "\t"
			if f.Nullable {
				// The nil check is here rather than inside the encoder
				// because what a nil means is "write no key", and an
				// encoder that answered a value could not say that.
				fmt.Fprintf(b, "\tif %s != nil {\n", access)
				access, indent = "*"+access, "\t\t"
			}
			encode, fallible := fieldEncoder(f, access)
			if fallible {
				value := valueName(i)
				fmt.Fprintf(b, "%s%s, err := %s\n", indent, value, encode)
				fmt.Fprintf(b, "%sif err != nil {\n%s\treturn nil, fmt.Errorf(%q, %q, err)\n%s}\n",
					indent, indent, fmt.Sprintf("encode %s: field %%q: %%w", p.pt), f.Key, indent)
				encode = value
			}
			fmt.Fprintf(b, "%sout[%q] = %s\n", indent, f.Key, encode)
			if f.Nullable {
				b.WriteString("\t}\n")
			}
		}
		b.WriteString("\treturn out, nil\n}\n")
	}
}

// fieldEncoder is the expression one record field's value crosses as, and
// whether evaluating it can fail.
//
// The nullability is dropped before the composer is asked, and that is
// the point of asking through a synthesised Param rather than reaching
// for the leaf encoder directly: a nullable field's pointer is already
// dereferenced by the caller, which writes no key for a nil, so what is
// left to encode is a value of the field's own width. Handing the
// combinator a nullability the access no longer has would wrap it in
// agtypeEncodedNullable and encode a pointer that is not there.
//
// Everything else — the leaf choice, the list nesting, the record arm —
// is fallibleParamEncoder's, unchanged, because "how does this Go value
// become agtype text" is one question and a record field asks it in the
// same words a bound parameter does.
//
// A DECLINED carrier crosses as the access itself. That is what the
// composer's false means — the widths it names are the ones whose Go
// value is not already an agtype value, and everything else is written
// into the map unchanged — but it answers the empty string beside it,
// because argsMapText spells the access at its own call site and passes
// no access to ask with. Here the access is in hand, so the decline has
// to be turned back into it; taking the empty text would emit `out[k] =`
// with nothing to its right, which is not Go.
func fieldEncoder(f codegen.RecordFieldPlan, access string) (string, bool) {
	encode, ok := fallibleParamEncoder(codegen.Param{GoType: f.GoType, Width: f.Width}, access)
	if !ok {
		return access, false
	}
	return encode, true
}

// writeRecordFieldHelper emits agtypeRecordField, the read of one member
// of a split record map.
//
// One helper for both nullabilities, answering a pointer either way. A
// NOT NULL field's failure names the record and the field, and neither
// name is in scope here, so the caller does the refusing and this
// reports only "no value here" as the nil.
//
// It is close kin to agtypeProperty and is deliberately NOT that helper.
// The two answer the same question about different things — a member of
// a record's own map, versus a property of a vertex — and only this one
// reads an explicit null as absence. Whether a stored property can
// arrive as an explicit null is a question about AGE's wire that this
// branch has not measured, and widening agtypeProperty to match would be
// answering it by assumption.
func writeRecordFieldHelper(b *strings.Builder) {
	b.WriteString(`
// agtypeRecordField reads one member of a record's map. An absent key and
// an explicit null are the same thing — no value here — and both answer
// nil, because a record spells "this field has no value" both ways and a
// reader that told them apart would report a difference the schema does
// not declare.
func agtypeRecordField[T any](fields map[string][]byte, key string, decode func([]byte) (T, error)) (*T, error) {
	raw, ok := fields[key]
	if !ok || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	out, err := decode(raw)
	if err != nil {
		return nil, fmt.Errorf("gqlc: field %q: %w", key, err)
	}
	return &out, nil
}
`)
}
