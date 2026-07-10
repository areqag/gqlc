# Stage 13 spec — Cypher query parser: MERGE

The implementation brief for Stage 13 of the Cypher implementation of
`query.Parser`. Thirteenth model evolution after Stage 12 per ADR 0004
(test-first, evolving until feature-complete), under the curation
discipline of ADR 0003 and the type-interface boundary of ADR 0005.
Stage 13 is the eighth stage of the ADR-0007 pre-freeze expansion
beyond the read core. It **adds one write clause**: `MERGE`. `MERGE`
is its own stage because the read/create alternation and its two
optional ON action branches (`ON MATCH SET …`, `ON CREATE SET …`) are
a distinct modelling decision from Stage 12's flat write-effect
variants, and a wrong representation would silently drop the ON-branch
pairing on the wire — a bug the model must prevent by construction.

MERGE is a query-side **effect** on the same axis as CREATE / DELETE /
SET / REMOVE: it produces zero result columns of its own and flips
`Query.StatementKind` to `StatementWrite`. Stage 12's projection-less
part relaxation carries verbatim. Stage 13 introduces one new Effect
sum variant (`MergeEffect`), one new sealed sub-sum
(`SetEffect` — implemented by exactly the three existing SetEffect
variants), and — invisibly to callers — one widening of parameter/ref
mining inside inline maps (§4.5) that Stage 12 already needed but
Merge1[11] is the first-in-corpus scenario to press.

The Stage 13 patterns riding through `EnterOC_Merge` share the same
`collectPattern` path CREATE uses (§4.1 of Stage 12): every binding
`MERGE`'s pattern introduces enters `curPart.bindings` as a MATCH-or-
CREATE binding indistinguishable from a MATCH one at the type-
interface boundary — the MERGE-vs-MATCH distinction is on the
`MergeEffect` (which bindings this clause introduced), not per-binding.

This document is a **delta** against Stages 0–12 (referenced
individually where relevant); everything not stated here carries over
verbatim. Sections appear here only where Stage 13 changes something.

