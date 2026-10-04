MATCH (a:Person) WHERE size([p = (a)-[:KNOWS]->(b) | p]) > 0 RETURN a
