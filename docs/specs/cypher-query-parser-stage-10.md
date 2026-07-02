# Stage 10 spec — Cypher query parser: aggregations

The implementation brief for Stage 10 of the Cypher implementation of
`query.Parser`. Tenth model evolution after Stage 9 per ADR 0004 (test-first,
evolving until feature-complete), under the curation discipline of ADR 0003
and the type-interface boundary of ADR 0005. Stage 10 is the fifth stage of
the ADR-0007 pre-freeze expansion beyond the read core. It **completes the
aggregate surface**: the parser already recognises the eight aggregate
functions and their `count(*)` degenerate case (Stage 3), but the model
records `TypeUnknown` for every aggregate result and silently drops
`DISTINCT`. Stage 10 fixes both: it types every aggregate result as
precisely as the parser can commit to schema-free, and it lifts `DISTINCT`
into `AggregateProjection` so the model preserves the deduplication axis
that changes result cardinality.

This document is a **delta** against Stages 0–9 (referenced individually
where relevant); everything not stated here carries over verbatim.
Sections appear here only where Stage 10 changes something.

Tracking: bead `gqlc-2av` (GitHub #45). Lands as one graphite branch
(`stage-10-aggregations`) with separated commits (spec → model red/green →
parser red/green → unlock+goldens), independently mergeable as a whole:
`just test` is green if this branch lands on `master` alone (AGENTS.md
stacked-branch invariant).

---

## 1. Deliverables

### 1.1 `AggregateProjection` grows a `DISTINCT` axis

The current `AggregateProjection` has three exported facets: `Func()` (the
cardinality-bearing kind), `Refs()` (the arguments' bindings), and `Type()`
(the Stage-6 result type, `TypeUnknown` today). Stage 10 adds a fourth:
`Distinct() bool` — true iff the aggregate call was written
`count(DISTINCT x)`, `collect(DISTINCT y)`, etc.

Rationale — why DISTINCT enters the model:

- `count(DISTINCT a)` and `count(a)` are semantically different queries:
  the former deduplicates its input before counting, the latter does not,
  and the two produce different values on the same dataset. The
  cardinality-bearing distinction the model already keeps for aggregates
  (§4 of ADR 0003) does not subsume DISTINCT — an aggregate's kind (the
  `AggregateFunc` enum) tells the resolver "this column is one row per
  group"; DISTINCT tells the resolver "the group's contribution to this
  column is deduplicated first." Dropping DISTINCT would let two
  observably-different queries lower to indistinguishable models, which
  breaks the model's promise that the generated code re-executes the
  original text against a faithful type interface (ADR 0005): the
  generated method's *signature* is unchanged, but its *contract* is
  not.

- ADR 0003's no-expression-tree rule is not violated: DISTINCT is a
  single-bit annotation on one node of the sum, not a tree fragment. It
  is the exact analogue of `EdgeBinding.Directed()` (Stage 5) and
  `Query.Combinators[i]` (Stage 4) — a scalar axis on a specific
  variant that changes the value semantics without dragging the
  expression tree into the model.

Wire encoding: `AggregateProjection` gains `"distinct": bool` as an
always-emitted field, alongside `func`, `refs`, and `type`. Always-emit
matches the convention (`nullable`, `returnsAll`, `directed`, `hops`).

`count(DISTINCT *)` is not a legal openCypher shape (the `*` and DISTINCT
are grammar alternatives, not compatible) — the grammar rejects it before
the classifier runs, so the model never sees a `count(*)` with
`Distinct() == true`. The constructor accepts both false and true for
`AggCount` regardless — no smart-constructor check needed, because the
grammar owns the invariant.

Smart constructor signature:

```
func NewAggregateProjection(fn AggregateFunc, refs []Ref, distinct bool, t Type) AggregateProjection
```

The parameter order groups the facets — kind, arguments, distinct axis,
result type — matching the field order in the JSON wire shape.

### 1.2 Aggregate result typing

The Stage-6 posture — "a wrong concrete type is strictly worse than an
honest `TypeUnknown` the resolver can upgrade from the schema" — governs
every rule below. When operand types are honest-Unknown, the aggregate
result stays honest-Unknown. When the operand's type is a concrete scalar
whose aggregation the openCypher specification commits to, the result
type is that commitment; nothing else.

The eight aggregate result-type rules, keyed by `AggregateFunc`:

| Aggregate      | Operand type                                                                                                    | Result type                       |
|----------------|-----------------------------------------------------------------------------------------------------------------|-----------------------------------|
| `count(*)`     | n/a                                                                                                             | `TypeInt`                         |
| `count(expr)`  | anything (nulls filtered at runtime, but the *count* is always an integer)                                      | `TypeInt`                         |
| `collect(expr)`| any Stage-6 type `T`                                                                                            | `TypeList(T)` — never Unknown at the outer level; the aggregate always yields a list, so the honest posture is `list<unknown>`, not `unknown` |
| `sum(expr)`    | `TypeInt`                                                                                                       | `TypeInt`                         |
| `sum(expr)`    | `TypeFloat`                                                                                                     | `TypeFloat`                       |
| `sum(expr)`    | `TypeDuration`                                                                                                  | `TypeDuration`                    |
| `sum(expr)`    | anything else (property, mixed, `TypeUnknown`, `TypeNull`)                                                      | `TypeUnknown`                     |
| `avg(expr)`    | `TypeDuration`                                                                                                  | `TypeDuration`                    |
| `avg(expr)`    | anything else (including `TypeInt`, `TypeFloat`, property, `TypeUnknown`)                                       | `TypeUnknown`                     |
| `min(expr)`    | `TypeInt`, `TypeFloat`, `TypeString`, `TypeBool`, `TypeDate`, `TypeTime`, `TypeLocalTime`, `TypeDateTime`, `TypeLocalDateTime`, `TypeDuration` | operand type |
| `min(expr)`    | anything else (`TypeList`, `TypeMap`, `TypeNode`, `TypeEdge`, `TypePath`, `TypeNull`, `TypeUnknown`)             | `TypeUnknown`                     |
| `max(expr)`    | same rule as `min` (symmetric)                                                                                  | operand type or `TypeUnknown`     |
| `stDev/stDevP` | anything                                                                                                        | `TypeUnknown`                     |
| `percentile*`  | anything                                                                                                        | `TypeUnknown`                     |

Design commitments captured in the table:

- **`count` always types as `TypeInt`.** openCypher's `count` returns an
  integer by specification; no operand type can widen that (a `count`
  of nulls is 0, an integer). The `AggCount` row is the only aggregate
  where the parser can commit unconditionally — a schema-agnostic
  concrete type that is never wrong.

- **`collect` always types as `list<T>`, never bare `TypeUnknown`.** The
  aggregate is a list constructor; the outer type IS list. When the
  parser cannot type the element, the honest posture is
  `list<unknown>` — a `TypeList` value whose element is `TypeUnknown{}`.
  Codegen post-freeze emits `[]T` (with `T` honestly-unknown, upgraded
  by the resolver from the schema) rather than `any`, which is
  strictly better than a bare `TypeUnknown` result.

- **`sum` commits when the operand is a fully-known numeric or
  duration**, else stays honest-Unknown. `sum(TypeInt) → TypeInt` is
  safe because openCypher's runtime promotion (int→float on overflow)
  is a value-level rule below the type-interface boundary (ADR 0005) —
  the generated code re-executes the original text, so a runtime
  promotion still surfaces via the driver. A property-typed operand
  (`sum(n.age)`) stays honest-Unknown because the schema might type
  `age` as float; the resolver upgrades.

- **`avg` stays honest-Unknown for numerics.** openCypher does not commit
  a single result type for `avg` — Neo4j returns integer when division
  is exact, float otherwise; other engines round differently. The
  parser has no way to know at parse time; committing either concrete
  type would be strictly worse than honest-Unknown. Duration is the
  only spec-committed case (`avg(duration) → duration`).

- **`min` / `max` return their operand type for scalar comparables.**
  These aggregates are order-preserving; if every input is an
  integer, the min/max is an integer (openCypher does not widen). For
  operand types where ordering is engine-defined (lists, maps, nodes,
  edges), the parser stays honest-Unknown — a bucket-3 runtime
  concern.

- **`stDev/stDevP` and `percentile*` stay honest-Unknown.** These are
  statistical outputs whose result-type table is engine-detailed and
  not worth committing at the parser boundary. The resolver upgrades
  from the schema post-freeze if the schema is willing to commit
  (few will be).

The rules are keyed off the operand's **Stage-6 computed type** — the
same type the rich-expression typer computes for any expression. This
composes: `sum(n.age + 1)` where `n.age` is honest-Unknown yields
`TypeUnknown` for the operand (Stage 6 `promoteAdd(Unknown, Int) =
Unknown`), so the aggregate arm returns `TypeUnknown` — the honest
posture end-to-end. On the wire that shape lands as
`ExprProjection{refs:[n.age], type:unknown}`, not
`AggregateProjection`, because a non-bare argument declines the
bare-atom classifier (§8 non-bare-aggregate-argument entry).
`sum(range(1, 3))` lands the same way: `range()` (a bare function
call, function identity below the boundary) types as `TypeUnknown`,
so `aggregateResultType(AggSum, TypeUnknown) = TypeUnknown` and
the wire shape is `ExprProjection{refs:null, type:unknown}`.
`collect(n)` where `n` is a `NodeBinding` yields `list<node>` — the
argument IS bare, so this one classifies as `AggregateProjection`.

### 1.3 Aggregates inside rich expressions

Stage 6's `typeAtom` in `typing.go` has an arm for `OC_FunctionInvocation`
that mines the argument refs and returns `TypeUnknown` for every function
call except the seven-name temporal constructors. Stage 10 adds one
special case to that arm: **if the call name matches an aggregate, the
call types as the aggregate's Stage-10 result type**, computed by the
same table above against the argument's own computed type.

This makes `count(n) + 1` type as `TypeInt` (via `promoteAdd(TypeInt,
TypeInt)`), `count(n) > 3` type as `TypeBool`, and `collect(n).x` type as
`TypeUnknown` (a property lookup on a list is a runtime rule, per
ADR 0003). None of these were rejectable before Stage 10; the change is
strict typing improvement inside rich expressions, so the model records
what the projected column is actually going to be.

The refinement is honest: an aggregate inside a rich expression IS an
aggregate, and its result type is what the resolver sees. The Stage-6
`ExprProjection.Type()` and the resolver-facing `AggregateProjection.Type()`
now record the same concrete type when the aggregate's operand is a
committable type — the model does not disagree with itself.

### 1.4 Aggregates in WHERE / ORDER BY / SKIP / LIMIT

These are the four "aggregate in non-projection position" scenarios the
TCK exercises. Per ADR 0007, each is classified as bucket 1 (parse-reject)
or bucket 3 (parse-accept, engine raises) with rationale.

**Aggregate in WHERE (`WHERE count(n) > 3`).** openCypher forbids
aggregates in WHERE (a WHERE runs per-row, before grouping). The current
parser already parse-accepts (the Stage-6 rich-expression typer sees
`count(n)` as a function invocation and types it as `TypeUnknown`); the
Stage-10 change makes it type as `TypeInt`, so `count(n) > 3` types as
`TypeBool`. The rejection is a **grouping-key semantic rule** — the same
family as `AmbiguousAggregationExpression` and `InvalidAggregation`,
which ADR 0007 §II bucket 3 places below the boundary (the engine
re-executing the original text raises the error). Existing skiplist
entry `[15] Fail on aggregation in WHERE` (match-where/MatchWhere1)
continues to fire; no new skiplist entry, no code change.

**Bucket:** 3 (aggregate-position rule, engine-side). Rationale:
per-position aggregate-legality is a grouping/binding-scope semantic
rule the type interface does not carry (ADR 0003 — no expression
tree, no position semantics beyond `ExprPosition.Predicate` vs
`.Projection`); the runtime engine enforces it via the re-executed
original text (ADR 0005).

**Aggregate in ORDER BY (`ORDER BY count(n)`).** The TCK's negative
scenarios (already in the Stage-9 skiplist:
`[14] Fail on aggregation in ORDER BY after RETURN`,
`[25] Fail on sorting by an aggregation`,
`[13] Fail on sorting by a non-projected aggregation on a variable`,
`[14] Fail on sorting by a non-projected aggregation on an expression`)
exercise this class. The current parser walks the sort key only for
parameter mining (`mineSortItemParameters`), so aggregate-position rules
in ORDER BY are already accept-and-defer.

**Bucket:** 3 (aggregate-position rule, engine-side). Rationale: same as
WHERE — sort-key structure is below the boundary, per Stage 9 §4.2.
Existing skiplist entries carry over; no new entry, no code change.

**Aggregate in SKIP / LIMIT.** The TCK does not exercise this shape
(SKIP/LIMIT accept only a `ClauseSlotSkip/Limit` bare parameter or a
literal integer per Stage 1 §6). An aggregate in SKIP/LIMIT would
fall through the `parameterFromExpr` gate and fail with
`ErrUnsupportedParameter` — the existing rejection covers it. No
Stage-10 change.

**Aggregate inside a projected aggregate (`count(count(*))`).** The
Stage-6 test suite already treats this as bucket 3 via the existing
skiplist entry `[14] Aggregates in aggregates`. Stage 10 preserves
the treatment. On the wire it types as `ExprProjection{refs:null,
type:int}`, not `AggregateProjection`: the outer `count`'s argument
(`count(*)`) is not a bare var / var.prop / scalar literal, so the
bare-atom classifier declines and `classifyRichExpression` runs.
That routes the whole call through `typeAtom`, which finds the
outer aggregate name, computes the inner arm's type via
`aggregateResultType(AggCount, nil) = TypeInt`, then
`aggregateResultType(AggCount, TypeInt) = TypeInt` — a strict type
improvement over Stage-3's `TypeUnknown`. The `AggregateFunc` kind
is not preserved (falls under the §8 non-bare-aggregate-argument
entry; resolver-side follow-up **gqlc-33k.1**), and position-legality
is a grouping-semantic rule below the boundary. No new skiplist
entry, no code change, no parser widening.

### 1.5 Grouping semantics

openCypher's implicit grouping rule — every non-aggregate projection
column is a grouping key — is a resolver concern (bead `gqlc-gyw`
tracks it), not a parser concern. The parser records what each
projection column's Stage-6 result type is; the resolver computes,
from the projection list, which columns are keys and which are
aggregates. The parser is not entangled: an `AggregateProjection`'s
`Func()` and `Distinct()` tell the resolver "this column collapses
rows"; the sibling non-aggregate columns tell it "these are the
groups." No new model field is needed at Stage 10.

Rationale: the type interface already carries per-column types (Stage
6) and the aggregate-vs-not distinction (Stage 3). Adding a `GroupKeys
[]Ref` field to `Query.Part` would duplicate information already
present in the projection list, and it would encode a semantic rule
(implicit grouping-key derivation) into the model — the exact
posture ADR 0003 warns against. Grouping-key derivation is a resolver
transformation, and the entangled follow-up (`gqlc-gyw`) is already
scoped.

### 1.6 Sentinel status

`ErrUnsupportedProjection` was already retired at Stage 6 (rich
projections lower to `ExprProjection`). Stage 10 does not touch the
remaining four sentinels — `ErrUnsupportedClause`,
`ErrUnsupportedParameter`, `ErrUnboundVariable`,
`ErrVariableKindConflict`. The `TestSentinelReachability` fixture
runs against the same four-sentinel set; the `mustReject` cases are
unchanged.

### 1.7 Corpus wiring

`readCoreDirs` gains **one** dir:

- `expressions/aggregation` — 8 feature files with 27 scenario outlines
  (Aggregation1..8; Aggregation4/7 are empty scaffolds at the pinned
  TCK tag). Positive scenarios exercise every aggregate function, DISTINCT
  variants, and OPTIONAL MATCH + collect. Negative scenarios (percentile
  bad arguments) are bucket-3 by the categorical `isBucketThreeDir`
  rule since the dir is under `expressions/`.

The dir joins the existing expressions dirs which categorically ride
`isBucketThreeError` for engine-side errors, so no new per-scenario
skiplist entries are needed for the aggregation TCK's negative
scenarios.

Additional coverage the wired dir gives:

- `[15] Fail on aggregation in WHERE` (match-where/MatchWhere1) stays
  in the existing skiplist.
- `[8] Fail if not projected variables are used inside an expression
  which contains an aggregation expression` (Return5) and its family
  (Stage 6 skiplist entries) all stay — they're grouping-key rules,
  bucket 3.

### 1.8 Skiplist

No new skiplist entries beyond the categorical `isBucketThreeDir`
coverage. The aggregation dir's four negative-scenario families
(percentileCont bad arguments, percentileDisc bad arguments,
percentileDisc failing on out-of-range percentile, and "no aggregate
inputs" runtime errors) are all TypeError / ArgumentError /
SyntaxError-with-InvalidArgumentValue kinds — every one is engine-side
by `isBucketThreeError`'s existing rules.

Stage-9's Stage-6-inherited grouping-key entries (Return5 [8], [9],
[20], [21]; Aggregation-in-aggregation `[14]`; rand-in-aggregation
`[15]`) all carry over unchanged: those are bucket-3 grouping-key
rules under the type-interface boundary.

### 1.9 Layer-2 pins

New `mustParse` cases for the Stage-10 shapes:

- **count with distinct**: `MATCH (a) RETURN count(DISTINCT a)` → one
  `AggregateProjection{AggCount, [Ref{a}], Distinct=true, TypeInt}`.
- **collect over node binding**: `MATCH (n) RETURN collect(n)` → one
  `AggregateProjection{AggCollect, [Ref{n}], Distinct=false,
  list<node>}`.
- **collect over property**: `MATCH (n) RETURN collect(n.name)` → one
  `AggregateProjection{AggCollect, [Ref{n,name}], Distinct=false,
  list<unknown>}` — the element type is `TypeUnknown` because a
  property lookup types as `TypeUnknown` (ADR 0003), so the honest
  posture is `list<unknown>`.
- **sum over unwind-int**: `UNWIND [1,2,3] AS x RETURN sum(x)` → one
  `UnwindBinding{"x", TypeInt}`, one
  `AggregateProjection{AggSum, [Ref{x}], Distinct=false, TypeInt}`.
- **avg over unwind-int**: `UNWIND [1,2,3] AS x RETURN avg(x)` — one
  `AggregateProjection{AggAvg, [Ref{x}], Distinct=false,
  TypeUnknown}` — avg stays honest-Unknown for int operand (spec
  §1.2 rationale).
- **min over unwind-string**: `UNWIND ['a','b'] AS x RETURN min(x)` →
  one `AggregateProjection{AggMin, [Ref{x}], Distinct=false,
  TypeString}`.
- **count-star**: `MATCH (n) RETURN count(*)` — the existing pin
  updates from `TypeUnknown` to `TypeInt`. (The other Stage-3 pin
  `count star aggregate` in `mustParse` migrates its `Type` too.)
- **aggregate in rich expression (WITH chaining)**: `MATCH (n) WITH
  collect(n) AS ns UNWIND ns AS m MATCH (m) RETURN m` — the existing
  Stage-9 pin `unwind of list-of-nodes reused as node match`
  updates its aggregate result type from `TypeUnknown` to
  `list<node>`, and the downstream UnwindBinding's `elemType`
  upgrades from `TypeUnknown` to `TypeNode`. That is a strict
  typing improvement — the model now records that `ns` is a
  list of nodes, so `m` is a node, so a subsequent `MATCH (m)`
  matches under the byVar-vs-nameBoundAsUnwind rule (Stage 8
  spec §4.3). The `RefProjection` on `m` updates its result
  type from `TypeUnknown` to `TypeNode`.
- **count in rich expression**: `MATCH (n) RETURN count(n) + 1` — one
  `ExprProjection{[Ref{n}], TypeInt}` (aggregate arm inside typeAtom
  yields TypeInt, then promoteAdd with the int literal is TypeInt).

`mustReject` is unchanged.

### 1.10 Docs inline

- This spec.
- ADR 0003's amendment notes gain a Stage-10 line (the
  `AggregateProjection` variant grows a `Distinct` axis; `Type()`
  becomes a per-aggregate table computed against the argument's
  Stage-6 type; aggregate calls inside rich expressions now type
  their enclosing expression via that same table).
