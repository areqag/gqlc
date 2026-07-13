# Resolver: the carried scope becomes a module (`gqlc-ls8.1`)

The resolver's per-branch carry — what a query part knows about each in-scope
binding across a `WITH` — is one concept. Today it is ten parallel maps
(`branchState.exported*`, `internal/resolver/resolve.go:88`) plus a five-field
parameter-witness snapshot (`partScope`, `:61`), threaded through helpers as
wide parameter lists (`exportScope` takes 12; `projectionType` and
`refProjectionType` take 10 each; every effect validator takes 7). A deletion
test on any one map today rewrites twenty-plus call sites. This spec pins the
deepened shape: **one `scope` module owning the ten lanes behind a small set
of lookup / carry-forward / demote operations**, so the helpers take one scope
value and the ten maps become private implementation.

This is a **pure internal refactor**. Nothing on the resolver's exported API
moves — `resolver.New`, `(*Resolver).Resolve`, `ValidatedQuery`, and its
`Column` / `ResolvedParameter` sums are pinned by ADR 0008 and unchanged.
Nothing on the `query.Query` / `schema.Schema` inputs moves. The 136 resolver
golden pairs are byte-identical without `-update`; `-update` is forbidden this
cycle. The refactor is judged by the interface it exposes and the locality it
buys downstream, not by novel behaviour.

Non-goals, explicit:

- **The ay9 cross-Part group-carry residual is not this cycle's target.**
  The `exportedOptionalGroup` lane and its consumption in
  `demoteNullableInPlace` (`resolve.go:1448-1547` — the `carriedGroups`
  seed + fixed-point loop) already carry group ids across `WITH` today
  and demote co-introduced siblings via the same closure. Whatever remains
  of the ay9 close-out note is a bead-triage question, not a code question;
  this cycle does not touch that surface.
- **No behaviour change of any kind.** Every fixture output is
  byte-identical. Any diff a golden shows is a defect.
- **No new sentinel.** The refactor moves code; it introduces no new
  reachable error path.
- **No `snapshotScope` inlining across the resolveBranch / top-level
  boundary.** `partScope` stays a distinct value carried out of
  `resolveBranch` — the parameter walker witnesses against the exact
  post-Phase-D tables and cannot see the local Part's private state.
  The scope module is where a Part *works*; `partScope` is what leaves
  a Part *for parameter attribution*. Two shapes, one derivation.

## 1. What moves

The following are folded into the scope module:

- `branchState`'s ten `exported*` fields (`resolve.go:88-99`) — become the
  module's *carry-in* state, seeded by `newScope`, and its *carry-out*
  state, returned by `Export`. Same envelope shape at both ends.
- The Part-local maps `nodeTypes`, `edgeTypes`, `edgeKeys`, `edgeCands`,
  `edgeBindings`, `nullableBinding`, `callTypes` (`resolve.go:168-193` seed
  loop, then written to by Phases A/B/C/D) — become the module's *live*
  state during a Part.
- The seed loop from carry → local (`:174-193`) — becomes the scope
  module's constructor from a prior carry.
- **The Part itself.** `Ingest(part query.Part)` on scope, called from
  `resolvePart` right after `newScope(carry)`, takes ownership of
  `part.Bindings`, `part.Effects`, `part.Returns`, `part.ReturnsAll`,
  and `part.Distinct` — they become private receiver fields for the
  duration of the Part cycle. Every per-phase method reads them off
  the receiver; the caller never re-supplies. **Closes D2 from
  Linus's grill**: the ordered `bindings []query.Binding` and
  `effects []query.Effect` become the eleventh and twelfth private
  lanes, not caller-threaded arguments. A future lane keyed on
  "which binding introduced this name" is a scope-internal edit,
  not a `resolvePart` edit.
- **`carriedGroups`** — the ay9 fixed-point's `map[string]int` — is a
  *carry lane on scope*, seeded by `newScope` from
  `branchState.exportedOptionalGroup`. `DemoteNullability` reads it
  from the receiver, not from a parameter. **Closes D1 from Linus's
  grill**: §2.1 and §2.2 both place it on the scope now, consistently.
