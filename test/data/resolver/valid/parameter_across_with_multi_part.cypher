MATCH (a:Person) WHERE a.id = $p WITH a MATCH (a)-[e:AUTHORED]->(pst:Post) WHERE e.views = $p RETURN pst.title
