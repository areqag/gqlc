MATCH (p:Person) WITH count(p) AS c
MATCH (x:Person)-[c:AUTHORED]->(y:Post)
RETURN c
