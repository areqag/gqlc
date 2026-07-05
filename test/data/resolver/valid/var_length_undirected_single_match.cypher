MATCH (p:Person)-[r:LIKES*1..2]-(post:Post) RETURN r
