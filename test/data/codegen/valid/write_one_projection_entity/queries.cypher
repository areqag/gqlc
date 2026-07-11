// name: CreatePerson :one
CREATE (p:Person {id: $id, name: $name}) RETURN p
