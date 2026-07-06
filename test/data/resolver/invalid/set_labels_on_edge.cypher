MATCH (a:Person)-[e:KNOWS]->(b:Person) SET e:Foo RETURN e
