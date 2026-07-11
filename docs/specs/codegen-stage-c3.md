# Stage C3 spec — codegen: collections, temporals, widths

The implementation brief for Stage C3 of `internal/codegen`, extending
the merged C2 slice (`docs/specs/codegen-stage-c2.md`) with the four
capability slices ADR 0010 D7 places at C3: **`ResolvedList<T>`
recursion into Go `[]T`**, **the six openCypher temporals via
`dbtype`**, **property-side `DATE` / `TIMESTAMP`** (moved from the
C2-owned `ErrOutOfC2Scope` catchment into first-class support),
**unrepresentable-width sentinels for INT128/256, UINT128/256, FLOAT16,
FLOAT128/256, DECIMAL** with the **FLOAT32 schema-width contract**
retained, and **`any` fallbacks** for `ResolvedUnknown` /
`ResolvedScalar` `null` / `ResolvedScalar` `map`. Build this
**test-first**. Scope, sequencing, and error posture inherit from
ADR 0010 and the C0 / C1 / C2 specs unchanged; this document revises
only the sections C3 touches.

Stage C3 keeps the C2 file set (`db.go` / `querier.go` /
`models.go` / `<name>.cypher.go`) byte-identical for the parts C3 does
not touch, extends the C2 property → Go type table with the four
temporal-property rows and the eight unrepresentable-width rows, adds
five new column-shape rows (`ResolvedList`, `ResolvedTemporal`,
`ResolvedScalar`, `ResolvedUnknown`), promotes the unrepresentable-
width check from lazy (C1 fail-site on a projected column) to eager
(Phase Z sweep across every property on every entity), extends the
per-query row assembly with three new column arms (list, temporal,
scalar/unknown), and refines the `dbtype` / `time` import invariants
to cover the new call sites. Every entity struct stays schema-shaped
only (D3 Resolved); every list depth renders `[][]…[]T` uniformly
(D3 Resolved: `list<T>` → `[]T` recursive); every temporal decodes
through `neo4j.GetRecordValue[dbtype.<Kind>]` or
`neo4j.GetRecordValue[time.Time]` (D3 Resolved: v5 uses stdlib
`time.Time` for zoned datetime). Writes (`:exec`, `WriteQuerier`
population, `ExecuteWrite` path) stay out of scope and continue to
route through `ErrOutOfC3Scope`; C4 owns them (ADR 0010 D7).

---

## 1. Deliverables

- `internal/codegen/generate.go` — extended with the four temporal-
  property table rows (§5.1), the five new column-shape rows
  (§5.1), the unrepresentable-width sentinel (§5.1, §9), the
  `ResolvedList<T>` recursive Go-type derivation (§4.7, §5.5), the
  temporal / list / scalar-unknown row-assembly arms (§5.5), the
  FLOAT32 encode-widen / decode-narrow pinning (§5.5, §5.7), and
  the Phase Z eager sweep over every schema property's width (§4.8,
  §5.2). The C0 file layout stands (`codegen.go` / `input.go` /
  `errors.go` / `generate.go`); no new files at C3.
- `internal/codegen/errors.go` — extended with one new sentinel and
  one rename (§9): `ErrUnrepresentableWidth`, plus `ErrOutOfC2Scope`
  → `ErrOutOfC3Scope`. No other sentinel additions or retirements.
- `test/data/codegen/valid/<name>/` — new C3 valid fixtures (§6.2),
  each with a schema `.gql` producing temporals / lists / scalar /
  unknown coverage, optionally query files projecting them, a
  `manifest.json`, and a `golden/` subdirectory with the complete
  generated package.
- `test/data/codegen/invalid/<name>/` — new C3 negative fixtures for
  the new sentinel plus the renamed `ErrOutOfC3Scope` retirements
  (§6.4). The C2 `out_of_c2_scope_int128` fixture retires from the
  invalid set and reappears (with the same schema) as an
  unrepresentable-width fixture under `ErrUnrepresentableWidth`
  (§6.4 details).
- `internal/codegen/codegen_test.go` — no structural change; the
  `sentinelByName` map grows one row and renames one.

Nothing downstream of collections + temporals + widths + honest-`any`
is built. Writes + `:exec` + cardinality × shape rejection (C4),
`edgeUnion` sealed interfaces + package-level collision-sweep
hardening (C5), version-stamp polish (C6), `:iter` streaming (post-v1,
`gqlc-1a5`) stay for their owning stage per ADR 0010 D7.

---

## 2. Architecture — deltas from C2

C2's architecture (§2 of the C2 spec) stands: the `Generator` seam,
the concrete `*Codegen` return, the empty `Option` surface, the
purity / determinism / short-circuit posture, the
`generate.go` / `generate` kernel split, the Phase Z / Phase A /
Phase B / cross-query sweep sequence. C3 extends Phase Z with a
schema-shape width sweep (§2.1), widens Phase A's admissible
`Column.Type` set to the full closed sum (§2.1), and extends
Phase B's row-field type derivation with the `ResolvedList`
recursion helper and the temporal / scalar / unknown mappings
(§2.1). No new exported types except the one new sentinel; no
API-shape delta (§3 below); the `Input` struct stays `{Schema,
Queries}` (ADR 0010 D6).

### 2.1 The C3 kernel structure

The kernel remains one linear pass with early returns. C3 extends
each of the four existing phases in-place:

- **Phase Z — schema-shape admission and entity naming**
  (§4.8, §5.2). C2 already runs an entity-naming pass and a per-
  entity property-field derivation pass. C3 folds the
  unrepresentable-width check into the same field-derivation pass:
  for every property on every entity, if `goType(p.Type)` reports
  `false` because the width is unrepresentable (INT128/256,
  UINT128/256, FLOAT16, FLOAT128/256, DECIMAL) the pass returns
  `ErrUnrepresentableWidth` naming the entity, property, and
  width. First offender wins across the schema-shape axis. The
  eager sweep is what D3 Resolved sanctions: a lazy check at
  column projection would make output depend on the query set, and
  Phase Z already exists as the natural home for schema-shape
  rejections (`ErrUnnamedMultiLabelType`, `ErrPropertyFieldCollision`).
  Temporal property widths (DATE / TIMESTAMP) are NOT rejected —
  they now map (§5.1) to `dbtype.Date` and `time.Time`. Phase Z's
  cache carries the extra bits Phase A needs: `anyTemporalDBType`
  (`dbtype.Date` in use), `anyTimeTime` (`time.Time` in use), so
  the emission walk (§5.2) can gate `models.go`'s `dbtype` /
  `time` imports without a second pass.
- **Phase A — batch admission** (unchanged shape, extended admission
  set). Every `NamedQuery` still passes C0's `validateQueries` gate
  and the C1/C2 per-query admission checks. C3 widens the
  admissible `Column.Type` set from
  `{ResolvedProperty, ResolvedNode, ResolvedEdge}` to
  `{ResolvedProperty, ResolvedNode, ResolvedEdge, ResolvedList,
  ResolvedTemporal, ResolvedScalar, ResolvedUnknown}` — every
  `ResolvedType` sum member except `ResolvedEdgeUnion`, which C5
  still owns. A `ResolvedEdgeUnion` column still routes to
  `ErrOutOfC3Scope`. Property columns with the unrepresentable
  widths still route to `ErrUnrepresentableWidth` **at Phase Z's
  earlier eager sweep** for any property that is part of any
  schema entity; a query projecting `foo.someUnrepresentableProp`
  is impossible because Phase Z has already rejected the schema.
  A property column whose width is temporal (DATE / TIMESTAMP) is
  now admissible — it maps through the C3 property table (§5.1).
  Parameter admission widens symmetrically for temporal property
  widths (DATE, TIMESTAMP) — a `$since :: TIMESTAMP` parameter is
  admissible and encodes as `time.Time`. Non-property parameters
  (whole node, whole edge, temporal expression, scalar literal,
  list, unknown) stay inadmissible and route to `ErrOutOfC3Scope` —
  the parameter axis is not what the resolver produces for the
  typical `$name :: STRING` case, and widening it further is a
  post-v1 axis (D3 Resolved's symmetric treatment applies to the
  representable widths, not to the whole `ResolvedType` sum).
- **Phase B — per-query name derivation** (unchanged shape, extended
  derivation set). Row-field text-shape analysis is unchanged;
  Params-field mangle is unchanged. C3 extends the Row-field Go-
  type text mapping: a `ResolvedProperty` column with a temporal
  width renders `dbtype.Date` or `time.Time`; a `ResolvedList<T>`
  column renders `[]<T's Go type>` recursively via the
  `resolvedListGoType(t ResolvedType)` helper (§4.7); a
  `ResolvedTemporal{Kind}` column renders the C3 temporal table
  row for that kind (§5.1); a `ResolvedScalar{Kind}` column
  renders `bool` / `int64` / `float64` / `string` / `any` /
  `map[string]any` per the D3 Resolved table row; a
  `ResolvedUnknown` column renders `any`. Nullable columns still
  render `*T` uniformly (D3 Resolved). Row-field collision rules
  (§4.3 of C1) are unchanged — the check is on derived field name,
  which came from `Column.Name`, not from the type.

