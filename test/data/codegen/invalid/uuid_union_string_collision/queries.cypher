// A closed union's members have to be pairwise distinct on the target's
// wire (spec §4): the emitted decode has the arriving shape and nothing else
// to pick a member by. On neo4j a UUID is carried as its RFC 9562 text — a
// STRING to the driver (ADR 0047) — so UNION<UUID|STRING> names one wire
// family twice and a value arriving as a string could be either member.
//
// Refused as a width, through the same channel UNION<INT32|INT64> is: the
// union has no carrier on this target, which is what the sentinel says.
//
// THIS IS THE COST OF THE STRING WIRE FORM, stated where an author will meet
// it. Under the driver's own dbtype.UUID the two members were distinct and
// this declaration generated; test/data/codegen/valid/uuid_property declared
// exactly this union until it moved to UNION<UUID|INT64>, the pairing that
// still generates.
//
// Both majors, because both answer it and for the same reason. A
// single-target fixture would leave the other major's refusal unwitnessed.

// name: AccountEither :many
MATCH (a:Account) RETURN a.either AS either
