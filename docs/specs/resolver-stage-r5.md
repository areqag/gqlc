# Stage R5 spec — resolver: multi-part & branches (WITH carry-forward, UNION, RETURN *, implicit grouping)

The implementation brief for Stage R5 of `internal/resolver`, extending the
merged R0/R1/R2/R3/R4 kernel (`docs/specs/resolver-stage-r0.md`,
`docs/specs/resolver-stage-r1.md`, `docs/specs/resolver-stage-r2.md`,
`docs/specs/resolver-stage-r3.md`, `docs/specs/resolver-stage-r4.md`) with the
five capability arms ADR 0009 assigns to R5: **multi-part queries via WITH
carry-forward, multi-branch queries via UNION, `RETURN *` / `WITH *`
expansion, implicit grouping keys for aggregate projections (including the
recorded `gqlc-gyw` contract), and the cross-WITH extension of R4's
nullability demotion**. Build this **test-first**. Scope, sequencing, error
posture, `ValidatedQuery`'s top-level shape, purity, and the golden-pair
harness inherit from ADR 0009 and R0–R4 unchanged; this document revises
only the rows, kernel arms, `ResolvedType` variants, `ValidatedQuery` axes,
sentinel set, and out-of-scope table entries that R5 changes.

Stage R5 admits every query shape R4 admits (labelled single-`MATCH`
patterns; directed/undirected × single-hop/var-length × single-type/
multi-type edges; R2 projection/parameter shapes; R4 regime-(a)
nullability), extended to:

- one or more `Branches` joined by `UNION` / `UNION ALL` — provided every
  branch is individually R5-admissible and the branches are
  column-compatible;
- one or more `Parts` per branch, each terminated by a `WITH` (non-final)
  or a `RETURN` (final) — the resolver walks Parts left-to-right, carrying
  the exported binding table across the boundary;
- `AggregateProjection` at any `RETURN` or `WITH`, with implicit grouping
  keys computed by the resolver per §4.5;
- `RETURN *` / `WITH *` (`Part.ReturnsAll == true`) with expansion owned
  by the resolver per §4.4;
- `Part.Distinct` on any Part (projection-body DISTINCT axis) — carried
  through to the wire without cardinality-checking, per §4.7.

Writes (R6), `CALL` / `YIELD` (R7), path bindings (`PathBinding`),
`UnwindBinding`, `CallBinding`, and untyped edges remain out of scope and
continue to route to `ErrOutOfR0Scope`. The same-Part regime (b)
nullability under-approximation surviving from R4 (§7.5 Class B, gap
tracked on gqlc-5xg) is unchanged at R5. The Class A OPTIONAL-clause-
sibling under-approximation (gap tracked on gqlc-ay9) is also unchanged
at R5. Neither class is closable without a model unfreeze (owner
decision pending); R5 does not contort the resolver around either.

R5 introduces **two new sentinels**: **`ErrUnionColumnMismatch`**
(§5.1), covering the column-incompatibility shapes UNION branches can
exhibit (disagreeing column count / disagreeing column names /
disagreeing column types / disagreeing nullability), and
**`ErrPartBindingTypeConflict`** (§5.1.1), covering the §6.4-discovery
case where a Part K > 0 re-declares a carried variable with a labelled
binding whose schema-typed identity differs from the carried type. The
R4 closed set is otherwise preserved.

R5's wire delta on `ValidatedQuery` is additive under the ADR 0009
provisional-through-R7 posture (§3): a `Branches []Column` axis is not
added — R5 keeps the flat `Columns []Column` top-level, because openCypher
semantics say a UNION's result columns are named by branch 0 and every
other branch is compatible-with-branch-0. `ValidatedQuery.Columns` is
therefore populated from the last Part of branch 0 (expanded per §4.4 for
`ReturnsAll`), and the compatibility check (§4.3) certifies that every
subsequent branch's last-Part columns type-match branch 0's; the wire
does not need to carry each branch's column list separately.

---

## 1. Deliverables

- `internal/resolver/validated.go` — one targeted addition:
  - a `Distinct bool` axis on `ValidatedQuery` (§3.2), carrying the
    logical-OR of every Part's `Distinct` bit AND the UNION combinator's
    distinctness (`UnionDistinct` = distinct, `UnionAll` = not). The eight
    existing R0–R4 `ResolvedType` variants and the `Column` /
    `ResolvedParameter` shapes are unchanged.
  The `ValidatedQuery.Columns` field's *meaning* widens (§3.1): at R5 it
  is populated from **the final Part of branch 0** (expanded per §4.4 for
  `ReturnsAll`); UNION branch compatibility is certified against it.
- `internal/resolver/errors.go` — two new sentinels,
  **`ErrUnionColumnMismatch`** (§5.1) and
  **`ErrPartBindingTypeConflict`** (§5.1.1), and revised prose on
  `ErrOutOfR0Scope` reflecting the R5-retired sub-cases (§5.2). R4
  sentinel identities are preserved; wrapped-message sets widen only
  where recorded.
- `internal/resolver/resolve.go` — extended with:
  - a **branch-and-part driver** (§4.1) that replaces the R4 kernel's
    single-part fast-path with a walk over `q.Branches` and, per branch,
    `branch.Parts`, running the R0–R4 per-Part resolution against each
    Part in isolation with the appropriate carried scope;
  - a **carried-scope construction** (§4.2) that computes, before each
    Part K > 0, the binding table (label → resolved type, parameter Uses,
    effective-nullability) inherited from Part K-1's WITH exports;
  - a **UNION column-compatibility check** (§4.3) run after every branch
    resolves, comparing branch B > 0's `Columns` against branch 0's;
  - a **`ReturnsAll` expansion** (§4.4) that produces the ordered column
    list from the Part's in-scope binding set when
    `Part.ReturnsAll == true`;
  - an **`AggregateProjection` handler** (§4.5) that emits the aggregate
    kind's result type and — for branch 0's final Part — computes the
    implicit grouping-key set from non-aggregate `RefProjection` /
    `LiteralProjection` / `FuncProjection` items (§4.5.2). At R5
    every `ExprProjection` residual is uniformly excluded from the
    grouping key (§4.5.3); the parser-side discrimination fix (Shape B
    `ContainsAggregate`) is a filed follow-up bead;
  - a **cross-WITH nullability extension** (§4.6) that carries R4's
    `nullableBinding` table across the WITH boundary — R4's demoteNullable
    is re-run on each Part's own bindings, seeded from the previous
    Part's exported `nullableBinding` for carried names;
  - a **`Part.Distinct` and `UnionKind` fold** (§4.7) computing the
    `ValidatedQuery.Distinct` axis.
- `test/data/resolver/valid/schemas/` — one new schema fixture
  (`social_r5.gql`, §6.2) that extends `social_r4.gql` with the shapes R5
  fixtures need to exercise: one label with disjoint properties on both
  sides of a UNION (so a mismatched-columns invalid fixture has a stable
  reference), one edge with an aggregatable numeric property.
  R0/R1/R2/R3/R4 schemas are untouched.
- `test/data/resolver/valid/*.cypher` and `.validated.golden.json` — new
  R5 valid fixtures (§6.3), each paired with its schema through the
  updated `schema.mapping.json`. **`ValidatedQuery.Distinct` is a new
  always-emit field on every golden** — every existing R0–R4 valid
  golden rebaselines with `"distinct": false` (§3.3). No other R0–R4
  golden field changes.
- `test/data/resolver/invalid/*.cypher` — new R5 invalid fixtures for
  the new sentinel (`ErrUnionColumnMismatch`) and for the retired-at-R5
  `ErrOutOfR0Scope` sub-cases that are now admitted — including the
  R4 `with_clause.cypher` fixture whose sentinel changes (§6.4). The R4
  invalid fixtures whose targets remain out of scope at R5
  (path/unwind/call binding, writes, CALL) stay unchanged.
- `internal/resolver/resolver_test.go` — updated `invalidFixtures` map
  (§6.4). No structural changes to `TestValid` / `TestInvalid` /
  `TestSentinelReachability` are required; the R0–R4 harness scales
  as-is.

Nothing downstream of the resolver is built — `ValidatedQuery` is
provisional through R7 (ADR 0009 §Decision); the added `Distinct` axis
and the widened `Columns` meaning are in-protocol additive changes to
the provisional shape.

---

## 2. Architecture — deltas from R4

R0/R1/R2/R3/R4's architecture stands (the `Resolver` struct, its
compile-time inputs, the `QueryResolver` interface + compile-time
assertion, purity and short-circuit posture, `resolve.go`/`Resolve`
split, the four-phase per-Part kernel A1 / A2 / B / C / D). R5 replaces
the R4 kernel's single-Part outer shell with a two-level driver — a walk
over branches, and per branch a walk over Parts — and factors the R4
kernel body into a per-Part callable that consumes a carried scope.
The per-Part body is R4's exactly, unchanged in behaviour, plus the
seed extensions §4.2 and §4.6 require.

### 2.1 The R5 kernel structure

The kernel's outer shape becomes:

```
Resolve(q):
  1. accept-empty-branches check         (§4.1 — always at least one branch;
                                          NewQuery invariant already enforces
                                          this, so this is a defensive tripwire)
  2. for each branchIdx b in 0..len(q.Branches)-1:
       cols, uses, err := resolveBranch(q.Branches[b], s, r)
       if err != nil: return err
       branchColumns[b] = cols
       paramUses = append(paramUses, uses...)   (§2.3 — per-branch Uses aggregated query-wide)
  3. compareBranchColumns(branchColumns)  (§4.3 — UNION column compatibility)
  4. unifyParameterUses(q.Parameters, paramUses)  (§2.3 — after column compat)
  5. compute Distinct                    (§4.7 — fold of Part.Distinct + UnionKind)
  6. return ValidatedQuery{
       Columns:    branchColumns[0],     (§3.1 — branch 0 names the result)
       Parameters: resolvedParameters,   (§2.3 — parameter merge across branches)
       Statement:  q.StatementKind,
       Distinct:   distinct,             (§3.2)
     }

resolveBranch(branch query.Branch, s schema.Schema, r procsig.Registry)
    (cols []Column, uses []parameterUseSite, err error):
  1. carriedScope := emptyScope()        (§4.2 — Part 1's incoming scope is empty)
  2. for each partIdx k in 0..len(branch.Parts)-1:
       columns, exports, partUses, err := resolvePart(branch.Parts[k], carriedScope, s, r)
       if err != nil: return nil, nil, err
       carriedScope = exports            (§4.2 — the WITH-exported carry-forward)
       uses = append(uses, partUses...)
  3. return final Part's columns and accumulated uses
```

`resolveBranch`'s three-arg pinned signature is the one referenced in §2.2,
§4.1.2, and every algorithmic pseudocode below; parameter-Use collection is
per-branch (each `resolveBranch` returns the Uses it observed) and
aggregated query-wide inside the top-level `Resolve` before the unification
lattice runs.

`resolvePart` is the R4 kernel (`resolve.go`'s current per-Part body:
Phase A1, A2, B, C, D, projection walk, parameter walk) with three
seed extensions:

- **A1 seed** — `nodeTypes`, `edgeTypes`, `edgeKeys`, `edgeCands`,
  `edgeBindings` are pre-populated from `carriedScope`'s exported binding
  tables before Phase A1 iterates this Part's own bindings (§4.2 step 3).
- **D seed** — `nullableBinding` is pre-populated with the carried names'
  effective-nullable bits before R4's demoteNullable runs on this Part's
  bindings (§4.6).
- **Projection walk** — recognises the new admitted forms
  (`AggregateProjection` per §4.5, `Part.ReturnsAll` per §4.4, `Distinct`
  per §4.7) that Phase-D-onwards R4 kernel rejected.

The per-Part kernel body itself is unchanged in edge-shape resolution,
Phase B unlabelled inference, R3 verdicts, and R4 demotion within a
Part. R5 only widens **what the kernel is called with** (the carried
scope) and **what the projection walk emits** (aggregates, `ReturnsAll`,
Distinct fold).

Parameters are resolved query-wide, not per-branch or per-Part: R5 walks
all Uses across every branch × every Part exactly once, unified per the
R2 parameter lattice (§2.3). Parameter-Use collection remains the R4
implementation — the `q.Parameters []Parameter` slice is the source of
truth (query-wide dedup at first appearance, per parser Stage 1).

### 2.2 Kernel helpers — three new; four revised

Three new helpers in `resolve.go`:

- **`resolveBranch(branch query.Branch, s schema.Schema, r procsig.Registry)
  (cols []Column, uses []parameterUseSite, err error)`** (new). Drives the
  per-Part walk within one branch. The carried `branchState` accumulator
  (§4.2 gives its concrete shape) is initialised empty inside the helper
  and threaded across the Parts; it does not appear in the exported
  signature. Parameter Uses observed within the branch are returned to the
  top-level `Resolve`, which aggregates them across branches and hands
  them to the R2 unification lattice (§2.3).
- **`compareBranchColumns(cols [][]Column) error`** (new). Runs the
  R5 UNION column-compatibility rule (§4.3): every branch's column
  count, names, and resolved types must match branch 0's. Returns
  `ErrUnionColumnMismatch` on any disagreement.
- **`expandReturnsAll(carried *branchState, part query.Part,
  perPartBindings *perPartState) []query.ReturnItem`** (new).
  Deterministically expands `Part.ReturnsAll` to an ordered
  `[]query.ReturnItem` slice against the union of `carriedScope`'s
  exported names and this Part's own bindings (§4.4). Produces the same
  virtual `ReturnItem` sequence a hand-written `RETURN a, b, c, …`
  would carry, so the projection walk that consumes the result stays
  the R4 code path.

Four R4 helpers gain revised signatures:

- **`refProjectionType`** and **`propertyUseWitness`** (R4-added
  `nullableBinding` argument). R5 additionally reads the carried
  binding tables (`carriedScope.nodeTypes`, `carriedScope.edgeTypes`,
  `carriedScope.edgeKeys`, `carriedScope.edgeCands`,
  `carriedScope.edgeBindings`) when this Part's local tables do not
  hold the ref. The lookup order is **this Part's local table first,
  then the carried table** — matching the parser's shadowing rule
  (Stage 4 §4: Part K's own bindings shadow same-named
  WITH-carried names). See §4.2 step 4.
- **`projectionType`** (R4 dispatcher over the `Projection` sum). R5
  removes the `AggregateProjection` reject arm (§4.5) and routes it to
  the new `aggregateProjectionType` handler.
- **`demoteNullable`** (R4-added). Unchanged in behaviour; R5 calls it
  per-Part with a **pre-seeded** `nullableBinding` map (§4.6) so the
  demotion algorithm reads the carried effective-nullable bits without
  regressing them.

R3 / R4's other helpers (`edgeCandidates`, `closeEdge`, `endpointLabels`,
`candidateTypes`, `touchingSide`, `intersect`, `unionProperty`,
`resolveType`, `unify`, `bindingVariable`, `qualifiedDemoter`) are
**behaviour-unchanged**.

### 2.3 Parameters — query-wide, unchanged in mechanic

`q.Parameters` is a query-wide slice with first-appearance ordering
(parser Stage 1) and dedup across every branch × Part. R5 walks every
`Parameter.Uses` exactly once, in the parser's given order, and unifies
witnesses per the R2 lattice (`resolve.go`'s `unify`). Each Use's
witness is computed against **the Part where the Use occurs** — the
parser encodes each Use as a `PropertyUse` / `ExprUse` / `ClauseSlotUse`
without a Part-identifying axis, but the resolver reconstructs which
Part each Use belongs to by walking the Parts' projections/predicates
in Part order and matching by object identity.

**Judgment call — Use-to-Part attribution via structural walk.** The
parser could have carried a `PartIndex` on each Use, but the frozen
model does not. The resolver walks each Part's projections and
predicates, and for every encountered `Ref` looks up its enclosing
Part's binding table. A parameter's Use whose witness computation
depends on a binding lookup (only `PropertyUse` — `ClauseSlotUse` and
`ExprUse` are Part-agnostic scalar types) uses the **Part-of-occurrence**
binding table. This is the only R5-touched part of parameter resolution;
the R2 unification lattice runs unchanged over the collected witnesses.

An `ExprUse` inside an `AggregateProjection`'s expression argument (per
parser aggregate-kind-rich-exprs §1.4) is Part-agnostic — its witness
is the aggregate's result type (an `ExprInProjection` position),
unaffected by which Part carries it. `ClauseSlotUse` (SKIP/LIMIT) is
similarly Part-agnostic.

### 2.4 Purity, determinism, short-circuit — unchanged

R0 §2.3 stands. R5 introduces no goroutine, no time source, no map
iteration that escapes into output. Per-branch walks proceed in
`q.Branches` slice order; per-Part walks proceed in `branch.Parts`
slice order; both are parser first-appearance order and deterministic.
Short-circuit is preserved: the first Part-level failure short-circuits
the branch, the first branch-level failure short-circuits the query.

`resolveBranch` returns as soon as any Part errors. `Resolve` returns
as soon as any branch errors OR the column-compatibility check fires.
Parameters unify at the end (after all branches resolve their columns
successfully), so a parameter-type-conflict short-circuits after
column-compatibility passes. This is the same one-verdict posture R0
committed to.

---

## 3. `ValidatedQuery` — the R5 shape

`ValidatedQuery`'s top-level shape (R0 §3.1) gains one axis at R5:
`Distinct bool`. `Columns` is unchanged in field type; its *population
rule* widens (§3.1). `Parameters` and `Statement` are unchanged. No new
`ResolvedType` variant; no new discriminator tag; no rename.

### 3.1 `ValidatedQuery.Columns` — populated from branch 0's final Part

