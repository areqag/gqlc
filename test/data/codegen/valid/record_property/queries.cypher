// AGE-only, and the single target is the point of the fixture rather than
// an omission (precedent: nested_list_property, AGE-only for exactly this
// reason; property_bytes, neo4j-only for the mirror image).
//
// A RECORD is a structured value, and this backend stores one. Whether
// neo4j does is a question about a server, not about gqlc, and it is not
// answered here: gqlc-jffyz step 5 owes the measurement and step 6 owes
// neo4j's storage answer once it has one. Enrolling neo4j before that
// would pin goldens for a claim nobody has made.
//
// What the four properties are for, none of them decoration:
//
//   home and prior are ONE encoding at two sites, in the two
//   nullabilities. The emission is per encoding, not per position, so
//   they must share a single carrier alias and a single helper pair —
//   and a golden that grew two of either is the regression.
//
//   moves is the same encoding again inside a LIST, which is the only
//   thing that puts the record through the list plumbing: the element's
//   width has to reach the decoder, and a list that lost it would emit a
//   decode for the wrong shape.
//
//   opened is a SECOND, distinct encoding, so the file carries more than
//   one and their names have to differ. Its DATE field is chosen because
//   a temporal field's two directions are two different helper names, so
//   a batch that marked only one of them emits a package naming what it
//   does not declare.
//
// The two reads are the two paths into that emission. The whole-entity
// :one goes through the models struct; the columns :many projects each
// property alone, so a per-column decoder is emitted for each — which is
// the record COLUMN position, and it is not the same code as the
// property read.

// name: DwellingWhole :one
MATCH (d:Dwelling) RETURN d

// name: DwellingColumns :many
MATCH (d:Dwelling) RETURN d.home AS home, d.prior AS prior, d.moves AS moves, d.opened AS opened
