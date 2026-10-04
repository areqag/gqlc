MATCH (a:Person) RETURN a ORDER BY size([path = (a)-[:KNOWS]->(b) | path])
