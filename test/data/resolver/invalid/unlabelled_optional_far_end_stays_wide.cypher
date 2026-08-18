MATCH (p)-[w:WORKS_AT]->(c:Company)
OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk)
RETURN p.personOnly AS only
