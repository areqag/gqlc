MATCH (p:Person) CALL test.labels() YIELD label
RETURN p.name, label
