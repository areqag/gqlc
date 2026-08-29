// LOCALDATETIME has no property spelling, so its carrier is reached the
// only way a batch can reach it — a column the server constructs. The
// literal is in the query text, so the components are known exactly and
// a conversion that dropped one is visible here.
//
// This query sits alone rather than beside the other zoneless widths in
// temporal_property_roundtrip, and the reason is a permanent one. Apache
// AGE defines no temporal constructor (internal/codegen/age/types.go
// Temporal, measured live in live_age_dialect_test.go), so the backend
// refuses this column at generation. A fixture carrying it can never
// enrol apache-age-pgx-v5, and the widths that fixture does carry are
// admitted on AGE from gqlc-mv3r — so keeping them together would have
// held the whole round trip to the narrowest query in it.

// name: BuiltLocalDateTime :one
RETURN localdatetime({year: 2024, month: 3, day: 5, hour: 6, minute: 7, second: 8, nanosecond: 9}) AS built
