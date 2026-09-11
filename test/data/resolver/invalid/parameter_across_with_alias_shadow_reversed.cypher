MATCH (a:Post) WITH a.title AS t MATCH (a:Person) WHERE a.title = $p RETURN a
