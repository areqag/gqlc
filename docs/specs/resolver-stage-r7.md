# Stage R7 spec — resolver: CALL (YIELD column typing via procsig.Registry)

The implementation brief for Stage R7 of `internal/resolver`, extending the
merged R0/R1/R2/R3/R4/R5/R6 kernel (`docs/specs/resolver-stage-r0.md`
through `docs/specs/resolver-stage-r6.md`) with the single capability arm
ADR 0009 assigns to R7: **CALL — YIELD column typing from the
`procsig.Registry`; argument assignability including `NUMBER`
assignable-from `INTEGER`-or-`FLOAT` (ADR 0007's Stage-14 note); unknown
procedure is a generation-time error by design**.

Verbatim ADR 0009 R7 line (`docs/adr/0009-resolver-test-first-staged-build.md`
lines 136-139):

> **R7 — CALL** (`gqlc-0mx.9`): `YIELD` column typing from the
> `procsig.Registry`; argument assignability including `NUMBER`
> assignable-from `INTEGER`-or-`FLOAT` (ADR 0007's Stage-14 note); unknown
> procedure is a generation-time error by design.

Build this **test-first**. Scope, sequencing, error posture,
`ValidatedQuery`'s top-level shape, purity, and the golden-pair harness
inherit from ADR 0009 and R0–R6 unchanged; this document revises only the
rows, kernel arms, `ValidatedQuery` axes, sentinel set, and out-of-scope
table entries that R7 changes.

R7 admits every query shape R6 admits, extended to:

- one or more `CallBinding` entries per `Part` (any Part, any branch)
  contributing to the Part's binding tables at Phase A1 alongside
  `NodeBinding` / `EdgeBinding` — the R6 default-reject arm at
  `resolve.go:234-236` (`return … ErrOutOfR0Scope: "call" binding`; verify
  verbatim) lifts;
- Refs whose `Variable` names a `CallBinding` — the R6/R5
  `refProjectionType` path at `resolve.go:1042-1052` widens to route a
  `CallBinding` Ref through the new call-binding lane before falling
  through to `carriedResolvedTypes`;
- `Part.ReturnsAll` expansion (`buildScopeOrder`, `resolve.go:370-404`)
  widens to include call-yielded variable names, so `CALL … YIELD *` /
  standalone-CALL synthetic `RETURN *` populate their columns from the
  CallBindings.

**Honest scope-vs-ADR delta**: the ADR 0009 R7 line names three axes —
YIELD column typing, argument assignability (incl. NUMBER assignable-from),
and unknown-procedure = generation-time error. R7 as this spec scopes it
delivers **YIELD column typing** fully. ~~**Argument assignability**
(including NUMBER assignable-from INTEGER-or-FLOAT) has **no
resolver-side application site** at R7 because the parser discards
CALL-arg attribution on the wire (spec §7.2 and §7.3).~~ [closed
2026-07-07 by gqlc-0ig (PRs #122/#123/#124,
`docs/specs/unfreeze-0ig-call-args.md`): the wire now carries per-
position `CallBinding.Args()` records; the resolver's Phase A1
CallBinding arm walks `Args()` against the matched
`procsig.Registry.Lookup(procedure).Params[i].Token` and fails with
`ErrCallArgAssignability` on mismatch under the ADR 0007 Stage-14
lattice. §7.1.1 banner.] **Unknown procedure** at generation time is
delivered by the parser (`cypher.ErrUnknownProcedure`;
`internal/query/cypher/call.go:41-44`); the resolver **does not re-check**
— R7 documents this as a trust posture mirroring R6's Refs referential-
integrity trust (spec §4.4). ~~This is not a scope trim: it is a frozen-
model deficiency in the same family as `gqlc-fvo` (cross-Part parameter
Use attribution, unchanged from R5/R6). §7.1 records the deficiency with
the exact widening required.~~ [closed 2026-07-07: the argument-
assignability deficiency this paragraph declared is closed by gqlc-0ig;
§7.1.1 carries the closure banner.]

~~The R4 Class A and Class B same-Part regime (b) nullability
under-approximations (`gqlc-ay9`, `gqlc-5xg`),~~ [closed 2026-07-10:
Class A landed via gqlc-ay9 (PRs #127/#128/#129,
`docs/specs/unfreeze-ay9-optional-group.md`; residual gqlc-984) and
Class B / same-Part regime (b) landed via gqlc-5xg (PRs
#132/#133/#134, `docs/specs/unfreeze-5xg-required-bare-ref.md`;
residual gqlc-0kq); R7 inherits both closures unchanged.] the R5
`ExprProjection`-residual grouping-key gap (`gqlc-gyw` Shape B /
`gqlc-hk0`), and the R5 cross-Part parameter-Use attribution gap
(`gqlc-fvo`) are unchanged at R7. R6's two design deferrals
(value-target assignability, Effects-on-wire) are unchanged at R7. ~~None
of these gaps is closable without a model unfreeze (owner decision
pending); R7 does not contort the resolver around any of them.~~
[closed 2026-07-10 for the two R4 nullability gaps (ay9, 5xg); the
R5 gaps (`gqlc-hk0`, `gqlc-fvo`) persist at R7; R7 does not contort
the resolver around them.] `PathBinding`
and `UnwindBinding` remain out of scope and continue to route to
`ErrOutOfR0Scope` (R-later).

R7 introduces **zero new sentinels**. Every R7 fail-site is covered by
an R0–R6 sentinel:

- Unknown procedure fires at parse time (`cypher.ErrUnknownProcedure`,
  parser-side, not on the resolver's `allSentinels`).
- Unknown YIELD result field fires at parse time (same
  `cypher.ErrUnknownProcedure`, per `internal/query/cypher/call.go:
  135-137`).
- Intra-YIELD name collision, imported-scope collision, and other
  parser-time scope faults fire as `cypher.ErrVariableKindConflict`.
- Arity mismatch on explicit invocation fires as
  `cypher.ErrProcedureArity`.

The **resolver's job at R7 is exclusively YIELD column typing** — turning
a `CallBinding` into a `Column{Name, Type, Nullable, GroupingKey}` on the
wire. Every failure mode above happens at parse and never reaches the
resolver.

R7's wire delta on `ValidatedQuery` is **zero**: `Columns`, `Parameters`,
`Statement`, `Distinct`, and per-`Column` `GroupingKey` are unchanged in
shape and in field type. The `Statement` kind for a query containing
CALL bindings is `StatementRead` — CALL is a Read clause per
`internal/query/query.go:838-842` (CallBinding does not flip
`Query.StatementKind`), verified against the parser's Stage 14 posture
(spec §3.2).

---

## 1. Deliverables

- `internal/resolver/validated.go` — **no additions**. `ValidatedQuery`,
  `Column`, `ResolvedParameter`, `StatementKind`, and the eight
  `ResolvedType` variants are unchanged in shape. R7's YIELD column
  typing populates `Column.Type` from the R2 lattice's existing
  `ResolvedProperty{Type, Nullable}` variant (INTEGER → `ResolvedProperty{
  Type: graph.TypeInt, Nullable: bool}`; FLOAT → `ResolvedProperty{Type:
  graph.TypeFloat, Nullable}`; STRING → `ResolvedProperty{Type: graph.TypeString,
  Nullable}`; NUMBER → `ResolvedUnknown{}`; §4.2 pins the mapping table).
- `internal/resolver/errors.go` — **no sentinel additions**. The
  `ErrOutOfR0Scope` message-set list revises to reflect one retirement
  (`call binding` — §5.2). R6 sentinel identities are preserved.
  **Two wrapped-message sets widen** on existing sentinels: one on
  `ErrUnknownProperty` for property-on-CALL-YIELD-scalar (§4.2.2),
  one on `ErrPartBindingTypeConflict` for the scalar-vs-entity
  shape mismatch at cross-Part re-bind (§4.1.2). Both are the
  "same sentinel, more messages" widening pattern from ADR 0009
  §Test strategy; neither creates a new sentinel. §5.3 pins both.
- `internal/resolver/resolve.go` — extended with:
  - A **`CallBinding` admission arm in Phase A1** (§4.1) that replaces
    the R6 default-reject arm at `resolve.go:234-236` for `BindingCall`.
    The arm seeds a new local `callTypes map[string]callBindingSlot`
    keyed on the CallBinding's Variable, carrying the (bridged
    `query.Type`, `nullable`, `procedure`, `sourceField`) tuple for
    downstream lookup. Shadowing rules: a local CallBinding shadows any
    carried node/edge state at the same name (mirroring R5 §4.2.3);
    the local CallBinding is a scalar-shaped scope entry so a subsequent
    labelled node re-binding at the same name is a
    `ErrPartBindingTypeConflict` — see §4.1.2.
  - A **`refProjectionType` widening** (§4.2) that dispatches a Ref
    against the new `callTypes` lane before the carried-alias bypass.
    Bare Ref (`Property == ""`) — witnessed to `ResolvedProperty{Type,
    Nullable}` or `ResolvedUnknown{}` per §4.2.1. Property lookup on a
    CallBinding (Ref with `Property != ""`) — routed to
    `ErrUnknownProperty` per §4.2.2 (a CallBinding's YIELD variable
    exposes a scalar/opaque value, not an entity with properties).
  - A **`bindingVariable` widening** (§4.3) that adds a `CallBinding`
    arm, so `buildScopeOrder`'s Part.Bindings walk (`resolve.go:370-404`)
    includes call-yielded variables in first-appearance order. This is
    what makes `RETURN *` — including the parser-synthesised
    standalone-CALL `Part.ReturnsAll = true` — emit the call-yielded
    columns.
  - A **`callTypes` carry-forward** entry on `branchState` (§4.6),
    exported by Phase-D-time `exportScope` (`resolve.go:406-484`) and
    consumed by Part K+1's Phase A1 seed step. This is what makes a WITH
    that projects `CALL … YIELD y` onward carry `y` as a typed scalar
    through the rest of the branch.
  - **Phase E is unchanged**: `validateEffects` never sees a
    `CallBinding` (CALL is Read; a projection-less Part with a
    CallBinding but no Returns fires the parser's Stage 14 synthesis
    into `Part.Returns` — see §4.5).
- `internal/resolver/resolver_test.go` — the harness gains **registry
  threading**. The R6 `loadQuery` at line 106-112 calls
  `cypher.New().Parse(...)` — no registry. R7 revises to
  `cypher.New(cypher.WithRegistry(reg)).Parse(...)` with a per-directory
  `registry.fixtures.go` loaded on suite setup (§6.2). Every R0–R6
  fixture parses identically because none contains a `CALL` (an
  empty-registry parse still succeeds on non-CALL queries — verified
  first-party against `internal/query/cypher/parser.go:47-50`).
- `internal/resolver/resolver_test.go` — a new package-private
  `signaturesR7` slice + `regR7` variable declaring the R7
  procedure signatures for the test harness. Uses the same
  `internal/procsig` package the parser tests wire (§6.2). One
  registry serves both `valid/` and `invalid/` — the CALL fail-sites
  in `invalid/` are the resolver's, not the parser's, so a fixture in
  `invalid/` that must reach the resolver first parses cleanly.
  **Rationale for placing this inside `resolver_test.go` rather than
  under `test/data/resolver/valid/`**: `test/data/resolver/` contains
  only `.cypher` and `.validated.golden.json` today (no `.go`), and
  the registry is test-harness wiring, not fixture data. §6.3 pins
  the placement decision.
- `internal/resolver/resolver.go` — the `WithRegistry` doc comment at
  `resolver.go:36-38` reads today "raises the R7 unknown-procedure
  sentinel" — an incorrect prediction. R7 **retires** that claim
  (§4.4 trust posture: the parser is authoritative; the resolver
  does not re-check). The code cycle rewrites the doc comment to
  state the registry is preserved for future stages but discarded
  at R7 (mirror the actual `_ = r` discard at `resolve.go:110`). No
  behavioural change; comment-only fix.
- `test/data/resolver/valid/schemas/social_r7.gql` — a new schema
  fixture. R7's YIELD columns are opaque scalar types (not schema
  entities), so schema shape barely matters; `social_r7.gql` is a
  minimal one-node one-edge extension of `social_r6.gql` (§6.1).
- `test/data/resolver/valid/*.cypher` and `.validated.golden.json` —
  new R7 valid fixtures (§6.3), each paired with its schema through
  the updated `schema.mapping.json`. **No axis on any existing R0–R6
  golden changes.** Every R0–R6 valid golden is byte-identical at
  branch tip (§3.3); the R7 code cycle asserts this by re-running
  `just test` without `-update` against the R0–R6 corpus.
- `test/data/resolver/invalid/*.cypher` — new R7 invalid fixtures
  (§6.4) exercising the widened `ErrUnknownProperty` fail-site on
  a CallBinding property lookup. The R6 invalid-fixture set is
  unchanged. No R7 fixture pins a parser-time sentinel — the
  parser-time faults (unknown procedure, arity, YIELD collision) fire
  before the resolver runs and are covered by the cypher parser's
  Stage-14 test suite, not the resolver's.
- `internal/resolver/resolver_test.go` — updated `invalidFixtures` map
  (§6.4). No structural changes to `TestValid` / `TestInvalid` /
  `TestSentinelReachability`; the R0–R6 harness scales as-is once
  `loadQuery` gains the registry option.

Nothing downstream of the resolver is built — `ValidatedQuery` remains
provisional through R7 close-out (ADR 0009 §Decision; codegen consumes
`ValidatedQuery` under a future ADR — the ADR 0008 analogue). R7 lands
zero wire-shape additions.

---

## 2. Architecture — deltas from R6

R0/R1/R2/R3/R4/R5/R6's architecture stands (the `Resolver` struct, its
compile-time inputs, the `QueryResolver` interface + compile-time
assertion, purity and short-circuit posture, `resolve.go`/`Resolve`
split, the branch-and-part driver, the R6 per-Part kernel body:
Phase A1, A2, B, C, D, E, projection walk, parameter walk). R7
preserves this shape verbatim and inserts one new binding lane —
`callTypes` — in Phase A1's kind-switch, along with three helper
widenings (`refProjectionType`, `bindingVariable`, `exportScope`).

### 2.1 The R7 kernel structure

The outer branch-and-part driver is unchanged from R6 §2.1. Inside
`resolvePart`, the kernel gains one new local map alongside
`nodeTypes` / `edgeTypes` / `edgeCands` / `edgeBindings`:

```
resolvePart(part, carry, s):
  Phase A1 — local labelled node bindings + edge admission screening
            + CALL binding admission (NEW at R7 — §4.1)
  Phase A2 — R3-admitted edges: candidate-set formation
  Phase B  — unlabelled-node inference over R3-admitted touching edges
  Phase C  — close deferred edges
  Phase D  — nullability seed + demotion (call-yielded nullability
            seeds in with the CallBinding's Nullable() bit — §4.1.4)
  Phase E  — effect validation (R6 — unchanged; validateEffects
            never sees a CallBinding)
  scopeOrder    = buildScopeOrder(...) — widened for CALL (§4.3)
  items         = materialiseReturns(...)
  columns       = projection walk over items (§4.2 widening for
                                              CallBinding Refs)
  site          = snapshotScope(...)
  exported      = exportScope(...) — widened for callTypes (§4.6)
  return columns, exported, [site]
```

Phase A1's CALL arm runs after node/edge arms so a CallBinding does
not participate in edge admission screening. This is not an ordering
invariant that failure depends on — CallBindings do not appear in
the same variable-name slot as an entity binding (parser Stage 14
`buildPart` enforces this via `ErrVariableKindConflict`
per `internal/query/cypher/build.go:127-146`) — but ordering keeps the
loop body readable.

**Phase E has no interaction with CallBindings.** The R6
`validateEffects` walks `part.Effects`, which is disjoint from
`part.Bindings`. A CallBinding lives on `Part.Bindings`; an Effect
lives on `Part.Effects`. They never co-occur in a single loop step,
and no R7 fixture creates a scenario where they interact.

