// name: PersonAtNow :one
MATCH (p:Person) RETURN p, datetime() AS now, [1, 2, 3] AS xs
