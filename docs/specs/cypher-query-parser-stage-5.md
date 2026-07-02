# Stage 5 spec — Cypher query parser: undirected patterns

The implementation brief for Stage 5 of the Cypher implementation of
`query.Parser`. This is the fifth model evolution after Stage 4 per ADR 0004
(test-first, evolving until feature-complete) and per the curation discipline
of ADR 0003 and the type-interface boundary of ADR 0005. Stage 5 lifts the one
remaining pattern restriction on the read surface: an **undirected relationship**
`(a)-[r]-(b)` (and its both-arrows spelling `(a)<-[r]->(b)`), which Stage 0–4
reject with `ErrUnsupportedPattern`. It is the largest single PENDING bucket in
the corpus — many `clauses/match` and `clauses/return` scenarios write undirected
patterns — so flipping it is the bulk of the remaining read-core progress.

This document is a **delta** against [Stage 0](./cypher-query-parser-stage-0.md),
[Stage 1](./cypher-query-parser-stage-1.md),
[Stage 2](./cypher-query-parser-stage-2.md),
[Stage 3](./cypher-query-parser-stage-3.md) and
[Stage 4](./cypher-query-parser-stage-4.md); everything not stated here carries
over verbatim. Sections appear here only where Stage 5 changes something.

Tracking: bead `gqlc-7z6`. Lands as one graphite branch (`stage-5-undirected`)
with separated commits (prep/spec → model+goldens → unlock+corpus+skiplist+
layer-2), independently mergeable as a whole: `just test` is green if this branch
lands on `master` alone (AGENTS.md stacked-branch invariant).

---

## 1. The "may be N/A" question, resolved

The bead notes Stage 5 "may end up N/A if the schema stays directed-only —
re-evaluate when reached." It is **not** N/A, and here is why, decided
autonomously per the overnight-loop mandate and recorded for the record.

The parser is **schema-agnostic** (ADR 0003): it records what the query *says*,
never whether a schema supports it. An undirected pattern is valid openCypher; a
schema-agnostic parser must lower it, exactly as it lowers a directed pattern
whose edge type the schema may not contain. "The schema is directed-only" is a
**resolution**-time fact, and resolution is a separate stage (ADR 0004) that does
not exist yet. The division of labour is:

- **Parser (this stage):** record that the edge is undirected — a single marker
  bit on the edge binding — and record its two endpoints in textual order. No
  orientation is chosen; none can be, without a schema.
- **Resolver (post-freeze, out of scope):** for an undirected edge, try **both**
  orientations (`source→target` and `target→source`) against the directed
  `schema.EdgeKey` and accept the binding if *either* resolves. If a future
  schema admits no undirected match the resolver rejects there — that is its job,
  not the parser's.

The one disanalogy to face squarely: an unknown directed edge type still yields a
*single* `schema.EdgeKey` the resolver looks up and fails to find; an undirected
edge yields *no single key* — the parser cannot canonicalise it to one
source→target triple, because there isn't one. That is precisely why the marker
is load-bearing rather than cosmetic: it is the parser telling the resolver "form
**two** candidate keys from these endpoints, not one." This is still a structural
fact (the edge has no authoritative orientation), not a resolution policy (the
two-key trial), so it sits on the parser side of the ADR 0003 line; but it means
the marker is *required* for correctness, not merely informative. Recording the
endpoints without the marker would silently assert a canonical orientation that
does not exist — the lie §3 forbids.

So the parser supports undirected patterns; the "directed-only schema" concern is
entirely a resolver concern and changes nothing here. Leaving undirected as
`ErrUnsupportedPattern` would mean the read-core PENDING count can never reach
zero (`gqlc-ude`), since the corpus exercises undirected patterns heavily. Stage 5
is therefore in scope and is the final read-surface pattern slice.

---

## 2. Deliverables

- **Model evolution** — `EdgeBinding` gains a **direction marker**:
  - A new unexported `directed bool` field with a `Directed() bool` accessor.
    `true` for a one-arrow edge (`-->` or `<--`); `false` for an undirected edge
    (`--`, neither arrowhead, **or** `<-->`, both arrowheads — openCypher treats
    both spellings as undirected).
  - The marker is the **only** new datum. The model does not record which
    spelling produced an undirected edge, nor any orientation preference — there
    is none to record (§3).
  - `NodeBinding`, `Endpoint`, and every other type are unchanged: direction is
    an edge-only attribute.
