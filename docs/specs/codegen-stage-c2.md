# Stage C2 spec — codegen: entity projections

The implementation brief for Stage C2 of `internal/codegen`, extending
the merged C1 slice (`docs/specs/codegen-stage-c1.md`) with the two
capabilities ADR 0010 D7 places at C2: **schema-shaped entity structs
emitted into `models.go` for every node and edge type in the schema**,
and **entity column projection (`ResolvedNode` / `ResolvedEdge`) landing
those structs on Row-struct fields via per-query row assembly**. Build
this **test-first**. Scope, sequencing, and error posture inherit from
ADR 0010 and the C0 / C1 specs unchanged; this document revises only
the sections C2 touches.

Stage C2 keeps the C1 file set (`db.go` / `querier.go` /
`<name>.cypher.go`) byte-identical for the parts C2 does not touch,
fills `models.go` with one exported entity struct per schema node type
and edge type plus their unexported decode helpers, extends the C1
naming kernel with the entity-naming rules ADR 0010 D3 Resolved pins
(explicit `Name` first, single-label mangle fallback, eager check that
every multi-label type and every ambiguous edge label carries an
explicit `Name`), extends the C1 property→Go type table with two new
column shapes (`ResolvedNode`, `ResolvedEdge`), and extends the C1
per-query row assembly with a call to the entity decode helper for
every entity column. Every entity struct is schema-shaped only —
`Property.Name` fields through the C1 mangle, no `ElementId`, no
start/end ids, no `Id` (D3 Resolved: driver identity is a projection,
not a schema property; a query saying `RETURN p, elementId(p) AS id`
puts identity on the Row struct beside the entity). Still-deferred
column shapes (`ResolvedEdgeUnion` at C5; `ResolvedList`,
`ResolvedTemporal`, `ResolvedScalar`, `ResolvedUnknown` at C3; the C3
property widths temporals share) continue to route to
`ErrOutOfC2Scope` (the renamed `ErrOutOfC1Scope` — the retirement is
in §9). Writes (`:exec`, `WriteQuerier` population, `ExecuteWrite`
path) stay out of scope and continue to route through the same
scope-sentinel; C4 owns them (ADR 0010 D7).

---

## 1. Deliverables

- `internal/codegen/generate.go` — extended with an entity naming
  sub-kernel (§4.5), the property→Go type table's two new
  column-shape rows (`ResolvedNode`, `ResolvedEdge`, §5.1), entity
  struct + decode helper rendering into `models.go` (§5.2), the
  eager multi-label / ambiguous-edge-label check that fires before
  the per-query Phase A walk (§4.5, §5.2), and the per-query row
  assembly's entity-column arm (§5.5). The C0 file layout stands
  (`codegen.go` / `input.go` / `errors.go` / `generate.go`); no new
  files are introduced at C2.
- `internal/codegen/errors.go` — extended with three new sentinels
  and one rename (§9): `ErrInvalidEntityName`,
  `ErrUnnamedMultiLabelType`, `ErrPropertyFieldCollision`, plus
  `ErrOutOfC1Scope` → `ErrOutOfC2Scope`. `ErrIdentifierCollision`
  (C1) covers cross-source exported-identifier collisions the C2
  additions expose (entity struct vs entity struct, entity struct vs
  method, entity struct vs Row struct); no distinct
  `ErrEntityNameCollision` is introduced (§9 defends).
- `test/data/codegen/valid/<name>/` — new C2 valid fixtures (§6.2),
  each with a schema `.gql` producing one or more entity structs,
  optionally query files projecting those entities, a
  `manifest.json`, and a `golden/` subdirectory whose `models.go` is
  now non-empty for every C2 fixture (and non-empty for the
  regenerated C0 / C1 goldens whose schemas contain any node/edge
  type).
- `test/data/codegen/invalid/<name>/` — new C2 negative fixtures for
  each of the three new sentinels plus the renamed
  `ErrOutOfC2Scope` (§6.4).
- `internal/codegen/codegen_test.go` — no structural change (the C0
  harness scales to C2 fixtures without churn); the `sentinelByName`
  map grows three rows and renames one.

Nothing downstream of entity projections is built. Collections +
temporals + unrepresentable-width sentinels + FLOAT32 schema-width
contract (C3), writes + `:exec` + cardinality × shape rejection
(C4), `edgeUnion` sealed interfaces + package-level collision-sweep
hardening (C5), version-stamp polish (C6), `:iter` streaming (post-
v1, `gqlc-1a5`) stay for their owning stage per ADR 0010 D7.

---

## 2. Architecture — deltas from C1

C1's architecture (§2 of the C1 spec) stands: the `Generator` seam,
the concrete `*Codegen` return, the empty `Option` surface, the
purity / determinism / short-circuit posture, and the
`generate.go` / `generate` kernel split are unchanged. C2 extends
the kernel with an entity-emission phase that runs **before Phase A**
(§2.1), extends the C1 per-query loop's Phase A / Phase B with
entity-column admission and entity-field derivation (§2.1), and
extends the cross-query collision sweep (§4.4 at C1) to include
entity struct names as a fourth identifier source (§4.6). No new
exported types except the three new sentinels; no API-shape delta
(§3 below); the `Input` struct stays `{Schema, Queries}` (ADR 0010
D6).

### 2.1 The C2 kernel structure

The kernel remains one linear pass with early returns. C2 inserts a
new **Phase Z — schema-shape admission and entity naming** that runs
before C1's Phase A, and extends Phase A / Phase B with the entity-
column rules:

- **Phase Z — schema-shape admission and entity naming**
  (§4.5, §5.2). One sweep over `Schema.Nodes` (in `graph.LabelSetKey`
  order per D5) and `Schema.Edges` (in `EdgeKey` triple-lex order per
  D5): for each node type and edge type derive its entity struct
  name via the entity-naming rules (§4.5); check that an explicit
  `Name` (when non-empty) is a valid exported Go identifier; check
  that a multi-label node type or an ambiguous edge label (one whose
  `Label` appears in more than one `EdgeKey`) has been given an
  explicit `Name`. First offender wins. Phase Z is EAGER — it fires
  on every schema regardless of whether any query projects the
  offending type — because a lazy check would make output depend on
  the query set in a way ADR 0010 D3 Resolved explicitly rejects.
  Also runs a per-entity property-field derivation pass: for every
  property on the type, mangle `Property.Name` via §4.2's rule; a
  same-entity derived-field collision is
  `ErrPropertyFieldCollision`. The results (entity struct name +
  ordered field list) are cached in a `preparedEntity` slice the
  emission walk (§5.2) reads.
- **Phase A — batch admission** (unchanged shape, extended admission
  set). Every `NamedQuery` still passes C0's `validateQueries` gate
  and the C1 per-query admission checks. C2 widens the admissible
  `Column.Type` set from `{ResolvedProperty}` to
  `{ResolvedProperty, ResolvedNode, ResolvedEdge}`; a
  `ResolvedEdgeUnion` / `ResolvedScalar` / `ResolvedTemporal` /
  `ResolvedList` / `ResolvedUnknown` column still routes to
  `ErrOutOfC2Scope`. A `ResolvedNode` column whose `Labels` does
  not correspond to any `Schema.Nodes` entry is not admissible —
  the resolver's R0 gate rules that out already (unknown node
  type), so no new codegen sentinel; if it slips past (a synthetic
  test seam), `ErrOutOfC2Scope` fires naming the mismatch.
  `ResolvedNode` on `Parameters` and `ResolvedEdge` on `Parameters`
  are NOT admissible — C1's parameter admission stays property-only
  (D3 Resolved's symmetric treatment applies to the axis, not to
  the shape; nodes / edges are not natural parameter types today, a
  future stage may widen). Property columns / parameters retain C1's
  unrepresentable-width check.
- **Phase B — per-query name derivation** (unchanged shape, extended
  derivation set). Row-field text-shape analysis is unchanged;
  Params-field mangle is unchanged. C2 extends the Row-field type
  mapping: a `ResolvedNode` column's Go type text is its entity
  struct name (looked up by `Labels` in the Phase Z result cache); a
  `ResolvedEdge` column's Go type text is its entity struct name
  (looked up by `EdgeKey`). Nullable entity column renders `*<Name>`
  uniformly with C1's property nullability rule (D3 Resolved:
  `Nullable` → pointer, uniformly). Row-field collision rules
  (§4.3) are unchanged — the check is on derived field name, which
  came from `Column.Name`, not from the type; two entity columns
  named `p` and `q` still derive `P` and `Q` and never collide.

