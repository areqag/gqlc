MATCH (p:Employee)-[q:WORKS_AT]->(c)
OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk)
RETURN c.largeId
