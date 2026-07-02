# Stage 6 spec — Cypher query parser: expression core & result typing

The implementation brief for Stage 6 of the Cypher implementation of
`query.Parser`. This is the sixth model evolution after Stage 5 per ADR 0004
(test-first, evolving until feature-complete) and per the curation discipline
of ADR 0003 and the type-interface boundary of ADR 0005. Stage 6 is the first
stage of the ADR-0007 pre-freeze expansion beyond the read core. It widens the
projection sum to accept the **full scalar expression grammar** (arithmetic,
comparisons, string/list/null predicates, CASE, list and map literals with
indexing and slicing, type-conversion functions, and unary/binary operator
precedence in general) at RETURN / WITH position and records each projected
column's **result type**. It closes the base type vocabulary the freeze locks:
`LIST` and `MAP` join the scalar types the model already carries implicitly;
`NODE` and `EDGE` are the two entity types a `RefProjection` already carries
transitively via its binding. Temporal types (`DATE`, `TIME`, `DATETIME`,
`LOCAL_TIME`, `LOCAL_DATETIME`, `DURATION`) and `PATH` are Stages 7 and 8 and
are deliberately absent from this stage.

This document is a **delta** against Stage 0–5 (referenced individually where
relevant); everything not stated here carries over verbatim. Sections appear
here only where Stage 6 changes something.

