// The emission carries a query's text through a Go raw string literal, and Go
// source must be valid UTF-8. A byte that is not makes the generated file
// unparseable, so nothing ships; unrefused, what the user is handed is
// `illegal UTF-8 encoding` against a line of a GENERATED file, naming neither
// this query nor the byte (bd gqlc-32n53).
//
// The offending byte is 0xff, which begins no UTF-8 sequence at all, so this
// is not a truncated or over-long encoding but a byte the format has no
// reading for. It sits inside a Cypher string literal for the reason the NUL
// fixture's does: bare, the openCypher lexer refuses it first and the refusal
// under test is never reached.

// name: PeopleNotTagged :many
MATCH (p:Person) WHERE toString(p.id) <> 'taÿg' RETURN p.id AS id
