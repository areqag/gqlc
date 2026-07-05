# Stage R4 spec — resolver: nullability flow-typing (regime (a), edge-chain demotion)

The implementation brief for Stage R4 of `internal/resolver`, extending the
merged R0/R1/R2/R3 kernel (`docs/specs/resolver-stage-r0.md`,
`docs/specs/resolver-stage-r1.md`, `docs/specs/resolver-stage-r2.md`,
`docs/specs/resolver-stage-r3.md`) with the capability ADR 0009 assigns to
R4: **nullability flow-typing — relaxing the conservative
everything-OPTIONAL-introduced-is-nullable default by demoting a binding's
effective `Nullable` flag when the query's structure proves the binding must
exist on every surviving row**. Build this **test-first**. Scope, sequencing,
error posture, `ValidatedQuery`'s top-level shape, purity, and the golden-pair
harness inherit from ADR 0009 and R0–R3 unchanged; this document revises only
the rows, kernel arms, `ResolvedType` variants, and out-of-scope table entries
that R4 changes.

Stage R4 admits every query shape R3 admits (labelled single-`MATCH` patterns
with directed/undirected × single-hop/var-length × single-type/multi-type
edges, R2 projection/parameter shapes) **plus** every one of those shapes
introduced by `OPTIONAL MATCH`. Multi-part (WITH), UNION, `RETURN *`,
`RETURN DISTINCT`, `AggregateProjection`, writes, and CALL remain out of
scope and continue to route to `ErrOutOfR0Scope` (unchanged name; category-
grained per R0 §5).

R4 is a **non-breaking refinement by construction**: demotion only turns
`Nullable=true → Nullable=false` on the resolver's output, never the reverse
(§4.2 lattice invariant). The conservative default — every binding
first-introduced in `OPTIONAL MATCH` stays nullable unless a required
non-nullable edge witnesses its existence — is preserved for every binding
the algorithm cannot prove exists.

R4 admits **regime (a) only** (edge-chain demotion). Regime (b) —
bare-pattern demotion via a required clause reusing an OPTIONAL-introduced
binding — is deferred to R5 with an explicit citation: the parser merges a
same-part `MATCH (b)` occurrence into the existing `rawBinding` without
recording the second reference (`internal/query/cypher/pattern.go:373-401`;
Stage 4 spec §3 lines 171-187 states this limit honestly), so within a
single Part R4 has no witness of the required reuse. Stage 4 (WITH/UNION)
introduces the per-clause structure regime (b) needs; the resolver only
admits multi-part queries at R5, so R5 is the correct home for regime (b).
This scope decision is defended in §7.4.

---

## 1. Deliverables

- `internal/resolver/validated.go` — three targeted additions:
  - a `Nullable bool` field on `ResolvedNode` (§3.1);
  - a `Nullable bool` field on `ResolvedEdge` (§3.2);
  - a `Nullable bool` field on `ResolvedEdgeUnion` (§3.3).
  `ResolvedProperty`'s `Nullable` field's semantic widens (§3.4): it now
  carries the disjunction of the schema property's nullability and the
  bearing binding's effective nullability after R4 demotion. The four other
  variants (`ResolvedScalar`, `ResolvedTemporal`, `ResolvedList`,
  `ResolvedUnknown`) are unchanged. `ResolvedList{Element}` inherits the
  new axis transparently — a var-length `list<edge>` element carries the
  edge's `Nullable` bit (§3.5).
- `internal/resolver/resolve.go` — three targeted additions:
  - a per-binding **effective-nullability** table (`nullableBinding
    map[string]bool`, §4.3) computed once between Phase C and the
    projection walk;
  - the **demotion fixed-point** (§4.4) that seeds this table from the
    binding's `Nullable()` flag and iterates edge-chain demotion until
    stable;
  - projection / parameter reads that consult the table when emitting
    `ResolvedNode.Nullable`, `ResolvedEdge.Nullable`,
    `ResolvedEdgeUnion.Nullable`, `ResolvedProperty.Nullable`, and the
    list-of-edges element `Nullable` (§4.5–§4.7).
- `internal/resolver/errors.go` — **no change**. The R3 sentinel set is
  unchanged; R4 introduces zero new sentinels (§5).
- `test/data/resolver/valid/schemas/` — one new schema fixture
  (`social_r4.gql`, §6.2) that reuses R3's shapes with one added edge type
  needed for a Phase-B-across-OPTIONAL fixture. R0/R1/R2/R3 schemas are
  untouched.
- `test/data/resolver/valid/*.cypher` and `.validated.golden.json` — new R4
  valid fixtures (§6.3), each paired with its schema through the updated
  `schema.mapping.json`. **All R0–R3 valid fixture goldens rebaseline**
  under the new whole-entity `Nullable` field (§3.6).
- `test/data/resolver/invalid/*.cypher` — new R4 invalid fixtures for the
  R4-remainder `ErrOutOfR0Scope` sub-cases (§6.4). The R3 invalid set stays.
  The R3 fixture `with_clause.cypher` is unchanged; a new
  `optional_match_with_clause.cypher` records the R4-in-scope-of-R5
  boundary.
- `internal/resolver/resolver_test.go` — updated `invalidFixtures` map
  (§6.4). No structural changes to `TestValid` / `TestInvalid` /
  `TestSentinelReachability` are required.

Nothing downstream of the resolver is built — `ValidatedQuery` is
provisional through R7 (ADR 0009 §Decision); the added `Nullable` fields are
in-protocol additive changes to the provisional shape.

---

## 2. Architecture — deltas from R3

R0/R1/R2/R3's architecture stands (the `Resolver` struct, its compile-time
inputs, the `QueryResolver` interface + compile-time assertion, purity and
short-circuit posture, `resolve.go`/`Resolve` split, the three-phase kernel
A1 / A2 / B / C with R3's edgeCandidates + verdict). R4 inserts one new
sub-step between Phase C and the projection walk — the **demotion
fixed-point** — and threads the resulting per-binding effective-nullability
table through every read that emits a `Nullable` bit. No new exported types
beyond the widened `ResolvedNode`/`ResolvedEdge`/`ResolvedEdgeUnion` shapes
(no new variants — the sum's arity is unchanged). The seam does not move.

### 2.1 The R4 kernel structure

The kernel remains one linear pass with early returns. R4 keeps R3's step
ordering:

1. Query-level gating (unchanged — R0 §4.7 step 1 / R2 §4.1 / R3 §4.1).
2. Phase A1 — labelled node bindings (unchanged, R1 §4.2).
3. Phase A2 — all edge bindings admitted by the R3 predicate have their
   candidate set computed via §4.4 and closed via §4.6 when both endpoints
   are labelled; edges with a still-pending unlabelled `VarEndpoint` are
   deferred to Phase C (unchanged, R3 §4.3).
4. Phase B — unlabelled-node inference over the R3-widened touching set
   (unchanged, R3 §4.5).
5. Phase C — closure of deferred edges (unchanged, R3 §4.6).
6. **Phase D (new) — demotion fixed-point** (§4.4). Reads the committed
   binding tables and produces `nullableBinding map[string]bool`,
   iterating regime (a) to a fixed point.
7. Projection walk (revised — §4.5). The `RefProjection` handler now
   consults `nullableBinding` when emitting `ResolvedNode`, `ResolvedEdge`,
   `ResolvedEdgeUnion`, and their `ResolvedList{Element:…}` var-length
   wrappers; a property projection's emitted `Nullable` bit is
   `schema.Property.Nullable OR nullableBinding[ref.Variable]` (§4.6).
8. Parameter walk (revised — §4.7). Inline-map `PropertyUse` witnesses
   apply the same disjunction rule so parameter unification behaves
   consistently with projection.
9. StatementKind copy (unchanged, R0 §4.7 step 6).

Phase D runs after Phase C because the demotion algorithm reads committed
edge shapes (single-hop directed, undirected, var-length, multi-type) —
Phase C's verdict fixes those shapes, and the demotion rule §4.2 references
them by their committed `resolvedEdgeKey` / `resolvedEdgeCand` /
`edgeBindings` state.

### 2.2 Kernel helpers — one new; no revisions

One new helper in `resolve.go`:

- **`demoteNullable(bindings []query.Binding, edgeBindings
  map[string]query.EdgeBinding, edgeKeys map[string]schema.EdgeKey,
  edgeCands map[string][]schema.EdgeKey) map[string]bool`** (new).
  Produces the effective-nullability table per §4.4. Deterministic —
  iteration is over the parser's `Part.Bindings` in first-appearance order,
  and each pass either shrinks the pending demotion candidates or halts.

R3's existing helpers (`edgeCandidates`, `closeEdge`, `endpointLabels`,
`candidateTypes`, `touchingSide`, `intersect`, `refProjectionType`,
`useWitness`, `propertyUseWitness`, `unionProperty`, `resolveType`,
`unify`) are **behaviour-unchanged**. Two of them — `refProjectionType`
and `propertyUseWitness` — gain new arguments (the `nullableBinding` table)
so they can consult effective nullability when emitting the `Nullable`
bit on the returned `ResolvedType`. The change is a signature widening,
not an algorithmic revision: their existing dispatch (single-candidate vs
multi-candidate vs var-length) is preserved verbatim.

### 2.3 Purity, determinism, short-circuit — unchanged

