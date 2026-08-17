MATCH (p:Person)-[w:WORKS_AT]->(co:Company), (c:Company)-[r:REVIEWED]->(p) RETURN p
