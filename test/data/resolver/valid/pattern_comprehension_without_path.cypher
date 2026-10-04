MATCH (a:Person) RETURN [(a)-[:KNOWS]->(b) | b.name] AS names
