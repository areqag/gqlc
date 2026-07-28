MATCH (a:Person)-[r:KNOWS]->(b:Person) WHERE r.notAProp = $x RETURN a
