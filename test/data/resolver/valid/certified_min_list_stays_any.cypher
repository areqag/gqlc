MATCH (p:Person) RETURN min([p.id, p.age]) AS m
