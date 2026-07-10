# Stage R0 spec — resolver: skeleton and the type-mapping table

The implementation brief for Stage R0 of `internal/resolver`, the first
consumer of the `query.Query` (ADR 0008). Build this **test-first**.
Scope, sequencing, and error posture are set by ADR 0009 (the resolver's
build charter). The externally visible surface is pinned by ADR 0008
(package, constructor, `Resolve` signature). The type vocabulary is
grounded in ADR 0002 (bit-width preservation) on the schema side and the
`query.Type` sum (ADR 0008) on the query side. This document is
the precise decision surface for R0 alone: the type-mapping table
(load-bearing per ADR 0009), the R0 shape of `ValidatedQuery`, the
`Resolver` seam, the R0 sentinel set, the golden-pair harness, and the R0
capability scope.

Stage R0 lowers a **labelled single-node `MATCH`/`RETURN`** — whole-entity
and single-level property projections — into the *current* `ValidatedQuery`
model. Later stages evolve the model (ADR 0009 §Decision); do not
anticipate them here.

---

## 1. Deliverables

- `internal/resolver/` — the package, mirroring the layout precedent of
  `internal/schema/gql`: `resolver.go` (the `Resolver` struct, `New`,
  functional options, the `Resolve` method), `validated.go` (the
  `ValidatedQuery` output model, its column and parameter types),
  `errors.go` (the R0 sentinel set), and `resolve.go` (the pure
  resolution kernel called by `Resolve`).
- `test/data/resolver/` — the hand-authored golden-pair fixture corpus,
  split `valid/` and `invalid/`, seeded with the R0 shapes.
- `internal/resolver/resolver_test.go` — the testify suite that drives
  the fixtures, gated by `-update` for golden regeneration and by the
  bidirectional sentinel-reachability sweep.

Nothing downstream of the resolver is built (no codegen, no driver) —
`ValidatedQuery` is provisional through R7 (ADR 0009 §Decision).

---

## 2. Architecture

### 2.1 Package layout

`internal/resolver/` — a sibling of `internal/query`, `internal/schema`,
`internal/graph`, and `internal/procsig`; it imports all four. Nothing
imports it back (the model packages stay consumer-free, ADR 0008).

- **`resolver.go`** — `Resolver`, `New(s schema.Schema, opts ...Option)
  *Resolver`, `Option`, `WithRegistry(r procsig.Registry) Option`, and the
  `Resolve(q query.Query) (ValidatedQuery, error)` method. The
  constructor binds the compile-time inputs (schema + registry); no
  per-query state lives on the struct — `Resolve` is pure.
- **`validated.go`** — `ValidatedQuery` and its constituent types
  (`Column`, `ResolvedParameter`, `ResolvedType`, discriminators).
- **`resolve.go`** — the pure resolution kernel: input is the trio
  `(query.Query, schema.Schema, procsig.Registry)`; output is
  `(ValidatedQuery, error)`. Called by `(*Resolver).Resolve`, no methods
  of its own — mirrors `schema/gql`'s `rawSchema.resolve()` posture.
- **`errors.go`** — the closed R0 sentinel set (§5).

The `resolve.go`/`Resolve` split is the same shape as `schema/gql`'s
listener → `resolve()` split: `Resolve` is the exported entry that binds
the inputs; the pure kernel does the work and can be unit-tested against
hand-constructed `query.Query` values without the constructor
ceremony.

### 2.2 The `Resolver` struct and the consumer seam

**Owner-mandated constraint.** The resolver is a concrete struct
(`*Resolver`, per ADR 0008's pinned `New`) that ALSO implements an
exported, minimal interface — a single-method `Resolve(q query.Query)
(ValidatedQuery, error)` seam for future consumers — with a compile-time
assertion.

```go
// QueryResolver is the consumer-facing seam for validated-query
// production. The concrete producer is *Resolver; consumers (codegen,
// tests, adapters) accept the interface so they can substitute a fake
// without importing procsig or schema.
type QueryResolver interface {
    Resolve(q query.Query) (ValidatedQuery, error)
}

var _ QueryResolver = (*Resolver)(nil)
```

**Reconciliation with ADR 0008.** ADR 0008 pins `New` returning the
concrete `*Resolver` deliberately — the constructor call site is where
compile-time inputs (schema, registry) get bound, and functional options
are additive, so the concrete return keeps that surface honest and
extensible. The interface is orthogonal: it is the **consumer** seam, not
the producer seam. Producers spell `resolver.New(...).Resolve(q)`; a
consumer that only needs to resolve — codegen, an integration test, an
adapter — takes a `QueryResolver` and stays free of the constructor's
compile-time-input coupling. This composes rather than competes: `New`
still returns `*Resolver`, `*Resolver` satisfies `QueryResolver`, and
the compile-time assertion (`var _ QueryResolver = (*Resolver)(nil)`) is
a build error if the method drifts from the interface (a signature
typo on `Resolve` fails the build before any test runs).

**Name defence.** `QueryResolver` (not `Resolver` — that name is taken
by the concrete struct in the same package, and Go's convention against
`I`-prefixed interface names rules that out too). Not `Validator` — the
resolver's job is broader than yes/no validation (it produces a typed
output, not a boolean). Not `Resolvable` — the noun-adjective mismatch
misleads. `QueryResolver` says exactly what the seam does: it resolves a
query. Method-name-plus-`-er` is idiomatic Go for one-method interfaces
(cf. `io.Reader`, `sort.Interface`), and one-method is what R0 needs.

The interface lives in `resolver.go` alongside the struct — the
consumer-facing seam co-located with its concrete producer, one file to
open when the surface changes.

### 2.3 Purity, determinism, short-circuit

