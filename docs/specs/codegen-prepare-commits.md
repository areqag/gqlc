# Spec — codegen: prepare commits every render decision

Bead: `gqlc-ls8.3` (parent `gqlc-ls8`, "Architecture deepening: five module-depth
refactors from the 2026-07-12 review").

Deepening target: split between `internal/codegen/prepare.go` +
`internal/codegen/types.go` and `internal/codegen/render_*.go` today shares
authority over the D3 type-mapping table (ADR 0010). Prepare switches on
`resolver.ResolvedType` on 8 arms; `types.go` carries the mapping tables
(`goType`, `temporalGoType`, `scalarGoType`, `resolvedListGoType`);
`render_queries.go` re-switches on the same closed sum at emission time (list-
element decode: `writeListElementBody` / `writeListElementDecode` /
`listElemUsesBareAppend`), and both `render_queries.go:294` and
`render_querier.go:{43,53}` reach into `resolver.StatementRead` /
`resolver.StatementWrite` to pick an access mode and interface membership.

Failure mode this seam produces: a new `ResolvedType` variant (or a new
`Statement` value) compiles through `default:` arms in prepare — Phase A rejects
the query today — but if a variant were added tomorrow that resolves to a
plausible type, the render-side switches would silently fall off their default
arms and emit wrong generated code. Because the generated code IS the product
(ADR 0010 D5), the corruption reaches users without a compile break.

Deepening: prepare commits every rendering decision into the prepared model.
Render walks prepared data only and never mentions a `resolver.*` symbol. The
D3 table then has exactly one home (`internal/codegen/prepare.go` +
`internal/codegen/types.go`), and a new resolver variant that outruns prepare
becomes one loud sentinel there rather than silent wrong output at emission.

Scope note: this is a **structural refactor**. `Generate(Input) ([]File,
error)` — ADR 0010 D4/D6 — does not move. All 67 valid + 24 invalid codegen
goldens stay byte-identical without regenerating. The prepared model gains
fields; no field on `NamedQuery`, `resolver.ValidatedQuery`, or `[]File` is
touched.

---

## 1. What moves

The decisions currently re-derived at render time, and the field or method on
the prepared model each commits to.

### 1.1 Access mode per method (`render_queries.go:293-298`)

```go
func accessModeText(p preparedQuery) string {
    if p.Validated.Statement == resolver.StatementWrite {
        return "neo4j.AccessModeWrite"
    }
    return "neo4j.AccessModeRead"
}
```

Moves to a `preparedQuery.AccessMode` string committed in Phase B:
`"neo4j.AccessModeWrite"` iff `Validated.Statement == StatementWrite`, else
`"neo4j.AccessModeRead"`. Emission reads the field directly. The
`resolver.StatementWrite` reference at `render_queries.go:294` goes away.

### 1.2 Querier interface membership (`render_querier.go:41-60`)

```go
if p.Validated.Statement != resolver.StatementRead { continue }
...
if p.Validated.Statement != resolver.StatementWrite { continue }
```

Moves to a `preparedQuery.IsWrite bool` committed in Phase B. Emission filters
`ReadQuerier` on `!p.IsWrite`, `WriteQuerier` on `p.IsWrite`. The two
`resolver.StatementRead` / `resolver.StatementWrite` references at
`render_querier.go:{43,53}` go away.

`prepare.go:493`'s `resolver.StatementWrite` reference (Phase A shape-mismatch
message) stays where it is — Phase A is the mapping-table's home, prepare/types
are allowed to see resolver types; only render_*.go must not.

### 1.3 List-element decode arms (`render_queries.go:569-672`)

The load-bearing switch. `writeListElementDecode` computes an index-var name by
peeking at the element type via `listElemUsesBareAppend`; `writeListElementBody`
carries 8 arms on `resolver.ResolvedType` and re-derives the decode shape
(carrier, type-assertion text, error-message tag, entity-decode helper name,
edgeUnion candidate lookup). This is the biggest re-derivation site.

Moves in three parts:

