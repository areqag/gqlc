package codegen

// UUIDCarrier is the exported name uuid.go declares: the gqlc-owned
// neutral carrier for the UUID property width, on the same ground ADR
// 0033 gives the five temporal carriers. Only one backend target carries
// the width at all today (neo4j-go-v6, whose driver declares
// dbtype.UUID), and the neutrality is owed for the same reason it was
// owed for DATE when only neo4j carried that: a caller writing against
// dbtype.UUID cannot swap targets without editing their program, and
// TestGoldenExportedSurfaceIsDriverFree refuses the leak whether or not
// a second target exists yet.
//
// A single name rather than a slice because there is one, and a slice of
// one reads as a set that is expected to grow. uuidCarrierSet below is
// what the walk needs.
const UUIDCarrier = "UUID"

// uuidCarrierSet is UUIDCarrier as the lookup referencesCarrier takes,
// built from the constant so the two cannot drift.
var uuidCarrierSet = map[string]struct{}{UUIDCarrier: {}}

// RenderUUID emits uuid.go: the neutral UUID carrier, byte-identical
// across every backend (ADR 0033 "Placement").
//
// [16]byte and not a struct, which is where this parts from the five
// temporal carriers without parting from their reasoning. Those are
// component structs because the driver's own types smuggle dimensions
// the width does not have — a dbtype.Date is a time.Time, so it carries
// a clock reading and a Location that == then compares. dbtype.UUID has
// no such residue: it is `type UUID [16]byte` (v6.2.0
// neo4j/dbtype/uuid.go), which is the RFC 9562 value and nothing else.
// Copying that shape costs nothing and buys the property the temporal
// carriers needed a bridge to get — the two are conversion-compatible,
// so narrowExpr and widenExpr route UUID through a plain Go conversion
// and this carrier needs no uuid_<driver>.go beside it.
//
// No String method, and no constructor. The canonical text form is the
// caller's to produce, and they almost certainly already have a library
// that produces it: github.com/google/uuid's UUID is [16]byte too, so
// `uuid.UUID(v)` is one conversion away, as is any other library that
// spells the value the same way. A String gqlc emitted would be a second
// implementation of RFC 9562 §4 in every generated package, competing
// with the one the caller has.
func RenderUUID(pkg string) []byte {
	return []byte(Header() + `package ` + pkg + `

// UUID is a 128-bit UUID in the byte order RFC 9562 lays down: the
// value, with no textual form attached. Convert it to whichever UUID
// library this program already uses — those are [16]byte too, so the
// conversion is direct.
type UUID [16]byte
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
