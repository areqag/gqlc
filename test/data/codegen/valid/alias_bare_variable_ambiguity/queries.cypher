// name: PeopleBare :many
MATCH (p:Person) RETURN p.name, p.age

// name: PeopleAliased :many
MATCH (p:Person) RETURN p.name AS name, p.age
