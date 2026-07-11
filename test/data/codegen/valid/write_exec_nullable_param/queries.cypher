// name: RemoveByTag :exec
MATCH (p:Person {optionalTag: $tag}) DELETE p
