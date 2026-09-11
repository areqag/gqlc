package codegen

import (
	"crypto/sha256"
	"encoding/hex"

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

// IsDeclaredUnion reports whether a Go type text and the width beside it
// are a closed union an emission declares a helper pair for.
//
// Both halves are asked and neither alone is the test, for the reason
// IsDeclaredRecord asks two. The TEXT cannot answer: `any` is also ANY
// VALUE's carrier and ANY PROPERTY VALUE's, and neither of those has a
// member set to validate against or a shape to narrow to — an emission
// that read the text alone would name a helper for every ANY column in
// the batch. The WIDTH cannot answer either, at the sites that have one:
// a union this backend refuses has no carrier text at all, and asking the
// kind alone would name a helper for a declaration preparation rejected.
//
// So the pair is the exact set UnionEncodings emits for, which is what
// makes a name derived at a call site resolve to a declaration.
func IsDeclaredUnion(goType string, width graph.PropertyType) bool {
	return width.Kind() == graph.KindUnion && goType == UnionCarrierText
}

// UnionMemberPlan is one member of a closed union as an emitter needs it:
// this backend's carrier text for the member's declared width, and the
// width itself.
//
// The width is kept beside the carrier for the reason RecordFieldPlan
// keeps it: a member that is a declared record names its helper from its
// canonical encoding, and the anonymous struct text does not run backwards
// into a PropertyType.
//
// A member's NOT NULL is deliberately absent. It has no codegen effect
// (spec §4): the nullability of the value is the property's own, spelled
// the way `any` spells absence, and a per-member NOT NULL constrains what
// the schema admits — the resolver's concern rather than the carrier's.
// Recording its absence here is what stops a reader hunting for the field.
type UnionMemberPlan struct {
	GoType string
	Width  graph.PropertyType
}

// UnionMembers renders the per-member emission plan for one closed union
// on one backend, in the members' own canonical order (graph.UnionOf sorts
// them), so the encode type-switch, the decode dispatch and the refusal
// message all read the same members in the same order.
//
// carrier is the backend's own Property — AGE's wrapped in its
// carriesZone container refusal, exactly as its Property arm wraps it —
// threaded in rather than imported, so a member inherits that backend's
// refusals. One refused member refuses the whole union and is reported as
// ok=false with no partial plan, because a dispatch missing an arm is not
// a carrier: it would accept a value at bind and have nothing to narrow it
// back to at decode.
//
// It does NOT re-ask the admission rule. UnionCarrier answers that, at the
// preparation sites, before any emission walk runs; a union that collides
// has no carrier and so never reaches an encoding set. Asking twice would
// be a second copy of the rule with its own chance to disagree with the
// one the refusal messages are worded from.
func UnionMembers(pt graph.PropertyType, carrier func(graph.PropertyType) (string, bool)) ([]UnionMemberPlan, bool) {
	members := pt.Members()
	out := make([]UnionMemberPlan, 0, len(members))
	for _, m := range members {
		goType, ok := carrier(m.Type)
		if !ok {
			return nil, false
		}
		out = append(out, UnionMemberPlan{GoType: goType, Width: m.Type})
	}
	return out, true
}

// UnionHelperSuffix is the identifier fragment naming one union encoding,
// so a backend spells its validation/dispatch pair "encode"+suffix /
// "decode"+suffix.
//
// A short hash of the canonical encoding for the reason
// RecordHelperSuffix is one: the encoding is unbounded, since a member may
// itself be a record or a list of one, and a name derived from the text
// would grow with it. Shared between the backends for the reason that one
// is shared — the two must not drift on which encodings they consider one
// — and it inherits that function's 32-bit collision bound and the
// argument recorded there for stating it rather than guarding it.
//
// "Union" rather than "Record" is the whole of the difference, and it is
// what keeps the two namespaces apart: a record and a union are different
// widths, so they never collide in graph.PropertyType, but their DIGESTS
// are over different strings and a shared prefix would let one emission
// declare two helpers of one name only by accident of the hash.
func UnionHelperSuffix(pt graph.PropertyType) string {
	sum := sha256.Sum256([]byte(pt))
	return "Union" + hex.EncodeToString(sum[:4])
}

// UnionHelperNames is every package-level identifier the union emission
// owns for one encoding.
//
// No carrier alias, which is the one entry the record list has that this
// one does not. A union carries as `any`, a predeclared name every
// emission already spells wherever it needs it, so there is no multi-line
// text an alias would save a reader from — and an alias for `any` would be
// a second spelling of a type the backends must not drift on
// (UnionCarrierText exists so they cannot).
//
// All four helpers, not the subset a given batch reaches, for the reason
// RecordHelperNames reserves all five: which directions are emitted is a
// per-backend reading of the batch and the identifier sweep runs before
// any backend has made one.
func UnionHelperNames(pt graph.PropertyType) []string {
	suffix := UnionHelperSuffix(pt)
	return []string{
		"encode" + suffix,
		"encode" + suffix + "Ptr",
		"encode" + suffix + "List",
		"decode" + suffix,
	}
}

// UnionEncodings is every distinct closed-union encoding one batch
// reaches, in canonical-encoding order, so a backend emits one helper pair
// per entry and a caller can look one up by width.
//
// TRANSITIVE through list elements, record fields and union members alike,
// for the reason RecordEncodings is: a union nested inside another shape
// still needs its own helper pair, and one that named a helper nothing
// declared would fail at `go build` of the EMITTED package. A union
// nested directly inside a union cannot arise — graph.UnionOf flattens an
// unqualified nested union into its parent — but a union under a record
// field under a union member can, and the walk is closed over all three
// rather than over the two that are reachable today.
//
// The order is the encoding's own, so the emitted file is byte-stable
// across runs.
func UnionEncodings(entities []Entity, prepared []Query) []graph.PropertyType {
	return reachableEncodings(entities, prepared, graph.KindUnion)
}
