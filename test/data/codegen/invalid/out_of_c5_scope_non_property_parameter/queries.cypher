// name: PeoplePaged :many
MATCH (p:Person) RETURN p.id AS id SKIP $offset LIMIT 10
