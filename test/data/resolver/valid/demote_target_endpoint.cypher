OPTIONAL MATCH (b:Post) MATCH (a:Person)-[:AUTHORED]->(b) RETURN a, b
