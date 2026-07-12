// name: MarkStale :exec
MATCH (p:Person) WHERE p.updatedAt < $since SET p.stale = true
