// name: PersonAction :one
MATCH (p:Person)-[r:AUTHORED|LIKES]->(:Post) WHERE p.id = $id RETURN p, r
