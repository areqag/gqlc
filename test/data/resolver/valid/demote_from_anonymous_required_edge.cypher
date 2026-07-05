OPTIONAL MATCH (a:Person)-[:AUTHORED]->(b:Post) MATCH (a)-[:AUTHORED]->(c:Post) RETURN a, b, c
