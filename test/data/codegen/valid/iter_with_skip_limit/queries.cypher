// name: StreamPersonPage :iter
MATCH (p:Person) RETURN p.name ORDER BY p.age SKIP 20 LIMIT 10
