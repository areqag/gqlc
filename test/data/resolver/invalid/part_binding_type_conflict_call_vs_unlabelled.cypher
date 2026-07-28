CALL test.labels() YIELD label
WITH label
MATCH (label)-[:AUTHORED]->(b:Post) RETURN b
