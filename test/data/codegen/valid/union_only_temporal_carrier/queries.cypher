// The ONLY mention of a temporal carrier in this batch is a union MEMBER, and
// that is the whole of what the fixture is for (bd gqlc-o8p3). A closed union
// carries as `any`, so no position of the prepared surface spells Date — while
// the union's emitted helper pair names it in a type-switch arm and calls its
// conversions. The emission trigger for temporal.go once read the surface
// alone, so this batch emitted, on all three targets, a package that named
// Date and declared none: `undefined: Date` from union_neo4j.go on the two
// neo4j majors and from models.go on AGE, with generation itself exiting 0.
//
// Nothing was red, because each corpus fixture that put a carrier in a union
// also declared one bare: uuid_property has ref and prior beside either, and
// union_property's members are not carriers at all. So id is an INT64 here and
// stays one. A bare DATE added to this schema would emit temporal.go for the
// older reason and the fixture would hold nothing.
//
// The claim is carried by TestGoldenBuild, which compiles the goldens, and not
// by the golden comparison: a golden regenerated without the file matches
// itself.
//
// INT64 is the other member because it sits in a family of its own against a
// DATE on all three targets. STRING would not: a DATE is ISO text on AGE, which
// no probe can tell from a STRING, so ANY<DATE | STRING> is refused there
// (union_property's head comment has the measurement's address).
//
// The three reads are union_property's three, for its reason — the models
// struct, the column position and the parameter — and they matter here for a
// second one. The two decode positions reach toDate and the parameter reaches
// fromDate, so the neo4j bridge emits both directions and each has a caller;
// one emitted with none reds the fence's `unused` run over the goldens.

// name: AccountWhole :one
MATCH (a:Account) WHERE a.id = $id RETURN a

// name: AccountEither :many
MATCH (a:Account) RETURN a.either AS either

// name: AccountsByEither :many
MATCH (a:Account) WHERE a.either = $either RETURN a.id AS id