- ADR 0007 already names Stage 10 (aggregations); no header change.
- CONTEXT.md gets an entry noting the `DISTINCT` axis on the
  Aggregate glossary term.

Nothing downstream of the parser is built (no resolver, no codegen)
— ADR 0004. The resolver's implicit-grouping computation lives in
`gqlc-gyw`; the resolver's DISTINCT interpretation is a resolver
transformation the model now honestly hands off.

---

## 2. Why one atomic cycle

Adding a `Distinct` axis to `AggregateProjection` and computing per-aggregate
result types is one restructure of the model's aggregate story. Splitting
the model change (add `Distinct`, upgrade `Type()`) from the parser change
(read the DISTINCT token, apply the typing table) would leave the model
underused on one branch; splitting the corpus wiring from either would
leave the acceptance suite in a mid-migration state where the aggregation
dir has no goldens. Stage 10 lands as one branch.

Within the branch, the commit inventory (§7) separates spec from model
from parser from corpus wiring so review can proceed incrementally.

---

## 3. Model shape

### 3.1 AggregateProjection with Distinct axis

```
type AggregateProjection struct {
    fn         AggregateFunc // which aggregate this is
    refs       []Ref         // the var/var.prop arguments the aggregate touches
    distinct   bool          // Stage 10: DISTINCT dedup axis
    resultType Type          // the Stage-10 typing table's result
}

func NewAggregateProjection(fn AggregateFunc, refs []Ref, distinct bool, t Type) AggregateProjection
func (p AggregateProjection) Func() AggregateFunc
func (p AggregateProjection) Refs() []Ref
func (p AggregateProjection) Distinct() bool     // Stage 10
func (p AggregateProjection) Type() Type
```

