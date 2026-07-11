MATCH (p:Person) WHERE exists { MATCH (q:Person) RETURN q LIMIT $lim } RETURN p.name
