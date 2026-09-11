// name: StreamCreatedPeople :iter
CREATE (p:Person {name: $name}) RETURN p.name
