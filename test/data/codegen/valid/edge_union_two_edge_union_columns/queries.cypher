// name: PairActions :one
MATCH (:Person)-[r1:AUTHORED|LIKES]->(:Post), (:Person)-[r2:AUTHORED|LIKES]->(:Post) RETURN r1, r2
