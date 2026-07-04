# Stage R1 spec — resolver: edges and unlabelled-binding inference

The implementation brief for Stage R1 of `internal/resolver`, extending the
merged R0 skeleton (`docs/specs/resolver-stage-r0.md`) with the two
capabilities ADR 0009 places at R1: directed `schema.EdgeKey` formation
from named and inline endpoints, and unlabelled-binding inference by
walking the edges that touch a binding (the ADR 0003 consequence). Build
this **test-first**. Scope, sequencing, and error posture inherit from
ADR 0009 and the R0 spec unchanged; this document revises only the rows,
sentinel fail-sites, and out-of-scope table entries that R1 changes.

Stage R1 lowers a **labelled single-`MATCH` pattern of one or more nodes
joined by directed single-hop edges** into the current `ValidatedQuery`
model. Whole-entity and single-level property projections resolve for
both nodes and edges. Undirected edges, multi-type edges, variable-length
edges, WITH, UNION, non-`RefProjection` items, writes, CALL, parameter
unification, and nullability refinement stay out of scope and continue to
route to `ErrOutOfR0Scope` (the sentinel name is unchanged at R1 —
category-grained per R0 §5; renaming is deferred with the R0
`ErrOutOfR0Scope` retirement plan).

---

## 1. Deliverables

- `internal/resolver/resolve.go` — extended with edge-binding resolution,
  inline-endpoint labelling, EdgeKey formation, unlabelled-binding
  inference, and edge-property projection/parameter lookup. The R0 file
  layout stands (`resolver.go` / `validated.go` / `errors.go` /
  `resolve.go`); no new files are introduced at R1.
- `internal/resolver/validated.go` — extended with one new
  `ResolvedType` variant, `ResolvedEdge` (§3), carrying the
  `schema.EdgeKey` the resolver formed. R0's `ResolvedNode` and
  `ResolvedProperty` variants are unchanged.
- `internal/resolver/errors.go` — extended with two new sentinels
  (`ErrUnknownEdge`, `ErrAmbiguousBinding`) and revised prose on
  `ErrUnknownProperty` and `ErrOutOfR0Scope` (§5). The R0 sentinel
  `ErrUnknownLabel` is unchanged. The R0 sentinel set is renamed
  conceptually from "the R0 set" to "the R0..R1 set" (still one
  `allSentinels`).
- `test/data/resolver/valid/schemas/` — one new schema fixture
  (`social_r1.gql`, §6.2) plus zero-to-one revision of the existing
  `social.gql` (unchanged if possible; see §6). The invalid corpus's
  existing `social.gql` (with `AUTHORED`) is reused as the R1 invalid
  schema unchanged.
- `test/data/resolver/valid/*.cypher` and `.validated.golden.json` —
  new R1 valid fixtures (§6.3), each paired with its schema through the
  updated `schema.mapping.json`.
- `test/data/resolver/invalid/*.cypher` — new R1 invalid fixtures for
  the two new sentinels and one revised `ErrOutOfR0Scope` fixture
  (§6.4).
- `internal/resolver/resolver_test.go` — updated `invalidFixtures` map
  (§6.4); no structural changes to `TestValid` / `TestInvalid` /
  `TestSentinelReachability` are required (the R0 harness already
  scales to the R1 fixture set — the invariant is that the map is total
  against `invalid/*.cypher`, and each new fixture adds one row).

Nothing downstream of the resolver is built (no codegen, no driver) —
`ValidatedQuery` is provisional through R7 (ADR 0009 §Decision).

---

## 2. Architecture — deltas from R0

R0's architecture (§2 of the R0 spec) stands: the `Resolver` struct, its
compile-time inputs, the `QueryResolver` interface + compile-time
assertion, the purity and short-circuit posture, and the
`resolve.go`/`Resolve` split are unchanged. R1 extends only the kernel
function; no new exported types except the new `ResolvedType` variant
(`ResolvedEdge`) and the two new sentinels.

### 2.1 The R1 kernel structure

The kernel remains one linear pass with early returns. R1 replaces R0's
single-binding walk (R0 §4.7 step 3) with a three-phase binding walk:

- **Phase A — labelled-binding resolution** (§4.2). Two sweeps in
  order: A1 resolves every labelled `NodeBinding` against
  `schema.Schema.Nodes`, then A2 forms a candidate `EdgeKey` for every
  labelled directed single-hop `EdgeBinding` — reading endpoint labels
  from A1's committed node table (`VarEndpoint`) or the pattern
  (`InlineEndpoint`). Edges whose endpoint is a still-unlabelled
  `VarEndpoint` or an empty-labels `InlineEndpoint` are set aside for
  Phase C.
- **Phase B — unlabelled-node inference** (§4.3). For each unlabelled
  `NodeBinding`, walk the part's directed single-hop labelled
  `EdgeBinding`s that touch it. Each touching edge contributes the set
  of schema node types compatible with that edge's position (source or
  target) — intersected across all touching edges. Iterate to a fixed
  point (§2.3). Each pending binding produces one of three outcomes:
  **no candidate** (`ErrUnknownLabel`), **single candidate** (bound,
  resolution continues), **multiple candidates** or an
  unbreakable-cycle pending set (`ErrAmbiguousBinding`).
- **Phase C — deferred edge closure** (§4.4). Single pass over the
  edges Phase A2 set aside: re-form each candidate `EdgeKey` against
  the now-complete node table and look it up in `schema.Schema.Edges`.
  On miss, `ErrUnknownEdge`; a still-unresolved endpoint (an anonymous
  inline endpoint) fails `ErrUnknownLabel`.

Phase B runs after Phase A because labelled bindings' resolutions must
be committed before inference reads them; Phase C runs after Phase B
so every Phase-B-resolvable binding is committed before the deferred
edges are closed. Phase A alone never fails on an unlabelled node
binding — the resolution is deferred, not attempted. A labelled node
binding with no schema witness fails A1 (`ErrUnknownLabel`); a
labelled edge with fully-committed endpoints and no matching schema
`EdgeKey` fails A2 (`ErrUnknownEdge`, §5).

The projection/parameter walk (R0 §4.7 steps 4–5) is unchanged in
shape; the resolver looks up the projection's `Ref.Variable` in the
resolved binding table (both nodes and edges now), then dispatches on
whether `Ref.Property == ""` (whole-entity: emit `ResolvedNode` or
`ResolvedEdge`) or not (property lookup: emit `ResolvedProperty`).

