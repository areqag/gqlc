// name: GetAction :one
MATCH (:Person)-[r:AUTHORED|LIKES|REPOSTED]->(:Post) RETURN r
