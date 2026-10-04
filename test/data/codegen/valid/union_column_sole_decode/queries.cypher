// The batch's only decode is a closed union column, and no entity is
// decoded. On Apache AGE that makes the union dispatch the only thing in
// models.go naming bytes — no entity, record, map or list splitter is
// emitted — so the import is held by the union disjunct of importsBytes
// alone (bd gqlc-m1dk).

// name: Eithers :many
MATCH (t:Thing) RETURN t.either AS either
