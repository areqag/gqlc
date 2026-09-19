package codegen

// UUIDCarrier is the exported name uuid.go declares for the UUID property
// width. It names no driver type, on the ground ADR 0033 gives the five
// temporal carriers, and it is not a type of gqlc's own either: it is an
// alias of the standard library's uuid.UUID (ADR 0047).
//
// A single name rather than a slice because there is one, and a slice of
// one reads as a set that is expected to grow. uuidCarrierSet below is
// what the walk needs.
const UUIDCarrier = "UUID"

// uuidCarrierSet is UUIDCarrier as the lookup referencesCarrier takes,
// built from the constant so the two cannot drift.
var uuidCarrierSet = map[string]struct{}{UUIDCarrier: {}}

// RenderUUID emits uuid.go: the UUID carrier, byte-identical across
// every backend (ADR 0033 "Placement").
//
// AN ALIAS, which is where this parts from the five temporal carriers.
// Those are gqlc-owned structs because no neutral type models them; for
// UUID the standard library has one since Go 1.27, so the carrier IS
// that type and a caller hands uuid.NewV7() to a generated method with
// no conversion. The alias rather than the qualified name at each site
// keeps the "uuid" import to this file and the backend's conversions
// file: every file that names the carrier on its surface spells it
// unqualified, so none of their import walks has a package to account
// for.
//
// On the wire a UUID is its RFC 9562 text form on all three enrolled
// targets — a Bolt STRING on both neo4j majors and an agtype string on
// Apache AGE — and no driver's own UUID type is involved (ADR 0047).
func RenderUUID(pkg string) []byte {
	return []byte(Header() + `package ` + pkg + `

import "uuid"

// UUID is the standard library's uuid.UUID, so any value that package
// makes — uuid.NewV7() among them — is one of these with no conversion.
// It is stored as its RFC 9562 text form, lowercase.
type UUID = uuid.UUID
`)
}

// ReferencesUUIDCarrier reports whether the prepared batch's public
// surface names the carrier — the emission trigger for uuid.go, on ADR
// 0033's rule that a carrier file is emitted only when the generated
// surface references it.
//
// Asked separately from ReferencesTemporalCarrier rather than folded
// into one "names any carrier" question, because the two files are
// emitted independently: a batch declaring a UUID property and no
// temporal width must get uuid.go and no temporal.go, and the reverse
// for every temporal fixture in the corpus. One combined trigger would
// emit both for either, which is the unreferenced-declaration cost ADR
// 0033 weighed and declined.
func ReferencesUUIDCarrier(p Prepared) bool {
	return referencesCarrier(p, uuidCarrierSet)
}