**Nullability composition.** R4's Phase D processes local Bindings via
`seedLocalNullability(part.Bindings, nullableBinding)` and
`demoteNullableInPlace(part.Bindings, nullableBinding)`. Both helpers
kind-switch on Binding — R7 widens the switch to include CallBinding
so the CallBinding's `Nullable()` bit seeds `nullableBinding[v]`
correctly (see §4.1.4).

### 2.2 Kernel helpers — one widened; three revised; the projection walk unchanged

Kernel changes in `resolve.go`:

- **`refProjectionType`** (`resolve.go:1027-1084`, R6). R7 adds a
  `callTypes[ref.Variable]` lookup arm before the `carriedResolvedTypes`
  bypass. Bare Ref hits the new arm and returns the CallBinding's
  bridged `ResolvedType`; Ref with a property triggers
  `ErrUnknownProperty`. See §4.2.

- **`bindingVariable`** (`resolve.go:1231-1240`, R6). R7 adds a
  `CallBinding` case returning `(bb.Variable(), true)`. This is what
  makes `buildScopeOrder` include CALL-yielded variable names in the
  `RETURN *` / synthetic-standalone-CALL expansion set. See §4.3.

- **`seedLocalNullability`** and **`demoteNullableInPlace`**
  (`resolve.go` — R4 helpers). R7 adds `CallBinding` arms to both
  switches — the seed arm assigns `nullableBinding[cb.Variable()] =
  cb.Nullable()`; the demote arm is a no-op because CallBinding is
  never a demoter (it introduces its own scope; it does not participate
  in edge closure). See §4.1.4.

- **`exportScope`** (`resolve.go:406-484`, R6). R7 adds a `callTypes`
  export map lane on `branchState` so a CALL-yielded variable carried
  across a WITH remains typed at Part K+1. The R6 WITH-exports rule
  is unchanged; §4.6 pins how `callTypes` participates.

- **`resolvePart`**'s `switch bb := b.(type)` at `resolve.go:181-236`
  (R6). R7 adds a `case query.CallBinding` arm before the default and
  lifts the default reject for `BindingCall`. See §4.1.

Every other R6 helper is behaviour-unchanged. In particular:
`buildScopeOrder`, `materialiseReturns`, `virtualProjection`,
`choose`, `fillGroupingKeys`, `compareBranchColumns`,
`resolvedTypeEqual`, `computeDistinct`,
`unifyParameterUsesAcrossScopes`, `witnessAcrossScopes`,
`scopeContains`, `snapshotScope`, `r3EdgeAdmissible`, `edgeCandidates`,
`closeEdge`, `endpointLabels`, `formatEdgeKey`, `formatEdgeKeys`,
`describeTriedEdges`, `inferUnlabelled`, `candidateTypes`,
`touchingSide`, `intersect`, `joinCandidates`, `projectionType`
(dispatch shape unchanged — sub-arm updated), `unionProperty`,
`resolveType`, `propertyUseWitness`, `qualifiedDemoter`, `unify`,
`validateEffects` and all R6 per-variant validators — none of these
are touched by R7.

### 2.3 Purity, determinism, short-circuit — unchanged

R0 §2.3 and R5–R6 §2.3 stand. R7 introduces no goroutine, no time
source, no map iteration that escapes into output. CALL-binding walks
proceed in `Part.Bindings` slice order (parser Stage 14 walk order —
CALL bindings appear in the order the parser emitted them, which is
deterministic per Stage 14 `buildPart` §4.7's `callBindings` slice).
Short-circuit is preserved: the first Phase A1 CALL-binding failure
short-circuits the Part; the first Part failure short-circuits the
branch; the first branch failure short-circuits the query.

R7 does not add any new short-circuit sites. The Phase A1 CALL arm
never fails on any parser-emitted input: a CallBinding is
model-valid by construction (parser `query.NewCallBinding` enforces
non-empty variable / procedure / sourceField), and R7 does not
re-check parser invariants. The only failure the R7 arm can produce
is a defensive tripwire (a variable-name collision with a local node
or edge binding that Phase A1 has already committed) — the parser's
`ErrVariableKindConflict` catches this before the resolver runs.
§4.1.3 pins the defensive posture.

### 2.4 CallBindings × multi-part × multi-branch composition

CallBindings can appear in ANY `Part` of ANY `Branch`. Two composition
rules follow:

- **Cross-Part CallBinding carry.** A `WITH x` in Part K (where `x`
  was a CALL YIELD variable in Part K) carries `x` through to Part
  K+1 as a typed scalar. The mechanism: `exportScope`'s R7-widened
  `callTypes` lane exports `x → CallBinding{Type, Nullable}`
  metadata; Part K+1's Phase A1 seed step reads it into its
  local `callTypes` map, so a subsequent Ref to `x` in Part K+1's
  projection walk finds it in the seeded `callTypes` before falling
  through. This is the direct analog of R5's `exportedResolvedTypes`
  export path for projection aliases (`resolve.go:433-439`). Fixture:
  `call_yield_carry_forward.cypher` (§6.3).

- **Cross-branch CALL columns.** A UNION branch containing a
  standalone-CALL produces a synthetic-Returns Part (parser Stage 14
  §4.3 — `Part.ReturnsAll = true` and `Part.Returns` populated from
  the CallBindings). R5's `compareBranchColumns` runs on each branch's
  final Part; a mixed CALL-yielded/entity UNION branch pair either
  agrees on column names and types (admits) or fails
  `ErrUnionColumnMismatch`. Fixture: (out of scope for R7 — no
  representative TCK query pairs a CALL branch with a MATCH branch
  under UNION; the shape is admitted by the machinery, not by a
  fixture pin).

The parser attributes CallBindings directly to `Part.Bindings`
(`internal/query/cypher/build.go:191-198`). R7 has no Use→Part
attribution problem for CallBindings — every CallBinding lives on the
Part that authored it, and the Ref that names it lives on the same
Part (bare RETURN case) or a downstream Part (WITH-carried case)
where `callTypes` carry-forward covers it.

**CALL argument Uses — the wire attribution gap.** The parser emits
CALL argument parameter uses as `ExprUse{miningType,
ExprInProjection}` per `internal/query/cypher/call.go:54-66` — the
`ExprInProjection` position is a stand-in ("SILENT INFO DROP" per
the Stage-14 spec §1.5); there is no `ExprInCallArg` position on the
wire. §7.1 records this gap and its consequences.

### 2.5 What R7 does NOT admit

Two constructs remain rejected at R7 and route to `ErrOutOfR0Scope`:

- **`PathBinding`** — R-later (dialect posture, ADR 0008
  §"shortestPath is a dialect extension"). `Part.Bindings` containing
  a `PathBinding` continues to fire the R6 default arm at Phase A1.
- **`UnwindBinding`** — R-later. Same reject arm.
- **Untyped edge** (`len(Labels()) == 0`) — R-later. `r3EdgeAdmissible`
  reject arm is unchanged.
- **`ExprProjection` typed `list<node>` / `list<edge>`** — R-later.
  `resolveType` reject arm unchanged.
- **Property projection on a variable-length edge binding** — R-later.
  `refProjectionType` reject arm unchanged.
- **Property lookup on a CallBinding YIELD variable** — R-later /
  never. A CallBinding's YIELD variable is scalar (`Ref{v, ""}` typed
  as a bridged `ResolvedProperty` or `ResolvedUnknown`); a
  `Ref{v, prop}` on a CallBinding routes to `ErrUnknownProperty`.
  R7's `refProjectionType` widening at §4.2.2 pins this. Fixture:
  `call_yield_property_lookup.cypher` (§6.4).

The R6 rejection of `BindingCall` (`resolve.go:234-236`) RETIRES at R7.

---

## 3. `ValidatedQuery` — the R7 shape

`ValidatedQuery`'s top-level shape (R0 §3.1, R5 §3, R6 §3) is unchanged
at R7. `Columns`, `Parameters`, `Statement`, `Distinct`, and per-`Column`
`GroupingKey` retain their R6 field types and semantics. R7 changes only
what the resolver puts INTO `Columns` for queries containing CALL YIELD.

### 3.1 `ValidatedQuery.Columns` — CALL YIELD populates `Column.Type`

At R6, `ValidatedQuery.Columns` was populated from branch 0's final
Part, with the projection-less refinement for writes-only Parts. R7
does not change this outer rule. Instead, R7 changes the **per-item
type resolution** for `RefProjection` when the Ref names a CallBinding:

- `RefProjection{Ref{v, ""}}` where `v` is a CallBinding in scope →
  `Column.Type` is one of:
  - `ResolvedProperty{Type: graph.TypeInt, Nullable: cb.Nullable()}` if
    the CallBinding's `ResultType()` is `query.TypeInt{}`
    (signature `INTEGER`);
  - `ResolvedProperty{Type: graph.TypeFloat, Nullable: cb.Nullable()}` if
    `ResultType()` is `query.TypeFloat{}` (signature `FLOAT`);
  - `ResolvedProperty{Type: graph.TypeString, Nullable: cb.Nullable()}` if
    `ResultType()` is `query.TypeString{}` (signature `STRING`);
  - `ResolvedUnknown{}` if `ResultType()` is `query.TypeUnknown{}`
    (signature `NUMBER` — the assignable-from marker bridges to
    TypeUnknown on the wire, spec §4.2.1).
- `RefProjection{Ref{v, prop}}` where `v` is a CallBinding in scope →
  `ErrUnknownProperty` (§4.2.2). A CallBinding is a scalar; the
  concept of "prop of the scalar" is not modelled.

**`Column.Name`** for a CALL-yielded projection follows the R2/R5
rule unchanged: from the `ReturnItem.Name` (either the parser-supplied
name or, for a `ReturnsAll` synthesised item, the CallBinding's
Variable which the parser injects — see §4.5). No R7-specific naming
rule.

**`Column.Nullable`** is composed as R4 composes it: the base
nullability comes from the CallBinding's `Nullable()` bit (Phase D
seeds `nullableBinding[cb.Variable()]` at §4.1.4); R4's demotion
mechanism (`demoteNullableInPlace`) never fires on a CallBinding
because CallBindings are not edge endpoints and do not participate
in R4's OPTIONAL-scoped demoter set. Consequence: a Ref to a
CallBinding yields a Column whose `Nullable` is exactly the
signature's declared `?` bit.

**Judgment call — `ResolvedProperty{Type, Nullable}` over
`ResolvedScalar{Kind}` for CALL YIELD scalars.** The R2 lattice
carries two scalar-typed variants: `ResolvedProperty{Type,
Nullable}` (for property-projected node/edge scalars) and
`ResolvedScalar{Kind}` (for literal / func-projected scalars in
`ExprProjection`). R7 routes CALL YIELD scalar columns to
`ResolvedProperty`, not `ResolvedScalar`. Two reasons:

1. **Only `ResolvedProperty` carries `Nullable`.** A CALL YIELD
   column carries a signature-declared `?` bit
   (`CallBinding.Nullable()`); `ResolvedScalar{Kind}` has no
   nullability slot, so a signature `col STRING?` would lose the
   `?` under `ResolvedScalar`. `ResolvedProperty` preserves it.
2. **The property-side lattice is the closest fit to a
   signature-typed scalar.** A CALL YIELD scalar is
   schema-untyped (the signature is not a schema entity), but its
   TYPE identity (INT / FLOAT / STRING / opaque) mirrors
   `graph.PropertyType`'s primary axis — the same axis
   `ResolvedProperty.Type` witnesses for the property arm.
   Rehydrating a distinct `ResolvedScalar` sub-lattice for CALL
   YIELD would fragment the wire encoding across two
   discriminator tags for no consumer gain.

**Acknowledged asymmetry — vs R2's `ResolvedScalar` literal
goldens.** Literals in `ExprProjection` (e.g.
`literal_int_projection.cypher`) currently pin
`{kind: "scalar", scalar: "int"}` in their goldens, not
`{kind: "property", type: "INT"}`. R7's use of
`ResolvedProperty` for CALL YIELD produces the `"property"`
discriminator, so R7 goldens read `{kind: "property", type: "INT",
nullable: true}` — the same disc tag R2 uses for
property-projected columns. This asymmetry is intentional: CALL
YIELD scalars carry a nullability bit, and literals do not.
Wire-encoding-wise, downstream consumers distinguish by the
`nullable` field's presence.

### 3.2 `ValidatedQuery.Statement` — CALL is a Read

CALL is a Read clause per `internal/query/query.go:838-842`:

> CallBinding … participates in the Stage-6 refType classifier the
> same way UnwindBinding does — a bare RETURN on a call-yielded
> variable types as ResultType() (see cypher's internal refType in
> typing.go).

The parser's Stage 12 §3.1 `writeSeen` flip fires ONLY on write-side
clauses (SET/REMOVE/DELETE/CREATE/MERGE). A CALL alone leaves
`Query.StatementKind = StatementRead`. R7 does not compute
`Statement`; R6's `resolve.go:51` copies from `q.StatementKind` via
the same code path. **Consequence**: every R7 valid golden pins
`"statement": "read"`. Verified against R6 §3.6.

### 3.3 R0–R6 golden byte-identical claim — no rebaseline

**Every R0–R6 valid golden is byte-identical after R7 lands.** R7
adds no field to `ValidatedQuery`, adds no field to `Column`, adds
no `ResolvedType` variant, adds no discriminator tag. Every existing
golden's `ValidatedQuery` JSON serialises identically before and
after the R7 code cycle merges. The R7 code cycle asserts this by
running `just test` **without** `-update` against the R0–R6 corpus;
any golden that regenerates differently is a bug.

Explicit byte-identical enumeration — the 102 valid stems at branch
base `origin/master @ 0900a8e` (from `ls test/data/resolver/valid/*.
validated.golden.json | sed 's|.*/||; s|\.cypher\.validated\.golden\.
json$||' | sort`):

