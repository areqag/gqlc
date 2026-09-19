// All three targets. A UUID is carried as the standard library's uuid.UUID
// and is a STRING on the wire — its RFC 9562 text — so no driver type is
// involved (ADR 0047). The two neo4j goldens differ in nothing but the
// driver's import path. The AGE golden differs from them in one way worth
// reading for: it has a checked decoder, agtypeUUID, and NO encoder, because
// its parameters cross through encoding/json and uuid.UUID marshals itself as
// the text that decoder reads.
//
// THE LIVE HALVES are TestNeo4jStoresAndRoundTripsAUUID (both majors, bd
// gqlc-ybk2) and TestAGEStoresAndRoundTripsAUUID (bd gqlc-ytf9), each against
// its pinned image: that a UUID written through OpenAccount is stored in a
// property slot, as canonical text, and reads back equal through each of the
// three read positions below.
//
// What the four UUID properties are for, none of them decoration:
//
//   ref and prior are the bare width in its two NULLABILITIES. The whole-value
//   star is the emission site's, so prior is *UUID and ref is not, and a
//   golden that lost the distinction has dropped the resolver's answer.
//
//   trail and chain are the same width inside a LIST. That is a different
//   path: the element carrier comes from the list arm of the type table, and
//   the per-element decode is emitted from it. They differ only in ELEMENT
//   nullability, which is the one axis the list helpers split on — a nullable
//   element carries its own nil check and a non-nullable one does not, so the
//   two declarations reach two different emitted helpers and neither
//   witnesses the other.
//
//   either is the width inside a closed UNION, which is the third path and
//   the one that reaches the union helper file. INT64 is the other member
//   because STRING cannot be: a UUID arrives as a string, the members of a
//   union have to be pairwise distinct on the wire (spec §4), and
//   UNION<UUID|STRING> is therefore a refusal —
//   test/data/codegen/invalid/uuid_union_string_collision, which is the two
//   neo4j majors alone: AGE refuses the same union through a different
//   sentinel, its unserved-column one, so one manifest cannot hold all three.
//
// span is not a UUID and is the only property here that is not. It is a
// DURATION, and DURATION SPECIFICALLY, because this fixture is the only place
// in the corpus where a temporal bridge and the UUID conversions are emitted
// side by side — and Duration is the one temporal carrier whose two conversion
// bodies name no time package, so temporal_neo4j.go here imports dbtype alone.
// That is the pairing needsTimePackage answers about. Driven off the uses
// map's own keys it would see the UUID entry, whose bodies live in the OTHER
// file, and put an unused time import in this one; driven off
// codegen.TemporalCarriers it does not. An emitted package with an unused
// import does not compile, so the claim is carried by TestGoldenBuild over
// this fixture rather than by an assertion naming the function.
//
// EVERY DECODE IS CHECKED, which is where this width parts from the temporal
// ones. A property slot holds whatever string a writer put there, so toUUID
// parses and can fail, and each read position below emits the error arm the
// numeric narrowings emit (ADR 0037) rather than a bare conversion.
//
// The reads are the three positions a width can reach. The whole-entity :one
// goes through the models struct; the columns :many projects each property
// alone, which is the COLUMN position and not the same code as the property
// read; and a parameter is the encode direction. All three ask the type table
// separately. AccountRef is the :one over a bare UUID column, whose error
// returns need the carrier's ZERO — an array, so a composite literal and not
// the numeric zero the default arm spells.
//
// On the neo4j targets the four match parameters are four distinct bind expressions and no two
// share a helper. $ref is the bare non-nullable width, rendered by fromUUID.
// $prior is nullable, so the nil check is fromUUIDPtr's — a *UUID handed to
// the driver as it stands is a pointer to a Go array, which the packer
// refuses rather than binding as text or as null. $trail and $chain are the
// LISTS, which convert per element for the same reason, through
// fromNullableUUIDList and fromUUIDList.
//
// OpenAccount is the write the live half drives, and binds every property so
// the node it creates is one AccountWhole can decode.
//
// Between them these reach every helper renderUUIDConversions can emit, and
// that is deliberate rather than thorough: an unexported function nothing
// calls fails the emitted package's own lint fence, so a helper emitted for
// no call site is a red fixture rather than a dead line.

// name: AccountWhole :one
MATCH (a:Account) WHERE a.id = $id RETURN a

// name: AccountColumns :many
MATCH (a:Account) RETURN a.ref AS ref, a.prior AS prior, a.trail AS trail, a.chain AS chain, a.either AS either, a.span AS span

// name: AccountRef :one
MATCH (a:Account) WHERE a.id = $id RETURN a.ref AS ref

// name: AccountByRef :many
MATCH (a:Account) WHERE a.ref = $ref RETURN a.id AS id

// name: AccountByPrior :many
MATCH (a:Account) WHERE a.prior = $prior RETURN a.id AS id

// name: AccountByTrail :many
MATCH (a:Account) WHERE a.trail = $trail RETURN a.id AS id

// name: AccountByChain :many
MATCH (a:Account) WHERE a.chain = $chain RETURN a.id AS id

// name: OpenAccount :exec
CREATE (a:Account {id: $id, ref: $ref, prior: $prior, trail: $trail, chain: $chain, either: $either, span: $span})
