MATCH (a:Person) RETURN a.name AS nm UNION ALL MATCH (c:Company) RETURN DISTINCT c.name AS nm
