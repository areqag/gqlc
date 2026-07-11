OPTIONAL MATCH (a:Person)-[r1:AUTHORED]->(b:Post) WITH a, b MATCH (b)-[r2:AUTHORED]->(c:Person) RETURN a, b, c
