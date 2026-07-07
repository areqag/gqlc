MATCH (a:Post) WITH a.title AS a MATCH (a:Person) WHERE a.title = $p RETURN a
