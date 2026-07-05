MATCH (a:Post) WITH a MATCH (a)-[:AUTHORED]->(p) RETURN a
