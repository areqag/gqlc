CALL test.labels() YIELD label
MATCH (p:Person) RETURN label, p.name
