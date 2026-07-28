MATCH (a:Person)-[r:AUTHORED|LIKES]->(b:Post) SET r.likedAt = date('2020-01-01') RETURN a
