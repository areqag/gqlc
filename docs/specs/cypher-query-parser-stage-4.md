# Stage 4 spec — Cypher query parser: multi-scope (WITH) & UNION

The implementation brief for Stage 4 of the Cypher implementation of
`query.Parser`. This is the fourth model evolution after Stage 3 per ADR 0004
(test-first, evolving until feature-complete) and per the curation discipline
of ADR 0003 and the type-interface boundary of ADR 0005. Stage 4 lifts the
parser from a **single flat scope** to the two structural axes the openCypher
read surface still needs: `WITH` chaining (sequential intermediate scopes) and
`UNION` (parallel result branches). It depends on Stage 3 because `WITH` projects
exactly the values Stage 3's projection sum introduced — a WITH item is a
RETURN item in an intermediate position.

This document is a **delta** against [Stage 0](./cypher-query-parser-stage-0.md),
[Stage 1](./cypher-query-parser-stage-1.md),
[Stage 2](./cypher-query-parser-stage-2.md) and
[Stage 3](./cypher-query-parser-stage-3.md); everything not stated here carries
over verbatim. Sections appear here only where Stage 4 changes something.

Tracking: bead `gqlc-v23`. Lands as one graphite branch (`stage-4-with-union`)
with separated commits (prep/spec → model+goldens → unlock+corpus+skiplist+
layer-2), independently mergeable as a whole: `just test` is green if this branch
lands on `master` alone (AGENTS.md stacked-branch invariant).

---

## 1. Why one atomic cycle (not two)

`WITH` and `UNION` look separable but are not, because both are the *same*
restructure of `Query`: the flat scope (`Bindings`/`Returns`/`ReturnsAll` on
`Query`) becomes a nested **Branch → Part** structure. `UNION` adds the branch
axis; `WITH` adds the part axis.

The tempting split — land `WITH` first (parts), then `UNION` (branches) — does
not survive the solo-mergeability rule, and the reason is concrete, not hand-
wavy. There are only two ways to stage it and both break:

- **`WITH`-first introduces only `Query.Parts []QueryPart`** (no branch type
  yet, since one query = one branch). Then the `UNION` branch must restructure
  `Query.Parts` into `Query.Branches []QueryBranch` where `QueryBranch` holds the
  parts. That is a *second* top-level `Query` shape change: it moves the field
  the first branch just added and regenerates every golden a second time. The
  `UNION` branch cannot land solo on `master` (where `Query.Parts` does not
  exist), so it is not independently mergeable — the invariant fails.
- **`WITH`-first introduces the full `QueryBranch`/`QueryPart` nesting** (one
  branch, ≥1 part) and `UNION` only flips the single branch to many. Now the
  `UNION` branch's code (`Combinators`, the multi-branch listener path) compiles
  only against the `QueryBranch` type the first branch introduced, so it again
  cannot land solo on `master`.

Either way the second branch depends on a type or field the first introduced, so
it fails the AGENTS.md stacked-branch invariant (`just test` green if the branch
lands on `master` alone). The model restructure is therefore one atomic unit and
lands as one cycle. (Per ADR 0004 the model is still unlocked; this is exactly
the kind of shape change the staging plan front-loads while no consumer is
attached.)

---

## 2. Deliverables

- **Model evolution** — the per-query scope fields move off `Query` into a
  nested structure:
  - `Query` becomes `{ Branches []QueryBranch; Combinators []UnionKind;
    Parameters []Parameter }`. `Parameters` **stays at `Query` level** (a
    parameter used in any part of any branch is one generated method argument,
    deduplicated across the whole query, first-appearance order — unchanged from
    Stage 1). `Combinators` has `len(Branches)-1` entries: the `i`-th records
    how branch `i+1` was joined to branch `i` (`UNION` distinct vs `UNION ALL`).
  - `QueryBranch` is `{ Parts []QueryPart }` — one `oC_SingleQuery`. At least one
    part; non-final parts each end in a `WITH`, the final part ends in `RETURN`
    (positional — no per-part terminal flag).
  - `QueryPart` is `{ Bindings []Binding; Returns []ReturnItem; ReturnsAll bool }`
    — the Stage-0..3 flat scope, verbatim, now scoped to one part. A non-final
    part's `Returns`/`ReturnsAll` carry its `WITH` projection (a WITH item is a
    RETURN item — same `oC_ProjectionBody`, same Stage-3 `Projection` sum); the
    final part's carry the branch's result columns.
  - `UnionKind` is an int-backed enum with a stringer (`UnionDistinct`,
    `UnionAll`), mirroring `AggregateFunc`/`ClauseSlot`.
