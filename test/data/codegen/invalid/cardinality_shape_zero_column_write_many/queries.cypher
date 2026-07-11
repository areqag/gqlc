// name: RemovePeople :many
MATCH (p:Person) WHERE p.id = $id DELETE p
