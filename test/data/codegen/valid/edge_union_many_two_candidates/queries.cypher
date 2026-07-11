// name: ListActions :many
MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r
