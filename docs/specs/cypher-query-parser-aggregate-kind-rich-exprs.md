# Aggregate-kind preservation on rich arguments — Cypher query parser

The implementation brief for the pre-freeze fix that stops the parser
from silently dropping the `AggregateFunc` kind when an aggregate call
carries a non-bare argument. One of the last two pre-freeze `query.Query`
model fixes before the ADR 0004 API freeze, under the curation
discipline of ADR 0003 and the type-interface boundary of ADR 0005.
Not a numbered stage: closes the "non-bare-aggregate-argument" gap
Stage 10 §8 recorded but did not close.

`MATCH (n) RETURN sum(n.age + 1)` currently lowers to the same
`query.Query` shape as any other rich expression of the same result
type: the outer aggregate name never enters the model. `classifyProjection`
in `expr.go` delegates to `classifyFunction`, which calls `functionArgRefs`;
`functionArgRefs` insists on bare var/var.prop or scalar-literal
arguments (Stage 6 §4 discipline), so an arithmetic operand makes
`ok=false`, `classifyProjection` returns `(nil,false)`, and
`collectReturnItem` falls through to `classifyRichExpression` —
producing an `ExprProjection` with the honest Stage-10 result type
(`aggregateResultType` runs inside `typeAtom` for the same call, spec
§1.3) but no `AggregateFunc` signal on the wire.

The Stage-10 rationale for modelling the aggregate kind applies
verbatim, one turn down the tree: `sum(x + 1)` collapses rows exactly
the way `sum(x)` does — a cardinality-bearing distinction the model
already keeps for every bare-argument aggregate call (§4 of ADR 0003).
Two observably-different queries, `sum(x + 1)` and `x + 1`, currently
lower to the *same* `ExprProjection{refs:[x], type:int}` — the same
"observably-different queries must not lower to indistinguishable
models" hazard §1.1 of the Stage-10 spec warns against, and Return6
scenario [16] is a live TCK-corpus data point: `sum(...)` over a
seven-term arithmetic expression lands as `ExprProjection{refs:[…],
type:unknown}` in `Return6_9d684013a026.golden.json` today.

This document is a **delta** against Stages 0–15 and the two pre-freeze
axes ([[cypher-query-parser-part-distinct-axis]], `gqlc-33k.2`) landing
in parallel; everything not stated here carries over verbatim. Sections
appear here only where this change adds something.