### 2.2 Endpoint labelling — a helper on the kernel

R1 introduces one internal helper, unexported, in `resolve.go`:

```go
// endpointLabels reads the labels an edge endpoint carries at the point
// EdgeKey formation needs them: for a VarEndpoint, the labels of the
// binding it names (already resolved in Phase A, or empty pending Phase
// B); for an InlineEndpoint, the labels written inline on the pattern.
// Returns (canonicalKey, ok): ok is false when the endpoint is an
// unlabelled VarEndpoint whose binding is still pending inference.
func endpointLabels(e query.Endpoint, resolved map[string]graph.LabelSetKey) (graph.LabelSetKey, bool)
```

The helper is grounded in `Endpoint`'s sum arity (`internal/query/
query.go:939-941`): closed at two variants (`VarEndpoint`,
`InlineEndpoint`), the type switch is exhaustive without a fallthrough
sentinel. `resolved` is the Phase A table `map[Variable] →
LabelSetKey`; an entry present with an empty key means the binding is
unlabelled and pending Phase B (the Phase A callers loop again).

### 2.3 Phase B termination — the fixed-point argument

Phase B's inference is iterative but finite. The pending set is the
finite subset of unlabelled node bindings in the part; each pass either
(a) commits at least one pending binding to a single candidate (any
number of commits per pass, up to all of them), (b) exits with
`ErrUnknownLabel` (a pending binding whose candidate set is empty), or
(c) exits with `ErrAmbiguousBinding` (a pending binding whose candidate
set has more than one element, or a zero-commit pass with pending
bindings remaining — the cycle case, §4.3). Because path (a) strictly
shrinks the pending set and paths (b)/(c) halt, the loop terminates in
at most N passes where N is the initial pending-set size — the upper
bound corresponds to the pathological case where each pass commits
exactly one binding. The R0 capability scope's "at most one binding"
invariant is discharged at R1: the R1 capability scope admits N nodes
and M edges, so the kernel is O(N·(N + M)) in the worst case (each of
up to N passes reads M edges to compute per-binding candidate sets).

**Why not fold Phase A + Phase B into one recursive descent?** The
recursive form couples the walk order to the pattern's textual order,
so a pattern `(a)-[:AUTHORED]->(b:Post)` and its written-backward
sibling `(b:Post)<-[:AUTHORED]-(a)` (both admitted by the parser, both
canonicalised to the same source→target edge) would take different
inference paths and could disagree on which binding gets a definite
type first. The two-phase form breaks that coupling: Phase A commits
labelled bindings unconditionally, Phase B reads that committed table
in an order-independent way. Determinism matters for the golden-file
harness — a resolver output that flips on written-order noise fails the
JSONEq check silently.

---

## 3. `ValidatedQuery` — the R1 shape

`ValidatedQuery`'s top-level shape (R0 §3.1) is unchanged at R1:
`Columns`, `Parameters`, `Statement`. The extension is on the
`ResolvedType` sum.

### 3.1 One new `ResolvedType` variant — `ResolvedEdge`

```go
// ResolvedEdge is a whole-entity edge projection: the schema's canonical
// (source, label, target) triple. R1 produces this variant for a
// RefProjection whose Ref names an EdgeBinding and whose Property is
// empty. Multi-hop (list<edge>) is R3's business.
type ResolvedEdge struct {
    EdgeKey schema.EdgeKey `json:"edgeKey"`
}

// String is the wire tag "edge".
func (ResolvedEdge) String() string { return "edge" }

// MarshalJSON emits a tagged-union object with a "kind" discriminator.
func (e ResolvedEdge) MarshalJSON() ([]byte, error) {
    return json.Marshal(struct {
        Kind    string         `json:"kind"`
        EdgeKey schema.EdgeKey `json:"edgeKey"`
    }{Kind: e.String(), EdgeKey: e.EdgeKey})
}

