MATCH (a:Person) WITH a.name AS nm MATCH (a:Post) WHERE a.title = $p RETURN a
