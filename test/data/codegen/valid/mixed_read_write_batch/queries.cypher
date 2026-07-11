// name: GetPersonName :one
MATCH (p:Person) WHERE p.id = $id RETURN p.name AS name

// name: RemovePerson :exec
MATCH (p:Person) WHERE p.id = $id DELETE p
