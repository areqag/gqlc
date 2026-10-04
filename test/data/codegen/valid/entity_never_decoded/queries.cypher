// Event is declared and never returned whole, so no backend emits
// decodeEvent, nor toDate, toUUID, agtypeInstant, agtypeDate, agtypeUUID
// or the union helpers that only it would call (bd gqlc-m1dk). Its struct
// is emitted all the same and names time.Time, Date and UUID, so the
// package still declares the carriers and imports time while converting
// nothing: the neo4j targets' temporal_neo4j.go and uuid_neo4j.go are a
// package clause alone, and AGE's models.go imports time for the struct
// alone.

// name: People :many
MATCH (p:Person) RETURN p
