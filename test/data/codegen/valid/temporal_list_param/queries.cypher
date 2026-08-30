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
// $windows :: LIST<LIST<DATE>> witnessed the emitter's rule that it records
// one list depth per carrier and must keep the DEEPEST, since the depth-2
// helper is written in terms of the depth-1 one — declared deepest-first it
// emits fromDateList2 without fromDateList and the goldens stop compiling.
// ADR 0035 makes that shape unconstructible on this backend: a nested list
// is refused as a stored property, so no neo4j schema can declare one and
// no parameter can take its type. The rule it guarded is still real and
// render_temporal.go's temporalListHelper still recurses for depth >= 2;
// what is gone is this fixture's witness of it, and there is no neo4j
// fixture that can replace it. Removing the generator's depth >= 2 support
// would be a special case bought for nothing, so it stays (bd gqlc-nxcj9).

// name: SlotsMatching :many
MATCH (s:Slot)
WHERE s.tags = $tags AND s.ranks = $ranks AND s.days = $days AND s.spans = $spans
RETURN s.id AS id
