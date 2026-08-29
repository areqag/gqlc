// name: Get :one
MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r AS userName

// name: GetUser :one
MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r AS name
