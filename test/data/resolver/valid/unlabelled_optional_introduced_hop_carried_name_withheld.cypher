MATCH (p:Person) WITH count(p) AS c
MATCH (y:Person)
OPTIONAL MATCH (c)-[a:AUTHORED]->(y)
OPTIONAL MATCH (c)-[r:AUTHORED|LIKES]->(y2:Person)
RETURN r
