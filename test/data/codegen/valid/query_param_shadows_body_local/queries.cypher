// Every parameter here is named after a local an emitted query body
// declares. The single-parameter form names its argument after the
// parameter the author wrote, so each of these puts that name in the
// body's own scope.
//
// $stmt against a STRING property is the silent one: the widths agree,
// so the composition assigns the SQL text over the caller's argument and
// then binds it as the value of $stmt. Nothing fails — the query looks
// for a person named after its own statement. Both cardinalities that
// reach the composition are here, because a read and an :exec share it.

// name: PersonByStmt :one
MATCH (p:Person) WHERE p.name = $stmt RETURN p.name

// name: PersonByArgs :many
MATCH (p:Person) WHERE p.name = $args RETURN p.name

// name: PersonByRows :many
MATCH (p:Person) WHERE p.name = $rows RETURN p.name

// name: PersonByRaw0 :one
MATCH (p:Person) WHERE p.name = $raw0 RETURN p.name

// name: PersonByValue0 :one
MATCH (p:Person) WHERE p.name = $value0 RETURN p.name

// name: DeleteByStmt :exec
MATCH (p:Person) WHERE p.name = $stmt DELETE p

// name: DeleteByArgs :exec
MATCH (p:Person) WHERE p.name = $args DELETE p
