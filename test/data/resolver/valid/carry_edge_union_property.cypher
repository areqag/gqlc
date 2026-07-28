MATCH (p:Person)-[r:AUTHORED|LIKES]->(post:Post) WITH r RETURN r.likedAt