- **Parser change** — `EnterOC_With` and `EnterOC_Union` stop rejecting. The
  listener collects into a **current part**, closing it and opening a new one at
  each `WITH`, and starts a **new branch** at each additional `oC_SingleQuery`
  (recording the `UnionKind`). `Parameters` accumulate query-wide as before.
  `ErrUnsupportedClause` no longer fires for `WITH`/`UNION` (the write clauses,
  `UNWIND`, `CALL` still raise it).
- **Scope validation across parts** — `build`'s self-consistency check becomes
  **per-part scoped**: a `Ref` in part *K* must resolve to a binding in part *K*
  **or** to a name *exported into* part *K* by part *K−1*'s `WITH` (the `AS`
  alias, or the bare variable for `WITH a`; under `WITH *` the exported set is
  computed transitively — see §4). `build` walks parts left to right, threading
  the exported-name set forward, instead of resolving every `Ref` against one
  flat binding pool. A `WITH *` / `RETURN *` part carries all prior in-scope
  names forward (`ReturnsAll`), so it imposes no new resolution obligation.
  Unresolved ⇒ `ErrUnboundVariable` (unchanged sentinel).
- **Layer-1 corpus** — `readCoreDirs` gains `clauses/union` and `clauses/with`
  (the model-changing core). The `with-where`, `with-orderBy`, `with-skip-limit`
  extension dirs are added **iff** they flip cheaply to PASSING once WITH parses
  (they need no further model change — they reuse Stage 1–3 capabilities under a
  WITH scope); the exact inclusion is decided empirically on the unlock commit
  (§6) and recorded in the bead, mirroring Stage 3's `expressions/aggregation`
  audit.
- **Layer-2 pins** — new verbatim `mustParse` cases for a canonical `WITH`
  chain and a canonical two-branch `UNION`; the `mustReject` cases that asserted
  `WITH`/`UNION` raise `ErrUnsupportedClause` are **removed** (those queries now
  parse). `ErrUnsupportedClause` reachability is preserved by the surviving
  write-clause / `UNWIND` / `CALL` rejects.
- **Sentinel doc trim** — `ErrUnsupportedClause`'s docstring drops `WITH` and
  `UNION`; it now lists only the write clauses, `UNWIND`, `CALL`.
- **Docs inline** — this spec; CONTEXT.md `Query` entry revised, new
  `Query part` / `Branch` / `Union` glossary entries, `Binding`/`Variable`
  scope notes updated for WITH-introduced names; an ADR 0003 note that the
  curated subset now includes branch/part structure. (No ADR 0006 edit is
  required: its "Stage 4 introduces the per-clause structure" consequence is
  already written; this spec satisfies it — see §3 — and records the precise
  limit. The `gqlc-lqm`(b) cross-`WITH`/no-`WITH` distinction is noted in §3 so
  the resolver work does not over-claim what this stage enables.)

Nothing downstream of the parser is built (no resolver, no codegen) — ADR 0004.
Grouping-key semantics across `WITH` remain a resolver concern (`gqlc-gyw`).

---

## 3. Model delta

```
Query        = { Branches []QueryBranch; Combinators []UnionKind; Parameters []Parameter }
QueryBranch  = { Parts []QueryPart }
QueryPart    = { Bindings []Binding; Returns []ReturnItem; ReturnsAll bool }
UnionKind    = UnionDistinct | UnionAll      (int-backed, stringer)
```

- The common case `MATCH (n) RETURN n` is one branch of one part:
  `Query{ Branches: [{Parts: [{Bindings: [n], Returns: [n]}]}] }`. The nesting is
  always present — the structure mirrors the grammar (`oC_RegularQuery →
  oC_SingleQuery → oC_SinglePartQuery/oC_MultiPartQuery`) so the resolver never
  special-cases "flat vs nested".
- `QueryBranch`/`QueryPart` are **product types**, so they use exported fields
  and the builder maintains their invariants (a branch has ≥1 part; a part's
  `Returns` is empty iff `ReturnsAll`) — matching `Query`/`Ref`/`ReturnItem`,
  which are also builder-maintained products, *not* the sealed-sum discipline
  reserved for `Binding`/`Endpoint`/`Use`/`Projection`.
