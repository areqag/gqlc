// name: FindPerson :one
MATCH (p:Person) WHERE p.id = $id RETURN p

// name: FindPerson :many
MATCH (p:Person) WHERE p.name = $name RETURN p
