MATCH (a:Person)-[r:AUTHORED]->(b:Post) REMOVE r.notAProp RETURN a
