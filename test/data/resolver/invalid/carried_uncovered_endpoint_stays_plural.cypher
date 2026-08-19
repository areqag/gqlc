MATCH (p:Employee) OPTIONAL MATCH (p)-[q:WORKS_AT]->(c) WITH c MATCH (c)<-[w:WORKS_AT]-(p2:Person) RETURN p2.employeeId
