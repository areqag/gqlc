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
| `sum(x + 1)` (x:int)            | `ExprProjection{refs:[x], type:int}`                      | `AggregateProjection{fn:AggSum, refs:[x], distinct:false, type:int}`                     |
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

- **The type is honest end-to-end.** Stage-6 `typeAtom`'s aggregate arm
  (typing.go:358) already runs `aggregateResultType` when it sees an
  aggregate-named function invocation inside a rich expression, so
  `count(n) + 1` types as `TypeInt` (via `promoteAdd(TypeInt, TypeInt)`)
  today. The rich expression's Stage-10 result type is what the
  resolver sees; committed by Stage 10 §1.3 and unchanged by this
  change.

- **The kind belongs to the outermost projection node.** ADR 0003 §4
  is explicit: the model carries one cardinality-bearing distinction
  per projection node (`AggregateFunc` for `AggregateProjection`), not
  a sub-projection tree. Adding a `Contains` or `Inner` axis on
  `ExprProjection` would either (a) drag a partial expression tree into
  the model (violating the rule this axis is meant to protect), or
  (b) collapse to a boolean signal ("some aggregate lurks in here")
  that is strictly weaker than what the resolver already gets from
  re-executing the original text against a driver that raises
  `AmbiguousAggregationExpression` / `InvalidAggregation` when the
  aggregate's position is illegal, and yields the aggregate's value
  when it is legal. ADR 0005 makes this concrete: the generated code
  re-executes the original text, so the aggregate's kind at an inner
  position is already legible to whoever needs it — the driver, not
  the parser-side type interface.

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
  rules; the engine does. No skiplist churn.

- **The `Refs()` list still covers referential integrity.** Every
  `var/var.prop` inside a rich expression flows into `curPart.refs`
  via `typeAtom`'s variable arm (typing.go:322-326) regardless of
  where in the sub-tree it sits, so a nested `sum(x)` inside
  `1 + sum(x)` still records `x` for `build()`'s referential-integrity
  sweep. The rich-expression's `ExprProjection.Refs()` continues to
  carry these; the deferral leaves referential integrity intact.

The commitment: **the model records the outermost projection's
aggregate kind and nothing else**. An inner aggregate's kind is a
sub-projection fact the model does not encode, matching ADR 0003 §4.
The resolver's grouping-key computation (bead `gqlc-gyw`) re-derives
inner-aggregate presence from the projection list by walking the
original text (ADR 0005), which is where the inverse case (Stage 10
§8's residual gap) already routes.

This closes the door explicitly: the change fixes the *outermost*-
aggregate-kind-dropped bug and does not open a new one for inner
aggregates. Silent dropping at the outer position (the SF-3 bug) is
fixed; deferral at the inner position (a different question, with a
different answer) is documented here as the model's committed posture.

### 1.4 Refs-mining semantics for rich aggregate arguments

The bare-argument aggregate path calls `functionArgRefs` (shape.go:260),
which walks each argument as a `nonArithmeticAtom` and records a `Ref`
for every `var/var.prop` atom, refusing (returning `ok=false`) the
moment it sees a shape it cannot classify without an expression tree.
The rich-argument path this change adds cannot use `functionArgRefs`
by construction — every rich shape fails it — so it uses the same
walker Stage 6 uses for a rich expression body:
`typeExpression(arg)` returns `(operand-type, refs)`, where `refs`
is the sequence of `Ref{Variable, Property}` values every `var/var.prop`
atom in the sub-tree touched, in first-occurrence order. The
aggregate's `Refs()` list is exactly that sequence.

The two paths' semantics for a bare argument are equivalent: for
`sum(x)`, `functionArgRefs` and `typeExpression`'s walker both mine
one `Ref{Variable:"x"}` and no others. The rich-argument path is
therefore a strict superset — anything `functionArgRefs` would accept,
`typeExpression`'s walker mines identically. The reason `classifyFunction`
still calls `functionArgRefs` first is `functionArgRefs`'s residual
role (§1.4 corollary): it acts as a *strict* signal that the call is
bare, so `classifyFunction`'s bare-argument path (which builds
`FuncProjection` for a non-aggregate call whose arguments are all bare)
retains its shape. Only the aggregate-name branch of `classifyFunction`
takes the rich fallback; a non-aggregate function with a rich argument
(`abs(n.age + 1)`) still returns `(nil, false)` and falls through to
`classifyRichExpression`, because function identity is below the
type-interface boundary (ADR 0005 §5) and `FuncProjection`'s existence
is a pass-through the parser does not extend.

Concretely, the mining rules for a rich aggregate argument:

