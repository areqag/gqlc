OPTIONAL MATCH (a:Person)-[:AUTHORED]->(b:Post) WITH b MATCH (b)-[:AUTHORED]->(c:Person) RETURN b
