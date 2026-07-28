MATCH (a:Person)-[:KNOWS]->(x) MATCH (x)-[r:AUTHORED]-(b:Post) RETURN r
