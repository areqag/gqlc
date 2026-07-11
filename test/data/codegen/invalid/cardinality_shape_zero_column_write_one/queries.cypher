// name: RemovePerson :one
MATCH (p:Person) WHERE p.id = $id DELETE p
