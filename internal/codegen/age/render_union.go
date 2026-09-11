package age

import (
	"fmt"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/graph"
)

// This file is the closed-UNION half of models.go: the bind-time
// validation of an `any` against the member set a union declares, and the
// decode dispatch that narrows an arriving agtype value to the member
// whose wire family it belongs to (spec §4).
//
// No carrier alias, which is the one thing the record half beside it has
// that this one does not: a union carries as `any`, a predeclared name
// every emission site already spells, so an alias would be a second
// spelling of a type the two backends must not drift on.
//
// Both directions are emitted per ENCODING and not per site, for the
// reason the record pair is: two schema positions declaring the same
// union are one canonical encoding (graph.UnionOf sorts the members), so
// they share one helper pair and a value read at one position binds at
// the other.

// unionPlan is one closed union's emission: its canonical encoding, the
// identifier fragment its helpers are named from, its members in
// canonical order, and which of the two directions the batch reached.
type unionPlan struct {
	pt      graph.PropertyType
	suffix  string
	members []codegen.UnionMemberPlan
	decode  bool
	encode  bool
}

// unionPlans builds the emission plan for every union encoding the batch
// reaches, in the order helpers marked them.
//
// A union this backend cannot carry is dropped rather than half-built,
// and it cannot reach here: helpers.needUnion marks an encoding only
// after typeMap.Property answered it, and Property answers a union only
// when every member carried AND the members' wire families were pairwise
// distinct. The guard is the belt on that.
func unionPlans(h helpers) []unionPlan {
	out := make([]unionPlan, 0, len(h.unions))
	for _, pt := range h.unions {
		members, ok := codegen.UnionMembers(pt, unionMemberCarrier)
		if !ok {
			continue
		}
		out = append(out, unionPlan{
			pt:      pt,
			suffix:  codegen.UnionHelperSuffix(pt),
			members: members,
			decode:  h.unionDecoders[pt],
			encode:  h.unionEncoders[pt],
		})
	}
	return out
}

// unionMemberCarrier is typeMap.Property wrapped in the same carriesZone
// refusal typeMap's own union arm wraps it in (types.go).
//
// Asked in the same words at both places on purpose: a member's carrier
// is what decides whether the union is admitted at all, and a plan built
// from an UNWRAPPED Property would hand this file a member the admission
// rule never saw — a zoned temporal, whose offset sidecar is named after
// a property the member has no name of its own inside.
func unionMemberCarrier(pt graph.PropertyType) (string, bool) {
	text, ok := typeMap{}.Property(pt)
	if !ok || carriesZone(text) {
		return "", false
	}
	return text, true
}

// writeUnionEncoders emits encode<Suffix> per union: the bind-time
// validation of an `any` against the declared member set, plus whatever
// widening the matched member owes before the args map is marshalled.
//
// A type switch over the MEMBER CARRIERS rather than over the wire
// families, because this side holds the caller's own Go value: a caller
// holding an int32 for an INT32 member is matched by that member and by
// no other. The arms are pairwise-distinct Go types because the admission
// rule already made their wire families pairwise distinct and a family is
// a FOLD of the carrier (types.go, wireFamily) — two members sharing a Go
// type would have shared a family and refused the union at preparation.
//
// Each matched member is widened by fallibleParamEncoder, the SAME
// composer that binds a top-level parameter and a record field, because a
// union member IS in the parameter position once its type is known. A
// second composer here would be a second chance to disagree about whether
// a DATE member crosses as agtypeDateText or as the struct.
//
// A member's NOT NULL has no effect here and that is a recorded decision
// rather than an omission (spec §4): whether nil is legal is the CALL
// SITE's question — a nullable position nil-checks through
// agtypeEncodedNullable before this is reached, and a non-nullable one
// lets the refusal below report it, since no member's carrier is nil.
func writeUnionEncoders(b *strings.Builder, plans []unionPlan) {
	for _, p := range plans {
		if !p.encode {
			continue
		}
		fmt.Fprintf(b, "\n// encode%s validates a value against the member set %s\n// declares, and renders the matched member as what this store binds it\n// as. A value outside the set is refused by name, which is what\n// declaring the members buys over ADR 0020's open union.\n",
			p.suffix, p.pt)
		fmt.Fprintf(b, "func encode%s(v %s) (%s, error) {\n", p.suffix, codegen.UnionCarrierText, codegen.UnionCarrierText)
		b.WriteString("\tswitch t := v.(type) {\n")
		for _, m := range p.members {
			fmt.Fprintf(b, "\tcase %s:\n", m.GoType)
			encode, fallible := fieldEncoder(codegen.RecordFieldPlan{GoType: m.GoType, Width: m.Width}, "t")
			if !fallible {
				fmt.Fprintf(b, "\t\treturn %s, nil\n", encode)
				continue
			}
			fmt.Fprintf(b, "\t\tout, err := %s\n", encode)
			fmt.Fprintf(b, "\t\tif err != nil {\n\t\t\treturn nil, fmt.Errorf(%q, err)\n\t\t}\n",
				"encode "+string(p.pt)+": %w")
			b.WriteString("\t\treturn out, nil\n")
		}
		b.WriteString("\t}\n")
		fmt.Fprintf(b, "\treturn nil, fmt.Errorf(%q, v)\n}\n",
			"encode "+string(p.pt)+": no member carries %T")
	}
}