- The shadow / delete cascades wired into resolvePart's A1
  (`:229-234, :256-263, :272-276`) — become methods
  (`scope.BindNode`, `scope.BindEdge`, `scope.BindCall` — each with the
  cross-lane shadow semantics baked in). These methods do NOT append to
  the private `bindings` slice — `Ingest` already owns the ordered list
  in parser order. Bind* only writes to the type/nullability/call lanes
  and runs the shadow cascade. This separation matters: Phase A1's
  admission order (labelled nodes first, then edges, then calls) is not
  the parser's binding order, so mixing the two would corrupt
  `Order`'s determinism.
- `snapshotScope`'s five-map deep-copy (`:903-927`) — becomes a
  `scope.Snapshot() partScope` method.
- **The full projection walk** — `buildScopeOrder` (`:479`),
  `virtualProjection` (`:449`), `materialiseReturns` (`:426`),
  `projectionType` / `refProjectionType` (`:1181` / `:1206`), and the
  per-item loop at `:397-404` — becomes one scope method
  `ResolveProjections(sch schema.Schema) ([]Column, error)`. Columns
  and materialised items become receiver state after the walk. Export
  reads them off the receiver. **Closes D3 from Linus's grill**:
  Export no longer takes columns / items / scopeOrder as parallel
  argument slices.
- `exportScope`'s wildcard-vs-explicit-Returns branch and its per-item
  binding-map population (`:524-638`) — becomes a parameter-free
  `scope.Export() branchState` method.
- `demoteNullableInPlace` (`:1427-1548`) — becomes a parameter-free
  `scope.DemoteNullability()` method that mutates the receiver's
  `nullableBinding` in place, reading `bindings` and `carriedGroups`
  off the receiver.
- The property-witness dispatch `propertyUseWitness` (`:1365-1391`) —
  becomes a method on `partScope`, taking a `query.Ref` and the schema.

The following **do not move**:

- The Part-level control flow inside `resolvePart` (Phase A1, A2, B, C, D,
  E, and the projection walk). Those stay named where they are; each phase
  calls scope methods instead of manipulating parallel maps directly.
- The top-level branch walker `resolveBranch` and the query walker
  `resolve`.
- `unifyParameterUsesAcrossBranches`, `witnessAcrossScopes`,
  `scopeContains` — they consume `partScope`, which stays a distinct
  external shape (§3.2).
- Every helper below the Column / ResolvedType layer
  (`resolveType`, `unify`, `unionProperty`, `callProjectionType`,
  `argAssignable`, `edgeCandidates`, `closeEdge`, `inferUnlabelled`,
  `qualifiedDemoter`, `resolvedTypeEqual`) — they operate on schema
  primitives, not on the scope, and stay verbatim.
- `parameterUseSite`, `useSitesToScopes` — unchanged.
- `branchState` itself, as a wire between Parts. It is now built by
  `scope.Export` and consumed by `scope.NewFromCarry`; the type is not
  deleted, it becomes the module's I/O envelope (§3.1). Choice of
  whether to keep the name `branchState` or rename to `scope.Carry` is a
  Phase-1 spec-round decision to close before implementation — the shape
  is more load-bearing than the name.

## 2. The scope module

New file: `internal/resolver/scope.go`. The scope module is one type
`scope` (unexported — the resolver is package-internal, no external
consumer) with a fixed set of methods. The ten maps become private fields;
callers touch them only via methods. The five `partScope` fields stay
separate — see §3.2.

The scope's job is: hold the tables in effect at one Part, evolve them
through the Part's phases, and hand off two things at the end — the
Part's outgoing carry, and the parameter-attribution snapshot.

### 2.1 Type and construction

