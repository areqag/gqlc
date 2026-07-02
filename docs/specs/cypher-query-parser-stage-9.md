# Stage 9 spec — Cypher query parser: read-clause completion

The implementation brief for Stage 9 of the Cypher implementation of
`query.Parser`. Ninth model evolution after Stage 8 per ADR 0004 (test-first,
evolving until feature-complete), under the curation discipline of ADR 0003 and
the type-interface boundary of ADR 0005. Stage 9 is the fourth stage of the
ADR-0007 pre-freeze expansion beyond the read core. It **closes out the read
clauses**: `UNWIND` as a reading clause that introduces a scalar binding whose
type is derived from the source list (Stage 6 typing machinery), `WITH ... WHERE`
as a filter after projection, `WITH ... ORDER BY / SKIP / LIMIT` as intermediate
pagination, and `RETURN ... ORDER BY` accepting parameters in the sort key
(the last `ORDER BY $param` rejection is retired).

This document is a **delta** against Stages 0–8 (referenced individually where
relevant); everything not stated here carries over verbatim. Sections appear
here only where Stage 9 changes something.

Tracking: bead `gqlc-ydk` (GitHub #46). Lands as one graphite branch
(`stage-9-read-clauses`) with separated commits (spec → model red/green →
parser red/green → unlock+goldens), independently mergeable as a whole:
`just test` is green if this branch lands on `master` alone (AGENTS.md
stacked-branch invariant).

---

## 1. Deliverables

### 1.1 Binding sum extension — UnwindBinding

The `Binding` sum grows to **four** variants: `NodeBinding`, `EdgeBinding`,
`PathBinding` (Stage 8), and Stage 9's **`UnwindBinding`** — a query variable
bound to the current value drawn from an UNWIND expression's list, with its
Stage-6 result type recorded.

`BindingKind` gains one value: `BindingUnwind`, stringer `"unwind"`. The Stage
8 vocabulary (`BindingNode` / `BindingEdge` / `BindingPath`) is unchanged and
its wire tags stay stable; `"unwind"` is a new wire tag joining the discriminator
vocabulary. Two of the four tags still overlap `graph.EntityKind.String()`
("node"/"edge") for wire continuity; `"path"` and `"unwind"` are query-side only.

`UnwindBinding` shape:

```
type UnwindBinding struct {
    variable string  // the AS name; non-empty
    elemType Type    // the element type of the source list expression (Stage 6 §3)
}

func NewUnwindBinding(variable string, elemType Type) (UnwindBinding, error)
func (b UnwindBinding) Variable() string
func (b UnwindBinding) ElementType() Type
func (b UnwindBinding) Kind() BindingKind        // BindingUnwind
func (b UnwindBinding) Nullable() bool           // always false at Stage 9
```

Constructor invariants:

- `variable` is non-empty (an UNWIND without `AS name` is a grammatical error,
  so the parser never emits an anonymous UnwindBinding).
- `elemType` is non-nil (nil normalises to `TypeUnknown` at the constructor's
  boundary, matching `NewTypeList`'s convention — the "cannot tell" case is
  `TypeUnknown`, never a nil `Type`).

Wire encoding:

```
{"kind":"unwind","variable":"x","elemType":"int","nullable":false}
```

The Stage 8 wire-shape decision — every Binding variant emits a `nullable`
key so consumers do not branch on presence — carries forward. `UnwindBinding`
is never nullable at Stage 9 (see §1.4), but the field is emitted to preserve
the discipline.

`UnwindBinding` has **no `EntityKind()`** — it does not refer to a graph
entity (a scalar drawn from a list is not a node or an edge), so the
resolver never forms a schema key from it. This mirrors `PathBinding`'s
posture (Stage 8 §3.1): only entity bindings expose `EntityKind`.

### 1.2 Type-projection for a UnwindBinding

The Stage 6 rich-expression typer (`refType()` in `expr.go`) gains one arm:
a `Ref{Variable: v}` (whole entity, empty Property) where `v` names an
`UnwindBinding` in the current part types as that binding's `ElementType()`.
A property lookup on the same name (`Ref{Variable: v, Property: p}`) stays
`TypeUnknown` — the same posture the resolver takes for entity properties
(ADR 0003 owns property typing).

The imported map (`curPart.imported`) picks up an UNWIND-introduced name
the same way it picks up an entity binding at a subsequent WITH boundary
(§1.5): `exportedTypes` records the binding's `ElementType()` under its
variable name so a downstream part's classifier can look it up when the
name is exported via a bare `WITH`.

### 1.3 Retire `ErrUnsupportedClause: UNWIND`

The `EnterOC_Unwind` handler in `listener.go` no longer fails: it collects the
UNWIND clause into the current part as an `UnwindBinding` via a new
`collectUnwind` routine in `expr.go` (mirroring `collectPattern` /
`collectProjection`). The routine:

1. Reads the `AS` variable from the `OC_UnwindContext` (grammatical guarantee:
   an UNWIND without `AS` never lexes; the parser reads it via
   `c.OC_Variable().GetText()`).
2. Types the source expression via `typeExpressionMining` — the Stage-6 typer,
   which also collects parameter uses and refs.
3. Computes the **element type**: if the source types as `TypeList<T>`, the
   element type is `T`; every other source type (including `TypeUnknown`,
   `TypeNull`, or a non-list result) collapses to `TypeUnknown`. This matches
   the Stage-6 posture: **a wrong concrete type is strictly worse than an
   honest `TypeUnknown`** that the resolver can upgrade from the schema.
4. Registers every `$param` under the source expression as an `ExprUse`
   against the source expression's type (not the element type — the
   parameter participates in the source expression, whose type is what the
   model records). Position: `ExprInProjection` — an UNWIND is a
   projection-position clause (its source binds a new name; it does not
   filter). Rationale: `ExprPosition` today has two values (projection /
   predicate); adding a third for UNWIND would over-model a distinction the
   type interface does not need, and `ExprInProjection` is the closer
   analogue — an UNWIND source is a value-producing expression, not a
   filter.
