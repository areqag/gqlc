OPTIONAL MATCH (a:Person)-[r:AUTHORED]->(b:Post) WITH r, b MATCH (b)-[:AUTHORED]->(c:Person) RETURN r, b
