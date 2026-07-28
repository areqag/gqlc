MATCH (a:Person)-[r:KNOWS*1..3]->(b:Person) WHERE r.since = $x RETURN a