- **A single positional argument.** `sum`, `count`, `collect`, `avg`,
  `min`, `max`, `stDev*` take one argument. The walker walks that
  one and mines its refs.
- **Two arguments — percentileCont/percentileDisc.** The second
  argument is the percentile (a scalar), which the current
  bare-argument path types via `typeExpression(args[0])` for the
  operand type — matching what `classifyFunction` (expr.go:353) does
  today. Under the rich fallback, the walker walks *every* argument
  (both operand and percentile), mining refs from both; the operand's
  type still comes from `args[0]`. The percentile's refs are usually
  empty (it is a scalar literal), but a variable-parameterised
  percentile (`percentileCont(n.score, p)` where `p` is an in-scope
  binding) contributes its own refs — matching the "referential
  integrity covers every ref inside the projection" invariant. This
  case is not exercised by any TCK scenario in the current corpus,
  but the mechanical rule falls out of the walker's uniform behaviour.
- **`count(*)` remains the degenerate case.** The star atom is handled
  by the `atom.COUNT()` arm of `classifyProjection` (expr.go:243) with
  its own no-refs contract. This change does not touch that arm.
- **Duplicates in the refs list are preserved.** Stage 6's mining
  does not dedupe: `sum(x + x)` produces `Refs()` of `[x, x]`, matching
  what `typeAtom` records for the same expression under
  `classifyRichExpression`. The bare-argument path's `functionArgRefs`
  also does not dedupe (each argument's ref is appended in argument
  order). No behaviour change; both paths agree on the shape.
- **Parameter mining is inherited from the walker.** A `$p` inside a
  rich aggregate argument records the parameter as an `ExprUse{operand-
  type, ExprInProjection}` — the same treatment `classifyRichExpression`
  would have given it if the aggregate had fallen through. The rich-
  aggregate path routes through `typeExpressionMining` so the parameter
  mining fires exactly once per call; the previous fall-through path
  already routed through the same helper.

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

**Estimated size.** Under 20 goldens touched. This is not a broad
rebaseline like [[cypher-query-parser-part-distinct-axis]]'s field-
addition-on-every-part; it is a targeted shape swap on exactly the
projection items that press a rich aggregate. The scripted audit in
§4.4 is the pre-RED verification.

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
the outer projection.

### 1.11 What this change does not do

- Does not add any new projection variant (`AggregateProjection` and
  `ExprProjection` already exist).
- Does not change any type in `aggregateResultType` (same table Stage
  10 committed).
- Does not read DISTINCT from any new grammar site (still
  `fi.DISTINCT()` on `oC_FunctionInvocation`, unchanged from Stage 10).
