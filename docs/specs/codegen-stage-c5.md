# Stage C5 spec — codegen: edgeUnion + collision-sweep hardening

The implementation brief for Stage C5 of `internal/codegen`, extending
the merged C4 slice (`docs/specs/codegen-stage-c4.md`) with the two
capability slices ADR 0010 D7 places at C5: **`ResolvedEdgeUnion`
projection** — the sealed-interface-per-query-column pattern per
ADR 0010 D3 Resolved (lines 334–350), and **package-level
exported-identifier collision-sweep hardening** — the sweep that
covered four identifier sources at C4 (entity struct names, method
names, `<Method>Params`, `<Method>Row`) grows to six as C5 introduces
per-query edgeUnion interfaces and lifts entity-decode-helper names
onto the swept surface. Build this **test-first**. Scope, sequencing,
and error posture inherit from ADR 0010 and the C0 / C1 / C2 / C3 / C4
specs unchanged; this document revises only the sections C5 touches.

Stage C5 keeps the C4 file set (`db.go` / `querier.go` / `models.go`
/ `<name>.cypher.go`) byte-identical for the parts C5 does not touch,
extends Phase A's column admission from
`{ResolvedProperty, ResolvedNode, ResolvedEdge, ResolvedTemporal,
ResolvedScalar, ResolvedUnknown, ResolvedList}` to the full closed
`ResolvedType` sum (adding `ResolvedEdgeUnion`) plus the same
recursion at every `ResolvedList` element leaf, extends Phase B with
edgeUnion-column derivation (a synthesised per-query-column interface
name; a candidate-entity ordered slice from `EdgeKeys`; nullable
tracking), extends the per-source emission with the edgeUnion
interface declaration + marker-method emission in `models.go` and the
edgeUnion decode-column arm in `<name>.cypher.go`, extends `sweepIdentifiers` from
four identifier sources to six (entity struct names + method names +
`<Method>Params` + `<Method>Row` + edgeUnion interface names +
entity-decode-helper names), and renames the catch-all sentinel
`ErrOutOfC4Scope` → `ErrOutOfC5Scope` per-stage. The C4 property → Go
type mapping stays byte-identical for reads and writes; edgeUnion
adds one new column-shape row (see §5.1). Every edgeUnion method
body is a `record.Get` + type-switch dispatch (§5.5) with a marker-
method call to prove each candidate satisfies the interface.

---

## 1. Deliverables

- `internal/codegen/generate.go` — extended with the C5 column
  admission (§4.9), the per-query edgeUnion synthesis in Phase B
  (§4.10), the hardened cross-query collision sweep with six
  identifier sources (§4.6, §5.7), the edgeUnion interface + marker-
  method emission in `renderModels` (§5.2), and the edgeUnion decode
  arm in the per-source method body (§5.5). The C0 file layout
  stands (`codegen.go` / `input.go` / `errors.go` / `generate.go`);
  no new files at C5.
- `internal/codegen/errors.go` — one rename (§9): `ErrOutOfC4Scope`
  → `ErrOutOfC5Scope`. No sentinel additions at C5. The
  `ResolvedEdgeUnion` sub-case retires from the catchment (edgeUnion
  is now in-scope); the sentinel keeps the non-property-parameter
  and raw-string-hostile-backtick cases (both still post-v1 / later).
- `test/data/codegen/valid/<name>/` — new C5 valid fixtures (§6.2),
  each with a schema `.gql` plus at least one query projecting an
  edgeUnion column, a `manifest.json`, and a `golden/` subdirectory
  with the complete generated package.
- `test/data/codegen/invalid/<name>/` — new C5 negative fixtures for
  the two new collision axes (§6.4) plus the renamed `ErrOutOfC5Scope`
  fixtures. The C4 `out_of_c4_scope_edge_union` fixture retires from
  the invalid set (edgeUnion is now in-scope) and reappears (with
  the same schema and query text) as a valid fixture (§6.2). The C4
  `out_of_c4_scope_non_property_parameter` fixture renames to
  `out_of_c5_scope_non_property_parameter`.
- `internal/codegen/codegen_test.go` — no structural change; the
  `sentinelByName` map renames one row.

Nothing downstream of edgeUnion + hardened collision sweep is built.
Version-stamp polish (C6), session-config polish (C6),
`gqlc-0aa` re-scope (C6), `:iter` streaming (post-v1, `gqlc-1a5`)
stay for their owning stage per ADR 0010 D7. Non-property parameters
(whole node, whole edge, temporal expression, scalar literal, list,
unknown) stay post-v1 and continue to route through `ErrOutOfC5Scope`
(the sentinel that catches every remaining category-grained residue).

---

## 2. Architecture — deltas from C4

C4's architecture (§2 of the C4 spec) stands: the `Generator` seam,
the concrete `*Codegen` return, the empty `Option` surface, the
purity / determinism / short-circuit posture, the `generate.go` /
`generate` kernel split, the Phase Z / Phase A / Phase B / cross-
query sweep sequence, the `resolvedListGoType` recursion, the eager
width sweep, the C4 cardinality × shape gate, the `WriteQuerier`
population, the `driverDB.run` write arm. C5 extends Phase A's
column admission with the `ResolvedEdgeUnion` variant (§2.1),
extends Phase B with a per-query-column edgeUnion synthesis step
(§2.1), hardens the cross-query sweep by admitting two new
identifier sources (§2.2), and extends the per-source emission with
the edgeUnion interface (in `models.go`) plus the edgeUnion decode
arm (in `<name>.cypher.go`) (§2.3). No new exported types except the
per-query edgeUnion interfaces themselves; no API-shape delta on the
skeleton (§3 below); the `Input` struct stays `{Schema, Queries}`
(ADR 0010 D6).

### 2.1 The C5 kernel structure

The kernel remains one linear pass with early returns. C5 extends
each of the three existing phases in-place:

- **Phase Z — schema-shape admission and entity naming**
  (unchanged from C4). The Rule 1–6 sequence at §4.5 / §4.8 of the
  C3 spec runs verbatim: entity naming, property-field derivation,
  eager unrepresentable-width sweep. Nothing in Phase Z sees the
  edgeUnion axis; edgeUnion is a per-query resolver artefact, not a
  schema-side one — the schema declares individual edge types with
  their labels and endpoint pairs, and the resolver commits an
  edgeUnion at query time when a binding's labels or label ×
  orientation cross-product resolves to more than one schema edge.
  Models-only adoption (an empty `Queries` slice) still produces the
  same entity emissions the C4 posture produced.
- **Phase A — batch admission** (unchanged shape, extended column
  admission). Every `NamedQuery` still passes C0's `validateQueries`
  gate, the C1 / C2 / C3 / C4 per-query admission checks
  (cardinality × shape, reserved-identifier, unrepresentable width,
  non-property-parameter), and the C4 raw-string-hostile-backtick
  gate. C5 widens the admissible column shape from the C4 set to
  the full closed `ResolvedType` sum: `ResolvedEdgeUnion` no longer
  routes to `ErrOutOfC4Scope`. The admission additionally verifies,
  per edgeUnion column: (a) `len(EdgeKeys) >= 2` — the resolver
  guarantees this per ADR 0010 D3 Resolved and the R3 spec §4.4
  (single-candidate collapses to `ResolvedEdge`), but the codegen
  gate reads the invariant defensively; a violation routes to
  `ErrOutOfC5Scope` naming the query, column, and length; (b) every
  `EdgeKey` in the slice has a Phase Z cache entry (i.e., the schema
  declared it) — a miss routes to `ErrOutOfC5Scope` naming the
  offending edge triple. Phase A also runs the recursion at every
  `ResolvedList` element leaf; a list-of-edgeUnion column is now in
  scope and recurses through `resolvedListGoType` to the same
  synthesised interface name (see §4.7 recursion rule below).
  Non-property parameters (whole node, whole edge, temporal literal,
  scalar literal, list, unknown) still route to `ErrOutOfC5Scope`.
  Widening the parameter axis is post-v1 per ADR 0010 D7. Phase A
  short-circuits: first offender wins across the extended admission
  set, in the C4 order (cardinality × shape check before column-type
  check before parameter-type check).
- **Phase B — per-query name derivation** (extended derivation set).
  The C4 helpers `paramFieldName`, `rowFieldName`, and `goType`
  stand unchanged. C5 extends the row-field type derivation to
  cover `ResolvedEdgeUnion`: the derived `GoType` for an edgeUnion
  column is the synthesised interface name
  `<QueryName><RowFieldName>` (§4.10); the `Kind` is a new
  `columnEdgeUnion` (§2.4); the `EdgeKeys` slice is retained on
  `preparedRow` so the emission walk (§5.5) can build the type-
  switch cases from it in candidate order. The Params-side
  derivation runs unchanged. The per-query `<Method>Params` /
  `<Method>Row` sweeps stand unchanged; edgeUnion adds no per-
  query collision axis (see §4.10 defence).

Phase A runs before Phase B because Phase B's name derivation reads
Phase A's admission decisions (the edgeUnion synthesis at Phase B is
safe because Phase A has already checked `len(EdgeKeys) >= 2` and
every candidate's schema-cache membership). The cross-query package-
level identifier collision sweep runs unchanged after Phase B, with
the extended identifier-source set (§2.2). C5 introduces the per-
query edgeUnion interfaces as a fifth exported-identifier source and
the entity decode helpers (`decode<EntityName>`) as an unexported
source promoted to the sweep so a symmetric identifier discipline
covers every identifier the emission produces at any visibility.

### 2.2 Package-level collision sweep hardening — the C5 rule

C2 introduced the sweep with four identifier sources (entity struct
names + method names + `<Method>Params` + `<Method>Row`). C4 added
no source (the two new sentinels `ErrExecOnProjection` /
`ErrCardinalityShapeMismatch` gate cardinality × shape, not
identifier collision). C5 grows the sweep to six sources, the full
enumeration of every possible collision axis a generated package can
produce:

