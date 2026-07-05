MATCH (p:Person), (q:Person) WHERE p.name = $x AND q.nickname = $x RETURN p.name