Tracking: bead `gqlc-xxx` (GitHub #NN — filled at branch-cut time).
Lands as one graphite branch (`stage-13-merge`) with separated commits
(spec + docs → parser red → parser green → TCK dir unlock + goldens →
inline-map ref widening + fallout re-key), independently mergeable as
a whole: `just test` is green if this branch lands on `master` alone
(AGENTS.md stacked-branch invariant).

---

## 1. Deliverables

### 1.1 `MergeEffect` — dedicated Effect variant (§3.2)

`Effect` gains a sixth (public) variant, `MergeEffect`, carrying:

- `Variables []string` — the pattern's bindings this MERGE introduced
  (identical semantics to `CreateEffect.Variables`: the delta over
  `curPart.bindings` after `collectPattern` runs, so a variable already
  bound by a prior clause is not repeated).
- `OnMatch []SetEffect` — ordered SetEffects from the `ON MATCH SET …`
  action (empty slice if no `ON MATCH` clause is present).
- `OnCreate []SetEffect` — ordered SetEffects from the `ON CREATE SET …`
  action (empty slice if no `ON CREATE` clause is present).

**Why a dedicated variant, not a repurposed CreateEffect.** A MERGE is
semantically a *match-or-create* alternation: the engine attempts
match first, and only creates on miss. Representing it as
`CreateEffect` would erase the "match this if you can" half — a wrong
concrete representation, strictly worse than an honest new variant.
Callers walking `Part.Effects` need to distinguish "this clause
definitely creates" from "this clause creates iff nothing matches";
collapsing them would silently drop that distinction (Q1 fold-in).

**Why not a plain `[]Effect` for `OnCreate` / `OnMatch`.** Grammar
truth: `oC_MergeAction` contains ONLY `oC_Set` — no CREATE, no
DELETE, no REMOVE, no nested MERGE. A `CreateEffect` inside `OnCreate`
is grammatically impossible. Declaring the field as `[]Effect` would
make an illegal state representable: a caller could hand-construct a
`MergeEffect{OnCreate: []Effect{CreateEffect{...}}}` that no parse
input can produce, and the model would honour it — losing the illegal-
states-unrepresentable discipline the rest of the sum enforces. The
sealed sub-sum `SetEffect` is the type-level fix (Q2 fold-in).

### 1.2 `SetEffect` sealed sub-sum (§3.2)

A new interface, next to `Effect`:

```go
type SetEffect interface {
    Effect
    isSetEffect()
}
```

Implemented by EXACTLY the three existing SetEffect variants:
`SetPropertyEffect`, `SetEntityEffect`, `SetLabelsEffect`. Each gains
one unexported marker method `isSetEffect()` on its value receiver;
no other field or accessor changes. Callers still receive
`SetPropertyEffect` etc. as concrete types when walking a `[]Effect`
slice, so the change is strictly additive at the wire and at the
existing consumer boundary.

**Why the marker method (not a plain type-assertion switch at every
consumer).** `MergeEffect.OnMatch` / `OnCreate` are `[]SetEffect`, so
the compiler enforces at the type level that only Set-family variants
appear in those slots — impossible to accidentally append a
`CreateEffect` or a `DeleteEffect`. A type-assertion switch inside
`NewMergeEffect` would work at runtime but push the invariant into
runtime error paths, whereas the sealed sub-sum makes it a compile-
time invariant. If a future stage widens `oC_MergeAction` to include
non-SET clauses (unlikely; the grammar is stable), the sealed sub-sum
would either grow to include those or a new sub-sum would join
`SetEffect` — either way, the model surface documents what the
grammar admits.

**Concrete Go obstruction to flag if it appears.** The `Effect`
interface today is public (`isEffect()` is unexported but the
interface is exported). `SetEffect` follows the same pattern:
exported interface, unexported marker method. If any downstream
consumer (query package, wire round-trip, JSON decode) writes a
value-receiver method that would need to switch on the concrete Set
type in a way `[]Effect` handled but `[]SetEffect` does not (e.g., a
consumer that walks `Part.Effects` and expects to freely append into
`OnMatch`), the spec's `[]SetEffect` posture would need to relax to
`[]Effect` with a runtime-invariant NewMergeEffect check. No such
consumer exists at the parser layer today — the write path is
listener → NewMergeEffect → MarshalJSON, and every append is at the
listener where the concrete type is known statically. Report the
obstruction and fall back to `[]Effect` + constructor validation ONLY
if a real one materialises during implementation.

### 1.3 Wire encoding — `MergeEffect` (§3.3)

Effect tag: `"merge"`. Fields (following existing slice-marshalling
convention — A2 fold-in):

- `"variables": [string...]` — empty slice marshals as `null`,
  mirroring `CreateEffect.Variables`.
- `"onMatch": [SetEffect...]` — empty slice marshals as `null`,
  mirroring `DeleteEffect.Targets` / `DeleteEffect.Refs`. Every
  element carries its own `"kind"` discriminator
  (`"setProperty"` / `"setEntity"` / `"setLabels"`) — no new tag
  space is introduced.
- `"onCreate": [SetEffect...]` — same posture as `"onMatch"`.

The empty-slice-as-null posture is the existing Stage-12 convention;
Stage 13 does not invent a new empty-vs-null story for the two ON
slots. A merge with no ON branches marshals as
`{"kind": "merge", "variables": [...], "onMatch": null, "onCreate": null}`.

### 1.4 Inline-map ref widening (§4.5)

Stage 12's `mineInlineMap` mines PARAMETER uses from an inline
property map (`(n {name: $p})` → `PropertyUse{Ref{n, name}}` on
`$p`), but it does NOT record the map's VALUE-side variable/property
references onto `curPart.refs`. So a query like

```cypher
MATCH (person:Person)
MERGE (city:City {name: person.bornIn})
```

(Merge1[11] verbatim) records `person` and `city` as bindings but
leaves the `person.bornIn` reference on the `city`'s inline map
absent from `curPart.refs`. A downstream part reference would not
notice — `buildPart`'s referential-integrity sweep does not press —
but the wire shape silently drops information the query pins,
undetected. Stage 12 shipped this way because no in-corpus write
scenario pressed the shape at the pinned tag; Stage 13's Merge1[11]
is the first-in-corpus scenario to press it.

**Fix.** `mineInlineMap` (Stage 13 amend) walks each map value and,
for every value that is a bare variable, `var.prop`, or a rich
expression, records the same refs `typeExpressionMining` would onto
`curPart.refs`. Concretely: after the parameter-key loop, for every
value expression NOT already handled as a `$param` key, route it
through `typeExpressionMining` (which returns
`(Type, []Ref, []antlr.Tree)`), append the refs to `curPart.refs`,
and forward the parameters through `addParameterUse` with a
`PropertyUse{Ref{variable, keyName}}` (the same shape the bare-
`$param` case records). This unifies the two paths — a `$param` value
and a `var.prop` value both record the same PropertyUse discipline —
and closes the ref-drop.

**Widening motivated by MERGE, applies to CREATE/MATCH too.** The
widening is in `mineInlineMap`, called from `collectNode` and
`collectEdge`; every MATCH, CREATE, and MERGE pattern with an inline
map inherits the fix. This is honest recording, not a MERGE-specific
carve-out. The Stage 12 spec disclosed the risk under §weakest-points
implicitly (parameter mining rides an existing path; value refs did
not); Stage 13 discloses it explicitly and lands the fix in-scope
because the MERGE wire-up cannot ship a golden that silently drops
the `person` ref for Merge1[11]. Q3 fold-in: verified probe (§8) and
widening committed.

**Layer-2 observability of the widening on the CREATE side (ruling
Q3(b)).** The widening's failure mode is symmetric — it must both
(a) accept-and-record for bound refs, and (b) reject unbound
refs. No wire-surface widening is needed to observe it: the
buildPart referential-integrity sweep already raises
`ErrUnboundVariable` on the widened refs list. So the CREATE-side
Layer-2 lock is a matched pair: a mustReject kill-probe on the
unbound path (`CREATE (a {name: b.c})` — parses silently today,
rejects with `ErrUnboundVariable` after the widening) and a
mustParse guard on the bound path (`MATCH (b) CREATE (a {name:
b.c}) RETURN a` — passes today via MATCH-side mining, must keep
passing after GREEN). See §1.8 for the exact pins; §5 for the
Layer-1 corpus counterpart (Create1[20], Create2[24] off the
skiplist).

**Existing goldens re-key (disclosure).** The widening flips every
pinned-corpus scenario whose CREATE/MATCH/MERGE pattern contains an
inline map with a variable-referencing VALUE. The audit was run
against every `When executing query:` docstring in every currently-
wired `readCoreDirs` entry (harvest primitive: identical to
`acceptance_test.go`'s `harvestExecutingQueries` — the same gherkin
parser, the same `isExecutingQueryStep` gate, so setup blocks
(`having executed:`) are correctly excluded). Each docstring was
parsed through the ANTLR grammar, and every OC_NodePattern /
OC_RelationshipDetail's OC_Properties → OC_MapLiteral was walked;
each value expression was inspected for an OC_Variable descendant.
Function-call map arguments like `datetime({year: 1984, ...})` and
projection-side maps (`RETURN {datetime: other}`) route through the
expression typer, NOT `mineInlineMap`, so they are correctly excluded
from the audit's scope. Result: **22 scenarios across 3 dirs**.

**Re-key delta (already-green goldens whose hash changes).** Only
Create6 falls into this category. Create6's 10 affected scenarios
mint `CREATE (:N {num: x})` / `CREATE ()-[r:R {num: x}]->()` with
`x` bound by a preceding `UNWIND`. Each gains the `x` ref on the
create part's refs list. Enumerated by name:

- `[3] Skipping and limiting to a few results after creating nodes …`
- `[4] Skipping zero result and limiting to all results after creating nodes …`
- `[5] Filtering after creating nodes affects the result set …`
- `[6] Aggregating in RETURN after creating nodes …`
- `[7] Aggregating in WITH after creating nodes …`
- `[10] Skipping and limiting to a few results after creating relationships …`
- `[11] Skipping zero result and limiting to all results after creating relationships …`
- `[12] Filtering after creating relationships …`
- `[13] Aggregating in RETURN after creating relationships …`
- `[14] Aggregating in WITH after creating relationships …`

Create6 total: **10 scenarios**. (Scenarios [1], [2], [8], [9] use
literal `{num: 42}` — no var ref, no golden change. Scenarios [15]+
do not exist; Create6 has 14 scenarios total.)

**Fresh-golden delta (no prior green golden, mints honestly at
Stage 13).** Every audit hit outside Create6 is currently either
(a) skiplisted as a bucket-3 negative and **comes OFF the skiplist
at Stage 13** because the widening now rejects them correctly via
`ErrUnboundVariable` (Create1[20], Create2[24] — bare-var unbound
scenarios), (b) currently PENDING via `ErrUnsupportedClause` on
MERGE (all Merge* scenarios AND both Unwind1 scenarios that call
MERGE inline), or (c) newly-wired at Stage 13 (the whole
`clauses/merge` dir). Full enumeration:

Un-skiplisted (posture flip: skip → pass; no golden minted because
these are negatives that raise `ErrUnboundVariable` — the
acceptance suite verifies the parse-fail):
- `clauses/create/Create1.feature [20] Fail when creating a node using
  undefined variable in pattern` (`CREATE (b {name: missing})`)
- `clauses/create/Create2.feature [24] Fail when creating a relationship
  using undefined variable in pattern` (`MATCH (a) CREATE
  (a)-[:KNOWS]->(b {name: missing})`)

Currently PENDING (parse-rejected today by MERGE's
`ErrUnsupportedClause`; parse-accepts at Stage 13 and mints a golden
that already includes the ref — no "re-key" because there is no
prior golden):
- `clauses/unwind/Unwind1.feature [6] Creating nodes from an unwound
  parameter list` — uses `MATCH (y:Year {year: event.year})` +
  `MERGE (e:Event {id: event.id})`; the MATCH's inline-map var-ref
  gains `event` on its match part's refs list — a MATCH-side gain
  that is a byproduct of the same widening.
- `clauses/unwind/Unwind1.feature [14] Unwind with merge`.

Newly-wired at Stage 13 (`clauses/merge`, so mint fresh):
- `clauses/merge/Merge1.feature [8] Merge should handle argument properly`
- `clauses/merge/Merge1.feature [9] Merge should support updates while
  merging`
- `clauses/merge/Merge1.feature [11] Merge should be able to merge
  using property of bound node` (archetype: `person.bornIn`)
- `clauses/merge/Merge1.feature [12] Merge should be able to merge
  using property of freshly created node` (archetype: `a.num`)
- `clauses/merge/Merge5.feature [14] Using list properties via variable`
- `clauses/merge/Merge9.feature [1] UNWIND with one MERGE`
- `clauses/merge/Merge9.feature [2] UNWIND with multiple MERGE`
- `clauses/merge/Merge9.feature [4] MERGE after WITH with predicate
  and WITH with aggregation`

**MATCH-side widening — no in-corpus re-key.** The widening applies
uniformly wherever `mineInlineMap` runs (MATCH / CREATE / MERGE),
because scoping the fix to writes only would leave the same
syntactic shape recording different refs depending on clause — an
inconsistent model that fails the "honest recording" bar. The
executing-query audit found ZERO MATCH-only scenarios currently
green with a pattern-inline-map var-ref value: Unwind1[6] contains
a MATCH with the shape but is currently PENDING via its inner
MERGE, so its golden mints fresh (not a re-key). No other pinned-
corpus MATCH scenario exercises the shape at the pinned tag. If a
future TCK bump adds one, that scenario's re-key is disclosed at
that stage.

**Audit methodology (reproducibility).** The audit script
mirrors `harvestExecutingQueries` exactly; a scratch test-only file
(`inlinemap_audit_test.go`, gated behind `STAGE13_AUDIT=1`) was
used to produce the enumeration above and is deleted after the
audit is signed off. Re-running the audit at any future stage takes
one command; disclosure preserves auditability.

### 1.5 EnterOC_Merge replaces its rejection with pattern collection

Stage 12's rejection handler:

```go
func (l *listener) EnterOC_Merge(*gen.OC_MergeContext) {
    if l.subqueryDepth > 0 {
        return
    }
    l.fail(fmt.Errorf("%w: MERGE", ErrUnsupportedClause))
}
```

is replaced with the collection handler (Stage 13 spec §4.1). The
`subqueryDepth > 0` early-return stays — a MERGE inside `EXISTS { ... }`
is suppressed at the parser boundary per Stage 11 §1.6, so
`writeSeen` does not flip, and the outer query stays a read (the
composition rule that rejects the write-in-subquery text is bucket 3
per the existing `[3] Full existential subquery with update clause
should fail` skiplist entry). Stage 13 amend: the writeSeen non-flip
for MERGE-in-EXISTS is pinned (§1.8 pin list) and disclosed under
§8 weakest-points as a deliberate walk-time semantic that a query-
wide static analyzer would need a second axis to see.

### 1.6 Sentinel status

Post-Stage-13, the sentinel roster:

- `ErrUnsupportedClause`: STAYS. Its remaining fail-site after Stage 13
  is `EnterOC_InQueryCall` and `EnterOC_StandaloneCall` (both Stage
  14). MERGE is retired from its reach.
- `ErrUnsupportedParameter`, `ErrUnboundVariable`,
  `ErrVariableKindConflict`, `ErrPatternInProjection`,
  `ErrNestedPropertyTarget`: UNCHANGED in meaning and reach.

`TestSentinelReachability` runs against the SAME six-sentinel set
Stage 12 established. The `mustReject` "write clause" pin (currently
`MERGE (n) RETURN n`, per Stage 12 §7's swap) pivots to CALL —
verbatim from `clauses/call/Call3.feature [1]` if a clean shape
exists at the pinned tag, else authored (Q5 fold-in). §1.8 lists
both the standalone and the in-query CALL pins (Q6 fold-in) — each
fail-site is exercised separately.

### 1.7 Corpus wiring

`readCoreDirs` gains one dir:

- `clauses/merge` — 9 feature files, 75 scenarios.

This dir is NOT under `expressions/*`, so `isBucketThreeDir` does
not categorically accept its negatives; every negative scenario
needs either an explicit skiplist entry with a bucket-3 rationale or
a real rejection.

The negative scenarios cluster into (survey pass):

- **Runtime-shape rules (bucket 3).** `MERGE (n)` after a MATCH that
  binds `n` to a value the engine cannot merge (an already-bound
  entity a MERGE cannot re-pattern) — `SyntaxError` variants that
  the engine raises when re-executing the original text. Each takes
  an enumerated skiplist entry with per-scenario rationale.
- **Parameter-as-predicate (bucket 1 via existing sentinel).**
  `MERGE (n $param)` — a parameter as the whole predicate map,
  same `ErrUnsupportedParameter` rejection the MATCH side already
  produces. Merge1[16] is the archetype. No new skiplist entry;
  parse-rejects, `mustReject` optionally pins the shape (§1.8).
- **Undefined variable in ON MATCH / ON CREATE (bucket 3).**
  Merge3[5], Merge3[8]-ish shapes: `MERGE (n) ON MATCH SET x.num = 1`
  where `x` is not bound. `SyntaxError:UndefinedVariable`; parses
  under the existing ON-action collection because SET's rich typer
  does not currently gate against unbound vars (that discipline is
  the resolver's job, ADR 0005). Skiplisted per-scenario with
  bucket-3 justification citing `isBucketThreeError`.
- **Null-property MERGE (bucket 3).** `MERGE ({num: null})` —
  the property literal is legal at parse but the engine's MERGE
  rules reject a null-valued key. Bucket 3, per-scenario entry.

**No ON-SET-with-pattern-RHS scenario in the corpus (survey
finding).** A `MERGE (a) ON MATCH SET a.foo = (:Label {…})` would be
a pattern-in-projection sentinel target (Stage 11), so it would
either parse-reject via `ErrPatternInProjection` (the outer SET's
value expression carries the pattern) or need a bucket-3 skiplist
entry with an accept-and-defer rationale. The Stage 13 audit
(Q7 fold-in) grepped for `ON \(MATCH\|CREATE\) SET.*=.*(` in
`clauses/merge/*.feature` and found zero hits — the exposure does
not materialise at the pinned tag. If a future TCK bump adds one,
its skiplist entry belongs in **bucket 3 (accept-and-defer)** —
Stage 12's family-[24] parallel — and the terminology is
"bucket-3 accept-and-defer" (a parse-shape rule the type interface
does not carry). "Bucket-1" means parse-reject and does NOT apply
here (Q7 terminology correction).

The precise per-scenario enumeration lands with the parser-green /
unlock commit after a red-lit survey pass; the spec commits to the
enumeration shape (one entry per scenario, rationale citing ADR
0007 bucket, sentinel absence, and TCK error class), not the exact
list.

### 1.8 Layer-2 pins

New `mustParse` cases exercising the Stage-13 shapes. Every entry is
verbatim from the TCK unless marked `// AUTHORED:` per the
parser_test.go layer-2 rule.

- **MERGE bare node** (`Merge1 [1]`): `MERGE (a)` — a projection-
  less write, one named NodeBinding, zero Returns, one MergeEffect
  with `Variables: ["a"]`, empty `OnMatch`, empty `OnCreate`,
  StatementWrite.
- **MERGE labelled node** (`Merge1 [2]`): `MERGE (a:TheLabel)` — one
  named NodeBinding with the label, one MergeEffect
  `Variables: ["a"]`.
- **MERGE with inline map referencing bound var** (`Merge1 [11]`):
  the two-clause MATCH+MERGE shape from §1.4. Pins the ref-widening
  fix: the MERGE part's refs list carries `person` (from
  `person.bornIn`); the MergeEffect carries `Variables: ["city"]`.
- **MERGE with ON MATCH SET labels** (`Merge3 [1]`): `MERGE (a) ON
  MATCH SET a:L` — one MergeEffect with an OnMatch slice of one
  `SetLabelsEffect{"a", ["L"]}`, zero OnCreate.
- **MERGE with ON CREATE SET labels** (`Merge2 [1]`): `MERGE (a) ON
  CREATE SET a:Foo` — mirror shape, OnCreate populated.
- **MERGE with both ON branches** (`Merge4 [1]`): both branches
  populated, distinct SetEffect payloads.
- **MERGE followed by RETURN** (`Merge1 [3]` or survey):
  `MERGE (a) RETURN a` — a MergeEffect + a RefProjection,
  StatementWrite.
- **AUTHORED: MERGE inside EXISTS does not flip StatementKind**
  — `MATCH (n) WHERE exists { MERGE (m) RETURN true } RETURN n`.
  Pins the writeSeen non-flip: the outer query's StatementKind
  stays `StatementRead`, the inner MERGE is suppressed at
  `subqueryDepth > 0`, and no MergeEffect appears anywhere in the
  model. This is the same posture Stage 11 documents for MATCH-
  after-EXISTS and Stage 12 documents for the write set;
  Stage 13 pins it for the MERGE variant explicitly because it
  exercises the "the outer query is a read despite a nested write
  clause" invariant on the newest write shape.
- **AUTHORED: branch-leak kill-probe for the two-level effects
  slot** — `MERGE (n) ON CREATE SET n.a = 1 SET n.b = 2`. Pins
  exactly one `SetPropertyEffect` inside `MergeEffect.OnCreate`
  (target `Ref{n, a}`, value type `TypeInt`) AND exactly one
  outer `SetPropertyEffect` on the same part (target `Ref{n, b}`,
  value type `TypeInt`). The two SETs sit adjacent in walk order:
  the first is inside the ON CREATE action's inner scope, the
  second is a top-level SET clause on the outer part. A missing
  save/restore around `curPart.effects` in `collectMergeAction`
  (§4.2) would either (a) leak the ON-CREATE SetEffect out to
  `Part.Effects` as a peer to the MergeEffect (double-recording
  `n.a = 1`) or (b) capture the OUTER SetEffect into `OnCreate`
  (silently dropping the top-level SET into the branch). Merge4[1]
  pins the branch-populated shape but not this leak — it does not
  place a top-level SET adjacent to an ON action in the same
  query, and the corpus contains no such shape at the pinned tag.
  This is a Layer-2 pin, not a review-time hope: the kill-probe
  goes in as pinned truth.
- **AUTHORED: CREATE-side bound inline-map var-PROP guard**
  — `MATCH (b) CREATE (a {name: b.c}) RETURN a`. Pins the shared
  `mineInlineMap` fix on the CREATE side at Layer 2 for the
  var-PROP shape when the referenced var IS bound (from the
  preceding MATCH): the create part's refs list carries `b` (from
  the value expression `b.c` via `typeExpressionMining` on the
  inline-map value), one CreateEffect with `Variables: ["a"]`, one
  RefProjection for `a`, StatementWrite. **This pin passes in RED
  and must keep passing after GREEN** — it is a regression lock
  against over-rejection by the widening, not a kill-probe. The
  Create6 re-keyed goldens exercise the shared fix at Layer 1 via
  bare-var shapes (`{num: x}`); this Layer-2 pin locks the shared
  fix at the var-PROP shape (which Create6 does not exercise) —
  a future regression that widens `mineInlineMap` into rejecting
  bound refs by mistake fails at `TestMustParse` before touching
  goldens. `b` is bound from the preceding MATCH, so the widened
  refs list on the CREATE part carries `b` and the buildPart
  referential-integrity sweep is satisfied without any sentinel —
  the pin asserts the accept-and-record shape directly.

Exact count: 7 verbatim + 3 authored = 10 new mustParse pins
(EXISTS-suppression StatementKind non-flip, branch-leak kill-probe,
CREATE-side bound var-PROP guard). No optional/ceiling posture —
the spec commits to this list. If RED turns up a red-lit shape
that argues for an addition or a swap, the spec is amended before
RED lands.

RED-phase posture, per pin:
- 7 verbatim MERGE pins + branch-leak kill-probe → **FAIL in RED**
  with `unsupported clause: MERGE` (8 mustParse failures total),
  since `EnterOC_Merge` still raises `ErrUnsupportedClause`
  outside `EXISTS { ... }`.
- EXISTS-suppression pin → **passes in RED** as a guard: the
  outer MATCH parses; `EnterOC_ExistentialSubquery` opens
  `subqueryDepth`; the inner MERGE's `subqueryDepth > 0`
  early-return short-circuits BEFORE `l.fail`, so no sentinel is
  raised; `writeSeen` stays false and the outer query stays
  `StatementRead`. This is the same suppression path Stage 11
  §1.6 relies on for MATCH-in-EXISTS — no suppression bug, just
  the honest Stage-11 posture exercised on the newest write
  shape.
- CREATE-side bound var-PROP guard → **passes in RED** as a
  regression lock (documented above).

New `mustReject` pins (Stage 13):

- **grammar-reject: MERGE with multiple pattern parts** (A1 fold-in)
  — `MERGE (a), (b)`. `oC_Merge` grammar admits only a SINGLE
  `oC_PatternPart` (spec §4.4 code cite), so this fails at the
  ANTLR grammar level BEFORE the listener runs. No sentinel is
  raised (the failure is a parse error, not a domain sentinel);
  the pin lives under a `mustRejectGrammar` bucket following the
  FOREACH precedent (§7 commit inventory notes the exact pin
  taxonomy). If a `mustRejectGrammar` map does not yet exist
  (Stage 12 used a plain `mustReject` for the FOREACH pinned tag),
  the pin joins `mustReject` with `want: nil` or `want: parseErr`
  per the existing convention — the spec accepts either shape,
  with parity to whatever Stage 12 chose.
- **write clause → CALL** (`mustReject` pivot from MERGE to CALL,
  Q5 fold-in): the pinned rejection moves from `MERGE (n) RETURN n`
  (Stage 12 posture) to a CALL shape. Two pins for the two
  fail-sites (Q6 fold-in):
  - **standalone CALL**: `CALL test.my.proc(42)` — verbatim from
    `clauses/call/Call3.feature [1]`. Fail-site:
    `EnterOC_StandaloneCall`, `ErrUnsupportedClause`.
  - **in-query CALL**: `CALL test.my.proc(42) YIELD out RETURN out`
    — verbatim from `clauses/call/Call3.feature` (in-query shape).
    Fail-site: `EnterOC_InQueryCall`, `ErrUnsupportedClause`.
- **AUTHORED: unbound inline-map ref kill-probe** (ruling Q3(b)
  replacement) — `CREATE (a {name: b.c})` (no preceding MATCH; `b`
  is unbound). Verbatim archetype of `clauses/create/Create1.feature
  [20] Fail when creating a node using undefined variable in
  pattern` (which reads `CREATE (b {name: missing}) RETURN b`);
  the pinned query strips the RETURN and switches the var to
  `b.c` (var-PROP shape) to lock the var-PROP variant that
  Create1[20]'s bare-var shape does not cover. Fail-site:
  `mineInlineMap`'s value-side widening records `b` on
  `curPart.refs`; the buildPart referential-integrity sweep then
  raises `ErrUnboundVariable` because `b` is not in scope.
  **FAILS in RED**: today's `mineInlineMap` walks OC_MapLiteral
  values only for PARAMETER uses, never records `b`, and the
  buildPart sweep sees no unbound ref — the query parses
  silently. That is exactly the "silent info drop where parser
  could reject" blocker the widening fixes. **Passes after
  GREEN** as a textbook RED→GREEN pin. This kill-probe is the
  Q3(b) observable — no wire-surface widening is needed to
  observe the widening's effect, and the Layer-1 corpus gains
  Create1[20] + Create2[24] off the skiplist (§5) as the
  corpus-level counterpart.

`count`s update summary:

- `mustParse`: 88 → 98 (exactly +10; 7 verbatim + 3 authored per
  §1.8's list — the three authored pins are the EXISTS-suppression
  StatementKind non-flip, the branch-leak kill-probe, and the
  CREATE-side bound var-PROP guard).
- `mustReject`: 15 → 18 (net +3). Breakdown: retire 1 pin (the
  current "write clause" MERGE pin), add 2 pins (standalone CALL,
  in-query CALL), add 1 pin (grammar-reject `MERGE (a), (b)`),
  add 1 pin (unbound inline-map ref kill-probe) →
  15 − 1 + 2 + 1 + 1 = 18.
- Sentinels: 6 → 6 (no additions, no retirements — the unbound
  kill-probe reuses `ErrUnboundVariable` at the buildPart sweep).

**RED-phase expectation, exact**: `TestMustParse` fails on
exactly 8 pins (7 verbatim MERGE shapes + branch-leak kill-probe,
all with `unsupported clause: MERGE`). `TestMustReject` fails on
exactly 1 pin (the unbound inline-map kill-probe — parses
silently today because `mineInlineMap` does not walk value-side
refs). The remaining new pins pass in RED as guards:
EXISTS-suppression, CREATE-side bound var-PROP guard, standalone
CALL, in-query CALL, grammar-reject `MERGE (a), (b)`.

### 1.9 Docs inline

- This spec.
- ADR 0003 gains a Stage-13 amendment note: `Effect` sum widens
  from six to seven variants (`MergeEffect` joins); a new sealed
  sub-sum `SetEffect` (implemented by the three existing Set
  variants) enforces at the type level that only Set-family
  effects may appear inside `MergeEffect.OnMatch` / `OnCreate`.
  No new `Type` sum variant. No new `Projection` sum variant.
- ADR 0007 already names Stage 13 (MERGE); no header change.
- CONTEXT.md gains one new entry — **Merge** — describing the
  match-or-create axis, the two ON branches, and the sealed
  Set-sub-sum for the ON payloads. The existing **Effect** entry
  gains a note that the sum has widened to seven variants.

Nothing downstream of the parser is built (no resolver, no codegen)
— ADR 0004.

---

## 2. Why one atomic cycle

Splitting the model change (MergeEffect + SetEffect sub-sum) from
the parser wire-up (EnterOC_Merge collection + inline-map ref
widening) would leave the model with an Effect variant no parse path
produces — a wire shape with no fail-site exercising it,
untestable. Splitting the widening from the MERGE wire-up would
either (a) ship Merge1[11] with a silent-drop golden known-wrong at
merge time, or (b) skiplist Merge1[11] with a bucket-3 rationale
that does not honestly describe the situation (the drop is a parser
bug, not a runtime semantic below the boundary). Neither is
acceptable. Stage 13 lands as one branch.

Within the branch, the commit inventory (§7) separates spec from
red pins from parser changes from goldens so review can proceed
incrementally without re-running the whole diff at each step. The
inline-map widening lands as its own commit AFTER the MergE
wire-up, so the Create6 re-key delta is isolated (auditable in
one diff, not tangled with the MergeEffect model surgery).

---

## 3. Model shape

### 3.1 `MergeEffect` and `SetEffect` sub-sum

```go
// SetEffect is the sealed sub-sum of Effect implemented by exactly
// the three SET-family effect variants: SetPropertyEffect,
// SetEntityEffect, SetLabelsEffect. MergeEffect.OnMatch and
// MergeEffect.OnCreate carry []SetEffect (not []Effect), so the
// type system rejects a CreateEffect / DeleteEffect / MergeEffect /
// RemovePropertyEffect / RemoveLabelsEffect inside an ON action
// slot — matching the grammar's oC_MergeAction rule which admits
// only oC_Set.
type SetEffect interface {
    Effect
    isSetEffect()
}

func (SetPropertyEffect) isSetEffect() {}
func (SetEntityEffect)   isSetEffect() {}
func (SetLabelsEffect)   isSetEffect() {}

// MergeEffect represents one MERGE clause. Its Variables mirror
// CreateEffect.Variables (the delta over curPart.bindings). OnMatch
// and OnCreate carry the two optional ON action branches, sealed to
// SetEffect payloads by the grammar.
type MergeEffect struct {
    variables []string
    onMatch   []SetEffect
    onCreate  []SetEffect
}

func NewMergeEffect(variables []string, onMatch, onCreate []SetEffect) MergeEffect { ... }
func (e MergeEffect) Variables() []string { return e.variables }
func (e MergeEffect) OnMatch() []SetEffect { return e.onMatch }
func (e MergeEffect) OnCreate() []SetEffect { return e.onCreate }
func (MergeEffect) isEffect() {}
```

The smart constructor `NewMergeEffect` follows Stage 12's discipline:
each string in `variables` is validated as either empty (anonymous
pattern element the CREATE variant already accepts) or a
non-whitespace identifier; each entry in `onMatch` / `onCreate`
non-nil (a nil interface value would surface as a marshal-time
`{"kind": ""}` on the wire, which the constructor rejects with a
domain error — the discipline `NewCreateEffect` and Stage 12's
Set/Delete constructors already enforce for their fields).

No `Refs` aggregate on `MergeEffect` (Q4 confirmation): the nested
`SetEffect`s already carry their own `Refs`, and an aggregate on the
parent would duplicate the information and can drift. The two
levels of refs are recorded independently at their walk depth. The
pattern's own value refs (inline-map values, per §1.4 / §4.5) flow
into `curPart.refs` directly, so part-level referential integrity
covers both.

### 3.2 Wire encoding — Effect kind constants

`internal/query/query.go` gains one constant next to the existing
`effectKind*` bag (Stage 12 §3.3):

```go
const (
    effectKindCreate         = "create"
    effectKindDelete         = "delete"
    effectKindSetProperty    = "setProperty"
    effectKindSetEntity      = "setEntity"
    effectKindSetLabels      = "setLabels"
    effectKindRemoveProperty = "removeProperty"
    effectKindRemoveLabels   = "removeLabels"
    effectKindMerge          = "merge"  // Stage 13
)
```

`MergeEffect.MarshalJSON` emits `{"kind": "merge", "variables":
[...] | null, "onMatch": [...] | null, "onCreate": [...] | null}`.
Each element of `onMatch` / `onCreate` marshals through the existing
SetEffect variant's `MarshalJSON`, so the discriminator (`"kind":
"setProperty" | "setEntity" | "setLabels"`) is preserved. No new
tag space is introduced.

Empty slices marshal as `null` — the existing slice-marshalling
convention `CreateEffect.Variables` / `DeleteEffect.Targets` /
`DeleteEffect.Refs` follow. This is deliberate (A2 fold-in): the
two ON slots must not invent a new empty-vs-null posture.

### 3.3 Listener state additions

None. `rawPart.effects` already carries `[]query.Effect`;
`MergeEffect` appends to the same slice through the same path.
`writeSeen` is flipped by `EnterOC_Merge` at outer scope (identical
to the other four write clauses' handlers).

---

## 4. Parser widening

### 4.1 EnterOC_Merge collects one MergeEffect

```go
func (l *listener) EnterOC_Merge(c *gen.OC_MergeContext) {
    if l.subqueryDepth > 0 {
        return // Stage 11 §1.6: writes inside EXISTS { ... } are suppressed
    }
    before := len(l.curPart.bindings)
    l.collectPatternPart(c.OC_PatternPart(), false)
    if l.err != nil {
        return
    }
    var vars []string
    for i := before; i < len(l.curPart.bindings); i++ {
        vars = append(vars, l.curPart.bindings[i].variable)
    }
    var onMatch, onCreate []query.SetEffect
    for _, action := range c.AllOC_MergeAction() {
        eff, kind := l.collectMergeAction(action)
        if l.err != nil {
            return
        }
        if kind == mergeActionOnMatch {
            onMatch = append(onMatch, eff...)
        } else {
            onCreate = append(onCreate, eff...)
        }
    }
    l.curPart.effects = append(l.curPart.effects, query.NewMergeEffect(vars, onMatch, onCreate))
    l.writeSeen = true
}
```

`collectPatternPart` is the same helper CREATE / MATCH use for a
single `oC_PatternPart` (as opposed to `collectPattern`, which is
the whole-pattern comma-list entry). MERGE grammar admits only ONE
pattern part; using the singular helper documents the shape at the
call site and gives the grammar-reject pin from A1 its correct
fail-site (the grammar itself, not a listener assertion).

`collectMergeAction` walks the `oC_MergeAction` context: reads the
`ON MATCH` vs `ON CREATE` axis from the terminal, then routes the
inner `oC_Set` through the SAME `collectSetItem` dispatch Stage 12
uses. The set items produced for the ON action append to a LOCAL
slice, NOT to `curPart.effects` — they are payloads on the parent
MergeEffect, not siblings in the part's effects list. The refs the
inner SetEffects touch DO flow into `curPart.refs` via the existing
`collectSetItem` path (which appends the target's variable and the
value expression's refs), so `buildPart`'s referential-integrity
sweep covers ON-action refs verbatim (Q4 confirmation: ON-branch
value refs flow into curPart.refs for part-level referential
integrity — the sub-effects' Refs and the part-level refs list
serve orthogonal purposes).

`mergeActionOnMatch` / `mergeActionOnCreate` are a two-value axis
(a small int, matching Stage 12's `SetOp`). If the future ON grammar
gains a third alternative, the axis widens without disrupting
callers.

### 4.2 collectMergeAction

```go
type mergeActionKind int

const (
    mergeActionOnMatch mergeActionKind = iota
    mergeActionOnCreate
)

func (l *listener) collectMergeAction(action gen.IOC_MergeActionContext) ([]query.SetEffect, mergeActionKind) {
    kind := mergeActionOnCreate
    if action.MATCH() != nil {
        kind = mergeActionOnMatch
    }
    // collectSetItem produces a query.Effect via curPart.effects; we intercept
    // by snapshotting/restoring curPart.effects across the inner walk so the
    // Set items land in a local slice, not the top-level effects list.
    saved := l.curPart.effects
    l.curPart.effects = nil
    for _, item := range action.OC_Set().AllOC_SetItem() {
        l.collectSetItem(item)
        if l.err != nil {
            l.curPart.effects = saved
            return nil, kind
        }
    }
    collected := l.curPart.effects
    l.curPart.effects = saved

    out := make([]query.SetEffect, 0, len(collected))
    for _, e := range collected {
        se, ok := e.(query.SetEffect)
        if !ok {
            // Grammar rules this out (oC_MergeAction admits only oC_Set),
            // but a belt-and-braces guard flags a future grammar widening.
            l.fail(fmt.Errorf("internal: MERGE ON action produced non-Set effect %T", e))
            return nil, kind
        }
        out = append(out, se)
    }
    return out, kind
}
```

The save/restore of `curPart.effects` is the walk-time equivalent
of Stage 11 §1.6's `subqueryDepth` counter — collect into a local
scope, then hoist the payload up as a nested field on the parent
MergeEffect. The refs the inner SET items touch flow into
`curPart.refs` (which is NOT saved/restored) so part-level
referential integrity is preserved.

### 4.3 mineInlineMap value-side ref widening

```go
func (l *listener) mineInlineMap(variable string, p gen.IOC_PropertiesContext) {
    if p == nil {
        return
    }
    if p.OC_Parameter() != nil {
        l.fail(fmt.Errorf("%w: parameter as a whole property map", ErrUnsupportedParameter))
        return
    }
    m := p.OC_MapLiteral()
    if m == nil {
        return
    }
    keys := m.AllOC_PropertyKeyName()
    exprs := m.AllOC_Expression()
    for i := range keys {
        // Fast path: value is a bare $param → PropertyUse{Ref{var, key}}.
        if param, node, ok := parameterFromExpr(exprs[i]); ok {
            if variable == "" {
                l.fail(fmt.Errorf("%w: %s in an anonymous pattern element", ErrUnsupportedParameter, param))
                return
            }
            l.addParameterUse(param, node, query.NewPropertyUse(query.Ref{Variable: variable, Property: keys[i].GetText()}))
            continue
        }
        // Widening (Stage 13 §4.3): value is a var / var.prop / rich
        // expression. Route through the Stage-6 rich typer to record refs
        // onto curPart.refs, then forward any parameters through the same
        // PropertyUse{Ref{var, key}} discipline the fast path uses.
        _, refs, params := l.typeExpressionMining(exprs[i])
        // typeExpressionMining already pushes rich refs onto curPart.refs;
        // record a defensive local variable so the ref-widening intent is
        // visible at the call site. (No double-appending: the returned
        // refs slice is a copy for local consumption, not a re-append.)
        _ = refs
        for _, node := range params {
            name := parameterName(node)
            if name == "" {
                continue
            }
            if variable == "" {
                l.fail(fmt.Errorf("%w: %s in an anonymous pattern element", ErrUnsupportedParameter, name))
                return
            }
            l.addParameterUse(name, node, query.NewPropertyUse(query.Ref{Variable: variable, Property: keys[i].GetText()}))
        }
    }
    l.requireAllParametersApproved(m)
}
```

Notes on the widening:

- The fast-path (bare-`$param`) semantics do NOT change: an inline-
  map value that is a bare `$param` still records
  `PropertyUse{Ref{var, key}}` on the parameter, and no ref for the
  parameter itself flows to `curPart.refs` (a parameter is not a
  binding).
- The widening applies to every remaining value shape: a bare
  variable (`{name: person}`) records `Ref{person, ""}` via
  `typeExpressionMining`; a `var.prop` value (`{name: person.bornIn}`)
  records `Ref{person, "bornIn"}` (or the closest shape the typer
  emits — the Stage 6 rich typer already handles the property-lookup
  suffix); a rich expression (`{sum: person.age + 1}`) records the
  refs the arithmetic touches.
- Parameters under a rich value expression record
  `PropertyUse{Ref{var, key}}` — the SAME shape the fast path
  produces — because the parameter's ULTIMATE contract is that the
  parser records "this parameter fills the `var.key` property slot,"
  and the resolver upgrades from the schema post-freeze. This is a
  cleaner unification than Stage 12's `ExprUse{TypeUnknown,
  ExprInSetValue}` posture because the inline-map key gives the
  parser a concrete property target the SET-value case does not
  have (a SET value with a `$param` on the RHS has NO enclosing
  property key — the ExprUse posture is honest there because the
  parser cannot pin one).
- `requireAllParametersApproved` still fires at the end: a parameter
  nested in a list inside a rich value expression (e.g.,
  `{tags: [$t, $u]}`) is NOT approved by the loop (it does not
  appear as a `parameterFromExpr` match on the top-level expression
  and typeExpressionMining does not approve it), so
  `requireAllParametersApproved` catches it with
  `ErrUnsupportedParameter` — matching the Stage 12 discipline.

### 4.4 Grammar-reject pin (`MERGE (a), (b)`)

`oC_Merge : MERGE SP? oC_PatternPart ( SP oC_MergeAction )* ;` admits
exactly ONE `oC_PatternPart`. Two comma-separated pattern parts fail
at the ANTLR-generated parser, before the listener runs. The
`mustReject` pin (§1.8 above) exercises the grammar failure path.
Precedent: the FOREACH pinned tag exercises the same grammar-only
posture with `parse-error` semantics; the Stage 13 pin follows
whichever convention Stage 12 chose (either `mustRejectGrammar`
sibling map, or a `mustReject` entry with `want: nil` / a designated
parse-error sentinel).

### 4.5 EXISTS-subquery suppression preserved

Verbatim Stage 11 posture: `EnterOC_Merge`'s top-of-handler
`if l.subqueryDepth > 0 { return }` early-return is kept from Stage
12's rejection posture; only the body changes. Inside `EXISTS
{ ... }` a MERGE clause is grammar-legal, the outer handler
early-returns, and Stage 11's `[3] Full existential subquery with
update clause should fail` skiplist entry covers the composition
rule at bucket 3. The writeSeen non-flip is pinned separately (§1.8
AUTHORED pin: `MATCH (n) WHERE exists { MERGE (m) RETURN true }
RETURN n`).

**Interaction with `ErrPatternInProjection`.** A MERGE inside EXISTS
is a MERGE inside a WHERE-value expression on the outer part. Per
Stage 11 §1.6, `EnterOC_Merge` early-returns; the outer EXISTS
subquery's parameter mining runs once at `EnterOC_ExistentialSubquery`,
and the outer WHERE is untouched. `ErrPatternInProjection` fires only
when a pattern (a NodePattern or a PatternExpression) appears
directly inside a projection column (a `RETURN pat` or `WITH pat`);
a MERGE clause inside an EXISTS scope does NOT count as
"pattern-in-projection" — it counts as a write-inside-subquery,
which the bucket-3 skiplist entry Stage 11 pinned already handles.
The two sentinels do not race; Stage 13 does not need to add a new
composition rule.

### 4.6 `build()` populates `StatementKind` — no change

Stage 12's `build()` reads `writeSeen` and writes `StatementWrite`
if true. `EnterOC_Merge` flipping `writeSeen` folds directly into
that path; no `build()` change is needed.

### 4.7 `nameBoundAsUnwind` interaction with MERGE

MERGE reuses `collectPatternPart` (same helper as CREATE / MATCH),
which reaches `nameBoundAsUnwind` before appending a new binding.
For a MERGE under a prior UNWIND that bound a variable to a scalar,
this yields the same byVar-collision → `ErrVariableKindConflict`
path Stage 9 documents (§1.5 pattern-vs-unwind). Stage 13 does not
change this — a MERGE naming a scalar-bound UNWIND variable
rejects the same way a MATCH would.

---

## 5. Corpus and bucket-3 skiplist

`clauses/merge` enters `readCoreDirs`. Per §1.7, negatives cluster
into runtime-shape rules (bucket 3), the parameter-as-predicate
shape (`ErrUnsupportedParameter` at parse — already covered by the
MATCH-side discipline via `mineInlineMap`), and undefined-variable
inside ON action (bucket 3).

Per-scenario skiplist entries follow Stage 12's shape: one entry
per skipped scenario, each carrying an ADR-0007-bucket-3 rationale,
the sentinel that would have fired (or the absence thereof), and
the TCK error class. Exhaustive enumeration lands with the
parser-green / unlock commit after a red-lit survey pass.

**Skiplist removals (§1.4 fold-in).** The `mineInlineMap` value-side
widening (§4.3) makes two currently-skiplisted Create-dir negatives
reject correctly, so they come OFF the skiplist in the widening
commit:

- `[20] Fail when creating a node using undefined variable in pattern`
- `[24] Fail when creating a relationship using undefined variable in pattern`

Both are `SyntaxError:UndefinedVariable` scenarios whose query
mints an inline-map value reference to a name that is not bound
by any preceding clause. After the widening, `mineInlineMap`
records the unbound name on the create part's refs list; the
buildPart referential-integrity sweep then raises
`ErrUnboundVariable`, satisfying the acceptance-suite parse-fail
step. Suite-count delta from these two removals: **skip −2,
pass +2**. Their bucket-3 rationale in the current skiplist is
retired at the same commit that lands the widening — the
rationale is superseded because the parser now honestly rejects
the shape rather than accepting and deferring.

The `mustReject` `write clause` pin moves from MERGE to CALL:

```go
// standalone CALL — Call3 [1]
"write clause (standalone CALL)": {
    query: "CALL test.my.proc(42)",
    want:  cypher.ErrUnsupportedClause,
},
// in-query CALL — Call3 [1] YIELD-body
"write clause (in-query CALL)": {
    query: "CALL test.my.proc(42) YIELD out\nRETURN out",
    want:  cypher.ErrUnsupportedClause,
},
```

Both exercise `ErrUnsupportedClause` — the first at
`EnterOC_StandaloneCall`, the second at `EnterOC_InQueryCall`.
Stage 14 will retire both.

---

## 6. Definition of done for Stage 13

1. `stage-13-merge` lands green and independently mergeable;
   `master` is green if it lands solo.
2. `just test` green: query-package unit tests (additive
   `MergeEffect`, `SetEffect` sub-sum markers on Stage-12 variants),
   cypher-package parser tests, mustParse pins, acceptance / orphan
   / reachability suites, property tests.
3. `just lint` green: zero issues.
4. `just fmt-check` green: zero diffs.
5. Layer-1 godog count rises by `clauses/merge`'s ~75 scenarios
   (less the bucket-3 skiplist negatives) plus **2** create-dir
   scenarios flipping skip → pass (Create1[20], Create2[24] — off
   the skiplist per §5 / §1.4 skiplist-removals). Zero FAIL is
   mandatory.
6. Documentation: this spec; CONTEXT.md **Merge** entry; ADR 0003
   amendment note; the **Effect** entry's variant-count note.
7. Beads: `gqlc-xxx` closed.

---

## 7. Commit inventory (single branch `stage-13-merge`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec + ADR 0003 note + CONTEXT.md entries (docs land in the branch, matching the DoD) |
| parser (red) | **8** failing `mustParse` pins for the Stage-13 shapes (7 verbatim: MERGE bare, MERGE with label, MERGE inline-map with bound var, MERGE ON MATCH labels, MERGE ON CREATE labels, MERGE with both ON branches, MERGE + RETURN; 1 authored: branch-leak kill-probe `MERGE (n) ON CREATE SET n.a=1 SET n.b=2`) — all 8 fail with `unsupported clause: MERGE`. **2** additional mustParse pins pass in RED as guards: MERGE-in-EXISTS StatementKind non-flip (suppressed by `subqueryDepth > 0`), CREATE-side bound var-PROP guard `MATCH (b) CREATE (a {name: b.c}) RETURN a` (widened refs list satisfies referential-integrity today via MATCH-side mining; explicit lock against future over-rejection by the widening). Net mustParse: 88 → 98 (+10). **1** failing `mustReject` pin: unbound inline-map ref kill-probe `CREATE (a {name: b.c})` (parses silently today; widening makes it reject via `ErrUnboundVariable`). `mustReject` also: retire MERGE pin, add 2 CALL pins (standalone + in-query, both pass in RED as guards) and 1 grammar-reject pin `MERGE (a), (b)` (passes in RED as guard, ANTLR-level parse fail) — 15 → 18. `query.MergeEffect` + `query.SetEffect` sub-sum + Stage-12 marker methods added, but `EnterOC_Merge` still emits `ErrUnsupportedClause`, so the 8 MERGE mustParse pins fail as intended; `mineInlineMap` value-side widening not yet in, so the unbound kill-probe fails as intended |
| parser (green) | `EnterOC_Merge` collects the pattern via `collectPatternPart`; `collectMergeAction` walks ON actions with a save/restore around `curPart.effects`; `writeSeen` flipped at outer scope; goldens regenerated for `clauses/merge` scenarios newly parse-green (∼60 scenarios out of 75, less skiplisted negatives) |
| unlock (dirs + skiplist) | `readCoreDirs` gains `clauses/merge`; skiplist entries per bucket-3 negative with ADR 0007 rationale; acceptance-suite step behavior for the merge dir mirrors Stage 12's transitions (the `write clause` pin swap has already happened in the red commit) |
| widening (mineInlineMap + fallout) | `mineInlineMap` widens per §4.3 to record value-side refs into `curPart.refs` and mint `PropertyUse{Ref{var, key}}` for parameters nested in rich value expressions. **10 existing Create6 goldens re-key** ([3], [4], [5], [6], [7], [10], [11], [12], [13], [14] per the audit in §1.4) to include the `x` ref on their `create` part's refs list — 10 files listed by name in the commit message. Every other audit hit (2 Unwind1, 8 Merge) is fresh-mint on this branch (no re-key: Unwind1 is currently PENDING via MERGE's `ErrUnsupportedClause`; the Merge dir is Stage-13 newly-wired). **Skiplist delta**: `Create1[20]` (bare-var unbound: `CREATE (b {name: missing})`) and `Create2[24]` (bare-var unbound in relationship RHS: `CREATE (a)-[:KNOWS]->(b {name: missing})`) come OFF the skiplist — both are unbound-inline-map scenarios that reject correctly via `ErrUnboundVariable` after the widening (the widening records `missing` on the create part's refs list; buildPart's referential-integrity sweep raises `ErrUnboundVariable` because `missing` is not in scope). Suite-count delta from the skiplist removals: **skip −2, pass +2** (both scenarios are `Then a SyntaxError should be raised at compile time: UndefinedVariable`, which the acceptance suite verifies via the standard "then" step for parse failures). The mustReject pin (§1.8 kill-probe) is the Layer-2 counterpart to the same Layer-1 gain. Spec + CONTEXT.md mirror the code |

Each commit is green in isolation of the ones after it — the parser
red commit adds the model surface and pins that fail; the parser
green commit adds the handler; the unlock commit wires the dir; the
widening commit lands the mineInlineMap fix with its own goldens
delta so the diff is auditable in one review.

---

## 8. Weakest points recorded honestly (per ADR 0004)

**The most fragile part of Stage 13 is the `mineInlineMap` widening's
posture on nested-parameter approval.** The fast-path
(bare-`$param` value) records `PropertyUse{Ref{var, key}}`, the
widened path (rich value expression) records the SAME `PropertyUse`
for any parameter inside, and the loop-terminating
`requireAllParametersApproved` catches parameters that neither loop
approved. But the widened path's `typeExpressionMining` call might
independently APPROVE a parameter it mines (via its own
`addParameterUse` path — e.g., an arithmetic use of `$p` under
`{sum: person.age + $p}` — which records `ExprUse{TypeUnknown,
ExprInProjection}` at Stage 6's discipline) OR NOT (if Stage 6
does not approve inline-map-nested arithmetic parameters — the
concrete behavior needs a red probe to confirm). If the typer
approves, the widened path's subsequent `addParameterUse` with a
`PropertyUse` would DOUBLE-record the parameter (two entries in
its Uses slice), which is wrong. If the typer does NOT approve
and the widened path's loop skips it (because
`parameterFromExpr` returns false for nested parameters), the
final `requireAllParametersApproved` would fire — surfacing
`ErrUnsupportedParameter` on a shape that the widening is trying
to accept.

The parser-green commit's red probe answers this concretely. Two
mitigation postures are available depending on the answer:

1. If `typeExpressionMining` approves the parameter under a rich
   inline-map value, the widened path SKIPS its own `addParameterUse`
   loop for parameters already approved, relying on the typer's
   record. The resulting parameter's Uses slice carries an
   `ExprUse{TypeUnknown, ExprInProjection}` — HONEST but SLIGHTLY
   LESS SPECIFIC than the `PropertyUse{Ref{var, key}}` the fast
   path produces. A future consumer that reads the parameter's
   Uses expecting a `PropertyUse` on inline-map keys would need
   to widen; disclosure: the widened path does not preserve the
   fast-path's PropertyUse guarantee for rich values.
2. If `typeExpressionMining` does NOT approve nested parameters
   AND `parameterFromExpr` is not exhaustive on rich shapes, the
   widened path calls `findParameters(exprs[i])` (Stage 8's
   deep-walk helper) instead of a top-level `parameterFromExpr`
   check, and forwards every found parameter through
   `addParameterUse` with `PropertyUse{Ref{var, key}}`. This
   preserves the fast-path guarantee for rich values but requires
   care that the resolver can unify a PropertyUse posture with
   the parameter also appearing under an unrelated arithmetic
   node (an over-eager PropertyUse on
   `{sum: person.age + $p}` claims `$p` is `Person.sum` — which is
   plainly wrong; the resolver would need to detect this and back
   off).

The spec commits to answering the probe in the parser-green commit
and disclosing which posture is chosen with its trade-off. ~~Neither
posture is obviously superior; the safer default is posture 1
(defer to Stage 6's typing) because the typer's ExprUse posture is
the honest ADR-0005 boundary posture for a rich value expression:
"the parser cannot commit to a concrete property target from a
sum expression, the resolver upgrades post-freeze." Posture 2 is
tighter but leaks a heuristic ("the parameter is at the inline-map
key value slot, so it fills `var.key`") that the sum expression
does not honestly support. The current draft chooses posture 1.~~
[retired 2026-07-10 by gqlc-11c: **posture 2 shipped**; §4.3 is the
source of truth. The widened path forwards each mined parameter
through `addParameterUse` with `PropertyUse{Ref{var, key}}` — the
same shape the fast path produces — and §4.3's third bullet
("Parameters under a rich value expression record
`PropertyUse{Ref{var, key}}` — the SAME shape the fast path
produces") documents the rationale. The posture-1 verdict recorded
above contradicts §4.3 and the parser-green implementation; the
enumeration of both postures is preserved as narrative history.]

**The next-most fragile part is the `collectMergeAction`'s save/
restore around `curPart.effects`.** The pattern is: snapshot the
slice, clear it, walk `collectSetItem` (which appends to
`curPart.effects`), collect the appended items, restore the
snapshot, transfer the collected items into a `[]SetEffect` local.
Fragility: if `collectSetItem` gains a NEW side effect on
`curPart.effects` (e.g., appending an unrelated Effect variant on
some grammar shape), the save/restore mechanism would silently
strip it out of the outer effects list. Today `collectSetItem`
appends exactly one Effect per SetItem — SetPropertyEffect /
SetEntityEffect / SetLabelsEffect — and nothing else. A future
widening that changes this needs to update `collectMergeAction`'s
transfer logic in lockstep. The belt-and-braces guard is the
type-assertion loop (line-item cite in §4.2 code): every
transferred item is asserted to `query.SetEffect`; a non-Set item
surfaces as an internal-invariant `fail` — visible in tests, not
silent.

**The `writeSeen` non-flip inside EXISTS is a walk-time semantic
that a query-wide static analyzer would need a second axis to
see.** Stage 12 §weakest-points documented this for CREATE / DELETE
/ SET / REMOVE; Stage 13 extends it to MERGE. A query like
`MATCH (n) WHERE exists { MERGE (m) RETURN true } RETURN n` does
NOT flip `writeSeen` — the outer `EnterOC_Merge` early-returns
under `subqueryDepth > 0` before the writeSeen assignment. The
outer query is classified as read, and the openCypher composition
rule that rejects writes-in-EXISTS is bucket 3 (the engine
raises when re-executing the original text). A future consumer
wanting "any write anywhere including in subqueries" would need a
second axis (`Query.HasNestedWrites` or similar); Stage 13 does
not provide one, and no known consumer needs it. The AUTHORED
pin in §1.8 locks this posture explicitly for MERGE.

**The sealed sub-sum `SetEffect` adds one marker method to each of
three existing types.** This is strictly additive at the value-
receiver signature level; no existing method changes, no field
changes. But: any downstream mock or fake of Effect that
implements `isEffect()` today (defensive testing) would need to
add `isSetEffect()` too if it wants to double as a SetEffect. The
parser's own test doubles are all concrete uses of the parser's
own types (no mocks), so the internal fallout is zero. External
consumers of the `query` package (none today besides `cypher`) do
not exist yet, so the exposure is theoretical.

The lesser risks, recorded for completeness:

- **Merge1[11]'s widening is disclosed as a pre-existing latent
  gap fixed in-scope (Q3 fold-in).** The `mineInlineMap` value-side
  ref-drop is a Stage 12 latent bug the Stage 13 corpus is the
  first to press. Fixing it inside Stage 13 is the only way to
  ship Merge1[11] with an honest golden; skipping the fix would
  require a bucket-3 skiplist entry that would not honestly
  describe the situation (a parser bug, not a runtime rule). The
  fix widens `mineInlineMap` for MATCH / CREATE / MERGE
  uniformly. The executing-query audit (§1.4) enumerated exactly
  which pinned-corpus scenarios re-key: 10 Create6 goldens gain
  the `x` ref, 2 Unwind1 and 8 Merge scenarios mint fresh on this
  branch (no re-key), and 2 create-dir negatives (Create1[20],
  Create2[24]) come OFF the skiplist because the widening makes
  them reject correctly via `ErrUnboundVariable` — a Layer-1
  posture flip (skip → pass) that mirrors the Layer-2 mustReject
  kill-probe (§1.8). Each re-keyed golden is listed by scenario
  name in the widening commit message; the change is
  strictly-more-information (existing consumers, if any, get a
  richer refs list, not a different one). The Stage 12 spec's
  §weakest-points disclosure of parameter-mining posture
  (`"parameter mining rides an existing path"`) implicitly
  covered the drop but did not explicitly name the value-side
  ref-drop; Stage 13 discloses it explicitly.
- **Grammar-reject pins DO exercise the grammar layer, not a
  domain sentinel.** The `MERGE (a), (b)` pin exercises the ANTLR
  parser directly (the grammar rejects the second pattern part
  before the listener sees it). This is the same posture the
  FOREACH grammar-reject pin uses. If the `mustReject` map
  requires a domain-error `want:` value, the pin can either
  designate a `parseErr` sentinel (whichever convention Stage 12
  established for FOREACH — the spec accepts either) or land in
  a `mustRejectGrammar` sibling map. Neither choice affects the
  semantic guarantee: `MERGE (a), (b)` never reaches a
  successfully-built Query, and the grammar-truth invariant
  (`oC_Merge` admits only one pattern part) is locked at the
  test layer.
- **The `MergeEffect.OnMatch` / `OnCreate` slices are ordered
  (walk order) and independent.** `MERGE ... ON MATCH SET a:L
  ON CREATE SET a:M` produces `OnMatch: [SetLabelsEffect{a,
  [L]}]`, `OnCreate: [SetLabelsEffect{a, [M]}]` in that order.
  A query with the two ON clauses in the OPPOSITE textual order
  produces the same two slices — the axis (MATCH vs. CREATE) is
  keyed by the ON-terminal, not by textual position. Wire shape:
  the two slices marshal independently; a consumer reading the
  JSON reconstructs the same axis. The grammar admits any number
  of ON actions in any order; Stage 13 folds each into its
  branch's slice preserving intra-branch order, and drops
  inter-branch order (a `MERGE ... ON MATCH SET a:X ON CREATE
  SET a:Y ON MATCH SET a:Z` produces `OnMatch:
  [SetLabelsEffect{a, [X]}, SetLabelsEffect{a, [Z]}]`,
  `OnCreate: [SetLabelsEffect{a, [Y]}]`, textual interleaving
  lost). This is a modelling decision aligned with the
  match-vs-create semantic axis; a future consumer wanting
  interleaved order would need a flat `[]MergeAction` slice.
  No known consumer needs it; Stage 13 does not provide one.
- **`Effects` still emits `null` for a nil slice.** Stage 12
  established the always-emit convention; Stage 13 does not
  change it. A pre-Stage-13 golden's `"effects": null` (for a
  read-core scenario) stays `"effects": null`. Post-Stage-13,
  the goldens for merge scenarios gain
  `"effects": [{"kind": "merge", ...}]`. This is a strictly
  additive change.
- **The Temporal4 LAST-wins re-key noted in Stage 12's
  §weakest-points is settled — no further re-keys are expected
  at Stage 13 (Temporal4 has no MERGE scenarios).** If a future
  bump of Temporal4 introduces a MERGE `executing query:`
  scenario, the same LAST-wins rule would apply; disclosure for
  parity, not action.