Wire encoding:

```
{"kind":"aggregate","func":"count","refs":[...],"distinct":true,"type":"int"}
```

The `distinct` key is always emitted (matches the always-emit
convention).

### 3.2 Aggregate typing helper

A new package-level helper in the cypher listener package (kept package-
local; it is not part of the public model surface):

```
// aggregateResultType returns the Stage-10 result type of an aggregate
// call given its AggregateFunc and its operand's Stage-6 result type.
// Follows §1.2. Nil operand (the count(*) degenerate case, where
// there is no operand) is treated as if the aggregate had no operand
// — the table's "count" rows apply.
func aggregateResultType(fn query.AggregateFunc, operand query.Type) query.Type
```

The helper is called from two sites:
- `classifyFunction` in `expr.go`, once per aggregate projection at
  RETURN or WITH position.
- `typeAtom` in `typing.go`, once per aggregate call inside a rich
  scalar expression.

Both sites feed the operand type they've already computed from the
same Stage-6 machinery, so the helper is the single source of the
table and cannot drift between the two positions.

### 3.3 refType, exportedTypes unchanged in shape

`exportedTypes` (listener.go) already reads `r.Value.Type()`, so the
upgraded aggregate result type flows into the imported map by
construction — a `WITH count(n) AS c` now exports `c → TypeInt`, and
a downstream `RETURN c` types as `TypeInt`. No new code path is
needed; the shape improvement is a strict widening.