**(a)** Every `preparedRow` with `Kind == columnList` grows a
`ListElemPlan preparedListElem` field. `preparedListElem` is the render-agnostic
description of one list-element decode step:

```go
type preparedListElem struct {
    Arm         listElemArm         // discriminator; see below
    GoType      string              // the emitted element Go type (== elemGoType today)
    Carrier     string              // driverCarrier(GoType) for the Property / narrow-int arm; "" for other arms
    NeedsIndex  bool                // false only for the bare-append arms; today's listElemUsesBareAppend
    UsesConvert bool                // Property arm: emit "GoType(v)" narrow-convert when Carrier != GoType
    EntityName  string              // Node/Edge arms: the entity struct name for the decode<Name> call
    Union       *preparedEdgeUnion  // EdgeUnion arm: the same preparedEdgeUnion the top-level column points at (positional Candidates), scoped to this leaf
    Nested      *preparedListElem   // List arm: the inner element plan; every field on the outer describes the outer iteration
}
```

`listElemArm` is the closed enum matching the current switch: `listElemProperty`,
`listElemTemporal`, `listElemScalar`, `listElemScalarNull`, `listElemUnknown`,
`listElemNode`, `listElemEdge`, `listElemEdgeUnion`, `listElemList`. Nine arms,
one per current switch case (`ResolvedScalar` splits `ScalarNull` off because
its decode is bare-append, no type assertion — matching today's
`listElemUsesBareAppend` boundary).

**(b)** Phase B builds `ListElemPlan` recursively from `t.Element`, mirroring
the existing `resolvedListGoType` walk (`internal/codegen/types.go:251-303`):
same recursion shape, same entity-index lookups, same edgeUnion-name synthesis
— but it commits the derived text into the plan instead of only returning the
GoType string. `resolvedListGoType`'s remaining job is unchanged (it still
returns the GoType text used both by Phase A's validity probe and by Phase B's
outer `preparedRow.GoType`); the new `listElemPlan` helper is called alongside
it in Phase B, or its output replaces `resolvedListGoType`'s at Phase B and
`resolvedListGoType` shrinks to a validity-only probe at Phase A — the C-side
call site chooses the pattern that keeps both call sites clean. Preferred:
`resolvedListGoType` retires and both Phase A (validity probe) and Phase B
(plan build) go through the new `listElemPlan`, whose `GoType` is exactly
what `resolvedListGoType` returned.

**(c)** `writeListElementDecode` / `writeListElementBody` /
`listElemUsesBareAppend` in `render_queries.go` retire and are replaced by one
`walkListElemPlan` that switches on `preparedListElem.Arm` (Go closed enum,
never `resolver.*`) and emits the same bytes. `render_queries.go` loses its
`import "github.com/areqag/gqlc/internal/resolver"` and every
`resolver.Resolved*` case.

### 1.4 `preparedRow.ListElem resolver.ResolvedType` field (`prepare.go:72`)

Retires. The field's only reader is `render_queries.go`'s list decode.
`preparedListElem` replaces it in `preparedRow` — carrying strictly Go-typed
committed decisions.

The `resolver.ResolvedType` type still appears in `preparedQuery` transitively
through `NamedQuery.Validated`, which is fine — the code contract is that
`render_*.go` walks nothing under `Validated` except `Statement` (already
lifted, §1.1/§1.2). To make this contract enforceable, an internal invariant
check runs in `prepare_test.go` (§5.3): `grep resolver\. internal/codegen/render_*.go`
returns nothing.

### 1.5 `preparedRow.EdgeKeys` field (`prepare.go:73`)

Kept as-is at Phase B. It carries `schema.EdgeKey` values (schema-side, not
resolver-side), so it does not fall under the "no resolver types in render"
contract; render_queries.go's edgeUnion arm reads `f.EdgeKeys[i].Label` to emit
the case-string, which is a schema-package reference the deepening does not
target. Confirmed as intentional in §5.1 below.

---

## 2. What does not move

