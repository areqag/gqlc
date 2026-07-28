MERGE (b:Person) ON MATCH SET b.age = 42, b.notAProp = 1 RETURN b
