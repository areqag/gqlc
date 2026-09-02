// A list parameter is bound by paramBindExpr, which used to answer the
// encode question with driverCarrier — a decode answer that is []any for
// every slice, so every list here emitted `[]any(arg)`. That is not a Go
// conversion, and nothing in the byte goldens could say so: the text is
// well-formed and only a compiler refuses it. These goldens are a package
// of the test/data/codegen module, which `just test-codegen-fence` builds
// and vets, so that is the gate this fixture reports to.
//
// Four parameters for four distinct bind expressions: a list the driver's
// packer walks itself (tags, ranks — bare), a list whose leaf is one of
// gqlc's own temporal carriers and so owes a per-element widen (days), and
// a nullable one whose helper has to carry the pointer through (spans).
//
// THE DEPTH-2 SHAPE USED TO BE HERE AND CANNOT BE. A fifth parameter
// $windows :: LIST<LIST<DATE>> witnessed the emitter's rule as it then
// stood: it recorded one list depth per carrier and had to keep the
// DEEPEST, since the depth-2 helper was written in terms of the depth-1
// one — declared deepest-first it emitted fromDateList2 without
// fromDateList and the goldens stopped compiling. ADR 0035 makes that
// shape unconstructible on this backend: a nested list is refused as a
// stored property, so no neo4j schema can declare one and no parameter can
// take its type. No neo4j fixture could replace it, and no other backend
// could: render_temporal.go has no AGE counterpart, so the one call site
// that reached depth >= 2 (sliceParamBindExpr) was reached from nowhere.
//
// gqlc-nxcj9 left that recursion in place and left the question of its
// removal open. bd gqlc-tlc3e settled it (2026-09-02): the depth machinery
// is deleted, temporalUse's listDepth int is now a list bool, and no
// from<X>List<n> helper exists to emit for any n > 1. The measurement that
// licensed the deletion — no golden here emitted one for any n — is why
// removing it moved no byte of this fixture's goldens.

// name: SlotsMatching :many
MATCH (s:Slot)
WHERE s.tags = $tags AND s.ranks = $ranks AND s.days = $days AND s.spans = $spans
RETURN s.id AS id
