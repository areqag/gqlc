// name: FindPerson :many
MATCH (p:Person) WHERE p.id = $id RETURN p
