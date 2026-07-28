MATCH (a:Person)-[e:KNOWS]->(b:Person) REMOVE e:Foo RETURN e