1. **Entity struct names** (introduced C2). Sorted by emission order
   (`Schema.Nodes` in `LabelSetKey` order then `Schema.Edges` in
   `EdgeKey` triple-lex order). First identifier source anchors the
   schema-side vocabulary.
2. **Entity decode helper names** (`decode<EntityStructName>`,
   promoted at C5). Unexported; historically kept off the sweep with
   the argument the C2 spec §4.6 makes ("two entity structs `Person`
   already collide at the exported layer, so the unexported helpers
   cannot collide independently"). That argument holds against C2's
   fixture set and remains true today; C5 promotes decode helpers to
   the sweep anyway because (a) the sweep's job is to name every
   identifier the emission produces at any visibility, so a hardened
   sweep reads more directly than an argument-by-elimination, and
   (b) if a future stage introduces an entity struct whose lowercased
   first-rune tricks the mangle (say a schema type explicitly named
   `Decode<Something>`), the collision fires at the exported-layer
   already, but the sweep also names the unexported helper site so
   the error message points the caller at the exact function whose
   name is dashed. Same insertion order as the entity structs.
3. **Method names** (introduced C1). Every prepared query's method,
   in `Input.Queries` slice order.
4. **`<Method>Params`** (introduced C1). Queries with two-plus
   parameters, in `Input.Queries` slice order.
5. **`<Method>Row`** (introduced C1). Queries with two-plus columns
   AND no `columnEdgeUnion` field short-circuiting the Row struct —
   the C1 rule stands; edgeUnion columns still count toward the
   two-plus-columns threshold that triggers a Row struct, and the
   edgeUnion field type in the Row struct is the interface name
   (§3.3). Order is `Input.Queries` slice order.
6. **EdgeUnion interface names** (introduced C5). Per-query-column
   `<QueryName><RowFieldName>`, in `Input.Queries` slice order,
   sub-ordered by column position. A query with two edgeUnion
   columns emits two interfaces; a batch with two queries each
   projecting one edgeUnion column emits two interfaces; the
   deduplication anti-pattern the ADR 0010 D3 Resolved defence
   rejected (§4.10) is honored — no set-derived names.

Any duplicate → `ErrIdentifierCollision` naming both identifier
sources (e.g. "edgeUnion interface `GetActionsAction` for query
`GetActions` column `action` collides with entity struct `GetActionsAction`
(schema labels `GetActionsAction`)"). The sweep is a single map
insertion pass; the first duplicate wins across all six sources.

**Why decode helpers now**, over C2's exclusion argument.
C2's argument was correct-by-elimination: an unexported `decodePerson`
collision requires two `Person` entity structs, which the exported
sweep already rejects. C5's edgeUnion arm changes the calculus in one
subtle way: an edgeUnion interface's marker method (`is<QueryName><RowFieldName>()`)
is emitted on every candidate entity struct as a receiver method
(§5.2). If a *user schema* declared two entity types whose Go names
mangled to the same thing at the schema layer — already an exported
collision — the marker method emitted on each would double-declare.
That's still exported collision, still caught. But the schema also
declares nothing about `decode<Name>` — a future codegen change that
promoted an entity decode helper to *exported* (say the R7 resolver
learned to type driver-side entity metadata and codegen exposed
`Decode<Name>` as public API) would blow past the current sweep.
Adding decode helpers to the sweep future-proofs the invariant at
zero cost today (no legitimate fixture collides). Precedent: the C1
`<methodName>QueryText` const sitting on the compile fence, not the
sweep — same discipline as C2 §4.6 explains, but C5 goes the other
way for decode helpers because the promotion-risk axis is real.

### 2.3 The edgeUnion emission — the C5 body

C4's per-source emission dispatched five column arms (`columnProperty`,
`columnNode`, `columnEdge`, `columnTemporal`, `columnScalar`,
`columnList`, `columnAny` — seven; see `generate.go:99-128`). C5 adds
an eighth: `columnEdgeUnion`. The arm decodes the column via
`record.Get(key)` (returning `(any, bool)`) — the same honest-any
carrier `columnAny` uses, because `neo4j.GetRecordValue[T]` has no
overload for a `dbtype.Relationship`-or-nil result — and dispatches
into a type switch on the driver's `dbtype.Relationship`:

```go
raw, ok := records[0].Get("action")
if !ok {
    return nil, fmt.Errorf("GetAction: column %q missing from record", "action")
}
if raw == nil {
    return nil, fmt.Errorf("GetAction: column %q is non-nullable but arrived null", "action")
}
rel, ok := raw.(dbtype.Relationship)
if !ok {
    return nil, fmt.Errorf("GetAction: column %q: expected dbtype.Relationship, got %T", "action", raw)
}
switch rel.Type {
case "AUTHORED":
    entity, err := decodeAUTHORED(rel)
    if err != nil {
        return nil, fmt.Errorf("GetAction: decode column %q: %w", "action", err)
    }
    return entity, nil
case "LIKES":
    entity, err := decodeLIKES(rel)
    if err != nil {
        return nil, fmt.Errorf("GetAction: decode column %q: %w", "action", err)
    }
    return entity, nil
default:
    return nil, fmt.Errorf("GetAction: column %q: unexpected relationship type %q", "action", rel.Type)
}
```

The dispatch is on `rel.Type` — the driver's `dbtype.Relationship`
struct carries the relationship type as a string field
(`Relationship.Type`, verified against
`pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5@v5.28.4/neo4j/dbtype`,
2026-07-11). The dispatch keys are the raw label strings from the
`EdgeKey` triples (`EdgeKey.Label`), not the mangled entity struct
names — the driver returns the wire label, not the codegen mangle.
The candidate branches are in the `EdgeKeys` slice order the resolver
committed (canonical per R3 spec §4.4).

- **`default` arm.** A driver-returned relationship type outside the
  committed candidate set is a decode error naming the offending
  type. The resolver commits `EdgeKeys` from schema-declared edges;
  a driver arrival with a foreign type indicates schema drift or a
  driver-side surprise, either way a fault the caller must see.
- **The interface is returned as the row value.** For a single-
  column `:one` edgeUnion query, the method's return type is
  `<QueryName><RowFieldName>` (the interface); the successful path
  returns the candidate entity struct value, which satisfies the
  interface via its marker method. A `:many` query returns a slice
  of the interface (`[]<QueryName><RowFieldName>`); the row-
  assembly loop appends each decoded value.
- **Nullability carve-out (ADR 0010 D3 Resolved, lines 343–345).**
  A nullable edgeUnion column stores a nil interface value, not
  `*<Interface>`. Pointer-to-interface is the Go anti-pattern the ADR
  called out; the interface value itself is nilable. Decode still
  enforces "non-nullable never nil": the `if raw == nil` gate above
  fires for a non-nullable column arriving null, exactly like the
  C2 entity carve-out. A nullable column skips the gate and lets the
  nil interface propagate to the Row field (or single-column return
  value) as-is.

The `models.go` edgeUnion emission is orthogonal: the per-query-
column interface declaration (`type GetActionAction interface {
isGetActionAction() }`) and the marker method on each candidate entity
struct live in `models.go` because they are consumed across every
per-source file that projects the union, so keeping them in the
schema-side file is the natural home. See §5.2.

### 2.4 A new `columnKind` — `columnEdgeUnion`

`preparedRow.Kind` gains one enum value:

```go
const (
    columnProperty columnKind = iota
    columnNode
    columnEdge
    columnTemporal
    columnScalar
    columnList
    columnAny
    columnEdgeUnion  // C5
)
```

The `Kind` is derived once at Phase B and carried onto `preparedRow`
so the row-assembly template needs no per-emission re-derivation —
same discipline C1 / C2 / C3 / C4 apply. The row-field also carries
the `EdgeKeys` slice (canonical order per R3 spec §4.4) so the
emission walks the type switch in that order.

### 2.5 Purity, determinism, short-circuit — unchanged

C4 §2.4's three invariants stand:

- **Pure.** No new I/O; the edgeUnion Phase A gate is one branch in
  the column-type sweep, the Phase B synthesis is one branch in the
  row-field derivation, the `models.go` edgeUnion emission is a
  template-literal walk over the prepared-query slice, the per-source
  file's edgeUnion arm is one branch in the method-body dispatch.
- **Deterministic.** Iteration order: Phase A / Phase B / per-source
  grouping are still `Input.Queries` slice order; the `models.go`
  edgeUnion walk is `Input.Queries` slice order (interfaces emitted
  in first-appearance order; marker methods emitted beside each
  candidate entity struct in entity-emission order). No map iteration
  escapes into the output.
- **Short-circuit.** First-error wins across Phase Z, Phase A
  (extended with the edgeUnion admission), Phase B, the cross-query
  collision sweep (extended with two new identifier sources), and
  per-source emission. Zero value on error: `(nil, err)`.

### 2.6 What the C5 change means for the emitted module

C5 revises the emitted `models.go` and `<name>.cypher.go` bodies. The
tightest invariants (extending C4 §2.5):

- **`models.go`'s edgeUnion block.** For every distinct per-query-
  column edgeUnion, one `type <QueryName><RowFieldName> interface {
  is<QueryName><RowFieldName>() }` declaration plus one
  `//sumtype:decl` line above it. For every candidate entity struct
  referenced by *any* edgeUnion column in the batch, one marker
  method declaration `func (<EntityStructName>) is<QueryName><RowFieldName>() {}`
  emitted beside the entity struct's decode helper. The candidate
  set for a given interface is the `EdgeKeys` slice; the marker
  methods are emitted per-interface, so an entity that participates
  in two interfaces gets two marker methods (see §4.10 grill).
- **`querier.go`'s interface blocks.** No structural change; a
  `:one` / `:many` on an edgeUnion column adds one line the same way
  a `:one` / `:many` on any other column does. The `Querier`
  embedding stays `type Querier interface { ReadQuerier;
  WriteQuerier }` — byte-identical to C4.
- **`<name>.cypher.go`'s method body for an edgeUnion column.** A
  single-column method returns `(<QueryName><RowFieldName>, error)`
  for `:one`, `([]<QueryName><RowFieldName>, error)` for `:many`. A
  multi-column method emits the edgeUnion field in the Row struct
  as the interface type (`<QueryName><RowFieldName>`); nullable
  edgeUnion fields remain the bare interface (nil-value carve-out).
- **`db.go`'s `driverDB.run` seam signature stands** (unchanged from
  C4). The edgeUnion decode uses `record.Get` — no seam change.

