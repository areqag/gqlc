// name: ActionOnPost :one
MATCH (:Person)-[r:AUTHORED|LIKES|FLAGGED]->(p:Post) WHERE p.id = $postId RETURN r
