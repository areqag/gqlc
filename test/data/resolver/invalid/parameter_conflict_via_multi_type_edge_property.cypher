MATCH (a:Person)-[r:AUTHORED|LIKES]->(b:Post) WHERE r.likedAt = $x RETURN a LIMIT $x