---

## 4. Parser widening

### 4.1 DISTINCT read from OC_FunctionInvocation (§1.1)

`classifyFunction` in `expr.go` reads `fi.DISTINCT()` (an ANTLR
terminal node accessor already generated on `OC_FunctionInvocationContext`).
A non-nil accessor means the DISTINCT keyword was present. The classifier
passes the boolean to `NewAggregateProjection`.

Grammatical guarantee: DISTINCT is only accepted inside a function
invocation by the openCypher grammar, so the classifier is the only
site that needs to read it. A DISTINCT on a non-aggregate function
(`toInteger(DISTINCT x)`) is legal by the grammar but semantically
undefined; the parser records DISTINCT only when the call classifies
as an aggregate. For a non-aggregate function call, DISTINCT is
silently ignored — the model does not carry a `FuncProjection.Distinct`
field, because DISTINCT on a non-aggregate is not a modelled semantic
(it either has no effect, per some engines, or is a runtime error, per
others — bucket 3).

### 4.2 Aggregate result typing at RETURN / WITH (§1.2)

`classifyFunction`'s existing aggregate arm calls the new
`aggregateResultType(fn, operandType)` helper, where `operandType`
is the Stage-6 result type of the aggregate's single expression
argument (via `typeExpression` on the argument's `OC_Expression`;
`count(*)`'s star-atom has no operand, so the helper is called
with a nil operand and returns `TypeInt`).