Phase Z runs before Phase A because Phase A reads Phase Z's cached
entity struct names to type-check a `ResolvedNode` / `ResolvedEdge`
column's Go type text (§5.5's row-assembly template needs the entity
name to render the decode helper call). Phase A alone never fails on
a schema-shape axis — every schema-shape rejection is Phase Z's.
Phase B's row-field derivation runs on `Column.Name` (unchanged),
independent of the type; the type informs only the Row struct field's
Go type text.

The cross-query package-level collision sweep (§4.6, extending C1's
§4.4) runs after Phase B. C2 adds entity struct names to the swept
identifier set — the fourth source after methods, `<Method>Params`,
and `<Method>Row`. C5 hardens further as decode-helper naming
enters the exported surface (a decision C2 revisits below in §5.2
and defers to C5 by picking an unexported name here).

### 2.2 The entity-naming sub-kernel — helpers on the emission walk

C2 introduces three internal helpers, unexported, in `generate.go`:

```go
// entityStructName derives the exported Go struct name for a schema
// node or edge type (§4.5). Returns (name, err): err is
// ErrInvalidEntityName when an explicit Name is set but is not a
// valid exported Go identifier; ErrUnnamedMultiLabelType when the
// type has a multi-label label set or its edge label is shared
// across endpoint pairs and no explicit Name is present. Never
// invents a name from the label set for a multi-label / ambiguous
// type — the schema author must be explicit.
func entityStructName(kind entityKind, labels graph.LabelSetKey, edgeKey schema.EdgeKey, explicitName string, ambiguousEdgeLabel bool) (string, error)

// exportedGoIdent reports whether s is a valid exported Go
// identifier — matches C1's queryfile front-end grammar
// (^[A-Z][A-Za-z0-9]*$). ASCII-only, deliberately: entity struct
// names are directory-adjacent readability surfaces the same way
// package names are; the non-ASCII escape hatch lives on field
// names (§4.2's mangle, which accepts Unicode letters per C1's
// 2026-07-11 resolution). If a schema author writes an entity
// Name containing Unicode letters they can spell it via the
// standard single-label mangle applied to a compatible label.
func exportedGoIdent(s string) bool

// edgeLabelAmbiguous reports whether an edge type's Label appears
// on more than one EdgeKey in the schema — the ambiguity axis
// entity-naming rule 4 keys on (§4.5). A single-EdgeKey label
// is unambiguous; a shared label is ambiguous even when the two
// endpoint pairs are structurally distinct.
func edgeLabelAmbiguous(edges map[schema.EdgeKey]schema.EdgeType, label graph.LabelSetKey) bool
```

The helpers are grounded in the C0 kernel's existing `generate`
scope: `entityStructName` reads `NodeType.Name` / `EdgeType.Name`
plus the pre-computed label / edge-key / ambiguity axes;
`exportedGoIdent` runs on the explicit-name string;
`edgeLabelAmbiguous` runs over `Schema.Edges` once (Phase Z
memoises the result set for reuse across every edge type).

### 2.3 Purity, determinism, short-circuit — unchanged

C1 §2.3's three invariants stand:

- **Pure.** No new I/O; the entity-naming helpers are pure text-to-
  text.
- **Deterministic.** Iteration order: Phase Z walks `Schema.Nodes`
  in `graph.LabelSetKey` lexical order (§5.2) and `Schema.Edges` in
  `EdgeKey` triple-lex order (§5.2) — both orders match
  `schema.Schema.MarshalJSON`'s golden-stability discipline; the
  ambiguity axis is a set membership over the same iteration. Phase
  A / Phase B / per-source grouping are unchanged (`Input.Queries`
  slice order). Entity property field iteration inside a struct is
  in the property map's key-sorted order (§5.2 pins the sort). No
  map iteration escapes into the output.
- **Short-circuit.** First-error wins across Phase Z, Phase A,
  Phase B, the cross-query collision sweep, and per-source
  emission. Zero value on error: `(nil, err)`.

### 2.4 What the models.go change means for gqlc's module

C2's `models.go` moves from C1's byte-empty `package <derived>`
body to a body that declares one exported struct per node type and
edge type in the schema, each followed by its unexported decode
helper. The emission adds one import to `models.go` —
`github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype` — for the
`dbtype.Node` / `dbtype.Relationship` decode-helper argument types.
`models.go`'s C1 `errors` / `fmt` / `context` import set was empty;
C2 adds `dbtype` and `fmt` (the decode helper wraps decode errors
with a column-position message).

The change is entirely inside the emitted `models.go` template
string (§5.2 gives the exact body); gqlc's own module is not
affected — the generator emits text, and text-level changes cross
no dependency boundary. The nested-module compile fence
(`just test-codegen-fence`, C0 §7) is what proves the emitted body
type-checks against the pinned driver version
(`test/data/codegen/go.mod`, `neo4j-go-driver/v5 v5.28.4` today).
The driver's `dbtype` sub-package is stable in v5.28.4 (verified
against `pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype`,
2026-07-11).

---

## 3. Emitted API surface — the C2 shape

The user-visible generated surface C2 adds on top of C1. C2's exported
package-level identifiers extend the C1 set with **one entity struct
per schema node type and per schema edge type** — unconditional
emission per D2 §D2 ("schema structs emitted unconditionally"). The
C0 / C1 exported skeleton set (`Queries`, `New`, `WithTx`,
`ReadQuerier`, `WriteQuerier`, `Querier`; `ErrNoRows` /
`ErrMultipleResults` when a `:one` query is present; per-query
methods; `<Method>Params` / `<Method>Row` structs) is unchanged.

### 3.1 Entity structs — the models.go additions

Every schema `NodeType` and every schema `EdgeType` emits one struct
into `models.go`. The struct's identifier is derived by the entity-
naming rules (§4.5). Its fields are the type's `Properties` in map-
key-sorted order (§5.2), each field's name derived by §4.2's mangle
(the same one Params fields use), each field's Go type derived by
§5.1's property → Go type table (unchanged from C1), each field's
nullable-property emission a `*T` pointer (D3 Resolved's uniform
nullability rule).

- **Entity structs are schema-shaped only.** D3 Resolved: no
  `ElementId`, no start/end ids, no deprecated `Id`. Driver identity
  is metadata of a *particular fetched record*, not of the schema
  type; the projection `RETURN p, elementId(p) AS id` puts identity
  on the Row struct beside the entity, where it belongs. The
  resolver types function invocations as `unknown` today, so the
  alias renders `any` for now; `gqlc-v5t` (resolver builtins) will
  type `elementId(node|edge)` → `string` later with zero codegen
  changes.
- **Zero-property node type** emits an empty struct. Legal Go; the
  struct still identifies a fetched-node projection at the Row-
  struct field position, and property gaps land through the same
  vocabulary a resolver upgrade would.
- **Every entity struct is exported** — the schema is the vocabulary
  the *user's* code consumes, so the structs live on the exported
  surface. Unexported entity structs would force every downstream
  consumer to shadow-declare a matching exported type.
- **Struct doc comment** is a single line: `// <Name> corresponds to
  the <labels-or-edge-key> node|edge type.` D5's readability
  affordance — a schema browser can jump from label to struct on
  grep. The doc string names the source axis (labels or edge key)
  verbatim from the schema, not the derived struct name — the
  derivation is one hop from the source and the source is the
  stable identity.

### 3.2 Entity decode helpers — the models.go additions

Every entity struct has one unexported decode helper in `models.go`,
beside its struct:

```go
// decodePerson decodes a driver dbtype.Node into a Person struct,
// enforcing per-property nullability against the schema.
func decodePerson(node dbtype.Node) (Person, error) { ... }

// decodeActedIn decodes a driver dbtype.Relationship into an
// ActedIn struct, enforcing per-property nullability against the
// schema.
func decodeActedIn(edge dbtype.Relationship) (ActedIn, error) { ... }
```

- **Naming convention: `decode<StructName>`** — lower-camel first
  rune, entity struct name suffix. Unexported: the helpers are
  called only by the same-package per-query row assembly (§5.5),
  never by user code; keeping them unexported avoids widening the
  public surface with implementation detail and defers the exported-
  collision question to C5 (as ADR 0010 D7 sequences).
- **Signature: takes the driver's carrier type** — `dbtype.Node`
  for a node-type helper, `dbtype.Relationship` for an edge-type
  helper. Returns `(<StructName>, error)`. Never a pointer receiver
  — this is a pure decoder, no state.
