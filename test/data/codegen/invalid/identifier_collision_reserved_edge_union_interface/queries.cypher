// name: Read :one
MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r AS querier
