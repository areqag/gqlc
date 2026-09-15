// AGE alone, and the single target is the point of the fixture rather than
// an omission. UUID divides the roster since stage 2 of bd gqlc-eg4b, so the
// three targets that once shared this refusal now give three different
// answers and no one fixture can hold them:
//
//   apache-age-pgx-v5 refuses with codegen.ErrUnrepresentableWidth, which is
//   this fixture. agtype's value vocabulary is boolean / integer / float /
//   string / list / map, so there is no shape a 128-bit identifier comes back
//   as itself in, and this is the sentinel that says so.
//
//   neo4j-go-v5 refuses with neo4j.ErrUnrepresentableOnDriverVersion —
//   test/data/codegen/invalid/uuid_width_driver_version. A different
//   sentinel because it is a different refusal: the backend HAS a carrier
//   and this driver major does not ship it.
//
//   neo4j-go-v6 EMITS — test/data/codegen/valid/uuid_property, with goldens.
//
// The AGE refusal now carries the AGE name, which it did not while every
// target refused: a refusal names its backend exactly when another enrolled
// target answers the declaration differently (ADR 0035), and one does. That
// is asserted at the composition root by
// TestAContingentRefusalNamesItsBackend rather than here.
//
// This fixture is not the only thing holding the width on this target, and it
// is the only one holding it END TO END. age's typeMap.Property switch has no
// default arm, so the exhaustive linter demands an arm per declared width,
// and TestTypeMapProperty then demands a representable/unrepresentable ROW
// per arm. Both are reconciliations INSIDE a type table: they say the table
// answers, and what it answers, and neither runs a schema through the
// pipeline. This says an author who writes `:: UUID` against AGE is refused
// rather than emitted for.

// name: GetAccount :one
MATCH (a:Account) RETURN a
