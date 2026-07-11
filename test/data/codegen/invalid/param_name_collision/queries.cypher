// name: PeopleBetween :many
MATCH (p:Person) WHERE p.age > $min_age AND p.age < $minAge RETURN p.age
