# Stage R6 spec — resolver: writes (Effect validation, projection-less writes)

The implementation brief for Stage R6 of `internal/resolver`, extending the
merged R0/R1/R2/R3/R4/R5 kernel (`docs/specs/resolver-stage-r0.md` through
`docs/specs/resolver-stage-r5.md`) with the single capability arm ADR 0009
assigns to R6: **effect validation against the schema — SET/REMOVE property
and label existence, CREATE/MERGE shape (labels form valid keys, endpoints
resolvable), DELETE targets — and the projection-less-writes wire posture
that resolves to a zero-column `ValidatedQuery`**. Build this **test-first**.
Scope, sequencing, error posture, `ValidatedQuery`'s top-level shape, purity,
and the golden-pair harness inherit from ADR 0009 and R0–R5 unchanged; this
document revises only the rows, kernel arms, `ValidatedQuery` axes,
sentinel set, and out-of-scope table entries that R6 changes.

Stage R6 admits every query shape R5 admits, extended to:

- one or more `Effect`s per `Part` (any Part, any branch) whose per-variant
  targets, refs, labels, and endpoints resolve against the schema per §4.1;
- `Part`s that are **projection-less** (`len(Effects) > 0 && Returns == nil
  && !ReturnsAll`) — the resolver drops the R5 rejection of empty
  `Returns`/`ReturnsAll` when Effects carry the Part;
- `ExprUse` at `ExprInSetValue` and `ExprInDeleteTarget` — the R5 arms at
  `resolve.go:709-712` retire; witnesses come from the enclosing Effect's
  typed-write / typed-delete-target contract per §4.4.

`CallBinding` / CALL / YIELD (R7), `PathBinding`, `UnwindBinding`, untyped
edges, and `ExprProjection` typed `list<node>` / `list<edge>` remain out of
scope and continue to route to `ErrOutOfR0Scope`. ~~The R4 Class A and
Class B same-Part regime (b) nullability under-approximation (`gqlc-ay9`,
`gqlc-5xg`)~~ [closed 2026-07-10: Class A landed via gqlc-ay9 (PRs
#127/#128/#129, `docs/specs/model-change-ay9-optional-group.md`; residual
gqlc-984) and Class B / same-Part regime (b) landed via gqlc-5xg
(PRs #132/#133/#134,
`docs/specs/model-change-5xg-required-bare-ref.md`; residual gqlc-0kq);
R6 inherits both closures unchanged.] and the R5
`ExprProjection`-residual grouping-key gap
(`gqlc-gyw` Shape B / `gqlc-hk0` per the R5 close-out) are unchanged at R6.
The R5 cross-Part parameter-Use attribution gap (`gqlc-fvo`) is unchanged at
R6. ~~None of these gaps is closable without a model change (owner decision
pending); R6 does not contort the resolver around any of them.~~
[closed 2026-07-10: the two R4 nullability gaps landed as additive
`Binding` axes (ay9, 5xg); the R5 gaps (`gqlc-hk0`, `gqlc-fvo`)
persist at R6; R6 does not contort the resolver around them.]

R6 introduces **one new sentinel**, **`ErrInvalidEffectTarget`** (§5.1),
covering the one class of write-shape failure that no R0–R5 sentinel names
honestly: a `SET`/`REMOVE` whose target variable is bound at the parser but
resolves to a projection-alias / literal / carried-scalar entry rather than
to an entity (node or edge) binding table — the only fault a write clause
can produce that is not "schema disagrees with the target's shape" and is
therefore not already covered by `ErrUnknownLabel` / `ErrUnknownProperty` /
`ErrUnknownEdge`. Existing sentinels' message sets widen where R6 admits new
fail-sites.