Multi-argument aggregates (`percentileDisc(x, 0.5)`) still type
as `TypeUnknown` — the second argument (the percentile) is a
percentile-position slot the parser does not use for result typing.

For a `count(*)` call, the classifier passes `nil` for the operand
type; the helper's zero-operand arm returns `TypeInt`.

### 4.3 Aggregate result typing inside rich expressions (§1.3)

`typeAtom` in `typing.go` — the `a.OC_FunctionInvocation() != nil`
arm — gains one branch: **if the call name matches an aggregate**,
compute its Stage-10 result type via `aggregateResultType` against
the argument's typed result, and return that instead of
`TypeUnknown`. Every other function call (temporal constructor,
generic function) keeps its current typing path.

The argument-refs mining stays the same: `mineFunctionArgs` walks
every argument's expression for refs, so the containing part's
`refs` list still records every var/var.prop the aggregate touches.

### 4.4 count(*) — the degenerate case

`count(*)` types as `TypeInt` — this is the sole case where the
Stage-3 pin `count star aggregate` shifts its shape (from
`TypeUnknown` to `TypeInt`). The classifier's count-star arm calls
`aggregateResultType(AggCount, nil)`.

### 4.5 Buildpart unchanged

`buildPart` does not care about DISTINCT or aggregate types; the
per-part scope and referential-integrity sweep are unchanged.

