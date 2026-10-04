// union_list_temporal_member's defect, off the temporal family (bd
// gqlc-oo5p): a union member that is a list of nullable INT32 or UUID elements
// once asserted each driver element to `*int32` / `*UUID`, which compiles —
// no dbtype import is involved — and is false for every element Bolt can
// hand back. TestNeo4jGoldensAssertOnlyDriverCarriers is what holds it.
//
// INT32 rather than INT64 so the element also owes the checked narrowing from
// the driver's widened int64; UUID because it rides the string wire and is
// parsed (ADR 0047), the third narrowing lane beside the temporal one.

// name: TallyWhole :one
MATCH (t:Tally) WHERE t.id = $id RETURN t

// name: TallyColumns :many
MATCH (t:Tally) RETURN t.counts AS counts, t.refs AS refs
