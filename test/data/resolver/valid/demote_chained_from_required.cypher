OPTIONAL MATCH (p:Person)-[r1:AUTHORED]->(post:Post) MATCH (post)-[r2:AUTHORED]->(author:Person) RETURN p, r1, post, r2, author
