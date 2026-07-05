MATCH (p:Person)-[r:LIKES]-(post:Post) RETURN p, r, post
