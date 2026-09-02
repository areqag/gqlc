// name: PersonId :many
MATCH (p:Person) RETURN p.id AS id

// name: PersonPair :many
MATCH (p:Person) RETURN [p.id, p.age] AS pair

// name: PersonCollectId :one
MATCH (p:Person) RETURN collect(p.id) AS ids

// name: PersonCollectPair :one
MATCH (p:Person) RETURN collect([p.id, p.age]) AS pairs

// name: PersonNullablePair :many
MATCH (p:Person) RETURN [p.score, p.score] AS scores

// name: PersonNarrowPair :many
MATCH (p:Person) RETURN [p.rank, p.rank] AS ranks

// name: PersonFoldPair :many
MATCH (p:Person) RETURN [p.id + p.age] AS folded