```go
// scope is the resolver-typed state evolving through one Part's phases.
// Fields are private; every mutation runs through a method so the
// twelve lanes stay consistent (a delete on one lane cascades to the
// others that mirror it).
type scope struct {
    // Live tables — written by Phases A/B/C/D via Bind*/CloseEdge/
    // InferUnlabelled/SeedLocalNullability/DemoteNullability.
    nodeTypes       map[string]schema.NodeType
    edgeTypes       map[string]schema.EdgeType
    edgeKeys        map[string]schema.EdgeKey
    edgeCands       map[string][]schema.EdgeKey
    edgeBindings    map[string]query.EdgeBinding
    nullableBinding map[string]bool
    callTypes       map[string]callBindingSlot

    // Ingested Part — set once by Ingest, read by every phase method.
    bindings   []query.Binding
    effects    []query.Effect
    returns    []query.ReturnItem
    returnsAll bool
    // Distinct is a Part-level bit consumed by the top-level
    // computeDistinct walk; scope carries it for symmetry (Ingest
    // captures the whole Part shape) but ResolveProjections /
    // Export do not read it.
    distinct bool

    // Projection-walk outputs — set by ResolveProjections, read by
    // Export.
    columns    []Column
    items      []query.ReturnItem
    scopeOrder []string

    // Carry-in lanes: seeded from the incoming branchState, read by
    // downstream phases, never written within this Part.
    carriedResolvedTypes map[string]ResolvedType
    carriedOrder         []string
    carriedGroups        map[string]int
}

// newScope seeds a scope from Part K's exported carry — the ten
// fields of branchState. Part 0's carry is the zero-value branchState
// (nil maps everywhere) and the constructor treats it as empty
// without nil-guards at every read.
func newScope(carry branchState) *scope

// Ingest binds the current Part into the scope. Called exactly once
// per Part cycle, immediately after newScope. After Ingest, every
// per-phase method reads the Part shape off the receiver.
func (s *scope) Ingest(part query.Part)
```

The seed loop currently at `resolve.go:174-193` moves into `newScope`. The
zero-carry case (Part 0, `var carry branchState`) is handled once here;
nowhere else needs `if carry.exportedX != nil`.

### 2.2 The interface, exhaustively

Every operation `resolvePart` performs on the ten maps runs through
one of these methods. This is the deletion test the current shape
fails and the new shape must pass: removing a lane means editing
this list plus its implementation, nowhere else. After the
D2/D3 rework, methods do not take Part-shape arguments — the
Part is on the receiver.

**Construction / Part ingestion** (already declared in §2.1 —
listed here for the count):

- `newScope(carry branchState) *scope`
- `(*scope).Ingest(part query.Part)`

**Local binding admission** (used by Phase A1, resolve.go:200-323).
`resolvePart`'s type switch still dispatches over `s.bindings`; each
arm calls one Bind method with the single binding it decoded and, for
nodes, the schema lookup it already performed. A batched
`BindAll(sch)` was considered and rejected: Phase A1's error posture
returns on the *first* fault with a variant-specific message, and
hiding the switch inside scope would either force scope to import
every sentinel string or return a coarse union sentinel that swallows
the Class-A/B/R7 distinctions the fixtures pin.

```go
// BindNode admits a labelled NodeBinding into scope. Cascades shadow /
// delete on the edge, call, and nullable lanes for the same variable
// per R5 §4.2.3 (the current :229-234 delete block). Returns
// ErrPartBindingTypeConflict if a carried entry at the same name has
// a distinct LabelSetKey. The R7 §4.1.2.1 call-vs-node shape check
// runs first. Does NOT append to s.bindings — Ingest owns that lane.
func (s *scope) BindNode(nb query.NodeBinding, nt schema.NodeType) error

// BindEdge admits a labelled EdgeBinding into scope's edgeBindings
// lane (Phase A1's supportedEdges arm). Cascades node / call / nullable
// shadow. Registers the binding for Phase A2/C to close endpoints
// against. Returns ErrPartBindingTypeConflict per R5 §6.4 edge parity
// and R7 §4.1.2.2.
func (s *scope) BindEdge(eb query.EdgeBinding) error

// BindCall admits a CallBinding — the R7 §4.1 arm. Cascades node /
// edge shadow, registers the slot for the projection walk and carry.
// Returns ErrPartBindingTypeConflict on a same-Part duplicate; runs
// the 0ig arg-assignability check against sch's registry.
func (s *scope) BindCall(cb query.CallBinding, r procsig.Registry) error
```

**Edge closure** (Phase A2 + C, `resolve.go:326-361`):

```go
// CloseEdges runs Phases A2 and C over s.bindings' edge bindings
// against the schema: A2 tries every supported edge; unfulfilled ones
// defer to Phase B; C retries the deferred set. The two-pass
// scaffolding at :326-361 becomes one scope method, and the
// pendingNodes / deferredEdges local slices become receiver-scoped
// during the call. Returns the first ErrUnknownLabel / edge-close
// error.
func (s *scope) CloseEdges(sch schema.Schema) error

// InferUnlabelled runs Phase B against s.bindings' pending
// unlabelled nodes. Writes inferred entries through the same shadow
// cascade BindNode uses. R7 §4.1.2.1 call-collision guard preserved.
func (s *scope) InferUnlabelled(sch schema.Schema) error
```

