OPTIONAL MATCH (a:Person)-[r1:AUTHORED]->(b:Post) MATCH (c:Person)-[r2:KNOWS]->(d:Person) RETURN a, b, c, d
