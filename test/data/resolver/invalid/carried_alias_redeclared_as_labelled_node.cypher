MATCH (p:Person) WITH count(p) AS c
MATCH (c:Post)
RETURN c