At R0–R4, `ValidatedQuery.Columns` was the single admitted Part's
`Returns` column list. At R5 it is **the resolved column list of branch
0's final Part**, expanded for `ReturnsAll` per §4.4. This mirrors
openCypher's rule: a UNION's result columns are named by the first query
in the union (openCypher §3.6.5, "The names of the columns in the result
set of a UNION operation are the names of the columns of the first query
in the union"), and the type is the type branch 0 assigns.

The compatibility check (§4.3) certifies every other branch's final-Part
columns type-match branch 0's — same count, same names, same
`ResolvedType`. UNION does not merge types across branches; if branch 1
carries a `ResolvedProperty{INT32}` under the same column name where
branch 0 has a `ResolvedProperty{INT64}`, the query is
`ErrUnionColumnMismatch`. No type widening / lattice-join across
branches.

### 3.2 `ValidatedQuery.Distinct` — the folded distinctness axis

```go
// Distinct is the folded distinctness of the query's result set:
// true iff at least one branch's final Part carries Distinct=true (RETURN
// DISTINCT / WITH DISTINCT at branch scope) OR at least one UnionKind in
// q.Combinators is UnionDistinct (plain UNION deduplicates result rows).
// Always emitted in JSON.
Distinct bool `json:"distinct"`
```

Semantics per parser Stage 3 / part-distinct-axis: `Part.Distinct`
records the projection-body DISTINCT bit; `UnionKind.UnionDistinct`
records the UNION combinator's deduplicating variant. R5 does not
verify cardinality — codegen owns that side of the type-interface
boundary — but records the **union** of all sources so downstream
codegen can act on it uniformly.

**Judgment call — logical OR, not per-branch encoding.** A finer
representation (per-branch `Distinct`, per-Union-arm `UnionKind`) would
carry more information but is not what codegen consumes at R5; the
codegen bead is R-later and this axis's shape is provisional until then.
The OR fold is the minimum information consumers need to know whether
the whole query's result stream is deduplicated. If a future codegen
stage needs finer granularity, the axis widens under the ADR 0009
provisional-through-R7 protocol.

`ValidatedQuery.Distinct` is populated from every Part's `Distinct` bit
across every branch, plus every non-final `UnionKind` in
`q.Combinators`:

```
distinct := false
for each branch:
    for each part:
        if part.Distinct:
            distinct = true
for each combinator in q.Combinators:
    if combinator == UnionDistinct:
        distinct = true
```

### 3.2.1 `Column.GroupingKey` — the per-column grouping-key axis

`Column` gains one axis at R5 alongside the existing `Name` and
`Type`, matching the always-emit posture of `ValidatedQuery.Distinct`:

```go
type Column struct {
    Name        string       `json:"name"`
    Type        ResolvedType `json:"type"`
    GroupingKey bool         `json:"groupingKey"`
}
```

Always-emit `"groupingKey": false | true`. Semantics: `true` iff the
column participates in the openCypher implicit-grouping key set
computed for the branch's final Part (per §4.5.2). For a Part with no
`AggregateProjection` or aggregate-carrying `ExprProjection` residual,
every column is `GroupingKey == false` (no grouping applies). For a
Part with at least one aggregate, non-aggregate projections are
`GroupingKey == true` and aggregates are `GroupingKey == false`. The
per-column axis makes the column's role self-describing on the wire;
codegen filters `Columns` by `GroupingKey == true` to emit the GROUP
BY. §4.5.2 details the computation; the axis is declared here so it
sits alongside the other per-column facts.

**Non-final WITH grouping keys.** Grouping-key computation runs only
for branch 0's final Part; `ValidatedQuery.Columns` reflects only that
Part. Non-final Parts fold and re-project into the carried scope
through `exportedResolvedTypes` (§4.2.2) — that carried scope
(§4.2.1 `branchState`) has no grouping-key axis at R5. Codegen
(R-later) reaches non-final-Part aggregate structure via the
`Query.Query` source of truth if it needs it; the resolver does not
surface intermediate-Part grouping decisions.

### 3.3 R0–R4 golden rebaseline plan — two fields added

**Every existing R0–R4 golden rebaselines** on the addition of two
fields: `"distinct": false` at the top level of `ValidatedQuery`
(per this section) and `"groupingKey": false` on every `Column` (per
§3.2.1). Both are shape changes to the provisional-through-R7 shape;
the R5 code cycle regenerates goldens with `-update` at the same
commit that introduces the fields. The regenerated JSON differs from
the R4 version only by these two added lines per golden (one
`"distinct"` at top level plus one `"groupingKey"` per column); no
wire-tag renames, no field reorderings, no discriminator changes.

The affected fixtures are **every** R0–R4 valid golden — identifiable
by `ls test/data/resolver/valid/*.validated.golden.json`. The
rebaseline is universal along both axes. Every top-level
ValidatedQuery has a `Distinct` axis; fixtures with
`Part.Distinct == true` or a UNION-DISTINCT combinator do not exist at
R4 (R4 rejects both), so at the rebaseline every existing golden's
`Distinct` is `false`. Every `Column` has a `GroupingKey` axis; no
R0–R4 valid fixture contains an `AggregateProjection` (R4's
`AggregateProjection` handler is scoped-out), so at the rebaseline
every column's `GroupingKey` is `false`. The first golden to carry
`"distinct": true` is a new R5 fixture (§6.3
`distinct_projection.cypher` and `union_matched_columns.cypher`); the
earliest §6.3 fixture whose golden carries at least one
`"groupingKey": true` column is `aggregate_with_grouping.cypher`
(`RETURN a.name, count(*) AS n` — `Columns[0].GroupingKey == true`).

**Explicit rebaseline enumeration** (from `ls
test/data/resolver/valid/*.validated.golden.json | sed 's|.*/||;
s|\.cypher\.validated\.golden\.json$||' | sort` at branch base
`origin/master`, 51 stems total):

```
demote_chained_from_required
demote_from_anonymous_required_edge
demote_required_edge_endpoints
demote_undirected_edge_endpoints
demote_var_length_positive_min
demote_var_length_unbounded_lower
edge_labelled_both_endpoints
edge_property_parameter
edge_property_projection
edge_property_union_agree
expr_projection_bool
expr_projection_list
expr_projection_unknown
func_projection_temporal
func_projection_unknown
inline_endpoint_source
inline_endpoint_target
literal_int_projection
literal_string_projection
multi_type_directed_union
multi_type_undirected
no_demote_var_length_zero_min
node_mixed_projection
node_property_nullable_int
node_property_string
node_property_with_parameter
node_whole_entity
optional_edge_property
optional_edge_whole_entity
optional_multi_type_union
optional_node_nullable_property
optional_node_property
optional_node_whole_entity
optional_var_length_whole_entity
parameter_clause_slot_limit
parameter_clause_slot_skip
parameter_expr_predicate
parameter_property_and_unknown_expr
parameter_two_property_uses_agree
self_loop_directed
two_edges_shared_binding
undirected_single_match
undirected_single_match_reverse
undirected_var_length_multi_type_property
unlabelled_binding_from_edge
unlabelled_binding_target_inferred
unlabelled_via_multi_type
unlabelled_via_undirected
var_length_directed
var_length_multi_type
var_length_undirected_single_match
```

Every one of the above rebaselines with **one added line**:
`"distinct": false,` at the top level of the `ValidatedQuery` JSON
object. For a wire-shape-audit reviewer: the regeneration diff on the
R5 commit should show exactly one added line per golden and nothing
else. Any golden whose diff shows additional changes is a bug in the
R5 implementation.

**Refresh the enumeration by scripting**, not by hand-transcription:
run the `ls | sed | sort` pipeline above at the branch base commit
before publishing the R5 spec, and paste the output verbatim. Any
divergence between the enumerated stems and the disk stems at HEAD is
a bug in the spec, not in the goldens.

**Ordering note for the diff.** The `Distinct` field's position in the
JSON encoding is fixed by the Go struct field order; adding
`Distinct bool` after `Statement` in `ValidatedQuery` places `"distinct"`
after `"statement"` in the JSON. This produces a clean diff at the end
of the top-level object.

### 3.4 Wire-encoding invariants — the R5 golden rebaseline

Every R5 fixture golden carries `"distinct": false | true` as an always-
emit field. UNION-carrying goldens (§6.3) have branches encoded through
the standard multi-branch model wire (Query.Branches); the resolver
does not re-encode the multi-branch shape into `ValidatedQuery` — the
top-level `Columns` list is populated from branch 0 only, and the
`Distinct` axis records the fold.

Multi-Part goldens have their `Bindings` and `Returns` per Part on the
`Query.Query` side (that is the frozen model's wire); the resolver's
`ValidatedQuery.Columns` reflects only the **final Part of branch 0**.

---

## 4. The R5 kernel algorithm

Each step below extends or replaces a numbered step of R4 §4. R0–R4's
per-Part kernel body (Phase A1, A2, B, C, D, projection walk, parameter
walk) is preserved verbatim; R5 wraps it in a branch-and-part driver
and threads a carried scope through the Part boundary.

### 4.1 The R5 admissibility widening — multi-part, multi-branch, RETURN *, DISTINCT, AggregateProjection

R4's kernel gate at `resolve.go:24-44` rejects: multi-branch (line
24-26), multi-part (line 28-30), writes (line 33-35), Distinct (line
36-38), ReturnsAll (line 39-41), and empty bindings (line 42-44).

R5 lifts four of the six gates. Each admitted form is enumerated below;
each still-rejected form remains at its sentinel with the R4 fail-site.
The `resolve.go:24-44` block is replaced entirely by the branch-and-part
driver (§4.1.1).

#### 4.1.1 Which R4-rejected forms become R5-admitted, which remain rejected

**Admitted at R5 — every UNION form:**

- Multi-branch queries with any `UnionKind` combinator (`UnionDistinct`
  = plain `UNION`, `UnionAll` = `UNION ALL`). Branches must be
  column-compatible per §4.3; failure short-circuits with
  `ErrUnionColumnMismatch`. `Query` is a product type (parser Stage 4
  §3 lines 141-160: exported fields, builder-maintained); the parser's
  buildBranch guarantees `len(q.Combinators) == len(q.Branches) - 1`
  (see `internal/query/cypher/build.go`). R5 does not re-verify.

**Admitted at R5 — every WITH form:**

- Multi-part queries where each Part carries any admissible
  `Part.Bindings` (R4 admits, extended per §4.2), any admissible
  projection body (R2 sum plus `AggregateProjection`), and no `Effects`.
- **Every WITH modifier the parser admits carries through.** WHERE
  clauses on any Part parse as predicates that the resolver's parameter-
  Use walk consumes but does not otherwise validate at R5 (parser has
  enforced scope). ORDER BY, SKIP, LIMIT on any Part are similarly
  parse-accepted; their `Ref`s enter the parameter-Use walk (SKIP/LIMIT
  literals record `ClauseSlotUse`); no resolver rejection.
- **Aggregate projections at WITH (non-final Part) are admitted** per
  §4.5. `AggregateProjection` at any Part's `Returns` (final or
  non-final) resolves to the aggregate's result type; grouping keys
  are computed against that Part's non-aggregate projections.
- **`WITH *` (`Part.ReturnsAll == true` on a non-final Part) is
  admitted**, expanded per §4.4.
- **`WITH DISTINCT` (`Part.Distinct == true` on a non-final Part) is
  admitted**; contributes to `ValidatedQuery.Distinct` per §3.2.

**Admitted at R5 — RETURN * at the final Part:**

- `Part.ReturnsAll == true` on the final Part expands to the ordered
  column list per §4.4 and contributes to `ValidatedQuery.Columns`.
- `RETURN DISTINCT` (`Part.Distinct == true` on the final Part) is
  admitted; contributes to `ValidatedQuery.Distinct`.

**Admitted at R5 — AggregateProjection:**

- Every parser-admitted `AggregateProjection` variant: bare-argument
  (`count(n)`, `sum(x)`, `count(*)`), rich-argument (`sum(x + 1)`),
  DISTINCT-input (`count(DISTINCT n)`). Grouping keys per §4.5.2.

**Still rejected at R5 (unchanged from R4):**

| Construct | Sentinel | R-stage owner |
|---|---|---|
| Any `Effect` (write clause) in any Part | `ErrOutOfR0Scope` | R6 |
| Any `CallBinding` | `ErrOutOfR0Scope` | R7 |
| Any `PathBinding` | `ErrOutOfR0Scope` | R-later (path bindings) |
| Any `UnwindBinding` | `ErrOutOfR0Scope` | R-later (UNWIND) |
| Untyped edge (`len(Labels()) == 0`) | `ErrOutOfR0Scope` | R-later |
| `ExprProjection` typed `TypeList{TypeNode\|TypeEdge}` | `ErrOutOfR0Scope` | R-later |
| `ExprUse` at `ExprInSetValue` / `ExprInDeleteTarget` | `ErrOutOfR0Scope` | R6 |
| Property projection on a variable-length edge binding | `ErrOutOfR0Scope` | R-later |
| Same-Part regime (b) nullability under-demote | silently under-demoted (§4.6) | gqlc-5xg (Class B, model unfreeze) |
| OPTIONAL-clause-sibling nullability under-demote | silently under-demoted (§4.6) | gqlc-ay9 (Class A, model unfreeze) |

The R4 §7.4 explanation stands: cross-WITH regime (b) is R5's business
and is closed by §4.6 without a model change; same-Part regime (b) and
Class A both remain safe under-approximations gated on the two model-
unfreeze beads.

**Special case — empty bindings.** R4's `resolve.go:42-44` gate rejected
a Part with empty `Bindings`. R5 **drops that gate entirely** — empty
`Bindings` is a legitimate shape for two families of Part:

- **Part K > 0** re-projects entirely from the carried scope
  (`MATCH (a) WITH a RETURN a` — the second Part has no MATCH of its
  own, so `Part[1].Bindings` is empty and every projection Ref resolves
  against `carriedScope`).
- **Part 0** with only `LiteralProjection` / `ExprProjection` /
  `FuncProjection` projections whose Refs are empty (`RETURN 1`,
  `RETURN 1 + 2`). `NewPart` (query.go:150-159) admits this shape
  (bindings, projection, or effects — any one suffices), and the
  parser emits Part 0 with `Bindings = []`, one non-Ref-carrying
  projection, and `ReturnsAll = false`. Codegen consumes it as a
  literal-only ValidatedQuery with a single scalar column.

There is no `ErrOutOfR0Scope`-flavoured Part-0-empty-bindings sentinel
at R5; `RETURN 1` is a **valid** shape and has a matching fixture
(§6.3 `literal_only_return.cypher`). The R4 §4.4.3 rule for `RETURN *`
with empty in-scope set (accept, `Columns = []`) remains unchanged and
consistent: empty inputs at any Part are structurally admissible.

**Special case — empty final projection.** `NewPart` requires at least
one of bindings, projection, or effects (`ErrEmptyPart`), so a Part with
no bindings, no returns/ReturnsAll, and no effects is unrepresentable —
R5 does not need to guard against it.

#### 4.1.2 The branch-and-part driver — the new `Resolve` body

```go
func resolve(q query.Query, s schema.Schema, r procsig.Registry) (ValidatedQuery, error) {
    if len(q.Branches) == 0 {
        // Defensive tripwire; the parser's buildBranch guarantees >= 1
        // (Query is a builder-maintained product; parser Stage 4 §3
        // lines 141-160). Unreachable via parse.
        return ValidatedQuery{}, fmt.Errorf("%w: empty branches", ErrOutOfR0Scope)
    }

    branchCols := make([][]Column, len(q.Branches))
    var paramUses []parameterUseSite  // (see §2.3 — aggregated across branches)

    for b, branch := range q.Branches {
        cols, uses, err := resolveBranch(branch, s, r)  // §2.2 pinned signature
        if err != nil {
            return ValidatedQuery{}, err
        }
        branchCols[b] = cols
        paramUses = append(paramUses, uses...)
    }

    if err := compareBranchColumns(branchCols); err != nil {
        return ValidatedQuery{}, err
    }

    params, err := unifyParameterUses(q.Parameters, paramUses)
    if err != nil {
        return ValidatedQuery{}, err
    }

    distinct := computeDistinct(q)  // §3.2, §4.7

    return ValidatedQuery{
        Columns:    branchCols[0],
        Parameters: params,
        Statement:  StatementKind(q.StatementKind),
        Distinct:   distinct,
    }, nil
}
```

Every failure short-circuits. Parameter unification is deferred until
after column compatibility because it walks branches × parts × uses,
which is redundant work if the query is already invalid.

### 4.2 Carried scope — what a WITH exports, and how a later Part reads it

R5 walks a branch's Parts left-to-right, and after resolving Part K
computes the **carried scope** the next Part inherits. The carried
scope is R5's structural analogue of the parser's scope-map at
`build.go:244-253` (the `exported` map buildPart returns), lifted into
the resolver's typed vocabulary.

#### 4.2.1 The `branchState` shape

```go
// branchState carries the resolver-typed binding tables from Part K to
// Part K+1 within a branch. It is a subset of the per-Part
// perPartState (see §4.2.3), restricted to the names Part K's
// projection body exports (§4.2.2).
type branchState struct {
    // exportedNodeTypes holds the schema.NodeType per carried node name.
    exportedNodeTypes map[string]schema.NodeType
    // exportedEdgeTypes / exportedEdgeKeys hold the single-candidate
    // edge shape per carried edge name.
    exportedEdgeTypes map[string]schema.EdgeType
    exportedEdgeKeys  map[string]schema.EdgeKey
    // exportedEdgeCands holds the multi-candidate edge shape per
    // carried edge name. exportedEdgeTypes and exportedEdgeCands are
    // mutually exclusive per name — the R3 single/multi candidate
    // invariant carries through.
    exportedEdgeCands map[string][]schema.EdgeKey
    // exportedEdgeBindings holds the query.EdgeBinding per carried edge
    // name so §4.5 / §4.6 can dispatch on the hops axis and
    // Nullable()-derived seed for a carried binding.
    exportedEdgeBindings map[string]query.EdgeBinding
    // exportedNullableBinding holds the R4-effective nullability of
    // each carried name at the moment of export (§4.6).
    exportedNullableBinding map[string]bool
    // exportedResolvedTypes holds the ResolvedType of every carried
    // name in the shape Part K's projection assigned it — for
    // AggregateProjection or ExprProjection carried by an AS alias
    // that shadows a binding-name projection, this is the AS alias's
    // resolved type, not a binding's. Used by §4.4 to expand a
    // downstream WITH * and by §4.5 to type carried-name refs.
    exportedResolvedTypes map[string]ResolvedType
    // exportedOrder is the deterministic ordering of the carried names,
    // used by §4.4 to expand `WITH *` in a stable order. Under an
    // explicit-item WITH, order matches Part K's Returns slice order.
    // Under WITH *, order matches Part K's incoming scope order (its
    // Bindings first-appearance order, then the previous carry).
    exportedOrder []string
}
```

