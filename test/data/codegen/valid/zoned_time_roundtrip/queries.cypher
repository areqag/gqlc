// The one temporal width that carries a zone: TIME WITH TIME ZONE out
// through a bound parameter and back through a projected column and a
// whole vertex, with the offset the writer chose still on the value.
//
// The offset is the point, and it is held apart from the zoneless widths
// because it is the component a conversion most easily drops: gqlc's
// Time carries OffsetSeconds beside the clock reading, and a from/to
// pair that built its driver value in the process's local zone instead
// would still answer the clock correctly and the offset wrongly. Seeded
// values therefore use an offset the test runner is very unlikely to be
// in.

// name: AddSlot :exec
CREATE (s:Slot {id: $id, startsAt: $startsAt})

// name: SlotsFrom :many
MATCH (s:Slot) WHERE s.startsAt >= $from RETURN s.id AS id ORDER BY s.startsAt

// name: SlotStart :one
MATCH (s:Slot) WHERE s.id = $id RETURN s.startsAt AS startsAt

// name: OneSlot :one
MATCH (s:Slot) WHERE s.id = $id RETURN s
