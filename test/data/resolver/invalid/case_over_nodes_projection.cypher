MATCH (a:Person)-[:KNOWS]->(b:Person) RETURN CASE WHEN a.id > 1 THEN a ELSE b END AS x