R6's wire delta on `ValidatedQuery` is **zero**: `Columns`, `Parameters`,
`Statement`, `Distinct`, and per-`Column` `GroupingKey` are unchanged in
shape and in field type. The `Columns` field's *population rule* widens
(§3.1): when branch 0's final `Part` is projection-less, `ValidatedQuery.
Columns` is `[]Column{}` — a non-nil empty slice, marshalling as
`"columns": []`. Effects themselves DO NOT surface on the wire at R6 — the
consumer (codegen, R-later) reaches the effect list on the ADR 0008
`query.Query` side, and the resolver's job is to certify that every Effect
would succeed against the schema. §3.4 records the exact wire posture.

---

## 1. Deliverables

- `internal/resolver/validated.go` — **no additions**. `ValidatedQuery`,
  `Column`, `ResolvedParameter`, `StatementKind`, and the eight
  `ResolvedType` variants are unchanged in shape. The `Columns` field
  populates from a zero-column-emitting kernel path for projection-less
  writes (§3.1); the type of the field is unchanged.
- `internal/resolver/errors.go` — one new sentinel, **`ErrInvalidEffectTarget`**
  (§5.1). `ErrOutOfR0Scope`'s message-set list revises to reflect the two
  retirements (`ExprInSetValue` / `ExprInDeleteTarget` — §5.2). R5 sentinel
  identities are preserved; wrapped-message sets widen for
  `ErrUnknownLabel`, `ErrUnknownProperty`, `ErrUnknownEdge` as recorded in
  §5.3.
- `internal/resolver/resolve.go` — extended with:
  - a **per-Part effect-admission gate lift** (§4.1) that replaces the
    R5 gate at `resolve.go:153-155` (`if len(part.Effects) != 0 { return …
    ErrOutOfR0Scope: "write clause" }`; verify verbatim) with a walk over
    `part.Effects` after Phase D nullability. Each Effect variant routes
    to its per-variant validator (§4.2 for CREATE/MERGE, §4.3 for
    SET/REMOVE, §4.4 for DELETE).
  - a **projection-less-Part relaxation** (§4.6) that admits a Part whose
    `Returns` is empty AND `ReturnsAll` is false AND `Effects` is
    non-empty; the projection walk emits no columns, and the Part
    contributes an empty `[]Column{}` to `exportScope` when non-final and
    to `ValidatedQuery.Columns` when the branch-0-final Part is
    projection-less (§3.1).
  - **`ExprUse` at `ExprInSetValue` / `ExprInDeleteTarget` admission**
    (§4.5) — `witnessAcrossScopes` (resolve.go:709-712 currently
    short-circuits with `ErrOutOfR0Scope`) revises to produce a witness
    from the enclosing Effect's typed-write / typed-delete-target
    contract.
- `test/data/resolver/valid/schemas/` — one new schema fixture
  (`social_r6.gql`, §6.2) extending `social_r5.gql` with two shapes R6
  exercises: (a) a `Draft` node type disjoint from `Post` so relabelling
  fixtures have a stable second `NodeType` label to add/remove, and (b) a
  `Tag` node type with an `AUTHORED` predecessor absent from the schema
  so `MERGE (…)-[:AUTHORED]->(:Tag)` reaches `ErrUnknownEdge` cleanly.
  R0–R5 schemas are untouched.
- `test/data/resolver/valid/*.cypher` and `.validated.golden.json` — new
  R6 valid fixtures (§6.3), each paired with its schema through the
  updated `schema.mapping.json`. **No axis on any existing R0–R5 golden
  changes.** Every R0–R5 valid golden is byte-identical at branch tip
  (§3.3); the R6 code cycle asserts this by re-running `just test`
  without `-update` against the R0–R5 corpus.
- `test/data/resolver/invalid/*.cypher` — new R6 invalid fixtures (§6.4)
  for the new sentinel `ErrInvalidEffectTarget` and for the widened
  fail-sites on `ErrUnknownLabel`, `ErrUnknownProperty`, `ErrUnknownEdge`.
  The R5 `expr_use_set_value.cypher` fixture RETIRES (its sentinel
  changes from `ErrOutOfR0Scope` to a validated-happy-path witness);
  §6.4 records the retirement inline. Other R5 invalid fixtures whose
  targets remain out of scope at R6 (CALL, path/unwind, `list<node|edge>`
  projections, var-length property projection, untyped edge) stay
  unchanged.
- `internal/resolver/resolver_test.go` — updated `invalidFixtures` map
  (§6.4). No structural changes to `TestValid` / `TestInvalid` /
  `TestSentinelReachability`; the R0–R5 harness scales as-is.

Nothing downstream of the resolver is built — `ValidatedQuery` remains
provisional through R7 (ADR 0009 §Decision). R6 lands zero wire-shape
additions; the widened `Columns` population rule is a behaviour change
that does not alter the JSON shape of any existing golden.

---

## 2. Architecture — deltas from R5

R0/R1/R2/R3/R4/R5's architecture stands (the `Resolver` struct, its
compile-time inputs, the `QueryResolver` interface + compile-time
assertion, purity and short-circuit posture, `resolve.go`/`Resolve`
split, the branch-and-part driver, the R5 per-Part kernel body:
Phase A1, A2, B, C, D, projection walk, parameter walk). R6 preserves
this shape verbatim and inserts one new phase after Phase D — **Phase E:
effect validation** — and lifts the projection-required guard for
Parts whose `Effects` carry the write side.

### 2.1 The R6 kernel structure

The outer branch-and-part driver is unchanged from R5 §2.1. Inside
`resolvePart`, the kernel gains one phase between R5's Phase D and the
projection walk:

```
resolvePart(part, carry, s):
  R5 §4.1.1 defensive tripwires (Effects gate LIFTED at R6 — §4.1)
  Phase A1 — local labelled node bindings + edge admission screening
  Phase A2 — R3-admitted edges: candidate-set formation
  Phase B  — unlabelled-node inference over R3-admitted touching edges
  Phase C  — close deferred edges
  Phase D  — nullability seed + demotion
  Phase E  — effect validation (NEW at R6 — §4.1..§4.5)
             validateEffects(part.Effects, nodeTypes, edgeTypes,
                             edgeKeys, edgeCands, edgeBindings,
                             carry.exportedResolvedTypes,
                             nullableBinding, s, part.Bindings)
  scopeOrder    = R5 §4.2.3 buildScopeOrder(...)
  items         = R5 §4.4 materialiseReturns(...)   (returns empty for
                                                    projection-less Parts)
  columns       = projection walk over items       (empty for projection-less)
  site          = R5 §4.2.4 snapshotScope(...)
  exported      = R5 §4.2.2 exportScope(...)       (columns may be empty)
  return columns, exported, [site]
```

Phase E runs after Phase D so effect targets see the same
schema-committed binding tables and effective-nullability map that the
projection walk sees. This is not an optional invariant — carried
bindings must be in `nodeTypes`/`edgeTypes`/`edgeCands`/`edgeBindings`
before their properties can be looked up on a SET or REMOVE target
(§4.3, §4.4). The seed order matches R5's Phase D → projection order.

**Phase E has no effect on the returned tables.** `validateEffects`
reads `nodeTypes` / `edgeTypes` / `edgeCands` / `edgeBindings` /
`nullableBinding` / `carry.exportedResolvedTypes` and either returns
nil or a sentinel-wrapped error. It never mutates them. In particular,
CREATE's newly-introduced NodeBindings and EdgeBindings enter
`part.Bindings` via the parser (Stage 12 §1.3 — CREATE reuses
`collectPattern`), so Phase A1/A2/B/C have already committed them
before Phase E runs; validate does not re-open the binding tables.
MERGE bindings behave identically (Stage 13 §1.1 — MERGE reuses
`collectPattern`).

### 2.2 Kernel helpers — one new; one revised; the projection walk unchanged

One new helper in `resolve.go`:

- **`validateEffects(effects []query.Effect, nodeTypes map[string]schema.
  NodeType, edgeTypes map[string]schema.EdgeType, edgeKeys map[string]
  schema.EdgeKey, edgeCands map[string][]schema.EdgeKey, edgeBindings
  map[string]query.EdgeBinding, carriedResolvedTypes map[string]
  ResolvedType, nullableBinding map[string]bool, s schema.Schema,
  bindings []query.Binding) error`** (new). Dispatches each Effect
  through the per-variant validator (`validateCreateEffect`,
  `validateDeleteEffect`, `validateSetPropertyEffect`,
  `validateSetEntityEffect`, `validateSetLabelsEffect`,
  `validateRemovePropertyEffect`, `validateRemoveLabelsEffect`,
  `validateMergeEffect`). Returns the first sentinel-wrapped failure.
  `bindings` is passed so CREATE / MERGE validators can locate the
  `NodeBinding` / `EdgeBinding` a variable name refers to (per Stage 12
  §1.3 / Stage 13 §1.1, CREATE / MERGE bindings enter `Part.Bindings`
  verbatim). The eight per-variant validators are private, each named
  after its Effect variant. Each is a pure function of its inputs.

One R5 helper gains a revised behaviour (signature unchanged):

- **`witnessAcrossScopes`** (`resolve.go:673-719`). The
  `ExprInSetValue` arm (line 709-710) and `ExprInDeleteTarget` arm
  (line 711-712) retire their `ErrOutOfR0Scope` fast-fails. The revised
  behaviour is per §4.5: `ExprInSetValue` witnesses as the enclosing
  SetPropertyEffect / SetEntityEffect's typed value contract (below
  the type-interface boundary — the enclosing Effect's `ValueType` is
  the type the R2 lattice unifies against); `ExprInDeleteTarget`
  witnesses as `ResolvedUnknown{}` (the entity-kind gate is runtime
  only per ADR 0005 / Stage 12 §1.4).

R0–R5's other helpers are behaviour-unchanged. In particular:
`buildScopeOrder`, `materialiseReturns`, `virtualProjection`,
`exportScope`, `choose`, `fillGroupingKeys`, `compareBranchColumns`,
`resolvedTypeEqual`, `computeDistinct`, `unifyParameterUsesAcrossScopes`,
`scopeContains`, `snapshotScope`, `r3EdgeAdmissible`, `edgeCandidates`,
`closeEdge`, `endpointLabels`, `formatEdgeKey`, `formatEdgeKeys`,
`describeTriedEdges`, `inferUnlabelled`, `candidateTypes`,
`touchingSide`, `intersect`, `joinCandidates`, `projectionType`,
`refProjectionType`, `unionProperty`, `resolveType`,
`propertyUseWitness`, `seedLocalNullability`, `demoteNullableInPlace`,
`bindingVariable`, `qualifiedDemoter`, `unify` — none of these are
touched by R6.

### 2.3 Purity, determinism, short-circuit — unchanged

R0 §2.3 and R5 §2.4 stand. R6 introduces no goroutine, no time source,
no map iteration that escapes into output. Effect walks proceed in
`Part.Effects` slice order — parser walk order per Stage 12 §1.2 /
Stage 13 §1.1 — which is deterministic. Short-circuit is preserved:
the first Effect failure short-circuits Phase E; the first Phase E
failure short-circuits the Part; the first Part failure short-circuits
the branch; the first branch failure short-circuits the query.

Effects DO NOT contribute parameter Uses beyond what the parser
already emitted on `q.Parameters` (Stage 12 §1.4-§1.6, Stage 13
inline-map widening) — the `ExprInSetValue` and `ExprInDeleteTarget`
Uses are already on the wire at R5 and reach the top-level
`unifyParameterUsesAcrossScopes` walker via the existing R5 mechanic.
R6 changes only what the walker does when it reaches an `ExprInSetValue`
or `ExprInDeleteTarget` Use (§4.5); it does not change WHEN or WHERE
the walker sees Uses.

### 2.4 Effects × multi-part × multi-branch composition

Effects can appear in ANY `Part` of ANY `Branch`. The parser accepts
this (Stage 12 §1.1: "a query composed only of writes is a legal
complete query"; `Query.StatementKind` flips to `StatementWrite` iff
any outer-scope write clause in any branch fires — Stage 12 §3.1).
The resolver runs Phase E per Part, independently. Two composition
rules follow:

- **Cross-Part effect target resolution.** A `SET` / `REMOVE` /
  `DELETE` / `MERGE` in Part K may target a binding carried from
  Part K-1's WITH exports. Phase A1/A2/B/C's carry-seed step (R5
  §4.2.3) has already populated Part K's `nodeTypes` / `edgeTypes` /
  `edgeCands` / `edgeBindings` from the carry; Phase E sees carried
  bindings identically to Part K's own bindings. Concrete: `MATCH
  (a:Person) WITH a SET a.age = 42 RETURN a` — Part 1 sees `a:Person`
  in `nodeTypes` via the carry, and `validateSetPropertyEffect`
  looks up `Person.age` on the schema NodeType. Fixture:
  `set_property_across_with.cypher` (§6.3).
- **Nullability composition — R6 does not police it.** OpenCypher
  runtime rejects a `SET` against a NULL target; that is bucket 3
  (ADR 0007 §III) and not a compile-time property of the query the
  resolver commits to. Concrete: `OPTIONAL MATCH (a:Person) SET
  a.name = 'x' RETURN a` — the parser accepts, `a` is nullable per
  R4 flow-typing, and R6 admits the write. The runtime raises on the
  NULL row. This posture matches the ADR 0009 discipline: the
  resolver certifies schema-shape agreement, not runtime effects.
  Fixture: `set_property_on_nullable.cypher` (§6.3).
- **Cross-branch effects.** A UNION branch containing an Effect is
  admitted per-branch by Phase E; UNION column-compatibility (R5
  §4.3) then runs on the branches' final Parts as usual. When one
  UNION branch is projection-less (writes-only, zero columns) and
  another branch has projections, R5's `compareBranchColumns`
  (`resolve.go:526-546`) fires `ErrUnionColumnMismatch` on column
  count (0 vs N). This is the correct outcome: openCypher's
  `InvalidClauseComposition` is a runtime rule (bucket 3), but the
  resolver's UNION-column check catches the shape at compile time
  by construction. Fixture: `union_writes_vs_returns_column_count.
  cypher` (§6.4 invalid).

The parser does not attribute Effects to any Part-index axis — every
Effect lives directly on `Part.Effects` (`internal/query/query.go:
116-122`). R6 has no Use→Part attribution problem for Effects because
the parser did not create one.

### 2.5 What R6 does NOT admit

Two constructs remain rejected at R6 and route to `ErrOutOfR0Scope`:

- **`CallBinding`** — R7 owns CALL/YIELD; the sentinel and message
  are unchanged from R5.
- **`PathBinding`** — path bindings are R-later (dialect posture,
  ADR 0008 §"shortestPath is a dialect extension"). `Part.Bindings`
  containing a `PathBinding` continues to fire the R5 default arm at
  Phase A1 (`resolve.go:238-239`).
- **`UnwindBinding`** — R-later. Same reject arm.
- **Untyped edge** (`len(Labels()) == 0`) — R-later. `r3EdgeAdmissible`
  reject arm is unchanged. This applies to CREATE/MERGE edge patterns
  too: a `CREATE (a)-[r]->(b)` with an unlabelled edge fails at Phase
  A1 before Phase E fires.
- **`ExprProjection` typed `list<node>` / `list<edge>`** — R-later.
  `resolveType` reject arm unchanged.
- **Property projection on a variable-length edge binding** — R-later.
  `refProjectionType` reject arm unchanged.
- **`ExprUse` at any position not in the closed set** — the R5
  `default` arm at `resolve.go:713-715` stands.

The R5 rejection of `ExprInSetValue` / `ExprInDeleteTarget`
(`resolve.go:709-712`) RETIRES at R6.

---

## 3. `ValidatedQuery` — the R6 shape

`ValidatedQuery`'s top-level shape (R0 §3.1, R5 §3) is unchanged at
R6. `Columns`, `Parameters`, `Statement`, `Distinct`, and per-`Column`
`GroupingKey` retain their R5 field types and semantics. R6 changes
only what the resolver puts INTO `Columns` for a specific query shape.

### 3.1 `ValidatedQuery.Columns` — the projection-less-Part rule

At R5, `ValidatedQuery.Columns` was populated from branch 0's final
Part (expanded per §4.4 for `ReturnsAll`). At R6, the same rule
applies with one refinement: **when branch 0's final Part is
projection-less** (`Returns == nil && !ReturnsAll && len(Effects) >
0`), `ValidatedQuery.Columns` is a **non-nil empty slice** (Go
literal `[]Column{}`), so the JSON encoding is `"columns": []` —
matching R5's always-emit posture on the field.

Concretely, the `resolveBranch` return path for a projection-less
final Part emits `finalCols := []Column{}`, not `finalCols := nil`.
This is a deliberate wire-encoding choice: `null` and `[]` are
semantically indistinguishable in the JSON reader used by the golden
harness, but `[]` reads better and is unambiguous downstream. Every
R6 golden with a projection-less branch-0 final Part pins
`"columns": []`.

**When is a Part projection-less at R6?** Exactly when the parser's
Stage 12 §1.2 relaxation applies: `Effects` is non-empty AND
`Returns` is empty AND `ReturnsAll` is false. Any other combination
follows the R5 rules (empty `Effects` requires non-empty
`Returns`/`ReturnsAll` per parser invariant; empty `Bindings` is
already admitted at R5 for literal-only shapes like `RETURN 1`).

**UNION with mixed projection-less and projected branches.** As
recorded in §2.4, this fails `ErrUnionColumnMismatch` on column
count — a projection-less branch has zero columns; a projected
branch has ≥ 1. Fixture: `union_writes_vs_returns_column_count.
cypher` (invalid, §6.4). This is a compile-time detection of the
shape openCypher rejects at runtime as `InvalidClauseComposition`
(Stage 12 §3.1 note); the type interface catches it by
construction.

### 3.2 `ValidatedQuery.Distinct` — unchanged

R5 §3.2's fold rule stands. A projection-less write Part has
`Part.Distinct` in scope but nonsensical (there is nothing to
deduplicate); the parser's `NewPart` invariant admits any
`distinct` value, so the fold reads it unchanged. If a
projection-less Part carries `Part.Distinct == true` (parser-
accepted, semantically odd), R6 folds it into `ValidatedQuery.Distinct`
per §3.2 — no rejection, no special-casing. Fixture: none. R6 does
not exercise this shape (out-of-corpus).

### 3.3 `Column.GroupingKey` — unchanged

R5 §3.2.1's `GroupingKey` axis is unchanged. A projection-less Part
emits zero columns; `fillGroupingKeys` (`resolve.go:493-521`) short-
circuits harmlessly on an empty column slice.

### 3.4 R0–R5 golden byte-identical claim — no rebaseline

**Every R0–R5 valid golden is byte-identical after R6 lands.** R6
adds no field to `ValidatedQuery`, adds no field to `Column`, adds
no `ResolvedType` variant, adds no discriminator tag. Every existing
golden's `ValidatedQuery` JSON serialises identically before and
after the R6 code cycle merges. The R6 code cycle asserts this by
running `just test` **without** `-update` against the R0–R5 corpus;
any golden that regenerates differently is a bug.

Explicit byte-identical enumeration — the 76 valid stems at branch
base `origin/master` (from `ls test/data/resolver/valid/*.validated.
golden.json | sed 's|.*/||; s|\.cypher\.validated\.golden\.json$||'
| sort`):

```
aggregate_at_with
aggregate_count_star
aggregate_sum_property
aggregate_with_expr_residual
aggregate_with_grouping
carry_nullable_binding
carry_wins_over_unlabelled_rebind
demote_chained_from_required
demote_cross_with_remerge
demote_from_anonymous_required_edge
demote_required_edge_endpoints
demote_undirected_edge_endpoints
demote_var_length_positive_min
demote_var_length_unbounded_lower
distinct_projection
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
literal_only_return
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
parameter_across_union_same_name
parameter_across_with_alias_shadow
parameter_across_with_multi_part
parameter_clause_slot_limit
parameter_clause_slot_skip
parameter_expr_predicate
parameter_property_and_unknown_expr
parameter_two_property_uses_agree
returns_all_local_first_ordering
returns_all_multiple_bindings
returns_all_simple
returns_all_with_edge
self_loop_directed
two_edges_shared_binding
undirected_single_match
undirected_single_match_reverse
undirected_var_length_multi_type_property
union_all_matched_columns
union_matched_columns
unlabelled_binding_from_edge
unlabelled_binding_target_inferred
unlabelled_via_multi_type
unlabelled_via_undirected
var_length_directed
var_length_multi_type
var_length_undirected_single_match
with_carry_binding
with_carry_property_projection
with_carry_transitive
with_distinct
with_star_forward
with_where_predicate
```

**Refresh the enumeration by scripting**, not by hand-transcription:
run the `ls | sed | sort` pipeline above at the branch base commit
before publishing this spec. Any divergence between the enumerated
stems and the disk stems at HEAD is a bug in the spec, not in the
goldens.

**Wire-shape audit for the R6 code cycle.** After the R6 code cycle
lands its `validateEffects` phase and its projection-less relaxation,
`git diff test/data/resolver/valid/*.validated.golden.json` on the
final commit MUST be empty. Any change indicates a regression in an
R0–R5 code path.

### 3.5 Effects DO NOT surface on `ValidatedQuery`

**Judgment call — Effects stay on `query.Query`, not on
`ValidatedQuery`, at R6.** The resolver's job at R6 is to certify
that every Effect would succeed against the schema. Codegen (R-later)
reaches the effect list on the `query.Query` side (accessible
to codegen via the passed-in `query.Query` alongside the
`ValidatedQuery`). Two reasons R6 does not add an `Effects`
axis to `ValidatedQuery`:

1. **No consumer at R6.** ValidatedQuery is provisional through R7
   (ADR 0009 §Decision). Adding an axis before a consumer demands
   it is speculative modelling — the exact anti-pattern the
   staged-build discipline prevents. When codegen lands (post-R7,
   under a separate ADR — the ADR 0008 analogue), it can choose
   whether to consume Effects from `query.Query` or from a resolved
   analogue on `ValidatedQuery`; that decision is not R6's to
   make.
2. **Every fact codegen needs is already on `query.Query`.** The
   sixty-day-query model records the effect kind, target Ref,
   value type, refs, labels, endpoints, MERGE-ON branches, and
   the `Detach` bit — all facts codegen consumes verbatim. The
   resolver's Phase E outcome is BINARY (admit or reject); the
   information codegen needs beyond that is already on the input.

If future evidence shows codegen needs a resolved-type analogue of
some Effect field (e.g. the schema `PropertyType` of a
`SetPropertyEffect`'s target), R7-close-out is the earliest natural
insertion point, under a superseding provisional ADR. R6 does not
pre-commit.

### 3.6 Wire-encoding invariants — the R6 golden posture

Every R6 fixture golden carries the R5 fields (`columns`,
`parameters`, `statement`, `distinct`) unchanged in shape. The
`columns` field for a projection-less branch-0 final Part is
`[]` (non-nil empty JSON array, §3.1). No Effect information
appears anywhere in the golden. Statement kind is `"write"` iff
`Query.StatementKind` is `StatementWrite` per the parser's Stage 12
§3.1 rule — R6 does not compute it; it copies from `q.StatementKind`
via the R5 code path (`resolve.go:51`).

---

## 4. The R6 kernel algorithm

Each step below extends or replaces a numbered step of R5 §4. R0–R5's
per-Part kernel body is preserved verbatim; R6 adds Phase E and
retires two `ExprUse` reject arms.

### 4.1 The R6 admissibility widening — Effects on any Part; projection-less shape

R5's kernel gate at `resolve.go:153-155` rejects Parts with any
Effects:

```go
if len(part.Effects) != 0 {
    return nil, branchState{}, nil, fmt.Errorf("%w: write clause", ErrOutOfR0Scope)
}
```

**Verify this exact block at branch base before writing the code.**
The line numbers are stable at branch base `origin/master @ ea04f2a`;
any drift is a merge conflict to resolve before proceeding.

R6 REPLACES this gate with two changes:

1. The `len(part.Effects) != 0` block is dropped entirely. Effects are
   admitted at every Part K, every Branch B.
2. After R5's Phase D (nullability seed + demote), Phase E fires:
   `validateEffects(part.Effects, nodeTypes, edgeTypes, edgeKeys,
   edgeCands, edgeBindings, carry.exportedResolvedTypes,
   nullableBinding, s, part.Bindings)`. First sentinel-wrapped error
   short-circuits.

#### 4.1.1 Admitted / rejected Effect variants

**All eight Effect variants are admitted at R6:**

| Effect variant | Validator (§) | Discriminating fixture |
|---|---|---|
| `CreateEffect` | §4.2 `validateCreateEffect` | `create_labelled_node.cypher` |
| `DeleteEffect` | §4.4 `validateDeleteEffect` | `delete_bare_node.cypher` |
| `SetPropertyEffect` | §4.3.1 `validateSetPropertyEffect` | `set_property_literal.cypher` |
| `SetEntityEffect` | §4.3.2 `validateSetEntityEffect` | `set_entity_replace_map.cypher` |
| `SetLabelsEffect` | §4.3.3 `validateSetLabelsEffect` | `set_labels_declared.cypher` |
| `RemovePropertyEffect` | §4.3.4 `validateRemovePropertyEffect` | `remove_property.cypher` |
| `RemoveLabelsEffect` | §4.3.5 `validateRemoveLabelsEffect` | `remove_labels.cypher` |
| `MergeEffect` | §4.2 `validateMergeEffect` | `merge_node_labelled.cypher` |

**No Effect variant is rejected at R6.** Every parser-emitted Effect
shape has an R6 validator. Fixtures cover the happy path and each
failure edge; each invalid fixture pins a sentinel per §6.4.

#### 4.1.2 Projection-less Part shape — the R5 gate relaxation

R5's kernel does not have an explicit `Returns == nil && !ReturnsAll`
gate — `materialiseReturns` (`resolve.go:324-341`) returns
`part.Returns` unchanged (which is `nil` for a projection-less
Part), and the projection walk iterates zero times (`resolve.go:301`
`for _, item := range items`). Then `columns` is `[]Column{}` (make
of capacity `len(items)` = 0). The empty projection walk is
already benign under R5's kernel.

**The R5 kernel already emits the correct shape for a
projection-less Part — it is Phase E that R6 needs to add.** The
projection-less relaxation is not a code change to the kernel's
projection walk; it is a code change to the effect-admission gate.
Every projection-less-Part fixture in §6.3 exercises the same code
path R5 already runs, minus the Phase E gate rejection.

**One implementation subtlety.** `materialiseReturns` returns
`part.Returns` verbatim for a non-`ReturnsAll` Part, which is `nil`.
The projection walk then produces `columns := make([]Column, 0,
len(items))` with `len(items) == 0`, so `columns` is a non-nil
empty slice — matching the §3.1 wire contract. `resolveBranch`
returns `finalCols = columns` (the empty non-nil slice) verbatim.
The top-level `resolve` sets `ValidatedQuery.Columns = branchCols[0]`
(the empty non-nil slice), which marshals as `"columns": []`. No
special-case code is needed; the R5 kernel already emits the
correct wire.

### 4.2 CREATE and MERGE validation

Both variants share the same shape rule: the clause's introduced
NodeBindings must have labels that resolve to a schema `NodeType`;
the clause's introduced EdgeBindings must have labels that form a
valid `EdgeKey` with the resolved endpoint types. Both are already
enforced by the R0–R5 kernel through Phase A1/A2/B/C — a CREATE's
NodeBinding enters `part.Bindings` via `collectPattern` (Stage 12
§1.3), and Phase A1's labelled-node arm at `resolve.go:191-195`
runs `ErrUnknownLabel` on a mismatch; Phase A2's edge closure at
`resolve.go:243-255` runs `ErrUnknownEdge` on a missing EdgeKey.

**Consequence — CREATE and MERGE need almost no new Phase E code.**
The variants themselves are validated as a side effect of Phase A1/
A2/B/C running against the parser's `Bindings` slice, because
CREATE and MERGE both use `collectPattern` and therefore both
contribute to `Part.Bindings`. Phase E's `validateCreateEffect`
and `validateMergeEffect` do exactly ONE additional check each,
described below.

#### 4.2.1 `validateCreateEffect`

Signature: `validateCreateEffect(e query.CreateEffect,
nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.
EdgeBinding, carriedResolvedTypes map[string]ResolvedType) error`.

Algorithm:

1. For each variable name `v` in `e.Variables()`:
   - If `v == ""` (anonymous edge — Stage 12 §1.3; the empty
     string enters `CreateEffect.Variables` from an anonymous
     edge binding at `internal/query/cypher/listener.go:349-350`
     ("A named binding contributes its variable; an anonymous edge
     contributes an empty string"). An anonymous node contributes
     no binding at all, so does not fire this arm.): skip.
     Anonymous bindings are legitimately anonymous; nothing to
     verify by name.
   - Else, verify `v` is present in `nodeTypes` OR `edgeBindings`
     of the current Part. If it is not, that is a resolver-internal
     invariant breach (Phase A1/A2 must have committed every named
     variable in `Part.Bindings` before Phase E runs) — return
     `fmt.Errorf("%w: CREATE variable %q not bound after phase C",
     ErrInvalidEffectTarget, v)`. Reachability is zero from parser
     input (parser scope check rejects unbound Refs, and CREATE's
     bindings are the parser's own emissions), but the guard is
     kept as a defensive tripwire.

No new sentinel fires from `validateCreateEffect` under any
parser-emitted input; the entire CREATE-shape validation surface is
already the R0–R5 Phase A1/A2/B/C surface for `Part.Bindings`. The
worked trace:

```
Query: CREATE (n:Person) RETURN n
Parser output:
  Part{
    Bindings: [NodeBinding{"n", ["Person"]}],
    Returns: [{Name: "n", Value: RefProjection{Ref{n, ""}, TypeNode{}}}],
    Effects: [CreateEffect{Variables: ["n"]}],
  }
Phase A1: nodeTypes["n"] = Person (labelled arm, succeeds against schema)
Phase E: validateCreateEffect ranges e.Variables() → "n";
         "n" is in nodeTypes → OK.
Projection: RefProjection{Ref{n, ""}} → ResolvedNode{Person, false}.
Result: 1 column {"n", node Person}, distinct false, statement "write".
```

The failure edge (unknown label on a CREATE'd binding) is caught at
Phase A1, not Phase E — the fixture `create_unknown_label.cypher`
pins `ErrUnknownLabel` there (§6.4).

#### 4.2.2 `validateMergeEffect`

Signature: `validateMergeEffect(e query.MergeEffect, nodeTypes map[
string]schema.NodeType, edgeTypes map[string]schema.EdgeType,
edgeKeys map[string]schema.EdgeKey, edgeCands map[string][]schema.
EdgeKey, edgeBindings map[string]query.EdgeBinding, carriedResolvedTypes
map[string]ResolvedType, nullableBinding map[string]bool,
s schema.Schema, bindings []query.Binding) error`.

Algorithm:

1. Run the `validateCreateEffect` variable-presence check on
   `e.Variables()` (same defensive tripwire, same reachability
   guarantees).
2. For each `SetEffect` in `e.OnMatch()`: dispatch through the
   SET-family per-variant validator (§4.3). First failure short-
   circuits.
3. For each `SetEffect` in `e.OnCreate()`: same as step 2.

The `[]SetEffect` sealed sub-sum (`query.go:1651-1660`) guarantees
`OnMatch` and `OnCreate` contain only `SetPropertyEffect` /
`SetEntityEffect` / `SetLabelsEffect` — the R6 validator has no
runtime type-check to write; the compiler enforces the invariant.

**Worked trace**:

```
Query: MERGE (a:Person) ON CREATE SET a.age = 30 RETURN a
Parser output (per parser_test.go line 2018-2023):
  Part{
    Bindings: [NodeBinding{"a", ["Person"]}],
    Returns: [{Name: "a", Value: RefProjection{Ref{a, ""}, TypeNode{}}}],
    Effects: [MergeEffect{
      Variables: ["a"],
      OnCreate: [SetPropertyEffect{Ref{a, age}, TypeInt, nil}],
    }],
  }
Phase A1: nodeTypes["a"] = Person.
Phase E: validateMergeEffect →
  - validateCreateEffect variable-presence: "a" in nodeTypes → OK.
  - OnMatch is nil → skip.
  - OnCreate[0] = SetPropertyEffect{Ref{a, age}, TypeInt}:
    validateSetPropertyEffect: a → Person → Properties["age"] exists
    (INT nullable) → OK.
Projection: 1 column {"a", node Person, false}.
Result: 1 column, distinct false, statement "write".
```

The failure edge (unknown label in a MERGE endpoint) is caught at
Phase A1 (via `collectPattern` binding emission) — fixture
`merge_endpoint_unknown_label.cypher` pins `ErrUnknownLabel`. The
failure edge (ON MATCH SET on unknown property) is caught in step 2
of `validateMergeEffect` via `validateSetPropertyEffect` — fixture
`merge_on_match_unknown_property.cypher` pins `ErrUnknownProperty`.

### 4.3 SET-family and REMOVE-family validation

Four private validators cover the five SET/REMOVE variants. Each
resolves the target variable against `nodeTypes` / `edgeTypes` /
`edgeCands` / `edgeBindings`, and — for property targets — looks up
the property on the resolved schema entity.

#### 4.3.1 `validateSetPropertyEffect`

Signature: `validateSetPropertyEffect(e query.SetPropertyEffect,
nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.
EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings
map[string]query.EdgeBinding, carriedResolvedTypes map[string]
ResolvedType, s schema.Schema) error`.

Algorithm — resolve `e.Target()` (a `Ref{Variable, Property}` — both
non-empty; the constructor `query.go:1815-1818` enforces non-empty
`Variable`, and non-empty `Property` is grammar-guaranteed by the
parser's `propertyExpressionRef` at `internal/query/cypher/shape.go:
456-469` (empty lookup rejected at build time). §8 pins both.):

1. Let `v := e.Target().Variable`, `p := e.Target().Property`.
2. If `v` is in `nodeTypes`:
   - Let `nt := nodeTypes[v]`. If `nt.Properties[p]` is absent,
     return `fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)`.
   - Else OK.
3. Else if `v` is in `edgeTypes` (single-candidate edge):
   - Let `et := edgeTypes[v]`. Check `edgeBindings[v].Hops() != nil`
     — a var-length edge property SET is disallowed (openCypher
     targets one edge per SET item; a var-length binding is a list of
     edges). Return `fmt.Errorf("%w: SET on variable-length edge %q",
     ErrInvalidEffectTarget, v)`.
   - Else if `et.Properties[p]` is absent, return
     `fmt.Errorf("%w: %s.%s", ErrUnknownProperty, v, p)`.
   - Else OK.
4. Else if `v` is in `edgeCands` (multi-candidate edge, R3):
   - Check `edgeBindings[v].Hops() != nil` — same rejection as step
     3. Return `ErrInvalidEffectTarget`.
   - Else run `unionProperty(edgeCands[v], s, v, p, false)` (R3
     helper, `resolve.go:1085-1104`); the returned type is discarded
     — R6 only cares that the lookup succeeds. On mismatch,
     `unionProperty` returns `ErrUnknownProperty` — surface it.
   - Else OK.
5. Else — `v` is NOT in any binding table. Check
   `carriedResolvedTypes[v]`:
   - If `v` is in `carriedResolvedTypes` (i.e., a projection-alias
     export), return `fmt.Errorf("%w: SET %s.%s: %q resolves to a
     projection alias, not an entity binding", ErrInvalidEffectTarget,
     v, p, v)`.
   - Else — the parser's scope check should have rejected this at
     parse time. Defensive tripwire: return
     `fmt.Errorf("%w: SET %s.%s: %q not in any Part scope",
     ErrInvalidEffectTarget, v, p, v)`.

**Value-side check** — R6 does NOT attempt to unify
`e.ValueType()` against the target property type at R6. The parser's
Stage-6 typer produces `e.ValueType()` from the value expression
(literal, arithmetic, `$param`); the resolver's job at R6 is to
certify the schema-shape agreement of the target, not the value
side. The typed-write contract's runtime enforcement is bucket-3
(ADR 0007 §III) and is one of two loci where this spec explicitly
defers a design axis to R-later (see §7.1 for the full deferral
statement).

**Judgment call — no value-target type-agreement check at R6.**
The query model records `e.ValueType()` as `TypeString`,
`TypeInt`, or `TypeUnknown` (parser Stage 6); the schema records
the target property's `graph.PropertyType`. A width-preserving
type-agreement check would say "value `TypeInt` writes to
property `INT32`" — but the value-side's `TypeInt` from the parser
is coarse (a literal `42` is `TypeInt`, so is `1234567890123` which
does not fit in `INT32`), and the schema's `INT32` is a
bit-width family. A coarse-to-precise assignment is neither
strictly right nor strictly wrong at compile time — the runtime
does the range check. R6 defers to R-later once a consumer needs
the assignability decision; the axis lives naturally on the
codegen side of the boundary. See §7.1 for the deferral rationale.
Fixture: `set_property_int_literal_to_int32.cypher` (§6.3) pins
today's happy-path outcome (admitted; no width check).

**Rich value expressions — Uses handled by §4.5.** A SET value that
mines a `$param` records an `ExprUse{ValueType, ExprInSetValue}` on
the parameter. §4.5 details how that Use witnesses; the value-side
type from `e.ValueType()` is the witness's `EnclosingType`.

**Rich value expressions — Refs.** `e.Refs()` are the refs the
value expression touched (Stage 12 §1.5 "References inside SET
value expressions"). R6 does NOT re-run referential integrity on
these — the parser's `buildPart` sweep at `internal/query/cypher/
build.go:155-158` (per Stage 4 §4) has already asserted every ref
is bound in scope. Defensive: `validateSetPropertyEffect` may
optionally iterate `e.Refs()` and confirm each is in
`nodeTypes` / `edgeTypes` / `edgeCands`, but the check is
redundant with the parser's referential-integrity sweep. R6
implementation skips the check.

Worked trace:

```
Query: MATCH (n:Person) SET n.age = 42 RETURN n
Parser output (per parser_test.go line 1798-1811):
  Part{
    Bindings: [NodeBinding{"n", ["Person"]}],
    Returns: [{Name: "n", Value: RefProjection{Ref{n, ""}, TypeNode{}}}],
    Effects: [SetPropertyEffect{Ref{n, age}, TypeInt, nil}],
  }
Phase A1: nodeTypes["n"] = Person.
Phase E: validateSetPropertyEffect →
  step 2: nodeTypes["n"] = Person; Person.Properties["age"] present
          (INT nullable) → OK.
Projection: RefProjection{n} → ResolvedNode{Person, false}.
Result: 1 column {"n", node Person}, distinct false, statement "write".
```

Failure edges — one per sentinel:
- `ErrUnknownProperty` — `SET n.notAProp = 1` where `n:Person`.
  Fixture: `set_property_unknown_property.cypher` (§6.4).
- `ErrInvalidEffectTarget` — `MATCH (n) WITH count(n) AS c SET c.p =
  1`. Fixture: `set_property_on_projection_alias.cypher` (§6.4).
- `ErrInvalidEffectTarget` — SET on a var-length edge target.
  Fixture: `set_property_on_var_length_edge.cypher` (§6.4).

#### 4.3.2 `validateSetEntityEffect`

Signature: `validateSetEntityEffect(e query.SetEntityEffect,
nodeTypes map[string]schema.NodeType, edgeTypes map[string]schema.
EdgeType, edgeCands map[string][]schema.EdgeKey, edgeBindings
map[string]query.EdgeBinding, carriedResolvedTypes map[string]
ResolvedType, s schema.Schema) error`.

Algorithm — resolve `e.TargetVariable()` (a non-empty string per
parser smart constructor `query.go:1872-1875`):

1. Let `v := e.TargetVariable()`.
2. If `v` is in `nodeTypes` OR `edgeTypes` OR `edgeCands`, the
   target is an entity binding. Check `edgeBindings[v].Hops() != nil`
   — same var-length rejection as SetPropertyEffect step 3. OK
   otherwise.
3. Else if `v` is in `carriedResolvedTypes`: return
   `fmt.Errorf("%w: SET %s = ...: %q resolves to a projection alias,
   not an entity binding", ErrInvalidEffectTarget, v, v)`.
4. Else — defensive tripwire, same as SetPropertyEffect step 5.

**No property-existence check** — `SetEntityEffect` sets the whole
entity's properties from a map RHS. The RHS map's keys are runtime
(a `$param` map is opaque at compile time), so no compile-time
property-existence check applies. `SetOpReplace` and `SetOpMerge`
are semantic axes preserved on the wire; R6 does not distinguish
their validation paths.

Failure edge:
- `ErrInvalidEffectTarget` — `MATCH (n) WITH count(n) AS c SET c =
  {}`. Fixture: `set_entity_on_projection_alias.cypher` (§6.4).

#### 4.3.3 `validateSetLabelsEffect`

Signature: `validateSetLabelsEffect(e query.SetLabelsEffect,
nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.
EdgeBinding, carriedResolvedTypes map[string]ResolvedType,
s schema.Schema) error`.

Algorithm:

1. Let `v := e.TargetVariable()`.
2. If `v` is in `nodeTypes`: proceed to step 4 with the node
   target.
3. Else if `v` is in `edgeBindings`: return `fmt.Errorf("%w: SET
   labels on edge binding %q", ErrInvalidEffectTarget, v)`. OpenCypher
   `SET var :Labels` semantically targets a node — edges are typed
   at creation and their label is immutable at the runtime level.
4. Else if `v` is in `carriedResolvedTypes`: return
   `fmt.Errorf("%w: SET labels on projection alias %q",
   ErrInvalidEffectTarget, v)`.
5. Else — defensive tripwire.
6. **Label declaration check.** For each label `L` in `e.Labels()`:
   look up whether `L` appears as the label of any declared
   `NodeType` in the schema. Formalisation: exists `nt ∈ s.Nodes`
   such that `L ∈ nt.Labels`, where `nt.Labels` is a
   `graph.LabelSetKey` — its component labels are its comma- or
   colon-joined constituents (schema fixture text uses `:Person`,
   the LabelSetKey is `"Person"`; the resolver must check `L` as a
   component of at least one NodeType's LabelSetKey).
   - If no NodeType's label set contains `L`, return
     `fmt.Errorf("%w: SET %s:%s: label %q not declared on any node
     type", ErrUnknownLabel, v, L, L)`.

**Judgment call — label-existence check, not label-union-existence.**
`SET n:Foo` where `n:Person` adds label `Foo`. Does the schema need
to declare a `(:Person:Foo)` node type? OpenCypher runtime does not
require it (the resulting entity has the union set at runtime,
even if no declared NodeType matches); R6 policy matches: each
label individually declared is sufficient, no union check. The
resolver's job is to prevent typos (a schema-unknown label written
in a query) and to leave the runtime rule to the runtime. Fixture:
`set_labels_declared.cypher` (§6.3) — `SET n:Draft` where `Draft`
is declared as its own NodeType, and `n:Person` — pins the admit;
`set_labels_undeclared.cypher` (§6.4) — `SET n:Nonexistent` — pins
`ErrUnknownLabel`.

**Judgment call — label-existence lookup mechanic.** The schema's
`s.Nodes` map key is `graph.LabelSetKey` (e.g., `"Person"`,
`"Person:Employee"`). To check "does label `L` appear in any
NodeType?", the R6 validator iterates `s.Nodes` and asks each
NodeType's LabelSet for membership. A future R6 refinement could
precompute a `declaredLabels map[graph.Label]bool` for O(1)
lookup, but the iteration is O(|s.Nodes| × avg-arity) per call and
schemas are small; the spec commits to the naive iteration for
readability.

Failure edge:
- `ErrInvalidEffectTarget` — `SET e:Foo` where `e` is an edge
  binding. Fixture: `set_labels_on_edge.cypher` (§6.4).
- `ErrUnknownLabel` — `SET n:Nonexistent`. Fixture:
  `set_labels_undeclared.cypher` (§6.4).

#### 4.3.4 `validateRemovePropertyEffect`

Signature identical to `validateSetPropertyEffect`. Algorithm
identical: `e.Target()` (a `Ref{Variable, Property}` — both non-
empty per parser smart constructor `query.go:1963-1969`) must
resolve; the property must exist on the target's schema type.

**No value-side check** — `RemovePropertyEffect` carries no value.
The variant is the removal analogue of `SetPropertyEffect`; the
Stage-12 spec §1.6 pins the shape symmetry.

Failure edge:
- `ErrUnknownProperty` — `REMOVE n.notAProp` where `n:Person`.
  Fixture: `remove_property_unknown.cypher` (§6.4).
- `ErrInvalidEffectTarget` — `REMOVE` target is a projection alias.
  Fixture: `remove_property_on_projection_alias.cypher` (§6.4).

#### 4.3.5 `validateRemoveLabelsEffect`

Signature identical to `validateSetLabelsEffect`. Algorithm
identical.

Failure edge:
- `ErrUnknownLabel` — `REMOVE n:Nonexistent`. Fixture:
  `remove_labels_undeclared.cypher` (§6.4).
- `ErrInvalidEffectTarget` — `REMOVE e:Foo` where `e` is an edge
  binding. Fixture: none. (One `SetLabels`-on-edge fixture exercises
  the sentinel; adding a `RemoveLabels`-on-edge fixture would be a
  duplicate discriminator. §6.4 records the choice.)

### 4.4 DELETE validation

`validateDeleteEffect` covers the two DELETE shapes (bare vs. rich).

Signature: `validateDeleteEffect(e query.DeleteEffect, nodeTypes
map[string]schema.NodeType, edgeTypes map[string]schema.EdgeType,
edgeCands map[string][]schema.EdgeKey, edgeBindings map[string]query.
EdgeBinding, carriedResolvedTypes map[string]ResolvedType,
s schema.Schema) error`.

Algorithm:

1. For each `t` in `e.Targets()` (bare shapes: `var` or `var.prop`):
   - Let `v := t.Variable`, `p := t.Property`.
   - **If `p == ""` (bare-var shape `DELETE n`):** the target must
     be a node or edge binding (an entity DELETE); it may not be a
     projection alias. Check `v` in `nodeTypes` OR `edgeTypes` OR
     `edgeCands`:
     - If yes: OK. (Var-length edge DELETE is a legal shape per
       openCypher — "delete every edge in the list" — but has
       runtime-only meaning; R6 admits without policing.)
     - Else if `v` in `carriedResolvedTypes`: return
       `fmt.Errorf("%w: DELETE %s: %q resolves to a projection
       alias, not an entity binding", ErrInvalidEffectTarget, v, v)`.
     - Else: defensive tripwire.
   - **If `p != ""` (bare-prop shape `DELETE n.p`):** the property
     must exist on the target's schema type. Same property-existence
     check as `validateSetPropertyEffect` steps 2-5 (delegate to a
     shared helper `resolvePropertyRefStrictly` for reuse; if the
     helper does not exist, inline the check). Fires
     `ErrUnknownProperty` on a missing property. **Note:** a bare-
     prop DELETE (`DELETE n.age`) is semantically odd but parser-
     admitted; openCypher runtime treats it as "delete a scalar
     value bound to n.age", which is a bucket-3 semantic. R6
     admits after property existence — same posture as `SET n.p =
     ...`.
2. For each `r` in `e.Refs()` (rich shapes — `nodes(p)`,
   `friends[$idx]`):
   - **Referential integrity is the parser's job.** `curPart.refs`
     already covered by the parser's Stage 4 `buildPart` sweep. R6
     runs no additional check on `e.Refs()`. Defensive iteration:
     verify each `r.Variable` is in `nodeTypes` / `edgeTypes` /
     `edgeCands` / `edgeBindings`; if not — defensive tripwire
     (unreachable via parser input).

**No entity-kind check for bare-var DELETE.** OpenCypher requires
that `DELETE v` targets a node or edge — a `$param` value or a
scalar cannot be deleted. The parser's Stage-12 §1.4 field contract
(bare shapes go to `Targets`; rich shapes go to `Refs`) plus the
parser's referential-integrity sweep means `Targets` contains only
resolved Refs; step 1 above verifies the entity-binding
constraint by the "must be in nodeTypes/edgeTypes/edgeCands" rule.

**No `Detach` distinction at R6.** `Detach() == true` (DETACH DELETE)
and `Detach() == false` (DELETE) have identical Phase E validation
— openCypher's semantic distinction (cascade edges vs. require them
absent) is runtime (bucket 3, Stage 12 §1.4). R6 preserves the axis
on the wire (via `query.Query`) but does not consume it.

Worked trace:

```
Query: MATCH (n:Person) DELETE n
Parser output (per parser_test.go line 1774-1782):
  Part{
    Bindings: [NodeBinding{"n", ["Person"]}],
    Effects: [DeleteEffect{Targets: [Ref{n, ""}], Refs: nil, Detach: false}],
  }
Phase A1: nodeTypes["n"] = Person.
Phase E: validateDeleteEffect →
  Targets[0] = Ref{n, ""}: Property=="" branch:
    "n" in nodeTypes → OK.
  Refs is nil → skip.
Projection walk: 0 items → columns = [].
Result: 0 columns (projection-less), distinct false, statement "write".
```

Rich-target trace:

```
Query: MATCH (n) DELETE nodes($p)
Parser output (per parser_test.go line 1926-1936):
  Part{
    Bindings: [NodeBinding{"n", nil}],
    Effects: [DeleteEffect{Targets: nil, Refs: nil, Detach: false}],
  }
Parameters: [{Name: "p", Uses: [ExprUse{TypeUnknown, ExprInDeleteTarget}]}]
Phase A1: nodeTypes["n"] = <unlabelled; phase B/C tries but this is a
  bare MATCH (n), so it stays unresolved — depends on schema>. Assume
  the schema has exactly one NodeType, so Phase B commits it.
Phase E: validateDeleteEffect → Targets is nil, Refs is nil → OK.
Parameter walk: $p Uses = [ExprUse{TypeUnknown, ExprInDeleteTarget}]
  → §4.5 witnesses as ResolvedUnknown{}.
Projection walk: 0 items → columns = [].
Result: 0 columns (projection-less), distinct false, statement "write",
parameters = [{"p", ResolvedUnknown{}}].
```

Failure edges:
- `ErrInvalidEffectTarget` — `MATCH (n) WITH count(n) AS c DELETE
  c`. Fixture: `delete_projection_alias.cypher` (§6.4).
- `ErrUnknownProperty` — `MATCH (n:Person) DELETE n.notAProp`.
  Fixture: `delete_bare_property_unknown.cypher` (§6.4).

### 4.5 Parameter Uses under SET value and DELETE target — the R5 arms retire

`witnessAcrossScopes` at `resolve.go:673-719` currently:

```go
case query.ExprInSetValue:
    return nil, fmt.Errorf("%w: parameter used in SET value", ErrOutOfR0Scope)
case query.ExprInDeleteTarget:
    return nil, fmt.Errorf("%w: parameter used in DELETE target", ErrOutOfR0Scope)
```

**Verify these exact lines** (position 709-712 at branch base
`origin/master @ ea04f2a`). Any drift is a merge conflict.

R6 replaces both arms:

- **`ExprInSetValue`**: the Use records `ExprUse{valueType,
  ExprInSetValue}` where `valueType` is the enclosing
  `SetPropertyEffect.ValueType()` or `SetEntityEffect.ValueType()`
  (Stage 12 §1.5). Route through `resolveType(uu.EnclosingType())`
  — the same code path R5 uses for `ExprInProjection` /
  `ExprInPredicate` (line 703-708). This yields a `ResolvedType`
  witness (a schema-vocabulary type when `valueType` is a
  parser-coarse scalar; `ResolvedUnknown{}` when `valueType` is
  `TypeUnknown`).
- **`ExprInDeleteTarget`**: the Use records `ExprUse{TypeUnknown,
  ExprInDeleteTarget}` (Stage 12 §1.4 — the parser always uses
  `TypeUnknown` for the enclosing type of a DELETE-target ExprUse;
  see `internal/query/cypher/listener.go:499`). Route through
  `resolveType(uu.EnclosingType())` → `ResolvedUnknown{}`. Same
  code path as ExprInSetValue with `TypeUnknown` enclosing.

**Consolidation** — both new arms are `return
[]ResolvedType{resolveType(uu.EnclosingType())}, nil` (with the
error surfaced). They can share the code path with the existing
`ExprInProjection` / `ExprInPredicate` arm:

```go
case query.ExprInProjection, query.ExprInPredicate,
     query.ExprInSetValue, query.ExprInDeleteTarget:
    w, err := resolveType(uu.EnclosingType())
    if err != nil { return nil, err }
    return []ResolvedType{w}, nil
```

The `default` arm at `resolve.go:713-715` stays (defensive tripwire
for a future `ExprPosition` variant that lands under ADR 0008's
additive protocol without an R6 refresh).

**Acknowledgment — enclosing-type panic surface.** The consolidated
arm calls `resolveType(uu.EnclosingType())` on the parser-supplied
enclosing `graph.Type`. Under today's parser mining, the enclosing
type of an `ExprInSetValue` Use is always a scalar / map / list /
`TypeUnknown` (Stage 12 §1.5 / §1.10 — SET RHS is a value-tree, and
per ADR 0005 the value-tree lives below the type-interface
boundary). `TypeNode` / `TypeEdge` / `TypePath` are NOT emitted as
the enclosing type of a SET-RHS ExprUse today; the same holds for
`ExprInDeleteTarget` (always `TypeUnknown`, per Stage 12 §1.4). The
same latent hazard already exists at R5 for `ExprInProjection` and
`ExprInPredicate` — a resolver-side entity-kind widening in the
parser could reach either arm. R6 does NOT add a defensive filter
in code (see the R5 arm at `resolve.go:703-708` which does not
filter either); the hazard is called out here so that if a future
parser stage widens the enclosing-type surface, the consolidated
arm gets a filter alongside its R5 twin.

**Judgment call — `TypeUnknown` for `ExprInDeleteTarget` is honest.**
The Use records `ExprUse{TypeUnknown, ExprInDeleteTarget}` because
a DELETE target's entity kind is a runtime lookup (Stage 12 §1.4
"the value's entity kind is a resolver-time lookup below the parser
boundary per ADR 0005"). Committing to `ResolvedUnknown{}` at R6 is
strictly correct: the resolver has no schema-side witness for
what `nodes($p)` returns. Future stages that press a resolver-side
entity-kind inference (e.g. `nodes(...)` is always `list<node>` so
`$p` inside is `list<node>` — a `FuncProjection`-like return-type
inference) could refine this witness, but R6 does not attempt it.

**Interaction with R5's parameter unifier.** R5's
`unifyParameterUsesAcrossScopes` (`resolve.go:611-653`) walks each
parameter's Uses in parser order and unifies witnesses via
`unify` (`resolve.go:1256-1307`). The R2 `unify` at
`resolve.go:1256-1262` widens `ResolvedUnknown{}` on either side —
a parameter with `PropertyUse{p, name}` (witnesses to
`ResolvedProperty{STRING, false}`) and a co-occurring
`ExprUse{TypeUnknown, ExprInSetValue}` (witnesses to
`ResolvedUnknown{}`) unifies to `ResolvedProperty{STRING, false}`.
The `TypeUnknown` witness is the resolver's honest widen-me
posture; R2's lattice already handles it.

Discriminating fixture:
`set_property_bare_param.cypher` — `MATCH (n:Person) SET n.age =
$newAge`. Golden pins `$newAge → ResolvedProperty{INT, true}` when
another PropertyUse witness resolves it against `n.age`; if only
the `ExprUse{TypeUnknown, ExprInSetValue}` fires, `$newAge` stays
`ResolvedUnknown{}`. Parser Stage 12 §1.10 pins the parser output
as `ExprUse{TypeUnknown, ExprInSetValue}` (there is no
PropertyUse{Ref{n, age}} for `$newAge` — the parser does not mine
a `$param` in a SET value as a PropertyUse because the SET LHS
already carries `Ref{n, age}` for referential integrity; the
inline-map case Stage 12 §1.10 pins PropertyUse, but this is a
bare-atom SET value, not an inline-map CREATE). So the R6 golden
pins `$newAge → ResolvedUnknown{}`.

`delete_rich_expression_with_param.cypher` — `MATCH (n) DELETE
nodes($p)`. Parser Stage 12 §1.4 pins `$p → ExprUse{TypeUnknown,
ExprInDeleteTarget}`. Golden pins `$p → ResolvedUnknown{}`.

### 4.6 `materialiseReturns` and projection walk — unchanged

R5 §4.4's `materialiseReturns` (`resolve.go:324-341`) returns
`part.Returns` unchanged when `!part.ReturnsAll`. A projection-less
Part has `part.Returns == nil` and `!part.ReturnsAll`, so
`materialiseReturns` returns `nil`. The projection walk iterates
zero times, and `columns := make([]Column, 0, 0)` is the empty
non-nil slice matching §3.1.

**One edge — `WITH *` on a Part with only Effects.** A parser input
like `MATCH (a:Person) SET a.age = 42 WITH * RETURN a` gives Part 0
with `Effects = [SetPropertyEffect...]`, `Bindings = [NodeBinding{a}]`,
`ReturnsAll = true`. `materialiseReturns` builds the wildcard-
expanded items from `scopeOrder` — the R5 §4.4 code path — and
projection walk emits columns for each. Phase E has already fired
BEFORE `materialiseReturns` runs (per §2.1's kernel order), so the
SetPropertyEffect is validated before the projection walk sees the
Part. No new code needed. Fixture: `set_property_with_star.cypher`
(§6.3).

### 4.7 `exportScope` for a projection-less non-final Part

**A projection-less non-final Part exports nothing.** R5's
`exportScope` (`resolve.go:408-479`) reads `part.Returns` (or the
wildcard-expanded items for `ReturnsAll`) — both empty for a
projection-less Part — so `exported.exportedResolvedTypes` and
`exported.exportedOrder` are both empty. Downstream Parts see no
carry.

**Is this correct openCypher?** OpenCypher allows a mid-query write
that carries no bindings forward — the write-then-return shape
follows the parser's Stage-12 §1.2 admission ("a part with Effects
need not have Returns / ReturnsAll"). A `MATCH ... CREATE (n) WITH
* MATCH ...` is a projected Part (WITH * is a projection), not a
projection-less Part; `WITH *` carries the previous scope forward
including new bindings the CREATE introduced. A truly
projection-less Part with an Effect has no explicit or wildcard
projection, so no carry — matching the parser's own scope rule
(build.go:244-253 reads `rp.returns` which is empty). R6 preserves
this by construction — no new code.

**Fixture — projection-less non-final Part.** `MATCH (a:Person)
CREATE (b:Post) RETURN a` produces one Part with both
`CreateEffect["b"]` and `Returns: [{a, RefProjection{Ref{a, ""}}}]`
— that is a projected Part with an Effect, not a projection-less
Part. To exercise a projection-less non-final Part, one needs
`MATCH (a:Person) SET a.age = 42 WITH a MATCH ...` — that has a
non-empty WITH, so it is projected. The only shape with a
projection-less non-final Part is one that uses a projection-less
Part in the middle of a chain without carrying anything forward,
which is grammatically legal but semantically dead — the following
Part cannot reference any binding from before. The R5 fixture
`with_carry_binding.cypher` shape does not naturally extend to
this case. R6 files no §6.3 fixture for a projection-less non-
final Part; the R5 code path handles the empty-export path
correctly by construction.

---

## 5. Sentinels — the R6 revision

R5's closed sentinel set is `ErrUnknownLabel`, `ErrUnknownProperty`,
`ErrOutOfR0Scope`, `ErrUnknownEdge`, `ErrAmbiguousBinding`,
`ErrParameterTypeConflict`, `ErrAmbiguousEdgeOrientation`,
`ErrUnionColumnMismatch`, `ErrPartBindingTypeConflict` (nine). R6 adds
one sentinel — `ErrInvalidEffectTarget` (§5.1) — keeps the others,
and revises `ErrOutOfR0Scope`'s message set to reflect two retirements.

### 5.1 New sentinel — `ErrInvalidEffectTarget`

```go
// ErrInvalidEffectTarget is returned when a write clause's target
// variable is bound at the parser scope but resolves to something
// other than an entity (node or edge) binding — a projection alias
// exported by a WITH, a literal, or a scalar-typed carried entry.
// Concretely: SET / REMOVE / DELETE on a variable that lives in
// carriedResolvedTypes but not in nodeTypes / edgeTypes / edgeCands.
// Also fires for SET / REMOVE labels on an edge binding (labels are
// node-only), for SET / REMOVE / DELETE on a var-length edge property
// (a var-length binding is a list, not a single edge), and for the
// defensive tripwire where a write's target variable is not in any
// Part scope (parser scope check should have caught this; the guard
// keeps the invariant tight). Introduced at R6. See R6 spec §4.3,
// §4.4 for the fail-sites.
ErrInvalidEffectTarget = errors.New("invalid effect target")
```

Added to `allSentinels`. Reachability sweep: at least one invalid
fixture per named class (§6.4).

**Why this sentinel is distinct from the others.**

- `ErrUnknownLabel` — a label written in the query does not
  correspond to a declared NodeType. This is a schema-lookup
  failure: the label itself is unknown. `ErrInvalidEffectTarget`
  is not a label failure; the target's shape is the mismatch.
- `ErrUnknownProperty` — a property written in the query does not
  correspond to a declared property on a NodeType or EdgeType.
  Schema-lookup failure on a property lookup key. Category-
  differently from `ErrInvalidEffectTarget`: the property either
  does not exist (unknown-property) or the target is not the kind
  of thing that has properties (invalid-effect-target).
- `ErrUnknownEdge` — a directed edge binding's endpoints and label
  form an EdgeKey the schema does not declare. Schema-lookup
  failure on the edge triple. `ErrInvalidEffectTarget` is not
  about a schema lookup; it is about the *shape* of the target.
- `ErrParameterTypeConflict` — parameter Uses do not unify.
  Unrelated class.
- `ErrOutOfR0Scope` — a construct not admitted at the current
  resolver stage. R6 retires two `ErrOutOfR0Scope` fail-sites
  (§5.2) — those retirements move to `ErrInvalidEffectTarget`
  only for a subset of their input (the `ExprInSetValue` /
  `ExprInDeleteTarget` Uses themselves are ADMITTED at R6; they
  no longer route to `ErrOutOfR0Scope`).

Reusing any existing sentinel would be a category mistake:
`ErrUnknownProperty` for "target is a projection alias" would
misrepresent the failure as a schema-lookup issue; `ErrOutOfR0Scope`
for "target is a projection alias" would misrepresent it as a
staging issue (when in fact R6 admits the construct in general —
only this specific target is invalid).

### 5.2 Revised `ErrOutOfR0Scope` message set — retirements

The R5-era `ErrOutOfR0Scope` sub-cases that retire at R6:

- **`ExprInSetValue`** (was `resolve.go:709-710`) — R6 admits per
  §4.5.
- **`ExprInDeleteTarget`** (was `resolve.go:711-712`) — R6 admits
  per §4.5.

The R5-era `ErrOutOfR0Scope` sub-cases that remain (unchanged):

- Write clause / effects gate at `resolve.go:153-155` — **DROPPED
  entirely at R6.** No new message set here; the gate is gone.
  The single write-clause fail-site retires along with the gate.
- CALL / YIELD — R7 owns.
- Path binding (`Part.Bindings` default arm) — R-later.
- Unwind binding — R-later.
- Untyped edge — R-later.
- `ExprProjection` typed list-of-nodes / list-of-edges — R-later.
- Property projection on a variable-length edge binding — R-later.
- Unknown projection variant (defensive tripwire) — persists.
- Unknown Use variant (defensive tripwire) — persists.
- Unknown `ExprPosition` (defensive tripwire) — persists.
- Empty branches (defensive tripwire) — persists.
- Empty parts (defensive tripwire) — persists.

### 5.3 R5 sentinels' message sets — widenings

R5's nine sentinels retain their identity and are all in the R6
closed set. Their message sets widen where R6 admits new fail-sites:

- **`ErrUnknownLabel`** widens to include:
  - Unknown label in a CREATE-pattern NodeBinding (Phase A1; fixture
    `create_unknown_label.cypher`).
  - Unknown label in a MERGE-pattern NodeBinding (Phase A1; fixture
    `merge_endpoint_unknown_label.cypher`).
  - Unknown label in a `SET var:Labels` (Phase E,
    `validateSetLabelsEffect` step 6; fixture
    `set_labels_undeclared.cypher`).
  - Unknown label in a `REMOVE var:Labels` (Phase E,
    `validateRemoveLabelsEffect` step 6; fixture
    `remove_labels_undeclared.cypher`).
- **`ErrUnknownProperty`** widens to include:
  - Unknown property in a `SET n.p = value` (Phase E,
    `validateSetPropertyEffect` step 2/3/4; fixture
    `set_property_unknown_property.cypher`).
  - Unknown property in a `REMOVE n.p` (Phase E,
    `validateRemovePropertyEffect`; fixture
    `remove_property_unknown.cypher`).
  - Unknown property in a bare-prop DELETE (`DELETE n.p`; Phase E,
    `validateDeleteEffect` step 1; fixture
    `delete_bare_property_unknown.cypher`).
  - Unknown property in a MERGE-ON action's SetPropertyEffect
    (via `validateMergeEffect` → `validateSetPropertyEffect`;
    fixture `merge_on_match_unknown_property.cypher`).
- **`ErrUnknownEdge`** widens to include:
  - Unknown edge in a CREATE-pattern EdgeBinding (Phase A2; fixture
    `create_unknown_edge.cypher`).
  - Unknown edge in a MERGE-pattern EdgeBinding (Phase A2; fixture
    `merge_unknown_edge.cypher`).
- `ErrParameterTypeConflict`, `ErrAmbiguousBinding`,
  `ErrAmbiguousEdgeOrientation`, `ErrUnionColumnMismatch`,
  `ErrPartBindingTypeConflict`, `ErrOutOfR0Scope` — unchanged in
  message set (except the retirements above).

### 5.4 The closed R6 set

```go
var allSentinels = []error{
    ErrUnknownLabel,
    ErrUnknownProperty,
    ErrOutOfR0Scope,
    ErrUnknownEdge,
    ErrAmbiguousBinding,
    ErrParameterTypeConflict,
    ErrAmbiguousEdgeOrientation,
    ErrUnionColumnMismatch,
    ErrPartBindingTypeConflict,
    ErrInvalidEffectTarget,   // R6 addition
}
```

Ten sentinels total. `TestSentinelReachability` verifies every
sentinel above has at least one invalid fixture (§6.4) and every
invalid fixture maps to a sentinel in this list.

---

## 6. The golden-pair harness — R6 revision

### 6.1 Schema fixture strategy — one new schema

R0–R5's harness (`resolver_test.go`, `test/data/resolver/{valid,
invalid}/`) is preserved verbatim. R6 adds fixtures under both
`valid/*.cypher` (with paired goldens) and `invalid/*.cypher` (with
the paired `invalidFixtures` map entry). One new schema fixture:

- **`social_r6.gql`** — extends `social_r5.gql` with (a) a `Draft`
  node type disjoint from `Post` so relabelling fixtures have a
  stable second `NodeType` label to add/remove, and (b) a `Tag`
  node type with no `AUTHORED` predecessor so
  `MERGE (…)-[:AUTHORED]->(:Tag)` reaches `ErrUnknownEdge` cleanly.
  Existing `social_r5.gql` fixtures continue to resolve against
  their existing schema.

### 6.2 Schema fixture text — `social_r6.gql`

```
CREATE PROPERTY GRAPH TYPE SocialR6 AS {
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
    (:Draft {
        id       :: INT NOT NULL,
        title    :: STRING NOT NULL
    }),
    (:Company {
        ein      :: INT NOT NULL,
        name     :: STRING NOT NULL,
        founded  :: DATE
    }),
    (:Tag {
        name     :: STRING NOT NULL
    }),
    (:Person) -[:AUTHORED { publishedAt :: TIMESTAMP, views :: INT NOT NULL, likedAt :: TIMESTAMP }]-> (:Post),
    (:Person) -[:LIKES { likedAt :: TIMESTAMP }]-> (:Post),
    (:Post)   -[:AUTHORED { authoredBy :: STRING NOT NULL }]-> (:Person),
    (:Person) -[:KNOWS { since :: DATE }]-> (:Person),
    (:Company) -[:EMPLOYS { since :: DATE, role :: STRING }]-> (:Person)
}
```

**Design note — no `AUTHORED` edge to `Tag`.** The schema
deliberately omits any edge involving `Tag`, so `MERGE (a:Person)
-[:AUTHORED]->(t:Tag)` reaches `ErrUnknownEdge` (no matching
EdgeKey). `Tag` exists as a NodeType so the endpoint labels
individually resolve; the fault is on the edge triple, isolating
the failure mode.

**Design note — `Draft` has disjoint properties from `Post`.**
`Draft.title :: STRING NOT NULL` matches `Post.title :: STRING NOT
NULL` (deliberate — for a plausible re-labelling scenario), and
`Draft` has no `body` (so `REMOVE n:Post` fixtures do not
accidentally reach into removed properties). This keeps
relabelling-fixture behaviour deterministic.

### 6.3 R6 valid fixtures

Each fixture keyed to an R6 arm. Each has a paired
`.validated.golden.json`, generated with `-update`. Each entry
below lists the query, the schema (defaults to `social_r6.gql` for
R6 fixtures), the discriminating breakage, and the expected column
count in the golden.

**CREATE (§4.2.1):**

- `create_labelled_node.cypher` — `CREATE (n:Person) RETURN n`.
  Discriminates: Phase A1 admits `n:Person` against the schema;
  Phase E's `validateCreateEffect` variable-presence check
  succeeds; projection emits `n → node Person`. **Breakage**:
  under a mutant that keeps R5's Effects gate, the query rejects.
  Under a mutant that runs Phase E BEFORE Phase A1, the check
  reports `n` not in nodeTypes. Golden: 1 column, statement
  "write".
- `create_anonymous_node.cypher` — `CREATE ()`. Projection-less.
  Discriminates: anonymous binding skip in
  `validateCreateEffect`; projection-less shape emits `[]Column{}`.
  **Breakage**: under a mutant that treats "" as a required
  variable name, `validateCreateEffect` fails; under a mutant that
  emits `null` columns, the golden diff shows `null` instead of
  `[]`. Golden: 0 columns, statement "write".
- `create_edge_pattern.cypher` — `CREATE (a:Person)-[r:KNOWS]->
  (b:Person) RETURN r`. Discriminates: Phase A2 admits the KNOWS
  edge against the schema; projection emits `r → edge
  Person-KNOWS-Person`. **Breakage**: unknown edge in a CREATE
  pattern reaches Phase A2's `ErrUnknownEdge`. Golden: 1 column
  (edge), statement "write".
- `create_then_return.cypher` — `CREATE (n:Person) RETURN n.name`.
  Discriminates: a CREATE-introduced binding's schema properties
  are accessible in the same Part's projection. Golden: 1 column
  (STRING NOT NULL), statement "write".

**MERGE (§4.2.2):**

- `merge_node_labelled.cypher` — `MERGE (a:Person) RETURN a`.
  Discriminates: minimum MERGE variant, no ON actions. **Breakage**:
  under a mutant that runs OnMatch/OnCreate handlers when the
  slices are nil, the mutant panics on nil dereference. Golden: 1
  column (node), statement "write".
- `merge_with_on_match_set.cypher` — `MERGE (a:Person) ON MATCH
  SET a.age = 30 RETURN a`. Discriminates: `validateMergeEffect`
  routes the OnMatch SetEffect through `validateSetPropertyEffect`;
  `a:Person.age` exists. **Breakage**: skipping OnMatch actions
  misses the property check; a mutant that ignores OnMatch admits
  `SET a.notAProp = 30`. Golden: 1 column (node), statement
  "write".
- `merge_with_on_create_set_property.cypher` — `MERGE (b:Person)
  ON CREATE SET b.age = 42 RETURN b`. Direct analogue of
  `merge_with_on_match_set` for the OnCreate branch. Golden: 1
  column (node), statement "write".

**SET property (§4.3.1):**

- `set_property_literal.cypher` — `MATCH (n:Person) SET n.age =
  42 RETURN n`. Baseline SET property; `Person.age` is INT
  nullable. **Breakage**: under a mutant that swaps target for
  value in the ValueType check, the mutant either fails or
  fabricates a different result. Golden: 1 column (node),
  statement "write".
- `set_property_across_with.cypher` — `MATCH (a:Person) WITH a
  SET a.name = 'x' RETURN a`. Discriminates: carried Person
  binding is reachable at Phase E for the SET target.
  **Breakage**: under a mutant that consults only local
  bindings, the SET fails to find `a`. Golden: 1 column (node),
  statement "write".
- `set_property_on_edge.cypher` — `MATCH (a:Person)-[e:AUTHORED]->
  (b:Post) SET e.views = 0 RETURN e`. Discriminates:
  `validateSetPropertyEffect` step 3 (edgeTypes arm) fires
  successfully. **Breakage**: under a mutant that skips the
  edgeTypes arm, unknown-property fires spuriously. Golden: 1
  column (edge), statement "write".
- `set_property_bare_param.cypher` — `MATCH (n:Person) SET n.age
  = $newAge RETURN n`. Discriminates: `ExprInSetValue`
  parameter Use witnesses per §4.5 to `ResolvedUnknown{}` (the
  parser records `EnclosingType = TypeUnknown` for a bare
  `$param` SET value; the SET target's schema type does not
  propagate to the `$param` because the parser does not mine a
  PropertyUse in this shape). Golden: 1 column (node),
  parameter `$newAge → ResolvedUnknown{}`, statement "write".
- `set_property_int_literal_to_int32.cypher` — `MATCH (n:Person)
  SET n.age = 42 RETURN n.age` under a schema whose `Person.age ::
  INT` (nullable) — Golden pins today's admit posture (no width
  check; see §4.3.1 judgment call). **Breakage**: a future R-later
  refinement that enforces bit-width assignability would reject
  this query if the parser types the literal as `TypeInt` but the
  schema property is a narrower `INT32`. R6 does not enforce.
  Golden: 1 property column `n.age`, `INT`, `nullable: true`,
  statement "write". (Note: this fixture uses the R6 `social_r6.gql`
  schema unchanged; the `age` property is declared `INT` — the
  same declaration as R5's `social_r5.gql`. The fixture pins the
  admit posture, not a width-refinement outcome. Projecting
  `n.age` (rather than the whole node `n`) exposes the property's
  declared type + nullability directly on the wire, which is what
  the width-check discriminator is about; the projection choice is
  orthogonal to the SET admit-posture the fixture pins.)

**SET entity (§4.3.2):**

- `set_entity_replace_map.cypher` — `MATCH (n:Person) SET n =
  {name: 'x'} RETURN n`. Discriminates: SetOpReplace admits
  against an entity target. Golden: 1 column (node), statement
  "write".
- `set_entity_merge_map.cypher` — `MATCH (n:Person) SET n +=
  {age: 42} RETURN n`. SetOpMerge counterpart. Golden: 1 column
  (node), statement "write".

**SET labels (§4.3.3):**

- `set_labels_declared.cypher` — `MATCH (n:Person) SET n:Draft
  RETURN n`. Discriminates: `Draft` is declared as a NodeType, so
  the per-label existence check succeeds. **Breakage**: a mutant
  that requires the resulting label union (`{Person, Draft}`) to
  match a declared NodeType would reject (no such combined
  NodeType). Golden: 1 column (node), statement "write".

**REMOVE property (§4.3.4):**

- `remove_property.cypher` — `MATCH (n:Person) REMOVE n.age
  RETURN n`. Baseline REMOVE property; `Person.age` exists.
  Golden: 1 column (node), statement "write".

**REMOVE labels (§4.3.5):**

- `remove_labels_declared.cypher` — `MATCH (n:Person) REMOVE
  n:Draft RETURN n`. `Draft` is declared. Golden: 1 column (node),
  statement "write".

**DELETE (§4.4):**

- `delete_bare_node.cypher` — `MATCH (n:Person) DELETE n`.
  Projection-less. **Breakage**: under a mutant that emits `null`
  columns, golden diff. Golden: 0 columns, statement "write".
- `delete_detach.cypher` — `MATCH (n:Person) DETACH DELETE n`.
  Same shape with `Detach()` on the query side, invisible on the
  wire. Golden: 0 columns, statement "write".
- `delete_bare_property.cypher` — `MATCH (n:Person) DELETE n.name`.
  Discriminates: bare-property DELETE with a declared property.
  **Breakage**: under a mutant that treats `n.name` as an unknown
  property, `ErrUnknownProperty` fires spuriously. Golden: 0
  columns, statement "write".
- `delete_rich_expression_with_param.cypher` — `MATCH (n)
  DELETE nodes($p)`. Discriminates: rich-expression DELETE with
  `ExprInDeleteTarget` parameter Use routes per §4.5 to
  `ResolvedUnknown{}`. Under a mutant that keeps R5's reject arm,
  the query fails. Golden: 0 columns, parameter `$p →
  ResolvedUnknown{}`, statement "write". Schema: this fixture
  uses a two-NodeType schema so `MATCH (n)` (unlabelled)
  ambiguates cleanly — pair with a single-NodeType schema
  variant if needed. Verification note: parser_test.go line 1929
  pins that unlabelled `(n)` in this shape parses as
  `NodeBinding{"n", nil}`, and Phase B against a single-NodeType
  schema commits it; the R6 fixture uses a single-NodeType
  schema slice (`social_r6_solo.gql` — see fixture list).

**Actually — schema for `delete_rich_expression_with_param.cypher`.**
`MATCH (n)` on the full `social_r6.gql` schema has five NodeTypes;
Phase B fails with `ErrAmbiguousBinding`. Two options:
1. Use `MATCH (n:Person)` — makes the unlabelled-inference issue
   moot.
2. Add a solo-NodeType schema fixture.

The spec commits to option 1: rewrite the fixture as `MATCH
(n:Person) DELETE nodes($p)`. Parser test 1926-1936's shape uses
`MATCH (n)` (unlabelled) because the parser test targets the
parser's ExprInDeleteTarget wiring, not the resolver. The R6
resolver fixture uses `MATCH (n:Person)` for the same shape —
same parser output at the ExprUse position, different NodeBinding
labels. This keeps schema selection deterministic and avoids
adding a schema fixture just for one test.

**Projection-less write with WITH-carried projection (§4.6):**

- `create_then_wildcard_return.cypher` — `MATCH (a:Person)
  CREATE (b:Post) WITH * RETURN a, b`. Discriminates: the
  wildcard carries both the MATCH-bound `a` and the CREATE-
  introduced `b` forward through WITH *. Golden: 2 columns (a,
  b), statement "write".
- `set_property_with_star.cypher` — `MATCH (a:Person) SET
  a.age = 30 WITH * RETURN a`. Discriminates: SET's effect is
  validated at Phase E before the WITH * projection walk emits
  the wildcard column. Golden: 1 column (a), statement "write".
- `set_property_on_nullable.cypher` — `OPTIONAL MATCH
  (a:Person) SET a.age = 30 RETURN a`. Discriminates: R6 admits
  a SET on a nullable target (the NULL-row failure is bucket 3,
  §2.4). Golden: 1 column (a; nullable Person), statement
  "write".

**Effects across UNION (§2.4):**

- `union_all_writes_all_writes.cypher` — `CREATE (a:Person)
  UNION ALL CREATE (b:Post)`. Both branches projection-less
  (zero columns each) — column count agrees. Discriminates: two
  projection-less branches pass UNION column compat. Golden: 0
  columns, distinct false (UNION ALL), statement "write".

**R5 harness widening — `WITH *` post-write (interaction fixture):**

- `merge_then_return.cypher` — `MERGE (a:Person) RETURN a`.
  Discriminates: MERGE-introduced binding is projectable in the
  same Part. Golden: 1 column (a), statement "write".

**Fixture count**: 21 new R6 valid fixtures.

### 6.4 R6 invalid fixtures — updated `invalidFixtures` map

**Retiring at R6:**

- **`expr_use_set_value.cypher`** was R5-invalid (routed to
  `ErrOutOfR0Scope` per `resolve.go:709-710`). At R6, that reject
  arm retires (§4.5); the fixture's shape becomes valid. The
  invalid fixture is REMOVED from `invalidFixtures`; its
  replacement in `valid/` is `set_property_bare_param.cypher`
  (§6.3).

**Retaining at R6:**

- Every R5 invalid fixture whose target remains out of scope at
  R6: `unknown_label`, `unknown_property`, `unknown_edge`,
  `unknown_edge_property`, `ambiguous_unlabelled_binding`,
  `unlabelled_binding_no_edge`, `empty_inline_endpoint`,
  `parameter_type_conflict_*`, `unknown_property_via_expr_use`,
  `list_of_nodes_projection`, `list_of_edges_projection`,
  `ambiguous_edge_orientation`, `unknown_edge_undirected`,
  `unknown_edge_multi_type_all_miss`,
  `unknown_property_union_missing`,
  `unknown_property_union_type_differs`, `untyped_edge`,
  `var_length_edge_property_projection`,
  `union_column_count_mismatch`, `union_column_name_mismatch`,
  `union_column_type_mismatch`,
  `union_column_nullability_mismatch`,
  `union_unknown_label_branch`, `part_binding_type_conflict`,
  `part_binding_type_conflict_edge`.

**Adding at R6:**

- `create_unknown_label.cypher` — `CREATE (n:NotDeclared)` →
  `ErrUnknownLabel` (Phase A1 emission during
  `collectPattern`-driven Phase A1 arm; no Phase E call needed,
  the sentinel fires before Phase E runs). Discriminating: the
  parser accepts unknown labels (the label is a bare Label token
  in the grammar); the resolver's Phase A1 catches it.
- `create_unknown_edge.cypher` — `CREATE (a:Person)-[r:UNKNOWN]->
  (b:Post)` → `ErrUnknownEdge` (Phase A2). Direct analogue on the
  edge axis.
- `merge_endpoint_unknown_label.cypher` — `MERGE (n:NotDeclared)
  RETURN n` → `ErrUnknownLabel`. Same as CREATE analogue; the
  MERGE-pattern goes through `collectPattern`.
- `merge_unknown_edge.cypher` — `MERGE (a:Person)-[r:AUTHORED]->
  (t:Tag) RETURN t` → `ErrUnknownEdge`. `(Person)-[:AUTHORED]->
  (Tag)` is not declared (the R6 schema declares only
  `(Person)-[:AUTHORED]->(Post)` and `(Post)-[:AUTHORED]->
  (Person)`), so the EdgeKey is unknown. Both endpoint labels are
  declared, isolating the failure to the edge triple.
- `merge_on_match_unknown_property.cypher` — `MERGE (a:Person)
  ON MATCH SET a.notAProp = 30 RETURN a` →
  `ErrUnknownProperty`. Discriminating: `validateMergeEffect`
  routes to `validateSetPropertyEffect`, which fires the property
  lookup.
- `set_property_unknown_property.cypher` — `MATCH (n:Person)
  SET n.notAProp = 1 RETURN n` → `ErrUnknownProperty`.
  Discriminating: SetPropertyEffect target property fails schema
  lookup.
- `set_property_on_projection_alias.cypher` — `MATCH (n:Person)
  WITH count(n) AS c SET c.p = 1` → `ErrInvalidEffectTarget`.
  Discriminating: `c` is in `carriedResolvedTypes` (from `WITH
  count(n) AS c`), not in any binding table; the SET fails at
  step 5 of `validateSetPropertyEffect`.
  - Parser verification note: `MATCH (n:Person) WITH count(n)
    AS c SET c.p = 1` — the parser accepts this? The parser's
    scope check (build.go:155-158) rejects unbound refs; `c` is
    an in-scope alias. `SET c.p = 1` mines `c` as the target
    variable; `c.p` as the target Ref. The parser's shape.go
    accepts a bare-var target's atom without asking whether it
    is a node/edge binding, so the shape reaches the resolver.
    Confirmed: parser admits, resolver rejects. Fixture
    verified.
- `set_property_on_var_length_edge.cypher` — `MATCH (a:Person)
  -[r:KNOWS*1..3]->(b:Person) SET r.since = date('2020-01-01')
  RETURN a` → `ErrInvalidEffectTarget`. Discriminating: SET
  target is a var-length edge binding; step 3 of
  `validateSetPropertyEffect` rejects.
- `set_entity_on_projection_alias.cypher` — `MATCH (n:Person)
  WITH count(n) AS c SET c = {}` → `ErrInvalidEffectTarget`.
  SetEntityEffect counterpart to
  `set_property_on_projection_alias`.
- `set_labels_undeclared.cypher` — `MATCH (n:Person) SET
  n:Nonexistent RETURN n` → `ErrUnknownLabel`. Discriminating:
  `Nonexistent` is not declared as any NodeType's label.
- `set_labels_on_edge.cypher` — `MATCH (a:Person)-[e:KNOWS]->
  (b:Person) SET e:Foo RETURN e` → `ErrInvalidEffectTarget`.
  Discriminating: labels are node-only; the edge target rejects.
- `remove_property_unknown.cypher` — `MATCH (n:Person) REMOVE
  n.notAProp RETURN n` → `ErrUnknownProperty`.
- `remove_property_on_projection_alias.cypher` — `MATCH (n:Person)
  WITH count(n) AS c REMOVE c.p` → `ErrInvalidEffectTarget`.
- `remove_labels_undeclared.cypher` — `MATCH (n:Person) REMOVE
  n:Nonexistent RETURN n` → `ErrUnknownLabel`.
- `delete_projection_alias.cypher` — `MATCH (n:Person) WITH
  count(n) AS c DELETE c` → `ErrInvalidEffectTarget`.
- `delete_bare_property_unknown.cypher` — `MATCH (n:Person)
  DELETE n.notAProp` → `ErrUnknownProperty`.
- `union_writes_vs_returns_column_count.cypher` — `CREATE
  (a:Person) UNION MATCH (b:Post) RETURN b` →
  `ErrUnionColumnMismatch`. Discriminating: branch 0 is
  projection-less (0 columns); branch 1 has 1 column. R5's
  `compareBranchColumns` fires on count. This fixture asserts
  the compile-time detection of openCypher's runtime
  `InvalidClauseComposition` (§2.4).

**Fixture count**: 16 new R6 invalid fixtures; 1 retirement.

**Updated `invalidFixtures` map** (delta from R5's 29 entries; R6
lands 44 entries):

```go
// R6 additions:
"create_unknown_label.cypher":                  ErrUnknownLabel,
"create_unknown_edge.cypher":                   ErrUnknownEdge,
"merge_endpoint_unknown_label.cypher":          ErrUnknownLabel,
"merge_unknown_edge.cypher":                    ErrUnknownEdge,
"merge_on_match_unknown_property.cypher":       ErrUnknownProperty,
"set_property_unknown_property.cypher":         ErrUnknownProperty,
"set_property_on_projection_alias.cypher":      ErrInvalidEffectTarget,
"set_property_on_var_length_edge.cypher":       ErrInvalidEffectTarget,
"set_entity_on_projection_alias.cypher":        ErrInvalidEffectTarget,
"set_labels_undeclared.cypher":                 ErrUnknownLabel,
"set_labels_on_edge.cypher":                    ErrInvalidEffectTarget,
"remove_property_unknown.cypher":               ErrUnknownProperty,
"remove_property_on_projection_alias.cypher":   ErrInvalidEffectTarget,
"remove_labels_undeclared.cypher":              ErrUnknownLabel,
"delete_projection_alias.cypher":               ErrInvalidEffectTarget,
"delete_bare_property_unknown.cypher":          ErrUnknownProperty,
"union_writes_vs_returns_column_count.cypher":  ErrUnionColumnMismatch,
// R5 retirement (dropped from map, source removed from disk):
// "expr_use_set_value.cypher": ErrOutOfR0Scope,  // RETIRED at R6.
```

### 6.5 Determinism check — R6 additions

The R0–R5 determinism sweep runs unchanged. R6 adds no
non-deterministic surface: `validateEffects` iterates
`part.Effects` in slice order (parser walk order); the SET/REMOVE
label existence check iterates `s.Nodes` — a Go map iteration —
but only OR-combines the boolean result, so map-iteration order
does not affect the outcome. **Belt-and-braces determinism
audit**: run `just test -race -count=5` before publishing the R6
code cycle.

### 6.6 Non-obvious harness invariants — R6 additions

Two invariants worth capturing:

- **The retired R5 fixture `expr_use_set_value.cypher` MUST be
  removed from `invalid/` on disk.** The `TestInvalid` totality
  check (`resolver_test.go:143-144`) asserts every disk file has
  a matching `invalidFixtures` entry AND every entry has a disk
  file. Leaving the R5 fixture on disk without a map entry fails
  the test. The R6 code cycle deletes the file at commit time.
- **The R0–R5 byte-identical claim (§3.4) is a test.** The R6
  code cycle runs `just test` WITHOUT `-update` before writing
  its own goldens. Any pre-existing golden that regenerates
  differently indicates the code change touched an R0–R5 path.
  This is a strong regression fence.

---

## 7. R6 capability scope — what resolves

**In scope:** a single Cypher query the parser accepts, whose parsed
`query.Query` shape is:

- The R5 in-scope predicate, PLUS
- `Part.Effects` is any (possibly empty) slice of
  `CreateEffect` / `DeleteEffect` / `SetPropertyEffect` /
  `SetEntityEffect` / `SetLabelsEffect` / `RemovePropertyEffect` /
  `RemoveLabelsEffect` / `MergeEffect`.
- Each Part may be projection-less (`Effects` non-empty, `Returns`
  empty, `ReturnsAll` false) or projected (as R5). A projection-less
  Part contributes zero columns to `ValidatedQuery.Columns` when
  it is branch 0's final Part.
- `StatementKind` is either `StatementRead` (R5) or `StatementWrite`
  (any Part carries an Effect anywhere across branches, per Stage
  12 §3.1's parser rule).

**Out of scope, routed to the appropriate sentinel:**

R5's out-of-scope table survives with revisions:

| Construct | Sentinel | R-stage owner |
|---|---|---|
| Untyped edge (`len(Labels()) == 0`) — read or write | `ErrOutOfR0Scope` | R-later |
| Path binding | `ErrOutOfR0Scope` | R-later |
| Unwind binding | `ErrOutOfR0Scope` | R-later |
| Call binding | `ErrOutOfR0Scope` | R7 |
| `ExprProjection` typed `TypeList{TypeNode\|TypeEdge}` | `ErrOutOfR0Scope` | R-later |
| Property projection on a variable-length edge binding | `ErrOutOfR0Scope` | R-later |
| CALL / YIELD | `ErrOutOfR0Scope` | R7 |
| Every candidate `(label, orientation)` misses the schema | `ErrUnknownEdge` | (unchanged) |
| Single-type undirected edge whose two orientations both match | `ErrAmbiguousEdgeOrientation` | (unchanged) |
| Property lookup on a multi-candidate edge; property missing on some union member | `ErrUnknownProperty` | (unchanged) |
| Property lookup on a multi-candidate edge; property type/nullability differs across union members | `ErrUnknownProperty` | (unchanged) |
| Labelled node with no matching schema NodeType — read or write | `ErrUnknownLabel` | (unchanged; R6 widens fail-sites) |
| Unlabelled node with an empty candidate set from R3-widened touching edges | `ErrUnknownLabel` | (unchanged) |
| Unlabelled node with a multi-candidate set that survives Phase B fixed-point | `ErrAmbiguousBinding` | (unchanged) |
| Parameter Uses that do not unify | `ErrParameterTypeConflict` | (unchanged) |
| UNION branches disagree on column count, names, types, or nullability | `ErrUnionColumnMismatch` | (unchanged; R6 exercises the mixed writes/reads column-count failure) |
| Part K > 0 re-declares a carried variable with a conflicting labelled binding | `ErrPartBindingTypeConflict` | (unchanged) |
| SET / REMOVE property on schema-unknown property | `ErrUnknownProperty` | **R6 (this stage)** |
| SET / REMOVE labels with schema-undeclared label | `ErrUnknownLabel` | **R6 (this stage)** |
| CREATE / MERGE with schema-unknown label | `ErrUnknownLabel` | **R6 (this stage)** |
| CREATE / MERGE with schema-unknown edge triple | `ErrUnknownEdge` | **R6 (this stage)** |
| DELETE bare-prop on schema-unknown property | `ErrUnknownProperty` | **R6 (this stage)** |
| SET / REMOVE / DELETE target resolves to a projection alias (non-entity binding) | `ErrInvalidEffectTarget` | **R6 (this stage)** |
| SET / REMOVE labels target is an edge binding | `ErrInvalidEffectTarget` | **R6 (this stage)** |
| SET / REMOVE property target is a var-length edge binding | `ErrInvalidEffectTarget` | **R6 (this stage)** |
| Value-target type-agreement in `SET n.p = value` (bit-width assignability, nullability lift) | silently admitted (§4.3.1 judgment call) | R-later (design axis for codegen boundary) |
| Runtime SET / DELETE on NULL target (row-drop semantics) | silently admitted | bucket 3 (ADR 0007 §III) |
| DETACH DELETE cascade semantics vs plain DELETE | silently admitted | bucket 3 |
| Same-Part regime (b) nullability under-demote | ~~silently under-demoted~~ **closed** (5xg, 2026-07-10) | ~~gqlc-5xg (Class B, model change — unchanged from R5)~~ **closed** by 5xg change + widening (`docs/specs/model-change-5xg-required-bare-ref.md`); edge-side non-bare missing-witness residual filed as gqlc-0kq |
| OPTIONAL-clause-sibling nullability under-demote | ~~silently under-demoted~~ **closed** (ay9, 2026-07-10) | ~~gqlc-ay9 (Class A, model change — unchanged from R5)~~ **closed** by ay9 change + widening (`docs/specs/model-change-ay9-optional-group.md`); residual cross-Part carry gap filed as gqlc-984 |
| `ExprProjection` residual grouping-key discrimination | silently under-grouped | gqlc-hk0 / Shape B follow-up (unchanged from R5) |
| Cross-Part parameter Use attribution gap | silently false-admitted | gqlc-fvo (unchanged from R5) |

**Silently accepted (not routed anywhere):**

R0/R1/R2/R3/R4/R5's silently-accepted set stands unchanged. R6
adds:
- Value-target type-agreement (§4.3.1 judgment call — R6 does not
  enforce bit-width assignability; the runtime handles range
  checks).
- MERGE ON-branch execution ordering (both branches recorded on
  the wire; runtime picks at execution).
- Post-CREATE / post-MERGE binding visibility in the same Part
  (parser adds new bindings to `Part.Bindings` verbatim; the R5
  projection walk sees them as R5 does).
- Detach-vs-non-detach DELETE semantic distinction.

**Recorded ADR 0009 cross-check.** ADR 0009 R6: "Effects
validation against the schema: SET/REMOVE property + label
existence, CREATE/MERGE shape (labels form valid keys, endpoints
resolvable), DELETE targets. Projection-less writes resolve to
zero-column ValidatedQuery." R6 as this spec scopes it:

- **SET/REMOVE property + label existence**: §4.3.1
  (`validateSetPropertyEffect` — target property existence via
  schema NodeType/EdgeType Properties lookup), §4.3.3
  (`validateSetLabelsEffect` — label existence via schema NodeType
  iteration), and the REMOVE counterparts §4.3.4-§4.3.5.
- **CREATE/MERGE shape (labels form valid keys, endpoints
  resolvable)**: §4.2 — the CREATE/MERGE pattern's NodeBindings
  and EdgeBindings enter `Part.Bindings` via `collectPattern`;
  Phase A1/A2's existing R0–R3 kernel arms fire `ErrUnknownLabel`
  and `ErrUnknownEdge` on any schema mismatch, so §4.2's Phase E
  code has almost nothing to add.
- **DELETE targets**: §4.4 — bare-var and bare-prop targets go
  through `validateDeleteEffect` step 1, rich shapes through
  step 2. Property existence check for bare-prop; entity-kind
  check for bare-var (via binding-table presence).
- **Projection-less writes resolve to zero-column
  ValidatedQuery**: §3.1 and §4.1.2 — a projection-less branch-0
  final Part emits `[]Column{}`, marshalled as `"columns": []`.

### 7.1 Under-approximation and R6-specific design deferrals

Following the R4 §7.5 / R5 §7.1 template (the same section renumbered
across specs) — R6 discovers two design axes worth naming honestly:

#### 7.1.1 Value-target type-agreement in SET

**openCypher semantics.** `SET n.p = value` writes `value` into
`n.p`. The runtime checks assignability at write time —
`SET person.age = 'thirty-five'` fails because a STRING cannot be
assigned to an INT column. openCypher recognises implicit
widening in narrow cases (INTEGER → FLOAT). At compile time,
openCypher does not enforce assignability; runtime does.

**The model posture.** `SetPropertyEffect.ValueType()`
records the parser's Stage-6 rich-typer output — `TypeString`,
`TypeInt`, `TypeFloat`, `TypeMap`, `TypeUnknown` — a coarse type
that does not carry a bit-width family. The schema's target
property carries a `graph.PropertyType` — the bit-width families
of ADR 0002 (`INT`, `INT32`, `INT64`, `FLOAT32`, etc.). A
type-agreement check would map `TypeInt → any INT family; TypeFloat
→ any FLOAT family; TypeString → STRING; TypeMap → (skip); TypeUnknown
→ (skip)`. This is the same mapping R2's parameter unifier uses
for property witnesses (R2 spec §3.1).

**What R6-as-scoped loses.** A `SET n.age = 'x'` where
`n:Person.age :: INT NOT NULL` admits at R6 and fails at runtime
with a type error. openCypher's compile-time posture matches R6
(no check), so R6 loses nothing versus the language. A narrower
check ("this write cannot possibly succeed at runtime, so reject
now") would be a strict improvement — but design axes deferred
to codegen is exactly where the boundary between resolver and
codegen lands (ADR 0009 §Test strategy: "codegen consumes the
resolver's output — resolved result columns, resolved parameters,
and the entity/transaction facts codegen needs"). The
assignability decision — including implicit widening — is
codegen's concern, and codegen has the resolved property type
(from the `query.Query` side) to run the check on if it chooses.

**Not a model gap.** `SetPropertyEffect.ValueType()` and
the schema's `Property.Type` are both on `query.Query`; no
model change is needed to add the check. R6 defers because the
check's placement (resolver vs codegen) is a design axis the R6
brief does not decide, not because the information is missing.

**Recommendation.** R6 proceeds without value-target
assignability. If a future R-later or codegen stage needs the
check, it is added at that stage without a resolver-side spec
revision.

**Not filing a bead.** Deferred design axes are not follow-ups;
they are open decisions surfaced in the spec for future spec
cycles to close. No bead.

#### 7.1.2 Effects on the wire vs on `query.Query`

**Related open decision:** whether a future codegen stage needs a
resolved-type analogue of Effects on `ValidatedQuery`, or whether
it consumes Effects directly from `query.Query`. §3.5 records
the R6 posture (Effects stay on `query.Query`; no wire delta at
R6). If codegen chooses to consume a resolved-form Effect (e.g., a
`ResolvedSetPropertyEffect{TargetVariable string, TargetProperty
string, TargetPropertyType graph.PropertyType, ValueType Type,
Refs []query.Ref}`), the shape lands under a future ADR (the ADR
0008 analogue) alongside the codegen consumer, per ADR 0009's
provisional-through-R7 discipline. R6 does not pre-commit.

#### 7.1.3 Model-change status

R6 discovers no new model gap. Effects and their
components (`Ref`, `Type`, `SetOp`, `LabelSet`) are all on the
surface at the resolution needed. Every R6 fail-site is a
schema mismatch (an existing sentinel widens) or a shape-only
issue (`ErrInvalidEffectTarget` covers it). No change
recommendation.

The R5-discovered gaps (gqlc-hk0 `ExprProjection` residual
discrimination; gqlc-fvo Use→Part attribution) persist at R6
unchanged, as the R5 spec §7.1 recorded.

#### 7.1.4 Summary of R6 deferrals

- **Value-target assignability in SET** — deferred to R-later or
  codegen. Not a model gap; a design axis on the resolver/
  codegen boundary. No follow-up bead. §7.1.1.
- **Effects on `ValidatedQuery`** — deferred to a future codegen-
  driven ADR. §7.1.2.
- **R5 open gaps carry unchanged** — ~~gqlc-ay9, gqlc-5xg,~~ gqlc-hk0,
  gqlc-fvo. §7.1.3. [closed 2026-07-10: gqlc-ay9 (Class A) and
  gqlc-5xg (Class B) both landed as additive `Binding` axes; R6
  inherits both closures unchanged. Residuals: gqlc-984 (ay9) and
  gqlc-0kq (5xg).]

---

## 8. Ground-truth cross-check

Every factual claim in this spec is verifiable against source; the
citations below name the file:line the claim rests on.

- **`Query.Branches`, `Query.Combinators`, `Query.Parameters`,
  `Query.StatementKind`** — `internal/query/query.go:26-52` (Query
  struct). §2.4 iterates `Branches`.
- **`Branch.Parts`** — `internal/query/query.go:59-63`.
- **`Part.Bindings`, `Part.Returns`, `Part.ReturnsAll`,
  `Part.Distinct`, `Part.Effects`** —
  `internal/query/query.go:81-123`. §4.1 reads `Effects`
  (line 116-122 pins the field).
- **`NewPart` invariant** — `internal/query/query.go:150-159`;
  admits `effects` as one of the three ways a Part can be
  non-empty.
- **`Effect` sealed sum + variants** —
  `internal/query/query.go:1631-1660` (Effect interface with
  `isEffect()` marker), plus:
  - `CreateEffect` — `internal/query/query.go:1663-1704`.
  - `DeleteEffect` — `internal/query/query.go:1706-1765`.
  - `SetOp` — `internal/query/query.go:1767-1795`.
  - `SetPropertyEffect` — `internal/query/query.go:1797-1856`.
  - `SetEntityEffect` — `internal/query/query.go:1858-1912`.
  - `SetLabelsEffect` — `internal/query/query.go:1914-1952`.
  - `RemovePropertyEffect` —
    `internal/query/query.go:1954-1986`.
  - `RemoveLabelsEffect` — `internal/query/query.go:1988-2022`.
  - `MergeEffect` — `internal/query/query.go:2024-2116`.
- **`SetEffect` sealed sub-sum** — `internal/query/query.go:
  1651-1660` (interface with `isEffect()` + `isSetEffect()` markers).
  `SetPropertyEffect.isSetEffect()` at `:1842`;
  `SetEntityEffect.isSetEffect()` at `:1900`;
  `SetLabelsEffect.isSetEffect()` at `:1942`.
- **`MergeEffect.OnMatch()` / `MergeEffect.OnCreate()` return
  `[]SetEffect`** — `internal/query/query.go:2093-2099` (the
  accessors return `[]SetEffect`, not `[]Effect`). Type-level
  guarantee only Set-family effects can appear inside.
- **The R5 kernel's effect-admission gate** —
  `internal/resolver/resolve.go:153-155`:
  ```
  if len(part.Effects) != 0 {
      return nil, branchState{}, nil, fmt.Errorf("%w: write clause", ErrOutOfR0Scope)
  }
  ```
  §4.1 DROPS this gate at R6.
- **The R5 `ExprInSetValue` / `ExprInDeleteTarget` reject arms** —
  `internal/resolver/resolve.go:709-712`:
  ```
  case query.ExprInSetValue:
      return nil, fmt.Errorf("%w: parameter used in SET value", ErrOutOfR0Scope)
  case query.ExprInDeleteTarget:
      return nil, fmt.Errorf("%w: parameter used in DELETE target", ErrOutOfR0Scope)
  ```
  §4.5 REPLACES these with a `resolveType(uu.EnclosingType())`
  pass-through.
- **`ExprPosition` enum** —
  `internal/query/query.go:1345-1391`. Values `ExprInProjection`
  (`iota`), `ExprInPredicate`, `ExprInSetValue`,
  `ExprInDeleteTarget`.
- **`ExprUse` struct with `EnclosingType()` accessor** —
  `internal/query/query.go:1394-1421`. §4.5 reads.
- **Parser emits `ExprUse{TypeUnknown, ExprInDeleteTarget}` for
  DELETE-target params** — `internal/query/cypher/listener.go:
  499` (`l.addParameterUse(name, p, query.NewExprUse(query.
  TypeUnknown{}, query.ExprInDeleteTarget))`).
- **Parser emits `ExprUse{valueType, ExprInSetValue}` for SET-
  value params** — `internal/query/cypher/expr.go:677` and `:707`.
- **Parser test pin: `MATCH (n) DELETE nodes($p)` emits
  `DeleteEffect(nil, nil, false)` with `$p → ExprUse{TypeUnknown,
  ExprInDeleteTarget}`** —
  `internal/query/cypher/parser_test.go:1926-1936`.
- **Parser test pin: `SET n.age = $newAge` emits
  `SetPropertyEffect(Ref{n, age}, TypeUnknown, nil)` with `$newAge
  → ExprUse{TypeUnknown, ExprInSetValue}`** —
  `internal/query/cypher/parser_test.go:1897-1910`.
- **Parser test pin: `MERGE (a:Person) ON CREATE SET b.created =
  1 …` emits `MergeEffect{Variables, nil OnMatch, [{SetProperty
  Effect}] OnCreate}`** —
  `internal/query/cypher/parser_test.go:2018-2023`.
- **CREATE reuses `collectPattern` — CREATE bindings enter
  `Part.Bindings`** — `internal/query/cypher/listener.go` (search
  for `EnterOC_Create`); Stage 12 §1.3.
- **MERGE reuses `collectPattern` — MERGE bindings enter
  `Part.Bindings`** — Stage 13 §1.1.
- **`Part.Effects` populated in walk order** — Stage 12 §1.2.
- **StatementKind flips to `StatementWrite` iff any outer-scope
  write clause fires** — Stage 12 §3.1.
- **openCypher rejects mixed writes/reads UNION as
  `InvalidClauseComposition` at runtime; parser accepts** —
  Stage 12 §1.1 lines 65-68.
- **`SetPropertyEffect.Target()` returns a `Ref{Variable, Property}`
  with both non-empty** — `internal/query/query.go:1815-1818`
  (constructor rejects empty target variable) plus Stage 12 §1.5
  (parser only emits with non-empty property; multi-level LHS
  rejects at parse with `ErrNestedPropertyTarget`).
- **`SetEntityEffect.TargetVariable()` non-empty** —
  `internal/query/query.go:1872-1875`.
- **`SetLabelsEffect.Labels()` non-empty** —
  `internal/query/query.go:1925-1932`.
- **`DeleteEffect.Targets()` are bare-shape Refs;
  `DeleteEffect.Refs()` are rich-shape refs** — Stage 12 §1.4
  field contract lines 213-224.
- **`DeleteEffect.Detach()` is a runtime-only distinction** —
  Stage 12 §1.4 lines 226-231.
- **`RemovePropertyEffect.Target()` returns a `Ref{Variable,
  Property}` with both non-empty** —
  `internal/query/query.go:1963-1969` (constructor rejects empty).
- **`RemoveLabelsEffect.Labels()` non-empty** —
  `internal/query/query.go:1996-2003`.
- **`schema.Schema` fields: `Nodes map[graph.LabelSetKey]NodeType`,
  `Edges map[EdgeKey]EdgeType`** —
  `internal/schema/schema.go:12-16`.
- **`schema.NodeType` fields: `Labels graph.LabelSetKey`, `Name`,
  `Properties map[string]Property`** —
  `internal/schema/schema.go:20-24`.
- **`schema.EdgeType` fields: `EdgeKey` (embedded), `Name`,
  `Properties map[string]Property`** —
  `internal/schema/schema.go:28-32`.
- **`schema.Property` fields: `Name`, `Type graph.PropertyType`,
  `Nullable bool`** — `internal/schema/schema.go:43-47`.
- **`R5 witnessAcrossScopes` structure** —
  `internal/resolver/resolve.go:673-719`. §4.5 revises lines
  709-712.
- **`R5 resolveType` for a `TypeUnknown` returns
  `ResolvedUnknown{}`** — `internal/resolver/resolve.go:1147-1148`.
- **`R5 unify` widens `ResolvedUnknown{}` on either side** —
  `internal/resolver/resolve.go:1256-1262`.
- **`R5 unifyParameterUsesAcrossScopes` unifies witnesses via the
  R2 lattice** — `internal/resolver/resolve.go:611-653`.
- **`R5 materialiseReturns` returns `part.Returns` unchanged when
  `!part.ReturnsAll`** —
  `internal/resolver/resolve.go:324-341`.
- **`R5 projectionType` dispatcher** —
  `internal/resolver/resolve.go:1009-1024`.
- **`R5 refProjectionType` — carried-alias bypass at line
  1044-1050** — `internal/resolver/resolve.go:1030-1083`.
- **`R5 exportScope` — the WITH-carry construction** —
  `internal/resolver/resolve.go:408-479`.
- **`R5 unionProperty` — multi-candidate edge property helper** —
  `internal/resolver/resolve.go:1085-1104`.
- **`R5 compareBranchColumns` — UNION column compat check** —
  `internal/resolver/resolve.go:526-546`. §2.4 relies on this to
  detect column-count mismatch on writes vs reads UNION.
- **`R5 resolveBranch` signature** —
  `internal/resolver/resolve.go:109-142`.
- **`R5 resolvePart` signature** —
  `internal/resolver/resolve.go:152-318`.
- **`R5 branchState` shape** —
  `internal/resolver/resolve.go:82-91`.
- **`R5 allSentinels` list** —
  `internal/resolver/errors.go:82-92`.
- **R5 `ValidatedQuery` top-level shape** —
  `internal/resolver/validated.go:16-21`. §3.1-§3.4 read.
- **R5 `Column` shape with `GroupingKey`** —
  `internal/resolver/validated.go:29-33`.
- **R5 `ResolvedType` sum interface + variants** —
  `internal/resolver/validated.go:79-362`.
- **R5 `StatementKind` enum with `String()` = "read" / "write"** —
  `internal/resolver/validated.go:48-71`.
- **Parser referential-integrity sweep rejects unbound refs at
  parse time** — `internal/query/cypher/build.go:155-158` (`if
  !scope[ref.name] { ErrUnboundVariable }`); Stage 4 §4. §4.3.1
  step 5 defensive tripwire relies on this.
- **ADR 0008 §Additions since Stage 14** —
  `docs/adr/0008-query-model-surface-resolver-api.md:143-169`.
  §3.5, §7.1.2 read.
- **ADR 0008 pinned resolver API** —
  `docs/adr/0008-query-model-surface-resolver-api.md:110-142`.
  §1 and §2.1 respect.
- **ADR 0009 R6 line** —
  `docs/adr/0009-resolver-test-first-staged-build.md:132-136`.
- **ADR 0009 Test strategy — resolver output for codegen** —
  `docs/adr/0009-resolver-test-first-staged-build.md:59-97`.
  §3.5 §7.1.1 read.
- **ADR 0005 type-interface boundary — value structure below the
  boundary** — `docs/adr/0005-generated-code-executes-original-
  query-text.md`. §2.4, §4.3.1, §4.4 read.
- **ADR 0007 §III bucket 3 — runtime semantics** —
  `docs/adr/0007-parser-scope-full-opencypher-surface.md`.
  §2.4, §4.4 read.
- **R5 spec §4.2 — carried scope construction** —
  `docs/specs/resolver-stage-r5.md:691-945`. §2.4 relies on the
  carry-seed step for cross-Part effect target resolution.
- **R5 spec §4.3 — UNION column compatibility** —
  `docs/specs/resolver-stage-r5.md:946-1007`. §2.4 relies on
  `compareBranchColumns` catching mixed writes/reads column-count.
- **R5 spec §4.5 — AggregateProjection handler and
  ExprProjection residual posture** —
  `docs/specs/resolver-stage-r5.md:1117-1559`. §3.3 preserves
  `GroupingKey` semantics for projection-less Parts (short-circuit
  on empty column slice).
- **R5 spec §5.1 — sentinel `ErrUnionColumnMismatch`** —
  `docs/specs/resolver-stage-r5.md:1799-1823`. §2.4 relies on the
  sentinel firing on column-count mismatch.
- **R5 spec §7.1 template (under-approximation walk applied to
  R5)** — `docs/specs/resolver-stage-r5.md:2380-2523`. §7.1 follows
  the template shape.
- **Stage 12 spec §1.1-§1.7 — write clause definitions** —
  `docs/specs/cypher-query-parser-stage-12.md:47-333`. §4.2, §4.3,
  §4.4 read.
- **Stage 12 spec §1.4 field contract on `DeleteEffect{Targets,
  Refs}`** —
  `docs/specs/cypher-query-parser-stage-12.md:213-231`. §4.4 relies
  on Targets holding bare shapes and Refs holding rich shapes.
- **Stage 13 spec §1.1 — MergeEffect + SetEffect sub-sum
  rationale** —
  `docs/specs/cypher-query-parser-stage-13.md:47-93`. §4.2.2 reads.
- **R4 spec §7.5 template** —
  `docs/specs/resolver-stage-r4.md:1270-1542`. §7.1 follows the
  template.

---

## 9. Definition of done for R6 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is
out of scope of this document. The spec is done when:

1. This file exists at `docs/specs/resolver-stage-r6.md`, committed
   on branch `resolver-r6-spec`.
2. §3 records the (unchanged) `ValidatedQuery` shape, the
   projection-less-Part `Columns` population rule (§3.1), and the
   byte-identical claim over the 76 R0–R5 valid goldens (§3.4).
3. §4 gives the algorithm for R6: Phase E's `validateEffects`
   dispatch over the eight Effect variants (§4.1.1), the eight
   per-variant validators (§4.2, §4.3, §4.4), and the two retired
   `ExprUse` reject arms replaced by `resolveType(uu.EnclosingType
   ())` (§4.5).
4. §5 records the one new sentinel `ErrInvalidEffectTarget` and
   revises `ErrOutOfR0Scope`'s message-set list for the two
   retirements; the R5 sentinels' identities are preserved and
   fail-site widenings are enumerated.
5. §6 designs the fixture set: the R6 valid schema `social_r6.gql`
   (§6.2), the R6 valid fixture list (21 fixtures), the R6 invalid
   fixture list (16 additions + 1 retirement), the revised
   `invalidFixtures` map (§6.4).
6. §7 states the R6 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel or under-demote
   posture for each construct. §7.1 walks the honest state on the
   discovered design axes (value-target assignability, Effects on
   the wire), states no new model gap, and confirms
   the R5-inherited gaps carry unchanged.
7. §8 cross-checks every factual claim against source file:line.
8. `just test` is untouched-green — this cycle is docs-only.
9. **At R6 code-cycle close-out** (Cycle 2, not this Cycle 1):
   - No change bead files (R6 discovers no model gap).
   - No follow-up beads file for the deferred design axes
     (value-target assignability and Effects-on-wire are open
     decisions surfaced in the spec, not bug reports).
   - gqlc-0mx.8 closes (this stage's bead).
   - ~~gqlc-ay9, gqlc-5xg,~~ gqlc-hk0, gqlc-fvo remain OPEN unchanged;
     R6 does not close any of them. [closed 2026-07-10: gqlc-ay9
     landed via PRs #127/#128/#129
     (`docs/specs/model-change-ay9-optional-group.md`; residual gqlc-984),
     and gqlc-5xg landed via PRs #132/#133/#134
     (`docs/specs/model-change-5xg-required-bare-ref.md`; residual gqlc-0kq);
     R6 inherits both closures unchanged.]
   - The R6 code cycle asserts §3.4's byte-identical claim by
     running `just test` on the R0–R5 corpus WITHOUT `-update`
     before writing any R6 goldens; any regeneration in the R0–R5
     corpus fails the cycle.
