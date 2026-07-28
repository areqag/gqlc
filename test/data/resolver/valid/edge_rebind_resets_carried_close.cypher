MATCH (q:Post)-[r:AUTHORED|LIKES]->(y:Person)
WITH r
MATCH (p:Person)-[r:AUTHORED|LIKES]->(x:Post)
RETURN r
