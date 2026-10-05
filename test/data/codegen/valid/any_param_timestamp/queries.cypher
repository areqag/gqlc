// A TIMESTAMP inside a parameter whose carrier is any (ANY VALUE) or []any
// (LIST<ANY VALUE>, and the bare LIST), each nullable and not, bound by a
// write and by a match (bd gqlc-nvb4). These carriers have no declared width
// to dispatch on, so a time.Time inside one reaches the driver as the caller
// built it unless the bind walks the value. The live rows are in
// live_neo4j_any_param_timestamp_test.go.
//
// `seenOn` and `ref` are never bound. They are here so the package declares
// gqlc's own temporal carriers and its UUID carrier, which a caller can
// then place inside an ANY value; neither driver major packs one (v5
// refuses the type, v6 sends its fields as a map).
//
// neo4j-only: the refusal is the neo4j drivers' zone-id rule (bd
// gqlc-m3ax). What AGE does with a time.Time inside an ANY value is not
// measured here.

// name: AddSlot :exec
CREATE (s:Slot {id: $id, payload: $payload, marker: $marker, bag: $bag, loose: $loose})

// name: SlotIdsByPayload :many
MATCH (s:Slot) WHERE s.payload = $payload RETURN s.id AS id ORDER BY s.id

// name: SlotIdsByMarker :many
MATCH (s:Slot) WHERE s.marker = $marker RETURN s.id AS id ORDER BY s.id

// name: SlotIdsByBag :many
MATCH (s:Slot) WHERE s.bag = $bag RETURN s.id AS id ORDER BY s.id

// name: SlotIdsByLoose :many
MATCH (s:Slot) WHERE s.loose = $loose RETURN s.id AS id ORDER BY s.id
