MATCH (a:Person)-[:KNOWS]->(b:Person) RETURN [a, b][0] AS x