- `Generate(Input) ([]File, error)` — the ADR 0010 D4/D6 API surface is
  unchanged. `Input.Schema` and `Input.Queries` unchanged;
  `NamedQuery.Validated` unchanged.
- `preparedQuery.NamedQuery` embedding (embeds `Validated` transitively) stays;
  Phase A / Phase B are free to walk it as they do today.
- Phase Z, Phase A, `sweepIdentifiers` — no visible change (Phase B grows plan
  construction, Phase A grows nothing).
- `internal/codegen/types.go`'s mapping-table functions (`goType`,
  `temporalGoType`, `scalarGoType`) — these ARE the D3 table's home. They
  stay in `types.go` and remain called only by prepare + list-plan
  construction. Their signatures do not change.
- `internal/codegen/render_models.go`, `render_db.go`, `render_querier.go`,
  `render_queries.go` — file layout unchanged. `render_models.go` and
  `render_db.go` have no `resolver.*` references today; `render_querier.go`
  and `render_queries.go` do — those references get lifted, but the files
  stay.
- The eight D3 sentinels (`ErrUnrepresentableWidth`, `ErrAliasRequired`,
  `ErrIdentifierCollision`, `ErrPropertyFieldCollision`,
  `ErrParamNameCollision`, `ErrRowFieldCollision`, `ErrExecOnProjection`,
  `ErrCardinalityShapeMismatch`) — no additions, no renames.

---

## 3. Deepened prepared model (final shape after this stage)

Additions to `preparedQuery`:

```go
type preparedQuery struct {
    NamedQuery
    MethodName  string
    Bare        string
    AccessMode  string    // NEW — §1.1
    IsWrite     bool      // NEW — §1.2
    ParamFields []preparedParam
    RowFields   []preparedRow
    EdgeUnions  []preparedEdgeUnion
}
```

Additions to `preparedRow`:

```go
type preparedRow struct {
    ColumnName string
    Field      string
    GoType     string
    Nullable   bool
    Kind       columnKind
    ListElem   *preparedListElem  // REPLACES the current ListElem resolver.ResolvedType (§1.3, §1.4). Non-nil iff Kind == columnList.
    EdgeKeys   []schema.EdgeKey
}
```

New type in `internal/codegen/prepare.go` (`preparedListElem` per §1.3 (a)).

---

## 4. Test plan

TDD, red-green-refactor, small commits. Testing is unit-first (§4.1) with the
existing golden fence unchanged (§4.2) as the hard fence.

### 4.1 New unit tests

`internal/codegen/prepare_test.go` gets one new table-driven test per lifted
decision, exercising Phase B's commit through the deepened `preparedQuery` /
`preparedRow` shape without touching render.

**TestPhaseBCommitsAccessMode**: table of (`Statement`, expected `AccessMode`).
Two rows: `StatementRead` → `"neo4j.AccessModeRead"`,
`StatementWrite` → `"neo4j.AccessModeWrite"`. Asserts `preparedQuery.AccessMode`
after `phaseBDerive` returns.

**TestPhaseBCommitsIsWrite**: table of (`Statement`, expected `IsWrite`). Two
rows: `StatementRead` → `false`, `StatementWrite` → `true`.

**TestPhaseBCommitsListElemPlan** — the mapping-table unit test the acceptance
criteria call for. Table over the full closed `resolver.ResolvedType` sum
(nine arms — `Property` × representable widths, `Node`, `Edge`, `EdgeUnion`,
`Temporal` × six kinds, `Scalar` × six kinds including `ScalarNull` split,
`Unknown`, `List` × recursion up to depth 2), plus the eight unrepresentable
widths at leaf level (each expected to error through `ErrUnrepresentableWidth`
via the recursion arm). Each row asserts:

- `preparedListElem.Arm` matches the expected `listElemArm` discriminant.
- `preparedListElem.GoType` matches `resolvedListGoType`'s pre-lift output for
  the same input (regression fence).
- `preparedListElem.Carrier` == `driverCarrier(GoType)` for Property /
  narrow-int arms; `""` for arms whose carrier is the GoType itself.
