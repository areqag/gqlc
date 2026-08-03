// name: GetAction :one
MATCH (p:Person)-[r:AUTHORED|LIKES|SHARED]->(q:Post), (p)-[r:AUTHORED|LIKES]->(q) RETURN r