The change is entirely inside the emitted templates; gqlc's own
module is not affected — the generator emits text, and text-level
changes cross no dependency boundary. The nested-module compile
fence (`just test-codegen-fence`, C0 §7) proves the emitted edgeUnion
interface + marker methods type-check against the pinned driver
version. `dbtype.Relationship.Type` is stable in v5.28.4 (verified
against `pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5@v5.28.4/neo4j/dbtype`,
2026-07-11): the struct's `Type` field is a string carrying the
relationship label, and `.Props` carries the property map used by
each candidate's decode helper.

---

## 3. Emitted API surface — the C5 shape

The user-visible generated surface C5 adds on top of C4. C5's
exported package-level identifiers grow by the per-query edgeUnion
interface names and the marker methods on candidate entity structs;
the `Querier` embedding stays byte-identical; the `WriteQuerier` /
`ReadQuerier` partition rule (on `Statement`) is unchanged. The C0 /
C1 / C2 / C3 / C4 exported skeleton set is unchanged.

### 3.1 EdgeUnion column returns and Row-struct fields

An edgeUnion column of shape `RETURN r` where `r :: edgeUnion({A, B,
C})` renders in the emitted method surface as follows. Single-column
`:one`:

```go
// name: GetAction :one
// MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r
func (q *Queries) GetAction(ctx context.Context, arg GetActionParams) (GetActionR, error)
```

Single-column `:many`:

```go
// name: ListActions :many
// MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r
func (q *Queries) ListActions(ctx context.Context) ([]ListActionsR, error)
```

Multi-column `:one` with an edgeUnion field:

```go
// name: PersonAction :one
// MATCH (p:Person)-[r:AUTHORED|LIKES]->(:Post) WHERE p.id = $id RETURN p, r
type PersonActionRow struct {
    P Person
    R PersonActionR
}
func (q *Queries) PersonAction(ctx context.Context, id int64) (PersonActionRow, error)
```

- **Interface naming: `<QueryName><RowFieldName>`.** The naming
  precedent is the C1/C2 invented-but-derived rule (`<Method>Row`,
  `<Method>Params`): the query name + a derived per-column suffix
  (§4.10). Two queries projecting the same multi-type edge produce
  two interfaces — deduplicating would reintroduce banned set-
  derived names (ADR 0010 D3 Resolved, lines 340–343).
- **Interface shape: sealed marker method.** The interface's method
  set is one unexported marker method whose name is the interface
  name prefixed with `is` and lowercased-first-rune:
  `is<QueryName><RowFieldName>()`. Every candidate entity struct
  (each `EdgeKey`'s entity struct from C2) implements the interface
  via a generated method with an empty body. This is the house
  closed-sum pattern (`ResolvedType` itself: `isResolvedType()`,
  verified against `internal/resolver/validated.go:81` and its
  variants — every variant implements a package-private marker).
- **`//sumtype:decl` annotation.** The interface declaration carries
  a `//sumtype:decl` comment line above it, so gochecksumtype users
  get static exhaustiveness on the consuming type switch. The
  annotation is a comment; it has zero runtime effect and is invisible
  to `go vet`. The tool is opt-in — a consumer running `gochecksumtype`
  against the generated package sees a warning if their code type-
  switches without covering every candidate.

### 3.2 EdgeUnion marker methods on candidate entity structs

Every candidate entity struct in an edgeUnion's `EdgeKeys` slice
gains one marker method per interface it participates in. Emitted
beside the entity's decode helper in `models.go`:

```go
// AUTHORED corresponds to the AUTHORED edge type (Person -> Post).
type AUTHORED struct {
    Since int64
}

func (AUTHORED) isGetActionR() {}
func (AUTHORED) isListActionsR() {}
func (AUTHORED) isPersonActionR() {}

func decodeAUTHORED(rel dbtype.Relationship) (AUTHORED, error) {
    // ... C2 decode helper body
}
```

- **Value receiver, empty body.** The marker's receiver is the
  entity struct value, not a pointer — pointer-to-struct-satisfies-
  interface is legal in Go, but a value receiver keeps the
  satisfaction visible for both struct values and pointers, aligning
  with the C2 `decode<Name>` returning a struct value.
- **Method name is unexported.** `isGetActionR`, not
  `IsGetActionR` — the marker is a package-private assertion of
  interface satisfaction, not part of the user-facing API. Same
  precedent as `isResolvedType()`. The exported sweep does NOT walk
  marker method names (they are unexported); a hypothetical
  collision — two edgeUnion interfaces with the same interface name
  — is caught by the interface-name axis of the sweep (§2.2 source
  6) before their marker methods ever collide.
- **One marker method per interface an entity participates in.** An
  entity struct `AUTHORED` referenced by three per-query-column
  edgeUnion interfaces gets three marker methods. This is the cost
  of the per-query-column naming discipline; grill §4.10 defends
  the alternative (a batch-scoped interface deduplicating across
  queries) as banned per ADR 0010 D3.

### 3.3 Nullable edgeUnion column on a Row struct

A nullable edgeUnion column renders as the bare interface, not a
pointer:

```go
type PersonActionRow struct {
    P Person
    R PersonActionR  // nullable → nil interface value
}
```

- **No pointer wrapping.** ADR 0010 D3 Resolved (lines 343–345) is
  explicit: nullable → nil interface value; pointer-to-interface is
  the anti-pattern. The Go idiom `var r PersonActionR = nil` is
  well-supported and reads naturally at the consumer:
  `if row.R == nil { ... } else { switch v := row.R.(type) { ... } }`.
- **Decode enforcement.** The decode arm still enforces non-nullable
  never-nil: the `if raw == nil { return ..., fmt.Errorf("...
  column %q is non-nullable but arrived null", ...) }` gate fires
  for a non-nullable column arriving null. A nullable column skips
  the gate and assigns nil.
- **`json.Unmarshal` cannot target an interface field.** ADR 0010
  D3 Resolved (lines 345–347) accepts this cost: Row structs are
  not a serialization format. Consumers who need to serialize should
  type-switch to the concrete candidate first and serialize that.

### 3.4 The C4 property → Go type mapping stands

C5 does not revise the C4 property → Go type table. Reads, writes,
mixed-statement, and edgeUnion-projecting queries all use the same
table for their non-edgeUnion columns. The edgeUnion axis adds one
new column-shape row (§5.1); the rest is byte-identical to C4.

### 3.5 The C1 `driverOrTx.run` seam signature stands

C5 does not revise the C1 seam signature. `[]*neo4j.Record` covers
the edgeUnion arm the same way it covered the entity arms — the
records carry `dbtype.Relationship` values in the appropriate column,
retrievable via `record.Get(key)`. Zero template change to the seam
interface declaration.

---

## 4. The naming kernel — C5 additions

C4's naming kernel (§4 of the C4 spec) stands: method names verbatim
(§4.1), Params fields via the one-mangle rule (§4.2), Row fields via
text-shape analysis on `Column.Name` (§4.3), package-level exported-
identifier collision sweep (§4.4 / §4.6), entity-naming rules (§4.5),
property-field mangle with collision sentinel (§4.5 Rule 5),
`ResolvedList<T>` recursion (§4.7), eager width sweep (§4.8),
cardinality × shape gate (§4.9). C5 adds the edgeUnion synthesis rule
(§4.10) and extends the sweep-source set (§4.6, integrated below).

### 4.7 `ResolvedList<T>` recursion — the C5 edgeUnion arm

The C3 recursion rule (§4.7 of C3 spec) stands, with one arm change:
a `ResolvedList{ResolvedEdgeUnion{...}}` element no longer routes to
`ErrOutOfC4Scope`; it now returns the interface name for that
column's edgeUnion, threaded through the list. The recursion helper
`resolvedListGoType` reads the ambient query name + column name so
the synthesised name matches the column's Row-field interface:

| `ResolvedType` variant | Go type text | Nullable? |
|---|---|---|
| `ResolvedProperty` (representable width) | property Go type | via list wrap |
| `ResolvedNode` | entity struct name | via list wrap |
| `ResolvedEdge` | entity struct name | via list wrap |
| `ResolvedEdgeUnion` | `<QueryName><RowFieldName>` (C5 addition) | via list wrap |
| `ResolvedScalar` | scalar Go type | via list wrap |
| `ResolvedTemporal` | temporal Go type | via list wrap |
| `ResolvedUnknown` | `any` | via list wrap |
| `ResolvedList{Element}` | `"[]" + recurse` | via list wrap |

- **Nullability at leaf level.** ResolvedList carries `Nullable` on
  the list itself, not on elements — C3 established this
  invariant. A nullable list-of-edgeUnion emits `*[]<Interface>`;
  each element inside a non-nullable list is a non-nil interface
  (arrival-null-in-a-list-element is a decode error — the honest
  posture the C3 list-scalar-of-non-null arm applied).
- **Recursion signature widens.** The helper needs the query and
  column context to synthesise the interface name; the C3 signature
  `resolvedListGoType(t, entities, entityIndex) (string, error)`
  extends to `resolvedListGoType(t, entities, entityIndex, queryName,
  columnField) (string, error)`. The extra parameters are inert for
  every non-edgeUnion arm; only the edgeUnion arm reads them.

### 4.10 EdgeUnion interface synthesis — Phase B's C5 extension

**Rule 8 (added at C5) — every admitted edgeUnion column at every
query synthesises a per-query-column interface name, and every
candidate entity struct in the column's `EdgeKeys` slice gains a
marker method for that interface.** Phase B's per-query row-field
derivation (§4.3 of C1) adds one arm:

- **Interface name derivation.** `<QueryName><RowFieldName>` — the
  concatenation of the query's `MethodName` (verbatim, exported by
  construction) and the row-field name derived from `Column.Name`
  via the C1 §4.3 rules. Grill options (a) deduplicating across
  queries under a schema-derived name (e.g. `AuthoredLikesEdge`),
  (b) per-query but shared column-suffix (e.g.
  `<QueryName>EdgeUnion` — same suffix regardless of column), (c)
  per-query-column: picked (c). ADR 0010 D3 Resolved (lines 340–343)
  bans (a) explicitly: "deduplicating would reintroduce banned set-
  derived names" (the set-derived-name is
  `AuthoredLikesEdge` — an alphabetised alphabet-soup that changes
  the moment the schema declares a third candidate). Option (b) is
  under-specified for a query with two edgeUnion columns
  (`RETURN r1, r2` where both are unions): two columns need two
  interfaces, so a column suffix is required. Option (c) is the
  only self-consistent choice.
