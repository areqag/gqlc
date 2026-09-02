// The corpus witness for age.ErrUndefinedNamespace. age.Sentinels
// publishes that name, and publication is a promise the corpus records
// the refusal — TestBackendSentinelReachability holds the promise — so
// this fixture is what makes the fourth dialect gap nameable from a
// manifest rather than reachable only from the package's own tests.
//
// duration.between is the call, because `duration` is the one namespace
// with a measured probe answer behind it (namespaceProbes in
// internal/codegen/age/dialect.go): the pinned AGE image answers
// SQLSTATE 3F000 `schema "duration" does not exist`, naming the
// NAMESPACE and no function, which is why this is its own sentinel and
// not another name in the catalogue behind ErrUndefinedFunction.
//
// The columns are TIMESTAMP, which is representable on this backend, so
// nothing on the schema axis can fail and the refusal this fixture pins
// is unambiguously the namespace one. The gate reads the query TEXT and
// runs ahead of the carrier question (ADR 0005), so the duration-typed
// projection never reaches codegen.Prepare — that is the same shadowing
// that made this batch's predecessor,
// test/data/codegen/invalid/unrepresentable_temporal_duration_column,
// unreachable and so deleted in this branch.

// name: EventDurations :many
MATCH (e:Event) RETURN e.id AS id, duration.between(e.startedAt, e.endedAt) AS span
