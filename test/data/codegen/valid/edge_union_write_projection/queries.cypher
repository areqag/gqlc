// name: MergeAction :one
MERGE (p:Person {id: $personId})-[r:AUTHORED|LIKES]->(post:Post {id: $postId}) RETURN r
