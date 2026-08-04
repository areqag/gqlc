MATCH (p:Person) MERGE (p)-[w:WORKS_AT]->(c:Company) RETURN p
