MATCH (a:Person) RETURN a.id AS x UNION MATCH (a:Post) WHERE a.title = $p RETURN a.id AS x
