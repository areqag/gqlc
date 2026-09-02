MATCH (p:Person) WITH p.id + p.age AS x RETURN [x, x] AS xs
