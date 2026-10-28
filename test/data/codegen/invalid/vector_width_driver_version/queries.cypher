// neo4j-go-v5 alone, and the sentinel is the whole point of the fixture.
//
// The declaration is held constant across three fixtures so that the answer
// is the only variable: test/data/codegen/valid/vector_property emits it on
// neo4j-go-v6, test/data/codegen/invalid/vector_width_age refuses it on Apache
// AGE, and this one refuses it on neo4j-go-v5.
//
// What this one asserts is that v5 does NOT refuse with
// codegen.ErrUnrepresentableWidth, which says the width has no faithful Go
// carrier and sends an author looking for a different BACKEND. The carrier
// exists — dbtype.Vector, in neo4j-go-driver v6 and absent from v5.28.4, the
// two versions test/data/codegen/go.mod pins — and the repair is a different
// DRIVER MAJOR inside the same backend (ADR 0050).
// neo4j.ErrUnrepresentableOnDriverVersion is what says that, and TestInvalid's
// ErrorIs holds this fixture to it.
//
// It is also the corpus witness for the published name: publication obliges a
// fixture to reach it (TestBackendSentinelReachability).

// name: GetDocument :one
MATCH (d:Document) RETURN d
