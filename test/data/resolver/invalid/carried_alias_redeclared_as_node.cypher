MATCH (p:Person) WITH count(p) AS c
MATCH (y:Person)
OPTIONAL MATCH (c)-[a:AUTHORED]->(y)
RETURN c
