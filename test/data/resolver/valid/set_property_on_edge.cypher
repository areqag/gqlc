MATCH (a:Person)-[e:AUTHORED]->(b:Post) SET e.views = 0 RETURN e
