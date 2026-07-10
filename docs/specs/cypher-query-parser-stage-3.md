# Stage 3 spec — Cypher query parser: projections & aggregations

The implementation brief for Stage 3 of the Cypher implementation of
`query.Parser`. This is the third model evolution after Stage 2 per ADR 0004
(test-first, evolving until feature-complete) and per the curation discipline
of ADR 0003 and the type-interface boundary of ADR 0005. Stage 3 widens what
a RETURN item may be: from "bare variable or single-level property lookup" to
a small, closed expression sum that adds literals, function calls, aggregates
(`count`/`sum`/`collect`/`min`/`max`/`avg`/…), and `RETURN *`.

This document is a **delta** against [Stage 0](./cypher-query-parser-stage-0.md),
[Stage 1](./cypher-query-parser-stage-1.md) and
[Stage 2](./cypher-query-parser-stage-2.md); everything not stated here
carries over verbatim. Sections appear here only where Stage 3 changes
something.

Tracking: bead `gqlc-npg`. Lands as one graphite branch (`stage-3-projections`)
with separated commits (prep/spec → model+goldens → unlock+skiplist+layer-2),
independently mergeable as a whole: `just test` is green if this branch lands on
`master` alone (AGENTS.md stacked-branch invariant).

---

## 1. Deliverables

- **Model evolution** — `ReturnItem.Ref` (a bare `query.Ref`) is replaced by
  `ReturnItem.Value`, a closed sum `Projection`. `Projection` has four variants:
  `RefProjection` (the existing var / var.prop case, wrapping a `Ref`),
  `LiteralProjection` (a value below the type-interface boundary, carrying
  nothing structured), `FuncProjection` (a non-aggregate function call carrying
  its referenced bindings as `[]Ref`), and `AggregateProjection` (an aggregate
  carrying its `AggregateFunc` enum and its referenced bindings as `[]Ref`).
  The query-level `Query.ReturnsAll bool` carries `RETURN *`. Smart constructors
  are the only writers; variant fields are unexported, mirroring the existing
  `Use` sum (`internal/query/query.go`).
- **Parser change** — the listener no longer rejects every RETURN item that
  isn't var/var.prop; it classifies the projection expression into one of the
  variants, and accepts the `*` alternative by setting `ReturnsAll`.
  `ErrUnsupportedProjection` stops firing for these shapes and is retained only
  for residual out-of-scope projections (arithmetic over projections, list/map
  literals, CASE, comprehensions, label predicates, nested aggregates). New
  classifiers mine the `[]Ref` a func/aggregate argument touches
  (`internal/query/cypher/{expr.go,build.go,listener.go}`).
- **Layer-1 corpus** — `readCoreDirs` is **unchanged**. `clauses/return/` is
  already in the suite; `expressions/aggregation/` is **NOT** added (§6 audit:
  it pulls in WITH-grouping and value-semantics scenarios with near-zero
  parse-only yield). The unlock flips the WITH-free count-aggregation and
  column-rename scenarios to PASSING; a new skiplist group absorbs the residual
  value-semantics negatives.
- **Layer-2 pins** — new verbatim `mustParse` cases for the canonical aggregate
  shape and `RETURN *`; the `mustReject` `"return star"` case is **removed** (its
  query now parses) and replaced by an AUTHORED residual-projection reject
  (arithmetic-over-projection) preserving `ErrUnsupportedProjection`
  reachability (`internal/query/cypher/parser_test.go`).
- **Sentinel doc trim** — `ErrUnsupportedProjection`'s docstring drops
  `RETURN *`, aggregations and function calls; it now describes only the residual
  rich-projection set (`internal/query/cypher/errors.go`).
- **Docs inline** — this spec; CONTEXT.md "Return item" plus new
  "Projection" / "Aggregate" glossary entries; an ADR 0003 note that the curated
  subset now includes the projection sum.

Nothing downstream of the parser is built (no resolver, no codegen) — ADR 0004.

---

## 2. Model delta

`ReturnItem` changes from `{ Name string; Ref Ref }` to `{ Name string;
Value Projection }`, where `Projection` is a sealed sum (closed interface
`isProjection()`, unexported variant fields, smart constructors only — the
exact discipline `Use` already follows):

```
Projection    = RefProjection | LiteralProjection | FuncProjection | AggregateProjection
AggregateFunc = AggCount | AggSum | AggCollect | AggMin | AggMax | AggAvg | AggStdev | AggPercentile
                (int-backed, stringer — mirrors graph.EntityKind / ClauseSlot)
```

- `RefProjection` wraps a `Ref` — the Stage-0/1/2 case verbatim. The listener
  only builds it after the shape gates accept var/var.prop.
- `LiteralProjection` carries no structured value — only the surface text is the
  column name (already on `ReturnItem.Name`). A literal's *value* lives **below
  the type-interface boundary** (ADR 0005, B1): re-executed from the original
  text, never reconstructed. The variant exists so the column is counted and
  named; dropping it would be a type-interface change. It carries no `Ref` (a
  literal traces back to no binding).
