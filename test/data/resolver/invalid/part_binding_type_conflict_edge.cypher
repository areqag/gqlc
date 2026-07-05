MATCH (a:Person)-[r:KNOWS]->(b:Person) WITH r MATCH (x:Person)-[r:LIKES]->(y:Post) RETURN r
