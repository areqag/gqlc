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

R5 introduces **one new sentinel** (§5.1), **`ErrUnionColumnMismatch`**,
covering the two column-incompatibility shapes UNION branches can exhibit
(disagreeing column count / disagreeing column names / disagreeing
column types). Zero other new sentinels; the R4 closed set is otherwise
preserved.

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
- `internal/resolver/errors.go` — one new sentinel,
  **`ErrUnionColumnMismatch`** (§5.1), and revised prose on
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
    kind's result type and — for the branch's final Part — computes the
    implicit grouping-key set by inspecting every non-aggregate projection
    (§4.5.2), with the `gqlc-gyw` re-parse mechanic for `ExprProjection`
    residuals (§4.5.3);
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
       branchColumns[b], err := resolveBranch(q.Branches[b], q.Parameters)
       if err != nil: return err
  3. compareBranchColumns(branchColumns)  (§4.3 — UNION column compatibility)
  4. compute Distinct                    (§4.7 — fold of Part.Distinct + UnionKind)
  5. return ValidatedQuery{
       Columns:    branchColumns[0],     (§3.1 — branch 0 names the result)
       Parameters: resolvedParameters,   (§2.3 — parameter merge across branches)
       Statement:  q.StatementKind,
       Distinct:   distinct,             (§3.2)
     }