**Initial state.** For Part 0 in every branch, `carriedScope` is the
empty `branchState`; every map is nil, `exportedOrder` is nil. Part 0's
kernel behaves exactly as R4's kernel does today.

#### 4.2.2 What Part K's WITH exports

For a non-final Part K:

- If `Part[K].ReturnsAll == false`: the exported set is exactly the
  Part's `Returns` slice, keyed by each item's `Name`. The
  `ResolvedType` in `exportedResolvedTypes[item.Name]` is what the
  projection walk emitted for `item.Value`.
- If `Part[K].ReturnsAll == true`: the exported set is exactly the
  Part's in-scope binding set at the moment WITH ran — computed the
  same way §4.4 expands it, and the same order §4.4 uses. Every
  in-scope name (this Part's own bindings + the carried scope from
  Part K-1) is re-exported unchanged.

For the final Part (K == len(branch.Parts) - 1), the returned columns
are `ValidatedQuery.Columns` for that branch (or the compared-against
column list for branch 0), not carried forward — no next Part exists.

**Named-projection derivation.** For an `AggregateProjection` carried by
a WITH item (e.g. `WITH count(n) AS c MATCH (…) WHERE $p = c RETURN c`),
the WITH item's `Name` is the alias `c`, and its `Value` is the
`AggregateProjection`. The exported entry is `exportedResolvedTypes["c"]`
= the aggregate's result type (an integer for `count`, per §4.5.1's
kind table). Downstream references to `c` — whether as a `RefProjection`
or as a property-slot `PropertyUse` — resolve against
`exportedResolvedTypes`, not against a schema binding.

**Un-aliased projection derivation.** For `WITH n.age` (no AS alias),
`ReturnItem.Name` is the verbatim source text `n.age` per parser
`expr.go:204`. Downstream refs to `n.age` are exceedingly unusual (the
name has a dot in it), and the parser's scope check would flag any
downstream `Ref{Variable: "n.age"}` — the resolver never sees such refs
because the parser never emits them.

#### 4.2.3 How Part K+1 reads the carried scope

Before Part K+1's Phase A1 begins, its kernel seeds `nodeTypes`,
`edgeTypes`, `edgeKeys`, `edgeCands`, `edgeBindings`, and
`nullableBinding` from `carriedScope`:

```
for name, nt in carriedScope.exportedNodeTypes:
    if name not in nodeTypes:
        nodeTypes[name] = nt
for name, et in carriedScope.exportedEdgeTypes:
    …  // similar
for name, cands in carriedScope.exportedEdgeCands:
    …
for name, b in carriedScope.exportedEdgeBindings:
    edgeBindings[name] = b
for name, nb in carriedScope.exportedNullableBinding:
    nullableBinding[name] = nb  // pre-seeded; demoteNullable revises
```

**Shadowing rule (labelled re-bind).** A **labelled** name introduced
by Part K+1's own bindings would shadow a same-named carried entry —
except §6.4's `ErrPartBindingTypeConflict` fires when the schema-typed
identity disagrees (different `LabelSetKey`). Same key = trivial
re-bind, admit. Different key = irreconcilable, reject. The
edge-parity variant of the same guard covers `EdgeBinding` re-bind
across WITH (§6.4).

**Carry-wins rule (unlabelled re-bind — N1).** An **unlabelled** name
introduced by Part K+1's own bindings that is already carried does
**not** trigger Phase B inference — the carry-seeded type stays
authoritative. openCypher semantics for `WITH a MATCH (a)-[...]` is a
JOIN on the same node identity, not a redeclaration; if Phase B's
touching-edge inference would resolve `a` to a different or ambiguous
type (e.g. `MATCH (a:Post) WITH a MATCH (a)-[:AUTHORED]->(p)` where
the touching edge admits `Post-source` under one schema orientation
but any of `{Post, Person}` under the raw candidate lattice), the
carry silently wins. This also erases the order-dependence that a raw
per-Part inference would exhibit — whether an unlabelled `(a)` after
`WITH a` reinferred consistently depended on whether the enclosing
edge's other endpoint had already committed. Implementation site is
`inferUnlabelled` in `resolve.go`: pending entries whose variable is
already in the resolved table are dropped from pending before the
inference fixed-point runs.

Alternative considered: fault on any inference disagreement with the
carry, forcing the user to label the re-bind. Rejected — silent
carry-wins matches openCypher's join semantics and preserves a
non-breaking posture on happy-path multi-part queries. Recorded as an
axis; not a follow-up bead (the code is deliberate, not a gap).

The parser's `Stage 4 §4` rule "Part K's own bindings + the prior
part's WITH names" — with "own bindings" first — governs SCOPE
membership; the resolver-side identity resolution above (labelled
shadow-with-guard, unlabelled carry-wins) governs TYPE.

**Discriminating fixture (§6.3).**
`carry_wins_over_unlabelled_rebind.cypher` — `MATCH (a:Post) WITH a
MATCH (a)-[:AUTHORED]->(p) RETURN a`. Golden pins `a → node Post`.
Under a raw Phase-B inference (no carry-wins guard), the fixture goes
RED with `ErrUnknownLabel: cannot infer type of unlabelled binding
"a"` because touching-edge target `p` is also unlabelled and not
committed at Phase B (see resolve.go's Phase A2 deferral).

**Judgment call — physically-separate tables vs single merged table.**
The kernel could either (a) merge carriedScope into the local tables at
Part-start, (b) keep two tables and dispatch each Ref through a "local
first, carry second" lookup. §4.2 pseudocode above merges (option a);
the implementation may prefer option b for simplicity if the shadowing
rule is easier to encode via lookup order than via table pre-population.
Both approaches produce identical resolver output. The spec commits to
option a's semantics; the implementation chooses the mechanic.

**Named-projection carried-type entries.** When a carried name has an
`exportedResolvedTypes[name]` entry but NO entry in the
`node/edge/edgeUnion` tables (i.e. it was a projection-alias, not a
binding), a downstream `RefProjection{Variable: name, Property: ""}`
resolves to `exportedResolvedTypes[name]` — bypassing the R2
`refProjectionType` binding-lookup arm entirely. §4.5.4 details this
path.

#### 4.2.4 Cross-Part parameter Uses — attribution

**Idealised rule.** A parameter Use encountered in Part K's projection
or predicate is witnessed against **Part K's binding tables (local +
carried)**. The parameter-unification lattice (§2.3) runs at
end-of-query on the collected witnesses.

**Actual rule (post-fvo) — lexical-Part witness.** `query.Query`'s
`Parameter.Uses` slice carries a Ref (variable + optional
property) AND a Part index (fvo per ADR 0008 amendment
2026-07-06 — see `docs/specs/unfreeze-fvo-use-part.md`). The
resolver's `witnessAcrossScopes` reads `u.Part()` and witnesses
against `branchScopes[u.Part()]` exactly once — the scope of the
Part the parser attributed the Use to at emission time. If the
scope does not contain the Ref's variable, the Use contributes
zero witnesses (treated as ResolvedUnknown by the unifier). If the
scope contains the variable but the property lookup fails,
`ErrUnknownProperty` surfaces immediately.

For a `PropertyUse` this discipline closes the pre-fvo
any-valid-witness soundness gap: a `MATCH (a:Post) WITH a.title
AS a MATCH (a:Person) WHERE a.title = $p` shape — where the
lexical Part 1 has `a: Person` (no `title`), but the pre-fvo
resolver would silently witness against Part 0's `a: Post` (which
has `title`) — now fires `ErrUnknownProperty` honestly. The
discriminating fixture at §6.3 (`parameter_across_with_alias_shadow_reversed.cypher`)
pins the rejection; see `docs/specs/unfreeze-fvo-use-part.md` §7.4.

`ClauseSlotUse` and `ExprUse` remain Part-agnostic in their type
witness — the Part axis on their records is a lexical-attribution
recorder for future consumer stages, not a witness discriminator
today.

**N3 — nullability posture for cross-Part parameter Uses (strict-agree).**
The R2 lattice at §2.3 already demands nullability agreement across a
parameter's Uses: two witnesses `INT NOT NULL` and `INT NULL` do not
unify. R5 preserves this strict-agree posture across Parts: a
parameter Use in Part 0 witnessed as `INT NOT NULL` (Person.id) and
in Part 1 as `INT NULL` (Person.age) fails
`ErrParameterTypeConflict`. This is consistent with §4.3's UNION
column strictness (nullability is a resolved-type field; agreement is
required). Liberalisation to lift-to-nullable is an axis for a
possible later stage; R5 does not do it.

**Discriminating fixtures (§6.3).**
- `parameter_across_with_alias_shadow.cypher` — the P1 shape,
  pins `$p → STRING NOT NULL` (Part 1's `a: Post` has `title`;
  lexical-Part witness admits, byte-identical to pre-fvo).
- `parameter_across_union_same_name.cypher` — the P2 shape,
  pins `$p → STRING NOT NULL` (branch 1's `a: Post` has `title`;
  lexical-Part witness + UNION cross-branch fallback admits,
  byte-identical to pre-fvo).
- `parameter_across_with_multi_part.cypher` — `$p` used consistently
  across Parts (Person.id × AUTHORED.views both `INT NOT NULL`),
  pins `$p → INT NOT NULL` (unification happy path).
- `parameter_across_with_alias_shadow_reversed.cypher` (fvo,
  under `invalid/`) — Part 1's `a: Person` lacks `title`;
  lexical-Part witness fires `ErrUnknownProperty`. This is the
  post-fvo close-out of the pre-fvo any-valid-witness gap; see
  `docs/specs/unfreeze-fvo-use-part.md` §7.4.

### 4.3 UNION column compatibility

After every branch resolves its `Columns` list, R5 runs the
compatibility check:

**Rule 1 — column count.** Every branch's `len(Columns)` must equal
`len(Columns[0])`. If any branch disagrees, fail:
`ErrUnionColumnMismatch: branch %d has %d columns; branch 0 has %d`.

**Rule 2 — column names.** For each column index i,
`Columns[b][i].Name == Columns[0][i].Name` for every branch b. If any
disagrees at index i, fail: `ErrUnionColumnMismatch: branch %d column %d
named %q; branch 0 column %d named %q`.

**Rule 3 — column types.** For each column index i,
`Columns[b][i].Type` must equal `Columns[0][i].Type` for every branch b.
Type equality is Go-value equality of the `ResolvedType` (the sealed sum
carries a stable representation per variant). If any disagrees at index
i, fail: `ErrUnionColumnMismatch: branch %d column %q has type %s;
branch 0 has type %s`.

**Judgment call — no lattice widening across branches.** openCypher's
column-compatibility rule for UNION is stricter than "assignable-from":
if branch 0 has `INT32` and branch 1 has `INT64`, the result type of
each column must match branch 0's exactly, not lift to the LUB. This
mirrors openCypher §3.6.5 "the column names and types of the result set
match those of the first query". The alternative (widen types across
branches, unify via a lattice) would be a strictly more permissive
posture; R5's stricter rule matches openCypher and is the safer default.

**Judgment call — no reordering across branches.** Column index (source
order) is significant: branch 0's `RETURN a, b` and branch 1's
`RETURN b, a` fail compatibility (different order at index 0). This
matches Rule 2's index-wise comparison. openCypher's grammar preserves
source order per branch; consumers relying on positional column access
require this posture.

**Judgment call — `Distinct` folds across branches, does NOT participate
in compatibility.** Two branches disagreeing on whether they emit
`DISTINCT` internally (`RETURN DISTINCT n.name` in branch 0,
`RETURN n.name` in branch 1) is not a compatibility failure — both
resolve to `ResolvedProperty{STRING, false}`. `ValidatedQuery.Distinct`
takes the OR, per §3.2.

**Property nullability under UNION.** A column resolved to
`ResolvedProperty{STRING, false}` in branch 0 and `ResolvedProperty
{STRING, true}` in branch 1 is `ErrUnionColumnMismatch` — the nullability
bit is a resolved-type field per R4 §3.4, and the strict-equality rule
demands agreement. This is stricter than openCypher's runtime, which
would left-join and produce a nullable result column. The R4 `Nullable`
axis on `ResolvedNode` / `ResolvedEdge` / `ResolvedEdgeUnion` behaves
identically: a nullable-in-branch-0 vs non-nullable-in-branch-1 mismatch
is a compatibility failure at R5. This preserves the "codegen consumes
the branch-0-typed shape" contract without ambiguity, and mirrors the
`ErrUnknownProperty` rule (R4 §4.5) where R4's own multi-candidate edge
union demands `Nullable` agreement before emitting.

**Discriminating fixture (§6.4).** `union_column_type_mismatch.cypher`
mismatches types on one column; `union_column_name_mismatch.cypher`
mismatches names; `union_column_count_mismatch.cypher` mismatches counts;
`union_column_nullability_mismatch.cypher` mismatches nullability.

### 4.4 `Part.ReturnsAll` expansion — the resolver-owned wildcard

When `Part.ReturnsAll == true`, the resolver expands `*` to the ordered
list of **every in-scope name at that Part** (its own bindings + the
carried scope from Part K-1). This is what parser Stage 3 §3 explicitly
defers to the resolver.

#### 4.4.1 The expanded column list — order and content

The expansion produces one column per in-scope name, in this
deterministic order:

1. Every name in Part K's own `Part.Bindings`, in first-appearance
   order per `part.Bindings` slice traversal — the same order parser
   `buildPart` populates it. Anonymous bindings (variable == "")
   contribute zero columns.
2. Then every name in the carried scope (in `carriedScope.exportedOrder`
   order, per §4.2.1) **whose stem is not already covered by Part K's
   own bindings**. This shadowing-dedup step is load-bearing: the
   parser re-records same-named MATCHes as fresh bindings inside
   Part K (`parser_test.go:1218-1246` — `MATCH (a) WITH DISTINCT a
   MATCH (a)-->(b) RETURN b` gives Part 1 a fresh `NewNodeBinding("a")`
   while the carry also has `a`), and `mergeBinding`
   (`pattern.go:373-401`) dedups only within one Part's own Bindings
   slice — not against the carry. Without this dedup step, `MATCH (a)
   WITH * MATCH (a)-->(b) RETURN *` would emit `a` twice.

Concrete case: for `MATCH (a) WITH * MATCH (a)-->(b) RETURN *`, Part 1's
own bindings are `[a, r?, b]` (a re-appears as a fresh own binding);
carry from Part 0 is `[a]`. Step 1 emits `a, b` (r? is anonymous, drops).
Step 2 filters carry `a` because Part 1's own bindings cover the stem
`a`. Expansion result: `[a, b]`. Own-binding wins, not carry — matching
the R4 shadowing rule §4.2.3 and R2's `refProjectionType` lookup-order.

The carry-name-first ordering of the previous spec draft is retracted:
own-Part bindings come first (they are the immediately in-scope names
at RETURN * / WITH *), then carry names that survive shadowing. This
is what the parser's scope-in-effect ordering gives.

Duplicate names within one Part's own bindings are rejected by the
parser at Stage-0 merge (`ErrDuplicateBinding`) before the resolver
ever sees them, so step 1 produces a duplicate-free ordered list. The
"structurally impossible" claim in the previous draft was wrong for
the carry × own-Part cross case and is retracted.

#### 4.4.2 The virtual `ReturnItem` sequence

For each expanded name v, the resolver constructs a virtual
`query.ReturnItem`:

```
ReturnItem{
    Name: v,
    Value: <see per-name-shape below>,
}
```

- If v names a `NodeBinding` (either local or carried): the value is a
  virtual `RefProjection{Ref{Variable: v}, TypeNode}` — the same shape a
  hand-written `RETURN v` would carry.
- If v names an `EdgeBinding`: virtual `RefProjection{Ref{Variable: v},
  TypeEdge}` (for a single-hop) or the var-length virtual (parser's
  Stage-6 result type — TypeList{TypeEdge}).
- If v names a projection-alias carried through WITH: the resolver's
  emitted column carries `Type: exportedResolvedTypes[v]` directly —
  §4.2.3 already covers this bypass path.

The virtual `ReturnItem`s are handed to the R4 `projectionType` walker
exactly as if they had been the parser's `Returns`. This keeps the
column-emission code path identical between wildcard and explicit-item
Parts.

#### 4.4.3 `RETURN *` with an empty in-scope set

A Part with `ReturnsAll == true` and an empty expanded set (no local
bindings, no carried scope) produces `Columns = []` — an empty column
list. Parser Stage 3 §6 skiplists this as
`NoVariablesInScope` at parse time in principle, but the parser's
own rule is to accept-and-let-the-boundary-flag-it. R5 accepts:
`ValidatedQuery.Columns == []` is a legal shape (the codegen bead is
R-later; a zero-column read is a valid but degenerate query). No
sentinel fires.

