// union_only_carrier_decode_only's claim, for the UUID carrier: a closed union
// with a UUID member that no query binds as a parameter. Read that fixture's
// head comment first, and union_only_uuid_carrier's for why the UUID family
// has fixtures of its own; this one records what differs.
//
// On the two neo4j majors the decode in union_neo4j.go reads
// `case string: v1, err := toUUID(t)` and returns the result as `any`. With no
// encode there is no `case UUID:` arm, so the package spells toUUID and spells
// the carrier's TYPE in no file but uuid.go and uuid_neo4j.go. It still does
// not compile without uuid.go, which declares toUUID's result.
//
// So it holds for uuid.go what its temporal twin holds for temporal.go. Under
// TestGoldenBuild, that codegen.ReferencesUUIDCarrier fires for a union nothing
// encodes. Under TestUUIDCarrierIsEmittedExactlyWhenReferenced, that a call to
// a function uuid_neo4j.go declares is read as naming the carrier: with that
// rule taken out of the sweep, both neo4j packages here read as uuid.go emitted
// "for nothing" (bd gqlc-51b0). union_only_uuid_carrier cannot show that,
// because its AccountsByEither binds the union and brings the `case UUID:` arm.
//
// uuid_neo4j.go here declares toUUID alone — no from* conversion has a caller —
// which is the decode-only emission of that file the parameter-bearing fixtures
// do not reach.
//
// On AGE agtypeUUID in models.go names UUID in its result type either way, so
// the third target is here as the control that the direction matters on neo4j
// alone.
//
// AccountEithers reads the union through collect() for the twin's reason: it is
// the one way a neo4j batch reaches a list of unions, since StorableProperty
// refuses a stored LIST<ANY<…>>. INT64 is the other member for uuid_property's
// reason — ANY<UUID | STRING> is a refusal.

// name: AccountWhole :one
MATCH (a:Account) WHERE a.id = $id RETURN a

// name: AccountEithers :many
MATCH (a:Account) RETURN collect(a.either) AS eithers
