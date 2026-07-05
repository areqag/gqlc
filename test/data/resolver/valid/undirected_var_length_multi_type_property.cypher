MATCH (p:Person)-[r:AUTHORED|LIKES*1..2]-(post:Post) RETURN r
