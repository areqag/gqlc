// union_only_temporal_carrier's claim, for the UUID carrier: the ONLY mention
// of UUID in this batch is a union MEMBER, so the surface spells `any` and
// names no carrier, while union_neo4j.go names UUID, fromUUID and toUUID. Read
// that fixture's head comment first; this one records what differs.
//
// It is a separate fixture and not a second property there because the two
// carrier files are triggered independently (codegen.ReferencesUUIDCarrier
// beside codegen.ReferencesTemporalCarrier). A batch holding both unions could
// lose either trigger's union reading only if it lost both. Apart, each golden
// tree also says the other family's file is absent: no temporal.go here, no
// uuid.go there.
//
// All three targets. On the two neo4j majors the file that names the carrier
// is union_neo4j.go; on AGE it is models.go, whose union helpers spell UUID in
// the encode arm and in the decode dispatch. AGE has carried the width since
// PR #2934, and on origin/master ab4ca4a9 this batch failed there on
// `undefined: UUID` as it did on neo4j.
//
// INT64 is the other member for uuid_property's reason: a UUID is a string on
// the wire, so ANY<UUID | STRING> is a refusal
// (test/data/codegen/invalid/uuid_union_string_collision).
//
// The three reads reach toUUID twice and fromUUID once, so uuid_neo4j.go emits
// both directions and each has a caller. AGE emits no conversions file and no
// encoder for the width; agtypeUUID in models.go is its whole half.

// name: AccountWhole :one
MATCH (a:Account) WHERE a.id = $id RETURN a

// name: AccountEither :many
MATCH (a:Account) RETURN a.either AS either

// name: AccountsByEither :many
MATCH (a:Account) WHERE a.either = $either RETURN a.id AS id