func (ResolvedEdge) isResolvedType() {}
```

The `EdgeKey` payload is the schema's canonical triple (`Source`,
`Label`, `Target`, all `graph.LabelSetKey`). Its wire shape is the
`schema.EdgeKey`'s existing JSON encoding
(`internal/schema/schema.go:36-40` — `source`/`label`/`target` string
keys). No `Nullable` bit at R1 (the R1 capability scope excludes
`OPTIONAL MATCH`; R4 owns nullability refinement — R0 §4.3 record).

**Alternative considered and rejected: carry the edge's property types
inline.** An edge column with `RETURN r` is a whole-entity projection,
not a per-property view; codegen decides the wire layout of an entity
row post-freeze (a struct of columns, a driver-native shape, or a
tuple). Carrying the property map here would duplicate the schema and
force every column-consumer to walk it. The `EdgeKey` alone is enough
to reach the schema `EdgeType` post-freeze; codegen indexes back via
`schema.Schema.Edges[EdgeKey]`. Symmetric with `ResolvedNode`
carrying only `Labels` (R0 §3.4).

### 3.2 `ResolvedProperty` — extended reuse

`ResolvedProperty` is unchanged in shape at R1. Its use is extended to
edge-property lookups: a `RefProjection` whose `Ref.Variable` names an
edge binding and whose `Ref.Property` is non-empty resolves via the
edge type's `Properties` map — identical to the R0 node-property lookup,
with the resolved schema type sourced from `schema.EdgeType` instead of
`schema.NodeType`. Same sentinel (`ErrUnknownProperty`) applies on a
miss. The `Nullable` bit on the emitted `ResolvedProperty` continues to
come from `schema.Property.Nullable`.

Parameter typing is the same shape: an inline-map property use on an
edge pattern (`[r:AUTHORED { at: $when }]`) records a
`PropertyUse{Variable: "r", Property: "at"}` — R1 upgrades this to
`ResolvedProperty{Type: r.Type.Properties["at"].Type, Nullable: …}`
via the resolved `EdgeBinding` in exactly the same shape R0 §4.7 step 5
uses for node parameters.

### 3.3 R0 variants — unchanged

`ResolvedNode` and `ResolvedProperty` keep their R0 shape and wire
encoding. The `ResolvedType` sum grows to three variants — nothing R0
produced changes wire form. Fixtures whose R0 golden encoded nothing
edge-related remain byte-identical.

---

## 4. The R1 kernel algorithm

Each step below extends or replaces a numbered step of R0 §4.7. Steps
1 and 6 are unchanged (query-level gating; StatementKind copy).

### 4.1 Step 1 (unchanged) — query-level gating

R0 §4.7 step 1 stands verbatim: one branch, one part, no writes, no
CALL. `Part.Distinct` / `Part.ReturnsAll` still route to
`ErrOutOfR0Scope`. R1 does not touch these gates.

### 4.2 Step 3 (replaced) — Phase A: labelled-binding resolution

Phase A is a two-sweep walk over `Part.Bindings`. The split is
mandatory, not stylistic: the edge sweep reads the node table the node
sweep just committed (via `endpointLabels` on a `VarEndpoint`), so
folding the two into a single interleaved pass would make an
edge-before-its-endpoint-node ordering silently produce
`ok == false` for a labelled endpoint that will resolve one iteration
later — a false deferral to Phase B for a shape Phase A can already
close. Two sweeps, in this order, keep the read strictly after the
write.

**Phase A1 — labelled node bindings.** Walk `Part.Bindings` once. For
each `NodeBinding` with non-empty `Labels()`, take `Labels().Key()`,
look up `schema.Schema.Nodes[key]`; on miss, return `ErrUnknownLabel`
(§5). On hit, record the resolved key in the per-binding table
(`resolvedNodeType map[string]NodeType`). Unlabelled node bindings are
left pending — the table has no entry for their variable until Phase B
adds one. Non-`NodeBinding` bindings are skipped in A1.

**Phase A2 — labelled directed single-hop edge bindings.** Walk
`Part.Bindings` again. For each `EdgeBinding` with `Directed() ==
true`, `Hops() == nil`, and `len(Labels()) == 1`, form the candidate
`EdgeKey`:
  - Read `Source()` and `Target()` (both `Endpoint`) and derive their
    label sets via `endpointLabels`:
    - `VarEndpoint` with a labelled binding → the binding's canonical
      key (from the Phase A1 node table).
    - `VarEndpoint` with an unlabelled binding → `ok == false`; the
      edge is deferred to Phase C (§4.4).
    - `InlineEndpoint` with non-empty `Labels()` → the inline
      `Labels().Key()`. This is the intended R1 endpoint shape for a
      labelled inline (e.g. `-[:AUTHORED]->(:Post)`).
    - `InlineEndpoint` with empty `Labels()` (the fully anonymous
      `()` endpoint) → `ok == false`; the edge is deferred to Phase C,
      but Phase C fails with `ErrUnknownLabel` for the anonymous case
      because the endpoint has no name Phase B could resolve into.
      Rationale: an unlabelled inline endpoint carries no label
      constraint and no binding — nothing in the pattern can commit
      it to a schema node type.
  - With both endpoint keys and the single edge label key, form
    `schema.EdgeKey{Source, Label: Labels().Key(), Target}` and look
    up `schema.Schema.Edges[key]`; on miss, `ErrUnknownEdge` (§5). On
    hit, record the resolved `EdgeKey` and `EdgeType` in the
    per-binding table (`resolvedEdgeType map[string]EdgeType` and
    `resolvedEdgeKey map[string]schema.EdgeKey`).
- **Everything else in `Part.Bindings`** — `PathBinding`,
  `UnwindBinding`, `CallBinding`, undirected `EdgeBinding`
  (`Directed() == false`), variable-length `EdgeBinding` (`Hops() !=
  nil`), multi-type `EdgeBinding` (`len(Labels()) > 1`), untyped
  `EdgeBinding` (`len(Labels()) == 0`) — routes to `ErrOutOfR0Scope`
  (§5) with a fail-message specifying which construct. Anonymous
  edges (empty `Variable()`) whose predicate matches the R1 admission
  criteria (directed, single-hop, single-type) are NOT rejected here:
  Phase A forms their EdgeKey identically to a named edge (§7 lines
  admitting anonymous edges). Anonymous edges cannot be projected as
  a column (a `ReturnItem.Value` is a `RefProjection` whose `Ref.
  Variable` is non-empty by parser invariant), so the projection walk
  in §4.5 cannot reach one; the anonymous-edge rejection surface is
  the projection walk, not Phase A admission.

The order of iteration over `Part.Bindings` is the parser's
first-appearance order (guaranteed by `internal/query/query.go:81-94`,
Part.Bindings). Determinism holds.

### 4.3 Step 3½ (new) — Phase B: unlabelled-node inference

After Phase A, walk each unlabelled `NodeBinding` (variable in
`Part.Bindings` with empty `Labels()`) and compute its candidate set
from the edges that touch it:

- **Find touching edges.** An `EdgeBinding` `e` touches an unlabelled
  node binding `n` iff `e.Source()` or `e.Target()` is a
  `VarEndpoint{Variable: n.Variable()}`. Iterate the R1-supported
  edges only (directed single-hop labelled) — unsupported edges
  already failed Phase A with `ErrOutOfR0Scope`, so Phase B does not
  see them.
- **Compute candidates from each touching edge.** For each touching
  edge `e`:
  - If `n` is on `e`'s source side (`e.Source().Variable == n.Variable`),
    the candidate set for `n` from this edge is
    `{k.Source | k ∈ schema.Schema.Edges, k.Label == e.Labels().Key(),
    k.Target == <target-side key>}` — where the target-side key
    comes from `endpointLabels(e.Target(), resolvedNodeType)`. If the
    target-side is itself unlabelled (a `(a)-[:R]->(b)` where both are
    unlabelled), skip this edge — it cannot constrain `n` alone.
  - Symmetric on the target side.
- **Intersect.** `n`'s candidate set is the intersection over all
  touching edges' contributions (an `n` untouched by any usable edge
  has an empty candidate set).
- **Verdict.** Match `|candidates|`:
  - `0` → `ErrUnknownLabel` (fail-message: "cannot infer type of
    unlabelled binding %q — no edge in the pattern reaches a
    compatible schema node type"). The fail-message names the binding.
  - `1` → bind `n` to the single candidate: update
    `resolvedNodeType[n.Variable]` with the canonical key. Any
    Phase A2 edge whose endpoint pointed at `n` is now Phase C's
    business (§4.4).
  - `>1` → **defer to the next pass.** A neighbour resolving on this
    pass may commit a definite type that narrows this binding's
    candidate set on the next iteration (the source-side key or
    target-side key an intersecting-edge contribution reads becomes
    concrete). Ambiguity is only pronounced on a zero-commit pass with
    pending bindings remaining (see **Fixed-point pass** below), which
    is when the candidate set is provably minimal — no future pass can
    narrow it further. Deferring here is a strict consequence of the
    fixed-point framing in §2.3: only the "exit with no progress"
    branch (path c) raises `ErrAmbiguousBinding`; a per-pass
    multi-candidate verdict is not itself terminal.

**Fixed-point pass.** Phase B iterates. Each pass computes the
candidate set for every pending binding against the current
`resolvedNodeType` table; each singleton verdict commits its binding
(a single pass may commit any number of pending bindings, including
all of them). The pass then re-runs. Termination in §2.3.

- **A pass that commits zero bindings but has pending unresolved
  ones** means the remaining bindings can only be resolved with each
  other's help — a cycle. R1's decision: return
  `ErrAmbiguousBinding` on the *first* pending binding (deterministic:
  the pattern's first-appearance order), with a fail-message that
  names the cycle (list all pending bindings in first-appearance
  order). Rationale: the ADR 0003 consequence says "the resolver
  needs each endpoint's labels (via a named binding or inline) to
  form the key … and this is also what lets it infer an unlabelled
  binding's type by walking the edges that touch it." A cycle
  violates the "walking the edges that touch it" premise —
  every edge in the cycle needs the neighbour resolved first, so
  the pattern is under-constrained. Treating this as ambiguous rather
  than a distinct sentinel keeps R1's sentinel set closed; the
  fail-message disambiguates the sub-case.

### 4.4 Step 3¾ (new) — Phase C: deferred edge closure

After Phase B, every unlabelled node binding that could resolve now
has an entry in `resolvedNodeType`. Phase C re-forms the EdgeKey for
every edge Phase A2 deferred (edges whose `endpointLabels` call
returned `ok == false` on one or both endpoints because a `VarEndpoint`
named a still-unlabelled node). For each such edge:

- Read `Source()` and `Target()` again via `endpointLabels` against
  the now-committed `resolvedNodeType` table.
- If either endpoint still returns `ok == false` — this can only
  happen for an empty-labels `InlineEndpoint` (the anonymous `()`
  case §4.2 flags), because every unlabelled `VarEndpoint` was either
  bound by Phase B or already failed with `ErrUnknownLabel` /
  `ErrAmbiguousBinding` — return `ErrUnknownLabel` on the edge's
  variable (or, for an anonymous edge, on the endpoint's textual
  position in the pattern; fail-message: "cannot infer type of
  anonymous inline endpoint …").
- With both endpoint keys committed, form
  `schema.EdgeKey{Source, Label, Target}` and look up
  `schema.Schema.Edges[key]`; on miss, `ErrUnknownEdge`. On hit,
  record `resolvedEdgeType[e.Variable]` / `resolvedEdgeKey[e.Variable]`
  (anonymous edges get no entry — nothing projects them, §4.5).

Phase C is a single non-iterative pass over the Phase-A2 defer list:
Phase B's fixed-point has already committed every node binding it
could, so no further node-inference iteration would change any
endpoint answer.

### 4.5 Step 4 (extended) — projection resolution over N bindings

Iterate `Part.Returns`; each `ReturnItem.Value` is still required to be
a `RefProjection` at R1 (non-`RefProjection` items are R2's business
and continue to route to `ErrOutOfR0Scope`).

Dispatch on the ref target and the property axis:

- `ref.Variable` names a `NodeBinding` (resolved key in
  `resolvedNodeType`):
  - `ref.Property == ""` → `Column{Name, ResolvedNode{Labels: <key>}}`
    (R0 shape).
  - `ref.Property != ""` → look up `ref.Property` on
    `resolvedNodeType[ref.Variable].Properties`; miss →
    `ErrUnknownProperty`; hit → `Column{Name, ResolvedProperty{Type,
    Nullable}}` (R0 shape).
- `ref.Variable` names an `EdgeBinding` (resolved key in
  `resolvedEdgeKey` / type in `resolvedEdgeType`):
  - `ref.Property == ""` → `Column{Name, ResolvedEdge{EdgeKey: <key>}}`
    (new at R1).
  - `ref.Property != ""` → look up `ref.Property` on
    `resolvedEdgeType[ref.Variable].Properties`; miss →
    `ErrUnknownProperty`; hit → `Column{Name, ResolvedProperty{Type,
    Nullable}}` (new at R1).

A `ref.Variable` naming a binding of an unsupported kind (path,
unwind, call) cannot reach this step at R1 — Phase A already rejected
it via `ErrOutOfR0Scope`. A `ref.Variable` naming a binding not present
in the part is impossible: the parser rejects unbound variables at
build time (R0 §5 records this).

### 4.6 Step 5 (extended) — parameter resolution over N bindings

Iterate `Query.Parameters`; each `Parameter` is still required to have
exactly one `Use`, and that `Use` is still required to be a
`PropertyUse` (non-`PropertyUse` and `len(Uses) != 1` continue to
route to `ErrOutOfR0Scope`; R2 owns unification).

Dispatch on the property use's target:

- `use.Ref().Variable` names a `NodeBinding` (labelled or Phase B
  inferred): look up on `resolvedNodeType[…].Properties`.
- `use.Ref().Variable` names an `EdgeBinding`: look up on
  `resolvedEdgeType[…].Properties`.

Miss → `ErrUnknownProperty`. Hit → `ResolvedParameter{Name, Type:
ResolvedProperty{Type, Nullable}}`. Behaviour and sentinel unchanged
from R0; the schema-witness source widens to include edges.

### 4.7 Step 6 (unchanged) — statement kind

Copy `Query.StatementKind` into `ValidatedQuery.Statement`
(R1 capability scope is read-only; the field is present for wire
stability).

---

## 5. Sentinels — the R1 revision

R0's three sentinels stand. R1 adds two and revises the message-set of
two more. The `allSentinels` list gains the two new members; the R0
`invalidFixtures` map gains rows for each; the reachability sweep
extends transparently.

### 5.1 New sentinels

```go
// ErrUnknownEdge is returned when a directed single-hop edge binding's
// endpoints and label form an EdgeKey the schema does not declare —
// i.e., the schema has no edge of that label with that (source, target)
// pair. Introduced at R1.
var ErrUnknownEdge = errors.New("unknown edge")

