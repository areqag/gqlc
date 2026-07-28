MATCH (a:Person)-[r:KNOWS*1..3]->(b:Person) RETURN r AS x UNION MATCH (c:Person)-[r2:AUTHORED*1..3]->(d:Post) RETURN r2 AS x
