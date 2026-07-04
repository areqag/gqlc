# Stage R2 spec — resolver: typing, non-Ref projections, parameter unification

The implementation brief for Stage R2 of `internal/resolver`, extending the
merged R0/R1 kernel (`docs/specs/resolver-stage-r0.md`,
`docs/specs/resolver-stage-r1.md`) with the capability ADR 0009 assigns to
R2: **property-type upgrade of `unknown` and parameter unification across
`Uses` (property, clause-slot, expression) with a conflict sentinel**. Build
this **test-first**. Scope, sequencing, error posture, `ValidatedQuery`
top-level shape, purity, and the golden-pair harness inherit from ADR 0009,
R0 and R1 unchanged; this document revises only the type-mapping rows,
kernel walks, `ResolvedType` sum, sentinel set, and out-of-scope table
entries that R2 changes.

Stage R2 lowers a **labelled single-`MATCH` pattern** (as R1 admits it) that
carries **non-`RefProjection` projection items** (`LiteralProjection`,
`FuncProjection`, `ExprProjection`) and **parameters with multiple uses or
non-`PropertyUse` uses** (`ClauseSlotUse`, read-side `ExprUse`). Undirected
/ multi-type / var-length edges, WITH / UNION / DISTINCT / `RETURN *`,
`AggregateProjection`, writes, CALL, and OPTIONAL-nullability refinement
stay out of scope and continue to route to `ErrOutOfR0Scope` (unchanged
name; category-grained per R0 §5).

---

## 1. Deliverables

- `internal/resolver/validated.go` — extended with four new `ResolvedType`
  variants (§3): `ResolvedScalar` (parser-coarse scalars from literals /
  clause slots), `ResolvedTemporal` (the six openCypher temporals),
  `ResolvedList` (parametrised over an element `ResolvedType`), and
  `ResolvedUnknown` (the resolver's honest admission when no schema
  witness commits and the parser was already `TypeUnknown`). R0/R1
  variants (`ResolvedNode`, `ResolvedProperty`, `ResolvedEdge`) are
  unchanged.
- `internal/resolver/errors.go` — extended with one new sentinel
  (`ErrParameterTypeConflict`, §5.1) and revised prose on
  `ErrOutOfR0Scope` (§5.2). R0/R1 sentinels
  (`ErrUnknownLabel`, `ErrUnknownProperty`, `ErrUnknownEdge`,
  `ErrAmbiguousBinding`) are unchanged in identity; `ErrUnknownProperty`'s
  wrapped fail-message set widens (§5.2).
- `internal/resolver/resolve.go` — extended with:
  - a projection dispatch that admits non-`RefProjection` items and maps
    each to a `ResolvedType` (§4.5);
  - a parameter walk that unifies each `Parameter.Uses` across
    `PropertyUse` / `ClauseSlotUse` / (read-side) `ExprUse` (§4.6);
  - a `queryType → ResolvedType` mapper (§4.7) used by both walks.
- `test/data/resolver/valid/schemas/` — one new schema fixture
  (`social_r2.gql`, §6.2) reusing the R1 shape with two extra properties
  wired for parameter-unification coverage. The R0 `social.gql` and R1
  `social_r1.gql` stay untouched; R2 fixtures point at `social_r2.gql`.
- `test/data/resolver/valid/*.cypher` + `.validated.golden.json` — new R2
  valid fixtures (§6.3), each paired with its schema through the updated
  `schema.mapping.json`.
- `test/data/resolver/invalid/*.cypher` — new R2 invalid fixtures for the
  new sentinel and for the R2-remainder `ErrOutOfR0Scope` sub-cases
  (§6.4).
- `internal/resolver/resolver_test.go` — updated `invalidFixtures` map
  (§6.4). No structural changes to `TestValid` / `TestInvalid` /
  `TestSentinelReachability` are required; the R0/R1 harness scales as-is.

Nothing downstream of the resolver is built — `ValidatedQuery` is
provisional through R7 (ADR 0009 §Decision).

---

## 2. Architecture — deltas from R1

R0/R1's architecture stands (the `Resolver` struct, its compile-time
inputs, `QueryResolver` interface + compile-time assertion, purity and
short-circuit, `resolve.go`/`Resolve` split, three-phase kernel A1/A2/B/C
for binding resolution — R1 §2). R2 extends only the projection walk and
the parameter walk; no new exported types except the four new
`ResolvedType` variants and the one new sentinel.

### 2.1 The R2 kernel structure

The kernel remains one linear pass. Steps 1–3 (query-level gating, Phase
A/B/C binding resolution) are unchanged. R2 replaces R1 §4.5's
projection step and R1 §4.6's parameter step with:

- **Step 4 (revised) — projection dispatch.** Every `Projection` variant
  admitted at R2 (`RefProjection`, `LiteralProjection`, `FuncProjection`,
  `ExprProjection`) is dispatched to a variant-specific handler that
  returns a `ResolvedType`; the R2-refused variant
  (`AggregateProjection`) still routes to `ErrOutOfR0Scope` (§4.5).
- **Step 5 (revised) — parameter unification.** Every `Parameter.Uses` is
  walked once; each `Use` contributes a *witness* `ResolvedType`; the
  witnesses are unified pairwise with the R2 lattice (§4.6);
  disagreement is `ErrParameterTypeConflict`. Non-admitted Uses
  (`ExprInSetValue`, `ExprInDeleteTarget`) continue to route to
  `ErrOutOfR0Scope`.

Both revised steps share the `queryType → ResolvedType` mapper (§4.7),
so the mapping table (§4.10) has one implementation surface.

### 2.2 Kernel helpers — three new, one revised

Four helpers in `resolve.go`:

- **`projectionType(p query.Projection, nodeTypes, edgeTypes, edgeKeys)
  (ResolvedType, error)`** — the new projection dispatcher (§4.5).
  Extends `r1ColumnType` with the non-`RefProjection` variants;
  `r1ColumnType` is inlined into the `RefProjection` case and retired as
  its own function.
- **`resolveType(t query.Type) ResolvedType`** — the parser-`Type` →
  resolver-`ResolvedType` mapper (§4.7). Total, deterministic, pure —
  the closed R0 §4 table's implementation. Used by
  `LiteralProjection` / `FuncProjection` / `ExprProjection` cases in
  §4.5 and by `ExprUse` in §4.6.
- **`useWitness(u query.Use, nodeTypes, edgeTypes) (ResolvedType,
  error)`** — the new per-`Use` witness computer (§4.6). Dispatches on
  the sealed `Use` sum: `PropertyUse` → schema lookup;
  `ClauseSlotUse` → `ResolvedScalar{ScalarInt}`; `ExprUse` (read-side
  positions) → `resolveType(u.EnclosingType())`. Write-side positions
  return `ErrOutOfR0Scope`.
- **`unify(a, b ResolvedType) (ResolvedType, bool)`** — the new
  unification primitive (§4.6). Total, deterministic. Returns the
  agreed type on success, `(_, false)` on conflict. The R2 lattice is
  small (§4.8), so the primitive is a switch over variant pairs.

### 2.3 Purity, determinism, short-circuit — unchanged

R0 §2.3 stands verbatim. R2 introduces no new goroutine, no map
iteration escaping into the output, no time source. Parameter walks in
first-appearance order (R0/R1 §2.3); projection walks in source order.
`unify` is deterministic given a deterministic Use order (which the
parser guarantees: `Uses` is a slice populated at
first-appearance time — `internal/query/query.go:1270-1273` and
`build.go` mining).

---

## 3. `ValidatedQuery` — the R2 shape

`ValidatedQuery`'s top-level shape (R0 §3.1) is unchanged at R2:
`Columns`, `Parameters`, `Statement`. The extension is on the
`ResolvedType` sum. Every added variant is chosen to keep the mapping
table (§4.10) total and to preserve ADR 0002's bit-width discipline on
the schema-witnessed rows (`ResolvedProperty` is untouched — it is
already width-carrying — and no new variant collapses a width the schema
would carry).

### 3.1 `ResolvedScalar` — parser-coarse scalars

