// name: GetAction :one
MATCH (p:Person)-[r:AUTHORED]->(q:Post)
MERGE (p)-[r:LIKES]->(q)
RETURN r
