MATCH (p:Person)-[w:WORKS_AT*0..2]->(c:Company) RETURN p
