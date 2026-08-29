// name: PostsTouched :many
MATCH (a:Person)-[r:AUTHORED|LIKES]->(b:Post) RETURN b.title AS title