resolveBranch(branch, params):
  1. carriedScope := emptyScope()        (§4.2 — Part 1's incoming scope is empty)
  2. for each partIdx k in 0..len(branch.Parts)-1:
       columns, exports, err := resolvePart(branch.Parts[k], carriedScope, params)
       if err != nil: return err
       carriedScope = exports            (§4.2 — the WITH-exported carry-forward)
  3. return columns of the final Part
```

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

- **`resolveBranch(branch query.Branch, params []query.Parameter,
  parts *branchState) ([]Column, error)`** (new). Drives the per-Part
  walk within one branch. `branchState` is the accumulator for the
  Part-carried scope; see §4.2 for its concrete shape.
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

### 3.3 R0–R4 golden rebaseline plan — one field added

**Every existing R0–R4 golden rebaselines** on the addition of the
`"distinct": false` field to `ValidatedQuery`. This is a shape change
to the provisional-through-R7 shape; the R5 code cycle regenerates them
with `-update` at the same commit that introduces the field. The
regenerated JSON differs from the R4 version only by the added
`"distinct": false` at the top level; no wire-tag renames, no field
reorderings, no discriminator changes.

The affected fixtures are **every** R0–R4 valid golden — identifiable
by `find test/data/resolver/valid/ -name '*.validated.golden.json'`.
The rebaseline is universal: every top-level ValidatedQuery has a
`Distinct` axis. Fixtures with `Part.Distinct == true` or a
UNION-DISTINCT combinator do not exist at R4 (R4 rejects both), so at
the rebaseline every existing golden's `Distinct` is `false`. The
first golden to carry `"distinct": true` is a new R5 fixture (§6.3
`distinct_projection.cypher` / `union_distinct.cypher`).

**Explicit rebaseline enumeration** (from `ls
test/data/resolver/valid/*.validated.golden.json` at branch base):

- R0/R1 goldens: `edge_labelled_both_endpoints`,
  `edge_property_projection`, `edge_property_parameter`,
  `inline_endpoint_source`, `inline_endpoint_target`,
  `literal_int_projection`, `literal_int_projection_named`,
  `node_property_projection`, `node_property_parameter`,
  `node_whole_entity`, `node_mixed_projection`, `parameter_no_uses`,
  `parameter_property_and_slot_int`,
  `parameter_two_properties_same_type`, `self_loop_directed`,
  `two_edges_shared_binding`, `unlabelled_binding_from_edge`,
  `unlabelled_binding_target_inferred`.
- R2 goldens: `expr_projection_scalar`, `expr_projection_unknown`,
  `func_projection_int`, `func_projection_unknown`,
  `parameter_expr_in_predicate`, `parameter_in_where`,
  `parameter_skip_limit`.
- R3 goldens: `multi_type_directed_union`, `multi_type_undirected`,
  `undirected_single_match`, `undirected_single_match_reverse`,
  `undirected_var_length_multi_type_property`,
  `unlabelled_via_multi_type`, `unlabelled_via_undirected`,
  `var_length_directed`, `var_length_multi_type`,
  `var_length_undirected_single_match`,
  `edge_property_union_agree`.
- R4 goldens (added on the R4 code cycle):
  `optional_match_node_whole_entity`,
  `optional_match_property_projection`,
  `optional_match_edge_whole_entity`,
  `optional_match_property_parameter`,
  `demote_directed_edge_chain`,
  `demote_undirected_edge_chain`,
  `demote_multi_type_edge_chain`,
  `demote_var_length_min_one`,
  `no_demote_var_length_min_zero`,
  `demote_chained_from_required`,
  `optional_match_and_required_match_share_edge`,
  `optional_match_property_union_agree`.

Every one of the above rebaselines with **one added line**:
`"distinct": false,` at the top level of the `ValidatedQuery` JSON
object. For a wire-shape-audit reviewer: the regeneration diff on the
R5 commit should show exactly one added line per golden and nothing
else. Any golden whose diff shows additional changes is a bug in the
R5 implementation.

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
a Part with empty `Bindings`. At R5, a Part may legitimately have empty
`Bindings` when it re-projects entirely from the carried scope (`MATCH
(a) WITH a RETURN a` — the second Part has no MATCH of its own, so
`Part[1].Bindings` is empty and every projection Ref resolves against
`carriedScope`). R5 removes this gate for Part K > 0. Part 0 with empty
bindings is still rejected — no carried scope exists.

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
    var paramUses []parameterUseSite  // (see §2.3)

    for b, branch := range q.Branches {
        cols, uses, err := resolveBranch(branch, s, r)
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

**Shadowing rule.** A name introduced by Part K+1's own bindings
**shadows** a same-named carried entry — Part K+1 writes its own entry
into the local table without consulting the carry. The parser's
`Stage 4 §4` rule "Part K's own bindings + the prior part's WITH
names" — with "own bindings" first — is the same shape.

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

A parameter Use encountered in Part K's projection or predicate is
witnessed against **Part K's binding tables (local + carried)**. The
resolver walks Parts left-to-right; when it encounters a Use whose
`PropertyUse` names a Ref, it resolves the Ref against the Part's
scope-in-effect at that moment. The parameter-unification lattice
runs at end-of-query on the collected witnesses (§2.3).

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

1. Every name in the carried scope, in `carriedScope.exportedOrder`
   order (per §4.2.1). This is Part K-1's WITH-exported ordering.
2. Then every named binding introduced in Part K's own `Part.Bindings`,
   in first-appearance order per `part.Bindings` slice traversal — the
   same order parser `buildPart` populates it.

Anonymous bindings (variable == "") contribute zero columns.

Duplicate names (the parser rejects them at Stage-0 merge; a same-named
carry + own binding is impossible because Part K's binding would
shadow the carry AND the parser would have rejected the merge before
lowering) are structurally impossible, so the expansion produces a
duplicate-free ordered list.

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
type per §4.5.1 and — for the branch's final Part **and** for any
non-final WITH that carries a grouping-key computation — the grouping
keys are recorded per §4.5.2.

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
    if no ReturnItem in part.Returns is an AggregateProjection or
       an ExprProjection containing an aggregate (§4.5.3):
        return nil  // no grouping applies
    keys := []GroupingKey{}
    for each item in part.Returns:
        switch item.Value.(type):
        case AggregateProjection:
            skip                    // aggregates are not grouping keys
        case RefProjection, LiteralProjection, FuncProjection:
            keys = append(keys, GroupingKeyFromRef(item))
        case ExprProjection:
            if containsAggregateResolverSideReparse(item):
                skip                // aggregate-residual is not a
                                    // grouping key (§4.5.3)
            else:
                keys = append(keys, GroupingKeyFromRef(item))
    return keys
```

The `GroupingKey` type is a structural handle on the projection item —
either the `Ref{Variable, Property}` for a bare projection, or the
verbatim `item.Name` (the expression text span) for an expression
projection. The exact shape of `GroupingKey` on the wire is deferred to
the codegen bead; at R5 the grouping-key list is computed and available
to consumers via a `Column.GroupingKey bool` axis (see §3.4 alternative
below).

**Wire representation of grouping keys.** Two options:

- Option A — Per-column `Column.GroupingKey bool` axis: each column
  emitted on `ValidatedQuery.Columns` gains a bool marking it a grouping
  key. Codegen filters `Columns` by `GroupingKey == true` to emit the
  GROUP BY. **Recommended.**
- Option B — Separate `ValidatedQuery.GroupingKeys []string` axis: a
  slice of column-names that form the group key. Requires the consumer
  to cross-reference; adds one axis but keeps `Column` clean.

R5 commits to **Option A**: adding `GroupingKey bool` to `Column`. The
rationale is that the grouping-key attribute is a column-level fact
(this column participates in the GROUP BY), and a per-column axis makes
the column's role self-describing on the wire.

**§3.2 addendum: `Column` gains one axis.** `Column` becomes:

```go
type Column struct {
    Name         string       `json:"name"`
    Type         ResolvedType `json:"type"`
    GroupingKey  bool         `json:"groupingKey"`
}
```

Always-emit `"groupingKey": false | true`. This is a shape change to
`Column`; the R0–R4 golden rebaseline plan of §3.3 extends: every
existing `Column` on every existing golden rebaselines with
`"groupingKey": false`. The `Distinct` axis rebaseline in §3.3 and the
`GroupingKey` axis rebaseline coalesce into one commit.

**Refined R0–R4 golden rebaseline (updates §3.3).** Every R0–R4 golden
rebaselines with TWO added lines: `"distinct": false,` at the top
level, and `"groupingKey": false,` on every column. For a wire-shape-
audit reviewer: the regeneration diff should show exactly one `distinct`
line per golden plus one `groupingKey` line per column, and nothing
else.

#### 4.5.3 `ExprProjection` residuals with nested aggregates — the `gqlc-gyw` re-parse contract

Per parser aggregate-kind-rich-exprs §1.3 and gqlc-gyw's notes: an
`ExprProjection` whose expression contains a nested aggregate
(`count(n) + 1`, `1 + sum(x)`, `CASE WHEN count(n) > 3 THEN 'a' ELSE
'b' END`) is not lowered as `AggregateProjection` — the model records
the projection as `ExprProjection{refs, type}` with the aggregate kind
invisible on the wire.

The contract on gqlc-gyw is:
- Grouping-key discovery for this residual class is a **resolver-side
  re-parse of the projection's original text span**.
- The `ContainsAggregate` axis on `ExprProjection` is **only** the ADR
  0008 escape hatch, invoked only if re-parse proves impractical, and
  is **never inferred from `Type`** (because `count(n) + 1` and `x + 1`
  under some operand-type combinations both type as `TypeUnknown`, and
  `Type`-based inference would silently miss the aggregate).

**§4.5.3.1 What "original text span" means in the frozen model.** The
frozen model does not carry a first-class original-text-span accessor
on `ExprProjection`. What it does carry is `ReturnItem.Name`, which per
parser `expr.go:204` is populated from `originalText(l.ts, e)` — the
verbatim source slice of the expression — **when no explicit AS alias
is given**. If an AS alias is present (`RETURN x + 1 AS s`),
`ReturnItem.Name == "s"` and the verbatim text is lost.

Two paths follow:

- **Un-aliased `ExprProjection`** — `ReturnItem.Name` IS the verbatim
  text span. The resolver re-parses this string as a Cypher expression
  and walks the tree for aggregate calls. This is the common case
  gqlc-gyw's notes assume.
- **Aliased `ExprProjection`** — `ReturnItem.Name` is the AS alias, not
  the text span. **This is a genuine frozen-model gap for the resolver
  contract** because the original text is not recoverable from
  `ExprProjection` alone.

**§4.5.3.2 The aliased-ExprProjection gap, stated honestly.**

The gqlc-gyw notes committed the resolver to "re-parse the original
text span". The frozen model preserves that text span on
`ReturnItem.Name` ONLY when the projection has no AS alias. For
`RETURN 1 + count(n) AS c`, `ReturnItem.Name == "c"` and the
resolver cannot recover the source text of `1 + count(n)` from
`ExprProjection` alone. The `Refs()` list carries `[Ref{Variable:"n"}]`
but does not distinguish this from `RETURN n.age AS c`
(`Refs()` also carries `[Ref{Variable:"n", Property:"age"}]`, of a
different shape but not distinguishable at the shape-of-refs level from
an aggregate-of-n).

This is a **frozen-model gap** distinct from the two R4-recorded gaps
(gqlc-ay9 / gqlc-5xg). Following the R4 §7.5 template:

**openCypher semantics.** For `RETURN 1 + count(n) AS c`, the query is
implicitly grouped (an aggregate is present); the grouping key set is
whatever non-aggregate projections exist in the same Part (in this case,
none — so the group key is empty, and the query folds to a single row).
The `c` column is `TypeInt`. The parser correctly types the projection
as `ExprProjection{[Ref{n}], TypeInt}` and correctly records the
projection name as `c`.

**Frozen-model gap.** The parser threw away the original text on
alias-application, keeping only the resolver-uninterpretable alias `c`.
The `ExprProjection.Refs()` list carries the touched refs but not the
tree structure that would distinguish aggregate-touch from non-aggregate-
touch. The `ExprProjection.Type()` may be `TypeUnknown` for various
non-aggregate expressions too (`n + 1` on a property-participating
arithmetic, when the operand's property type is `TypeUnknown`).

**What R5-as-scoped loses.** For an aliased `ExprProjection` whose
expression contains a nested aggregate, the resolver **cannot
distinguish** it from an aliased `ExprProjection` with no aggregate. The
consequences for grouping-key computation:

- If the resolver treats every aliased `ExprProjection` as a
  grouping-key candidate, it will incorrectly include aliased-aggregate
  residuals in the grouping key (over-grouping — the query fold row-
  set is smaller than semantics dictates).
- If the resolver treats every aliased `ExprProjection` as an
  aggregate-residual, it will incorrectly exclude aliased-non-aggregate
  expressions from the grouping key (under-grouping — the query fold
  row-set is larger than semantics dictates).

Both errors change result cardinality. Neither is safe under a lattice
invariant.

**§4.5.3.3 The unfreeze option — three shapes to weigh, one recommendation.**

Per ADR 0008 post-freeze revision protocol, additive axes are
in-protocol. Three axis shapes could close this gap:

Shape A — `ExprProjection.OriginalText string` axis. The parser
threads `originalText(l.ts, e)` into `NewExprProjection` at construction
time, regardless of AS alias. The resolver re-parses this string to
walk for aggregates. Additive, zero-value-safe (default `""`; empty
means "text was not preserved", triggers under-approximation
fallback). **Pros:** literal implementation of gqlc-gyw's committed
strategy; smallest axis; independent of `Type`. **Cons:** the parser
now carries the expression text as data on the model — a data
duplication (the `Query.Query` wire keeps the parsed model AND the
verbatim text). Consumers holding a `ExprProjection` value hold a
sub-string of the source that was already the wire's origin. ADR 0003
argued against carrying the expression tree; ADR 0005's re-execute-
original-text posture already lives at the codegen edge; putting the
text on the model puts it in the shared-vocabulary tier where ADR 0003
said it does not belong.

Shape B — `ExprProjection.ContainsAggregate bool` axis. The parser
sets this at construction time (visible in the parser's
classifyRichExpression walk when it encounters a nested aggregate).
Additive, zero-value-safe (default `false`; wire-safe). **Pros:** the
smallest possible signal — one bit — with a clear semantic. Independent
of `Type`. Consumers hold no expression sub-string. **Cons:** the exact
axis gqlc-gyw's notes called the "escape hatch, invoked only if the
resolver's re-parse proves impractical". The gqlc-gyw contract's re-
parse strategy is committed *specifically to avoid* this axis: gqlc-gyw
notes say "the sanctioned escape hatch is a post-freeze additive axis
on `ExprProjection` (`ContainsAggregate bool`) via the ADR 0008 revision
protocol", i.e. Shape B is what gqlc-gyw is trying to avoid. Adopting
Shape B is invoking the escape hatch — an acknowledged option but a
retreat from the committed strategy.

Shape C — Preserve `ReturnItem.Name` as the AS alias AND add a separate
`ReturnItem.TextSpan string` axis that holds the verbatim text
regardless of alias. Additive on `ReturnItem`, not on `ExprProjection`.
Zero-value-safe (default `""`). **Pros:** preserves gqlc-gyw's re-parse
strategy while retaining the AS alias as the column name; solves the
gap fully; the axis's meaning ("the source text of this return item's
expression, regardless of alias") is precise. **Cons:** adds an axis
to a shape (`ReturnItem`) that is currently three lines of Go
(`Name`, `Value`); doubles its wire shape. Same data-duplication
concern as Shape A but scoped to items rather than projections.

**Recommendation.** For the aliased-ExprProjection-with-nested-
aggregate case ONLY (bounded and characterisable):

- **Under-approximate at R5** by treating every aliased
  `ExprProjection` as a grouping-key candidate (Option 1 above:
  over-group). This preserves the correctness of un-aliased and
  bare-aggregate paths, and it produces conservative (correct-but-
  possibly-more-granular-than-needed) grouping for the aliased-residual
  case. Consumers running a query that falls into the residual will
  see result rows partitioned more finely than openCypher semantics
  dictate — safe under most consumer contracts (aggregation-over-
  key-set is monotone in the key set's fineness), but observably
  slower or more rows than expected. Not a lattice-safe under-
  approximation of the result set itself, but a lattice-safe under-
  approximation of the *grouping decision*: more grouping keys ⇒
  more rows in output ⇒ finer partitioning ⇒ the resolver never
  claims a group where there isn't one.

- **File a follow-up unfreeze bead** — "Model: preserve
  `ExprProjection` original text (Shape A) OR record ContainsAggregate
  (Shape B) OR add ReturnItem.TextSpan (Shape C); revise resolver R5
  grouping-key discovery to consume the added axis, closing §4.5.3.3
  aliased-ExprProjection gap". Owner picks the shape; R5 does not.

- **Do not contort R5 around the gap.** The three shapes are
  ADR-0008-in-protocol and gated on the owner's unfreeze decision. R5
  ships as scoped; the follow-up bead is filed alongside the R5 code-
  cycle close-out (per the R4 §7.5.5 template).

**§4.5.3.4 What R5's re-parse mechanic looks like for un-aliased
`ExprProjection`.**

For the un-aliased case (the majority of parser corpus), the mechanic
is:

```
containsAggregateResolverSideReparse(item query.ReturnItem) bool =
    // Only ExprProjection carries this question.
    exprProj, ok := item.Value.(query.ExprProjection)
    if !ok:
        return false

    // The parser's ReturnItem.Name IS the verbatim source text for
    // un-aliased items (parser expr.go:204). Detect the un-aliased
    // shape structurally: if item.Name would-be a valid Ref token (a
    // single identifier, or "identifier.identifier"), it MIGHT be an
    // AS alias for a shape whose text was "n" or "n.age" — but for
    // an ExprProjection specifically, an un-aliased projection's
    // source text CAN be anything (an arithmetic like "1 + 2",
    // a CASE, a list expression), and these do NOT look like Ref
    // tokens. The heuristic: if item.Name PARSES as a Cypher
    // expression via cypher.ParseExpression(item.Name), and the parsed
    // expression tree contains any AggregateFunc call (checked via a
    // walk of the parsed cypher.Expr sub-tree), the projection is an
    // aggregate-residual.

    expr, parseErr := cypher.ParseExpression(item.Name)
    if parseErr != nil:
        // item.Name was an AS alias, not a source text; §4.5.3.3
        // over-group fallback kicks in — assume aggregate residual.
        return true      // conservative over-approximation

    return expressionContainsAggregate(expr)
```

**Judgment call — parse-failure means AS-aliased.** For an un-aliased
`ExprProjection`, `ReturnItem.Name` is the verbatim source of a valid
Cypher expression. If `cypher.ParseExpression(item.Name)` fails, the
name is almost certainly an AS alias (which is a Cypher identifier, not
a valid expression when the identifier isn't in scope). The over-group
fallback treats every parse-failure as aggregate-residual — the safe
direction per §4.5.3.3.

An alias that happens to be a valid parseable expression (`RETURN x + 1
AS y`; `y` parses as `Ref{Variable:"y"}`) is a legitimate valid parse
of the alias-as-Ref. But `y` on its own contains no aggregate call, so
`expressionContainsAggregate` returns `false`, and the projection is
treated as a grouping-key candidate — correct for the alias case, and
correct for the un-aliased `RETURN y` case (both are non-aggregate
refs).

The tricky sub-case is `RETURN 1 + count(n) AS x`: `item.Name == "x"`
parses as `Ref{Variable:"x"}`, contains no aggregate, so treated as
grouping-key — the WRONG answer per openCypher semantics. This is the
§4.5.3.2 gap materialising. Recommendation §4.5.3.3 stands: **file the
unfreeze bead**; do not contort R5.

**Actual implementation — pragmatic version.**

The re-parse mechanic above requires `cypher.ParseExpression` — a
parser entry point that parses a single expression, not a whole query.
The parser package currently exposes `cypher.New().Parse(io.Reader)`
which parses a complete query.

**Two options:**

- **Option P1 — Add `cypher.ParseExpression(text string) (Expr, error)`
  as a new parser entry point.** The Cypher grammar has an
  `oC_Expression` production; the parser can call the ANTLR visitor on
  the `oC_Expression` rule directly. This is a parser-side addition
  with a small surface (one exported function, one method on
  `cypher.Parser`), not a model or wire change — additive to the parser
  package, not to `query.Query`. **Recommended.**
- **Option P2 — Synthesise a full query around the text and parse
  it.** `q := cypher.New().Parse("RETURN " + item.Name)` — the RETURN
  wraps the expression in a valid parse position. Resolves without a
  new parser entry point. **Pros:** no parser API change. **Cons:**
  every re-parse allocates a new parser, walks a full listener, and
  synthesises a Query — expensive per fixture.

**Recommendation — Option P1.** Filed as a separate follow-up bead:
"Parser: add `cypher.ParseExpression(text string) (Expr, error)` for
resolver-side text-span re-parse (per gqlc-gyw contract; R5 grouping-
key mechanic)". This is a **parser API addition, not a model change** —
independent of ADR 0008's frozen-model contract. The parser package's
exported surface is not what ADR 0008 froze; only `query.Query`'s shape
was frozen. Adding a parser method that consumes text and returns a
tree is orthogonal to the freeze.

**§4.5.3.5 Discriminating fixture.** `aggregate_with_expr_residual.cypher`
= `MATCH (n:Person) RETURN 1 + count(n)` — no AS alias, so
`ReturnItem.Name == "1 + count(n)"` (the verbatim text). Re-parse
detects the aggregate. Grouping key is empty (only one projection, an
ExprProjection-residual). `Columns[0].GroupingKey == false`. The
`Distinct` is not affected. If instead a fixture has `RETURN n.name, 1
+ count(n)`, the grouping key includes `n.name` (index 0), and the
ExprProjection-residual at index 1 is not a key
(`Columns[0].GroupingKey == true`, `Columns[1].GroupingKey == false`).

The discriminating question: swap the fixture to `RETURN n.name, 1 +
n.name` (no aggregate), and the grouping key should be BOTH — every
projection is a key. The `Columns[0].GroupingKey == true,
Columns[1].GroupingKey == true` outcome pins that the re-parse mechanic
distinguishes the two shapes.

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
`ErrParameterTypeConflict`, `ErrAmbiguousEdgeOrientation`. R5 adds one
sentinel, keeps the others, and revises `ErrOutOfR0Scope`'s message set
to reflect retirements.

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

### 5.2 Revised `ErrOutOfR0Scope` message set — retirements

The R4-era `ErrOutOfR0Scope` sub-cases that retire at R5:

- WITH / multi-part query (was `resolve.go:28-30`) — R5 admits.
- UNION / multi-branch query (was `resolve.go:24-26`) — R5 admits.
- RETURN * / WITH * (was `resolve.go:39-41`) — R5 admits.
- RETURN DISTINCT / WITH DISTINCT (was `resolve.go:36-38`) — R5 admits.
- AggregateProjection (was `resolve.go:459-460`) — R5 admits.
- Empty binding set (was `resolve.go:42-44`) — R5 admits for K > 0.

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
    ErrUnionColumnMismatch,      // R5 addition
}
```

Eight sentinels total. The reachability sweep in `TestSentinelReachability`
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
  AS s`. Aggregate over INT; result type per parser (INT).
- `aggregate_with_grouping.cypher` — `MATCH (a:Person) RETURN a.name,
  count(*) AS n`. Two columns; `Columns[0].GroupingKey == true`,
  `Columns[1].GroupingKey == false`.
- `aggregate_with_expr_residual.cypher` — `MATCH (n:Person) RETURN 1 +
  count(n)`. One column; the un-aliased ExprProjection's Name is the
  verbatim source text; §4.5.3 re-parse detects aggregate; group key
  is empty. `Columns[0].GroupingKey == false`.
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
  MATCH (a:Post) RETURN a`. Part 1 attempts to re-declare `a` with a
  conflicting label. Verify the parser's actual behaviour: does the
  parser accept `MATCH (a:Post)` when `a` is already an in-scope
  Person? If yes, R5 must reject with a sentinel. If no, this fixture
  is unreachable and dropped. (Discovery task for R5 code cycle.)

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
| Aliased `ExprProjection` with nested aggregate — grouping-key discovery gap | silently over-grouped | §4.5.3.3 follow-up bead (model or parser API) |

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
- **`gqlc-gyw` re-parse contract:** implemented in §4.5.3.4 for
  un-aliased `ExprProjection`. The aliased case (§4.5.3.2) is a
  discovered frozen-model gap, resolved by owner-decided unfreeze
  (§4.5.3.3) OR a parser-API addition (§4.5.3.4 Option P1); R5 ships
  with the un-aliased mechanic and the over-group fallback for
  aliased, and files a follow-up bead.
- **`ContainsAggregate` axis as escape hatch, never inferred from
  Type:** honoured in §4.5.3.3 (Shape B is one option among three;
  R5 does not pre-commit to it; the escape hatch remains).

### 7.1 Under-approximation vs the bead's canonical example (R4 template applied to R5)

Following R4 §7.5's honest-state template:

#### 7.1.1 openCypher semantics

Cross-WITH re-MATCH of an OPTIONAL-introduced binding: `OPTIONAL MATCH
(a)-[:R]->(b) WITH b MATCH (b) RETURN b`. On every surviving row, the
row survives Part 1's required MATCH (b), so `b` is non-NULL. Ideal
flow-typing demotes `b`. Same for `MATCH (b)-[:S]->(c)` — both `b` and
`c` are non-NULL.

Grouping-key semantics for aliased-ExprProjection-with-nested-aggregate:
`RETURN 1 + count(n) AS c`. The query is implicitly grouped; the empty
non-aggregate set means one result row. `c` is INT (the sum's result).

#### 7.1.2 The frozen-model gap, stated honestly

R4 recorded two gaps (gqlc-ay9, gqlc-5xg). R5 discovers one new gap
distinct from both:

- **Cross-WITH regime (b) — closed by R5 without a model change**
  (§4.6). Not a gap; the parser's cross-WITH re-MATCH behaviour is
  pinned by `parser_test.go:1218-1246` (`"with distinct"` test case:
  Part 1 re-records `a` as a fresh non-nullable NodeBinding, so the
  local seed override closes the demotion path). R4 §7.4 item 1's
  hand-off statement is directly evidenced by source.
- **Aliased `ExprProjection` original-text preservation** (§4.5.3.2):
  `ReturnItem.Name` holds the AS alias, not the verbatim text, so
  gqlc-gyw's re-parse strategy cannot execute on aliased residuals.
  Distinct from gqlc-ay9 (missing group membership on `Binding`) and
  gqlc-5xg (missing witness on `Binding` after mergeBinding). This gap
  is on a different model surface: `ReturnItem` or `ExprProjection`
  (three shape candidates per §4.5.3.3).

#### 7.1.3 What R5-as-scoped loses, quantified

- **Cross-WITH regime (b) — nothing lost.** Closed by §4.6 without a
  model change. The parser evidence in `parser_test.go:1218-1246`
  confirms Part K+1 re-records same-named MATCHes as fresh Bindings,
  so R5's per-Part local-seed override sees the required MATCH and
  demotes.
- **Aliased-ExprProjection grouping:** R5 conservatively over-groups
  every aliased-ExprProjection (§4.5.3.3 recommendation). Consumers
  running a query with an aliased ExprProjection containing a nested
  aggregate will see a result row-set partitioned more finely than
  openCypher dictates. Not a lattice-safe result-set under-
  approximation, but a lattice-safe grouping-decision over-
  approximation.

#### 7.1.4 The unfreeze options (following §4.5.3.3)

Three shape candidates per §4.5.3.3 for the aliased-ExprProjection gap:
- Shape A — `ExprProjection.OriginalText` axis.
- Shape B — `ExprProjection.ContainsAggregate` axis (the escape hatch).
- Shape C — `ReturnItem.TextSpan` axis.

Owner decision pending.

#### 7.1.5 Recommendation

**R5 proceeds as scoped; one follow-up bead is filed alongside the R5
code-cycle close-out, on the model of R4 §7.5.5 beads 1+2.**

Rationale:
- The over-group fallback is safe under the "correct-grouping-implies-
  correct-columns" invariant: over-grouping produces more rows than
  needed, which is safer than under-grouping (which would fold rows
  that should remain distinct).
- The gap is bounded and characterisable: it fires ONLY on aliased
  `ExprProjection` that contains a nested aggregate — a strict subset
  of aggregate residuals.
- All three shape options are ADR 0008 in-protocol or parser-API-
  scope, so the follow-up is a normal escalation, not an ADR
  supersedure.
- No R5-fixture-level correctness gap: R5 fixtures use un-aliased
  ExprProjection for aggregate residuals (`aggregate_with_expr_
  residual.cypher`), where the re-parse mechanic works.

**Follow-up bead to file:**

- "Resolver R5 — grouping-key discovery for aliased `ExprProjection`
  with nested aggregates. Three shape options per resolver-stage-r5
  spec §4.5.3.3 (Shape A: `ExprProjection.OriginalText`; Shape B:
  `ExprProjection.ContainsAggregate` (ADR 0008 escape hatch); Shape C:
  `ReturnItem.TextSpan`). Owner picks one; independent of gqlc-ay9 /
  gqlc-5xg. R5 code cycle ships with un-aliased mechanic + over-group
  fallback for aliased. Filed alongside R5 code cycle close per this
  spec §7.1.5." Dependency: gqlc-0mx.7 (R5 code cycle) at close;
  blocks the resolver R5 grouping-key-refinement PR.

The `aggregate_with_expr_residual.cypher` fixture (§6.3) exercises the
un-aliased path (works); no fixture at R5 exercises the aliased-residual
path (that fixture arrives on the widening PR that lands the chosen
shape).

**Parser API follow-up bead to file (independent, may be filed with
R5 close):**

- "Parser: add `cypher.ParseExpression(text string) (Expr, error)` for
  resolver-side text-span re-parse (per gqlc-gyw contract; R5 grouping-
  key mechanic §4.5.3.4 Option P1). Additive parser API surface, not
  a model change — orthogonal to ADR 0008's freeze. Enables §4.5.3.4's
  re-parse mechanic without allocating a full parser per expression."

**No cross-WITH assumption verification bead needed.** Parser test
`"with distinct"` at `parser_test.go:1218-1246` pins the cross-WITH
re-MATCH re-binding behaviour §4.6 relies on. No follow-up needed.

---

## 8. Ground-truth cross-check

Every factual claim in this spec is verifiable against source; the
citations below name the file:line the claim rests on.

- **`Query.Branches`, `Query.Combinators`, `Query.Parameters`,
  `Query.StatementKind`** — `internal/query/query.go:26-36` (Query
  struct); §4.1 iterates `Branches`, folds `Combinators` per §3.2 /
  §4.7.
- **`Branch.Parts`** — `internal/query/query.go:59-63` (Branch struct);
  §4.1 iterates.
- **`Part.Bindings`, `Part.Returns`, `Part.ReturnsAll`, `Part.Distinct`,
  `Part.Effects`** — `internal/query/query.go:81-123` (Part struct);
  §4.1-§4.7 read.
- **`NewPart` invariant** — `internal/query/query.go:145-159`; §4.1.1
  relies on it for "at least one of bindings/projection/effects".
- **`ReturnItem.Name`, `ReturnItem.Value`** —
  `internal/query/query.go:993-998` (ReturnItem struct); §4.5.3 depends
  on `Name` being the verbatim source text of an expression when no AS
  alias is present.
- **`ReturnItem.Name` is populated from `originalText(l.ts, e)` for
  un-aliased expressions** — `internal/query/cypher/expr.go:204`
  (`name := originalText(l.ts, e)`) with alias override at line
  205-207. `originalText` returns `ts.GetTextFromInterval(ctx.
  GetSourceInterval())` — `internal/query/cypher/shape.go:434-443`.
  §4.5.3.1 relies on this.
- **`Projection` sum: `RefProjection`, `LiteralProjection`,
  `FuncProjection`, `AggregateProjection`, `ExprProjection`** —
  `internal/query/query.go:1012-1030` (Projection sum
  declaration).
- **`AggregateProjection.Func()`, `.Refs()`, `.Distinct()`,
  `.Type()`** — `internal/query/query.go:1126-1158`.
- **`AggregateFunc` closed enum** —
  `internal/query/query.go:1209-1234`; parser Stage 3 §4 pins the eight
  values.
- **`ExprProjection` struct with `refs` and `resultType` fields only**
  — `internal/query/query.go:1171-1174`. No original-text axis at
  freeze — §4.5.3.2's gap.
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
   (§4.5.1-§4.5.2), the gqlc-gyw re-parse mechanic for un-aliased
   `ExprProjection` residuals and the honest gap-record for aliased
   residuals (§4.5.3), the cross-WITH nullability extension (§4.6),
   and the Distinct fold (§4.7).
4. §5 records the one new sentinel `ErrUnionColumnMismatch`, revises
   `ErrOutOfR0Scope`'s message-set list for retirements, and preserves
   the R4 sentinels' identity.
5. §6 designs the fixture set: the R5 valid schema `social_r5.gql`
   (§6.2), the R5 valid fixture list (~19 fixtures), the R5 invalid
   fixture list (5 additions + 5 retirements), the revised
   `invalidFixtures` map (§6.4), the golden-rebaseline plan (§3.3
   refined by §4.5.2 — two added lines per golden), and the R5 harness
   invariants (§6.6).
6. §7 states the R5 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel or under-demote posture
   for each construct. §7.1 walks the honest state on the three
   discovered gap classes: cross-WITH regime (b) closes (contingent on
   parser assumption); aliased-ExprProjection grouping-key gap
   over-approximates; Class A + Class B under-approximations from R4
   persist unchanged.
7. §8 cross-checks every factual claim against source file:line.
8. `just test` is untouched-green — this cycle is docs-only.
9. **At R5 code-cycle close-out** (Cycle 2, not this Cycle 1):
   - The follow-up unfreeze bead for the aliased-ExprProjection gap is
     filed (§7.1.5): "Resolver R5 — grouping-key discovery for aliased
     `ExprProjection` …". Owner picks between Shape A / Shape B / Shape
     C.
   - The parser-API follow-up bead is filed (§7.1.5): "Parser: add
     `cypher.ParseExpression(text string) (Expr, error)`". Independent
     of the model.
   - gqlc-ay9 and gqlc-5xg remain OPEN; R5 does not close them.
   - gqlc-gyw closes (R5 admits AggregateProjection and implements the
     re-parse contract for un-aliased residuals; the aliased-residual
     gap is scoped to the new follow-up bead above, not gqlc-gyw).

The spec is a review artefact for Linus (adversarial reviewer); every
blocker he raises is fixed on this same branch before the branch
merges. Cycle 2 (the R5 code cycle) begins only when the spec cycle
merges.
