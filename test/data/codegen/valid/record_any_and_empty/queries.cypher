// The two record spellings that declare no fields, which the front end
// keeps as DISTINCT types rather than folding together
// (internal/schema/gql/propertytype.go:83): a braceless RECORD says its
// fields are undeclared, `RECORD { }` says there are none. The x9tg7
// ruling gives them different carriers for that reason —
// map[string]any and struct{} — and this fixture is where the two
// carriers are held apart. A change that collapsed either into the
// other, or into the field-carrying case, moves these goldens.
//
// AGE-only for the reason valid/record_property gives: whether neo4j
// stores a structured property is unmeasured (gqlc-jffyz step 5, blocked
// on gqlc-p9g2i), and enrolling it would pin goldens for a claim nobody
// has made.
//
// `maybe` is the nullable of the same undeclared-field spelling, so the
// pointer shape is pinned beside the value shape. There is deliberately
// no nullable `RECORD { }`: struct{} carries no information, so a
// pointer to one distinguishes only presence, and nothing about the
// emission differs from the nullable case above it.

// name: BlobWhole :one
MATCH (b:Blob) RETURN b

// name: BlobColumns :many
MATCH (b:Blob) RETURN b.loose AS loose, b.maybe AS maybe, b.blank AS blank
