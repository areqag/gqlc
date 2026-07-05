MATCH (p:Person) WHERE p.age > 0 AND $flag RETURN p.name
