// name: PeopleNearOrigin :many
MATCH (p:Person) WHERE p.x = point({x: 1, y: 2}) RETURN p.name AS name
