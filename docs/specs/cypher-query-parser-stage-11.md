# Stage 11 spec — Cypher query parser: predicate expressions

The implementation brief for Stage 11 of the Cypher implementation of
`query.Parser`. Eleventh model evolution after Stage 10 per ADR 0004
(test-first, evolving until feature-complete), under the curation
discipline of ADR 0003 and the type-interface boundary of ADR 0005.
Stage 11 is the sixth stage of the ADR-0007 pre-freeze expansion beyond
the read core. It **completes the predicate-expression surface**: the
four list quantifiers (`ALL` / `ANY` / `NONE` / `SINGLE`), existential
subqueries (`EXISTS { ... }`), and — folded in from the parallel bead
`gqlc-3r0` — the bucket-1 rejection of a pattern predicate in
`RETURN` / `WITH` projection position.

Every quantifier and every `EXISTS {}` yields a boolean, so the model
impact is thin: the `Type` sum already carries `TypeBool`, and no new
`Projection` variant is required. The work concentrates in the parser:
grammar coverage (four Quantifier atoms + one ExistentialSubquery atom),
**scoping** (the iteration variable of a quantifier is bound to its
filter body only; every entity binding declared inside an `EXISTS {}`
must not leak into the outer part's `Bindings` list, `byVar` map,
`pathBindings`, or `unwindBindings`), and per-position typing (a
predicate-expression atom in a WHERE / rich-expression position types
as `TypeBool` instead of the current `TypeUnknown`). The `gqlc-3r0`
fold adds a fifth true-rejection sentinel and removes two skiplist
entries, aligning bucket-1 discipline with the widened surface.

This document is a **delta** against Stages 0–10 (referenced individually
where relevant); everything not stated here carries over verbatim.
Sections appear here only where Stage 11 changes something.

Tracking: bead `gqlc-665` (GitHub #44) plus folded scope `gqlc-3r0`.
Lands as one graphite branch (`stage-11-quantifiers`) with separated
commits (spec + docs → parser red → parser green → gqlc-3r0
sentinel/reject → dir unlock + goldens), independently mergeable as a
whole: `just test` is green if this branch lands on `master` alone
(AGENTS.md stacked-branch invariant).

---

## 1. Deliverables

### 1.1 Quantifier atoms — `ALL` / `ANY` / `NONE` / `SINGLE`

The openCypher grammar rule `oC_Quantifier` expands to one of four
keyword alternatives around a shared `oC_FilterExpression` body:

```
oC_Quantifier: (ALL|ANY|NONE|SINGLE) SP? '(' oC_FilterExpression ')'
oC_FilterExpression: oC_IdInColl (SP oC_Where)?
oC_IdInColl: oC_Variable SP IN SP oC_Expression
```

Semantically every quantifier is a fold that returns a boolean:
`all(x IN xs WHERE p(x))` is true iff `p` holds for every element of
`xs`; `any` is true iff `p` holds for at least one; `none` is
`NOT any`; `single` is true iff `p` holds for exactly one. The type
interface commits `TypeBool` on every path.

**Result type.** Stage 11 types a `oC_Quantifier` atom as `TypeBool`
in `typeAtom` (Stage-6 rich-expression typer) — the current arm
returns `TypeUnknown`. This is the sole change on the projection side:
because `TypeBool` is not a new type variant and quantifiers already
parse (Stage 6 accepted them as opaque `TypeUnknown` atoms), the model
gains no new sum member. A quantifier that appears at RETURN /
projection position rides `classifyRichExpression`, which now produces
an `ExprProjection{refs, TypeBool}`. A quantifier that appears in
WHERE / predicate position rides `mineWhere`, whose enclosing-type
sees `TypeBool`.

**Scoping.** The iteration variable `x` in `x IN xs WHERE p(x)` is
bound to the filter body — the `oC_Where` predicate — not to the
outer part. Stage 6's Ref-mining walk would otherwise emit `x` as a
ref against the outer part (breaking referential integrity —
`x` has no binding in the enclosing part). The Stage-11 quantifier
arm in `typeAtom`:

- types the source expression (the `oC_Expression` in `oC_IdInColl`)
  under the caller's `refs` slice — refs from the source list belong
  to the outer scope, so they enter the outer part's refs and
  parameters normally;
- types the filter `oC_Where`'s predicate under a **local refs
  slice** that is discarded (mirrors the `mineWhere` savedRefs
  discipline) — the iteration variable never surfaces as an outer
  ref, and any `x.prop` inside the WHERE resolves against the
  iteration variable rather than a phantom outer binding;
- mines **parameters** in both the source expression and the filter
  WHERE, so a `$param` under a quantifier records an
  `ExprUse{TypeBool, ExprInPredicate}` (WHERE position) or an
  `ExprUse{sourceType, …}` (source position — see §1.5). No parameter
  is silently dropped.

Iteration-variable scoping is enforced structurally (a local refs
sink, not a scope stack), not enforced as a name-collision rule: a
quantifier that shadows an outer binding (`WITH a MATCH (a) WHERE
any(a IN [1,2] WHERE a > 0)`) is a legal-but-shadowing query
whose runtime semantics the engine handles. Adding a shadowing check
here would encode a naming-hygiene rule that is neither in the type
interface nor a scope leak (the iteration variable does not enter the
outer refs list under §1.5), so the parser stays honest and lets the
resolver decide whether shadowing is a downstream error.

### 1.2 Existential subqueries — `EXISTS { ... }`

The openCypher grammar rule `oC_ExistentialSubquery` covers two
shapes:

```
oC_ExistentialSubquery:
    EXISTS SP? '{' SP? (oC_RegularQuery | (oC_Pattern (SP oC_Where)?)) SP? '}'
```

- The **inline-pattern form** — `EXISTS { (n)-->(m) [WHERE …] }` —
  is a pattern (plus optional WHERE) that is true iff the pattern
  matches at least one row.
- The **subquery form** — `EXISTS { MATCH … [WITH …] RETURN … }` —
  is a nested read (or write, per `ExistentialSubquery3`; see §1.6)
  that is true iff it produces at least one row.

Every EXISTS yields a boolean. Stage 11 types `oC_ExistentialSubquery`
as `TypeBool` in `typeAtom`.

**Scoping — the meat of Stage 11.** An `EXISTS {}` opens a nested scope
whose bindings are **local**: outer variables are visible inside
(so the correlated `n` in `MATCH (n) WHERE exists { (n)-->() }`
resolves against the outer part), but inner bindings — the `m`
introduced by the inner pattern, any inner path binding, any inner
UNWIND — MUST NOT leak into the outer part's `Bindings`, `byVar`,
`pathBindings`, or `unwindBindings`. A leak would make an inner name
appear in the outer part's referential-integrity model (a phantom
binding), and the resolver would try to type it — with the wrong
scope.

