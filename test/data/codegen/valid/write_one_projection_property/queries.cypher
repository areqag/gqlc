// name: CreatePersonName :one
CREATE (p:Person {id: $id, name: $name}) RETURN p.name AS name