Phase Z runs before Phase A because Phase A reads Phase Z's cached
entity-naming results (unchanged from C2) plus, at C3, the
`anyTemporalDBType` / `anyTimeTime` bits so the `models.go` import
block can be prepared before the emission walk. Phase A alone never
fails on the schema-shape width axis — every unrepresentable-width
rejection is Phase Z's, whether the width lives on a schema
property that no query projects or on one that many do. This
eagerness is the load-bearing decision (§4.8's defence).

The cross-query package-level identifier collision sweep (§4.6 of
C2) runs unchanged after Phase B. C3 introduces no new exported
identifiers — the temporal cell types (`dbtype.Date` etc.) and the
`any` fallbacks are library or built-in, not gqlc-emitted. The
sweep's four identifier sources stay: entity struct names, method
names, `<Method>Params`, `<Method>Row`.

### 2.2 The `resolvedListGoType` recursion — the C3 helper

C3 introduces one new internal helper in `generate.go`:

```go
// resolvedListGoType derives the Go type text for a ResolvedType
// used as a Column.Type or as a ResolvedList element (§5.1). Returns
// (text, err): err wraps ErrUnrepresentableWidth for a leaf that is
// an unrepresentable property width (identical fail-message shape
// to the Phase Z sweep — the two call sites converge on one
// sentinel); err wraps ErrOutOfC3Scope for a ResolvedEdgeUnion leaf
// (C5 owns). A ResolvedList element recurses; every other leaf is
// one dispatch on the ResolvedType sum. Never returns "any" for a
// list element whose leaf resolves — the "any" fallback fires only
// when the leaf itself is ResolvedUnknown / ResolvedScalar{null} /
// ResolvedScalar{map} per §5.1's table.
func resolvedListGoType(t resolver.ResolvedType, entities []preparedEntity, entityIndex map[entityLookupKey]int) (string, error)
```

The helper is grounded in the existing `generate` scope: it reads
the entity-index (populated by Phase Z) for a `ResolvedNode` /
`ResolvedEdge` leaf, delegates to `goType` for a `ResolvedProperty`
leaf, and recurses on itself for a `ResolvedList` element. The
recursion terminates on every leaf variant of the `ResolvedType`
sum; there is no bounded-depth cutoff — a `list<list<list<string>>>`
column produces `[][][]string` in three recursive calls, a
`list<unknown>` produces `[]any`, a `list<list<unknown>>` produces
`[][]any`. The leaf-termination invariant is a property of the
sum, not of a depth counter: every non-`ResolvedList` variant
returns without recursion, so the total depth is bounded by the
resolver's depth for that column.

### 2.3 Purity, determinism, short-circuit — unchanged

C2 §2.3's three invariants stand:

- **Pure.** No new I/O; the list-recursion helper is pure text-to-
  text; the width-check sweep reads schema properties, no external
  lookup.
- **Deterministic.** Iteration order: Phase Z walks `Schema.Nodes`
  in `graph.LabelSetKey` lexical order and `Schema.Edges` in
  `EdgeKey` triple-lex order (unchanged from C2); the width sweep
  runs inside each entity's property-field derivation in the same
  map-key-sorted order (§5.2). Per-query Phase A / Phase B walks
  are unchanged. No map iteration escapes into the output.
- **Short-circuit.** First-error wins across Phase Z (width sweep
  now included), Phase A, Phase B, the cross-query collision
  sweep, and per-source emission. Zero value on error: `(nil,
  err)`.

### 2.4 What the C3 change means for the emitted module

C3 revises the emitted `models.go` and `<name>.cypher.go` import
blocks. The tightest invariants (extending C2 §2.4):

- **`models.go` `dbtype` iff any entity struct emits** (unchanged
  from C2) — every helper's argument type is `dbtype.Node` or
  `dbtype.Relationship`.
- **`models.go` `time` iff any entity property decodes as
  `time.Time`** — introduced at C3 for TIMESTAMP properties, and
  for the future case (post-v1) of a TIMESTAMP field on any entity
  struct.
- **`models.go` `neo4j` iff any non-nullable property is decoded**
  (unchanged from C2) — `neo4j.GetProperty[T]` still gates the
  non-nullable arm; the nullable arm reads `Props` directly.
- **`<name>.cypher.go` `dbtype` iff any column decodes through a
  `dbtype.<Kind>` carrier** — extended from C2's "any entity
  column" to include temporal columns (DATE property, six
  temporals) and list columns whose leaf uses `dbtype.<Kind>`.
- **`<name>.cypher.go` `time` iff any column decodes as
  `time.Time`** — introduced at C3 for TIMESTAMP property columns,
  the zoned `TemporalDateTime`, and list columns whose leaf is
  either of the two.

The change is entirely inside the emitted templates; gqlc's own
module is not affected — the generator emits text, and text-level
changes cross no dependency boundary. The nested-module compile
fence (`just test-codegen-fence`, C0 §7) is what proves the emitted
body type-checks against the pinned driver version. The driver's
`dbtype` sub-package and `neo4j.RecordValue` /
`neo4j.PropertyValue` unions are stable in v5.28.4 (verified
against `pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5/neo4j` and
`.../neo4j/dbtype`, 2026-07-11): `RecordValue` includes
`Date | LocalTime | LocalDateTime | Time | Duration | time.Time`
plus `[]any` and `map[string]any`; `PropertyValue` includes the
same six temporals for entity-property decode.

---

## 3. Emitted API surface — the C3 shape

The user-visible generated surface C3 adds on top of C2. C3's
exported package-level identifiers are unchanged; every widening is
in the Go-type text of Row-struct fields, Params-struct fields,
entity-struct property fields, and bare `:one` / `:many` returns.
The C0 / C1 / C2 exported skeleton set (`Queries`, `New`,
`WithTx`, `ReadQuerier`, `WriteQuerier`, `Querier`; `ErrNoRows` /
`ErrMultipleResults` when a `:one` query is present; per-query
methods; entity structs; `<Method>Params` / `<Method>Row` structs)
is unchanged.

### 3.1 Temporal-column returns and Row-struct fields

A query projecting a temporal column renders one of six Go types
per the D3 Resolved table (§5.1). A `:one` single-column query
returns the temporal type bare; a `:many` single-column returns a
slice; a two-plus-column projection lands the temporal on a Row-
struct field.

```go
// RETURN date('2024-01-01') AS d :one
func (q *Queries) TodayDate(ctx context.Context) (dbtype.Date, error)

// RETURN datetime() AS now :one
func (q *Queries) Now(ctx context.Context) (time.Time, error)

// RETURN duration({days: 7}) AS d :one
func (q *Queries) OneWeek(ctx context.Context) (dbtype.Duration, error)
```

- **`dbtype.Duration` for openCypher DURATION**, uniformly and
  deliberately (D3 Resolved). The Go `time.Duration` type is
  monotonic-nanoseconds; openCypher DURATION is calendar-aware
  (months + days + seconds + nanoseconds — see
  `dbtype/temporal.go`). Mapping DURATION to `time.Duration` would
  silently corrupt month/day arithmetic; the schema author who
  wrote `duration({months: 3})` expects three months, not
  `3 * 30 * 24h`. `dbtype.Duration` faithfully represents the
  four-component shape.
- **`time.Time` for zoned DATETIME**, uniformly (D3 Resolved). The
  driver's v5 `RecordValue` union includes `time.Time` (with a
  comment `// OffsetTime == Time == dbtype.Time`) as the mapping
  for zoned datetime — the standard library's `time.Time` carries
  timezone information natively, so passing through a distinct
  `dbtype.DateTime` shape would add ceremony with no gain. The
  driver validated this choice on the write side (the resolver
  emits DATETIME literals into `time.Time` on decode).
- **Nullable temporal column** renders `*dbtype.Date`,
  `*time.Time`, etc. — the uniform pointer rule (D3 Resolved).

### 3.2 List-column returns and Row-struct fields

A query projecting a `list<T>` column renders `[]<T's Go type>`
recursively. A `:one` single-column query returns the slice bare;
a `:many` single-column returns a slice of slices; a two-plus-
column projection lands the slice on a Row-struct field.

```go
// RETURN [1, 2, 3] AS xs :one
func (q *Queries) OneListInt(ctx context.Context) ([]int64, error)

// RETURN [[1], [2, 3]] AS xss :one
func (q *Queries) NestedList(ctx context.Context) ([][]int64, error)

// RETURN [datetime(), datetime()] AS ts :one
func (q *Queries) TwoTimes(ctx context.Context) ([]time.Time, error)

// RETURN [p.name] AS xs :one   // p.name :: STRING
func (q *Queries) OneNameList(ctx context.Context) ([]string, error)
```

- **Recursion is unbounded** — the `resolvedListGoType` helper
  (§2.2) walks the `ResolvedList.Element` chain. A
  `list<list<list<string>>>` renders `[][][]string` uniformly.
