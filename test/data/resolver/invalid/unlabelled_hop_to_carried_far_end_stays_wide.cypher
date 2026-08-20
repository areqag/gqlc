MATCH (dd:Desk) WITH dd
MATCH (p:Person)-[q:WORKS_AT]->(c)
MATCH (c)-[h:HAS_DESK]->(dd)
RETURN c.smallOnly
