MATCH (p:Person) WHERE exists { MATCH (q:Person) RETURN q SKIP $off } RETURN p.name
