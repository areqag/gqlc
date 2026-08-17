MATCH (p:Person)-[w:WORKS_AT*0..1]->(c:Company) RETURN p