5. Appends the `UnwindBinding` to `curPart.bindings` — as a graph-agnostic
   Binding, it lives in the same slice as node/edge/path bindings. The
   part's `byVar` map keys the binding by its variable name, so a same-name
   collision with a prior node/edge/path binding raises
   `ErrVariableKindConflict` (the check `mergeBinding` already runs for
   entity bindings; §1.6 extends it).
6. Every scenario in `clauses/unwind` (14 total) either parses green or
   defers via `ErrUnsupportedClause` on a downstream write clause (CREATE /
   MERGE / SET) that Stage 12–13 will retire. The three read-only UNWIND
   scenarios ([1] [2] [3] [4] [7] [8] [9] [10] [11] [12] [13]) all parse
   green; scenarios that pair UNWIND with CREATE/MERGE/SET ([5] [6] [14])
   defer.

**`ErrUnsupportedClause` is not retired.** Its remaining fail-sites are
CREATE, MERGE, SET, DELETE, REMOVE, standalone-CALL, in-query-CALL —
retired incrementally in Stages 12–14.

### 1.4 UNWIND and OPTIONAL

Grammar: `oC_Unwind` has no `OPTIONAL` prefix; UNWIND itself never introduces
a nullable binding. If the source list is empty or NULL, the UNWIND yields
zero rows — that is runtime semantics below the type-interface boundary
(ADR 0005). The resolver / engine handles the empty-result case; the
model records the binding as non-nullable. This mirrors the reasoning for
PathBinding (Stage 8 §1.2): a value that "may not exist at a row" is a
row-cardinality fact, not a per-binding static nullability.

