MATCH (p:Person), (q:Person) WHERE p.age = $threshold AND q.age = $threshold RETURN p.name
