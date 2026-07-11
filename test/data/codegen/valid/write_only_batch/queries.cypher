// name: RemovePerson :exec
MATCH (p:Person) WHERE p.id = $id DELETE p

// name: CreatePerson :exec
CREATE (p:Person {id: $id, name: $name})
