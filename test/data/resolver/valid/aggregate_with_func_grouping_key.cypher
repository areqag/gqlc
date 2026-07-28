MATCH (p:Person) RETURN toString(p.age) AS s, count(p) AS c
