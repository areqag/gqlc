MATCH (a:Person) RETURN a.name AS x UNION MATCH (b:Post) WITH b MATCH (c:Person) WHERE c.name = $p RETURN b.title AS x