The ANTLR listener walk is a top-down traversal that fires
`EnterOC_Match` / `EnterOC_With` / `EnterOC_Return` / `EnterOC_Unwind`
regardless of the surrounding context. Without a guard, an `EnterOC_Match`
inside `EXISTS { MATCH ... }` invokes `collectPattern` against
`l.curPart`, which appends every inner node and edge to the outer
part's `bindings`. Stage 11 adds a **suppression depth counter**
`subqueryDepth int` on the listener:

- `EnterOC_ExistentialSubquery` increments `subqueryDepth`; parameters
  under the subquery are mined via a dedicated pass (below), so
  the subquery's clauses do not need to run their own collection
  passes to see parameters. The counter is decremented on
  `ExitOC_ExistentialSubquery`.
- Every clause-collecting Enter* handler (`EnterOC_Match`,
  `EnterOC_With`, `EnterOC_Return`, `EnterOC_Unwind`, `EnterOC_Create`,
  `EnterOC_Merge`, `EnterOC_Delete`, `EnterOC_Set`, `EnterOC_Remove`,
  `EnterOC_InQueryCall`, `EnterOC_StandaloneCall`) returns immediately
  when `subqueryDepth > 0`. The write / CALL handlers still fire
  their `ErrUnsupportedClause` at the outer level (a write clause
  appearing outside every `EXISTS {}` is unsupported); inside an
  `EXISTS {}` the write is accepted-and-defer per §1.6.
- `EnterOC_SingleQuery` / `EnterOC_Union` are **not** guarded — the
  subquery's `oC_RegularQuery` opens its own throwaway
  branch/part structure that we never reach into (no goldens
  reference it). This is intentional: opening the inner
  singleQuery is grammar-driven and cheap; the guard on the
  clause-collecting handlers is what stops the inner clauses from
  writing to any state the outer walk keeps.

The parameter mining under an `EXISTS {}` is handled directly on the
`EnterOC_ExistentialSubquery` entry: a helper walks the subquery
tree, finds every `oC_Parameter` node, and records
`ExprUse{TypeBool, ExprInPredicate}` for each. Parameter dedup by
first-appearance order (Stage 0 §D) is preserved because the outer
walk order visits the subquery in source order.

**Correlated outer names.** A subquery reference to an outer variable
(`MATCH (n) WHERE exists { (n)-->() }`) is not a scope violation —
`n` is bound in the outer part and the resolver honours the
correlation at execution (the engine re-runs the original text per
ADR 0005). Because the parser suppresses inner collection entirely
under `EXISTS {}`, no ref for `n` inside the subquery is recorded
against the outer part; correlated correctness is a runtime
concern the type interface does not carry, matching the ADR 0005
posture.

**Nested `EXISTS {}`.** The counter is a depth, not a flag, so
`EXISTS { MATCH (m) WHERE exists { (l)-->(n)-->(m) } RETURN true }`
increments to 2 in the inner subquery and decrements symmetrically
on exit. Every inner binding is suppressed at every depth. The
`ExistentialSubquery3` corpus (Scenarios [1] / [2] / [3]) exercises
two-level nesting; the depth counter covers arbitrary depth.

**Reused byVar / pathBindings across the subquery boundary.** A
`byVar` collision between an inner and an outer binding is a scoping
non-event once suppression is in place: the inner binding never
reaches the outer `byVar`. The Stage-8 three-way collision sweep
(entity / path / unwind) still fires within one scope — outer vs.
outer, or inner vs. inner — and this remains correct because the
suppression stops inner writes from touching outer state.

### 1.3 Pattern predicates — a WHERE-position atom

`oC_PatternPredicate` — an atom of the shape `(n)-[]->(m)` used as a
boolean predicate — is already accepted by Stage 6's `typeAtom` as
`TypeUnknown` (no ref mining, no widening). Stage 11 tightens it to
`TypeBool` (§1.5) so a WHERE containing only a pattern predicate
types as `TypeBool` end-to-end. This matches the earlier tightening
`Stage 10` did for aggregates inside rich expressions.

