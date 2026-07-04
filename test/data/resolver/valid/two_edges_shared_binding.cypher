MATCH (p:Person)-[a:AUTHORED]->(post:Post)<-[l:LIKES]-(p2:Person) RETURN p, post, p2
