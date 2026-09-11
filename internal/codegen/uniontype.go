package codegen

import (
	"github.com/areqag/gqlc/internal/graph"
)

// UnionCarrierText is the Go type text a closed dynamic union carries as,
// spec §4 (docs/specs/codegen-record-union-carriers.md). It is "any" on
// every backend, because Go has no sum type and every alternative spells
// one badly — the member list is enforced by generated validation rather
// than by the type.
//
// Declared as a constant rather than written at the two call sites so the
// backends cannot drift on it: the declared surface does not vary by
// backend (TestBackendInvariantSurface), and two literals are two chances
// to break that.
const UnionCarrierText = "any"

// WireFamilyIndistinct is the wire family of a width that has no single
// wire shape at all — Go's `any`, which is what ANY VALUE and a nested
// union both carry as. It collides with EVERY other family, including
// itself, because decode has nothing but the wire shape to dispatch on
// and a member that can arrive as any shape leaves no shape for another
// member to own.
//
// A distinguished tag rather than a sentinel value, so a backend's family
// table answers it the same way it answers every other family and the
// collision walk needs no second channel to read it through.
const WireFamilyIndistinct = "*"

// UnionMemberCollision reports the first pair of pt's members that land in
// ONE wire family on the backend whose family table is passed in, which is
// the admission rule of spec §4: a closed union is emittable on a backend
// iff its members map to pairwise-distinct wire families there.
//
// The rule is not an ergonomic preference. Decode has the wire shape and
// nothing else to dispatch on, so two members arriving as one shape leave
// the decoder with no way to say which member it was handed — and the
// whole point of declaring the member list, as against ADR 0020's open
// union, is that an INT32 member comes back int32 rather than the driver's
// widened int64. Where the shapes collide that narrowing is not available
// and gqlc refuses rather than guessing.
//
// carrier is the backend's own Property and family its own fold from a
// carrier text to a family tag, both threaded in rather than imported, for
// the reason RecordStructText threads the carrier in: a wire family is a
// PER-BACKEND fact and the two backends disagree about it — DATE and
// STRING are one family on AGE (both ISO text) and two on neo4j
// (dbtype.Date against string). What is shared is the walk, so the two
// cannot drift on WHICH pairs a family table condemns, only on what the
// families are.
//
// family is asked of the CARRIER TEXT rather than of the width, which is
// not a convenience: it is what keeps the family a fold over the answer
// the backend's decode is actually built on. Asked of the width it would
// be a second table beside Property with its own opportunity to disagree
// with it.
//
// A member the carrier refuses is SKIPPED rather than reported here. It
// refuses the union already, on the ordinary carrier ground and through a
// message that names the width rather than a pair — a BYTES member on AGE
// is refused because BYTES is, not because it collides with anything.
//
// The answer is the first colliding pair in the members' own canonical
// order (graph.UnionOf sorts them), later member last, so the pair a
// message names is a function of the encoding and not of iteration order.
//
// Pairwise rather than a family-keyed map, because "one family" is not an
// equivalence relation here: WireFamilyIndistinct collides with every tag
// including itself, and a map keyed on the tag cannot express a relation
// that is not transitive. The quadratic scan is over a member list an
// author wrote by hand.
func UnionMemberCollision(pt graph.PropertyType, carrier func(graph.PropertyType) (string, bool), family func(goType string) string) (a, b graph.PropertyType, collides bool) {
	members := pt.Members()
	tags := make([]string, len(members))
	for i, m := range members {
		goType, ok := carrier(m.Type)
		if !ok {
			// The empty tag marks a member this walk does not speak for.
			// It is not a family: the loop below skips it on both sides
			// rather than matching it against another empty one.
			continue
		}
		tags[i] = family(goType)
	}
	for j := range members {
		if tags[j] == "" {
			continue
		}
		for i := range members[:j] {
			if tags[i] == "" {
				continue
			}
			if tags[i] == tags[j] || tags[i] == WireFamilyIndistinct || tags[j] == WireFamilyIndistinct {
				return members[i].Type, members[j].Type, true
			}
		}
	}
	return "", "", false
}

// UnionCarrier answers the carrier text for a closed dynamic union on one
// backend, and whether that backend admits it at all.
//
// Two refusals, and they are deliberately not one. A member whose width
// this backend has no carrier for refuses the union on the ORDINARY
// carrier ground — BYTES on AGE is the example, and the union is refused
// there for the same reason a bare BYTES property is. A member that
// collides with another in one wire family refuses it on the ADMISSION
// rule, which is about the pair rather than about either member. Both
// route to ErrUnrepresentableWidth, because both are the same claim about
// the same backend, but only the second has a pair to name.
//
// carrier is the backend's own Property and family its own wire-family
// table, threaded in for the reason RecordStructText threads the first of
// them in: a member must inherit that backend's refusals, container rules
// included. AGE's caller wraps its Property in the carriesZone refusal
// before handing it over, because a union is a CONTAINER position and a
// zoned temporal has no property name inside one to hang its offset
// sidecar on (spec §2, generalised from the list rule).
//
// A member NOT NULL has no effect here and that is a recorded gqlc
// decision rather than an omission (spec §4): the nullability of the value
// is the property's own, spelled the way `any` spells absence, and a
// per-member NOT NULL constrains what the schema admits — the resolver's
// concern, not the carrier's.
func UnionCarrier(pt graph.PropertyType, carrier func(graph.PropertyType) (string, bool), family func(goType string) string) (string, bool) {
	for _, m := range pt.Members() {
		if _, ok := carrier(m.Type); !ok {
			return "", false
		}
	}
	if _, _, collides := UnionMemberCollision(pt, carrier, family); collides {
		return "", false
	}
	return UnionCarrierText, true
}
