// Every parameter here is named after something an emitted method
// resolves, and the invariant is that none of them reaches the emission:
// the Go argument is `arg` whatever the query text says.
//
// The names cover the four things that scope holds — a local the body
// declares, the package-level query-text const a body references and
// never declares, what the signature itself binds, and a package-level
// name the body resolves but no emitted declaration introduces (an
// import, a decoder). The const-named ones reproduce their own const,
// which derives from the method name; HelperShadow returns the entity so
// the decoder is actually called, and names its parameter after that
// decoder, which derives from the schema and so could never have been
// held by a reserved-name list.
//
// The fixture is enrolled in all three targets so TestGoldenBuild
// type-checks every one of these methods. That is the point of it: the
// format gate parses the emission and does not type-check it, so a
// capture that still parses is invisible everywhere else.

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
MATCH (p:Person) WHERE p.name = $decodePerson RETURN p

// name: ErrShadow :one
MATCH (p:Person) WHERE p.name = $err RETURN p.name

// name: RecordsShadow :many
MATCH (p:Person) WHERE p.name = $records RETURN p.name

// name: OutShadow :many
MATCH (p:Person) WHERE p.name = $out RETURN p.name

// name: BlankShadow :one
MATCH (p:Person) WHERE p.name = $_ RETURN p.name
