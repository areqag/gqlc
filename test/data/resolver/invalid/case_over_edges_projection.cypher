MATCH (a:Person)-[r:KNOWS]->(b:Person)-[s:KNOWS]->(c:Person) RETURN CASE WHEN a.id > 1 THEN r ELSE s END AS x
