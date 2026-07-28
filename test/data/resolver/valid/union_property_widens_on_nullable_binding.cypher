MATCH (p:Person)
OPTIONAL MATCH (p)-[r:AUTHORED|LIKES]->(post:Post)
RETURN r.weight AS w
