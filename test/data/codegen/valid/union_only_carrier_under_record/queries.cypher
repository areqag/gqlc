// union_only_temporal_carrier's claim, with the containers the other way
// round from union_only_carrier_record_member: the property is a RECORD and
// the union is its field. The surface text is struct{ Stamp *any }, which names
// no carrier, while the union's helper pair in models.go names Date. Read the
// first of those head comments first.
//
// Measured on origin/master a99ab3b2 in a git-archive extract: this schema
// emitted no temporal.go and models.go failed to compile on `undefined: Date`
// (bd gqlc-o8p3).
//
// AGE alone, for record_property's reason: a record is a map, and neo4j does
// not hold a map in a property.
//
// The three reads are union_only_temporal_carrier's three. The column query is
// not named LedgerEntry because that identifier is taken: it is the site alias
// the record emission declares for this property, and a query of that name is
// an identifier collision.

// name: LedgerWhole :one
MATCH (l:Ledger) WHERE l.id = $id RETURN l

// name: EntryColumn :many
MATCH (l:Ledger) RETURN l.entry AS entry

// name: LedgersByEntry :many
MATCH (l:Ledger) WHERE l.entry = $entry RETURN l.id AS id
