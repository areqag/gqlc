MATCH (a:Person)-[r:KNOWS*1..3]->(b:Person) WITH r MATCH (c:Person)-[r:KNOWS]->(d:Person) RETURN r
