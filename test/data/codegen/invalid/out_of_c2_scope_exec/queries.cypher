// name: RemovePerson :exec
MATCH (p:Person) WHERE p.id = $id DELETE p
