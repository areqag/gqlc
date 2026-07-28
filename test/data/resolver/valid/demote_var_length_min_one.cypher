OPTIONAL MATCH (p:Person) MATCH (p)-[r:KNOWS*1..3]->(q:Person) RETURN p, r, q
