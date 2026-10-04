MATCH (a:Person) WITH [p = (a)-[:KNOWS]->(b) | p] AS ps RETURN ps
