// name: GetAction :one
MATCH (:Person)-[r:AUTHORED]->(:Post), (:Person)-[r:LIKES]->(:Post) RETURN r
