MATCH (a:Person)-[r:AUTHORED]->(b:Post) SET r.notAProp = 1 RETURN a
