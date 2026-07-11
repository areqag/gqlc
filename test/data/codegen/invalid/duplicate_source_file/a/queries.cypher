// name: OneQuery :one
MATCH (p:Person) WHERE p.id = $id RETURN p