```go
// ResolvedScalar carries a parser-coarse scalar kind: an openCypher
// literal or an integer clause slot (SKIP / LIMIT) — the row of the R0
// §4 mapping table where the parser has already committed a specific
// query.Type but the type has no bit width because the source is not a
// schema-typed column. Distinct from ResolvedProperty, which carries a
// bit-width family from the schema (ADR 0002).
type ResolvedScalar struct {
    Kind Scalar `json:"scalar"`
}

// Scalar is the closed enum of parser-coarse scalar kinds. Mirrors
// query.Type's scalar sub-sum (bool, int, float, string, null, map) plus
// the R2 clause-slot integer contribution — the schema-free rows of the
// R0 §4 mapping table. Int-backed with a String() so the JSON tag has
// one source (the pattern AggregateFunc / ClauseSlot follow).
type Scalar int

const (
    ScalarBool Scalar = iota
    ScalarInt
    ScalarFloat
    ScalarString
    ScalarNull
    ScalarMap
)

// String is the wire tag ("bool" / "int" / ...). Single source the JSON
// encoding derives from.
func (s Scalar) String() string { ... }
```

`ResolvedScalar` is `final` in the R0 §4 sense: the parser already
committed, the resolver copies. Wire tag: `"scalar"`; JSON discriminator
adds the scalar's kind. Nullability is *not* a field on
`ResolvedScalar` — a literal `1` is not nullable in the openCypher sense
(literals are always non-null), and the clause-slot integer is defined
by the slot's own nullability semantics (SKIP / LIMIT reject a NULL
integer at runtime — ADR 0005, the original text runs). If a future
consumer needs a nullable-scalar distinction, it lands as an additive
axis under the ADR 0008 protocol post-freeze.

### 3.2 `ResolvedTemporal` — the six openCypher temporals

```go
// ResolvedTemporal carries an openCypher temporal kind. Distinct from
// ResolvedProperty's TIMESTAMP / DATE bit-width families (ADR 0002) —
// the schema records only DATE / TIMESTAMP, while expressions produce
// the full openCypher temporal set (date, time, localtime, datetime,
// localdatetime, duration). R2 draws the storage-vs-expression line.
type ResolvedTemporal struct {
    Kind Temporal `json:"temporal"`
}

type Temporal int

const (
    TemporalDate Temporal = iota
    TemporalTime
    TemporalLocalTime
    TemporalDateTime
    TemporalLocalDateTime
    TemporalDuration
)
```

`final` classification. Wire tag: `"temporal"`. The rationale for a
separate variant (rather than reusing `ResolvedProperty` with a
`graph.PropertyType`) is recorded in R0 §4.5: `graph.PropertyType`
carries only `DATE` and `TIMESTAMP`; the expression-side vocabulary
must carry the full temporal set. Not reached by any R0/R1 fixture; R2
introduces the variant with fixtures that exercise a temporal
`FuncProjection` result (e.g. `RETURN date() AS d`) — see §6.3.

**Judgment call — reaching the parser wall.** Parser Stage 7 emits the
temporal variants only from the temporal constructors (`date(...)`,
`time(...)`, etc.) and from temporal arithmetic. R2 admits these via
the `FuncProjection` / `ExprProjection` row of the projection walk
(§4.5). A `RefProjection` from a schema `TIMESTAMP` property emits
`ResolvedProperty{TypeTimestamp}` — that path already works at R0/R1 and
is untouched; the two paths (property vs expression) sit alongside each
other in the sum without collision.

### 3.3 `ResolvedList` — parametrised list

```go
// ResolvedList is a list of elements. Element is the recursive
// resolved type of the list's element position — reached via the same
// resolveType mapper the parametric TypeList{element} row of the R0 §4
// mapping table names.
type ResolvedList struct {
    Element ResolvedType `json:"element"`
}
```

`schema-upgraded` classification when the element is
`ResolvedProperty` (a schema-typed list column would carry the element's
`graph.PropertyType` — deferred to R3+ when the schema gains list-typed
properties). At R2, the reachable case is a literal list expression
(`[1, 2, 3]` in a `LiteralProjection`) whose element is a
`ResolvedScalar`. Wire tag: `"list"`; JSON payload carries the recursive
element.

**Judgment call — a `TypeList` whose element is `TypeUnknown`.** Parser
lowers a mixed-element or empty list as
`TypeList(TypeUnknown)` (`type.go:139-142`). R2's `resolveType` mapper
lowers `TypeUnknown` to `ResolvedUnknown` (§3.4) — so the R2 element of
such a list is `ResolvedList{Element: ResolvedUnknown{}}`. The R3 stage
takes the honest schema-side upgrade when var-length hop-ranges land
(`list<edge>`); R2 does not attempt element-type synthesis for a
literal empty list — the parser's `unknown` element is the ground
truth at R2.

### 3.4 `ResolvedUnknown` — the resolver's admission

```go
// ResolvedUnknown is the resolver's honest posture when no schema
// witness commits and the parser was already TypeUnknown: a rich
// expression whose result type the parser could not compute (property-
// participating arithmetic, NULL propagation), a non-aggregate function
// call's result (function identity below the type-interface boundary,
// ADR 0005), a list of unknown-element parser-coarse type. The runtime
// re-executes the original text; the resolver records "unknown" as
// the honest column / parameter type. Codegen post-freeze may reject
// or degrade a ResolvedUnknown-typed column, but that is a future
// consumer decision — the R2 model does not fail on it.
type ResolvedUnknown struct{}
```

Wire tag: `"unknown"`. `schema-upgraded` classification per the R0 §4
table's `TypeUnknown (other)` row — R2 owns the "other" sub-case (a
`FuncProjection`, an `ExprProjection` with an unknown residue). The row
that says "aggregate result" defers to R6/R7 per R0 §4.3 (aggregate is
not admitted at R2). The row that says "list element" is covered by
§3.3.

**Judgment call — why R2 does not attempt schema-side inference for
`FuncProjection` / `ExprProjection`.** ADR 0005 places function identity
below the type-interface boundary; the parser's `TypeUnknown` on a
`FuncProjection` result is the honest posture, and the resolver has no
signature registry for non-procedure functions (the `procsig.Registry`
covers CALL / YIELD — R7). Inferring a function's result type from its
name would either duplicate an unknown corpus or fail
overwhelmingly-often. The correct resolver posture is to admit the
column, emit `ResolvedUnknown`, and let codegen decide whether to
tolerate or reject at generation time. Same posture for `ExprProjection`
whose parser result is `TypeUnknown`: the resolver could re-parse the
projection's original text span (the gqlc-gyw pattern R5 records for
grouping keys), but ADR 0009 places rich-expression inference at R5,
not R2. R2 draws the honest boundary.

### 3.5 R0/R1 variants — unchanged

`ResolvedNode`, `ResolvedProperty`, and `ResolvedEdge` keep their R0/R1
shape and wire encoding. The `ResolvedType` sum grows to seven variants;
nothing R0/R1 produced changes wire form. Fixtures whose R0/R1 golden
encoded nothing R2-adjacent remain byte-identical.

### 3.6 Wire-encoding invariants

Every new variant emits a tagged-union JSON object with a `"kind"`
discriminator — the sealed-sum convention `query.Type` and R0/R1
`ResolvedType` already use. The wire tags:

| Variant | `"kind"` | Extra fields |
|---|---|---|
| `ResolvedScalar` | `"scalar"` | `"scalar"` (one of `bool`/`int`/`float`/`string`/`null`/`map`) |
| `ResolvedTemporal` | `"temporal"` | `"temporal"` (one of `date`/`time`/`localtime`/`datetime`/`localdatetime`/`duration`) |
| `ResolvedList` | `"list"` | `"element"` (recursive tagged-union) |
| `ResolvedUnknown` | `"unknown"` | (none) |

The `"kind"` discriminator is disjoint across every `ResolvedType`
variant, so a consumer can identify a resolved type from its JSON
without a preview of the enclosing structure — same discipline as
`query.Type`.

---

## 4. The R2 kernel algorithm

Each step below extends or replaces a numbered step of R1 §4. Steps 1–3
(query-level gating, Phases A/B/C for bindings) are unchanged. R2
revises the projection and parameter walks, adds one shared mapper, and
adds the unification primitive.

### 4.1 Steps 1–3 (unchanged) — query-level and binding gating

