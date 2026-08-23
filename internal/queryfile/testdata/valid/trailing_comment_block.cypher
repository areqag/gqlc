// Query file exercising comment placement around query bodies (bd gqlc-kc5w).
// Text crosses the wire verbatim, so free-standing prose must not join the
// preceding query.

// name: FindPerson :one
MATCH (p:Person {id: $id})
// an interior comment stays: it is part of this query
RETURN p

// The block below is free-standing prose introducing the NEXT query. It is
// separated from FindPerson's last line by a blank line, and must NOT end up
// inside FindPerson's statement text.

// name: ListPeople :many
MATCH (p:Person) RETURN p
// an abutting comment stays: no blank line separates it from the query

// name: DeletePerson :exec
MATCH (p:Person {id: $id}) DETACH DELETE p

// Trailing prose at end of file, likewise dropped.
