MATCH (a:Person) RETURN [p = (a)-[:KNOWS]->(b) | p] AS ps
