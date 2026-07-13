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

**Revised per B2.** Committed as a closed prepare-side Go enum, not raw text.
A raw string bakes the driver's package name into prepared data — a hazard when
a second target lands (ADR 0010 D4 keeps the door open) and precisely the
"looked right on Tuesday" bug this deepening exists to close.

New in `internal/codegen/prepare.go`:

```go
type accessMode int

const (
    accessModeRead accessMode = iota
    accessModeWrite
)
```

Phase B commits `preparedQuery.AccessMode accessMode`, derived once from
`Validated.Statement`. The `accessModeText` function stays in
`render_queries.go` but switches on the local enum, not on `resolver.*`:

```go
func accessModeText(m accessMode) string {
    switch m {
    case accessModeWrite:
        return "neo4j.AccessModeWrite"
    }
    return "neo4j.AccessModeRead"
}
```

`render_queries.go` calls `accessModeText(p.AccessMode)`; the
`resolver.StatementWrite` reference at line 294 goes away.

### 1.2 Querier interface membership (`render_querier.go:41-60`)

```go
if p.Validated.Statement != resolver.StatementRead { continue }
...
if p.Validated.Statement != resolver.StatementWrite { continue }
```

Moves to a `preparedQuery.IsWrite bool` committed in Phase B (linus confirmed:
real two-value axis, boolean is the honest type). Emission filters
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

**Revised per B1 — enum unification.** No new `listElemArm` enum. Instead,
`columnKind` grows one arm — `columnScalarNull` — which today's
`listElemUsesBareAppend` distinguishes as a per-element special case at
render time. Every other list-element arm maps 1:1 to an existing
`columnKind`. There is **one** closed enum for both the top-level column
shape and the list-element shape. A future resolver variant is added exactly
once, in `columnKind`, and both the top-level switch and the list-element
switch fail to compile until the new arm is handled.

`columnKind` after this stage:

```go
const (
    columnProperty columnKind = iota
    columnNode
    columnEdge
    columnTemporal
    columnScalar
    columnScalarNull        // NEW — was folded into columnScalar/columnAny, split for enum unification
    columnList
    columnAny
    columnEdgeUnion
)
```

Behavioural note on `columnScalarNull`: at the top level today, Phase B sends
`ScalarNull` to `columnAny` (`prepare.go:668-670`) because it decodes through
`record.Get` (no `GetRecordValue[any]` overload). That decode-shape
distinction is preserved: the top-level switch treats `columnScalarNull`
and `columnAny` identically (both dispatch to the record.Get arm at
`writeAnyColumnDecodeIndent`) — a two-case switch, not a re-derivation. The
split exists so the list-element arm can distinguish "bare append (any-typed
element, no assertion)" from "typed scalar type-assert". Phase B assigns
`columnScalarNull` at both call sites so the arm's meaning is the same
whether it appears on a top-level row or inside a list plan.

Every `preparedRow` with `Kind == columnList` grows a `ListElem
*preparedListElem` field (see §1.4 for retirement of the old field).
`preparedListElem` mirrors `preparedRow` at the element level — carrying
strictly Go-typed committed decisions:

```go
type preparedListElem struct {
    Kind        columnKind          // the SAME closed enum as the top level
    GoType      string              // the emitted element Go type (== the elemGoType arg today)
    Carrier     string              // driverCarrier(GoType) for the Property arm; "" when Carrier == GoType
    UsesConvert bool                // true iff Property arm and Carrier != GoType (emit "GoType(v)")
    EntityName  string              // Node/Edge arms: the entity struct name for decode<Name>
    UnionIdx    int                 // EdgeUnion arm: index into owning preparedQuery.EdgeUnions (see B3)
    Nested      *preparedListElem   // List arm: the inner element plan; non-nil iff Kind == columnList
}
```

Notes:

- No `NeedsIndex` field: the bare-append vs typed-assert distinction is now
  `Kind == columnScalarNull || Kind == columnAny`, computed at emission with
  a two-case check (`listElemUsesBareAppend` retires; the check moves inline
  to the loop-head arm as a switch on `Kind`).
- `Kind == columnEdgeUnion` at a list-element level is legal (list-of-edgeUnion
  is a supported shape). The `UnionIdx` field points into the owning
  `preparedQuery.EdgeUnions` — see B3 for pointer-vs-index resolution.
