MATCH (a:Person)-[r:KNOWS*1..3]->(b:Person) SET r.since = date('2020-01-01') RETURN a