- Does not touch `classifyRichExpression`, `typeAtom`, or Stage 6's
  rich-expression walk (they still see the same subtree they see today
  when a rich-argument aggregate is inside a larger rich expression;
  the classifier only *precedes* those calls at the outer projection).
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
is `typeExpression(args[0])`, matching what `classifyFunction` does
today at expr.go:353-355.

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
    args := fi.AllOC_Expression()
    if len(args) > 0 {
        var opRefs []query.Ref
        operand, opRefs = l.typeExpression(args[0])
        refs = append(refs, opRefs...)
    }
    for _, arg := range args[1:] {
        _, more := l.typeExpression(arg)
        refs = append(refs, more...)
    }
    distinct := fi.DISTINCT() != nil
    return query.NewAggregateProjection(fn, refs, distinct, aggregateResultType(fn, operand))
}
```

Structural notes on the code shape:

- **Refs flow onto `curPart.refs` via `typeExpression`'s `typeAtom`
  arm.** The walker (typing.go:322-326) appends every `var/var.prop`
  atom to `l.curPart.refs` as it walks. So the aggregate call's refs
  land on the current part exactly the way a bare-argument aggregate's
  do today, via `functionArgRefs`'s loop at classifyFunction:341-343.
  The two paths converge on the same `curPart.refs` shape.
- **Parameter mining rides `typeExpression`, not
  `typeExpressionMining`.** `classifyFunction` at RETURN position is
  reached from `collectReturnItem` (expr.go:199), which does not run
  the mining wrapper — it dispatches through `classifyProjection`.
  Under this change, `classifyAggregateCall` uses `typeExpression`
  (not `Mining`) so parameter mining continues to run via `typeAtom`'s
  parameter arm (typing.go:329-339) — which approves the node and
  records it on `l.exprParams`, but the outer caller's mining sweep
  drives the `ExprUse` registration. Wait — this is a subtlety worth
  pinning: `classifyRichExpression` (typing.go:867) calls
  `typeExpressionMining` and records `ExprUse{TypeInProjection}` for
  every parameter. The bare-argument aggregate path today
  (classifyFunction expr.go:353) calls plain `typeExpression`, which
  approves the parameter node in `typeAtom` but does NOT register a
  `Use` for it. So `RETURN sum($p)` today mines the parameter's node
  as approved (no unbound-parameter error) but records no `Use` on
  the aggregate call — a Stage-10 shape the goldens have baked in.
  This change preserves the same discipline: `classifyAggregateCall`
  uses `typeExpression`, not `Mining`; a `$p` inside a rich aggregate
  argument is approved but not recorded as a `Use`. The pre-RED
  audit (§4.4) verifies this against the corpus: no scenario in the
  corpus writes `RETURN sum($p + 1)`. If one exists, the audit adds a
  pin for it and the spec's behaviour matches the bare-argument
  precedent (approve, don't record) — the alternative (running
  `Mining`) would be a behaviour widening beyond this change's scope.
- **`functionArgRefs`'s residual role.** After the widening,
  `functionArgRefs` is only consulted on the FuncProjection path
  (non-aggregate name). This preserves the "no expression tree, no
  nested calls" discipline for `FuncProjection` — function identity
  is below the boundary (ADR 0005), so widening `FuncProjection` to
  carry rich arguments is out of scope.

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
grep -l '"kind": "expr"' internal/query/cypher/testdata/golden/*.golden.json > /tmp/expr-goldens.txt
# For each candidate, check whether the return-item name (which is the
# verbatim source text of the projection expression) starts with an
# aggregate name and has an open paren immediately after:
while read f; do
    if grep -E '"name":\s*"(count|sum|collect|min|max|avg|stdev|stdevp|percentilecont|percentiledisc)\s*\(' "$f" > /dev/null; then
        echo "$f"
    fi
done < /tmp/expr-goldens.txt > /tmp/target-goldens.txt
wc -l /tmp/target-goldens.txt
```

The `wc -l` output is the upper bound on touched goldens.

**Confirmed candidates from a spot-check:**

- `Return6_9d684013a026.golden.json` — Return6[16], `sum(...)` over
  a seven-term arithmetic expression. Return item `name: "sum"`;
  the value expression's aliased name aliases the aggregate, so
  the grep above uses the *expression* text mined from the pre-alias
  source. The audit's grep is on the field the classifier populates
  before an `AS` alias overrides it.

The pre-RED phase must produce the full enumeration; the spec commits
that the audit's output — plus any WITH-scoped analogues found under
the same grep — is the exhaustive set. Every RED pin (§4.5) names a
subset; every GREEN rebaseline is bounded to this set.

The audit's grep is deliberately loose (matches any function-name
prefix followed by paren); a false positive is a return item whose
alias contains an aggregate keyword but whose expression is not an
aggregate — the RED phase's fine-grained pin distinguishes these.

### 4.5 RED pins

Six parser pins in `internal/query/cypher/parser_test.go`, one for
each shape category:

