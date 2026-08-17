MATCH (p:Person) OPTIONAL MATCH (p)-[r:WORKS_AT]->(c:Company) RETURN p