- `FuncProjection` carries the function's referenced bindings as `[]Ref` (the
  var/var.prop arguments the resolver must trace) and **nothing about the
  function itself** — not its name, not its arity, not its return type. The
  function's identity and signature are a resolver/engine concern below the
  boundary (ADR 0005); the engine re-executes `type(r)` / `coalesce(a.num, b.num)`
  from the original text. The model carries only "this column depends on these
  bindings" so referential integrity (every `Ref` resolves) holds.
- `AggregateProjection` carries an `AggregateFunc` and the referenced bindings
  as `[]Ref`. The aggregate *kind* is carried (§4) because it is the one
  function-call distinction that is type-interface-relevant before Stage 14: an
  aggregate collapses rows, changing result cardinality the generated code must
  model. `count(*)` is the degenerate case — `AggCount` with empty `[]Ref`.

This is a wider sum than Stage 1's `Use` but obeys the same rule: it carries
exactly what the resolver needs to reach a schema type (the `[]Ref`s) and the
one cardinality-bearing distinction (aggregate vs not), and **nothing of the
expression tree** (ADR 0003: pattern topology and the full expression tree are
deliberately outside the model).

---

## 3. `RETURN *` representation

`RETURN *` is a **query-level wildcard over the in-scope bindings**, not a
return item. Without a schema the parser cannot enumerate the columns `*`
expands to, and the resolver owns expansion anyway (since `WITH` (Stage 4)
rebinds scope). The honest representation is a boolean on `Query`:

```
Query.ReturnsAll bool          // true iff the projection body was the '*' alternative
Query.Returns    []ReturnItem  // empty when ReturnsAll, else the explicit columns
```

`RETURN *` does not mix with explicit items in the read core (`RETURN *, x` is a
Stage-4-scope shape), so the two are mutually exclusive at Stage 3: when
`ReturnsAll` is true, `Returns` is empty. The resolver expands `*` to the
in-scope bindings later. The parser records "the author asked for every
in-scope binding" without guessing the column list — the schema-agnostic posture
of ADR 0003.

`RETURN *` with nothing in scope (`MATCH () RETURN *`, NoVariablesInScope) is a
scope/value error below the boundary: it parse-accepts as `ReturnsAll` and is
**skiplisted**, not rejected (§6).

---

## 4. Aggregate vs ordinary function call

The distinction **is** needed before Stage 14, for one reason: an aggregate changes
result **cardinality** (it collapses matched rows into groups), a type-interface
fact the generated code models differently from a row-wise function. A
non-aggregate function (`type(r)`, `coalesce(...)`) is row-wise; column count and
grouping are unaffected. So `AggregateProjection` is a distinct variant from
`FuncProjection`.

The set of aggregate names is **closed and known** (the openCypher aggregating
functions: `count`, `sum`, `collect`, `min`, `max`, `avg`, `stDev`/`stDevP`,
`percentileCont`/`percentileDisc`), so `AggregateFunc` is an int-backed enum with
a stringer, classified by case-insensitive name match in the listener (the TCK
uses `cOuNt`, `aVg`). Anything not in the set is a `FuncProjection`. We do **not**
carry the non-aggregate function's name: its identity is below the boundary. We
do carry the aggregate's kind because it is the cardinality signal.

Out of scope at Stage 3: implicit-grouping correctness (which non-aggregate
columns form the grouping key), nested aggregates, aggregates-inside-expressions.
These are grouping *semantics*, a resolver concern, and most are entangled with
`WITH` (Stage 4). See §6 and the weakest-point note in §9.

---

## 5. Wire format (JSON shapes)

`ReturnItem` marshals with its `Value` as a tagged-union member, one level deep,
matching the `Binding`/`Use` convention (`"kind"` discriminator):

```
RefProjection        →  {"name": "...", "value": {"kind": "ref", "variable": "...", "property": "..."}}
LiteralProjection    →  {"name": "...", "value": {"kind": "literal"}}
FuncProjection       →  {"name": "...", "value": {"kind": "func", "refs": [{"variable":"...","property":"..."}, ...]}}
AggregateProjection  →  {"name": "...", "value": {"kind": "aggregate", "func": "count", "refs": [...]}}
```

`Query` gains `"returnsAll": false|true`, always emitted (matching the
always-emit convention). The `"func"` value derives from `AggregateFunc.String()`
— the single source the discriminator follows. The projection `"kind"` constants
live next to the existing kind constants in `query.go`.

This moves the `ReturnItem` shape: every golden's `"ref"` field under a return
item becomes a nested `"value"` object, so **all 77 existing goldens regenerate**
on the model commit (the only diff outside `internal/query/` is the golden tree).
New goldens are added on the unlock commit for the newly-passing aggregate /
rename / `RETURN *` scenarios.

---

## 6. Test corpus and skiplist

