// union_only_carrier_under_record with every read of the record taken away,
// which is union_only_carrier_undecoded's claim one container down. Read that
// fixture's head comment first.
//
// The record's site alias is emitted either way, the entity's struct being
// exported, and it spells the union field as *any. What names Date is the
// union's helper pair, and nothing here reaches it.
//
// AGE alone, for record_property's reason: neo4j does not hold a map in a
// property. This is bd gqlc-r2dp's reproducer as filed.

// name: AccountIDs :many
MATCH (a:Account) RETURN a.id AS id