- `Kind == columnList` at a list-element level is legal (nested lists) and
  triggers `Nested` recursion. Every other `columnKind` at `Nested` is
  disallowed structurally: `Nested` is nil except for `columnList`.

Phase B builds the plan recursively from `t.Element`, mirroring the existing
`resolvedListGoType` walk (`internal/codegen/types.go:251-303`): same
recursion shape, same entity-index lookups, same edgeUnion-name synthesis —
but it commits the derived shape into the plan instead of only returning the
GoType string. `resolvedListGoType` retires: both Phase A (validity probe)
and Phase B (plan build) go through the new `listElemPlan` builder, whose
`GoType` field is exactly what `resolvedListGoType` returned.

`writeListElementDecode` / `writeListElementBody` / `listElemUsesBareAppend`
in `render_queries.go` retire and are replaced by one `walkListElemPlan` that
switches on `preparedListElem.Kind` (the shared `columnKind` enum) and emits
the same bytes. `render_queries.go` loses its
`import "github.com/areqag/gqlc/internal/resolver"` line entirely (once §1.1
and §1.2 also land).

### 1.4 `preparedRow.ListElem resolver.ResolvedType` field (`prepare.go:72`)

Retires and is **replaced in one atomic commit** (B4). The field's only reader
is `render_queries.go`'s list decode. `preparedListElem` replaces it.
`preparedRow.ListElem` — same field name — is retyped from
`resolver.ResolvedType` to `*preparedListElem` in a single commit that also
updates Phase B's write site and the render read site. There is no transient
period during which two writers exist for the same decision (§6 revised).

