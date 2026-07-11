// name: PeopleOverAge :many
MATCH (p:Person) WHERE p.age > $minAge RETURN p.name