- `preparedListElem.NeedsIndex` == `false` iff Arm is `listElemUnknown` or
  `listElemScalarNull` (matching `listElemUsesBareAppend`).
- `preparedListElem.UsesConvert` == `true` iff Property arm and carrier
  differs from GoType.
- `preparedListElem.EntityName` matches `entities[entityIndex[...]].Name`
  for Node / Edge arms; `""` otherwise.
- `preparedListElem.Union` matches the top-level column's `preparedEdgeUnion`
  for EdgeUnion arms; `nil` otherwise.
- `preparedListElem.Nested` recursion produces the expected inner plan (walks
  the same table).

**TestRenderQueriesNoResolverImport**: a build-time invariant test — reads
`internal/codegen/render_queries.go`, `render_querier.go`,
`render_models.go`, `render_db.go`, `render.go` as bytes and asserts none
contains the substring `resolver.` outside of a same-file struct-embedded
`preparedQuery.NamedQuery`-derived read (of which there is none post-lift).
Runs in ~1ms; catches accidental re-introduction. Simpler alternative: an
`import` inspection via `go/parser` — the substring check is chosen for
readability since the render files are small.

### 4.2 Existing golden fence — unchanged, hard acceptance

The 67 valid + 24 invalid `test/data/codegen/` goldens run through
`just test-codegen-fence` unchanged, WITHOUT `-update`. Byte-identical output
is the primary regression fence; the D5 double-run determinism test stays
green (deepening does not touch iteration order).

The nested-module compile fence (`go build ./... && go vet ./...` inside
`test/data/codegen/go.mod`) stays green.

### 4.3 Static-analysis fence — grep-verifiable acceptance

```sh
! grep -qE 'resolver\.(Resolved|Statement|Temporal|Scalar)' \
    internal/codegen/render_queries.go \
    internal/codegen/render_querier.go \
    internal/codegen/render_models.go \
    internal/codegen/render_db.go \
    internal/codegen/render.go
```

Must exit 0 (zero matches). This is the mechanical form of "render_*.go
contains zero type-switches on resolver.ResolvedType and zero references to
resolver enums" from the bead's acceptance criteria. Added to the
`just test-codegen-fence` recipe (or as a small shell fragment in
`prepare_test.go` — the former is more visible).

### 4.4 Quality gates

`just test && just fmt-check && just lint` all green in the worktree. Re-run
`just fmt-check && just lint` after any post-review edit, even a
one-character one (workflow rule).

---

## 5. Design notes

### 5.1 Why `preparedRow.EdgeKeys` stays

`render_queries.go`'s edgeUnion column arm reads `f.EdgeKeys[i].Label` at
`writeEdgeUnionDispatchBody` (line 743) to emit the switch case string. That
label is a `schema.EdgeKey.Label` (a `graph.LabelSetKey`) — a schema-package
value, not a resolver-package one. The "no resolver types in render" contract
is scoped to `resolver.*`; `schema.*` and `graph.*` references at render time
are intentional (schema shapes generated code by design). Moving `.Label` into
a pre-computed string on `preparedEdgeUnion` would be pointless indirection.

### 5.2 Why the list-element plan carries `Union *preparedEdgeUnion`, not
a copy of the candidates

The candidates slice and interface name are already committed at Phase B (see
`prepare.go:684-736`); duplicating them on `preparedListElem` would create two
places to update on any future edgeUnion tweak. The plan holds a pointer to
the existing `preparedEdgeUnion` for its owning column, and render walks the
pointer.

Alternative considered: flat `Candidates []string` on `preparedListElem`.
Rejected: two writers to the same schema of derivation invites drift.

### 5.3 Why prepare keeps its `resolver.*` imports

The mapping-table's home is `internal/codegen/prepare.go` and
`internal/codegen/types.go`. They still switch on `resolver.ResolvedType`
because that IS their job — reading the resolver's output and committing
render decisions. What deepens is the render side: it stops re-doing prepare's
work.

Grep after this stage:

```
internal/codegen/prepare.go   — many resolver.* refs (correct)
internal/codegen/types.go     — many resolver.* refs (correct)
internal/codegen/input.go     — 1 (the NamedQuery.Validated field, ADR 0010 D1)
internal/codegen/render_*.go  — 0
```

That grep is the litmus: prepare owns the boundary, render never crosses it.

### 5.4 Sentinel behaviour on new `ResolvedType` variants (the failure mode
this deepening closes)

Today: Phase A rejects an unknown variant via its `default:` arm
(`prepare.go:565`), but a variant that Phase A were extended to admit could
silently miscompile through render's `default:`. After this deepening: any
render_*.go walk only sees `preparedListElem.Arm` (a closed Go enum owned by
prepare) and `columnKind` (already closed). Adding a resolver variant means
adding a `columnKind` (or a `listElemArm`) — a prepare-side change — and
render's `switch` compiles a warning-free exhaustive check via
`gochecksumtype`-style discipline once its type-switches are Go-enum-only.
Whether we add the `//sumtype:decl` marker to `columnKind` / `listElemArm` is
a nice-to-have; the mechanical closure alone kills the silent-miscompile mode.

---

## 6. Migration plan (sequenced commits, TDD)

Each commit stays small enough to bisect, keeps every existing test green,
and re-runs `just fmt-check && just lint` before staging.

1. **Red**: add `TestPhaseBCommitsAccessMode` — fails (field absent). No
   render changes.
2. **Green**: add `AccessMode` to `preparedQuery`, set in `phaseBDerive`. Test
   passes; existing render still reads `Validated.Statement` (both paths
   compute the same string).
3. **Refactor**: switch `render_queries.go:accessModeText` to read
   `p.AccessMode`; delete the `resolver.StatementWrite` reference; drop the
   `resolver` import from `render_queries.go` on the two `Statement` sites
   (the list-element sites remain to §7).
4. Same red/green/refactor triplet for `IsWrite` and `render_querier.go`.
5. **Red**: add `TestPhaseBCommitsListElemPlan` (skipped Union/Nested arms
   first) — fails.
6. **Green**: introduce `preparedListElem` type + `listElemArm` enum + the
   plan-builder helper in prepare.go / types.go. Populate
   `preparedRow.ListElem` (still the `resolver.ResolvedType` field) AND a
   new `preparedRow.ListElemPlan *preparedListElem` field in parallel. Test
   passes.
7. **Refactor**: rewrite `writeListElementDecode` + `writeListElementBody` +
   `listElemUsesBareAppend` in `render_queries.go` to walk `ListElemPlan`,
   never `resolver.*`. Goldens byte-identical.
8. **Delete**: retire `preparedRow.ListElem resolver.ResolvedType`, rename
   `ListElemPlan` → `ListElem`. Retire `resolvedListGoType`'s callers now
   that both call sites (Phase A validity probe, Phase B plan build) go
   through the plan-builder; keep `resolvedListGoType` only if a caller
   still needs the bare text (probably not).
9. **Enforce**: add the grep test (§4.3) to `just test-codegen-fence`. Run
   the full acceptance battery.

Commits 1-9 are one branch (`codegen-prepare-commits`, already created).
Each stage's diff is byte-reviewable; steps 2/6 add fields, steps 3/4/7
remove references, step 8 deletes. Golden diffs are expected to be zero
throughout — if any commit changes goldens, the plan is wrong and we stop.

---

## 7. Acceptance fence (from bead)

- All 67 valid + 24 invalid codegen goldens byte-identical, `-update` never
  invoked.
- Double-run determinism test green.
- `just test && just fmt-check && just lint` green in the worktree.
- Nested-module compile fence green (`go build ./... && go vet ./...` in
  `test/data/codegen/`).
- `render_*.go` grep for `resolver.` returns zero matches (§4.3).
- New mapping-table unit tests exist and pass (§4.1); include the
  unrepresentable-width sentinels in the `preparedListElem` recursion arm.

No push, no PR from this bead — team-lead handles merge after linus-3 PASS.