Pattern predicates in WHERE / EXISTS position are legal openCypher.
Pattern predicates in **projection** position (RETURN / WITH) are a
parse-shape rejection, handled by §1.4.

### 1.4 Reject pattern predicates in projection position (`gqlc-3r0`)

The two currently-skiplisted TCK scenarios

- `Pattern1 [22] Fail on using pattern in RETURN projection`:
  `MATCH (n) RETURN (n)-[]->()`
- `Pattern1 [23] Fail on using pattern in WITH projection`:
  `MATCH (n) WITH (n)-[]->() AS x RETURN x`

are bucket-1 (parse-rejectable, ADR 0007 §I): a pattern predicate at
projection position is not `openCypher`, and the TCK cites
`SyntaxError:UnexpectedSyntax`. Stage 11 detects the shape in
`collectReturnItem` (RETURN projection) and `collectProjection`
(the item loop `WITH` shares with RETURN via `oC_ProjectionBody`) and
raises a new sentinel `ErrPatternInProjection` at the fail-site.

**New sentinel.** `ErrPatternInProjection` names a category that no
existing sentinel covers:
- `ErrUnsupportedClause` — clause-level rejection (writes, CALL).
- `ErrUnsupportedParameter` — non-bindable `$param` position.
- `ErrUnboundVariable` — a ref with no binding.
- `ErrVariableKindConflict` — a variable used as two kinds.

None of the four match: a pattern predicate at projection position is
not a clause, parameter, unbound name, or kind conflict — it is a
position-shape violation. Rather than overload one of the four with
a fifth meaning, Stage 11 adds a fifth sentinel that is
freeze-durable (pattern predicates never become legal projection
atoms; the openCypher grammar admits them only inside boolean
positions). The sentinel set grows from four to five.

Alternative considered: fold it into `ErrUnsupportedParameter`. Rejected:
`ErrUnsupportedParameter` is about `$param` shapes, and reusing it for
a pattern-in-projection would confuse the sentinel's meaning at the
parser boundary — future readers of an `ErrUnsupportedParameter`
message about a pattern predicate would rightly be confused.

**Fail-site.** The rejection fires when `collectReturnItem`'s
expression classifies as a bare pattern-predicate atom — the
existing `classifyProjection` returns `(nil, false)` for atoms
outside its var/literal/function set, then `classifyRichExpression`
runs. A helper `isPatternPredicateAtom` inspects the expression's
non-arithmetic atom (via the same precedence-tower collapse as
`nonArithmeticAtom`); when the atom holds an `OC_PatternPredicate`,
`collectReturnItem` fails with `ErrPatternInProjection` before
falling into `classifyRichExpression`. The check runs against the
same `OC_ProjectionBody` path both `RETURN` and `WITH` use, so both
scenarios reject.

**Skiplist removal.** The two `Pattern1 [22]/[23]` entries in
`acceptance_test.go`'s `skiplist` are deleted, and the two
`gqlc-3r0` deferral comment paragraphs go with them. Stage 11's
`gqlc-3r0` fold retires the deferral.

### 1.5 Predicate-expression parameter mining and typing

Two positions matter for parameter mining under a predicate
expression, matching the Stage 6 posture:

- **Quantifier source-list expression.** The `oC_Expression` inside
  `oC_IdInColl` (`x IN xs`) is typed under the outer scope: refs go
  to the outer part, parameters record an
  `ExprUse{sourceType, ExprInPredicate}` when the quantifier is in
  a WHERE / rich-expression predicate position, or
  `ExprUse{sourceType, ExprInProjection}` when the quantifier is at
  RETURN / WITH projection position — the same discriminator the
  Stage-6 rich-expression typer already threads. `sourceType` is
  the Stage-6 type of the source expression (typically
  `list<int>`, `list<string>`, `TypeUnknown` for a `$param` source).
- **Quantifier filter WHERE.** The predicate expression is a
  boolean; every parameter under it records
  `ExprUse{TypeBool, ExprInPredicate}` — the iteration variable
  is not a parameter, and the outer refs slice is not mutated by
  refs the filter body touches (§1.1 scoping).
- **EXISTS body.** Every parameter under an `EXISTS {}` records
  `ExprUse{TypeBool, ExprInPredicate}` — the subquery's own
  result is boolean, and its refs never enter the outer refs
  slice.

`typeAtom` gains three arms (or extends the existing three
placeholder arms):

- `OC_Quantifier`: type the source list, discard the filter's refs,
  mine parameters both sides, return `TypeBool`.
- `OC_ExistentialSubquery`: mine parameters via `findParameters`
  (the same helper `requireAllParametersApproved` uses), record each
  as an approved `ExprUse{TypeBool, ExprInPredicate}`, return
  `TypeBool`. No ref walking — the suppression counter handles
  clauses; the parameter walk covers the rest.
- `OC_PatternPredicate`: no ref mining (matching the current Stage-6
  arm's posture — pattern-predicate refs are semantically inside
  a nested WHERE and are runtime-checked), but the atom types as
  `TypeBool`. Parameters under a pattern predicate are mined by
  the existing `findParameters` sweep at the enclosing rich
  expression (Stage 6 §4) — `typeExpressionMining` already
  collects every `oC_Parameter` node the walk touched — so no new
  parameter path is required for this arm.

