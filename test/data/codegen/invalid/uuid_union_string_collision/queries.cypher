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
// All three targets, because all three answer it and for the same reason: on
// Apache AGE a UUID rides the agtype string, so the pair is one family there
// too.
//
// A WHOLE-ENTITY READ, and the position is what lets one manifest hold the
// three. Every target refuses the entity's property in its entity sweep, under
// the sentinel below. A COLUMN projection of the same property would not do:
// the neo4j targets still answer this sentinel, and AGE answers "unsupported
// query" first, from its own check that it can serve each column — measured
// when this fixture projected `a.either` and its AGE arm went red on exactly
// that.

// name: AccountWhole :many
MATCH (a:Account) RETURN a
