// The TIMESTAMP width's whole round trip, on one schema, so a backend
// whose wire encoding is its own is exercised on every side of it: a
// non-nullable instant crossing as a bound parameter, a nullable one, an
// instant coming back as a projected column, a nullable instant coming
// back as one, an instant coming back inside a whole vertex, and a range
// predicate plus an ORDER BY over the stored property.
//
// The ordering pair is the point. On Apache AGE the property is stored
// as an integer and the comparison is agtype's own integer comparison,
// so EventsAfter is answered correctly with the author's text executed
// exactly as written (ADR 0005). An encoding that sorted by anything but
// the instant would return the wrong rows here and nowhere else.

// name: AddEvent :exec
CREATE (e:Event {id: $id, occurredAt: $occurredAt})

// name: EventsAfter :many
MATCH (e:Event) WHERE e.occurredAt > $since RETURN e.id AS id ORDER BY e.occurredAt

// name: EventsSeenAfter :many
MATCH (e:Event) WHERE e.seenAt > $seenAfter RETURN e.id AS id ORDER BY e.seenAt

// name: EventAt :one
MATCH (e:Event) WHERE e.id = $id RETURN e.occurredAt AS occurredAt

// name: EventSeenAt :one
MATCH (e:Event) WHERE e.id = $id RETURN e.seenAt AS seenAt

// name: OneEvent :one
MATCH (e:Event) WHERE e.id = $id RETURN e