The `OC_PatternComprehension` arm — `[(n)-->() | n.name]` — stays
at `TypeUnknown` for Stage 11 (comprehensions carry a list result
whose element type depends on the projection sub-tree; a wrong
concrete would be strictly worse than the honest `TypeUnknown`).
Pattern comprehensions in projection position are a follow-up
question the corpus does not currently press (the `expressions/pattern`
dir's negative scenarios that Stage 11 unlocks target pattern
predicates, not comprehensions).

### 1.6 Writes inside `EXISTS {}` — a scoping-first accept-and-defer

`ExistentialSubquery2 [3]` exercises the subquery-form EXISTS with a
`SET` clause inside:

```cypher
MATCH (n) WHERE exists { MATCH (n)-->(m) SET m.prop='fail' } RETURN n
```

Per Stage 12's roadmap (writes) this becomes an accepted subquery
after Stage 12; today (Stage 11), the outer `EnterOC_Set` handler
suppresses inside `EXISTS {}` (§1.2), so the query parses. The
scenario has an `Then a SyntaxError should be raised at compile
time: InvalidClauseComposition` outcome — a bucket-3 semantic rule
the type interface does not carry: an engine re-executing the
original text raises it, per ADR 0005. The `expressions/*` dir
categorically rides `isBucketThreeDir`, so the acceptance-suite
categorical accept-path handles the mismatch without a per-scenario
skiplist entry.

If the corpus later moves writes out of `expressions/` into a
projection-mediated position, the categorical rule would need
revisiting — Stage 12 owns that revisit.

### 1.7 Sentinel status

`ErrPatternInProjection` is the fifth sentinel. The other four —
`ErrUnsupportedClause`, `ErrUnsupportedParameter`, `ErrUnboundVariable`,
`ErrVariableKindConflict` — remain unchanged in meaning and reach.
`TestSentinelReachability` runs against the five-sentinel set; a
`mustReject` case exercises the new sentinel with a verbatim TCK
query (`Pattern1 [22]` — `MATCH (n) RETURN (n)-[]->()`).

### 1.8 Corpus wiring

`readCoreDirs` gains **two** dirs:

- `expressions/quantifier` — 12 feature files. The dir joins the
  categorical `isBucketThreeDir` regime, so negative scenarios like
  `[15] Fail none quantifier on type mismatch` ride the accept-and-
  defer path automatically.
- `expressions/existentialSubqueries` — 3 feature files. Same
  categorical treatment.

Neither dir needs per-scenario skiplist entries at Stage 11: every
positive scenario parses under the widened rules; every negative
scenario is bucket-3 by directory. Two existing skiplist entries
(`Pattern1 [22]/[23]` in §1.4) are deleted.

### 1.9 Layer-2 pins

New `mustParse` cases exercising the Stage-11 shapes (all verbatim
from the corpus — the layer-2 accept-path rule; no authored inputs):

- **all quantifier**: `RETURN all(x IN [1,2,3] WHERE x > 0) AS r`
  (`Quantifier4 [3]`) → `ExprProjection{refs:nil,
  Type:TypeBool}`.
- **none quantifier over corpus list literal**:
  `RETURN none(x IN [] WHERE true) AS a, none(x IN [] WHERE false)
  AS b, none(x IN [] WHERE x) AS c` (`Quantifier1 [1]`) → three
  `ExprProjection{refs:nil, Type:TypeBool}`.
- **any quantifier with corpus predicate**: chosen from
  `Quantifier7 [1]` — `RETURN any(x IN [] WHERE true) AS a, any(x IN
  [] WHERE false) AS b, any(x IN [] WHERE x) AS c` — three
  `ExprProjection{refs:nil, Type:TypeBool}`.
- **single quantifier with parameter in source**:
  `RETURN single(x IN $xs WHERE x > 0) AS r` — a synthetic pin only
  if the corpus does not carry it; otherwise the corpus verbatim
  form. This exercises the `$xs` source-list parameter mining path
  (§1.5) and is the only place parameters cross into the
  quantifier arm. **Provisional**: replaced by the verbatim form if
  the pinned-tag corpus supplies one; else added as
  `// AUTHORED:` per the parser_test.go layer-2 rule.
- **exists inline pattern in WHERE**:
  `MATCH (n) WHERE exists { (n)-->() } RETURN n`
  (`ExistentialSubquery1 [1]`) → one outer `NodeBinding{n}`, one
  outer `RefProjection{n, TypeNode}`; **no inner binding leaks**
  into the outer part's Bindings, byVar, pathBindings, or
  unwindBindings.
- **exists subquery form**:
  `MATCH (n) WHERE exists { MATCH (n)-->() RETURN true } RETURN n`
  (`ExistentialSubquery2 [1]`) → same outer shape.
- **exists nested**:
  `MATCH (n) WHERE exists { MATCH (m) WHERE exists { (n)-[]->(m)
  WHERE n.prop = m.prop } RETURN true } RETURN n`
  (`ExistentialSubquery3 [1]`) — one outer `NodeBinding{n}`;
  every inner binding at every depth suppressed.

New `mustReject` case:

- **pattern predicate in RETURN projection** (`Pattern1 [22]`) —
  `MATCH (n) RETURN (n)-[]->()` → `ErrPatternInProjection`. The
  Layer-2 rule (verbatim corpus for accept, verbatim-or-authored for
  reject) permits the corpus form because the TCK does exercise the
  fail-site.

The corpus provides no verbatim query for the four-parameter
quantifier scoping property (`WITH a MATCH (a) WHERE all(a IN ...) ...`
shadowing) at the pinned tag, so shadowing is not layer-2-pinned;
the property test's referential-integrity sweep guards the
"inner iteration variable never enters outer refs" invariant on
every parsed corpus query (§4.3).