`resolvePart`'s A1/A2/B/C phases collapse to one call each:
`s.CloseEdges(sch)` covers A2+C (the two-pass logic is scope-internal),
`s.InferUnlabelled(sch)` covers B. The `pendingNodes` /
`supportedEdges` / `deferredEdges` slices leave `resolvePart` entirely.

**Nullability** (Phase D, `resolve.go:363-372`):

```go
// SeedLocalNullability writes each binding's own Nullable() bit into
// the nullable lane, overwriting the carry per §4.6. Reads s.bindings.
func (s *scope) SeedLocalNullability()

// DemoteNullability runs the ay9+5xg-widened demotion in place: 5xg
// pre-pass, ay9 pre-pass, edge-driven fixed point. Reads s.bindings
// and s.carriedGroups; writes s.nullableBinding. Parameter-free.
func (s *scope) DemoteNullability()
```

**Effect validation** (Phase E, `resolve.go:378-380`):

```go
// ValidateEffects runs R6 Phase E over s.effects: dispatch each
// through its per-variant validator against the scope's committed
// binding tables and the carried resolved types. The seven per-variant
// validators (validateCreateEffect, validateMergeEffect, …) become
// unexported package-level helpers taking `*scope` — moving them onto
// the receiver directly would clutter the method surface without
// changing their reach. Single public entry.
func (s *scope) ValidateEffects(sch schema.Schema) error
```

**Projection walk** — one method covers the full `:384-404` block:

```go
// ResolveProjections runs the projection walk end-to-end: builds
// scopeOrder (§4.4.1), materialises Returns (RETURN * / WITH *
// expansion at §4.4.2, or verbatim s.returns), types each item via
// projectionType / refProjectionType. Populates s.scopeOrder,
// s.items, and s.columns. GroupingKey stays false — fillGroupingKeys
// (already a free function on `part` alone) is called by
// resolveBranch on the final Part, and does not need to be a method.
func (s *scope) ResolveProjections(sch schema.Schema) error
```

`buildScopeOrder`, `virtualProjection`, `materialiseReturns`,
`projectionType`, and `refProjectionType` become private methods on
scope reading receiver state. `callProjectionType`, `resolveType`,
`unionProperty` stay as free functions — they are schema-driven and
scope-agnostic, and putting them on the receiver widens the surface
without buying anything.

**Export / Snapshot** (end of resolvePart, `resolve.go:409-416`):

```go
// Snapshot returns the parameter-witness partScope this Part
// contributes: a deep copy of the five witness lanes. Called once per
// Part after Phase D.
func (s *scope) Snapshot() partScope

// Export builds the branchState Part K passes to Part K+1 (§4.2.2).
// Reads s.returns / s.returnsAll / s.items / s.columns / s.scopeOrder
// off the receiver. Parameter-free — the twelve-parameter
// exportScope's arguments are all scope-owned by the time Export
// runs.
func (s *scope) Export() branchState
```

**Total, post-rework**: **11 methods on `*scope`**, plus `newScope` and
`Ingest`. Down from the 14 in the initial draft. The collapse comes
from folding Order + VirtualProjection + ProjectionType +
materialiseReturns into `ResolveProjections`, and folding
`EndpointLabels` + the A2/C pass into `CloseEdges`.

**On `partScope`**: `propertyUseWitness` moves onto `partScope` as a
method, unchanged in semantics. `scopeContains` and
`witnessAcrossScopes` also become methods on `partScope` in step 6
(§5), tightening the parameter-walker's surface.

### 2.3 What the interface guarantees

Read as invariants a caller can rely on without opening the file:

1. **Shadow-cross-lanes is atomic.** After `BindNode(nb)`, no lane
   contains a stale entry at `nb.Variable()` — edgeTypes, edgeKeys,
   edgeCands, edgeBindings, callTypes are all deleted at that variable.
   The three cross-lane delete blocks currently interleaved with the
   admission logic (`:229-234`, `:256-263`, `:272-276`) are guaranteed
   in-order by the method itself, not by call-site discipline.