**`readCoreDirs` is unchanged.** `clauses/return/` is already in the suite from
Stage 0. The unlock is a pure parser-accept widening within the existing dirs.

**`expressions/aggregation/` is NOT added.** Audit: its feature files are
result-value semantics (group-by correctness, distinct semantics, null
handling) — every scenario either uses `WITH` (Stage 4), asserts a computed
result value below the boundary, or exercises grouping semantics the parser does
not model. Adding the dir would add dozens of PENDING/skip entries for near-zero
parse-only signal and mislead the progress meter. Defer to Stage 4.

**Scenarios expected to flip PENDING → PASSING in `clauses/return/`** (WITH-free,
no named path, no write clause; shape is var/var.prop/literal/func/aggregate or
`RETURN *`): the bare-aggregate and column-rename families in Return4 and Return6
(e.g. `count(*)`, `count(*) AS c`, `RETURN n.num, count(*)`). The exact set is
pinned by the unlock commit's `-update` run and recorded in the bead; the spec
does not pin scenario indices because TCK reindexing would stale them — the
harness's `TestSkiplistOrphans` is the guard against drift.

**New skiplist group — "projection value/semantics below the boundary"** (each
entry parse-accepts; the error lives below the type-interface boundary, ADR 0005,
so the re-executed text raises it):

- `RETURN *` with no variables in scope (NoVariablesInScope) — `*` expands to
  zero columns, a scope/value error.
- Projecting a non-existent function (UnknownFunction) — function identity is
  below the boundary; the parser carries no function name (§2).
- Multiple columns with the same name (ColumnNameConflict) — a value-level
  result-shape check; `Returns` is duplicate-preserving (Stage-0 rule).
- Aggregate value-semantics negatives that parse-accept to a flat aggregate
  shape (genuinely nested aggregates stay residual `ErrUnsupportedProjection`
  PENDING — do **not** skiplist those).

Each entry is pinned to its actual cause in the established style; the exact
scenario titles are filled in on the unlock commit against the live corpus.
`TestSkiplistOrphans` guards the new entries; `TestNoUndefinedSteps` and
`TestSentinelReachability` stay green (the `return star` reject is replaced by an
authored residual-projection reject preserving `ErrUnsupportedProjection`).

### Layer-2 rule

Unchanged directional discipline (Stage 1 §6). Stage 3 adds two verbatim
`mustParse` cases (a flat aggregate; `RETURN *`), removes the `"return star"`
`mustReject` (now parses), and adds one AUTHORED `mustReject` for the residual
fail-site (`RETURN a.num + 1` arithmetic-over-projection → `ErrUnsupportedProjection`),
since no clean verbatim corpus query exercises the residual fail-site without a
disqualifying clause. Net Layer-2: +2 mustParse, −1 mustReject (return star),
+1 mustReject (authored residual).

---

## 7. Definition of done for Stage 3

1. `stage-3-projections` lands green and independently mergeable; `master` is
   green if it lands solo.
2. `just test` green: query-package unit tests (the new constructors,
   `AggregateFunc` stringer, JSON shapes, property tests) and cypher-package
   `TestMustParse` / `TestMustReject` / `TestReadCoreAcceptance` /
   `TestNoUndefinedSteps` / `TestSkiplistOrphans` / `TestSentinelReachability`.
3. Layer-1 godog count rises by the clean flips (passed up, pending down; exact
   deltas pinned by the unlock commit's `-update` run and recorded in the bead).
4. Documentation: this spec; CONTEXT.md "Return item" + new "Projection" /
   "Aggregate" entries; ADR 0003 note that the curated subset now includes the
   projection sum.
5. Beads: `gqlc-npg` closed; a resolver-side grouping-semantics follow-up filed
   (depends on Stage 14 + Stage 4 WITH).

---

## 8. Commit inventory (single branch `stage-3-projections`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec; CONTEXT.md Projection/Aggregate entries; ADR 0003 note |
| model | `Projection` sum + `AggregateFunc` enum + `ReturnsAll` + constructors + JSON tags + all 77 goldens regenerated |
| unlock | listener classifies projections + accepts `*`; new skiplist group; Layer-2 swaps; new goldens; `ErrUnsupportedProjection` docstring trim |

---

## 9. Weakest point (recorded honestly per ADR 0004)

The aggregate-vs-func cardinality justification (§4) is a promise the parser
cannot fully keep alone. The parser marks a column as aggregate, but *implicit
grouping* — which non-aggregate columns become the group key — is genuinely a
resolver/Stage-4 concern, and the openCypher rules here are subtle
(ambiguous-aggregation errors). There is a real risk that when the resolver and
Stage 4 arrive, `AggregateProjection`'s `[]Ref`-only payload proves insufficient
and the variant must be reopened. That is acceptable *only* because ADR 0004
keeps the model unlocked until Stage 14 and no consumer is attached yet — the exact
cost the staging plan was designed to absorb. The discipline that keeps this safe
is refusing to model the expression tree: hold that line against every change
that wants "just one more field."
