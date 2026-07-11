// name: PersonById :one
MATCH (p:Person) WHERE p.id = $id RETURN p.name, p.age