- **Parser change** — `collectEdge` stops rejecting `left == right`. Instead:
  - one arrow (`left != right`): **directed** (`directed = true`),
    canonicalised exactly as today — a right arrow keeps `prev→next`, a left
    arrow flips to `next→prev`.
  - no arrow or both arrows (`left == right`): **undirected**
    (`directed = false`), endpoints recorded in **textual order** (`prev→next`),
    with **no** canonical flip — the resolver tries both orientations, so there
    is no canonical one to pick.
  - `ErrUnsupportedPattern` no longer fires for the undirected case. It still
    fires for named paths, variable-length relationships, and multi-type
    relationships (unchanged), so an undirected edge that is *also* one of those
    (e.g. `()-[r*]-()`, `()-[:A|:B]-()`) stays PENDING via the surviving cause.
    The surviving causes fire through **independent paths**, which is why removing
    the one `left == right` check cannot leak any of them: named paths fail in
    `collectPattern` *before* `collectEdge` is reached; variable-length fails in a
    separate `EnterOC_RangeLiteral` walk callback, not in `collectEdge` at all;
    multi-type fails on the `relTypes` check *inside* `collectEdge`, after the
    (now-removed) undirected check. Only the undirected check is deleted; each
    other guard is on a path the deletion does not touch. (`l.fail` latches the
    first error only, so for a doubly-unsupported edge whichever guard the walk
    reaches first wins — but all of them still reject, which is what matters.)
- **Layer-1 corpus** — **no new directory.** Undirected patterns already live
  throughout the existing `readCoreDirs` (`clauses/match`, `clauses/return`,
  `clauses/match-where`, …); the unlock simply flips every scenario whose *only*
  unsupported feature was the undirected edge from PENDING to PASSING.
- **Layer-2 pins** — a new verbatim `mustParse` case for a canonical undirected
  pattern (`MATCH (a)-[r]-(b) RETURN r`) and, if a both-arrows scenario is worth
  pinning, `MATCH (a)<-[r]->(b) RETURN r`. Any existing `mustReject` case that
  pinned an undirected pattern to `ErrUnsupportedPattern` is **removed** (its
  query now parses). `ErrUnsupportedPattern` reachability is preserved by the
  surviving named-path / variable-length / multi-type rejects.
- **Sentinel doc trim** — `ErrUnsupportedPattern`'s docstring drops "undirected
  relationships"; it now lists named paths, variable-length, and multi-type
  relationships.
- **Docs inline** — this spec; CONTEXT.md `Endpoint` (query side) and `Binding`
  entries updated to describe the direction marker and undirected lowering, plus
  a query-side **Direction** note distinguishing it from the schema side's
  directed-only **Direction**; an ADR 0003 note that the curated subset now
  includes the edge direction marker.

Nothing downstream of the parser is built (no resolver, no codegen) — ADR 0004.
Trying both orientations against the schema is a resolver concern (named in §1,
to be filed/confirmed as a follow-up bead).

---

## 3. Model delta

```
EdgeBinding = { variable; labels; source Endpoint; target Endpoint; nullable bool; directed bool }   // + directed
```

- The common case `MATCH (a)-[r:KNOWS]->(b) RETURN r` is `directed = true`, with
  the existing canonical `source→target`. The undirected case
  `MATCH (a)-[r]-(b) RETURN r` is `directed = false`, `source = a`, `target = b`
  (textual order).
- **Why a bare bool, not an enum.** Direction is binary on the read surface:
  directed or undirected. (The schema side already canonicalises a left arrow to
  `source→target` and "undirected edges are not supported" there; the *query*
  side adds the undirected case but still has only two states.) A bool mirrors
  `nullable`/`ReturnsAll`; an int-enum is reserved for the genuinely multi-valued
  axes (`AggregateFunc`, `UnionKind`, `ClauseSlot`). There is no third direction.
  Note the bool is **not** a left-vs-right axis: both arrow spellings collapse to
  `directed = true`, with the left/right distinction discharged at lowering time
  by the existing canonical flip (`<--` becomes `source→target`). The bool's two
  states are therefore "has an authoritative orientation" (true) versus "has none,
  try both" (false) — exactly the one bit the resolver branches on. A left-vs-
  right enum would re-introduce a distinction the model deliberately erases.
