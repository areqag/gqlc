// name: GetAction :one
MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r
