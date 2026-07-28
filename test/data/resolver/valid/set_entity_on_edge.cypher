MATCH (a:Person)-[r:KNOWS]->(b:Person) SET r = {since: date('2020-01-01')} RETURN a