**Judgment call — no `ErrEmptyProjection` at R5.** R4 has a documentary
"empty projection" reject at `resolve.go:123-125` (`ErrOutOfR0Scope`).
That reject only fires when `part.Returns == nil` AND `ReturnsAll ==
false`, which the parser rules out via `NewPart`. Under R5, the same
condition holds — `NewPart` enforces at least one of bindings /
returns / effects — so an admitted Part necessarily has *something*.
An `ReturnsAll == true` Part with empty expansion resolves to
`Columns == []` without firing that reject.

#### 4.4.4 The expansion is deterministic and repeatable

Order is fixed by the `exportedOrder` slice (§4.2.1) and the parser's
`part.Bindings` first-appearance order. Purity is preserved: two
resolves of the same query with the same schema produce byte-identical
`ValidatedQuery.Columns` — the wildcard expansion is a pure function of
`(carriedScope, part.Bindings)`.

**Discriminating fixture (§6.3).** `returns_all_simple.cypher`
(`MATCH (a:Person) RETURN *`) resolves to a single-column list with
`Columns[0].Name == "a"` and `Type == ResolvedNode{Person,
Nullable:false}`. `with_star_forward.cypher`
(`MATCH (a:Person) WITH * MATCH (a)-[:AUTHORED]->(b) RETURN a, b`)
carries `a` through, adds `b`, resolves both. A discriminating check:
the expanded order matches the source-declared order — swapping
`RETURN *` to `RETURN a, b` should produce the SAME `Columns` slice.

### 4.5 `AggregateProjection` — result-type emission and implicit grouping

`AggregateProjection` is the fifth `Projection` sum variant, admitted at
R5. The R4 dispatcher's reject arm (`resolve.go:459-460`) is removed;
the new `aggregateProjectionType` handler emits the aggregate's result
type per §4.5.1. Grouping keys are computed only for **branch 0's
final Part** — the Part whose columns populate `ValidatedQuery.Columns`
(§3.1) — because the per-column `GroupingKey` bit lives on
`ValidatedQuery.Columns` (§3.2.1) and no other consumer reads it.
Non-final Parts fold and re-project into the carried scope through
`exportedResolvedTypes` (§4.2.2); their per-item grouping-key
computation is not carried forward because the resolver's carried
scope (§4.2.1 `branchState`) has no grouping-key axis and R5's
consumers do not need one on intermediate Parts.

#### 4.5.1 Aggregate result-type table

The parser records the `AggregateFunc` kind on the `AggregateProjection`
value; parser Stage 3 §4 pins the closed set. The resolver reads it
directly and emits:

| AggregateFunc | Parser Type() | Resolver emits |
|---|---|---|
| `AggCount` | `TypeInt` | `ResolvedScalar{ScalarInt}` |
| `AggSum` | `TypeInt` / `TypeFloat` when operand commits, else `TypeUnknown` | `resolveType(p.Type())` — dispatches to `ResolvedScalar{ScalarInt}`, `ResolvedScalar{ScalarFloat}`, or `ResolvedUnknown{}` |
| `AggCollect` | `TypeList{Element: <operand type>}` | `resolveType(p.Type())` — dispatches to `ResolvedList{Element: <resolved operand>}` |
| `AggMin` / `AggMax` | Operand's parser type (commits when operand commits) | `resolveType(p.Type())` |
| `AggAvg` | `TypeUnknown` (engine-dependent per query.go:1153-1158) | `ResolvedUnknown{}` |
| `AggStdev` | `TypeUnknown` (engine-dependent) | `ResolvedUnknown{}` |
| `AggPercentile` | `TypeUnknown` (engine-dependent) | `ResolvedUnknown{}` |

**Judgment call — `AggregateProjection.Type()` is authoritative.** The
parser already types the aggregate result (per Stage 3 §4 and
aggregate-kind-rich-exprs §1.2). The resolver reads
`AggregateProjection.Type()` and passes it through `resolveType` — no
new type mapping needed. The table above is documentary; the actual
implementation dispatches on `AggregateProjection.Type()` exactly as
`ExprProjection` does today (`resolve.go:457-458`). This keeps the
handler's implementation code identical to `ExprProjection`, minus the
grouping-keys step.

The `AggregateProjection.Distinct()` axis (parser Stage 3, aggregate
input distinct) is **read but not represented in `ValidatedQuery`** at
R5 — codegen consumes it via the same
`Query.Query -> ValidatedQuery.Columns` pairing that carries other
`Projection`-level facts through the query.Query source of truth. The
resolver does not re-encode the distinct-input bit on
`ValidatedQuery.Columns[i]`; consumers reach it via
`Query.Branches[0].Parts[len-1].Returns[i].Value.(AggregateProjection).
Distinct()`.

**Judgment call — no `AggregateDistinct` axis on `Column`.**
Cardinality-affecting facts about a column (whether the aggregate
deduplicates its input; whether the whole projection body is DISTINCT;
whether UNION deduplicates) are three independent axes per parser
Stage 3 / part-distinct-axis. R5 folds `Part.Distinct` and `UnionKind`
into `ValidatedQuery.Distinct` (§3.2) because that fold is the
minimum-information consumers need to reason about the whole result
set's dedup. The per-column aggregate-DISTINCT axis is *within* one
aggregate function's row-processing — a strictly different question,
and the codegen bead R-later already reaches it through the
`AggregateProjection.Distinct()` accessor on the source `query.Query`.
Adding a redundant axis on `ValidatedQuery` would double the surface
without adding information.

#### 4.5.2 Implicit grouping keys — the algorithm

openCypher's implicit grouping rule (openCypher §3.4.5 `GROUP BY`
inference): when a Part's `Returns` mixes an `AggregateProjection` with
one or more non-aggregate projections, the non-aggregate projections
form the **implicit grouping key** (the "GROUP BY" set). A Part with
only aggregate projections has an empty grouping key (fold to a single
result row). A Part with no aggregate projections has no grouping-key
concept at all.

The grouping-key algorithm at R5:

```
computeGroupingKeys(part query.Part) []GroupingKey =
    // Grouping applies iff at least one ReturnItem is an
    // AggregateProjection. ExprProjection cannot signal aggregate
    // presence in the frozen model (§4.5.3.1), so it is not a
    // grouping trigger at R5.
    hasAggregate := false
    for each item in part.Returns:
        if item.Value is AggregateProjection:
            hasAggregate = true
            break
    if !hasAggregate:
        return nil  // no grouping applies

    keys := []GroupingKey{}
    for each item in part.Returns:
        switch item.Value.(type):
        case AggregateProjection:
            skip                    // aggregates are not grouping keys
        case RefProjection, LiteralProjection, FuncProjection:
            keys = append(keys, GroupingKeyFromRef(item))
        case ExprProjection:
            skip                    // §4.5.3 uniform-exclude posture:
                                    // ExprProjection is ALWAYS treated
                                    // as an aggregate-residual candidate
                                    // (never a grouping key) at R5,
                                    // regardless of alias status.
    return keys
```

**Judgment call — ExprProjection is uniformly excluded from grouping
keys.** The parser's Stage-6 residual classifier `classifyRichExpression`
(typing.go:857-877) unconditionally produces an opaque `ExprProjection
{refs, resultType}` for any expression that is not a bare atom / literal
/ function call / aggregate call; the aggregate structure of a nested
aggregate (`count(n) + 1`) is dropped at construction. The parser test
`"count in arithmetic"` at parser_test.go:1320-1324 pins this
concretely: `RETURN count(n) + 1` produces
`ExprProjection{[Ref{n}], TypeInt}` — refs and result type only, no
aggregate visibility. Because a resolver cannot distinguish
aggregate-carrying from aggregate-free ExprProjection using the frozen
model alone, R5 uniformly treats every `ExprProjection` as a
non-grouping-key column. See §4.5.3 for the blast-radius argument and
the follow-up beads that close this.

The `GroupingKey` computation returns a per-column boolean — true iff
the column participates in the openCypher implicit-grouping key set for
that Part. The wire representation of this axis is declared in §3.2.1
(`Column.GroupingKey bool`, always emitted) — the algorithm here fills
the per-column bit; the wire posture and per-column rationale live
alongside `Distinct` in §3.2.1 to keep the wire-shape declaration
co-located.

**R0–R4 golden rebaseline addendum (extends §3.3).** Every R0–R4 golden
rebaselines with TWO added lines: `"distinct": false,` at the top level
(per §3.3), and `"groupingKey": false,` on every column (per §3.2.1).
For a wire-shape-audit reviewer: the regeneration diff should show
exactly one `distinct` line per golden plus one `groupingKey` line per
column, and nothing else. Any golden whose diff shows additional
changes is a bug in the R5 implementation.

#### 4.5.3 `ExprProjection` residuals with nested aggregates — the R5 uniform-exclude posture

Per parser aggregate-kind-rich-exprs §1.3 and gqlc-gyw's notes: an
`ExprProjection` whose expression contains a nested aggregate
(`count(n) + 1`, `1 + sum(x)`, `CASE WHEN count(n) > 3 THEN 'a' ELSE
'b' END`) is not lowered as `AggregateProjection` — the model records
the projection as `ExprProjection{refs, type}` with the aggregate kind
invisible on the wire.

**§4.5.3.1 What the frozen model records for a rich residual — the
classifier is unconditional.** The parser's Stage-6 residual classifier
`classifyRichExpression` (`internal/query/cypher/typing.go:857-877`)
unconditionally returns `NewExprProjection(refs, t)` for every
expression that is not a bare atom / scalar literal / function call /
aggregate call. There is no branch that inspects the sub-tree for a
nested aggregate call; the whole rich expression is collapsed to
`ExprProjection{refs, resultType}`. Direct source witness — parser test
`"count in arithmetic"` (`parser_test.go:1320-1324`):

```go
src: "MATCH (n)\nRETURN count(n) + 1",
want: {Name: "count(n) + 1",
        Value: query.NewExprProjection(
            []query.Ref{{Variable: "n"}}, query.TypeInt{})},
```

Refs = `[Ref{n}]`, Type = INT, aggregate function kind DROPPED. A
resolver holding this `ExprProjection` sees only `Refs()` and `Type()`;
the aggregate structure is invisible to the wire regardless of whether
the projection has an AS alias.

**Corollary: the gqlc-gyw re-parse strategy is not implementable
against the frozen model at R5.** Any re-parse of the residual text —
whether via a synthesised `RETURN <text>` (Option P2), a direct
`ParseExpression` API (Option P1), or reading the residual off a
future `ExprProjection.OriginalText` axis (Shape A) — feeds the same
listener through the same `classifyRichExpression` code path, so the
re-parse produces the same opaque `ExprProjection{refs, type}` back.
The walk step (`walk parsed tree for AggregateFunc calls`) has nothing
to find: the aggregate is dropped at classification, not deferred to
the walker. This is orthogonal to alias status and to text-span
recovery — the discrimination capability requires a **parser-side
change** that emits either an explicit `AggregateProjection` for
nested-aggregate residuals or an additive `ContainsAggregate` bit
(Shape B). Neither exists at R5 freeze; both are follow-up beads.

**§4.5.3.2 The R5 posture: uniform-exclude for every
`ExprProjection` residual.**

R5 uniformly treats every `ExprProjection` as a non-grouping-key
column, regardless of alias status and regardless of whether the
underlying expression contained a nested aggregate. The
`computeGroupingKeys` algorithm (§4.5.2) implements this by skipping
every `ExprProjection` item — no re-parse mechanic, no alias-based
branching, no P1/P2 mechanic dependency.

**Preserved-vs-violated split for the R5 posture.** The
uniform-exclude posture has a clean split of guarantees:

- **PRESERVED — monotonicity of the grouping-decision lattice element.**
  The grouping-decision element is the set of columns marked
  `GroupingKey == true`. Excluding an `ExprProjection` from the key
  set produces a KEY-SET STRICT SUBSET of the correct one when the
  residual would have been a key (non-aggregate expression), and the
  EXACT key set when the residual would not have been a key
  (aggregate-carrying expression). Either way the resolver's declared
  keys are a subset of openCypher's implicit keys. The resolver never
  claims a group boundary that is not present.

- **VIOLATED — result-set semantics for non-aggregate ExprProjection
  residuals in an aggregate-containing Part.** When a non-aggregate
  `ExprProjection` (e.g. `1 + n.age`) appears alongside an
  `AggregateProjection` (e.g. `count(n)`), openCypher would treat the
  arithmetic residual as a grouping key and partition rows by its
  value; R5 excludes it from the key set, so consumers observe
  aggregate rollup across rows that openCypher would keep separate.
  Fewer output rows than openCypher semantics dictate.
  This is a semantically observable error, not just a coarser plan.

Note the reversal versus round 1: at the result-set level the R5
posture UNDER-groups (fewer keys than openCypher, so rows collapse
that openCypher would keep separate); round 1's spec described this
as "over-group", inverting the sign. The grouping-decision lattice
naming still holds — the resolver's key-set element is a strict
subset of the correct one — but at the result-set level the error
bites the non-aggregate residual (excluded when it should be a key),
not the aggregate residual (correctly excluded). This spec uses
"uniform-exclude" everywhere for the resolver's action ("exclude
every ExprProjection from the key set"), and "under-group" for the
result-set-level effect; "over-group" is not used.

**Blast radius of the R5 posture.** The residual-under-group case
fires exactly when ALL of the following hold for a Part K's `Returns`:

(i) at least one `AggregateProjection` present (so grouping mode is
    active — §4.5.2 `hasAggregate` predicate);
(ii) at least one `ExprProjection` present whose expression body is
     NOT itself an aggregate call.

Empirically this covers most rich-projection-and-aggregate Parts.
Fully bounded and characterisable: R5 knows which Parts are affected
by inspection alone (any Part whose `Returns` mixes at least one
`AggregateProjection` with at least one `ExprProjection`).

- **No R5 fixture in §6.3 exercises this shape.** The R5 fixture set
  deliberately does not carry a Part that mixes an `AggregateProjection`
  with an `ExprProjection`. The two aggregate fixtures
  `aggregate_with_grouping.cypher` (mixes with a plain `RefProjection`,
  which IS correctly identified as a grouping key) and
  `aggregate_with_expr_residual.cypher` (mixes with an `ExprProjection`
  RESIDUAL that contains an aggregate — the openCypher-correct
  grouping key is empty; R5 emits empty; agrees by accident) do not
  exercise the failure shape. See NIT-4-fix: the fixture set
  deliberately DOES NOT include a mixed-shape residual because R5
  cannot resolve it correctly.
- The follow-up beads (§4.5.3.3) close the gap: once the parser emits
  a discriminating signal on `ExprProjection`, R5's algorithm gains a
  branch that treats non-aggregate residuals as grouping keys and
  aggregate residuals as skipped. The uniform-skip posture is R5's
  admitted under-approximation, filed as an outstanding refinement
  bead, not as a permanent posture.

**§4.5.3.3 The unfreeze options — parser-side discrimination is
required.**

Per ADR 0008 post-freeze revision protocol, additive axes are
in-protocol. Two shape families could close the gap; both live at the
parser boundary because that is where classification happens. Shape C
(text-on-`ReturnItem`) is retired: this round's B7 finding shows that
text alone does not help — re-parsing the text feeds the SAME
`classifyRichExpression` and produces the same opaque `ExprProjection`
back. The discriminator must be produced BY the parser, not
reconstructed by the resolver.

**Shape B — `ExprProjection.ContainsAggregate bool` axis** (parser-side
discrimination bit; the gqlc-gyw-documented escape hatch, elevated to
the R5-recommended fix). `classifyRichExpression` is amended to walk
the ANTLR sub-tree for aggregate-function invocations before
constructing the projection: if any `oC_FunctionInvocation` in the
sub-tree resolves to a known `AggregateFunc` (`aggregateFunc` at
`expr.go:347`), set `ContainsAggregate = true` on the emitted
`ExprProjection`. Additive, zero-value-safe (default `false`;
wire-safe). **Pros:** direct signal at the point of classification;
the resolver reads `exprProj.ContainsAggregate()` and skips iff true;
no re-parse, no text, no unbound-ref problem. Independent of `Type`
and of AS alias. **Cons:** this is the axis gqlc-gyw's notes labelled
"the escape hatch"; adopting it accepts that the "re-parse the text
span" strategy was based on a mistaken assumption about what the
frozen model preserves. Given B7 (parser drops aggregate structure at
classification, so no re-parse recovers it), the escape hatch is the
correctly-scoped fix, not a retreat — it directly addresses the
information the frozen model does not carry.

**Shape A/A′ — parser refines the classifier to emit
`AggregateProjection` for nested-aggregate residuals** (parser-side
model refinement). Rather than mark a bit, the classifier promotes a
rich expression whose sub-tree contains at least one aggregate to an
`AggregateProjection` (populated with the aggregate's kind, refs, and
`Type`), pushing the outer arithmetic into an ExprInProjection use
against the aggregate's result. **Pros:** no new axis; the existing
sum discriminates. **Cons:** significant parser refactor —
`AggregateProjection`'s semantics change from "the projection is
literally an aggregate call" to "the projection contains at least one
aggregate", which is a semantic widening of an existing sum variant;
all downstream consumers must be audited. This is a heavier lift than
Shape B and blurs the aggregate-vs-residual boundary. Ranked below
Shape B.

**Ranking recommendation.**

1. **Shape B** — recommended. Small (one bit), sits at the exact site
   the discrimination is needed, no downstream semantic widening.
   Adopts the gqlc-gyw-documented escape hatch honestly: B7 evidence
   proves the re-parse strategy is not implementable against the
   frozen model, so the "escape hatch" characterisation was itself
   under-specified — the bit is the correctly-scoped fix, not a
   retreat from a working strategy.
2. **Shape A/A′** — second choice. Correct but a semantic widening of
   `AggregateProjection`; heavier refactor for the same signal.

