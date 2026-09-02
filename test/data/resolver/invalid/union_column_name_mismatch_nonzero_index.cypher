MATCH (a:Person) RETURN a.name AS x, a.id AS y, a.age AS p UNION MATCH (c:Company) RETURN c.name AS x, c.ein AS y, c.founded AS q
