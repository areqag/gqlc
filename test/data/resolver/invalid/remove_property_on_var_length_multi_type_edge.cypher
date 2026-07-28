MATCH (a:Person)-[r:AUTHORED|LIKES*1..3]->(b:Post) REMOVE r.likedAt RETURN a
