MATCH (a:Person) RETURN [p = (a)-[:KNOWS]->(b) | length(p)] AS lens