- **Marker method name.** `is<QueryName><RowFieldName>()`, lowercase
  first rune per the Go unexported-method convention. The house
  precedent is `isResolvedType()` on every `ResolvedType` variant
  (`internal/resolver/validated.go:107, 135, 161, 191, 256, 319,
  340, 362`). The receiver is the entity struct value; the body is
  empty. This is the classic sealed-sum marker method idiom.
- **Where the marker method lives.** In `models.go`, beside the
  entity struct's decode helper. Alternative options: (a) in the
  per-query `<name>.cypher.go` file, scoped to that query column;
  picked NOT (a). The interface itself is emitted in `models.go`
  (see below), and the marker method must be defined in the same
  package that declares the interface, but Go allows marker methods
  on a receiver type to be declared in any file within the same
  package — so file placement is a readability choice, not a
  language constraint. Placing the marker beside the entity struct
  reads better because the reader following `Person` sees the full
  method set (decode helper + every marker method it implements)
  in one file. Placing it beside the per-query file would split the
  entity's method set across N files (one per query the entity
  participates in), degrading grep quality. Precedent: the C2 decode
  helper lives in `models.go` beside the entity struct, not in
  `<name>.cypher.go` where the decode helper is called from — same
  reasoning applied here.
- **Interface declaration placement.** In `models.go`, in
  `Input.Queries` slice order (sub-ordered by column position for a
  query with two edgeUnion columns). The declaration reads as one
  block per interface, with the `//sumtype:decl` comment line
  above. Placing interfaces in `models.go` — not the per-query file
  — reads as one axis: `models.go` is "schema-shape identifiers +
  the sealed-sum identifiers derived from queries". A query that
  projects the union is textually adjacent (in
  `<name>.cypher.go`) to a `models.go`-declared interface it
  returns, exactly like C2's entity-decode helper: the decode call
  site is in `<name>.cypher.go`, the helper is in `models.go`.
- **Does adding a marker method violate C2's schema-shaped-only
  posture?** No. C2's posture is "entity struct *fields* come from
  schema properties and nothing else" — the struct's shape is
  schema-derived. A method attached to the struct is orthogonal to
  the struct's shape (Go's method set is not part of the struct's
  memory layout). The marker method is generated code that satisfies
  an interface, not a field that changes what the entity *is*. The
  struct itself remains schema-shaped. Same reasoning C2 used to
  admit the entity `decode<Name>` helper: the helper is generated
  code that operates on the struct, not a schema-shape change.
- **Collision axes.** The synthesised interface name enters the
  cross-query collision sweep as source (6) (§2.2). A per-query-
  column interface `GetActionAction` colliding with (a) an entity
  struct `GetActionAction` (unlikely-but-possible if a schema
  declared such a type explicitly), (b) another interface with the
  same name (two queries with the same method name — already
  caught by source (3)'s method-name sweep, but the interface-name
  sweep catches the axis symmetrically), (c) a `<Method>Params`
  named `GetActionAction` (rare but possible if a query is named
  `GetActi` and takes params `onAction` — the mangle produces
  same), or (d) a `<Method>Row` — every axis fires
  `ErrIdentifierCollision`. The marker method names are unexported
  and not swept individually (the interface name is what discriminates
  them; two interfaces named the same means two marker methods
  named the same, but the interface-name collision fires first).

### 4.6 Package-level identifier collision sweep — C5 hardening

C4's sweep runs unchanged over the four C1/C2 identifier sources.
C5 adds two: entity decode helpers (source 2 in the sweep, promoted
from off-sweep at C2) and edgeUnion interface names (source 6 in the
sweep, introduced at C5). The sweep order remains:

1. **Entity struct names** (C2). Sorted by their emission order.
2. **Entity decode helper names** (`decode<Name>`, promoted at C5).
   Same insertion order as entity structs.
3. **Method names** (C1).
4. **`<Method>Params`** (C1; queries with two-plus parameters).
5. **`<Method>Row`** (C1; queries with two-plus columns).
6. **EdgeUnion interface names** (C5; per-query-column, in
   `Input.Queries` slice order sub-ordered by column position).

Any duplicate → `ErrIdentifierCollision` naming both identifier
sources. The sweep is a single map insertion pass; the first
duplicate wins across all six sources.

- **Grill option: sweep the marker method names too.** Rejected.
  Marker methods are unexported; two edgeUnion interfaces with the
  same name (already rejected by source 6) would have identical
  markers, but the interface-name collision fires first. A collision
  between a marker method name and a legitimate method receiver
  (e.g. an entity struct with a hand-written method — impossible
  for generated code, but conceivable for a future schema that
  declared computed methods) is ruled out because C5 (like C2)
  produces no user-defined receiver methods on entity structs.
- **Grill option: sweep the `<methodName>QueryText` const names
  (`getActionQueryText`).** Rejected. Same discipline as C2 §4.6:
  unexported query-text consts collide only if two queries have the
  same method name (a case source 3 catches). The compile fence
  would also catch it. Adding them to the sweep would produce
  redundant error messages naming the same axis twice.
- **Fail-message discipline.** Six source labels, each fail-message
  names the offending identifier and both source labels. Source
  strings:
  - `entity struct %q (schema labels %q)` / `entity struct %q (schema
    edge %s -[:%s]-> %s)` (C2, unchanged);
  - `entity decode helper %q for entity struct %q` (C5 new);
  - `query %q method` (C1, unchanged);
  - `query %q Params struct` (C1, unchanged);
  - `query %q Row struct` (C1, unchanged);
  - `edgeUnion interface %q for query %q column %d %q` (C5 new).

---

## 5. Emission templates and per-query files — C5 additions

### 5.1 Property → Go type mapping — the C5 new column-shape row

The C4 property → Go type table stands byte-identical at C5 for
every existing row. C5 extends the *column-shape* set — the
`Column.Type` variants Phase A admits — with one new row:

| `Column.Type` | Go type | Nullable emission |
|---|---|---|
| `ResolvedEdgeUnion{EdgeKeys, Nullable}` | `<QueryName><RowFieldName>` (interface) | `<QueryName><RowFieldName>` (nil-carve-out) |

- **Interface, not struct.** The Go type text is the synthesised
  interface name. Consumers type-switch on the interface at the
  call site; the sealed sum guarantees exhaustive coverage under
  `gochecksumtype`.
- **Nullable emission is bare interface, not `*<Interface>`.** The
  ADR 0010 D3 Resolved (lines 343–345) rule: pointer-to-interface
  is anti-pattern; nil is the natural absence value for an
  interface.
- **Parameter-side edgeUnion?** Impossible. Parameters bind schema-
  side property types; the resolver produces `ResolvedProperty` for
  every legal parameter reference (`gqlc-8i0` Q7). An
  edgeUnion-typed parameter would require inline-map or non-
  property parameters (both post-v1); the axis is closed off at
  the parameter table.

### 5.2 `models.go` — the C5 additions

C4's `renderModels` stands for entity struct + decode helper
emission. C5 extends `models.go` with two additions:

**(a) EdgeUnion interface declarations.** After the last entity
struct + decode helper block, one block per per-query-column
edgeUnion, in `Input.Queries` slice order sub-ordered by column
position:

```go
//sumtype:decl
type GetActionAction interface { isGetActionAction() }

//sumtype:decl
type ListActionsAction interface { isListActionsAction() }
```

- **`//sumtype:decl` comment line** above each interface declaration.
  The tool (gochecksumtype) reads the comment as a directive; the Go
  compiler treats it as a bare comment. Consumers who do not run the
  tool see a plain interface; consumers who do get exhaustive-switch
  warnings on type switches over the interface. Zero-cost adoption.
- **Interface method set: one unexported marker.** No other methods.
  The interface is a phantom-marker sealed type; consumers do NOT
  call the marker method — it exists purely to gate satisfaction.
- **One interface per per-query-column edgeUnion.** A query with two
  edgeUnion columns emits two interfaces; a batch with two queries
  each projecting one edgeUnion emits two interfaces. Deduplication
  across queries is banned (§4.10, ADR 0010 D3).

**(b) Marker methods on candidate entity structs.** Emitted beside
each candidate entity struct's decode helper, in interface-emission
order:

```go
type AUTHORED struct { ... }

func (AUTHORED) isGetActionAction() {}
func (AUTHORED) isListActionsAction() {}

func decodeAUTHORED(rel dbtype.Relationship) (AUTHORED, error) { ... }
```

- **Placement — between struct declaration and decode helper.** The
  reader following the struct sees the shape first, the sealed-sum
  marker second, the decode helper third. Grill: place them all
  after the decode helper — rejected because the marker is the
  semantically-relevant piece for consumers (the decode helper is
  the runtime piece); front-loading the semantics reads better.
- **One marker method per interface the entity participates in.**
  Emitted in `Input.Queries` interface-emission order, sub-ordered
  by column position. An entity that participates in three
  interfaces gets three marker methods.
