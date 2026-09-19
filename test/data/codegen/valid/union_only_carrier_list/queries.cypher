// union_only_temporal_carrier's claim, one container out: the only mention of a
// temporal carrier is a member of a union that is itself a LIST ELEMENT. The
// surface text is []any. Read that fixture's head comment first.
//
// Measured on origin/master a99ab3b2 in a git-archive extract: this schema
// emitted no temporal.go and models.go failed to compile on `undefined: Date`
// (bd gqlc-o8p3).
//
// AGE alone, because the two neo4j majors refuse a stored LIST<ANY<…>>
// (StorableProperty; live_union_list_property_test.go is the measurement).
//
// A fixture of its own rather than a second property on the bare one, because
// one temporal.go serves a whole package: beside a bare union that already
// triggers the file, this shape would witness nothing.

// name: LedgerWhole :one
MATCH (l:Ledger) WHERE l.id = $id RETURN l

// name: LedgerEntries :many
MATCH (l:Ledger) RETURN l.entries AS entries

// name: LedgersByEntries :many
MATCH (l:Ledger) WHERE l.entries = $entries RETURN l.id AS id
