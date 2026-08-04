// An instant on the read side only: every query here projects one and
// none binds one. That asymmetry is the point. The emitted files import
// "time" because a signature spells it, and a gate that consulted only
// the bound parameters would leave the import out of a file that needs
// it — a gap no fixture that binds an instant can show, because there
// the parameter alone already opens the import.
//
// The vertex carries a single non-nullable instant, so the offset
// sidecar's decode is reached from the non-nullable side. Every other
// fixture reaches it from the nullable one.

// name: ReadingAt :one
MATCH (r:Reading) WHERE r.id = $id RETURN r.takenAt AS takenAt

// name: OneReading :one
MATCH (r:Reading) WHERE r.id = $id RETURN r

// name: AllReadings :many
MATCH (r:Reading) RETURN r.takenAt AS takenAt ORDER BY r.takenAt
