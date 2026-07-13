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
  module's outgoing state, returned by the carry-forward operation.
- The Part-local maps `nodeTypes`, `edgeTypes`, `edgeKeys`, `edgeCands`,
  `edgeBindings`, `nullableBinding`, `callTypes` (`resolve.go:168-193` seed
  loop, then written to by Phases A/B/C/D) — become the module's *live*
  state during a Part.
- The seed loop from carry → local (`:174-193`) — becomes the scope
  module's constructor from a prior carry.
- The shadow / delete cascades wired into resolvePart's A1
  (`:229-234, :256-263, :272-276`) — become methods
  (`scope.BindNode`, `scope.BindEdge`, `scope.BindCall` — each with the
  cross-lane shadow semantics baked in).
- `snapshotScope`'s five-map deep-copy (`:903-927`) — becomes a
  `scope.Snapshot() partScope` method.
- `exportScope`'s wildcard-vs-explicit-Returns branch and its per-item
  binding-map population (`:524-638`) — becomes a
  `scope.Export(part query.Part, columns []Column, items []query.ReturnItem, scopeOrder []string) branchState`
  method, moving all twelve parameters behind the receiver.
- `demoteNullableInPlace` (`:1427-1548`) — becomes a
  `scope.DemoteNullability(bindings []query.Binding, carriedGroups map[string]int)`
  method that mutates the receiver's `nullableBinding` in place. The
  `carriedGroups` argument stays a distinct input because it comes from
  the *incoming* carry, not the current scope — see §5.
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
// scope is the resolver-typed carry evolving through one Part's phases.
// Fields are private; every mutation runs through a method so the ten
// lanes stay consistent (a delete on one lane cascades to the others
// that mirror it).
type scope struct {
    nodeTypes       map[string]schema.NodeType
    edgeTypes       map[string]schema.EdgeType
    edgeKeys        map[string]schema.EdgeKey
    edgeCands       map[string][]schema.EdgeKey
    edgeBindings    map[string]query.EdgeBinding
    nullableBinding map[string]bool
    callTypes       map[string]callBindingSlot
    // Carry-only lanes: seeded from the incoming carry, read by
    // downstream phases, never written by A1/A2/B/C/D within this Part.
    carriedResolvedTypes map[string]ResolvedType
    carriedOrder         []string
    carriedGroups        map[string]int
}

// newScope seeds a scope from Part K's exported carry — the ten fields
// of branchState. Part 0's carry is the zero-value branchState (nil
// maps everywhere) and the constructor treats it as empty without
// nil-guards at every read.
func newScope(carry branchState) *scope
```

The seed loop currently at `resolve.go:174-193` moves into `newScope`. The
zero-carry case (Part 0, `var carry branchState`) is handled once here;
nowhere else needs `if carry.exportedX != nil`.

### 2.2 The interface, exhaustively

Every operation `resolvePart` performs on the ten maps becomes exactly one
method. This is the deletion test the current shape fails and the new
shape must pass: removing a lane means editing this list plus its
implementation, nowhere else.

**Local binding admission** (used by Phase A1, resolve.go:200-323):

```go
// BindNode admits a labelled NodeBinding into scope. Cascades shadow /
// delete on the edge, call, and nullable lanes for the same variable
// per R5 §4.2.3 (the current :229-234 delete block). Returns
// ErrPartBindingTypeConflict if a carried entry at the same name has
// a distinct LabelSetKey. The R7 §4.1.2.1 call-vs-node shape check
// runs first.
func (s *scope) BindNode(nb query.NodeBinding, nt schema.NodeType) error

// BindEdge admits a labelled EdgeBinding into scope's edgeBindings
// lane (Phase A1's supportedEdges arm). Cascades node / call / nullable
// shadow. Registers the binding for Phase A2/C to close endpoints
// against. Returns ErrPartBindingTypeConflict per R5 §6.4 edge parity
// and R7 §4.1.2.2.
func (s *scope) BindEdge(eb query.EdgeBinding) error

// BindCall admits a CallBinding — the R7 §4.1 arm. Cascades node /
// edge shadow, registers the slot for the projection walk and carry.
// Returns ErrPartBindingTypeConflict on a same-Part duplicate.
func (s *scope) BindCall(cb query.CallBinding) error
```

**Edge closure** (Phase A2 + C, `resolve.go:326-361`):

```go
// CloseEdge writes an edge's schema-committed endpoint pair into
// edgeTypes / edgeKeys / edgeCands. Thin wrapper over the existing
// closeEdge helper — the free function stays; the method routes it
// through the scope's own maps.
func (s *scope) CloseEdge(e query.EdgeBinding, src, tgt graph.LabelSetKey, sch schema.Schema) error