---

## 5. Corpus and bucket-3 whitelist

`expressions/aggregation` enters `readCoreDirs`. Its ~27 scenario
outlines are a mix of positive-case aggregate demos (Aggregation1/2/3/5)
and outcome-value negative scenarios (Aggregation6 percentile-out-of-
range, Aggregation2 min/max over lists). Every negative scenario is
categorically bucket-3 via `isBucketThreeDir` (the dir is under
`expressions/`) — no per-scenario skiplist entries needed.

The Stage-6 skiplist entries covering aggregate-position semantic
rules (`[8]`, `[9]`, `[20]`, `[21]`, `[14]`, `[15]` in various files;
`[15] Fail on aggregation in WHERE` in match-where) all stay: those
are grouping-key semantic rules under the type-interface boundary.

Layer-2 rule (Stage 1 §6). Stage 10 adds the `mustParse` cases §1.9
names. The `mustReject` set is unchanged.

`TestSentinelReachability` runs against the four-sentinel set.

---

## 6. Definition of done for Stage 10

1. `stage-10-aggregations` lands green and independently mergeable;
   `master` is green if it lands solo.
2. `just test` green: query-package unit tests (extended
   `AggregateProjection` shape, `aggregateResultType` helper), the
   cypher-package classifier + typing tests, the `mustParse` pins, the
   acceptance / orphan / reachability suites, the property tests.