The `resolver.ResolvedType` type still appears in `preparedQuery` transitively
through `NamedQuery.Validated`, which is fine — the code contract is that
`render_*.go` walks nothing under `Validated` except `Statement` (already
lifted, §1.1/§1.2). To make this contract enforceable, a grep test runs in
the fence (§4.3).

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
    AccessMode  accessMode          // NEW — §1.1, closed enum
    IsWrite     bool                // NEW — §1.2
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
    Kind       columnKind         // now nine arms (adds columnScalarNull, §1.3)
    ListElem   *preparedListElem  // REPLACES the current ListElem resolver.ResolvedType (§1.3, §1.4)
    EdgeKeys   []schema.EdgeKey
}
```

New types in `internal/codegen/prepare.go`: `accessMode` (with two constants),
`preparedListElem` (per §1.3).

Retired constants: none. `columnAny` stays — top-level `Unknown` and top-level
scalar-null both continue to route there today via `writeAnyColumnDecodeIndent`;
`columnScalarNull` is a new sibling that lives on list-element plans and any
future call site that needs the "any-typed but specifically null-origin"
distinction. Top-level Phase B assignment stays at `columnAny` for
`ScalarNull` (no golden change).

### 3.1 Edge-union storage — pointer-stability by construction (B3)

`preparedQuery.EdgeUnions` type changes from `[]preparedEdgeUnion` to
`[]*preparedEdgeUnion` (linus's preferred option (b)). One-line type change;
every current reader that ranges values just ranges pointers instead:

- `prepare.go:695-736` (Phase B append sites) — the append target becomes a
  `*preparedEdgeUnion`, constructed at the call site.
- `prepare.go:822-828` (`sweepIdentifiers` source 6) — ranges pointers.
- `render_models.go:48-59` (`unions` local + `markersByEntity` walk) —
  ranges pointers.
- `render_queries.go:759-770` (`edgeKeyToEntityName`) — ranges pointers.
- `render_queries.go:828-853` (`findEdgeUnionCandidates`) — ranges pointers.

`preparedListElem.UnionIdx int` (per §1.3) stores an index into
`preparedQuery.EdgeUnions`. Index is chosen over a raw `*preparedEdgeUnion`
pointer inside `preparedListElem` because:

- the plan is built one column at a time, and the top-level column's
  `preparedEdgeUnion` may be appended AFTER the list plan for that same column
  is constructed (see prepare.go:695 vs 727 — top-level union appended before
  list-of-edgeUnion; but a future refactor could reorder). An index is
  reorder-safe.
- `sweepIdentifiers` and `render_models.go` walk `EdgeUnions` by position;
  storing an index aligns the plan with that indexing without introducing a
  second citation of the same object.
- indices survive slice growth even if a future edit removes the
  `[]*preparedEdgeUnion` pointer-stability guarantee.

Two-layer defence: pointers for external callers (they don't want to worry
about slice growth), indices for internal cross-references inside the same
`preparedQuery` (dead simple, no lifetime story).

---

## 4. Test plan

TDD, red-green-refactor, small commits. Testing is unit-first (§4.1) with the
existing golden fence unchanged (§4.2) as the hard fence.

### 4.1 New unit tests

`internal/codegen/prepare_test.go` gets one new table-driven test per lifted
decision, exercising Phase B's commit through the deepened `preparedQuery` /
`preparedRow` shape without touching render.

**TestPhaseBCommitsAccessMode**: table of (`Statement`, expected `AccessMode`
enum value). Two rows:
`StatementRead → accessModeRead`, `StatementWrite → accessModeWrite`.
Asserts `preparedQuery.AccessMode` after `phaseBDerive` returns.

**TestPhaseBCommitsIsWrite**: table of (`Statement`, expected `IsWrite`
bool). Two rows: `StatementRead → false`, `StatementWrite → true`.

**TestPhaseBCommitsListElemPlan** — the mapping-table unit test the acceptance
criteria call for (revised per B6, exact row counts named).

Top-level element table — **34 positive rows** exercising every arm of the
sum × each variant × nullable/non-nullable where the axis exists:

- **17 property widths** (representable): `TypeString`, `TypeBool`, `TypeInt`,
  `TypeInt8`, `TypeInt16`, `TypeInt32`, `TypeInt64`, `TypeUint`, `TypeUint8`,
  `TypeUint16`, `TypeUint32`, `TypeUint64`, `TypeFloat`, `TypeFloat32`,
  `TypeFloat64`, `TypeDate`, `TypeTimestamp`. Each row asserts the leaf's
  `GoType` matches `goType(pt)` and `Carrier` = `driverCarrier(GoType)`;
  `UsesConvert` is `true` iff Carrier differs from GoType (narrow-int and
  FLOAT32); `Kind == columnProperty`. Half the rows are nullable so the
  top-level Nullable propagation is exercised on a non-list wrapper (list
  element itself is never nullable — nullability lives at the row level).
- **6 temporal kinds**: `TemporalDate`, `TemporalTime`, `TemporalLocalTime`,
  `TemporalDateTime`, `TemporalLocalDateTime`, `TemporalDuration`. Each
  asserts `Kind == columnTemporal`, `GoType == temporalGoType(k)`,
  `Carrier == ""`, `UsesConvert == false`.
- **6 scalar kinds**: `ScalarBool`, `ScalarInt`, `ScalarFloat`,
  `ScalarString`, `ScalarNull`, `ScalarMap`. Rows 1-4 and 6 assert
  `Kind == columnScalar`, `GoType == scalarGoType(k)`; the `ScalarNull` row
  asserts `Kind == columnScalarNull`, `GoType == "any"` — the arm-splitting
  behaviour from §1.3.
- **1 Unknown**: `Kind == columnAny`, `GoType == "any"`.
- **1 Node**: fixture schema with one node type "Person"; asserts
  `Kind == columnNode`, `EntityName == "Person"`, `GoType == "Person"`.
- **1 Edge**: fixture schema with one edge type; asserts `Kind == columnEdge`,
  `EntityName` and `GoType` match the schema-derived struct name.
- **1 EdgeUnion**: fixture schema with two candidate edges; asserts
  `Kind == columnEdgeUnion`, `UnionIdx` points at the constructed
  `preparedEdgeUnion` with matching `EdgeKeys` and `Candidates`.
- **1 nested list** at depth 2 (`list<list<int64>>`): asserts outer
  `Kind == columnList`, `Nested != nil`, `Nested.Kind == columnList`,
  `Nested.Nested.Kind == columnProperty` with `GoType == "int64"`.

Negative table — **9 rows** all expected to route to `ErrUnrepresentableWidth`
via the recursion arm, exercising the list-element leaf branch of the eager
width sweep:

- Property leaves at each of the 8 unrepresentable widths: `TypeInt128`,
  `TypeInt256`, `TypeUint128`, `TypeUint256`, `TypeFloat16`, `TypeFloat128`,
  `TypeFloat256`, `TypeDecimal`. Each row asserts the plan-builder returns
  `ErrUnrepresentableWidth` naming the width.
- **1 synthetic-variant negative row** (per B6's malformed-variant fence):
  a test-file-local stub `ResolvedType` implementation that satisfies the
  sealed interface via `isResolvedType()` but is neither in the sum's
  known arms nor rejected by Phase A. Passed directly to the plan-builder
  (bypassing Phase A), asserts the builder returns a sentinel error — not
  silent success. This is the fence for the specific failure mode this
  bead exists to close: an unknown variant reaching plan construction
  must yield a loud error, never a default arm.

Table total: 34 positive + 9 negative = **43 rows** on
`TestPhaseBCommitsListElemPlan`. Row count named so the reviewer can hold
me to it (per B6). If a variant is added to the resolver, the table grows
by exactly one row per new arm — the same reviewability property the
deepening buys everywhere else.

**TestPreparedListElemMapsToColumnKind**: five-row assertion table that every
value the plan-builder can assign to `preparedListElem.Kind` is one of the
nine `columnKind` values (`columnProperty`, `columnNode`, `columnEdge`,
`columnTemporal`, `columnScalar`, `columnScalarNull`, `columnList`,
`columnAny`, `columnEdgeUnion`). Explicit exhaustive assertion — if a
tenth `columnKind` is added without extending the plan-builder, this test
fails.

### 4.2 Existing golden fence — unchanged, hard acceptance

The 67 valid + 24 invalid `test/data/codegen/` goldens run through
`just test-codegen-fence` unchanged, WITHOUT `-update`. Byte-identical output
is the primary regression fence; the D5 double-run determinism test stays
green (deepening does not touch iteration order).

The nested-module compile fence (`go build ./... && go vet ./...` inside
`test/data/codegen/go.mod`) stays green.

### 4.3 Static-analysis fence — grep-verifiable acceptance (revised per B5)

Widened grep pattern:

```sh
! grep -qE 'resolver\.' \
    internal/codegen/render_queries.go \
    internal/codegen/render_querier.go \
    internal/codegen/render_models.go \
    internal/codegen/render_db.go \
    internal/codegen/render.go
