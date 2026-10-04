MATCH (a:Person)-[:KNOWS]->(b:Person) WITH CASE WHEN a.id > 1 THEN a ELSE b END AS x RETURN x.name AS n
