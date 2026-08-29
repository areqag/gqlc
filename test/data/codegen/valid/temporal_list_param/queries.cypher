// A list parameter is bound by paramBindExpr, which used to answer the
// encode question with driverCarrier — a decode answer that is []any for
// every slice, so every list here emitted `[]any(arg)`. That is not a Go
// conversion, and nothing in the byte goldens could say so: the text is
// well-formed and only a compiler refuses it. These goldens are a package
// of the test/data/codegen module, which `just test-codegen-fence` builds
// and vets, so that is the gate this fixture reports to.
//
// Five parameters for five distinct bind expressions: a list the driver's
// packer walks itself (tags, ranks — bare), a list whose leaf is one of
// gqlc's own temporal carriers and so owes a per-element widen (days), the
// same at a second nesting depth (windows), and a nullable one whose helper
// has to carry the pointer through (spans).
//
// $windows precedes $days deliberately. The emitter records one list depth
// per carrier and has to keep the DEEPEST, because the depth-2 helper is
// written in terms of the depth-1 one. Declared shallow-first, a
// last-one-wins emitter arrives at the same depth by accident and the
// fixture witnesses nothing; declared deepest-first it emits fromDateList2
// without fromDateList and the goldens stop compiling.

// name: SlotsMatching :many
MATCH (s:Slot)
WHERE s.tags = $tags AND s.ranks = $ranks AND s.windows = $windows AND s.days = $days AND s.spans = $spans
RETURN s.id AS id