R1 §4.1–§4.4 stand verbatim: one branch, one part, no writes, no CALL,
`Part.Distinct` / `Part.ReturnsAll` continue to route to
`ErrOutOfR0Scope`. Bindings resolve through Phase A1 (labelled nodes) /
A2 (labelled directed single-hop edges) / B (unlabelled inference) / C
(deferred edge closure). R2 does not touch these gates. The R1 edge
predicate (`Directed() == true`, `Hops() == nil`, `len(Labels()) == 1`)
still governs — undirected / multi-type / var-length edges continue to
route to `ErrOutOfR0Scope` (R3's business).

### 4.2 Step 4 (revised) — projection dispatch

Iterate `Part.Returns` in source order. For each `ReturnItem`, dispatch
its `Value Projection` on the `Projection` sum (five variants —
`internal/query/query.go:1030-1036`):

- **`RefProjection`** — unchanged from R1 §4.5. Look up
  `ref.Variable` in `nodeTypes` / `edgeTypes` / `edgeKeys`; emit
  `ResolvedNode` / `ResolvedEdge` (whole-entity) or `ResolvedProperty`
  (property lookup). `ErrUnknownProperty` on a property miss. The R2
  extension: the property-side `TypeUnknown` row from R0 §4.3 is the
  *only* projection-side `TypeUnknown` R0/R1 emits at
  `RefProjection.Type()` — that row was R0-owned already, so R2 keeps
  it verbatim.
- **`LiteralProjection`** — call `resolveType(p.Type())` (§4.7) and
  emit `Column{Name, <resolved>}`. Total: `LiteralProjection.Type()` is
  never `TypeNode` / `TypeEdge` / `TypePath` (no literal syntax for
  those, `internal/query/query.go:107-113`, 123-125, 271-278) and
  the coarse scalar / list / temporal rows all have a `resolveType`
  answer.
- **`FuncProjection`** — call `resolveType(p.Type())`. Parser posture:
  `FuncProjection.Type()` is `TypeUnknown` today for every non-aggregate
  function call (parser has no signature registry for non-procedure
  functions, `internal/query/query.go:1112-1114`). `resolveType(TypeUnknown)`
  is `ResolvedUnknown` (§4.7), so every `FuncProjection` column resolves
  to `ResolvedUnknown` at R2 — the honest admission (§3.4). If a future
  parser stage commits a concrete `FuncProjection.Type()` for a specific
  function (a temporal constructor, say, per Stage-7 typing), the R2
  mapper's temporal row is already ready and the column upgrades
  automatically — no R2-side kernel change.
- **`ExprProjection`** — call `resolveType(p.Type())`. Parser posture:
  `ExprProjection.Type()` is often concrete (a boolean predicate on a
  literal returns `TypeBool`) and often `TypeUnknown` (any
  property-participating expression, NULL propagation). Both paths
  resolve honestly: concrete rows → their resolved counterpart;
  `TypeUnknown` → `ResolvedUnknown`.
- **`AggregateProjection`** — return
  `ErrOutOfR0Scope` with the wrapped message `"aggregate projection
  (R5 owns grouping)"`. R5's grouping-key work (ADR 0009) is where the
  cardinality-affecting shape lands; R2 is a pure typing stage and
  refuses to speculate.

The dispatch is exhaustive over the `Projection` sum (five variants, all
covered — four resolved, one refused). A future additive variant on
`Projection` (post-freeze, ADR 0008 protocol) is a compile error at the
switch's zero-arms default — a stage-later specification will pick it up.

### 4.3 Step 5 (revised) — parameter unification

Iterate `Query.Parameters` in first-appearance order. For each
`Parameter`:

1. Compute a *witness* `ResolvedType` per `Use` via
   `useWitness(u, nodeTypes, edgeTypes)` (§4.6, §4.7).
2. Fold the witnesses left-to-right with `unify` (§4.8). On a conflict
   (`unify` returns `(_, false)`), return
   `ErrParameterTypeConflict` wrapped with the parameter name and the
   disagreeing witnesses' `String()`s.
3. Emit `ResolvedParameter{Name: p.Name, Type: <unified>}`.

A parameter with zero `Uses` cannot occur — the parser only records a
`Parameter` when at least one `Use` is mined
(`internal/query/cypher/build.go:mining paths` cited in the Cypher
parser tests). The R2 walk therefore does not guard on the empty-Uses
case; a golden fixture reaching that shape would be a parser bug.

### 4.4 Step 6 (unchanged) — statement kind

Copy `Query.StatementKind` into `ValidatedQuery.Statement`. R2 remains
read-only (writes at R6).

### 4.5 `projectionType` — the projection dispatcher

Signature:

```go
func projectionType(
    p query.Projection,
    nodeTypes map[string]schema.NodeType,
    edgeTypes map[string]schema.EdgeType,
    edgeKeys map[string]schema.EdgeKey,
) (ResolvedType, error)
```

The dispatch table:

| Variant | Handler | Returns |
|---|---|---|
| `query.RefProjection` | R1 §4.5 path (inlined) | `ResolvedNode` / `ResolvedEdge` / `ResolvedProperty` |
| `query.LiteralProjection` | `resolveType(p.Type())` | any of §3.1–§3.4 |
| `query.FuncProjection` | `resolveType(p.Type())` | `ResolvedUnknown` (today's parser posture) |
| `query.ExprProjection` | `resolveType(p.Type())` | concrete or `ResolvedUnknown` |
| `query.AggregateProjection` | reject | `ErrOutOfR0Scope` |

The `RefProjection` case does *not* route through `resolveType` — a
`RefProjection` carries a `Ref`, and the resolver's schema witness lives
on the referenced binding, not on the projection's `Type()`. Routing
through `resolveType` would collapse `TypeNode` → `ResolvedNode{}` (no
label set), forfeiting the schema witness R0/R1 already committed.
Same shape for `TypeEdge`.

### 4.6 `useWitness` — the per-Use witness computer

Signature:

```go
func useWitness(
    u query.Use,
    nodeTypes map[string]schema.NodeType,
    edgeTypes map[string]schema.EdgeType,
) (ResolvedType, error)
```

Dispatch on the sealed `Use` sum (three variants —
`internal/query/query.go:1275-1285`):

- **`PropertyUse`** — look up `u.Ref().Property` on
  `nodeTypes[u.Ref().Variable]` or
  `edgeTypes[u.Ref().Variable]`; miss →
  `ErrUnknownProperty`; hit → `ResolvedProperty{Type, Nullable}`. This
  is the R0/R1 property-parameter path lifted verbatim (R1 §4.6). A
  ref naming no admitted binding is impossible at R2's kernel entry —
  the parser rejects unbound variables at build time (R0 §5), and Phase
  A/B/C either committed the binding or already failed.
- **`ClauseSlotUse`** — return `ResolvedScalar{ScalarInt}`. SKIP and
  LIMIT expect an integer; parameter unification pins the parameter's
  type to `int` and any concurrent `PropertyUse` witness that disagrees
  fails with `ErrParameterTypeConflict`. No `Slot()` discrimination
  needed — both `ClauseSlotSkip` and `ClauseSlotLimit` are integer
  slots (`internal/query/query.go:1313-1321`). If a future clause slot
  admits a non-integer, this handler dispatches on `u.Slot()` — an
  additive R-later change.
- **`ExprUse`** — dispatch on `u.Position()` (four positions —
  `internal/query/query.go:1352-1391`):
  - `ExprInProjection` or `ExprInPredicate` (read-side): return
    `resolveType(u.EnclosingType())`. Parser posture: the enclosing
    type is `TypeBool` for a predicate the typer committed to,
    `TypeUnknown` for a shape the typer could not commit
    (`internal/query/cypher/expr.go:444-459`), or the projection's
    Stage-6 result type for a projection-position parameter. Every
    case maps into `ResolvedType` via §4.7.
  - `ExprInSetValue` or `ExprInDeleteTarget` (write-side): return
    `ErrOutOfR0Scope` — writes are R6's business, and the parameter
    witness cannot be honestly computed until the write's target
    property is committed.

The dispatch is exhaustive over the sealed `Use` sum (three variants,
all covered). A parser-side additive `Use` variant post-freeze would be
a compile error at the switch default — a stage-later specification
picks it up.

### 4.7 `resolveType` — the closed R0 §4 mapping table

Signature and behaviour:

```go
// resolveType maps a parser Type into its resolver ResolvedType, per
// the R0 §4 mapping table (updated at R2 to include ResolvedScalar /
// ResolvedTemporal / ResolvedList / ResolvedUnknown rows). Total,
// deterministic. TypeNode / TypeEdge do not appear here — the schema
// witness lives on the binding, not on the projection's Type(), and
// callers of resolveType are the non-Ref projection dispatch and the
// ExprUse handler, neither of which reaches TypeNode/TypeEdge (they
// come only from RefProjection.Type() at R0/R1, handled elsewhere).
// TypePath is R5's business; if reached at R2, panic (unreachable —
// parser stages 6/7 do not produce path outside a PathBinding, and R2
// does not admit path bindings).
func resolveType(t query.Type) ResolvedType
```

The switch table:

| `query.Type` | `ResolvedType` returned | Row from §4.10 |
|---|---|---|
| `TypeBool{}` | `ResolvedScalar{ScalarBool}` | 4.10 bool |
| `TypeInt{}` | `ResolvedScalar{ScalarInt}` | 4.10 int |
| `TypeFloat{}` | `ResolvedScalar{ScalarFloat}` | 4.10 float |
| `TypeString{}` | `ResolvedScalar{ScalarString}` | 4.10 string |
| `TypeNull{}` | `ResolvedScalar{ScalarNull}` | 4.10 null |
| `TypeMap{}` | `ResolvedScalar{ScalarMap}` | 4.10 map |
| `TypeDate{}` | `ResolvedTemporal{TemporalDate}` | 4.10 date |
| `TypeTime{}` | `ResolvedTemporal{TemporalTime}` | 4.10 time |
| `TypeLocalTime{}` | `ResolvedTemporal{TemporalLocalTime}` | 4.10 localtime |
| `TypeDateTime{}` | `ResolvedTemporal{TemporalDateTime}` | 4.10 datetime |
| `TypeLocalDateTime{}` | `ResolvedTemporal{TemporalLocalDateTime}` | 4.10 localdatetime |
| `TypeDuration{}` | `ResolvedTemporal{TemporalDuration}` | 4.10 duration |
| `TypeList{element}` | `ResolvedList{Element: resolveType(element)}` | 4.10 list |
| `TypeUnknown{}` | `ResolvedUnknown{}` | 4.10 unknown (other) |
| `TypeNode{}` | panic (not reached — RefProjection path) | 4.10 node (R0) |
| `TypeEdge{}` | panic (not reached — RefProjection path) | 4.10 edge (R1) |
| `TypePath{}` | panic (not reached — R5) | 4.10 path (R5) |

The three panic arms are unreachable-by-construction at R2 (see §4.5's
`RefProjection` note); a panic reaching production would be a resolver
bug. The panic is the right posture — a returned zero value would
silently hide the bug, and returning an error would force every caller
site (four) to handle an impossible error branch. R5's spec revises the
`TypePath` arm when path bindings are admitted; R1 already covered
`TypeEdge` via the `RefProjection` path.

### 4.8 `unify` — the R2 unification lattice

Signature:

```go
// unify agrees two ResolvedTypes iff they are structurally equal or one
// side is ResolvedUnknown (the resolver's honest bottom — any concrete
// witness dominates it). Returns the agreed type on success,
// (nil, false) on conflict. Total, deterministic, symmetric,
// associative modulo the ResolvedUnknown-absorbs identity.
func unify(a, b ResolvedType) (ResolvedType, bool)
```

The rules:

1. **`ResolvedUnknown` is the bottom.** `unify(ResolvedUnknown{}, x) =
   (x, true)` and `unify(x, ResolvedUnknown{}) = (x, true)` for every
   `x`. A parameter with two Uses whose witnesses are
   `ResolvedUnknown` and `ResolvedProperty{INT, false}` unifies at
   `ResolvedProperty{INT, false}` — the concrete witness wins. This
   matches the R0 §4 mapping table's `unified-across-uses` row (the
   parser's honest `unknown` upgrades from any concrete witness).
2. **Otherwise, structural equality.** Two `ResolvedProperty`s agree
   iff both `Type` and `Nullable` match. Two `ResolvedScalar`s agree
   iff `Kind` matches. Two `ResolvedTemporal`s agree iff `Kind`
   matches. Two `ResolvedList`s agree iff `Element` recursively
   unifies (returning the element-unified `ResolvedList`). Two
   `ResolvedNode`s agree iff `Labels` matches. Two `ResolvedEdge`s
   agree iff `EdgeKey` matches.
3. **Otherwise, conflict.** `(nil, false)`.

**Judgment call — asymmetric nullability.** `ResolvedProperty{INT,
false}` and `ResolvedProperty{INT, true}` do *not* unify at R2. The
Nullable bit is a first-class part of the resolved type per R0 §3.4;
collapsing on disagreement would lose information the schema declared.
A future ADR may relax the rule (e.g. lattice-based nullability where
`nullable=true` dominates), but ADR 0009 places nullability *flow-typing*
at R4 — R2 keeps nullability strict-equality. This means a parameter
used against two properties with different nullability signals a
schema-shape conflict, which is the honest resolver posture: the caller
must decide which the parameter really models.

**Judgment call — SKIP and LIMIT integer families.** `ClauseSlotUse`
contributes `ResolvedScalar{ScalarInt}`. A parameter that appears both
in `SKIP $n` and against a schema `INT32` property produces witnesses
`ResolvedScalar{ScalarInt}` and `ResolvedProperty{INT32, false}` — two
distinct variants. R2's lattice does not fold `ResolvedScalar{ScalarInt}`
into `ResolvedProperty{INT/…, _}` because the coarse scalar has no bit
width, the schema does, and preserving ADR 0002 forbids the collapse.
This is a genuine `ErrParameterTypeConflict` — the parameter cannot be
both an untyped integer and an `INT32` column. A future R-later stage
may open a numeric assignability lattice (the ADR 0007 CALL / NUMBER
note is a comparable case, deferred to R7); R2 stays strict.

### 4.9 Determinism and short-circuit

Determinism reads from parser guarantees:
- `Query.Parameters` in first-appearance order
  (`internal/query/query.go:79` and the R1 spec §6.5 sweep).
- `Parameter.Uses` in slot-appearance order (mining walks in listener
  order — `internal/query/cypher/expr.go` and neighbours).
- Projection walk in `Part.Returns` order.

Short-circuit: the first `ErrParameterTypeConflict` fails resolution
(R0 §2.3 posture). No partial `ValidatedQuery` is returned. The
short-circuit is deterministic given the deterministic Uses order.

### 4.10 The revised type-mapping table

R0 §4.6's twenty-row spine is the reference. R2 revises the owner
column for the twelve rows R0/R1 marked `R2`, converts the R0
speculative row for `ResolvedScalar{ScalarInt}` into the concrete
`ClauseSlotUse` witness path (§3.1), and adds one clarifying note on the
`TypeUnknown (other)` row.

| Variant | Resolver counterpart | Classification | Owner (before R2) | Owner (R2) |
|---|---|---|---|---|
| `TypeBool` | `ResolvedScalar{ScalarBool}` | final | R2 | **R2** |
| `TypeInt` | `ResolvedScalar{ScalarInt}` | final | R2 | **R2** |
| `TypeFloat` | `ResolvedScalar{ScalarFloat}` | final | R2 | **R2** |
| `TypeString` | `ResolvedScalar{ScalarString}` | final | R2 | **R2** |
| `TypeNull` | `ResolvedScalar{ScalarNull}` | final | R2 | **R2** |
| `TypeMap` | `ResolvedScalar{ScalarMap}` | final | R2 | **R2** |
| `TypeNode` | `ResolvedNode{Labels}` | schema-upgraded | R0 | R0 (unchanged) |
| `TypeEdge` | `ResolvedEdge{EdgeKey}` | schema-upgraded | R1 | R1 (unchanged) |
| `TypeList` | `ResolvedList{Element}` | schema-upgraded | R2/R3 | **R2 (literal); R3 widens** |
| `TypeUnknown` (prop projection) | `ResolvedProperty{...}` | schema-upgraded | R0 | R0 (unchanged) |
| `TypeUnknown` (prop parameter) | `ResolvedProperty{...}` | schema-upgraded | R0 | R0 (unchanged) |
| `TypeUnknown` (list element) | via list-row recursion | schema-upgraded | R2 | **R2** (as `ResolvedUnknown` element when literal; R3 for var-length `list<edge>`) |
| `TypeUnknown` (function result) | `ResolvedUnknown{}` | schema-upgraded (honest posture) | R2 | **R2** |
| `TypeUnknown` (expr residue) | `ResolvedUnknown{}` | schema-upgraded (honest posture) | R2 | **R2** |
| `TypeUnknown` (aggregate result) | (n/a — refused) | schema-upgraded | R6/R7 | R5 (grouping) |
| `TypeDate` | `ResolvedTemporal{TemporalDate}` | final | R2 | **R2** |
| `TypeTime` | `ResolvedTemporal{TemporalTime}` | final | R2 | **R2** |
| `TypeLocalTime` | `ResolvedTemporal{TemporalLocalTime}` | final | R2 | **R2** |
| `TypeDateTime` | `ResolvedTemporal{TemporalDateTime}` | final | R2 | **R2** |
| `TypeLocalDateTime` | `ResolvedTemporal{TemporalLocalDateTime}` | final | R2 | **R2** |
| `TypeDuration` | `ResolvedTemporal{TemporalDuration}` | final | R2 | **R2** |
| `TypePath` | `ResolvedPath{}` (empty) | final | R5 | R5 (unchanged) |

**Row changes vs R0 §4.6:**

- The `TypeUnknown (list element)` row's classification prose is
  tightened: R2 owns the *literal-list* case (element resolves via
  §4.7's recursion, yielding `ResolvedUnknown` when the parser's
  element is `TypeUnknown`); R3 owns the *var-length hop-range* case
  (`list<edge>`).
- The `TypeUnknown (other)` row of R0 §4.3 is split into two sub-rows
  above (function result, expr residue), both R2-owned. The
  aggregate-result sub-row moves from R6/R7 to R5, aligning with the
  R5 grouping-key work — aggregate results appear in a projection
  only via an `AggregateProjection`, which R5 admits.
- The six temporal rows and six scalar rows keep their R2 owner —
  R0 recorded them in advance; R2 is where the concrete map lands.
- `ResolvedScalar{ScalarInt}` gains a second producer: R0's spec
  named it as the literal `TypeInt` counterpart; R2 also emits it from
  a `ClauseSlotUse` witness. The counterpart column is unchanged; the
  producer axis just widens.

The table is closed at R2: every variant of the frozen `query.Type` sum
appears, each classified, each with an R-stage owner. R3–R7 revise
rows they take up (R3 for var-length `list<edge>`, R5 for path and
aggregate result); no row is renamed or reclassified silently.

---

## 5. Sentinels — the R2 revision

R0/R1's five sentinels stand. R2 adds one and revises the message-set
of two. The `allSentinels` list gains the new member; the
`invalidFixtures` map gains rows for the new sentinel and the R2 sub-cases
of `ErrOutOfR0Scope`; the reachability sweep extends transparently.

### 5.1 New sentinel

```go
// ErrParameterTypeConflict is returned when a parameter's Uses carry
// witnesses that do not unify: a mixed-property parameter whose two
// PropertyUses point at properties of different types or nullability;
// a parameter that appears both in a SKIP/LIMIT slot and against a
// non-integer property; a rich-expression predicate use whose enclosing
// type disagrees with a co-occurring property use. Introduced at R2.
// See §4.8 for the unification lattice.
var ErrParameterTypeConflict = errors.New("parameter type conflict")
```

**Naming defence — `ErrParameterTypeConflict`, not
`ErrParameterUnificationFailed` or `ErrParameterType`.** ADR 0009 names
this sentinel in prose (`ErrParameterTypeConflict` — the "conflict"
sentinel). The name reads as "the parameter's type is in conflict",
matching how a compiler names a type-check failure ("type conflict",
"type mismatch"). `ErrParameterUnificationFailed` imports the
resolver's internal algorithm name into the sentinel — a leak the R0
§5 rationale rejects. `ErrParameterType` is a superset ("the
parameter's type is wrong"), which the other sentinels
(`ErrUnknownProperty` on a use miss) already cover. The R2 addition is
specifically "two Uses disagreed on the type", so `Conflict` is the
right suffix.

### 5.2 Revised sentinels

- **`ErrUnknownProperty`.** Prose gains "or an inline-map parameter use
  or a rich-expression predicate use" wording — the miss set widens
  from R0/R1's projection-or-inline-map-parameter to R2's projection-
  or-any-`PropertyUse`. The sentinel identity and fail-message wrapping
  (`fmt.Errorf("%w: %s.%s", ...)`) are unchanged; goldens continue to
  match on `errors.Is`.
- **`ErrOutOfR0Scope`.** Prose is revised to reflect the R2 retirements
  (multi-use parameters no longer route here; `ClauseSlotUse` no
  longer routes here; read-side `ExprUse` no longer routes here;
  non-`RefProjection` items except `AggregateProjection` no longer
  route here) and the R2 add (`AggregateProjection` explicitly).
  Sentinel name is unchanged (R0 §5's retirement plan holds).

### 5.3 Not added at R2

- **`ErrUnboundVariable`.** Parser rejects at build time (R0 §5).
- **`ErrNumericAssignability`** (a hypothetical R7 sentinel per ADR
  0007's Stage-14 NUMBER note). Not R2's business.
- **A dedicated `ErrUnsupportedProjection`** for `AggregateProjection`.
  R0 §5 chose the category-grained posture — `ErrOutOfR0Scope`
  covers the "R-later owns this construct" surface — and R2 does not
  revise that choice. The fail-message specifies "aggregate
  projection".

### 5.4 The closed R2 set

```go
var allSentinels = []error{
    ErrUnknownLabel,           // R0; unchanged at R2
    ErrUnknownProperty,        // R0; message-set widens at R2 to include ExprUse property-witness lookups
    ErrOutOfR0Scope,           // R0; sub-cases shift at R2 (see §5.2)
    ErrUnknownEdge,            // R1; unchanged at R2
    ErrAmbiguousBinding,       // R1; unchanged at R2
    ErrParameterTypeConflict,  // R2
}
```

Six sentinels. Every member has at least one fixture (§6.4); every
fixture maps to a canonical sentinel. Bidirectional sweep unchanged.

---

## 6. The golden-pair harness — R2 revision

R0 §6 and R1 §6's harness stand: the `test/data/resolver/{valid,invalid}`
layout, the `-update` flag, the invalid-fixture map, the reachability
sweep, the schema-mapping totality. R2 revises the fixture set only, not
the harness code.

### 6.1 Schema fixture strategy — one new schema; R0/R1 untouched

The R2 valid schema (`social_r2.gql`) is a proper superset of R1's:
- The R1 `Person`, `Post`, `AUTHORED`, `LIKES` shapes unchanged (so R1
  fixtures could theoretically be repointed here without semantic
  change; R2 does not repoint them — churn cost outweighs benefit).
- Two extra properties on `Person`: `nickname :: STRING` (nullable —
  for the `ExprInPredicate` parameter witness against a nullable
  string) and `score :: FLOAT NOT NULL` (for the mixed-property
  parameter-unification conflict fixture, §6.4).
- One extra property on `AUTHORED`: `views :: INT NOT NULL` (for the
  ClauseSlotUse × PropertyUse conflict fixture, §6.4).

The R0 `social.gql` and R1 `social_r1.gql` stay verbatim. R1 valid
fixtures continue to point at `social_r1.gql` via
`schema.mapping.json`; R2 fixtures point at `social_r2.gql`.

The R2 invalid schemas reuse the R1 invalid `social.gql` (Person + Post
+ AUTHORED — R1's kept-as-authored invalid schema) unless a specific
fixture needs a shape only `social_r2.gql` carries; those fixtures
point at `social_r2.gql` in the invalid corpus (added there as
`invalid/schemas/social_r2.gql`, a byte-copy of the valid one). No new
"structural" invalid schema is needed at R2 — the R2 conflict
sentinels arise from *query* shapes against the same schema
(mixed-property parameter, SKIP against a non-int property), not from
schema mis-shape.

### 6.2 Schema fixture text

`test/data/resolver/valid/schemas/social_r2.gql`:

```gql
CREATE PROPERTY GRAPH TYPE SocialR2 AS {
    (:Person {
        id       :: INT NOT NULL,
        name     :: STRING NOT NULL,
        age      :: INT,
        nickname :: STRING,
        score    :: FLOAT NOT NULL
    }),
    (:Post {
        id       :: INT NOT NULL,
        title    :: STRING NOT NULL,
        body     :: STRING
    }),
    (:Person) -[:AUTHORED { publishedAt :: TIMESTAMP, views :: INT NOT NULL }]-> (:Post),
    (:Person) -[:LIKES]-> (:Post)
}
```

Add `invalid/schemas/social_r2.gql` as an exact byte copy so the invalid
fixtures that need the R2-specific shape can share it.

### 6.3 R2 valid fixtures

Added under `test/data/resolver/valid/`. Each fixture is one Cypher
file; each has one paired `.validated.golden.json` regenerated by
`-update`. `schema.mapping.json` grows one row per fixture pointing at
`social_r2.gql`.

| Fixture | Shape | Purpose |
|---|---|---|
| `literal_int_projection.cypher` | `MATCH (p:Person) RETURN 42 AS answer` | `LiteralProjection{TypeInt}` → `ResolvedScalar{ScalarInt}` |
| `literal_string_projection.cypher` | `MATCH (p:Person) RETURN 'hi' AS greeting` | `LiteralProjection{TypeString}` → `ResolvedScalar{ScalarString}` |
| `expr_projection_list.cypher` | `MATCH (p:Person) RETURN [1, 2, 3] AS xs` | `ExprProjection{TypeList<TypeInt>}` → `ResolvedList{ResolvedScalar{ScalarInt}}`. **Parser routing note:** list literals are not scalar, so `classifyProjection` refuses them (`internal/query/cypher/expr.go:254-258`, gated by `isScalarLiteral`); they fall through to `classifyRichExpression` and land in `ExprProjection`. The R2 projection walk resolves them via §4.5's `ExprProjection` arm, so the golden's `kind` is `list` with element `scalar` — same `ResolvedType` shape, different `Projection` producer. |
| `func_projection_unknown.cypher` | `MATCH (p:Person) RETURN toString(p.age) AS s` | `FuncProjection{TypeUnknown}` → `ResolvedUnknown{}` (honest posture, §3.4) |
| `expr_projection_bool.cypher` | `MATCH (p:Person) RETURN p.age > 18 AS is_adult` | `ExprProjection{TypeBool}` → `ResolvedScalar{ScalarBool}` |
| `expr_projection_unknown.cypher` | `MATCH (p:Person) RETURN p.age + 1 AS bumped` | `ExprProjection{TypeUnknown}` → `ResolvedUnknown{}` (property-participating arithmetic) |
| `parameter_two_property_uses_agree.cypher` | `MATCH (p:Person), (q:Person) WHERE p.age = $threshold AND q.age = $threshold RETURN p.name` | two `PropertyUse{age}` witnesses agree → `ResolvedProperty{INT, true}` |
| `parameter_clause_slot_skip.cypher` | `MATCH (p:Person) RETURN p.name SKIP $offset` | one `ClauseSlotUse{Skip}` → `ResolvedScalar{ScalarInt}` |
| `parameter_clause_slot_limit.cypher` | `MATCH (p:Person) RETURN p.name LIMIT $n` | one `ClauseSlotUse{Limit}` → `ResolvedScalar{ScalarInt}` |
| `parameter_expr_predicate.cypher` | `MATCH (p:Person) WHERE p.age > 0 AND $flag RETURN p.name` | one `ExprUse{TypeBool, Predicate}` → `ResolvedScalar{ScalarBool}` |
| `parameter_expr_and_property.cypher` | `MATCH (p:Person) WHERE p.age = $x AND $x > 0 RETURN p.name` | `PropertyUse{age}` (`ResolvedProperty{INT, true}`) + `ExprUse{TypeBool, Predicate}` (`ResolvedScalar{ScalarBool}`); this is a **conflict** — see §6.4 invalid, and this row moves to §6.4. **Deleted from valid.** |
| `parameter_property_and_unknown_expr.cypher` | `MATCH (p:Person) WHERE p.age = $x RETURN p.age + $x AS bumped` | `PropertyUse{age}` (`ResolvedProperty{INT, true}`) + `ExprUse{TypeUnknown, Projection}` (`ResolvedUnknown{}` — property-participating arithmetic types as `TypeUnknown` per parser Stage 6); unifies at `ResolvedProperty{INT, true}` (bottom-absorbs, §4.8). **Parser routing note:** the RETURN projection `p.age + $x` is an `ExprProjection{TypeUnknown}` (§4.5's ExprProjection arm resolves the column to `ResolvedUnknown`); the `$x` inside it is queued for `ExprUse{TypeUnknown, ExprInProjection}` because Stage-6's rich typer refuses to commit on property-participating arithmetic (`internal/query/cypher/typing.go` addition/subtraction lines). |

**Erratum — the `parameter_expr_and_property.cypher` row above is an
invalid fixture, not a valid one.** The row is preserved as a
documentation trail: `PropertyUse{INT}` vs `ExprUse{TypeBool}` unifies
`ResolvedProperty{INT, true}` against `ResolvedScalar{ScalarBool}`,
which are not structurally equal and neither is `ResolvedUnknown`, so
the unify lattice returns conflict (§4.8). The fixture belongs in
§6.4 as `parameter_type_conflict_property_vs_expr_bool.cypher`.
Removed from the valid list.

**Coverage sketch (one line per row, keyed to the algorithm):**

- `literal_int_projection` / `literal_string_projection` — exercise
  §4.5's `LiteralProjection` handler and §4.7's mapping table for
  `TypeInt` / `TypeString` (the scalar-literal path,
  `internal/query/cypher/expr.go:254-258`).
- `expr_projection_list` — exercise §4.5's `ExprProjection` handler
  on a list-literal residue and §4.7's `TypeList` row.
- `func_projection_unknown` — exercise §4.5's `FuncProjection` handler
  and the `TypeUnknown → ResolvedUnknown` row (§4.7).
- `expr_projection_bool` — exercise §4.5's `ExprProjection` handler
  with a parser-committed concrete type.
- `expr_projection_unknown` — exercise §4.5's `ExprProjection` handler
  with the parser's `TypeUnknown` residue.
- `parameter_two_property_uses_agree` — exercise §4.6's `PropertyUse`
  witness and §4.8's structural-equality unification.
- `parameter_clause_slot_*` — exercise §4.6's `ClauseSlotUse` witness
  and §4.7's `ResolvedScalar{ScalarInt}` row for both slot values.
- `parameter_expr_predicate` — exercise §4.6's `ExprInPredicate` arm
  and §4.7's `TypeBool → ResolvedScalar{ScalarBool}` row.
- `parameter_property_and_unknown_expr` — exercise §4.8's
  `ResolvedUnknown` bottom-absorbs rule (a real, non-conflicting
  mixed-Use case).

### 6.4 R2 invalid fixtures — updated `invalidFixtures` map

The R0/R1 map's rows stand where they refer to R2-still-out-of-scope
constructs (WITH, UNION, DISTINCT, RETURNS-ALL, undirected /
var-length / multi-type edges, unknown labels/properties/edges,
ambiguous bindings, empty inline endpoints). R2 adds new rows for the
new sentinel and for R2-remainder `ErrOutOfR0Scope` sub-cases.

```go
var invalidFixtures = map[string]error{
    // R0/R1 rows carried forward
    "unknown_label.cypher":                ErrUnknownLabel,
    "unknown_property.cypher":             ErrUnknownProperty,
    "with_clause.cypher":                  ErrOutOfR0Scope,
    "aggregate_projection.cypher":         ErrOutOfR0Scope,
    "return_distinct.cypher":              ErrOutOfR0Scope,
    "returns_all.cypher":                  ErrOutOfR0Scope,
    "unknown_edge.cypher":                 ErrUnknownEdge,
    "unknown_edge_property.cypher":        ErrUnknownProperty,
    "ambiguous_unlabelled_binding.cypher": ErrAmbiguousBinding,
    "unlabelled_binding_no_edge.cypher":   ErrUnknownLabel,
    "empty_inline_endpoint.cypher":        ErrUnknownLabel,
    "undirected_edge.cypher":              ErrOutOfR0Scope,
    "var_length_edge.cypher":              ErrOutOfR0Scope,
    "multi_type_edge.cypher":              ErrOutOfR0Scope,

    // R2 new rows
    "parameter_type_conflict_two_properties.cypher":         ErrParameterTypeConflict,
    "parameter_type_conflict_clause_slot_vs_string.cypher":  ErrParameterTypeConflict,
    "parameter_type_conflict_property_vs_expr_bool.cypher":  ErrParameterTypeConflict,
    "parameter_type_conflict_nullability.cypher":            ErrParameterTypeConflict,
    "unknown_property_via_expr_use.cypher":                  ErrUnknownProperty,
    "expr_use_set_value.cypher":                             ErrOutOfR0Scope,
}
```

**R2 invalid fixture contents:**

- `parameter_type_conflict_two_properties.cypher`:
  `MATCH (p:Person) WHERE p.name = $x AND p.age = $x RETURN p.name` —
  `PropertyUse{name}` yields `ResolvedProperty{STRING, false}`;
  `PropertyUse{age}` yields `ResolvedProperty{INT, true}`; §4.8
  disagrees on both `Type` and `Nullable`.
- `parameter_type_conflict_clause_slot_vs_string.cypher`:
  `MATCH (p:Person) WHERE p.name = $x RETURN p.name SKIP $x` —
  `PropertyUse{name}` (`STRING`) vs `ClauseSlotUse{Skip}`
  (`ResolvedScalar{ScalarInt}`); §4.8 conflict.
- `parameter_type_conflict_property_vs_expr_bool.cypher`:
  `MATCH (p:Person) WHERE p.age = $x AND $x RETURN p.name` —
  `PropertyUse{age}` (`ResolvedProperty{INT, true}`) vs `ExprUse` in
  a WHERE conjunct whose enclosing type is `TypeBool` (`ResolvedScalar
  {ScalarBool}`); §4.8 conflict. Depending on the parser's `$x`
  atom-vs-conjunct handling, the `ExprUse` may register only when the
  conjunct-wide typer runs (see `internal/query/cypher/expr.go:444-459`);
  the golden is regenerated to whatever the parser emits, and the
  test's assertion is `errors.Is(err, ErrParameterTypeConflict)` —
  the resolver's job is to reject *when* the parser gives it two
  disagreeing witnesses, not to fabricate them.
- `parameter_type_conflict_nullability.cypher`:
  `MATCH (p:Person), (q:Person) WHERE p.name = $x AND q.nickname = $x
   RETURN p.name` — `p.name` (`ResolvedProperty{STRING, false}`) vs
  `q.nickname` (`ResolvedProperty{STRING, true}`); §4.8's strict-Nullable
  rule fires. Schema: `social_r2.gql`.
- `unknown_property_via_expr_use.cypher`:
  `MATCH (p:Person) WHERE p.doesnt_exist = $x RETURN p.name` — the
  `PropertyUse` at `p.doesnt_exist` misses in `useWitness` (§4.6),
  emitting `ErrUnknownProperty`. Exercises the message-set widening
  (§5.2).
- `expr_use_set_value.cypher` — a SET-clause fixture:
  `MATCH (p:Person) SET p.nickname = $x RETURN p.name`. Parser mines a
  `Use{ExprInSetValue}`. §4.6's write-side arm returns
  `ErrOutOfR0Scope`. Note: the outer-scope SET also flips
  `StatementKind` to `write`; the R1 kernel's step-1 write gate
  (`len(part.Effects) != 0` — R1 §4.1) fires first and returns
  `ErrOutOfR0Scope` for `write clause`. The fixture therefore tests
  the write-clause gate, not the write-side ExprUse arm — but its
  sentinel is `ErrOutOfR0Scope` either way. A pure write-side
  `ExprUse` fixture without a SET clause is unreachable (the parser
  only emits `ExprInSetValue` inside a SET; and a SET drives the
  write gate). The fixture is kept as a reachability witness that
  write-clause routes to `ErrOutOfR0Scope`; the ExprUse arm's
  reachability is architectural, not exercised by a fixture. See
  §6.5's note.

Each fixture is paired to its schema via `invalid/schema.mapping.json`,
extended to include the new fixtures. The
`parameter_type_conflict_nullability.cypher` fixture uses
`social_r2.gql`; the others use `social.gql` (which already carries
Person / Post / AUTHORED). If a fixture needs a property not on
`social.gql` (e.g. `p.nickname`), it points at `social_r2.gql`.

### 6.5 The unreachable-write-side-ExprUse note

R2's `useWitness` §4.6 has a write-side arm (`ExprInSetValue` /
`ExprInDeleteTarget`) that returns `ErrOutOfR0Scope`. As §6.4 notes,
the arm is unreachable through a fixture because the outer-scope write
gate fires first. This is a deliberate belt-and-braces posture: the
resolver's kernel does not depend on the write gate for parameter
gating, so a future R6 that removes the write gate (writes become
resolvable) but does not yet type SET values still has a clean
`ErrOutOfR0Scope` posture for the `ExprInSetValue` arm.

`TestSentinelReachability` is unaffected — the arm doesn't have a
fixture, but it doesn't need one: `ErrOutOfR0Scope` already has
fixtures (the write-clause one, the aggregate one, and others).
Sentinel reachability is a coverage sweep over the *sentinel* set,
not over every fail-site.

### 6.6 Determinism check

The R2 kernel's iteration order is:
- `Part.Bindings` in first-appearance order (R1 §4.2 note).
- `Part.Returns` in source order (R0 §2.3).
- `Query.Parameters` in first-appearance order (R0 §2.3, R1 §6.5).
- `Parameter.Uses` in parser-mining order (§4.9).
- Unification fold left-to-right (§4.3).

Every ordered surface is either the parser's guaranteed order or a
left-fold — no map iteration escapes into the output. The golden JSON
is deterministic; `-update` regenerates a byte-stable file. The
short-circuit reports the first conflict in Uses order (deterministic
given the parser's guarantee).

### 6.7 Non-obvious harness invariants — R2 additions

R0 §6.6 and R1 §6.6 invariants stand. R2 adds:

- **Fixture-to-schema pairing is many-to-one and stable across R
  stages.** R0 fixtures point at `social.gql`; R1 at `social_r1.gql`;
  R2 at `social_r2.gql`. R2 does *not* repoint R0/R1 fixtures at
  `social_r2.gql` — the goldens would rebaseline (property lookups
  against the new node-type instance would produce byte-identical
  goldens on the shared columns, but the harness assumes the goldens
  match exactly, and a repoint is a wire-shape claim we'd have to
  audit). The churn cost is not worth the schema-file consolidation.
- **The `ExprUse` fixture text depends on parser mining.** The parser
  mines an `ExprUse` at whichever slot its rich-expression typer
  identifies — an `AND`-conjunct predicate at a WHERE clause admits
  the whole predicate as an `ExprInPredicate` position
  (`internal/query/cypher/expr.go:444-459`). Fixture authors write the
  query, run `go test -update`, inspect the golden, and confirm the
  parameter's `Uses` axis matches the fixture's stated intent. If the
  parser's `Use` mining changes (e.g. a Stage-6 refinement narrows
  the ExprInPredicate arm), the golden regenerates and the fixture's
  intent may need a message-only revision. The harness enforces
  totality; the fixture's *shape* is the parser's business.

---

## 7. R2 capability scope — what resolves

**In scope:** a single Cypher query the parser accepts, whose parsed
`query.Query` shape is:

- Exactly one `Branch` in `Branches`; zero `Combinators`.
- Exactly one `Part` in the branch's `Parts`.
- The part's `Bindings` are a non-empty slice of `NodeBinding` and/or
  `EdgeBinding` values admitted by R1 (labelled or Phase-B-inferable
  nodes; directed single-hop single-type edges; anonymous edges with
  labelled endpoints).
- The part's `Returns` is a non-empty slice of `ReturnItem`s. Each
  `ReturnItem.Value` is one of:
  - `RefProjection` (R0/R1 shape, unchanged);
  - `LiteralProjection` (new at R2);
  - `FuncProjection` (new at R2);
  - `ExprProjection` (new at R2).
  `AggregateProjection` is out — R5 owns.
- `ReturnsAll` is false; `Distinct` is false; `Effects` is empty.
- `Parameters` is a slice of `Parameter`s, each with **one or more**
  `Use`s, each of which is one of:
  - `PropertyUse` (R0/R1 shape, unchanged);
  - `ClauseSlotUse` (new at R2, SKIP or LIMIT);
  - `ExprUse` at position `ExprInProjection` or `ExprInPredicate`
    (new at R2).
  Unification across Uses runs per §4.6/§4.8.
- `StatementKind` is `StatementRead`.

**Out of scope, routed to the appropriate sentinel:**

| Construct | Sentinel | R-stage owner |
|---|---|---|
| Undirected edge (`Directed() == false`) | `ErrOutOfR0Scope` | R3 |
| Multi-type edge (`len(Labels()) > 1`) | `ErrOutOfR0Scope` | R3 |
| Variable-length edge (`Hops() != nil`) | `ErrOutOfR0Scope` | R3 |
| Untyped edge (`len(Labels()) == 0`) | `ErrOutOfR0Scope` | R-later |
| Path binding | `ErrOutOfR0Scope` | R5 |
| Unwind binding | `ErrOutOfR0Scope` | R5 or later |
| Call binding | `ErrOutOfR0Scope` | R7 |
| `AggregateProjection` | `ErrOutOfR0Scope` | R5 |
| `ExprUse` at `ExprInSetValue` | `ErrOutOfR0Scope` | R6 |
| `ExprUse` at `ExprInDeleteTarget` | `ErrOutOfR0Scope` | R6 |
| Nullability upgrades (OPTIONAL MATCH regimes) | `ErrOutOfR0Scope` | R4 |
| `Part.Distinct == true` | `ErrOutOfR0Scope` | R5 |
| `Part.ReturnsAll == true` | `ErrOutOfR0Scope` | R5 |
| WITH carry-forward; UNION | `ErrOutOfR0Scope` | R5 |
| Writes / CREATE / MERGE / SET / REMOVE / DELETE | `ErrOutOfR0Scope` | R6 |
| CALL / YIELD | `ErrOutOfR0Scope` | R7 |
| Labelled edge with no matching schema EdgeKey | `ErrUnknownEdge` | R3 widens |
| Property lookup with no matching schema property (from any RefProjection or PropertyUse in projection, WHERE, or inline map) | `ErrUnknownProperty` | (R2 widens) |
| Labelled node with no matching schema NodeType | `ErrUnknownLabel` | (unchanged) |
| Unlabelled node with an empty candidate set from edges | `ErrUnknownLabel` | (unchanged) |
| Unlabelled node with a multi-candidate set | `ErrAmbiguousBinding` | (unchanged) |
| Parameter Uses that do not unify | `ErrParameterTypeConflict` | (new at R2) |

**Silently accepted (not routed anywhere):**

R0/R1's silently-accepted set stands unchanged. Literal-only WHERE /
ORDER BY continue to leave no witness in `query.Query`; ADR 0005
continues to say the original text runs. R2 does not extend the
silently-accepted set. A WHERE / ORDER BY that *does* carry a
parameter surfaces at R2 as a Use in the parameter walk (a
`ClauseSlotUse` for SKIP/LIMIT, an `ExprUse` for a WHERE conjunct);
R2 resolves it or fails with `ErrParameterTypeConflict`. The clause
itself is still architecturally invisible; the parameter is the
witness.

**Recorded ADR 0009 cross-check.** ADR 0009 R2: "property-type upgrade
of `unknown`; parameter unification across `Uses` (property,
clause-slot, expression) with a conflict sentinel." §4.5 (property-type
upgrade for non-Ref projections), §4.6/§4.8 (parameter unification
across the three Use kinds), and §5.1 (`ErrParameterTypeConflict`) are
that sentence unpacked. Nothing in §7 disagrees with ADR 0009 by
construction.

---

## 8. Ground-truth cross-check

Every factual claim in this spec is verifiable against source; the
citations below name the file:line the claim rests on. A future reader
should be able to open each citation and confirm the shape the spec
describes still holds.

- **Projection sum arity ×5** — `internal/query/query.go:1030-1036`;
  closed at RefProjection, LiteralProjection, FuncProjection,
  AggregateProjection, ExprProjection.
- **`FuncProjection.Type()` is `TypeUnknown` today** —
  `internal/query/query.go:1112-1114` (documented) and
  `internal/query/cypher/expr.go:175` (a listener path emitting an
  ExprUse with a TypeUnknown enclosing type from a function call
  residue); the parser has no signature registry for non-procedure
  functions (§3.4's judgment call).
- **`LiteralProjection.Type()` is the literal's scalar / list / map
  kind** — `internal/query/query.go:1073-1087`.
- **`ExprProjection.Type()` is the parser-computed rich-expression
  result type, `TypeUnknown` when uncomputable** —
  `internal/query/query.go:1171-1191`.
- **`AggregateProjection` is the aggregate case with `AggregateFunc`
  and `Distinct` axes** — `internal/query/query.go:1126-1160`.
- **Use sum arity ×3: `PropertyUse`, `ClauseSlotUse`, `ExprUse`** —
  `internal/query/query.go:1275-1285`.
- **`ClauseSlot` is `Skip` / `Limit` (integer slots)** —
  `internal/query/query.go:1310-1321`.
- **`ExprPosition` is `Projection` / `Predicate` / `SetValue` /
  `DeleteTarget`** — `internal/query/query.go:1352-1391`.
- **Parser emits `ExprInSetValue` at SET RHS** —
  `internal/query/cypher/expr.go:677`, `707`.
- **Parser emits `ExprInDeleteTarget` at DELETE target** —
  `internal/query/cypher/listener.go:499`.
- **Parser emits `ExprInPredicate` at WHERE conjunct rich-expression
  residue** — `internal/query/cypher/expr.go:458`,
  `internal/query/cypher/typing.go:445`, `458`.
- **Parser emits `ExprInProjection` at RETURN / WITH rich-expression
  residue** — `internal/query/cypher/expr.go:131`, `403`,
  `internal/query/cypher/typing.go:873`,
  `internal/query/cypher/call.go:64`.
- **Parser emits `ClauseSlotUse` at bare `$p` in SKIP / LIMIT** —
  `internal/query/cypher/expr.go:414-424`.
- **`graph.PropertyType` carries widths INT/INT8..INT256 etc.** —
  `internal/graph/propertytype.go:9-38` (ADR 0002).
- **`schema.Property` carries `Type` and `Nullable`** —
  `internal/schema/schema.go:43-47`.
- **`schema.NodeType.Properties` and `schema.EdgeType.Properties` are
  `map[string]Property`** — `internal/schema/schema.go:23-32`.
- **Frozen `query.Type` seventeen-variant sum** — ADR 0008,
  `internal/query/type.go` (all variants).
- **R1 kernel's parameter walk currently rejects
  `len(Uses) != 1` and non-`PropertyUse` with `ErrOutOfR0Scope`** —
  `internal/resolver/resolve.go:129-135` (R2 replaces these two
  rejections with §4.6's unification).
- **R1 kernel's projection walk currently rejects
  non-`RefProjection` with `ErrOutOfR0Scope`** —
  `internal/resolver/resolve.go:348-354` (R2 replaces this with §4.5's
  dispatch).
- **`Parameter.Uses` slot** — `internal/query/query.go:1270-1273`.
- **Cypher parser rejects unbound variables at build time** —
  `internal/query/cypher/build.go:157` (R0 §5's record).

---

## 9. Definition of done for R2 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is out
of scope of this document. The spec is done when:

1. This file exists at `docs/specs/resolver-stage-r2.md`, committed on
   branch `resolver-r2-spec`.
2. §3 names the four new `ResolvedType` variants (`ResolvedScalar`,
   `ResolvedTemporal`, `ResolvedList`, `ResolvedUnknown`) with wire
   encoding invariants and the schema-vs-expression rationale (§3.2
   temporal note; §3.4 unknown posture).
3. §4 gives the algorithm for both R2 capabilities: property-type
   upgrade of `unknown` for non-Ref projections (§4.5, §4.7) and
   parameter unification across the three Use kinds (§4.6, §4.8), with
   the mapping table's owner-column revisions (§4.10).
4. §5 names and defends the one new sentinel
   (`ErrParameterTypeConflict`) and revises the message-sets of the
   two sentinels that widen (`ErrUnknownProperty`, `ErrOutOfR0Scope`).
5. §6 designs the fixture set: the R2 valid schema `social_r2.gql`,
   the R2 valid fixture list, the R2 invalid fixture list, the
   revised `invalidFixtures` map, and the harness invariants.
6. §7 states the R2 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel each construct routes to
   and the R-stage that owns the next widening.
7. §8 cross-checks every factual claim against source file:line.
8. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer); every
blocker he raises is fixed on this same branch before the branch
merges. Cycle 2 (the R2 code cycle) begins only when the spec cycle
merges.
