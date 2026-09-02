// AGE-only, and the single target is the point of the fixture rather than
// an omission. ADR 0035 has neo4j refuse a nested list as a stored
// property, so a depth-2 list is constructible on this backend alone —
// same split as nested_list_property, which carries the non-temporal
// widths for the same reason.
//
// ADR 0036 admits lists of non-zoned temporals at EVERY depth, because the
// admission is a property of the element and recursion through Property
// makes it compose. This fixture is the corpus half of that claim: DATE
// carries to a string and DURATION to an int64, so between them they pin
// both sides of the encoded-parameter table at a depth greater than one.
//
// THE PARAMETER IS WHY THIS FIXTURE EXISTS, not the projection. Admission
// and decode already composed before gqlc-vhvz7; encode stripped exactly
// one list level, so a depth-2 parameter matched no leaf arm and crossed
// through plain json.Marshal as its Go struct while decode expected ISO
// strings. Nothing red: the equality predicate simply never matched. The
// $dates parameter below is the shape that reaches it.
//
// The zoned family needs no fixture here. A list element has no sibling to
// carry ADR 0033's offset, so LIST<TIME> and LIST<TIMESTAMP> are refused
// at every depth, and invalid/unrepresentable_width_list_element_schema
// already pins that refusal against ErrUnrepresentableWidth.

// name: RosterWhole :one
MATCH (r:Roster) RETURN r

// name: RostersOn :many
MATCH (r:Roster) WHERE r.dates = $dates RETURN r.id AS id
