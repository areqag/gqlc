// UUID is stage 1 of gqlc-do1 (bd gqlc-eg4b): the schema front end reaches
// graph.TypeUUID now, and no backend has a carrier for it yet, so every
// enrolled target refuses the width. That is the whole claim this fixture
// makes, and it is enrolled on all three deliberately — a fixture naming only
// one would leave the other two unmeasured.
//
// It is not the only thing holding the width now, and it is the only one
// holding it END TO END. Each backend's typeMap.Property switch has no default
// arm, so the exhaustive linter demands an arm per declared width, and each
// backend's TestTypeMapProperty then demands a representable/unrepresentable
// ROW per arm. Both of those are reconciliations INSIDE a type table: they say
// the table answers, and what it answers, and neither runs a schema through the
// pipeline. This fixture is what says an author who writes `:: UUID` is refused
// rather than emitted for, which is a different claim and the one a reader of
// the error message cares about.
//
// gqlc-eg4b stage 2 is what moves this: neo4j-go-v6 gains a carrier
// (dbtype.UUID landed in neo4j-go-driver v6.2.0, absent from v5.28.4, both the
// versions test/data/codegen/go.mod pins), so v6 must come OUT of targets here
// and get a valid fixture with a golden, while v5 moves to the driver-version
// sentinel that says the width is representable but not on this driver. Two
// targets are expected to stay: an unrepresentable-width refusal is the right
// answer for v5 only until that sentinel exists, and for AGE for as long as
// its store has no uuid carrier.
//
// Landing that also removes graph.TypeUUID from age's uncarriedEverywhere, and
// the composition root reds until it does: once v6 carries UUID the width
// divides the roster, and AGE's refusal starts owing its own name
// (TestAContingentRefusalNamesItsBackend, ADR 0035).

// name: GetAccount :one
MATCH (a:Account) RETURN a
