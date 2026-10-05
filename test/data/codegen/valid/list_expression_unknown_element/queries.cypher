// A list expression whose every element is a property the schema declares,
// reached through something that is not a bare ref lookup: an index into a
// list property, or a property beside a literal null. Each column is []any on
// purpose (bd gqlc-4ro6); list_expression_record_field_element carries the
// third shape, a field of a record, which neo4j cannot store.
//
// Spec model-change-f45qn fills a list expression's element from the schema
// only when every leaf is literally one of its Refs(). a.dates[0] touches a
// ref without being its value, and `null` is no ref at all, so the
// certificate is not minted and []any is the answer that spec rules permanent
// for heterogeneous elements and rich operands. It is not a width a backend
// lost: the bare a.dates[0] resolves to the same unknown, which every target
// serves as a nullable `*any` column (AccountBareUnknown; AGE refused it until
// bd gqlc-2omj, and a null failed the row until bd gqlc-14u0l).
//
// The regression these columns are for is a typing that fills the leaves
// anyway. Filled from its ref, a.dates[0] would type as LIST<DATE>, which is
// confidently wrong. Treating `null` as a member of the element's type is the
// alternative bd gqlc-4ro6 declined, not a defect: it needs the certificate to
// admit a leaf that is not one of its refs, which the spec's mint rule does
// not. mixed is the union's half of that and mixed_scalar the INT32's. At
// 43422c03 AGE decoded a union list element through agtypeListOfAny even when
// certified (bd gqlc-k1dg narrows that), so mixed_scalar is the column that
// sees the decision on AGE either way.

// name: AccountUnknownElements :many
MATCH (a:Account) RETURN [a.dates[0]] AS dated, [a.either, null] AS mixed, [a.n, null] AS mixed_scalar ORDER BY a.id

// name: AccountBareUnknown :many
MATCH (a:Account) RETURN a.dates[0] AS dated ORDER BY a.id
