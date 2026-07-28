MATCH (a:Person)-[r:AUTHORED|LIKES*1..3]->(b:Post) DELETE r.likedAt
