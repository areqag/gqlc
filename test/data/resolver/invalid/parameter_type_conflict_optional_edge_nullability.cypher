MATCH (a:Person)-[r:AUTHORED]->(b:Post) WHERE r.views = $x OPTIONAL MATCH (c:Person)-[s:AUTHORED]->(d:Post) WHERE s.views = $x RETURN a
