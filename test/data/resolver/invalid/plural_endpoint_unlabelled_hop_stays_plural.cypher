MATCH (:Person)-[r:WORKS_AT]->(c)<-[q:WORKS_AT]-(p:Person) RETURN p