`count`s update summary:

- `mustParse`: 66 → up to 73 (six new pins for Stage 11 quantifier /
  EXISTS shapes; the seventh — `single` over a `$param` source — is
  provisional pending corpus check).
- `mustReject`: 12 → 13 (one new pin for the gqlc-3r0 fold).
- Sentinels: 4 → 5 (`ErrPatternInProjection`).

The actual new-pin count is chosen against the pinned-tag corpus once
the parser is red-lit; the spec caps the shape (verbatim, ≤ 8 new
pins) rather than commits an exact number ahead of TDD.

### 1.10 Docs inline

- This spec.
- ADR 0003 gains a Stage-11 amendment note: the `Type` sum admits
  no new variants; the `Projection` sum admits no new variants;
  a fifth true-rejection sentinel `ErrPatternInProjection`
  enforces the bucket-1 rule "pattern predicates are legal only
  inside boolean positions." Scoping of quantifier iteration
  variables and `EXISTS {}` inner bindings is documented as a
  parser invariant, not a model surface — the model records no
  quantifier or subquery structure.
- ADR 0007 already names Stage 11 (predicate expressions); no
  header change.
- CONTEXT.md gains a **Predicate expression** entry describing the
  four quantifiers, `EXISTS {}`, and pattern predicates as
  scoped-locally, boolean-typed atoms whose structure the model
  does not carry.

Nothing downstream of the parser is built (no resolver, no codegen)
— ADR 0004.

---

## 2. Why one atomic cycle

The quantifier atoms, `EXISTS {}` atom, and the pattern-in-projection
rejection all touch the same predicate-expression surface: the four
quantifier arms and the `EXISTS {}` arm share the same scoping
discipline (a local refs sink for the filter body / inner bindings;
outer parameters mined against the outer scope), and the
pattern-in-projection rejection is the boundary rule of *the same*
"pattern-predicate atoms are boolean" story — a pattern predicate is
legal in WHERE / EXISTS position and rejected in projection
position, which is one contiguous rule.

Splitting the quantifier arm from the `EXISTS {}` arm would leave a
half-migrated `typeAtom` where quantifiers type as `TypeBool` but the
`EXISTS {}` beside them stays `TypeUnknown`; splitting the
`gqlc-3r0` fold out would leave the deferral note in the skiplist
naming Stage 11 with no delivery. Stage 11 lands as one branch.

Within the branch, the commit inventory (§7) separates spec from
parser changes from goldens so review can proceed incrementally
without re-running the whole diff at each step.

---

## 3. Model shape

### 3.1 No new model variant

Every Stage-11 shape lowers into existing model surface:

- Quantifier at projection position → `ExprProjection{refs,
  TypeBool}` (Stage 6 `ExprProjection`).
- Quantifier at WHERE / predicate position → parameters recorded as
  `ExprUse{TypeBool, ExprInPredicate}`; refs from the source
  expression enter the outer part; refs from the filter body do
  not.
- `EXISTS {}` at projection position → `ExprProjection{refs,
  TypeBool}` (refs is nil or the parameter-mined subset that reaches
  the outer scope — see §1.5).
- `EXISTS {}` at WHERE position → parameters recorded as
  `ExprUse{TypeBool, ExprInPredicate}`; no inner bindings enter the
  outer part.
- Pattern predicate at WHERE position → parameters recorded as
  `ExprUse{TypeBool, ExprInPredicate}`; typing is `TypeBool`
  (previously `TypeUnknown`).
- Pattern predicate at projection position → rejected with
  `ErrPatternInProjection`.

The `Query`, `Branch`, `Part`, `Binding` sum, `Type` sum, and
`Projection` sum are all unchanged. Nothing in the wire encoding
changes.

### 3.2 New sentinel

```
// ErrPatternInProjection rejects a pattern predicate at RETURN or
// WITH projection position — a bucket-1 parse-shape rule
// (ADR 0007 §I). Pattern predicates are legal openCypher only inside
// WHERE / EXISTS position; using one as a scalar column is
// SyntaxError:UnexpectedSyntax (TCK Pattern1 [22]/[23]). Stage 11.
var ErrPatternInProjection = errors.New("pattern predicate in projection position")
```

The sentinel is category-grained (position-shape rejection) rather
than shape-specific (which pattern shape triggered it), matching
the discipline of `ErrUnsupportedClause` and `ErrUnsupportedParameter`.

### 3.3 Listener state

`listener` gains one field:

```go
subqueryDepth int // Stage 11: >0 while EnterOC_ExistentialSubquery has fired without matching Exit
```

The counter is decremented on `ExitOC_ExistentialSubquery`; every
Enter* that collects clause state early-returns when the counter is
positive (§1.2). The counter is not exported and never enters the
wire encoding.

---

## 4. Parser widening

### 4.1 Suppression on the clause-collecting Enter handlers (§1.2)

Add the guard `if l.subqueryDepth > 0 { return }` at the top of:

- `EnterOC_Match`
- `EnterOC_With`
- `EnterOC_Return`
- `EnterOC_Unwind`
- `EnterOC_Create`, `EnterOC_Merge`, `EnterOC_Delete`, `EnterOC_Set`,
  `EnterOC_Remove`
- `EnterOC_InQueryCall`, `EnterOC_StandaloneCall`