Tracking: bead `gqlc-33k.1` (GitHub #78). Blocks the `query.Query`
freeze (gqlc-cta) alongside [[cypher-query-parser-part-distinct-axis]].

---

## 1. Deliverables

### 1.1 `classifyProjection` recognises rich-argument aggregates

The current `classifyProjection` (expr.go:223) dispatches on the bare
atom's shape; its `OC_FunctionInvocation` arm calls `classifyFunction`,
which itself requires every argument to be a bare `var/var.prop` or a
scalar literal via `functionArgRefs` before an `AggregateProjection`
can be built (§4/§9 of Stage 6). When the argument is anything richer
— arithmetic (`sum(x + 1)`, `count(n.age + 1)`), a boolean composite
(`collect(a OR b)`), a list literal (`min([1, 2, 3])`), a nested
function call (`sum(range(1, 3))`), a chained comparison, a CASE, a
parenthesised composite, or a further aggregate (`count(count(*))`)
— `functionArgRefs` returns `ok=false`, `classifyFunction` returns
`(nil, false)`, `classifyProjection` returns `(nil, false)`, and the
call falls through to `classifyRichExpression`. The aggregate name
never reaches the model.

The fix: extend `classifyFunction` to handle a rich-argument aggregate
by mining refs through the Stage-6 rich-expression walker
(`typeExpression`) rather than the strict `functionArgRefs`, and
lowering the call as an `AggregateProjection` when the name matches
the closed aggregate set. Every other function-name path — namespaced,
non-aggregate bare, or aggregate-with-nested-aggregate (§1.3) — keeps
its current behaviour bit-for-bit.

The behavioural change in one line: an aggregate call whose argument
is non-bare no longer type-erases through `ExprProjection`; it lowers
to `AggregateProjection{fn, mined-refs, distinct, aggregateResultType(fn,
operand)}`, matching bit-for-bit the shape a bare-argument aggregate
lands as today for the same operand type.

### 1.2 Current-vs-target lowering table

Every shape below is exercised by an existing TCK scenario (§4.4
enumerates the goldens); every current row is the parser's observed
lowering today.

| Source                          | Current lowering                                          | Target lowering                                                                          |
|---------------------------------|-----------------------------------------------------------|------------------------------------------------------------------------------------------|
| `sum(x + 1)` (x:int — from UNWIND/WITH-alias binding, not exercised in the current corpus) | `ExprProjection{refs:[x], type:int}` | `AggregateProjection{fn:AggSum, refs:[x], distinct:false, type:int}` |
| `count(n.age + 1)`              | `ExprProjection{refs:[n.age], type:int}`                  | `AggregateProjection{fn:AggCount, refs:[n.age], distinct:false, type:int}`               |
| `collect(a OR b)` (a,b bindings)| `ExprProjection{refs:[a,b], type:list<bool>}`             | `AggregateProjection{fn:AggCollect, refs:[a,b], distinct:false, type:list<bool>}`        |
| `min([1, 2, 3])`                | `ExprProjection{refs:null, type:unknown}`                 | `AggregateProjection{fn:AggMin, refs:null, distinct:false, type:unknown}`                |
| `sum(range(1, 3))`              | `ExprProjection{refs:null, type:unknown}`                 | `AggregateProjection{fn:AggSum, refs:null, distinct:false, type:unknown}`                |
| `sum(DISTINCT x + 1)` (x:int)   | `ExprProjection{refs:[x], type:int}`                      | `AggregateProjection{fn:AggSum, refs:[x], distinct:true, type:int}`                      |
| `sum((1 - abs(r1.times/H1 - r2.times/H2)) * ...)` (Return6[16]) | `ExprProjection{refs:[r1.times, H1, r2.times, H2, …], type:unknown}` | `AggregateProjection{fn:AggSum, refs:[r1.times, H1, r2.times, H2, …], distinct:false, type:unknown}` |

**Every row's `type` column is preserved verbatim** across the change —
`aggregateResultType(fn, operand)` is the same table Stage 10 already
uses for bare-argument aggregates, and Stage 6's `typeAtom` already runs
the same table on aggregates inside a rich expression (§1.3 below).
The result type is not what this change fixes; the aggregate *kind* is.

### 1.3 Nested case — an aggregate *inside* a rich expression

The reciprocal shape — an aggregate that is not itself the whole
projection but appears inside a rich expression (`count(n) + 1`,
`1 + sum(x)`, `2 * avg(n.age)`, `[count(n), 3]`, `CASE WHEN count(n) > 3
THEN 'a' ELSE 'b' END`) — is **documented deferral, not a new axis**.
The whole projection stays `ExprProjection{refs, type}`; the aggregate
sub-expression's kind does not enter the model at that node.

Position and rationale:

- **The Type axis already distinguishes many inner-aggregate cases,
  and where it does not the residual is bounded.** Stage-6 `typeAtom`'s
  aggregate arm (typing.go:358 for named aggregates; typing.go:346 for
  the `count(*)` star-atom) already runs `aggregateResultType` when it
  sees an aggregate invocation inside a rich expression. So
  `count(n) + 1` types as `TypeInt` (via `promoteAdd(TypeInt, TypeInt)`)
  and lowers as `ExprProjection{[Ref{"n"}], TypeInt}`, while `n + 1`
  where `n` is a `TypeNode` binding types as `promoteAdd(TypeNode,
  TypeInt) = TypeUnknown` and lowers as `ExprProjection{[Ref{"n"}],
  TypeUnknown}` — the two are already distinguishable on `Type()`
  alone. This spec's residual is exactly the intersection where the
  operand's aggregate result type coincides with the same expression's
  non-aggregate result type — a strictly smaller set than "every
  aggregate inside a rich expression". Stage 10 §1.3 committed the
  typing table (this spec does not extend or repeat it); §7 records
  the residual honestly (a strict intersection, not the full class).

- **The resolver-side contract for the intersection residual is
  recorded on bead `gqlc-gyw`.** From `bd show gqlc-gyw` (notes,
  dated 2026-07-03): grouping-key computation must treat "does this
  `ExprProjection` contain an aggregate?" as a **resolver-side
  discovery over the projection's original text span** (e.g. reusing
  the cypher package's expression walker); the model will not answer
  it. If that re-parse proves impractical, the **sanctioned escape
  hatch is a post-freeze additive axis on `ExprProjection`
  (`ContainsAggregate bool`) via the ADR 0008 revision protocol** —
  and even then, it is not inferred from `Type` (because
  `count(n) + 1` and `x + 1` can both type `TypeUnknown` under
  operand types the current corpus doesn't press but future dialect
  extensions could). This spec therefore closes the door at the
  outermost projection node (the SF-3 fix) and, per the recorded
  contract on `gqlc-gyw`, does **not** open a `ContainsAggregate`
  axis pre-freeze. The escape hatch remains — additive, gated on the
  revision protocol, invoked only if the resolver's re-parse is
  impractical.

- **The kind belongs to the outermost projection node.** ADR 0003 §4
  is explicit: the model carries one cardinality-bearing distinction
  per projection node (`AggregateFunc` for `AggregateProjection`), not
  a sub-projection tree. Adding an inner-projection axis on
  `ExprProjection` pre-freeze would either (a) drag a partial
  expression tree into the model (violating the rule this axis is
  meant to protect) or (b) collapse to a boolean signal ("some
  aggregate lurks in here") that pre-empts the `gqlc-gyw` contract
  above and forecloses the re-parse path before it has been proved
  impractical.

- **The precedent is dense.** Stage 10 already accepts the *reciprocal*
  half of this asymmetry — the `AggregateFunc` kind belongs to the
  outermost `AggregateProjection` and does not surface for nested calls
  inside a bare `Refs()` list either. A bare `sum(collect(n))` would
  hit `functionArgRefs`'s "no nested calls" clause and fall through
  to `classifyRichExpression` today — under this change it lowers as
  `AggregateProjection{fn:AggSum, refs:null, distinct:false,
  type:unknown}`, with the inner `collect(n)` invisible on the wire.
  The outer name enters; the inner does not. That is the same posture
  this section commits to for inverted nesting.

- **openCypher itself rejects nested aggregation.**
  `RETURN count(count(*))` is `SyntaxError:NestedAggregation`
  (Return6.feature scenario [14]; test/data/query/cypher/tck/features/
  clauses/return/Return6.feature:257). The current parser accept-and-
  defers this to the engine — bucket 3 per ADR 0007, catalogued at
  acceptance_test.go:287 as `[14] Aggregates in aggregates`. Under
  this change, `RETURN count(count(*))` lowers as
  `AggregateProjection{fn:AggCount, refs:null, distinct:false,
  type:int}` (the outer `count`'s result is `TypeInt`; the inner
  `count(*)` is invisible), and the engine still raises
  `NestedAggregation` at compile time. The bucket-3 disposition is
  preserved verbatim — the parser doesn't own aggregate-position
  rules; the engine does. No skiplist churn. §4.7 pins this
  outcome at the classifier boundary.

- **The `Refs()` list still covers referential integrity.** Every
  `var/var.prop` inside a rich expression flows into `curPart.refs`
  via `typeAtom`'s variable arm (typing.go:322-326) regardless of
  where in the sub-tree it sits, so a nested `sum(x)` inside
  `1 + sum(x)` still records `x` for `build()`'s referential-integrity
  sweep. The rich-expression's `ExprProjection.Refs()` continues to
  carry these; the deferral leaves referential integrity intact.

The commitment: **the model records the outermost projection's
aggregate kind and nothing else**. An inner aggregate's kind is a
sub-projection fact the model does not encode, matching ADR 0003 §4
and the `gqlc-gyw` contract. The resolver's grouping-key computation
re-derives inner-aggregate presence from the projection list by
walking the original text (ADR 0005). The sanctioned escape hatch —
a post-freeze additive `ContainsAggregate` axis via the ADR 0008
revision protocol, never inferred from `Type` — remains available
if the re-parse proves impractical.

This closes the door explicitly: the change fixes the *outermost*-
aggregate-kind-dropped bug and does not open a new one for inner
aggregates. Silent dropping at the outer position (the SF-3 bug) is
fixed; deferral at the inner position (a different question, with a
different answer) is documented here as the model's committed posture,
matching what `bd show gqlc-gyw` records.

### 1.4 Refs-mining semantics for rich aggregate arguments

The bare-argument aggregate path calls `functionArgRefs` (shape.go:260),
which walks each argument as a `nonArithmeticAtom` and records a `Ref`
for every `var/var.prop` atom via `refFromNonArithmetic` (shape.go:29-48),
refusing (returning `ok=false`) the moment it sees a shape it cannot
classify without an expression tree. Its doc-comment enumerates the
rejection set explicitly: "arithmetic, nested call, list/map literal,
parameter, CASE, comprehension, '*'". A `$p` is therefore refused —
this is load-bearing for §7's parameter-mining note.

The rich-argument path this change adds cannot use `functionArgRefs`
by construction — every rich shape fails it — so it uses the same
walker Stage 6 uses for a rich expression body:
`typeExpressionMining(arg)` returns `(operand-type, refs, params)`,
where `refs` is the sequence of `Ref{Variable, Property}` values every
`var/var.prop` atom in the sub-tree touched, in **depth-first,
left-to-right traversal order; duplicates preserved**. The
aggregate's `Refs()` list is exactly that sequence.

**Bit-identity of refs on a bare argument, traced (Blocker 3(b)).**
The claim "for a bare `var/var.prop` argument the two paths mine
identical refs" is verifiable at three call sites:

- **Bare variable atom (`sum(x)`).** `functionArgRefs` calls
  `refFromNonArithmetic` (shape.go:29), which for zero property lookups
  returns `Ref{Variable: variable}` (shape.go:41-42). The walker's
  `typeAtom.OC_Variable()` arm (typing.go:322-326) appends
  `query.Ref{Variable: name}` to the local refs slice. Same shape:
  `Ref{Variable:"x", Property:""}`.

- **Bare property lookup (`sum(n.age)`).** `functionArgRefs` calls
  `refFromNonArithmetic` (shape.go:29), which for one property lookup
  returns `Ref{Variable: variable, Property: lookups[0].
  OC_PropertyKeyName().GetText()}` (shape.go:43-44). The walker's
  path is two-step: `typeAtom.OC_Variable()` appends
  `Ref{Variable: name}` (typing.go:322-326), then
  `typeNonArithmetic`'s single-lookup upgrade at typing.go:292-300
  rewrites the just-appended ref in-place to
  `Ref{Variable: (*refs)[preAtomRefLen].Variable, Property:
  lookups[0].OC_PropertyKeyName().GetText()}`. The `preAtomRefLen`
  bookmark ensures exactly the ref this call appended is upgraded,
  not an earlier one. Same shape: `Ref{Variable:"n", Property:"age"}`.

- **Scalar literal argument (`sum(1)`).** `functionArgRefs` (shape.go:271-274)
  approves a scalar literal with `continue` — no ref appended. The
  walker's `typeAtom.OC_Literal()` arm returns via
  `literalOrCollectionType`, which walks list/map inner refs but not
  scalar-literal refs (there are none). No ref appended by either
  path. Same: no ref.

The RED phase pins bit-identity with the `sum(n.age)` regression pin
(§4.5 pin #8): the projection's `Refs()` sequence pre- and post-
widening is `[Ref{Variable:"n", Property:"age"}]`, and the pin
carries a comment naming shape.go:29-48 and typing.go:292-300 as the
two sites that must agree.

**Path residual for the widening.** The rich-argument path is a
strict superset of the bare-argument path for refs mining: anything
`functionArgRefs` would accept, `typeExpressionMining`'s walker
mines identically (traced above). Only the aggregate-name branch of
`classifyFunction` takes the rich fallback; a non-aggregate function
with a rich argument (`abs(n.age + 1)`) still returns `(nil, false)`
and falls through to `classifyRichExpression`, because function
identity is below the type-interface boundary (ADR 0005 §5) and
`FuncProjection`'s existence is a pass-through the parser does not
extend.

Concretely, the mining rules for a rich aggregate argument:

- **A single positional argument.** `sum`, `count`, `collect`, `avg`,
  `min`, `max`, `stDev*` take one argument. `typeExpressionMining`
  walks that one and mines its refs and parameters.
- **Two arguments — percentileCont/percentileDisc.** The second
  argument is the percentile (a scalar), which the current
  bare-argument path types via `typeExpression(args[0])` for the
  operand type — matching what `classifyFunction` (expr.go:353) does
  today. Under the rich fallback, the walker walks *every* argument
  (both operand and percentile), mining refs and parameters from
  both; the operand's type still comes from `args[0]`. The percentile's
  refs are usually empty (it is a scalar literal), but a
  variable-parameterised percentile (`percentileCont(n.score, p)`
  where `p` is an in-scope binding) contributes its own refs. Not
  exercised by any TCK scenario in the current corpus; the mechanical
  rule falls out of the walker's uniform behaviour.
- **`count(*)` remains the degenerate case.** The star atom is handled
  by the `atom.COUNT()` arm of `classifyProjection` (expr.go:243) with
  its own no-refs contract. This change does not touch that arm.
- **Duplicates in the refs list are preserved.** Stage 6's mining
  does not dedupe: `sum(x + x)` produces `Refs()` of `[x, x]` in
  depth-first, left-to-right order, matching what `typeAtom` records
  for the same expression under `classifyRichExpression`. The
  bare-argument path's `functionArgRefs` also does not dedupe (each
  argument's ref is appended in argument order — a degenerate case of
  the same rule). Both paths agree on order and duplicates.
- **Parameter mining is inherited from the walker.** A `$p` inside a
  rich aggregate argument records the parameter as
  `ExprUse{aggregateResultType(fn, operand), ExprInProjection}` via
  `l.addParameterUse` — matching what `classifyRichExpression` would
  have registered if the aggregate had fallen through (see §7's
  parameter-mining note for the corrected account of current
  behaviour). The rich-aggregate path routes through
  `typeExpressionMining` so the walker collects the parameter nodes,
  and `classifyAggregateCall` iterates them and calls
  `l.addParameterUse(name, node, NewExprUse(t, ExprInProjection))`
  with `t = aggregateResultType(fn, operand)` — the aggregate call's
  own result type, the analogue of the "enclosing rich expression's
  type" `classifyRichExpression` uses at typing.go:867-874.

### 1.5 DISTINCT interaction

DISTINCT keeps its Stage-10 position on the `oC_FunctionInvocation`
grammar node (Cypher.g4 §426, `oC_FunctionInvocation: … (DISTINCT SP)?
oC_Expression …`). `fi.DISTINCT() != nil` is the ANTLR terminal
accessor; `classifyFunction` reads it today at expr.go:356. This
change reads it in the exact same place, before deciding which
`NewAggregateProjection` call to build.

`sum(DISTINCT x + 1)` lowers to `AggregateProjection{fn:AggSum,
refs:[x], distinct:true, type:int}`. `collect(DISTINCT a OR b)`
lowers to `AggregateProjection{fn:AggCollect, refs:[a,b], distinct:true,
type:list<bool>}`. Every combination of `{DISTINCT, rich-arg-shape}` is
mechanically covered by reading `fi.DISTINCT()` once and passing the
result to `NewAggregateProjection`.

The [[cypher-query-parser-part-distinct-axis]] invariant (§1.5 of
that spec) — the two DISTINCT grammar sites (`oC_FunctionInvocation`
vs. `oC_ProjectionBody`) are read independently — holds verbatim.
`sum(DISTINCT x + 1)` sets `AggregateProjection.Distinct=true` on
the return item, does not touch `Part.Distinct`; `RETURN DISTINCT
sum(x + 1)` sets `Part.Distinct=true`, does not touch the aggregate's
`Distinct`. Both true is a legal, reachable, distinct-model shape.

### 1.6 Wire format

No new JSON keys. `AggregateProjection.MarshalJSON` (query.go:1621)
already emits `{"kind":"aggregate", "func":…, "refs":…, "distinct":…,
"type":…}`; every field's serialisation is a Stage-10 commitment. The
change swaps which projection variant a rich-aggregate return item
marshals as: `{"kind":"expr", "refs":…, "type":…}` becomes
`{"kind":"aggregate", "func":…, "refs":…, "distinct":…, "type":…}`,
with `refs` and `type` preserved bit-for-bit.

The wire-shape delta per touched golden is exactly:

- `"kind"` changes value from `"expr"` to `"aggregate"`.
- Two keys appear (`"func"` and `"distinct"`) between `"kind"` and
  `"refs"`.

Every other field on the return item is unchanged. See §4.3 for the
purity check.

### 1.7 Golden-impact statement

The scope of golden change is bounded by exactly the TCK scenarios
whose RETURN or WITH projection carries an aggregate over a rich
argument. §4.4 enumerates the four categories the audit finds:

- **Return6[16]** — `sum(...)` over a seven-term arithmetic expression;
  one golden (`Return6_9d684013a026.golden.json`).
- **Return6[14]** — `count(count(*))`; one golden (nested-aggregation
  bucket-3 scenario). Under this change, the outer `count` lowers as
  `AggregateProjection{fn:AggCount, refs:null, distinct:false, type:int}`
  — the golden for this scenario, if present today, gains the same
  swap. If the scenario is skiplist-pending today (compile-time
  `NestedAggregation` reject), it stays pending; no golden change.
- **Return6[9]** — `{name: count(b)}` inside a map literal projection;
  one golden. `count(b)` is the whole *value* under a map key, not
  the whole projection — the projection itself is `{name: count(b)}`,
  a map-literal `ExprProjection`. Not affected by this change: the
  outer projection is not an aggregate call, and per §1.3 the model
  does not lift an inner aggregate through a map-literal wrapper.
- **Other TCK aggregate-over-rich shapes.** A scripted audit (§4.4)
  is the primary evidence for the count; the pre-RED audit will
  enumerate them individually. The upper bound is Return-scoped
  aggregate scenarios plus WITH-scoped analogues (WithOrderBy4,
  Aggregation1/2/3, Match-style aggregation-in-comprehensions).

The diff must be **shape-preserving on refs and type**: every touched
golden's `"refs"` and `"type"` values are unchanged bit-for-bit. The
only changes are the `"kind"` value flipping and the `"func"` +
`"distinct"` keys appearing. §4.3 formalises the check.

**Actual size (post-GREEN).** 75 goldens touched, across nine feature
families: Precedence1 ×59 (its scenarios [14]–[35] alias every
`collect(...)` projection as `AS eq` / `AS neq`), Return5 ×3, Return6
×3, Comparison1 ×3, Aggregation6 ×2 (percentileCont/percentileDisc),
WithOrderBy4 ×2, Pattern2 ×1, Remove1 ×1, ReturnOrderBy2 ×1. The
category — aggregate over rich argument — is exactly what the change
targets; the count is 3-4x the pre-RED estimate because §4.4's audit
script had a false-negative bug (fixed in that section). This is not
a broad rebaseline like [[cypher-query-parser-part-distinct-axis]]'s
field-addition-on-every-part; it is a targeted shape swap on exactly
the projection items that press a rich aggregate.

**Pre-RED estimate (retained for archaeology).** The pre-RED estimate
was "under 20 goldens". It undercounted because §4.4's audit grep
matched the return-item `"name"` field on disk, which carries the `AS
alias` when the projection is aliased. Precedence1's `WITH
collect((a OR b XOR c) = (a OR (b XOR c))) AS eq, collect(...) AS
neq` scenarios have `"name": "eq"` / `"name": "neq"` in every touched
golden — the aggregate name never appears where the audit looked.
§4.4 below now grep`s the TCK feature files directly for the aggregate
keyword followed by an open paren, so the next spec that reuses the
audit template does not inherit the bug.

### 1.8 Sentinel status

No new sentinel. `TestSentinelReachability` runs against the four-
sentinel set (`ErrUnsupportedClause`, `ErrUnsupportedParameter`,
`ErrUnboundVariable`, `ErrVariableKindConflict`) — none is touched.
This change is accept-and-record: every grammar-legal aggregate over
a rich argument surfaces on the model as an `AggregateProjection`.
No parse-time rejection is added or removed.

### 1.9 Corpus wiring

No new dir enters `readCoreDirs`. The 3897-scenario / 3459-passed /
438-pending / 0-failed / 16006-step baseline must hold verbatim
after the change: this fix reshapes an already-lowered projection,
it does not gate scenarios that previously deferred and it does not
un-defer scenarios previously in the skiplist. The scenario-level
accept/reject outcome is preserved for every entry in the corpus.

`acceptance_test.go`'s bucket-3 catalogue entry for `[14] Aggregates
in aggregates` (line 289) stays unchanged: the outer `count` lowers
as an `AggregateProjection` with `TypeInt`, and the engine still
raises `NestedAggregation` at compile time. The parser's disposition
is preserved bit-for-bit.

### 1.10 Docs inline

- This spec.
- ADR 0003's amendment notes gain a line: "an aggregate call whose
  argument is a rich expression (`sum(x + 1)`) lowers as
  `AggregateProjection` with `Refs()` mined through the Stage-6
  rich-expression walker; the outer aggregate's kind enters the model
  at the outermost projection node only, matching §4."
- Stage 10 §8's "aggregates with non-bare arguments drop their
  `AggregateFunc` kind" bullet is retired (closed by this change).
  The residual — an aggregate *inside* a rich expression is documented
  deferral, not silence — is captured in §1.3 here.
- CONTEXT.md's **Query part** entry needs no update; the projection
  variants are unchanged, only the classifier's dispatch widens.

Nothing downstream of the parser is built (no resolver, no codegen)
— ADR 0004. The resolver's grouping-key computation is bead
`gqlc-gyw`; this change strictly widens the signal it consumes at
the outer projection, and defers the intersection-residual (§7)
according to the contract recorded on `gqlc-gyw` (2026-07-03 notes):
resolver-side re-parse first; post-freeze additive `ContainsAggregate`
axis via the ADR 0008 revision protocol only if the re-parse proves
impractical; never inferred from `Type`.

### 1.11 What this change does not do

- Does not add any new projection variant (`AggregateProjection` and
  `ExprProjection` already exist).
- Does not change any type in `aggregateResultType` (same table Stage
  10 committed).
- Does not read DISTINCT from any new grammar site (still
  `fi.DISTINCT()` on `oC_FunctionInvocation`, unchanged from Stage 10).
- Does not add a `ContainsAggregate` (or any inner-projection) axis
  to `ExprProjection` pre-freeze. The sanctioned escape hatch remains
  post-freeze, additive, via the ADR 0008 revision protocol —
  recorded on bead `gqlc-gyw`, invoked only if the resolver-side
  re-parse proves impractical.
- Does not touch `classifyRichExpression`, `typeAtom`, or Stage 6's
  rich-expression walk (they still see the same subtree they see today
  when a rich-argument aggregate is inside a larger rich expression;
  the classifier only *precedes* those calls at the outer projection).
- Does not change Stage 6 §4's "no parameter is silently dropped"
  discipline: `classifyAggregateCall` routes through
  `typeExpressionMining` and registers `ExprUse{aggregateResultType(
  fn, operand), ExprInProjection}` for every parameter, matching
  what `classifyRichExpression` records today at fall-through
  (§4.1 code shape; §7 parameter-mining note).
- Does not touch the parser's WHERE / ORDER BY / SKIP / LIMIT
  aggregate-position handling (bucket 3 per ADR 0007; the engine
  raises).
- Does not change the `Refs()` mining discipline for nested aggregates
  under a rich argument (`sum((count(*) + 1))` — see §7).

---

## 2. Why one atomic cycle

Widening `classifyFunction`'s aggregate arm to route through the
Stage-6 walker for rich arguments, and rebaselining the small set
of goldens where the shape changes, is one restructure of the
classifier's outer-aggregate story. Splitting the classifier change
from the golden rebaseline would leave the acceptance suite in a
mid-migration state where every affected part's JSON disagrees with
the wire the model emits. The full change lands as one branch, atomic
against the two other pre-freeze axes.

Within the branch, the commit inventory (§6) separates spec from
RED from GREEN, matching the Stage-10 / Cycle-1 template.

---

## 3. Model shape

### 3.1 `AggregateProjection` unchanged

No field is added or removed. `NewAggregateProjection(fn AggregateFunc,
refs []Ref, distinct bool, t Type) AggregateProjection` is used exactly
as it exists today. The change is entirely in the parser's classifier
— which call site of `NewAggregateProjection` fires — not in the model.

### 3.2 `ExprProjection` unchanged

No field is added or removed. §1.3's deferral means `ExprProjection`
continues to be the honest shape for a rich expression that has an
aggregate somewhere inside it but isn't itself an aggregate call.
`Refs()` and `Type()` continue to be the two accessors.

### 3.3 `AggregateFunc` closed set unchanged

`AggCount`, `AggSum`, `AggCollect`, `AggMin`, `AggMax`, `AggAvg`,
`AggStdev`, `AggPercentile` — the eight enum values Stage 10 named.
`aggregateFunc` (shape.go:174) maps the same nine lowercased names
(`percentilecont` and `percentiledisc` collapse to `AggPercentile`;
`stdev` and `stdevp` collapse to `AggStdev`), unchanged.

### 3.4 `aggregateResultType` unchanged

Stage 10's per-aggregate table (shape.go:211) is the source of truth
for the aggregate's result type. `aggregateResultType(fn, operand)`
runs at two sites already (classifyFunction for bare-arg aggregates,
typeAtom for aggregates inside a rich expression); this change adds
a third caller — the same function, no rule change. The operand type
is `typeExpressionMining(args[0])`'s first return value, matching
what `typeAtom`'s aggregate arm does today at typing.go:358-364 and
what `classifyFunction`'s bare path does at expr.go:353-355 up to
the choice of walker entry point (Blocker 1: the widening upgrades
`typeExpression` to `typeExpressionMining` to preserve Stage 6 §4's
parameter-mining discipline).

---

## 4. Parser widening

### 4.1 `classifyFunction` gains a rich-argument aggregate arm

The current function (expr.go:336) does one thing: if
`functionArgRefs` accepts every argument, it either builds an
`AggregateProjection` (name matches) or a `FuncProjection` (name does
not); if `functionArgRefs` rejects any argument, it returns
`(nil, false)`.

The widening flips the order so the aggregate-name check runs
before the bare-argument check on the aggregate path:

```go
func (l *listener) classifyFunction(fi gen.IOC_FunctionInvocationContext) (query.Projection, bool) {
    name, hasName := functionName(fi)
    if hasName {
        if fn, ok := aggregateFunc(name); ok {
            return l.classifyAggregateCall(fi, fn), true
        }
    }
    refs, ok := functionArgRefs(fi)
    if !ok {
        return nil, false
    }
    for _, ref := range refs {
        l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
    }
    resultType := query.Type(query.TypeUnknown{})
    if t, ok := temporalConstructorType(fullFunctionName(fi)); ok {
        resultType = t
    }
    return query.NewFuncProjection(refs, resultType), true
}

func (l *listener) classifyAggregateCall(fi gen.IOC_FunctionInvocationContext, fn query.AggregateFunc) query.Projection {
    var operand query.Type
    var refs []query.Ref
    var params []antlr.Tree
    args := fi.AllOC_Expression()
    if len(args) > 0 {
        var opRefs []query.Ref
        var opParams []antlr.Tree
        operand, opRefs, opParams = l.typeExpressionMining(args[0])
        refs = append(refs, opRefs...)
        params = append(params, opParams...)
    }
    for _, arg := range args[1:] {
        _, more, moreParams := l.typeExpressionMining(arg)
        refs = append(refs, more...)
        params = append(params, moreParams...)
    }
    resultType := aggregateResultType(fn, operand)
    for _, p := range params {
        name := parameterName(p)
        if name == "" {
            continue
        }
        l.addParameterUse(name, p, query.NewExprUse(resultType, query.ExprInProjection))
    }
    distinct := fi.DISTINCT() != nil
    return query.NewAggregateProjection(fn, refs, distinct, resultType)
}
```

Structural notes on the code shape:

- **Refs flow onto `curPart.refs` via `typeExpressionMining`'s
  `typeAtom` arm.** The walker (typing.go:322-326) appends every
  `var/var.prop` atom to `l.curPart.refs` as it walks. So the
  aggregate call's refs land on the current part exactly the way a
  bare-argument aggregate's do today, via `functionArgRefs`'s loop at
  classifyFunction:341-343. The two paths converge on the same
  `curPart.refs` shape (bit-identity traced in §1.4).

- **Parameter mining routes through `typeExpressionMining` and
  registers `ExprUse`.** Correcting the previous spec draft's factual
  error: `functionArgRefs` (shape.go:260, doc-comment) rejects
  parameters, so `sum($p)` today falls through to
  `classifyRichExpression`, which registers
  `ExprUse{TypeUnknown, ExprInProjection}` for the parameter via
  `typeExpressionMining` (typing.go:867-874) — `TypeUnknown` because
  `aggregateResultType(AggSum, TypeUnknown) = TypeUnknown`;
  `count($p)` today records `ExprUse{TypeInt, ExprInProjection}`
  because `aggregateResultType(AggCount, _) = TypeInt`
  unconditionally. The Stage-6 discipline
  is explicit: "no parameter is silently dropped" (Stage 6 spec §4;
  `typeExpressionMining` doc-comment). This change preserves that
  discipline verbatim by routing through `typeExpressionMining`
  (not `typeExpression`) and calling
  `l.addParameterUse(name, node, NewExprUse(resultType,
  ExprInProjection))` for every parameter the walker touches. The
  `enclosingType` is the aggregate call's own result type
  (`aggregateResultType(fn, operand)`) — the analogue of the
  "enclosing rich expression's type" `classifyRichExpression` uses,
  and different from the individual operand's type (which is what
  seeds `aggregateResultType` and would be wrong for `count($p)`
  where operand is `TypeUnknown` but the aggregate result is
  `TypeInt`). The parameter mining fires exactly once per call site:
  `classifyAggregateCall` is the sole caller under this arm, and it
  drives the `Use` registration itself — the walker's `typeAtom`
  parameter arm (typing.go:329-339) only approves the node and
  collects it onto `l.exprParams`; `Use` registration is the caller's
  responsibility, matching `classifyRichExpression`'s posture.

- **`functionArgRefs`'s residual role.** After the widening,
  `functionArgRefs` is only consulted on the FuncProjection path
  (non-aggregate name). This preserves the "no expression tree, no
  nested calls" discipline for `FuncProjection` — function identity
  is below the boundary (ADR 0005), so widening `FuncProjection` to
  carry rich arguments is out of scope.

- **`classifyRichExpression` no longer runs on aggregate-named
  projection items.** After the widening, `collectReturnItem`'s
  fall-through path (expr.go:199-202) still fires for non-aggregate
  rich shapes (`n.age + 1`, `[1, 2, 3]`, `n IS NULL`, etc.). Only the
  outermost-aggregate case is diverted to `classifyAggregateCall`,
  and it does its own parameter-Use registration — no double-
  registration is possible because the walker's `l.exprParams` is
  drained by `typeExpressionMining` and the outer `collectReturnItem`
  path (which would otherwise call `classifyRichExpression`) never
  fires for that item.

### 4.2 `classifyProjection` unchanged

`classifyProjection` (expr.go:223) still dispatches on the bare
atom's shape and delegates to `classifyFunction` for the function-
invocation arm. Nothing changes at the classifier's outer layer;
the widening is one hop deeper.

`atom.COUNT() != nil` (count-star at expr.go:243) is a separate arm
that constructs `AggregateProjection{AggCount, nil, false, TypeInt}`
directly. It is not touched — count-star is not a "rich argument"
case (no argument at all), and the star atom doesn't carry a
`DISTINCT` alternative in the grammar.

### 4.3 Golden purity check

After `go test -update ./internal/query/cypher/...`, run:

```bash
git diff --stat internal/query/cypher/testdata/golden/ | tail
```

to confirm the touched-file count is bounded (§1.7: under 20 goldens).
Then a shape-preservation grep:

```bash
# Every changed non-cosmetic line in a modified golden must be either
# the "kind" value swap ("expr" -> "aggregate"), or one of the two
# added keys ("func" and "distinct"), or a re-indented existing key
# whose value is unchanged.
git diff internal/query/cypher/testdata/golden/ | \
    grep -E '^[-+]\s+"' | \
    grep -v -E '"kind":\s*"(expr|aggregate)"|"func":|"distinct":' | \
    grep -v -E '"refs":|"type":|"variable":|"property":' | tee /tmp/purity-check.txt
```

`/tmp/purity-check.txt` must be empty modulo trailing-comma / brace
punctuation. Any other output — a `"refs"` value drifting, a `"type"`
value drifting, or a stray key change — is a purity violation and
GREEN cannot land until it's zero.

A second, stronger check runs a scripted `refs` and `type` value
comparison between the master and worktree golden for each touched
file, asserting bit-identity on both. §1.7 commits to this
invariant; §5 elevates it to a hard precondition.

### 4.4 Pre-RED audit — the exact goldens

Before RED, run:

```bash
# Grep the TCK feature files (RETURN / WITH bodies) for an aggregate
# name followed by an open paren whose argument is anything other than
# a bare var/var.prop or scalar literal. This catches aliased
# projections (RETURN collect(a OR b) AS eq) that the golden's "name"
# field cannot — that field carries the AS alias when present, so
# grepping the on-disk goldens for `"name": "collect(..."` silently
# misses every aliased case (which turns out to be the majority).
grep -REn '^\s*(RETURN|WITH|\s+)\s+.*(count|sum|collect|min|max|avg|stdev|stdevp|percentilecont|percentiledisc)\s*\(' \
    test/data/query/cypher/tck/features/ \
    | grep -Ev '(count|sum|collect|min|max|avg|stdev|stdevp|percentilecont|percentiledisc)\s*\(\s*(DISTINCT\s+)?[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?\s*\)' \
    | grep -Ev '(count|sum|collect|min|max|avg|stdev|stdevp|percentilecont|percentiledisc)\s*\(\s*\*\s*\)' \
    | tee /tmp/rich-aggregate-lines.txt
wc -l /tmp/rich-aggregate-lines.txt
```

The first `grep` finds every RETURN/WITH line calling an aggregate;
the two `grep -v`s filter out the bare-arg cases (`count(n)`,
`sum(n.age)`, `count(*)`) that the pre-widening path already lowers
as `AggregateProjection`. What remains is the rich-argument set — the
lines that press the widening.

Then map each surviving line to its golden by scenario:

```bash
# For each line, note the feature file + line number; grep back to the
# nearest `Scenario:` header to name the scenario. The golden's hash
# suffix comes from goldenPath()'s deterministic input (the scenario
# query text), so `find internal/query/cypher/testdata/golden/ -name
# '<Feature>_*.golden.json'` bounds the feature's contribution.
```

**Confirmed families from the GREEN diff:**

- **Precedence1** ×59 goldens — scenarios [14]–[35] every write
  `WITH collect((<boolean composite>) = (<boolean composite>)) AS eq,
  collect((<boolean composite>) <> (<boolean composite>)) AS neq`.
  Two aliased `collect(...)` projections per scenario, each over a
  boolean comparison expression (OR / XOR / AND / NOT / NULL predicate
  / list predicate). The `AS eq` / `AS neq` alias is what made the
  original name-based audit miss these.
- **Aggregation6** ×2 — percentileCont / percentileDisc scenarios,
  each with a percentile call over a projection whose first argument
  is (per Aggregation6.feature) a bare property or a rich expression.
- **Comparison1** ×3, **Return5** ×3, **Return6** ×3 — mix of the
  Return6[16] `sum((1 - abs(r1.times/H1 - r2.times/H2)) * ...)`
  seven-term arithmetic already called out in §1.7, plus other
  aggregate-over-rich shapes.
- **WithOrderBy4** ×2, **Pattern2** ×1 (aggregate on a pattern
  comprehension), **Remove1** ×1, **ReturnOrderBy2** ×1 — trailing
  rebase surface.

Total: **75 goldens** — see §1.7 for the empirical breakdown that
matches this audit's output line-for-line.

The audit is deliberately loose (matches any aggregate name followed
by any non-bare argument shape); a false positive is a comment line
or a string-literal query fragment that mentions an aggregate but
does not exercise one. The RED phase's fine-grained pin
(`aggregate_sum_on_bare_property_arg_(regression)`, pin #10) plus the
GREEN rebaseline's purity check (§4.3) distinguish false positives:
a golden whose refs/type would drift under the widening is not a
rich-aggregate case.

### 4.5 RED pins

Ten parser pins in `internal/query/cypher/parser_test.go`. Each pin's
expected value asserts one shape category from §1.2, §1.3, or the
bare-arg regression / parameter-mining coverage §7 requires. The
`type` value on each pin is the value Stage 10's `aggregateResultType`
table actually returns for the operand type the parser will compute
schema-free (property lookups → `TypeUnknown`; scalar literals →
their concrete type; entity vars → `TypeNode`/`TypeEdge`; other
functions → `TypeUnknown` unless temporal-constructor).

The six rich-arg shape pins:

1. `aggregate count on arithmetic property arg` — `MATCH (n) RETURN
   count(n.age + 1)`. `n.age` types as `TypeUnknown` (property);
   `n.age + 1` types as `promoteAdd(TypeUnknown, TypeInt) = TypeUnknown`.
   `aggregateResultType(AggCount, TypeUnknown) = TypeInt` (count is
   unconditional). Expect
   `AggregateProjection{AggCount, [Ref{"n","age"}], false, TypeInt}`.
   Also asserts `Part.Distinct=false` (structural coverage of
   [[cypher-query-parser-part-distinct-axis]] §1.5 independence).

2. `aggregate sum on arithmetic property arg` — `MATCH (n) RETURN
   sum(n.age + 1)`. Same operand typing as #1; `aggregateResultType(
   AggSum, TypeUnknown) = TypeUnknown`. Expect
   `AggregateProjection{AggSum, [Ref{"n","age"}], false, TypeUnknown}`.

3. `aggregate collect on boolean composite` — `MATCH (a), (b) RETURN
   collect(a.name = b.name)`. `a.name` and `b.name` type `TypeUnknown`;
   the comparison types as `TypeBool`; `aggregateResultType(AggCollect,
   TypeBool) = list<bool>`. Expect
   `AggregateProjection{AggCollect, [Ref{"a","name"}, Ref{"b","name"}],
   false, list<bool>}`. Also asserts depth-first left-to-right refs
   order.

4. `aggregate min on list literal arg` — `RETURN min([1, 2, 3])`. The
   list-literal argument has `TypeList(TypeInt)` as its Stage-6 type;
   `aggregateResultType(AggMin, TypeList{...}) = TypeUnknown` (min
   over list is engine-inconsistent, Stage 10 §8). Expect
   `AggregateProjection{AggMin, nil, false, TypeUnknown}`.

5. `aggregate sum on nested function call` — `RETURN sum(range(1, 3))`.
   `range(1, 3)` types as `TypeUnknown` (function identity below the
   boundary); `aggregateResultType(AggSum, TypeUnknown) = TypeUnknown`.
   Expect `AggregateProjection{AggSum, nil, false, TypeUnknown}`.

6. `aggregate sum distinct on arithmetic arg` — `MATCH (n) RETURN
   sum(DISTINCT n.age + 1)`. Same shape as #2 with `distinct:true`.
   Expect `AggregateProjection{AggSum, [Ref{"n","age"}], true,
   TypeUnknown}`. Verifies §1.5's DISTINCT interaction and the
   independence from `Part.Distinct` ([[cypher-query-parser-part-distinct-axis]]
   §1.5).

The negative deferral pin (Blocker 2 lock):

7. `nested aggregate inside rich projection` — `MATCH (n) RETURN
   count(n) + 1`. The outer expression is not an aggregate call; per
   §1.3 the model does not lift an inner aggregate through a
   rich-expression wrapper. `typeAtom`'s aggregate arm
   (typing.go:358-366) types `count(n)` as `TypeInt`; `promoteAdd(
   TypeInt, TypeInt) = TypeInt`. Expect
   `ExprProjection{[Ref{"n"}], TypeInt}`. This pin locks §1.3's
   deferral in place: if a future change silently introduces an inner-
   aggregate axis on `ExprProjection`, the pin breaks structurally.

The parameter pins (Blocker 1):

8. `aggregate sum on arithmetic parameter arg` — `MATCH (n) RETURN
   sum($p + 1)`. `$p` types `TypeUnknown`, `$p + 1` types
   `promoteAdd(TypeUnknown, TypeInt) = TypeUnknown`,
   `aggregateResultType(AggSum, TypeUnknown) = TypeUnknown`. Expect
   `AggregateProjection{AggSum, nil, false, TypeUnknown}` AND
   `parameters["p"].Uses = [ExprUse{TypeUnknown, ExprInProjection}]`.
   Preserves Stage 6 §4's "no parameter is silently dropped" verbatim.

9. `aggregate count on bare parameter arg` — `RETURN count($p)`. `$p`
   types `TypeUnknown`; `aggregateResultType(AggCount, TypeUnknown) =
   TypeInt` (count is unconditional). Expect
   `AggregateProjection{AggCount, nil, false, TypeInt}` AND
   `parameters["p"].Uses = [ExprUse{TypeInt, ExprInProjection}]`. The
   `enclosingType` is the aggregate call's result type (`TypeInt`),
   not the operand's type (`TypeUnknown`) — this is the corrected
   Blocker-1 posture: the `Use`'s `enclosingType` matches what
   `classifyRichExpression` records today for the same shape at
   fall-through.

The bare-arg regression pin (Blocker 3(a)):

10. `aggregate sum on bare property arg (regression)` — `MATCH (n)
    RETURN sum(n.age)`. `n.age` types as `TypeUnknown` (property);
    `aggregateResultType(AggSum, TypeUnknown) = TypeUnknown`. Expect
    `AggregateProjection{AggSum, [Ref{"n","age"}], false, TypeUnknown}`.
    Pre- and post-widening the parser must emit the *same*
    `Refs()` sequence, `Distinct` flag, and `Type()` value. The pin
    comment names the two agreeing sites — `refFromNonArithmetic`
    (shape.go:29-48) for `functionArgRefs`, and `typeAtom`+
    `typeNonArithmetic` (typing.go:322-326 + typing.go:292-300) for
    the walker. Bit-identity of refs on a bare argument is a hard
    precondition of the widening; this pin surfaces any drift as a
    structural break.

The `count(count(*))` unit-level pin (Blocker 4):

11. `nested count of count star` — `RETURN count(count(*))`. Under
    this change the outer `count` classifies as
    `AggregateProjection{AggCount, nil, false, TypeInt}` — the inner
    `count(*)` is a rich subexpression that lands via
    `typeExpressionMining` with `typeAtom`'s COUNT arm
    (typing.go:340-345), which mines no refs and yields `TypeInt`.
    Outer `aggregateResultType(AggCount, TypeInt) = TypeInt` (count
    is unconditional). The engine still raises `NestedAggregation`
    at compile time; parser disposition is unchanged. Expect
    `AggregateProjection{AggCount, nil, false, TypeInt}`. Also
    verifies the `acceptance_test.go:289` bucket-3 catalogue entry
    stays green (godog scenario continues to pend as
    NestedAggregation-semantic).

Each pin's expected shape asserts:
- `AggregateProjection` variant (not `ExprProjection`) — the SF-3
  fix. Except #7, which asserts `ExprProjection` (the §1.3 deferral
  lock).
- Correct `AggregateFunc` — the outer aggregate name enters.
- Correct `Refs()` sequence — every var/var.prop atom in depth-first,
  left-to-right traversal order, duplicates preserved.
- Correct `Distinct` flag — DISTINCT preserved via `fi.DISTINCT()`.
- Correct `Type()` — Stage 10's table, unchanged.
- Correct parameter `Uses` (pins #8, #9) — `ExprUse{aggregate-result-
  type, ExprInProjection}` for every `$param` under the rich arg.

RED failure mode: each shape pin (#1–#6, #8, #9, #11) has an expected
`AggregateProjection` value; the parser's current output is an
`ExprProjection` (or, for #7, agrees — that pin is a positive-lock
regression pin and stays green pre- and post-widening). The
parameter pins (#8, #9) additionally assert on the `Parameters`
slice's `Uses` shape — the widening is done when both the projection
shape and the parameter Use shape land. Pin #10 (the bare-arg
regression) is green pre-widening and must stay green post-widening
— it is the bit-identity guard for the refs mining path §1.4 traces.

### 4.6 Negative pin roll-up

Pin #7 above is the sole negative pin; §1.3's commitment made
testable. Its failure mode: if a future change introduces an axis on
`ExprProjection` for "contains an inner aggregate", the pin's
expected `ExprProjection{[Ref{"n"}], TypeInt}` shape will no longer
match — a structural break that surfaces the widening at review time.
The pin exists to lock §1.3's deferral in place; deleting it silently
would let the door re-open.

### 4.7 Godog-corpus coverage of Return6[14]

The `[14] Aggregates in aggregates` scenario in Return6.feature
(bucket 3, catalogued at acceptance_test.go:289) is not touched by
this spec's parser widening — the scenario's *engine outcome* stays
the same (raises `NestedAggregation`), and the *scenario disposition*
stays the same (skiplist entry `catGroupingKeySemantic`). The
change is purely in the parser-side lowering shape, pinned at
unit level by RED pin #11. No skiplist churn; no acceptance_test
edit; no godog summary drift.

### 4.8 Guard quartet

`TestNoUndefinedSteps`, `TestSkiplistOrphans`, `TestGoldenOrphans`,
`TestSkiplistCategoryPolicy` must all stay green. This change
touches no skiplist entries, no step definitions, no test dir
enumeration, and the goldens it rebaselines are already in the corpus
(no new goldens, no orphaned goldens).

---

## 5. Definition of done

1. Spec commit lands (`docs(spec): aggregate-kind preservation on rich
   arguments`).
2. RED commit lands (eleven pins §4.5: six rich-arg shape pins #1–#6,
   the §1.3 deferral-lock negative pin #7, two parameter pins #8–#9
   preserving Stage 6 §4 discipline, the bare-arg regression pin #10,
   and the `count(count(*))` unit-level pin #11). Pin #10 is green
   both pre- and post-widening (bit-identity guard); pins #1–#6, #8,
   #9, #11 are red pre-widening and green post-widening; pin #7 is
   green both pre- and post-widening (locks §1.3 deferral). The RED
   verification records the exact failure text of the eight failing
   pins.
4. `just test` green.
5. `just lint` green.
6. `just fmt-check` green.
7. Godog summary line reads verbatim
   `3897 scenarios (3459 passed, 438 pending)` /
   `16006 steps (15568 passed, 438 pending)`. Any drift is a
   regression to reconcile before commit.
8. Golden diff is shape-preserving-on-refs-and-type per §1.6 / §4.3.
9. GitHub issue #78 closed via `Closes #78` in the branch's cover
   commit / PR body.

---

## 6. Commit inventory (single branch `aggregate-kind-rich-exprs`)

| Commit | Scope |
|--------|-------|
| spec   | this spec |
| RED    | Eleven parser pins per §4.5 (nine RED, two positive-lock regression) covering the aggregate-kind swap, the §1.3 deferral lock, Stage 6 §4 parameter discipline, bare-arg refs bit-identity, and the `count(count(*))` unit-level shape |
| GREEN  | `classifyFunction` widening + `classifyAggregateCall` helper + goldens rebaselined |
| gates  | `just test` / `just lint` / `just fmt-check` verification pass (recorded in report) |

Each commit is green in isolation of the ones after it: the spec
commit is docs-only; the RED commit's failing tests do not touch
production code so `just build` still passes; the GREEN commit
routes through the new classifier arm and rebaselines the small
target-golden set in one atomic step.

---

## 7. Weakest point (recorded honestly per ADR 0004)

**The nested case's deferral leaves a strict-intersection residual:
where an aggregate inside a rich expression happens to type
identically to the same expression without the aggregate, the model
does not distinguish the two on `Type()` alone.** The set is
strictly smaller than "every aggregate inside a rich expression"
because the Type axis already discriminates many cases (Stage-6
`typeAtom` typing.go:346 for `count(*)`, typing.go:358 for named
aggregates, run `aggregateResultType` in-line): `count(n) + 1`
types `TypeInt` and `n + 1` where `n` is a `TypeNode` binding types
`TypeUnknown` — already distinguishable. The residual is exactly
the case where operand types coincide (e.g. both sides `TypeUnknown`
because `n.age + 1` also types unknown for a property-lookup case).

The alternatives, considered and declined:

- **A `ContainsAggregate` boolean on `ExprProjection` pre-freeze.**
  Declined per the recorded contract on bead `gqlc-gyw` (see
  `bd show gqlc-gyw`, notes dated 2026-07-03): grouping-key
  discovery for `ExprProjection` residuals is a resolver-side
  re-parse of the projection's original text span; the sanctioned
  escape hatch for a `ContainsAggregate` axis is **post-freeze,
  additive, via the ADR 0008 revision protocol**, invoked only if
  the re-parse proves impractical. Pre-freeze introduction would
  foreclose the re-parse path before it has been proved impractical
  and pre-empt the sanctioned escape hatch. The bead is the source
  of truth for this decision; this spec defers to it.

- **A sub-projection axis (`InnerAggregate AggregateProjection`)
  on `ExprProjection`.** Structurally rejected: this IS the
  expression tree ADR 0003 §4 removed. It would work for one level
  of nesting and fall over on two (`1 + (count(n) + 2)`).

- **Widen `Refs()` to record which refs are aggregate operands.**
  Would break the invariant that `Refs()` is a flat sequence of
  bindings the projection touches. The resolver's referential-
  integrity sweep would need re-work. Declined; the resolver-side
  re-parse recorded on `gqlc-gyw` is cleaner.

- **Infer `ContainsAggregate` from `Type`.** Explicitly rejected on
  `gqlc-gyw`: `count(n) + 1` and `x + 1` can both type `TypeUnknown`
  under operand types the current corpus doesn't press but future
  dialect extensions could. Any inference would fail exactly on the
  intersection residual it was meant to fix.

The residual is bounded: it applies only to aggregates *not* at the
outermost projection node, *and* only where the aggregate's result
type coincides with the same expression's non-aggregate type. Every
RETURN or WITH item whose *whole* value is an aggregate call is now
correctly modelled as `AggregateProjection`, which is the vast
majority of the TCK corpus's aggregate usage. The §1.3 deferral is
the model's honest position on the remaining strict-intersection
fraction, not silence.

The lesser risks, recorded for completeness:

- **Parameter-mining behaviour is preserved verbatim (not changed).**
  Correcting a factual error in the previous draft of this section:
  `functionArgRefs` (shape.go:260) rejects parameters — its doc
  comment enumerates the rejection set explicitly ("arithmetic,
  nested call, list/map literal, parameter, CASE, comprehension,
  '*'"). So `sum($p)` today falls through to
  `classifyRichExpression`, which registers
  `ExprUse{aggregateResultType(AggSum, TypeUnknown),
  ExprInProjection} = ExprUse{TypeUnknown, ExprInProjection}` via
  `typeExpressionMining`. The Stage 6 §4 discipline is explicit:
  "no parameter is silently dropped" (echoed in the
  `typeExpressionMining` doc-comment). Under this change,
  `classifyAggregateCall` routes through `typeExpressionMining` and
  registers `ExprUse{aggregateResultType(fn, operand),
  ExprInProjection}` — same discipline, same `enclosingType`
  (matching what `classifyRichExpression` would have recorded for
  the whole aggregate call as a rich expression). RED pins #8 and
  #9 (§4.5) fix this at test level for `sum($p + 1)` and
  `count($p)`. Any behaviour drift here is a Stage-6 discipline
  violation.

- **The percentile* aggregates' second argument is walked without a
  dedicated slot.** `percentileCont(n.score, 0.5)` under this
  change routes through `classifyAggregateCall`, which walks *all*
  arguments and mines refs and parameters from each. The operand
  type comes from `args[0]`; the percentile's own type is not
  recorded. This matches the bare-argument aggregate path at
  expr.go:353 (`operand, _ = l.typeExpression(args[0])`) — the
  second argument's type is discarded there too. No behaviour
  change; the shape is preserved. No TCK scenario in the current
  corpus writes `percentileCont` with a rich second argument.

- **`ExprProjection`'s `Refs()` list order is preserved by the
  walker.** For a rich expression like `sum((r1.times + r2.times) /
  (H1 + H2))`, the mined refs sequence is `[r1.times, r2.times, H1,
  H2]` — **depth-first, left-to-right; duplicates preserved**
  (unified terminology, matching §1.4). This is the same order
  Stage 6 produces today, so the golden refs sequences under
  `AggregateProjection` match the current `ExprProjection`'s refs
  sequences bit-for-bit (§1.6). Any drift here is a purity-check
  regression.
