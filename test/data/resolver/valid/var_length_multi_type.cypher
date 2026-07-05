MATCH (p:Person)-[r:AUTHORED|LIKES*1..3]->(post:Post) RETURN r
