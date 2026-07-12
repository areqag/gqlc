MATCH (a:Post) WHERE a.age > $p RETURN a.id AS x UNION MATCH (a:Person) RETURN a.id AS x
