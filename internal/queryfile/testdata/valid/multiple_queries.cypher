// Query file with multiple annotated queries.
// The leading comments and blank lines are permitted per §4.1.

// name: FindPerson :one
MATCH (p:Person) WHERE p.id = $id RETURN p

// name: ListPeople :many
MATCH (p:Person)
RETURN p.name AS name, p.age AS age
ORDER BY p.name

// name: DeletePerson :exec
MATCH (p:Person) WHERE p.id = $id DELETE p
