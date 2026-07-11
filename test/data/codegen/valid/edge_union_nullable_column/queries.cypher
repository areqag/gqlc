// name: MaybeAction :one
MATCH (p:Person) WHERE p.id = $id
OPTIONAL MATCH (p)-[r:AUTHORED|LIKES]->(:Post)
RETURN r