The write-clause and CALL handlers still fire `ErrUnsupportedClause`
when they run at outer scope; inside `EXISTS {}` they early-return.

### 4.2 EXISTS enter/exit

Add:

```go
func (l *listener) EnterOC_ExistentialSubquery(c *gen.OC_ExistentialSubqueryContext) {
    l.subqueryDepth++
    // Mine parameters under the subquery: every $param inside gets an
    // ExprUse{TypeBool, ExprInPredicate}. Uses findParameters (Stage 0
    // helper) — walks the whole subtree.
    for _, p := range findParameters(c) {
        if !l.approved[p] {
            name := parameterName(p)
            if name == "" { continue }
            l.addParameterUse(name, p, query.NewExprUse(query.TypeBool{}, query.ExprInPredicate))
        }
    }
}

func (l *listener) ExitOC_ExistentialSubquery(*gen.OC_ExistentialSubqueryContext) {
    if l.subqueryDepth > 0 {
        l.subqueryDepth--
    }
}
```

Parameter mining runs at Enter; approvals reach every `$param` in
the subquery in a single sweep, so the pre-existing
`requireAllParametersApproved` gate (called from `mineInlineMap`)
does not fire spuriously for parameters mined by the EXISTS entry.

### 4.3 `typeAtom` — quantifier arm (§1.1)

Add the new arm before the existing catch-all `OC_ExistentialSubquery`
/ `OC_PatternPredicate` / `OC_PatternComprehension` branch:

```go
case a.OC_Quantifier() != nil:
    return l.typeQuantifier(a.OC_Quantifier(), refs)
```

`typeQuantifier` (new helper in `typing.go`):

```go
func (l *listener) typeQuantifier(q gen.IOC_QuantifierContext, refs *[]query.Ref) query.Type {
    filter := q.OC_FilterExpression()
    if filter == nil {
        return query.TypeBool{}
    }
    if idInColl := filter.OC_IdInColl(); idInColl != nil {
        if src := idInColl.OC_Expression(); src != nil {
            // Source list is outer-scope: refs flow to the caller's slice,
            // parameters get an ExprUse{sourceType, ExprInPredicate}.
            sourceType, _, params := l.typeExpressionMining(src)
            _ = sourceType // recorded on parameters below
            // Refs from the source list enter the caller's refs slice — the
            // typer already appended them via inner typeAtom, and we do not
            // want to lose them, so type again against the caller refs.
            _ = l.typeOr(src.OC_OrExpression(), refs)
            for _, p := range params {
                name := parameterName(p)
                if name == "" { continue }
                l.addParameterUse(name, p, query.NewExprUse(sourceType, query.ExprInPredicate))
            }
        }
    }
    if w := filter.OC_Where(); w != nil {
        // Filter body: mine parameters, DISCARD refs. The iteration
        // variable and any x.prop it touches must not enter the outer
        // refs slice.
        var local []query.Ref
        savedOuter := l.curPart.refs
        _, _, params := l.typeExpressionMining(w.OC_Expression())
        _ = local
        l.curPart.refs = savedOuter // typeExpressionMining pushes onto curPart.refs; restore
        for _, p := range params {
            name := parameterName(p)
            if name == "" { continue }
            l.addParameterUse(name, p, query.NewExprUse(query.TypeBool{}, query.ExprInPredicate))
        }
    }
    return query.TypeBool{}
}
```

The `savedOuter` restore mirrors `mineWhere` / `mineSortItemParameters`
— the same idiom the Stage-6 typer uses to discard refs a walk
touched without losing the walk's parameter side-effects.

### 4.4 `typeAtom` — existential subquery arm (§1.2)

Change the existing catch-all:

```go
case a.OC_ExistentialSubquery() != nil:
    // Parameters have already been mined at EnterOC_ExistentialSubquery
    // (§4.2). The atom itself types as TypeBool; refs from inside the
    // subquery are suppressed by the subqueryDepth counter and never
    // reach here.
    return query.TypeBool{}
```

### 4.5 `typeAtom` — pattern predicate arm (§1.3)

Change the existing catch-all:

```go
case a.OC_PatternPredicate() != nil:
    // Pattern predicates in WHERE / rich-expression predicate positions
    // are boolean; the atom's inner refs (`n` / `m`) are runtime-scope
    // and do not enter the outer refs slice — the model does not carry
    // pattern-predicate structure (ADR 0003).
    return query.TypeBool{}
```

`OC_PatternComprehension` stays at `TypeUnknown` (§1.5 rationale).

### 4.6 Projection-position pattern-predicate rejection (§1.4)

In `collectReturnItem` in `expr.go`, before the bare-atom
classifier runs:

```go
if isPatternPredicateAtom(item.OC_Expression()) {
    l.fail(fmt.Errorf("%w: %s", ErrPatternInProjection,
        originalText(l.ts, item.OC_Expression())))
    return
}
```

`isPatternPredicateAtom` (new helper in `shape.go`):

```go
// isPatternPredicateAtom reports whether the expression is a bare
// pattern-predicate atom in projection position — the shape Pattern1
// [22]/[23] rejects. The check collapses the precedence tower via
// nonArithmetic and inspects the resulting atom; a pattern predicate
// inside an arithmetic or comparison expression (unrepresentable but
// grammatically distinguishable) does not trigger the check because
// the enclosing shape is not "a bare pattern in projection position."
func isPatternPredicateAtom(e gen.IOC_ExpressionContext) bool {
    nae := nonArithmetic(e)
    if nae == nil {
        return false
    }
    a := nae.OC_Atom()
    return a != nil && a.OC_PatternPredicate() != nil
}
```

