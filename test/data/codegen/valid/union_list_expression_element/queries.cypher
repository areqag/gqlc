// A list EXPRESSION whose element is a nullable closed-union property (bd
// gqlc-dkcz). Not union_only_carrier_list's shape, which is a STORED list of
// unions: neo4j refuses to store that (StorableProperty), so a query-built
// list is the only way a neo4j column reaches a union element at all.
//
// Measured on origin/master 2d801f52: the element walk dispatched every
// element through decodeUnion<suffix> with no nil arm, so an Account with no
// `either` failed the whole column on `no member carries <nil>`, though every
// closed-union property is nullable. The plan's `any` exemption from the
// element star had zeroed the element's nullability along with it.
//
// live_neo4j_union_list_expression_test.go reads back an Account holding each
// member and one holding none.

// name: AccountEithers :many
MATCH (a:Account) RETURN [a.either] AS xs ORDER BY a.id
