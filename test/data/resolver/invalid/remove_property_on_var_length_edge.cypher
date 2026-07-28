MATCH (a:Person)-[r:KNOWS*1..3]->(b:Person) REMOVE r.since RETURN a
