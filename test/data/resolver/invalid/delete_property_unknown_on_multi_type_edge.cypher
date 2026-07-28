MATCH (a:Person)-[r:AUTHORED|LIKES]->(b:Post) DELETE r.notAProp
