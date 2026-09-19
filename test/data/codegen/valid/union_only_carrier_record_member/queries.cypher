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
// AGE alone, and not because neo4j refuses the declaration — it does not. Both
// neo4j majors generate this schema, and on origin/master a99ab3b2 their
// record_neo4j.go failed to compile for the same missing file. They are not
// enrolled for the reason record_any_and_empty gives for keeping its own
// record member off neo4j: whether the server holds a map in a property is
// unmeasured (gqlc-jffyz step 5), and a golden here would pin that claim.
//
// A fixture of its own because one temporal.go serves a whole package, so this
// shape beside another that triggers the file would witness nothing.

// name: LedgerWhole :one
MATCH (l:Ledger) WHERE l.id = $id RETURN l

// name: LedgerEntry :many
MATCH (l:Ledger) RETURN l.entry AS entry

// name: LedgersByEntry :many
MATCH (l:Ledger) WHERE l.entry = $entry RETURN l.id AS id
