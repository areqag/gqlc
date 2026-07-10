OPTIONAL MATCH (a:Person)-[r1:AUTHORED]->(b:Post) OPTIONAL MATCH (c:Person)-[r2:LIKES]->(d:Post) MATCH (c)-[r3:KNOWS]->(e:Person) RETURN a, b, c, d, e