An UNWIND inside a chain that follows an `OPTIONAL MATCH` does **not**
inherit the nullable flag either: OPTIONAL MATCH's nullability is scoped
to the bindings the OPTIONAL clause itself introduces (ADR 0006), and
UNWIND is a distinct reading clause with its own binding.

### 1.5 WITH ... WHERE

Already accepted by the current listener: `EnterOC_With` (listener.go)
calls `collectProjection(body)` then `mineWhere(w)` if a WHERE clause is
present, then closes the current part and opens a fresh one. The WHERE
mines parameter uses against the **closing** part's bindings — which is
the correct scope, because openCypher's `WITH ... WHERE` runs against the
projection body of the same clause (i.e. against the names the WITH would
export). No change to the listener structure; Stage 9 wires the
`clauses/with-where` dir (~19 scenarios) into `readCoreDirs` and expects
every positive scenario to parse green.

Two subtleties recorded honestly:

- The pair miner (`mineComparisons`) only records `PropertyUse` entries
  for pairs whose left side is a bound variable's property. When the
  WITH exports an alias — `WITH a.name AS n` — the WHERE's `n = 'x'`
  is not a `var.prop=$p` pair, so no PropertyUse is recorded. This is
  the same posture the MATCH-WHERE miner takes (a WHERE without a
  parameter reference contributes no `Use` entries).
- The rich-expression typer runs against the closing part's scope
  (which includes the bindings the WITH is about to project). Any
  parameter it finds is recorded as an `ExprUse{TypeBool,
  ExprInPredicate}` — matching the Stage-6 posture for WHERE. The
  `savedRefs` snapshot around the rich walk still restores the part's
  refs so predicate structure stays below the boundary (ADR 0003).

### 1.6 WITH ... ORDER BY / SKIP / LIMIT

`OC_Order`, `OC_Skip`, `OC_Limit` are already accepted at the projection
body level (they hang off `oC_ProjectionBody`, which RETURN and WITH
share) in `collectProjection`. Stage 9 changes one behaviour and leaves
one:

- **`ORDER BY $param` at either RETURN or WITH position** — Stage 9 accepts.
  The current `rejectClauseParameter` fails hard on any parameter inside an
  ORDER BY expression. Two TCK scenarios exercise the shape (ReturnOrderBy6
  and WithOrderBy4, both `ORDER BY $age + avg(person.age) - 1000`). The
  Stage-9 handler runs the Stage-6 rich-expression typer on the ORDER BY
  expression, which records every `$param` as an `ExprUse{TypeUnknown,
  ExprInProjection}` — the enclosing expression's result type is
  TypeUnknown (an aggregate-participating expression), and position is
  `ExprInProjection` because ORDER BY sits over a projection column.
  `savedRefs` is snapshotted around the walk to keep the ORDER BY's
  bindings out of the part's ref list (matching mineWhere's discipline:
  predicate/sort-key structure is below the boundary).
- **`SKIP $p` / `LIMIT $p`** — bare parameters at SKIP/LIMIT positions
  already type as `ClauseSlotUse` (Stage 1 §6). Any non-bare parameter
  (`SKIP $p + 1`) still fails with `ErrUnsupportedParameter` — the
  clause-slot use requires a bare atom to match a single generated
  argument (the slot). This is unchanged.

`rejectClauseParameter` is deleted; its guard-role is subsumed by the
rich-expression typer's ExprUse registration (§1.5-adjacent: the typer
records every parameter, so none silently drops).

Wiring: `clauses/return-orderby` (~35 scenarios), `clauses/with-orderBy`
(~99 scenarios), `clauses/with-skip-limit` (~9 scenarios) all enter
`readCoreDirs`. Scenarios that use writes (CREATE/MERGE) inside the
setup or the query defer via ErrUnsupportedClause.

### 1.7 Parser widening — one rejection retired

`EnterOC_Unwind` is retired (§1.3): the handler no longer fails with
`ErrUnsupportedClause: UNWIND`. `rejectClauseParameter` is deleted
(§1.6). No new rejections are added.

