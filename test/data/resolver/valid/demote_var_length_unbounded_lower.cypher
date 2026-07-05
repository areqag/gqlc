OPTIONAL MATCH (p:Person) MATCH (p)-[r:KNOWS*]->(q:Person) RETURN p, r, q
