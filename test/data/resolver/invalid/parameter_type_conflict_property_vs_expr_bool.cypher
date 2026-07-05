MATCH (p:Person) WHERE p.age = $x AND $x RETURN p.name
