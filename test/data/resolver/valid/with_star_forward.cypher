MATCH (a:Person) WITH * MATCH (a)-[:AUTHORED]->(b:Post) RETURN a, b
