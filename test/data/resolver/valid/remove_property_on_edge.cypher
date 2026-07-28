MATCH (a:Person)-[r:KNOWS]->(b:Person) REMOVE r.since RETURN a
