// name: PeopleAtHeight :many
MATCH (p:Person) WHERE p.height = $h RETURN p.name
