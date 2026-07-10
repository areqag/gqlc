OPTIONAL MATCH (a:Person)-[r1:AUTHORED]->(b:Post) MATCH (b) RETURN a, r1, b
