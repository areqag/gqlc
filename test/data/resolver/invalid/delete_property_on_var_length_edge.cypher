MATCH (a:Person)-[r:KNOWS*1..3]->(b:Person) DELETE r.since