2. **Carry is seeded exactly once.** `newScope(carry)` is the only path
   that populates the lanes from a carry; no downstream method reaches
   into `carry.exported*`. The seed order (carry first, then local
   shadows) is a fact of the constructor, not a call-site convention.

3. **`partScope`'s five fields are a strict subset of the scope's
   seven witness-relevant live lanes** (nodeTypes, edgeTypes,
   edgeKeys, edgeCands, edgeBindings, nullableBinding, callTypes —
   the seven Phase A/B/C/D maps; the four Ingest-owned Part lanes
   and the three carry-in lanes are scope-private). Adding a new
   live lane to `scope` does not silently widen `partScope`; the
   Snapshot method is where the two shapes are pinned. If a new
   lane needs to reach the parameter walker, the widening must be
   explicit in both `partScope` *and* `Snapshot`.

4. **Demotion mutates only the `nullableBinding` lane.** The 5xg
   pre-pass, the ay9 pre-pass, and the edge fixed-point all write false
   to entries already present in `nullableBinding`. No other lane
   changes during demotion. This is testable by golden diff on a
   post-demotion Snapshot vs. a pre-demotion Snapshot with only the
   nullable field masked.

5. **Every `exportScope`-visible export is derived from post-demotion
   state.** The current call order (`demoteNullableInPlace` at :372,
   then `exportScope` at :416) is preserved: Phase D writes into the
   scope's nullable lane before Export consults it.

Any diff a golden shows against these invariants is a defect the
implementation, not the spec, must fix. These invariants are the
oracles §4's unit tests pin.

### 2.4 What is deliberately not on the interface

- **No public getters returning the maps themselves.** Callers that want
  a "is `v` a node in scope?" read use `scope.HasNode(v)` (a bool).
  Handing out `map[string]schema.NodeType` re-opens the deletion-test
  failure the refactor exists to fix.
- **No batch `Merge(other *scope)` operation.** The one merge shape the
  resolver needs — carry → local — is `newScope(carry)`; there is no
  cross-Part scope union anywhere, and speculating one would be
  ADR-0009-violating gold-plating.
- **No mutation of the incoming carry.** `newScope` deep-copies the ten
  incoming maps (as the current seed loop does at `:174-193`). A
  Part cannot mutate its predecessor's exported state, matching R5's
  functional model.

## 3. Two shapes: scope vs partScope

The resolver has two distinct scope-shaped things and the refactor
sharpens rather than merges them.

### 3.1 branchState — the Part→Part carry envelope

Unchanged in shape. Ten `exported*` fields, produced by
`scope.Export`, consumed by `newScope`. Its role is Part-boundary
transport; it is not touched inside a Part.

### 3.2 partScope — the parameter-witness snapshot

Unchanged in shape: five fields (nodeTypes, edgeTypes, edgeCands,
edgeBindings, nullableBinding). Its role is the *external* consumer's
view of the Part's post-demotion tables — the top-level parameter
walker uses it to attribute each PropertyUse to the exact
lexical-Part scope (fvo per ADR 0008 amendment 2026-07-06).

`partScope` remains a distinct type, deliberately narrower than
`scope`. Callers who witness Uses (`witnessAcrossScopes`,
`propertyUseWitness`, `scopeContains`) do not need the seven live
lanes, only the five witness-relevant ones. Keeping the shapes
separate:

- prevents the parameter walker from ever accidentally consulting
  live Part-internal state (like the carry lanes, or `callTypes`),
- gives `snapshotScope` a well-defined domain (map five of seven
  live lanes into five witness lanes),
- keeps the top-level unifier's interface unchanged, so this refactor
  is scoped to one function's implementation.

`propertyUseWitness` moves onto `partScope` as a method
(`(sc partScope) PropertyUseWitness(ref, sch) (ResolvedType, error)`),
eliminating its five map parameters — but it stays semantically
identical.

## 4. Test plan

The interface is the test surface. Tests live in a new file
`internal/resolver/scope_test.go` and exercise the scope module directly,
not through `Resolve`. Fixture golden pairs continue to pin the observable
end-to-end behaviour.

### 4.1 New unit tests (mandatory)