### 1.8 Sentinel status

`ErrUnsupportedClause` remains: it still fires for CREATE / MERGE / SET /
DELETE / REMOVE / CALL (both `oC_InQueryCall` and `oC_StandaloneCall`).
`ErrUnsupportedParameter` remains: SKIP/LIMIT non-bare parameters still
fail. `ErrUnboundVariable` and `ErrVariableKindConflict` are unchanged
(the latter extends to UnwindBinding — an UNWIND name reusing a prior
node/edge/path binding's name within the same part is a kind conflict).

`TestSentinelReachability` runs against the four-sentinel set (same as
Stage 8). The `mustReject` set gains no new entry from Stage 9: the
existing SKIP-non-bare authored case still exercises
`ErrUnsupportedParameter`; every retired shape now parses.

### 1.9 Corpus wiring

`readCoreDirs` gains **five** dirs:

- `clauses/return-orderby` — 35 scenarios across 6 files (order-by shapes:
  ints, floats, strings, lists, distinct types, expressions, aggregates,
  aliases, DESC, parameter-in-order-by).
- `clauses/with-orderBy` — 99 scenarios across 4 files (WITH intermediate
  order-by, with a trailing LIMIT to expose the ordering effect; expressions,
  aggregates, chained WITHs).
- `clauses/with-skip-limit` — 9 scenarios across 3 files (WITH intermediate
  pagination, with a following MATCH or RETURN).
- `clauses/with-where` — 19 scenarios across 7 files (post-projection
  filters, including OPTIONAL MATCH interactions).
- `clauses/unwind` — 14 scenarios in 1 file (list unwinding, list literal /
  range / concatenation / collect + unwind / null / parameter list / double
  unwind).

Total ~176 scenario outlines. Each dir's runtime-error scenarios that don't
fit bucket 1 (parse-shape) or bucket 2 (accept-and-ignore) defer via
`ErrUnsupportedClause` when a write clause appears inside the setup or the
query itself.

### 1.10 Skiplist

`clauses/unwind`, `clauses/with-where`, `clauses/with-orderBy`,
`clauses/with-skip-limit`, `clauses/return-orderby` are **clause-level**
dirs (not under `expressions/`), so their bucket-3 scenarios do NOT ride
the categorical `isBucketThreeDir` accept-and-defer. Any negative
scenario the parser now accepts must be listed by name in the
`skiplist`, pinned to bucket 3 with a comment naming the runtime rule
below the type-interface boundary.

Expected skiplist growth (audit result — audited during the unlock
commit's `-update` run; the exact list is pinned in the commit):

- **Runtime NegativeIntegerArgument / InvalidArgumentType on SKIP/LIMIT
  with a parameter or expression** — these WITH-side scenarios mirror
  the pre-existing return-skip-limit entries; the value lives below the
  boundary (ADR 0005), so the parser accepts and the skiplist defers.
  Any names surfacing in `clauses/with-skip-limit` join the same bucket-3
  block as the existing return-skip-limit entries.
- **ColumnNameConflict / NoExpressionAlias / VariableTypeConflict** on
  WITH — mirror the pre-existing entries in the `clauses/with` block
  (Stage 4). Any new occurrences in `clauses/with-where` /
  `clauses/with-orderBy` join those entries by name.
- **AmbiguousAggregationExpression** — the grouping-key correctness
  rule is a semantic constraint the type interface does not carry
  (ADR 0003). Every WITH ORDER BY / RETURN ORDER BY scenario that
  cites AmbiguousAggregationExpression joins the Stage-6 block by name.
- **UNWIND-negatives whose fail-site is a downstream write clause**
  parse-reject via `ErrUnsupportedClause` and ride PENDING — no
  skiplist entry needed.

### 1.11 Layer-2 pins

New `mustParse` cases for the Stage-9 shapes:

- **UNWIND scalar list**: `UNWIND [1, 2, 3] AS x RETURN x` → one
  UnwindBinding{"x", TypeInt}, one RETURN item RefProjection with
  type TypeInt.
- **UNWIND range function**: `UNWIND range(1, 3) AS x RETURN x` → one
  UnwindBinding{"x", TypeUnknown} — `range()` is a bare function call
  that types as TypeUnknown (function identity is below the boundary
  per ADR 0005). The RETURN item types as TypeUnknown.
- **UNWIND empty list**: `UNWIND [] AS empty RETURN empty` → one
  UnwindBinding{"empty", TypeUnknown} — an empty list literal types as
  `TypeList<TypeUnknown>`, so the element is TypeUnknown.
- **UNWIND null**: `UNWIND null AS nil RETURN nil` → one
  UnwindBinding{"nil", TypeUnknown} — a null source is not a list; the
  element type collapses to TypeUnknown (better than a wrong concrete
  type).
- **UNWIND with parameter list**: `UNWIND $props AS prop MATCH …` — the
  parameter records an ExprUse{TypeUnknown, ExprInProjection}; the
  UnwindBinding element type is TypeUnknown (a bare $param types as
  TypeUnknown). *Deferred* — the specific TCK scenario ([6] and [14])
  chains into CREATE/MERGE/SET, so it defers via
  `ErrUnsupportedClause` in the acceptance suite, but the shape is
  pinned as a mustParse case with a read-only tail (RETURN prop).
- **WITH ... WHERE**: `MATCH (a) WITH a WHERE a.name = 'B' RETURN a` →
  two parts, the first with a NodeBinding{"a"} and one RETURN item; the
  second with a RETURN item resolving against the imported name. No
  new pin structure; verified by the acceptance corpus.
- **WITH ORDER BY LIMIT**: `MATCH (a) WITH a ORDER BY a.name LIMIT 1
  RETURN a` — ORDER BY / SKIP / LIMIT stay accept-and-ignored (their
  clause structure is not modelled); the LIMIT literal is not a
  parameter, so no ClauseSlotUse fires. Verified by the acceptance
  corpus.
- **ORDER BY parameter**: `MATCH (n) RETURN n ORDER BY $p` — the
  parameter records an `ExprUse{TypeUnknown, ExprInProjection}` on the
  Parameters slice; the RETURN item is a bare RefProjection typed
  TypeNode.

`mustReject` is unchanged. The retired `EnterOC_Unwind` fail-site is
replaced by CREATE/MERGE/SET/DELETE/REMOVE at the sentinel-reachability
side (`ErrUnsupportedClause` still has multiple fail-sites).

### 1.12 Docs inline

- This spec.
- ADR 0003's amendment notes gain a Stage-9 line (the `Binding` sum
  grows a fourth variant `UnwindBinding` carrying a scalar element type;
  the parser widens its "reading clause" grammar arm accordingly).
- ADR 0007 already declared `UNWIND` retirement in Stage 9; no ADR text
  change beyond the header note.
- CONTEXT.md gets an `UnwindBinding` entry and a note extending the
  `Binding` glossary to name the fourth variant.

Nothing downstream of the parser is built (no resolver, no codegen) —
ADR 0004. The resolver's use of the UnwindBinding for cardinality
computation (an UNWIND multiplies the row count by the list length) is
resolver work and out of scope; the existing gqlc-lqm-style follow-up
bead can cover it or a new one is filed.

---

## 2. Why one atomic cycle

Adding one variant to the `Binding` sum, teaching the parser to accept
UNWIND (retiring one arm of `ErrUnsupportedClause`), accepting parameters
inside ORDER BY at both RETURN and WITH positions, and wiring five new
dirs is one restructure of the parser's reading-clause model. Splitting
the sum change from the parser change would leave `UnwindBinding` unused
on one branch; splitting the parser change from the corpus wiring would
leave the acceptance suite in a mid-migration state where the five new
dirs have no goldens. Neither split lands independently on `master`, so
Stage 9 lands as one branch.

Within the branch, the commit inventory (§7) separates spec from model
from parser from corpus wiring so review can proceed incrementally.

---

## 3. Model shape

### 3.1 BindingKind extension

```
const (
    BindingNode BindingKind = iota
    BindingEdge
    BindingPath
    BindingUnwind  // Stage 9
)
```

`String()` gains `"unwind"` for `BindingUnwind`. The other three tags
are unchanged.

### 3.2 UnwindBinding

```
type UnwindBinding struct {
    variable string
    elemType Type
}

func NewUnwindBinding(variable string, elemType Type) (UnwindBinding, error)
func (b UnwindBinding) Variable() string
func (b UnwindBinding) ElementType() Type
func (b UnwindBinding) Kind() BindingKind         // BindingUnwind
func (b UnwindBinding) Nullable() bool            // false
```

Wire encoding:

```
{"kind":"unwind","variable":"x","elemType":"int","nullable":false}
```

The `elemType` key is always emitted (matches the always-emit convention
`nullable` / `directed` / `hops` / `returnsAll` follow). Nil elemType at
construction normalises to `TypeUnknown` — the "cannot tell" case is
never a nil `Type` on the wire.

### 3.3 refType extension

`refType()` in `internal/query/cypher/expr.go` gains one arm:

```
// existing: NodeBinding -> TypeNode; EdgeBinding.Hops nil -> TypeEdge, non-nil -> list<edge>
// existing: PathBinding -> TypePath
// new: UnwindBinding -> its ElementType
```

The imported-map path is unchanged: an exported UNWIND name flows
through `exportedTypes` in `listener.go` the same way an entity binding's
type does.

### 3.4 exportedTypes extension

`exportedTypes()` in `listener.go` extends the WITH-* branch to include
UnwindBindings: their variable maps to `ElementType()` in the exported map.

---

## 4. Parser widening

### 4.1 UNWIND (§1.3)

`EnterOC_Unwind(c *gen.OC_UnwindContext)`:

- Reads `c.OC_Variable().GetText()` (grammatical guarantee).
- Types `c.OC_Expression()` via `typeExpressionMining`, collecting parameter
  uses into `params` and refs into `refs`.
- Registers every parameter as an `ExprUse{sourceType, ExprInProjection}`
  where `sourceType` is the source expression's type (not the element type;
  the parameter participates in the source expression).
