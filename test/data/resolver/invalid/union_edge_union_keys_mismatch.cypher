MATCH (p:Person)-[r:AUTHORED|LIKES]->(x:Post) RETURN r AS e UNION MATCH (q:Person)-[r2:AUTHORED|LIKES]->(y:Note) RETURN r2 AS e