Tracking: bead `gqlc-97z` (GitHub #49). Lands as one graphite branch
(`stage-6-expressions`) with separated commits (prep/spec → red → green → any
refactor), independently mergeable as a whole: `just test` is green if this
branch lands on `master` alone (AGENTS.md stacked-branch invariant).

---

## 1. Deliverables

- **Model evolution** — `Projection` gains a new sealed-sum variant,
  `ExprProjection`, carrying a `Type` (the result type of the projected
  expression) and its `[]Ref` (the bindings the expression touches so the
  referential-integrity sweep covers them). `RefProjection`,
  `LiteralProjection`, `FuncProjection`, and `AggregateProjection` gain a
  `Type()` accessor so **every projection variant carries a result type** —
  this is the whole point of the stage. `LiteralProjection`'s type is known
  at parse time (a boolean literal is `BOOL`, an integer literal is `INT`, a
  list literal is `LIST`, etc.), so the variant grows one exported datum.
  `RefProjection`'s type is `NODE` for a whole-entity node reference, `EDGE`
  for a whole-entity edge reference, and `UNKNOWN` for a property lookup
  (`p.name` — the parser cannot type it schema-free, ADR 0003) — it is
  computed at build time from the referenced binding's kind. `FuncProjection`
  and `AggregateProjection` return `UNKNOWN` — a non-aggregate function's
  identity is below the boundary (ADR 0005, and Stage 3 §2), and an
  aggregate's result type depends on the argument type the parser also
  cannot compute schema-free.

- **A `Type` sum on the query package.** A sealed sum, `Type`, with
  variants for the parser-decidable openCypher types: `TypeBool`, `TypeInt`,
  `TypeFloat`, `TypeString`, `TypeNull`, `TypeList` (parameterised by an
  element `Type`), `TypeMap`, `TypeNode`, `TypeEdge`, and one distinguished
  `TypeUnknown`. `TypeAny` is deliberately absent — see §3. Sealed via a
  private `isType()` method; a stringer emits the wire name (`"bool"`,
  `"list<int>"` for a typed list, `"list<unknown>"` for an untyped list, and
  so on). Smart constructors are the only writers, mirroring the existing
  `Projection` / `Use` discipline. The temporal types (`DATE` and friends)
  and `TypePath` are **not** added — they are Stage 7 and Stage 8. That is
  explicit in the sum: adding them later is a variant addition, not a
  restructure of any existing variant.

- **Parser change** — the listener stops rejecting the projection shapes
  Stage 3 defined as "residual." `classifyProjection` no longer returns
  `nil, false` on a residual shape; instead it recognises the whole
  scalar-expression tree and returns an `ExprProjection` with the computed
  type. Bare-atom cases (`Ref`, scalar literal, function call, aggregate,
  `count(*)`) remain their existing dedicated variants — carrying their
  now-populated `Type()`. Only genuinely rich shapes (arithmetic, string
  predicates, `IS NULL`, list/map literals, list indexing, list slicing,
  `CASE`, chained comparisons, parenthesised expressions) fall through to
  `ExprProjection`. The classifier also mines refs from the whole
  sub-expression (the same `varRef` mechanism `functionArgRefs` uses today),
  so referential integrity holds — every `Ref` inside `expr.OC_Or...`
  resolves to a binding in the current part's scope.

  Rich expressions with a `$param` inside are accepted; the parameter is
  recorded via the existing mining channel with a `PropertyUse` on the
  paired binding property if the parser can recognise the pattern (a bare
  `var.prop = $p` comparison inside a rich expression), or via the mining
  for `WHERE` if the expression sits there. In a projection, the `$param`
  is more free-form; the honest recording is a new `ExprUse` variant on
  the `Use` sum — see §4.

- **`ErrUnsupportedProjection` retired.** The sentinel is deleted from
  `internal/query/cypher/errors.go`, from `unsupportedSentinels` and
  `allSentinels` in `internal/query/cypher/{acceptance_test.go,
  parser_test.go}`, and every skiplist entry pinned to it that now parses
  cleanly is removed. The one authored `mustReject` case
  ("arithmetic over projection") and the "unary-signed projection" case
  in `parser_test.go` become **`mustParse`** cases with authored
  expectations of an `ExprProjection` carrying the computed type; the
  ADR-0007 direction is that arithmetic over a projection is now
  supported, not rejected. `TestSentinelReachability` is updated: the
  canonical sentinel list drops `ErrUnsupportedProjection`, matching the
  reality that no fail-site exercises it any longer. This is the
  disposition the brief demanded be spelled out.

  This is the second sentinel retirement (Stage 4 already dropped
  `WITH`/`UNION` from `ErrUnsupportedClause`, but the sentinel survived
  via its write-clause + `UNWIND` + `CALL` covers). Here the
  disposition is different: `ErrUnsupportedProjection` was carried
  entirely by the projection residue, and Stage 6's whole point is that
  the residue no longer exists — so the sentinel has nothing left to
  guard and is deleted, not merely trimmed. The remaining sentinel set is
  five: `ErrUnsupportedClause`, `ErrUnsupportedPattern`,
  `ErrUnsupportedParameter`, `ErrUnboundVariable`, `ErrVariableKindConflict`.

- **`ErrUnsupportedParameter` retreats accordingly.** With rich expressions
  accepted at projection position, a parameter appearing inside such an
  expression is no longer implicitly a `ErrUnsupportedProjection` (via the
  outer shape) nor a `ErrUnsupportedParameter` (via `requireAllParametersApproved`
  on `WHERE`), because the projection classifier now approves any parameter
  it sweeps into a rich expression's `ExprUse`. The `WHERE` miner is
  extended in kind but restrained in scope: it still tries to pair
  `var.prop`-vs-`$p` as a `PropertyUse` (the honest, resolvable case); an
  unpaired `$p` inside a rich `WHERE` expression is now recorded as an
  `ExprUse` on the same `Use` sum. See §4.

- **Layer-1 corpus** — `readCoreDirs` (renamed conceptually to
  `expressionDirs` internally? — no, kept as `readCoreDirs` since Stage 6
  scenarios are read-only; the name refers to the *parser's* read/write
  split, not the *TCK's* directory taxonomy) gains the eleven expression
  dirs the brief names: `expressions/literals`, `expressions/boolean`,
  `expressions/comparison`, `expressions/mathematical`, `expressions/string`,
  `expressions/null`, `expressions/precedence`, `expressions/typeConversion`,
  `expressions/list`, `expressions/map`, `expressions/conditional`.
  Everything else in the TCK (`expressions/{aggregation, existentialSubqueries,
  graph, path, pattern, quantifier, temporal}`) is Stage 7–11 material and
  stays out of the suite until its stage lands. The unlock flips a large
  bloc of scenarios PENDING → PASSING (the ones whose only feature was a
  rich projection); scenarios that additionally use `UNWIND`, `WITH`
  patterns beyond Stage 4/5, or aggregation as an expression, stay
  PENDING via their surviving sentinels — that is the honest measure of
  where the parser stands post-Stage 6, and the ADR-0007 progress meter
  works exactly like the read-core meter did.

- **Skiplist** — a new group, "expressions value/semantics below the
  boundary," absorbs the bucket-3 negatives from the new dirs: runtime
  arithmetic errors (division by zero, integer overflow), type-coercion
  failures (`toBoolean(1.0)`), and result-value mismatches on expression
  scenarios that assert engine-side behaviour rather than a parser
  contract (ADR 0005, ADR 0007 bucket 3). Each entry pinned to its
  actual cause. The existing skiplist group titled "projection
  value/semantics below the type-interface boundary (Stage 3)" is
  reviewed and any entry now covered by the wider Stage-6 acceptance is
  moved to the Stage-6 group with an updated commentary; the entry
  covering "RETURN * without variables in scope" stays where it is
  (that scenario has nothing to do with expressions).

- **Layer-2 pins** — the two authored `mustReject` cases for
  `ErrUnsupportedProjection` become `mustParse` cases (they were the
  simplest arithmetic-in-RETURN queries; now they parse to an
  `ExprProjection`). New `mustParse` cases cover: (a) a bare integer-add
  arithmetic (`RETURN 1 + 2 AS x` — result type INT); (b) an IS NULL
  predicate (`RETURN n.x IS NULL AS b` — result type BOOL); (c) a list
  literal (`RETURN [1, 2, 3] AS xs` — result type LIST<INT>); (d) a
  CASE (`RETURN CASE n WHEN 1 THEN 'a' ELSE 'b' END AS s` — result type
  STRING); (e) a scalar literal typed correctly (`RETURN 1 AS x` — the
  existing `LiteralProjection` case, now with `Type() = TypeInt`).
  No mustReject case is added — the Stage-6 sentinel set has no new
  member. `TestSentinelReachability` becomes strictly narrower.

- **Docs inline** — this spec; CONTEXT.md gets new `Type` / `Result type`
  entries and revised `Projection` and `Return item` entries reflecting
  the widened sum; ADR 0003's amendment notes gain a Stage-6 line
  ("the curated subset now includes a closed `Type` sum for
  projection result types, plus an `ExprProjection` variant carrying
  result type + touched refs — the expression tree itself is still
  outside the model").

Nothing downstream of the parser is built (no resolver, no codegen) —
ADR 0004. Grouping-key semantics across `WITH`, cross-clause NULL flow
typing, and the resolver's use of the `Type` sum are all resolver work
and out of scope.

---

## 2. Why one atomic cycle

Adding the `Type` sum, the `ExprProjection` variant, and the rich-expression
classifier is one restructure of `query.Projection` — a variant addition,
a per-variant `Type()` accessor, and a matching wire shape. Splitting the
sum change from the parser change would leave `Type` unused (dead code) on
one branch, and splitting the parser change from the skiplist / dir wiring
would leave the acceptance suite in a mid-migration state where the newly
accepted scenarios have no goldens. Neither split lands independently on
`master` (Stage 4 §1's argument in miniature), so Stage 6 lands as one
branch.

Within the branch, the commit inventory (§8) still separates spec from
model from unlock from acceptance-widening, so review can proceed
incrementally — the constraint is landing-solo, not commit-of-one.

---

## 3. Type sum — what's in and what's out

The `Type` sum is the freeze-locked type vocabulary the resolver reads.
It is designed to be **incremental**: Stage 6 lands its scalar and
collection base; Stage 7 adds temporal variants; Stage 8 adds `PATH`. No
Stage-6 decision commits any variant that Stage 7/8 would need to widen or
restructure.

**In (Stage 6):**

- `TypeBool` — a boolean literal, an `IS NULL` predicate, a comparison, a
  string-comparison predicate (`STARTS WITH`, etc.), a boolean operator.
- `TypeInt` — an integer literal, arithmetic over integer-typed operands,
  a `size(list)` call would notionally return int (but function identity
  stays below the boundary, so any function call returns `TypeUnknown` —
  see below).
- `TypeFloat` — a float literal, arithmetic when at least one operand is
  float (integer + float = float; a division `/` between integers stays
  int in the model — Cypher's coercion rules on `/` are dialect-specific,
  and the parser's job is not to decide semantics per ADR 0005; both
  arms are still valid `INT` at the type-interface level).
- `TypeString` — a string literal, `STARTS WITH` / `ENDS WITH` / `CONTAINS`
  are `TypeBool` (they are predicates), string concatenation via `+`
  where both operands are `TypeString` is `TypeString`.
- `TypeNull` — the `null` literal. `NULL` participating in arithmetic /
  comparison / string predicates produces `TypeUnknown` in the model
  (the parser refuses to compute NULL's propagation through operators —
  that is engine semantics, ADR 0005; a query author sees the correct
  result at runtime).
- `TypeList` (parameterised by an element `Type`) — a list literal (element
  type is the common type of the elements if uniform, or `TypeUnknown`
  if mixed or empty), a list slice (list stays typed), a list
  concatenation (elements from both sides — `TypeUnknown` element if
  the two lists' element types differ).
- `TypeMap` — a map literal. No parameterisation over key/value types;
  openCypher maps are heterogeneous in value type (`{a: 1, b: 'x'}` is
  legal), so a fully typed map would immediately need per-key typing,
  which reintroduces the expression tree the model curates out
  (ADR 0003). `TypeMap` carries the shape "this is a map"; the resolver
  types keys post-freeze if it needs to.
- `TypeNode` — a whole-entity `RefProjection` whose `Ref` names a node
  binding. Not directly constructible from an expression literal (no
  node-literal syntax exists); reached only via `RefProjection.Type()`.
- `TypeEdge` — a whole-entity `RefProjection` whose `Ref` names an edge
  binding. Same construction posture as `TypeNode`.
- `TypeUnknown` — a distinguished type standing for "the parser cannot
  compute this schema-free." Covers: property lookups (`p.name` — its
  type comes from the schema, ADR 0003), function results (function
  identity below the boundary, ADR 0005), aggregate results (aggregate
  return type depends on argument type, likewise below the boundary),
  arithmetic involving `TypeNull` or `TypeUnknown` (unknown propagates),
  and any expression whose result type the parser is not willing to
  commit to at this stage. `TypeUnknown` is the parser's honest posture
  and precisely mirrors ADR 0003's ADR-0005-consistent "record the
  structural fact, refuse to commit resolution policy" line.

**Out (deferred to Stage 7):** `TypeDate`, `TypeTime`, `TypeDateTime`,
`TypeLocalTime`, `TypeLocalDateTime`, `TypeDuration`. A temporal literal
(`date('2020-01-01')`) is a function call today (`OC_FunctionInvocation`
with the name `date`), so at Stage 6 it types as `TypeUnknown` — the
same treatment every other function call gets. Stage 7 replaces the
`TypeUnknown` result with a temporal type by adding the six variants
and a small name-based lookup for the temporal constructor set (the
same posture Stage 3 took for aggregate functions).

**Out (deferred to Stage 8):** `TypePath`. Named paths (`p = (a)-[r]->(b)`)
are Stage 8's scope; without a named-path binding kind the model has no
way to reach `TypePath` in the first place. A `nodes(p)` call at Stage 6
types as `TypeUnknown` (function identity below the boundary), and Stage 8
does not need to widen `Type` to unblock — it adds `TypePath` when it
adds the binding kind.

**Deliberately absent: `TypeAny`.** OpenCypher's `ANY?` (an untyped-yet)
maps naturally to `TypeUnknown` in the parser's vocabulary. Adding both
would be a distinction without a difference at the parser boundary; the
resolver may introduce `TypeAny` in its own vocabulary if it needs to
distinguish "we cannot type this" from "the schema does not commit to a
type" — that is a resolver concern per ADR 0003.

**Rationale for a Type parameter on `TypeList` only.** Two arguments
against parameterisation, and one against non-parameterisation, all
weighed:

- Against parameterisation: it enlarges the sum's structural surface
  and, in principle, could bloat further (a nested `LIST<LIST<...>>`
  tower). Rejected: the tower is bounded by the query, not by the model
  — a query with a five-deep nested list has a five-deep nested
  `TypeList`, and the model records what the query said. No new
  invariant is introduced.
- Against parameterisation: it commits the parser to computing an
  element type it cannot always compute (mixed-element list). Rejected:
  a mixed list types as `TypeList(TypeUnknown)`, honest and correct —
  the resolver widens or complains post-freeze. This is not a promise
  the parser cannot keep; it is a "I cannot tell" the parser records
  once and consistently.
- Against non-parameterisation: it would collapse `LIST<INT>` and
  `LIST<STRING>` into a single `TypeList`, losing the one piece of
  information that distinguishes a numeric-array projection from a
  string-array projection at codegen time. The whole point of the
  Stage-6 model is that codegen can emit a typed method signature from
  the projection column — dropping the element type defeats that.
  Parameterisation wins.

`TypeMap` is intentionally *not* parameterised (see §3's `TypeMap`
paragraph). If a dialect later gains typed maps the freeze ADR can
revisit.

---

## 4. Parameters inside a rich expression

Stage 6 widens the projection surface, and rich expressions may contain
`$param` occurrences the parser cannot resolve to a single property
`Ref` (e.g. `RETURN a.n + $delta`). Two treatments were considered:

- **Reject any `$param` inside a rich projection expression.** Rejected:
  it would leave the sentinel `ErrUnsupportedParameter` as the *actual*
  reason a large fraction of Stage-6 scenarios still reject, defeating
  the "typed projection" outcome. ADR 0007's stated direction is that
  parameters outside the current binding-property + clause-slot set
  become typed by the expression they participate in.

- **Extend the `Use` sum with an `ExprUse` variant.** Chosen. `ExprUse`
  records that the parameter appears inside an expression whose result
  type the model carries; the parameter's own type is inferred from the
  expression it participates in (the resolver's job, post-freeze). At
  the parser boundary, `ExprUse` carries just the `Type` of the enclosing
  projection column and a discriminator for the position (a projection
  vs a WHERE expression). This keeps the closed-sum discipline of `Use`
  intact and lets the resolver see every parameter occurrence with the
  minimum context it needs.

`ExprUse` on the `Use` sum:

```
Use = PropertyUse | ClauseSlotUse | ExprUse
ExprUse = { enclosingType Type; position ExprPosition }
ExprPosition = ExprInProjection | ExprInPredicate    (int-backed, stringer)
```

The two existing `Use` variants stay unchanged. The `Use` sum's marshal
convention (a `kind` discriminator) extends to `expr` in the same wire
shape.

The parameter miner is layered accordingly: (i) the existing
`WHERE`-comparison pair miner runs first and records `PropertyUse` on
resolvable `var.prop = $p` pairs; (ii) any residual `$p` occurrence is
mined as an `ExprUse{ enclosingType, ExprInPredicate }` for a `WHERE`
occurrence, or `ExprUse{ enclosingType, ExprInProjection }` for a
projection occurrence. `ClauseSlotUse` continues to fire only from
`SKIP`/`LIMIT` slots. **No parameter is silently dropped.**

`ExprPosition` is a two-value int-backed enum. It could grow later
(`ExprInOrderBy`, `ExprInPatternPredicate`, …) as ADR-0007's expansion
proceeds; each addition is a variant append, following the discipline
`AggregateFunc` and `ClauseSlot` established.

---

## 5. Wire format (JSON shapes)

`Type` marshals as a JSON string via a stringer (`String() string`), the
single source the wire value follows so drift is impossible — the same
posture `AggregateFunc` and `UnionKind` take:

```
TypeBool             → "bool"
TypeInt              → "int"
TypeFloat            → "float"
TypeString           → "string"
TypeNull             → "null"
TypeList(elem)       → "list<" + elem.String() + ">"      (e.g. "list<int>", "list<unknown>")
TypeMap              → "map"
TypeNode             → "node"
TypeEdge             → "edge"
TypeUnknown          → "unknown"
```

`Projection` marshals with a new key `"type"` on **every** variant, always
emitted, matching the always-emit convention:

```
RefProjection        → {"kind": "ref", "variable": "...", "property": "...", "type": "node"|"edge"|"unknown"}
LiteralProjection    → {"kind": "literal", "type": "bool"|"int"|"float"|"string"|"null"|"list<...>"|"map"}
FuncProjection       → {"kind": "func", "refs": [...], "type": "unknown"}
AggregateProjection  → {"kind": "aggregate", "func": "count", "refs": [...], "type": "unknown"}
ExprProjection       → {"kind": "expr", "refs": [...], "type": "..."}
```

`ExprProjection`'s wire shape mirrors `FuncProjection`'s: refs + type,
no expression tree.

`Use` gains the `ExprUse` variant on the same convention:

```
ExprUse              → {"kind": "expr", "enclosingType": "int", "position": "projection"|"predicate"}
```

`ExprPosition` marshals as a lowercase stringer value.

This moves the projection shape (every existing golden gains
`"type": "unknown"` on `RefProjection` with a property, or `"type":
"node"` on a whole-entity node ref, etc.) so **every existing golden
regenerates on the model commit**. The unlock commit adds new goldens for
newly-passing scenarios. The scale (~145 existing goldens; ~50–100 new
goldens on unlock) is comparable to the Stage 3 golden regeneration and
uses the same `-update` flow.

Golden regeneration is a mechanical, `-update`-mediated edit; the
regressibility guard is `TestMustParse`, which hand-authors the
projection shape and does not follow `-update`.

---

## 6. Test corpus and skiplist

**`readCoreDirs` gains the eleven expression dirs the brief names.** The
audit for each:

- `expressions/literals` — canonical scalar-literal projections. Flip
  bloc: PENDING → PASSING for every WITH-free scalar-return scenario;
  `Literals7 { lists }`, `Literals8 { maps }` also flip if they are
  WITH-free.
- `expressions/boolean` — boolean operator combinations at RETURN
  position. Flips PENDING → PASSING for every WITH-free scenario.
  UNWIND-heavy scenarios stay PENDING via `ErrUnsupportedClause`.
- `expressions/comparison` — comparisons in RETURN/WHERE. Most scenarios
  use `UNWIND` (comparisons over a range) and stay PENDING via
  `ErrUnsupportedClause`; the ones that do not flip. Comparison
  chaining is a WHERE / projection shape and flips on this stage.
- `expressions/mathematical` — arithmetic. The addition/subtraction/
  precedence scenarios flip PENDING → PASSING. Function calls
  (`abs`, `sqrt`, …) already parsed as `FuncProjection`; their new
  `Type()` is `TypeUnknown` — that's honest and unchanged.
- `expressions/string` — string operators + string function calls.
  Function calls stay `FuncProjection` with `TypeUnknown`; concatenation
  and predicates flip PENDING → PASSING.
- `expressions/null` — `IS NULL` / `IS NOT NULL`. Flips PENDING →
  PASSING for every scenario that projects a bare `IS NULL` predicate.
- `expressions/precedence` — operator precedence tests. Every scenario
  is a `RETURN <arithmetic>` shape; all flip PENDING → PASSING.
- `expressions/typeConversion` — `toInteger` / `toFloat` / `toString`
  / `toBoolean`. Function calls whose result the parser cannot type
  (`FuncProjection` with `TypeUnknown`). Some flip PENDING → PASSING;
  the runtime-type-error scenarios (bucket 3, ADR 0007) join the new
  skiplist group.
- `expressions/list` — list literals, indexing, slicing. Flips for
  the scenarios that do not require UNWIND (many require it).
- `expressions/map` — map literals + property access. Flips for the
  scenarios that project a map or a `map.key` value at RETURN. UNWIND
  scenarios stay PENDING.
- `expressions/conditional` — `CASE`. Flips for the scenarios that
  project a `CASE` expression.

Every runtime-error / result-value scenario in these dirs joins the new
skiplist group, "expressions value/semantics below the boundary."
`TestSkiplistOrphans` guards every entry; `TestNoUndefinedSteps` stays
green (no new step phrasing is introduced by the new dirs beyond the
existing set the read-core suite already handles).

The exact scenario titles and the flip counts are pinned on the unlock
commit's `-update` run and recorded in the bead — the Stage 3 §6 /
Stage 5 §6 discipline.

### Layer-2 rule

Unchanged directional discipline (Stage 1 §6). Stage 6 adds five verbatim
`mustParse` cases for the canonical Stage-6 shapes (§1) and **removes** the
two authored `mustReject` cases pinned to `ErrUnsupportedProjection` (their
queries now parse to `ExprProjection`). Because
`ErrUnsupportedProjection` is retired entirely, `TestSentinelReachability`
now checks a five-sentinel canonical set; no authored replacement is
needed for the deleted sentinel.

---

## 7. Definition of done for Stage 6

1. `stage-6-expressions` lands green and independently mergeable;
   `master` is green if it lands solo.
2. `just test` green: query-package unit tests (the new `Type` sum, the
   `ExprProjection` variant, per-variant `Type()` accessor, `ExprUse`
   variant, JSON shapes, and property tests) and the cypher-package
   acceptance / pin / orphan / reachability suites.
3. Layer-1 godog count rises by the flip bloc (passed up, pending down;
   exact deltas pinned by the unlock commit's `-update` run and recorded
   in the bead). The stage's success metric is not "all 391 new
   scenarios parse" — many depend on UNWIND / temporal / aggregation-in-
   expression, out of Stage 6's scope — but "every scenario whose only
   Stage-6 blocker was projection richness or a result type now flips
   PASSING or is skiplisted with a bucket-3 rationale."
4. Documentation: this spec; CONTEXT.md `Type` / `Result type` entries +
   revised `Projection` / `Return item`; ADR 0003 note.
5. Beads: `gqlc-97z` closed; the resolver-side "type inference over the
   frozen `Type` sum" follow-up filed or confirmed to cover Stage-6
   work (a resolver-side bead already exists for Stage-4 grouping;
   Stage 6's follow-up mirrors that shape).

---

## 8. Commit inventory (single branch `stage-6-expressions`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec; CONTEXT.md Type/Result type entries; ADR 0003 note |
| model (red) | Failing unit tests for the `Type` sum, `Projection.Type()` accessors, `ExprProjection` constructor, `ExprUse` variant, and JSON shapes |
| model (green) | `Type` sum + `TypeList`/`TypeMap` constructors, per-variant `Type()` accessors, `ExprProjection`, `ExprUse` + JSON tags + all existing goldens regenerated to include the always-emitted `"type"` key |
| parser (red) | Failing tests exercising the new `mustParse` cases for arithmetic / IS NULL / list literal / CASE / typed scalar literal; the current `mustReject` cases for those shapes are moved into the `mustParse` map |
| parser (green) | `classifyProjection` widened; the residual-projection fall-through now builds an `ExprProjection` with computed type + mined refs; ref-mining sweep runs over the whole scalar-expression tree |
| unlock (dirs + skiplist + goldens) | `readCoreDirs` gains the eleven expression dirs; new skiplist group; new goldens for the flip bloc; `ErrUnsupportedProjection` deleted from `errors.go` / `allSentinels` / `unsupportedSentinels`; docstring for the surviving sentinels re-read; `TestSentinelReachability` updated |

Each commit is green in isolation of the ones after it — the model
commits leave `Type()` returning zero values on the four existing
Projection variants until the parser commits populate them; the parser
commits leave the new corpus dirs unwired until the unlock commit.

---

## 9. Weakest point (recorded honestly per ADR 0004)

The riskiest piece is **`TypeUnknown` as a load-bearing type**. A large
fraction of Stage-6 projections type as `TypeUnknown` — every property
lookup, every function call, every aggregate, every arithmetic
expression touching a property, every list mixing types, every
NULL-participating expression. On the wire this shows up as many
`"type": "unknown"` values, and the freeze locks that vocabulary.

The honest question: does the resolver have enough information from
`TypeUnknown` + touched-refs to produce a typed codegen output? For
`RETURN p.name` the resolver looks up `p`'s label set in the schema
and reads the `name` property's type — the parser hands off refs and
`TypeUnknown`, the resolver fills in a concrete type. That works. For
`RETURN 1 + p.age`, the resolver sees `TypeInt` on the left arm of the
addition and `TypeUnknown` on the right (the property lookup), and it
has to be prepared to reconstruct the addition's result type from
scratch — which requires walking the expression tree the model
deliberately does not carry.

Two mitigations, in decreasing order of confidence:

- **The parser hands off the original text (ADR 0005) and the result
  type it computed.** The resolver's job on an `ExprProjection` is to
  read the recorded `Type` and treat it as the column type; it does
  not need to re-derive the type from scratch. `TypeUnknown` at
  `RETURN 1 + p.age` is the parser conceding "I could not compute
  this" — which the resolver upgrades by reading the schema for `p.age`
  and re-computing the arithmetic result type. That upgrade is *the
  resolver's job*, and it needs the parsed expression text or an
  expression-tree fragment. The former (per ADR 0005) is what generated
  code executes anyway; recovering the type at generation time is the
  same textual re-analysis the runtime performs, one step earlier.
  This is the intended posture and works with the current model.

- **If the resolver proves it needs an expression tree**, Stage 6's
  `ExprProjection` can grow one field (a tree fragment) without any
  restructure — the closed-sum discipline is preserved because
  `ExprProjection` stays one variant. That is the same cheap widening
  ADR 0004 admits: the model is unlocked, no consumer is attached, and
  a per-variant field addition is a golden regeneration plus a
  constructor parameter, no sum-shape change.

The lesser risks, recorded for completeness:

- **`TypeMap` unparameterised** — noted in §3. If dialects gain typed
  maps, revisit at freeze.
- **NULL propagation** — the parser refuses to compute it; every
  NULL-participating expression types as `TypeUnknown`. Might be
  worth a nullability bit on `Type` at freeze if the resolver proves
  it consistently over-conservatively rejects. Cheap widening.
- **`ExprUse` and grouping semantics** — a projection-position
  parameter and a WHERE-position parameter both type from their
  enclosing expression, but the resolver's job to unify them is
  materially harder for `ExprInProjection` (grouping interacts with
  aggregates). This is a resolver risk, not a parser one, but it is
  the reason `ExprPosition` distinguishes the two — so the resolver
  can tell which parameter uses are which.

The discipline that keeps all this safe is the same as every prior
stage: refuse to model the expression tree, record the structural
fact (the result type + touched refs), and let the resolver upgrade
`TypeUnknown` from the schema.
