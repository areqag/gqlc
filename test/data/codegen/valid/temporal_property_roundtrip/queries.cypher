// The zoneless temporal widths' whole round trip on one schema: DATE,
// LOCAL TIME and DURATION crossing outward as bound parameters, coming
// back as projected columns and inside a whole vertex, plus a range
// predicate and an ORDER BY over a stored DATE. LOCALDATETIME has no
// property spelling, so its carrier is reached the only way a batch can
// reach it — a constructed column.
//
// Every value here crosses the neutral carriers of ADR 0033: the
// generated surface names Date, LocalTime, LocalDateTime and Duration,
// and the driver's dbtype.* appears only inside the emitted conversions.
// A conversion that lost a component would answer the wrong row on
// ReadingsFrom or hand back a value the seeded literal contradicts.
//
// DURATION is deliberately not ordered on: neo4j refuses to compare two
// durations, because a month has no fixed length in seconds.

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

// name: BuiltLocalDateTime :one
RETURN localdatetime({year: 2024, month: 3, day: 5, hour: 6, minute: 7, second: 8, nanosecond: 9}) AS built
