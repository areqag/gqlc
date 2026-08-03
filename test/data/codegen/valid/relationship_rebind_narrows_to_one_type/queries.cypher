// name: GetAction :one
MATCH (p:Person)-[r:AUTHORED|LIKES]->(q:Post), (p)-[r:LIKES|SHARED]->(q) RETURN r
