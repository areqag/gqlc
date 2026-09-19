// union_only_temporal_carrier's claim with the ENCODE direction taken away: no
// query binds the union as a parameter. Read that fixture's head comment first.
//
// What that changes is on the two neo4j majors. A union's decode dispatches on
// the DRIVER's type and returns `any`, so union_neo4j.go here reads
// `case dbtype.Date: v1 := toDate(t)` and spells the carrier's TYPE nowhere —
// it is encode's `case Date:` arm that names it, and this batch emits none.
// The package still does not compile without temporal.go, which declares
// toDate's result.
//
// So this fixture holds two things the parameter-bearing ones cannot. Under
// TestGoldenBuild, that the trigger fires for a union nothing encodes. Under
// TestTemporalCarriersAreEmittedExactlyWhenReferenced, that a call to a
// function the bridge file declares is read as naming a carrier: read for the
// type names alone, that test called this package a breach — temporal.go
// emitted "for nothing" — and its text pointed at narrowing the trigger
// (bd gqlc-o8p3).
//
// On AGE the decode helpers in models.go name Date either way, so the third
// target is here as the control that the direction matters on neo4j alone.
//
// AccountEithers reads the union through collect(), which is the list-element
// position of a query VALUE. It is the one way a neo4j batch reaches a list of
// unions, since StorableProperty refuses a stored LIST<ANY<…>>.

// name: AccountWhole :one
MATCH (a:Account) WHERE a.id = $id RETURN a

// name: AccountEithers :many
MATCH (a:Account) RETURN collect(a.either) AS eithers
