// The zoneless temporal widths' whole round trip on one schema: DATE,
// LOCAL TIME and DURATION crossing outward as bound parameters, coming
// back as projected columns and inside a whole vertex, plus a range
// predicate and an ORDER BY over a stored DATE.
//
// Every value here crosses the neutral carriers of ADR 0033: the
// generated surface names Date, LocalTime and Duration, and neither the
// neo4j driver's dbtype.* nor AGE's agtype encoding appears on it — each
// lives inside that target's emitted conversions. A conversion that lost
// a component would answer the wrong row on ReadingsFrom or hand back a
// value the seeded literal contradicts.
//
// Every query here is admitted by both backends, which is what lets the
// live battery run one body against all three arms. The LOCALDATETIME
// carrier is reached only through a constructed column, which Apache AGE
// refuses permanently, so it is witnessed by
// local_datetime_constructed_column instead — see that fixture's header.
//
// DURATION is deliberately not ordered on: neo4j refuses to compare two
// durations, because a month has no fixed length in seconds. The
// encoding's sign handling is witnessed by round-tripping durations
// either side of zero instead.

// name: AddReading :exec
CREATE (r:Reading {id: $id, onDate: $onDate, atLocal: $atLocal, elapsed: $elapsed})

// name: ReadingsFrom :many
MATCH (r:Reading) WHERE r.onDate >= $from RETURN r.id AS id ORDER BY r.onDate

// name: ReadingsSeenFrom :many
MATCH (r:Reading) WHERE r.seenOn >= $seenFrom RETURN r.id AS id ORDER BY r.seenOn

// name: ReadingDate :one
MATCH (r:Reading) WHERE r.id = $id RETURN r.onDate AS onDate

// name: ReadingLocalTime :one
MATCH (r:Reading) WHERE r.id = $id RETURN r.atLocal AS atLocal

// name: ReadingElapsed :one
MATCH (r:Reading) WHERE r.id = $id RETURN r.elapsed AS elapsed

// name: OneReading :one
MATCH (r:Reading) WHERE r.id = $id RETURN r