- **Pure.** `Resolve` is a pure function of `(query.Query, schema.Schema,
  procsig.Registry)` — no I/O, no goroutines, no time source, no mutation
  of inputs. Resolving the same query twice against the same constructor
  inputs yields the same `ValidatedQuery` (byte-identical wire form) or
  the same error.
- **Deterministic.** Iteration over `Query.Parameters` and `Part.Returns`
  is in the parser's first-appearance order; no map iteration escapes
  into the output shape. `ValidatedQuery.Columns` is column-order; the
  R0 harness's golden encoding is `encoding/json` on a shape whose
  slices are pre-ordered.
- **Short-circuit.** Resolution stops at the first error — mirrors
  `schema/gql`'s `resolve()`. There is no multi-error accumulation
  (ADR 0009: the consumer aborts on any failure, the original text
  never runs). Zero value on error: `ValidatedQuery{}` is returned
  alongside the sentinel — the schema/gql convention (asserted in the
  invalid-fixture test, see §6.4).

### 2.4 Nothing named `resolution` or `validation`

The R0 kernel file is `resolve.go`; the resolver's output is
`ValidatedQuery`. Terminology per CONTEXT.md: **resolver** is the stage,
**validated query** is what it produces. No sibling terms
(`validator.go`, `Resolution` type, etc.) are introduced. The internal
kernel function is named `resolve` (not `validate` or `typecheck`) —
this matches the schema/gql precedent and the CONTEXT.md discipline.

---

## 3. `ValidatedQuery` — the R0 shape

`ValidatedQuery` is the schema-checked, fully typed description codegen
will one day consume (ADR 0008). The R0 shape is minimal: exactly the
information the R0 capability scope (§7) produces, and no speculative
fields for later stages. It is provisional through R7 (ADR 0009); no
consumer exists yet, so every later stage may add fields, add variants,
or rename what R0 introduces — the stability pressure that governs
`query.Query` does not apply here yet.

### 3.1 Top-level shape

```go
// ValidatedQuery is the resolver's R0 output: resolved result columns,
// resolved parameters, and the statement kind (read/write) codegen keys
// its transaction-mode decision on. Provisional through R7; freed by a
// future ADR (the 0008 analogue) when the resolver is feature-complete.
type ValidatedQuery struct {
    Columns    []Column            `json:"columns"`
    Parameters []ResolvedParameter `json:"parameters"`
    Statement  StatementKind       `json:"statement"`
}
```

- **`Columns`** is the result column list, in projection order,
  duplicates preserved. Empty is legal at later stages (a projection-less
  write, R6); R0 always produces at least one column because the R0
  capability scope requires a non-empty `RETURN`.
- **`Parameters`** is the deduplicated parameter list, in query-wide
  first-appearance order (the parser's invariant, preserved by the
  resolver). Empty when the query takes no parameters.
- **`Statement`** is the read/write axis, sourced directly from
  `Query.StatementKind`. R0's capability scope is read-only, so R0
  never emits `StatementWrite`; the field is present for wire stability
  and to keep the codegen key-in from day one.

**`StatementKind`** is a resolver-local enum of the same shape as
`query.StatementKind` (two values, `read`/`write` string tags). The
resolver package necessarily imports `internal/query` — `Resolve`
takes a `query.Query` as its input type — so the isolation claim is
narrower than "the resolver does not import query". The claim is
that **`ValidatedQuery`'s own wire shape** must not leak
`query.StatementKind`: `ValidatedQuery` is the codegen contract, and
if it embedded `query.StatementKind`, every consumer that reads a
resolved `Statement` field would be forced to import `internal/query`
just to name the enum values. Redeclaring the enum in
`internal/resolver` keeps the codegen-facing shape self-contained; the
wire tag matches so `errors.Is`-style equivalence checks work if any
consumer already holds both.

### 3.2 `Column` — a resolved result column

```go
type Column struct {
    Name string       `json:"name"`
    Type ResolvedType `json:"type"`
}
```

- **`Name`** is the column name (an explicit `AS` alias or the parser's
  source-text name, verbatim from `ReturnItem.Name` — the R0 resolver
  does not rename).
- **`Type`** is the column's resolved type: a `ResolvedType` sum (§3.4).

R0's capability scope produces exactly two column shapes:
whole-entity node projections (`ResolvedType` = the node-entity variant)
and property-of-node projections (`ResolvedType` = the R0-unresolved
property variant — see §3.4 and §4.3).

### 3.3 `ResolvedParameter` — a resolved parameter

```go
type ResolvedParameter struct {
    Name string       `json:"name"`
    Type ResolvedType `json:"type"`
}
```

R0's capability scope permits parameters only from inline property maps
on a labelled node pattern (§7). Each such use is a `query.PropertyUse`
pointing at a `(binding, property)` pair. R0's parameter typing rule:
- The parameter's `ResolvedType` is the property's resolved type as
  looked up on the node type's schema property, i.e. a
  `ResolvedProperty` carrying the `graph.PropertyType` from the
  schema and the nullability bit from `schema.Property.Nullable`. This
  is one of the two shapes R0 upgrades — the parameter-typing shape,
  because the property side of the inline map already gives a total
  schema witness at the use site.
- Parameter unification across multiple uses is R2's business (ADR
  0009). At R0, each parameter has exactly one use (from an inline
  property map on a single node), so no unification is required — the
  R0 capability scope guarantees `len(Parameter.Uses) == 1`.

An R0 fixture with a parameter appearing more than once (e.g. two nodes
in the same query both using `$id`) is out of scope until R2 for the
"multiple uses" reason and until R1 for the "more than one node" reason;
either way, it is not an R0 fixture.

### 3.4 `ResolvedType` — the resolved-type sum

`ResolvedType` is the sealed sum the columns and parameters carry. It is
the resolver's vocabulary, distinct from `query.Type`: it may add
variants `query.Type` does not carry (`ResolvedProperty` with a
`graph.PropertyType` payload), it may collapse variants `query.Type`
distinguishes but the resolver does not use, and it evolves per stage
until Stage 14. The R0 shape carries only the variants R0's capability
scope produces.

