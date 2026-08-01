// name: GetLink :one
MATCH (x:Person)-[r:LIKES|WROTE]-(y:Person) RETURN r
