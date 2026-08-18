MATCH (p)-[w:WORKS_AT]->(c:Company)
MATCH (c)-[h:HAS_DESK]->(d:Desk)
RETURN p.personOnly AS only
