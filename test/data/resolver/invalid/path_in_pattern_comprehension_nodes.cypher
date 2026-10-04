MATCH (a:Person) RETURN [p = (a)-[:KNOWS]->(b) | nodes(p)] AS ns
