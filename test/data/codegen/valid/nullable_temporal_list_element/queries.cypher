// A temporal list whose ELEMENTS are nullable, in both positions that
// convert one: the stored property that decodes per element, and the
// parameter that encodes per element.
//
// temporal_list_param next door declares every element NOT NULL and is
// the control for this one. It cannot also be this fixture: an element
// star changes the helper's own signature, so the two shapes cannot
// share a from<X>List. Declaring both in one schema would have let a
// regeneration that lost the star still find a helper to call.
//
// WHY DATE. It is a carrier the driver cannot pack — packStruct's cases
// are the dbtype temporals and gqlc's neutral Date is not one — so the
// list owes a per-element conversion, which is the code path an element
// star has to reach through. A leaf the packer walks natively (string,
// int32) would bind bare and witness nothing here.
//
// spans is the in-fixture control on the same axis: NOT NULL elements
// keep []Duration and the bare from<X>List, so a fix that starred every
// temporal element fails this fixture rather than passing it.
//
// maybe is the composition, and it is here because it is the only thing
// that reaches one emitted arm: LIST<DATE> nullable AT BOTH POSITIONS is
// *[]*Date, so the whole-value wrapper has to wrap the element-nullable
// helper rather than the plain one. The two nullabilities are answered
// by different code — one is written from Nullable at struct emission,
// the other is part of the carrier text — and this is where they meet.

// name: SlotsMatching :many
MATCH (s:Slot)
WHERE s.days = $days AND s.spans = $spans AND s.maybe = $maybe
RETURN s.id AS id