R0 §2.3 stands verbatim. R4 introduces no goroutine, no map iteration
that escapes into the output, no time source. `demoteNullable` iterates
`Part.Bindings` in the parser's first-appearance order (guaranteed by
the parser's mergeBinding discipline in
`internal/query/cypher/pattern.go:373-401`) plus a small deterministic pass over
`edgeBindings` in the same order (edges are enumerated via
`part.Bindings`, not via `edgeBindings` map iteration). The fixed-point
termination argument is spelled out in §4.4.4.

Short-circuit is unaffected: the demotion algorithm never fails
(§4.2 lattice invariant). Any fail-site in R4 is an existing R0–R3
one — R4 adds no new sentinel.

---

## 3. `ValidatedQuery` — the R4 shape

`ValidatedQuery`'s top-level shape (R0 §3.1) is unchanged at R4:
`Columns`, `Parameters`, `Statement`. The R4 changes are on three
`ResolvedType` variants that carry a whole-entity witness — the axis is
strictly additive under the ADR 0009 provisional-through-R7 posture. No
new `ResolvedType` variant, no new discriminator tag, no rename.

### 3.1 `ResolvedNode` — gains `Nullable`

```go
// ResolvedNode is a whole-entity projection whose Ref names a node
// binding, keyed by the resolved node type's canonical label set. R4
// adds Nullable: the binding's effective nullability after R4 demotion
// — true iff the binding was first introduced in an OPTIONAL MATCH
// clause AND no non-nullable edge in the pattern proves its existence.
type ResolvedNode struct {
    Labels   graph.LabelSetKey `json:"labels"`
    Nullable bool              `json:"nullable"`
}
```

Wire encoding gains the always-emit `"nullable"` field — the same
`omitempty`-free discipline the parser applies to `NodeBinding` /
`EdgeBinding` (parser Stage 2 spec lines 134-143). The `"kind"`
discriminator stays `"node"`; existing tagged-union consumers dispatch
unchanged and see one new field.

**Rejected alternative: a new variant `ResolvedNullableNode`.** Doubles
the `ResolvedType` sum for zero information gain — the axis is
orthogonal to the label-set, and there is no case where the two axes
combine to produce more than {node × {nullable, not-nullable}} = one
struct with two fields. A new variant would force every consumer's
`case ResolvedNode:` to add a symmetric `case ResolvedNullableNode:`
that reads the same `Labels`.

**Rejected alternative: encode `Nullable` on the enclosing `Column`.**
The `Column` is the resolver's output *slot*; the resolved type is
what the resolver *typed*. Nullability of a node value is a property
of the type, not of the slot — moving it up would misplace it and
would not compose with `ResolvedList{Element: ResolvedNode}` (a list
of nodes, where every element carries its own nullability but the
list itself has its own nullability — §3.5).

### 3.2 `ResolvedEdge` — gains `Nullable`

```go
// ResolvedEdge is a whole-entity edge projection: the schema's
// canonical (source, label, target) triple plus (R4) the binding's
// effective nullability. R4 semantics identical to ResolvedNode: true
// iff the edge binding was first introduced in an OPTIONAL MATCH and
// R4 demotion could not prove it non-nullable.
type ResolvedEdge struct {
    EdgeKey  schema.EdgeKey `json:"edgeKey"`
    Nullable bool           `json:"nullable"`
}
```

Wire encoding gains `"nullable"`; `"kind"` stays `"edge"`. Var-length
edges wrap this element in `ResolvedList{Element: ResolvedEdge{…}}`;
§3.5 records how the `Nullable` axis flows through the wrap.

### 3.3 `ResolvedEdgeUnion` — gains `Nullable`

```go
// ResolvedEdgeUnion is a multi-candidate edge whole-entity projection
// (R3 §3.2) with (R4) the binding's effective nullability. Because
// every union member is projected from the same edge binding, they
// share one Nullable — the axis is on the binding, not the schema
// side.
type ResolvedEdgeUnion struct {
    EdgeKeys []schema.EdgeKey `json:"edgeKeys"`
    Nullable bool             `json:"nullable"`
}
```

Wire encoding gains `"nullable"`; `"kind"` stays `"edgeUnion"`; the
`EdgeKeys` slice ordering (R3 §4.4 canonical order) is unchanged.

### 3.4 `ResolvedProperty` — semantic widening of the `Nullable` field

Shape is unchanged (`Type graph.PropertyType`, `Nullable bool`), but
the `Nullable` field's meaning widens: it becomes the **disjunction**
of the schema property's declared nullability and the bearing
binding's effective nullability after R4 demotion.

```
Nullable(ResolvedProperty) = schema.Property.Nullable
                             OR
                             effectiveNullable(binding named by Ref.Variable)
```

Where `effectiveNullable(b)` is the binding's `Nullable()` flag (from
the parser) demoted by regime (a) in §4.4. For a required-MATCH
binding — the R0/R1/R2/R3 case — `effectiveNullable = false`, so the
disjunction collapses to `schema.Property.Nullable`. Every R0–R3
existing property golden encodes exactly `schema.Property.Nullable`,
so their goldens **do not rebaseline on this row** — they only
rebaseline on the whole-entity R4 additions in §3.1/§3.2/§3.3.

**Rationale — disjunction, not conjunction.** An OPTIONAL-introduced
node `p` may not exist on the row at all; a property projection
`p.name` where `name` is `NOT NULL` in the schema is still nullable
in the row (the whole node's absence propagates NULL to every
projection off it). Conjunction would only report null if both the
schema and the binding said so — which contradicts openCypher's
value semantics. Disjunction is the honest rule: null iff either the
binding may be absent OR the property may itself be NULL. Cypher's
row model composes them by OR.

**Rejected alternative: keep the R3 semantics, add a separate
`BindingNullable` axis on `ResolvedProperty`.** Two nullability bits
on one column would be an information leak the consumer must always
compose with `OR`; letting the resolver compose it once, at the
point of authority, is the ADR 0002 discipline for the schema side
carried over to R4.

### 3.5 `ResolvedList{Element}` — element-level `Nullable` flows through

`ResolvedList`'s shape is unchanged; its element carries the R4
addition. The two producing sites at R4:

- **Var-length whole-entity edge projection** (R3 §4.7): a var-length
  edge binding named `r` with a singleton candidate set emits
  `ResolvedList{Element: ResolvedEdge{EdgeKey: <k>, Nullable:
  <effectiveNullable[r]>}}`. Multi-candidate emits
  `ResolvedList{Element: ResolvedEdgeUnion{EdgeKeys: <cands>,
  Nullable: <effectiveNullable[r]>}}`.
- **Literal list of scalars** (R2 §3.3): unchanged — the list's
  element is a `ResolvedScalar` / `ResolvedTemporal` / `ResolvedUnknown`,
  none of which carry a `Nullable` axis (R2 §3.1 records the choice
  to omit nullable from `ResolvedScalar`; the same argument applies
  transitively here).

The `ResolvedList` outer type itself does **not** gain a
`Nullable` axis at R4. Rationale: a Cypher var-length edge with
`min=0` matches the zero-hop case (an empty list), so the list is
never NULL — it is either populated or empty. The empty-list case
does not correspond to "the list is null"; it corresponds to "the
list has zero elements". The distinction matters: an
OPTIONAL-introduced var-length edge, when it fails to match, would
produce NULL for the whole element in Cypher — but the resolver's
existing R3 convention is that a var-length edge binding, whether
OPTIONAL or not, projects a list value; codegen composes the NULL /
empty-list rule at runtime. Adding a `Nullable` axis to `ResolvedList`
would prematurely commit to a wire-shape decision codegen owns; R4
carries the axis on the *element* (`ResolvedEdge.Nullable`) instead,
which is where the schema witness lives.

**Rejected alternative: add `Nullable` to `ResolvedList`.** Would
duplicate the axis on the two places (`ResolvedList.Nullable` AND
`ResolvedList.Element.Nullable`) without a clear rule for when they
agree or disagree. R4 draws the boundary at "the element carries
the binding's nullable bit".

### 3.6 Wire-encoding invariants — the R0–R3 golden rebaseline

**Every existing R0–R3 golden that contains at least one
`ResolvedNode`, `ResolvedEdge`, or `ResolvedEdgeUnion` column
rebaselines** on the addition of the `"nullable": false` field.
This is a shape change to the provisional-through-R7
`ValidatedQuery` (ADR 0009) — the R4 code cycle regenerates them
with `-update` at the same commit that introduces the field. The
regenerated JSON differs from the R3 version only by the added
`"nullable": false` on the affected columns; no wire-tag renames,
no field reorderings, no discriminator changes.

The following R0–R3 goldens are affected (identified from the
current fixture set by grepping the golden JSON for any
`"kind": "node"`, `"kind": "edge"`, or `"kind": "edgeUnion"`):

- `edge_labelled_both_endpoints.cypher`
- `inline_endpoint_source.cypher`, `inline_endpoint_target.cypher`
- `multi_type_directed_union.cypher`, `multi_type_undirected.cypher`
- `node_whole_entity.cypher`, `node_mixed_projection.cypher`
- `self_loop_directed.cypher`, `two_edges_shared_binding.cypher`
- `undirected_single_match.cypher`,
  `undirected_single_match_reverse.cypher`,
  `undirected_var_length_multi_type_property.cypher`
- `unlabelled_binding_from_edge.cypher`,
  `unlabelled_binding_target_inferred.cypher`,
  `unlabelled_via_multi_type.cypher`, `unlabelled_via_undirected.cypher`
- `var_length_directed.cypher`, `var_length_multi_type.cypher`,
  `var_length_undirected_single_match.cypher`

