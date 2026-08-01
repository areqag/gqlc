// name: GetAction :one
MATCH (x:Person)-[r:LIKES|WROTE]-(y:Post) RETURN r