- **Every candidate in every column's `EdgeKeys` slice gets its
  marker.** A candidate absent from an interface's `EdgeKeys` slice
  gets no marker for that interface — the sealed-sum's closed set
  is exactly the resolver's committed set.

**Import gates unchanged.** `dbtype` is already imported for the C2
decode helpers; the edgeUnion emission uses no new import. `fmt` is
already imported for the C2 decode helpers' error wrapping.

**Order deterministic across the whole file.** Entity structs +
their decode helpers + their marker methods come first (in Phase Z
emission order); the pure edgeUnion interface declarations come
last (in `Input.Queries` slice order sub-ordered by column position).
A read through `models.go` proceeds schema-first, query-derived-
second.

### 5.3 Method rendering into `db.go` — unchanged

C4's method rendering into `db.go` stands byte-identical at C5. The
edgeUnion column arm lives in the per-source file (§5.5), not in
`db.go`. `db.go`'s `driverDB.run` is unchanged; its `New` / `WithTx`
seam is unchanged; the sentinel-emission gate for `ErrNoRows` /
`ErrMultipleResults` (based on any `:one` query being present) is
unchanged.

### 5.4 `querier.go` regeneration — unchanged

C4's populated `WriteQuerier` / `ReadQuerier` blocks stand
byte-identical at C5. An edgeUnion-column query adds one line the
same way any other query does — the interface member is the exact
method signature, which for a single-column edgeUnion `:one` is
`(<QueryName><RowFieldName>, error)` and for a `:many` is
`([]<QueryName><RowFieldName>, error)`. The `Querier` embedding
stays `type Querier interface { ReadQuerier; WriteQuerier }`. The
`var _ Querier = (*Queries)(nil)` compile-time assertion still holds
— an edgeUnion method emitted on `*Queries` without being listed in
the appropriate arm would fail to compile at the nested-module fence.

**Import gates: `querierImports` unchanged.** The edgeUnion interfaces
are declared in `models.go` in the same package; `querier.go`
references them by the bare interface name — no cross-package
import needed. The C2 `dbtype` / C3 `time` gates apply unchanged
based on the non-edgeUnion columns / parameters.

### 5.5 The per-source `<name>.cypher.go` file — the C5 edgeUnion arm

C4's per-source file shape stands: query-text const, Params struct
(if two-plus params), Row struct (if two-plus columns), method with
row-assembly body. C5 extends the method-body dispatch with the
`columnEdgeUnion` arm.

**Per-column decode template — `columnEdgeUnion` (single-column
`:one`):**

```go
raw, ok := records[0].Get(<columnName>)
if !ok {
    return <zero>, fmt.Errorf("<Method>: column %q missing from record", <columnName>)
}
if raw == nil {  // omitted for nullable columns
    return <zero>, fmt.Errorf("<Method>: column %q is non-nullable but arrived null", <columnName>)
}
rel, ok := raw.(dbtype.Relationship)
if !ok {
    return <zero>, fmt.Errorf("<Method>: column %q: expected dbtype.Relationship, got %T", <columnName>, raw)
}
switch rel.Type {
case <edgeKeys[0].Label>:
    entity, err := decode<edgeKeys[0].EntityName>(rel)
    if err != nil {
        return <zero>, fmt.Errorf("<Method>: decode column %q: %w", <columnName>, err)
    }
    return entity, nil
// ... one case per EdgeKey in slice order ...
default:
    return <zero>, fmt.Errorf("<Method>: column %q: unexpected relationship type %q", <columnName>, rel.Type)
}
```

- **`<zero>` is the interface's zero value: `nil`.** For a `:one`
  method, the return signature is `(<Interface>, error)`; the zero
  value is `nil` (interface), and the error carries the diagnostic.
- **Nullable column skip the raw==nil gate.** The nullable arm
  reads: `if raw == nil { return nil, nil }` — the nil interface
  is the natural absence value, and the method returns nil-interface
  + nil-error to signal "present column, null value".
- **`rel.Type` type-switch dispatch on the driver's wire label.**
  The driver's `dbtype.Relationship.Type` carries the raw label
  string from Cypher (e.g. `"AUTHORED"`, `"LIKES"`). The dispatch
  keys are the `EdgeKey.Label` values (also raw wire strings), not
  the mangled entity struct names — the driver knows nothing about
  the codegen's mangle.
- **Candidate decode via existing helper.** Each case body calls
  the entity's decode helper (`decode<EntityName>(rel)`), same as
  the C2 single-column edge arm. The decode helper's return type is
  the entity struct value, which satisfies the interface via its
  marker method.

**Per-column decode template — `columnEdgeUnion` (single-column
`:many`):**

Same as `:one`, but wrapped in the C1 `:many` per-record loop:

```go
out := make([]<Interface>, 0, len(records))
for i, record := range records {
    raw, ok := record.Get(<columnName>)
    // ... same body as :one ...
    switch rel.Type {
    case <label0>:
        entity, err := decode<Name0>(rel)
        if err != nil { return nil, fmt.Errorf("<Method>: record %d: decode column %q: %w", i, <columnName>, err) }
        out = append(out, entity)
    // ... one case per EdgeKey ...
    default:
        return nil, fmt.Errorf("<Method>: record %d: column %q: unexpected relationship type %q", i, <columnName>, rel.Type)
    }
}
return out, nil
```

- **Per-record error naming.** The record index is threaded into
  each error message, following the C2 `:many` entity arm's
  precedent. A driver-arrival with a foreign relationship type in
  record 42 of a 1000-record `:many` reads as
  `... record 42: column "action": unexpected relationship type "FOLLOWED"`.

**Per-column decode template — `columnEdgeUnion` (multi-column, Row
struct field):**

Same decode logic as single-column, but the assignment is into a
Row-struct field rather than the return value:

```go
// inside per-record row assembly
raw, ok := record.Get(<columnName>)
// ... same gates ...
switch rel.Type {
case <label0>:
    entity, err := decode<Name0>(rel)
    if err != nil { return <ZeroRow>, fmt.Errorf(...) }
    row.<Field> = entity
// ...
}
```

- **Row-field assignment.** The `entity` local satisfies the
  interface, so the assignment `row.<Field> = entity` type-checks
  under Go's assignability rules.

**List-of-edgeUnion column decode.** A `list<edgeUnion({A, B})>`
column decodes through the C3 list-column arm, with the per-element
dispatch delegated to the same edgeUnion type-switch:

```go
raw, ok := record.Get(<columnName>)
if !ok { ... }
list, ok := raw.([]any)
if !ok { return <zero>, fmt.Errorf(...) }
inner := make([]<Interface>, 0, len(list))
for j, elem := range list {
    if elem == nil {
        return <zero>, fmt.Errorf("<Method>: column %q element %d: unexpected null (list-of-non-null)", <columnName>, j)
    }
    rel, ok := elem.(dbtype.Relationship)
    if !ok { return <zero>, fmt.Errorf(...) }
    switch rel.Type {
    case <label0>:
        entity, err := decode<Name0>(rel)
        if err != nil { return <zero>, fmt.Errorf(...) }
        inner = append(inner, entity)
    // ...
    default:
        return <zero>, fmt.Errorf("<Method>: column %q element %d: unexpected relationship type %q", <columnName>, j, rel.Type)
    }
}
```

- **Element nullability.** The C3 list-element rule (elements of a
  non-nullable list are non-null) holds: a nil element in a list-
  of-edgeUnion is a decode error. The consumer who wants nullable
  elements uses a top-level nullable list (nullable at the list
  level, not the element level).

**Example — single-column `:one` edgeUnion projection:**

```go
// GetAction executes the get-action query.
//
//   MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) WHERE r.since > $since RETURN r
func (q *Queries) GetAction(ctx context.Context, since int64) (GetActionR, error) {
    records, err := q.db.run(ctx, getActionQueryText, map[string]any{"since": since}, neo4j.AccessModeRead)
    if err != nil {
        return nil, err
    }
    if len(records) == 0 {
        return nil, ErrNoRows
    }
    if len(records) > 1 {
        return nil, ErrMultipleResults
    }
    raw, ok := records[0].Get("r")
    if !ok {
        return nil, fmt.Errorf("GetAction: column %q missing from record", "r")
    }
    if raw == nil {
        return nil, fmt.Errorf("GetAction: column %q is non-nullable but arrived null", "r")
    }
    rel, ok := raw.(dbtype.Relationship)
    if !ok {
        return nil, fmt.Errorf("GetAction: column %q: expected dbtype.Relationship, got %T", "r", raw)
    }
    switch rel.Type {
    case "AUTHORED":
        entity, err := decodeAUTHORED(rel)
        if err != nil {
            return nil, fmt.Errorf("GetAction: decode column %q: %w", "r", err)
        }
        return entity, nil
    case "LIKES":
        entity, err := decodeLIKES(rel)
        if err != nil {
            return nil, fmt.Errorf("GetAction: decode column %q: %w", "r", err)
        }
        return entity, nil
    default:
        return nil, fmt.Errorf("GetAction: column %q: unexpected relationship type %q", "r", rel.Type)
    }
}
```

- **`AccessModeRead`** because the resolver typed this as
  `StatementRead` (no write clause). A write-with-projection
  edgeUnion (e.g. `MERGE (:Person)-[r:AUTHORED|LIKES]->(:Post)
  RETURN r`) would use `AccessModeWrite` — the axis is unchanged
  from C4.
- **`ErrNoRows` / `ErrMultipleResults` apply.** The `:one` arity
  gate runs unchanged; edgeUnion is a column-type axis, not a
  cardinality axis.
- **Lint parity (Owner directive, 2026-07-11, extending C4 §5.5).**
  The `errorlint` posture holds: every `fmt.Errorf` uses `%w` for
  wrap, lowercase prefix, no ending punctuation. The
  `stylecheck` posture: the type-switch reads as idiomatic Go.
  `errcheck` and `ineffassign` pass by construction (every err
  is checked or returned).

### 5.6 The `driverDB.run` seam signature stands

