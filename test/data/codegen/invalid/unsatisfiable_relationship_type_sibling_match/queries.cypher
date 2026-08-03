// name: GetAction :one
MATCH (:Person)-[r:AUTHORED]->(:Post)
MATCH (:Person)-[r:LIKES]->(:Post)
RETURN r
