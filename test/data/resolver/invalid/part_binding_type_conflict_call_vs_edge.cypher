CALL test.labels() YIELD label
WITH label
MATCH ()-[label:KNOWS]->() RETURN label
