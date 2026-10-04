// Three LIST properties whose element carrier is `any` in ONE entity, so the
// surface text of all three is []any (bd gqlc-3s7q). dates and tags are two
// DISTINCT closed unions and bag is ANY VALUE, which has no member set at all.
// Each must decode through a list wrapper bound to its OWN element decoder:
// before the fix all three shared one agtypeListOfAny, bound to whichever
// width was registered first, and the other union's decodeUnion<h> was
// declared with no caller — which `just check-goldens-unused` names.
//
// AGE alone, because the two neo4j majors refuse a stored LIST<ANY<…>>
// (StorableProperty; live_union_list_property_test.go is the measurement).
//
// Every union list a schema can spell has NULLABLE elements — the closed-union
// alternatives take no NOT NULL — so each wrapper and each list encoder also
// passes a null element through as nil ahead of the member dispatch (bd
// gqlc-3jhv). TestAGERoundTripsANullElementInAUnionList stores and reads one
// back through this package.

// name: LedgerWhole :one
MATCH (l:Ledger) WHERE l.id = $id RETURN l

// name: LedgerLists :many
MATCH (l:Ledger) RETURN l.dates AS dates, l.tags AS tags, l.bag AS bag

// name: LedgersByTags :many
MATCH (l:Ledger) WHERE l.tags = $tags RETURN l.id AS id

// name: CreateLedger :exec
CREATE (l:Ledger {id: $id, dates: $dates, tags: $tags, bag: $bag})
