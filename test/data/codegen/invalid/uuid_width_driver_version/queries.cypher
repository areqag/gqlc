// neo4j-go-v5 alone, and the sentinel is the whole point of the fixture.
//
// The schema here is byte-identical in the part that matters to
// test/data/codegen/invalid/uuid_width_unrepresentable (AGE) and to
// test/data/codegen/valid/uuid_property (neo4j-go-v6): one UUID NOT NULL
// property, read whole. Three targets, three answers, and the declaration
// is held constant so that the answer is the only variable.
//
// What this one asserts is that v5 does NOT refuse with
// codegen.ErrUnrepresentableWidth. That sentinel says the width has no
// faithful Go carrier here, and an author reading it looks for a different
// BACKEND. It is false of this refusal: the carrier exists — dbtype.UUID,
// which landed in neo4j-go-driver v6.2.0 and is absent from v5.28.4, the two
// versions test/data/codegen/go.mod pins — and the repair is a different
// DRIVER MAJOR, inside the same backend. neo4j.ErrUnrepresentableOnDriverVersion
// is what says that, and TestInvalid's per-target ErrorIs is what holds this
// fixture to it: the width sentinel is not in this error's chain, so a
// regression that routed v5 back through the shared width channel reds here.
//
// It is also the corpus witness for the published name. neo4j publishes that
// sentinel through its registry entry, and publication obliges a fixture to
// reach it (TestBackendSentinelReachability) — an unwitnessed published name
// reads from the outside exactly like coverage.
//
// The refusal names the neo4j backend, which is owed under ADR 0035 because
// neo4j-go-v6 answers this same declaration by emitting. Asserted at the
// composition root (TestAContingentRefusalNamesItsBackend), not here.

// name: GetAccount :one
MATCH (a:Account) RETURN a
