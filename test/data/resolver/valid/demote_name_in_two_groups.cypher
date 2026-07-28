MATCH (a:Person)
OPTIONAL MATCH (a)-[:KNOWS]->(b:Person)
WITH a, b
OPTIONAL MATCH (b)-[:AUTHORED]->(c:Post)
MATCH (b)-[r:KNOWS]->(d:Person)
RETURN b, c, d