The check runs against `oC_ProjectionBody` — both `RETURN` and
`WITH` route through `collectProjection`, which delegates each item
to `collectReturnItem`, so both scenarios reject at one site.

### 4.7 Nothing new in `buildPart`, `classifyProjection`, or model exports

`buildPart` is unchanged (no new binding variants). `classifyProjection`
is unchanged (a pattern predicate is not a bare-atom variant it
classifies; the rejection fires before it runs). The model exports
(`query.Query`, `query.Type`, `query.Projection`) gain no new
variants.

---

## 5. Corpus and bucket-3 whitelist

`expressions/quantifier` and `expressions/existentialSubqueries`
enter `readCoreDirs`. Their combined ~110 scenario outlines are a
mix of positive-case quantifier / EXISTS demos (`Quantifier1 [1..14]`,
`Quantifier2 [1..15]`, `ExistentialSubquery1 [1..4]`, …) and
outcome-value negative scenarios (`Quantifier1 [15] Fail on type
mismatch`, `Quantifier2 [16] Fail on type mismatch`, …). Every
negative scenario is categorically bucket-3 via `isBucketThreeDir`
(the dir is under `expressions/`) — no per-scenario skiplist entries
needed.

The `expressions/pattern` dir's `Pattern1 [22]/[23]` skiplist entries
are **removed** (§1.4). Those scenarios must now actually reject
with `ErrPatternInProjection` — the acceptance suite gates on that
transition automatically.

The other Stage-6 / Stage-8 skiplist entries covering aggregate-
position semantic rules and pattern-scope rules all stay unchanged.

Layer-2 rule (Stage 1 §6). Stage 11 adds the `mustParse` cases §1.9
names and one `mustReject` entry.

`TestSentinelReachability` runs against the five-sentinel set (the
new `ErrPatternInProjection` is exercised by the new mustReject).

---

## 6. Definition of done for Stage 11

1. `stage-11-quantifiers` lands green and independently mergeable;
   `master` is green if it lands solo.
2. `just test` green: query-package unit tests (unchanged;
   `query.Query` shape holds), the cypher-package parser tests, the
   `mustParse` pins, the acceptance / orphan / reachability suites,
   the property tests (with the iteration-variable non-leak
   invariant verified by the existing referential-integrity sweep —
   §4.3).
3. `just lint` green: zero issues.
4. `just fmt-check` green: zero diffs.
5. Layer-1 godog count rises by the quantifier + existential dirs'
   ~110 scenarios, less the runtime-value scenarios that ride
   bucket-3 categorical accept. Zero FAIL is mandatory. `Pattern1
   [22]/[23]` transition from PENDING (skiplist) to PASSING (real
   rejection).
6. Documentation: this spec; CONTEXT.md entry for **Predicate
   expression**; ADR 0003 note.
7. Beads: `gqlc-665` closed; `gqlc-3r0` closed as folded.

---

## 7. Commit inventory (single branch `stage-11-quantifiers`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec + ADR 0003 note + CONTEXT.md entry (docs land in the branch, matching the DoD) |
| parser (red) | Failing `mustParse` pins for quantifier / EXISTS shapes; failing `mustReject` pin for gqlc-3r0; new `ErrPatternInProjection` sentinel added but no fail-site yet (so pin fails) |
| parser (green) | `typeAtom` quantifier / EXISTS / pattern-predicate arms; `subqueryDepth` counter; `collectReturnItem` pattern-predicate rejection; `typeQuantifier` helper; goldens regenerated for scenarios newly parse-green |
| unlock (dir + skiplist) | `readCoreDirs` gains `expressions/quantifier` + `expressions/existentialSubqueries`; delete `Pattern1 [22]/[23]` skiplist entries; goldens audited |

Each commit is green in isolation of the ones after it — the parser
red commit adds the sentinel and pins that fail; the parser green
commit adds the arms; the unlock commit wires the dirs.

---

## 8. Weakest points recorded honestly (per ADR 0004)

**The most fragile part of Stage 11 is the scoping guarantee — that
no inner binding of an `EXISTS {}` reaches the outer part.** The
guarantee rests on a **negative** property: after `EnterOC_ExistentialSubquery`,
every clause-collecting Enter* handler must early-return until
`ExitOC_ExistentialSubquery`. The failure mode is silent: a leaked
binding surfaces as a phantom `NodeBinding` / `EdgeBinding` /
`PathBinding` / `UnwindBinding` in the outer part's Bindings list,
which the resolver would then try to type against the schema — with
the wrong scope. The Stage-11 mustParse pins for `ExistentialSubquery1
[1]`, `ExistentialSubquery2 [1]`, and `ExistentialSubquery3 [1]`
each assert the outer Bindings list contains ONLY the outer `n`, so
a regression on any of the six suppression sites would fail the pins
loudly. The parser property test also crosschecks referential
integrity: an inner-only variable name in the outer refs list would
fail the existing sweep. Together these guard the invariant without
requiring a new property helper.

