MATCH (p:Person)-[q:WORKS_AT]->(c) OPTIONAL MATCH (c)-[h:HAS_DESK]->(d:Desk) WITH c MATCH (c)<-[w:WORKS_AT]-(p2:Person) RETURN p2.personOnly