C5 does not revise the C4 `driverDB.run` seam signature.
`[]*neo4j.Record` covers the edgeUnion arm; edgeUnion decodes
through the same records the C1/C2/C3/C4 arms decode through. Zero
template change to `db.go`.

### 5.7 Cross-query package-level identifier collision sweep — the C5 hardening

C4's sweep (four sources) becomes C5's (six sources). See §2.2 and
§4.6 above for the enumeration and defence. The sweep runs after
Phase B, in one map insertion pass, in the order:

1. Entity struct names (schema-side vocabulary anchor).
2. Entity decode helpers (`decode<Name>`).
3. Method names.
4. `<Method>Params`.
5. `<Method>Row`.
6. EdgeUnion interface names.

Any duplicate → `ErrIdentifierCollision` naming both sources.

- **Test coverage.** Two new invalid fixtures cover the two new
  collision axes (§6.4): (a) an entity struct whose name mangles to
  match a synthesised edgeUnion interface name, (b) a query name
  that produces a `<Method>Row` colliding with an entity decode
  helper's name. Both axes route to `ErrIdentifierCollision`
  through the hardened sweep.

---

## 6. The golden harness — C5 revision

C0 §6's harness stands: the `test/data/codegen/{valid,invalid}`
layout, the nested Go module, the `manifest.json` shape, the
`-update` flag, the testify suites, the compile fence. C4 §6.6's
lint parity applies transitively. C5 revises the fixture set only,
not the harness code.

### 6.1 Fixture strategy

C4's discipline stands (fixture-per-capability, one schema per
fixture). C5 adds valid fixtures for each edgeUnion capability slice
(§5) plus a small set of negative fixtures for the two new collision
axes and the renamed `ErrOutOfC5Scope` catchment. The nested module's
`go.mod` (pin `neo4j-go-driver/v5 v5.28.4`) does not change — the C5
emissions add no new imports. Existing C0 / C1 / C2 / C3 / C4 valid
fixtures whose golden set contains no edgeUnion column stay byte-
identical; they emit no `models.go` interface block, no marker
methods, no per-source edgeUnion arm.

### 6.2 C5 valid fixtures

Under `test/data/codegen/valid/`, each new directory holds a
`schema.gql`, one or more `.cypher` files, a `manifest.json`, and a
`golden/` subdirectory with the complete generated package:

| Fixture | Coverage |
|---|---|
| `edge_union_one_two_candidates` | `MATCH (:Person)-[r:AUTHORED\|LIKES]->(:Post) RETURN r :one` — canonical two-candidate edgeUnion `:one`. Reincarnates the C4 `out_of_c4_scope_edge_union` fixture as the honest positive case. Exercises interface emission, marker methods on two candidates, decode dispatch, `//sumtype:decl` line. |
| `edge_union_many_two_candidates` | `MATCH (:Person)-[r:AUTHORED\|LIKES]->(:Post) RETURN r :many` — the `:many` sibling; per-record loop with per-record error index. |
| `edge_union_three_candidates` | Three-candidate edgeUnion — verifies the type-switch scales; three cases in slice order; three marker methods per candidate. |
| `edge_union_nullable_column` | Nullable edgeUnion column via `OPTIONAL MATCH` (R4 nullability flow-typing). Exercises the nil-interface carve-out — no `raw == nil` gate; nil-interface propagates to the Row field / return value. |
| `edge_union_multi_column_row` | `:one` with two columns, one edgeUnion + one property. Exercises the Row struct with an edgeUnion field; the field is the interface type; the row-assembly decode arm assigns the entity value to the interface field. |
| `edge_union_two_edge_union_columns` | `:one` returning two edgeUnion columns (`RETURN r1, r2`). Exercises the sub-ordering-by-column-position rule; emits two interfaces with distinct names; each candidate participating in both gets two marker methods. |
| `edge_union_list` | `RETURN collect(r) AS actions :one` where `r` is an edgeUnion. Exercises the list-of-edgeUnion recursion via `resolvedListGoType`; return type is `[]<Interface>`; per-element decode dispatch. |
| `edge_union_write_projection` | `MERGE (:Person)-[r:AUTHORED\|LIKES]->(:Post) RETURN r :one` — write-with-projection edgeUnion. Exercises `AccessModeWrite` on the run call; edgeUnion column decode is otherwise identical. |
| `edge_union_two_queries_same_column_shape` | Two queries in one batch, both projecting the same multi-type edge under different query names (`GetAction`, `ListActions`). Exercises the per-query-column naming: two interfaces (`GetActionR`, `ListActionsR`) — no deduplication. Each candidate entity struct gains two marker methods. |

Nine new valid fixtures. Each is one `schema.gql`, at least one
`.cypher` file, a `manifest.json`, and a `golden/` tree. The
`golden/` trees compile under the C0 compile fence against
`neo4j-go-driver/v5 v5.28.4` — the `dbtype.Relationship.Type` field
access, the `record.Get` return shape, and the marker-method
satisfaction all type-check against the pinned driver.

### 6.3 Schema fixture text — illustrative

`test/data/codegen/valid/edge_union_one_two_candidates/schema.gql`:

```gql
CREATE PROPERTY GRAPH TYPE EdgeUnionOneTwoCandidates AS {
    (:Person { id :: INT64 NOT NULL }),
    (:Post   { id :: INT64 NOT NULL }),
    (:Person) -[:AUTHORED { since :: INT64 NOT NULL }]-> (:Post),
    (:Person) -[:LIKES]-> (:Post)
}
```

Paired query file
`test/data/codegen/valid/edge_union_one_two_candidates/queries.cypher`:

```cypher
// name: GetAction :one
MATCH (:Person)-[r:AUTHORED|LIKES]->(:Post) RETURN r
```

Resolved: `Columns = [{Name: "r", Type: ResolvedEdgeUnion{EdgeKeys:
[{Person, AUTHORED, Post}, {Person, LIKES, Post}], Nullable: false}}]`,
`Parameters = []`, `Statement = StatementRead`, `Cardinality =
CardinalityOne`. Phase Z emits `Person`, `Post`, `AUTHORED`, `LIKES`
entity structs. Phase A: `(One, Read, 1)` — admitted; the column
resolves as `ResolvedEdgeUnion` — admitted at C5 (was
`ErrOutOfC4Scope` at C4). Phase B: method name `GetAction`, no
parameters, single-column `:one` with `columnEdgeUnion` kind;
synthesised interface name `GetActionR`. `models.go` emits both
entity structs' decode helpers plus `isGetActionR()` marker methods
on `AUTHORED` and `LIKES` plus the `//sumtype:decl` line + interface
declaration. `queries.cypher.go` emits the single-column `:one`
method with the type-switch dispatch on `rel.Type`. Return type is
`(GetActionR, error)`.

`test/data/codegen/valid/edge_union_nullable_column/queries.cypher`:

```cypher
// name: MaybeAction :one
MATCH (p:Person) WHERE p.id = $id
OPTIONAL MATCH (p)-[r:AUTHORED|LIKES]->(:Post)
RETURN r
```

Resolved: `Columns = [{Name: "r", Type: ResolvedEdgeUnion{EdgeKeys:
[...], Nullable: true}}]` — nullable per R4 OPTIONAL MATCH flow-
typing. Emitted decode arm skips the `raw == nil` non-null gate;
returns nil interface + nil error when the column arrives null.

### 6.4 C5 invalid fixtures — the new + renamed set

Added under `test/data/codegen/invalid/`:

| Fixture | Sentinel | Coverage |
|---|---|---|
| `identifier_collision_edge_union_interface_entity` | `ErrIdentifierCollision` | Schema declares an entity struct name that collides with a synthesised edgeUnion interface. Exercises sweep source (6) vs source (1). |
| `identifier_collision_decode_helper_method` | `ErrIdentifierCollision` | A query whose method name matches an entity decode helper name (`decodeSomething`). Exercises sweep source (2) vs source (3) — proves the C5 decode-helper promotion catches a case the C4 sweep would have missed. |
| `out_of_c5_scope_non_property_parameter` | `ErrOutOfC5Scope` | `$p :: ResolvedNode` — non-property parameter, still post-v1. Renamed from `out_of_c4_scope_non_property_parameter`. |

Three invalid fixtures — two new (both `ErrIdentifierCollision`
axes) plus one renamed from `out_of_c4_scope_*`. The retired
`out_of_c4_scope_edge_union` fixture (the C4 sentinel for the
`ResolvedEdgeUnion` catchment) reappears in `valid/` as
`edge_union_one_two_candidates` (the honest positive case). The C5
retirement is a clean cut: C4's edgeUnion catchment retires because
edgeUnion is now in-scope.

The C0 / C1 / C2 / C3 / C4 invalid fixtures whose sentinel is
unchanged (`invalid_package_name`, `duplicate_query_name`,
`duplicate_source_file`, `invalid_cardinality`,
`param_name_collision`, `row_field_collision`,
`alias_required_function_call`, `alias_required_expression`,
`identifier_collision_reserved`, `invalid_entity_name_node`,
`invalid_entity_name_edge`, `unnamed_multi_label_type`,
`property_field_collision`, `identifier_collision_entity_row`,
`unrepresentable_width_int128_schema`,
`unrepresentable_width_uint256_schema`,
`unrepresentable_width_float16_schema`,
`unrepresentable_width_decimal_schema`,
`unrepresentable_width_int128_parameter`,
`unrepresentable_width_float128_list_column`,
`exec_on_projection_read`, `exec_on_projection_write`,
`cardinality_shape_zero_column_read_one`,
`cardinality_shape_zero_column_write_one`,
`cardinality_shape_zero_column_write_many`) stay unchanged.

### 6.5 Determinism — C5 additions

C0's `TestDoubleRun` runs unchanged. C5's kernel adds no new ordered
surfaces: the edgeUnion admission runs per-query in `Input.Queries`
slice order; the Phase B synthesis walks the same slice; the
`models.go` interface block walks the same slice sub-ordered by
column position; the marker methods on entity structs walk the entity
emission order sub-ordered by interface-emission order. Every
ordered surface is either the C4 `Input.Queries` order or a
deterministic derivation of it.

