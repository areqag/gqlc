// name: PeopleBetween :many
MATCH (p:Person) WHERE p.age > $_ AND p.age < $max RETURN p.age
