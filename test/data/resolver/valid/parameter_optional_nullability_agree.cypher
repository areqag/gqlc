MATCH (p:Person) WHERE p.nickname = $x OPTIONAL MATCH (q:Person) WHERE q.nickname = $x RETURN p.name
