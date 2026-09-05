MATCH (a:Person)-[r:KNOWS*1..3]->(b:Person) RETURN r AS x UNION MATCH (c:Person) OPTIONAL MATCH (c)-[r2:KNOWS*1..3]->(d:Person) RETURN r2 AS x