- **Terminology, against the schema-side `Direction` glossary.** The schema-side
  `Direction` entry states flatly "undirected edges are not supported." That is
  *still true of the schema*, and Stage 5 does not change it: the schema model
  remains directed-only and stores each edge type as one canonical `EdgeKey`. The
  query side now diverges — it admits undirected patterns — so the two `Direction`
  notions are no longer the same concept under one word. CONTEXT.md gains a
  query-side `Direction` note that names the split explicitly (schema: directed-
  only, canonicalised; query: directed *or* undirected, the latter a deferred-
  orientation marker) so the shared word does not paper over a real asymmetry. No
  schema-side entry changes.
- **Why no orientation is recorded for undirected.** A directed edge has one true
  orientation, which the parser canonicalises (a left arrow becomes
  `source→target`). An undirected edge has *no* true orientation: both
  `(a)→(b)` and `(b)→(a)` are candidate matches, and choosing one would be a lie
  the resolver then has to undo. The parser records the endpoints in the order
  written (`prev→next`) purely so the two ends are *identified*; `directed=false`
  tells the resolver that order is not authoritative and both must be tried. This
  is the ADR 0003 line: record the structural fact (undirected), not the
  resolution policy (try-both).
- **EdgeBinding stays a sealed-sum member with smart constructors.** Adding a
  field does not change that `Binding` is a node-xor-edge choice; the new
  `directed` field is set through the constructor like `source`/`target`. The
  constructor signature question (how to admit `directed` without a 2×2 variant
  explosion against `nullable`) is settled in §4.
- **Anonymous and self-loop cases are unchanged in handling.** An anonymous
  undirected edge `()-[]-()` is its own binding with `directed=false`, exactly as
  an anonymous directed edge is its own binding today. A self-loop `(a)-[r]-(a)`
  records both endpoints as `VarEndpoint(a)` with `directed=false`. Reusing a
  relationship variable across an undirected pattern remains the
  already-skiplisted relationship-uniqueness concern (below the boundary).

This stays within ADR 0003 curation: the model gains exactly one marker bit the
resolver needs (try one orientation vs both) and nothing of the expression tree,
the spelling, or the orientation-trial policy.

---

## 4. Constructor shape (the one design decision)

`EdgeBinding` today has two constructors, `NewEdgeBinding` and
`NewNullableEdgeBinding` — `nullable` is modelled as a *variant constructor* (it
is shared with `NodeBinding` and carries ADR 0006 weight). Adding `directed` as a
second variant axis would force a 2×2 explosion (`New[Nullable][Undirected]
EdgeBinding`), which is bad taste.

**Decision: `directed` is a constructor parameter, not a variant.** The two edge
constructors gain a trailing `directed bool`:

```
NewEdgeBinding(variable string, labels graph.LabelSet, source, target Endpoint, directed bool) (EdgeBinding, error)
NewNullableEdgeBinding(variable string, labels graph.LabelSet, source, target Endpoint, directed bool) (EdgeBinding, error)
```

Rationale: `nullable` and `directed` are not symmetric. `nullable` is shared
across node and edge bindings, has its own ADR (0006), and reads naturally as a
named variant ("the OPTIONAL-introduced edge"). `directed` is edge-only, binary,
and carries no cross-cutting semantics — it is just another field of the edge,
like `source`/`target`, which are already plain parameters. Making it a parameter
keeps the constructor count at two and the call sites explicit (`directed: true`
at every existing site, `directed: false` only in the undirected path). If the
freeze ADR later unifies the binding constructors (e.g. options), it does so for
`nullable` and `directed` together as one policy — not unilaterally mid-evolution
(the same posture Stage 4 §3 took on product invariants).

