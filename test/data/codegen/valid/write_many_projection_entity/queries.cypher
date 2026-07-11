// name: MarkAdults :many
MATCH (p:Person) WHERE p.age >= $minAge SET p.checked = true RETURN p