Each test constructs a `scope` directly (via `newScope(carry)` +
`s.Ingest(part)`) and asserts one invariant. No fixtures needed — the
tests exist to pin the scope-module's contract, so they are all
in-code table-driven Go tests. Because Bind/Close/Demote/Export are
parameter-free after the D2/D3 rework, every test's setup is:
"construct a `branchState`, construct a `query.Part`, run the phase
methods, assert on the receiver's observable output" (Snapshot,
Export, or a targeted read-only predicate).

1. **Empty carry + empty Part → empty scope.**
   `s := newScope(branchState{}); s.Ingest(query.Part{})` — every
   `HasX` predicate returns false; `Snapshot()` returns a `partScope`
   with five empty maps; `Export()` returns a zero-value `branchState`.

2. **Carry-forward round-trip (the deletion test).** A `branchState`
   `c1` seeded with a node, an edge, and one nullable entry round-trips
   through `newScope(c1); s.Ingest(part)` — where `part` is
   `ReturnsAll=true` with no local bindings — followed by
   `s.ResolveProjections(sch); s.Export()`. The returned `branchState`
   equals `c1` modulo the `exportedResolvedTypes` lane (which
   wildcard-expand populates). Dropping any of the ten carry lanes
   from scope + Export makes exactly this test fail.

3. **BindNode shadow cascade.** Seed a scope with
   `c.exportedEdgeBindings["r"]` populated; Ingest a Part whose
   Bindings contain one `NodeBinding{Variable: "r", ...}`; call
   `s.BindNode(nb, nt)`. Post-condition: `HasNode("r")` true;
   `HasEdgeBinding("r")` false. Every one of the five shadow lanes
   the current `:229-234` block deletes is checked via targeted
   `HasX` predicates. Symmetric tests for `BindEdge` (shadow
   node/edge-closed state) and `BindCall` (shadow node + edge).

4. **DemoteNullability — 5xg pre-pass.** Ingest a Part whose
   Bindings contain one binding with
   `ReferencedInRequiredBarePattern() == true` and a nullable table
   entry set to true. `s.SeedLocalNullability(); s.DemoteNullability()` —
   the entry is false. Re-running `SeedLocalNullability()` a second
   time reasserts the local Nullable() bit (idempotence of the local
   seed, and the 5xg pre-pass demotes again).

5. **DemoteNullability — ay9 cross-Part group closure.** Two-Part
   chain in one test: Part 0 introduces two OPTIONAL-group siblings
   `a, b` at group `g > 0` and both are nullable; `s0.Export()`
   produces a carry with `exportedOptionalGroup{"a": g, "b": g}`.
   Part 1 re-MATCHes `a` in a required clause (`a`'s local
   Nullable() = false). `s1 := newScope(s0.Export()); s1.Ingest(part1);
   s1.SeedLocalNullability(); s1.DemoteNullability()` — `b`'s entry
   is demoted to false via the group closure on carriedGroups. Pins
   D1: DemoteNullability reads carriedGroups off the receiver, not a
   parameter.

6. **DemoteNullability — edge fixed point, two rounds.** A witness
   that requires two iterations of the edge fixed-point loop
   (proving `a` via edge `e1`, which demotes group `G`, which admits
   edge `e2` to prove `c`). Assert convergence to the correct
   nullableBinding table.

7. **Snapshot / partScope narrowing.** After a full Part cycle
   (BindNode, BindEdge, BindCall, CloseEdges, SeedLocalNullability,
   DemoteNullability), `Snapshot()` contains exactly the five
   witness lanes (nodeTypes, edgeTypes, edgeCands, edgeBindings,
   nullableBinding). No callTypes lane leaks. The `bindings`,
   `effects`, and carry lanes are unobservable through `partScope`.

8. **Export wildcard vs. explicit.** Given a Part with
   `ReturnsAll=true`, `s.ResolveProjections(sch); s.Export()`
   populates `exportedResolvedTypes` with every scopeOrder entry;
   given an explicit `WITH v, e.p AS x`, Export populates
   `exportedResolvedTypes` with `v` and `x`, and populates the
   binding lanes only for `v` (not `x`).

9. **CloseEdges writes only edge lanes.** After
   `s.BindNode(...); s.BindNode(...); s.BindEdge(...); s.CloseEdges(sch)`,
   `nodeTypes` / `nullableBinding` / `callTypes` are unchanged from
   their post-Bind* state; `edgeTypes` / `edgeKeys` / `edgeCands`
   are populated at the edge's variable.