- Computes the element type: if `sourceType` is `TypeList<T>`, element type
  is `T`; else `TypeUnknown`.
- Builds `UnwindBinding{variable, elementType}` and appends it to
  `curPart.bindings`.
- Registers the variable in `curPart.byVar` for the kind-conflict check.
- Registers every ref against `curPart.refs` — the source expression's
  bindings must resolve in this part.

The listener structure follows `collectProjection` / `mineWhere`: one
routine per clause, invoked from a single `EnterOC_*` handler.

### 4.2 ORDER BY parameter (§1.6)

`collectProjection` in `expr.go` currently rejects parameters in ORDER BY
expressions. Stage 9 replaces `rejectClauseParameter` with a full
rich-typer sweep: each `OC_SortItem`'s expression is typed via
`typeExpressionMining`, and every parameter is registered as an
`ExprUse{TypeUnknown, ExprInProjection}` on the parameter's uses. The
enclosing expression's type for ORDER BY is `TypeUnknown` (the sort
key's semantic role is ordering, not computation; recording a computed
type on the enclosing expression would be misleading — the parameter's
actual role is a sort-key contributor).

`savedRefs` is snapshotted around each sort-item walk (mirroring
`mineWhere`'s discipline) so ORDER BY refs stay out of the part's ref
list (ORDER BY structure is below the boundary, ADR 0003).

### 4.3 buildPart extension

`buildPart` treats UnwindBinding as any Binding: its variable enters
the part's `scope` for referential-integrity checks. No new endpoint or
ref-kind check applies (an UNWIND-var is never an edge endpoint, so
the `endpointRef` kind check is skipped by construction).

The kind-conflict check extends to UnwindBinding across **all three**
same-part collisions: entity vs unwind, path vs unwind, and
unwind vs unwind (the same-name second UNWIND in the same part).
Every one raises `ErrVariableKindConflict`. The listener catches each
at first chance so the fail-site stays local to the offending clause:

- `collectUnwind` (`expr.go`) scans `byVar`, `pathBindings`, and
  `unwindBindings` before appending the new UnwindBinding. This is
  the fail-site for entity-vs-unwind (byVar), path-vs-unwind
  (`MATCH p=(a)-->(b)` preceding `UNWIND [1] AS p`), and
  unwind-vs-unwind (`UNWIND … AS x` preceding `UNWIND … AS x` again).
- `collectPattern` (`pattern.go`) scans `unwindBindings` before
  appending a new `pathBinding`, so a `MATCH p=(...)` after
  `UNWIND [1] AS p` fails at the MATCH.
- `buildPart` (`build.go`) re-scans the three-way collision matrix
  as a belt-and-braces backstop — a fresh listener path that appends
  an UnwindBinding without going through `collectUnwind` cannot slip
  a duplicate through.

The pattern-position reuse skip (`nameBoundAsUnwind` in
`pattern.go`) is a **narrower** rule that only applies to MATCH-reuse
of an UNWIND-bound name inside a node or edge pattern. It fires only
when the UNWIND element type is `TypeNode`, `TypeEdge`, or
`TypeUnknown` — the three cases where the source list could
plausibly yield an entity value (a concrete list-of-nodes /
list-of-edges, or an aggregate whose element type is not pinned at
parse time). Any other concrete element type (int, string, bool,
list, temporal, …) falls through to `mergeBinding` → byVar
collision → `ErrVariableKindConflict`; a scalar-typed UNWIND is
never a legitimate node or edge source at the type-interface
boundary. Edge-position reuse follows the same allowlist: a
`MATCH (a)-[r]->(b)` after `UNWIND xs AS r` is accepted when `xs`'s
element type is edge or unknown, and rejected as a kind conflict for
every scalar element type (per the six fix-round `mustReject` pins).

---

## 5. Corpus and bucket-3 whitelist

The five new dirs are all clause-level, so bucket-3 categorical accept
does NOT apply (only expression-level dirs categorically defer). Every
negative scenario the parser now accepts must be listed by name in the
skiplist with a bucket-3 comment.

Layer-2 rule (Stage 1 §6). Stage 9 adds the `mustParse` cases §1.11
names. The `mustReject` set is unchanged.

`TestSentinelReachability` runs against the four-sentinel set.

---

## 6. Definition of done for Stage 9

1. `stage-9-read-clauses` lands green and independently mergeable;
   `master` is green if it lands solo.
2. `just test` green: query-package unit tests (new `UnwindBinding`,
   extended `BindingKind`, refType and exportedTypes extensions), the
   cypher-package listener tests, the `mustParse` pins, the acceptance /
   orphan / reachability suites, the property tests.
3. `just lint` green: zero issues.
4. `just fmt-check` green: zero diffs.
5. Layer-1 godog count rises by the five new dirs' scenarios (~176)
   less the runtime-writes and value-error scenarios that defer via
   `ErrUnsupportedClause` or the skiplist. Zero FAIL is mandatory.
   Success metric: every scenario whose only Stage-9 need was UNWIND or
   an ORDER BY parameter now flips PASSING; scenarios that chain into
   writes stay PENDING via `ErrUnsupportedClause`.
6. Documentation: this spec; CONTEXT.md entries; ADR 0003 note.
7. Beads: `gqlc-ydk` closed.

---

## 7. Commit inventory (single branch `stage-9-read-clauses`)

| Commit | Scope |
|--------|-------|
| prep / spec | this spec + CONTEXT.md + ADR 0003 note (docs land in the branch, matching the DoD) |
| model (red) | Failing unit tests for BindingUnwind, UnwindBinding, refType extension |
| model (green) | BindingUnwind added; UnwindBinding introduced; refType and exportedTypes extended |
| parser (red) | Failing `mustParse` cases for UNWIND shapes + ORDER BY-parameter |
| parser (green) | `EnterOC_Unwind` retired; `collectUnwind` added; ORDER BY parameter accepted; `rejectClauseParameter` deleted |
| unlock (dir + skiplist + goldens) | `readCoreDirs` gains the five dirs; skiplist entries pinned to bucket 3 for the value-level negatives; goldens regenerated and audited |

Each commit is green in isolation of the ones after it — the model
commits leave UnwindBinding unreferenced by the parser until the
parser commits use it; the parser commits leave the new dirs unwired
until the unlock commit.

---

## 8. Weakest point (recorded honestly per ADR 0004)

**UnwindBinding is not a graph entity, yet it lives in `Part.Bindings`
alongside NodeBinding and EdgeBinding.** The choice to put it there
(rather than in the `imported` map, or a separate slice) folds the
"names in scope" concept into one slice, so consumers of a query part
see one canonical list of bound names. The alternative — a separate
`ScalarBindings` slice or a typed `imported` map at construction time —
would double the resolver's per-part scope walk, and the wire shape
would gain a new key. The four-variant sum with `Kind() BindingUnwind`
is the minimal representation that keeps every consumer's per-part
scope walk one loop, and the discriminator makes the "which kind of
binding is this?" check exhaustive at the type-switch level (mirrors
the Stage 8 path binding decision, §8).

