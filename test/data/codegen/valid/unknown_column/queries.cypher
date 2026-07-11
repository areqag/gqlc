// name: OneUnknown :one
MATCH (p:Person) RETURN foo(p.id) AS r
