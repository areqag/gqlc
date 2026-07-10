OPTIONAL MATCH (a:Person)-[r1:KNOWS]->(b:Person)-[r2:AUTHORED]->(c:Post) MATCH (c)-[r3:AUTHORED]->(d:Person) RETURN a, r1, b, r2, c, d