The trade-off: `UnwindBinding` shares no fields with entity bindings,
so a naive downstream walker that assumes every `Binding.EntityKind()`
returns a valid `graph.EntityKind` would panic on an UnwindBinding.
The mitigation: `EntityKind()` is only present on `NodeBinding` /
`EdgeBinding` (Stage 8 §3.1's design — path bindings have none either),
so the missing method is a **compile-time** signal to the resolver
"branch on Kind() before calling EntityKind()". The type-switch on
Kind() BindingUnwind is the honest check; a `nil` return or a synthetic
value would be strictly worse than the missing method.

The lesser risks, recorded for completeness:

- **UNWIND element type is TypeUnknown for many shapes.** The parser
  types the source expression via the Stage-6 typer; a `range()` call,
  a `null` literal, or a $param source all yield TypeUnknown for the
  source expression (function identity below the boundary, null is
  not a list, parameter type unknown). The element type therefore
  collapses to TypeUnknown for those shapes. This is honest: the
  resolver can upgrade from the schema (for a $param whose type it
  infers post-freeze), and a wrong concrete type would be strictly
  worse.
- **UNWIND parameter records an `ExprUse{sourceType,
  ExprInProjection}`.** The `ExprPosition` enum has two values today
  (projection / predicate); UNWIND is neither strictly. Choosing
  `ExprInProjection` — the value-producing side — is the closer
  analogue than `ExprInPredicate` (the filter side). Adding a third
  enum value `ExprInUnwind` would over-model a distinction the type
  interface does not need. If future stages introduce more clause-
  slot uses, the enum grows honestly then.
- **ORDER BY parameter records `TypeUnknown` for its enclosing type.**
  The parser could record `TypeBool` (ORDER BY compares) or the sort
  key's actual computed type. Both are incidental to the parameter's
  role, which is a sort-key contributor. `TypeUnknown` is honest:
  the resolver upgrades from the schema post-freeze. A concrete
  type would be strictly worse (a wrong-shape ExprUse the resolver
  cannot invalidate).