3. `just lint` green: zero issues.
4. `just fmt-check` green: zero diffs.
5. Layer-1 godog count rises by the aggregation dir's ~27 scenarios,
   less the runtime-value scenarios that ride bucket-3 categorical
   accept. Zero FAIL is mandatory. Every scenario whose only
   Stage-10 need was aggregate typing or DISTINCT propagation now
   flips PASSING (subject to prior chain-dependency deferrals).
6. Documentation: this spec; CONTEXT.md entry for DISTINCT; ADR 0003
   note.
7. Beads: `gqlc-2av` closed.

---

## 7. Commit inventory (single branch `stage-10-aggregations`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec + CONTEXT.md + ADR 0003 note (docs land in the branch, matching the DoD) |
| model (red) | Failing unit tests for `NewAggregateProjection` with `Distinct`, wire shape, typing table |
| model (green) | `AggregateProjection` gains `Distinct`; wire tags emit `"distinct"`; constructor signature updates |
| parser (red) | Failing `mustParse` cases for Stage-10 shapes (typing, DISTINCT propagation) |
| parser (green) | `classifyFunction` reads DISTINCT and uses `aggregateResultType`; `typeAtom` typing arm; goldens regenerated |
| unlock (dir + goldens) | `readCoreDirs` gains `expressions/aggregation`; goldens audited; skiplist review |

Each commit is green in isolation of the ones after it — the model
commits leave the parser writing `false` for `Distinct` and
`TypeUnknown` for aggregate results (existing behaviour); the parser
commits use the new shape at the aggregate call sites; the unlock
commit wires the dir.

