MATCH (p:Person)-[r:AUTHORED]->(post:Post) RETURN p, r, post