// ErrAmbiguousBinding is returned when an unlabelled node binding
// cannot be uniquely typed from the edges that touch it: either its
// candidate set (across touching edges) has more than one node type,
// or the pattern's unlabelled bindings form a cycle no single edge
// can break. Introduced at R1.
var ErrAmbiguousBinding = errors.New("ambiguous binding")
```

**Naming defence — `ErrUnknownEdge`, not `ErrUnknownEdgeType` or
`ErrUnknownRelationship`.** `ErrUnknownLabel` is the R0 shape ("the
node type identified by this label set is not in the schema"); the
symmetric edge case is "the edge type identified by this (source,
label, target) triple is not in the schema" — `ErrUnknownEdge`. The
schema uses `EdgeType` for the *declared type* and `EdgeKey` for its
canonical identity; the sentinel names the thing the resolver could
not find (an edge in the schema), which reads as "unknown edge" the
way a compiler says "unknown identifier". `ErrUnknownRelationship`
imports the openCypher noun into the resolver vocabulary; the resolver
speaks in `EdgeKey`, so `ErrUnknownEdge` is closer to the domain
language.

**Naming defence — `ErrAmbiguousBinding`, not `ErrAmbiguousLabel` or
`ErrUnresolvableBinding`.** The failure is on a specific binding (a
named query variable), not on a label — the ambiguity is in the schema
witnesses touching this one variable. `ErrUnresolvableBinding` reads
as a superset ("could not resolve"); the resolver's other
binding-related sentinels (`ErrUnknownLabel`, `ErrUnknownProperty`)
already cover "could not resolve because …", and the R1 addition is
specifically "resolved to more than one candidate". Cycle sub-case:
the fail-message says so; the sentinel identity is category-grained.

### 5.2 Revised sentinels

- **`ErrUnknownProperty`.** The prose gains "on the resolved node or
  edge type" wording — the miss set widens from R0's node-only to R1's
  node-or-edge. The sentinel identity and fail-message wrapping
  (`fmt.Errorf("%w: %s.%s", …)`) are unchanged; goldens continue to
  match on `errors.Is`.
- **`ErrOutOfR0Scope`.** The sentinel name is unchanged at R1 —
  category-grained per R0 §5; the sentinel is renamed only when its
  last fail-site retires (R6-ish). The set of constructs it covers
  shrinks by one (edges are no longer routed here; they are either
  resolved or route to `ErrUnknownEdge` / `ErrAmbiguousBinding`) and
  grows by explicit sub-rows for the R1 in-scope surface's boundaries:
  undirected edges, variable-length edges, multi-type edges, anonymous
  edges. The fail-message strings distinguish these — no new sentinel
  is introduced for them; R3 owns the retirement of the undirected /
  var-length / multi-type sub-cases together (ADR 0009 §Stages R3).

### 5.3 Not added at R1

- **`ErrParameterTypeConflict`.** R2's business (ADR 0009). R1's
  capability scope still guarantees exactly one `Use` per parameter,
  so unification is unreachable at R1 — adding the sentinel with no
  fail-site would break the reachability sweep.
- **`ErrUnboundVariable`.** The parser rejects unbound variables at
  build time (R0 §5 records the fail-site: `internal/query/cypher/
  build.go:157`). R1's kernel walks bindings that already exist and
  refs whose variables the parser guaranteed are bound. Defensive
  coverage lives in the parser, not the resolver.

### 5.4 The closed R1 set

```go
var allSentinels = []error{
    ErrUnknownLabel,     // R0; extended at R1 to include unlabelled-node no-candidate
    ErrUnknownProperty,  // R0; extended at R1 to include edge properties
    ErrOutOfR0Scope,     // R0; sub-cases widen (undirected, var-length, multi-type, anon edge)
    ErrUnknownEdge,      // R1
    ErrAmbiguousBinding, // R1
}
```

Five sentinels. Every member has at least one fixture (§6.4); every
fixture maps to a canonical sentinel. Bidirectional sweep unchanged.

---

## 6. The golden-pair harness — R1 revision

R0 §6's harness stands: the `test/data/resolver/{valid,invalid}` layout,
the `-update` flag, the invalid-fixture map, the reachability sweep, the
schema-mapping totality. R1 revises the fixture set only, not the
harness code.

### 6.1 Schema fixture strategy — one new schema, R0's untouched

The R1 valid schema (`social_r1.gql`) is a superset of R0's:
- The R0 `Person` node type (with `id`, `name`, `age`) unchanged.
- A `Post` node type with `id NOT NULL`, `title NOT NULL`, and a
  nullable `body`.
- An `AUTHORED` edge from `Person` to `Post` carrying `at ::
  TIMESTAMP` (nullable via absent NOT NULL).
- A `LIKES` edge from `Person` to `Post` (property-free).

Two edge types on the same `Person → Post` pair are deliberate: the
unlabelled-binding inference tests (§6.3) rely on the schema having
enough shape that an unlabelled node binding on the source side of
`AUTHORED` uniquely infers to `Person`, and a fixture with a
non-uniquely-inferable schema (multiple edges from different sources
into `Post`) is included as an ambiguity fixture (§6.4).

The R0 valid schema (`social.gql`, node-only) stays as-is; the R0 valid
fixtures continue to point at it via `schema.mapping.json`. R1 adds new
mapping rows for the R1 fixtures pointing at `social_r1.gql`.

The R0 invalid schema (`invalid/schemas/social.gql`, already
`Person`+`Post`+`AUTHORED`) is unchanged; the R0 invalid fixtures
(`edge_binding.cypher`, etc.) that were R0-out-of-scope will change
sentinels at R1 (see §6.4). To keep R1 negative fixtures for the new
sentinels compact, a second invalid schema `social_ambiguous.gql`
is added: two node types (`Author`, `Publisher`) each with an
`AUTHORED` edge to `Post`, so an unlabelled binding on the
`Post`-side-source of `AUTHORED` has two candidates.

### 6.2 Schema fixture text

`test/data/resolver/valid/schemas/social_r1.gql`:

```gql
CREATE PROPERTY GRAPH TYPE SocialR1 AS {
    (:Person {
        id      :: INT NOT NULL,
        name    :: STRING NOT NULL,
        age     :: INT
    }),
    (:Post {
        id      :: INT NOT NULL,
        title   :: STRING NOT NULL,
        body    :: STRING
    }),
    (:Person) -[:AUTHORED { publishedAt :: TIMESTAMP }]-> (:Post),
    (:Person) -[:LIKES]-> (:Post)
}
```

**Property-name erratum.** The originally-drafted property name `at`
and the parameter name `$when` in §6.3 below are both reserved lexer
tokens: `AT` is a GQL keyword (`internal/grammar/gql/GQL.g4:3288`) and
`WHEN` is a Cypher keyword (`internal/grammar/cypher/Cypher.g4:390`),
so the ANTLR-generated parsers reject them before any resolver code
runs. Fixtures use `publishedAt` and `$pubTime` instead. Semantically
identical — the property still has type `TIMESTAMP` and nullability
`true` from the schema; the parameter still unifies at
`ResolvedProperty{TIMESTAMP, Nullable=true}`.

`test/data/resolver/invalid/schemas/social_ambiguous.gql`:

```gql
CREATE PROPERTY GRAPH TYPE SocialAmbiguous AS {
    (:Author { id :: INT NOT NULL }),
    (:Publisher { id :: INT NOT NULL }),
    (:Post { id :: INT NOT NULL }),
    (:Author) -[:AUTHORED]-> (:Post),
    (:Publisher) -[:AUTHORED]-> (:Post)
}
```

The invalid `social.gql` (Person+Post+AUTHORED) stays as authored at
R0; it continues to serve fixtures that need a legal Person→AUTHORED→
Post shape.

### 6.3 R1 valid fixtures

Added under `test/data/resolver/valid/`:

| Fixture | Shape | Schema |
|---|---|---|
| `edge_labelled_both_endpoints.cypher` | `MATCH (p:Person)-[r:AUTHORED]->(post:Post) RETURN p, r, post` | social_r1 |
| `edge_property_projection.cypher` | `MATCH (p:Person)-[r:AUTHORED]->(post:Post) RETURN r.publishedAt` | social_r1 |
| `edge_property_parameter.cypher` | `MATCH (p:Person)-[r:AUTHORED { publishedAt: $pubTime }]->(post:Post) RETURN p.name` | social_r1 |
| `inline_endpoint_source.cypher` | `MATCH (:Person)-[r:AUTHORED]->(post:Post) RETURN post, r` | social_r1 |
| `inline_endpoint_target.cypher` | `MATCH (p:Person)-[r:AUTHORED]->(:Post) RETURN p, r` | social_r1 |
| `unlabelled_binding_from_edge.cypher` | `MATCH (a)-[r:AUTHORED]->(p:Post) RETURN a, r, p` | social_r1 |
| `unlabelled_binding_target_inferred.cypher` | `MATCH (p:Person)-[r:AUTHORED]->(x) RETURN x, r` | social_r1 |
| `two_edges_shared_binding.cypher` | `MATCH (p:Person)-[a:AUTHORED]->(post:Post)<-[l:LIKES]-(p2:Person) RETURN p, post, p2` | social_r1 |

Each fixture is one Cypher file; each has one paired
`.validated.golden.json` regenerated by `-update`. The
`schema.mapping.json` grows one row per fixture.

**Coverage sketch (per row):**

- `edge_labelled_both_endpoints`: exercises the Phase A EdgeKey
  formation happy path (both endpoints labelled `VarEndpoint`); the
  golden columns are `ResolvedNode(Person)`, `ResolvedEdge(EdgeKey{
  Person, AUTHORED, Post })`, `ResolvedNode(Post)`.
- `edge_property_projection`: exercises §4.5's edge-property lookup
  (`ResolvedProperty{TIMESTAMP, Nullable=true}`).
- `edge_property_parameter`: exercises §4.6's edge-parameter typing
  (`ResolvedParameter{Name: "pubTime", Type:
  ResolvedProperty{TIMESTAMP, Nullable=true}}`).
- `inline_endpoint_source` / `inline_endpoint_target`: exercises
  `InlineEndpoint` labelling (the endpoint's own labels).
- `unlabelled_binding_from_edge`: exercises Phase B (the unlabelled
  `a` is inferred to `Person` — the unique source of `AUTHORED →
  Post`).
- `unlabelled_binding_target_inferred`: symmetric on the target side.
- `two_edges_shared_binding`: exercises a two-edge pattern where a
  binding sits on both edges' endpoints; verifies the golden orders
  columns in projection order regardless of walk order.

### 6.4 R1 invalid fixtures — updated `invalidFixtures` map

The R0 map's rows for R1-now-valid constructs (edge bindings) change or
retire; two new rows are added for the R1 sentinels. Aggregate/return-
distinct/returns-all fixtures stay (still R2/R5's business).

```go
var invalidFixtures = map[string]error{
    // R0 rows, unchanged
    "unknown_label.cypher":        ErrUnknownLabel,
    "unknown_property.cypher":     ErrUnknownProperty,
    "with_clause.cypher":          ErrOutOfR0Scope,
    "aggregate_projection.cypher": ErrOutOfR0Scope,
    "return_distinct.cypher":      ErrOutOfR0Scope,
    "returns_all.cypher":          ErrOutOfR0Scope,

    // R0's edge_binding.cypher retires (was ErrOutOfR0Scope; now a
    // valid fixture at R1). The R0 file is deleted from invalid/.

    // R1 rows, new
    "unknown_edge.cypher":              ErrUnknownEdge,
    "unknown_edge_property.cypher":     ErrUnknownProperty,
    "ambiguous_unlabelled_binding.cypher": ErrAmbiguousBinding,
    "unlabelled_binding_no_edge.cypher": ErrUnknownLabel,
    "empty_inline_endpoint.cypher":     ErrUnknownLabel,
    "undirected_edge.cypher":           ErrOutOfR0Scope,
    "var_length_edge.cypher":           ErrOutOfR0Scope,
    "multi_type_edge.cypher":           ErrOutOfR0Scope,
}
```

**R1 invalid fixture contents (illustrative):**

- `unknown_edge.cypher`: `MATCH (p:Person)-[:KNOWS]->(q:Person)
  RETURN p` — schema social (R0 invalid) has no `KNOWS`.
- `unknown_edge_property.cypher`: `MATCH (p:Person)-[r:AUTHORED]->
  (post:Post) RETURN r.priority` — `AUTHORED` has no `priority`.
- `ambiguous_unlabelled_binding.cypher`: `MATCH (x)-[:AUTHORED]->
  (p:Post) RETURN x` — schema `social_ambiguous` has both `Author →
  AUTHORED → Post` and `Publisher → AUTHORED → Post`; `x` cannot be
  uniquely inferred.
- `unlabelled_binding_no_edge.cypher`: `MATCH (n) RETURN n` — no
  labels, no touching edges; Phase B's candidate set is empty →
  `ErrUnknownLabel` per §4.3.
- `empty_inline_endpoint.cypher`: `MATCH (p:Person)-[r:AUTHORED]->()
  RETURN r` — exercises Phase A2's empty-labels `InlineEndpoint`
  branch (§4.2 lines admitting inline endpoints) and its Phase C
  closure (§4.4): the anonymous target has no name for Phase B to
  resolve into, so Phase C fails with `ErrUnknownLabel` naming the
  endpoint's textual position.
- `undirected_edge.cypher`: `MATCH (p:Person)-[r:AUTHORED]-(q:Post)
  RETURN r` — undirected; R3's business.
- `var_length_edge.cypher`: `MATCH (p:Person)-[r:AUTHORED*1..3]->
  (q:Post) RETURN p` — var-length; R3's business.
- `multi_type_edge.cypher`: `MATCH (p:Person)-[r:AUTHORED|LIKES]->
  (q:Post) RETURN r` — multi-type; R3's business.

Each fixture is paired to its schema via `invalid/schema.mapping.json`,
extended to include the new fixtures and the `social_ambiguous.gql`
schema.

### 6.5 Determinism check

The R1 kernel's iteration order is:
- `Part.Bindings` in first-appearance order (§4.2 note).
- `Part.Returns` in source order (R0 §2.3).
- `Query.Parameters` in query-wide first-appearance order (R0 §2.3).
- Ambiguous-binding candidate list sorted ascending (§4.3).

Every ordered surface is either the parser's guaranteed order or a
sort — no map iteration escapes into the output. The golden JSON is
deterministic; `-update` regenerates a byte-stable file.

### 6.6 Non-obvious harness invariants — R1 additions

R0's §6.6 invariants stand. R1 adds:

- **Fixture files are named for shape, not sentinel.** `unknown_edge`
  names the sentinel; `ambiguous_unlabelled_binding` names the
  shape+sentinel; `unlabelled_binding_no_edge` names the shape. R1
  keeps the convention loose — fixture names are for the reader's
  scan, the pairing is the mapping.
- **Fixture-to-schema pairing is many-to-one.** All R1 valid fixtures
  point at `social_r1.gql`; the invalid corpus uses two schemas
  (`social.gql` for the R0-carryover shapes and `unknown_edge`,
  `social_ambiguous.gql` for `ambiguous_unlabelled_binding`).
  `schema.mapping.json` totality is enforced against the fixture dir,
  so an unmapped fixture fails the test at load.
- **`unlabelled_binding_no_edge.cypher` and its schema.** `MATCH (n)
  RETURN n` is a single-node no-labels no-edges case; Phase A leaves
  `n` unlabelled, Phase B finds no touching edges. The candidate set
  is empty, so the sentinel is `ErrUnknownLabel` (§4.3 verdict-0
  branch). The schema is any legal schema — `social.gql` (invalid
  dir's) suffices. The fixture is *not* an `ErrOutOfR0Scope` case at
  R1: R0 rejected a single unlabelled node binding as unlabelled
  (`internal/resolver/resolve.go:44`); R1's Phase B is the successor
  gate, so the sentinel changes from R0's out-of-scope to
  `ErrUnknownLabel` (aligned with the ADR 0003 consequence: an
  unlabelled binding *is* resolvable only when an edge touches it).

---

## 7. R1 capability scope — what resolves

**In scope:** a single Cypher query the parser accepts, whose parsed
`query.Query` shape is:

- Exactly one `Branch` in `Branches`; zero `Combinators`.
- Exactly one `Part` in the branch's `Parts`.
- The part's `Bindings` are a non-empty slice of `NodeBinding` and/or
  `EdgeBinding` values:
  - Each `NodeBinding` is either labelled (non-empty `Labels()`) or
    unlabelled (empty `Labels()`) — an unlabelled node binding is
    resolved via Phase B (§4.3) if at least one supported edge touches
    it.
  - Each `EdgeBinding` is directed (`Directed() == true`), single-hop
    (`Hops() == nil`), single-type (`len(Labels()) == 1`), and named
    (`Variable() != ""`) *or* anonymous with a labelled endpoint pair
    that lets Phase A form an EdgeKey. **Anonymous edges are in
    scope** at R1 because they are legal openCypher and the parser
    produces an `EdgeBinding` for them with empty `Variable()` (see
    `internal/query/cypher/pattern.go:291-301`); the resolver forms
    their EdgeKey identically. The R1 rejection is on **anonymous edges
    the fixture cannot reach as a projected column** — projected
    columns require named bindings, but the pattern itself is legal.
    (An anonymous edge is a filter, not a projected value.)
- The part's `Returns` is a non-empty slice of `ReturnItem`s, each
  carrying a `RefProjection` whose `Ref` names one of the part's
  **named** bindings (a node or an edge), either whole-entity
  (`Property == ""`) or single-level property (`Property != ""`).
- `ReturnsAll` is false; `Distinct` is false; `Effects` is empty.
- `Parameters` is a slice of `Parameter`s each with exactly one
  `PropertyUse` sitting on the inline property map of a **named**
  node or edge binding.
- `StatementKind` is `StatementRead`.

**Out of scope, routed to the appropriate sentinel:**

| Construct                                              | Sentinel             | R-stage owner |
|--------------------------------------------------------|----------------------|---------------|
| Undirected edge (`Directed() == false`)                | `ErrOutOfR0Scope`    | R3            |
| Multi-type edge (`len(Labels()) > 1`)                  | `ErrOutOfR0Scope`    | R3            |
| Variable-length edge (`Hops() != nil`)                 | `ErrOutOfR0Scope`    | R3            |
| Untyped edge (`len(Labels()) == 0`)                    | `ErrOutOfR0Scope`    | R-later†      |
| Path binding                                           | `ErrOutOfR0Scope`    | R5            |
| Unwind binding                                         | `ErrOutOfR0Scope`    | R5 or later   |
| Call binding                                           | `ErrOutOfR0Scope`    | R7            |
| Non-`RefProjection` items                              | `ErrOutOfR0Scope`    | R2            |
| Parameter with more than one `Use`                     | `ErrOutOfR0Scope`    | R2            |
| `ClauseSlotUse` / `ExprUse` parameter                  | `ErrOutOfR0Scope`    | R2 / R5       |
| Nullability upgrades (OPTIONAL MATCH regimes)          | `ErrOutOfR0Scope`    | R4            |
| `Part.Distinct == true`                                | `ErrOutOfR0Scope`    | R5            |
| `Part.ReturnsAll == true`                              | `ErrOutOfR0Scope`    | R5            |
| WITH carry-forward; UNION                              | `ErrOutOfR0Scope`    | R5            |
| Writes / CREATE / MERGE / SET / REMOVE / DELETE        | `ErrOutOfR0Scope`    | R6            |
| CALL / YIELD                                           | `ErrOutOfR0Scope`    | R7            |
| Labelled edge with no matching schema EdgeKey          | `ErrUnknownEdge`     | R3 widens     |
| Property lookup with no matching schema property       | `ErrUnknownProperty` | (R2 widens)   |
| Labelled node with no matching schema NodeType         | `ErrUnknownLabel`    | (R2 widens)   |
| Unlabelled node with an empty candidate set from edges | `ErrUnknownLabel`    | —             |
| Unlabelled node with a multi-candidate set             | `ErrAmbiguousBinding`| —             |

† Untyped edges (`(a)-->(b)` with no `[:LABEL]`) are not explicitly
named in ADR 0009's R3 line, which enumerates only "undirected
two-orientation trial", "multi-type edges", and "var-length hop-range
lookups". An untyped edge candidate set is `{k ∈ Schema.Edges |
k.Source == …, k.Target == …}` — the resolver walks every schema edge
whose endpoints match, with no label filter. That surface has the
same "multi-candidate outcome, no unique EdgeKey" shape as multi-type
edges and the same "resolver-side surface" character as undirected —
R3 is the only stage plausibly close to it, but naming it R3 without
an ADR citation would overspecify. Marked R-later; the stage that
takes it up defends the placement in its own spec cycle.

**Silently accepted (not routed anywhere):**

R0's silently-accepted set stands unchanged. Literal-only WHERE / ORDER
BY / SKIP / LIMIT continue to leave no witness in the frozen
`query.Query`; ADR 0005 continues to say the original text runs. R1
does not extend the silently-accepted set.

---

## 8. Ground-truth cross-check

Every factual claim in this spec is verifiable against source; the
citations below name the file:line the claim rests on. A future reader
should be able to open each citation and confirm the shape the spec
describes still holds.

- **R1 capability scope's edge-binding predicate** — `Directed() ==
  true` (`internal/query/query.go:445-447`), `Hops() == nil`
  (`internal/query/query.go:449-452`), `len(Labels()) == 1`
  (`internal/query/query.go:434-437`).
- **Endpoint sum** — `VarEndpoint` and `InlineEndpoint`
  (`internal/query/query.go:934-980`); closed at two variants; both
  hold data in unexported fields.
- **Multi-type relationship lowers as a `LabelSet` on `EdgeBinding`** —
  `relTypes` (`internal/query/cypher/pattern.go:430-447`); an `[r:A|B]`
  yields `LabelSet{"A", "B"}` on the binding, so
  `len(Labels()) > 1` is the discriminator.
- **Anonymous edge is its own binding** — `internal/query/cypher/
  pattern.go:291-301`; empty `Variable()`, populated `Source()` and
  `Target()`.
- **Directed left-arrow canonicalises** — `internal/query/cypher/
  pattern.go:261-263`; a `<--` edge is stored source→target.
- **`schema.EdgeKey`** — `internal/schema/schema.go:36-40`; the
  `(Source, Label, Target)` triple with all three
  `graph.LabelSetKey`.
- **`schema.Schema.Edges` keying** — `internal/schema/schema.go:12-16`;
  `map[EdgeKey]EdgeType`. The R1 kernel iterates this map for Phase B
  candidate collection.
- **`RefProjection`'s Stage-6 type on an edge** — `TypeEdge` for a
  single-hop whole-entity ref (`internal/query/cypher/expr.go:283-296`
  and `internal/query/cypher/listener.go:319-321`); the R1 kernel
  reads `Ref.Variable`'s binding kind, so this is a corroboration,
  not a discriminator.
- **Cypher parser rejects unbound variables at build time** —
  `internal/query/cypher/build.go:157` (per R0 §5); no resolver
  sentinel needed.
- **`Part.Bindings` iteration order** — `internal/query/query.go:81-94`;
  bindings are appended in the parser's textual first-appearance order,
  and Part.Bindings is preserved verbatim through `NewPart`.

---

## 9. Definition of done for R1 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is out
of scope of this document. The spec is done when:

1. This file exists at `docs/specs/resolver-stage-r1.md`, committed on
   branch `resolver-r1-spec`.
2. §4 gives the algorithm for both R1 capabilities (EdgeKey formation
   and unlabelled-binding inference), with the fixed-point termination
   argument (§2.3) and the deterministic tie-break rules (§4.3, §6.5).
3. §3 names the one new `ResolvedType` variant (`ResolvedEdge`) and
   confirms `ResolvedProperty`'s reuse for edge properties/parameters.
4. §5 names and defends the two new sentinels (`ErrUnknownEdge`,
   `ErrAmbiguousBinding`) and revises the message-sets of the two
   sentinels that widen (`ErrUnknownProperty`, `ErrOutOfR0Scope`).
5. §6 designs the fixture set: the R1 valid schema `social_r1.gql`,
   the R1 valid fixture list, the R1 ambiguity schema
   `social_ambiguous.gql`, and the revised `invalidFixtures` map.
6. §7 states the R1 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel each construct routes to
   and the R-stage that owns the next widening.
7. §8 cross-checks every factual claim against source file:line.
8. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer); every
blocker he raises is fixed on this same branch before the branch
merges. Cycle 2 (the R1 code cycle) begins only when the spec cycle
merges.
