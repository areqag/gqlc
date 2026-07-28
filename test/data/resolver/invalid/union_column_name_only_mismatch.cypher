MATCH (a:Person) RETURN a.name AS x UNION MATCH (c:Company) RETURN c.name AS y
