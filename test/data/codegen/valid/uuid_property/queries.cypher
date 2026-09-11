// neo4j-go-v6 alone, and the single target is the point of the fixture. UUID
// is the first width the two neo4j majors answer differently: dbtype.UUID
// landed in neo4j-go-driver v6.2.0 and v5.28.4 has no counterpart, the two
// versions test/data/codegen/go.mod pins. v5's refusal is
// test/data/codegen/invalid/uuid_width_driver_version and AGE's is
// test/data/codegen/invalid/uuid_width_unrepresentable; the three schemas
// agree on the declaration so that the answer is the only variable.
//
// NO LIVE TEST BACKS THIS, AND ONE CANNOT TODAY. dbtype.UUID is a Bolt 6.1
// value — the v6 hydrator and packUUID both refuse an older protocol — and
// the server image this repository pins speaks Bolt 5.x. So what is asserted
// here is that the EMISSION is right: the goldens compile against the pinned
// v6 driver and spell dbtype.UUID where they should. That a UUID round-trips
// against a server, and whether one can be STORED in a property slot at all,
// is unmeasured and is nobody's claim yet (bd gqlc-eg4b names the follow-up).
// The storage axis is the one to watch: neo4j.typeMap.StorableProperty admits
// UUID by silence rather than by measurement, the way it admits every scalar,
// and the two refusals it does carry — a map-valued property and a nested
// list — were each measured against the pinned image before they were
// written.
//
// What the four UUID properties are for, none of them decoration:
//
//   ref and prior are the bare width in its two NULLABILITIES. The whole-value
//   star is the emission site's, so prior is *dbtype.UUID and ref is not, and
//   a golden that lost the distinction has dropped the resolver's answer.
//
//   trail and chain are the same width inside a LIST. That is a different
//   path: the element carrier comes from the list arm of the type table, and
//   the per-element decode is emitted from it. A table threaded for the major
//   at the top level but not inside the list arm emits []interface{} here, or
//   nothing at all. They differ only in ELEMENT nullability, which is the one
//   axis the list bridge splits on — a nullable element carries its own nil
//   check and a non-nullable one does not, so the two declarations reach two
//   different emitted helpers and neither witnesses the other.
//
//   either is the width inside a closed UNION, which is the third path and
//   the one that reaches the union helper file. It also asserts that UUID is
//   its OWN wire family on this backend: the members have to be pairwise
//   distinct on the wire for the union to be admitted at all (spec §4), and a
//   driver that handed a UUID back as a string would make this declaration a
//   refusal rather than an emission. STRING is the member chosen for exactly
//   that reason — it is the family a UUID would collapse into if it were text.
//
// The union's own file is where the import walk is tested: UUID reaches
// union_neo4j.go's dbtype import through the same DECODE-ONLY rule the five
// neutral temporal carriers reach it through — the decode arm dispatches on
// dbtype.UUID, and the encode arm names fromUUID, whose own bridge file
// carries the mention. A walk that gated the import on the temporal names
// alone, or on a "dbtype." prefix over the rendered text, emits a file naming
// an unimported package or importing one nothing names. Neither compiles, and
// the goldens are compiled.
//
// The reads are the three positions a width can reach. The whole-entity :one
// goes through the models struct; the columns :many projects each property
// alone, which is the COLUMN position and not the same code as the property
// read; and a parameter is the encode direction. All three ask the type table
// separately.
//
// The four parameters are four distinct bind expressions and no two share a
// helper. $ref is the bare non-nullable width, which widens through fromUUID.
// $prior is nullable, so the star is the emission site's and the nil check is
// fromUUIDPtr's — a null bound as a zero UUID is a different value from a
// null, and only the helper can tell them apart. $trail and $chain are the
// LISTS, and they are the parameters that MOTIVATE the neutral-carrier bridge
// rather than merely exercising it: v6's packArray
// (neo4j/internal/bolt/outgoing.go) type-switches on the concrete dbtype.UUID,
// so a []UUID reaching the wire is an UnsupportedTypeError, and no Go
// conversion turns a []UUID into a []dbtype.UUID. Each element converts on
// its way out, which is what fromNullableUUIDList and fromUUIDList do and
// what nothing inline could.
//
// Between them these four reach every helper renderUUIDConversions can emit,
// and that is deliberate rather than thorough: an unexported function nothing
// calls fails the emitted package's own lint fence, so a helper emitted for
// no call site is a red fixture rather than a dead line. The count is not
// asserted anywhere and is not meant to be — what holds it is that dropping a
// parameter drops a caller and reddens the fence.

// name: AccountWhole :one
MATCH (a:Account) RETURN a

// name: AccountColumns :many
MATCH (a:Account) RETURN a.ref AS ref, a.prior AS prior, a.trail AS trail, a.chain AS chain, a.either AS either

// name: AccountByRef :many
MATCH (a:Account) WHERE a.ref = $ref RETURN a.id AS id

// name: AccountByPrior :many
MATCH (a:Account) WHERE a.prior = $prior RETURN a.id AS id

// name: AccountByTrail :many
MATCH (a:Account) WHERE a.trail = $trail RETURN a.id AS id

// name: AccountByChain :many
MATCH (a:Account) WHERE a.chain = $chain RETURN a.id AS id