Fixtures whose column-set contains only property projections,
scalars, temporals, unknowns, or lists thereof — including
`edge_property_projection.cypher`, `edge_property_parameter.cypher`,
`edge_property_union_agree.cypher`, `literal_int_*`,
`func_projection_*`, `expr_projection_*`, `parameter_*`,
`node_property_*.cypher` — **do not rebaseline** on this axis. Their
`ResolvedProperty.Nullable` value is unchanged (the bearing binding
is not OPTIONAL, so §3.4's disjunction collapses to
`schema.Property.Nullable`).

For a wire-shape-audit reviewer: the regeneration diff on the R4
commit should show exactly one added line (`"nullable": false,`) on
each affected column and nothing else.

---

## 4. The R4 kernel algorithm

Each step below extends or replaces a numbered step of R3 §4. Steps 1
(query-level gating) and 6 (StatementKind copy) are unchanged. The R3
edge-shape kernel (Phases A/B/C, `edgeCandidates`, `closeEdge`, the
verdict table, the projection-walk hops-axis dispatch) is unchanged in
behaviour — R4 only threads the new nullability axis through the
existing dispatch.

### 4.1 The R4 admissibility widening — OPTIONAL MATCH bindings

R3's kernel gate at `resolve.go:24-44` (one branch, one part, no writes,
no CALL, no `Distinct`, no `ReturnsAll`, non-empty bindings) is
**unchanged** at R4. The R3 §7 out-of-scope entry "Nullability upgrades
(OPTIONAL MATCH regimes) → `ErrOutOfR0Scope` → R4" **retires** at R4:
the R3 fail-site was documentary only — the R3 kernel has no code path
that inspects `binding.Nullable()`, so the actual behaviour today is
that an `OPTIONAL MATCH`-introduced binding parses fine and resolves as
if it were a required MATCH (with a resolver-side `Nullable=false`
whole-entity encoding that R4 fixes to be honest). R4 formalises this
by reading `Nullable()` (§4.4) and emitting it on the output.