- **Resolved (was an open question): product with builder-maintained invariants,
  not a smart constructor.** The sealed-sum + smart-constructor discipline exists
  to make a *choice* illegal-state-free — to guarantee a value is exactly one of
  N variants with each variant's fields present (`Binding` is a node xor an edge;
  `Use` is a property xor a clause slot). `QueryBranch`/`QueryPart` are not
  choices; they are records. Their invariants are arity/cross-field constraints
  (≥1 part; `Returns` empty iff `ReturnsAll`), which is *exactly* the kind of
  invariant `Query` already carries today as a builder-maintained product (its
  own `Returns`-empty-iff-`ReturnsAll` rule — `internal/query/query.go`). Adding
  a smart constructor for the part would mean either (a) `Query` should have had
  one too and didn't — an inconsistency to fix *project-wide* in the freeze ADR,
  not to introduce unilaterally for one new type mid-evolution, or (b) treating
  arity invariants as sum-grade, which over-applies the discipline. The
  "make illegal states unrepresentable" value (commit #11) is satisfied by the
  builder being the *sole writer* and `-update` goldens proving the invariant
  holds across the whole corpus — the same guarantee `Query` relies on. **Mirror
  `Query`: exported fields, builder-maintained, no smart constructor.** If the
  freeze ADR later decides product invariants deserve constructors, it converts
  `Query`, `Ref`, `ReturnItem`, `QueryBranch` and `QueryPart` together, as one
  consistent policy.
- **Why parts carry their own bindings.** A binding is scoped: `MATCH (a) WITH
  a.x AS n RETURN n` has `a` in part 1's scope and `n` (a WITH-introduced name)
  in part 2's. A later part may also open its **own** reading clauses before its
  terminal `WITH`/`RETURN` (the grammar's `oC_MultiPartQuery` is
  `( oC_ReadingClause* oC_With )+ oC_SinglePartQuery`), so part *K*'s `Bindings`
  are exactly the `MATCH`es written *in part K*, not a query-wide pool. The
  resolver chains a final-`RETURN` `Ref` back through each part's `WITH` `Returns`
  to the originating `MATCH` binding; the parser records each part's bindings and
  named projection items so that chain exists. The parser does **not** type the
  chain (ADR 0003 / 0005) — it records structure only.
- **Per-part bindings are the clause structure ADR 0006 / `gqlc-lqm`(b) names.**
  ADR 0006's "two flow-typing regimes" consequence states that bare-pattern
  nullable demotion — a required clause that reuses an `OPTIONAL`-introduced
  binding, e.g. `OPTIONAL MATCH (a)-[:R]->(b) WITH b MATCH (b) RETURN b` —
  "require[s] per-clause structure to record the second reference; that structure
  is what Stage 4 (`WITH`/`UNION`) introduces." The per-part `Bindings` axis is
  precisely that structure: part 1 records the nullable `b`, the `WITH` carries
  `b` forward, and part 2 records its *own* non-nullable `b` from `MATCH (b)`.
  **The limit, stated honestly:** the part boundary is `WITH`, so this only
  records the *cross-`WITH`* second reference. A second reference within a single
  part — `OPTIONAL MATCH (a)-[:R]->(b) MATCH (b) RETURN b` with **no** `WITH` —
  still merges both `MATCH`es into one `rawBinding` (the Stage-0 merge rule is
  unchanged), so the resolver sees one binding and cannot demote it from clause
  structure alone. Stage 4 therefore enables `gqlc-lqm`(b) only for the
  `WITH`-separated form; the no-`WITH` form remains a resolver concern that
  needs more than this stage gives. The parser still records only the static
  introduction fact per ADR 0006 — it never demotes `Nullable` itself.
- **Why `UnionKind` is carried but column compatibility is not.** `UNION` vs
  `UNION ALL` is a cardinality distinction (distinct collapses duplicate rows)
  the generated code models — the same reason Stage 3 carries the aggregate
  kind. Whether two branches' columns are union-compatible (same names) is a
  value-level result-shape check below the type-interface boundary; the parser
  records each branch's columns (each branch's final-part `Returns`) and the
  resolver/engine checks compatibility (§6 skiplist). The single source for the
  *result column names* is branch 0's final part (openCypher's rule: a `UNION`'s
  output columns are named by the first query); the parser does not pre-pick that
  branch or merge the column lists — it records each branch verbatim and leaves
  the "branch 0 names the result, the rest must be compatible" rule to the
  resolver, the same posture as not enumerating `RETURN *` columns (Stage 3 §3).

