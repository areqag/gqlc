// A list expression whose every element is a property the schema declares,
// reached through something that is not a bare ref lookup: an index into a
// list property, a field of a record property, and a property beside a
// literal null. Each column is []any on purpose (bd gqlc-4ro6).
//
// Spec model-change-f45qn fills a list expression's element from the schema
// only when every leaf is literally one of its Refs(). a.dates[0] and a.ul.u
// each touch a ref without being its value, and `null` is no ref at all, so
// the certificate is not minted and []any is the permanent answer that spec
// rules for heterogeneous elements and rich operands, not a width this
// backend lost. The bare forms resolve to the same unknown; it is this
// backend that refuses a bare unknown column, having no Go type to declare
// its row field with, while neo4j serves it as `any` (measured 2026-10-04 on
// 43422c03).
//
// The regression this fixture is for is a typing that fills these leaves
// anyway. Filling a.dates[0] from its ref would type it LIST<DATE>, and
// a.ul.u would take the RECORD's shape: both confidently wrong.
//
// AGE-only because neo4j does not store a RECORD property (StorableProperty).

// name: AccountUnknownElements :many
MATCH (a:Account) RETURN [a.dates[0]] AS dated, [a.ul.u] AS field, [a.either, null] AS mixed ORDER BY a.id
