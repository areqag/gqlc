MATCH (:Person)-[r:WORKS_AT]->(c) WITH c MATCH (c)<-[q:WORKS_AT]-(p:Person) RETURN p.personOnly
