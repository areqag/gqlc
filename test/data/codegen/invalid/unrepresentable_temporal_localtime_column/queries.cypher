// The schema carries only a representable width, so nothing on the
// schema axis can fail: the temporal expression in the RETURN clause is
// the whole of what this fixture refuses. Apache AGE is the enrolled
// target because agtype has no temporal value, so its type table has no
// carrier for localtime() — the neo4j targets carry every temporal kind
// and generate this batch happily.
//
// The constructor is localtime() and not date(), and that is the whole
// reason this fixture reaches the carrier refusal at all. AGE's dialect
// gate (internal/codegen/age/dialect.go) refuses the five constructor
// names a live session was measured on — date() among them — on the
// query TEXT, ahead of the carrier question, because generated code runs
// that text verbatim (ADR 0005) and no projection of date() will ever
// parse on this server. localtime() has no such witness and so is not
// refused there, which leaves the portable answer this fixture names.
// Give localtime() a witness and this fixture has to move with it.

// name: EventsSeenOn :many
MATCH (e:Event) RETURN e.id AS id, localtime() AS seenOn
