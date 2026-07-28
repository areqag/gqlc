MATCH (a:Person)-[r:AUTHORED|LIKES]->(b:Post) REMOVE r.likedAt RETURN a
