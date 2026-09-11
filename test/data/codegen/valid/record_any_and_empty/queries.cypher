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
//
// `either` is the same braceless spelling in a UNION MEMBER position,
// which is the only position that asks the carrier a second question:
// what WIRE FAMILY it arrives as (types.go, wireFamily). map[string]any
// answers "map", and nothing else here does, so the union is admitted
// and its decode dispatches the record on the `{` opening byte. A
// wireFamily that had no row for the map carrier would fold it onto
// WireFamilyIndistinct, which collides with every member including
// itself, and this property would be REFUSED rather than emitted
// differently — so the fixture's verdict on that arm is whether it
// generates at all (bd gqlc-bbn3).
//
// INT32 is the partner member because integer is the family furthest
// from map on this backend: it shares no opening byte, so the emitted
// dispatch is unambiguous, and it is not a temporal (every zoned
// temporal is refused in a container position before the family
// question is reached). A second RECORD member of any spelling would
// collide — `RECORD { }` folds onto "map" too, through
// codegen.IsRecordStruct — so there is exactly one record member here
// and that is the grammar's own limit rather than a choice.
//
// It lives in this AGE-only fixture rather than beside the other unions
// in valid/union_property for the reason the header gives: that fixture
// enrols neo4j, and neo4j will not hold a map in a property, so a record
// member there would pin goldens for a storage claim nobody has measured
// (gqlc-jffyz step 5).

// name: BlobWhole :one
MATCH (b:Blob) RETURN b

// name: BlobColumns :many
MATCH (b:Blob) RETURN b.loose AS loose, b.maybe AS maybe, b.blank AS blank

// A :one projecting `loose` ALONE is the only shape that emits a zero
// literal for the map carrier, and it is here to drive that arm rather
// than to pin a second decode of a column BlobColumns already covers.
// zeroValueText (render_queries.go) reaches zeroLiteral only for a :one
// whose single row field is non-nullable and is neither a node nor an
// edge. Neither query above qualifies, and they fail it on DIFFERENT
// grounds: BlobColumns is a :many, which zeroes to `nil` before the
// type is consulted at all, and BlobWhole is a :one over an ENTITY,
// which takes the ColumnNode arm and zeroes to `Blob{}`. `maybe` could
// not have qualified either, being nullable, since pointerWrapped
// answers `nil` ahead of the type. So `loose` alone under a :one is the
// whole of the shape. The literal that must come back is `nil`: the
// numeric default would emit `return 0, err` against a map[string]any
// return, which does not compile (bd gqlc-bbn3).
// name: BlobLoose :one
MATCH (b:Blob) RETURN b.loose AS loose
