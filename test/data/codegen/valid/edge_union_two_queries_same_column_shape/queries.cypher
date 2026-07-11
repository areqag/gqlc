// name: GetAction :one
MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r

// name: ListActions :many
MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r
