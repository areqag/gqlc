MATCH (p:Person), (c:Company) CREATE (p)-[w:WORKS_AT]->(c) RETURN p
