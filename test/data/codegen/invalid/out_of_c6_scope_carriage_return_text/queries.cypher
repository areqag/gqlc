// The emission carries a query's text through a Go raw string, whose value
// is the source bytes with every carriage return DISCARDED -- the Go spec's
// rule, not an implementation detail. So a carriage return is unrepresentable
// for the same reason a backtick is, and is refused beside it.
//
// This carriage return is bare, not the CR of a CRLF: queryfile reads with
// bufio.ScanLines, which drops a line ending's CR before SourceText ever sees
// it, so no line-ending convention reaches here. Only content does. This one
// sits inside a Cypher string literal, so the query parses and resolves and
// the refusal is codegen's own.
//
// Unrefused it forges the AGE dollar-quote delimiter: dollarTag is handed
// bytes holding no $gqlc$ and the SQL parser is handed bytes holding one, so
// the literal closes inside the query body. It corrupts the neo4j emission
// too, which is why every target is enrolled -- there the discarded byte
// glues the tokens it separated.

// name: PeopleNotTagged :many
MATCH (p:Person) WHERE toString(p.id) <> '$gqlc$' RETURN p.id AS id
