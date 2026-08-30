// AGE-only, and the manifest's single target is the point of the fixture
// rather than an omission (precedent: property_bytes, neo4j-only for the
// mirror-image reason).
//
// The three properties here are the ones ADR 0035 removed from
// schema_list_property_widths and schema_list_any_property, because neo4j
// refuses a nested list as a stored property: its server answers such a
// write with "Collections containing collections can not be stored in
// properties". Apache AGE stores them, so the emission they exercise —
// agtype's list-of-list decode helpers, at each of the three element
// widths — would have left the corpus with the trims. It lives here
// instead.
//
// The two reads are the two shapes that reach that emission by different
// paths: the whole-entity :one goes through the models struct, and the
// columns :many projects each property on its own so a per-column decoder
// is emitted for each. A regression in either is a golden diff.
//
// If AGE ever refuses one of these, this fixture stops generating and the
// premise of the split dies here rather than silently (bd gqlc-v0gk).

// name: GridWhole :one
MATCH (g:Grid) RETURN g

// name: GridColumns :many
MATCH (g:Grid) RETURN g.matrix AS matrix, g.grid AS grid, g.piles AS piles
