OPTIONAL MATCH (a:Person)-[:AUTHORED]->(b:Post) MATCH (c:Person)-[:KNOWS]->(d:Person) RETURN a, b, c, d