// writeUnionDecoders emits decode<Suffix> per union: the dispatch of an
// arriving agtype value onto the one member its WIRE FAMILY belongs to,
// narrowed to that member's declared width.
//
// The dispatch is agtypeValue's own, read one level in. agtype's
// structured values are self-delimiting and its scalars share no opening
// byte, so the first byte chooses between string, list and map; the two
// boolean spellings are whole tokens; and a number is tried as an integer
// before a float, which is the rule agtypeValue already states and this
// inherits rather than restates — a value AGE wrote without a fractional
// part keeps the width agtype held it at.
//
// Only the families the member set actually claims get an arm. A value of
// any other family falls to the refusal at the bottom, which names the
// union rather than the member that was tried: what is wrong with it is
// that it is no member's shape, and naming one member would send the
// reader to the wrong declaration.
//
// The family probe and the member's own narrowing are two reads of the
// same bytes in the numeric arms, deliberately. The probe decides WHICH
// family arrived and its failure is not an error — it is the next arm's
// cue — while the narrowing's failure IS one and has to be reported: an
// INT32 member handed 2^40 arrived in its own family and is out of its
// declared range, which "no member's wire shape" would misreport as a
// value belonging to nobody.
//
// A null is refused rather than answered as nil, and every site that can
// legitimately deliver one has already dealt with it before this is
// called: a nullable property through agtypeNullableProperty, a record
// field through agtypeRecordField, a nullable list element through
// agtypeNullableElem. What reaches here is a null the schema did not
// declare, which is a decode failure naming the union.
func writeUnionDecoders(b *strings.Builder, plans []unionPlan) {
	for _, p := range plans {
		if !p.decode {
			continue
		}
		byFamily := unionMembersByFamily(p.members)
		fmt.Fprintf(b, "\n// decode%s dispatches an agtype value onto the member of\n// %s whose wire family it arrived as, and narrows it\n// to that member's declared width.\n",
			p.suffix, p.pt)
		fmt.Fprintf(b, "func decode%s(raw []byte) (%s, error) {\n", p.suffix, codegen.UnionCarrierText)
		b.WriteString("\tbody := bytes.TrimSpace(raw)\n")
		fmt.Fprintf(b, "\tif len(body) == 0 {\n\t\treturn nil, fmt.Errorf(%q, raw)\n\t}\n",
			"decode "+string(p.pt)+": %q is not an agtype value")

		// The three self-delimiting families, chosen by the opening byte.
		opener := []struct {
			family string
			byte   string
		}{
			{"string", `'"'`},
			{"list", `'['`},
			{"map", `'{'`},
		}
		var arms strings.Builder
		for _, o := range opener {
			m, ok := byFamily[o.family]
			if !ok {
				continue
			}
			fmt.Fprintf(&arms, "\tcase %s:\n", o.byte)
			writeUnionNarrow(&arms, p.pt, m, "\t\t")
		}
		if arms.Len() > 0 {
			b.WriteString("\tswitch body[0] {\n")
			b.WriteString(arms.String())
			b.WriteString("\t}\n")
		}

		if m, ok := byFamily["boolean"]; ok {
			// The two spellings are whole tokens rather than an opening
			// byte, so this is a second switch and not a third case above:
			// 't' and 'f' open nothing else in agtype's vocabulary, but
			// matching on them would admit any text starting with either.
			b.WriteString("\tif s := string(body); s == \"true\" || s == \"false\" {\n")
			writeUnionNarrow(b, p.pt, m, "\t\t")
			b.WriteString("\t}\n")
		}

		// Integer before float, agtypeValue's rule.
		numeric := []struct {
			family string
			probe  string
		}{
			{"integer", "agtypeInt64"},
			{"float", "agtypeFloat64"},
		}
		for _, n := range numeric {
			m, ok := byFamily[n.family]
			if !ok {
				continue
			}
			// A FLOAT64 member's narrowing IS the probe — agtypeFloat64
			// both ways — and emitting the pair would read the same bytes
			// twice to no effect. Collapsed rather than tolerated because
			// a reader of the generated file cannot tell a redundant
			// second read from a deliberate one, and the deliberate ones
			// are right beside it: an INT32 member probes with
			// agtypeInt64 and narrows with agtypeIntAs[int32], where the
			// two reads answer different questions. Equivalent, not
			// merely shorter: where probe and narrowing are one call, a
			// failure sends the value to the next arm under either
			// spelling.
			if narrow := decodeFunc(m.GoType, m.Width); narrow == n.probe {
				fmt.Fprintf(b, "\tif out, err := %s(body); err == nil {\n\t\treturn out, nil\n\t}\n", narrow)
				continue
			}
			fmt.Fprintf(b, "\tif _, err := %s(body); err == nil {\n", n.probe)
			writeUnionNarrow(b, p.pt, m, "\t\t")
			b.WriteString("\t}\n")
		}

		fmt.Fprintf(b, "\treturn nil, fmt.Errorf(%q, raw)\n}\n",
			"decode "+string(p.pt)+": %q is no member's wire shape")
	}
}