This stays within ADR 0003 curation: the model gains exactly the two structural
axes the resolver needs (sequential scope chaining for WITH; parallel branches +
their join kind for UNION) and nothing of the expression tree, grouping keys, or
column-type compatibility.

---

## 4. WITH scope semantics the parser must honour

`WITH` is a scope **boundary and filter**, unlike `RETURN`:

- The next part's scope is **exactly** the names the `WITH` projects **plus the
  bindings that part itself introduces** in its own reading clauses. A name not
  carried by the `WITH` is out of scope downstream (this is what makes
  `MATCH (a) WITH a.x AS n RETURN a` an `ErrUnboundVariable`: `a` is not in
  scope after the `WITH`). A later part introducing its own `MATCH` is in scope:
  `MATCH (a) WITH a WHERE a.x > 1 MATCH (b) RETURN b` resolves `b` against part
  2's own binding and `a` against the `WITH`-carried name. `WITH *` carries all
  prior in-scope names forward.
- The carry is **transitive across parts, not just K−1 → K**: the scope at the
  entry of part *K* is the names exported by part *K−1*'s `WITH` (its `AS`
  aliases / bare variables, or — under `WITH *` — every name in scope at part
  *K−1*'s entry, recursively, unioned with part *K−1*'s own bindings). The
  per-part scope-resolution check therefore reads "is this `Ref` resolvable
  against {part *K*'s bindings} ∪ {names part *K−1* exported}", where the
  exported set is computed by walking parts left to right — it is **not** the
  full set of every binding ever introduced.
- A `WITH` item's name is its `AS` alias, or the bare variable for `WITH a`
  (`oC_ProjectionItem` second alternative). An un-aliased non-variable
  projection in a non-final `WITH` (`WITH a.x`) has no name to carry forward and
  is a value-level error below the boundary — skiplisted, not rejected, if the
  TCK exercises it.
- Parameters are unaffected by scope boundaries: `$p` in any part is the same
  query input. The existing first-appearance dedup runs query-wide.

The parser models the boundary structurally (each part's scope = its bindings +
the prior part's WITH names) and leaves *grouping semantics* (which non-aggregate
WITH columns form the implicit group key) to the resolver — `gqlc-gyw`, which
explicitly depends on this stage.

---

## 5. Wire format (JSON shapes)

`Query` marshals to its new nested shape. `QueryPart` and `QueryBranch` are plain
objects (no `"kind"` discriminator — they are products, not sum members):

```
Query  → {"branches": [ <branch>, ... ], "combinators": ["union"|"unionAll", ...], "parameters": [ ... ]}
branch → {"parts": [ <part>, ... ]}
part   → {"bindings": [ ... ], "returns": [ ... ], "returnsAll": false|true}
```

`Combinators` is always emitted (null when one branch), matching the always-emit
convention. The `UnionKind` string derives from `UnionKind.String()` — the single
source the wire value follows, so it cannot drift. `Parameters` keeps its
Stage-1 shape and Query-level position.

This moves the top-level `Query` shape, so **every existing golden regenerates**
on the model commit (the only diff outside `internal/query/` is the golden tree).
New goldens are added on the unlock commit for the newly-passing WITH/UNION
scenarios.

---

## 6. Test corpus and skiplist

**`readCoreDirs` gains `clauses/union` and `clauses/with`.** The unlock flips the
single-scope WITH chains and the well-formed UNIONs to PASSING. The `with-where`,
`with-orderBy`, `with-skip-limit` dirs are added on the unlock commit **iff**
their scenarios flip green without a further model change (they layer Stage 1–3
features under a WITH scope); if any pull in disproportionate skip/PENDING noise
for near-zero parse-only yield they are deferred and the deferral recorded in the
bead — the Stage 3 §6 discipline.

**New skiplist group — "multi-scope/branch value-semantics below the boundary"**
(each entry parse-accepts; the error lives below the type-interface boundary):

- `UNION` with mismatched / differently-named columns across branches
  (union-compatibility): a value-level result-shape check; the parser records
  each branch's columns and the resolver/engine checks compatibility.
- A `WITH` item whose value/semantics the TCK rejects at runtime (e.g. a
  non-aliased expression carried into a later scope, an aggregation-grouping
  error): the parser carries structure, the re-executed text raises it.

Genuinely unsupported shapes stay PENDING via their existing sentinels (e.g.
`UNWIND`/`CALL` as a reading clause inside a part → `ErrUnsupportedClause`;
projection shapes outside the Stage-3 sum → `ErrUnsupportedProjection`). The
exact skiplist titles are pinned on the unlock commit against the live corpus and
guarded by `TestSkiplistOrphans`.

### Layer-2 rule

Unchanged directional discipline (Stage 1 §6). Stage 4 adds two verbatim
`mustParse` cases (a `WITH` chain; a two-branch `UNION`), and removes the
`mustReject` cases that pinned `WITH`/`UNION` to `ErrUnsupportedClause` (their
queries now parse). `ErrUnsupportedClause` reachability is preserved by the
surviving write-clause / `UNWIND` / `CALL` rejects, so `TestSentinelReachability`
stays green with no authored replacement needed.

---

## 7. Definition of done for Stage 4

1. `stage-4-with-union` lands green and independently mergeable; `master` is
   green if it lands solo.
2. `just test` green: query-package unit tests (the new `QueryBranch`/`QueryPart`
   shapes, `UnionKind` stringer, JSON shapes, scope-validation property tests)
   and the cypher-package acceptance/pin/orphan/reachability suites.
3. Layer-1 godog count rises by the clean flips (passed up, pending down; exact
   deltas pinned by the unlock commit's `-update` run and recorded in the bead).
4. Documentation: this spec; CONTEXT.md `Query` revision + new `Query part` /
   `Branch` / `Union` entries; ADR 0003 note that the curated subset now includes
   branch/part structure.
5. Beads: `gqlc-v23` closed; the resolver-side WITH grouping follow-up
   (`gqlc-gyw`) confirmed still open and correctly dependent on this stage.

---

## 8. Commit inventory (single branch `stage-4-with-union`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec; CONTEXT.md Query/Branch/Part/Union entries; ADR 0003 note |
| model | `QueryBranch`/`QueryPart`/`UnionKind` + `Query` restructure + constructors/accessors + JSON + all goldens regenerated; parser minimally rebuilt to emit a single-branch/single-part `Query` (old behaviour, new shape) |
| unlock | listener splits parts at `WITH` and branches at `UNION`; per-part scope validation; new corpus dirs; new skiplist group; Layer-2 swaps; new goldens; `ErrUnsupportedClause` docstring trim |

---

## 9. Weakest point (recorded honestly per ADR 0004)

The per-part scope model (§4) is the riskiest piece: openCypher's `WITH` scope
rules interact with implicit grouping, `WITH *` expansion, and `ORDER BY`/`WHERE`
that reference both pre- and post-projection names, and the parser deliberately
models only the *structural* boundary, not the grouping semantics.

**The "carried-through binding vs freshly-projected scalar" question, resolved.**
The earlier draft worried `QueryPart`'s `Returns`-only record could not let the
resolver tell `WITH a` (carry the *entity* `a` forward, so the next part may
write `a.name`) from `WITH a.x AS n` (project a *scalar* `n`, on which `n.name`
is meaningless). It can, and the discriminator already exists in the Stage-3
`Projection` sum: a `WITH a` item is a `RefProjection` whose `Ref` has an
**empty `Property`** (a whole-entity reference); `WITH a.x AS n` is a
`RefProjection` with a **non-empty `Property`**; `WITH count(a) AS c` is an
`AggregateProjection`; `WITH 1 AS k` a `LiteralProjection`. So the resolver
reads each non-final part's `Returns` and classifies every exported name:
whole-entity `RefProjection` ⇒ a binding carried forward (chases its `Ref` back
to the originating binding, type and all); everything else ⇒ a scalar column
whose type the resolver computes but on which property access does not chain.
**Firm recommendation: do not add a field to `QueryPart` for this.** The
`Returns []ReturnItem` record is sufficient; the named-projection structure is
the discriminator. The residual risk is narrower than the draft implied: it is
*grouping* semantics (`gqlc-gyw` — which non-aggregate `WITH` columns form the
implicit group key), not entity-vs-scalar carry, and that risk is identical to
Stage 3's recorded weakest point (an aggregate's `[]Ref`-only payload may need
reopening). That is acceptable only because ADR 0004 keeps the model unlocked
until freeze and no consumer is attached yet. The discipline that keeps this
safe is the same as every prior stage: refuse to model the expression tree or
the grouping key; record structure and named projection items, and let the
resolver chain types.