A hostile reviewer's objection answered: "you've made the edge constructor take a
naked trailing `bool`, the classic unreadable call site (`NewEdgeBinding(v, ls,
s, t, true)` — true *what*?)." Two mitigations make this acceptable rather than
ugly: (1) every existing call site is `toBinding` (one place), which passes a
named source (`!rb.undirected`, see below), not a literal `true`; the Layer-2
pins read `directed: true` via a struct field, not a positional literal. (2) the
field is documented and accessor-backed (`Directed()`), so the meaning is one
hop away. A named-variant `NewUndirectedEdgeBinding` would read better at a call
site but reintroduces the 2×2 explosion against `nullable` the moment an
`OPTIONAL` undirected edge appears (`()-[r]-()` inside `OPTIONAL MATCH` is legal
openCypher), which is the worse outcome. Parameter it is.

**The raw-binding plumbing — and a Go zero-value correction to the earlier
draft.** The earlier draft said `rawBinding` gains a `directed bool` that
"defaults to `true`." That is **wrong**: a Go `bool` field zero-values to
`false`, so a bare `directed bool` would silently make every existing
struct-literal construction site (`pattern.go` lines ~134 and ~181) *undirected*
— the exact inversion of intent, and a latent corruption of every directed edge.
Two ways to keep the common (directed) case as the untouched zero value:

- **Chosen: invert the raw field to `undirected bool`.** `rawBinding` gains
  `undirected bool`, whose `false` zero value means directed — so both existing
  literals are unchanged with no edit, and `collectEdge` sets `undirected: true`
  *only* on the undirected branch. `toBinding` passes `directed: !rb.undirected`
  to the constructor, so the **model field stays positive** (`directed`, matching
  the wire key and the schema side's positive `Direction`) while the raw field
  stays zero-value-safe. The polarity flip lives at the one `toBinding` hop and is
  commented there.
- Rejected: keep `rawBinding.directed bool` and add `directed: true` to both
  existing literals. Correct, but it edits untouched directed-construction sites
  for a field they don't care about and invites the next literal to forget it
  (re-arming the same zero-value trap). Inverting the raw field removes the trap
  structurally rather than relying on every author to remember it.

The model-facing constructor parameter and JSON key remain `directed` (positive);
only the listener's transient `rawBinding` carries the inverted `undirected`.

---

## 5. Wire format (JSON shapes)

`EdgeBinding` marshals with one new always-emitted key:

```
edge → {"kind":"edge", "variable":..., "labels":..., "source":..., "target":..., "nullable":..., "directed": true|false}
```

`directed` is always emitted (matching the always-emit convention `nullable` /
`returnsAll` follow), so **every existing golden that contains an edge binding
regenerates** (it gains `"directed": true`). Node-only goldens are unchanged. New
goldens are added on the unlock commit for the newly-passing undirected scenarios.

---

## 6. Test corpus and skiplist

**No new `readCoreDirs` entry.** The unlock flips the undirected-only scenarios
already in the corpus to PASSING. The before/after godog counts are pinned on the
unlock commit's `-update` run and recorded in the bead (expected: a large PENDING
→ PASSING shift, bounded by the scenarios that *also* use variable-length /
multi-type / named-path, which stay PENDING via the surviving sentinels).

**Skiplist:** Stage 5 introduces **no new sentinel-rejection** and is expected to
add **few or no** skiplist entries — supporting undirected does not create new
*negative* scenarios; the negatives undirected patterns appear in (relationship
re-use, etc.) are already skiplisted or already rejected by an unchanged rule. The
unlock commit audits the live corpus and pins any genuinely-new
accept-then-value-error scenario (e.g. an undirected pattern whose only TCK error
is a runtime/value rule below the boundary), guarded by `TestSkiplistOrphans`.
If the audit finds none, the skiplist is unchanged — recorded explicitly in the
bead so the empty delta is intentional, not an oversight.

### Layer-2 rule

Unchanged directional discipline (Stage 1 §6). Stage 5 adds the undirected
`mustParse` case(s) above and removes any `mustReject` that pinned an undirected
pattern. `ErrUnsupportedPattern` reachability is preserved by the surviving
named-path / variable-length / multi-type rejects, so `TestSentinelReachability`
stays green with no authored replacement needed (the implementer **must** confirm
this against the live pins, not assume it).

