// name: PeopleSince :many
MATCH (p:Person) WHERE p.since > datetime() RETURN p.name AS name