Shape C (`ReturnItem.TextSpan`) and Shape A (`ExprProjection.
OriginalText`) as previously specified are retired at R5 — B7 shows
that text-based recovery cannot re-materialise the aggregate structure
that `classifyRichExpression` dropped at classification.

**R5 disposition.**

- **Uniformly exclude at R5** for every `ExprProjection` residual
  (per §4.5.2 and §4.5.3.2). This is the R5 code-cycle mechanic:
  simple, deterministic, no re-parse. Preserved-vs-violated split as
  §4.5.3.2 pins.

- **File a follow-up model unfreeze bead** — "Model: add
  `ExprProjection.ContainsAggregate bool` (Shape B per
  resolver-stage-r5 §4.5.3.3), populated by the parser's
  `classifyRichExpression` walk of the ANTLR sub-tree for aggregate-
  function invocations. Closes the R5 residual-under-group gap: the
  resolver reads the bit and skips residuals that are true, treats
  residuals that are false as grouping-key candidates. Secondary
  option: Shape A/A′ (classifier promotes nested-aggregate residuals
  to `AggregateProjection`)." Independent of gqlc-ay9 / gqlc-5xg.
  Dependency: gqlc-0mx.7 (R5 code cycle) at close; blocks the resolver
  R5 grouping-key refinement PR.

- **Do not contort R5 around the gap.** R5 ships as scoped with the
  uniform-exclude posture; the follow-up bead is filed alongside
  the R5 code-cycle close-out (per the R4 §7.5.5 template).

**§4.5.3.4 Why R5 does not attempt a resolver-side re-parse.**

The gqlc-gyw notes committed to "resolver-side re-parse of the
projection's original text span". This round-2 review discovered that
the strategy is not implementable against the frozen model, for two
independent reasons:

- **B7a — synthesise-and-parse (Option P2) breaks on unbound refs.**
  Any residual carrying a `Ref{Variable: "n"}` re-parses as `RETURN
  <text>` with no MATCH — the parser's referential-integrity sweep
  at `internal/query/cypher/build.go:155-158` (pinned by parser_test
  `"unbound variable"`) rejects unbound `n` with `ErrUnboundVariable`
  before the classifier runs. Scaffolding the ref via an anonymous
  `MATCH (n)` would work AT the parse level, but see B7b.

- **B7b — `classifyRichExpression` drops the aggregate structure at
  classification.** Even if the re-parse succeeds (bare-atom shape,
  or scaffolded refs), `classifyRichExpression` at
  `typing.go:857-877` unconditionally returns
  `NewExprProjection(refs, t)`. The `AggregateFunc` sub-tree is not
  preserved; the walker step of any P1/P2 mechanic has nothing to
  find. Parser test `"count in arithmetic"` at
  `parser_test.go:1320-1324` pins the shape: `RETURN count(n) + 1` →
  `ExprProjection{[Ref{n}], TypeInt}`, aggregate kind not recorded.

Both problems are parser-side. The resolver cannot solve them; a
parser-side change (Shape B, `ContainsAggregate` bit set by
`classifyRichExpression` during its own tree walk) is the right
level. Deferred to the follow-up bead §4.5.3.3.

The R5 code cycle SHIPS with no re-parse mechanic. The
`containsAggregateResolverSideReparse` pseudocode of round 1's spec
is retracted; no such helper is implemented; the algorithm in §4.5.2
skips every `ExprProjection` uniformly.

**§4.5.3.5 Fixture posture — no discriminating fixture at R5.**

The R5 fixture set (§6.3) deliberately does not include a Part that
mixes `AggregateProjection` with `ExprProjection`. Under the
uniform-exclude posture, such a fixture would encode the R5 gap
directly (non-aggregate `ExprProjection` gets `GroupingKey == false`
when openCypher demands `true`). Two consequences:

- The `aggregate_with_expr_residual.cypher` fixture (`MATCH
  (n:Person) RETURN 1 + count(n)`) at §6.3 is retained. Its
  `ExprProjection` residual is aggregate-carrying; openCypher's
  correct answer is empty grouping key (aggregate present, no
  non-aggregate columns); R5 emits empty grouping key (aggregate
  present, `ExprProjection` skipped); the results agree. The fixture
  documents the R5 posture; it does not test discrimination, because
  no discrimination is implemented at R5.

- A discriminating fixture cannot exist at R5. Under the
  uniform-exclude posture, every fixture that would discriminate
  residual kinds gets the same answer regardless of underlying
  residual structure.
  The discriminating fixture arrives on the follow-up bead's widening
  PR, alongside the parser-side Shape B change.

**Post-follow-up target discriminating fixture** (documented here for
the follow-up bead's design intent, NOT included in the R5 fixture
set):

- `aggregate_with_expr_grouping_key.cypher` — `MATCH (n:Person)
  RETURN 1 + n.age, count(n)`. After Shape B lands: the
  `ExprProjection` for `1 + n.age` has `ContainsAggregate == false`,
  so R5's refined algorithm marks it as a grouping key
  (`Columns[0].GroupingKey == true`); the `AggregateProjection` for
  `count(n)` is skipped (`Columns[1].GroupingKey == false`). At R5
  today, this fixture would fail (both would be `false` under the
  uniform-exclude posture, WRONG for `Columns[0]`) — which is why
  it is excluded from §6.3.

#### 4.5.4 Carried-alias projection — the `RefProjection` bypass

When a `RefProjection` in Part K's projection body references a name v
that lives ONLY in `carriedScope.exportedResolvedTypes[v]` (not in any
of the binding tables), the emitted column type is
`carriedScope.exportedResolvedTypes[v]` — bypassing the R2
`refProjectionType` binding-lookup entirely. This is what makes the
following work:

```
MATCH (n:Person) WITH count(n) AS c RETURN c
```

Part 0 exports `c` with `exportedResolvedTypes["c"] =
ResolvedScalar{ScalarInt}`. Part 1's `RefProjection{Ref{Variable:"c"}}`
emits `Column{Name:"c", Type: ResolvedScalar{ScalarInt}, GroupingKey:
false}`.

An `AS`-carried `AggregateProjection` becomes a first-class scalar
column in the next Part — same shape any binding-carrying projection
would have. Grouping-key discovery does not re-cross the WITH boundary:
each Part's grouping-key decision is local to that Part's `Returns`
list.

### 4.6 Cross-WITH nullability demotion — extending R4's regime (a) across Parts

R4's `demoteNullable` (`resolve.go:685-717`) operates on a single
Part's `Bindings` slice, seeded from `binding.Nullable()`. At R5, each
Part runs the same algorithm, but with the initial `nullableBinding`
map **pre-seeded** from the carried scope:

```
resolvePart(part, carriedScope, params):
    …  // Phase A/B/C from R4 unchanged
    nullableBinding := make(map[string]bool)
    // Seed from carriedScope's exportedNullableBinding (§4.2.3).
    for name, nb := range carriedScope.exportedNullableBinding:
        nullableBinding[name] = nb
    // Then R4's per-Part demoteNullable extends the map with the
    // Part's own bindings and iterates for local demotion.
    demoteNullableInPlace(part.Bindings, nullableBinding)
    …  // projection + parameter walks
```

**The critical R4 §7.4 hand-off.** An OPTIONAL-introduced binding
carried through WITH and re-matched in a later Part demotes via the
later Part's own Bindings slice. Concretely:

```
OPTIONAL MATCH (a)-[:R]->(b)
WITH b
MATCH (b)-[:S]->(c)
RETURN b
```

- Part 0 has `Bindings = [a, r?, b]` (r? is the anonymous edge if the
  edge is unnamed; the example uses `-[:R]->` with no variable, so the
  parser emits an anonymous EdgeBinding). Regime (a) demotes: none
  (the required edge is OPTIONAL); `a`, `b` stay `Nullable = true` after
  R4 demoteNullable in Part 0. Part 0's WITH exports `b` with
  `exportedNullableBinding["b"] = true`.
- Part 1 has `Bindings = [b (fresh non-nullable NodeBinding), r?
  (anonymous EdgeBinding), c (non-nullable NodeBinding)]`. The parser
  records `b` again as a fresh, first-introduction NodeBinding in
  Part 1 because Part 1's own MATCH re-introduces it in a required
  clause. The `mergeBinding` rule (`pattern.go:373-401`) applies WITHIN
  a single Part, not across Parts; cross-Part, Part 1's `MATCH (b)` is
  a fresh entry in `part[1].Bindings`, not a merge into Part 0's binding.

**Verification of parser modelling for the cross-WITH re-MATCH case.**

The parser's Stage 4 spec §3 (lines 171-187) asserts: "part 1 records
the nullable `b`, the `WITH` carries `b` forward, and part 2 records
its *own* non-nullable `b` from `MATCH (b)`."

**Direct source witness — parser test `"with distinct"`.**
`internal/query/cypher/parser_test.go:1218-1246` pins exactly this shape
for the query `MATCH (a) WITH DISTINCT a MATCH (a)-->(b) RETURN b`:

```go
"with distinct": {
    src: "MATCH (a)\nWITH DISTINCT a\nMATCH (a)-->(b)\nRETURN b",
    want: query.Query{Branches: []query.Branch{{Parts: []query.Part{
        {   // Part 0
            Bindings: []query.Binding{must(query.NewNodeBinding("a", nil))},
            Returns: []query.ReturnItem{
                {Name: "a", Value: query.NewRefProjection(query.Ref{Variable: "a"}, query.TypeNode{})},
            },
            Distinct: true,
        },
        {   // Part 1 — re-records "a" as a fresh NodeBinding, not a merge
            Bindings: []query.Binding{
                must(query.NewNodeBinding("a", nil)),   // <-- fresh entry
                must(query.NewEdgeBinding("", nil, must(query.NewVarEndpoint("a")), must(query.NewVarEndpoint("b")), true)),
                must(query.NewNodeBinding("b", nil)),
            },
            …
        },
    }}}},
}
```

Part 1's `Bindings[0]` is a fresh `NewNodeBinding("a", nil)` — the
non-nullable variant, because Part 1's `MATCH (a)-->(b)` is a required
clause. `NewNodeBinding` returns `Nullable() == false`; the OPTIONAL
variant `NewNullableNodeBinding` was NOT called at Part 1. This is
direct source evidence that the parser cross-WITH assumption holds.

**OPTIONAL Part 0 → Part K+1 argument.** The `"with distinct"` fixture
proves the fresh-re-binding shape for a Part 0 that is REQUIRED. The
OPTIONAL analogue is symmetric by construction: if Part 0 is `OPTIONAL
MATCH (a)`, Part 0's `Bindings[0]` is a nullable `NewNullableNodeBinding
("a", nil)`, but Part K+1's own `MATCH (a)` re-records `a` as a fresh
non-nullable NodeBinding via the same code path (parser
`build.go:155-158` scope check + Part-local `mergeBinding` per
`pattern.go:373-401`). Part K+1's OPTIONAL status is a property of the
CLAUSE that introduced `a` in Part K+1, not a property inherited from
Part 0's OPTIONAL — the parser's per-Part fresh-binding rule means each
Part decides nullability of its own binding introductions
independently. R5's per-Part local-seed override in §4.6 relies on
this: Part K+1's `nullableBinding[name]` is set from Part K+1's own
binding at that name, overwriting whatever the carry brought in. The
`"with distinct"` fixture pins the mechanic; the OPTIONAL analogue
inherits the same guarantee because the parser code path is the same.

**Concrete verification for the demotion example.** Applied to the R5
canonical `OPTIONAL MATCH (a)-[:R]->(b) WITH b MATCH (b)-[:S]->(c)
RETURN b`:

- Part 0 has `Bindings = [a (nullable NodeBinding), anon (nullable
  EdgeBinding), b (nullable NodeBinding)]`. R4's per-Part
  demoteNullable leaves everything at `Nullable = true` because no
  required edge witnesses in Part 0.
- Part 0's WITH exports `b` with `exportedNullableBinding["b"] = true`.
- Part 1 has `Bindings = [b (fresh non-nullable NodeBinding — the
  required MATCH re-introduction), anon (non-nullable EdgeBinding on
  :S), c (non-nullable NodeBinding)]`.
- Part 1's seed pass writes `nullableBinding["b"] = true` from the
  carry, then Part 1's local-Bindings seed pass overrides it to
  `nullableBinding["b"] = false` (from
  `part[1].Bindings[0].Nullable()`).
- Part 1's Phase D demoteNullable additionally demotes `c` via the
  required `:S` edge witness (R4 regime (a) applied to Part 1).

**Corner case — Part K's MATCH does NOT re-record a name.** For
`OPTIONAL MATCH (a) WITH a RETURN a`, Part 1 has no MATCH of its own
and no Bindings. Part 1's `nullableBinding["a"]` comes purely from
the carry (`exportedNullableBinding["a"] = true` in Part 0's export),
and nothing overrides it. `a` stays nullable in the resolved column.
This is the correct behaviour — no required clause proves `a`
non-NULL, so it remains nullable. Discriminated by fixture
`carry_nullable_binding.cypher` (§6.3).

**Verdict.** The cross-WITH assumption is confirmed by parser
test `"with distinct"` at `parser_test.go:1218-1246`. R5 code-cycle
does not need to independently verify this behaviour — the parser
test pins it. R5's fixture `demote_cross_with_remerge.cypher` (§6.3)
exercises the demotion end-to-end against this confirmed shape.

The complete per-Part algorithm reads:

```
Seed with carry:
    for name, nb := range carriedScope.exportedNullableBinding:
        nullableBinding[name] = nb    // Part 0's export: b -> true

Seed with local bindings (may override carry):
    for _, b := range part[k].Bindings:
        v, ok := bindingVariable(b)
        if !ok || v == "":
            continue
        nullableBinding[v] = b.Nullable()   // local override: b -> false

Then demote via Phase D (R4 unchanged):
    demoteNullable walks part[k].Bindings for required-edge witnesses,
    demoting any endpoint entry in nullableBinding[v].
```

The critical order is: **carry first, local second**. A same-named
local Binding overrides the carry, so a Part K+1 that re-MATCHes an
OPTIONAL-carried `b` sees `nullableBinding["b"] = false` after the
override step, and any further required-edge witnesses in Part K+1
(demoting `c` via `:S`, etc.) apply R4's regime (a) locally.

**Carry-only nullability propagation across Parts (no re-MATCH).** For
a name in the carried scope that Part K+1 does NOT re-reference in its
own Bindings, its `nullableBinding` entry is carried unchanged. Example: `MATCH (a:Person) WITH a MATCH (b:Post) RETURN a,
b`: Part 1's Bindings has only `b`; the `a` in `RETURN a` is served by
the carried scope. `carriedScope.exportedNullableBinding["a"]` (which
is `false` because `a` came from a required MATCH) is threaded through
into Part 1's `nullableBinding["a"] = false`, and `Column{Name:"a",
Type: ResolvedNode{Person, Nullable:false}}` emits correctly.

**§4.6.1 Same-Part regime (b) still under-approximates at R5.** R4 §7.5
Class B — same-Part bare-pattern re-MATCH of an OPTIONAL binding — is
NOT closed by R5. The gap (missing witness — parser's `mergeBinding`
discards the second occurrence within one Part) persists; the Class B
fixture in R4 §7.5.5 remains a documented under-approximation at R5
too. Owner decision on gqlc-5xg is still pending. If gqlc-5xg lands,
R5's `demoteNullable` reads the new axis; no R5-spec-level revision
needed beyond that.

**§4.6.2 Class A OPTIONAL-clause-sibling gap still under-approximates
at R5.** Same story: gqlc-ay9. R5 does not close Class A.

**Under-approximations at R5 are consistent with R4.** No new class of
under-approximation appears at R5. What R5 gains is the ability to
**demote across the WITH boundary** for the cross-WITH regime-(b) case.
R4 §7.4 item 1 was correct: the boundary is the enabler, not a new
axis.

### 4.7 `Part.Distinct` and `UnionKind` — folding into `ValidatedQuery.Distinct`

Per §3.2 the fold is:

```
distinct := false
for _, branch := range q.Branches:
    for _, part := range branch.Parts:
        if part.Distinct:
            distinct = true
for _, comb := range q.Combinators:
    if comb == query.UnionDistinct:
        distinct = true