```go
type ResolvedType interface {
    String() string
    isResolvedType()
}

// ResolvedNode is a whole-entity projection whose Ref names a node
// binding, keyed by the node type's canonical label set.
type ResolvedNode struct {
    Labels graph.LabelSetKey
}

// ResolvedProperty is a property-of-entity projection or parameter use:
// the schema's normalised property type plus the nullability bit. R0
// produces this variant for both a projected property column (Column)
// and an inline-map parameter (ResolvedParameter).
type ResolvedProperty struct {
    Type     graph.PropertyType
    Nullable bool
}
```

Each variant carries a `String()` that is the wire tag (`"node"`,
`"property:INT32"`, etc.) and a `MarshalJSON` that routes through it —
the same tagged-union pattern `query.Type` carries. R0 introduces the
sealed sum with only the two variants it actually produces; every
later stage adds the variants it needs (R1 will add
`ResolvedEdge{schema.EdgeKey}`, R3 will add `ResolvedList{element}` for
var-length `list<edge>`, R5 will add `ResolvedPath{}`, etc.), driven by
the R-stage capability spec.

The R0 minimal sum is not a promise that later stages cannot rename or
restructure this type — `ValidatedQuery` is provisional through R7
(ADR 0009). The commitment is that at each stage the type-mapping table
(§4) is the source of truth and every added `ResolvedType` variant maps
back to a specific row of it.

**Alternative considered and rejected: reuse `query.Type` directly.**
`query.Type` is the parser's sum, deliberately schema-free —
its `TypeUnknown` is the parser's honest "cannot tell" for a property
ref, and its scalar variants (`TypeInt`, `TypeString`) do not carry the
bit width the schema does. Reusing `query.Type` would force the
resolver to either lose the bit-width information the resolver exists
to produce (ADR 0002) or overload `TypeUnknown` as a bag-of-schema-
info container. A separate resolver-vocabulary sum keeps each type
carrying exactly the information its layer knows.

---

## 4. The type-mapping table

ADR 0009 makes this table the R0 spec's first design item and the
future `ValidatedQuery` ADR 0008's seed. Every one of the seventeen
`query.Type` variants gets a row: the resolver's counterpart, a
classification (`final` / `schema-upgraded` / `unified-across-uses`),
and the stage that owns the upgrade. The **classification key** below
comes straight from ADR 0009's language:

- **final** — the parser already gives a fully specific type; the
  resolver copies it into its own vocabulary unchanged. A literal's
  `int` is the archetype.
- **schema-upgraded** — the parser gives an honest coarse type
  (`unknown` for a property lookup, `unknown` for a `TypeList` element
  the parser could not narrow) that the resolver widens or narrows
  from a schema witness. The R0 owner is R0 for the parameter-side
  upgrade of `unknown` sitting on an inline-map `PropertyUse`; the R2
  owner is R2 for the projection-side upgrade of `unknown` on a
  `RefProjection` property lookup and for typing `list<unknown>`.
- **unified-across-uses** — a parameter's `Uses` slice carries several
  witnesses (a property use plus a clause-slot use, or two property
  uses against different-typed properties); the resolver picks the one
  consistent type or fails with a conflict. R2's business.

The table is closed at R0: every variant appears exactly once. Where a
variant is not reachable in R0's fixture corpus, the row still records
which stage introduces it, so R1–R7 have a shared spine to widen from.

### 4.1 Node/edge/path — the structural kinds

| Variant       | Resolver counterpart      | Classification    | Owner |
|---------------|---------------------------|-------------------|-------|
| `TypeNode`    | `ResolvedNode{Labels}`    | schema-upgraded   | **R0** |
| `TypeEdge`    | `ResolvedEdge{EdgeKey}`   | schema-upgraded   | R1    |
| `TypePath`    | `ResolvedPath{}` (empty)  | final             | R5    |

**`TypeNode`** (R0). A `RefProjection` whose Ref names a `NodeBinding`
carries `TypeNode` from the parser. R0 upgrades it to
`ResolvedNode{Labels: <canonical label-set key of the resolved node
type>}`. The label set on the binding may be a *subset* (in later
stages, once merges or WITH-carry paths land); R0's simpler world has
one node binding whose declared labels resolve directly to exactly one
`schema.NodeType`, and the canonical key is that node type's
`Labels`.

**`TypeEdge`** (R1). Edges enter with R1. The resolver counterpart is
`ResolvedEdge{schema.EdgeKey}` — the schema's canonical (source,
label, target) triple. Recorded here for the mapping-table spine only.

**`TypePath`** (R5). A named-path binding (`p = (a)-[r]->(b)`) surfaces
here at R5's multi-part/branches stage — a path value is just a
schema-independent structural kind, hence `final`; the resolver
records the projected column has type "path" and codegen materialises
whatever wire shape it commits to. No schema witness upgrades the
value semantics (ADR 0005 — path components live below the
type-interface boundary).

### 4.2 Scalars — literal-final variants

| Variant       | Resolver counterpart            | Classification | Owner |
|---------------|---------------------------------|----------------|-------|
| `TypeBool`    | `ResolvedScalar{ScalarBool}`    | final          | R2    |
| `TypeInt`     | `ResolvedScalar{ScalarInt}`     | final          | R2    |
| `TypeFloat`   | `ResolvedScalar{ScalarFloat}`   | final          | R2    |
| `TypeString`  | `ResolvedScalar{ScalarString}`  | final          | R2    |
| `TypeNull`    | `ResolvedScalar{ScalarNull}`    | final          | R2    |
| `TypeMap`     | `ResolvedScalar{ScalarMap}`     | final          | R2    |

