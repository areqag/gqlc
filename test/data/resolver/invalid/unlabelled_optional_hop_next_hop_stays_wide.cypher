MATCH (p:Person)-[q:WORKS_AT]->(c)
OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk)
MATCH (c)<-[w:WORKS_AT]-(x)
RETURN x.personOnly