10. **Ingest is single-shot.** Calling `s.Ingest(part)` a second time
    is a resolver-side bug (Part cycle is one-shot). The method
    panics on second call, and a targeted Go test asserts the panic
    via `require.Panics`. Cheap tripwire that keeps the phase
    orchestration honest.

### 4.2 Regression fence

- All 136 resolver golden pairs in `test/data/resolver/{valid,invalid}`
  are byte-identical without `-update`. Command: `just test`. Any diff
  is a defect.
- `TestSentinelReachability` sweep is green. No new sentinel means the
  reachability map is unchanged.
- `just fmt-check` and `just lint` green. Re-run both after every edit,
  including one-character touches.

### 4.3 Anti-tests (what would falsify the refactor)

- Any golden that flips is a failure to preserve behaviour. The
  golden set is the ground truth; the code must match, not the
  other way around.
- Any test in `resolver_test.go` that fails is a failure to preserve
  behaviour. R0–R7 stage tests are untouched.
- Any new resolver-package public identifier is a failure to keep the
  refactor internal.

## 5. Migration plan

The refactor lands in one branch, `resolver-branch-scope`, one PR (this
cycle owns the whole diff). Commit order inside the PR — each commit
compiles and passes `just test`, so `git bisect` on a fixture flip
lands on the exact offending commit:

1. **Skeleton commit.** Add `internal/resolver/scope.go` with the
   `scope` struct, `newScope` constructor, `Ingest`, and empty
   method stubs (each returns `nil` / a zero value or `panic("TODO")`
   as appropriate). Add `scope_test.go` with the unit tests marked
   `t.Skip("not implemented")`. `resolve.go` unchanged; goldens green.

2. **Move Bind / CloseEdges / InferUnlabelled + carry seed +
   Snapshot in one commit.** `newScope` implements the `:174-193`
   seed loop; `Ingest` binds the Part; `Snapshot` implements
   `snapshotScope`; the Bind/CloseEdges/InferUnlabelled methods land
   with real bodies. `resolvePart` is rewritten so it never touches
   `nodeTypes` / `edgeTypes` / `edgeKeys` / `edgeCands` /
   `edgeBindings` / `callTypes` as local variables — its Phase A1
   type-switch calls `s.BindNode` / `s.BindEdge` / `s.BindCall`
   directly, and Phases A2/B/C become `s.CloseEdges(sch)` +
   `s.InferUnlabelled(sch)`. Goldens green. **Closes D4 from
   Linus's grill**: no transitional bridge accessor is introduced
   only to be deleted; Phases A1/A2/B/C move together in one atomic
   commit. The cost of the larger commit is bought back by removing
   the churn Linus flagged.

3. **Move Demote and Effect validation.** Phase D is `s.SeedLocalNullability()` +
   `s.DemoteNullability()` (parameter-free — carriedGroups is on the
   receiver per §2.1/§2.2 and D1). Phase E is `s.ValidateEffects(sch)`.
   The free functions `seedLocalNullability`, `demoteNullableInPlace`,
   `validateEffects` and the seven per-variant validators either move
   onto `*scope` as methods or become unexported package-level helpers
   taking `*scope` — the Phase-3 code-review call. Goldens green.

4. **Move the projection walk into `ResolveProjections`.** The walk
   at `:384-404` — `buildScopeOrder`, `materialiseReturns`,
   `virtualProjection`, per-item `projectionType` loop — becomes one
   `s.ResolveProjections(sch)` call. `s.columns`, `s.items`,
   `s.scopeOrder` populate on the receiver. Goldens green.

5. **Replace `exportScope` with parameter-free `s.Export()`.** The
   12-parameter free function is deleted; `s.Export()` reads
   columns / items / scopeOrder / returns / returnsAll off the
   receiver. `resolvePart` is now ~30 lines of phase-orchestration
   calling scope methods with no local map or slice variables.
   Goldens green.

6. **Move `propertyUseWitness` / `scopeContains` /
   `witnessAcrossScopes` onto `partScope`.** The parameter walker
   uses `sc.PropertyUseWitness(ref, sch)` in place of
   `propertyUseWitness(ref, sc.nodeTypes, ...)`. The map parameters
   the current five-argument helpers take disappear. Goldens green.

7. **Enable the unit tests.** Remove the `t.Skip` calls. `just test`
   green.

