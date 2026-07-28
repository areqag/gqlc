MATCH (a:Person)-[r:AUTHORED|LIKES]->(b:Post) REMOVE r.notAProp RETURN a
