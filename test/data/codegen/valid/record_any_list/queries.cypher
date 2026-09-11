// The two widths bd gqlc-9xiz found unparseable, pinned as GOLDENS rather
// than as an assertion about them.
//
// A braceless RECORD says its fields are undeclared, and AGE carries that
// as map[string]any (valid/record_any_and_empty pins the bare case). It is
// the only carrier this backend answers with that is not a Go identifier,
// and age's listHelperName derives a decode-helper NAME from the carrier
// text — so before gqlc-9xiz the brackets survived into the emitted call,
// which then parsed as an index expression inside an argument list and
// failed at go/format with no taxonomy sentinel to name the declaration
// that caused it. Exactly these two widths failed; the bare RECORD and the
// two record-of-record forms did not.
//
// What these goldens hold is therefore narrower than "it compiles": they
// pin the helper NAMES, agtypeListOfNullableAnyRecord and
// agtypeListOfNullableListOfNullableAnyRecord. The class is held by
// TestEveryAdmittedListCarrierDerivesAGoIdentifier
// (internal/codegen/age/render_models_test.go), which asks the type table
// for every admitted list carrier and requires each derived name to be a
// Go identifier. This fixture is the instance that reads.
//
// AGE-only for the reason valid/record_property and
// valid/record_any_and_empty give: whether neo4j stores a structured
// property is unmeasured (gqlc-jffyz step 5, blocked on gqlc-p9g2i), and
// enrolling it would pin goldens for a claim nobody has made.

// name: PayloadWhole :one
MATCH (p:Payload) RETURN p

// name: PayloadColumns :many
MATCH (p:Payload) RETURN p.rows AS rows, p.grid AS grid
