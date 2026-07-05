# Stage R3 spec — resolver: edge semantics (undirected orientation, multi-type, var-length)

The implementation brief for Stage R3 of `internal/resolver`, extending the
merged R0/R1/R2 kernel (`docs/specs/resolver-stage-r0.md`,
`docs/specs/resolver-stage-r1.md`, `docs/specs/resolver-stage-r2.md`) with the
three edge shapes ADR 0009 assigns to R3: **undirected two-orientation trial
against the directed-only schema, multi-type edges as one candidate `EdgeKey`
per type (cross-product with the orientation trial), and variable-length
edges as `EdgeKey` lookup with a hop range producing `list<edge>` results**.
Build this **test-first**. Scope, sequencing, error posture, `ValidatedQuery`
top-level shape, purity, and the golden-pair harness inherit from ADR 0009,
R0/R1/R2 unchanged; this document revises only the kernel arms, the
`ResolvedType` sum, the sentinel set, and the out-of-scope table entries
that R3 changes.

Stage R3 lowers a labelled single-`MATCH` pattern (as R2 admits it) that
also carries **undirected edge bindings** (`Directed() == false`),
**multi-type edge bindings** (`len(Labels()) > 1`), and **variable-length
edge bindings** (`Hops() != nil`), together with every combination thereof.
Nullability flow-typing (R4), WITH/UNION/multi-part/`RETURN *` (R5),
writes (R6), CALL/YIELD (R7), and untyped edges (still R-later) stay out of
scope and continue to route to `ErrOutOfR0Scope` (unchanged name;
category-grained per R0 §5).

This spec also decides the **double-match question** on bead `gqlc-int`
(§4.6 and §3.5) — recorded on parser Stage 5 §9 as "the open resolution
contract the undirected marker implicitly hands off" — and pins the shape
of orientation reporting (§3.2). The parser side (`query.Query`) is frozen
by ADR 0008; every R3 addition lands on `ValidatedQuery`, which stays
provisional through R7 (ADR 0009).

---

## 1. Deliverables

- `internal/resolver/validated.go` — extended with one new `ResolvedType`
  variant, **`ResolvedEdgeUnion`** (§3.2), carrying an ordered slice of
  `schema.EdgeKey`s for the multi-candidate multi-type / mixed-orientation
  case. R0/R1/R2 variants (`ResolvedNode`, `ResolvedProperty`,
  `ResolvedEdge`, `ResolvedScalar`, `ResolvedTemporal`, `ResolvedList`,
  `ResolvedUnknown`) are unchanged in wire form.
- `internal/resolver/errors.go` — extended with one new sentinel,
  **`ErrAmbiguousEdgeOrientation`** (§5.1), and revised prose on
  `ErrUnknownProperty` and `ErrOutOfR0Scope` (§5.2). R0/R1/R2 sentinels
  keep their identity; wrapped-message sets widen only where recorded.
- `internal/resolver/resolve.go` — extended with:
  - a **candidate-set** helper (`edgeCandidates`, §4.4) that materialises
    the labels × orientations × schema cross-product for one edge
    binding and closes each candidate against `schema.Schema.Edges`;
  - a revised **Phase A2** (§4.3) that admits every R3 shape and routes
    the candidate set through the new helper, replacing `r1EdgeAdmissible`;
  - a revised **Phase B** (§4.5) whose candidate collection now iterates
    the multi-type × two-orientation cross-product per touching edge;
  - a revised **Phase C** (§4.6) that emits the R3 decision on the
    candidate set (single, union, ambiguity, zero);
  - a revised **projection walk** (§4.7) that dispatches on the binding's
    hops axis and multiplicity to emit `ResolvedEdge`, `ResolvedEdgeUnion`,
    `ResolvedList{Element: ResolvedEdge|ResolvedEdgeUnion}`, or an
    error;
  - a revised **property lookup on a multi-candidate edge** (§4.8) that
    demands per-member type/nullability agreement.
- `test/data/resolver/valid/schemas/` — one new schema fixture
  (`social_r3.gql`, §6.2) authored so every R3 arm — orientation
  single-match, orientation double-match (rejected), multi-type union,
  var-length list<edge>, and every cross-product — has a fixture that
  witnesses it.
- `test/data/resolver/valid/*.cypher` and `.validated.golden.json` —
  new R3 valid fixtures (§6.3), each paired with its schema through the
  updated `schema.mapping.json`.
- `test/data/resolver/invalid/*.cypher` — new R3 invalid fixtures for
  the new sentinel and the retired-at-R3 `ErrOutOfR0Scope` sub-cases
  (§6.4). R2's `undirected_edge.cypher`, `var_length_edge.cypher`, and
  `multi_type_edge.cypher` retire — they are R3 valid inputs; the R2
  invalid files are moved / renamed (§6.4).
- `internal/resolver/resolver_test.go` — updated `invalidFixtures` map
  (§6.4). No structural changes to `TestValid` / `TestInvalid` /
  `TestSentinelReachability` are required; the R0/R1/R2 harness scales
  as-is.

Nothing downstream of the resolver is built — `ValidatedQuery` is
provisional through R7 (ADR 0009 §Decision).

---

## 2. Architecture — deltas from R2

R0/R1/R2's architecture stands (the `Resolver` struct, its compile-time
inputs, `QueryResolver` interface + compile-time assertion, purity and
short-circuit posture, `resolve.go`/`Resolve` split, the three-phase
kernel A1 / A2 / B / C). R3 changes the *content* of Phases A2, B, and C
— the shape and closure rules for the candidate set — plus the
projection walk. No new exported types beyond `ResolvedEdgeUnion` and
the sentinel. The seam does not move.

### 2.1 The R3 kernel structure

The kernel remains one linear pass with early returns. R3 keeps R2's
step ordering:

1. Query-level gating (unchanged, R0 §4.7 step 1 / R2 §4.1).
2. Phase A1 — labelled node bindings (unchanged, R1 §4.2).
3. Phase A2 — **all** edge bindings admitted by the R3 predicate (§4.2)
   have their candidate set computed via §4.4 and closed via §4.6 when
   both endpoints are labelled; edges with a still-pending unlabelled
   `VarEndpoint` are deferred to Phase C.
4. Phase B — unlabelled-node inference (revised, §4.5) walks every
   R3-admitted touching edge and computes candidate-endpoint sets over
   the label × orientation cross-product.
5. Phase C — closure of deferred edges (unchanged in structure, revised
   internally, §4.6) reads the now-committed endpoint table and calls
   the same §4.4 helper, then applies §4.6's verdict.
6. Projection walk (revised, §4.7) dispatches on the binding's hops
   axis and its committed candidate multiplicity.
7. Parameter walk (unchanged, R2 §4.3).
8. StatementKind copy (unchanged, R0 §4.7 step 6).

The kernel invariant is the R1 one: labelled bindings' resolutions are
committed before inference reads them; Phase B commits every
Phase-B-resolvable node binding before Phase C closes deferred edges.

### 2.2 Kernel helpers — one new, two revised

Three helpers in `resolve.go`:

- **`edgeCandidates(e query.EdgeBinding, src, tgt graph.LabelSetKey,
  s schema.Schema) []schema.EdgeKey`** (new). Enumerates the closed
  candidate set for one edge binding whose endpoints are already
  committed: it forms **one candidate `EdgeKey` per (label, orientation)
  pair** and returns only the pairs the schema declares. Ordering is
  deterministic — see §4.4. Callable from both Phase A2 (labelled
  endpoints) and Phase C (Phase-B-inferred endpoints); the caller
  supplies the endpoint keys. The candidate set is closed against the
  schema at construction: an entry in the return slice is *by
  construction* a key present in `s.Edges`.

- **`closeEdge`** (revised). R1's `closeEdge` formed a single candidate
  `EdgeKey`, looked it up, and either recorded or failed with
  `ErrUnknownEdge`. R3's `closeEdge` calls `edgeCandidates`, applies the
  §4.6 verdict (zero → `ErrUnknownEdge`; one → `ResolvedEdge`; two+ →
  `ResolvedEdgeUnion` for multi-type, `ErrAmbiguousEdgeOrientation` for
  single-type undirected), and records the resolved shape against the
  binding's variable — as a new companion table
  (`resolvedEdgeCand map[string][]schema.EdgeKey`) alongside R1's
  `resolvedEdgeType map[string]schema.EdgeType` and `resolvedEdgeKey
  map[string]schema.EdgeKey`. The two R1 tables continue to be
  populated in the single-candidate case (so R1/R2 goldens keep their
  wire shape); the new table is consulted only when
  `len(resolvedEdgeCand[v]) > 1` (§4.8).

- **`candidateTypes`** (revised, §4.5). R1's implementation walks the
  R1-supported edges (single-type directed single-hop). R3's widens
  the walk to the R3-supported edges — every R3 shape contributes to
  inference — and, for a touching edge, iterates *every* (label,
  orientation) pair against the schema, unioning per-label matches
  into that edge's contribution before intersecting across touching
  edges (see §4.5.2 for why the union-across-labels-per-edge rule is
  the right one, not intersection).

### 2.3 Purity, determinism, short-circuit — unchanged

