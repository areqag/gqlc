OPTIONAL MATCH (p:Person) MATCH (p)-[r:KNOWS*0..3]->(q:Person) RETURN p, r, q
