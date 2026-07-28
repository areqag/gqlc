MATCH (a:Person) RETURN a.name AS x UNION MATCH (c:Company) RETURN c.name AS x UNION MATCH (p:Post) RETURN p.id AS x
