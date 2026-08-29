// TIMESTAMP is a representable width here, so nothing on the schema axis
// can fail: the temporal expression in the RETURN clause is the whole of
// what this fixture refuses. Apache AGE is the enrolled target because
// agtype has no temporal value, so its type table has no carrier for a
// duration — the neo4j targets carry every temporal kind and generate
// this batch happily.
//
// The constructor is NAMESPACED, and that is the whole reason this
// fixture reaches the carrier refusal at all. AGE's dialect gate
// (internal/codegen/age/dialect.go) refuses the constructor names a live
// session was measured on, on the query TEXT, ahead of the carrier
// question, because generated code runs that text verbatim (ADR 0005).
// Every bare temporal constructor openCypher spells is now in that set,
// measured against the pinned image (bd gqlc-osf1), so no bare
// constructor reaches Prepare any more. duration.between is the one
// temporal spelling left: Cypher.g4 §oC_FunctionName is `oC_Namespace
// oC_SymbolicName`, so it is a different name from duration, and
// cypher.UnqualifiedFunctionCalls drops namespaced calls by design.
//
// So this fixture stands on a gap in the gate, and bd gqlc-dy40s is the
// bead to close that gap: the pinned image refuses duration.between too,
// under SQLSTATE 3F000 rather than 42883, because Postgres reads the
// namespace as a schema qualifier. Refuse it on the text and no query
// text reaches codegen.ErrUnrepresentableTemporal on this backend — and
// TestSentinelReachability requires a fixture that does, this being the
// only one. Whoever takes gqlc-dy40s owes that question an answer before
// deleting this directory.

// name: EventDurations :many
MATCH (e:Event) RETURN e.id AS id, duration.between(e.startedAt, e.endedAt) AS span
