MATCH (a:Person) RETURN [p = (a)-[:KNOWS]->(b) WHERE length(p) > 1 | b.name] AS names
