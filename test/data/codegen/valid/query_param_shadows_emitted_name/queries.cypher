// Every parameter here is named after something an emitted method
// resolves, so this batch is where the corpus holds the emitted Go
// argument to a name the generator owns. The single-parameter form used
// to name its argument after the parameter the author wrote, which put
// that name in the scope the method's own identifiers resolve in; it
// binds `arg` now, so none of these reaches the emission at all.
//
// The four groups are the four things that scope used to hold, and each
// of them broke differently.
//
// The first group is a local the body declares. $stmt against a STRING
// property was the silent one: the widths agree, so the composition
// assigned the SQL text over the caller's argument and then bound it as
// the value of $stmt. Nothing failed — the query looked for a person
// named after its own statement.
//
// The second group is the package-level query-text const, which a body
// references and never declares. That one was worse: the const is a
// string and the composer takes a string, so the caller's argument did
// not merely get overwritten, it *became* the statement.
// ConstShadowOne(ctx, "MATCH (n) DETACH DELETE n") would have run that
// text with no concatenation anywhere to find. The const name derives
// from the method name, so each of these is named to reproduce its own.
//
// The third group is what the signature itself binds — the receiver and
// the context argument — and the fourth is a package-level name the body
// resolves but no emitted declaration introduces: an import, and a
// helper. Those four did not compile, and generation reported none of
// them, because the format gate parses the emission and does not
// type-check it. $Blank is the odd one out: it mangled to the empty
// string and reached gofmt as a parameter with no name, so it was the
// only member of the class that failed loudly, and it failed pointing at
// a column in querier.go rather than at the parameter.
//
// These are here so the corpus compiles them: this fixture is enrolled
// in all three targets, so TestGoldenBuild type-checks every one of
// these methods, which is the check the format gate cannot do.
//
// Both cardinalities that reach the composition are in the first two
// groups, because a read and an :exec share it — and on the Neo4j
// targets they are two separate reference sites.

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

// name: ReceiverShadow :one
MATCH (p:Person) WHERE p.name = $q RETURN p.name

// name: ContextShadow :one
MATCH (p:Person) WHERE p.name = $ctx RETURN p.name

// name: ImportShadow :one
MATCH (p:Person) WHERE p.name = $fmt RETURN p.name

// name: HelperShadow :one
MATCH (p:Person) WHERE p.name = $agtypeArgs RETURN p.name

// name: ErrShadow :one
MATCH (p:Person) WHERE p.name = $err RETURN p.name

// name: RecordsShadow :many
MATCH (p:Person) WHERE p.name = $records RETURN p.name

// name: OutShadow :many
MATCH (p:Person) WHERE p.name = $out RETURN p.name

// name: BlankShadow :one
MATCH (p:Person) WHERE p.name = $_ RETURN p.name