```

Any `resolver.` substring — not just `resolver.(Resolved|Statement|Temporal|Scalar)`
— fails the fence. `resolver.Property`, `resolver.ValidatedQuery`,
`resolver.Column`, `resolver.ResolvedParameter`, and every future symbol
are covered. If a legitimate resolver reference sneaks into render_*.go, the
grep fails LOUD and it gets defended in review.

Test-file scope: the grep also runs against
`internal/codegen/*_test.go` — test files can drift the same way. Rationale:
prepare_test.go is where the resolver mapping tests live, but there is no
`render_*_test.go` today; if one appears, it inherits the fence. Documented
exemption: none. All render-side files and their tests: zero `resolver.`
references.

Added to the `just test-codegen-fence` recipe. On failure the recipe prints
the offending file:line so the fix site is obvious.

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

### 5.2 Why the list-element plan carries `UnionIdx int`, not a pointer

Two rents drive this (B3 resolution):

- **Slice-append reallocation.** `preparedQuery.EdgeUnions` grows via `append`
  inside the Phase B column loop, both at the top-level edgeUnion arm and at
  the list-of-edgeUnion recursion arm. A raw `*preparedEdgeUnion` inside
  `preparedListElem` would go stale on reallocation.
- **Cross-slice reordering resilience.** A future Phase B edit might build
  the list plan before the top-level union is appended. An index is order-safe
  because `sweepIdentifiers` and `render_models.go` already index EdgeUnions
  by position.

External callers (render code) get pointer stability via `EdgeUnions
[]*preparedEdgeUnion` — no lifetime story to remember. Internal cross-
references inside the same `preparedQuery` use an index — trivially safe.

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
render_*.go walk only sees `preparedListElem.Kind` and `preparedRow.Kind`
(the shared closed `columnKind`) and `preparedQuery.AccessMode` (the closed
`accessMode`). Adding a resolver variant means adding a `columnKind` arm — a
prepare-side change — and every render switch on `columnKind` gets flagged
by the compiler if it lacks a case (exhaustive-switch discipline via
gochecksumtype / go vet's exhaustive analyser on request). The mechanical
closure alone kills the silent-miscompile mode: one enum, one place to add
a variant, every switch on it gets reviewed at the same time.

The `TestPhaseBCommitsListElemPlan` synthetic-variant negative row (§4.1)
seals this: even if a variant sneaks past Phase A somehow, the plan-builder
itself returns a sentinel.

---

## 6. Migration plan (sequenced commits, TDD — revised per B4)

Each commit stays small enough to bisect, keeps every existing test green,
and re-runs `just fmt-check && just lint` before staging. **No transient
window with two writers for the same decision.**

1. **Red**: add `TestPhaseBCommitsAccessMode` — fails (field absent). No
   render changes.
2. **Green + refactor**: introduce `accessMode` enum + `AccessMode`
   field on `preparedQuery`, populate in `phaseBDerive`. Update
   `render_queries.go:accessModeText` to switch on the enum. Delete the
   `resolver.StatementWrite` reference in that file. One atomic commit —
   old and new paths do not coexist.
3. Same red / green+refactor pair for `IsWrite` and `render_querier.go`.
4. **Red**: add `TestPhaseBCommitsListElemPlan` and
   `TestPreparedListElemMapsToColumnKind` — fails (plan builder absent,
   `columnScalarNull` absent).
5. **Green + refactor — atomic**: in one commit:
   - Add `columnScalarNull` to `columnKind` (constant only; no assignment
     site changes at the top level for now).
   - Add `preparedListElem` type.
   - Change `EdgeUnions` on `preparedQuery` from `[]preparedEdgeUnion` to
     `[]*preparedEdgeUnion` and update all readers (prepare, sweepIdentifiers,
     render_models, render_queries).
   - Retype `preparedRow.ListElem` from `resolver.ResolvedType` to
     `*preparedListElem`. Update Phase B write site (build the plan
     recursively via a new `buildListElemPlan` helper). Update
     `render_queries.go`'s list decode to walk the plan
     (`writeListElementDecode`, `writeListElementBody`,
     `listElemUsesBareAppend` all retire; `walkListElemPlan` replaces
     them, switching on `preparedListElem.Kind`).
   - `render_queries.go` loses its `import
     "github.com/areqag/gqlc/internal/resolver"` line.
   - `resolvedListGoType` retires (its callers now go through
     `buildListElemPlan.GoType`).

   This is one commit because there is no correct intermediate state — the
   list-plan reader in render and the list-plan writer in prepare change
   together, or the goldens change. Bigger-and-atomic beats
   smaller-and-drift-window (B4). Diff will be large but reviewable;
   goldens byte-identical throughout is the hard fence.
6. **Enforce**: add the widened grep test (§4.3) to `just test-codegen-fence`.
   Run the full acceptance battery (`just test-codegen-fence`, `just test`,
   `just fmt-check`, `just lint`, nested-module compile fence).

Commits 1-6 are one branch (`codegen-prepare-commits`, already created).
Each commit's diff is byte-reviewable; commits 2 and 3 add/lift; commit 5 is
the atomic list-plan swap. Golden diffs expected to be zero throughout — if
any commit changes goldens, the plan is wrong and we stop.

---

## 7. Acceptance fence (from bead)

- All 67 valid + 24 invalid codegen goldens byte-identical, `-update` never
  invoked.
- Double-run determinism test green.
- `just test && just fmt-check && just lint` green in the worktree.
- Nested-module compile fence green (`go build ./... && go vet ./...` in
  `test/data/codegen/`).
- `render_*.go` (and `internal/codegen/*_test.go`) grep for `resolver\.`
  returns zero matches (§4.3, widened per B5).
- New mapping-table unit tests exist and pass (§4.1): 43-row
  `TestPhaseBCommitsListElemPlan` covering the full sum + eight
  unrepresentable widths + one synthetic malformed variant;
  `TestPhaseBCommitsAccessMode` (2 rows);
  `TestPhaseBCommitsIsWrite` (2 rows);
  `TestPreparedListElemMapsToColumnKind` (nine-arm closed-enum assertion).

No push, no PR from this bead — team-lead handles merge after linus-3 PASS.
