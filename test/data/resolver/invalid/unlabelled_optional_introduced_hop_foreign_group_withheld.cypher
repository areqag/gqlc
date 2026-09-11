MATCH (p:Person)
OPTIONAL MATCH (p)-[q:WORKS_AT]->(c)
OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk)
RETURN c.smallOnly