1. `aggregate sum on arithmetic arg` — `MATCH (n) RETURN sum(n.age + 1)`
   lowers to one part with `Returns[0].Value =
   AggregateProjection{AggSum, [Ref{"n","age"}], false, TypeInt}` (n
   is a node, n.age's type is `TypeUnknown`, but the RED pin uses
   a scenario where the outer promoteAdd commits to `TypeInt` — the
   spec's target-lowering-table row is preserved verbatim).

   Wait — a subtle point: `n.age` is `TypeUnknown` because the parser
   does not have schema info (Stage 6 §1.3). So `n.age + 1` types
   as `promoteAdd(TypeUnknown, TypeInt) = TypeUnknown`, and
   `aggregateResultType(AggSum, TypeUnknown) = TypeUnknown`. The
   correct RED expectation for `sum(n.age + 1)` is `AggregateProjection
   {AggSum, [Ref{"n","age"}], false, TypeUnknown}`. The target
   lowering table's row for `count(n.age + 1)` has `type:int` because
   count always types as int regardless of operand; the row for
   `sum(n.age + 1)` where the operand's own type is `TypeUnknown`
   (n.age → unknown) actually resolves to `type:unknown`. The spec
   corrects this: the target-lowering-table row values are what the
   parser will actually compute per Stage 10's table; the RED pin
   uses `TypeUnknown` for `sum(n.age + 1)` where n.age is a property
   lookup, and `TypeInt` for `sum(x + 1)` where x is a bound variable
   with a known integer type (rare in the current corpus — most
   bindings type as `TypeNode` / `TypeEdge`, not `TypeInt`).

   RED pin picks the shape whose behaviour is unambiguous: `RETURN
   count(n.age + 1)` — `count` always types as `TypeInt`, so the pin
   asserts `AggregateProjection{AggCount, [Ref{"n","age"}], false,
   TypeInt}`. The other RED pins pick similarly unambiguous shapes.

2. `aggregate count on arithmetic property arg` — `MATCH (n) RETURN
   count(n.age + 1)` — `AggregateProjection{AggCount, [Ref{"n","age"}],
   false, TypeInt}`.

3. `aggregate collect on boolean composite` — `MATCH (a), (b) RETURN
   collect(a.p OR b.p)` — a.p and b.p are unknown, so the OR types
   as `TypeBool`, and `aggregateResultType(AggCollect, TypeBool) =
   list<bool>`. Expect `AggregateProjection{AggCollect,
   [Ref{"a","p"}, Ref{"b","p"}], false, list<bool>}`.

4. `aggregate min on list literal arg` — `RETURN min([1, 2, 3])` — no
   binding needed; the list-literal argument has `TypeList(TypeInt)`
   as its Stage-6 type; `aggregateResultType(AggMin, TypeList{...}) =
   TypeUnknown` (min over list is engine-inconsistent, Stage 10 §8).
   Expect `AggregateProjection{AggMin, nil, false, TypeUnknown}`.

5. `aggregate sum on nested function call` — `RETURN sum(range(1, 3))`
   — `range(1, 3)` types as `TypeUnknown` (function identity below
   the boundary); `aggregateResultType(AggSum, TypeUnknown) = TypeUnknown`.
   Expect `AggregateProjection{AggSum, nil, false, TypeUnknown}`.

6. `aggregate sum distinct on arithmetic arg` — `MATCH (n) RETURN
   sum(DISTINCT n.age + 1)` — `AggregateProjection{AggSum,
   [Ref{"n","age"}], true, TypeUnknown}` (n.age is unknown → sum result
   is unknown; DISTINCT is preserved).

Each pin's expected shape asserts:
- `AggregateProjection` variant (not `ExprProjection`) — the SF-3 fix.
- Correct `AggregateFunc` — the outer aggregate name enters.
- Correct `Refs()` sequence — every var/var.prop atom in order.
- Correct `Distinct` flag — DISTINCT preserved via `fi.DISTINCT()`.
- Correct `Type()` — Stage 10's table, unchanged.

RED failure mode: each pin's expected value is an `AggregateProjection`;
the parser's current output is an `ExprProjection`; go-cmp's `Diff`
prints a type-name mismatch at the top of the assertion — the sixfold
red is the RED evidence.

### 4.6 Negative pin — the nested case is documented deferral

One additional pin, `nested aggregate inside rich projection`, is the
§1.3 commitment made testable. `MATCH (n) RETURN count(n) + 1`
continues to lower as `ExprProjection{[Ref{"n"}], TypeInt}` — the
outer expression is not an aggregate call, and per §1.3 the model
does not lift an inner aggregate through a rich-expression wrapper.

The pin's failure mode: if a future change introduces an axis on
`ExprProjection` for "contains an inner aggregate", the pin's
`ExprProjection` shape will no longer match — a structural break
that surfaces the widening at review time. The pin exists to lock
§1.3's deferral in place; deleting it silently would let the door
re-open.

### 4.7 The `count(count(*))` scenario

The pin `nested count of count star` covers the bucket-3 scenario
Return6[14]: `RETURN count(count(*))` continues to lower as
`AggregateProjection{AggCount, nil, false, TypeInt}` — the outer
`count`'s aggregate name enters the model (this change's fix), the
inner `count(*)` is a rich subexpression under §1.3 (deferred), and
the engine still raises `NestedAggregation` at compile time (bucket
3, unchanged).

The pin asserts the parser-level shape (the model the parser produces),
not the engine's outcome (which is under acceptance_test's skiplist).
The bucket-3 disposition catalogued at acceptance_test.go:289 stays
verbatim.

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
2. RED commit lands (seven pins §4.5–§4.7; the exact failure text
   recorded in the RED verification output).
3. GREEN commit lands (classifier widening §4.1; goldens rebaselined
   per §4.3 purity check).
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
| RED    | Seven failing parser pins for the aggregate-kind swap and the §1.3 negative-pin deferral |
| GREEN  | `classifyFunction` widening + `classifyAggregateCall` helper + goldens rebaselined |
| gates  | `just test` / `just lint` / `just fmt-check` verification pass (recorded in report) |