// writeUnionNarrow emits the body of one dispatch arm: the member's own
// decoder, its failure reported against the union, and the narrowed value
// returned as the carrier.
//
// decodeFunc is the same chooser every other decode site asks, so a
// member that is a list decodes through the batch's named list wrapper
// and a member that is a declared record through that record's own
// decoder — both of which needUnion has marked by then.
func writeUnionNarrow(b *strings.Builder, pt graph.PropertyType, m codegen.UnionMemberPlan, indent string) {
	fmt.Fprintf(b, "%sout, err := %s(body)\n", indent, decodeFunc(m.GoType, m.Width))
	fmt.Fprintf(b, "%sif err != nil {\n%s\treturn nil, fmt.Errorf(%q, err)\n%s}\n",
		indent, indent, "decode "+string(pt)+": %w", indent)
	fmt.Fprintf(b, "%sreturn out, nil\n", indent)
}

// unionMembersByFamily keys a union's members on the agtype wire family
// each arrives as, which is what the decode dispatch is written from.
//
// One member per family and no collision handling, because there can be
// no collision: the admission rule refused the union at preparation
// unless its members' families were pairwise distinct, so a second member
// landing on a key here would be a union that should never have reached
// an emission. The last writer would win silently, which is why the
// caller's plan comes from UnionMembers over the same carrier the
// admission rule was answered with.
//
// WireFamilyIndistinct is not a key it can hold: it collides with every
// tag including itself, so any member carrying it refused the union.
func unionMembersByFamily(members []codegen.UnionMemberPlan) map[string]codegen.UnionMemberPlan {
	out := make(map[string]codegen.UnionMemberPlan, len(members))
	for _, m := range members {
		out[wireFamily(m.GoType)] = m
	}
	return out
}
