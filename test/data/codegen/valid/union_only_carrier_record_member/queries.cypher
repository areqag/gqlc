// union_only_temporal_carrier's claim, with the carrier one level further in:
// the union's member is a RECORD and the DATE is that record's field. The
// surface text is `any`; the record's struct text names Date, and it is
// spelled in models.go for the record's alias and in the union's member arm,
// neither of which is a position of the prepared surface. Read that fixture's
// head comment first.
//
// Measured on origin/master a99ab3b2 in a git-archive extract: this schema
// emitted no temporal.go and models.go failed to compile on `undefined: Date`
// (bd gqlc-o8p3).
//
// AGE alone, for record_property's reason: a record is a map, and neo4j does
// not hold a map in a property.
//
// A fixture of its own because one temporal.go serves a whole package, so this
// shape beside another that triggers the file would witness nothing.

// name: LedgerWhole :one
MATCH (l:Ledger) WHERE l.id = $id RETURN l

// name: LedgerEntry :many
MATCH (l:Ledger) RETURN l.entry AS entry

// name: LedgersByEntry :many
MATCH (l:Ledger) WHERE l.entry = $entry RETURN l.id AS id
