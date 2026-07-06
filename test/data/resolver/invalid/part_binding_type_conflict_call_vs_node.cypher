CALL test.labels() YIELD label
WITH label
MATCH (label:Person) RETURN label