**A subtler variant of the same failure is the parameter-mining
correctness under `EXISTS {}`.** The parameter walk runs at
`EnterOC_ExistentialSubquery` and mines every `$param` in the
subtree; if a later Enter handler tried to mine the same subtree
again after being un-suppressed, we would double-count. The
suppression counter prevents that — no other Enter runs the
subquery — but this depends on the ANTLR walk visiting Enter
handlers depth-first, which is standard but load-bearing here.
A guard against re-entry would be to check `l.approved[p]` before
appending an `ExprUse` in the mining sweep, which the code does
via `addParameterUse`'s idempotent-uses-append (a `$p` seen twice
just accumulates a second use on the same Parameter — dedup by
name is enforced). The invariant this holds: a `$param` in an
`EXISTS {}` sees one `ExprUse{TypeBool, ExprInPredicate}`, not
two. The `mustParse` pins on nested-EXISTS shapes (`ExistentialSubquery3
[1]`) exercise this end-to-end.

The lesser risks, recorded for completeness:

- **Correlated outer names in `EXISTS {}` are not verified.** A
  reference to an outer variable inside `EXISTS {}` (`WHERE exists
  { (n)-->() }` where `n` is outer-bound) is a scope-correct
  correlation the parser does not check — the parser suppresses
  every inner binding, but it does not check that inner references
  to outer names resolve. This matches the ADR 0005 posture: the
  engine re-executing the original text raises an
  `UndefinedVariable` if an inner reference names nothing in the
  outer scope. If the query author writes `WHERE exists {
  (undefined)-->() }`, we do not reject; the engine does.
- **Quantifier-source-list refs are typed twice.** The
  `typeQuantifier` helper calls `typeExpressionMining` (which pushes
  refs onto `curPart.refs`) and then `typeOr` against the caller's
  refs slice, to make sure refs from the source list flow into the
  outer part. The second pass is a re-walk of the same subtree, so
  the work is O(2n) on the source expression size. For a
  parameter-only source (`x IN $xs`) the extra walk is trivial; for
  a longer literal source (`x IN [1,2,3,4,5,6,7,8]`) it is still
  cheap. Choosing the two-pass form over threading refs through
  `typeExpressionMining` avoids widening the mining signature —
  fine at Stage 11's cost model, but if Stage 12's writes press
  the hot path we would consolidate. A second consequence of the
  two-pass form: at RETURN / WITH projection position, every source
  ref (`n` in `RETURN all(x IN n.tags WHERE x > 0)`) is appended to
  `curPart.refs` twice — once by each pass. `buildPart`'s dedupe
  step collapses them into one entry today, so the observable model
  is unchanged, but a downstream consumer that counts refs (a code
  generator emitting one bind per ref) would see the double-write
  without dedupe. Pinned by ADR 0005 (the resolver runs the
  original text, not model refs), but noted here so a Stage-12
  refs-widening does not build on the double-write invariant.
- **Pattern predicates inside a quantifier's filter body are
  double-nested.** A predicate like `all(x IN xs WHERE (x)-->())`
  types the WHERE as `TypeBool` (matches the pattern predicate
  rule), and the inner pattern's `x` refers to the iteration
  variable, not to an outer binding. Because the filter-body
  refs are discarded (§1.1), the inner `(x)-->()`'s ref is not
  recorded — correctly. The property test would flag a leak if
  one occurred. No corpus scenario at the pinned tag exercises
  this composition; the pin set covers each piece separately.
- **Pattern predicates in `WITH` / `RETURN` projection position
  that hide one level up from a bare atom** are not rejected. The
  `isPatternPredicateAtom` check runs against the expression's
  non-arithmetic atom via `nonArithmetic` — a pattern predicate
  wrapped in another shape climbs above the check. Four confirmed
  hole shapes at Stage 11:
    - `WITH (n)-->() = true AS x` — a comparison of a pattern
      predicate to a boolean. The comparison expression is not a
      bare atom; `classifyRichExpression` runs and produces an
      `ExprProjection{refs:[], TypeBool}` — the inner pattern's
      `n` is silently dropped from refs (§4.5 arm).
    - `WITH NOT (n)-[]->() AS x` — a unary-NOT over a pattern
      predicate. Same result: `ExprProjection{refs:[], TypeBool}`,
      `n` silently dropped.
    - `WITH ((n)-[]->()) AS x` — a parenthesised pattern predicate.
      `nonArithmetic` unwraps the paren once, so this shape is
      caught by `isPatternPredicateAtom` today (verify against the
      corpus if it appears); if a future ANTLR update reshuffles
      the atom-vs-parenthesised distinction, this could regress.
    - `WITH [(n)-[]->()] AS x` — a pattern predicate inside a list
      literal. Not a bare atom; `classifyRichExpression` runs and
      the list types as `TypeUnknown`, again dropping `n` from
      refs.
  The current narrow shape is what `Pattern1 [22]/[23]` exercises
  and what gqlc-3r0's charter names ("Pattern1 [22]/[23]") —
  widening the rejection to climb the precedence tower would be a
  scope creep beyond the fold, but the shapes are recorded here so
  a future TCK tag or downstream consumer press does not surprise
  a reviewer. The corpus does not currently exercise any of the
  four at the pinned tag.
- **The `expressions/quantifier` and `existentialSubqueries` dirs
  join `expressions/*` for bucket-3 categorical negatives.** If a
  future TCK tag adds a *positive* runtime-error scenario in those
  dirs whose parse-shape the current parser would reject, the
  categorical rule would over-accept. The dirs' current
  negative-scenario set is entirely value/type errors
  (`InvalidArgumentType`, `TypeError`), which is exactly the
  bucket-3 target set — so the categorical rule holds today. A TCK
  bump audit should confirm this posture still fits.