return distinct
```

This computation runs after all branches resolve their columns, before
`Resolve` returns. It reads directly from `q.Query`'s frozen model
without additional binding table access. Deterministic; short-circuit-
safe (no fail-sites).

### 4.8 Statement kind — unchanged in mechanic

`Resolve` copies `q.StatementKind` into `ValidatedQuery.Statement` — the
R0 §4.7 step. R5 does not admit writes, so `StatementKind` is always
`read`. If a write clause appears in any Part, the write-clause reject
in §4.1.1 fires before StatementKind is computed.

### 4.9 The revised type-mapping table — R5 owner column

R4 §4.8 stands with two revisions:

- The `AggregateProjection` row (previously "R5") retires; its column
  becomes "R5 (§4.5)" with a note that R5 now emits the aggregate's
  result type from `AggregateProjection.Type()`.
- The `Part.Distinct == true` and `Part.ReturnsAll == true` rows
  (previously "R5" in R4 §7's out-of-scope table) retire; both are
  handled at R5.

No other rows change.

---

## 5. Sentinels — the R5 revision

R4's closed sentinel set is `ErrUnknownLabel`, `ErrUnknownProperty`,
`ErrOutOfR0Scope`, `ErrUnknownEdge`, `ErrAmbiguousBinding`,
`ErrParameterTypeConflict`, `ErrAmbiguousEdgeOrientation`. R5 adds two
sentinels — `ErrUnionColumnMismatch` (§5.1) and
`ErrPartBindingTypeConflict` (§5.1.1) — keeps the others, and revises
`ErrOutOfR0Scope`'s message set to reflect retirements.

### 5.1 New sentinel — `ErrUnionColumnMismatch`

```go
// ErrUnionColumnMismatch is returned when a UNION query has branches
// whose result columns disagree: different column counts, differently-
// named columns at the same index, or different resolved types at the
// same-named column. Introduced at R5. See R5 spec §4.3 for the
// compatibility rule.
ErrUnionColumnMismatch = errors.New("union column mismatch")
```

Added to `allSentinels`. Reachability sweep: at least one invalid
fixture per named case (§6.4).

**Why this sentinel is distinct from the others.**
`ErrParameterTypeConflict` (R2) is about unification within one
parameter's Uses. `ErrUnknownProperty` (R4 with the union-member-
disagreement message) is about disagreement across an EdgeUnion's
schema-declared members for one property lookup — a schema-side
divergence, not a query-side divergence. `ErrUnionColumnMismatch` is
about branches disagreeing on the query's own output shape — a
different class of error from either. Reusing `ErrParameterTypeConflict`
would be a category mistake (a UNION column disagreement is not a
parameter conflict); reusing `ErrUnknownProperty` similarly (not a
schema-lookup problem).

### 5.1.1 New sentinel — `ErrPartBindingTypeConflict`

```go
// ErrPartBindingTypeConflict is returned when a Part K > 0 re-declares
// a carried variable with a labelled binding whose schema-typed
// identity disagrees with the carried type. Concretely: at Part K a
// labelled NodeBinding for name `v` resolves to a schema.NodeType
// whose LabelSetKey differs from the carry-seed's LabelSetKey for `v`.
// Same key = trivial re-binding, admitted. Different key =
// irreconcilable, rejected. Introduced at R5. See R5 spec §6.4.
ErrPartBindingTypeConflict = errors.New("part binding type conflict")
```

Added to `allSentinels`. Reachability sweep: one invalid fixture,
`part_binding_type_conflict.cypher` (§6.4).

**Why this sentinel is distinct from the others.** The parser accepts
`MATCH (a:Person) WITH a MATCH (a:Post) RETURN a` and emits two Parts
with independent labelled `NodeBinding`s for `a` — the parser does not
reason across a WITH boundary about label conflict (parser observation
recorded in §6.4). The disagreement is between the carried
schema-typed identity and the local labelled re-binding — a
cross-Part, resolver-only class of error. `ErrAmbiguousBinding`
(R4) is about unlabelled bindings whose candidate set from touching
edges is not unique — same-Part, no carry involvement.
`ErrParameterTypeConflict` (R2) is about parameter-Use unification —
about parameter Uses, not variable declarations. `ErrUnknownLabel`
does not fit: both labels resolve. Reusing any of them would be a
category mistake.

### 5.2 Revised `ErrOutOfR0Scope` message set — retirements

The R4-era `ErrOutOfR0Scope` sub-cases that retire at R5:

- WITH / multi-part query (was `resolve.go:28-30`) — R5 admits.
- UNION / multi-branch query (was `resolve.go:24-26`) — R5 admits.
- RETURN * / WITH * (was `resolve.go:39-41`) — R5 admits.
- RETURN DISTINCT / WITH DISTINCT (was `resolve.go:36-38`) — R5 admits.
- AggregateProjection (was `resolve.go:459-460`) — R5 admits.
- Empty binding set (was `resolve.go:42-44`) — R5 admits for every
  Part K, including K == 0 (`RETURN 1`, `RETURN 1 + 2`); §4.1.1's
  updated special-case block covers this.

The R4-era `ErrOutOfR0Scope` sub-cases that remain (unchanged):

- Write clause / effects — R6 owns.
- CALL / YIELD — R7 owns.
- Path binding — R-later.
- Unwind binding — R-later.
- Untyped edge — R-later.
- ExprProjection typed list-of-nodes / list-of-edges — R-later.
- ExprUse at ExprInSetValue / ExprInDeleteTarget — R6 owns.
- Property projection on variable-length edge binding — R-later.
- Unknown projection variant (defensive tripwire) — persists.

### 5.3 R4 sentinels' message sets — mostly unchanged

`ErrUnknownLabel`, `ErrUnknownEdge`, `ErrAmbiguousBinding`,
`ErrAmbiguousEdgeOrientation`, `ErrParameterTypeConflict`,
`ErrUnknownProperty` retain their R4 fail-sites and message sets. R5
runs these per-Part; a Part K's Phase A/B/C can still fire any of them.
The messages themselves are unrevised.

**One widening — `ErrParameterTypeConflict` across Parts.** A parameter
with two Uses in two different Parts (one PropertyUse on `a.age` in
Part 0, one PropertyUse on `b.title` in Part 1, where the properties
have different types) still fires `ErrParameterTypeConflict`. The R2
lattice's failure mode extends transparently to the cross-Part case;
message format is unchanged.

### 5.4 The closed R5 set

```go
var allSentinels = []error{
    ErrUnknownLabel,
    ErrUnknownProperty,
    ErrOutOfR0Scope,
    ErrUnknownEdge,
    ErrAmbiguousBinding,
    ErrParameterTypeConflict,
    ErrAmbiguousEdgeOrientation,
    ErrUnionColumnMismatch,       // R5 addition
    ErrPartBindingTypeConflict,   // R5 addition (§6.4 discovery outcome)
}
```

Nine sentinels total. The reachability sweep in `TestSentinelReachability`
verifies every sentinel above has at least one invalid fixture pinning
it (§6.4) and every invalid fixture maps to a sentinel in this list.

---

## 6. The golden-pair harness — R5 revision

### 6.1 Schema fixture strategy — one new schema

R0–R4's harness (`resolver_test.go`, `test/data/resolver/{valid,
invalid}/`) is preserved verbatim. R5 adds fixtures under both
`valid/*.cypher` (with paired goldens) and `invalid/*.cypher` (with the
paired `invalidFixtures` map entry). One new schema fixture:

- `social_r5.gql` — extends `social_r4.gql` with (a) a `Company` node
  type (labelled with disjoint property set from `Person` and `Post`,
  so mismatched-columns UNION fixtures have a stable disjoint reference),
  and (b) an `EMPLOYS` edge type on `(Company) -[:EMPLOYS]-> (Person)`
  so cross-schema UNION and multi-part MATCH have separate edge types
  to walk.

R0–R4 schemas are untouched. Existing R0–R4 fixtures continue to
resolve against their existing schemas.

### 6.2 Schema fixture text — `social_r5.gql`

```
CREATE PROPERTY GRAPH TYPE SocialR5 AS {
    (:Person {
        id       :: INT NOT NULL,
        name     :: STRING NOT NULL,
        age      :: INT,
        nickname :: STRING,
        score    :: FLOAT NOT NULL
    }),
    (:Post {
        id       :: INT NOT NULL,
        title    :: STRING NOT NULL,
        body     :: STRING
    }),
    (:Company {
        ein      :: INT NOT NULL,
        name     :: STRING NOT NULL,
        founded  :: DATE
    }),
    (:Person) -[:AUTHORED { publishedAt :: TIMESTAMP, views :: INT NOT NULL, likedAt :: TIMESTAMP }]-> (:Post),
    (:Person) -[:LIKES { likedAt :: TIMESTAMP }]-> (:Post),
    (:Post)   -[:AUTHORED { authoredBy :: STRING NOT NULL }]-> (:Person),
    (:Person) -[:KNOWS { since :: DATE }]-> (:Person),
    (:Company) -[:EMPLOYS { since :: DATE, role :: STRING }]-> (:Person)
}
```

### 6.3 R5 valid fixtures

Each fixture keyed to an R5 arm. Each has a paired `.validated.golden.
json`, generated with `-update`.

**WITH carry-forward (§4.1.1, §4.2, §4.6):**

- `with_carry_binding.cypher` — `MATCH (a:Person) WITH a MATCH (a)
  -[:AUTHORED]->(b:Post) RETURN a, b`. Discriminates: Part 1 must
  resolve `a` from the carry. Golden: two columns (Person, Post).
- `with_carry_property_projection.cypher` — `MATCH (a:Person) WITH
  a.name AS nm RETURN nm`. Discriminates: `carriedScope.
  exportedResolvedTypes["nm"] = ResolvedProperty{STRING, false}` is
  reached by Part 1's `RefProjection`.
- `with_carry_transitive.cypher` — `MATCH (a:Person) WITH a WITH a
  RETURN a.name`. Discriminates: transitive carry across two Parts.
- `with_where_predicate.cypher` — `MATCH (a:Person) WITH a WHERE a.age
  > 18 RETURN a.name`. Discriminates: WHERE is transparent; parser
  handles scope; resolver emits one string column.
- `with_star_forward.cypher` — `MATCH (a:Person) WITH * MATCH (a)
  -[:AUTHORED]->(b:Post) RETURN a, b`. Discriminates: `WITH *` carries
  `a` forward without an explicit item.

**UNION column compatibility (§4.3):**

- `union_matched_columns.cypher` — `MATCH (a:Person) RETURN a.name AS
  nm UNION MATCH (c:Company) RETURN c.name AS nm`. Discriminates: both
  branches emit one column named `nm` of type STRING; passes
  compatibility. `Distinct = true` per §3.2.
- `union_all_matched_columns.cypher` — same as above with `UNION ALL`.
  `Distinct = false`.

**RETURN * / WITH * expansion (§4.4):**

- `returns_all_simple.cypher` — `MATCH (a:Person) RETURN *`. One
  column, `a` of `ResolvedNode{Person, Nullable:false}`.
- `returns_all_multiple_bindings.cypher` — `MATCH (a:Person),
  (b:Post) RETURN *`. Two columns: `a` (Person), `b` (Post), in
  declaration order.
- `returns_all_with_edge.cypher` — `MATCH (a:Person)-[r:AUTHORED]->
  (b:Post) RETURN *`. Three columns (a, r, b) in that order.

**AggregateProjection (§4.5):**

- `aggregate_count_star.cypher` — `MATCH (a:Person) RETURN count(*) AS
  n`. One column, `ScalarInt`. `GroupingKey == false` (empty group).
- `aggregate_sum_property.cypher` — `MATCH (a:Person) RETURN sum(a.age)
  AS s`. Aggregate over a bare property argument; result type is
  `ResolvedUnknown{}` per the parser's bare-property AggregateProjection
  typing (parser_test.go pin `"aggregate sum on bare property arg
  (regression)"` at parser_test.go:1541 emits `AggregateProjection{AggSum,
  [{n, age}], false, TypeUnknown{}}`). The resolver's AggregateProjection
  arm at §4.5.1 is a straight `resolveType(pp.Type())` pass-through
  (resolve.go:958-959), so the schema-declared `Person.age :: INT` does
  not enrich the aggregate's result type — the aggregate stays Unknown,
  and the golden pins `"kind": "unknown"`. This is the honest witness of
  today's parser+resolver contract; schema-side enrichment of aggregate
  operand types is a design axis for a later stage (out of R5 scope).
- `aggregate_with_grouping.cypher` — `MATCH (a:Person) RETURN a.name,
  count(*) AS n`. Two columns; `Columns[0].GroupingKey == true`,
  `Columns[1].GroupingKey == false`.
- `aggregate_with_expr_residual.cypher` — `MATCH (n:Person) RETURN 1 +
  count(n)`. One column, an `ExprProjection` residual. Grouping mode
  is active (an `AggregateProjection` — the `count(n)` — sits INSIDE
  the residual, but the parser collapses the whole rich expression to
  `ExprProjection{[Ref{n}], TypeInt}` per §4.5.3.1 and
  parser_test.go:1320-1324, so R5 sees no top-level
  `AggregateProjection` at all — `hasAggregate` in §4.5.2 evaluates
  false and grouping does not fire; the single residual gets
  `GroupingKey == false`. `Columns[0].GroupingKey == false`. This
  fixture documents the R5 posture: agrees with openCypher's empty
  grouping key by construction, does NOT discriminate the residual's
  aggregate content (§4.5.3.5).
- `aggregate_at_with.cypher` — `MATCH (a:Person) WITH count(a) AS n
  RETURN n`. Aggregate at non-final Part carries through as a scalar
  column.

**Cross-WITH nullability demotion (§4.6):**

- `demote_cross_with_remerge.cypher` — `OPTIONAL MATCH (a)-[:R1]->(b)
  WITH b MATCH (b)-[:R2]->(c) RETURN b`. Discriminates the R4 §7.4
  item 1 hand-off: the second Part's required MATCH demotes `b`. This
  fixture doubles as the parser-shape verification (§4.6 parser
  assumption); the golden pins the outcome per the confirmed-assumption
  path.
- `carry_nullable_binding.cypher` — `OPTIONAL MATCH (a:Person) WITH a
  RETURN a`. Discriminates: `a` stays nullable across the WITH boundary
  (no demotion evidence).

**Part.Distinct (§4.7):**

- `distinct_projection.cypher` — `MATCH (a:Person) RETURN DISTINCT
  a.name`. `Distinct = true`.
- `with_distinct.cypher` — `MATCH (a:Person) WITH DISTINCT a.name AS
  nm RETURN nm`. `Distinct = true` (non-final Part contributes).

**Literal-only Part 0 (§4.1.1 special case — empty Bindings):**

- `literal_only_return.cypher` — `RETURN 1`. Discriminates the
  §4.1.1 "empty bindings is legitimate on Part 0 when only literal /
  expression / func projections carry the query" case. One column,
  `Columns[0].Name == "1"` (parser expr.go:204 verbatim text — no
  alias), `Type == ResolvedScalar{ScalarInt}`, `GroupingKey == false`,
  `Distinct == false`. The R4 `resolve.go:42-44` gate is removed for
  this shape.

**Cross-Part parameter Uses (§4.2.4 lexical-Part witness, post-fvo):**

- `parameter_across_with_alias_shadow.cypher` — `MATCH (a:Person) WITH
  a.name AS a MATCH (a:Post) WHERE a.title = $p RETURN a`. The P1
  discriminator: Part 0's `a` is `Person` (no `title`); Part 1's `a`
  is `Post` (has `title`). Golden pins column `a → node Post` and
  parameter `$p → STRING NOT NULL`. RED under a naive
  every-scope-must-agree witness (Part 0 fails `a.title`).
- `parameter_across_union_same_name.cypher` — `MATCH (a:Person) RETURN
  a.id AS x UNION MATCH (a:Post) WHERE a.title = $p RETURN a.id AS x`.
  The P2 discriminator: two UNION branches, same variable name, two
  different schema types. Golden pins column `x → INT NOT NULL`,
  `$p → STRING NOT NULL`, `Distinct = true` (UNION default). RED under
  the naive rule (branch 0's `a: Person` scope fails `a.title`).
- `parameter_across_with_multi_part.cypher` — `MATCH (a:Person) WHERE
  a.id = $p WITH a MATCH (a)-[e:AUTHORED]->(pst:Post) WHERE e.views =
  $p RETURN pst.title`. Happy-path $param×WITH: `$p` unified across
  two Parts on same-typed operands (Person.id × AUTHORED.views, both
  `INT NOT NULL`). Golden pins `$p → INT NOT NULL`. Not
  discriminating for C1 (passes under both rules); coverage for the
  `$param + WITH` gap.

**Carry-wins unlabelled re-bind (§4.2.3 N1):**

- `carry_wins_over_unlabelled_rebind.cypher` — `MATCH (a:Post) WITH a
  MATCH (a)-[:AUTHORED]->(p) RETURN a`. Golden pins column
  `a → node Post`. RED under a raw Phase-B inference without the
  carry-wins guard (Phase B's `candidateTypes` yields nothing for
  `a` — target `p` is unlabelled and not committed at Phase B).

**RETURN * / WITH * local-first ordering (§4.4.1):**

- `returns_all_local_first_ordering.cypher` — `MATCH (a:Person),
  (b:Post) WITH a, b MATCH (b:Post), (a:Person) RETURN *`. Golden
  pins columns `[b, a]` — Part 1's local bindings win first-appearance
  order over the carried `[a, b]`. Discriminates the local-first
  invariant against the carry-first mutant.

### 6.4 R5 invalid fixtures — updated `invalidFixtures` map

**Retiring at R5 (moved to valid/ OR removed):**

- `with_clause.cypher` was R4-invalid (multi-part reject). At R5,
  multi-part is admitted, so this fixture is either:
  - Retired (removed from invalid/, no valid counterpart needed —
    already covered by `with_carry_binding.cypher` in valid/);
  - Repurposed with a different WITH shape that is still invalid at
    R5. Given no clean choice, **retire the fixture** and confirm
    `ErrOutOfR0Scope` retains reachability via other still-out-of-scope
    entries (`aggregate_projection` is retired too, but many others
    remain: `expr_use_set_value`, `list_of_nodes_projection`,
    `list_of_edges_projection`, `untyped_edge`,
    `var_length_edge_property_projection`).
- `aggregate_projection.cypher` was R4-invalid; retired at R5, moved
  or replaced by `aggregate_count_star.cypher` in valid/.
- `return_distinct.cypher` was R4-invalid; retired at R5, moved or
  replaced by `distinct_projection.cypher` in valid/.
- `returns_all.cypher` was R4-invalid; retired at R5, moved or
  replaced by `returns_all_simple.cypher` in valid/.
- `optional_match_with_clause.cypher` was R4-invalid; retired at R5,
  moved or replaced by `demote_cross_with_remerge.cypher` in valid/.

**Retaining at R5:**

- `unknown_label`, `unknown_property`, `unknown_edge`,
  `unknown_edge_property`, `ambiguous_unlabelled_binding`,
  `unlabelled_binding_no_edge`, `empty_inline_endpoint`,
  `parameter_type_conflict_*`, `unknown_property_via_expr_use`,
  `expr_use_set_value`, `list_of_nodes_projection`,
  `list_of_edges_projection`, `ambiguous_edge_orientation`,
  `unknown_edge_undirected`, `unknown_edge_multi_type_all_miss`,
  `unknown_property_union_missing`,
  `unknown_property_union_type_differs`, `untyped_edge`,
  `var_length_edge_property_projection`.

**Adding at R5:**

- `union_column_count_mismatch.cypher` — `MATCH (a:Person) RETURN
  a.name UNION MATCH (a:Person) RETURN a.name, a.age`. →
  `ErrUnionColumnMismatch`.
- `union_column_name_mismatch.cypher` — `MATCH (a:Person) RETURN
  a.name UNION MATCH (a:Person) RETURN a.age AS name2`. Different
  aliases at index 0. → `ErrUnionColumnMismatch`.
- `union_column_type_mismatch.cypher` — `MATCH (a:Person) RETURN
  a.name AS x UNION MATCH (a:Person) RETURN a.age AS x`. STRING vs
  INT. → `ErrUnionColumnMismatch`.
- `union_column_nullability_mismatch.cypher` — `MATCH (a:Person)
  RETURN a.name AS x UNION MATCH (a:Person) RETURN a.nickname AS x`
  (name is NOT NULL, nickname is nullable). → `ErrUnionColumnMismatch`.
- `union_unknown_label_branch.cypher` — `MATCH (a:Person) RETURN
  a.name UNION MATCH (b:NotDeclared) RETURN b.name`. Second branch
  fails resolution independently. → `ErrUnknownLabel` (from branch 1).
- `part_binding_type_conflict.cypher` — `MATCH (a:Person) WITH a
  MATCH (a:Post) RETURN a`. Part 1 re-declares `a` with a conflicting
  label. **Discovery outcome (R5 code cycle):** the parser ACCEPTS this
  input and emits two Parts with independent labelled `NodeBinding`s
  for `a` — Part 0 `NodeBinding{a, {Person}}`, Part 1
  `NodeBinding{a, {Post}}`. The parser does not reason across a WITH
  boundary about label conflict; per-Part fresh re-binding pins hold
  (see parser_test.go:1219-1246 for the unlabelled analogue). Same-Part
  duplicates like `MATCH (a:Person), (a:Post) RETURN a` collapse to a
  single `NodeBinding{a, {Person, Post}}` in the parser (within-Part
  conjunctive labels), so the WITH-boundary shape is the ONLY input
  that reaches the resolver as two labelled bindings for the same name
  with disjoint schema types. R5 rejects with `ErrPartBindingTypeConflict`
  at Phase A1 of `resolvePart` (resolve.go, in the labelled-node arm):
  when the carry seed at §4.2.3 has already populated `nodeTypes[v]`
  with a `schema.NodeType` whose `Labels` (LabelSetKey) differs from
  the local binding's, the re-binding is genuinely irreconcilable and
  fails. Same `LabelSetKey` = trivial re-binding, admitted (preserves
  the non-breaking posture on happy-path multi-part queries).
- `part_binding_type_conflict_edge.cypher` — `MATCH (a:Person)-[r:KNOWS]->
  (b:Person) WITH r MATCH (x:Person)-[r:LIKES]->(y:Post) RETURN r`.
  Edge parity of the node case. Same-name edge re-bind across WITH
  with a different label set (`KNOWS` vs `LIKES`) is the direct
  analogue — an irreconcilable cross-Part edge re-typing. R5 rejects
  with `ErrPartBindingTypeConflict` (same sentinel — same conflict
  class). Same `Labels().Key()` = trivial re-bind, admitted. Reuses
  the existing sentinel; count stays at nine. Local edge re-bind also
  clears any carried `edgeTypes`/`edgeKeys`/`edgeCands` for `r` so
  Phase A2/C's `closeEdge` is authoritative for the new binding's
  source/target endpoints. openCypher on this shape: Cypher's
  identifier-uniqueness rule for relationship variables within a
  pattern is stricter than the standard's re-declaration semantics
  across MATCHes; treating cross-MATCH edge re-bind with a different
  label as a fault aligns with the intuitive "the second MATCH cannot
  possibly match the same edge instance" reading, while trivial
  same-label re-bind stays a join on the same edge identity.

Updated `invalidFixtures` map:

```go
var invalidFixtures = map[string]error{
    // R0/R1/R2/R3/R4 retained:
    "unknown_label.cypher":                                 ErrUnknownLabel,
    "unknown_property.cypher":                              ErrUnknownProperty,
    "unknown_edge.cypher":                                  ErrUnknownEdge,
    "unknown_edge_property.cypher":                         ErrUnknownProperty,
    "ambiguous_unlabelled_binding.cypher":                  ErrAmbiguousBinding,
    "unlabelled_binding_no_edge.cypher":                    ErrUnknownLabel,
    "empty_inline_endpoint.cypher":                         ErrUnknownLabel,
    "parameter_type_conflict_two_properties.cypher":        ErrParameterTypeConflict,
    "parameter_type_conflict_clause_slot_vs_string.cypher": ErrParameterTypeConflict,
    "parameter_type_conflict_property_vs_expr_bool.cypher": ErrParameterTypeConflict,
    "parameter_type_conflict_nullability.cypher":           ErrParameterTypeConflict,
    "unknown_property_via_expr_use.cypher":                 ErrUnknownProperty,
    "expr_use_set_value.cypher":                            ErrOutOfR0Scope,
    "list_of_nodes_projection.cypher":                      ErrOutOfR0Scope,
    "list_of_edges_projection.cypher":                      ErrOutOfR0Scope,
    "ambiguous_edge_orientation.cypher":                    ErrAmbiguousEdgeOrientation,
    "unknown_edge_undirected.cypher":                       ErrUnknownEdge,
    "unknown_edge_multi_type_all_miss.cypher":              ErrUnknownEdge,
    "unknown_property_union_missing.cypher":                ErrUnknownProperty,
    "unknown_property_union_type_differs.cypher":           ErrUnknownProperty,
    "untyped_edge.cypher":                                  ErrOutOfR0Scope,
    "var_length_edge_property_projection.cypher":           ErrOutOfR0Scope,
    // R5 additions:
    "union_column_count_mismatch.cypher":                   ErrUnionColumnMismatch,
    "union_column_name_mismatch.cypher":                    ErrUnionColumnMismatch,
    "union_column_type_mismatch.cypher":                    ErrUnionColumnMismatch,
    "union_column_nullability_mismatch.cypher":             ErrUnionColumnMismatch,
    "union_unknown_label_branch.cypher":                    ErrUnknownLabel,
    "part_binding_type_conflict.cypher":                    ErrPartBindingTypeConflict,
    "part_binding_type_conflict_edge.cypher":               ErrPartBindingTypeConflict,
}

// Removed at R5 (moved to valid/):
//   "with_clause.cypher"           (retired — multi-part is admitted)
//   "aggregate_projection.cypher"  (retired — aggregates are admitted)
//   "return_distinct.cypher"       (retired — DISTINCT is admitted)
//   "returns_all.cypher"           (retired — RETURN * is admitted)
//   "optional_match_with_clause.cypher"  (retired — cross-WITH is admitted)
```

### 6.5 Determinism check — R5 additions

Beyond R4's determinism checks:

- **Two Resolves of the same UNION query produce the same
  `ValidatedQuery.Columns` slice** — branch iteration is
  slice-order-deterministic; no map iteration escapes.
- **Two Resolves of the same multi-part query produce the same
  `ValidatedQuery.Distinct` value** — the fold is a pure function of
  `q`.
- **`ReturnsAll` expansion is order-stable** — the `exportedOrder`
  slice fixes the order (§4.4.1).
- **Cross-WITH nullability is order-stable** — Parts iterate in
  `branch.Parts` order; seed → local override → Phase-D demote is
  deterministic within each Part.

### 6.6 Non-obvious harness invariants — R5 additions

- **Happy path values unchanged.** The R0–R4 golden rebaseline
  (§3.3 refined by §4.5.2) adds exactly one top-level `distinct` field
  and one per-column `groupingKey` field. Every other field is
  byte-identical to the R4 golden. `TestValid` remains a strict
  `JSONEq` against the rebaselined goldens.
- **Every new sentinel has ≥ 1 invalid fixture.**
  `ErrUnionColumnMismatch` has four (mismatch by count, name, type,
  nullability). `TestSentinelReachability` sweeps.
- **`invalidFixtures` remains total against `invalid/*.cypher`.**
  The retirement-and-addition delta preserves the R0 harness invariant.
- **`compareBranchColumns` is order-independent in error reporting.**
  The fail-message names the first-diverging branch and index (branch
  1 vs branch 0, etc.); rerunning the same fixture yields the same
  message.

---

## 7. R5 capability scope — what resolves

**In scope:** a single Cypher query the parser accepts, whose parsed
`query.Query` shape is:

- One or more `Branch`es in `Branches`; `Combinators` matches
  `len(Branches) - 1` per the parser's smart-constructor invariant.
- Every branch has one or more `Part`s in its `Parts` slice.
- Each Part's `Bindings` is either:
  - a non-empty slice of `NodeBinding` and/or `EdgeBinding` values
    (R4-admitted shapes — labelled or Phase-B-inferable nodes; directed
    or undirected × single-hop or var-length × single-type or
    multi-type edges; nullable or required per OPTIONAL MATCH); or
  - empty AND Part index > 0 (a projection-only Part consuming the
    carried scope).
- Each Part's `Returns` is a non-empty slice of `ReturnItem`s or
  `ReturnsAll == true` (mutually exclusive per Stage 3). Each
  `ReturnItem.Value` is any of the five `Projection` variants:
  `RefProjection`, `LiteralProjection`, `FuncProjection`,
  `AggregateProjection`, or `ExprProjection`.
- Each Part's `Distinct` is any value; R5 folds it into
  `ValidatedQuery.Distinct`.
- Each Part's `Effects` is empty.
- `Parameters` is a slice of `Parameter`s with the R2 shape.
- `StatementKind` is `StatementRead`.

**Out of scope, routed to the appropriate sentinel:**

R4's out-of-scope table survives with revisions:

| Construct | Sentinel | R-stage owner |
|---|---|---|
| Untyped edge (`len(Labels()) == 0`) | `ErrOutOfR0Scope` | R-later |
| Path binding | `ErrOutOfR0Scope` | R-later |
| Unwind binding | `ErrOutOfR0Scope` | R-later |
| Call binding | `ErrOutOfR0Scope` | R7 |
| `ExprProjection` typed `TypeList{TypeNode\|TypeEdge}` | `ErrOutOfR0Scope` | R-later |
| Property projection on a variable-length edge binding | `ErrOutOfR0Scope` | R-later |
| `ExprUse` at `ExprInSetValue` / `ExprInDeleteTarget` | `ErrOutOfR0Scope` | R6 |
| Writes / CREATE / MERGE / SET / REMOVE / DELETE | `ErrOutOfR0Scope` | R6 |
| CALL / YIELD | `ErrOutOfR0Scope` | R7 |
| Every candidate `(label, orientation)` misses the schema | `ErrUnknownEdge` | (unchanged) |
| Single-type undirected edge whose two orientations both match | `ErrAmbiguousEdgeOrientation` | (unchanged) |
| Property lookup on a multi-candidate edge; property missing on some union member | `ErrUnknownProperty` | (unchanged) |
| Property lookup on a multi-candidate edge; property type/nullability differs across union members | `ErrUnknownProperty` | (unchanged) |
| Labelled node with no matching schema NodeType | `ErrUnknownLabel` | (unchanged) |
| Unlabelled node with an empty candidate set from R3-widened touching edges | `ErrUnknownLabel` | (unchanged) |
| Unlabelled node with a multi-candidate set that survives Phase B fixed-point | `ErrAmbiguousBinding` | (unchanged) |
| Parameter Uses that do not unify | `ErrParameterTypeConflict` | (unchanged) |
| UNION branches disagree on column count, names, types, or nullability | `ErrUnionColumnMismatch` | **R5 (this stage)** |
| Nullability upgrades (regime (b), same-Part re-MATCH — Class B: missing-witness model gap) | silently under-demoted | gqlc-5xg (model unfreeze) |
| Nullability upgrades (OPTIONAL-clause-sibling — Class A: missing-group-membership model gap) | silently under-demoted | gqlc-ay9 (model unfreeze) |
| `ExprProjection` residual mixed with `AggregateProjection` in the same Part's Returns — grouping-key discrimination gap | silently under-grouped (uniform-exclude posture) | §4.5.3.3 follow-up bead (Shape B `ContainsAggregate` parser-side bit) |
| Cross-Part parameter Use where the true attributed Part would reject but another same-name Part admits — Use→Part attribution gap (§4.2.4) | **closed by gqlc-fvo (2026-07-06)**: `docs/specs/unfreeze-fvo-use-part.md` threads `Part` on every `Use` and the resolver's `witnessAcrossScopes` witnesses against `branchScopes[u.Part()]` exactly. Residual WITH-aliased-projection shadow (see fvo spec §7.6.1) is filed for a future scope-attribution cycle. | closed |

**Silently accepted (not routed anywhere):**

R0/R1/R2/R3/R4's silently-accepted set stands unchanged. R5 adds:
- WHERE / ORDER BY / SKIP / LIMIT on any Part (per §4.1.1); the parser
  handles scope-checking, the resolver walks the parameter Uses.
- `AggregateProjection.Distinct()` (aggregate-input DISTINCT) — read
  from the Query.Query source; not re-encoded on ValidatedQuery.

**Recorded ADR 0009 cross-check.** ADR 0009 R5: "`WITH` carry-forward
scope checks, `UNION` column compatibility, `RETURN *` expansion (the
resolver owns expansion), and implicit grouping keys — including the
recorded `gqlc-gyw` contract: grouping keys for `ExprProjection`
residuals come from a resolver-side re-parse of the projection's
original text span; the `ContainsAggregate` axis is only the ADR 0008
escape hatch." R5 as this spec scopes it:

- **WITH carry-forward scope checks:** implemented in §4.2 through the
  `branchState` carried scope. Scope enforcement itself is at the
  parser (Stage 4 §4, `build.go:155-158`); the resolver's job is
  **type-carrying** the WITH-exported names into the next Part's
  binding tables. No new sentinel needed (the parser rejects
  unbound-variable refs).
- **UNION column compatibility:** implemented in §4.3 with
  `ErrUnionColumnMismatch`. Rules 1-3 (count, names, types) plus the
  nullability sub-rule.
- **RETURN * expansion:** implemented in §4.4 through the resolver-
  owned virtual-ReturnItem construction over the in-scope binding set.
- **Implicit grouping keys:** implemented in §4.5.2 via a per-Part
  algorithm.
- **`gqlc-gyw` re-parse contract:** NOT implemented at R5. Round-2
  evidence (§4.5.3.4) shows the re-parse strategy is not implementable
  against the frozen model — `classifyRichExpression` (typing.go:
  857-877) drops aggregate structure at classification, so no
  resolver-side re-parse recovers it, regardless of P1/P2/text-span
  mechanic. R5 ships with uniform-exclude of every `ExprProjection`
  residual (§4.5.2, §4.5.3.2). One follow-up bead (§7.1.5) files the
  parser-side discrimination bit (Shape B — `ExprProjection.
  ContainsAggregate`, populated by the parser during the same walk
  that types the sub-tree).
- **`ContainsAggregate` axis re-characterised.** Round 1 treated it
  as a retreat from re-parse; round 2 promotes it to the R5-
  recommended parser-side fix, because B7 evidence shows the re-parse
  strategy was never workable and the bit is the correctly-scoped
  fix.

### 7.1 Under-approximation vs the bead's canonical example (R4 template applied to R5)

Following R4 §7.5's honest-state template:

#### 7.1.1 openCypher semantics

Cross-WITH re-MATCH of an OPTIONAL-introduced binding: `OPTIONAL MATCH
(a)-[:R]->(b) WITH b MATCH (b) RETURN b`. On every surviving row, the
row survives Part 1's required MATCH (b), so `b` is non-NULL. Ideal
flow-typing demotes `b`. Same for `MATCH (b)-[:S]->(c)` — both `b` and
`c` are non-NULL.

Grouping-key semantics for `ExprProjection`-residuals mixed with
`AggregateProjection`: e.g. `MATCH (n:Person) RETURN n.age + 1,
count(n)`. openCypher's implicit grouping rule (openCypher §3.4.5)
partitions rows by every non-aggregate projection; here the arithmetic
residual `n.age + 1` (an `ExprProjection` on Refs=[{n,age}]) is a
grouping key and `count(n)` aggregates within each partition. R5's
uniform-exclude posture (§4.5.3.2) treats the residual as
`GroupingKey == false`, so rows with different `n.age + 1` values are
folded together — fewer output rows than openCypher.
The complementary case `RETURN 1 + count(n)` (nested aggregate in the
residual) has no non-aggregate projection, so openCypher's implicit
grouping key is empty; R5 also emits empty grouping key (no
top-level aggregate, `hasAggregate` false — the residual is opaque);
the results agree by construction (see
`aggregate_with_expr_residual.cypher` at §6.3).

#### 7.1.2 The frozen-model gap, stated honestly

R4 recorded two gaps (gqlc-ay9, gqlc-5xg). R5 discovers one new gap
distinct from both:

- **Cross-WITH regime (b) — closed by R5 without a model change**
  (§4.6). Not a gap; the parser's cross-WITH re-MATCH behaviour is
  pinned by `parser_test.go:1218-1246` (`"with distinct"` test case:
  Part 1 re-records `a` as a fresh non-nullable NodeBinding, so the
  local seed override closes the demotion path). R4 §7.4 item 1's
  hand-off statement is directly evidenced by source.
- **`ExprProjection` residual aggregate-content discrimination**
  (§4.5.3.2): `classifyRichExpression` (typing.go:857-877) collapses
  every rich expression to opaque `ExprProjection{refs, resultType}`
  regardless of whether the sub-tree contains a nested aggregate;
  parser_test.go:1320-1324 pins the shape. Consequence: no
  resolver-side re-parse can distinguish `count(n) + 1` from `n.age
  + 1`; gqlc-gyw's re-parse strategy is not implementable against
  the frozen model (§4.5.3.4). Distinct from gqlc-ay9 (missing group
  membership on `Binding`) and gqlc-5xg (missing witness on `Binding`
  after mergeBinding). This gap sits at the parser's classification
  boundary and is closed by a parser-side change (Shape B, §4.5.3.3).

#### 7.1.3 What R5-as-scoped loses, quantified

- **Cross-WITH regime (b) — nothing lost.** Closed by §4.6 without a
  model change. The parser evidence in `parser_test.go:1218-1246`
  confirms Part K+1 re-records same-named MATCHes as fresh Bindings,
  so R5's per-Part local-seed override sees the required MATCH and
  demotes.
- **`ExprProjection` residual grouping-key discrimination:** R5
  uniformly excludes every `ExprProjection` residual from the
  grouping-key set (§4.5.2, §4.5.3.2). Consumers running a query that
  mixes an `AggregateProjection` with a non-aggregate `ExprProjection`
  observe aggregate rollup across rows that openCypher would partition
  by the residual's value — fewer output rows than openCypher
  semantics dictate. The R5 code cycle KNOWS which Parts are affected:
  any Part whose `Returns` mixes at least one `AggregateProjection`
  with at least one `ExprProjection`. The blast radius is fully
  characterisable by inspection; consumers can be warned. No R5
  fixture exercises the failure shape (§6.3 deliberately excludes
  mixed-shape residuals; §4.5.3.5).

#### 7.1.4 The unfreeze options (following §4.5.3.3)

Two shape candidates for the residual-discrimination gap, ranked:

1. **Shape B** — `ExprProjection.ContainsAggregate bool` axis.
   Recommended. Parser-side discrimination bit set by
   `classifyRichExpression` during the same ANTLR walk that types the
   sub-tree. Zero-value-safe; smallest possible signal at the exact
   site the information becomes available; resolver consumes with one
   accessor call, no re-parse.
2. **Shape A/A′** — parser refines the classifier to promote
   nested-aggregate residuals to `AggregateProjection`. Correct but
   heavier: semantic widening of an existing sum variant; downstream
   audit required.

Shape A (`ExprProjection.OriginalText`) and Shape C (`ReturnItem.
TextSpan`) as previously proposed are retired at R5 — B7 shows that
text-based recovery cannot re-materialise the aggregate structure
dropped by `classifyRichExpression` at classification.

R5 spec commits to Shape B as the recommendation. Owner still holds
merge authority for the unfreeze PR, but reviews a specific
recommendation, not a menu — per the R4 §7.5.5 precedent.

#### 7.1.5 Recommendation

**R5 proceeds as scoped; one follow-up bead is filed alongside the R5
code-cycle close-out, on the model of R4 §7.5.5 beads 1+2.**

Rationale:
- The uniform-exclude posture is deterministic and simple:
  every `ExprProjection` residual gets `GroupingKey == false`, no
  re-parse, no alias branching, no unbound-ref problem.
- The failure mode is bounded and characterisable: any Part whose
  `Returns` mixes at least one `AggregateProjection` with at least one
  `ExprProjection` mis-groups the residual. R5 knows these Parts
  syntactically; consumers can be warned at code-generation time.
- No R5-fixture-level correctness gap that goes undetected: §6.3
  deliberately does not include a fixture with the failure shape;
  §4.5.3.5 documents the excluded post-follow-up target fixture
  (`aggregate_with_expr_grouping_key.cypher`).
- Shape B is ADR-0008 in-protocol (an additive bool on
  `ExprProjection`); the follow-up is a normal escalation, not an
  ADR supersedure. No parser-API follow-up (P1) is needed under
  fix 4 — the resolver does not re-parse at R5, and under Shape B it
  does not re-parse post-unfreeze either; the discrimination bit is
  read directly. The round-1 P1 follow-up bead is RETIRED.

**Follow-up bead — parser-side discrimination (Shape B):**

- "Model: add `ExprProjection.ContainsAggregate bool` (Shape B per
  resolver-stage-r5 §4.5.3.3), populated by the parser's
  `classifyRichExpression` walk of the ANTLR sub-tree for aggregate-
  function invocations. Closes the R5 `ExprProjection`-residual
  grouping-key gap: the resolver reads `exprProj.ContainsAggregate()`
  and treats residuals with `false` as grouping-key candidates,
  `true` as skipped. Secondary option if Shape B is rejected:
  Shape A/A′ (classifier promotes nested-aggregate residuals to
  `AggregateProjection`, semantic widening of the sum variant)."
  Independent of gqlc-ay9 / gqlc-5xg. Dependency: gqlc-0mx.7 (R5 code
  cycle) at close; blocks the resolver R5 grouping-key refinement PR.

**No parser-API bead (P1) — retired.** Round 1 proposed
`cypher.ParseExpression` as a follow-up parser API for resolver-side
re-parse. B7 evidence shows the re-parse strategy is not implementable
against the frozen model; the parser API alone would not close the
gap. Shape B (a parser-side discrimination bit) closes it directly;
`ParseExpression` is not needed at R5 or after Shape B lands.

**No cross-WITH assumption verification bead needed.** Parser test
`"with distinct"` at `parser_test.go:1218-1246` pins the cross-WITH
re-MATCH re-binding behaviour §4.6 relies on. No follow-up needed.

---

## 8. Ground-truth cross-check

Every factual claim in this spec is verifiable against source; the
citations below name the file:line the claim rests on.

- **`Query.Branches`, `Query.Combinators`, `Query.Parameters`,
  `Query.StatementKind`** — `internal/query/query.go:26-52` (Query
  struct); §4.1 iterates `Branches`, folds `Combinators` per §3.2 /
  §4.7.
- **`Branch.Parts`** — `internal/query/query.go:59-63` (Branch struct);
  §4.1 iterates.
- **`Part.Bindings`, `Part.Returns`, `Part.ReturnsAll`, `Part.Distinct`,
  `Part.Effects`** — `internal/query/query.go:81-123` (Part struct);
  §4.1-§4.7 read.
- **`NewPart` invariant** — `internal/query/query.go:150-159`; §4.1.1
  relies on it for "at least one of bindings/projection/effects".
- **`ReturnItem.Name`, `ReturnItem.Value`** —
  `internal/query/query.go:995-998` (ReturnItem struct); §4.5.3 depends
  on `Name` being the verbatim source text of an expression when no AS
  alias is present.
- **`ReturnItem.Name` is populated from `originalText(l.ts, e)` for
  un-aliased expressions** — `internal/query/cypher/expr.go:204`
  (`name := originalText(l.ts, e)`) with alias override at line
  205-207. `originalText` returns `ts.GetTextFromInterval(ctx.
  GetSourceInterval())` — `internal/query/cypher/shape.go:434-443`.
  §4.5.3.1 relies on this.
- **`Projection` sum interface + variants: `RefProjection` (1043),
  `LiteralProjection` (1073), `FuncProjection` (1097),
  `AggregateProjection` (1126), `ExprProjection` (1171)** —
  `internal/query/query.go:1030-1192` (Projection interface at
  1030-1036 plus the five variants and their methods; the sum closes
  at the ExprProjection method block ending at 1192).
- **`AggregateProjection.Func()`, `.Refs()`, `.Distinct()`,
  `.Type()`** — `internal/query/query.go:1126-1158`.
- **`AggregateFunc` closed enum** —
  `internal/query/query.go:1209-1234`; parser Stage 3 §4 pins the eight
  values.
- **`ExprProjection` struct with `refs` and `resultType` fields only**
  — `internal/query/query.go:1171-1174`. No original-text axis and no
  aggregate-content bit at freeze — the boundary that produces
  §4.5.3.2's gap.
- **`classifyRichExpression` drops aggregate structure at
  classification** — `internal/query/cypher/typing.go:857-877` (the
  Stage-6 residual classifier unconditionally returns
  `NewExprProjection(refs, t)` for every non-atom expression; there
  is no branch that inspects the sub-tree for a nested aggregate).
  §4.5.3.1 and §4.5.3.4 rely on this fact.
- **Parser test `"count in arithmetic"` pins the classifier
  output for a nested-aggregate residual** —
  `internal/query/cypher/parser_test.go:1320-1324`: `RETURN count(n)
  + 1` → `ExprProjection{[Ref{n}], TypeInt}`, aggregate function
  kind not recorded. Direct evidence for the round-2 B7 finding.
- **Parser referential-integrity sweep rejects unbound refs at parse
  time** — `internal/query/cypher/build.go:155-158` (`if !scope
  [ref.name] { ErrUnboundVariable }`). §4.5.3.4 relies on this: any
  resolver-side `RETURN " + item.Name"` synthesise-and-parse fails
  the sweep as soon as the residual carries a Ref, so P2 is
  unimplementable against the frozen surface without ref
  scaffolding (and even with scaffolding, `classifyRichExpression`
  drops the aggregate — see the two prior bullets).
- **`ExprProjection.Refs()`, `ExprProjection.Type()`** —
  `internal/query/query.go:1185-1192`.
- **`UnionKind` enum: `UnionDistinct`, `UnionAll`** —
  `internal/query/query.go:171-180`. §3.2 / §4.7 fold reads this.
- **`StatementKind` enum** — `internal/query/query.go:200-233`. §4.8
  copies.
- **R4's per-Part kernel (Phase A1, A2, B, C, D, projection walk,
  parameter walk)** — `internal/resolver/resolve.go:23-717` as merged.
  §4.1.2 wraps this in the branch-and-part driver; §4.2 threads the
  carried scope.
- **R4's `demoteNullable`** — `internal/resolver/resolve.go:685-717`.
  §4.6 pre-seeds `nullableBinding` before this runs per-Part.
- **R4's `refProjectionType`, `propertyUseWitness`, `unionProperty`,
  `projectionType`, `useWitness`** —
  `internal/resolver/resolve.go:449-674`. §2.2 lists which R5 revises.
- **R4 kernel gates at `resolve.go:24-44`** — the six admissibility
  rejects the R5 driver replaces (§4.1.1).
- **Aggregate reject at `resolve.go:459-460`** — retired at R5 (§4.5).
- **R4's `nullableBinding` seed reads
  `internal/query/query.go:354-356` (NodeBinding.Nullable) and
  `:462-464` (EdgeBinding.Nullable)** — the static parser fact per
  ADR 0006.
- **Parser scope enforcement (WITH carry-forward, unbound-variable
  reject)** — `internal/query/cypher/build.go:155-158` (`if !scope
  [ref.name] { ErrUnboundVariable }`); Part's exported-names build at
  `build.go:241-253`. Per parser Stage 4 §4. §4.2 relies on this: the
  resolver never sees a Ref to a name the parser rejected as
  out-of-scope.
- **Parser records Part's exported names to carry** —
  `build.go:244-253` (`exported := ...`; walks `rp.returns` OR
  `WITH *` = all in-scope).
- **Parser's cross-WITH re-MATCH shape (Part K+1 records own fresh
  Binding for the same-named MATCH)** — parser Stage 4 spec §3 lines
  171-187 asserts this; parser test `"with distinct"` at
  `internal/query/cypher/parser_test.go:1218-1246` pins it in source:
  Part 1 of `MATCH (a) WITH DISTINCT a MATCH (a)-->(b) RETURN b` has
  `Bindings[0] = NewNodeBinding("a", nil)` — a fresh non-nullable
  entry, distinct from Part 0's `a`. §4.6's local-seed override
  algorithm relies on this shape and consumes it correctly.
- **Parser's within-Part `mergeBinding` merges same-Part re-MATCH**
  — `internal/query/cypher/pattern.go:373-401`. This is what makes
  same-Part regime (b) an under-approximation gap at R4 and R5
  (§4.6.1).
- **Query is builder-maintained, not smart-constructor-guarded** —
  parser Stage 4 §3 lines 141-160 documents this posture (exported
  fields, `-update` goldens as the invariant witness); the parser's
  buildBranch (`internal/query/cypher/build.go`) guarantees
  `len(Branches) >= 1` and `len(Combinators) == len(Branches) - 1`.
  §4.1.2's defensive tripwire is redundant with the builder guarantee,
  kept as a belt-and-braces guard for any future ADR-0008-in-protocol
  additive shape change that could reach this call site with different
  invariants.
- **`ReturnsAll` documented as resolver-owned** —
  `internal/query/query.go:99-103` ("A query-level wildcard over the
  part's in-scope bindings, not a return item; the resolver owns
  expansion"). Confirms §4.4's authorship.
- **openCypher UNION column naming rule (branch 0 names)** —
  documented at parser Stage 4 spec §3 lines 188-199 ("The single
  source for the result column names is branch 0's final part").
  §3.1 and §4.3 encode this.
- **`ValidatedQuery` shape (top-level)** —
  `internal/resolver/validated.go:10-18`. §3 adds `Distinct` here.
- **`Column` shape** — `internal/resolver/validated.go:20-26`. §4.5.2
  adds `GroupingKey` here.
- **`allSentinels` list (R4)** — `internal/resolver/errors.go:63-75`.
  §5.4 extends.
- **`ResolvedProperty.Nullable`** —
  `internal/resolver/validated.go:106-109`. §4.3 nullability sub-rule
  reads this.
- **ADR 0008 §Post-freeze revision protocol** —
  `docs/adr/0008-query-model-freeze-resolver-api.md:143-169`. §4.5.3.3
  reads.
- **ADR 0009 R5 line** —
  `docs/adr/0009-resolver-test-first-staged-build.md:126-131`.
- **ADR 0009 R5 aggregate-strategy contract** —
  `docs/adr/0009-resolver-test-first-staged-build.md:129-131` (gqlc-gyw
  re-parse; ContainsAggregate as escape hatch only).
- **Parser Stage 3 §3 (RETURN * resolver-owned)** —
  `docs/specs/cypher-query-parser-stage-3.md:108-129`.
- **Parser Stage 4 §3 (per-Part Bindings for cross-WITH scope)** —
  `docs/specs/cypher-query-parser-stage-4.md:171-187`.
- **Parser aggregate-kind-rich-exprs §1.3 (inner-aggregate ExprProjection
  deferral + gqlc-gyw re-parse)** —
  `docs/specs/cypher-query-parser-aggregate-kind-rich-exprs.md:99-207`.
- **R4 spec §7.4 item 1 (cross-WITH regime (b) hand-off to R5)** —
  `docs/specs/resolver-stage-r4.md:1220-1231`.
- **R4 spec §7.5 template (openCypher semantics → frozen-model gap →
  quantified loss → unfreeze options → recommendation)** —
  `docs/specs/resolver-stage-r4.md:1270-1542`. §7.1 above follows this
  template.

---

## 9. Definition of done for R5 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is out
of scope of this document. The spec is done when:

1. This file exists at `docs/specs/resolver-stage-r5.md`, committed on
   branch `resolver-r5-spec`.
2. §3 records the widened `ValidatedQuery` shape: the added `Distinct`
   axis on `ValidatedQuery` (§3.2), the added `GroupingKey` axis on
   `Column` (§4.5.2), and the widened semantics of `Columns`
   (populated from branch 0's final Part; §3.1).
3. §4 gives the algorithm for R5: the branch-and-part driver (§4.1),
   the carried-scope construction (§4.2), the UNION column
   compatibility rule (§4.3), the `ReturnsAll` expansion (§4.4), the
   `AggregateProjection` handler with grouping-key discovery
   (§4.5.1-§4.5.2), the honest R5 posture on `ExprProjection`
   residuals — uniform-exclude with parser-side discrimination
   deferred to a follow-up bead (§4.5.3), the cross-WITH nullability
   extension (§4.6), and the Distinct fold (§4.7).
4. §5 records the two new sentinels `ErrUnionColumnMismatch` and
   `ErrPartBindingTypeConflict`, revises `ErrOutOfR0Scope`'s
   message-set list for retirements, and preserves the R4 sentinels'
   identity.
5. §6 designs the fixture set: the R5 valid schema `social_r5.gql`
   (§6.2), the R5 valid fixture list (20 fixtures), the R5 invalid
   fixture list (5 additions + 5 retirements), the revised
   `invalidFixtures` map (§6.4), the golden-rebaseline plan (§3.3
   refined by §3.2.1 — two added lines per golden: `"distinct"` at
   top level, `"groupingKey"` per column), and the R5 harness
   invariants (§6.6).
6. §7 states the R5 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel or under-demote posture
   for each construct. §7.1 walks the honest state on the discovered
   gap classes: cross-WITH regime (b) closes (contingent on parser
   assumption); `ExprProjection` residual grouping-key discrimination
   uniformly under-groups pending Shape B follow-up; Class A + Class B
   under-approximations from R4 persist unchanged.
7. §8 cross-checks every factual claim against source file:line.
8. `just test` is untouched-green — this cycle is docs-only.
9. **At R5 code-cycle close-out** (Cycle 2, not this Cycle 1):
   - The parser-side discrimination bead is filed (§7.1.5): "Model:
     add `ExprProjection.ContainsAggregate bool` (Shape B) closing
     the R5 `ExprProjection`-residual grouping-key gap". Owner picks
     between Shape B (recommended) and Shape A/A′ (classifier
     promotion).
   - The round-1 parser-API bead (`cypher.ParseExpression`) is
     RETIRED — the resolver does not re-parse at R5 and does not
     re-parse under Shape B either.
   - gqlc-ay9 and gqlc-5xg remain OPEN; R5 does not close them.
   - gqlc-gyw closes (R5 admits AggregateProjection; the residual-
     discrimination question is scoped to the new Shape B follow-up
     bead above, not gqlc-gyw).

The spec is a review artefact for Linus (adversarial reviewer); every
blocker he raises is fixed on this same branch before the branch
merges. Cycle 2 (the R5 code cycle) begins only when the spec cycle
merges.
