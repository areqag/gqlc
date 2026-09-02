MATCH (p:Person) RETURN collect([p.id, p.age]) AS xs