```
aggregate_at_with
aggregate_count_star
aggregate_sum_property
aggregate_with_expr_residual
aggregate_with_grouping
carry_nullable_binding
carry_wins_over_unlabelled_rebind
create_anonymous_node
create_edge_pattern
create_labelled_node
create_then_return
create_then_wildcard_return
delete_bare_node
delete_bare_property
delete_detach
delete_rich_expression_with_param
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
merge_node_labelled
merge_then_return
merge_with_on_create_set_property
merge_with_on_match_set
multi_type_directed_union
multi_type_undirected
node_mixed_projection
no_demote_var_length_zero_min
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
remove_labels_declared
remove_property
returns_all_local_first_ordering
returns_all_multiple_bindings
returns_all_simple
returns_all_with_edge
self_loop_directed
set_entity_merge_map
set_entity_replace_map
set_labels_declared
set_property_across_with
set_property_bare_param
set_property_int_literal_to_int32
set_property_literal
set_property_on_edge
set_property_on_nullable
set_property_with_star
two_edges_shared_binding
undirected_single_match
undirected_single_match_reverse
undirected_var_length_multi_type_property
union_all_matched_columns
union_all_writes_all_writes
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

**Wire-shape audit for the R7 code cycle.** After the R7 code cycle
lands its CALL Phase A1 admission and its `refProjectionType`
widening, `git diff test/data/resolver/valid/*.validated.golden.json`
on the final commit MUST be empty. Any change indicates a regression
in an R0–R6 code path.

### 3.4 CallBindings DO NOT surface on `ValidatedQuery`

**Judgment call — CallBindings stay on `query.Query`, not on
`ValidatedQuery`, at R7.** The resolver's job at R7 is to certify
that every `CallBinding` bridges to a resolved column type. Codegen
(post-R7) reaches the CallBinding list on the frozen `query.Query`
side (accessible to codegen via the passed-in `query.Query`
alongside the `ValidatedQuery`) if it needs the procedure name,
sourceField, or nullability metadata for driver call planning. Two
reasons R7 does not add a `Calls` axis to `ValidatedQuery`:

1. **No consumer at R7.** ValidatedQuery is provisional through R7
   close-out (ADR 0009 §Decision). Adding an axis before a consumer
   demands it is speculative modelling — the exact anti-pattern the
   staged-build discipline prevents. When codegen lands (under a
   separate ADR — the ADR 0008 analogue), it can choose whether
   to consume CallBindings from `query.Query` or from a resolved
   analogue on `ValidatedQuery`; that decision is not R7's to make.
2. **Every fact codegen needs is already on `query.Query`.** The
   sixty-day-frozen model records the CallBinding's procedure name,
   sourceField, resultType (bridged), and nullable bit — all
   codegen consumes verbatim. The resolver's R7 outcome is a
   `Column` on the wire for each YIELD-projected variable; no
   additional resolved-form CallBinding is warranted.

If future evidence shows codegen needs a resolved-type analogue of
some CallBinding field (e.g. a resolved procedure-signature record
with `procsig.Signature` fields), R7-close-out is the earliest
natural insertion point, under a superseding provisional ADR. R7
does not pre-commit.

### 3.5 Wire-encoding invariants — the R7 golden posture

Every R7 fixture golden carries the R6 fields (`columns`,
`parameters`, `statement`, `distinct`) unchanged in shape. The
`statement` field for a CALL-only query is `"read"` (§3.2). No
CallBinding-specific tag appears in the golden — the CallBinding
metadata does not surface on `ValidatedQuery`. `Column.Type` for a
CALL-yielded column carries the bridged `ResolvedProperty` /
`ResolvedUnknown` per §3.1.

---

## 4. The R7 kernel algorithm

Each step below extends or replaces a numbered step of R6 §4. R0–R6's
per-Part kernel body is preserved verbatim; R7 adds one arm to Phase
A1's kind-switch, widens `refProjectionType`, widens `bindingVariable`,
and adds `callTypes` to `branchState`.

### 4.1 The R7 admissibility widening — CallBindings admitted in Phase A1

R6's Phase A1 kind-switch at `resolve.go:181-236` rejects CALL:

```go
default:
    return nil, branchState{}, nil, fmt.Errorf("%w: %s binding", ErrOutOfR0Scope, b.Kind())
```

For `b.Kind() == BindingCall`, this fires `ErrOutOfR0Scope: call
binding` today. **Verify this exact block at branch base before
writing the code.** The line numbers are stable at branch base
`origin/master @ 0900a8e`; any drift is a merge conflict to resolve
before proceeding.

R7 REPLACES the default's coverage of `BindingCall` with a new
`case query.CallBinding` arm inserted before the default. The new arm:

```go
case query.CallBinding:
    v := bb.Variable()
    // Shadowing: a local CallBinding shadows any carried entity
    // binding at the same name. Same rule as R5 §4.2.3 for node
    // and edge binding shadowing.
    delete(nodeTypes, v)
    delete(edgeTypes, v)
    delete(edgeKeys, v)
    delete(edgeCands, v)
    delete(edgeBindings, v)
    // Same-Part collisions are a parser invariant (Stage-14
    // buildPart §4.7 ErrVariableKindConflict — see /internal/
    // query/cypher/build.go:127-146). The resolver-side defensive
    // tripwire fires only if the parser invariant is breached
    // (unreachable via parse).
    if prev, seen := callTypes[v]; seen {
        // Same-name re-CALL in one Part is grammar-impossible
        // (Stage 14 buildPart §4.7 — a duplicate CallBinding
        // variable fires ErrVariableKindConflict at parse). If we
        // reach here, the parser invariant is broken. Defensive
        // tripwire: return ErrPartBindingTypeConflict.
        _ = prev
        return nil, branchState{}, nil, fmt.Errorf(
            "%w: variable %q re-CALL-bound in single part",
            ErrPartBindingTypeConflict, v)
    }
    callTypes[v] = callBindingSlot{
        resultType:  bb.ResultType(),
        nullable:    bb.Nullable(),
        procedure:   bb.Procedure(),
        sourceField: bb.SourceField(),
    }
```

Where `callBindingSlot` is a package-private struct:

```go
// callBindingSlot carries the resolver-side view of a CallBinding
// at a Part's Phase A1: bridged Stage-6 type, nullability, and the
// two identity strings codegen may consult on the query.Query side.
type callBindingSlot struct {
    resultType  query.Type
    nullable    bool
    procedure   string
    sourceField string
}
```

`callTypes` is a new local map inside `resolvePart`, initialised
alongside `nodeTypes` / `edgeTypes` / etc. at `resolve.go:153-157`:

```go
callTypes := make(map[string]callBindingSlot)
// Carry seed happens BEFORE local bindings write in — local shadows
// carry per §4.2.3 (R5) / §4.6 (R7 callTypes).
for name, slot := range carry.exportedCallTypes {
    callTypes[name] = slot
}
```

The kind-switch default now fires only for `PathBinding` and
`UnwindBinding` — the remaining `BindingKind` values that R7 does
not admit.

#### 4.1.1 Admitted CallBinding shapes

Every parser-emitted `CallBinding` shape is admitted at R7:

| CallBinding shape | Discriminating fixture |
|---|---|
| Standalone CALL, YIELD *, single INTEGER result | `call_standalone_yield_star_integer.cypher` |
| Standalone CALL, YIELD *, single STRING result nullable | `call_standalone_yield_star_string_nullable.cypher` |
| Standalone CALL, YIELD *, single FLOAT result | `call_standalone_yield_star_float.cypher` |
| Standalone CALL, YIELD *, NUMBER result (bridged to Unknown) | `call_standalone_yield_star_number.cypher` |
| In-query CALL YIELD, RETURN | `call_in_query_yield_return.cypher` |
| In-query CALL YIELD AS, RETURN aliased var | `call_in_query_yield_as.cypher` |
| Standalone CALL, YIELD items in signature order | `call_standalone_yield_items.cypher` |
| Standalone CALL, YIELD items reordered from signature | `call_standalone_yield_items_reordered.cypher` |
| CALL YIELD carried through WITH | `call_yield_carry_forward.cypher` |
| CALL YIELD projected in a mixed RETURN (call var + MATCH var) | `call_yield_mixed_return.cypher` |
| CALL YIELD with two result columns | `call_yield_two_columns.cypher` |
| Multi-Part with CALL in Part K and MATCH in Part K+1 | `call_yield_before_match.cypher` |

**No CallBinding shape is rejected at R7.** Every parser-emitted
CallBinding admits at Phase A1. Failure edges are limited to invalid
uses of the CALL-yielded scalar (property lookup — see §4.2.2).

#### 4.1.2 Cross-clause re-bind — prescriptive shape-posture extension

**Frame.** R5 §6.4 introduced the `ErrPartBindingTypeConflict` shape
posture: a carried variable name whose LOCAL re-bind resolves to a
different SHAPE (LabelSetKey mismatch on a node; Labels().Key()
mismatch on an edge) fails at Phase A1 with
`ErrPartBindingTypeConflict`. R5's discriminator is
schema-typed LabelSet identity. R7 EXTENDS this posture: it adds a
new "shape" — the CALL-YIELD scalar — and adds the reciprocal
collision checks for that shape to every existing entity-binding
arm. This is a widening of R5's shape-posture family, not a new
sentinel and not a new discipline. The `ErrPartBindingTypeConflict`
identity is preserved; the wrapped message set widens to name the
new fault.

**Message-set widening declaration.** R7 widens
`ErrPartBindingTypeConflict`'s wrapped message set by one shape:
`variable %q carried as CALL YIELD scalar, re-bound as %s` — where
`%s` names the local-side shape (labelled node's LabelSetKey; edge's
Labels().Key()). This is a SECOND widening of an EXISTING sentinel
in the same spec (alongside `ErrUnknownProperty`'s widening in
§4.2.2), keeping §5's zero-new-sentinels claim TRUE. §5.3 records
both widenings side by side.

**Same-Part collisions are parser-enforced.** Parser Stage 14
`buildPart` (`internal/query/cypher/build.go:127-153`) enforces that
a same-Part collision between a NodeBinding / EdgeBinding /
PathBinding / UnwindBinding and a CallBinding fires
`ErrVariableKindConflict` at parse (five-way sweep — entity / path
/ unwind / prior call / imported). The resolver never sees a Part
whose local `Bindings` contain both a NodeBinding{n} and a
CallBinding{n}. R7 does not defend against this.

**Cross-Part direction A — carried entity → local CALL YIELD.** A
Part K MATCH binding `n:Person` carried through WITH, then Part K+1
`CALL … YIELD n` — this is caught at PARSE by
`build.go:148-150`'s `imported[v]` check (an imported name of ANY
kind collides with a local CallBinding name). Parser fires
`ErrVariableKindConflict` before the resolver runs. Verified
first-party at `build.go:148-150`. R7 does not defend against this.

**Cross-Part direction B — carried CALL YIELD → local entity
(node / edge).** A Part K `CALL … YIELD n` carried through WITH,
then Part K+1 `MATCH (n:Person) RETURN n`. This is
**resolver-reachable**: parser `buildPart` at `build.go:81-85`
populates the local `scope` map from `rp.bindings` (entity sweep)
WITHOUT any collision check against `imported` names — verified
first-party at `build.go:81-85`. The `imported[v]` collision check
at `build.go:148-150` is ONE-DIRECTIONAL (fires only when a local
CallBinding shadows an imported name); the reciprocal (local
NodeBinding / EdgeBinding shadowing an imported CALL-YIELD name)
is silently admitted at parse. Consequence: Part K+1's resolver
Phase A1 sees `imported = {n}` (a call-yielded scalar) and a local
`NodeBinding{n:Person}`. **R7 must prescriptively detect this and
raise `ErrPartBindingTypeConflict`** — otherwise the fixture
silently admits with `n` typed as a NodeBinding and the CALL-YIELD
scalar identity lost. The R6 code guards Phase A1 collisions
INSIDE `case query.CallBinding` only (the arm §4.1 introduces);
the NodeBinding arm at `resolve.go:198-201` and EdgeBinding arm at
`resolve.go:213-233` have no `callTypes` collision check today, so
the fixture would silently admit if R7 didn't add one. §4.1.2.1
below pins the prescriptive checks; §4.1.2.2 covers the reciprocal
directions.

##### 4.1.2.1 NodeBinding × carried CALL YIELD — prescriptive check

R7 EXTENDS the R6 NodeBinding arm at `resolve.go:198-201` with a
`callTypes` collision check. The R7 arm becomes:

```go
case query.NodeBinding:
    if len(bb.Labels()) == 0 {
        pendingNodes = append(pendingNodes, bb)
        continue
    }
    v := bb.Variable()
    key := bb.Labels().Key()
    nt, ok := s.Nodes[key]
    if !ok {
        return nil, branchState{}, nil, fmt.Errorf(
            "%w: %s", ErrUnknownLabel, key)
    }
    // R7 addition — carried CALL YIELD → local labelled node is
    // a cross-clause shape mismatch (scalar vs entity). Message
    // set widening of ErrPartBindingTypeConflict; no new sentinel.
    if _, seenCall := callTypes[v]; seenCall {
        return nil, branchState{}, nil, fmt.Errorf(
            "%w: variable %q carried as CALL YIELD scalar, re-bound as %s",
            ErrPartBindingTypeConflict, v, key)
    }
    // R5 §6.4 unchanged — carried labelled-node shape check.
    if prev, seen := nodeTypes[v]; seen && prev.Labels != nt.Labels {
        return nil, branchState{}, nil, fmt.Errorf(
            "%w: variable %q carried as %s, re-bound as %s",
            ErrPartBindingTypeConflict, v, prev.Labels, nt.Labels)
    }
    nodeTypes[v] = nt
    // Local binding shadows carried edge state (unchanged).
    delete(edgeTypes, v)
    delete(edgeKeys, v)
    delete(edgeCands, v)
    delete(edgeBindings, v)
    // R7 addition — local NodeBinding does NOT shadow-delete the
    // carried callTypes[v] here because the check above already
    // short-circuited; if we reach the delete-block, callTypes[v]
    // was NOT set. No delete(callTypes, v) needed.
```

**Ordering invariant** — the check MUST run BEFORE the `nodeTypes[v]`
write. Otherwise the shape conflict would be masked by the R5
LabelSetKey check if the carry-seed happened to sit alongside a
carried NodeBinding (which cannot co-occur because a variable is
either a CALL-YIELD or an entity carry, not both — but the ordering
discipline is defensive).

**Unlabelled node arm** — `len(bb.Labels()) == 0` falls into
`pendingNodes` before the check, so the collision is NOT detected
at the NodeBinding arm entry. Phase B (`inferUnlabelled`) commits
an unlabelled node's inferred type into `nodeTypes[v]`; the same
`callTypes[v]` check must fire in Phase B before the commit.
**R7 code cycle**: add the check inside `inferUnlabelled`'s commit
site, wrapping `ErrPartBindingTypeConflict` with the same widened
message shape (`carried as CALL YIELD scalar, re-bound as %s`).

##### 4.1.2.2 EdgeBinding × carried CALL YIELD — prescriptive check

R7 EXTENDS the R6 EdgeBinding arm at `resolve.go:213-233` with a
`callTypes` collision check on `bb.Variable()`. The R7 arm becomes:

```go
case query.EdgeBinding:
    if err := r3EdgeAdmissible(bb); err != nil {
        return nil, branchState{}, nil, err
    }
    supportedEdges = append(supportedEdges, bb)
    v := bb.Variable()
    if v == "" {
        continue // anonymous edge; no scope entry
    }
    // R7 addition — carried CALL YIELD → local edge is a
    // cross-clause shape mismatch. Message-set widening of
    // ErrPartBindingTypeConflict.
    if _, seenCall := callTypes[v]; seenCall {
        return nil, branchState{}, nil, fmt.Errorf(
            "%w: variable %q carried as CALL YIELD scalar, re-bound as edge with labels %s",
            ErrPartBindingTypeConflict, v, bb.Labels().Key())
    }
    // R5 §6.4 edge parity — unchanged.
    if prev, seen := edgeBindings[v]; seen && prev.Labels().Key() != bb.Labels().Key() {
        return nil, branchState{}, nil, fmt.Errorf(
            "%w: variable %q carried as edge with labels %s, re-bound with labels %s",
            ErrPartBindingTypeConflict, v, prev.Labels().Key(), bb.Labels().Key())
    }
    edgeBindings[v] = bb
    delete(nodeTypes, v)
    delete(edgeTypes, v)
    delete(edgeKeys, v)
    delete(edgeCands, v)
```

##### 4.1.2.3 PathBinding / UnwindBinding — R-later, unreachable at R7

Path bindings and unwind bindings remain out-of-scope at R7; the
kind-switch default arm at `resolve.go:234-236` continues to reject
them with `ErrOutOfR0Scope`. Because the default fires BEFORE any
`callTypes` collision check could run, the reciprocal collision
(carried CALL YIELD → local PathBinding / UnwindBinding) is
unreachable at R7 — the OOR reject short-circuits first. When
either binding kind is admitted (R-later stages), that stage MUST
add the `callTypes` collision check to its admission arm on the
same posture as §4.1.2.1 / §4.1.2.2. Recorded as a
forward-compatibility note for those stages.

##### 4.1.2.4 CallBinding × carried entity — the CallBinding arm covers it

Reciprocal direction — carried NodeBinding / EdgeBinding into a
local CallBinding — is parser-rejected (build.go:148-150's
`imported[v]` check fires at parse). Additionally, the R7
CallBinding arm at §4.1 unconditionally SHADOW-DELETES the carried
node / edge state via `delete(nodeTypes, v); delete(edgeTypes, v);
delete(edgeKeys, v); delete(edgeCands, v); delete(edgeBindings,
v)` before writing `callTypes[v]`. So even if the parser check
were bypassed, the resolver's local CallBinding correctly shadows
the carried entity, mirroring R5's local-shadows-carry rule for
node/edge (§4.2.3 R5). This is safe because the collision is
resolver-unreachable via parse; the shadow-delete is a defensive
carry-forward-safety measure, not a fault site.

##### 4.1.2.5 Reachability matrix — the fixture pin

| Carried shape | Local shape | Resolver reachable? | Handling |
|---|---|---|---|
| entity (node/edge) | CALL YIELD scalar | NO — parser `build.go:148-150` rejects | (parser-side) |
| CALL YIELD scalar | labelled NodeBinding | **YES** | §4.1.2.1 `ErrPartBindingTypeConflict` — fixture pins |
| CALL YIELD scalar | unlabelled NodeBinding | **YES** (post-Phase-B) | §4.1.2.1 addendum inside `inferUnlabelled` |
| CALL YIELD scalar | EdgeBinding | **YES** | §4.1.2.2 `ErrPartBindingTypeConflict` |
| CALL YIELD scalar | PathBinding / UnwindBinding | NO — R-later OOR reject | (out of scope) |
| entity (node/edge) | CALL YIELD scalar (same Part) | NO — parser `build.go:127-153` rejects | (parser-side) |

**Fixture**: `part_binding_type_conflict_call_vs_node.cypher`
(§6.4) — a labelled MATCH after a carried CALL YIELD. Pins
`ErrPartBindingTypeConflict` at the §4.1.2.1 fail-site.

**Additional fixture**:
`part_binding_type_conflict_call_vs_edge.cypher` (§6.4) — an
EdgeBinding after a carried CALL YIELD. Pins the §4.1.2.2
fail-site. Both fixtures target the SAME sentinel; both add to the
widened `ErrPartBindingTypeConflict` message set.

#### 4.1.3 Defensive tripwires

The R7 CALL arm carries two defensive tripwires:

- **Same-Part duplicate CallBinding variable** — grammar-impossible
  per parser Stage 14 §4.7 (`ErrVariableKindConflict`). If reached,
  emit `ErrPartBindingTypeConflict` with a "re-CALL-bound" message.
- **CallBinding whose Variable is empty** — model-invariant guard;
  `query.NewCallBinding` at `internal/query/query.go:857-877` rejects
  empty Variable. If reached, the switch never fires because the
  parser never emits it. No explicit R7-side check.

Both tripwires are unreachable via parse; the R7 code cycle asserts
they are unreachable by not adding fixtures for them.

#### 4.1.4 Phase D — CallBinding nullability seed

R6's `seedLocalNullability` and `demoteNullableInPlace` walk
`part.Bindings` and kind-switch. R7 widens each switch:

```go
// seedLocalNullability (R7 addition):
case query.CallBinding:
    nullableBinding[bb.Variable()] = bb.Nullable()
// demoteNullableInPlace (R7 addition):
case query.CallBinding:
    // CallBinding never demotes other bindings (not an edge, not a
    // demoter). Do nothing.
```

**Composition with R4 OPTIONAL demotion.** A CallBinding cannot
appear inside an `OPTIONAL MATCH` (CALL is a top-level clause,
grammar-disjoint from `OPTIONAL MATCH`); no R4 demoter widens it.
Consequence: `nullableBinding[cb.Variable()]` equals `cb.Nullable()`
after Phase D, and the R2 propagation into `ResolvedProperty`'s
`Nullable` field in `refProjectionType` reads this bit unchanged.

**Composition with carried nullability.** A CallBinding at Part K
whose Variable was carried nullable from Part K-1 (impossible — a
CallBinding introduces the variable; it cannot be a re-binding of
a carried variable, per §4.1.2) is a non-issue. A carried
CALL-yielded variable at Part K+1 reads `nullableBinding[v]` from
the carry-seed step, and R4's `seedLocalNullability` re-writes it
only if a local Binding for `v` fires — which does not happen (a
Part K+1 without a fresh CALL for `v` does not touch it). The
carry-forward path is honest.

### 4.2 CALL YIELD column typing via `refProjectionType`

R6's `refProjectionType` at `resolve.go:1027-1084` handles Refs
against `nodeTypes` / `edgeTypes` / `edgeCands` / `carriedResolvedTypes`.
R7 adds a `callTypes` lookup arm before the `carriedResolvedTypes`
bypass. The revised skeleton:

```go
func refProjectionType(
    ref query.Ref,
    nodeTypes map[string]schema.NodeType,
    edgeTypes map[string]schema.EdgeType,
    edgeKeys map[string]schema.EdgeKey,
    edgeCands map[string][]schema.EdgeKey,
    edgeBindings map[string]query.EdgeBinding,
    nullableBinding map[string]bool,
    callTypes map[string]callBindingSlot,             // NEW at R7
    carriedResolvedTypes map[string]ResolvedType,
    s schema.Schema,
) (ResolvedType, error) {
    // R6 node arms unchanged.
    if nt, ok := nodeTypes[ref.Variable]; ok { ... }

    _, singleCand := edgeTypes[ref.Variable]
    cands, multiCand := edgeCands[ref.Variable]
    if !singleCand && !multiCand {
        // R7 addition — CALL YIELD lane:
        if slot, ok := callTypes[ref.Variable]; ok {
            return callProjectionType(slot, ref, nullableBinding)
        }
        // R6 carried-alias bypass unchanged.
        if rt, ok := carriedResolvedTypes[ref.Variable]; ok && ref.Property == "" {
            return rt, nil
        }
        return nil, fmt.Errorf("%w: %s", ErrOutOfR0Scope, ref.Variable)
    }
    // R6 edge arms unchanged.
    ...
}
```

Signature change is not spec-load-bearing — `projectionType` at
`resolve.go:1010` will thread `callTypes` through similarly. The R6
`projectionType` signature widens by one parameter; no other
callers exist.

#### 4.2.1 Bare Ref — the type mapping

`callProjectionType(slot, ref, nullableBinding)` for
`ref.Property == ""`:

| CallBinding `ResultType()` | Signature token | `Column.Type` |
|---|---|---|
| `query.TypeInt{}` | `INTEGER` | `ResolvedProperty{Type: graph.TypeInt, Nullable: nullableBinding[ref.Variable]}` |
| `query.TypeFloat{}` | `FLOAT` | `ResolvedProperty{Type: graph.TypeFloat, Nullable: nullableBinding[ref.Variable]}` |
| `query.TypeString{}` | `STRING` | `ResolvedProperty{Type: graph.TypeString, Nullable: nullableBinding[ref.Variable]}` |
| `query.TypeUnknown{}` | `NUMBER` | `ResolvedUnknown{}` |
| (any other) | — | defensive: `ResolvedUnknown{}` |

The `nullableBinding[ref.Variable]` lookup reads exactly what §4.1.4
seeded — the CallBinding's `Nullable()` bit — because no R4 demoter
touches a call-yielded variable (§4.1.4).

**Judgment call — NUMBER bridges to ResolvedUnknown, not to a
NUMBER-flavoured resolved type.** Two reasons:

1. The parser's bridge already collapses NUMBER to `TypeUnknown` on
   the wire (`typeForToken(TokenNumber) → query.TypeUnknown{}`, per
   `internal/query/cypher/call.go:240-241`). Rehydrating a
   NUMBER-flavoured resolved type in the resolver would re-introduce
   NUMBER identity into a place ADR 0007's Stage-14 note
   deliberately expunged it.
2. `graph.PropertyType` (the schema vocabulary the resolver's
   `ResolvedProperty` carries) does not enumerate a NUMBER family;
   INT and FLOAT are separate families. A "NUMBER column" has no
   `PropertyType` to name. Consumers that need NUMBER identity
   consult `procsig.Registry` on the query.Query side (per ADR 0007
   §Note "codegen consumes procsig.Registry values directly").

**Assignability at the write site is deferred**. R7 does not
generate any `SET n.p = callYieldedVar` fixture that would exercise a
CALL-yielded variable at a write site; §7.1.1 records the same
value-target assignability deferral that R6 §7.1.1 records for
literals, unchanged for CALL YIELD.

#### 4.2.2 Ref with a property — `ErrUnknownProperty`

`callProjectionType(slot, ref, nullableBinding)` for
`ref.Property != ""`:

```go
return nil, fmt.Errorf("%w: %s.%s (CALL YIELD variable %q is a scalar)",
    ErrUnknownProperty, ref.Variable, ref.Property, ref.Variable)
```

A CallBinding YIELD variable carries a scalar (INT / FLOAT / STRING /
opaque) — never an entity with properties. `ref.Variable.prop` is
grammar-legal (openCypher does not distinguish "property access on
a scalar" as a compile-time error) but resolver-time invalid.

**Sentinel choice**: `ErrUnknownProperty` matches the fail-site's
shape ("a property was named, but the entity carries no such
property, because the entity is not an entity"). No new sentinel is
warranted — R7 widens `ErrUnknownProperty`'s message set at the
existing R2 fail-site. R6 sentinel `ErrOutOfR0Scope` is NOT
appropriate here (CALL YIELD is in scope; the property lookup
itself is what fails).

Failure edge — fixture `call_yield_property_lookup.cypher` (§6.4):
`CALL test.my.proc('Stefan', 1) YIELD city\nRETURN city.length`.
Golden pins `ErrUnknownProperty: city.length`.

**Judgment call — no separate ErrCallScalarPropertyAccess
sentinel.** Adding a sentinel per failure family is anti-pattern
(the R6 spec §5's discipline: only add when no existing sentinel
names the fault honestly). `ErrUnknownProperty` is exactly right:
the property does not exist. Message text disambiguates for
diagnostics.

### 4.3 `RETURN *` and CALL YIELD — `buildScopeOrder` widening

R6's `bindingVariable` at `resolve.go:1231-1240` returns
`(bb.Variable(), true)` for NodeBinding and EdgeBinding, and
`("", false)` for the default case. `buildScopeOrder`
(`resolve.go:370-404`) iterates `part.Bindings`, calling
`bindingVariable`, and skipping any binding it does not name — so a
CallBinding today does not participate in `RETURN *` expansion.

R7 widens `bindingVariable`:

```go
case query.CallBinding:
    return bb.Variable(), true
```

`buildScopeOrder`'s subsequent `nodeTypes` / `edgeBindings`
membership check widens for CallBindings — the current arm rejects
any variable that is not in `nodeTypes` or `edgeBindings`. R7
updates the check to include `callTypes`:

```go
if _, isNode := nodeTypes[v]; isNode { ... }
if _, isEdge := edgeBindings[v]; isEdge { ... }
if _, isCall := callTypes[v]; isCall {
    seen[v] = true
    out = append(out, v)
    continue
}
```

**Consequence** — `Part.ReturnsAll = true` on a Part containing a
CallBinding now includes that CallBinding's variable in the
synthetic-Returns expansion. This is what makes the parser's Stage
14 §4.3 `Part.ReturnsAll` synthesis for standalone CALL land
resolved correctly:

```
CALL test.labels() YIELD *   →   parser synthesises
                                 Part{Bindings: [CallBinding{"label",
                                                             "test.labels",
                                                             "label",
                                                             TypeString, true}],
                                      ReturnsAll: true,
                                      Returns: [(synthesised at buildPart)]}
```

R7's `buildScopeOrder` widening makes `scopeOrder = ["label"]`, and
R6's `materialiseReturns` walks that list to produce a
`RefProjection{Ref{"label", ""}, TypeString}` per item, which
`refProjectionType` witnesses via the new `callTypes` arm to
`ResolvedProperty{Type: graph.TypeString, Nullable: true}`.

### 4.4 Unknown procedure — parser-authoritative trust posture

**Unknown procedure is a generation-time error by design (ADR 0009
R7 line, verbatim).** The parser fires
`cypher.ErrUnknownProcedure` at parse (per
`internal/query/cypher/call.go:41-44` for procedure-name miss, and
`call.go:135-137` for YIELD result-field miss). The resolver
**never runs** on a query with an unknown procedure — the parser
returned the error before the resolver was called.

**The resolver does NOT re-check.** No R7 code path consults
`r.registry` (the R6 resolver receives it via WithRegistry but
today discards it at `resolve.go:110-112`). R7 continues to
receive the registry, and continues to discard it: the registry is
the parser's compile-time input, not the resolver's. The R6 line
`_ = r` at `resolve.go:110` stays.

**Wait — the resolver receives a registry, why doesn't it use it?**
Because the parser has already done the work. The parser's
`CallBinding` on the wire records the procedure name, the source
field, and the bridged result type — every fact the resolver needs
for YIELD column typing is on the parser's output. Re-looking-up
the procedure in the resolver's registry would:

1. **Duplicate work**. The parser already resolved. The resolver
   would confirm the same fact.
2. **Introduce a divergence risk**. If the resolver is
   constructed with a different registry than the parser (e.g. a
   test-side error), the resolver could reach `ErrUnknownProcedure`
   for a query that parsed cleanly. That is a diagnostic-quality
   regression, not a correctness gain.
3. **Add a new sentinel** (resolver-side `ErrUnknownProcedure`) with
   the same message text as the parser's, breaking the "one
   sentinel per fault" discipline (ADR 0009 §Test strategy).

**Trust posture — same family as R6's Refs referential integrity.**
R6 §4.3.1 records that "R6 does NOT re-run referential integrity on
these — the parser's `buildPart` sweep at `internal/query/cypher/
build.go:155-158` has already asserted every ref is bound in scope."
R7 applies the identical posture to procedure lookup: the parser
did it; the resolver trusts it. Fixture: no positive test — the
absence of an R7 sentinel is the trust posture.

**Registry threading is preserved for future use.** The
`WithRegistry` option, `Resolver.registry` field, and
`resolve.go:110-112` `_ = r` discard all stay untouched. If a future
stage (post-R7, under a new ADR) needs resolver-side registry
consultation (e.g. cross-checking parser and resolver see the same
signatures), the threading is already in place. R7 does not build
on it.

**Empty registry semantics.** A resolver constructed without
`WithRegistry` receives the zero `procsig.Registry`. If given a
query containing CALL, the parser would have raised
`ErrUnknownProcedure` at parse time; the query never reaches the
resolver. Consequence: an empty-registry resolver behaves identically
to a full-registry resolver on any query it actually sees, because
any query containing CALL against an empty parser-registry never
reaches the resolver in the first place. The R6 `_ = r` discard is
correct.

### 4.5 Standalone CALL synthetic `Returns`

Parser Stage 14 §4.3 populates `Part.Returns` at build time for a
standalone CALL:

- **`CALL test.labels()` (no YIELD, standalone)**: `Part.ReturnsAll
  = true` and `Part.Returns` is `[{Name: "label", Value:
  RefProjection{Ref{"label", ""}, TypeString{}}}]` (one item per
  signature Result, in declaration order).
- **`CALL test.labels() YIELD *` (standalone)**: same synthesis
  as above.
- **`CALL test.labels() YIELD label` (standalone, explicit)**:
  `Part.ReturnsAll = false` and `Part.Returns` is the explicit
  parser-emitted list.
- **`CALL test.labels() YIELD label\nRETURN label` (in-query with
  downstream RETURN)**: `Part.ReturnsAll = false` and
  `Part.Returns` is the author's explicit RETURN.

The R6 `materialiseReturns` (`resolve.go:324-341`) processes both
`ReturnsAll = true` (via `virtualProjection`) and
`ReturnsAll = false` (verbatim) paths uniformly. R7 does not change
`materialiseReturns`.

**Consequence — standalone CALL "just works" once §4.1 (Phase A1
admission) + §4.2 (`refProjectionType` widening) + §4.3
(`buildScopeOrder` widening) are in place.** The parser has already
done the work of naming what a `RETURN *` for a standalone CALL
should project; the resolver's job is to type-resolve each item.

**Worked trace — Standalone CALL, no YIELD:**

```
Query: CALL test.labels()
Parser output (per parser_test.go:2200-2216):
  Query{
    Branches: [Branch{Parts: [Part{
      Bindings: [CallBinding{"label", "test.labels", "label", TypeString, true}],
      ReturnsAll: true,
      Returns: (parser synthesises at buildPart — one RefProjection per
                CallBinding),
    }]}],
    StatementKind: StatementRead,
  }
Phase A1: callTypes["label"] = {TypeString, true, "test.labels", "label"}.
Phase D: nullableBinding["label"] = true (signature `?`).
scopeOrder = ["label"] (buildScopeOrder R7 widening).
materialiseReturns: parser's synthetic Returns walked.
Projection walk: RefProjection{Ref{"label", ""}} →
  refProjectionType hits the callTypes arm →
  callProjectionType returns ResolvedProperty{Type: graph.TypeString, Nullable: true}.
Result: 1 column {"label", string?, distinct false, statement "read"}.
```

**Worked trace — In-query CALL YIELD with downstream RETURN:**

```
Query: CALL test.labels() YIELD label\nRETURN label
Parser output (per parser_test.go:2222-2240):
  Query{
    Branches: [Branch{Parts: [Part{
      Bindings: [CallBinding{"label", "test.labels", "label", TypeString, true}],
      ReturnsAll: false,
      Returns: [{Name: "label", Value: RefProjection{Ref{"label", ""}, TypeString{}}}],
    }]}],
    StatementKind: StatementRead,
  }
Phase A1: callTypes["label"] = {TypeString, true, "test.labels", "label"}.
Phase D: nullableBinding["label"] = true.
Projection walk: RefProjection{Ref{"label", ""}} →
  refProjectionType hits the callTypes arm →
  ResolvedProperty{STRING, true}.
Result: 1 column {"label", string?, distinct false, statement "read"}.
```

**Worked trace — CALL YIELD carried through WITH:**

```
Query: CALL test.labels() YIELD label\nWITH label\nRETURN label
Parser output:
  Query{
    Branches: [Branch{Parts: [
      Part{
        Bindings: [CallBinding{"label", ...}],
        Returns: [{Name: "label", Value: RefProjection{...}}],
      },
      Part{
        Bindings: [],
        Returns: [{Name: "label", Value: RefProjection{Ref{"label", ""}, TypeString{}}}],
      },
    ]}],
  }
Part 0: Phase A1 seeds callTypes["label"]; Phase D nullableBinding["label"]=true;
        projection walks RefProjection{"label", ""} → ResolvedProperty{STRING, true};
        exportScope carries callTypes["label"] forward to Part 1.
Part 1: Phase A1 seed step reads callTypes["label"] from carry.exportedCallTypes;
        no local bindings; projection walks RefProjection{"label", ""} →
        refProjectionType hits callTypes["label"] → ResolvedProperty{STRING, true}.
Result: 1 column {"label", string?, statement "read"}.
```

### 4.6 `exportScope` — `callTypes` carry-forward

R6's `exportScope` at `resolve.go:406-484` builds the branchState
Part K passes to Part K+1. R7 adds a `callTypes` export lane to
`branchState`:

```go
// branchState (R7 addition):
exportedCallTypes map[string]callBindingSlot
```

`exportScope` populates `exportedCallTypes` following the same rule
as R5's `exportedNullableBinding` / `exportedNodeTypes`: **only the
CallBinding names that survive the WITH's export filter are
exported**. Concretely:

- **Explicit WITH (`ReturnsAll = false`)**: iterate `items`; if
  `item.Value` is a `RefProjection{Ref{v, ""}}` and `v` is in
  `callTypes`, export `callTypes[v]` under `item.Name`. Alias renames
  are honest: `WITH label AS lbl` carries `callTypes["label"]` as
  `exportedCallTypes["lbl"]`.
- **WITH * (`ReturnsAll = true`)**: export every `callTypes[v]`
  entry, keyed by `v` (same key as the local map).

Non-obvious detail — the R5 `exportedResolvedTypes` lane at
`resolve.go:433-439` for aliased scalar projections handles renamed
CALL YIELD variables via the R5 path already: a `WITH label AS lbl`
where `label` is a CALL YIELD variable projects as
`ResolvedProperty{STRING, true}` (via `callTypes` in Part K), and
that ResolvedType lands in `exportedResolvedTypes["lbl"]`. The R7
`exportedCallTypes` lane is redundant with `exportedResolvedTypes`
for the aliased case; R7 keeps both because `exportedCallTypes`
carries the CallBinding-specific metadata (procedure, sourceField)
that `exportedResolvedTypes` does not — for future stages that may
need it. **R7 does not consume `exportedCallTypes` for anything
beyond re-seeding `callTypes` at Part K+1's Phase A1.** The two
lanes agree on the type witness; `exportedCallTypes` is the
identity metadata lane.

**Simplification opportunity acknowledged.** A future stage may
collapse `exportedCallTypes` into `exportedResolvedTypes` +
metadata table. R7 keeps them separate because:

1. `exportedResolvedTypes` alone loses the CallBinding's procedure
   / sourceField identity, which the code-cycle implementation of
   `refProjectionType` may want (for diagnostic messages).
2. Aliased-scalar exports and CALL YIELD exports are semantically
   distinct kinds of "carried scalar"; keeping the lanes distinct
   preserves the shape for future stages.
3. The overhead is one extra map on `branchState`. Negligible.

### 4.7 `virtualProjection` and CALL YIELD — no change needed

R6's `virtualProjection` at `resolve.go:347-368` synthesises a
`RefProjection` for a bare-name binding in a `RETURN *` /
`WITH *` context. For a CALL YIELD variable, the parser has ALREADY
populated `Part.Returns` with a properly-typed
`RefProjection{Ref{v, ""}, cb.ResultType()}` — parser Stage 14 §4.3
does this at `buildPart`. Consequence: `materialiseReturns`'s
`ReturnsAll` path finds a Returns list already populated (parser-side
synthesis), and `virtualProjection` is not called for CALL YIELD.

**Verify** that the R5 `materialiseReturns` handles the case where
`ReturnsAll = true` AND `Returns` is non-empty (parser-synthesised).
Reading `resolve.go:324-341`:

```go
if part.ReturnsAll {
    items := make([]query.ReturnItem, 0, len(scopeOrder))
    for _, name := range scopeOrder {
        item, err := virtualProjection(name, carry, nodeTypes, edgeBindings)
        if err != nil { return nil, err }
        items = append(items, item)
    }
    return items, nil
}
return part.Returns, nil
```

The R5 code branches on `ReturnsAll` — if true, it IGNORES
`part.Returns` and synthesises via `virtualProjection`. This means
the parser's Stage 14 §4.3 synthesis for standalone CALL is
**overwritten** by the R5 kernel's `virtualProjection` synthesis.

**Consequence**: R7's `virtualProjection` MUST widen to synthesise
correctly for a call-yielded variable. The current arm at
`resolve.go:347-368`:

```go
func virtualProjection(name string, carry branchState, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding) (query.ReturnItem, error) {
    if _, ok := nodeTypes[name]; ok {
        return query.NewRefProjection(query.Ref{Variable: name}, query.TypeNode{}), nil
    }
    if binding, ok := edgeBindings[name]; ok {
        if binding.Hops() != nil {
            return query.NewRefProjection(query.Ref{Variable: name}, query.TypeList{}), nil
        }
        return query.NewRefProjection(query.Ref{Variable: name}, query.TypeEdge{}), nil
    }
    // §4.5.4 bypass path serves it. Use a placeholder RefProjection whose
    // ...
    if _, ok := carry.exportedResolvedTypes[name]; ok {
        return query.NewRefProjection(query.Ref{Variable: name}, query.TypeUnknown{}), nil
    }
    ...
}
```

R7 widens to include CallBinding synthesis. **Signature-order
discipline**: the existing R6 signature is `virtualProjection(name,
nodeTypes, edgeBindings, carry)` — R7 appends `callTypes` at the
TAIL, preserving parameter order for every prior arg. No
reordering.

```go
func virtualProjection(name string, nodeTypes map[string]schema.NodeType, edgeBindings map[string]query.EdgeBinding, carry branchState, callTypes map[string]callBindingSlot) (query.Projection, error) {
    if _, ok := nodeTypes[name]; ok { ... /* unchanged */ }
    if binding, ok := edgeBindings[name]; ok { ... /* unchanged */ }
    if slot, ok := callTypes[name]; ok {
        return query.NewRefProjection(query.Ref{Variable: name}, slot.resultType), nil
    }
    // R6 carried-alias bypass unchanged.
    ...
}
```

The wrapping `NewRefProjection` also needs to reflect the
CallBinding's type. `slot.resultType` is `query.TypeInt` /
`query.TypeFloat` / `query.TypeString` / `query.TypeUnknown` — all
of which `NewRefProjection` admits per `internal/query/query.go`
NewRefProjection constructor.

**One implementation subtlety.** `NewRefProjection`'s signature is
`(ref Ref, t Type) (RefProjection, error)`. `slot.resultType`
matches. No shape error.

**Signature widening**: `virtualProjection` gains `callTypes` as its
fifth (tail) parameter — new-only-at-tail; no existing param is
reordered. The single caller (`materialiseReturns`) threads it.
`materialiseReturns` gains `callTypes` as its final trailing
parameter too. No other callers.

**Correctness check on the parser's Stage 14 §4.3 synthesis.** The
parser injects RefProjection items into `Part.Returns` for a
standalone CALL — the R5 kernel's `ReturnsAll = true` branch
IGNORES those items and re-synthesises via `virtualProjection`. R7's
widened `virtualProjection` re-produces the same items (name and
type match the parser's). The parser's synthesis is redundant with
the resolver's synthesis; both agree on the wire. **R7 does not
change the parser's behaviour** — it just makes the resolver's
`ReturnsAll` synthesis honest for CallBindings.

### 4.8 Parameter Uses under CALL argument expressions

Parser Stage 14 §4.5 emits CALL argument parameter uses as
`ExprUse{miningType, ExprInProjection}` per
`internal/query/cypher/call.go:54-66`:

```go
for _, e := range argExprs {
    t, refs, params := l.typeExpressionMining(e)
    for _, ref := range refs {
        l.curPart.refs = append(l.curPart.refs, varRef{name: ref.Variable})
    }
    for _, p := range params {
        n := parameterName(p)
        if n == "" { continue }
        l.addParameterUse(n, p, query.NewExprUse(t, query.ExprInProjection))
    }
}
```

The `ExprInProjection` position is a **stand-in** — there is no
`ExprInCallArg` position on the wire. R5's parameter unifier
(`unifyParameterUsesAcrossScopes`) walks each parameter's Uses and
witnesses them via `witnessAcrossScopes` at `resolve.go:673-719`.
R7 does NOT change `witnessAcrossScopes`.

**Consequence** — a CALL argument parameter use is witnessed as
`resolveType(uu.EnclosingType())`, exactly as an
`ExprInProjection` use in a normal projection. If the enclosing
type is a scalar (INT / FLOAT / STRING / Unknown), the witness is
`ResolvedProperty{Type, false}` or `ResolvedUnknown{}`. If the
parameter also appears elsewhere in the query (in a MATCH's
`WHERE`, in a property projection), the R2 unifier composes the
witnesses. **No R7 code change is required.**

**Non-obvious consequence**: a CALL argument parameter that is
NOT declared in `query.Query.Parameters` but appears in `argExprs`
is a wire consistency violation that the parser would have caught
(parser's declared vs used sweep). R7 does not defend against this.

~~**The NUMBER assignable-from axis has NO R7 application site.**
The team-lead ruling stands: because parser attribution is
`ExprInProjection` (not `ExprInCallArg`), the resolver cannot tell
that a Use came from a CALL argument. Even if the CALL param's
declared token is NUMBER, the resolver has no way to link the Use
back to the param position. The assignability check would need
per-position CALL-arg attribution on the wire; the parser does not
emit it. §7.1 records the widening required.~~ [closed 2026-07-07
by gqlc-0ig (PRs #122/#123/#124,
`docs/specs/unfreeze-0ig-call-args.md`): per-position CALL-arg
attribution now lands on the wire as `CallBinding.Args()`; the
resolver's Phase A1 CallBinding arm walks `Args()` against
`procsig.Registry.Lookup(procedure).Params[i].Token` and enforces
the ADR 0007 Stage-14 assignability lattice (NUMBER accepts
INTEGER-or-FLOAT; FLOAT accepts INTEGER; STRING / INTEGER strict;
`TypeUnknown` / `TypeNull` wildcards), firing
`ErrCallArgAssignability` on mismatch. The paragraph above is
preserved as-of-R7-shipping for historical grounding; §7.1.1
banner is authoritative.]

---

## 5. Sentinels — the R7 revision

R7 introduces **zero new sentinels**. Every R7 fail-site is covered
by an R0–R6 sentinel. Explicit inventory:

### 5.1 Sentinel additions — none

R7 introduces no new sentinel. The R6 close-out committed to one
per stage as a hard maximum (spec §5.1 R6); zero is a floor R7 hits
naturally because YIELD column typing is a positive-output
operation, not a rejection-emitting one.

**Explicit design decision — do NOT add
`ErrCallScalarPropertyAccess` or similar.** The one R7 failure mode
(property lookup on a call-yielded scalar) is exactly
`ErrUnknownProperty`'s shape: "a property was named, but the entity
carries no such property, because the referent is not an entity."
Adding a new sentinel would violate "one sentinel per fault"
without a corresponding surface area gain — the wrapped message
disambiguates for the diagnostic reader.

### 5.2 Revised `ErrOutOfR0Scope` message set — one retirement

R7 retires ONE `ErrOutOfR0Scope` fail-site:

```go
// R7 retires (Phase A1 default arm at resolve.go:234-236 for
// BindingCall):
"%w: call binding" (from ErrOutOfR0Scope)
```

**The R6 default arm survives** — path bindings and unwind bindings
continue to fire `%w: <kind> binding` there. Only the `BindingCall`
sub-case is retired via the new `case query.CallBinding` arm.

R6's message-set list under `errors.go` documenting
`ErrOutOfR0Scope`'s remaining fail-sites should update to remove the
"call binding" entry. The comment already reads (per errors.go:24-30):

> `ErrOutOfR0Scope` is returned when the query contains a construct
> the current capability scope does not support (multi-part,
> multi-branch, AggregateProjection, WITH, UNION, writes, CALL,
> RETURN DISTINCT, RETURN *, undirected / var-length / multi-type /
> untyped edges, path / unwind / call bindings, write-side ExprUses
> (SET / DELETE), ExprProjection typed as list-of-entities). Its
> fail-sites retire stage by stage as R2..R7 introduce the
> constructs.

R7 updates the "call bindings" portion of that comment — the CALL
retirement lands the R7 arm.

### 5.3 R6 sentinels' message sets — TWO widenings

R7 widens the wrapped message set of TWO existing sentinels;
NEITHER is a new sentinel; both keep §5.1's zero-new-sentinels
claim TRUE.

**`ErrUnknownProperty` widens** at the `refProjectionType` CALL
YIELD arm (§4.2.2):

```go
// R7 addition:
"%w: %s.%s (CALL YIELD variable %q is a scalar)" (ErrUnknownProperty)
```

Fixture: `call_yield_property_lookup.cypher` (§6.4).

**`ErrPartBindingTypeConflict` widens** at the NodeBinding /
EdgeBinding admission arms (§4.1.2.1 / §4.1.2.2):

```go
// R7 additions:
"%w: variable %q carried as CALL YIELD scalar, re-bound as %s"           (ErrPartBindingTypeConflict) // NodeBinding arm
"%w: variable %q carried as CALL YIELD scalar, re-bound as edge with labels %s" (ErrPartBindingTypeConflict) // EdgeBinding arm
```

Fixtures: `part_binding_type_conflict_call_vs_node.cypher` and
`part_binding_type_conflict_call_vs_edge.cypher` (§6.4).

**Framing — the widening is a shape-posture extension, not a new
policy.** R5 §6.4 established that
`ErrPartBindingTypeConflict` fires when a carried-name's
LOCAL re-bind resolves to a different SHAPE (LabelSetKey /
Labels().Key() mismatch). R7 EXTENDS the shape universe by adding
the CALL-YIELD scalar as a new shape, and adds the collision
checks so the scalar-vs-entity direction fires the SAME sentinel
under a WIDENED wrapped-message set. This is the same discipline
R7 applies to `ErrUnknownProperty` at §4.2.2 (widening the
"property was named but doesn't exist" family to include
"property was named on a scalar CALL-YIELD"). Both widenings live
under the "same sentinel, more messages" rule ADR 0009 §Test
strategy encodes.

R6 sentinels other than `ErrUnknownProperty` and
`ErrPartBindingTypeConflict` do NOT widen at R7. The CALL-related
parser-time sentinels (`ErrUnknownProcedure`, `ErrProcedureArity`,
`ErrVariableKindConflict`) fire in the `cypher` package before the
resolver runs; they are not on the resolver's `allSentinels` and
are not counted here.

### 5.4 The closed R7 set — unchanged from R6

```go
// allSentinels closed set — R7 UNCHANGED FROM R6:
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
    ErrInvalidEffectTarget,
}
```

**TestSentinelReachability discipline.** R7's fixture set must
maintain the R6 bidirectional invariant: every `allSentinels`
member has ≥1 negative fixture; every mapped sentinel is in
`allSentinels`. R7 does not add a sentinel, so the invariant is
maintained iff every existing member still has a fixture.

**`ErrPartBindingTypeConflict` — the R7 addition sites.** R7's
`part_binding_type_conflict_call_vs_node.cypher` and
`part_binding_type_conflict_call_vs_edge.cypher` fixtures (§6.5)
pin `ErrPartBindingTypeConflict` for the two cross-clause
CALL-YIELD-scalar re-bind directions (node arm and edge arm). Both
are new fail-sites for an EXISTING sentinel under a WIDENED
wrapped-message set (§5.3); the sentinel identity is unchanged.
The bidirectional discipline continues to hold.

**`ErrUnknownProperty` — R7 pins.** R7's
`call_yield_property_lookup.cypher` pins `ErrUnknownProperty` for
the property-on-scalar fail-site. R6 already has fixtures for
`ErrUnknownProperty` (`unknown_property.cypher` and others); R7
adds one more.

---

## 6. The golden-pair harness — R7 revision

### 6.1 Schema fixture strategy — one new schema

R0–R6's harness (`resolver_test.go`, `test/data/resolver/{valid,
invalid}/`) is preserved verbatim except for the `loadQuery`
registry threading (§6.2). R7 adds fixtures under both
`valid/*.cypher` (with paired goldens) and `invalid/*.cypher`
(with the paired `invalidFixtures` map entry). One new schema
fixture:

- **`social_r7.gql`** — a copy of `social_r6.gql`. R7's YIELD
  column types are opaque scalars, disconnected from the graph
  schema — the schema shape barely matters for the CALL fixtures.
  Copying preserves R6 invariants for the R7 fixtures that
  incidentally MATCH before or after CALL (e.g.
  `call_yield_before_match.cypher` matches a `Person` after
  yielding).

Existing `social_r5.gql` / `social_r6.gql` / earlier fixtures
continue to resolve against their existing schema.

### 6.2 Schema fixture text — `social_r7.gql`

Identical to `social_r6.gql` — copy verbatim. No new node types,
no new edges, no new properties. The redundant copy pins the
per-stage schema convention (R2–R6 each got a schema of their own,
even when the delta was minimal).

**Design note.** A future spec cycle may collapse
`social_r7.gql = social_r6.gql = social_r5.gql`; the per-stage
schema convention is a strict discipline (each stage may claim its
own schema shape) that R7 preserves.

### 6.3 Registry fixture — inside `resolver_test.go`

R7's registry fixture lives inside `internal/resolver/resolver_test.go`
as a package-private `signaturesR7` slice. **Judgment call**: keep
the declaration in the test package rather than under
`test/data/resolver/valid/`. Rationale: (1) `test/data/resolver/`
contains only `.cypher` sources and `.validated.golden.json`
goldens today — introducing the first Go source in that tree
breaks the "data only" convention there (verified via
`find test/data/resolver -name '*.go'` — zero results at
`origin/master @ 0900a8e`); (2) the registry is test-harness state,
not a golden the harness verifies against, so it belongs beside
`loadQuery` (the sole consumer); (3) a package-private
`signaturesR7` var is the shortest wiring path from declaration to
`cypher.WithRegistry(procsig.NewRegistry(signaturesR7))` in
`TestMain` / `SetupSuite`.

Sketch (all inside `internal/resolver/resolver_test.go`):

```go
// signaturesR7 declares the procedures the R7 fixture set exercises.
// Each mirrors a Stage-14 corpus signature (parser_test.go §Call*).
var signaturesR7 = []procsig.Signature{
    // Single STRING? result — the "test.labels" from Call1[5].
    {
        Name:    "test.labels",
        Params:  nil,
        Results: []procsig.Result{
            {Name: "label", Token: procsig.TokenString, Nullable: true},
        },
    },
    // Two-column result — the "test.my.proc" from Call5[3].
    {
        Name:   "test.my.proc",
        Params: []procsig.Param{
            {Name: "name", Token: procsig.TokenString, Nullable: true},
            {Name: "id",   Token: procsig.TokenInteger, Nullable: true},
        },
        Results: []procsig.Result{
            {Name: "city",         Token: procsig.TokenString,  Nullable: true},
            {Name: "country_code", Token: procsig.TokenInteger, Nullable: true},
        },
    },
    // Single INTEGER? result — for the INTEGER-column fixture.
    {
        Name:    "test.count",
        Params:  nil,
        Results: []procsig.Result{
            {Name: "n", Token: procsig.TokenInteger, Nullable: true},
        },
    },
    // Single FLOAT? result — for the FLOAT-column fixture.
    {
        Name:    "test.temperature",
        Params:  nil,
        Results: []procsig.Result{
            {Name: "celsius", Token: procsig.TokenFloat, Nullable: true},
        },
    },
    // Single NUMBER? result — for the NUMBER-bridge fixture.
    // NUMBER is a signature-only marker; the wire bridges to
    // TypeUnknown (parser call.go:240-241), and R7's resolver
    // bridges further to ResolvedUnknown (spec §4.2.1).
    {
        Name:    "test.number",
        Params:  nil,
        Results: []procsig.Result{
            {Name: "value", Token: procsig.TokenNumber, Nullable: true},
        },
    },
    // Nullable STRING result with the trailing `?` OFF (non-nullable) —
    // for the non-nullable-column typing fixture.
    {
        Name:    "test.constants",
        Params:  nil,
        Results: []procsig.Result{
            {Name: "constant", Token: procsig.TokenString, Nullable: false},
        },
    },
}
```

**Design note — mixed nullable/non-nullable results.** R7 tests
must exercise both `Nullable: true` (the corpus norm) and
`Nullable: false` cases so `Column.Nullable` is not always `true`.
The `test.constants` signature is authored specifically for this.

**Design note — registry construction is asserted at suite
setup.** `resolver_test.go` gains a package-level `regR7` variable
and a `TestMain` or `init()` that calls
`procsig.NewRegistry(signaturesR7)` — the same posture as
`parser_test.go:2517-2525`'s `newParserFor` helper. Registry-level
failures fail the suite, not individual fixtures.

**Harness wire.** `loadQuery` at `resolver_test.go:106-112`
revises to:

```go
func (s *ResolverSuite) loadQuery(path string) query.Query {
    src, err := os.ReadFile(path)
    s.Require().NoError(err)
    q, err := cypher.New(cypher.WithRegistry(regR7)).Parse(bytes.NewReader(src))
    s.Require().NoError(err)
    return q
}
```

**Backwards-compatibility of the harness change.** All 102 R0–R6
valid fixtures parse cleanly with an empty registry today. A
non-empty registry does not change parse behaviour for queries
without CALL — the parser's registry is consulted only inside
`collectCall` (verified against `internal/query/cypher/call.go:41`).
Consequence: threading `regR7` through `loadQuery` does not alter
any R0–R6 fixture's parse output — the R7 harness change is
byte-identity-safe.

**R7 sentinel-reachability harness discipline.** R7 fixtures
targeting parser-time sentinels (`ErrUnknownProcedure`,
`ErrProcedureArity`, `ErrVariableKindConflict`) MUST NOT appear
under `test/data/resolver/invalid/` — those sentinels are the
parser's, not the resolver's, and the resolver's
`TestSentinelReachability` sweep only checks the resolver's
`allSentinels`. Parser-time fixtures live in `cypher`-package
tests (already there: `parser_test.go` mustReject entries at line
2536-2560+).

### 6.4 R7 valid fixtures

Each fixture keyed to an R7 arm. Each has a paired
`.validated.golden.json`, generated with `-update`. Each entry
below lists the query, the schema (defaults to `social_r7.gql`
for R7 fixtures), the discriminating breakage, and the expected
column count in the golden.

**Standalone CALL, no YIELD, single result column (§4.5):**

- `call_standalone_yield_star_integer.cypher` —
  `CALL test.count`.
  Schema: `social_r7.gql`. Discriminates: parser Stage 14 §4.3
  synthesises `Part.ReturnsAll = true` + `Part.Returns` with a
  RefProjection for `n`. R7 Phase A1 admits CallBinding{"n", ...};
  R7 §4.3 buildScopeOrder widening includes "n" in scopeOrder;
  R7 §4.7 virtualProjection widening returns a RefProjection with
  TypeInt; R7 §4.2.1 refProjectionType returns
  `ResolvedProperty{INT, true}`.
  Golden: 1 column `{Name: "n", Type: {kind: "property", type:
  "INT", nullable: true}, GroupingKey: false}`, statement "read",
  distinct false, parameters [].

- `call_standalone_yield_star_string_nullable.cypher` —
  `CALL test.labels`.
  Schema: `social_r7.gql`. Discriminates: single STRING? column.
  Golden: 1 column `{Name: "label", Type: {kind: "property", type:
  "STRING", nullable: true}, GroupingKey: false}`, statement "read".

- `call_standalone_yield_star_float.cypher` —
  `CALL test.temperature`.
  Schema: `social_r7.gql`. Discriminates: single FLOAT? column.
  Golden: 1 column `{Name: "celsius", Type: {kind: "property", type:
  "FLOAT", nullable: true}, GroupingKey: false}`, statement "read".

- `call_standalone_yield_star_number.cypher` —
  `CALL test.number`.
  Schema: `social_r7.gql`. Discriminates: NUMBER bridges to
  ResolvedUnknown (§4.2.1).
  Golden: 1 column `{Name: "value", Type: {kind: "unknown"},
  GroupingKey: false}`, statement "read".

- `call_standalone_yield_star_non_nullable.cypher` —
  `CALL test.constants`.
  Schema: `social_r7.gql`. Discriminates: signature Nullable=false;
  §4.1.4 seeds nullableBinding["constant"]=false; §4.2.1 returns
  `ResolvedProperty{STRING, false}`.
  Golden: 1 column `{Name: "constant", Type: {kind: "property", type:
  "STRING", nullable: false}, GroupingKey: false}`, statement "read".

**In-query CALL YIELD, RETURN (§4.5):**

- `call_in_query_yield_return.cypher` —
  `CALL test.labels() YIELD label\nRETURN label`.
  Schema: `social_r7.gql`. Discriminates: parser emits explicit
  Returns (not synthesised); R7 §4.2.1 witnesses `label` via
  `callTypes` arm.
  Golden: 1 column `{Name: "label", Type: {kind: "property", type:
  "STRING", nullable: true}, GroupingKey: false}`, statement "read".

- `call_in_query_yield_as.cypher` —
  `CALL test.labels() YIELD label AS lbl\nRETURN lbl`.
  Schema: `social_r7.gql`. Discriminates: YIELD AS aliases the
  variable; parser Stage 14 §4.2 §6 mints CallBinding{Variable="lbl",
  SourceField="label"}. R7 §4.1 admits under "lbl"; §4.2.1
  witnesses `lbl` via callTypes["lbl"].
  Golden: 1 column `{Name: "lbl", Type: {kind: "property", type:
  "STRING", nullable: true}, GroupingKey: false}`, statement "read".

- `call_yield_two_columns.cypher` —
  `CALL test.my.proc('Stefan', 1) YIELD city, country_code\nRETURN city, country_code`.
  Schema: `social_r7.gql`. Discriminates: two CallBindings for one
  CALL; per-column bridging (STRING and INT); parameters unified.
  Note: `$` parameters are not used here (positional literals in
  args); parser Stage 14 §4.5 records refs and params from arg
  mining. This fixture pins the parser's argument-mining discard
  (no ExprInCallArg) — the resolver has ZERO argument-side visibility
  to test, so no assertion beyond the two-column YIELD.
  Golden: 2 columns
  `[{Name:"city", Type:{kind:"property", type:"STRING", nullable: true}},
  {Name:"country_code", Type:{kind:"property", type:"INT", nullable: true}}]`,
  statement "read".

**Standalone CALL, YIELD items (§4.5):**

- `call_standalone_yield_items.cypher` —
  `CALL test.my.proc('Stefan', 1) YIELD city, country_code`.
  Schema: `social_r7.gql`. Discriminates: standalone CALL WITH
  explicit YIELD — parser sets `Part.ReturnsAll = false` +
  `Part.Returns` from explicit yield items (per parser_test.go
  Call5[3] pin).
  Golden: 2 columns per Call5[3] pin.

- `call_standalone_yield_items_reordered.cypher` —
  `CALL test.my.proc('Stefan', 1) YIELD country_code, city`.
  Schema: `social_r7.gql`. Discriminates: YIELD order controls
  Column order (parser Stage 14 §4.2.6 by-name lookup).
  Golden: 2 columns in reversed order.

**CALL YIELD across WITH (§4.6):**

- `call_yield_carry_forward.cypher` —
  `CALL test.labels() YIELD label\nWITH label\nRETURN label`.
  Schema: `social_r7.gql`. Discriminates: Part 0 exports
  callTypes["label"]; Part 1 seeds from carry.exportedCallTypes;
  R7 §4.2.1 witnesses in Part 1 identically.
  Golden: 1 column `{Name: "label", Type: {kind: "property", type:
  "STRING", nullable: true}, GroupingKey: false}`.

- `call_yield_carry_with_alias.cypher` —
  `CALL test.labels() YIELD label\nWITH label AS lbl\nRETURN lbl`.
  Schema: `social_r7.gql`. Discriminates: aliased carry — R5's
  `exportedResolvedTypes["lbl"] = ResolvedProperty{STRING, true}`
  path handles this. R7 also exports
  `exportedCallTypes["lbl"] = callBindingSlot{...}` for identity
  metadata (§4.6).
  Golden: 1 column `{Name: "lbl", Type: {kind: "property", type:
  "STRING", nullable: true}, GroupingKey: false}`.

**CALL YIELD mixed with MATCH:**

- `call_yield_mixed_return.cypher` —
  `MATCH (p:Person) CALL test.labels() YIELD label\nRETURN p.name, label`.
  Schema: `social_r7.gql`. Discriminates: entity binding + CALL YIELD
  in one Part; both project in RETURN.
  Golden: 2 columns
  `[{Name: "p.name", Type: {kind: "property", type: "STRING", nullable: false}, GroupingKey: false},
  {Name: "label", Type: {kind: "property", type: "STRING", nullable: true}, GroupingKey: false}]`.

- `call_yield_before_match.cypher` —
  `CALL test.labels() YIELD label\nMATCH (p:Person) RETURN label, p.name`.
  Schema: `social_r7.gql`. Discriminates: CALL YIELD in Part 0
  (single-Part with subsequent MATCH is not multi-part per parser —
  MATCH-after-CALL fires within the same Part; verify via parser
  test).
  Golden: 2 columns
  `[{Name: "label", Type: {kind: "property", type: "STRING", nullable: true}, GroupingKey: false},
  {Name: "p.name", Type: {kind: "property", type: "STRING", nullable: false}, GroupingKey: false}]`.

**Ordering summary:** 14 valid fixtures at R7 (five standalone YIELD
* / no-yield, three in-query YIELD, two YIELD items, two carry
variants, two mixed). Each exercises one distinct arm of §4.

### 6.5 R7 invalid fixtures — updated `invalidFixtures` map

Each fixture keyed to a sentinel it must pin. §5 pins the three R7
fail-sites: property lookup on CALL YIELD scalar
(`ErrUnknownProperty`), cross-clause CALL-YIELD-scalar vs
NodeBinding re-bind (`ErrPartBindingTypeConflict`), and
cross-clause CALL-YIELD-scalar vs EdgeBinding re-bind
(`ErrPartBindingTypeConflict`).

**R7 additions:**

- `call_yield_property_lookup.cypher` —
  `CALL test.my.proc('Stefan', 1) YIELD city\nRETURN city.length`.
  Schema: `social_r7.gql`. Fires: `ErrUnknownProperty:
  city.length (CALL YIELD variable "city" is a scalar)` at
  `refProjectionType` §4.2.2.
  Pins: `ErrUnknownProperty`.

- `part_binding_type_conflict_call_vs_node.cypher` —
  `CALL test.labels() YIELD label\nWITH label\nMATCH (label:Person) RETURN label`.
  Schema: `social_r7.gql`. Fires:
  `ErrPartBindingTypeConflict` at Part 1's NodeBinding admission
  arm — carry seeds callTypes["label"] as STRING scalar; local
  MATCH binds NodeBinding{"label", ["Person"]}; the resolver
  detects the cross-clause scalar-vs-entity type shape conflict
  per §4.1.2.1.
  Pins: `ErrPartBindingTypeConflict`. Wrapped message:
  `variable "label" carried as CALL YIELD scalar, re-bound as
  Person`.

  **Parser reachability — VERIFIED reachable at spec-review.** The
  parser's `build.go:81-85` entity-sweep populates `scope` from
  `rp.bindings` WITHOUT a collision check against `imported`; the
  `imported[v]` collision at `build.go:148-150` fires only on the
  CallBinding-import direction (imported name → local CallBinding),
  not the reciprocal (imported CALL YIELD → local NodeBinding).
  Consequence: this fixture PARSES cleanly and reaches the
  resolver; without R7's §4.1.2.1 check, the resolver would
  SILENTLY ADMIT with `n` typed as a NodeBinding and the CALL
  YIELD scalar identity silently lost. §4.1.2 details the
  reachability analysis with first-party citations.

- `part_binding_type_conflict_call_vs_edge.cypher` —
  `CALL test.labels() YIELD label\nWITH label\nMATCH ()-[label:KNOWS]->() RETURN label`.
  Schema: `social_r7.gql`. Fires:
  `ErrPartBindingTypeConflict` at Part 1's EdgeBinding admission
  arm — carry seeds callTypes["label"] as STRING scalar; local
  MATCH binds EdgeBinding{"label", ["KNOWS"]}; the resolver
  detects the cross-clause scalar-vs-entity type shape conflict
  per §4.1.2.2.
  Pins: `ErrPartBindingTypeConflict`. Wrapped message:
  `variable "label" carried as CALL YIELD scalar, re-bound as
  edge with labels KNOWS`.

  Parser reachability: identical to the node case (verified via
  same `build.go:81-85` sweep — the entity sweep runs for edges
  too by shared code path). Confirm by parser-side dry run before
  writing the golden.

**Updated `invalidFixtures` map** (R6 base + R7 additions):

```go
var invalidFixtures = map[string]error{
    // R0-R6 entries UNCHANGED (see resolver_test.go:27-75).
    // ... [omitted for brevity; every R6 entry is preserved verbatim] ...

    // R7 additions:
    "call_yield_property_lookup.cypher":                   ErrUnknownProperty,
    "part_binding_type_conflict_call_vs_node.cypher":      ErrPartBindingTypeConflict,
    "part_binding_type_conflict_call_vs_edge.cypher":      ErrPartBindingTypeConflict,
}
```

**Ordering summary:** 3 invalid fixtures at R7 (one property-on-
CALL-YIELD-scalar; two CALL-YIELD-scalar-vs-entity re-bind — one
node-side, one edge-side). All three parse cleanly and are
resolver-reachable per §4.1.2's first-party build.go analysis.
R7 remains a positive-output stage; the three fail-sites are the
complete resolver-side rejection surface for CALL.

**Sentinel reachability sweep passes**: every R6 sentinel still has
a fixture; the R7 additions target `ErrUnknownProperty` (already
covered — R7 widens the message set with the CALL-YIELD-scalar
arm) and `ErrPartBindingTypeConflict` (already covered by R5's
`part_binding_type_conflict.cypher` — R7 adds two additional
message-shape angles per §5.3, without adding a sentinel).

### 6.6 Determinism check — R7 additions

R0–R6 §6.5 determinism rules (map iteration determinism, fixture
ordering) stand at R7. R7's `callTypes` map is walked only by
carry-seed and by `refProjectionType` lookup — both are single-key
operations, never iteration.

`buildScopeOrder`'s `part.Bindings` walk (widened at §4.3 to include
CallBindings) is over a slice, in slice order — parser-deterministic.
`materialiseReturns`'s ReturnsAll expansion is over `scopeOrder`
(built by `buildScopeOrder`), also parser-deterministic. R7 adds no
new map iteration.

### 6.7 Non-obvious harness invariants — R7 additions

R7 preserves R0–R6 harness invariants and adds one:

- **Registry threading is byte-identity-safe for the R0–R6 corpus.**
  Every R0–R6 fixture parses identically with or without a
  registry (no fixture contains a CALL). Verified against
  `internal/query/cypher/call.go:41` — the registry is consulted
  only inside `collectCall`, and `collectCall` is called only when
  the parser sees a CALL. R7's harness change is transparent to the
  R0–R6 corpus.

R0–R6 harness invariants (goldens are byte-identical after
regenerating with `-update` on the same commit; the mapping and
`invalidFixtures` map are total against `*.cypher`) continue to
hold at R7.

---

## 7. R7 capability scope — what resolves

**In scope:** a single Cypher query the parser accepts, whose parsed
`query.Query` shape is:

- The R6 in-scope predicate, PLUS
- `Part.Bindings` may contain one or more `CallBinding` entries
  alongside NodeBinding and EdgeBinding. Each CallBinding contributes
  a scalar column to any bare-Ref RETURN (§4.2.1).
- `Part.ReturnsAll = true` may fire for a Part containing a
  CallBinding — parser Stage 14 §4.3 populates `Part.Returns` for
  standalone CALL; R5's `ReturnsAll` path re-synthesises via
  `virtualProjection` (§4.7), and R7's `virtualProjection` widening
  produces the correct output.
- `StatementKind` is `StatementRead` for CALL-only queries; a mixed
  query (CALL + write clause) inherits R6's `StatementWrite` flip
  from Stage 12 §3.1.

**Out of scope, routed to the appropriate sentinel:**

R6's out-of-scope table survives with revisions:

| Construct | Sentinel | R-stage owner |
|---|---|---|
| Untyped edge (`len(Labels()) == 0`) — read or write | `ErrOutOfR0Scope` | R-later |
| Path binding | `ErrOutOfR0Scope` | R-later |
| Unwind binding | `ErrOutOfR0Scope` | R-later |
| Call binding | ~~`ErrOutOfR0Scope`~~ ADMITTED at R7 (§4.1) | (retired) |
| `ExprProjection` typed `TypeList{TypeNode\|TypeEdge}` | `ErrOutOfR0Scope` | R-later |
| Property projection on a variable-length edge binding | `ErrOutOfR0Scope` | R-later |
| CALL / YIELD | ~~`ErrOutOfR0Scope`~~ ADMITTED at R7 (§4.1) | (retired) |
| Property lookup on a CALL YIELD scalar | `ErrUnknownProperty` | **R7 (this stage)** |
| Every candidate `(label, orientation)` misses the schema | `ErrUnknownEdge` | (unchanged) |
| Single-type undirected edge whose two orientations both match | `ErrAmbiguousEdgeOrientation` | (unchanged) |
| Property lookup on a multi-candidate edge; property missing on some union member | `ErrUnknownProperty` | (unchanged) |
| Property lookup on a multi-candidate edge; property type/nullability differs across union members | `ErrUnknownProperty` | (unchanged) |
| Labelled node with no matching schema NodeType — read or write | `ErrUnknownLabel` | (unchanged) |
| Unlabelled node with an empty candidate set from R3-widened touching edges | `ErrUnknownLabel` | (unchanged) |
| Unlabelled node with a multi-candidate set that survives Phase B fixed-point | `ErrAmbiguousBinding` | (unchanged) |
| Parameter Uses that do not unify | `ErrParameterTypeConflict` | (unchanged) |
| UNION branches disagree on column count, names, types, or nullability | `ErrUnionColumnMismatch` | (unchanged) |
| Part K > 0 re-declares a carried variable with a conflicting labelled binding | `ErrPartBindingTypeConflict` | (unchanged; R5) |
| Part K CALL YIELD carried through WITH; Part K+1 MATCH re-binds the same name as a labelled node | `ErrPartBindingTypeConflict` | **R7 (this stage; §4.1.2.1; message-set widening — §5.3)** |
| Part K CALL YIELD carried through WITH; Part K+1 MATCH re-binds the same name as an edge | `ErrPartBindingTypeConflict` | **R7 (this stage; §4.1.2.2; message-set widening — §5.3)** |
| SET / REMOVE property on schema-unknown property | `ErrUnknownProperty` | (unchanged) |
| SET / REMOVE labels with schema-undeclared label | `ErrUnknownLabel` | (unchanged) |
| CREATE / MERGE with schema-unknown label | `ErrUnknownLabel` | (unchanged) |
| CREATE / MERGE with schema-unknown edge triple | `ErrUnknownEdge` | (unchanged) |
| DELETE bare-prop on schema-unknown property | `ErrUnknownProperty` | (unchanged) |
| SET / REMOVE / DELETE target resolves to a projection alias (non-entity binding) | `ErrInvalidEffectTarget` | (unchanged) |
| SET / REMOVE labels target is an edge binding | `ErrInvalidEffectTarget` | (unchanged) |
| SET / REMOVE property target is a var-length edge binding | `ErrInvalidEffectTarget` | (unchanged) |
| Value-target type-agreement in `SET n.p = value` | silently admitted | R-later (design axis; unchanged from R6) |
| Runtime SET / DELETE on NULL target (row-drop semantics) | silently admitted | bucket 3 (unchanged) |
| DETACH DELETE cascade semantics vs plain DELETE | silently admitted | bucket 3 (unchanged) |
| Same-Part regime (b) nullability under-demote | ~~silently under-demoted~~ **closed** (5xg, 2026-07-10) | ~~gqlc-5xg (unchanged)~~ **closed** by 5xg unfreeze + widening (`docs/specs/unfreeze-5xg-required-bare-ref.md`); edge-side non-bare missing-witness residual filed as gqlc-0kq |
| OPTIONAL-clause-sibling nullability under-demote | ~~silently under-demoted~~ **closed** (ay9, 2026-07-10) | ~~gqlc-ay9~~ **closed** by ay9 unfreeze + widening (`docs/specs/unfreeze-ay9-optional-group.md`); residual cross-Part carry gap filed as gqlc-984 |
| `ExprProjection` residual grouping-key discrimination | silently under-grouped | gqlc-hk0 / Shape B (unchanged) |
| Cross-Part parameter Use attribution gap | silently false-admitted | gqlc-fvo (unchanged) |
| CALL argument-vs-parameter type check (incl. NUMBER assignable-from) | ~~silently admitted~~ **checked** (0ig, 2026-07-07) | ~~frozen-model deficiency filed at R7 close-out~~ CLOSED (§7.1.1; `docs/specs/unfreeze-0ig-call-args.md`) |
| Parser trusts CALL procedure lookup; resolver does not re-check | silently admitted | (trust posture; §4.4 — same family as R6 Refs referential integrity) |

**Silently accepted (not routed anywhere):**

R0–R6's silently-accepted set stands unchanged. ~~R7 adds:~~ **Both
R7 additions below were closed by the 0ig unfreeze cycle
(2026-07-07; §7.1.1 banner, `docs/specs/unfreeze-0ig-call-args.md`):**
- ~~**CALL argument type-agreement against signature params** (§7.1.1
  frozen-model deficiency — resolver has no attribution to run the
  check).~~ **checked** (0ig, 2026-07-07): the resolver's Phase A1
  CallBinding arm walks `CallBinding.Args()` per position and fires
  `ErrCallArgAssignability` on mismatch.
- ~~**NUMBER assignable-from at argument sites** — no resolver
  application site because CALL-arg attribution is dropped (§7.1.1;
  §4.8).~~ **delivered** (0ig, 2026-07-07): the assignability lattice
  (`unfreeze-0ig-call-args.md §8.2`) applies NUMBER
  assignable-from-INTEGER-or-FLOAT at the argument site.

**Recorded ADR 0009 cross-check.** ADR 0009 R7: "CALL — YIELD column
typing from the procsig.Registry; argument assignability including
NUMBER assignable-from INTEGER-or-FLOAT (ADR 0007's Stage-14 note);
unknown procedure is a generation-time error by design." R7 as this
spec scopes it:

- **YIELD column typing from `procsig.Registry`**: delivered. §4.2
  witnesses call-yielded variables to `ResolvedProperty` (or
  `ResolvedUnknown` for NUMBER). The registry threading through the
  parser produces the CallBinding's bridged type; the resolver reads
  that type off `CallBinding.ResultType()` and produces the resolved
  column. Signature identity carries via `CallBinding.Procedure()`
  and `CallBinding.SourceField()` on `query.Query` for codegen.
- **Argument assignability including NUMBER assignable-from
  INTEGER-or-FLOAT**: ~~DEFERRED~~ **DELIVERED** by the 0ig cycle
  (`docs/specs/unfreeze-0ig-call-args.md`; ADR 0008 amendment
  2026-07-07). The wire now carries per-position `CallArg` records
  on `CallBinding.Args()`; the resolver's Phase A1 CallBinding arm
  applies the assignability lattice (spec §8.2) and fails with
  `ErrCallArgAssignability` on mismatch. See §7.1.1 for the historical
  context of the R7-shipped deferral.
- **Unknown procedure = generation-time error by design**: delivered
  by the parser (`cypher.ErrUnknownProcedure`). The resolver does
  not re-check per §4.4 (trust posture — same family as R6 Refs
  referential-integrity trust). This is a legitimate delivery of the
  ADR line, not a scope trim.

### 7.1 Under-approximation and R7-specific design deferrals

Following the R6 §7.1 template — R7 discovers ONE new frozen-model
deficiency (§7.1.1) and inherits every R6 open axis unchanged
(§7.1.2).

#### 7.1.1 CALL-arg attribution — a frozen-model deficiency ~~open~~ CLOSED (2026-07-07)

> **Cycle 3 errata (2026-07-07, gqlc-0ig unfreeze cycle):** the
> frozen-model deficiency this section records has been **closed** by
> the ADR 0008 amendment adopted 2026-07-07 (`docs/adr/0008-query-
> model-freeze-resolver-api.md` top). The wire now carries per-
> position `CallArg` records on `CallBinding.Args()`; the resolver's
> Phase A1 CallBinding arm walks `Args()` against the matched
> `procsig.Registry.Lookup(procedure).Params[i].Token` and fails with
> the new `ErrCallArgAssignability` sentinel on mismatch under the
> ADR 0007 Stage-14 assignability lattice (`docs/specs/unfreeze-0ig-
> call-args.md §8.2`: NUMBER accepts INTEGER-or-FLOAT; FLOAT accepts
> INTEGER per TCK Call3 [5]; STRING / INTEGER strict; TypeUnknown
> and TypeNull wildcards — bare `null` literals mine to `TypeNull`,
> a distinct sum member from `TypeUnknown`, per §8.2's E1 row). The
> prose below is preserved as-of-R7-shipping for
> historical grounding; the current model surface is the amendment's.
> Escape-hatch entry in the "Known deferred additions" list is now
> `CallBinding.Args` axis (adopted).

**The gap.** The parser's Stage 14 `collectCall` at
`internal/query/cypher/call.go:47-66` mines CALL arguments and
records:

1. **Refs** — appended to `l.curPart.refs` for referential integrity
   validation.
2. **Parameter uses** — appended via `addParameterUse(n, p,
   query.NewExprUse(t, query.ExprInProjection))`.

The parameter uses are recorded as `ExprInProjection` — a stand-in
position. There is **no `ExprInCallArg` position on the wire**.
Consequence: the resolver's `unifyParameterUsesAcrossScopes`
(`resolve.go:611-653`) cannot distinguish a Use from a CALL arg
from a Use from a RETURN projection. Even if the CALL param's
declared token is NUMBER (or INTEGER, or FLOAT, or STRING), the
resolver has no way to link the Use back to the param position.

**The ADR line asks for**: argument assignability including NUMBER
assignable-from INTEGER-or-FLOAT. R7 as scoped delivers zero of this
because the wire does not carry the attribution the check would
consult.

**What a widening would look like.**

Two minimal frozen-model changes would enable the resolver-side
check:

1. **Add `ExprInCallArg` to the `ExprPosition` sum.** Parser's
   `collectCall` would emit `ExprUse{t, ExprInCallArg}` per arg
   parameter, replacing the current `ExprInProjection` stand-in.
2. **Add a `Calls` axis on `query.Part`** — one per CALL clause —
   carrying (procedure name, positional param → arg-source ref/param
   mapping). Then `witnessAcrossScopes` at R7 (or a follow-up
   stage) could hunt for each Use's Part-relative CALL position,
   look up the sig param's declared token via
   `procsig.Registry.Lookup(procedureName).Params[position].Token`,
   and either widen the witness (NUMBER accepts INTEGER-or-FLOAT)
   or narrow it (STRING param requires TypeString-only Uses).

Either widening lands under a new frozen-model unfreeze bead
(same family as `gqlc-fvo` — the cross-Part parameter Use
attribution gap, which is the parent of THIS gap: `gqlc-fvo` says
"the wire loses which Part a Use came from"; this gap says "the
wire loses which CLAUSE inside a Part a Use came from"). The R7
close-out files an owner-decision bead reference documenting the
gap; the bead is filed at close-out per the R5-established
convention (`gqlc-lta`, `gqlc-fvo`, etc.). **Do NOT gate the R7
spec on the bead** — the ADR 0009 R7 delivery is honestly delivered
without the widening (YIELD column typing works standalone; the
gap is on the check axis, not the typing axis).

**Consequence for R7 fixtures.** No R7 fixture exercises argument
assignability. The `call_yield_two_columns.cypher` fixture uses
literal argument values (`'Stefan', 1`) that would be checked at
runtime (or, with the widening, at generation time). R7's fixture
set is deliberately silent on argument-side; the ADR delivery is
the YIELD column typing axis.

**Recommendation.** File the owner-decision bead at close-out
under the `gqlc-fvo` pattern (owner unfreeze decision pending).
Reference: bead filed at close-out (see §9). R7 code cycle
proceeds without the widening.

#### 7.1.2 R7 inherits R6 open deferrals unchanged

The R6-discovered design axes (§7.1.1 value-target assignability;
§7.1.2 Effects-on-wire) persist at R7 unchanged. R5-inherited gaps
(`gqlc-hk0` ExprProjection residual discrimination, `gqlc-fvo`
cross-Part Use attribution ~~— of which R7's CALL-arg gap is a
child~~ — the CALL-arg child gap was closed by 0ig, 2026-07-07; see
§7.1.1) persist at R7 unchanged. R4-inherited gaps ~~(`gqlc-ay9` Class A,
`gqlc-5xg` Class B nullability) persist at R7 unchanged~~ — Class A
(`gqlc-ay9`) was closed by the ay9 unfreeze + widening on 2026-07-10
(PRs #127/#128/#129, `docs/specs/unfreeze-ay9-optional-group.md`;
residual cross-Part carry gap filed as gqlc-984); ~~Class B
(`gqlc-5xg`) persists at R7 unchanged.~~ Class B (`gqlc-5xg`) was
closed by the 5xg unfreeze + widening on 2026-07-10 (PRs
#132/#133/#134, `docs/specs/unfreeze-5xg-required-bare-ref.md`;
edge-side non-bare missing-witness residual filed as gqlc-0kq); R7
inherits the closure unchanged.

#### 7.1.3 Freeze-not-a-wall status

R7 discovers ONE new frozen-model deficiency (§7.1.1 — CALL-arg
attribution). Owner-decision bead filed at close-out (that bead,
`gqlc-0ig`, has since closed the deficiency — 2026-07-07). R7 does not
delay the spec on it; the CALL-YIELD-typing arm delivers standalone.

#### 7.1.4 Summary of R7 deferrals

- ~~**CALL-arg attribution** — deferred to a future frozen-model
  widening (owner-decision bead filed at close-out). NUMBER
  assignable-from and argument-vs-param type agreement are both
  downstream of this gap. Not a code-side deferral. §7.1.1.~~
  **CALL-arg attribution — closed by 0ig (2026-07-07).** The
  owner-decision bead delivered the widening; argument-vs-param
  type agreement and NUMBER assignable-from now run in the
  resolver's Phase A1 CallBinding arm. §7.1.1 banner.
- **R6 open axes carry unchanged** — value-target assignability
  (§7.1.1 R6), Effects-on-wire (§7.1.2 R6). §7.1.2.
- **R5 / R4 open axes carry unchanged** — `gqlc-fvo`, `gqlc-hk0`,
  ~~`gqlc-ay9`, `gqlc-5xg`~~. §7.1.2. [closed 2026-07-10: `gqlc-ay9`
  landed via PRs #127/#128/#129
  (`docs/specs/unfreeze-ay9-optional-group.md`; residual gqlc-984),
  and `gqlc-5xg` landed via PRs #132/#133/#134
  (`docs/specs/unfreeze-5xg-required-bare-ref.md`; residual gqlc-0kq);
  R7 inherits both closures unchanged.]

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
  `internal/query/query.go:81-123`.
- **`Binding` interface + `BindingKind`** —
  `internal/query/query.go:233-282`. `BindingCall` at line 255-262.
- **`CallBinding` struct + `NewCallBinding` constructor** —
  `internal/query/query.go:830-877`. Field-by-field pins:
  - `variable` at :844 — always non-empty.
  - `procedure` at :845 — fully-qualified, always non-empty.
  - `sourceField` at :846 — signature-declared result column, always
    non-empty.
  - `resultType` at :847 — bridged Stage-6 type; TypeUnknown for
    NUMBER.
  - `nullable` at :848 — signature's trailing `?`.
- **`CallBinding.Variable() / .Procedure() / .SourceField() /
  .ResultType() / .Nullable()`** —
  `internal/query/query.go:882-907`.
- **`CallBinding.Kind() == BindingCall`** —
  `internal/query/query.go:903`.
- **`CallBinding.MarshalJSON` — the wire discriminator** —
  `internal/query/query.go:916-932`.
- **CALL is a Read clause pin** — `internal/query/query.go:838-842`
  ("CallBinding … participates in the Stage-6 refType classifier
  the same way UnwindBinding does — a bare RETURN on a call-yielded
  variable types as ResultType()").
- **`ExprInProjection` position (the stand-in for CALL args)** —
  `internal/query/query.go:1353-1355`.
- **`procsig` package doc** — `internal/procsig/procsig.go:1-13`.
- **`procsig.TypeToken` closed sum with `TokenNumber` signature-only
  marker** — `internal/procsig/procsig.go:29-63`.
- **`procsig.Signature`, `Param`, `Result`, `Registry`, `NewRegistry`,
  `Registry.Lookup`** — `internal/procsig/procsig.go:68-149`.
- **Parser's CALL entry — `collectCall`** —
  `internal/query/cypher/call.go:29-97`.
- **Parser fires `ErrUnknownProcedure` at procedure lookup miss** —
  `internal/query/cypher/call.go:41-45`.
- **Parser mines CALL arg parameter uses as
  `ExprUse{t, ExprInProjection}` — the wire attribution loss** —
  `internal/query/cypher/call.go:54-66` (§4.8, §7.1.1).
- **Parser fires `ErrUnknownProcedure` at YIELD result field miss** —
  `internal/query/cypher/call.go:133-138`.
- **Parser fires `ErrProcedureArity` at explicit-invocation arity
  mismatch** — `internal/query/cypher/call.go:71-75`.
- **Parser fires `ErrVariableKindConflict` at YIELD intra-collision** —
  `internal/query/cypher/call.go:139-142`.
- **`typeForToken(TokenNumber) → query.TypeUnknown{}` bridge** —
  `internal/query/cypher/call.go:234-247`.
- **Parser's `buildPart` §4.7 CallBinding integration** —
  `internal/query/cypher/build.go:127-198`.
- **Parser's `buildPart` `writeSeen` non-flip for CALL** — parser
  Stage 14 §3.1 (no CALL in the write-clause set — CALL is Read).
- **`refType(cb)` in parser's typing.go — CallBinding participates
  in refType** — `internal/query/cypher/expr.go:309-317`.
- **ADR 0009 R7 line verbatim** —
  `docs/adr/0009-resolver-test-first-staged-build.md:136-139`.
- **ADR 0007 Stage-14 note on NUMBER assignable-from** —
  `docs/adr/0007-pre-freeze-scope-full-opencypher-surface.md:166-181`.
- **ADR 0008 pinned resolver API** —
  `docs/adr/0008-query-model-freeze-resolver-api.md:117-135`.
- **Stage 14 §4.5 — arg-type check bucket-3 skiplist** —
  `docs/specs/cypher-query-parser-stage-14.md:971-994`. Direct
  quote: "the arg-vs-param type dance itself is not modelled".
- **Resolver — `Resolver` struct, `WithRegistry`, `New`,
  `Resolve`** — `internal/resolver/resolver.go:22-58`.
- **Resolver — `resolve` top-level** —
  `internal/resolver/resolve.go:15-54`.
- **Resolver — `resolveBranch` (pinned R5 §2.2 signature; `_ = r`
  discard at :110-112)** — `internal/resolver/resolve.go:103-142`.
- **Resolver — `resolvePart` kernel body** —
  `internal/resolver/resolve.go:152-320`.
- **R6 default-reject arm for BindingCall — the R7 target** —
  `internal/resolver/resolve.go:234-236`.
- **`refProjectionType` — the arm R7 widens** —
  `internal/resolver/resolve.go:1027-1084`.
- **`projectionType` — signature widens by one parameter** —
  `internal/resolver/resolve.go:1010-1025`.
- **`bindingVariable` — the switch R7 widens** —
  `internal/resolver/resolve.go:1231-1240`.
- **`buildScopeOrder` — the walk R7 widens** —
  `internal/resolver/resolve.go:370-404`.
- **`virtualProjection` — the ReturnsAll synthesis R7 widens** —
  `internal/resolver/resolve.go:347-368`.
- **`exportScope` — the R7 `exportedCallTypes` lane** —
  `internal/resolver/resolve.go:406-484`.
- **`branchState` — the R7 `exportedCallTypes` field** —
  `internal/resolver/resolve.go:82-91`.
- **`errors.go` — the R7 message-set retirement site** —
  `internal/resolver/errors.go:24-31`.
- **`allSentinels` closed set — unchanged at R7** —
  `internal/resolver/errors.go:97-108`.
- **Resolver harness — `loadQuery`'s registry-less parse (the R7
  target)** — `internal/resolver/resolver_test.go:106-112`.
- **`invalidFixtures` map — the R7 update site** —
  `internal/resolver/resolver_test.go:27-75`.
- **`TestSentinelReachability` bidirectional discipline** —
  `internal/resolver/resolver_test.go:185-202`.

**102 valid stems and 45 invalid stems at branch base** — verified
via `ls test/data/resolver/{valid,invalid}/*.cypher | sed | sort |
wc -l` at commit `0900a8e`.

**Test-side registry fixture in parser tests (the model for R7's
`signaturesR7` slice inside `resolver_test.go`)** —
`internal/query/cypher/parser_test.go:2517-2525` (`newParserFor`).

---

## 9. Definition of done for R7 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is
out of scope of this document. The spec is done when:

1. This file exists at `docs/specs/resolver-stage-r7.md`, committed
   on branch `resolver-r7-spec`.
2. §3 records the (unchanged) `ValidatedQuery` shape, the CALL YIELD
   `Column.Type` type-mapping table (§3.1), the CALL-is-Read
   `Statement` pin (§3.2), and the byte-identical claim over the
   102 R0–R6 valid goldens (§3.3).
3. §4 gives the algorithm for R7: Phase A1's `CallBinding` admission
   arm (§4.1), `refProjectionType`'s widening for CALL YIELD Refs
   (§4.2), `buildScopeOrder`'s widening for CALL YIELD variables
   (§4.3), the parser-authoritative unknown-procedure trust posture
   (§4.4), the standalone-CALL synthetic Returns handling (§4.5),
   `exportScope`'s callTypes carry-forward (§4.6), and the
   argument-parameter Use posture (§4.8).
4. §5 records the zero-sentinel-addition posture, the one
   `ErrOutOfR0Scope` message-set retirement (§5.2), and the TWO
   R6-sentinel message-set widenings — `ErrUnknownProperty`
   (property-on-CALL-YIELD-scalar) and `ErrPartBindingTypeConflict`
   (scalar-vs-entity cross-clause re-bind, §5.3). The `allSentinels`
   closed set is unchanged from R6 (§5.4).
5. §6 designs the fixture set: the R7 valid schema `social_r7.gql`
   (§6.2), the R7 registry fixture — `signaturesR7` slice inside
   `internal/resolver/resolver_test.go` (§6.3), the R7 valid
   fixture list (~14 fixtures), the R7 invalid fixture list (three
   additions: one property-on-scalar, one call-vs-node, one
   call-vs-edge), the revised `invalidFixtures` map (§6.5).
6. §7 states the R7 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel or under-demote posture
   for each construct. §7.1 records the ONE new frozen-model
   deficiency (CALL-arg attribution — §7.1.1) with the widening
   required and the owner-decision bead-file-at-close-out
   commitment. Confirms the R6-inherited gaps carry unchanged.
7. §8 cross-checks every factual claim against source file:line.
8. `just test` is untouched-green — this cycle is docs-only.
9. **At R7 code-cycle close-out** (Cycle 2, not this Cycle 1):
   - File the frozen-model deficiency owner-decision bead
     (CALL-arg attribution; parent of NUMBER assignable-from's
     missing application site; sibling of `gqlc-fvo`). Pattern:
     `gqlc-fvo`, `gqlc-lta`, etc.
   - No new sentinel bead files.
   - gqlc-0mx.9 closes (this stage's bead).
   - ~~gqlc-ay9, gqlc-5xg,~~ gqlc-hk0, gqlc-fvo, gqlc-lta remain OPEN
     unchanged; R7 does not close any of them. [closed 2026-07-10:
     gqlc-ay9 landed via PRs #127/#128/#129
     (`docs/specs/unfreeze-ay9-optional-group.md`; residual gqlc-984),
     and gqlc-5xg landed via PRs #132/#133/#134
     (`docs/specs/unfreeze-5xg-required-bare-ref.md`; residual gqlc-0kq);
     R7 inherits both closures unchanged.]
   - The R7 code cycle asserts §3.3's byte-identical claim by
     running `just test` on the R0–R6 corpus WITHOUT `-update`
     before writing any R7 goldens; any regeneration in the R0–R6
     corpus fails the cycle.
   - **CRITICAL post-R7 hard stop**: NO codegen work begins. ADR
     0009 §Decision pins ValidatedQuery as provisional through R7;
     the ADR 0008 analogue (validating and freezing ValidatedQuery
     for codegen) is a human-gated ADR that must land before any
     downstream stage runs.