### 6.6 Non-obvious harness invariants — C5 additions

C4's §6.6 invariants stand. C5 adds:

- **Every valid fixture's `golden/models.go` and `golden/*.cypher.go`
  compile under the C5 templates.**
  `test/data/codegen/go.mod` still pins `neo4j-go-driver/v5 v5.28.4`;
  the emitted `dbtype.Relationship.Type` field access and the
  marker-method satisfaction all type-check against the pinned
  driver. Any driver-side signature drift at a version bump would
  fail the compile fence at the version bump.
- **Every `golden/models.go` regenerates on the C4→C5 diff.** For
  fixtures that project no edgeUnion column, the diff is nil (no
  interface block, no marker methods). For edgeUnion-projecting
  fixtures, the diff is the addition of the interface block + marker
  methods. `-update` rewrites the goldens; the compile fence catches
  any template regression at the version bump.
- **`gochecksumtype` opt-in verification.** The `//sumtype:decl` line
  is a comment; the compile fence does not verify exhaustiveness. A
  future CI recipe running `gochecksumtype ./...` against the nested
  module would surface the exhaustiveness guarantee. Not added at
  C5; noted for future tooling.
- **Owner directive (2026-07-11) — lint-clean parity.** Every emitted
  file must lint-clean under gqlc's `.golangci.yml`. The edgeUnion
  emissions are structurally similar to the C2 entity emissions modulo
  the type-switch dispatch. The `.golangci.yml` posture (no `//nolint`
  directives at file heads) still holds. `errorlint` wrapping
  discipline: every `fmt.Errorf` uses `%w` for wrap, lowercase prefix,
  no ending punctuation. `stylecheck` posture: the type-switch reads
  as idiomatic Go. `errcheck` and `ineffassign` pass by construction
  (every err is checked or returned, no ineffectual assigns in the
  case bodies).

---

## 7. C5 capability scope — what emits

**In scope:** an `Input` whose:

- `Schema.Nodes` and `Schema.Edges` produce entity struct names via
  Rules 1–4 without failure (C2 invariant), and every property on
  every entity has a representable width (Rule 6 at C3 §4.8).
- Every `NamedQuery` still satisfies C4's admission (cardinality ×
  shape gate, reserved-identifier check, etc.).
- Column resolved types now include `ResolvedEdgeUnion`; every
  `EdgeKey` in an edgeUnion's slice has a Phase Z schema-cache entry;
  `len(EdgeKeys) >= 2` (defensive gate — the resolver guarantees this).
- List-of-edgeUnion columns recurse cleanly through
  `resolvedListGoType`.

**Out of scope, routed to the appropriate sentinel:**

| Construct                                                    | Sentinel                          | Stage owner |
|--------------------------------------------------------------|-----------------------------------|-------------|
| Non-property parameter (whole node/edge, temporal literal, etc.) | `ErrOutOfC5Scope`             | Post-v1     |
| Query text containing a Go raw-string-hostile backtick       | `ErrOutOfC5Scope`                 | C5-or-later |
| `:exec` cardinality on `len(Columns) > 0`                    | `ErrExecOnProjection`             | —           |
| `:one` / `:many` cardinality on `len(Columns) == 0`          | `ErrCardinalityShapeMismatch`     | —           |
| `ResolvedProperty` column / parameter with INT128 / INT256   | `ErrUnrepresentableWidth`         | —           |
| `ResolvedProperty` column / parameter with UINT128 / UINT256 | `ErrUnrepresentableWidth`         | —           |
| `ResolvedProperty` column / parameter with FLOAT16           | `ErrUnrepresentableWidth`         | —           |
| `ResolvedProperty` column / parameter with FLOAT128 / FLOAT256 | `ErrUnrepresentableWidth`       | —           |
| `ResolvedProperty` column / parameter with DECIMAL           | `ErrUnrepresentableWidth`         | —           |
| Schema property with any of the eight unrepresentable widths | `ErrUnrepresentableWidth`         | —           |
| `list<T>` with an unrepresentable leaf                        | `ErrUnrepresentableWidth`         | —           |
| Explicit `NodeType.Name` / `EdgeType.Name` not a valid ident | `ErrInvalidEntityName`            | —           |
| Multi-label node type without explicit `Name`                | `ErrUnnamedMultiLabelType`        | —           |
| Ambiguous edge label without explicit `Name`                 | `ErrUnnamedMultiLabelType`        | —           |
| Two properties on one entity mangling to one field           | `ErrPropertyFieldCollision`       | —           |
| Method name matches reserved identifier                      | `ErrIdentifierCollision`          | —           |
| Two params mangling to one field                             | `ErrParamNameCollision`           | —           |
| Two columns deriving to one Row field                        | `ErrRowFieldCollision`            | —           |
| Column text neither bare-ident nor prop-access               | `ErrAliasRequired`                | —           |
| Two emitted top-level identifiers colliding (any of six axes) | `ErrIdentifierCollision`         | —           |

**Silently accepted (not routed anywhere):**

- Empty `Schema.Nodes` and `Schema.Edges` (unchanged from C4).
- Schema node type or edge type with zero properties (unchanged).
- `Validated.Distinct == true` — unchanged from C1.
- `Validated.Columns[i].GroupingKey` — unchanged from C1.
- Comments in the query text — unchanged from ADR 0005.
- `list<list<...<unknown>>>` — the `any` fallback propagates
  through the recursion (C3 §7).
- `RETURN null AS n :one` — the `any` return is legal-but-pointless
  (C3 §7).
- A `:exec` on a `StatementRead` query with zero columns.
- A write-with-projection edgeUnion.
- A batch with only writes.
- A `:many` edgeUnion with zero rows — legal, returns `[]<Interface>{}`.
- A nullable edgeUnion column — returns nil interface value.

**The C1 / C2 / C3 / C4 shape stands unchanged** for anything C5
does not touch: package-name derivation (C0 §5.1), generated-file
header (C0 §5.2), `Queries` handle constructors, `driverOrTx`
interface shape, `txDB` behaviour, the sentinel-set discipline (with
the C5 rename), the double-run determinism test, the compile fence,
the entity-naming rules, the property-field mangle rule, the
property → Go type table, the `resolvedListGoType` recursion (with
the C5 edgeUnion arm), the eager width sweep, the cardinality ×
shape gate, the `WriteQuerier` population, the `driverDB.run` write
arm.

---

## 8. Compile fence (unchanged)

C0 `just test-codegen-fence` (`cd test/data/codegen && go build
./... && go vet ./...`) covers C5's emissions without change: the
nested module builds every fixture's `golden/` tree, so every
edgeUnion interface declaration, every marker method, every type-
switch dispatch, and every `dbtype.Relationship.Type` field access
type-checks against the pinned driver. Failure modes:

- **A template regression in the edgeUnion emission.** The fence
  fails with the standard Go compiler error naming the file and
  line — same diagnostic quality as C1/C2/C3/C4.
- **A `dbtype.Relationship` shape drift.** Bumping
  `neo4j-go-driver/v5` may reshape the struct (e.g., v6 renames
  `.Type` to `.Label` — hypothetical); the fence catches at the
  version bump. The D7 standing instruction directs re-verification
  at each stage spec cycle, honored above (2026-07-11).
- **A marker-method satisfaction drift.** A future codegen change
  that renamed the marker method inconsistently across the interface
  declaration and the entity satisfier would fail with a "does not
  implement" error naming both sites.

C5 does not add a second fence recipe. C4 §5.5's lint parity
directive extends transitively to the C5 emissions; if CI runs
`golangci-lint` against the nested module, this is enforced
automatically.

---

## 9. Sentinel set delta — the C5 view

C4's fifteen sentinels stand at C5 with one rename and zero
additions. C5 renames `ErrOutOfC4Scope` → `ErrOutOfC5Scope` for the
same reason C4 renamed from C3 (per-stage rename discipline, §9 of
C4 spec defence extends). C5 adds no new sentinel: edgeUnion is a
capability admission, not a rejection axis; the hardened collision
sweep reuses `ErrIdentifierCollision`.

**Renamed sentinel:**

```go
// ErrOutOfC5Scope is returned when a C5-admissible input carries a
// construct C5 does not project: a non-property parameter (post-v1;
// whole-node / whole-edge / scalar-literal / list / unknown / bare-
// temporal-expression parameter is still out of scope), or a query
// text carrying a raw-string-hostile backtick. Category-grained per
// C0's precedent. Renamed from ErrOutOfC4Scope at C5 —
// ResolvedEdgeUnion retires from the catchment (edgeUnion is now
// in-scope via the sealed-interface pattern).
var ErrOutOfC5Scope = errors.New("out of C5 scope")
```

**Retired at C5:** `ErrOutOfC4Scope` — the constant is dropped from
the package, the fixtures rename to `out_of_c5_scope_*`, and the
`sentinelByName` map's entry renames. The retirement is a clean cut:
no `//nolint:staticcheck` for a lingering alias, no deprecation
window.

**Naming defence — `ErrOutOfC5Scope`, per-stage rename on its own
merits.** C4's §9 defence extends verbatim: the failing surface is
textually different at every stage boundary; `errors.Is` consumers
who branched on `ErrOutOfC4Scope` break at C5 — this is desirable,
they were claiming knowledge of C4's scope, and C5 has revised what
"out of scope" means (edgeUnion is now in). Grill options (a)
freeze, (b) neutral (`ErrOutOfCurrentScope`), (c) per-stage: picked
(c). The `staged rename → staging observable at the error site`
axis holds.

**Rejected — a new sentinel for edgeUnion admission failures
(`len(EdgeKeys) < 2`, unknown candidate).** The two defensive gates
described in §2.1 (a) route through `ErrOutOfC5Scope` because the
resolver guarantees the invariants; a violation is an internal-
invariant failure catchable at generation but not usefully addressed
by a named sentinel. Same discipline as C1/C2/C3/C4's "Phase A missed
non-property type" defensive gate.

