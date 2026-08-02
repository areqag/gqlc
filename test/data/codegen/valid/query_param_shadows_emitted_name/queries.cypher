// Every parameter here is named after something an emitted method
// resolves. The single-parameter form names its argument after the
// parameter the author wrote, so each of these puts that name in the
// scope the method's own identifiers resolve in.
//
// The first group shadows a local the body declares. $stmt against a
// STRING property is the silent one: the widths agree, so the
// composition assigns the SQL text over the caller's argument and then
// binds it as the value of $stmt. Nothing fails — the query looks for a
// person named after its own statement.
//
// The second group shadows the package-level query-text const, which a
// body references and never declares. That one is worse: the const is a
// string and the composer takes a string, so the caller's argument does
// not merely get overwritten, it *becomes* the statement.
// ConstShadowOne(ctx, "MATCH (n) DETACH DELETE n") would run that text
// with no concatenation anywhere to find. The const name is derived from
// the method name, so each of these is named to reproduce its own.
//
// Both cardinalities that reach the composition are in each group,
// because a read and an :exec share it — and on the Neo4j targets they
// are two separate reference sites.

// name: PersonByStmt :one
MATCH (p:Person) WHERE p.name = $stmt RETURN p.name

// name: PersonByArgs :many
MATCH (p:Person) WHERE p.name = $args RETURN p.name

// name: PersonByRows :many
MATCH (p:Person) WHERE p.name = $rows RETURN p.name

// name: PersonByRaw0 :one
MATCH (p:Person) WHERE p.name = $raw0 RETURN p.name

// name: PersonByValue0 :one
MATCH (p:Person) WHERE p.name = $value0 RETURN p.name

// name: DeleteByStmt :exec
MATCH (p:Person) WHERE p.name = $stmt DELETE p

// name: DeleteByArgs :exec
MATCH (p:Person) WHERE p.name = $args DELETE p

// name: ConstShadowOne :one
MATCH (p:Person) WHERE p.name = $constShadowOneQueryText RETURN p.name

// name: ConstShadowMany :many
MATCH (p:Person) WHERE p.name = $constShadowManyQueryText RETURN p.name

// name: ConstShadowExec :exec
MATCH (p:Person) WHERE p.name = $constShadowExecQueryText DELETE p