// EndpointLabels resolves a query.Endpoint against the scope's
// nodeTypes lane (the endpointLabels free function moves through the
// receiver so pending-node inference does not touch the raw map).
func (s *scope) EndpointLabels(e query.Endpoint) (graph.LabelSetKey, bool)
```

**Unlabelled inference** (Phase B, `resolve.go:344-346`):

The existing `inferUnlabelled` free function takes maps by pointer today
because it writes to nodeTypes. It becomes:

```go
// InferUnlabelled runs Phase B against the scope's nodeTypes / callTypes.
// Writes inferred entries via scope's own BindNode-style cascade so the
// R7 §4.1.2.1 call-collision guard remains active at commit.
func (s *scope) InferUnlabelled(pending []query.NodeBinding, edges []query.EdgeBinding, sch schema.Schema) error
```

**Nullability** (Phase D, `resolve.go:363-372`):

```go
// SeedLocalNullability writes each binding's own Nullable() bit into
// the nullable lane, overwriting the carry per §4.6.
func (s *scope) SeedLocalNullability(bindings []query.Binding)

// DemoteNullability runs the ay9+5xg-widened demotion in place: the 5xg
// pre-pass, the ay9 pre-pass, then the edge-driven fixed point. Reads
// carriedGroups from the scope's own carry lane (seeded at newScope).
// The current free function's carriedGroups parameter becomes implicit
// on the receiver.
func (s *scope) DemoteNullability(bindings []query.Binding)
```

**Effect validation** (Phase E, `resolve.go:378-380`):

```go
// ValidateEffects runs R6 Phase E: dispatch each effect through its
// per-variant validator against the scope's committed binding tables
// and the carried resolved types. The 7-param free functions
// (validateEffect and the seven per-variant validators) become methods
// or private helpers on scope; the public entry is one method.
func (s *scope) ValidateEffects(effects []query.Effect, sch schema.Schema) error
```

**Wildcard-expansion / scope-order** (materialiseReturns setup,
`resolve.go:384`):

```go
// Order returns the deterministic name list for RETURN * / WITH * and
// export order (§4.4.1). Consults local bindings first, then the
// carry's exportedOrder.
func (s *scope) Order(bindings []query.Binding) []string

// VirtualProjection resolves one wildcard-expanded name to its
// Projection value (the free function at resolve.go:449 becomes a
// method, dropping the four map parameters).
func (s *scope) VirtualProjection(name string) (query.Projection, error)
```

**Projection walk** (the type walker itself, `resolve.go:399`):

```go
// ProjectionType resolves one Projection to its ResolvedType. The
// current 10-param free function becomes a 2-param method (projection +
// schema).
func (s *scope) ProjectionType(p query.Projection, sch schema.Schema) (ResolvedType, error)
```

`refProjectionType`, `propertyUseWitness`, `callProjectionType`,
`unionProperty` all lose their map parameters and become internal
callees. `unionProperty` stays a free function because its behaviour is
purely schema-driven, not scope-driven.

**Export / snapshot** (end of resolvePart, `resolve.go:409-416`):

```go
// Snapshot returns the parameter-witness partScope this Part
// contributes: a deep copy of the five fields the top-level walker
// consults. Called once per Part after Phase D.
func (s *scope) Snapshot() partScope

// Export builds the branchState Part K passes to Part K+1 (§4.2.2).
// The current 12-param exportScope becomes a 4-param method: part,
// columns, items, scopeOrder. All ten map lanes come from the receiver.
func (s *scope) Export(part query.Part, columns []Column, items []query.ReturnItem, scopeOrder []string) branchState
```

Total: 14 methods on `*scope`, plus the `newScope` constructor. Every
lane read or written in `resolvePart` today runs through one of these
in the refactored file.

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

3. **`snapshotScope`'s five fields are a strict subset of the scope's
   seven live-lane fields.** Adding a new lane to `scope` does not
   silently widen `partScope`; the Snapshot method is where the two
   shapes are pinned.

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

Each test constructs a `scope` directly (via `newScope` with an
explicit carry) and asserts one invariant. No fixtures needed — the
tests exist to pin the scope-module's contract, so they are all in-code
table-driven Go tests.

1. **Empty carry → empty scope.** `newScope(branchState{})` returns
   a scope where every `HasX` predicate returns false; `Snapshot()`
   returns a `partScope` with five empty maps; `Export(...)`  on a
   `part` with no Returns and no bindings returns a zero-value
   `branchState`.

2. **Carry-forward round-trip.** A branchState `c1` seeded with a
   node, an edge, and one nullable entry round-trips through
   `newScope(c1)` followed by `Export(...)` on a Part that
   wildcard-exports its scope — the returned `branchState` equals `c1`
   modulo the exportedResolvedTypes lane (which the wildcard-expand
   populates). This is the deletion test: dropping any of the ten lanes
   from scope + Export makes exactly this test fail.

3. **BindNode shadow cascade.** Seed a scope with `carry.exportedEdgeBindings["r"]`
   populated; call `BindNode(NodeBinding{Variable: "r", ...})` with a
   labelled node. Post-condition: `HasNode("r")` true;
   `HasEdgeBinding("r")` false. Every one of the five shadow lanes
   the current `:229-234` block deletes is checked. Symmetric tests for
   `BindEdge` (shadow node/edge-closed state) and `BindCall` (shadow
   node+edge).

4. **DemoteNullability — 5xg pre-pass.** A binding with
   `ReferencedInRequiredBarePattern() == true` and a table entry set
   to true has its entry flipped to false. Ensure the entry stays
   false if seedLocalNullability re-runs (idempotence).

5. **DemoteNullability — ay9 group closure.** Two carried bindings
   `a, b` with the same carried group id `g > 0` and a proven local
   witness for `a` demotes `b` too. Cross-Part carry: verify
   `carriedGroups` propagation via a two-Part chain.

6. **DemoteNullability — edge fixed point.** A required non-nullable
   edge whose OPTIONAL endpoint is proven demotes the endpoint. One
   round of iteration is enough because the loop's `changed` bit is
   correct — verify with a witness that requires two rounds
   (proving `a` via edge `e1`, which demotes group `G`, which admits
   edge `e2` to prove `c`).

7. **Snapshot / partScope narrowing.** After a full Part cycle
   (BindNode, BindEdge, CloseEdge, SeedLocalNullability,
   DemoteNullability), `Snapshot()` contains exactly the five witness
   lanes, with no carry-only lane leaking (e.g. `callTypes` is not
   observable through `partScope`).

8. **Export wildcard vs. explicit branch.** Two golden-shaped
   assertions in code: given a Part with `ReturnsAll=true`, Export
   populates exportedResolvedTypes with every scopeOrder entry;
   given an explicit `WITH v, e.p AS x`, Export populates
   exportedResolvedTypes with `v` and `x`, and populates the binding
   lanes only for `v` (not `x`).

9. **CloseEdge writes only edge lanes.** After CloseEdge, nodeTypes /
   nullableBinding / callTypes are unchanged; edgeTypes / edgeKeys /
   edgeCands are populated at the edge's variable.

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
   `scope` type, `newScope` constructor, and empty method stubs. Add
   `scope_test.go` with the unit tests marked `t.Skip("not
   implemented")`. `resolve.go` unchanged; goldens green.

2. **Move the carry seed and Snapshot.** `newScope` implements the
   `:174-193` seed loop; `Snapshot` implements `snapshotScope`.
   `resolvePart` calls `newScope(carry)` and reads its live lanes
   directly for one commit — the raw maps still exist as local
   variables assigned from the scope's private fields (a one-line
   accessor is the transitional bridge). Goldens green.

