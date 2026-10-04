// Event is declared and never returned whole, so no backend emits
// decodeEvent, nor toDate, agtypeInstant, agtypeDate or the union helpers
// that only it would call (bd gqlc-m1dk). Its struct is emitted all the
// same and names time.Time and Date, so the emitted package still has to
// import time and declare the Date carrier while converting nothing:
// temporal_neo4j.go carries no import block, and AGE's models.go imports
// time for the struct alone.

// name: People :many
MATCH (p:Person) RETURN p