- **Body: per-property decode via `neo4j.GetProperty[T]`**, the
  driver-documented reader over `dbtype.Entity`'s `Props` map,
  parameterised on the property's carrier Go type (C1's C1 §5.5
  narrow-carrier discipline — int64 for every integer family,
  float64 for every float family, string/bool passthrough). Per-
  property nullability check: nullable-property arriving-nil sets
  the pointer field to `nil`; non-nullable-property arriving-nil
  is a decode error naming the property. The same posture the C1
  row-assembly template uses at the column-decode surface, moved
  one layer down.
- **Property order in the decode body** is the same map-key-sorted
  order the struct emits its fields in (§5.2), so the golden diff
  is one contiguous block.

### 3.3 Nullable entity column on a Row struct

D3 Resolved: nullable → pointer, uniformly. A `ResolvedNode` /
`ResolvedEdge` column whose `Nullable == true` on `Validated.Columns`
renders `*<EntityName>` at the Row-struct field position. The per-
query row assembly's entity-column arm (§5.5) handles the nullable
case: a nil driver record value → the pointer field is `nil`; a
non-nil driver record value → address of a decoded local. Non-
nullable entity column arriving nil → decode error naming the
column (uniform with C1's property-column non-nullable rule; not a
sentinel — the fail-message is fixture-worthy prose).

### 3.4 Per-query row assembly for entity columns — no new user API

Entity column projections use the C1 method-shape ergonomics
unchanged (§3.1 of C1). A `RETURN p, elementId(p) AS id :one` query
with two columns still emits an `XRow` struct with two fields
(`P Person`, `Id any` — the `any` because the resolver types the
alias as `unknown` today; §3.1 mentions the gqlc-v5t future). A
`RETURN p :one` query with one column still emits a bare-value
return: `func (q *Queries) OnePerson(ctx context.Context) (Person, error)`.

The row-assembly body for a bare-value entity `:one` calls
`decodePerson(node)` after a `neo4j.GetRecordValue[dbtype.Node]`
extraction — the pattern C1's row-assembly template naturally
generalises to (the carrier just changes from a scalar to
`dbtype.Node`). Full row-assembly bodies in §5.5.

### 3.5 The C1 `driverOrTx.run` seam signature stands

C2 does not revise the C1 seam signature. The `[]*neo4j.Record`
return type accommodates entity columns unchanged — a record's
`Values()` is `[]any` where an entity column's value is a
`dbtype.Node` or `dbtype.Relationship`, and `neo4j.GetRecordValue[T]`
resolves against the record's value slice regardless of the target
type. The compile fence proves both integer-family and entity-
family calls type-check against the pinned driver version.

---

## 4. The naming kernel — C2 additions

C1's naming kernel (§4 of the C1 spec) stands: method names verbatim
(§4.1), Params fields via the one-mangle rule (§4.2), Row fields via
text-shape analysis on `Column.Name` (§4.3), package-level exported-
identifier collision sweep (§4.4). C2 adds the entity-struct-name
derivation rules (§4.5) and extends the collision sweep to include
entity struct names as a fourth identifier source (§4.6).

### 4.5 Entity struct names — the entity-naming rules

D3 Resolved (2026-07-11) pins five rules; C2 renders them as the
`entityStructName` helper (§2.2). Applied uniformly to node types
and edge types; the axes differ (`Labels` for a node type,
`EdgeKey` + `Label` ambiguity for an edge type) but the disposition
of each rule is identical.

**Rule 1 — Explicit `Name` wins.** If `NodeType.Name` /
`EdgeType.Name` is non-empty:

