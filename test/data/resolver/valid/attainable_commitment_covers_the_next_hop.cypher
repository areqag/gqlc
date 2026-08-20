MATCH (c)-[h:HAS_DESK]->(d:Desk)
OPTIONAL MATCH (p:Person)-[q:WORKS_AT]->(c)
MATCH (c)<-[w:WORKS_AT]-(x:Person)
RETURN x.personOnly
