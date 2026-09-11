MERGE (p:Person) ON CREATE SET p.employeeId = 1 RETURN p.id