R0 §2.3 stands verbatim. R3 introduces no goroutine, no map iteration
that escapes into the output, no time source. Every helper's return
order derives from parser-guaranteed ordering (`Part.Bindings`,
`EdgeBinding.Labels()`) plus a small deterministic sort where
schema-map iteration would otherwise leak (see §4.4). `edgeCandidates`
returns a deterministic slice; `resolvedEdgeCand[v]` is that slice
verbatim; `ResolvedEdgeUnion` marshals it in place. Two resolves of the
same query produce byte-identical goldens.

---

## 3. `ValidatedQuery` — the R3 shape

`ValidatedQuery`'s top-level shape (R0 §3.1) is unchanged at R3:
`Columns`, `Parameters`, `Statement`. The extension is on the
`ResolvedType` sum — one new variant and a widened reuse of
`ResolvedList`.

### 3.1 `ResolvedEdge` — unchanged wire shape, extended reuse

`ResolvedEdge{EdgeKey}` (R1 §3.1) is unchanged in wire form and remains
the emit for **every single-candidate case**:

- a directed single-hop single-type edge (R1's original producer);
- a directed single-hop multi-type edge whose schema declares exactly
  one matching `EdgeKey` (multi-type in the query, single match in the
  schema);
- an undirected single-hop single-type edge whose schema declares
  exactly one orientation (single-match undirected — the R3 happy
  path);
- an undirected single-hop multi-type edge whose schema declares
  exactly one matching (label, orientation) pair.

In every single-match case, `ResolvedEdge.EdgeKey` records the
**committed orientation** — the `EdgeKey`'s `Source`/`Target` fields
name which orientation matched. R3 does not add a separate "which
orientation matched" axis: the `EdgeKey` triple already carries that
information, and consumers of a resolved `ResolvedEdge` need no
further discrimination.

**Rejected alternative: add a `Committed graph.LabelSetKey` axis to
`ResolvedEdge` marking the query's textual source endpoint.** Two
reasons to reject: (a) the `EdgeKey.Source` already fixes the schema
orientation; adding a "textual source" would only tell codegen which
of the query's two endpoints in `-[r]-` was on the left, which
codegen already knows because ADR 0005 makes it re-emit the original
text; (b) it would break R1's wire shape without buying any new
information.

### 3.2 `ResolvedEdgeUnion` — the multi-candidate variant

```go
// ResolvedEdgeUnion is a multi-candidate edge whole-entity projection:
// the closed set of schema EdgeKeys the resolver committed for a
// multi-type edge binding whose labels (or the label × orientation
// cross-product for an undirected multi-type edge) resolve to more than
// one edge in the schema. Produced by R3 for a RefProjection whose Ref
// names an EdgeBinding with a multi-candidate committed set (§4.6). The
// single-candidate case stays ResolvedEdge (§3.1). EdgeKeys is
// deterministic — the ordering is per §4.4 — and non-empty; the empty
// case is ErrUnknownEdge, the single case is ResolvedEdge.
type ResolvedEdgeUnion struct {
    EdgeKeys []schema.EdgeKey `json:"edgeKeys"`
}

// String is the wire tag "edgeUnion".
func (ResolvedEdgeUnion) String() string { return "edgeUnion" }

// MarshalJSON emits a tagged-union object with a "kind" discriminator
// plus the ordered candidate slice.
func (u ResolvedEdgeUnion) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind     string           `json:"kind"`
        EdgeKeys []schema.EdgeKey `json:"edgeKeys"`
    }{Kind: u.String(), EdgeKeys: u.EdgeKeys})
}

func (ResolvedEdgeUnion) isResolvedType() {}
```

**Producer surface.** `ResolvedEdgeUnion` is produced only in the
multi-candidate case, exactly per the ADR 0009 R3 line ("multi-type
edges as one candidate key per type") extended with the orientation
cross-product for undirected patterns. `len(EdgeKeys) >= 2` is a
constructor invariant (the single-candidate case stays `ResolvedEdge`,
the zero-candidate case fails `ErrUnknownEdge`, the ambiguous-single-
type-undirected case fails `ErrAmbiguousEdgeOrientation` — see §4.6's
verdict table).

**Ordering.** `EdgeKeys` is emitted in **§4.4's canonical order**: outer
loop iterates `e.Labels()` in first-appearance order (parser Stage 8's
LabelSet slice); inner loop fixes orientation (`(src,tgt)` before
`(tgt,src)` when both apply). Determinism is a golden-file
precondition; schema-map iteration order is not.

**Rejected alternative: always use a list.** R1's `ResolvedEdge{EdgeKey}`
would collapse to `ResolvedEdge{EdgeKeys: []EdgeKey{one}}` — every R1
and R2 golden would rebaseline. The churn buys nothing over a small
sum-widening; a tagged-union consumer already dispatches on `kind`, so
the extra variant is one more `case`.

**Rejected alternative: reject multi-match by policy.** Refuse to
resolve a multi-type edge that finds more than one schema
`EdgeKey`. That folds the ADR 0009 R3 remit's "multi-type edges as one
candidate key per type" into "at most one candidate key per type"
without ADR support. The author who writes `[r:A|B]` and whose schema
declares both `A` and `B` between the endpoints is asking a legitimate
union question; the resolver has enough information to type it. Force-
choosing one collapses information; erroring on multi-match forbids a
useful shape.

### 3.3 `ResolvedList` — var-length whole-entity

R2's `ResolvedList{Element ResolvedType}` (R2 §3.3) is unchanged in
wire form. R3 introduces one new element type: `ResolvedEdge` or
`ResolvedEdgeUnion`. A `RefProjection` naming a variable-length edge
binding emits `ResolvedList{Element: <committed edge shape>}` — the
element mirrors the committed candidate set with the same
single-vs-union verdict rules (§4.7 and §4.8).

The element is *always* an edge kind (`ResolvedEdge` or
`ResolvedEdgeUnion`) for a var-length edge binding — never a scalar or
property. Var-length in openCypher / the Cypher parser produces a
`list<edge>` result type; the resolver's job is to type-refine the
element via the schema witness. Property projection on a var-length
edge binding (`r.publishedAt` where `r` is var-length) is *not*
admitted at R3 (§7 out-of-scope table); it routes to
`ErrOutOfR0Scope` because the semantics of "one property from a list
of edges" is a list-element operation (R5 grouping / UNWIND in R5 or
later) rather than a schema witness.

### 3.4 `ResolvedProperty` — extended reuse

Unchanged in shape. R3 extends its reach only marginally: an edge
whose committed shape is `ResolvedEdgeUnion` still admits property
projection under the §4.8 uniform-property rule (every union member's
schema must declare the property with structurally-equal `Type` and
`Nullable`; otherwise `ErrUnknownProperty`).

### 3.5 The double-match decision, recorded

For an **undirected single-type single-hop edge** whose two candidate
`EdgeKey`s both resolve against the schema (`s.Edges` contains both
`{A, L, B}` and `{B, L, A}` as distinct entries), the resolver
**errors** with `ErrAmbiguousEdgeOrientation` (§5.1). The
fail-message lists both matched keys and names the offending binding
variable. The decision and its considered alternatives are recorded
in §4.6's verdict table and its rationale block. The resolved shape
of every other case (single-match, zero-match, multi-type,
var-length) is determined without ambiguity error; the only
double-match arm that raises `ErrAmbiguousEdgeOrientation` is the
single-type single-hop undirected × double-schema-match arm — every
other multi-candidate case emits `ResolvedEdgeUnion`.

The consequence for `ValidatedQuery` shape: `ResolvedEdge` retains a
single `EdgeKey`, and the "which orientation matched" question is
answered by inspecting that key's `Source` and `Target`. No orientation
axis is added to the model.

### 3.6 Wire-encoding invariants

Every new variant emits a tagged-union JSON object with a `"kind"`
discriminator — the sealed-sum convention `query.Type` and R0/R1/R2
`ResolvedType` already use. The wire tag:

| Variant | `"kind"` | Extra fields |
|---|---|---|
| `ResolvedEdgeUnion` | `"edgeUnion"` | `"edgeKeys"` (deterministically-ordered array of `schema.EdgeKey`) |

The `"kind"` discriminator is disjoint across every `ResolvedType`
variant.

---

## 4. The R3 kernel algorithm

Each step below extends or replaces a numbered step of R1 §4 / R2 §4.
Step 1 (query-level gating), Phase A1 (labelled node bindings), Step 6
(statement kind), and the parameter walk (R2 §4.3) are unchanged.

### 4.1 Steps 1 and 6 (unchanged)

R1 §4.1 (query-level gating) and R0 §4.7 step 6 (StatementKind copy)
stand verbatim. `Part.Distinct` / `Part.ReturnsAll` still route to
`ErrOutOfR0Scope`. Write-scope Effects still route to
`ErrOutOfR0Scope`.

### 4.2 Edge admissibility — the R3 predicate

R1's `r1EdgeAdmissible` retires. R3's admissibility gate keeps only the
"untyped edge" refusal:

```go
// r3EdgeAdmissible screens an EdgeBinding against R3's edge shape
// predicate: labelled (at least one type). Every R3 shape — directed
// or undirected, single-hop or variable-length, single-type or
// multi-type — is admitted here; the R3 kernel's Phase A2, B, and C
// take over the schema-side closure. Untyped edges (len(Labels()) == 0)
// route to ErrOutOfR0Scope: they are the last edge shape ADR 0009 does
// not name at R3, and their candidate set (every schema edge whose
// endpoints match) is an R-later stage's business.
func r3EdgeAdmissible(e query.EdgeBinding) error {
    if len(e.Labels()) == 0 {
        return fmt.Errorf("%w: untyped edge", ErrOutOfR0Scope)
    }
    return nil
}
```

R3 admits directed/undirected × single-hop/var-length × single-type/
multi-type — the full 2×2×2 R3-scope matrix — as one admissibility
gate. Untyped edges (still R-later) stay rejected here.

### 4.3 Phase A2 (revised) — candidate-set formation for labelled edges

R1's Phase A2 formed one candidate key per edge and closed it. R3's
Phase A2 walks `Part.Bindings` again, and for each `EdgeBinding` that
passes §4.2:

- Derive both endpoint keys via `endpointLabels` (R1 §2.2). If either
  is not yet resolved (`ok == false`), defer the edge to Phase C.
- With both endpoint keys committed, call `edgeCandidates` (§4.4) to
  build the closed candidate set, then apply §4.6's verdict.

`endpointLabels` is unchanged from R1. The single-orientation trial
of R1 (source-side and target-side keys read once) still holds — the
orientation *trial* is inside `edgeCandidates`, not `endpointLabels`.

### 4.4 `edgeCandidates` — the labels × orientations × schema cross-product

Signature and semantics:

```go
// edgeCandidates enumerates the closed candidate set for one edge
// binding whose endpoint keys are already committed. It iterates the
// binding's Labels() in first-appearance order (parser guarantees this
// via LabelSet's slice-backed representation — iterate with a plain
// `for _, L := range e.Labels()`); for each label it emits one
// candidate EdgeKey per orientation admitted by the binding's
// Directed() marker (one for a directed edge, two for an undirected
// edge). Each candidate is retained iff the schema declares it
// (present in s.Edges). The return slice is deterministically
// ordered: outer loop label-first-appearance, inner loop orientation
// (source->target before target->source when both apply). Duplicate
// EdgeKeys are impossible by construction — the same (label,
// orientation) pair generates the same key exactly once.
func edgeCandidates(
    e query.EdgeBinding,
    src, tgt graph.LabelSetKey,
    s schema.Schema,
) []schema.EdgeKey
```

**Loop shape.**

```
for each label L in e.Labels():        # first-appearance order
    tryOrientations := [(src, tgt)]    # directed default
    if !e.Directed():
        tryOrientations = [(src, tgt), (tgt, src)]
    for each (S, T) in tryOrientations:
        k := schema.EdgeKey{Source: S, Label: graph.LabelSet{L}.Key(), Target: T}
        if _, ok := s.Edges[k]; ok:
            append k to result
```

- **Directed × single-type**: one iteration → one candidate key
  attempted → at most one match.
- **Directed × multi-type** (N labels): N iterations, one candidate
  attempted each → up to N matches.
- **Undirected × single-type**: one label, two orientations
  attempted → up to two matches (the double-match case, §4.6 verdict).
- **Undirected × multi-type** (N labels): N × 2 candidates attempted
  → up to 2N matches.

`edgeCandidates` never returns duplicates: an `(A, L, B)` key can be
generated at most once because the outer loop iterates each label once
and the inner loop attempts each orientation once per label.

**Determinism.** Ranging `e.Labels()` iterates in first-appearance
order (the underlying `[]string` slice, populated in textual order per
`internal/graph/labelset.go` and parser Stage 8). The orientation
inner loop is fixed at `(src, tgt)` then `(tgt, src)`. Both orders
are stable across runs; the return slice is deterministic.

**Var-length interaction.** `edgeCandidates` does not read `e.Hops()`
— the hop range is a runtime axis (ADR 0005: the original text runs),
not a schema-side one. Var-length edges use the same candidate set as
single-hop edges. The projection walk (§4.7) reads `e.Hops()` to
decide whether to wrap the element in `ResolvedList`.

### 4.5 Phase B (revised) — inference over the R3-widened touching set

Phase B (R1 §4.3) walks each pending unlabelled `NodeBinding` and
computes candidates against the touching edges. R3 revises Phase B's
per-edge contribution:

**4.5.1. Widened touching set.** Every R3-admitted edge (directed or
undirected, single-hop or var-length, single-type or multi-type)
contributes to Phase B. `touchingSide` (R1 §4.3) is unchanged — an
edge touches a binding via a `VarEndpoint` naming the binding.

**4.5.2. Per-edge candidate contribution — union across labels,
union across orientations.** For an unlabelled pending binding `n`
and a touching edge `e`:

- Determine `n`'s side (source or target of `e`, or *both* sides
  when `n` sits at both endpoints of the same edge — e.g. a
  self-loop). If both sides are `n`, the two-orientation-plus-labels
  cross-product still applies; the candidate contribution is the
  union of {source-side keys} and {target-side keys} against the
  edge's other side, which is `n` itself — meaning the label-side
  cross-product is trivially `{k.Source, k.Target} ∈ s.Edges` with
  `k.Label ∈ { graph.LabelSet{L}.Key() | L ∈ e.Labels() }` (the set
  of single-label keys derived from the edge binding's labels).
  R3 admits self-loops iff the schema
  has a self-loop edge type; the candidate collection is uniform
  with the non-self-loop case (see the self-loop fixture in §6.3).
- Read the other endpoint's key via `endpointLabels`; skip the edge
  if the other endpoint is unlabelled (an edge with two unlabelled
  endpoints contributes nothing to either — cannot constrain).
- For each label `L` in `e.Labels()` (first-appearance order):
  - For each orientation admitted by `e.Directed()`:
    - Query `s.Edges` for keys with matching `Label == L.Key()` and
      the *other* endpoint on the appropriate side; the candidate
      for `n` is `k.Source` (n on source side) or `k.Target` (n on
      target side).
- Union all matched candidates for `e` into a single per-edge
  contribution set.

**4.5.3. Intersect across touching edges.** As in R1: `n`'s
candidate set is the intersection of per-edge contributions from
every touching edge. Empty intersection with at least one touching
edge → `ErrUnknownLabel` (verdict-0, unchanged from R1 §4.3);
singleton intersection → commit; multi-set with pending remaining
→ next pass; zero-commit pass with pending → `ErrAmbiguousBinding`
(unchanged).

**4.5.4. Why union-across-labels-per-edge, not intersection.** In
R1 the per-edge contribution came from a single label — no
per-label choice to make. In R3, a multi-type edge `[:A|B]` between
`n` and a labelled endpoint declares "n is on the other side of an
A OR a B edge to this endpoint" — the union is the honest reading
of the `|` operator. Intersection ("n is a schema type that appears
on the other side of BOTH an A edge AND a B edge") would model the
author's edge annotation as a conjunction, which contradicts
Cypher semantics. Undirected × multi-type further widens the
per-edge union across the two-orientation trial — same rationale.

Phase B commits a node label from an orientation union before
Phase C decides the edge's verdict; when Phase C later returns
`ErrAmbiguousEdgeOrientation` on that edge (§4.6 case C), the
node commit is discarded with the query — the phase ordering
(§2.1) makes this safe: no persistent state escapes a failed
resolve.

### 4.6 Phase C (revised) — verdict on the closed candidate set

Phase C reads the now-complete node table, re-forms `edgeCandidates`
for every deferred edge, and applies the **verdict table**:

| Case | Candidate-set size | Query edge shape | Verdict |
|---|---|---|---|
| A | 0 | any | `ErrUnknownEdge` (fail-msg lists tried (label, orientation) pairs) |
| B | 1 | any | `ResolvedEdge{EdgeKey: cands[0]}`; record in `resolvedEdgeType` / `resolvedEdgeKey` |
| C | ≥ 2 | `!e.Directed() && len(e.Labels()) == 1` (single-type undirected) | **`ErrAmbiguousEdgeOrientation`** (§5.1); fail-msg lists both matched keys |
| D | ≥ 2 | any other R3 shape (multi-type; multi-type × undirected; directed multi-type) | `ResolvedEdgeUnion{EdgeKeys: cands}`; record in `resolvedEdgeCand` |

**Rationale — case C (the double-match decision).** The candidate set
of size 2 for a **single-type undirected edge** arises exactly when
the schema declares both `{A, L, B}` and `{B, L, A}` as *distinct*
edge types with the same label. In every practical schema this is a
modelling choice with meaning — the two directions are distinct
concepts (`Person → FOLLOWS → Person` where the reciprocal
`Person → FOLLOWS → Person` in the other direction is also declared:
the same key, so there is no ambiguity). The genuine double-match
case requires distinct source/target label sets in the two
directions (`Author → REVIEWED → Book` and `Book → REVIEWED →
Author`, if a schema authored that pair). Under those conditions,
the author's undirected `[:REVIEWED]` is genuinely ambiguous: does
the pattern refer to authors reviewing books, or books reviewing
authors? The resolver cannot infer intent; it forces the author to
disambiguate by writing a directed arrow, and it says so.

**Rationale — case D (multi-type is union).** The `|` operator in
`[r:A|B]` is Cypher's union-of-edge-types syntax. An author who
writes it is *asking* the resolver to accept whichever declared
type(s) the schema supplies. Emitting `ResolvedEdgeUnion` honours the
author's stated intent; erroring on multi-match would contradict
the operator. The same posture extends to undirected multi-type:
if the author writes `-[:A|B]-` and the schema supplies four
matching (label, orientation) pairs, all four join the union — the
author already opted into the union semantics by writing `|`.

**Rejected verdicts considered and recorded.**

- **Verdict on case C: silently union.** Rejected — the schema
  distinguishes the two orientations for a reason; silently
  collapsing them into a union would erase the schema's declared
  asymmetry. The author's undirected marker has no `|` — no
  union-opt-in — so unioning would be a modeller's surprise.
- **Verdict on case C: pick the first orientation and warn.**
  Rejected — the resolver has no warning channel (short-circuit,
  one-verdict-per-query per ADR 0009 §Purity), and "the first
  orientation" is a lexical accident of how the author typed the
  pattern rather than a semantic pick.
- **Verdict on case C: emit `ResolvedEdgeUnion` and let the caller
  disambiguate.** Rejected — property projection on the union
  (§4.8) would then have to reconcile *the same property name* on
  edge types the schema deliberately declared as distinct kinds;
  a `publishedAt :: TIMESTAMP` on `Author → REVIEWED → Book` and a
  `publishedAt :: DATE` on `Book → REVIEWED → Author` cannot be
  unified, so property projection would fail anyway — with a
  worse diagnostic (property-conflict inside a union the author
  never asked for). Failing at binding time with
  `ErrAmbiguousEdgeOrientation` is the cleaner surface.
- **Verdict on case D: error on multi-match.** Rejected — see
  case D rationale.
- **Add a "which orientation(s) matched" axis to `ResolvedEdge`.**
  Rejected — the `EdgeKey` already carries `Source`/`Target`, so
  the winning orientation is inspectable from the resolved value.
  No new axis buys new information.

**Falsifiability.** Every arm of the verdict is witnessed by a fixture
(§6.3 for cases B and D, §6.4 for cases A and C). Case C is the
double-match arm: the fixture uses a schema that authors both
orientations and an undirected query pattern between them; the golden
is the `ErrAmbiguousEdgeOrientation` sentinel path.

### 4.7 Projection walk — hops-axis and multiplicity dispatch

The projection walk (R2 §4.2) is unchanged in outer shape: iterate
`Part.Returns` in source order; dispatch on the `Projection` sum;
`AggregateProjection` still `ErrOutOfR0Scope`. R3 revises only the
`RefProjection` arm's edge-binding handling; the node arm and every
non-`RefProjection` variant are R2-unchanged.

For a `RefProjection` whose `Ref.Variable` names an edge binding:

- **Whole-entity** (`Ref.Property == ""`):
  - If the binding is single-hop (`e.Hops() == nil`) and its
    candidate set is a singleton: emit
    `Column{Name, ResolvedEdge{EdgeKey: <the one key>}}` (R1 shape).
  - If the binding is single-hop and its candidate set has ≥ 2
    entries: emit `Column{Name, ResolvedEdgeUnion{EdgeKeys: <cands>}}`.
    (Note: case C — single-type undirected double-match — never
    reaches this arm because Phase C already returned
    `ErrAmbiguousEdgeOrientation`.)
  - If the binding is variable-length (`e.Hops() != nil`) and its
    candidate set is a singleton: emit
    `Column{Name, ResolvedList{Element: ResolvedEdge{EdgeKey}}}`.
  - If the binding is variable-length and its candidate set has ≥ 2
    entries: emit
    `Column{Name, ResolvedList{Element: ResolvedEdgeUnion{EdgeKeys}}}`.

- **Property lookup** (`Ref.Property != ""`):
  - If the binding is single-hop and single-candidate: R1's path —
    look up on the resolved `EdgeType.Properties`; miss →
    `ErrUnknownProperty` (R1 shape).
  - If the binding is single-hop and multi-candidate: apply §4.8's
    uniform-property rule.
  - If the binding is variable-length: return `ErrOutOfR0Scope`
    with fail-message `"property projection on variable-length
    edge binding: reach list elements via list-element access
    (UNWIND in R5 or later)"` (§7's scope statement records this).
    Rationale: a var-length edge projects as `list<edge>`, and
    `r.publishedAt` on a list has no scalar type — the semantics
    requires list-element access (`[i]`, UNWIND) that R5's
    grouping / carry-forward work owns.

### 4.8 Property lookup on a multi-candidate edge

For a `RefProjection` (or a `PropertyUse` inline parameter) whose
`Ref.Variable` names an edge binding with `len(resolvedEdgeCand[v])
>= 2`:

1. Look up `Ref.Property` on **every** union member's
   `schema.EdgeType.Properties` (keyed by the member's `EdgeKey` via
   `s.Edges[key]`). Any miss → `ErrUnknownProperty` (fail-message:
   `"property %s.%s missing on union member %s"`, naming the
   union member's `EdgeKey`).
2. Compare each hit's `(Type, Nullable)` to the first hit's. Any
   disagreement → `ErrUnknownProperty` (fail-message:
   `"property %s.%s type differs across union members: %s vs %s"`,
   naming the two disagreeing member keys and the two types).
3. All hits agree → emit `Column{Name, ResolvedProperty{Type,
   Nullable}}` (or `ResolvedParameter` for a `PropertyUse`).

**Same-sentinel-widened-message posture** (R2 §5.2). The property lookup
fail-modes are all "the schema does not declare this property with a
stable type on this edge binding"; a widened prose set on
`ErrUnknownProperty` keeps the sentinel set closed. Rejected
alternative: introduce `ErrHeterogeneousUnionProperty` for the
type-disagreement case. Reason to reject: adds a sentinel whose
usefulness is a fine-grained diagnostic — the fail-message already
distinguishes the two arms, and R-later can split the sentinel if a
consumer needs to branch on it.

**Rationale.** The union's property surface must be homogeneous
because the resolved column has *one* type. Codegen emits one column
of one wire shape; a union whose members disagree on a property has
no single resolved type. This is analogous to R2 §4.8's strict
`ResolvedProperty` unification for parameters — same posture, applied
to the schema side.

### 4.9 Parameter walk — inline-map on an R3 edge

R2's parameter walk (R2 §4.3, §4.6) is unchanged in shape. R3 extends
the property lookup for a `PropertyUse` inline-map parameter on an
edge binding to route through §4.8 when the edge's candidate set has
≥ 2 members. No new sentinel is needed — the existing
`ErrUnknownProperty` covers both arms, and the existing
`ErrParameterTypeConflict` still governs Uses-side unification once a
witness is committed.

### 4.10 Determinism and short-circuit

- `Part.Bindings` in first-appearance order (R1 §4.2 note).
- `Part.Returns` in source order (R0 §2.3).
- `Query.Parameters` in first-appearance order (R0 §2.3).
- `Parameter.Uses` in parser-mining order (R2 §4.9).
- `EdgeBinding.Labels()` in first-appearance order (parser Stage 8
  `LabelSet`; per `internal/graph/labelset.go` the label slice
  preserves textual insertion order).
- `edgeCandidates` return order: outer label first-appearance, inner
  orientation (source→target then target→source when both apply).
- `ResolvedEdgeUnion.EdgeKeys` is the `edgeCandidates` return slice
  verbatim.

Every ordered surface derives from a parser guarantee or a fixed
inner-loop order. Two resolves of the same query produce byte-
identical goldens.

Short-circuit posture is R0 §2.3 unchanged: the first error stops
resolution; the order of guard hits is deterministic given the ordered
walk above.

### 4.11 The revised type-mapping table — R3 owner column

R2 §4.10's twenty-row spine is the reference. R3 revises three rows
(the two `TypeList{TypeEdge}` sub-cases and the R3 owner column on
`TypeEdge`):

| Variant | Resolver counterpart | Classification | Owner (before R3) | Owner (R3) |
|---|---|---|---|---|
| `TypeEdge` | `ResolvedEdge` or `ResolvedEdgeUnion` | schema-upgraded | R1 | **R1 (single); R3 widens to include `ResolvedEdgeUnion` for multi-candidate** |
| `TypeList` (element `TypeEdge`) | `ResolvedList{Element: ResolvedEdge or ResolvedEdgeUnion}` | schema-upgraded | R2 (literal rejected; R3 widens) | **R3** (var-length edge binding) |
| `TypeList` (element `TypeNode`) | (rejected) | schema-upgraded | R5 (grouping) | R5 (unchanged) |
| `TypeList` (element other) | (R2 shape unchanged) | schema-upgraded | R2 | R2 (unchanged) |

The R2 rejection of `TypeList{TypeEdge}` in the projection walk
(R2 §4.5, §4.7) is retired *only* for the RefProjection-of-var-length-
edge path (§4.7's edge-binding dispatch above). A **literal list of bare
edge variables** — `RETURN [r] AS xs` where `r` is a single-hop edge
binding — still routes through `ExprProjection` (R2 §4.5) with
`p.Type() == TypeList{TypeEdge}`, and R2's `resolveType` mapper still
rejects it as `ErrOutOfR0Scope` (deferred to R5's grouping /
collect() path). The two producers of `TypeList{TypeEdge}` — parser
Stage 6 (`refType` for var-length, returning it from a RefProjection)
versus parser Stage 6 (`typeAtom` composed into `listLiteralType` for
a bare-var list literal in an ExprProjection) — remain distinguished
by the enclosing `Projection` variant, not by `TypeList` alone.

The table stays closed at R3: every variant of the frozen `query.Type`
sum still appears, each classified, each with an R-stage owner.
R4/R5/R6/R7 revise rows they take up.

---

## 5. Sentinels — the R3 revision

R0/R1/R2's six sentinels stand. R3 adds one and revises the message-set
of two more. The `allSentinels` list gains the new member; the
`invalidFixtures` map gains rows for the new sentinel; the reachability
sweep extends transparently.

### 5.1 New sentinel

```go
// ErrAmbiguousEdgeOrientation is returned when an undirected single-type
// single-hop edge binding's two-orientation trial matches TWO distinct
// EdgeKeys against the schema — the schema declares both
// {A, L, B} and {B, L, A} as distinct edge types with the same label,
// and the author's undirected pattern (which carries no `|` union-of-
// types opt-in) cannot commit to one without erasing the other.
// Introduced at R3. See §4.6's verdict-C rationale.
var ErrAmbiguousEdgeOrientation = errors.New("ambiguous edge orientation")
```

**Naming defence — `ErrAmbiguousEdgeOrientation`, not
`ErrDoubleOrientationMatch` or `ErrUndirectedEdgeAmbiguity`.** The
domain word for "the edge could go either way but the schema
declared both ways" is *orientation*; the marker on `EdgeBinding` is
called `Directed()` and its false state names the "no authoritative
orientation" fact (parser Stage 5 §9). `ErrDoubleOrientationMatch`
imports the algorithm's inner term (a "double match" is what the
resolver's `edgeCandidates` returns) — the R0 §5 rationale rejects
algorithm-leaks in sentinel names. `ErrUndirectedEdgeAmbiguity`
buries the *what* (orientation) inside a longer noun; the R3
sentinel is specifically about **orientation** ambiguity, not about
the whole binding, and the resolver already carries
`ErrAmbiguousBinding` for the whole-binding-is-ambiguous case (R1 §5).
`ErrAmbiguousEdgeOrientation` reads as "the edge's orientation is
ambiguous" — the closest domain-language rendering, and disjoint in
prefix from every other R0/R1/R2 sentinel so a `errors.Is` sweep
does not confuse two similar names.

### 5.2 Revised sentinels

- **`ErrUnknownProperty`.** Prose gains **two** new message shapes:
  - `"property %s.%s missing on union member %s"` — the multi-candidate
    edge case where the property exists on some but not all union
    members (§4.8 step 1).
  - `"property %s.%s type differs across union members: %s vs %s"` — the
    multi-candidate edge case where the property exists on every
    member but with disagreeing `Type` or `Nullable` (§4.8 step 2).
  Sentinel identity unchanged; `errors.Is(err, ErrUnknownProperty)`
  matches all three message shapes (missing on single-candidate
  edge from R2; missing on some member; disagreeing on all
  members).
- **`ErrOutOfR0Scope`.** Prose is revised to reflect the R3
  retirements (undirected, multi-type, and var-length edges no
  longer route here — they are R3 valid inputs) and the R3 addition:
  - `"property projection on variable-length edge binding: reach
    list elements via list-element access (UNWIND in R5 or later)"`
    — the property-on-list-of-edges case (§4.7).
  Untyped edges (`len(e.Labels()) == 0`) still route here at R3
  (R-later takes them up; §4.2's admissibility gate).

### 5.3 Not added at R3

- **`ErrHeterogeneousUnionProperty`.** The type-differs-across-
  union-members case reuses `ErrUnknownProperty` (§4.8); a
  dedicated sentinel is deferred to R-later if a consumer needs to
  branch on it.
- **`ErrVarLengthProperty`.** The var-length-property-projection
  case (§4.7) reuses `ErrOutOfR0Scope` (category-grained per R0 §5,
  R2 §5); a dedicated sentinel is unnecessary while the message
  disambiguates.
- **`ErrUnknownEdge` widening.** No new prose is added at R3 —
  the fail-message stays R1's `"unknown edge: %s-[%s]->%s"` shape
  for the zero-candidate case, extended to name every tried
  (label, orientation) pair when the trial had more than one
  attempt (multi-type × undirected). `errors.Is` matches
  unchanged.

### 5.4 The closed R3 set

```go
var allSentinels = []error{
    ErrUnknownLabel,             // R0; unchanged at R3
    ErrUnknownProperty,          // R0; message-set widened at R3 to union-member cases
    ErrOutOfR0Scope,             // R0; sub-cases shift at R3 (see §5.2)
    ErrUnknownEdge,              // R1; fail-message widens on multi-attempt trials
    ErrAmbiguousBinding,         // R1; unchanged at R3
    ErrParameterTypeConflict,    // R2; unchanged at R3
    ErrAmbiguousEdgeOrientation, // R3
}
```

Seven sentinels. Every member has at least one fixture (§6.4); every
fixture maps to a canonical sentinel. Bidirectional sweep unchanged.

---

## 6. The golden-pair harness — R3 revision

R0 §6, R1 §6, and R2 §6's harness stand: the
`test/data/resolver/{valid,invalid}` layout, the `-update` flag, the
invalid-fixture map, the reachability sweep, the schema-mapping
totality. R3 revises the fixture set only, not the harness code.

### 6.1 Schema fixture strategy — one new schema

The R3 valid schema (`social_r3.gql`) is a superset of R2's:

- The R2 `Person`, `Post`, `AUTHORED`, `LIKES` shapes unchanged, with
  their R2 properties (`nickname`, `score`, `AUTHORED.views`).
- A **second reciprocal `AUTHORED` edge** — `(:Post) -[:AUTHORED]-> (:Person)` —
  authored deliberately as the *distinct* reverse-direction edge type
  so the undirected double-match arm (case C, §4.6) has a witness.
  Its properties intentionally *differ* from the forward edge to make
  the ambiguity semantically meaningful: `authoredBy :: STRING NOT NULL`
  (a display attribution the reverse edge carries, that the forward
  edge does not). No two-way keying puzzle here — `EdgeKey` is a
  triple, so `{Person, AUTHORED, Post}` and `{Post, AUTHORED, Person}`
  are distinct keys.
- A **self-loop edge** — `(:Person) -[:KNOWS]-> (:Person)` — so a
  self-loop fixture (§4.5.2) has a witness.
- A **second multi-type edge shape** kept implicit via the union
  `AUTHORED` + `LIKES` between `Person` and `Post` (both already in
  R2's schema); a multi-type undirected fixture (`[:AUTHORED|LIKES]`
  from Person to Post) exercises the multi-type × undirected case.

The R2 `social_r2.gql` stays untouched; R2 fixtures continue to point
at it. R3 valid fixtures point at `social_r3.gql`.

The R3 invalid corpus reuses:
- `social.gql` (R1 shape, no reciprocal edge) — for
  `unknown_edge_undirected.cypher` and the multi-type miss cases.
- `social_ambiguous.gql` (R1 addition, kept) — for
  `ambiguous_unlabelled_binding.cypher` (unchanged from R1).
- **New**: `social_r3.gql` copy in the invalid corpus (byte-identical
  to the valid one) for the double-match ambiguity fixture.

### 6.2 Schema fixture text

`test/data/resolver/valid/schemas/social_r3.gql`:

```gql
CREATE PROPERTY GRAPH TYPE SocialR3 AS {
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

`test/data/resolver/invalid/schemas/social_r3.gql` — an exact byte copy
of the valid one, for the fixtures that need the R3-specific shape.

### 6.3 R3 valid fixtures

Added under `test/data/resolver/valid/`. Each fixture is one Cypher
file; each has one paired `.validated.golden.json` regenerated by
`-update`. `schema.mapping.json` grows one row per fixture pointing at
`social_r3.gql`.

| Fixture | Shape | R3 arm exercised |
|---|---|---|
| `undirected_single_match.cypher` | `MATCH (p:Person)-[r:LIKES]-(post:Post) RETURN r` | undirected × single-type × single-match → `ResolvedEdge{Person→LIKES→Post}` (case B) |
| `undirected_single_match_reverse.cypher` | `MATCH (post:Post)-[r:LIKES]-(p:Person) RETURN r` | undirected × single-type × single-match, textual order reversed → same committed `EdgeKey` as above (verifies orientation-commit is schema-driven, not textual) |
| `multi_type_directed_union.cypher` | `MATCH (p:Person)-[r:AUTHORED\|LIKES]->(post:Post) RETURN r` | directed × multi-type × 2-match → `ResolvedEdgeUnion{[Person→AUTHORED→Post, Person→LIKES→Post]}` (case D) |
| `multi_type_undirected.cypher` | `MATCH (p:Person)-[r:AUTHORED\|LIKES]-(post:Post) RETURN r` | undirected × multi-type; matches: `Person→AUTHORED→Post`, `Person→LIKES→Post`, `Post→AUTHORED→Person` (three matches) → `ResolvedEdgeUnion` |
| `var_length_directed.cypher` | `MATCH (p:Person)-[r:KNOWS*1..3]->(q:Person) RETURN r` | directed × single-type × var-length → `ResolvedList{Element: ResolvedEdge{Person→KNOWS→Person}}` |
| `var_length_undirected_single_match.cypher` | `MATCH (p:Person)-[r:LIKES*1..2]-(post:Post) RETURN r` | undirected × single-type × var-length × single-match → `ResolvedList{Element: ResolvedEdge{Person→LIKES→Post}}` |
| `var_length_multi_type.cypher` | `MATCH (p:Person)-[r:AUTHORED\|LIKES*1..3]->(post:Post) RETURN r` | directed × multi-type × var-length → `ResolvedList{Element: ResolvedEdgeUnion}` |
| `self_loop_directed.cypher` | `MATCH (p:Person)-[r:KNOWS]->(q:Person) RETURN r` | self-loop admits normally (both endpoints Person, edge Person→KNOWS→Person) — §4.5.2 self-loop witness |
| `unlabelled_via_undirected.cypher` | `MATCH (a)-[r:LIKES]-(post:Post) RETURN a, r` | Phase B inference through an undirected edge: `a` inferred to `Person` from the *source-side* match of `Person→LIKES→Post` (the reverse orientation `Post→LIKES→Person` is not in the schema, so `a`'s candidate set is `{Person}` — singleton) |
| `unlabelled_via_multi_type.cypher` | `MATCH (a)-[r:AUTHORED\|LIKES]->(post:Post) RETURN a` | Phase B inference through a multi-type edge: `a`'s candidate set is `union({Person from AUTHORED→Post}, {Person from LIKES→Post}) = {Person}` — singleton (§4.5.4) |
| `edge_property_union_agree.cypher` | `MATCH (p:Person)-[r:AUTHORED\|LIKES]->(post:Post) RETURN r.likedAt` (schema declares `likedAt :: TIMESTAMP` on both `AUTHORED` and `LIKES` — see §6.2) | multi-candidate property lookup with all members agreeing on type/nullability → §4.8 emits `ResolvedProperty{Type: TIMESTAMP, Nullable: true}` |
| `undirected_var_length_multi_type_property.cypher` | `MATCH (p:Person)-[r:AUTHORED\|LIKES*1..2]-(post:Post) RETURN r` | undirected × multi-type × var-length whole-entity → `ResolvedList{Element: ResolvedEdgeUnion}` (element carries all matching (label, orientation) pairs) |

**Note on `edge_property_union_agree.cypher`:** the shared union
property is declared inline in §6.2's schema — `likedAt :: TIMESTAMP`
appears on **both** `Person→AUTHORED→Post` (alongside its existing
`publishedAt` / `views`) and `Person→LIKES→Post`. The fixture projects
`r.likedAt`, so §4.8's property-lookup pass finds a hit on every union
member with matching `(Type, Nullable) = (TIMESTAMP, true)` and emits
`ResolvedProperty{Type: TIMESTAMP, Nullable: true}` without an
`ErrUnknownProperty`. Wire parity with R2's `LIKES` (property-free) is
intentionally broken — R3 does not repoint R2 fixtures at
`social_r3.gql`, so R2's goldens are untouched. This spec chose
option (i) of the two we considered (inline shared property on
`social_r3.gql`) over option (ii) (a dedicated `social_r3_union.gql`)
because the schema fixture stays single-file per stage and no new
mapping row is needed for the union-property fixture.

**Coverage sketch (per row, keyed to the algorithm):**

- `undirected_single_match` / `undirected_single_match_reverse` —
  exercise §4.4's two-orientation trial (case B verdict); the
  reverse-textual-order variant proves the committed `EdgeKey` is
  schema-driven, not textual (both fixtures' goldens carry the same
  `EdgeKey` in `ResolvedEdge`).
- `multi_type_directed_union` — exercises §4.4's multi-type
  cross-product (case D verdict); the golden's
  `ResolvedEdgeUnion.EdgeKeys` is ordered by
  first-appearance-of-label (`AUTHORED` before `LIKES`).
- `multi_type_undirected` — exercises §4.4's full 2 × 2 cross-product;
  the golden's union has three entries in canonical order (label
  first-appearance, then orientation).
- `var_length_directed` — exercises §4.7's var-length whole-entity
  arm (single-candidate); the golden's outer type is
  `ResolvedList{Element: ResolvedEdge}`.
- `var_length_undirected_single_match` — combined undirected +
  var-length; verdict-B on the candidate set, ResolvedList wrap on
  var-length.
- `var_length_multi_type` — verdict-D on candidate set, ResolvedList
  wrap.
- `self_loop_directed` — §4.5.2's self-loop witness (§4.5.2 covers
  the Phase B self-loop case; this fixture covers the labelled self-
  loop case in Phase A2).
- `unlabelled_via_undirected` — Phase B through an undirected edge
  (§4.5's widened touching set); the schema's asymmetry (only
  Person→LIKES→Post, not the reverse) collapses `a`'s candidate set
  to a singleton.
- `unlabelled_via_multi_type` — Phase B through a multi-type edge
  (§4.5.4's union-across-labels rationale); both AUTHORED and LIKES
  contribute `Person` to `a`'s candidate set — the union of two
  singletons.
- `edge_property_union_agree` — §4.8's uniform-property rule (happy
  path).
- `undirected_var_length_multi_type_property` — combined case (all
  three R3 axes) whole-entity projection.

### 6.4 R3 invalid fixtures — updated `invalidFixtures` map

The R0/R1/R2 map's rows for R3-now-valid constructs (`undirected_edge`,
`var_length_edge`, `multi_type_edge`) **retire** — they are R3 valid
inputs. The three fixture files are deleted from `invalid/` and their
positive analogues appear in `valid/` (§6.3). Two new rows are added
for the R3 sentinel and its zero-match sibling; three new rows are
added for the R3-remainder `ErrOutOfR0Scope` and property-widening
sub-cases.

```go
var invalidFixtures = map[string]error{
    // R0/R1/R2 rows carried forward (unchanged)
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

    // R2 rows RETIRED at R3 (moved to valid/):
    //   "undirected_edge.cypher":                          ErrOutOfR0Scope,  -> valid/undirected_single_match.cypher
    //   "var_length_edge.cypher":                          ErrOutOfR0Scope,  -> valid/var_length_directed.cypher
    //   "multi_type_edge.cypher":                          ErrOutOfR0Scope,  -> valid/multi_type_directed_union.cypher

    // R3 new rows
    "ambiguous_edge_orientation.cypher":                    ErrAmbiguousEdgeOrientation,
    "unknown_edge_undirected.cypher":                       ErrUnknownEdge,
    "unknown_edge_multi_type_all_miss.cypher":              ErrUnknownEdge,
    "unknown_property_union_missing.cypher":                ErrUnknownProperty,
    "unknown_property_union_type_differs.cypher":           ErrUnknownProperty,
    "untyped_edge.cypher":                                  ErrOutOfR0Scope,
    "var_length_edge_property_projection.cypher":           ErrOutOfR0Scope,
}
```

**R3 invalid fixture contents:**

- `ambiguous_edge_orientation.cypher`:
  `MATCH (p:Person)-[r:AUTHORED]-(post:Post) RETURN r` — the
  double-match arm (case C). Schema: `social_r3.gql` (both
  `Person→AUTHORED→Post` and `Post→AUTHORED→Person` declared).
  Verdict: §4.6 case C → `ErrAmbiguousEdgeOrientation`.
- `unknown_edge_undirected.cypher`:
  `MATCH (p:Person)-[r:KNOWS]-(post:Post) RETURN r` — undirected
  single-type, both orientations tried, neither in schema (KNOWS
  is `Person→Person` in `social_r3.gql`, not `Person↔Post`).
  Verdict: §4.6 case A → `ErrUnknownEdge`. Fail-message lists both
  tried orientations.
- `unknown_edge_multi_type_all_miss.cypher`:
  `MATCH (p:Person)-[r:AUTHORED|KNOWS]->(post:Post) RETURN r` — a
  multi-type edge where every candidate misses the schema
  (`Person→AUTHORED→Post` exists in `social_r3.gql`, so this fixture
  needs a different schema OR different labels — spec authors: use
  `social.gql` (R1 invalid schema, just `Person + Post + AUTHORED`)
  and the query `MATCH (p:Person)-[r:KNOWS|LIKES]->(post:Post) RETURN r`
  — neither label declared in that schema, both orientations of
  each also miss → verdict A). Adjust schema pairing accordingly
  in `invalid/schema.mapping.json`.
- `unknown_property_union_missing.cypher`:
  `MATCH (p:Person)-[r:AUTHORED|LIKES]->(post:Post) RETURN r.views` —
  schema `social_r3.gql` (with the `LIKES.likedAt` addition per
  §6.3's note): `AUTHORED.views` exists (`INT NOT NULL`), `LIKES.views`
  does not; §4.8 step 1 fails → `ErrUnknownProperty` with the
  "missing on union member" prose.
- `unknown_property_union_type_differs.cypher`:
  same schema and query pattern but with a project property whose
  types are declared to differ on the two union members — this
  requires a schema addition:
  `social_r3_typediff.gql` (a variant of `social_r3.gql` where
  `AUTHORED` and `LIKES` both declare a property `count`, but as
  `INT NOT NULL` on `AUTHORED` and `FLOAT NOT NULL` on `LIKES`).
  The fixture: `MATCH (p:Person)-[r:AUTHORED|LIKES]->(post:Post)
  RETURN r.count` → §4.8 step 2 fails → `ErrUnknownProperty` with
  the "type differs" prose. Pairing recorded in
  `invalid/schema.mapping.json`.
- `untyped_edge.cypher`:
  `MATCH (p:Person)-[]->(post:Post) RETURN p` — untyped edge in an
  anonymous binding. §4.2 admissibility gate → `ErrOutOfR0Scope`
  (fail-message: "untyped edge").
- `var_length_edge_property_projection.cypher`:
  `MATCH (p:Person)-[r:KNOWS*1..3]->(q:Person) RETURN r.since` — the
  §4.7 var-length property projection reject → `ErrOutOfR0Scope`
  (fail-message: "property projection on variable-length edge
  binding: reach list elements via list-element access (UNWIND
  in R5 or later)").

Each fixture is paired to its schema via `invalid/schema.mapping.json`,
extended to include the new fixtures. The `social_r3_typediff.gql`
schema is a new invalid-corpus schema added for the type-differs
fixture.

### 6.5 Determinism check — R3 additions

R0 §6.5 and R1 §6.5 stand. R3 adds:

- `EdgeBinding.Labels()` iteration order is parser-guaranteed
  first-appearance order (per `internal/graph/labelset.go`'s
  slice-backed representation).
- `edgeCandidates`' inner orientation loop is fixed at
  `(src, tgt)` then `(tgt, src)`.
- `ResolvedEdgeUnion.EdgeKeys` marshals the `edgeCandidates` slice
  verbatim; no re-sort inside the golden encoder.

Every ordered surface is either the parser's guaranteed order or a
fixed inner-loop order. The golden JSON is deterministic;
`-update` regenerates a byte-stable file.

### 6.6 Non-obvious harness invariants — R3 additions

R0 §6.6, R1 §6.6, and R2 §6.7 invariants stand. R3 adds:

- **Schema fixtures are R-stage-scoped, but R3's schema is the
  first with a declared-reciprocal edge.** The `social_r3.gql`
  file authors both `Person→AUTHORED→Post` and
  `Post→AUTHORED→Person` deliberately so the double-match arm has
  a witness. Reviewers reading the schema fixture must understand
  the reciprocal is *intentional*: it is the schema's part of the
  double-match falsifiability. Deleting the reciprocal breaks
  the ambiguity fixture without a golden signal (the fixture
  would re-resolve as a happy case-B).
- **Wire compatibility with R0/R1/R2 goldens.** R3 does not
  repoint R0/R1/R2 fixtures at `social_r3.gql` or the widened
  schema; those fixtures continue to point at their R-stage schema
  files (`social.gql` / `social_r1.gql` / `social_r2.gql`). Their
  goldens continue to match byte-for-byte. This is the R2 §6.7
  invariant, extended one stage.
- **The `LIKES.likedAt` addition (§6.3's note).** The R3 schema
  gives `LIKES` a `likedAt :: TIMESTAMP` property to support the
  union-property happy path. That means `LIKES` is no longer
  property-free at R3 — a divergence from R2's `social_r2.gql`.
  R3 does not need to alter R2's file (R2 fixtures stay on
  `social_r2.gql`); the property is only visible through R3
  fixtures.

---

## 7. R3 capability scope — what resolves

**In scope:** a single Cypher query the parser accepts, whose parsed
`query.Query` shape is:

- Exactly one `Branch` in `Branches`; zero `Combinators`.
- Exactly one `Part` in the branch's `Parts`.
- The part's `Bindings` are a non-empty slice of `NodeBinding` and/or
  `EdgeBinding` values:
  - Each `NodeBinding` is labelled (non-empty `Labels()`) OR
    unlabelled and Phase-B-inferable (R1 §7; R3 widens the touching
    set to every R3-admitted edge).
  - Each `EdgeBinding` is labelled (`len(e.Labels()) >= 1`); its
    axes are unconstrained across:
    - **Directed** or undirected (`e.Directed()` either value).
    - **Single-hop** or variable-length (`e.Hops()` either nil or
      non-nil).
    - **Single-type** or multi-type (`len(e.Labels()) >= 1`, any
      value).
  Anonymous edges (empty `Variable()`) remain admitted at R3 (R1 §7);
  they cannot be projected as columns but the pattern is legal.
- The part's `Returns` is a non-empty slice of `ReturnItem`s. Each
  `ReturnItem.Value` is `RefProjection`, `LiteralProjection`,
  `FuncProjection`, or `ExprProjection` (R2 §7).
  `AggregateProjection` is out.
- A `RefProjection` whose `Ref.Variable` names a **variable-length
  edge binding** MUST have `Ref.Property == ""` — whole-entity
  projections are admitted; property projection is not
  (§4.7 / out-of-scope table below).
- `ReturnsAll` is false; `Distinct` is false; `Effects` is empty.
- `Parameters` is a slice of `Parameter`s with the R2 shape (one or
  more Uses each; each Use is `PropertyUse`, `ClauseSlotUse`, or
  read-side `ExprUse`).
- `StatementKind` is `StatementRead`.

**Out of scope, routed to the appropriate sentinel:**

| Construct | Sentinel | R-stage owner |
|---|---|---|
| Untyped edge (`len(Labels()) == 0`) | `ErrOutOfR0Scope` | R-later |
| Path binding | `ErrOutOfR0Scope` | R5 |
| Unwind binding | `ErrOutOfR0Scope` | R5 or later |
| Call binding | `ErrOutOfR0Scope` | R7 |
| `AggregateProjection` | `ErrOutOfR0Scope` | R5 |
| `ExprProjection` typed `TypeList{TypeNode}` (list literal of bare node vars) | `ErrOutOfR0Scope` | R5 |
| `ExprProjection` typed `TypeList{TypeEdge}` (list literal of bare edge vars — distinct from a RefProjection of a var-length edge, §4.11) | `ErrOutOfR0Scope` | R5 |
| Property projection on a variable-length edge binding | `ErrOutOfR0Scope` | R5 |
| `ExprUse` at `ExprInSetValue` / `ExprInDeleteTarget` | `ErrOutOfR0Scope` | R6 |
| Nullability upgrades (OPTIONAL MATCH regimes) | `ErrOutOfR0Scope` | R4 |
| `Part.Distinct == true` / `Part.ReturnsAll == true` | `ErrOutOfR0Scope` | R5 |
| WITH carry-forward; UNION | `ErrOutOfR0Scope` | R5 |
| Writes / CREATE / MERGE / SET / REMOVE / DELETE | `ErrOutOfR0Scope` | R6 |
| CALL / YIELD | `ErrOutOfR0Scope` | R7 |
| Every candidate `(label, orientation)` misses the schema | `ErrUnknownEdge` | (unchanged) |
| Single-type undirected edge whose two orientations both match | `ErrAmbiguousEdgeOrientation` | (new at R3) |
| Property lookup on a multi-candidate edge; property missing on some union member | `ErrUnknownProperty` | (widened at R3) |
| Property lookup on a multi-candidate edge; property type/nullability differs across union members | `ErrUnknownProperty` | (widened at R3) |
| Labelled node with no matching schema NodeType | `ErrUnknownLabel` | (unchanged) |
| Unlabelled node with an empty candidate set from R3-widened touching edges | `ErrUnknownLabel` | (unchanged) |
| Unlabelled node with a multi-candidate set that survives Phase B fixed-point | `ErrAmbiguousBinding` | (unchanged) |
| Parameter Uses that do not unify | `ErrParameterTypeConflict` | (unchanged) |

**Silently accepted (not routed anywhere):**

R0/R1/R2's silently-accepted set stands unchanged (literal-only
WHERE / ORDER BY / SKIP / LIMIT). R3 does not extend it.

**Recorded ADR 0009 cross-check.** ADR 0009 R3: "undirected two-
orientation trial against the schema (and the double-match question
recorded on `gqlc-int`); multi-type edges as one candidate key per
type; var-length hop-range lookups with `list<edge>` results."

- §4.4 (labels × orientations × schema cross-product) implements the
  first ("two-orientation trial") and second ("one candidate key per
  type") clauses in one helper.
- §3.5 and §4.6 case C decide the double-match question:
  `ErrAmbiguousEdgeOrientation` for single-type undirected;
  `ResolvedEdgeUnion` for multi-type union.
- §4.7 (projection walk) implements the `list<edge>` result typing
  for var-length via `ResolvedList{Element: ResolvedEdge or
  ResolvedEdgeUnion}`.

Nothing in §7 disagrees with ADR 0009 by construction.

**Recorded parser Stage 5 §9 cross-check.** Parser Stage 5 §9
recorded three open issues: (i) what a double match means; (ii)
whether the resolver reports *which* orientation(s) matched; (iii)
whether the marker widens post-freeze. R3 decides:

- (i) Double match on a single-type undirected edge is an
  ambiguity error (§4.6 case C). Double match on a multi-type
  undirected edge is a union (§4.6 case D); no ambiguity.
- (ii) The winning orientation is inspected from the committed
  `EdgeKey`'s `Source`/`Target` (§3.1); no new axis on
  `ResolvedEdge`. For a `ResolvedEdgeUnion`, every matched
  orientation is present in `EdgeKeys`. No parser-side widening.
- (iii) The `directed` marker on `EdgeBinding` needs no widening;
  the resolver has enough from the boolean plus its
  `edgeCandidates` cross-product. `query.Query` stays frozen (ADR
  0008 respected).

---

## 8. Ground-truth cross-check

Every factual claim in this spec is verifiable against source; the
citations below name the file:line the claim rests on. A future reader
should be able to open each citation and confirm the shape the spec
describes still holds.

- **`EdgeBinding.Directed()`** — `internal/query/query.go:445-447`;
  false for undirected.
- **`EdgeBinding.Hops()`** — `internal/query/query.go:449-452`; nil
  for single-hop, non-nil for variable-length.
- **`EdgeBinding.Labels()`** — `internal/query/query.go:436-437`;
  slice of `graph.Label`, first-appearance order (Stage 8 multi-type
  admits `len >= 1`).
- **Multi-type relationship lowers as a `LabelSet` on `EdgeBinding`** —
  `internal/query/cypher/pattern.go:279`
  (`labels = relTypes(...)`) and the multi-type collector
  `relTypes` at `internal/query/cypher/pattern.go:430-447` — each
  `[r:A|B]` yields `LabelSet{"A", "B"}` on the binding.
- **`EdgeBinding.Source()` / `Target()`** —
  `internal/query/query.go:439-443`; both always set.
- **Directed left-arrow canonicalises to source→target** —
  `internal/query/cypher/pattern.go:262-263`; a `<--` edge is
  stored source→target. Undirected keeps textual order (line 261's
  `srcNode, tgtNode := prev, next`; the guard at 262 gates the swap
  on `directed && left`).
- **Endpoint sum ×2 (`VarEndpoint`, `InlineEndpoint`)** —
  `internal/query/query.go:934-980`; sealed; both hold data in
  unexported fields.
- **`schema.EdgeKey` triple** — `internal/schema/schema.go:36-40`;
  `(Source, Label, Target)` all `graph.LabelSetKey`.
- **`schema.Schema.Edges` keying** — `internal/schema/schema.go:15`;
  `map[EdgeKey]EdgeType`. R3's `edgeCandidates` reads this map;
  determinism relies on the *explicit* candidate enumeration, not
  on map iteration.
- **`EdgeHops`** — `internal/query/query.go:474-514`; the
  variable-length hop range with optional min/max.
- **`RefProjection.Type()` for a var-length edge is `TypeList{TypeEdge}`** —
  `internal/query/cypher/expr.go:283-296` (`refType`);
  specifically lines 293-296 (`if rb.hops != nil { return
  query.NewTypeList(query.TypeEdge{}) }`).
- **`RefProjection.Type()` for a single-hop edge is `TypeEdge`** —
  `internal/query/cypher/expr.go:296` (falls through to
  `return query.TypeEdge{}` when `rb.hops == nil`).
- **`RefProjection.Type()` for a property lookup is `TypeUnknown`** —
  `internal/query/cypher/expr.go:284-286`; property axis
  short-circuits.
- **Parser accepts undirected edges (`directed := left != right`)** —
  `internal/query/cypher/pattern.go:256`.
- **Parser accepts multi-type edges (`relTypes` union)** —
  `internal/query/cypher/pattern.go:279`, `430-447`.
- **Parser accepts var-length edges (`edgeHopsFromRangeLiteral`)** —
  `internal/query/cypher/pattern.go:280-287`.
- **R2 kernel currently rejects R3 shapes via
  `r1EdgeAdmissible`** —
  `internal/resolver/resolve.go:155-170`
  (specifically lines 156-160 for `!Directed()`, `Hops() != nil`,
  `len(Labels()) > 1`); R3 replaces this function with
  `r3EdgeAdmissible` (§4.2) whose only refusal is untyped.
- **R2 kernel's `closeEdge`** —
  `internal/resolver/resolve.go:203-214`; R3 revises to route
  through §4.4's `edgeCandidates` and §4.6's verdict.
- **R2 kernel's `candidateTypes` (Phase B contribution)** —
  `internal/resolver/resolve.go:261-299`; R3 widens per §4.5.
- **R2 kernel's `refProjectionType`** —
  `internal/resolver/resolve.go:371-393`; R3 widens per §4.7
  (var-length wrap + union dispatch).
- **R2's `resolveType` rejects `TypeList{TypeNode|TypeEdge}`** —
  `internal/resolver/resolve.go:428-434`; R3 does NOT change this
  function — the rejection continues to fire for a list literal
  of bare entity variables (an `ExprProjection`'s type), which is
  a distinct producer from a var-length edge's `RefProjection`
  (see §4.11).
- **`graph.LabelSet` is `[]string`** —
  `internal/graph/labelset.go:13`; the slice preserves textual
  insertion order (parser Stage 8 note in ADR 0003 records this).
- **`graph.LabelSet.Key()`** — `internal/graph/labelset.go:21-26`;
  returns the canonical `LabelSetKey` used as map keys in
  `schema.Nodes` and `schema.EdgeKey` fields (labels sorted,
  deduped, joined by "&").
- **ADR 0002 bit-width preservation** — `docs/adr/0002-...` — the
  `ResolvedProperty` uniform-typing rule in §4.8 preserves the
  schema's declared width.
- **ADR 0003 curated model — edge marker vs orientation-trial
  policy** — `docs/adr/0003-...`, Stage-5 note (lines 59-69):
  the direction marker is a bit; the trial policy is the
  resolver's. R3 implements the trial policy.
- **ADR 0005 type-interface boundary** — `docs/adr/0005-...`; the
  runtime executes the original text, so var-length hop traversal
  and undirected orientation walking are runtime concerns; the
  resolver types the result column only.
- **ADR 0008 frozen query.Query** — `docs/adr/0008-...`; every R3
  addition lives in `ValidatedQuery`, not `query.Query`.
- **ADR 0009 R3 remit** — `docs/adr/0009-...`, R3 stage line
  ("undirected two-orientation trial ... multi-type edges as one
  candidate key per type ... var-length hop-range lookups").
- **Parser Stage 5 §9 (undirected handoff, `gqlc-int`)** —
  `docs/specs/cypher-query-parser-stage-5.md:325-366`; the open
  contract R3 discharges.
- **Cypher parser rejects unbound variables at build time** —
  `internal/query/cypher/build.go:157` (R0 §5's record);
  a `ref.Variable` naming no binding cannot reach the R3 kernel.

---

## 9. Definition of done for R3 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is out
of scope of this document. The spec is done when:

1. This file exists at `docs/specs/resolver-stage-r3.md`, committed on
   branch `resolver-r3-spec`.
2. §3 names the one new `ResolvedType` variant (`ResolvedEdgeUnion`)
   with wire encoding invariants and extends `ResolvedList`'s
   producer surface to var-length edge bindings; §3.5 records the
   double-match decision at the top level and cross-links to §4.6.
3. §4 gives the algorithm for all three R3 capabilities:
   two-orientation trial (§4.4); multi-type cross-product (§4.4);
   var-length wrap (§4.7). §4.6's verdict table decides the
   double-match question with rationale for both accepted and
   rejected alternatives, and §4.8's uniform-property rule handles
   property lookup on a multi-candidate edge. §4.11 records the
   revised type-mapping-table owner column.
4. §5 names and defends the one new sentinel
   (`ErrAmbiguousEdgeOrientation`) and revises the message-sets of
   the two sentinels that widen (`ErrUnknownProperty`,
   `ErrOutOfR0Scope`).
5. §6 designs the fixture set: the R3 valid schema `social_r3.gql`
   (with reciprocal `AUTHORED` and self-loop `KNOWS`), the R3 valid
   fixture list (11 fixtures, each keyed to an R3 arm), the R3
   invalid fixture list (7 fixtures — 5 truly new + 2 R2 retirements
   that stay negative under a different shape), the revised
   `invalidFixtures` map, and the R3 harness invariants.
6. §7 states the R3 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel each construct routes
   to and the R-stage that owns the next widening.
7. §8 cross-checks every factual claim against source file:line.
8. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer); every
blocker he raises is fixed on this same branch before the branch
merges. Cycle 2 (the R3 code cycle) begins only when the spec cycle
merges.
