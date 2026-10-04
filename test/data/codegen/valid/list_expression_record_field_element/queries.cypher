// A list expression over a field of a record property. Each column is []any
// on purpose (bd gqlc-4ro6), for the reason list_expression_unknown_element
// gives: a.ul.u is not literally a ref, so spec model-change-f45qn mints no
// certificate and the element stays unknown. The bare a.ul.u resolves to the
// same unknown, which this backend refuses as a column it has no Go type to
// declare.
//
// What would type these wrongly is reading a.ul.u as the ref a.ul: the
// element would take the whole RECORD's shape. u is a union field and n an
// INT32 one, so the pin does not rest on a union, whose list element this
// backend decoded through agtypeListOfAny even when its width was known
// (43422c03; bd gqlc-k1dg narrows that).
//
// The union has no temporal member on purpose. With ANY<DATE | INT64> this
// package emits temporal.go with nothing naming a carrier (bd gqlc-r2dp,
// 47b60c09), which is a defect of its own and not the one this pins.
//
// AGE-only because neo4j does not store a RECORD property (StorableProperty).

// name: AccountRecordFieldElements :many
MATCH (a:Account) RETURN [a.ul.u] AS u, [a.ul.n] AS n ORDER BY a.id
