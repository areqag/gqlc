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
// The two neo4j majors and not AGE, which still refuses the UUID width
// (test/data/codegen/invalid/uuid_width_unrepresentable).
//
// INT64 is the other member for uuid_property's reason: a UUID is a string on
// the wire, so ANY<UUID | STRING> is a refusal
// (test/data/codegen/invalid/uuid_union_string_collision).
//
// The three reads reach toUUID twice and fromUUID once, so uuid_neo4j.go emits
// both directions and each has a caller.

// name: AccountWhole :one
MATCH (a:Account) WHERE a.id = $id RETURN a

// name: AccountEither :many
MATCH (a:Account) RETURN a.either AS either

// name: AccountsByEither :many
MATCH (a:Account) WHERE a.either = $either RETURN a.id AS id
