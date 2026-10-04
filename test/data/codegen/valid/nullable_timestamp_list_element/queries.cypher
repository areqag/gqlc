// A LIST<TIMESTAMP> whose ELEMENTS are nullable, bound as a top-level
// parameter (bd gqlc-gk6q). Its carrier is []*time.Time, and before the fix
// it was handed to the driver bare: neo4j-go-v5 refused it client-side
// (`Usage of type '*time.Time' is not supported`) and neo4j-go-v6 packed each
// element as an empty map. No golden bound that width as a parameter, so this
// fixture is the first to reach the arm.
//
// nullable_temporal_list_element next door is the same shape over DATE, a
// neutral carrier with a from<X>List helper of its own. TIMESTAMP is not
// one: time.Time is the driver's own type and packs bare, so a NOT NULL
// element list needs nothing — the pointer is the whole defect.
//
// fixed is the in-fixture control: NOT NULL elements keep []time.Time and
// bind bare, so a fix that routed every TIMESTAMP list through the helper
// fails this fixture rather than passing it. maybe is the composition, nullable
// at both positions, *[]*time.Time, which is the one shape that reaches the
// Ptr wrapper.
//
// live_neo4j_nullable_timestamp_list_test.go writes and reads these through
// the generated code on both driver majors.

// name: OpenEntry :exec
CREATE (l:Entry {id: $id, stamps: $stamps, maybe: $maybe, fixed: $fixed})

// name: EntriesMatching :many
MATCH (l:Entry)
WHERE l.stamps = $stamps AND l.fixed = $fixed
RETURN l.id AS id

// name: EntryStamps :one
MATCH (l:Entry) WHERE l.id = $id
RETURN l.stamps AS stamps, l.maybe AS maybe, l.fixed AS fixed