**Rejected — a new sentinel for edgeUnion interface-name collisions
(`ErrEdgeUnionInterfaceCollision`).** The collision is one axis of
the six the sweep covers; naming it separately would fragment
`ErrIdentifierCollision` without semantic benefit. Consumers gate
on `errors.Is(err, ErrIdentifierCollision)` uniformly, and the fail
message discriminates which axis fired. Same discipline as C2's
"add entity-struct sweep source without adding a sentinel" — the
sweep grew, the sentinel did not.

**Closed set for the C5 sweep.** `allSentinels` at C5:

```go
var allSentinels = []error{
    ErrInvalidPackageName,        // C0
    ErrDuplicateSourceFile,       // C0
    ErrDuplicateQueryName,        // C0
    ErrInvalidCardinality,        // C0
    ErrOutOfC5Scope,              // C5 (renamed from ErrOutOfC4Scope)
    ErrParamNameCollision,        // C1
    ErrRowFieldCollision,         // C1
    ErrAliasRequired,             // C1
    ErrIdentifierCollision,       // C1 (C5 hardens sweep coverage)
    ErrInvalidEntityName,         // C2
    ErrUnnamedMultiLabelType,     // C2
    ErrPropertyFieldCollision,    // C2
    ErrUnrepresentableWidth,      // C3
    ErrExecOnProjection,          // C4
    ErrCardinalityShapeMismatch,  // C4
}
```

Fifteen sentinels — same count as C4. `ErrFormatFailure` stays
excluded (C0 §9.2 rationale unchanged). Every C5 member has at least
one negative fixture (§6.4); the reachability sweep is C0's
`TestSentinelReachability` unchanged.

---

## 10. Out-of-scope table

Every downstream capability C5 does not deliver, with the stage that
owns it. Read as ADR 0010 D7 unpacked to the C5-vs-later boundary
(C4's version tightens as C5's slice retires the edgeUnion +
collision-sweep-hardening axis):

| Capability                                          | Stage owner |
|-----------------------------------------------------|-------------|
| Raw-string-hostile query text (backtick escape / fallback) | C5-or-later (deliberately deferred; not on any bead) |
| Non-property parameters (whole node, whole edge, temporal literal, scalar literal, list, unknown) | Post-v1 |
| Version-stamp polish (`-ldflags -X` wiring)         | C6          |
| Session-config polish (transaction timeouts, metadata) | C6       |
| `gqlc-0aa` re-scope against D4's no-runtime-package decision | C6 |
| `:iter` streaming cardinality (fourth enum value)   | `gqlc-1a5` (post-v1) |
| Configuration file (`gqlc.yaml` analogue), CLI     | future config effort |
| Disk writes, out-dir sync (stale deletion)          | future CLI effort |

Rows above the `gqlc-1a5` line are staged by ADR 0010 D7; the last
two are ADR 0010 D6 futures. C5 closes out the "hard residue" bead
(`gqlc-8i0.7`); C6 (`gqlc-8i0.8`) is the polish sweep before v1 ship.

---

## 11. Definition of done for C5 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is
out of scope of this document. The spec is done when:

1. This file exists at `docs/specs/codegen-stage-c5.md`, committed
   on branch `codegen-c5-spec`.
2. §3 pins the C5 emitted surface additions (edgeUnion interface
   names in method returns / Row-field types, marker methods on
   candidate entity structs, `//sumtype:decl` annotation) and
   confirms the unchanged property → Go type table and the unchanged
   C1 seam signature.
3. §4 gives the new naming-kernel rule — the edgeUnion synthesis
   rule (§4.10) — and defends the per-query-column naming discipline
   against the two rejected alternatives (schema-derived dedup,
   query-suffix-only).
4. §4.6 / §5.7 give the hardened cross-query collision sweep with
   its six identifier sources and defend the C5 additions (entity
   decode helpers promoted from off-sweep, edgeUnion interface names
   as a new axis).
5. §5 gives the emission templates: the unchanged property → Go type
   table plus the new column-shape row (§5.1), the extended
   `models.go` with interface + marker methods (§5.2), the unchanged
   `db.go` (§5.3), the unchanged `querier.go` (§5.4), the C5 method-
   body arm including single-column and multi-column and list-of-
   edgeUnion (§5.5), the unchanged seam (§5.6), the hardened sweep
   (§5.7).
6. §9 names and defends the rename (`ErrOutOfC4Scope` →
   `ErrOutOfC5Scope`); confirms the closed set of fifteen. Defends
   the "no new sentinel" decision (edgeUnion is admission, collision
   axes route through `ErrIdentifierCollision`).
7. §6 designs the fixture set: the nine valid fixtures (§6.2), the
   three invalid fixtures (§6.4), the retirement of
   `out_of_c4_scope_edge_union` into `edge_union_one_two_candidates`,
   the rename (`out_of_c4_scope_non_property_parameter` →
   `out_of_c5_scope_non_property_parameter`), the fixture-per-
   capability discipline.
8. §7 states the C5 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel each construct routes
   to and the stage that owns the next widening.
9. §8 confirms the C0 compile fence covers C5 emissions without
   change; §6.6 flags the linting-parity owner directive extending
   transitively.
10. §12 gives the fixture-count summary.
11. §10 enumerates every downstream capability with its stage owner
    (post-C5, only C6 remains before v1).
12. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer);
every blocker he raises is fixed on this same branch before the
branch merges. Cycle 2 (the C5 code cycle,
`codegen-c5-implementation` stacked on this branch) begins only when
the spec cycle merges.

---

## 12. Fixture-count summary

- **C0 valid fixtures kept:** `skeleton`, `queries_ignored`.
- **C1 valid fixtures kept:** `one_col_one_param_one`,
  `one_col_many`, `many_col_one_row`, `many_col_many`,
  `nullable_columns`, `nullable_parameter`, `multi_source_files`,
  `alias_bare_variable_ambiguity`, `all_widths`.
- **C2 valid fixtures kept:** `entity_node_projected_one`,
  `entity_edge_projected_one`, `entity_node_and_scalar_row`,
  `entity_node_many`, `entity_nullable_node`, `entity_explicit_name`,
  `entity_multi_label_named`, `entity_edge_ambiguous_label_named`,
  `entity_zero_property_node`, `entity_with_nullable_property`.
- **C3 valid fixtures kept (20):** `temporal_column_date`,
  `temporal_column_datetime`, `temporal_column_localtime`,
  `temporal_column_localdatetime`, `temporal_column_duration`,
  `temporal_column_time`, `property_date`, `property_timestamp`,
  `float32_column`, `float32_parameter`, `list_int`, `list_string`,
  `list_list_int`, `list_entity`, `list_nullable`, `list_unknown`,
  `scalar_null`, `scalar_map`, `unknown_column`,
  `many_columns_mixed_temporal_list`.
- **C4 valid fixtures kept (13):** `write_exec_delete`,
  `write_exec_zero_param`, `write_exec_two_params`, `write_exec_read`,
  `write_one_projection_entity`, `write_one_projection_property`,
  `write_many_projection_entity`, `write_many_projection_property`,
  `mixed_read_write_batch`, `write_exec_nullable_param`,
  `write_exec_float32_param`, `write_exec_temporal_param`,
  `write_only_batch`.
- **C5 valid fixtures added (9):** `edge_union_one_two_candidates`,
  `edge_union_many_two_candidates`, `edge_union_three_candidates`,
  `edge_union_nullable_column`, `edge_union_multi_column_row`,
  `edge_union_two_edge_union_columns`, `edge_union_list`,
  `edge_union_write_projection`,
  `edge_union_two_queries_same_column_shape`.
- **C0 invalid fixtures kept:** `invalid_package_name`,
  `duplicate_query_name`, `duplicate_source_file`,
  `invalid_cardinality`.
- **C1 / C2 / C3 invalid fixtures kept:** `param_name_collision`,
  `row_field_collision`, `alias_required_function_call`,
  `alias_required_expression`, `identifier_collision_reserved`,
  `invalid_entity_name_node`, `invalid_entity_name_edge`,
  `unnamed_multi_label_type`, `property_field_collision`,
  `identifier_collision_entity_row`,
  `unrepresentable_width_int128_schema`,
  `unrepresentable_width_uint256_schema`,
  `unrepresentable_width_float16_schema`,
  `unrepresentable_width_decimal_schema`,
  `unrepresentable_width_int128_parameter`,
  `unrepresentable_width_float128_list_column`.
- **C4 invalid fixtures kept:** `exec_on_projection_read`,
  `exec_on_projection_write`,
  `cardinality_shape_zero_column_read_one`,
  `cardinality_shape_zero_column_write_one`,
  `cardinality_shape_zero_column_write_many`.
- **C4 invalid fixtures renamed / retired:**
  `out_of_c4_scope_non_property_parameter` →
  `out_of_c5_scope_non_property_parameter`.
  `out_of_c4_scope_edge_union` retires from the invalid set and
  reappears in `valid/` as `edge_union_one_two_candidates`.
- **C5 invalid fixtures added (3):**
  `identifier_collision_edge_union_interface_entity`,
  `identifier_collision_decode_helper_method`,
  `out_of_c5_scope_non_property_parameter` (rename of
  `out_of_c4_scope_non_property_parameter`).

**Totals at C5:**
- Valid fixtures: 2 (C0) + 9 (C1) + 10 (C2) + 20 (C3) + 13 (C4) + 9
  (C5) = 63.
- Invalid fixtures: 4 (C0) + 16 (C1/C2/C3 kept) + 5 (C4 kept) + 3
  (C5 new including one rename) = 28. (`out_of_c4_scope_edge_union`
  retires from the invalid set into `valid/`.)
- Sentinels in `allSentinels`: 15.
- Every sentinel has ≥1 invalid fixture; the reachability sweep is
  C0's `TestSentinelReachability` unchanged.
