OPTIONAL MATCH (a:Person) WITH a OPTIONAL MATCH (a)-[:AUTHORED]->(b:Post) WITH a, b MATCH (b)-[:AUTHORED]->(c:Person) RETURN a, b, c
