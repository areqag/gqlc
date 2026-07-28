CALL test.labels() YIELD label
MATCH (p:Person) WHERE label.zip = $z
RETURN p.name AS nm
