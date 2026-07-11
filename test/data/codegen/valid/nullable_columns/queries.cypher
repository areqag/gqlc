// name: PersonProfile :one
MATCH (p:Person) WHERE p.id = $id RETURN p.name, p.nickname, p.age
