MATCH (a:Person)-[r:KNOWS]->(b:Person) WITH r
MATCH (y:Person)
OPTIONAL MATCH (r)-[k:AUTHORED]->(y)
RETURN k