---

## 8. Weakest point (recorded honestly per ADR 0004)

**The Stage-10 `avg` typing choice — honest-Unknown for numerics — will
be revisited if the openCypher standard (or the pinned TCK tag)
commits a concrete rule.** Neo4j returns int for exact-int avg,
float otherwise; the CIP openCypher standard has no single
committed rule for avg's numeric result type. The Stage-10 parser
therefore records `TypeUnknown` for `avg(TypeInt)` and
`avg(TypeFloat)` — the honest posture the resolver upgrades from
the schema. The alternative — commit `TypeFloat` — would be strictly
worse if an engine returns int for exact-int input (the model would
mis-shape codegen's downstream cast) and no better if the engine
returns float (the resolver could have inferred float anyway). We
stay honest.

If the TCK bump introduces an `Aggregation4 - Avg` scenario with an
observable float-vs-int result, the rule migrates then; the two-line
table entry is where the change lands.

The lesser risks, recorded for completeness:

- **Aggregates with non-bare arguments drop their `AggregateFunc` kind.**
  `sum(x + 1)`, `count(n.age + 1)`, `collect(a OR b)`, `min([1, 2, 3])`,
  `sum(range(1, 3))`, and `count(count(*))` all classify as
  `ExprProjection` rather than `AggregateProjection` — Stage 6's
  `functionArgRefs` discipline (each argument must be a bare
  var/var.prop or a scalar literal, no expression tree, no nested
  calls) declines the bare-atom classifier, so
  `classifyRichExpression` runs. The Stage-10 aggregate arm in
  `typeAtom` still computes the honest per-aggregate result type
  (§1.3), so the projection's `Type()` is *right* — but the outer
  node is `ExprProjection{refs, type}`, and the
  `AggregateFunc`-kind signal is not preserved on the wire. A
  resolver reading the model cannot distinguish `sum(x + 1)` from a
  plain scalar expression of the same result type, so it cannot know
  the column collapses rows (and it cannot see DISTINCT either — the
  DISTINCT bit lives on `AggregateProjection`, not `ExprProjection`).
  Inherited from Stage 6 and made starker by Stage 10's own DISTINCT
  rationale: this is exactly the "two observably-different queries
  lower to indistinguishable models" hazard §1.1 warns against, one
  turn further down the tree. The honest posture is to record it
  here rather than widen the bare-atom classifier: expanding it to
  cover arithmetic operands would drag the expression tree into the
  model (ADR 0003), so the follow-up is scoped past the parser
  boundary — the resolver's grouping-key computation (bead
  **gqlc-33k.1**) is where the aggregate-vs-not signal has to be
  re-derived from the projection list anyway, and it can walk the
  original text (ADR 0005) to recognise the outer aggregate name.

- **`DISTINCT` on a non-aggregate function call is silently ignored.**
  The grammar accepts `toInteger(DISTINCT x)`; the parser classifies
  it as a `FuncProjection` and drops DISTINCT on the floor. Adding
  `FuncProjection.Distinct` would over-model a distinction the
  aggregate boundary already carries — for a non-aggregate function
  DISTINCT is either a no-op (Neo4j: same value returned) or an
  engine-side error (per §CIP-2015-06). Either way it is a runtime
  concern, not a type-interface concern, and dropping it is honest.
  `FuncProjection` is a pass-through of the driver's function; the
  parser doesn't model the function's identity (ADR 0005), so
  DISTINCT-on-the-function has no place to sit either.

- **`min` / `max` over `TypeList` yields `TypeUnknown`.** The TCK
  Aggregation2 scenarios [9]/[10]/[11]/[12] exercise min/max over
  lists and mixed values; the result value is a well-defined runtime
  behaviour, but the *type* the parser can commit to is
  engine-inconsistent (some engines lift list-of-int min to the
  element type; others keep it as list). The honest posture is
  `TypeUnknown` — a wrong concrete type would break codegen for
  downstream engines. The generated code re-executes the original
  text and reads the driver's return type, which honestly-Unknown
  covers.

- **`stDev/stDevP` / `percentile*` all type as TypeUnknown.** These
  aggregates' result-type tables are engine-detailed enough that
  committing at parse time would fossilise one engine's convention
  into the model. The resolver upgrades if the schema commits.
