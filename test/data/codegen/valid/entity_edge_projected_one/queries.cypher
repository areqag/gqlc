// name: OneActedIn :one
MATCH (:Person)-[r:ACTED_IN]->(:Movie) RETURN r
