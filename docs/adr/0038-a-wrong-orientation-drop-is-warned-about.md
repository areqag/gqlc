# A wrong-orientation drop is warned about

`MATCH (:Person)-[r:AUTHORED|REPORTED]->(p:Post)` against a schema that declares
`(:Post)-[:REPORTED]->(:Person)` — and REPORTED nowhere else — now prints, on
stderr:

    warning: relationship type "REPORTED" on edge "r" is declared only in the
    opposite orientation (Post-[REPORTED]->Person); the pattern's arrow drops it
    and no decoder is generated for it, so a row of that type fails at runtime.
    Flip the arrow if the direction is the mistake, or remove "REPORTED" from the
    pattern if the type is stale.

The generate still succeeds and still writes the same code it wrote before.
Previously it said nothing at all.

This answers `gqlc-he8v`, the question ADR 0032 recorded and deliberately left
open: whether a relationship type that IS declared, but is dropped from this
edge's candidate set by ADR 0022 endpoint narrowing, deserves a diagnostic. The
answer is yes for one narrow shape and no for everything broader.

## What was silent

The runtime hazard is identical to ADR 0032's. The type is dropped from the
candidate set, no decoder arm is emitted for it, and the query TEXT is left
verbatim — so the server is still asked for it. What differs is only why the
type dropped: 0032's type is declared nowhere, this one is declared backwards.

The author who wrote the arrow the wrong way round gets a query that matches
less than they believe, silently, with no runtime error to read.

## Decision

Warn about type L on edge e when, and only when, all of these hold:

1. **L is declared somewhere** in the schema. A type declared nowhere is ADR
   0032's, and one type must never collect two accusations.
2. **L is dropped entirely** from e's committed candidate set.
3. **Both endpoints are covering** (`endpointKeys.covers` on each). A WITH carry
   or an uncovered Phase-B inference reports only the keys it happens to know, so
   the drop may be an artefact of what the resolver failed to learn rather than
   of the arrow the author drew.
4. **A reversed declaration witnesses the wrong arrow**: for some committed
   `(src, tgt)` pair of e, `EdgeKey{Source: tgt, KeyLabels: L, Target: src}` is
   declared.
5. **L survives nowhere in the query.** If L reached any edge's candidate set in
   any branch, the author has demonstrably not lost it.
6. **Success path only**, as in ADR 0032: a refused query gets its error and
   nothing else.

Two of these are not separate branches in the code, because the others imply
them, and a branch that cannot be made to fail is not a guard:

- Clause 1 is implied by clause 4 — a reversed key IS a declaration of L.
- The edge being directed is implied by clauses 2 and 4: an undirected edge
  probes both orientations, so a declared reversed key would already be in its
  own candidate set and L would not have dropped.

Clause 5 is what makes this per-query rather than per-edge, so the evidence is
collected during edge closure and decided at `Resolve` after every branch has
run.

## Why not broader

Measured over the corpus at `origin/master` 13157072 — 322 queries × 40 schemas
= 12880 sweep cells:

| Detector | Accepted cells that fire |
|---|---|
| Broad: declared-somewhere and dropped | 27, over 3 legitimate queries |
| Clauses 1–4, no survival suppression | 9, all one legitimate idiom |
| Clauses 1–3 + 5, no wrong-arrow witness | 18, over 2 legitimate fixtures |
| Full shape | 0 |

The broad detector is the one ADR 0032 rejected by argument; the corpus agrees
with the argument. All 9 cells that clause 5 removes are the mirrored-alternation
idiom — the same `AUTHORED|LIKES` alternation written on both orientations across
two MATCHes — in `valid/edge_rebind_resets_carried_close.cypher`. Clause 4
removes 18 cells that are ordinary narrowing with no wrong arrow to report.

That the full shape fires on zero pre-existing corpus cells is the point: this
detector adds no noise to any query the corpus already holds.
`valid/edge_wrong_orientation_drop_warns.cypher` is added to give it one cell it
does fire on, so "fires on zero cells" cannot be satisfied by a detector that
fires on nothing at all.

## Consequences

### The warning lane grows a producer tag

ADR 0032 ruled that "if a second warning producer appears, the string lane should
become a small struct with a producer tag before it acquires a third". This is
that second producer, so `ValidatedQuery.Warnings` becomes `[]Warning` where
`Warning` is `{Producer, Text string}`.

Minimally. No severity enum (every entry is a warning), no source position (these
diagnostics place themselves in the author's own query text), no interface. The
tag exists because the per-project suppression ADR 0032 names as the next step
will need to select on the detector, and recovering that by matching message
prose would couple suppression to wording. The suppression itself is not built
here.

Both `Warnings` fields carry `omitempty`, so a query that warns about nothing
serialises identically before and after — the 82 corpus manifest rows whose
digest moved under the refactor are exactly the warning-bearing ones.

### ADR 0032's lane leads

`generate.go` prints the warning block ahead of the diagnostics on the reasoning
that a misspelled relationship type is a plausible cause of the refusal beneath
it. That argument is about the 0032 lane specifically, so 0032's entries stay
first within the block.

### ADR 0022 is unchanged

Narrowing behaviour is untouched. This reports one narrow class of what narrowing
already did; it does not change what any query resolves to.

## Residual risk accepted

Survival suppression is per-query, so a copy of the mirrored idiom split across
two separate query FILES would warn on each. Bounded: it is a warning, not an
error, and ADR 0032 already names per-project suppression as the sanctioned next
step if real-world noise appears.

## Rejected alternatives

1. **No diagnostic.** The runtime hazard is identical to ADR 0032's — verbatim
   query text still asks the server for the type and no decoder arm exists.
   Silence hides the same class of bug.
2. **The broad detector** (any declared-but-dropped type): 27 corpus cells over 3
   legitimate queries. ADR 0032 rejected it by argument and the measurement
   agrees.
3. **Wrong arrow without survival suppression**: 9 corpus cells, all one
   legitimate idiom.
