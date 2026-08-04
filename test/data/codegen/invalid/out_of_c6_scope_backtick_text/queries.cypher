// The emission carries a query's text through a Go raw string, which
// cannot hold a backtick. The backtick here sits inside a Cypher string
// literal, so the query parses and resolves and the refusal is codegen's
// own; a backtick used as a delimited identifier would be refused
// upstream by the resolver instead.

// name: PeopleNotTicked :many
MATCH (p:Person) WHERE toString(p.id) <> '`' RETURN p.id AS id