- If `exportedGoIdent(Name)` is true: emit the entity struct as
  `Name`, no mangle. This gives the schema's `Name` field its
  consumer — the schema author declared the identifier they want in
  the generated code. Non-ASCII in `Name` is deliberately not
  accepted (§2.2's `exportedGoIdent` grammar rationale).
- If `exportedGoIdent(Name)` is false: `ErrInvalidEntityName` naming
  the schema type (labels for a node, edge-key triple for an edge)
  and the offending `Name`. The fix is a schema edit.

**Rule 2 — Single-label node type → label through the standard
mangle.** If `Rule 1` did not fire (empty `Name`) and
`len(labels.Split()) == 1`: emit the entity struct as
`paramFieldName(label)` — reuses C1's split-on-`_` + capitalise
first-rune-per-segment + preserve internal case discipline (§4.2).
`Person` → `Person`; `Person_type` → `PersonType`; `PERSON` →
`PERSON`. If the mangle result is not a valid exported Go
identifier (starts with digit, contains punctuation other than
underscore): `ErrInvalidEntityName` naming the labels and the
derived text. Practical case: a label of `_person` mangles to
`Person` (leading underscore dropped, §4.2); a label of `1st` fails
the check.

**Rule 3 — Single-label edge type with unambiguous label → same
mangle.** If `Rule 1` did not fire and `len(Label.Split()) == 1`
and `!edgeLabelAmbiguous(schema.Edges, Label)`: emit the entity
struct as `paramFieldName(label)` — `ACTED_IN` → `ActedIn`;
`KNOWS` → `Knows`. Same failure disposition as Rule 2 (mangle-
result-not-valid → `ErrInvalidEntityName`).

**Rule 4 — Multi-label type OR ambiguous edge label →
required-Name.** If `Rule 1` did not fire and either
`len(labels.Split()) > 1` (node type) OR `len(Label.Split()) > 1`
(multi-label edge) OR `edgeLabelAmbiguous(schema.Edges, Label)`
(single-label edge whose Label appears on more than one
`EdgeKey`): `ErrUnnamedMultiLabelType` naming the schema type and
the axis that made it ambiguous. This is the "no invented names"
rule ADR 0010 D3 pins; a generated `ActorPerson` /
`KnowsPersonCompany` would smuggle a heuristic into the API
surface where users cannot see it. Checked **eagerly** — Phase Z's
sweep runs on every schema node type and edge type regardless of
whether any query projects it, because a lazy check would make
output depend on the query set (ADR 0010 D3 Resolved).

**Rule 5 — Property fields via §4.2's mangle; same-entity
collision → `ErrPropertyFieldCollision`.** Every `Property.Name`
on the type runs through `paramFieldName` (§4.2). Two properties
mangling to the same field name (e.g., a property named `min_age`
and another named `minAge` on the same node type) →
`ErrPropertyFieldCollision` naming both properties and the entity.
Package-level collisions between two entity struct names, or
between an entity struct name and a method or Row struct, land in
the extended §4.6 sweep. Rejected: a distinct
`ErrEntityNameCollision` — the fix surface is the same as C1's
existing `ErrIdentifierCollision` (rename an entity via `Name`,
rename a query, or restructure the schema); duplicating the
sentinel splits errors.Is queries across two identical-fix
surfaces.

**Property field ordering inside an entity struct** is the
`Properties` map's key-sorted order (§5.2 pins this). Two consumers
of the same schema must produce the same struct layout; Go's map
iteration randomness rules out reading from the map directly. The
sort is on the source property name (the map's key), not on the
derived field name — sorting on the derived name would depend on
the mangle output stability, which is stable but one hop removed
from the schema's own vocabulary.

### 4.6 Package-level identifier collision sweep — C2 additions

C1's §4.4 sweep runs unchanged over the per-query identifier
sources (methods, `<Method>Params`, `<Method>Row`). C2 adds a
fourth source: entity struct names from Phase Z's cache. The sweep
runs in this order:

1. Every entity struct name from Phase Z's cache — sorted by their
   emission order (`Schema.Nodes` in `LabelSetKey` order then
   `Schema.Edges` in `EdgeKey` triple-lex order). Inserted first
   because entity structs are the anchor of the schema-side
   vocabulary — a query struct colliding with a schema struct is
   the query struct's rename opportunity, not the schema struct's.
2. Every per-query method name.
3. Every `<Method>Params` (queries with two-plus parameters).
4. Every `<Method>Row` (queries with two-plus columns).

Any duplicate → `ErrIdentifierCollision` naming both identifier
sources (e.g. "entity struct `Person` (schema labels `Person`)
collides with row struct `Person` from query `X`"). The sweep is a
single map insertion pass; the first duplicate wins.

**Why the reserved-identifier check at Phase A still stands.** C1's
Phase A rejects a query named `Person` when `Person` is a reserved
identifier (a C1 skeleton name). C2 does NOT add entity struct
names to Phase A's reserved-identifier set — entity struct names
are schema-derived, so a query named `Person` on a schema without a
`Person` node emits fine, and a query named `Person` on a schema
with a `Person` node fails the §4.6 exported sweep. The distinction
matters because the reserved list is a fixed set (Phase A can hash
it), while the entity-derived set is per-schema (Phase A cannot
close its check surface without a Phase Z result).

**Why decode helpers do not participate in the sweep.** Entity
decode helpers are unexported (`decodePerson`, not `DecodePerson` —
§3.2); the exported sweep only walks exported identifiers.
Unexported cross-source collisions (two schemas producing two
`decodePerson` names — impossible today because two entity structs
`Person` already collide at the exported layer) fail the nested-
module compile fence with a "redeclared" error naming both source
sites, a strictly better diagnostic than an
`ErrIdentifierCollision` fail-message that only names the schema
type pair. Same discipline as C1's `<methodName>QueryText` const
sitting on the fence, not the sweep.

---

## 5. Emission templates and per-query files — C2 additions

### 5.1 Property → Go type mapping — the C2 new column-shape rows

The C1 property → Go type table (C1 spec §5.1) stands unchanged for
the property-side rows. C2 extends the *column-shape* set — the
`Column.Type` variants Phase A admits — with two new rows:

| `Column.Type` | Go type | Nullable emission |
|---|---|---|
| `ResolvedNode{Labels, Nullable}`  | `<EntityStructName>` | `*<EntityStructName>` |
| `ResolvedEdge{EdgeKey, Nullable}` | `<EntityStructName>` | `*<EntityStructName>` |

- `<EntityStructName>` is the Phase Z cache's entry for the column's
  `Labels` (node) or `EdgeKey` (edge). Phase A's admission proves
  the entry exists; Phase B's row-field mapping does the lookup.
- **Still deferred at C2** (Phase A routes to `ErrOutOfC2Scope`):
  - `ResolvedEdgeUnion` — C5 owns (sealed interface, ADR 0010 D3
    Resolved).
  - `ResolvedScalar`, `ResolvedTemporal`, `ResolvedList`,
    `ResolvedUnknown` — C3 owns (temporals, lists, `any` fallback).
  - `ResolvedProperty` with `DATE` / `TIMESTAMP` — C3 owns
    (property-side temporals via `dbtype.Date` / `time.Time`).
  - `ResolvedProperty` with `INT128` / `INT256` / `UINT128` /
    `UINT256` / `FLOAT16` / `FLOAT128` / `FLOAT256` / `DECIMAL` —
    C3 owns (unrepresentable-width sentinels).
- **Nullable → pointer, uniformly** (unchanged from C1). A
  `ResolvedNode{Labels: L, Nullable: true}` renders `*<EntityStruct
  Name>`; the row-assembly arm nil-checks and produces a nil pointer
  or takes the address of a decoded local (§5.5).

### 5.2 `models.go` — entity structs and decode helpers

The C1 `renderModels` produced only the file header + package line.
C2's `renderModels` walks Phase Z's cache and emits one struct +
one decode helper per entity, in `Schema.Nodes` `LabelSetKey`-order
then `Schema.Edges` `EdgeKey`-triple-lex-order. The file's shape:

```go
// Code generated by gqlc <version>. DO NOT EDIT.

package <derived>

import (
    "fmt"

    "github.com/neo4j/neo4j-go-driver/v5/neo4j"
    "github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype"
)

// Person corresponds to the Person node type.
type Person struct {
    Id   int64
    Name string
}

// decodePerson decodes a driver dbtype.Node into a Person struct,
// enforcing per-property nullability against the schema.
func decodePerson(node dbtype.Node) (Person, error) {
    var out Person
    id, err := neo4j.GetProperty[int64](node, "id")
    if err != nil {
        return Person{}, fmt.Errorf("decode Person.Id: %w", err)
    }
    out.Id = id
    name, err := neo4j.GetProperty[string](node, "name")
    if err != nil {
        return Person{}, fmt.Errorf("decode Person.Name: %w", err)
    }
    out.Name = name
    return out, nil
}

// ActedIn corresponds to the ACTED_IN edge type (Person -> Movie).
type ActedIn struct {
    Since int64
}

// decodeActedIn decodes a driver dbtype.Relationship into an
// ActedIn struct, enforcing per-property nullability against the
// schema.
func decodeActedIn(edge dbtype.Relationship) (ActedIn, error) {
    var out ActedIn
    since, err := neo4j.GetProperty[int64](edge, "since")
    if err != nil {
        return ActedIn{}, fmt.Errorf("decode ActedIn.Since: %w", err)
    }
    out.Since = since
    return out, nil
}
```

- **Iteration order.** `Schema.Nodes` is a map keyed by
  `graph.LabelSetKey` (a comparable string), walked in ascending
  lexical order (the same order `schema.Schema.MarshalJSON` uses).
  `Schema.Edges` is a map keyed by `schema.EdgeKey` (a struct of
  three `LabelSetKey`s), walked in `(Source, Label, Target)` triple-
  lex order (again, `MarshalJSON`'s convention). Property fields
  inside a struct walk the `Properties` map's keys in ascending
  lexical order.
- **Nullable property emission.** A property with `Nullable == true`
  emits `*T`; the decode-helper body handles the nil case:

  ```go
  namePtr, err := neo4j.GetProperty[string](node, "name")  // note: nullable case
  if err != nil && !errors.Is(err, neo4j.ErrNilProperty) {
      return Person{}, fmt.Errorf("decode Person.Name: %w", err)
  }
  if err == nil {
      v := namePtr
      out.Name = &v
  }
  ```

  where `neo4j.ErrNilProperty` is the driver's documented sentinel
  for a missing property key (verified against
  `pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5/neo4j`,
  2026-07-11 — a `GetProperty` for a missing key returns a
  zero-value + wrapped `ErrNilProperty`). If the driver's
  sentinel-shape at v5.28.4 turns out to differ from this
  reading, the code cycle records the driver's actual missing-key
  posture (returned error type or a `bool` companion) and revises
  this template accordingly; the C2 goldens bake the truth once
  the code compiles. **Non-nullable property arriving nil / missing
  → decode error naming the property**, uniform with C1's non-
  nullable column posture at the row-assembly surface.
- **Imports.** `fmt` is unconditionally imported (every decode
  helper emits at least one `fmt.Errorf`); `neo4j` is
  unconditionally imported (`neo4j.GetProperty` lives on the
  driver-root package); `dbtype` is unconditionally imported (every
  decode helper's argument type is `dbtype.Node` or
  `dbtype.Relationship`). A schema with zero node/edge types emits
  no structs and no imports — `models.go` stays byte-empty at
  `package <derived>` (matches C1). The zero-entity case is
  legitimate for a query-only sub-schema (extreme, but the shape
  supports it).
- **`errors` import.** The nullable-property arm above wraps
  `errors.Is`; the emission includes `errors` in the import block
  only when the schema contains at least one nullable property.
  Otherwise `errors` stays absent — the linter's unused-import gate
  catches drift.
- **Doc comment on the struct.** Node: `// <Name> corresponds to the
  <labels> node type.` (the `<labels>` verbatim from
  `LabelSetKey.Split()` joined with `&`). Edge: `// <Name>
  corresponds to the <label> edge type (<source> -> <target>).`
  (label / source / target verbatim from the `EdgeKey`). The doc
  string names the schema-side axis so a reader browsing `models.go`
  can jump back to the schema without a rename map.
- **Doc comment on the decode helper.** One line: `// decode<Name>
  decodes a driver dbtype.<Kind> into a <Name> struct, enforcing
  per-property nullability against the schema.` No further prose —
  the body is self-explanatory beside the struct.

### 5.3 Method rendering into `db.go` — unchanged

C1's `db.go` template stands byte-identical at C2. The per-method
body renders in `<name>.cypher.go` (§5.5 below); `db.go` carries
only the `Queries` handle + constructors + `driverOrTx` interface +
`driverDB` / `txDB` implementations + `ErrNoRows` /
`ErrMultipleResults` when a `:one` query is present.

### 5.4 `querier.go` regeneration — unchanged

C1's `querier.go` template stands byte-identical at C2. `ReadQuerier`
still lists every read method in `Input.Queries` order;
`WriteQuerier` stays empty (C4 populates); `Querier` embeds both;
the compile-time `Querier = (*Queries)(nil)` assertion still fences
drift. Entity structs are top-level identifiers, not interface
members, so `querier.go` sees no diff.

### 5.5 The per-source `<name>.cypher.go` file — entity-column arm

C1's per-source file shape stands: query-text const, Params struct
(if two-plus params), Row struct (if two-plus columns), method with
row-assembly body. C2 extends the row-assembly body's per-column
decode block with an **entity-column arm** — the property-column
arm still runs unchanged for `ResolvedProperty` columns.

**Per-column decode block — property column (unchanged from C1):**

```go
value, isNil, err := neo4j.GetRecordValue[<carrier>](records[0], "<column-name>")
if err != nil {
    return <zero>, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
}
<nullability-check>
<assign-or-return>
```

**Per-column decode block — node-entity column, non-nullable:**

```go
node, isNil, err := neo4j.GetRecordValue[dbtype.Node](records[0], "<column-name>")
if err != nil {
    return <zero>, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
}
if isNil {
    return <zero>, fmt.Errorf("<method>: column %q is non-nullable but arrived null", "<column-name>")
}
value, err := decode<EntityName>(node)
if err != nil {
    return <zero>, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
}
<assign-or-return>
```

**Per-column decode block — node-entity column, nullable:**

```go
node, isNil, err := neo4j.GetRecordValue[dbtype.Node](records[0], "<column-name>")
if err != nil {
    return <zero>, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
}
var valuePtr *<EntityName>
if !isNil {
    v, err := decode<EntityName>(node)
    if err != nil {
        return <zero>, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
    }
    valuePtr = &v
}
<assign-or-return>  // uses valuePtr
```

**Per-column decode block — edge-entity column** is structurally
identical, substituting `dbtype.Relationship` for `dbtype.Node`
and `decode<EdgeEntityName>` for the node decode helper.

- **Carrier type at the driver surface.** `dbtype.Node` for node
  columns; `dbtype.Relationship` for edge columns. Both are the
  driver's stable v5 shapes for whole-entity record values
  (verified against
  `pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype`,
  2026-07-11). Unlike C1's integer-narrowing pattern, no narrowing
  step exists — the carrier is the operand of the decode helper,
  and the helper produces the struct directly.
- **`dbtype` import in `<name>.cypher.go`.** Adds
  `github.com/neo4j/neo4j-go-driver/v5/neo4j/dbtype` to the import
  block iff at least one entity column emits in that file. Purely
  property-column files (C1's fixture universe) keep C1's import
  set. Import determinism per emission order: `dbtype` slots after
  `neo4j` in a single-group `import ()` block, `goimports`
  ordering.
- **`:many` entity column** wraps the arm inside the same per-record
  `for` loop C1 emits; nothing structurally new — the arm just
  substitutes into the loop body's per-column slot.

**Example — `:one`, single node column, non-nullable:**

```go
func (q *Queries) OnePerson(ctx context.Context) (Person, error) {
    records, err := q.db.run(ctx, onePersonQueryText, nil, neo4j.AccessModeRead)
    if err != nil {
        return Person{}, err
    }
    if len(records) == 0 {
        return Person{}, ErrNoRows
    }
    if len(records) > 1 {
        return Person{}, ErrMultipleResults
    }
    node, isNil, err := neo4j.GetRecordValue[dbtype.Node](records[0], "p")
    if err != nil {
        return Person{}, fmt.Errorf("OnePerson: decode column %q: %w", "p", err)
    }
    if isNil {
        return Person{}, fmt.Errorf("OnePerson: column %q is non-nullable but arrived null", "p")
    }
    value, err := decodePerson(node)
    if err != nil {
        return Person{}, fmt.Errorf("OnePerson: decode column %q: %w", "p", err)
    }
    return value, nil
}
```

**Owner directive (2026-07-11) — lint-clean parity.** Every
emitted `models.go` and `<name>.cypher.go` must lint-clean under
gqlc's `.golangci.yml` (C1 spec §6.6's directive extends to C2's
new emissions). If a lint rule constrains a template — e.g.,
`stylecheck`'s `ST1000` requires a package-level doc comment on
files with a body — the C1 template already accommodates by
carrying a header comment; C2 inherits without a template change.
Rejected: `//nolint:all` at file heads.

### 5.6 The `driverOrTx.run` seam signature — unchanged from C1

C2 does not revise C1's seam signature. `[]*neo4j.Record` still
carries entity columns as driver-native `dbtype.Node` /
`dbtype.Relationship` values inside the record's value slice, and
`neo4j.GetRecordValue[dbtype.Node]` / `neo4j.GetRecordValue[dbtype.Relationship]`
resolve against them. No new template changes to `db.go`.

---

## 6. The golden harness — C2 revision

C0 §6's harness stands: the `test/data/codegen/{valid,invalid}`
layout, the nested Go module, the `manifest.json` shape, the
`-update` flag, the testify suites, the compile fence. C1 §6.6's
lint parity applies transitively. C2 revises the fixture set only,
not the harness code.

### 6.1 Fixture strategy

C1's discipline stands (fixture-per-capability, one schema per
fixture). C2 adds valid fixtures for each capability slice of §5
plus a small set of negative fixtures for each new sentinel. C1's
existing valid fixtures whose schemas contain any node/edge type
regenerate under the new `renderModels` — the `models.go` byte diff
is expected and captured with `-update`. This is not a spec break:
C1's spec §7's "silently accepted" set already reserved schema-
side content for later stages, and the C1 goldens' `models.go` file
existed byte-empty; C2 populates it in place.

**Existing C1 valid fixtures whose `models.go` regenerates** —
every C1 fixture with a non-empty `Schema.Nodes` or `Schema.Edges`
sees its `models.go` grow. This is the "models-only adoption path"
D6 references realising for the first time: the same schema now
produces the entity struct set even if no query projects an
entity. Expected fixtures (from the C1 spec §6.2 table):
`one_col_one_param_one` (Person struct with `id`, `name`),
`one_col_many` (Person struct with `name`), `many_col_one_row`,
`many_col_many`, `nullable_columns`, `nullable_parameter`,
`multi_source_files`, `alias_bare_variable_ambiguity`,
`all_widths`. The `skeleton` and `queries_ignored` C0 fixtures
whose schemas contain node types also regenerate. The `-update`
run on the C2 branch produces this diff once.

### 6.2 C2 valid fixtures

Under `test/data/codegen/valid/`, each new directory holds a
`schema.gql`, zero or more `.cypher` files, a `manifest.json`, and
a `golden/` subdirectory with the complete generated package:

| Fixture | Coverage |
|---|---|
| `entity_node_projected_one`     | `RETURN p :one` — a whole-node column, `:one` bare-value return. Exercises the smallest node-entity surface. |
| `entity_edge_projected_one`     | `MATCH (:Person)-[r:ACTED_IN]->(:Movie) RETURN r :one` — a whole-edge column, `:one` bare-value return. Exercises single-label unambiguous edge naming (Rule 3). |
| `entity_node_and_scalar_row`    | `RETURN p, p.age AS age :one` — mixes a node column with a property column in a two-column row struct. Exercises the row-assembly extension. |
| `entity_node_many`              | `MATCH (p:Person) RETURN p :many` — `:many` slice of entities. Exercises `dbtype.Node` in the per-record loop. |
| `entity_nullable_node`          | `OPTIONAL MATCH ... RETURN p :one` — a nullable node column. Exercises the `*Person` field emission and the nullable arm. |
| `entity_explicit_name`          | A schema with `NodeType.Name = "Actor"` on a labelled `Person` node. Exercises Rule 1 (explicit `Name` wins). |
| `entity_multi_label_named`      | A schema whose node type carries `Labels = {Person, Employee}` and `Name = "PersonEmployee"`. Exercises Rule 4 (multi-label with required explicit `Name`). |
| `entity_edge_ambiguous_label_named` | A schema with two `KNOWS` edge types (one between `Person, Person`, one between `Company, Company`), each with an explicit `Name`. Exercises Rule 4's ambiguous-edge-label axis with the required name. |
| `entity_zero_property_node`     | A schema with a `Marker` node type carrying no properties. Exercises the empty-struct emission. |
| `entity_with_nullable_property` | A schema with a nullable `Person.middleName :: STRING` property. Exercises the nullable-property decode arm in the entity decode helper. |

Ten new valid fixtures. Each is one `schema.gql` (some with zero
`.cypher` files — the models-only adoption path), a `manifest.json`,
and a `golden/` tree. The `golden/` trees compile under the C0
compile fence.

### 6.3 Schema fixture text — illustrative

`test/data/codegen/valid/entity_node_projected_one/schema.gql`:

```gql
CREATE PROPERTY GRAPH TYPE EntityNodeProjectedOne AS {
    (:Person {
        id   :: INT64 NOT NULL,
        name :: STRING NOT NULL
    })
}
```

Paired query file
`test/data/codegen/valid/entity_node_projected_one/queries.cypher`:

```cypher
// name: OnePerson :one
MATCH (p:Person) RETURN p
```

Resolved: `Columns = [{Name: "p", Type: ResolvedNode{Labels:
"Person", Nullable: false}}]`, `Parameters = []`, `Statement =
read`. Phase Z admission: `Person` node type has empty `Name`,
single label — Rule 2 fires, entity struct emits as `Person`. Phase
A admission: one column, `Column.Type` is `ResolvedNode` with
Labels `Person` — admissible under C2's widened column-shape set.
Phase B derivation: `p` matches bare-identifier shape → `P` field
name candidate on the Row struct. Single column → bare-value return
(§3.1 of C1): the method returns `(Person, error)`, no Row struct.
Emitted method:

```go
func (q *Queries) OnePerson(ctx context.Context) (Person, error) {
    records, err := q.db.run(ctx, onePersonQueryText, nil, neo4j.AccessModeRead)
    if err != nil {
        return Person{}, err
    }
    if len(records) == 0 {
        return Person{}, ErrNoRows
    }
    if len(records) > 1 {
        return Person{}, ErrMultipleResults
    }
    node, isNil, err := neo4j.GetRecordValue[dbtype.Node](records[0], "p")
    if err != nil {
        return Person{}, fmt.Errorf("OnePerson: decode column %q: %w", "p", err)
    }
    if isNil {
        return Person{}, fmt.Errorf("OnePerson: column %q is non-nullable but arrived null", "p")
    }
    value, err := decodePerson(node)
    if err != nil {
        return Person{}, fmt.Errorf("OnePerson: decode column %q: %w", "p", err)
    }
    return value, nil
}
```

And `models.go` for this fixture carries a `Person` struct with two
fields (`Id`, `Name`) plus the `decodePerson` helper (§5.2's shape).

### 6.4 C2 invalid fixtures — one per new sentinel

Added under `test/data/codegen/invalid/`:

| Fixture | Sentinel | Coverage |
|---|---|---|
| `invalid_entity_name_node`         | `ErrInvalidEntityName`     | Schema node type with `Name = "1st"` — starts with a digit, not a valid exported Go identifier. |
| `invalid_entity_name_edge`         | `ErrInvalidEntityName`     | Schema edge type with `Name = "with-hyphen"` — contains a hyphen. |
| `unnamed_multi_label_node`         | `ErrUnnamedMultiLabelType` | Schema node type with `Labels = {Person, Employee}` and empty `Name` — Rule 4 fires eagerly, no query needed. |
| `unnamed_ambiguous_edge_label`     | `ErrUnnamedMultiLabelType` | Schema with two edge types both labelled `KNOWS`, empty `Name` on either — ambiguous label axis fires eagerly. |
| `property_field_collision`         | `ErrPropertyFieldCollision`| Schema node type with two properties `min_age` and `minAge` both mangling to `MinAge`. |
| `out_of_c2_scope_edge_union`       | `ErrOutOfC2Scope`          | A query projecting a multi-candidate edge column (`ResolvedEdgeUnion`) — deferred to C5. |
| `out_of_c2_scope_scalar_return`    | `ErrOutOfC2Scope`          | A query projecting a `ResolvedScalar` column (e.g., `RETURN 1 AS n :one`) — deferred to C3. |
| `identifier_collision_entity_row`  | `ErrIdentifierCollision`   | A schema with a `Person` node type and a query with `// name: Person :one MATCH (p:Person) RETURN p.name AS field1, p.age AS field2` — the query's `PersonRow` collides with the entity struct `Person` only when the method name matches — so the fixture uses `RETURN p.name AS name, p.age AS age` in a query `// name: X :one` whose annotated name collides. Specifically: query name `Person` with two-plus columns produces `PersonRow` (no direct collision), but query name `Person` alone collides — the fixture pins the method-vs-entity collision axis: a query named `Person` on a schema with a `Person` node type. |

Seven invalid fixtures — four for the three new C2 sentinels plus
two for the renamed `ErrOutOfC2Scope` (edge union, scalar) plus one
for the extended `ErrIdentifierCollision` sweep. The C1 invalid
fixtures whose sentinel was `ErrOutOfC1Scope` (`out_of_c1_scope_*`)
rename to `out_of_c2_scope_*` in-place; the fixture directories
rename and the `manifest.json` `expectedError` field updates to
`codegen.ErrOutOfC2Scope`. No behavioural change; the C0 invalid
fixtures (`invalid_package_name`, `duplicate_query_name`,
`duplicate_source_file`, `invalid_cardinality`) are unchanged.

The existing C1 `identifier_collision_reserved` fixture stays
unchanged — it exercises the query-name-vs-reserved-identifier
axis of `ErrIdentifierCollision`, which is orthogonal to C2's new
entity-struct axis.

### 6.5 Determinism — C2 additions

C0's `TestDoubleRun` runs unchanged. C2's kernel adds two new
ordered surfaces, neither of which iterates a map for emission
without a preceding sort:

- Phase Z entity iteration: `Schema.Nodes` in `LabelSetKey` order,
  then `Schema.Edges` in `EdgeKey` triple-lex order (both extracted
  by sorting the map keys into a slice, exactly as
  `schema.Schema.MarshalJSON` does).
- Per-entity property field iteration: `Properties` map keys sorted
  ascending; the sort key is the source property name.

The cross-query identifier collision sweep (§4.6) inserts in
emission order (entity structs first, then per-query identifiers),
and the map is queried, never iterated for emission. Every ordered
surface is either the resolver's guaranteed order or the C2 sort.

### 6.6 Non-obvious harness invariants — C2 additions

C1's §6.6 invariants stand. C2 adds:

- **Every valid fixture's `golden/models.go` compiles with the
  pinned driver's `dbtype` sub-package.** `test/data/codegen/go.mod`
  pins `neo4j-go-driver/v5 v5.28.4` at C0; the emitted `models.go`
  imports the driver-root package for `neo4j.GetProperty` and the
  `dbtype` sub-package for `dbtype.Node` / `dbtype.Relationship`.
  Both APIs are stable in v5.28.4 (verified against
  `pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5/neo4j` and
  `.../dbtype`, 2026-07-11); D7's standing instruction directs
  re-verification at the C2 code cycle. **Grill-worthy risk:**
  `neo4j.GetProperty` may not exist in the exact shape §5.2's
  template above shows — the driver documents `dbtype.Entity.Props`
  as a `map[string]any` and offers `neo4j.GetProperty` as the
  documented reader with the same generic-typed signature as
  `GetRecordValue`. If the driver's actual API differs (e.g., the
  reader is `dbtype.Node.Get[T](key)` rather than
  `neo4j.GetProperty[T](node, key)`), the C2 code cycle records
  the correct shape and this template revises before goldens bake.
  The compile fence catches drift, so the fixture set is truthful
  once the code compiles under the pinned driver.
- **Owner directive (C1 §6.6, 2026-07-11) extends transitively.**
  Every emitted `models.go` and `<name>.cypher.go` must lint-clean
  under gqlc's `.golangci.yml`. C2's decode helpers are unexported
  but non-trivial (per-property error wrapping), so
  `stylecheck`'s `ST1005` (error strings should not be
  capitalised, should not end with punctuation) and `errorlint`'s
  wrapping discipline apply. Templates comply by construction:
  every `fmt.Errorf` in §5.2's helper body is
  `fmt.Errorf("decode <StructName>.<Field>: %w", err)` —
  lowercase prefix, `%w` wrap.
- **Fixture files named for shape or sentinel.** `entity_node_*`
  names the shape (entity column, node kind). Sentinel-fixtures
  name the sentinel: `unnamed_multi_label_node` is the sentinel,
  not the shape. Same convention as C1.

---

## 7. C2 capability scope — what emits

**In scope:** an `Input` whose:

- `Schema.Nodes` and `Schema.Edges` produce entity struct names via
  Rules 1–4 without failure — every multi-label node type and every
  ambiguous edge label carries an explicit `Name` that is a valid
  exported Go identifier.
- `Schema.Nodes` and `Schema.Edges` property field derivations are
  collision-free per entity.
- Every `NamedQuery` still satisfies C1's admission (per §7 of the
  C1 spec) with the following widening: `Columns[i].Type` may now
  be `ResolvedProperty`, `ResolvedNode` (with `Labels` matching a
  `Schema.Nodes` key), or `ResolvedEdge` (with `EdgeKey` matching a
  `Schema.Edges` key); parameter admissibility is unchanged
  (property only).

**Out of scope, routed to the appropriate sentinel:**

| Construct                                                    | Sentinel                     | Stage owner |
|--------------------------------------------------------------|------------------------------|-------------|
| `ResolvedEdgeUnion` column                                   | `ErrOutOfC2Scope`            | C5          |
| `ResolvedScalar` column                                      | `ErrOutOfC2Scope`            | C3          |
| `ResolvedTemporal` column                                    | `ErrOutOfC2Scope`            | C3          |
| `ResolvedList` column                                        | `ErrOutOfC2Scope`            | C3          |
| `ResolvedUnknown` column                                     | `ErrOutOfC2Scope`            | C3          |
| `ResolvedProperty` column with `DATE` / `TIMESTAMP`          | `ErrOutOfC2Scope`            | C3          |
| `ResolvedProperty` column with `INT128` / `INT256`           | `ErrOutOfC2Scope`            | C3          |
| `ResolvedProperty` column with `UINT128` / `UINT256`         | `ErrOutOfC2Scope`            | C3          |
| `ResolvedProperty` column with `FLOAT16`                     | `ErrOutOfC2Scope`            | C3          |
| `ResolvedProperty` column with `FLOAT128` / `FLOAT256`       | `ErrOutOfC2Scope`            | C3          |
| `ResolvedProperty` column with `DECIMAL`                     | `ErrOutOfC2Scope`            | C3          |
| Non-`ResolvedProperty` parameter (whole node/edge, etc.)     | `ErrOutOfC2Scope`            | C3          |
| Unrepresentable-width parameter (INT128+, DECIMAL, …)        | `ErrOutOfC2Scope`            | C3          |
| `CardinalityExec`                                            | `ErrOutOfC2Scope`            | C4          |
| Query text containing a Go raw-string-hostile backtick       | `ErrOutOfC2Scope`            | C4-or-later |
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

- Empty `Schema.Nodes` and `Schema.Edges` (no entity structs to
  emit; `models.go` stays byte-empty at `package <derived>` per
  C1). The `Input.Queries` slice can still be non-empty (property-
  only queries, C1's fixture universe).
- Schema node type or edge type with zero properties — empty
  struct emits with only the type declaration and an empty decode
  helper body.
- `Validated.Distinct == true` — unchanged from C1.
- `Validated.Columns[i].GroupingKey` — unchanged from C1.
- Comments in the query text — unchanged from ADR 0005.

**The C1 shape stands unchanged** for anything C2 does not touch:
package-name derivation (C0 §5.1), generated-file header (C0
§5.2), `Queries` handle constructors, `driverOrTx` interface
shape, `txDB` behaviour, `querier.go`'s interface population
(C1 §5.4), `db.go`'s read-arm body (C1 §5.6), the sentinel-set
discipline (with the C2 additions), the double-run determinism
test, the compile fence, the C1 valid-fixture set (with `models.go`
regenerated in place, §6.1).

---

## 8. Compile fence (unchanged)

C0 `just test-codegen-fence` (`cd test/data/codegen && go build
./... && go vet ./...`) covers C2's emissions without change: the
nested module builds every fixture's `golden/` tree, so every new
entity struct, decode helper, and per-source file entity-column
arm type-checks against the pinned driver. Failure modes:

- **A template regression in `models.go`.** The fence fails with
  the standard Go compiler error naming the file
  (`test/data/codegen/valid/<fixture>/golden/models.go:12: ...`),
  pointing at the exact fixture and line — same diagnostic quality
  as C1.
- **A `dbtype` API drift.** Bumping `neo4j-go-driver/v5` may
  reshape `dbtype.Node` / `dbtype.Relationship` or
  `neo4j.GetProperty`. The fence catches at the version bump.
- **Unused imports.** A `models.go` emitting no nullable property
  would carry no `errors` import; a schema producing zero entity
  types would produce a byte-empty `models.go` with no imports;
  a schema with only nullable properties would carry `errors`. The
  compile fence's `go vet` catches drift.

C2 does not add a second fence recipe. C1 §6.6's lint parity
directive extends transitively to `models.go`; if CI runs
`golangci-lint` against the nested module, this is enforced
automatically.

---

## 9. Sentinel set delta — the C2 view

C0's four sentinels + C1's five sentinels stand at C2 with one
rename. C2 adds three new sentinels and renames one to reflect the
retirement of a C1-owned sub-case (the entity-column axis, now
handled). The rename is deliberate and follows the resolver's
`ErrOutOfR0Scope` precedent: each stage's out-of-scope sentinel is
named for the stage that most tightly claims it; when a later
stage retires sub-cases, the sentinel renames rather than staying
frozen with an outdated stage marker. The failing surface (the
sentinel value) is textually different, so `errors.Is` consumers
against the C1 constant break — this is desirable: a consumer who
was branching on `ErrOutOfC1Scope` is claiming knowledge of C1's
scope, and C2 has revised what "out of scope" means for that error
site.

**New sentinels at C2:**

```go
// ErrOutOfC2Scope is returned when a C2-admissible input carries
// a construct C2 does not project: a column whose resolved type is
// ResolvedEdgeUnion (C5) or ResolvedScalar / ResolvedTemporal /
// ResolvedList / ResolvedUnknown (C3), a ResolvedProperty column
// or parameter with an unrepresentable width or a temporal
// property type (C3), a non-property parameter (C3), a :exec
// cardinality (C4), or a query text carrying a raw-string-hostile
// backtick. Category-grained per C0's precedent; C3/C4/C5 retire
// the sub-cases as they land. Renamed from ErrOutOfC1Scope at C2 —
// the entity-column axis retired.
var ErrOutOfC2Scope = errors.New("out of C2 scope")

// ErrInvalidEntityName is returned when an explicit NodeType.Name
// or EdgeType.Name is set but is not a valid exported Go
// identifier (spec §4.5 Rule 1), or when a single-label mangle
// (Rule 2 / Rule 3) produces text that fails the exported-Go-
// identifier grammar. The fail-message names the schema type
// (labels for a node, edge-key triple for an edge) and the
// offending string. Introduced at C2.
var ErrInvalidEntityName = errors.New("invalid entity name")

// ErrUnnamedMultiLabelType is returned when a multi-label node
// type, a multi-label edge type, or a single-label edge type
// whose Label is shared across endpoint pairs, has an empty
// NodeType.Name / EdgeType.Name — Rule 4 requires an explicit
// name to avoid guessing. The fail-message names the schema type
// and the axis that made it ambiguous. Checked eagerly regardless
// of query projection. Introduced at C2.
var ErrUnnamedMultiLabelType = errors.New("unnamed multi-label type")

// ErrPropertyFieldCollision is returned when two properties on
// the same entity mangle to the same struct field name (spec
// §4.5 Rule 5). The fail-message names both properties and the
// entity. Introduced at C2.
var ErrPropertyFieldCollision = errors.New("property field collision")
```

**Retired at C2:** `ErrOutOfC1Scope` — the constant is dropped
from the package, the fixtures rename to `out_of_c2_scope_*`, and
the `sentinelByName` map's entry renames. The retirement is a
clean cut: no `//nolint:staticcheck` for a lingering alias, no
deprecation window. The stage renaming precedent (the resolver's
`ErrOutOfR0Scope` → `ErrOutOfR1Scope` per stage) sets the pattern.

**Naming defence — `ErrOutOfC2Scope`, matching the resolver's
per-stage rename precedent.** Grill option: keep the sentinel
stable across stages as `ErrOutOfScope`. Rejected: `errors.Is`
consumers of the codegen sentinel need to know *which* stage claims
a rejection, because the "how to fix" surface varies by stage
(entity column → C2 fixed; scalar column → wait for C3; edge union
column → wait for C5). A per-stage sentinel is a machine-readable
staging marker; a stable sentinel would push the stage encoding
into the fail-message string, which `errors.Is` can't inspect. The
resolver's precedent is exactly this: `ErrOutOfR0Scope` retired for
`ErrOutOfR1Scope` etc. Match, don't diverge.

**Naming defence — `ErrInvalidEntityName` vs
`ErrInvalidIdentifier`.** The failure is specifically on an entity
struct's name (from `NodeType.Name` / `EdgeType.Name` or from the
label mangle), not on an arbitrary identifier — the reserved-
identifier axis stays on C1's `ErrIdentifierCollision`. The entity
axis has a distinct fix surface (a schema `Name` edit or a label
change), so a distinct sentinel makes the fix discoverable via
`errors.Is`.

**Naming defence — `ErrUnnamedMultiLabelType`, one sentinel
covering three axes (multi-label node, multi-label edge,
ambiguous single-label edge).** All three have the same fix
(assign an explicit `NodeType.Name` / `EdgeType.Name`) and the
same failure discovery site (Phase Z's eager sweep). Three
sentinels would triple the reachability-fixture surface without
adding fix-surface distinction. The fail-message names the axis
that fired ("multi-label node type", "multi-label edge type",
"ambiguous edge label"), so `errors.Is` consumers who want per-
axis handling read the string; grep-across-source auditability
finds three sentinels worse than one with a discriminating
message.

**Rejected — a distinct `ErrEntityNameCollision`.** A candidate
sentinel for entity-vs-entity or entity-vs-method collisions was
considered. Rejected: C1's `ErrIdentifierCollision` already
covers cross-source exported-identifier collisions (§4.4's design
explicitly anticipates C5's entity struct addition), and the fail-
message names both sources ("entity struct `Person` (schema
labels `Person`) collides with row struct `Person` from query
`X`"). Adding a distinct sentinel would fragment the `errors.Is`
surface for identical fix flows (rename an identifier). Reused
sentinel wins.

**Closed set for the C2 sweep.** `allSentinels` at C2:

```go
var allSentinels = []error{
    ErrInvalidPackageName,       // C0
    ErrDuplicateSourceFile,      // C0
    ErrDuplicateQueryName,       // C0
    ErrInvalidCardinality,       // C0
    ErrOutOfC2Scope,             // C2 (renamed from ErrOutOfC1Scope)
    ErrParamNameCollision,       // C1
    ErrRowFieldCollision,        // C1
    ErrAliasRequired,            // C1
    ErrIdentifierCollision,      // C1
    ErrInvalidEntityName,        // C2
    ErrUnnamedMultiLabelType,    // C2
    ErrPropertyFieldCollision,   // C2
}
```

Twelve sentinels. `ErrFormatFailure` stays excluded (C0 §9.2
rationale unchanged). Every C2 member has at least one negative
fixture (§6.4); the reachability sweep is C0's
`TestSentinelReachability` unchanged.

---

## 10. Out-of-scope table

Every downstream capability C2 does not deliver, with the stage
that owns it. Read as ADR 0010 D7 unpacked to the C2-vs-later
boundary (C1's version tightens as C2's slice retires the entity-
column axis):

| Capability                                          | Stage owner |
|-----------------------------------------------------|-------------|
| Collections (`list<T>`)                             | C3          |
| Six temporals via `dbtype`                          | C3          |
| Property columns of type `DATE` / `TIMESTAMP`       | C3          |
| Unrepresentable-width sentinels (INT128+, FLOAT16, DECIMAL) | C3   |
| FLOAT32 schema-width contract (encode widen / decode narrow) | C3 |
| `unknown` / `scalar null` / `scalar map` → `any`    | C3          |
| Writes (`:exec`, zero-column methods, `WriteQuerier` population) | C4 |
| `ExecuteWrite` path in `driverDB.run`               | C4          |
| Cardinality × shape rejection (`:exec` on a projection query, `:one`/`:many` on a zero-column write) | C4 |
| Raw-string-hostile query text (backtick escape / fallback) | C4-or-later |
| `edgeUnion` sealed interfaces + `//sumtype:decl`    | C5          |
| Package-level collision sweep hardening (decode-helper names as identifier sources, if C5 promotes them exported) | C5 |
| Version-stamp polish (`-ldflags -X` wiring)         | C6          |
| Session-config polish                               | C6          |
| `gqlc-0aa` re-scope against D4's no-runtime-package decision | C6 |
| `:iter` streaming cardinality (fourth enum value)   | `gqlc-1a5` (post-v1) |
| Configuration file (`gqlc.yaml` analogue), CLI     | future config effort |
| Disk writes, out-dir sync (stale deletion)          | future CLI effort |

Rows above the `gqlc-1a5` line are staged by ADR 0010 D7; the last
two are ADR 0010 D6 futures.

---

## 11. Definition of done for C2 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is
out of scope of this document. The spec is done when:

1. This file exists at `docs/specs/codegen-stage-c2.md`, committed
   on branch `codegen-c2-spec`.
2. §3 pins the C2 exported-surface additions (entity structs,
   unchanged method surface, unchanged Querier interface set) and
   defers decode-helper naming to unexported (C5 revisits).
3. §4 gives the entity-naming rules (§4.5) — explicit `Name`
   first, single-label mangle fallback for nodes and unambiguous
   single-label edges, required `Name` for multi-label and
   ambiguous cases (eagerly checked), property field mangle with
   collision sentinel — plus the extended package-level collision
   sweep (§4.6).
4. §5 gives the emission templates: the C2 property → Go type
   table additions (§5.1), the `models.go` entity struct + decode
   helper rendering (§5.2), the unchanged `db.go` / `querier.go`
   (§5.3, §5.4), the extended per-query row assembly's entity-
   column arm (§5.5), the unchanged seam (§5.6).
5. §9 names and defends the three new sentinels
   (`ErrInvalidEntityName`, `ErrUnnamedMultiLabelType`,
   `ErrPropertyFieldCollision`) and the rename
   (`ErrOutOfC1Scope` → `ErrOutOfC2Scope`); confirms the closed set.
6. §6 designs the fixture set: the ten valid fixtures (§6.2), the
   seven invalid fixtures (§6.4), the in-place regeneration of C1
   fixture `models.go` files, the renames of `out_of_c1_scope_*`
   to `out_of_c2_scope_*`, and the fixture-per-capability
   discipline.
7. §7 states the C2 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel each construct
   routes to and the stage that owns the next widening.
8. §8 confirms the C0 compile fence covers C2 emissions without
   change; §6.6 flags the linting-parity owner directive extending
   transitively.
9. §5.2 gives the fixture-count summary in §12 below.
10. §10 enumerates every downstream capability with its stage
    owner.
11. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer);
every blocker he raises is fixed on this same branch before the
branch merges. Cycle 2 (the C2 code cycle,
`codegen-c2-implementation` stacked on this branch) begins only
when the spec cycle merges.

---

## 12. Fixture-count summary

- **C0 valid fixtures kept:** `skeleton`, `queries_ignored`
  (`models.go` regenerates in place; other files unchanged).
- **C1 valid fixtures kept:** `one_col_one_param_one`,
  `one_col_many`, `many_col_one_row`, `many_col_many`,
  `nullable_columns`, `nullable_parameter`, `multi_source_files`,
  `alias_bare_variable_ambiguity`, `all_widths` (all nine —
  `models.go` regenerates in place for every schema with any
  node/edge type).
- **C2 valid fixtures added (10):** `entity_node_projected_one`,
  `entity_edge_projected_one`, `entity_node_and_scalar_row`,
  `entity_node_many`, `entity_nullable_node`,
  `entity_explicit_name`, `entity_multi_label_named`,
  `entity_edge_ambiguous_label_named`, `entity_zero_property_node`,
  `entity_with_nullable_property`.
- **C0 invalid fixtures kept:** `invalid_package_name`,
  `duplicate_query_name`, `duplicate_source_file`,
  `invalid_cardinality`.
- **C1 invalid fixtures renamed:** `out_of_c1_scope_node_column` →
  `out_of_c2_scope_node_column` retires (the node-column case is
  now in scope — this fixture becomes a *valid* fixture and moves
  to §6.2 as `entity_node_projected_one`, effectively); the
  remaining C1 out-of-scope fixtures rename verbatim:
  `out_of_c1_scope_exec` → `out_of_c2_scope_exec`,
  `out_of_c1_scope_int128` → `out_of_c2_scope_int128`.
  `param_name_collision`, `row_field_collision`,
  `alias_required_function_call`, `alias_required_expression`,
  `identifier_collision_reserved` stay unchanged.
- **C2 invalid fixtures added (7):** `invalid_entity_name_node`,
  `invalid_entity_name_edge`, `unnamed_multi_label_node`,
  `unnamed_ambiguous_edge_label`, `property_field_collision`,
  `out_of_c2_scope_edge_union`, `out_of_c2_scope_scalar_return`,
  `identifier_collision_entity_row`.

**Totals at C2:**
- Valid fixtures: 2 (C0) + 9 (C1) + 10 (C2) = 21.
- Invalid fixtures: 4 (C0) + 7 (C1 kept + renamed; the
  node-column fixture retires) + 8 (C2 new) = 19.
- Sentinels in `allSentinels`: 12.
- Every sentinel has ≥1 invalid fixture; the reachability sweep
  passes with the enlarged set.