The scalar row is `final` because the parser already commits — a
literal `1` is `TypeInt` at parse time, and the resolver reads it
unchanged. The **judgment call**: literals are the resolver's *only*
source of these variants at R0's capability scope (R0 has no
projections other than `RefProjection`, so no `LiteralProjection`
reaches R0). They are recorded here as R2-owned because R2 introduces
`LiteralProjection`, `FuncProjection`, and `ExprProjection` on the
projection side.

**Why a `ResolvedScalar` wrapper rather than direct
`graph.PropertyType`?** A scalar literal in Cypher has *no* bit width —
`1` is `TypeInt`, not `TypeInt32`. The resolver's scalar row therefore
does *not* map into the width-carrying `graph.PropertyType` families;
it needs its own coarse-scalar set. `ResolvedProperty` (§3.4) is where
schema-bound bit widths live; `ResolvedScalar` (added at R2) is where
literal-derived scalars live. Two distinct rows, two distinct types,
both grounded in the mapping table.

### 4.3 The parser's honest `unknown` — the schema-upgrade row

| Variant       | Resolver counterpart      | Classification    | Owner |
|---------------|---------------------------|-------------------|-------|
| `TypeUnknown` | see below                 | schema-upgraded   | R0/R2 |

`TypeUnknown` is the parser's honest posture (`type.go`) for
schema-free typing. The resolver upgrades it by the *source* of the
`TypeUnknown`, not by pattern-matching on the parent Projection:

