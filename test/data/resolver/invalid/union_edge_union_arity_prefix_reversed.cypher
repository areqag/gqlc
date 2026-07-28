MATCH (p:Person)-[r:AUTHORED|LIKES]->(post:Post) RETURN r UNION MATCH (p:Person)-[r:AUTHORED|LIKES|SHARED]->(post:Post) RETURN r
