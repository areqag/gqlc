MATCH (a:Person)-[r:AUTHORED|LIKES]->(b:Post) SET r.notAProp = 1 RETURN a
