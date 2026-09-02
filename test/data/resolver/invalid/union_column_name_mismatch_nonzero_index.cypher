MATCH (a:Person) RETURN a.name AS x, a.id AS y UNION MATCH (c:Company) RETURN c.name AS x, c.ein AS z