- **Property projection** (`RefProjection` with a `Ref{Variable, Property}`
  where `Property != ""`, whose `Type()` is `TypeUnknown` per parser
  Stage 6). Upgrade path: look up `Property` on the resolved node
  type's `Properties` map; on hit, emit
  `ResolvedProperty{Type: schema.Property.Type, Nullable:
  schema.Property.Nullable}`; on miss, emit `ErrUnknownProperty`. **Owner
  at R0**: R0 does *not* upgrade the projection-side `TypeUnknown`
  (R2's line item in ADR 0009 is "property-type upgrade of
  `unknown`"). At R0, the projected `Column.Type` for a property
  lookup is still schema-upgraded to `ResolvedProperty` — the R0
  capability scope has enough witness to do the upgrade at R0 without
  R2's other work (parameter unification), so R0 carries the
  projection-side upgrade for the single-node case. R2 formalises the
  upgrade for the general case (property lookup after WITH
  carry-forward, chained bindings, etc.).

  **This is the judgment call the R0 spec makes.** ADR 0009 places
  "property-type upgrade of `unknown`" at R2. The R0 capability scope
  (labelled single-node MATCH/RETURN, property refs) already produces
  the *simplest* form of this upgrade: one binding, one label set, one
  property on a schema-resolved node type. Deferring the upgrade to
  R2 would leave R0 emitting `ResolvedType = TypeUnknown-equivalent`
  columns for the property refs the capability scope explicitly
  supports — an internally inconsistent output for R0. Instead, R0
  performs the upgrade and R2 generalises it (unifies across uses,
  extends to properties after WITH). ADR 0009's placement of the
  upgrade line at R2 remains correct as the *finalisation* stage; R0
  is the *introduction* stage for the single-node case. This
  interpretation is consistent with R0's capability scope ("property
  refs resolve against the schema") — a property ref that does not
  resolve to a typed schema column is not resolved.

- **Parameter use** (a `PropertyUse` from an inline property map on a
  node pattern). Upgrade path: identical to the projection case —
  look up the property on the resolved node type, emit
  `ResolvedProperty` or `ErrUnknownProperty`. **Owner at R0**.

- **List element** (`TypeList` whose element is `TypeUnknown`). Out of
  R0 scope; **owner at R2**: the parser's list-element `TypeUnknown`
  is a genuinely schema-free question until the resolver looks at the
  element's producer. Recorded for the table's spine only.

- **Any other `TypeUnknown` source** (function result, aggregate
  result, `null` arithmetic). Out of R0 scope; owners at R2 (function
  result via a signature), R6/R7 (aggregate result). Recorded for the
  table's spine only.

### 4.4 Collection — `TypeList<T>`

| Variant     | Resolver counterpart               | Classification    | Owner |
|-------------|------------------------------------|-------------------|-------|
| `TypeList`  | `ResolvedList{Element ResolvedType}`| schema-upgraded  | R2/R3 |

`TypeList` is parameterised; its element `Type()` is upgraded the same
way any other resolver upgrade is done — recursively, via the same
table. R3 introduces `list<edge>` for var-length hops; R2 introduces
`list<T>` for property lookups whose schema type is a list-typed
column (deferred until the schema gains list-typed properties — the
current `graph.PropertyType` set does not include lists). Recorded for
the spine.

### 4.5 Temporal — the six openCypher temporal types

| Variant             | Resolver counterpart                        | Classification | Owner |
|---------------------|---------------------------------------------|----------------|-------|
| `TypeDate`          | `ResolvedTemporal{TemporalDate}`            | final          | R2    |
| `TypeTime`          | `ResolvedTemporal{TemporalTime}`            | final          | R2    |
| `TypeLocalTime`     | `ResolvedTemporal{TemporalLocalTime}`       | final          | R2    |
| `TypeDateTime`      | `ResolvedTemporal{TemporalDateTime}`        | final          | R2    |
| `TypeLocalDateTime` | `ResolvedTemporal{TemporalLocalDateTime}`   | final          | R2    |
| `TypeDuration`      | `ResolvedTemporal{TemporalDuration}`        | final          | R2    |

`final`: the parser gives a fully specific temporal type at the
expression's construction (Stage 7). The resolver copies it. The
**judgment call**: `graph.PropertyType` carries only `TypeDate` and
`TypeTimestamp` — it does not distinguish zoned/non-zoned time or
`TypeDuration`. That divergence is deliberate on the schema side
(bit-width preservation applied to storage-shaped types, ADR 0002),
but the resolver's expression-side vocabulary must carry the full
openCypher temporal set because expressions produce them. Hence
`ResolvedTemporal` as its own resolver-side enum, not
`graph.PropertyType`. A schema-bound property typed `TIMESTAMP`
resolves to `ResolvedProperty{Type: graph.TypeTimestamp, ...}`, not
`ResolvedTemporal`; the two rows sit alongside each other in the sum
and R2 draws the storage-vs-expression line.

Recorded for the spine; no R0 fixture reaches a temporal type.

### 4.6 Summary — the seventeen rows

| Variant             | Resolver counterpart               | Classification    | Owner |
|---------------------|------------------------------------|-------------------|-------|
| `TypeBool`          | `ResolvedScalar{ScalarBool}`       | final             | R2    |
| `TypeInt`           | `ResolvedScalar{ScalarInt}`        | final             | R2    |
| `TypeFloat`         | `ResolvedScalar{ScalarFloat}`      | final             | R2    |
| `TypeString`        | `ResolvedScalar{ScalarString}`     | final             | R2    |
| `TypeNull`          | `ResolvedScalar{ScalarNull}`       | final             | R2    |
| `TypeMap`           | `ResolvedScalar{ScalarMap}`        | final             | R2    |
| `TypeNode`          | `ResolvedNode{Labels}`             | schema-upgraded   | **R0** |
| `TypeEdge`          | `ResolvedEdge{EdgeKey}`            | schema-upgraded   | R1    |
| `TypeList`          | `ResolvedList{Element}`            | schema-upgraded   | R2/R3 |
| `TypeUnknown` (prop projection) | `ResolvedProperty{...}`| schema-upgraded   | **R0** |
| `TypeUnknown` (prop parameter)  | `ResolvedProperty{...}`| schema-upgraded   | **R0** |
| `TypeUnknown` (list element)    | via §4.4 recursion     | schema-upgraded   | R2    |
| `TypeUnknown` (other)           | see §4.3               | schema-upgraded   | R2/R6/R7 |
| `TypeDate`          | `ResolvedTemporal{TemporalDate}`   | final             | R2    |
| `TypeTime`          | `ResolvedTemporal{TemporalTime}`   | final             | R2    |
| `TypeLocalTime`     | `ResolvedTemporal{TemporalLocalTime}` | final          | R2    |
| `TypeDateTime`      | `ResolvedTemporal{TemporalDateTime}`  | final          | R2    |
| `TypeLocalDateTime` | `ResolvedTemporal{TemporalLocalDateTime}` | final      | R2    |
| `TypeDuration`      | `ResolvedTemporal{TemporalDuration}`  | final          | R2    |
| `TypePath`          | `ResolvedPath{}`                   | final             | R5    |

(Twenty rows: the `TypeUnknown` split is documented as one variant
with four rows because `TypeUnknown`'s upgrade path depends on its
source. The seventeen-variant count matches ADR 0009's "seventeen
`query.Type` variants" — the four `TypeUnknown` rows collapse
into one variant, giving 20 − 4 + 1 = 17.)

Rows classified as R0 owners are the only rows R0 implements. Every
other row is `not yet reachable at R0` in the resolver's code path
and is either a stage-later kernel branch or a stage-later sentinel.

### 4.7 What the R0 code path actually does

At R0, the resolve kernel walks the query and:

1. Verifies the query is one branch, one part, no writes, no CALL
   (§7 gate) — else the appropriate out-of-scope treatment (§5).
2. Verifies the part's projection flags match the R0 in-scope shape:
   `Part.Distinct == false` and `Part.ReturnsAll == false`. Either bit
   set → `ErrOutOfR0Scope` (R5 owns both — `RETURN DISTINCT` and
   `RETURN *` are cardinality-affecting first-class bits per
   ADR 0008 lines 38-40 and `query.go:105-114`; silently ignoring
   them would collapse distinct-cardinality output onto plain-return
   cardinality).
3. Resolves each `NodeBinding.Labels()` (a `graph.LabelSet`) to a
   `schema.NodeType` via `LabelSet.Key()` — the canonical
   `graph.LabelSetKey` that keys `schema.Schema.Nodes`.
   - Miss → `ErrUnknownLabel`.
4. For each `ReturnItem` (all are `RefProjection` at R0):
   - **Whole-entity** (`Ref.Property == ""` and the parser's
     `RefProjection.Type()` is `TypeNode`): emit
     `Column{Name, ResolvedNode{Labels: <canonical key>}}`.
   - **Property** (`Ref.Property != ""` and the parser's
     `RefProjection.Type()` is `TypeUnknown`): look up
     `Ref.Property` on the resolved node type's `Properties`.
     - Miss → `ErrUnknownProperty`.
     - Hit → emit `Column{Name, ResolvedProperty{Type, Nullable}}`.
5. For each `Query.Parameters`:
   - The lone `Use` is a `PropertyUse` (parser R0-scope invariant).
   - Look up the `PropertyUse.Ref().Property` on the resolved node
     type keyed by `PropertyUse.Ref().Variable`.
     - Miss → `ErrUnknownProperty`.
     - Hit → emit `ResolvedParameter{Name, ResolvedProperty{Type,
       Nullable}}`.
6. Copy `Query.StatementKind` (always `StatementRead` at R0).

Everything else — parameter unification, non-`RefProjection`, more than
one binding, edges, WITH, UNION, writes, CALL — falls into an
`ErrOutOfR0Scope` gate (§5).

---

## 5. Sentinels — the R0 set

Package-level sentinels in `internal/resolver/errors.go`, wrapped at
the fail-site with detail (`fmt.Errorf("%w: %s", ErrUnknownLabel,
label)`) — the cypher/schema-gql convention (ADR 0009).

R0's closed set:

```go
// ErrUnknownLabel is returned when a node binding's label set does not
// resolve to a declared node type in the schema.
var ErrUnknownLabel = errors.New("unknown label")

// ErrUnknownProperty is returned when a property reference (a property
// projection or an inline-map parameter use) names a property the
// resolved node type does not declare.
var ErrUnknownProperty = errors.New("unknown property")

// ErrOutOfR0Scope is returned when the query contains a construct the
// R0 capability scope does not support (edges, multiple bindings,
// non-Ref projections, WITH, UNION, writes, CALL, parameters outside
// an inline node property map, more than one Use per Parameter).
// Its fail-sites are retired stage by stage as R1..R7 introduce the
// constructs. **Intermediate retirement, not per-stage retirement of
// the sentinel itself:** R1 removes the edge-binding fail-site but the
// sentinel stays in `allSentinels` because CALL, writes, WITH, UNION,
// aggregates, etc. still route to it. The sentinel is retired from
// `allSentinels` only when the *last* fail-site goes away — expected
// around R6 when CALL/YIELD (R7) is the only remaining construct and
// a dedicated `ErrUnknownProcedure` (or the R7-owned successor) takes
// over. Between R0 and that retirement, `TestSentinelReachability`
// keeps ErrOutOfR0Scope reachable via at least one surviving
// out-of-scope fixture.
var ErrOutOfR0Scope = errors.New("query construct not supported at resolver stage R0")
```

**Set closure at R0.** Three sentinels: `ErrUnknownLabel`,
`ErrUnknownProperty`, `ErrOutOfR0Scope`. Each carries at least one
negative fixture (§6.3). The reachability sweep (§6.5) is bidirectional:
every sentinel has a fixture; every fixture maps to a canonical
sentinel.

**Why no `ErrUnboundVariable` at R0?** The Cypher parser rejects
unbound variables at build time (`internal/query/cypher/build.go:157`,
`errors.go:37`): a `query.Query` value that reaches `Resolve` cannot
carry an unbound `Ref` — the parser fails first. A resolver-side
`ErrUnboundVariable` sentinel would have no reachable fail-site and
would break the reachability sweep for the same reason
`ErrParameterTypeConflict` does (below). Defensive coverage against a
hypothetical parser regression belongs in the parser's own test suite,
not in the resolver's closed sentinel set.

**Why `ErrOutOfR0Scope` and not per-construct sentinels
(`ErrUnsupportedEdge`, `ErrUnsupportedProjection`, ...)?** The parser's
Stage-0 spec kept the sentinel set category-grained for exactly this
reason: when a later stage starts supporting a construct, the resolver
deletes one `Err…Scope` fail-site — no sentinel renames, no consumer
migration. Category-grained keeps churn low; the wrapped message
carries the specifics ("unsupported at R0: edge binding", "unsupported
at R0: WITH clause"). By R2 the sentinel will have narrowed enough
that a rename may be warranted — that is R2's judgment call, not R0's.
Same posture as `ErrUnsupportedClause` on the parser side.

**Why not `ErrParameterTypeConflict` at R0?** ADR 0009 names it in the
consequential sentinel list (`ErrUnknownLabel`, `ErrUnknownProperty`,
`ErrParameterTypeConflict`, …). Parameter unification is R2's business
(ADR 0009 §Stages). R0's capability scope guarantees
`len(Parameter.Uses) == 1`, so a type conflict is unreachable at R0.
Adding a sentinel with no fail-site would break the reachability sweep.
The sentinel arrives with R2 — that is the correct home.

---

## 6. The golden-pair harness

Under `test/data/resolver/`, mirroring the `internal/schema/gql`
harness shape (`test/data/schema/gql/{valid,invalid}`) but adapted for
the two-input stage: schemas + queries → `ValidatedQuery` snapshots.

### 6.1 Layout

```
test/data/resolver/
  valid/
    schemas/
      social.gql
      catalog.gql
    <query>.cypher
    <query>.cypher.validated.golden.json
    schema.mapping.json
  invalid/
    schemas/
      social.gql
    <query>.cypher
    schema.mapping.json
```

- **`schemas/`** — hand-authored GQL schemas, one file each. Shared by
  many queries where possible (ADR 0009: "one schema fixture is shared
  by many query fixtures where possible"). R0 seeds `social.gql` — one
  node type, three properties (a mix of nullable and not-null, an INT
  and a STRING and a nullable INT) — enough to exercise the R0 fixture
  set without breeding schema files.
- **`<query>.cypher`** — the Cypher query, one file each. The file
  name is the fixture identity used in the golden pairing and the
  invalid-fixture-to-sentinel map. The name must sort deterministically
  and read as a description of the shape (`node_whole_entity.cypher`,
  `node_property_string.cypher`, `unknown_label.cypher`, ...).
- **`<query>.cypher.validated.golden.json`** — the `ValidatedQuery`
  serialised by `encoding/json.MarshalIndent`. One golden per
  valid-fixture query. `-update` regenerates. **Deliberate deviation
  from `schema/gql`'s `<fixture>.gql.golden.json` naming:** the
  resolver's stage sits downstream of the parser, and the same
  `<query>.cypher` file could plausibly carry a parser-side golden
  (a snapshot of `query.Query` before resolution) if the resolver's
  test suite were ever combined with a parser-side snapshot in this
  fixture tree. Interposing `.validated.` makes the resolver golden
  self-labelling — a reader sees "this is the *validated* form of
  `<query>.cypher`, not the parsed form" without opening the file.
  The parser suite already has its own golden convention in
  `internal/query/cypher/testdata/`; the resolver stays disjoint.
- **`schema.mapping.json`** — a static JSON file that pairs each
  query fixture (basename) with its schema (basename inside
  `schemas/`). Explicit pairing, not a naming convention: it means one
  schema can serve many queries (the ADR 0009 goal) and a fixture's
  intent stays legible in one place. The mapping is:

  ```json
  {
    "node_whole_entity.cypher": "social.gql",
    "node_property_string.cypher": "social.gql",
    "unknown_label.cypher": "social.gql",
    "unknown_property.cypher": "social.gql"
  }
  ```

  The test asserts the mapping is total against the fixture dir
  (mirrors `invalidFixtures` totality in schema/gql).

### 6.2 The `-update` flag

A test-local `var update = flag.Bool("update", false, "regenerate
resolver goldens")` in `resolver_test.go`. When set, the valid-fixture
sub-test writes the `.validated.golden.json` file instead of asserting
against it. Same shape as `schema/gql`'s golden regeneration.

### 6.3 The invalid-fixture map

`invalidFixtures` maps each `<query>.cypher` in `test/data/resolver/
invalid/` to its expected R0 sentinel:

```go
var invalidFixtures = map[string]error{
    "unknown_label.cypher":        ErrUnknownLabel,
    "unknown_property.cypher":     ErrUnknownProperty,
    "edge_binding.cypher":         ErrOutOfR0Scope,
    "with_clause.cypher":          ErrOutOfR0Scope,
    "aggregate_projection.cypher": ErrOutOfR0Scope,
    "return_distinct.cypher":      ErrOutOfR0Scope,
    // ... one row per invalid-fixture file
}
```

The test requires the map to be total against the fixture dir (every
`.cypher` in `invalid/` is mapped; every mapped key exists on disk).
Same discipline as `schema/gql`'s `invalidFixtures`.

### 6.4 The suite

`resolver_test.go` — testify `suite.Suite`, one test per concern:

- **`TestValid`** — walks `valid/*.cypher`, parses each via
  `cypher.New().Parse(...)`, parses its paired schema via
  `gql.New().Parse(...)`, constructs a `resolver.New(schema)` (no
  registry needed at R0), calls `Resolve`, and either writes the
  golden (`-update`) or `s.JSONEq`s against the golden. `Resolve` must
  succeed; a resolver failure on a valid fixture fails the test with
  the fixture name.
- **`TestInvalid`** — walks `invalid/*.cypher`, parses each via
  `cypher.New().Parse(...)`, parses its schema, calls `Resolve`, and
  asserts (a) the returned `ValidatedQuery` is the zero value, and
  (b) `errors.Is(err, invalidFixtures[name])`. Map totality asserted at
  the top of the test.
- **`TestSentinelReachability`** — the bidirectional sweep: the
  covered set (from `invalidFixtures`'s values) equals the canonical
  set (`allSentinels`). Fails when a sentinel has no negative fixture
  or a fixture uses a non-canonical sentinel.

The suite is testify (AGENTS.md: `suite.Suite` for
setup/grouping, `require` for fail-fast), table-driven per convention.

### 6.5 Sentinel reachability sweep

An exact port of `schema/gql`'s `TestSentinelReachability` (§6.4).
`allSentinels` in `errors.go` is the canonical list; `TestSentinelReachability`
compares it against the values of `invalidFixtures` in `resolver_test.go`.
A new R0 sentinel must be added to both; a retired one must be dropped
from both. Same posture as the parser.

### 6.6 Non-obvious harness invariants

- **Fixtures use only R0 shapes.** A valid fixture is one query
  matching one labelled node, projecting whole-entity and/or property
  refs, with at most one parameter per node inline map. **Literal-only
  WHERE / ORDER BY / SKIP / LIMIT are silently accepted** (ADR 0005:
  the original text runs; codegen re-emits it) — these constructs are
  architecturally invisible in the model because `query.Part` carries
  only `Bindings`, `Returns`, `ReturnsAll`, `Distinct`, and `Effects`
  (`internal/query/query.go:81-123`). They surface in the model *only*
  when they contribute a `Parameter.Use` (a `ClauseSlotUse` for
  `SKIP $n` / `LIMIT $n`, or an `ExprUse` for a predicate parameter),
  and it is that `Use` — not the WHERE / ORDER BY / SKIP / LIMIT
  clause itself — that routes to `ErrOutOfR0Scope` via the "Parameter
  with more than one `Use`; `ClauseSlotUse`; `ExprUse`" row of §7. A
  literal-only clause leaves no witness in `query.Query`, so the R0
  kernel cannot see it — that is a fixture-hygiene rule the harness
  cannot police from the output, and this bullet is where the
  discipline lives: valid fixtures avoid these clauses so no golden
  accidentally exercises a future stage's typing via a parametric
  path, and reviewers reading a fixture know the file is R0-scope by
  its literal absence of them.
- **Invalid fixtures are the exact minimum for reachability.** One
  fixture per sentinel is the floor. `ErrOutOfR0Scope` gets multiple
  fixtures because it covers many constructs; the test only requires
  membership, not per-construct coverage.
- **`schema.mapping.json` totality.** The valid and invalid tests each
  read their own mapping file; the map's keys are exactly the file
  basenames in the fixture directory. `TestValid` and `TestInvalid`
  both `s.Require().Len(mapping, len(files))` before iterating.

---

## 7. R0 capability scope — what resolves

**In scope:** a single Cypher query the parser accepts, whose parsed
`query.Query` shape is:

- Exactly one `Branch` in `Branches`; zero `Combinators`.
- Exactly one `Part` in the branch's `Parts`.
- The part's `Bindings` are exactly one `NodeBinding` with a non-empty
  `Labels()` (labelled — R0's line says "labelled").
- The part's `Returns` is a non-empty slice of `ReturnItem`s, each
  carrying a `RefProjection` whose `Ref` names the (single) node
  binding, either whole-entity (`Property == ""`) or single-level
  property (`Property != ""`).
- `ReturnsAll` is false; `Distinct` is false; `Effects` is empty.
- `Parameters` is a slice of `Parameter`s each with exactly one
  `PropertyUse` sitting on the node binding's inline property map.
- `StatementKind` is `StatementRead`.

**Out of scope, routed to `ErrOutOfR0Scope`:**

| Construct                                              | R-stage owner |
|--------------------------------------------------------|---------------|
| Edge bindings; two-orientation trial; multi-type edges | R1 / R3       |
| Two or more bindings in a part (edges, unlabelled node inference) | R1 |
| Non-`RefProjection` items (literals, funcs, exprs, aggregates) | R2 |
| Parameter with more than one `Use`; `ClauseSlotUse`; `ExprUse`  | R2 / R5 |
| Property-typed `PropertyUse` on an edge property       | R1            |
| Nullability upgrades (OPTIONAL MATCH regimes)          | R4            |
| `Part.Distinct == true` (`RETURN DISTINCT` / `WITH DISTINCT`) | R5      |
| `Part.ReturnsAll == true` (`RETURN *` / `WITH *`)      | R5            |
| `WITH` carry-forward; `UNION`                          | R5            |
| CREATE / MERGE / SET / REMOVE / DELETE                 | R6            |
| CALL / YIELD                                           | R7            |
| Path bindings                                          | R5            |
| Unwind bindings                                        | R5 or later   |

**Silently accepted (not routed anywhere):**

| Construct                                              | Owner      |
|--------------------------------------------------------|------------|
| Literal-only `WHERE <predicate>` (no parameter)        | ADR 0005   |
| Literal-only `ORDER BY <expr>` (no parameter)          | ADR 0005   |
| Literal `SKIP <int>` / `LIMIT <int>` (no parameter)    | ADR 0005   |

These clauses leave *no witness* in the `query.Query` model:
`query.Part` carries `Bindings`, `Returns`, `ReturnsAll`, `Distinct`,
and `Effects` (`internal/query/query.go:81-123`) — and *none* of those
five fields carries a WHERE, an ORDER BY, or a literal SKIP/LIMIT.
WHERE and ORDER BY have no field at all; SKIP and LIMIT surface only
via a `ClauseSlotUse` on a `Parameter`. Without a parameter, the R0
kernel cannot see the clause — and by ADR 0005 the executed query is
the original text, so the runtime honours WHERE / ORDER BY / SKIP /
LIMIT verbatim. Codegen re-emits the same original text; the
resolver's job is to produce a `ValidatedQuery` that *types* the
result columns and parameters, not to enumerate every accepted
clause.

A future reader must not attempt to route these to `ErrOutOfR0Scope`:
there is no witness in the model to guard on, and proposing a
`query.Query` amendment to expose the clauses would be an ADR 0008
additions convention change — out of scope for R0.

**`RETURN DISTINCT` / `WITH DISTINCT` / `RETURN *` / `WITH *` are NOT
in this silently-accepted set.** They *are* witnesses in the model:
the parser sets `Part.Distinct = true`
(`internal/query/cypher/expr.go:35-37`) and forwards it to `NewPart`
(`internal/query/cypher/build.go:236`); `Part.ReturnsAll` is set the
same way. Both are first-class cardinality-affecting bits by ADR 0008
lines 38-40 (the before Stage 14 Part-DISTINCT axis) and by `query.go:105-114`
("Distinct is true iff the part's projection body carried the DISTINCT
keyword … Composes freely with ReturnsAll … a different cardinality
decision on a different model surface"). Silently ignoring
`Part.Distinct` would let `RETURN DISTINCT n` produce the same
`ValidatedQuery` as `RETURN n` — exactly the cardinality mismatch the
before Stage 14 fix exists to prevent. They therefore route to
`ErrOutOfR0Scope` via the two dedicated rows above.

Each row above corresponds to at least one guard in the R0 kernel that
emits `ErrOutOfR0Scope` — the parser accepts, but the resolver rejects
until the stage that owns the construct lands.

The R0 kernel returns the first out-of-scope guard hit (short-circuit
per §2.3). A query that violates two out-of-scope rules yields the
first one the walk sees; the mapping table in `invalidFixtures` pins
the exact fixture-to-message pair so a resolver reorder is caught by
the reachability sweep and the invalid test.

**Recorded ADR 0009 cross-check.** ADR 0009 R0: "labelled single-node
MATCH/RETURN (whole-entity and property refs) resolves end to end."
This spec's §7 in-scope list is exactly that sentence unpacked to
`query.Query` shape terms; the out-of-scope list is the exhaustive
complement inside the parser's accept set. Nothing in §7 disagrees
with ADR 0009 by construction.

---

## 8. Definition of done for R0 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is out
of scope of this document. The spec is done when:

1. This file exists at `docs/specs/resolver-stage-r0.md`, committed on
   branch `resolver-r0-spec`.
2. The type-mapping table (§4) covers all seventeen
   `query.Type` variants, each classified and each with an owner
   stage. R0-owned rows are named.
3. `ValidatedQuery`'s R0 shape (§3) is decided: fields, `Column`,
   `ResolvedParameter`, the `ResolvedType` sum with its two R0
   variants (`ResolvedNode`, `ResolvedProperty`).
4. The `Resolver` seam (§2.2) is decided: `QueryResolver` interface
   name, one method, compile-time assertion location, reconciliation
   with ADR 0008's `New` returning the concrete type.
5. The R0 sentinel set (§5) is closed and named.
6. The golden-pair harness (§6) is designed: layout, `-update`,
   invalid-fixture map totality, reachability sweep.
7. The R0 capability scope (§7) is stated in shape terms and its
   out-of-scope complement is enumerated with R-stage owners.
8. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer); every
blocker he raises is fixed on this same branch before the branch
merges. Cycle 2 (the skeleton implementation, `resolver-r0-skeleton`
stacked on this branch) begins only when the spec cycle merges.
