MATCH (p:Person)
OPTIONAL MATCH (p)-[q:WORKS_AT]->(c)
OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk)
WITH c
MATCH (e:Employee)-[w:WORKS_AT]->(c)
RETURN c