The R3 kernel gate rejects multi-part queries (`resolve.go:28-30`,
"WITH / multi-part query"). R4 keeps this rejection: OPTIONAL MATCH in
a **single-part** query resolves; OPTIONAL MATCH in a multi-part query
still routes to `ErrOutOfR0Scope` (R5's business). This gate is what
makes regime (b) unreachable at R4 (§7.4).

The R3 kernel gate does **not** distinguish OPTIONAL MATCH from
required MATCH in Phase A1's node-label lookup (`resolve.go:63-68`):
an unknown label on an OPTIONAL-introduced node is still
`ErrUnknownLabel`. This is correct — the schema witness is required
even for a hypothetically-absent binding; a query that names a label
the schema does not declare is misdescribing the graph type it
targets.

### 4.2 The demotion lattice — a non-breaking refinement

The demotion algorithm operates on a per-binding boolean lattice:

- **Bottom** (⊥): `Nullable=true` — the binding was first introduced
  in an OPTIONAL MATCH and R4 has not yet proven its existence.
- **Top** (⊤): `Nullable=false` — the binding either was not
  OPTIONAL-introduced (parser-side `Nullable() == false`) or R4
  proved its existence from an edge-chain witness.

The lattice invariant is **directionality**: R4 only moves a binding
from ⊥ to ⊤, never the reverse. Consequences:

- A binding with `binding.Nullable() == false` starts and stays at ⊤;
  R4 never sets it back to ⊥.
- A binding with `binding.Nullable() == true` starts at ⊥. R4 may
  promote it to ⊤ if an edge chain proves existence; otherwise it
  stays at ⊥.

The invariant makes R4 provably non-breaking: any consumer of a
`ValidatedQuery` that treats a `Nullable=true` value as "may or may
not be present" is correct whether the resolver was pre-R4
(everything was implicitly ⊤ but wrongly emitted as absent-from-wire
because the whole-entity types had no `Nullable` axis) or post-R4
(explicitly ⊤ or explicitly ⊥ with a rebaselined wire). No consumer
that was correct on the pre-R4 wire becomes incorrect on the post-R4
wire; only more information is available.

### 4.3 The effective-nullability table

Between Phase C (§4.6 of R3) and the projection walk, the R4 kernel
builds:

```go
// nullableBinding maps each named binding variable to its effective
// R4-demoted Nullable flag. A binding whose Variable() is "" (anonymous
// edge) has no entry — no projection or parameter reads it.
nullableBinding := map[string]bool{}
```

Seed:

```
for each b in part.Bindings with named Variable():
    nullableBinding[b.Variable()] = b.Nullable()
```

The seed reads the frozen `Nullable()` accessor
(`internal/query/query.go:356` for `NodeBinding`, `query.go:464` for
`EdgeBinding`), which is a static local fact per ADR 0006 — never
demoted by the parser. Anonymous edges are
skipped in the seed (their `Variable() == ""`; they cannot appear in
a projection or parameter Ref); their nullability does not affect
the algorithm because the demotion witness in §4.4 walks named-edge
existence, and an anonymous edge whose existence would prove an
endpoint's non-nullability is still a witness — see §4.4.2.

**Anonymous-edge exception (subtle).** An anonymous edge with
`Nullable() == false` and `VarEndpoint` on both sides *does* prove
its endpoints exist. §4.4.2's iteration therefore walks
`part.Bindings` (which contains both named and anonymous edges), not
the `edgeBindings` map (which contains only named ones). The
`nullableBinding` table only stores *named* bindings' effective
flags — the anonymous edges contribute to the algorithm as *witnesses*
without themselves being demoted or read downstream.

### 4.4 The demotion fixed-point (regime (a))

Regime (a) is: **a required (non-nullable) edge that touches a
binding demotes that binding's effective nullability to false**. The
formal rule:

> A binding `b` is effectively non-nullable if there exists an
> `EdgeBinding e` in `part.Bindings` such that (i) `e.Nullable() ==
> false` in the effective table (initially the parser's flag, then
> the fixed-point-iterated table), (ii) `e` touches `b` (one of `e`'s
> endpoints is a `VarEndpoint{Variable: b.Variable()}`), and (iii)
> `e`'s existence proves `b`'s existence per §4.4.2's edge-shape
> table.

The rule is applied iteratively because a demoted binding can serve
as the witness endpoint of another edge whose demotion depends on
having a non-nullable endpoint. Consider:

```
OPTIONAL MATCH (a)-[r1:R1]->(b)
MATCH          (b)-[r2:R2]->(c)
RETURN a, b, c
```

The parser produces (verifiable via the
"optional match reuses prior binding" test at
`internal/query/cypher/parser_test.go:339-356` and the OPTIONAL
lowering path in `pattern.go:37-47`, `collectPattern`): `a`
nullable, `r1` nullable, `b` nullable (introduced by OPTIONAL),
`r2` non-nullable, `c` non-nullable.

Pass 1: `r2` is non-nullable; it touches `b` and `c`; both endpoints
are proven-existent → `nullableBinding[b] = false`,
`nullableBinding[c] = false`. `b`'s demotion is the seed for the
next pass — `r1`, which was OPTIONAL, still has `Nullable=true` at
this point.

Pass 2: **The edge-chain demotion is only from edges to endpoints,
not from endpoints to edges.** Regime (a) walks per-edge witnesses:
each non-nullable edge demotes its two named endpoints. It does not
walk sibling relations inside an OPTIONAL clause — the frozen model
records bindings, not OPTIONAL-clause groupings (§7.5). So on Pass
2 nothing new happens in the example: `r1` (OPTIONAL) is skipped;
`a` has no other touching edge, so `a` stays nullable.

Note that this is a deliberate under-approximation, not the full
openCypher semantics — the ideal flow-typing would demote `a` and
`r1` too, since a surviving row must have matched the OPTIONAL
clause that introduced all three together. R4 leaves them nullable
because the frozen model does not preserve the sibling-group
information regime (a)'s per-edge witness would need to walk. §7.5
records the gap and its resolution posture.

The R4 bead description (gqlc-lqm) states: "OPTIONAL MATCH
(a)-[:R]->(b) MATCH (b)-[:S]->(c) RETURN a, b, c — both a and b
demote." The bead is **semantically correct** under openCypher's
left-join row model: if `b` is proven existent by a downstream
required clause, then on every surviving row the OPTIONAL clause
that introduced `a`, `r1`, `b` together must have matched, so `a`
and `r1` also exist. R4 nonetheless demotes only `b`, as an
**under-approximation** — the frozen model records bindings, not
OPTIONAL-clause groupings, so regime (a)'s per-edge witness cannot
walk from `b`'s demotion to its OPTIONAL-clause siblings without new
model information. Leaving `a` and `r1` at `Nullable = true` is
safe (the conservative default is never wrong) and preserves the
non-breaking-refinement invariant. §7.5 records this
under-approximation and the model-information gap that motivates
it.

#### 4.4.1 The demotion witness — which edges prove which endpoints exist?

The R3 kernel produces four kinds of resolved edge shapes (R3 §4.6
verdict). R4 needs a rule per kind for whether a non-nullable edge
of that kind demotes its endpoints. The rule:

| Edge shape (R3-resolved) | Effective non-nullable? | Demotes source endpoint? | Demotes target endpoint? |
|---|---|---|---|
| Directed, single-hop, single-candidate `ResolvedEdge` (case B) | if `Nullable()` false | **yes** | **yes** |
| Directed, single-hop, multi-candidate `ResolvedEdgeUnion` (case D — multi-type union) | if `Nullable()` false | **yes** | **yes** |
| Undirected, single-hop, single-candidate `ResolvedEdge` (case B — single-match undirected) | if `Nullable()` false | **yes** | **yes** |
| Undirected, single-hop, multi-candidate `ResolvedEdgeUnion` (case D — multi-type × multi-orientation) | if `Nullable()` false | **yes** | **yes** |
| Directed, var-length `ResolvedList{Element: ResolvedEdge/Union}` | if `Nullable()` false AND `Hops().Min() >= 1` (see §4.4.3) | conditional | conditional |
| Undirected, var-length (same wrap) | same | conditional | conditional |

For every **single-hop** shape (rows 1-4), the demotion is
unconditional: a required (non-nullable) single-hop edge between `a`
and `b` means "for every surviving row there is at least one such
edge, therefore `a` and `b` both exist on that row". This holds for
directed and undirected alike, for single-type and multi-type
(union) alike, because in every case the edge's existence entails
both endpoints exist.

For **var-length** shapes (rows 5-6), the demotion is conditional on
the hop range's minimum (§4.4.3).

Case C from R3 (single-type undirected double-match) never appears in
the effective binding table — it fails resolution with
`ErrAmbiguousEdgeOrientation` (R3 §4.6 verdict-C). So it does not
enter §4.4.1's table.

Anonymous edges (whose `Variable() == ""`) participate as witnesses
identically: an anonymous non-nullable edge in `part.Bindings` walks
its `Source()` and `Target()` endpoints and demotes any named
binding they name via `VarEndpoint`.

#### 4.4.2 Iteration — the fixed-point loop

```
nullableBinding := seed()  // §4.3

for {
    changed := false
    for each edge e in part.Bindings (in first-appearance order):
        if e.Nullable() {
            continue  // seed unchanged; regime (a) only demotes from
                      // required edges. The fixed-point does not
                      // promote an OPTIONAL edge to required.
        }
        if not qualifiedDemoter(e):  // §4.4.3 (var-length gate)
            continue
        for each side in {Source, Target}:
            if side is VarEndpoint{Variable: v} and v != "":
                if nullableBinding[v] {
                    nullableBinding[v] = false
                    changed = true
    if not changed:
        break
}
```

An `EdgeBinding.Nullable()` is a **static** parser fact (ADR 0006):
the seed reads it once and never re-reads it. R4 regime (a) does not
demote edge bindings themselves — only node bindings and *other*
edge bindings' existence claims flow from other edges' existence, and
regime (a) as scoped here only makes the endpoint claim. An
OPTIONAL edge whose existence *could* be inferred from other
structure is regime (b)'s question (deferred to R5).

Wait — an edge is also a binding with a `Nullable()` bit that is
worth demoting. Consider:

```
MATCH          (a:Person)-[r1:AUTHORED]->(b:Post)
OPTIONAL MATCH (b)-[r2:LIKES]->(a)
RETURN a, b, r1, r2
```

Here `r2` is OPTIONAL. Nothing in the query proves `r2` exists —
regime (a) does not demote it. This is correct. Now:

```
OPTIONAL MATCH (a)-[r1:AUTHORED]->(b)
MATCH          (a)-[r1:AUTHORED]->(b)
RETURN r1
```

Same-part re-MATCH of `r1` — the parser merges into one binding
whose first-introduction was OPTIONAL, so `r1.Nullable()` stays
true. Regime (a) cannot see the required re-MATCH (Stage 4 spec §3
lines 179-187) — this is regime (b), deferred to R5.

Therefore R4's loop as written never demotes an edge binding. Only
node bindings are ever demoted. The loop *reads* every edge's
existence but only *writes* to `nullableBinding[<node-var>]`. To
make this explicit, the pseudocode simplification:

```
for each edge e (in first-appearance order):
    if e.Nullable() or not qualifiedDemoter(e):
        continue
    for each side in {Source, Target}:
        if side is VarEndpoint{Variable: v}:
            nullableBinding[v] = false
```

Because the loop only writes `false` to `nullableBinding[v]` for
node bindings (edges don't have a `VarEndpoint`-referable side), a
single pass is enough — the demotion set is monotone (⊥ → ⊤, never
back), and every witness edge is inspected once. No iteration is
needed. The initial "fixed-point" framing was a defensive
generalisation for future regimes; regime (a) alone terminates in
one pass.

**Judgment call — single-pass is sufficient at R4.** The claim that
one pass suffices is only true for regime (a). Regime (b) at R5 will
require multiple passes because a bare-pattern demoted binding can
serve as another regime-(b) chain's endpoint. R4's implementation
uses a single pass; R5's spec revises this if needed. §4.4.4 records
the termination argument.

#### 4.4.3 Var-length edges as witnesses

A var-length edge `-[r:R*1..3]->` between `a` and `b`, non-nullable
and required, matches at least one hop in every surviving row.
Therefore both endpoints exist — the same demotion rule as
single-hop applies. Formal check on the parser side: `EdgeHops.Min()`
returns `*int` (`internal/query/query.go:500-501`), where `nil` is
"unbounded lower bound" — parser Stage 8 records `[r*]`, `[r*..N]`
as `Min() == nil`. openCypher treats an unbounded lower bound as
`1`, so `Min() == nil` is a demoting shape. `Min()` pointing at `0`
(`[r*0..N]`) is the zero-hop-admitting shape: the edge may match
zero physical hops, so `a == b` on those rows and the edge does not
independently witness `a`'s or `b`'s existence. Rule:

```
qualifiedDemoter(e query.EdgeBinding) bool =
    if e.Hops() == nil:              // single-hop
        return true
    min := e.Hops().Min()
    if min == nil:                   // [r*] / [r*..N] — openCypher: min = 1
        return true
    return *min >= 1                 // var-length: explicit min >= 1
```

A `MATCH (a)-[r:R*0..3]->(b)` — parser accepts (Stage 8) — is a
non-demoting witness: even though `r.Nullable() == false`, the
zero-hop match means `a == b` and the edge itself is a no-op. Since
`a` and `b` are the same node, `a` is proven-existent iff `b` is
proven-existent — a tautology that regime (a) does not resolve. For
completeness: if either `a` or `b` is separately proven-existent by
some other clause, the var-length edge's existence carries over — but
that is regime (a) applied to the OTHER witnessing edge, not to this
one. So a `*0..N` var-length edge cannot itself be a demotion
witness.

**Judgment call — zero-hop var-length edges are non-witnesses.** This
choice preserves the demotion invariant (non-breaking refinement)
even in the tricky edge case. A future R-later stage may revisit if
graph-model semantics evolve; R4's decision is documented here so
the reviewer can trace the reasoning.

**Judgment call — unbounded-lower (`[r*]`, `[r*..N]`) IS a witness.**
openCypher's runtime treats an unbounded lower bound as `1`, so the
range `[r*]` semantically equals `[r*1..∞]` — every match has at
least one hop, therefore both endpoints exist on every row. This
matches the single-hop demoter arm.

#### 4.4.4 Termination

Regime (a) at R4 is a single pass, so termination is trivial. The
defensive fixed-point framing (§4.4.2 pseudocode) terminates in at
most `|part.Bindings|` passes because each pass either demotes at
least one binding or exits (no change). The `nullableBinding`
table's size is fixed (`|part.Bindings|` at most), and the update
direction is monotone (⊥ → ⊤). Both give a linear termination bound.

### 4.5 Projection walk — reading the effective table

The R3 `refProjectionType` (`resolve.go:464-514`) is R4-revised to
consult `nullableBinding` when emitting the `Nullable` bit on the
returned `ResolvedType`. The revisions:

- **Whole-entity node** (`ref.Variable` names a `NodeBinding`,
  `ref.Property == ""`): emit `ResolvedNode{Labels: nt.Labels,
  Nullable: nullableBinding[ref.Variable]}` instead of the R3
  `ResolvedNode{Labels: nt.Labels}`.
- **Whole-entity edge, single-candidate, single-hop** (edge in
  `edgeTypes`/`edgeKeys`, not var-length): emit
  `ResolvedEdge{EdgeKey: edgeKeys[ref.Variable], Nullable:
  nullableBinding[ref.Variable]}`.
- **Whole-entity edge, multi-candidate, single-hop** (edge in
  `edgeCands`, not var-length): emit
  `ResolvedEdgeUnion{EdgeKeys: cands, Nullable:
  nullableBinding[ref.Variable]}`.
- **Whole-entity edge, single-candidate, var-length**: emit
  `ResolvedList{Element: ResolvedEdge{EdgeKey: edgeKeys[ref.Variable],
  Nullable: nullableBinding[ref.Variable]}}`.
- **Whole-entity edge, multi-candidate, var-length**: emit
  `ResolvedList{Element: ResolvedEdgeUnion{EdgeKeys: cands,
  Nullable: nullableBinding[ref.Variable]}}`.
- **Property of node or edge** (`ref.Property != ""`, not var-length):
  compute `bindingNullable := nullableBinding[ref.Variable]`; emit
  `ResolvedProperty{Type: prop.Type, Nullable: prop.Nullable ||
  bindingNullable}`.
- **Property of var-length edge** — unchanged: still routes to
  `ErrOutOfR0Scope` (R3 §4.7 property-on-var-length reject).

The `unionProperty` helper (R3 §4.8, `resolve.go:519-537`) is
R4-revised: it takes an additional `bindingNullable bool` argument
and applies the OR before returning the `ResolvedProperty` on the
happy path. The union-agreement rule stays unchanged (every member
must agree on `(Type, Nullable)` before OR'ing) — the disagreement
sentinel `ErrUnknownProperty` still fires on §4.8 step 2 conflicts
BEFORE the OR is applied.

**Judgment call — union agreement compared *before* OR.** A
multi-candidate edge's members may agree on the schema property's
type but if one has `Nullable=true` and another `Nullable=false`,
R3's §4.8 fires `ErrUnknownProperty` for "type differs across union
members". R4 keeps that behaviour: the members must agree on the
schema shape first; the binding-side OR happens after. Rationale:
the members disagreeing on schema nullability is a real schema-shape
divergence; the OR-then-compare direction would mask it silently.

### 4.6 Parameter walk — reading the effective table

The R3 `propertyUseWitness` helper (`resolve.go:627-652`) is
R4-revised identically to §4.5's property-projection path: it
consults `nullableBinding[ref.Variable]` and OR's it with the
schema's `prop.Nullable`. The `useWitness` dispatch
(`resolve.go:600-620`) is unchanged in shape; `PropertyUse` routes
through the R4-revised `propertyUseWitness`. `ClauseSlotUse` and
`ExprUse` are unchanged (they carry no binding-side Ref).

**Unification lattice (R2 §4.8) — unchanged.** The R2 rule that two
`ResolvedProperty`s with different `Nullable` bits do not unify still
holds. If a parameter has two Uses — one on an OPTIONAL binding's
property that R4 pushes to `Nullable=true`, and one on a required
binding's non-nullable property — they will still not unify, and
`ErrParameterTypeConflict` still fires. R4's addition of the
disjunction rule at the *witness* stage means the input to the
unification lattice already carries the disjunction — a parameter
that appears on an OPTIONAL node's non-nullable property and on a
required node's non-nullable-same-typed property will now unify at
`Nullable=true`, not fail — because both witnesses now correctly
carry `Nullable=true`. This is a strict widening: parameters that
would have "conflicted" pre-R4 (had R0-R3 been able to see the
binding-side Nullable, which they didn't) can now unify. Since R0-R3
never emitted an OPTIONAL-binding-derived witness, this widening
does not retroactively change any pre-R4 golden — every R2 parameter
fixture has non-OPTIONAL bindings, so their unification remains
identical.

### 4.7 Statement kind — unchanged

R0 §4.7 step 6 stands: copy `q.StatementKind` into the output. R4
does not admit writes; StatementKind is always `read`.

### 4.8 The revised type-mapping table — R4 owner column

R3 §4.11's twenty-row spine is the reference. R4 revises no row's
type-counterpart or classification — the mapping table's rows are
schema/type-level; R4's addition (a `Nullable` axis on three
whole-entity variants) is orthogonal to the table's rows. For
completeness, the two rows R4 touches operationally:

| Variant | Resolver counterpart | Classification | Owner (before R4) | Owner (R4) |
|---|---|---|---|---|
| `TypeNode` | `ResolvedNode{Labels, Nullable}` | schema-upgraded | R0 | R0 (unchanged); R4 adds Nullable axis |
| `TypeEdge` | `ResolvedEdge{EdgeKey, Nullable}` or `ResolvedEdgeUnion{EdgeKeys, Nullable}` | schema-upgraded | R1 (single); R3 widens; R4 adds Nullable axis | R1/R3/R4 |
| `TypeList` (element `TypeEdge`) | `ResolvedList{Element}` with Element carrying Nullable | schema-upgraded | R3 | R3/R4 (element gains axis) |

Every other row is unchanged.

The `TypeUnknown (property projection)` and `TypeUnknown (property
parameter)` rows are already schema-upgraded to `ResolvedProperty`
carrying `Nullable` — R4 widens the *meaning* of that bit, not the
row's counterpart. Same for the R2 `TypeUnknown (list element)`
row when the element is a `ResolvedEdge` (only the `ResolvedEdge`
element gains the axis).

The table stays closed at R4: every variant of the frozen
`query.Type` sum still appears, each classified, each with an
R-stage owner. R5–R7 revise rows they take up.

---

## 5. Sentinels — the R4 revision

**Zero new sentinels at R4.** The R3 seven-sentinel set is unchanged
in identity. The `allSentinels` list is unchanged. The
`invalidFixtures` map gains rows only for R4-remainder
`ErrOutOfR0Scope` sub-cases (§6.4) — no new sentinel to seed the
reachability sweep.

### 5.1 Why zero new sentinels

Regime (a) demotion is a **total** function: for every possible
input (any `part.Bindings` shape R4 admits), the algorithm produces
a well-defined `nullableBinding` table without fail-modes.
Non-existence of an edge witness leaves a binding at ⊥ (the
conservative default) — that is a successful outcome, not a failure.
No schema witness is required beyond what R0–R3 already required
(the labels/edge-keys must resolve — those failures are already
R0/R1's business). The lattice is a boolean semilattice with no
"unresolvable" state; the fixed-point converges in one pass.

The rejected alternative was a hypothetical
`ErrNullabilityContradiction` for a case where the query author
signals conflicting nullability intent — e.g. `OPTIONAL MATCH (n)
MATCH (n) RETURN n` (a re-MATCH of the same variable). The parser
collapses this into one binding whose static `Nullable()` flag is
whatever the first-introduction clause set — here `true` because the
OPTIONAL MATCH appeared first (`pattern.go:373-401`'s `mergeBinding`
merges in first-appearance order and honours OPTIONAL only on first
introduction). The later required MATCH is invisible in the model,
so R4's regime (a) — which walks per-edge witnesses only — sees one
nullable binding with no required-edge witness and leaves it
nullable. No fail-mode; no sentinel needed. This is the same
model-information gap that motivates deferring bare-pattern regime
(b) to R5 (§7.4); if a future regime introduces a genuine
contradiction surface, its spec argues for its own sentinel.

### 5.2 R3 sentinels' message sets — unchanged

- `ErrUnknownLabel`, `ErrUnknownProperty`, `ErrUnknownEdge`,
  `ErrAmbiguousBinding`, `ErrAmbiguousEdgeOrientation`,
  `ErrParameterTypeConflict` — behaviour and message set unchanged
  at R4.
- `ErrOutOfR0Scope` — the R3 category-grained sentinel. R4 retires
  no sub-case (multi-part, UNION, AggregateProjection, writes, CALL,
  DISTINCT, `RETURN *` all stay out-of-scope). R4 adds no new
  sub-case: OPTIONAL MATCH itself is not out-of-scope at R4 (that's
  the whole point of R4). §6.4 lists one new invalid fixture that
  witnesses the R4-still-out-of-scope surface: `OPTIONAL MATCH` +
  `WITH` = multi-part → R5.

---

## 6. The golden-pair harness — R4 revision

R0/R1/R2/R3's harness stands: the `test/data/resolver/{valid,invalid}`
layout, the `-update` flag, the invalid-fixture map, the reachability
sweep, the schema-mapping totality. R4 revises the fixture set only,
not the harness code.

### 6.1 Schema fixture strategy — one new schema

The R4 valid schema (`social_r4.gql`) is a superset of R3's:

- The R3 shapes (`Person`, `Post`, `AUTHORED`, `LIKES`, reverse
  `AUTHORED`, self-loop `KNOWS`) unchanged in text.
- No new node types.
- No new edge types.

Since R4 adds fixtures that exercise OPTIONAL MATCH but reuse the
same shape space (Person / Post / AUTHORED / LIKES / KNOWS), the R3
schema shape is sufficient. R4's `social_r4.gql` is effectively a
byte-copy of `social_r3.gql` under a new file name — the file exists
so R3 fixtures' schema mapping stays pinned at `social_r3.gql` and
R4 fixtures point at `social_r4.gql`, keeping the fixture-to-schema
pairing R-stage-scoped (R3 §6.6 invariant).

**Judgment call — separate `social_r4.gql` when it's a byte copy of
`social_r3.gql`.** Two reasons: (i) preserves the R3 §6.6 "schema
fixtures are R-stage-scoped" invariant so a future R5 that widens
the R4 schema does not affect R4 goldens; (ii) makes it obvious to
a reader that the R4 fixture set is scoped to R4-specific queries,
even if the schema shape is unchanged. The alternative — reuse
`social_r3.gql` — would work but crosses the R-stage schema
boundary, which reviewers must trust invariant-wise.

The R4 invalid corpus reuses `social.gql` (R1 shape) for the
multi-part reject fixture.

### 6.2 Schema fixture text

`test/data/resolver/valid/schemas/social_r4.gql`:

```gql
CREATE PROPERTY GRAPH TYPE SocialR4 AS {
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
    (:Person) -[:AUTHORED { publishedAt :: TIMESTAMP, views :: INT NOT NULL, likedAt :: TIMESTAMP }]-> (:Post),
    (:Person) -[:LIKES { likedAt :: TIMESTAMP }]-> (:Post),
    (:Post)   -[:AUTHORED { authoredBy :: STRING NOT NULL }]-> (:Person),
    (:Person) -[:KNOWS { since :: DATE }]-> (:Person)
}
```

Byte-identical to `social_r3.gql` up to the type name (`SocialR4`
vs `SocialR3`). The type name difference is documentary; both
schemas would resolve identically for any query.

### 6.3 R4 valid fixtures

Added under `test/data/resolver/valid/`. Each fixture is one Cypher
file; each has one paired `.validated.golden.json` regenerated by
`-update`. `schema.mapping.json` grows one row per fixture pointing
at `social_r4.gql`.

**In addition, every R0–R3 valid fixture's golden is regenerated to
add the `"nullable": false` field on every whole-entity column
(§3.6).** The regeneration is done at the R4 code cycle, not this
spec cycle.

| Fixture | Shape | R4 arm exercised |
|---|---|---|
| `optional_node_whole_entity.cypher` | `OPTIONAL MATCH (p:Person) RETURN p` | seed: `p.Nullable() == true`; no witness; `ResolvedNode{Person, Nullable: true}` |
| `optional_node_property.cypher` | `OPTIONAL MATCH (p:Person) RETURN p.name` | `p.Nullable() == true`; `p.name` schema `NOT NULL`; §3.4 disjunction → `ResolvedProperty{STRING, Nullable: true}` |
| `optional_node_nullable_property.cypher` | `OPTIONAL MATCH (p:Person) RETURN p.age` | `p.Nullable() == true`; `p.age` schema nullable; disjunction stays `Nullable: true` |
| `optional_edge_whole_entity.cypher` | `OPTIONAL MATCH (p:Person)-[r:AUTHORED]->(post:Post) RETURN r` | seed: `r.Nullable() == true`; no witness; `ResolvedEdge{Person→AUTHORED→Post, Nullable: true}` |
| `optional_edge_property.cypher` | `OPTIONAL MATCH (p:Person)-[r:AUTHORED]->(post:Post) RETURN r.views` | `r.Nullable() == true`; `views :: INT NOT NULL`; disjunction → `ResolvedProperty{INT, Nullable: true}` |
| `demote_required_edge_endpoints.cypher` | `MATCH (p:Person)-[r:AUTHORED]->(post:Post) RETURN p, r, post` | R0/R1 shape, no OPTIONAL — every binding non-nullable from parser seed; whole-entity `ResolvedNode`/`ResolvedEdge` all `Nullable: false` (regenerated R0/R1 golden shape) |
| `demote_chained_from_required.cypher` | `OPTIONAL MATCH (p:Person)-[r1:AUTHORED]->(post:Post) MATCH (post)-[r2:AUTHORED]->(author:Person) RETURN p, r1, post, r2, author` | seed: `p`, `r1`, `post` nullable; `r2`, `author` non-nullable; §4.4.1 rule: `r2` is required directed single-hop → demotes both `post` and `author`; `post` transitions to `Nullable: false`. `p` and `r1` stay `Nullable: true` (no required witness). Golden: `ResolvedNode{Person, Nullable: true}` for `p`, `ResolvedEdge{Nullable: true}` for `r1`, `ResolvedNode{Post, Nullable: false}` for `post` (demoted), `ResolvedEdge{Nullable: false}` for `r2`, `ResolvedNode{Person, Nullable: false}` for `author`. |
| `demote_undirected_edge_endpoints.cypher` | `MATCH (p:Person)-[r:LIKES]-(post:Post) RETURN p, r, post` | undirected single-match required edge; §4.4.1 row 3 → both endpoints demoted (they were non-nullable already; witness verified). Whole-entity nullable stays false. Fixture confirms undirected witnesses same as directed. |
| `demote_var_length_positive_min.cypher` | `MATCH (p:Person)-[r:KNOWS*1..3]->(q:Person) RETURN p, r, q` | var-length required edge with `Min() == 1 >= 1` (§4.4.3) → demotes both endpoints. Whole-entity nullable false; `r` projects as `ResolvedList{Element: ResolvedEdge{Nullable: false}}` (a var-length required edge is a required list; the element carries the required flag). |
| `no_demote_var_length_zero_min.cypher` | `OPTIONAL MATCH (p:Person)-[r:KNOWS*0..3]->(q:Person) RETURN p, r, q` | OPTIONAL var-length edge with `*Min() == 0` (§4.4.3 exclusion) → does NOT demote from *this* edge; nothing else in the pattern is a witness either, so `p`, `r`, `q` all stay `Nullable: true`. Exercises the zero-min-exclusion arm directly: without the exclusion the required-looking `r` would wrongly demote `p` and `q`, but the parser's OPTIONAL flag is preserved on the emitted goldens.
| `demote_var_length_unbounded_lower.cypher` | `MATCH (p:Person)-[r:KNOWS*]->(q:Person) RETURN p, r, q` | var-length required edge with `Min() == nil` (unbounded lower ⇒ openCypher-semantic min=1, §4.4.3 second judgment call) → does demote both endpoints. Whole-entity `p`, `r`, `q` all `Nullable: false`. Pins that the `nil`-min case is admitted as a witness. |
| `demote_from_anonymous_required_edge.cypher` | `OPTIONAL MATCH (a:Person)-[:AUTHORED]->(b:Post) MATCH (a)-[:AUTHORED]->(c:Post) RETURN a, b, c` | anonymous required edge in the second MATCH demotes `a` (via §4.4.2's walk over anonymous edges); `c` is directly non-nullable from parser (introduced in required MATCH). `b` stays `Nullable: true` — the OPTIONAL edge does not prove `b` exists. Note: `a` is initially OPTIONAL-nullable, but the anonymous non-nullable edge in the second required MATCH's pattern demotes it. |
| `optional_multi_type_union.cypher` | `OPTIONAL MATCH (p:Person)-[r:AUTHORED\|LIKES]->(post:Post) RETURN r` | multi-candidate + nullable: `ResolvedEdgeUnion{[Person→AUTHORED→Post, Person→LIKES→Post], Nullable: true}` |
| `optional_var_length_whole_entity.cypher` | `OPTIONAL MATCH (p:Person)-[r:KNOWS*1..3]->(q:Person) RETURN r` | var-length + nullable: `ResolvedList{Element: ResolvedEdge{Person→KNOWS→Person, Nullable: true}}` — the element's Nullable is the binding's effective nullable per §3.5 |

**Coverage sketch (per row, keyed to the algorithm):**

- `optional_node_whole_entity` — exercises the seed (§4.3) with a
  single OPTIONAL-introduced node and no edges. `nullableBinding[p]
  = true`; column golden confirms.
- `optional_node_property` / `optional_node_nullable_property` —
  exercise §3.4's disjunction on both a schema-`NOT NULL` and
  schema-nullable property. Both golden as `Nullable: true`.
- `optional_edge_whole_entity` / `optional_edge_property` —
  exercise the seed on an OPTIONAL edge and §3.4's disjunction for
  an edge-property.
- `demote_required_edge_endpoints` — exercises the seed on a
  required binding (baseline: non-nullable).
- `demote_chained_from_required` — the canonical R4 regime (a)
  case, from ADR 0006's example ("non-nullable edge → non-nullable
  endpoints → demote those endpoints' nullability"): `post`
  demotes from `Nullable=true` to `Nullable=false` because `r2`
  requires it. This is the load-bearing fixture for §4.4.
- `demote_undirected_edge_endpoints` — exercises §4.4.1 row 3
  (undirected single-match is a demotion witness).
- `demote_var_length_positive_min` — exercises §4.4.3's positive-min
  branch (var-length with `*Min() >= 1` IS a witness).
- `no_demote_var_length_zero_min` — exercises §4.4.3's zero-min
  exclusion using an OPTIONAL var-length so the parser seed leaves
  the endpoints nullable; the fixture's golden pins that the
  required-looking `r` does NOT demote them.
- `demote_var_length_unbounded_lower` — exercises §4.4.3's second
  judgment call (`Min() == nil` ⇒ min=1 ⇒ demoter).
- `demote_from_anonymous_required_edge` — exercises §4.4.2's
  named-plus-anonymous walk: anonymous edges are witnesses too.
  Same-part reuse of `a` doesn't help (regime (b), R5); the
  demotion comes from the *anonymous* edge in the same required
  MATCH.
- `optional_multi_type_union` — exercises §3.3's `Nullable` axis
  on a `ResolvedEdgeUnion`.
- `optional_var_length_whole_entity` — exercises §3.5's element-
  level `Nullable` on `ResolvedList{ResolvedEdge}`.

### 6.4 R4 invalid fixtures — updated `invalidFixtures` map

The R0/R1/R2/R3 map stands. R4 adds one new row that witnesses the
R4-still-out-of-scope surface:

```go
var invalidFixtures = map[string]error{
    // R0/R1/R2/R3 rows carried forward (unchanged)
    "unknown_label.cypher":                                 ErrUnknownLabel,
    "unknown_property.cypher":                              ErrUnknownProperty,
    "with_clause.cypher":                                   ErrOutOfR0Scope,
    "aggregate_projection.cypher":                          ErrOutOfR0Scope,
    "return_distinct.cypher":                               ErrOutOfR0Scope,
    "returns_all.cypher":                                   ErrOutOfR0Scope,
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

    // R4 new row
    "optional_match_with_clause.cypher":                    ErrOutOfR0Scope,
}
```

**R4 invalid fixture contents:**

- `optional_match_with_clause.cypher`:
  `OPTIONAL MATCH (p:Person) WITH p RETURN p` — the R4 gate at
  `resolve.go:28-30` rejects the multi-part query (WITH creates a
  second Part in the parser Stage 4 model). Verdict: `ErrOutOfR0Scope`.
  Also serves as a scope-boundary witness: `OPTIONAL MATCH` is
  admitted, but `OPTIONAL MATCH + WITH` is not — R5 admits the
  combination. The fixture explicitly documents that R4 does not
  reach regime (b) via WITH (§7.4).

The pre-existing `with_clause.cypher` (R0-era, `MATCH (p:Person)
WITH p.name AS x RETURN x`) continues to route to
`ErrOutOfR0Scope`. It exercises the WITH-clause gate at
`resolve.go:28-30` in the non-OPTIONAL case; `optional_match_with_
clause.cypher` exercises the same gate in the OPTIONAL case. Both
fixtures share the sentinel; neither is retired at R4.

### 6.5 Determinism check — R4 additions

R0 §6.5, R1 §6.5, R2 §6.6, R3 §6.5 stand. R4 adds:

- `part.Bindings` first-appearance order is the parser's
  guarantee; §4.4.2's loop respects it.
- `nullableBinding` is a `map[string]bool` — the walk is
  bindings-first-appearance, not map-order. No map iteration
  escapes to the wire.
- The `Nullable` field on the widened variants marshals in the
  order the struct declares (`Labels` then `Nullable` for
  `ResolvedNode`; `EdgeKey` then `Nullable` for `ResolvedEdge`;
  `EdgeKeys` then `Nullable` for `ResolvedEdgeUnion`). Existing
  tools that read the golden and expect deterministic field order
  see the new field in a stable position.

Every ordered surface is either the parser's guaranteed order or a
fixed struct declaration. Two resolves of the same query produce
byte-identical goldens; `-update` regenerates a byte-stable file.

### 6.6 Non-obvious harness invariants — R4 additions

R0 §6.6, R1 §6.6, R2 §6.7, R3 §6.6 invariants stand. R4 adds:

- **Every R0-R3 golden with a whole-entity column rebaselines on
  the R4 code cycle.** The rebaseline diff is exactly the added
  `"nullable": false` field on every affected column. Reviewers
  auditing the rebaseline should look for no other diff.
- **Fixture files that don't touch whole-entity columns don't
  rebaseline.** `literal_int_projection.cypher`,
  `parameter_expr_predicate.cypher`, etc., produce the same wire
  bytes pre- and post-R4.
- **The demotion algorithm's happy path — a query with no OPTIONAL
  MATCH — still produces the same `Nullable: false` values on
  every whole-entity column.** R4's rebaseline is additive; the
  values do not change from what a hypothetical "R3 emitted
  Nullable" would have produced. This is the non-breaking-refinement
  invariant §4.2 argues.

---

## 7. R4 capability scope — what resolves

**In scope:** a single Cypher query the parser accepts, whose parsed
`query.Query` shape is:

- Exactly one `Branch` in `Branches`; zero `Combinators`.
- Exactly one `Part` in the branch's `Parts`.
- The part's `Bindings` are a non-empty slice of `NodeBinding` and/or
  `EdgeBinding` values (R3 §7 shape — labelled or Phase-B-inferable
  nodes; directed/undirected × single-hop/var-length × single-type/
  multi-type edges).
- Bindings may be first-introduced in `MATCH` (non-nullable per parser
  ADR 0006) OR in `OPTIONAL MATCH` (nullable). Every binding's
  `Nullable()` accessor is read by the R4 kernel §4.3-§4.4.
- The part's `Returns` is a non-empty slice of `ReturnItem`s. Each
  `ReturnItem.Value` is `RefProjection`, `LiteralProjection`,
  `FuncProjection`, or `ExprProjection` (R2 §7).
- `ReturnsAll` is false; `Distinct` is false; `Effects` is empty.
- `Parameters` is a slice of `Parameter`s with the R2 shape (one or
  more Uses each; each Use is `PropertyUse`, `ClauseSlotUse`, or
  read-side `ExprUse`).
- `StatementKind` is `StatementRead`.

**Out of scope, routed to the appropriate sentinel:**

R3 §7's out-of-scope table stands verbatim, with one line's
sentinel-relationship revised:

| Construct | Sentinel | R-stage owner |
|---|---|---|
| Untyped edge (`len(Labels()) == 0`) | `ErrOutOfR0Scope` | R-later |
| Path binding | `ErrOutOfR0Scope` | R5 |
| Unwind binding | `ErrOutOfR0Scope` | R5 or later |
| Call binding | `ErrOutOfR0Scope` | R7 |
| `AggregateProjection` | `ErrOutOfR0Scope` | R5 |
| `ExprProjection` typed `TypeList{TypeNode\|TypeEdge}` | `ErrOutOfR0Scope` | R5 |
| Property projection on a variable-length edge binding | `ErrOutOfR0Scope` | R5 |
| `ExprUse` at `ExprInSetValue` / `ExprInDeleteTarget` | `ErrOutOfR0Scope` | R6 |
| **Nullability upgrades (OPTIONAL MATCH regime (a))** | **~~ErrOutOfR0Scope → in-scope at R4~~** | **R4 (this stage)** |
| **Nullability upgrades (OPTIONAL MATCH regime (b) — bare-pattern demotion in a re-MATCH)** | `ErrOutOfR0Scope` (via the WITH-clause gate; §7.4) | **R5** |
| `Part.Distinct == true` / `Part.ReturnsAll == true` | `ErrOutOfR0Scope` | R5 |
| WITH carry-forward; UNION | `ErrOutOfR0Scope` | R5 |
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

**Silently accepted (not routed anywhere):**

R0/R1/R2/R3's silently-accepted set stands unchanged (literal-only
WHERE / ORDER BY / SKIP / LIMIT). R4 does not extend it.

**Recorded ADR 0009 cross-check.** ADR 0009 R4: "ADR 0006's
flow-typing regimes a and b (`gqlc-lqm`), relaxing the conservative
everything-OPTIONAL-is-nullable default — a non-breaking refinement
by construction." R4 as this spec scopes it implements regime (a)
fully and defers regime (b) to R5 with an explicit citation (§7.4).
The **non-breaking refinement** claim is defended by §4.2's lattice
invariant and by §6.6's "happy path values unchanged" invariant.

### 7.4 Regime (b) — defended deferral to R5

ADR 0009 assigns "flow-typing regimes a and b" to R4. This spec
scopes R4 to regime (a) only. The defence:

1. **Regime (b) requires per-clause structure the frozen model
   records only across a WITH boundary.** Parser Stage 4 spec §3
   (lines 171-187) states this explicitly: "The limit, stated
   honestly: the part boundary is `WITH`, so this only records the
   *cross-`WITH`* second reference. A second reference within a
   single part — `OPTIONAL MATCH (a)-[:R]->(b) MATCH (b) RETURN b`
   with **no** `WITH` — still merges both `MATCH`es into one
   `rawBinding` (the Stage-0 merge rule is unchanged), so the
   resolver sees one binding and cannot demote it from clause
   structure alone."
2. **R4 admits only single-Part queries.** The current kernel gate
   at `resolve.go:28-30` rejects multi-part queries with
   `ErrOutOfR0Scope`. R4 does not widen this gate (R5's business).
   Therefore, R4 never sees a WITH-separated OPTIONAL/required pair.
3. **The single-part regime-(b) case is unreachable by
   construction.** As the parser Stage 4 spec §3 line 179 states:
   within a single Part, `MATCH (b) MATCH (b)` merges to one
   binding — the resolver receives one `NodeBinding("b")` whose
   first-introduction `Nullable()` flag is whatever the first
   clause set. If the FIRST clause is OPTIONAL, the binding is
   Nullable and the second required MATCH's presence is invisible
   in the model; there is no way for R4 to demote from what the
   model doesn't record. If the FIRST clause is required, the
   binding is non-nullable and there is nothing to demote (an
   OPTIONAL second occurrence does not re-nullable it, per the
   `mergeBinding` docstring at `pattern.go:373-401`).

Conclusion: at R4, regime (b) is either (i) invisible in the
single-Part model (same-Part re-MATCH) or (ii) not admitted at all
(WITH-separated multi-Part). Both cases require R5's admission gate
to widen before regime (b) has any surface to run on. Deferring
regime (b) to R5 is not scope-narrowing R4 — it is aligning R4 with
the frozen model's structural boundary.

**Bead update (informational, not spec-authoritative).** gqlc-lqm's
description will be revised to reflect this scope split at the R4
code cycle close-out: regime (a) closes with R4, regime (b) closes
with R5. This spec does not commit to that revision — it lives in
beads workflow.

### 7.5 Under-approximation vs the bead's canonical example

The bead gqlc-lqm's description says: "OPTIONAL MATCH (a)-[:R]->(b)
MATCH (b)-[:S]->(c) RETURN a, b, c — both a and b demote."

This spec (§4.4) demotes only `b`; `a` and `r1` stay nullable.

**The bead is semantically correct.** openCypher's row model is
left-join: if the OPTIONAL match fails, `a`, `r1`, `b` are all NULL
on that row. If `b` is NULL, the required `MATCH (b)-[:S]->(c)`
cannot match (NULL joins nothing), so the row is dropped. Therefore
on every surviving row `b` is non-NULL, which means the OPTIONAL
clause matched, which means `a` and `r1` are also non-NULL. Ideal
flow-typing would demote all three.

**R4's regime (a) demotes only `b` — a deliberate under-approximation.**
The reason is a model-information limit, not a semantic one:

- Regime (a)'s witness is per-edge. It knows "a non-nullable edge
  proves its two endpoints exist". It does NOT know "these three
  bindings were introduced by the same OPTIONAL clause and therefore
  share a fate".
- The frozen `query.Query` model (Stage 2 + freeze ADR 0008) records
  bindings, not clause groupings. `part.Bindings` is a flat slice of
  `NodeBinding` and `EdgeBinding` values; there is no
  `OptionalMatchClause` sibling-group marker to walk from `b`'s
  demotion outward to `a` and `r1`. Adding one would be new model
  information — outside R4's remit (ADR 0009: R4 admits no new
  clause forms).
- Under-approximation is safe. `Nullable = true` is the conservative
  default; leaving `a` and `r1` there rather than demoting them is
  correct-but-pessimistic. Consumers that treat `Nullable = true` as
  "may or may not be present" remain correct. The lattice invariant
  (§4.2) holds: no binding is ever demoted incorrectly.
- A future regime — call it regime (c), "OPTIONAL-clause-sibling
  demotion" — would need clause-group tracking in the parser or the
  resolver. It is not in ADR 0009's R4/R5/R6/R7 roster; if raised,
  it is a post-R7 refinement that would need its own ADR to justify
  the model addition.

The bead description will be updated at the R4 close-out to reflect
the R4 scope: "regime (a) demotes edge endpoints only; sibling
demotion within an OPTIONAL clause is a separate, later refinement".
This spec does not commit to the bead revision — that lives in beads
workflow.

The `demote_chained_from_required.cypher` fixture (§6.3) encodes the
spec's decision (`a` stays nullable, `b` demotes) and pins the
under-approximation as a testable outcome. If a future stage widens
the flow-typing rule, that fixture's golden updates then.

---

## 8. Ground-truth cross-check

Every factual claim in this spec is verifiable against source; the
citations below name the file:line the claim rests on. A future
reader should be able to open each citation and confirm the shape
the spec describes still holds.

- **`NodeBinding.Nullable()`** — `internal/query/query.go:354-356`
  ("Nullable reports whether the binding was first introduced inside
  an OPTIONAL"). Read once by R4 §4.3; static per ADR 0006.
- **`EdgeBinding.Nullable()`** — `internal/query/query.go:462-464`.
  Read once by R4 §4.3; static per ADR 0006.
- **`Endpoint` sealed sum (`VarEndpoint`, `InlineEndpoint`)** —
  `internal/query/query.go:939-979`; §4.4.2's endpoint walk switches
  on this sum.
- **`VarEndpoint.Variable()`** — `internal/query/query.go:960`
  (accessor for the endpoint's variable name).
- **`EdgeBinding.Hops()`** — `internal/query/query.go:449-452`;
  §4.4.3 reads `Min()` off the returned `*EdgeHops`.
- **`EdgeHops.Min()`** — `internal/query/query.go:500-501` (returns
  `*int`; `nil` = unbounded lower). §4.4.3's zero-min exclusion
  reads `*Min()`; the `nil` case is treated as `1` per openCypher.
- **`Part.Bindings`** — `internal/query/query.go:81-123` (declaration
  at 90); §4.4.2 iterates in the parser's first-appearance order
  (guaranteed by the builder).
- **Parser merges same-part re-MATCH into one binding, keeping
  first-introduction `Nullable()`** —
  `internal/query/cypher/pattern.go:373-401` (`mergeBinding` and its
  docstring: "optional is honoured only on first introduction (ADR
  0006): a binding's nullability is a static fact about its
  *introducing* clause; a later non-OPTIONAL occurrence neither sets
  nor clears the flag — that demotion is the resolver's job
  (gqlc-lqm)"). The behaviour justifies §7.4's regime-(b) deferral to
  R5.
- **Parser accepts OPTIONAL MATCH via `OPTIONAL()` axis on
  `oC_Match`** — `internal/query/cypher/listener.go:261-270`
  (`EnterOC_Match` reads `c.OPTIONAL() != nil` and threads `optional`
  through `collectPattern`).
- **Parser lowers OPTIONAL MATCH to `NewNullableNodeBinding` /
  `NewNullableEdgeBinding` / `NewNullableVarLengthEdgeBinding`** —
  `internal/query/cypher/build.go:258-286` (`toBinding` dispatch on
  `rb.nullable`).
- **Parser test pins `OPTIONAL MATCH (n) RETURN n` → single
  `NewNullableNodeBinding("n", nil)`** —
  `internal/query/cypher/parser_test.go:320-333`.
- **Parser test pins `MATCH (n) OPTIONAL MATCH (n)-[:NOT_EXIST]->(x)
  RETURN n, x` → `n` non-nullable, `x` and edge nullable** —
  `internal/query/cypher/parser_test.go:339-356`.
- **R2 unification lattice: two `ResolvedProperty`s with different
  `Nullable` bits do not unify** — R2 §4.8 (`resolve.go:665-671`,
  `unify` case `ResolvedProperty`); §4.6 records this stays
  unchanged.
- **R3 kernel gate rejects multi-part queries** —
  `internal/resolver/resolve.go:28-30` (`WITH / multi-part query`
  fail-msg). R4 keeps this gate — §4.1 confirms.
- **R3 kernel gate rejects writes, CALL, Distinct, ReturnsAll** —
  `internal/resolver/resolve.go:33-44`. R4 keeps these gates.
- **R3 `refProjectionType` emits `ResolvedNode` / `ResolvedEdge` /
  `ResolvedEdgeUnion` / `ResolvedProperty` — the four sites R4
  widens** — `internal/resolver/resolve.go:464-514`. §4.5 revises
  these emits to consult `nullableBinding`.
- **R3 `propertyUseWitness` emits `ResolvedProperty` — the fifth
  site R4 widens** — `internal/resolver/resolve.go:627-652`. §4.6
  revises.
- **R3 `unionProperty` emits `ResolvedProperty` on the happy path
  — the sixth site R4 widens** — `internal/resolver/resolve.go:519-537`.
  §4.5 revises with a `bindingNullable` argument.
- **`ResolvedNode` shape (R0)** —
  `internal/resolver/validated.go:77-94`. §3.1 widens.
- **`ResolvedEdge` shape (R1)** —
  `internal/resolver/validated.go:124-143`. §3.2 widens.
- **`ResolvedEdgeUnion` shape (R3)** —
  `internal/resolver/validated.go:145-170`. §3.3 widens.
- **`ResolvedProperty` shape (R0)** —
  `internal/resolver/validated.go:96-122`. §3.4 widens semantic
  only; shape unchanged.
- **`allSentinels` list (R3)** —
  `internal/resolver/errors.go:63-75`. §5.1 confirms unchanged at
  R4.
- **`schema.Property.Nullable`** —
  `internal/schema/schema.go:42-46`. Source of the schema-side
  nullable bit §3.4 disjuncts with the binding-side.
- **ADR 0006 — parser records `Nullable` as a static introduction
  fact; flow-typing belongs to the resolver** —
  `docs/adr/0006-nullability-parsed-static-flow-typing-in-resolver.md`.
- **ADR 0006 §Consequences — "two flow-typing regimes, with
  different model dependencies"** —
  `docs/adr/0006-nullability-parsed-static-flow-typing-in-resolver.md`
  lines 49-55. Regime (a) is Stage-2-model-alone-shippable; regime
  (b) needs Stage-4 clause structure — §7.4 uses this as R4/R5 scope
  boundary.
- **ADR 0009 R4 line** —
  `docs/adr/0009-resolver-test-first-staged-build.md` line 123-125.
- **Parser Stage 4 spec §3 — cross-`WITH` vs no-`WITH` regime-(b)
  distinction** —
  `docs/specs/cypher-query-parser-stage-4.md:171-187`. §7.4 relies
  on this.
- **Cypher parser rejects unbound variables at build time** —
  `internal/query/cypher/build.go:157`. R4 does not admit any new
  binding; the parser's guarantee still holds.

---

## 9. Definition of done for R4 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is
out of scope of this document. The spec is done when:

1. This file exists at `docs/specs/resolver-stage-r4.md`, committed
   on branch `resolver-r4-spec`.
2. §3 records the three widened variants (`ResolvedNode`,
   `ResolvedEdge`, `ResolvedEdgeUnion`) with their new `Nullable`
   axis and wire encoding, plus §3.4's semantic widening of
   `ResolvedProperty.Nullable` and §3.5's element-level flow
   through `ResolvedList{Element}`.
3. §4 gives the algorithm for R4 regime (a): the effective-nullability
   table (§4.3), the demotion rule per resolved edge shape (§4.4.1),
   the fixed-point / single-pass loop (§4.4.2), and the var-length
   zero-min exclusion (§4.4.3); §4.5-§4.6 describe how the
   projection / parameter walks consume the table.
4. §5 confirms zero new sentinels and defends the closed R3 sentinel
   set as sufficient for R4.
5. §6 designs the fixture set: the R4 valid schema `social_r4.gql`
   (byte-copy of `social_r3.gql`), the R4 valid fixture list (~14
   fixtures each keyed to an R4 arm), the R4 invalid fixture list
   (one new fixture — the OPTIONAL+WITH multi-part reject), the
   revised `invalidFixtures` map, the golden-rebaseline plan (§3.6,
   §6.6), and the R4 harness invariants.
6. §7 states the R4 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel each construct routes
   to and the R-stage that owns the next widening. §7.4 defends
   regime (b)'s deferral to R5 with source citations. §7.5 records
   the disagreement with the bead example.
7. §8 cross-checks every factual claim against source file:line.
8. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer);
every blocker he raises is fixed on this same branch before the
branch merges. Cycle 2 (the R4 code cycle) begins only when the
spec cycle merges.
