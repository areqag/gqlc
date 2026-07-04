MATCH (p:Person)-[r:AUTHORED { publishedAt: $pubTime }]->(post:Post) RETURN p.name
