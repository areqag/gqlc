MATCH (a:Person) WITH a.name AS a MATCH (a:Post) WHERE a.title = $p RETURN a
