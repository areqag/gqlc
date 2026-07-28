MATCH (a)-[:AUTHORED|KNOWS]->(x:Person), (a)-[:EMPLOYS|KNOWS]->(y:Person) RETURN a