---

## 7. Definition of done for Stage 5

1. `stage-5-undirected` lands green and independently mergeable; `master` is green
   if it lands solo.
2. `just test` green: query-package unit tests (the `Directed()` accessor, the
   JSON shape, construction invariants) and the cypher-package
   acceptance/pin/orphan/reachability suites.
3. Layer-1 godog count rises by the undirected flips (passed up, pending down;
   exact deltas pinned by the unlock commit's `-update` run and recorded in the
   bead).
4. Documentation: this spec; CONTEXT.md `Endpoint`/`Binding` revisions + the
   query-side `Direction` note; ADR 0003 note that the curated subset now includes
   the edge direction marker.
5. Beads: `gqlc-7z6` closed; the resolver-side "try both orientations" follow-up
   filed (or an existing resolver bead confirmed to cover it) so the dropped
   concern is tracked, not lost.

---

## 8. Commit inventory (single branch `stage-5-undirected`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec; CONTEXT.md Endpoint/Binding/Direction entries; ADR 0003 note |
| model | `EdgeBinding.directed` field + `Directed()` accessor + constructor param + JSON emit + all edge goldens regenerated; parser still rejects undirected (every emitted edge is `directed:true`, old behaviour, new shape) |
| unlock | `collectEdge` stops rejecting `left == right`, sets `rawBinding.undirected=true` on the undirected branch only (zero-value-safe, §4); `toBinding` passes `directed: !rb.undirected`; new goldens for flipped scenarios; any new skiplist entry; Layer-2 `mustParse` add / `mustReject` remove; `ErrUnsupportedPattern` docstring trim |

---

## 9. Weakest point (recorded honestly per ADR 0004)

The riskiest piece is **not** the `directed bool` shape itself (a bool is plainly
right for a binary, spelling-collapsed read surface — see below). It is the
**resolution contract the marker implicitly hands off**: a `directed = false` edge
tells the resolver to try both orientations against the directed schema, and that
trial can match **zero, one, or two** distinct directed edge types. The parser
records one binding with one endpoint pair and one marker; it does **not** — and
under ADR 0003 must not — say what a *double* match means (the schema has both
`A→B` and `B→A` with the queried label: is the undirected edge ambiguous, a union
of both, or an error?). The bool is sufficient *input* for the resolver to make
that call, but the call is unmade and unwritten, because the resolver does not
exist (ADR 0004 freeze gate). The honest risk is therefore: when the resolver is
built it may discover it needs more than "try both" can express from a single
marker (e.g. to report *which* orientation(s) matched, for a typed result or a
diagnostic). If so, the marker widens — but that is a resolver-era model revision
the freeze ADR explicitly admits (`gqlc-cta` is downstream of the resolver design,
not upstream of it), and it is cheap because no consumer is attached yet. This
risk rides with the "try both orientations" resolver follow-up bead (§7 item 5)
so it is tracked as a known open contract, not discovered late.

Two lesser, genuinely low futures, recorded for completeness — both schema/dialect
futures, not parser gaps, both cheap to widen (bool → enum):

- **A schema that admits undirected edge types.** If the schema side ever gains
  undirected edges (today it does not — left arrows canonicalise and undirected is
  unsupported), a query-side undirected edge would resolve directly rather than by
  trying both orientations, and the resolver might want to distinguish "undirected
  because the author wrote `--`" from a directed edge. The bool already captures
  exactly that, so this future is covered.
- **Distinguishing `--` from `<-->`.** openCypher treats both as undirected, and
  no read-surface semantics depend on the spelling, so the model collapses them.
  If a dialect ever attached meaning to the both-arrows form, the bool would need
  to become a small enum. This is judged vanishingly unlikely (it is undirected in
  every openCypher dialect), so collapsing is the right curation now; the freeze
  ADR can revisit if a dialect proves otherwise.

The discipline that keeps all three safe is the same as every prior stage: record
the structural fact (undirected — no authoritative orientation), refuse to model
the resolution policy (try-both, and what a multi-match means), and let the
resolver own orientation.
