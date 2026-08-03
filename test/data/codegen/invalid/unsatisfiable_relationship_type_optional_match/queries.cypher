// name: GetAction :one
MATCH (p:Person)-[r:AUTHORED]->(:Post)
OPTIONAL MATCH (p)-[r:LIKES]->(:Post)
RETURN r