At each commit, `just fmt-check && just lint` are green. Any commit
that flips a golden fails the refactor's central acceptance fence and
must be reworked, not merged forward.

**Six-commit total** (not seven — step 2's collapse absorbs the old
step 3).

## 6. Grill resolutions (Phase 1, round 1)

Round-1 grill by linus-1 closed four defects (D1-D4, absorbed into
§1, §2.1, §2.2, §5 above) and four open questions:

- **Name.** `scope`. Matches CONTEXT.md; owns both live + carry.
  Linus: confirmed.
- **Fold `partScope` into `scope`.** Do not fold. The parameter
  walker's inability to see live Part-internal state IS the
  invariant §3.2 pins. Two shapes, two invariants. Linus: confirmed.
- **File split.** One file (`scope.go`). ~600 lines. Splitting hides
  the invariants the interface exists to pin. Linus: confirmed.
- **Preserve `branchState` as the carry envelope.** Preserved; not
  renamed. Linus: confirmed.

### 6.1 Interface recount (Linus's follow-up)

Linus asked me to recount methods after D2 + D3 collapse. Below is
the exhaustive list on `*scope` and `partScope`, matching §2.2:

**On `*scope`** (11 methods, plus constructor and Ingest):

| # | Method | Phase / role |
|---|--------|--------------|
| — | `newScope(carry branchState) *scope` | Construction |
| — | `Ingest(part query.Part)` | Part binding, single-shot |
| 1 | `BindNode(nb, nt) error` | Phase A1 labelled node |
| 2 | `BindEdge(eb) error` | Phase A1 edge admission |
| 3 | `BindCall(cb, r) error` | Phase A1 CALL YIELD |
| 4 | `CloseEdges(sch) error` | Phase A2 + Phase C |
| 5 | `InferUnlabelled(sch) error` | Phase B |
| 6 | `SeedLocalNullability()` | Phase D seed |
| 7 | `DemoteNullability()` | Phase D fixed point |
| 8 | `ValidateEffects(sch) error` | Phase E |
| 9 | `ResolveProjections(sch) error` | Projection walk end-to-end |
| 10 | `Snapshot() partScope` | Parameter-witness narrowing |
| 11 | `Export() branchState` | Part→Part carry-out |

Down from 14 in the initial draft. The collapse:

- Old `Order` + `VirtualProjection` + `ProjectionType` + a
  materialiseReturns-shaped method → one `ResolveProjections`.
- Old `EndpointLabels` + a separate A2/C pair → one `CloseEdges`
  (the two-pass logic is scope-internal).
- Old `SeedLocalNullability(bindings)` +
  `DemoteNullability(bindings, carriedGroups)` → parameter-free
  versions reading receiver state (D1 + D2).

Read-only predicates like `HasNode(v)`, `HasEdge(v)`,
`HasCall(v)` — used inside `resolvePart`'s conflict-detection
arms today — are unexported helper methods, not counted as
public interface; they exist because §2.4 rejects handing out
the raw maps.

**On `partScope`** (3 methods, moved in step 6):

- `PropertyUseWitness(ref, sch) (ResolvedType, error)`
- `Contains(v string) bool`
- `WitnessUse(u query.Use, sch) ([]ResolvedType, error)`

`witnessAcrossScopes` collapses onto `partScope.WitnessUse` (its
`branchScopes []partScope` argument stays at the caller — the
unifier iterates and dispatches to each scope's method).

### 6.2 Residuals for Phase 3 code review

Not spec commitments — flagged so the code-review round knows
where I made judgement calls:

- Whether the seven Phase-E per-variant validators
  (`validateCreateEffect`, `validateMergeEffect`, …) move onto
  `*scope` as methods or become unexported package-level
  helpers taking `*scope`. Preferred: package-level helpers —
  putting seven schema-driven variant switches on the method
  surface would drown the eleven core methods above without
  changing reach.
- Whether `refProjectionType` / `callProjectionType` /
  `unionProperty` similarly stay as free functions
  (schema-driven, scope-agnostic) or move to methods.
  Preferred: free functions, same reasoning.
- Whether `HasNode` / `HasEdge` / `HasCall` are the only
  read-only predicates that need to exist, or whether
  `resolvePart` still has a case that reaches for a map value
  (not just presence). Answered by inspection during
  implementation of step 2.