3. **Move Bind / CloseEdge / InferUnlabelled.** Phase A1/A2/B/C
   routes through scope methods; the local `nodeTypes` / `edgeTypes`
   / etc. variables in `resolvePart` are eliminated. Cross-lane
   shadow blocks (`:229-234`, `:256-263`, `:272-276`) live inside
   the Bind methods. Goldens green.

4. **Move Demote and Effect validation.** Phase D and Phase E become
   method calls; the free functions `demoteNullableInPlace`,
   `validateEffects`, and the per-variant validators are moved onto
   `scope` (or become unexported package-level helpers reading
   scope's private state — the choice is a Phase-3 code-review
   detail, not a spec commitment). Goldens green.

5. **Move ProjectionType / Order / Export.** The projection walk,
   `buildScopeOrder`, `virtualProjection`, and `exportScope` become
   methods. `resolvePart` is now ~40 lines of phase-orchestration
   calling scope methods. Goldens green.

6. **Move propertyUseWitness onto partScope.** The parameter walker
   uses `sc.PropertyUseWitness(ref, sch)` in place of
   `propertyUseWitness(ref, sc.nodeTypes, ...)`. `witnessAcrossScopes`
   and `scopeContains` become methods on `partScope` too, tightening
   the parameter-walker's surface. Goldens green.

7. **Enable the unit tests.** Remove the `t.Skip` calls. `just test`
   green.

At each commit, `just fmt-check && just lint` are green. Any commit
that flips a golden fails the refactor's central acceptance fence and
must be reworked, not merged forward.

## 6. Open questions for the spec grill (Phase 1)

Explicitly leaving these unresolved for Linus to close:

- **Name.** `scope` (matches CONTEXT.md's "the part's scope"
  descriptive phrasing) vs. `partCarry` vs. leaving `branchState` and
  giving it methods. The name is downstream churn if wrong. Preferred:
  `scope`, both because it matches the domain glossary and because
  the module owns *both* live-Part state and carry state, whereas
  "branchState" and "carry" each read as only half the shape.
- **Whether to fold `partScope` into `scope`.** Rejected in §3.2 for
  the reasons listed; if Linus disagrees, the alternative is one
  `scope` type with a `witnessView()` method returning a narrowing
  wrapper. Cost: the top-level unifier's tests change signature and
  the two shapes' invariants live in one file.
- **File split.** All of scope in one `scope.go` (~600 lines after
  the move) vs. splitting into `scope.go` (type + Bind + Snapshot),
  `scope_demote.go` (Phase D), `scope_export.go` (Export). Preferred:
  one file — 600 lines is not a locality problem, and split files
  hide the invariants the interface pins.
- **Whether to preserve `branchState` as the export/import envelope
  type (this spec's choice) or rename to `scope.Carry`.** Preferred:
  preserve, because renaming touches no logic and existing golden /
  test names read cleanly against the current spelling.
