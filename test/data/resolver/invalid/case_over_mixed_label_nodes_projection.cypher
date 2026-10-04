MATCH (a:Person)-[:AUTHORED]->(p:Post) RETURN CASE WHEN a.id > 1 THEN a ELSE p END AS x