Each commit is green in isolation of the ones after it: the spec
commit is docs-only; the RED commit's failing tests do not touch
production code so `just build` still passes; the GREEN commit
routes through the new classifier arm and rebaselines the small
target-golden set in one atomic step.

---

## 7. Weakest point (recorded honestly per ADR 0004)

**The nested case's deferral means the model still cannot distinguish
`count(n) + 1` from a plain `n + 1` on the wire when both would
produce `TypeInt`.** §1.3 argues this is the honest posture — the
resolver walks the original text (ADR 0005), the driver raises
`AmbiguousAggregationExpression` / `InvalidAggregation` when the
position is illegal, and the outermost projection's kind is what
the model commits to. But it is a real residual: two observably-
different queries (one collapses rows, one does not) still lower
to the same `ExprProjection` when the aggregate sits inside a
larger rich expression.

The alternatives, considered and declined:

- **A `ContainsAggregate` boolean on `ExprProjection`.** Cheapest
  widening: one bit says "some aggregate lurks inside this rich
  expression". Declined because a boolean is strictly weaker than
  what the resolver already gets from re-execution (which yields
  the *value*, not just the presence) and it opens the door to
  further axes (`ContainsInnerCount`, `AggregateCount`, etc.) that
  would collectively re-derive the expression tree ADR 0003 §4
  removed from the model.

- **A sub-projection axis (`InnerAggregate AggregateProjection`)
  on `ExprProjection`.** Structurally rejected: this IS the
  expression tree ADR 0003 §4 removed. It would work for one level
  of nesting and fall over on two (`1 + (count(n) + 2)`).

- **Widen `Refs()` to record which refs are aggregate operands.**
  Would break the invariant that `Refs()` is a flat sequence of
  bindings the projection touches. The resolver's referential-
  integrity sweep would need re-work. Declined; the ADR 0005 path
  is cleaner.

The residual is bounded: it applies only to aggregates *not* at the
outermost projection node. Every RETURN or WITH item whose *whole*
value is an aggregate call is now correctly modelled as
`AggregateProjection`, which is the vast majority of the TCK corpus's
aggregate usage. The §1.3 deferral is the model's honest position on
the remaining fraction, not silence.

The lesser risks, recorded for completeness:

- **`sum($p)` — a bare-parameter aggregate argument — is not
  registered as a Use.** The bare-argument path (`functionArgRefs`)
  refuses parameters (it accepts only var/var.prop and scalar
  literals), so today `sum($p)` falls through to
  `classifyRichExpression`, which registers the parameter as
  `ExprUse{TypeUnknown, ExprInProjection}`. Under this change,
  `sum($p)` will hit the new aggregate arm (name matches, arg is
  rich), and `classifyAggregateCall` uses `typeExpression` (not
  `Mining`) — the parameter is approved but no `Use` is recorded.
  This is a **behaviour change** from today for the specific shape
  `sum($p)` / `count($p)` / etc., and the RED phase must decide:
  either (a) accept the change and let the parameter show up
  unused-but-approved, matching how bare-argument aggregates handle
  scalar literals (approve, no Use because there's no ref), or (b)
  route through `typeExpressionMining` and register the ExprUse.
  Position: (a) — the bare-argument aggregate precedent
  (approve-but-no-Use for a scalar literal) covers this shape.
  A parameter is a scalar-like operand for aggregate purposes; the
  same treatment applies. If any TCK scenario writes `sum($p)`, the
  audit surfaces it and the pin nails down (a). This is called out
  here so the reviewer can push back if (b) is the right call.

- **The percentile* aggregates' second argument is walked without a
  dedicated slot.** `percentileCont(n.score, 0.5)` under this change
  routes through `classifyAggregateCall`, which walks *all*
  arguments and mines refs from each. The operand type comes from
  `args[0]`; the percentile's own type is not recorded. This
  matches the bare-argument aggregate path at expr.go:353
  (`operand, _ = l.typeExpression(args[0])`) — the second argument's
  type is discarded there too. No behaviour change; the shape is
  preserved. No TCK scenario in the current corpus writes
  `percentileCont` with a rich second argument.

- **`ExprProjection`'s `Refs()` list order is preserved by the
  walker.** For a rich expression like `sum((r1.times + r2.times) /
  (H1 + H2))`, the mined refs sequence is `[r1.times, r2.times, H1,
  H2]` — depth-first, left-to-right. This is the same order Stage 6
  produces today, so the golden refs sequences under `AggregateProjection`
  match the current `ExprProjection`'s refs sequences bit-for-bit
  (§1.6). Any drift here is a purity-check regression.
