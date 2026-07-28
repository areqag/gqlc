MATCH (p:Person) WHERE p.name = 'a' AND $x RETURN p.name LIMIT $x
