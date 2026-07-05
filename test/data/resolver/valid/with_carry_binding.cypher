MATCH (a:Person) WITH a MATCH (a)-[:AUTHORED]->(b:Post) RETURN a, b
