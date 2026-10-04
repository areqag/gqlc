// A closed union one of whose MEMBERS is a list of a temporal carrier
// (bd gqlc-oo5p). Not union_only_carrier_list's shape, which is a list whose
// ELEMENT is a union: neo4j refuses to store that (StorableProperty), while a
// union holding a list is one homogeneous array or one INT64 and is stored.
//
// Measured on origin/master 97fa7f34: union_neo4j.go failed to compile on both
// majors with `dbtype imported and not used`, and every member's decode arm
// asserted the driver element to a POINTER (`v3.(*Date)`, `v3.(*time.Time)`,
// ...) — false for every element Bolt can hand back, because the element's
// star is the schema's NULL and no wire value is a pointer. TestGoldenBuild
// carries the first half and TestNeo4jGoldensAssertOnlyDriverCarriers the
// second; neither golden comparison can, since a regenerated golden matches
// itself.
//
// Every element is nullable on purpose. A LIST<DATE NOT NULL> member beside
// these asserted dbtype.Date correctly and so named the import, which masked
// the compile failure while leaving the five runtime ones in place.
//
// One property per temporal width a schema can declare: DATE, ZONED TIME,
// LOCAL TIME, TIMESTAMP and DURATION. LOCAL DATETIME is not a sixth, because
// as a declared width it folds to TIMESTAMP. One union per width rather than
// one union of all of them, because every list shares the []any wire family
// and the admission rule refuses two members in one family.
//
// The three reads are union_property's three: the models struct, the column
// position, and a parameter for the encode direction. OpenDiary binds every
// member list, and is what live_neo4j_union_list_temporal_test.go writes
// through.

// name: DiaryWhole :one
MATCH (d:Diary) WHERE d.id = $id RETURN d

// name: DiaryColumns :many
MATCH (d:Diary)
RETURN d.days AS days, d.offsets AS offsets,
       d.locals AS locals, d.stamps AS stamps, d.spans AS spans

// name: DiariesByDays :many
MATCH (d:Diary) WHERE d.days = $days RETURN d.id AS id

// name: OpenDiary :exec
CREATE (d:Diary {id: $id, days: $days, offsets: $offsets, locals: $locals, stamps: $stamps, spans: $spans})
