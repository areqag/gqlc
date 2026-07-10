OPTIONAL MATCH (a:Person)-[r1:KNOWS]->(b:Person) OPTIONAL MATCH (b)-[r2:KNOWS]->(c:Person) MATCH (c)-[r3:KNOWS]->(d:Person) RETURN a, r1, b, r2, c, d
