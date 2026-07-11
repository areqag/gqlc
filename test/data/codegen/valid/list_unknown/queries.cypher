// name: OneUnknownList :one
MATCH (p:Person) RETURN [foo(p.id)] AS xs
