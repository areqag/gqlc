// name: OnePersonWithId :one
MATCH (p:Person) RETURN p, elementId(p) AS id
