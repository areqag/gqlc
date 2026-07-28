MATCH (p:Person) WHERE p.name = $x OPTIONAL MATCH (q:Person) WHERE q.name = $x RETURN p.name
