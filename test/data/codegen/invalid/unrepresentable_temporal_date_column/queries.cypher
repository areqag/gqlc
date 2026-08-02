// The schema carries only a representable width, so nothing on the
// schema axis can fail: the temporal expression in the RETURN clause is
// the whole of what this fixture refuses. Apache AGE is the enrolled
// target because agtype has no temporal value, so its type table has no
// carrier for date() — the neo4j targets carry every temporal kind and
// generate this batch happily.

// name: EventsSeenOn :many
MATCH (e:Event) RETURN e.id AS id, date() AS seenOn
