// name: PathActions :one
MATCH (:Person)-[r:AUTHORED|LIKES*]->(:Post) RETURN r