- **Nullable list column** renders `*[]T` (pointer to slice) —
  uniform with C1's nullable-property pointer rule. The alternative
  of a nil slice for null is common Go idiom but conflates two
  distinct signals: "the column is null" vs "the column is a
  non-null empty list". Pointer-to-slice disambiguates: `nil`
  pointer = null column, `&[]T{}` = empty list. Rejected for the
  same reason C1 rejected zero-value-for-nullable-scalars: silent
  ambiguity fails downstream (D3 Resolved's uniform rule).
- **`list<node>` / `list<edge>`** — a column that projects a
  collect of entities, `collect(p)`. The Go type is
  `[]<EntityName>` recursively; the row-assembly walks the
  driver-returned `[]any` element by element, delegating each
  element to the entity decode helper. Fully supported at C3 — the
  entity naming already resolved at Phase Z (C2), and the
  recursion helper reads the same entity-index.
- **`list<unknown>` / `list<scalar null>` / `list<scalar map>`** —
  the leaf's `any` fallback propagates through the recursion, so
  `[]any` / `[][]any` are legitimate. The honest-type posture (D3
  Resolved) means a list of untypeable elements produces a slice
  of `any`.

### 3.3 Scalar / unknown column returns

`ResolvedScalar{bool|int|float|string}` renders `bool` / `int64` /
`float64` / `string` — the driver's native carriers for openCypher
literals appearing in a `RETURN` position. `ResolvedScalar{null}`
renders `any` (always nil at runtime — the openCypher `null`
literal is a legal-but-pointless projection; D3 Resolved's honest
posture). `ResolvedScalar{map}` renders `map[string]any` (ADR
0003's model carries no per-key structure — nothing richer exists
to generate). `ResolvedUnknown` renders `any` — the truthful type
for a column the resolver cannot yet type. Resolver upgrades
silently tighten the emission: when `gqlc-v5t` teaches the
resolver about `elementId(node|edge)` → `string`, the same query
yields `string` where it yielded `any` before, with zero codegen
changes (the Q9 gradient from ADR 0010).

- **A `RETURN 1 AS n :one` query** returns `(int64, error)`. The
  resolver types the `1` literal as `ResolvedScalar{Int}`; C3
  maps that to `int64`.
- **A `RETURN 'x' AS s :one` query** returns `(string, error)`.
- **A `RETURN null AS n :one` query** returns `(any, error)`.
  Legal but pointless — the return is always `nil`. The generated
  method still runs the query; a resolver upgrade could reject
  the projection, but C3's honest posture is that no schema-side
  reason forbids it.
- **A `RETURN {a: 1} AS m :one` query** returns
  `(map[string]any, error)`.
- **A `RETURN foo(x) AS r :one` query** (where the resolver types
  `foo` as unknown) returns `(any, error)`.

### 3.4 Property-side DATE / TIMESTAMP on entity structs

A schema property `p :: DATE` on the `Person` node type emits a
`Person.D dbtype.Date` field; a property `p :: TIMESTAMP NOT NULL`
emits a `time.Time` field. Nullable → pointer. The `models.go`
entity decode helper uses `neo4j.GetProperty[dbtype.Date]` /
`neo4j.GetProperty[time.Time]` on the non-nullable arm; the
nullable arm still reads `Props` directly with a type assertion
(§5.2's shape at C2 stands, extended for the new carrier types).

- **`neo4j.PropertyValue`'s union** (verified 2026-07-11) includes
  `Date | LocalTime | LocalDateTime | Time | Duration | time.Time`
  — every temporal type C3 emits satisfies the generic constraint,
  so `neo4j.GetProperty[dbtype.Date]` and
  `neo4j.GetProperty[time.Time]` compile against the pinned
  driver.
- **`Property.Nullable` still governs pointer vs value** (unchanged).
  A `p :: DATE` (default nullable) renders `*dbtype.Date`; a
  `p :: DATE NOT NULL` renders `dbtype.Date`.

### 3.5 The FLOAT32 schema-width contract

`FLOAT32` is fully supported at C3 (already partially in the C1
table, now fully pinned):

- **Emitted field is `float32`** on entity structs, Row-struct
  fields, Params-struct fields, `:one` / `:many` bare returns.
- **Encode widens losslessly** — a `float32` value passed as a
  method parameter widens to the driver's `float64` at the
  parameter-binding map site: `map[string]any{"x": float64(x)}`.
  No range check needed; `float32` fits in `float64` by definition.
- **Decode narrows by plain conversion** — the row-assembly reads
  `neo4j.GetRecordValue[float64](record, "x")` and narrows via
  `float32(...)`. No range check needed; the schema author
  declared the width, and the store validated writes fit
  (D3 Resolved).

The transformation site is the row-assembly template for column
decodes (§5.5) and the entity decode helper for property decodes
(§5.2, extending C2's `driverCarrier` dispatch). Both sites already
had a "carrier widens to driver-native, narrow on read" pattern at
C1 (the integer-family analog); C3 just extends the same pattern
to FLOAT32.

### 3.6 The C1 `driverOrTx.run` seam signature stands

C3 does not revise the C1 seam signature. The `[]*neo4j.Record`
return type accommodates every C3 column shape unchanged — temporal
columns land as `dbtype.<Kind>` / `time.Time` in the record's value
slice, list columns land as `[]any` (with recursive shape), and
`neo4j.GetRecordValue[T]` resolves against them for every T in the
driver's `RecordValue` union. The compile fence proves both
temporal and list calls type-check against the pinned driver
version.

---

## 4. The naming kernel — C3 additions

C2's naming kernel (§4 of the C2 spec) stands: method names verbatim
(§4.1), Params fields via the one-mangle rule (§4.2), Row fields via
text-shape analysis on `Column.Name` (§4.3), package-level exported-
identifier collision sweep (§4.4 / §4.6 at C2), entity-naming rules
(§4.5 at C2), property-field mangle with collision sentinel (§4.5
Rule 5 at C2). C3 adds the `ResolvedList<T>` recursion rule (§4.7)
and the eager width sweep (§4.8).

### 4.7 `ResolvedList<T>` recursion — the list Go-type rule

D3 Resolved (2026-07-11): `list<T>` → `[]T` recursive. C3 renders
the rule as the `resolvedListGoType` helper (§2.2). The base cases
(non-list leaves) match the C3 property → Go type table (§5.1):

| Leaf `ResolvedType` | Go type |
|---|---|
| `ResolvedProperty{STRING|BOOL|INT*|UINT*|FLOAT*}` | via §5.1 table (property row) |
| `ResolvedProperty{DATE}` | `dbtype.Date` |
| `ResolvedProperty{TIMESTAMP}` | `time.Time` |
| `ResolvedProperty{INT128|INT256|UINT128|UINT256|FLOAT16|FLOAT128|FLOAT256|DECIMAL}` | `ErrUnrepresentableWidth` |
| `ResolvedNode{Labels}` | Phase Z's entity struct name for those labels |
| `ResolvedEdge{EdgeKey}` | Phase Z's entity struct name for that edge |
| `ResolvedEdgeUnion` | `ErrOutOfC3Scope` (C5 owns) |
| `ResolvedTemporal{Kind}` | via §5.1 table (temporal row) |
| `ResolvedScalar{bool|int|float|string}` | `bool` / `int64` / `float64` / `string` |
| `ResolvedScalar{null}` | `any` |
| `ResolvedScalar{map}` | `map[string]any` |
| `ResolvedUnknown` | `any` |
| `ResolvedList{Element}` | `"[]" + resolvedListGoType(Element)` — RECURSE |

- **Recursion terminates** on every non-list variant of the
  `ResolvedType` sum. The total depth is bounded by the resolver's
  depth for that column (which is bounded by the input query's
  syntactic depth). No cycle is possible — `ResolvedList` is a
  container; its `Element` is another `ResolvedType`, not a
  reference back to the list.
- **Leaf-`Unknown` at any depth propagates `any`** — a
  `list<list<unknown>>` renders `[][]any`. The outer slices carry
  concrete Go structure (the recursion depth); only the innermost
  leaf is opaque. This is the "honest type at the leaf" posture
  D3 Resolved pins.
- **Unrepresentable-width leaf is a hard error** — a
  `list<INT128>` column (should the resolver produce one) routes
  to `ErrUnrepresentableWidth` naming the query, column, and
  width. Same sentinel value as the Phase Z schema-shape sweep
  (§4.8) — `errors.Is` consumers do not distinguish the two
  fail-sites, and the fail-message is the discriminator.
- **`ResolvedEdgeUnion` leaf routes to `ErrOutOfC3Scope`** — the
  resolver may produce a `list<edgeUnion>` (a `collect(r)` over a
  multi-candidate edge). C5 will add both the sealed-interface
  emission for the leaf and the list-of-union recursion arm; at
  C3 the column-level check pre-empts the recursion helper (Phase
  A refuses `ResolvedEdgeUnion` at any depth). Rejected: silently
  degrading to `[]any` — the honest type for a resolved-to-a-
  closed-set column is not `any`; the union is knowable, just not
  by C3.

### 4.8 Eager width sweep — Phase Z's C3 extension

**Rule 6 (added at C3) — every entity property's width is
representable, or `ErrUnrepresentableWidth`.** Phase Z's per-
entity property-field derivation pass (§4.5 Rule 5 at C2) adds a
width check: for each property, call `goType(p.Type)`; if `ok` is
false AND the width is one of the eight unrepresentable widths
(INT128, INT256, UINT128, UINT256, FLOAT16, FLOAT128, FLOAT256,
DECIMAL), return `ErrUnrepresentableWidth` naming the entity,
property, and width. Fires eagerly — regardless of whether any
query projects the offending entity — because the sentinel signals
a schema-side unrepresentability that the emitted `models.go`
would surface anyway (an entity struct with an unrepresentable
field cannot compile). The C2 pass already inspected every
schema property; folding the width check in adds one branch and
zero new iterations.

- **Why eager, not lazy at column projection.** The alternatives
  were (a) lazy at Phase A per column, matching C1's original
  posture, or (b) eager in Phase Z, matching C2's
  `ErrUnnamedMultiLabelType` precedent. Picked (b) on ADR 0010 D3
  Resolved's language: a schema-shape rejection makes generation
  fail deterministically regardless of the query set. A lazy
  check would mean: a project with a schema declaring
  `p :: INT128` and no query projecting `p` compiles fine today;
  a project that later adds `RETURN foo.p AS x` fails at that
  query's addition — surprising staging. Phase Z fails at the
  schema; a fix is one schema edit, not a per-query game of
  whack-a-mole. Also: the C1 lazy check produced `ErrOutOfC1Scope`
  because C1 could not yet distinguish "wait for C3" from "never
  representable"; C3 has that distinction and paints the
  unrepresentable widths with the honest sentinel.
- **Temporal property widths (DATE, TIMESTAMP) are representable
  and pass the sweep.** They map via §5.1's table. Only the eight
  widths in the list above trigger `ErrUnrepresentableWidth`.
- **FLOAT32 is representable** and passes the sweep. §5.1 maps it
  to `float32`; §5.5 and §5.7 pin the encode/decode transformation.
- **Query-level fail-site for unrepresentable widths.** A
  parameter with an unrepresentable width still routes to
  `ErrUnrepresentableWidth` at Phase A (a query can declare
  `$x :: INT128` without the schema containing that width
  anywhere). Same sentinel, different fail-message — the
  `errors.Is` consumer sees one axis; the fail-message names the
  query and parameter. Same-sentinel-two-sites is the C2
  precedent (`ErrIdentifierCollision` covers both reserved-
  identifier and cross-source collisions).
- **List-leaf fail-site for unrepresentable widths** (§4.7 above).
  The `resolvedListGoType` helper delegates to `goType`; a false
  return with an unrepresentable width wraps
  `ErrUnrepresentableWidth`. Third fail-site for the same
  sentinel; same-message discipline.

The C1 sentinel `ErrOutOfC2Scope` (the C1 name at C1, `ErrOutOfC2Scope`
at C2) was the catchment for column projections with unrepresentable
widths at C1/C2. C3 splits it: the widths that will NEVER be
representable retire to `ErrUnrepresentableWidth`; the widths that
would be representable but were merely deferred (DATE, TIMESTAMP)
retire because C3 handles them. `ErrOutOfC2Scope` renames to
`ErrOutOfC3Scope` for the residue (writes, edgeUnion, backtick
query text — §9's list).

---

## 5. Emission templates and per-query files — C3 additions

### 5.1 Property → Go type mapping — the C3 rows

The C2 property → Go type table stands unchanged for STRING / BOOL,
the representable integer widths, and FLOAT32 / FLOAT64. C3 extends
the property side with the temporal rows and the sentinel rows, and
adds the five new column-shape rows.

**Property side** (extends the C1 table's property rows):

| `ResolvedProperty.Type` | Go type | Import |
|---|---|---|
| `DATE` | `dbtype.Date` | `dbtype` |
| `TIMESTAMP` | `time.Time` | `time` |
| `INT128` / `INT256` / `UINT128` / `UINT256` | `ErrUnrepresentableWidth` | — |
| `FLOAT16` | `ErrUnrepresentableWidth` | — |
| `FLOAT128` / `FLOAT256` | `ErrUnrepresentableWidth` | — |
| `DECIMAL` | `ErrUnrepresentableWidth` | — |
| `FLOAT32` | `float32` (schema-width contract; encode widen / decode narrow, §5.5, §5.7) | — |

**Column-shape side** (extends the C2 table's column rows):

| `Column.Type` | Go type | Nullable emission |
|---|---|---|
| `ResolvedTemporal{Date}` | `dbtype.Date` | `*dbtype.Date` |
| `ResolvedTemporal{Time}` | `dbtype.Time` | `*dbtype.Time` |
| `ResolvedTemporal{LocalTime}` | `dbtype.LocalTime` | `*dbtype.LocalTime` |
| `ResolvedTemporal{DateTime}` | `time.Time` | `*time.Time` |
| `ResolvedTemporal{LocalDateTime}` | `dbtype.LocalDateTime` | `*dbtype.LocalDateTime` |
| `ResolvedTemporal{Duration}` | `dbtype.Duration` | `*dbtype.Duration` |
| `ResolvedScalar{Bool}` | `bool` | `*bool` |
| `ResolvedScalar{Int}` | `int64` | `*int64` |
| `ResolvedScalar{Float}` | `float64` | `*float64` |
| `ResolvedScalar{String}` | `string` | `*string` |
| `ResolvedScalar{Null}` | `any` | `any` (pointer-to-any is silly; nullable stays `any`) |
| `ResolvedScalar{Map}` | `map[string]any` | `map[string]any` (nil-map is null-map; pointer redundant) |
| `ResolvedUnknown` | `any` | `any` (same reasoning as null; nullable stays `any`) |
| `ResolvedList{Element}` | `[]` + recurse via §4.7 | `*[]T` |

- **`any` and `map[string]any` do not accept the nullable-pointer
  rule.** The value `nil` is already a legitimate zero for both;
  wrapping in a pointer adds ceremony with no gain (a `*any` field
  is `nil` iff the outer pointer is nil OR the outer pointer points
  at nil — two-level indirection for the same signal). The
  nullable-column check at row assembly still runs, but its "null
  arrived" branch assigns `nil` to the `any` field directly.
  Rejected: forcing `*any` for uniformity — the D3 Resolved rule
  is "nullable → pointer, uniformly" only where the underlying
  type has no null representation of its own; `any` and `map` do,
  and forcing `*any` would violate the rule's own rationale.
- **`ResolvedTemporal{Time}` maps to `dbtype.Time`, not `time.Time`**
  — openCypher TIME is zoned time-of-day (no date), which
  `dbtype.Time` faithfully represents (an alias for `time.Time`
  underneath, but distinguishing zoned time-of-day from zoned
  datetime at the type level).
  `ResolvedTemporal{DateTime}` is the one that maps to bare
  `time.Time`: the driver's v5 documented behavior for zoned
  DATETIME (see `graph.go` comment
  `time.Time | /* OffsetTime == Time == dbtype.Time */`).
  Confirmed against the driver source, 2026-07-11.
- **`ResolvedList` renders `[]T` via the recursion helper** — the
  Go-type text is prepended `"[]"` and the element type is
  computed by `resolvedListGoType` (§2.2). Nullable list → `*[]T`
  (uniform pointer rule applies here because a nil slice and a
  null column are distinguishable signals; §3.2).
- **Still deferred at C3** (Phase A routes to `ErrOutOfC3Scope`):
  - `ResolvedEdgeUnion` column — C5 owns (sealed interface, ADR
    0010 D3 Resolved).
  - Non-`ResolvedProperty` parameter (whole node, whole edge,
    temporal expression, scalar literal, list, unknown) — C3
    keeps parameter admission at property widths only, extended
    to temporal property widths (DATE, TIMESTAMP). Widening the
    parameter axis further is post-v1.
  - `:exec` cardinality — C4.
  - Query text carrying a Go raw-string-hostile backtick — C4-or-
    later.

### 5.2 `models.go` — the C3 additions

C2's `renderModels` walks Phase Z's cache emitting one struct + one
decode helper per entity in `Schema.Nodes` `LabelSetKey`-order then
`Schema.Edges` `EdgeKey`-triple-lex-order. C3 revises the emission
in three places:

1. **Import invariants** (extending §2.4). The import block gates
   are recomputed by the C3 emission walk:

   ```go
   anyDBType   := len(entities) > 0  // helper argument types (unchanged)
   anyProp     := <any entity has any property> (unchanged)
   anyNonNull  := <any entity has any non-nullable property> (unchanged)
   anyTime     := <any property decodes as time.Time>  // NEW at C3
   ```

   The `models.go` import block:

   ```go
   import (
       "fmt"     // iff anyProp
       "time"    // iff anyTime  — NEW at C3
       "github.com/neo4j/neo4j-go-driver/v5/neo4j"          // iff anyNonNull
       "github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"   // iff anyDBType (equiv. len(entities) > 0)
   )
   ```

   Alphabetical order (`goimports`): `fmt`, `time`, then the
   external `neo4j` / `dbtype`. The template renders the block in
   a single-group `import ()` — `time` slots between `fmt` and
   `neo4j`.

2. **Property decode template** — the C2 non-nullable arm ran
   `neo4j.GetProperty[<carrier>](arg, key)` with `<carrier>` from
   C1's `driverCarrier` dispatch (int64 / float64 / string / bool
   passthrough). C3 extends `driverCarrier` for the new property
   types:

   | Emitted Go type | Driver carrier | Narrow expression |
   |---|---|---|
   | `dbtype.Date` | `dbtype.Date` | none — passthrough |
   | `time.Time` (TIMESTAMP) | `time.Time` | none — passthrough |
   | `float32` (FLOAT32) | `float64` | `float32(local)` |

   The nullable arm still reads `Props` directly with a type
   assertion. `dbtype.Date` and `time.Time` are legitimate type-
   assertion targets on the `Props` map (both satisfy
   `neo4j.PropertyValue`).

3. **The `time.Time` case for TIMESTAMP** — the emitted decode
   helper body for a `p :: TIMESTAMP NOT NULL` property on
   `Person`:

   ```go
   p, err := neo4j.GetProperty[time.Time](node, "p")
   if err != nil {
       return Person{}, fmt.Errorf("decode Person.P: %w", err)
   }
   out.P = p
   ```

   For a `p :: TIMESTAMP` (nullable):

   ```go
   if v, ok := node.Props["p"]; ok {
       s, ok := v.(time.Time)
       if !ok {
           return Person{}, fmt.Errorf("decode Person.P: property %q: expected time.Time, got %T", "p", v)
       }
       out.P = &s
   }
   ```

   The `dbtype.Date` cases are structurally identical, substituting
   the type name.

4. **The `float32` case for FLOAT32 property** — the emitted decode
   helper body for a `p :: FLOAT32 NOT NULL` property on `Person`:

   ```go
   p, err := neo4j.GetProperty[float64](node, "p")
   if err != nil {
       return Person{}, fmt.Errorf("decode Person.P: %w", err)
   }
   out.P = float32(p)
   ```

   The nullable arm reads `Props`, asserts `float64`, narrows via
   conversion, and assigns the address of a `float32` local (not a
   `float64` local — the entity struct field is `float32`, so the
   pointer type must match). C2's `writeEntityFieldDecode` already
   emits the narrow-if-carrier-differs branch; C3 just widens the
   `driverCarrier` mapping.

**Iteration order and Phase Z eager sweep** — the emission walk
still runs in `Schema.Nodes` `LabelSetKey`-order then `Schema.Edges`
`EdgeKey`-triple-lex-order; per-entity properties still walk in
map-key-sorted order. Phase Z's width sweep (§4.8) fires before
the emission walk, so a schema with `p :: INT128` never reaches
`renderModels`.

### 5.3 Method rendering into `db.go` — unchanged

C2's `db.go` template stands byte-identical at C3. The per-method
body renders in `<name>.cypher.go` (§5.5 below); `db.go` carries
only the `Queries` handle + constructors + `driverOrTx` interface +
`driverDB` / `txDB` implementations + `ErrNoRows` /
`ErrMultipleResults` when a `:one` query is present.

### 5.4 `querier.go` regeneration — unchanged

C2's `querier.go` template stands byte-identical at C3. The
compile-time `Querier = (*Queries)(nil)` assertion still fences
drift; entity structs are top-level identifiers, not interface
members, so `querier.go` sees no diff from the C3 emissions.

### 5.5 The per-source `<name>.cypher.go` file — the C3 column arms

C2's per-source file shape stands: query-text const, Params struct
(if two-plus params), Row struct (if two-plus columns), method
with row-assembly body. C3 extends the row-assembly body's per-
column decode block with three new column arms:

**Per-column decode block — temporal column, non-nullable:**

```go
value, isNil, err := neo4j.GetRecordValue[dbtype.Date](records[0], "<column-name>")
if err != nil {
    return <zero>, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
}
if isNil {
    return <zero>, fmt.Errorf("<method>: column %q is non-nullable but arrived null", "<column-name>")
}
<assign-or-return>
```

The carrier changes per temporal kind: `dbtype.Date`, `dbtype.Time`,
`dbtype.LocalTime`, `time.Time`, `dbtype.LocalDateTime`,
`dbtype.Duration`. Every one satisfies `neo4j.RecordValue` (verified
against v5.28.4 `neo4j/record.go`, 2026-07-11). The property-side
`ResolvedProperty{DATE}` and `{TIMESTAMP}` arms use the same
carriers: `dbtype.Date` for DATE, `time.Time` for TIMESTAMP.

**Per-column decode block — list column, non-nullable:**

```go
value, isNil, err := neo4j.GetRecordValue[[]any](records[0], "<column-name>")
if err != nil {
    return <zero>, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
}
if isNil {
    return <zero>, fmt.Errorf("<method>: column %q is non-nullable but arrived null", "<column-name>")
}
out := make([]<ElementGoType>, 0, len(value))
for i, elem := range value {
    <element decode → append>
}
<assign-or-return>  // uses out
```

The element-decode step is one dispatch on the resolved element
type: a scalar element goes through a type assertion + narrow
convert; a temporal element goes through a type assertion; an
entity element (`ResolvedNode` / `ResolvedEdge` leaf) goes through
the entity decode helper with a `dbtype.Node` / `dbtype.Relationship`
type assertion; a nested `ResolvedList` element recurses with an
inner loop. The recursion depth in the emitted code matches the
recursion depth in `resolvedListGoType` — one loop level per
`ResolvedList` layer.

- **Element type assertion.** The driver's `[]any` carries per-
  element `any` values whose runtime types match the driver's
  `RecordValue` set (bool, int64, float64, string, temporals,
  `dbtype.Node`/`Relationship`, `[]any` for nested lists, etc.).
  The type assertion is `elem.(int64)` / `elem.(dbtype.Date)` /
  etc. Type-assertion failure → decode error naming the column,
  element index, and observed Go type.
- **Element `any` at the leaf** (a `list<unknown>` / `list<scalar
  null>` / `list<scalar map>`) skips the type assertion entirely
  and appends `elem` directly — the loop degenerates to
  `out = append(out, elem)`. Same shape a hand-written
  `[]any` decoder would take.
- **Nested list element** (`list<list<T>>`) — the outer loop's
  element is `[]any`; the inner loop unpacks it. The recursion
  helper produces `[][]T`; the code helper produces two nested
  `for` loops.

**Per-column decode block — scalar / unknown column, non-nullable:**

```go
value, isNil, err := neo4j.GetRecordValue[<carrier>](records[0], "<column-name>")
if err != nil {
    return <zero>, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
}
if isNil {
    return <zero>, fmt.Errorf("<method>: column %q is non-nullable but arrived null", "<column-name>")
}
<assign-or-return>
```

The carrier per scalar kind: `bool` for `ScalarBool`, `int64` for
`ScalarInt`, `float64` for `ScalarFloat`, `string` for `ScalarString`,
`map[string]any` for `ScalarMap`. `ScalarNull` and `ScalarUnknown`
never reach a typed decode — the emitted Go type is `any`, and the
column decode reduces to `record.Get(key)` returning the raw driver
value (either `nil` or whatever the driver put there). To keep the
`GetRecordValue` shape uniform, the emitted code uses
`neo4j.GetRecordValue[map[string]any]` when the resolved type is
`ResolvedScalar{Map}` (because `map[string]any` satisfies
`RecordValue`) and drops back to `record.Get(key)` for `any` /
`unknown` columns — the driver-native lookup that returns `any`
without a type constraint.

- **`record.Get` for `any`-columns** — `Get` returns `(any, bool)`
  where the bool is "found" (not "null"). C3's emission for an
  `any` column:

  ```go
  v, ok := records[0].Get("<column-name>")
  if !ok {
      return <zero>, fmt.Errorf("<method>: decode column %q: key not found", "<column-name>")
  }
  <assign-or-return>  // uses v (any)
  ```

  Same fail-message shape as `GetRecordValue`, distinct message
  because the driver's "found" and "type-mismatch" are different
  signals for `Get` vs `GetRecordValue`.
- **Nullable scalar arriving null** — the `isNil` branch of
  `GetRecordValue` handles it uniformly with the temporal arm
  (§5.5 above): set the pointer field to `nil`. For an `any`
  column decoded via `Get`, a `nil` value assigns directly (no
  pointer wrap; the nullable/non-nullable distinction is silent
  for `any`-typed columns because `any`'s zero is `nil`).

**FLOAT32 column decode:** for a `ResolvedProperty{FLOAT32}` column,
the carrier is `float64` (uniform with C1's integer-widen pattern)
and the narrow step is `float32(value)`:

```go
value, isNil, err := neo4j.GetRecordValue[float64](records[0], "<column-name>")
if err != nil {
    return <zero>, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
}
if isNil {
    return <zero>, fmt.Errorf("<method>: column %q is non-nullable but arrived null", "<column-name>")
}
row.X = float32(value)
```

Same site as C1's `driverCarrier`; C3 extends the switch to include
FLOAT32 → `float64` narrow. Encode is symmetric (§5.7).

- **`dbtype` / `time` import in `<name>.cypher.go`** — extended from
  C2's "any entity column" rule to cover the new column kinds:
  - `dbtype` iff any column in the file decodes through a
    `dbtype.<Kind>` carrier (entity column, DATE property, or a
    temporal column whose kind is Date / Time / LocalTime /
    LocalDateTime / Duration), OR any list column whose leaf uses
    `dbtype.<Kind>`.
  - `time` iff any column in the file decodes as `time.Time`
    (TIMESTAMP property, TemporalDateTime column, or a list column
    whose leaf is either of the two).
  A property-column-only file (C1's fixture universe) still keeps
  C1's import set.
- **`:many` list / temporal / scalar column** wraps the arm inside
  the per-record `for` loop; nothing structurally new — the arm
  substitutes into the loop body's per-column slot.

**Example — `:one`, single duration column, non-nullable:**

```go
func (q *Queries) OneWeek(ctx context.Context) (dbtype.Duration, error) {
    records, err := q.db.run(ctx, oneWeekQueryText, nil, neo4j.AccessModeRead)
    if err != nil {
        return dbtype.Duration{}, err
    }
    if len(records) == 0 {
        return dbtype.Duration{}, ErrNoRows
    }
    if len(records) > 1 {
        return dbtype.Duration{}, ErrMultipleResults
    }
    value, isNil, err := neo4j.GetRecordValue[dbtype.Duration](records[0], "d")
    if err != nil {
        return dbtype.Duration{}, fmt.Errorf("OneWeek: decode column %q: %w", "d", err)
    }
    if isNil {
        return dbtype.Duration{}, fmt.Errorf("OneWeek: column %q is non-nullable but arrived null", "d")
    }
    return value, nil
}
```

**Example — `:one`, single `list<int64>` column, non-nullable:**

```go
func (q *Queries) OneListInt(ctx context.Context) ([]int64, error) {
    records, err := q.db.run(ctx, oneListIntQueryText, nil, neo4j.AccessModeRead)
    if err != nil {
        return nil, err
    }
    if len(records) == 0 {
        return nil, ErrNoRows
    }
    if len(records) > 1 {
        return nil, ErrMultipleResults
    }
    value, isNil, err := neo4j.GetRecordValue[[]any](records[0], "xs")
    if err != nil {
        return nil, fmt.Errorf("OneListInt: decode column %q: %w", "xs", err)
    }
    if isNil {
        return nil, fmt.Errorf("OneListInt: column %q is non-nullable but arrived null", "xs")
    }
    out := make([]int64, 0, len(value))
    for i, elem := range value {
        v, ok := elem.(int64)
        if !ok {
            return nil, fmt.Errorf("OneListInt: decode column %q element %d: expected int64, got %T", "xs", i, elem)
        }
        out = append(out, v)
    }
    return out, nil
}
```

**Owner directive (2026-07-11) — lint-clean parity.** Every
emitted `models.go` and `<name>.cypher.go` must lint-clean under
gqlc's `.golangci.yml` (C1 §6.6, C2 §5.5's directive extends).
The C3 emissions are structurally identical to C1/C2 modulo the
new carriers, and the `.golangci.yml` posture (no `//nolint`
directives at file heads) still holds. `errorlint` wrapping
discipline: every new `fmt.Errorf` in §5.5 uses `%w` for
propagated errors and lowercase prefix. `stylecheck` posture: the
type-assertion error message ("expected `<Type>`, got `%T`")
uses lowercase, no punctuation. C3-specific concern: the list-
column loop reads `elem` and asserts to the element type; the
`for i, elem := range value` uses idiomatic Go — `errcheck` and
`ineffassign` pass by construction.

### 5.6 The `driverOrTx.run` seam signature — unchanged from C1

C3 does not revise C1's seam signature. `[]*neo4j.Record` carries
temporal columns as `dbtype.<Kind>` / `time.Time`, list columns as
`[]any` (recursive), scalar/unknown columns as their driver-native
Go values inside the record's value slice.
`neo4j.GetRecordValue[T]` resolves against every T in the
`RecordValue` union (verified above). No template changes to
`db.go`.

### 5.7 FLOAT32 encode-widen — the parameter binding site

The Params-struct field for a `$x :: FLOAT32` parameter is
`float32`. The parameter-binding map site widens to `float64` in
the emitted method body:

```go
records, err := q.db.run(ctx, methodQueryText, map[string]any{"x": float64(x)}, neo4j.AccessModeRead)
```

- **Widening site is the emitted method body**, not a wrapper. The
  C2 `paramsMapText` helper composes the map literal from the
  Params-struct fields; C3 extends it with a per-field carrier
  cast: FLOAT32 → `float64(x)` widen, temporal / DATE / TIMESTAMP
  passthrough (the driver accepts `dbtype.Date` / `time.Time` in
  parameter bindings), every other type unchanged. Same site
  covers the single-parameter method form (`arg` directly) and the
  Params-struct multi-parameter form (`arg.X` accessed).
- **Nullable FLOAT32 parameter** — `*float32` field, nil pointer
  emits Cypher `null` (unchanged from C1's uniform nullable
  encoding), non-nil pointer widens the pointed-to value:
  `map[string]any{"x": float64(*x)}`. The nil-check runs at the
  parameter-binding site; nil-map entry (`nil` value in the
  `map[string]any`) is the driver's Cypher `null` binding.
- **Decode narrow site is the row-assembly** (§5.5). Symmetric
  with encode: `neo4j.GetRecordValue[float64]` + `float32(value)`
  narrow. Both sites use plain Go conversion; no range check
  (D3 Resolved: the schema author declared the width).
- **`float64` FLOAT / FLOAT64 parameters** encode with no cast —
  the driver's parameter binding accepts `float64` directly. The
  C2 `paramsMapText` per-field dispatch handles this by leaving
  the field unwrapped.

---

## 6. The golden harness — C3 revision

C0 §6's harness stands: the `test/data/codegen/{valid,invalid}`
layout, the nested Go module, the `manifest.json` shape, the
`-update` flag, the testify suites, the compile fence. C2 §6.6's
lint parity applies transitively. C3 revises the fixture set only,
not the harness code.

### 6.1 Fixture strategy

C2's discipline stands (fixture-per-capability, one schema per
fixture). C3 adds valid fixtures for each capability slice of §5
plus a small set of negative fixtures for each new sentinel /
retirement. The nested module's `go.mod` (pin
`neo4j-go-driver/v5 v5.28.4`) does not change — the C3 emissions
add `time` (stdlib) and use existing `dbtype` sub-package types.

**Existing C2 valid fixtures whose `models.go` regenerates** —
none. C2's fixtures use property widths STRING / BOOL / INT* /
UINT* / FLOAT32 / FLOAT64 for entity properties; none use DATE /
TIMESTAMP or the unrepresentable widths, so their emitted
`models.go` bytes stay identical. Their `<name>.cypher.go` files
similarly do not regenerate. C3's -update run produces no diff on
the C2 fixture set. This is a clean staging property — C3 widens
without perturbing C2 goldens.

### 6.2 C3 valid fixtures

Under `test/data/codegen/valid/`, each new directory holds a
`schema.gql`, zero or more `.cypher` files, a `manifest.json`, and
a `golden/` subdirectory with the complete generated package:

| Fixture | Coverage |
|---|---|
| `temporal_column_date` | `RETURN date() AS d :one` — `ResolvedTemporal{Date}` column. Exercises the smallest temporal column surface. Emits `import "github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"` in `<name>.cypher.go`. |
| `temporal_column_datetime` | `RETURN datetime() AS now :one` — `ResolvedTemporal{DateTime}` column. Exercises the `time.Time` mapping. Emits `import "time"` in `<name>.cypher.go`. |
| `temporal_column_localtime` | `RETURN localtime() AS t :one` — `ResolvedTemporal{LocalTime}` column. |
| `temporal_column_localdatetime` | `RETURN localdatetime() AS ldt :one` — `ResolvedTemporal{LocalDateTime}` column. |
| `temporal_column_duration` | `RETURN duration({days: 7}) AS d :one` — `ResolvedTemporal{Duration}` column. Guards against `time.Duration` regression. |
| `temporal_column_time` | `RETURN time() AS t :one` — `ResolvedTemporal{Time}` column. |
| `property_date` | `Person.dob :: DATE NOT NULL` schema property with no query — models-only adoption. `Person.Dob dbtype.Date`. |
| `property_timestamp` | `Person.updatedAt :: TIMESTAMP` (nullable) schema property. `Person.UpdatedAt *time.Time`. Exercises the `time` import in `models.go`. |
| `float32_column` | `Person.height :: FLOAT32 NOT NULL` + `RETURN p.height AS h :one`. Exercises the encode-widen / decode-narrow contract on both entity property and column. |
| `float32_parameter` | `MATCH (p:Person {height: $h}) RETURN p :one` with `$h :: FLOAT32`. Exercises the parameter-binding `float64(x)` widen. |
| `list_int` | `RETURN [1, 2, 3] AS xs :one` — `ResolvedList{ResolvedScalar{Int}}` column. |
| `list_string` | `RETURN [p.name] AS xs :one` — `ResolvedList{ResolvedProperty{STRING}}` column via list-of-property projection. |
| `list_list_int` | `RETURN [[1], [2, 3]] AS xss :one` — nested list. Exercises the recursion helper's depth. |
| `list_entity` | `MATCH (p:Person) WITH collect(p) AS ps RETURN ps :one` — list of entities. Exercises the entity decode helper inside a list loop. |
| `list_nullable` | Nullable list column — `*[]T` field emission. |
| `list_unknown` | `RETURN [foo(x)] AS xs :one` — list-of-unknown → `[]any`. |
| `scalar_null` | `RETURN null AS n :one` — `any` return, always nil. |
| `scalar_map` | `RETURN {a: 1} AS m :one` — `map[string]any` return. |
| `unknown_column` | `RETURN foo(x) AS r :one` (foo unknown to resolver) — `any` return via `record.Get`. |
| `many_columns_mixed_temporal_list` | `:one` with three columns: an entity, a temporal, a list. Exercises the multi-column row assembly's varied arms. |

Twenty new valid fixtures. Each is one `schema.gql` (some with
zero `.cypher` files — models-only adoption), a `manifest.json`,
and a `golden/` tree. The `golden/` trees compile under the C0
compile fence against `neo4j-go-driver/v5 v5.28.4` — every
`GetRecordValue`, `GetProperty`, and type-assertion invocation
type-checks against the pinned driver.

### 6.3 Schema fixture text — illustrative

`test/data/codegen/valid/temporal_column_duration/schema.gql`:

```gql
CREATE PROPERTY GRAPH TYPE TemporalColumnDuration AS {
    (:Person {
        id :: INT64 NOT NULL
    })
}
```

Paired query file
`test/data/codegen/valid/temporal_column_duration/queries.cypher`:

```cypher
// name: OneWeek :one
RETURN duration({days: 7}) AS d
```

Resolved: `Columns = [{Name: "d", Type: ResolvedTemporal{Duration}}]`,
`Parameters = []`, `Statement = read`. Phase Z admission: `Person`
node type with `Id` int64 property emits fine (no width sentinel).
Phase A admission: one column, `Column.Type` is
`ResolvedTemporal{Duration}` — admissible under C3's widened
column-shape set. Phase B derivation: `d` matches bare-identifier
shape → `D` field name candidate; single column → bare-value
return (§3.1 of C1): the method returns `(dbtype.Duration, error)`,
no Row struct. Emitted method matches §5.5's example above; and
`models.go` for this fixture carries `Person` with `Id int64` plus
`decodePerson` (no `time` import — only the entity is affected;
the Duration column lives in the cypher file's imports).

`test/data/codegen/valid/list_entity/schema.gql`:

```gql
CREATE PROPERTY GRAPH TYPE ListEntity AS {
    (:Person {
        id   :: INT64 NOT NULL,
        name :: STRING NOT NULL
    })
}
```

`queries.cypher`:

```cypher
// name: AllPeople :one
MATCH (p:Person) WITH collect(p) AS ps RETURN ps
```

Resolved: `Columns = [{Name: "ps", Type: ResolvedList{Element:
ResolvedNode{Labels: "Person"}}}]`. Phase B derivation:
`resolvedListGoType` recurses one step: element is
`ResolvedNode{"Person"}`, which looks up the entity index for
`Person` and returns `"Person"`; the outer list prepends `"[]"` to
produce `[]Person`. Emitted method returns `([]Person, error)`;
the row assembly's list arm loops the driver's `[]any`, type-
asserts each element to `dbtype.Node`, and delegates each to
`decodePerson`:

```go
value, isNil, err := neo4j.GetRecordValue[[]any](records[0], "ps")
if err != nil {
    return nil, fmt.Errorf("AllPeople: decode column %q: %w", "ps", err)
}
if isNil {
    return nil, fmt.Errorf("AllPeople: column %q is non-nullable but arrived null", "ps")
}
out := make([]Person, 0, len(value))
for i, elem := range value {
    node, ok := elem.(dbtype.Node)
    if !ok {
        return nil, fmt.Errorf("AllPeople: decode column %q element %d: expected dbtype.Node, got %T", "ps", i, elem)
    }
    v, err := decodePerson(node)
    if err != nil {
        return nil, fmt.Errorf("AllPeople: decode column %q element %d: %w", "ps", i, err)
    }
    out = append(out, v)
}
return out, nil
```

### 6.4 C3 invalid fixtures — the new + renamed set

Added under `test/data/codegen/invalid/`:

| Fixture | Sentinel | Coverage |
|---|---|---|
| `unrepresentable_width_int128_schema` | `ErrUnrepresentableWidth` | Schema property `p :: INT128` on a node type — Phase Z's eager sweep fires without a query projecting it. |
| `unrepresentable_width_uint256_schema` | `ErrUnrepresentableWidth` | `p :: UINT256` on an edge type. |
| `unrepresentable_width_float16_schema` | `ErrUnrepresentableWidth` | `p :: FLOAT16`. |
| `unrepresentable_width_decimal_schema` | `ErrUnrepresentableWidth` | `p :: DECIMAL`. |
| `unrepresentable_width_int128_parameter` | `ErrUnrepresentableWidth` | Query with `$x :: INT128` parameter (no schema property of that width). Phase A fail-site. |
| `unrepresentable_width_float128_list_column` | `ErrUnrepresentableWidth` | Query with a `list<FLOAT128>` column. `resolvedListGoType` fail-site. |
| `out_of_c3_scope_edge_union` | `ErrOutOfC3Scope` | Query projecting a `ResolvedEdgeUnion` column — deferred to C5. Renamed from `out_of_c2_scope_edge_union`. |
| `out_of_c3_scope_exec` | `ErrOutOfC3Scope` | `:exec` cardinality — deferred to C4. Renamed from `out_of_c2_scope_exec`. |
| `out_of_c3_scope_non_property_parameter` | `ErrOutOfC3Scope` | `$p :: ResolvedNode` — non-property parameter, still out of scope. |

Nine invalid fixtures — six for `ErrUnrepresentableWidth` (three
axes: schema, parameter, list-leaf) plus three for the renamed
`ErrOutOfC3Scope`. The retired `out_of_c2_scope_int128` fixture
(the C2 sentinel for the C1/C2 catchment) reappears here as
`unrepresentable_width_int128_schema` (the honest sentinel);
`out_of_c2_scope_scalar_return` retires (scalar columns are now
in-scope) and its schema graduates into the `valid/` set as
whichever scalar variant it exercised
(e.g., `RETURN 1 AS n` → the `scalar_int` case, covered as an
implicit sub-case of the `list_int` fixture — no separate
scalar_int fixture is needed because the leaf mapping is the
same).

The C0 / C1 / C2 invalid fixtures whose sentinel is unchanged
(`invalid_package_name`, `duplicate_query_name`,
`duplicate_source_file`, `invalid_cardinality`,
`param_name_collision`, `row_field_collision`,
`alias_required_function_call`, `alias_required_expression`,
`identifier_collision_reserved`, `invalid_entity_name_node`,
`invalid_entity_name_edge`, `unnamed_multi_label_type`,
`property_field_collision`, `identifier_collision_entity_row`)
stay unchanged.

### 6.5 Determinism — C3 additions

C0's `TestDoubleRun` runs unchanged. C3's kernel adds no new
ordered surfaces beyond Phase Z's width sweep, which piggy-backs
on the existing map-key-sorted property iteration. The
`resolvedListGoType` helper is a pure recursion on the
`ResolvedType` sum — no map iteration. The row-assembly's list
arm iterates the driver's `[]any` in slice order (the driver's
own determinism); the emitted code carries no ordering axis of
its own. Every ordered surface is either the resolver's
guaranteed order, C2's Phase Z sort, or the recursion depth.

### 6.6 Non-obvious harness invariants — C3 additions

C2's §6.6 invariants stand. C3 adds:

- **Every valid fixture's `golden/models.go` and
  `golden/<name>.cypher.go` compile with the pinned driver's
  `dbtype`, `RecordValue`, and `PropertyValue` unions.**
  `test/data/codegen/go.mod` still pins
  `neo4j-go-driver/v5 v5.28.4`; the emitted files import
  `github.com/neo4j/neo4j-go-driver/v5/neo4j` for
  `GetRecordValue` / `GetProperty` and the `dbtype` sub-package
  for the temporal cell types. Both unions include the six
  temporal shapes (`dbtype.Date`, `dbtype.Time`, `dbtype.LocalTime`,
  `dbtype.LocalDateTime`, `dbtype.Duration`, `time.Time`) plus
  `[]any` and `map[string]any` (verified against
  `/neo4j/record.go` and `/neo4j/graph.go`, 2026-07-11). The
  compile fence catches drift at the version bump.
- **The `time` package import is stdlib** — every emitted
  `models.go` / `<name>.cypher.go` that emits a `time.Time`
  carrier imports `"time"`. Deterministic gating on schema shape:
  a schema with no TIMESTAMP property + no query projecting
  `TemporalDateTime` / TIMESTAMP list-leaf → no `time` import;
  otherwise `time` in the group. Alphabetical placement:
  `fmt` < `time` < the external `neo4j` / `dbtype`.
- **List loops use `for i, elem := range value`** — the emitted
  loop variable pair is idiomatic; `errcheck` / `ineffassign` /
  `stylecheck` pass by construction. The type-assertion fail-
  message uses the loop index (`element %d`) so a fixture whose
  input contains a wrong-type element decodes to a diagnostic
  naming the bad index. Fixtures: element type mismatch is not a
  golden-testable case (the driver never returns wrong-type
  elements in a valid `RecordValue`), so it lives as a code-cycle
  unit test, not a fixture.
- **Owner directive (C1 §6.6, C2 §5.5, 2026-07-11) extends
  transitively.** The `errorlint` + `stylecheck` posture holds:
  every new `fmt.Errorf` in §5.5 uses `%w` for wrap, lowercase
  prefix, no ending punctuation. The type-assertion error
  ("expected int64, got %T") is lowercase. C3-specific concern:
  the list arm's inner loop is emitted at the standard method-
  body indent + one extra tab; `gofmt` normalises whitespace on
  the way out of `format.Source`, so the golden bytes are
  reproducible.

---

## 7. C3 capability scope — what emits

**In scope:** an `Input` whose:

- `Schema.Nodes` and `Schema.Edges` produce entity struct names via
  Rules 1–4 without failure (C2 invariant), and every property on
  every entity has a representable width (Rule 6 at §4.8; the eight
  unrepresentable widths trigger `ErrUnrepresentableWidth`).
- `Schema.Nodes` and `Schema.Edges` property field derivations are
  collision-free per entity (C2 invariant).
- Every `NamedQuery` still satisfies C1's admission + C2's
  entity-column admission (per §7 of the C1/C2 specs) with the
  following widening: `Columns[i].Type` may now be
  `ResolvedProperty` (STRING / BOOL / INT* / UINT* / FLOAT32 /
  FLOAT64 / DATE / TIMESTAMP), `ResolvedNode`, `ResolvedEdge`,
  `ResolvedList<T>` (recursive per §4.7), `ResolvedTemporal`,
  `ResolvedScalar` (any kind), `ResolvedUnknown`. Unrepresentable
  property widths on columns and parameters still route to
  `ErrUnrepresentableWidth`; the schema-side eager sweep at Phase
  Z catches them ahead of any query using them.
- Parameter admissibility widens for temporal property widths:
  `$since :: TIMESTAMP` and `$dob :: DATE` are admissible. Non-
  property parameters still route to `ErrOutOfC3Scope`.

**Out of scope, routed to the appropriate sentinel:**

| Construct                                                    | Sentinel                     | Stage owner |
|--------------------------------------------------------------|------------------------------|-------------|
| `ResolvedEdgeUnion` column                                   | `ErrOutOfC3Scope`            | C5          |
| Non-property parameter (whole node/edge, temporal literal, etc.) | `ErrOutOfC3Scope`        | Post-v1    |
| `CardinalityExec`                                            | `ErrOutOfC3Scope`            | C4          |
| Query text containing a Go raw-string-hostile backtick       | `ErrOutOfC3Scope`            | C4-or-later |
| `ResolvedProperty` column / parameter with INT128 / INT256   | `ErrUnrepresentableWidth`    | —           |
| `ResolvedProperty` column / parameter with UINT128 / UINT256 | `ErrUnrepresentableWidth`    | —           |
| `ResolvedProperty` column / parameter with FLOAT16           | `ErrUnrepresentableWidth`    | —           |
| `ResolvedProperty` column / parameter with FLOAT128 / FLOAT256 | `ErrUnrepresentableWidth`  | —           |
| `ResolvedProperty` column / parameter with DECIMAL           | `ErrUnrepresentableWidth`    | —           |
| Schema property with any of the eight unrepresentable widths | `ErrUnrepresentableWidth`    | —           |
| `list<T>` with an unrepresentable leaf                        | `ErrUnrepresentableWidth`    | —           |
| Explicit `NodeType.Name` / `EdgeType.Name` not a valid ident | `ErrInvalidEntityName`       | —           |
| Multi-label node type without explicit `Name`                | `ErrUnnamedMultiLabelType`   | —           |
| Ambiguous edge label without explicit `Name`                 | `ErrUnnamedMultiLabelType`   | —           |
| Two properties on one entity mangling to one field           | `ErrPropertyFieldCollision`  | —           |
| Method name matches reserved identifier                      | `ErrIdentifierCollision`     | —           |
| Two params mangling to one field                             | `ErrParamNameCollision`      | —           |
| Two columns deriving to one Row field                        | `ErrRowFieldCollision`       | —           |
| Column text neither bare-ident nor prop-access               | `ErrAliasRequired`           | —           |
| Two emitted top-level identifiers colliding (incl. entity)   | `ErrIdentifierCollision`     | C5 hardens  |

**Silently accepted (not routed anywhere):**

- Empty `Schema.Nodes` and `Schema.Edges` (unchanged from C2).
- Schema node type or edge type with zero properties (unchanged).
- `Validated.Distinct == true` — unchanged from C1.
- `Validated.Columns[i].GroupingKey` — unchanged from C1.
- Comments in the query text — unchanged from ADR 0005.
- `list<list<...<unknown>>>` — the `any` fallback propagates
  through the recursion; a slice of slices of… of `any` is honest.
- `RETURN null AS n :one` — the `any` return is legal-but-
  pointless; the resolver could reject the projection later but
  C3's honest posture is that no schema-side reason forbids it.

**The C1 / C2 shape stands unchanged** for anything C3 does not
touch: package-name derivation (C0 §5.1), generated-file header
(C0 §5.2), `Queries` handle constructors, `driverOrTx` interface
shape, `txDB` behaviour, `querier.go`'s interface population,
`db.go`'s read-arm body, the sentinel-set discipline (with the C3
additions), the double-run determinism test, the compile fence,
the entity-naming rules, the property-field mangle rule, and the
package-level exported-identifier sweep.

---

## 8. Compile fence (unchanged)

C0 `just test-codegen-fence` (`cd test/data/codegen && go build
./... && go vet ./...`) covers C3's emissions without change: the
nested module builds every fixture's `golden/` tree, so every new
temporal / list / scalar / unknown column arm, every FLOAT32
encode-widen / decode-narrow site, and every DATE / TIMESTAMP
property field type-checks against the pinned driver. Failure
modes:

- **A template regression in `models.go` or `<name>.cypher.go`.**
  The fence fails with the standard Go compiler error naming the
  file and line — same diagnostic quality as C1/C2.
- **A `dbtype` / `RecordValue` / `PropertyValue` drift.** Bumping
  `neo4j-go-driver/v5` may reshape the unions (e.g., a v6 rename
  of `dbtype.Time`); the fence catches at the version bump. The
  D7 standing instruction directs re-verification at each stage
  spec cycle, honored above (2026-07-11).
- **Unused imports.** A `models.go` emitting only STRING / BOOL /
  INT* properties (no DATE / TIMESTAMP) omits `time`; a
  `<name>.cypher.go` with only property-column queries (no
  temporal / list / entity columns) omits `dbtype`. The compile
  fence's `go vet` catches any drift.

C3 does not add a second fence recipe. C2 §5.5's lint parity
directive extends transitively to the C3 emissions; if CI runs
`golangci-lint` against the nested module, this is enforced
automatically.

---

## 9. Sentinel set delta — the C3 view

C2's twelve sentinels stand at C3 with one rename and one addition.
C3 renames `ErrOutOfC2Scope` → `ErrOutOfC3Scope` for the same
reason C2 renamed from C1 (per-stage rename discipline, §9 of C2
spec defence extends). C3 adds `ErrUnrepresentableWidth` as the
sentinel for the eight widths neo4j cannot represent — a **hard
error, not a scope error**: no future stage will "fix" INT128 by
implementing arbitrary-precision integer decoding; the sentinel
signals a permanent unrepresentability, distinct from
`ErrOutOfC3Scope`'s "wait for stage X" semantics.

**New sentinels at C3:**

```go
// ErrOutOfC3Scope is returned when a C3-admissible input carries
// a construct C3 does not project: a column whose resolved type is
// ResolvedEdgeUnion (C5), a non-property parameter, a :exec
// cardinality (C4), or a query text carrying a raw-string-hostile
// backtick. Category-grained per C0's precedent; C4/C5 retire the
// sub-cases as they land. Renamed from ErrOutOfC2Scope at C3 —
// collections, temporals, unrepresentable-width sentinels, and
// the honest-`any` fallbacks all retire from the C2 catchment.
var ErrOutOfC3Scope = errors.New("out of C3 scope")

// ErrUnrepresentableWidth is returned when a schema property, a
// query column, a query parameter, or a list element's leaf has a
// property width that has no faithful Go representation on the
// neo4j-go-driver v5 target: INT128, INT256, UINT128, UINT256,
// FLOAT16, FLOAT128, FLOAT256, DECIMAL. Distinct from
// ErrOutOfC3Scope: no future stage retires the eight widths — the
// underlying store (neo4j) stores integers as int64 and floats as
// float64; the sentinel is a permanent unrepresentability, not a
// deferred capability. The fail-message names the fail-site
// (entity + property; query + column; query + parameter; query +
// column + element depth) and the offending width. Checked eagerly
// at Phase Z for schema properties; lazily at Phase A for
// parameters and columns; lazily during list recursion for list
// leaves. Introduced at C3.
var ErrUnrepresentableWidth = errors.New("unrepresentable property width")
```

**Retired at C3:** `ErrOutOfC2Scope` — the constant is dropped
from the package, the fixtures rename to `out_of_c3_scope_*`, and
the `sentinelByName` map's entry renames. The retirement is a
clean cut: no `//nolint:staticcheck` for a lingering alias, no
deprecation window. The `out_of_c2_scope_int128` fixture retires
from `out_of_c3_scope_*` (the width is NOT out of C3 scope; it is
unrepresentable) and reappears as
`unrepresentable_width_int128_schema` under the honest sentinel.

**Naming defence — `ErrUnrepresentableWidth`, distinct from
`ErrOutOfC3Scope`.** The two sentinels encode different
semantics: `errors.Is(err, ErrOutOfC3Scope)` says "wait for a
later stage", `errors.Is(err, ErrUnrepresentableWidth)` says "the
input is fundamentally incompatible with the neo4j target — no
future stage will accept it". A consumer library that wants to
gate on "would this work with a hypothetical C7?" branches on the
first; a consumer library that wants to gate on "is this schema
representable at all?" branches on the second. Merging the two
into one sentinel would erase the axis. Rejected: name the
sentinel `ErrDriverIncompatibleWidth` — too specific to the
current target (a future TypeScript generator would inherit the
same sentinel; the "driver" framing bakes in the neo4j-go-driver
choice); `ErrUnrepresentableWidth` is target-neutral and reads
right for any generator.

**Naming defence — `ErrOutOfC3Scope`, per-stage rename on its
own merits.** C2's §9 defence extends verbatim: the failing
surface is textually different at every stage boundary,
`errors.Is` consumers who branched on `ErrOutOfC2Scope` break at
C3 — this is desirable, they were claiming knowledge of C2's
scope, and C3 has revised what "out of scope" means. Grill
options (a) freeze, (b) neutral, (c) per-stage: still picked (c).
The `staged rename → staging observable at the error site` axis
holds. Codegen chooses this discipline on its own terms; the
resolver's stable `ErrOutOfR0Scope` reads at a different axis
(fail-site retirement rather than sentinel rename).

**Rejected — collapsing `ErrUnrepresentableWidth` into
`ErrOutOfC3Scope` with an "out of every stage's scope" message.**
The C2 catchment lumped INT128 with :exec because both fell
through the C2 admissible set. At C3 the two axes diverge: :exec
will land at C4, INT128 will never land; the sentinel must
distinguish. A collapsed sentinel would name both as "wait", which
is a lie for the widths.

**Rejected — three sentinels: `ErrUnrepresentableIntWidth`,
`ErrUnrepresentableFloatWidth`, `ErrDecimalUnrepresentable`.**
All three have the same fix (change the schema property to a
representable width; there is no widening lever a downstream
consumer can pull to accept them), and one sentinel with a
discriminating fail-message is grep-across-source auditable in a
way three sibling sentinels are not. The fail-message names the
width explicitly ("INT128", "DECIMAL", etc.).

**Closed set for the C3 sweep.** `allSentinels` at C3:

```go
var allSentinels = []error{
    ErrInvalidPackageName,        // C0
    ErrDuplicateSourceFile,       // C0
    ErrDuplicateQueryName,        // C0
    ErrInvalidCardinality,        // C0
    ErrOutOfC3Scope,              // C3 (renamed from ErrOutOfC2Scope)
    ErrParamNameCollision,        // C1
    ErrRowFieldCollision,         // C1
    ErrAliasRequired,             // C1
    ErrIdentifierCollision,       // C1
    ErrInvalidEntityName,         // C2
    ErrUnnamedMultiLabelType,     // C2
    ErrPropertyFieldCollision,    // C2
    ErrUnrepresentableWidth,      // C3
}
```

Thirteen sentinels. `ErrFormatFailure` stays excluded (C0 §9.2
rationale unchanged). Every C3 member has at least one negative
fixture (§6.4); the reachability sweep is C0's
`TestSentinelReachability` unchanged.

---

## 10. Out-of-scope table

Every downstream capability C3 does not deliver, with the stage
that owns it. Read as ADR 0010 D7 unpacked to the C3-vs-later
boundary (C2's version tightens as C3's slice retires the
collections + temporals + widths + honest-`any` axis):

| Capability                                          | Stage owner |
|-----------------------------------------------------|-------------|
| Writes (`:exec`, zero-column methods, `WriteQuerier` population) | C4 |
| `ExecuteWrite` path in `driverDB.run`               | C4          |
| Cardinality × shape rejection (`:exec` on a projection query, `:one`/`:many` on a zero-column write) | C4 |
| Raw-string-hostile query text (backtick escape / fallback) | C4-or-later |
| `edgeUnion` sealed interfaces + `//sumtype:decl`    | C5          |
| List-of-edgeUnion column recursion                   | C5          |
| Package-level collision sweep hardening (decode-helper names as identifier sources, if C5 promotes them exported) | C5 |
| Non-property parameters (whole node, whole edge, temporal literal, scalar literal, list, unknown) | Post-v1 |
| Version-stamp polish (`-ldflags -X` wiring)         | C6          |
| Session-config polish                               | C6          |
| `gqlc-0aa` re-scope against D4's no-runtime-package decision | C6 |
| `:iter` streaming cardinality (fourth enum value)   | `gqlc-1a5` (post-v1) |
| Configuration file (`gqlc.yaml` analogue), CLI     | future config effort |
| Disk writes, out-dir sync (stale deletion)          | future CLI effort |

Rows above the `gqlc-1a5` line are staged by ADR 0010 D7; the last
two are ADR 0010 D6 futures.

---

## 11. Definition of done for C3 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is
out of scope of this document. The spec is done when:

1. This file exists at `docs/specs/codegen-stage-c3.md`, committed
   on branch `codegen-c3-spec`.
2. §3 pins the C3 emitted surface additions (temporal returns and
   Row fields, list returns and Row fields, scalar / unknown
   returns, property-side DATE / TIMESTAMP on entity structs, the
   FLOAT32 schema-width contract) and confirms the unchanged
   exported-skeleton surface.
3. §4 gives the two new naming-kernel rules — the
   `ResolvedList<T>` recursion (§4.7) with its termination
   invariant, and the eager width sweep (§4.8) — and defends
   eager-vs-lazy on ADR 0010 D3 Resolved's language.
4. §5 gives the emission templates: the C3 property → Go type
   table additions (§5.1), the `models.go` extensions for
   temporal / FLOAT32 property decode (§5.2), the unchanged
   `db.go` / `querier.go` (§5.3, §5.4), the extended per-query
   row assembly's temporal / list / scalar-unknown arms (§5.5),
   the unchanged seam (§5.6), the FLOAT32 encode-widen site
   (§5.7).
5. §9 names and defends the new sentinel
   (`ErrUnrepresentableWidth`) and the rename (`ErrOutOfC2Scope`
   → `ErrOutOfC3Scope`); confirms the closed set of thirteen.
6. §6 designs the fixture set: the twenty valid fixtures (§6.2),
   the nine invalid fixtures (§6.4), the retirement of
   `out_of_c2_scope_int128` into
   `unrepresentable_width_int128_schema`, and the fixture-per-
   capability discipline.
7. §7 states the C3 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel each construct
   routes to and the stage that owns the next widening.
8. §8 confirms the C0 compile fence covers C3 emissions without
   change; §6.6 flags the linting-parity owner directive
   extending transitively.
9. §12 gives the fixture-count summary.
10. §10 enumerates every downstream capability with its stage
    owner.
11. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer);
every blocker he raises is fixed on this same branch before the
branch merges. Cycle 2 (the C3 code cycle,
`codegen-c3-implementation` stacked on this branch) begins only
when the spec cycle merges.

---

## 12. Fixture-count summary

- **C0 valid fixtures kept:** `skeleton`, `queries_ignored`.
- **C1 valid fixtures kept:** `one_col_one_param_one`,
  `one_col_many`, `many_col_one_row`, `many_col_many`,
  `nullable_columns`, `nullable_parameter`, `multi_source_files`,
  `alias_bare_variable_ambiguity`, `all_widths`.
- **C2 valid fixtures kept:** `entity_node_projected_one`,
  `entity_edge_projected_one`, `entity_node_and_scalar_row`,
  `entity_node_many`, `entity_nullable_node`,
  `entity_explicit_name`, `entity_multi_label_named`,
  `entity_edge_ambiguous_label_named`, `entity_zero_property_node`,
  `entity_with_nullable_property`.
- **C3 valid fixtures added (20):** `temporal_column_date`,
  `temporal_column_datetime`, `temporal_column_localtime`,
  `temporal_column_localdatetime`, `temporal_column_duration`,
  `temporal_column_time`, `property_date`, `property_timestamp`,
  `float32_column`, `float32_parameter`, `list_int`, `list_string`,
  `list_list_int`, `list_entity`, `list_nullable`, `list_unknown`,
  `scalar_null`, `scalar_map`, `unknown_column`,
  `many_columns_mixed_temporal_list`.
- **C0 invalid fixtures kept:** `invalid_package_name`,
  `duplicate_query_name`, `duplicate_source_file`,
  `invalid_cardinality`.
- **C1 / C2 invalid fixtures kept:** `param_name_collision`,
  `row_field_collision`, `alias_required_function_call`,
  `alias_required_expression`, `identifier_collision_reserved`,
  `invalid_entity_name_node`, `invalid_entity_name_edge`,
  `unnamed_multi_label_type`, `property_field_collision`,
  `identifier_collision_entity_row`.
- **C2 invalid fixtures renamed:** `out_of_c2_scope_edge_union` →
  `out_of_c3_scope_edge_union`; `out_of_c2_scope_exec` →
  `out_of_c3_scope_exec`. `out_of_c2_scope_scalar_return` retires
  (scalar columns in-scope). `out_of_c2_scope_int128` retires
  from `out_of_c3_scope_*` and reappears as
  `unrepresentable_width_int128_schema` under the honest sentinel.
- **C3 invalid fixtures added (9):**
  `unrepresentable_width_int128_schema`,
  `unrepresentable_width_uint256_schema`,
  `unrepresentable_width_float16_schema`,
  `unrepresentable_width_decimal_schema`,
  `unrepresentable_width_int128_parameter`,
  `unrepresentable_width_float128_list_column`,
  `out_of_c3_scope_edge_union` (rename of `out_of_c2_scope_edge_union`),
  `out_of_c3_scope_exec` (rename of `out_of_c2_scope_exec`),
  `out_of_c3_scope_non_property_parameter`.

**Totals at C3:**
- Valid fixtures: 2 (C0) + 9 (C1) + 10 (C2) + 20 (C3) = 41.
- Invalid fixtures: 4 (C0) + 10 (C1/C2 kept) + 9 (C3 new including
  two renames) = 23. (`out_of_c2_scope_scalar_return` and
  `out_of_c2_scope_int128` retire from the invalid set.)
- Sentinels in `allSentinels`: 13.
- Every sentinel has ≥1 invalid fixture; the reachability sweep
  passes with the enlarged set.
