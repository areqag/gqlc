# `query.Query` is frozen; the resolver API is pinned

ADR 0004's freeze gate is discharged. All fifteen parser stages (the read core
of Stages 0–5 plus the ADR 0007 expansion through Stage 14) and the TCK corpus
sweep are complete, and the two pre-freeze cardinality fixes — the Part-level
DISTINCT axis and aggregate-kind preservation over rich arguments — have
landed. `query.Query` is **frozen**: its Go API surface and its wire shape are
now the stable contract of ADR 0003, changed only under the additive-only
revision protocol below. This ADR records the frozen shape, pins the
resolver's package and API (discharging the deferral in ADR 0006's
consequences), and opens consumer work — resolver, codegen, generated driver —
that ADR 0004 gated on this moment.

## Context

The progress meter at the freeze: godog over the entire vendored TCK reports
**3897 scenarios — 3459 parse-green, 438 pending, 0 failed**. Every pending
scenario is skiplist-pinned to bucket 3 of ADR 0007 — runtime semantics below
the type-interface boundary (ADR 0005), where the parser accepts and the
driver raises on the original text. No scenario is pending for "not supported
yet": the `ErrUnsupported*` sentinels that carried that meaning through
Stages 0–14 are retired.

Two model fixes were pulled ahead of the freeze because they are
cardinality-critical for the resolver's grouping-key work, where an aggregate
column must be distinguishable from a plain expression column:

- **Part-level DISTINCT axis** (`Part.Distinct`): `RETURN DISTINCT` / `WITH
  DISTINCT` as a first-class bit, independent of the aggregate-input and
  UNION deduplication axes.
- **Aggregate kind over rich arguments**: `sum(x + 1)` lowers as an
  `AggregateProjection` with its `AggregateFunc` kind, refs, and DISTINCT
  preserved, instead of decaying to an `ExprProjection`. Nested aggregates
  inside a rich expression (`count(n) + 1`) remain `ExprProjection` — a
  documented deferral with a recorded resolver strategy (see the revision
  protocol below).

ADR 0004's economic argument now inverts: while the parser was the only
consumer, model churn was cheap; from here every change propagates through
the resolver, codegen, and generated driver code. The freeze is the point
where that cost flips, so the shape is locked against the corpus alone,
before any consumer exists.

## Decision

### The frozen shape

`query.Query` is the model as of this ADR — sealed sums over marker-method
interfaces, smart constructors that make illegal states unrepresentable, and
tagged-union JSON marshalling (the golden-file wire shape). The inventory:

**Top-level structure.** `Query` = `Branches []Branch` (UNION-joined arms,
always at least one) + `Combinators []UnionKind` (how each subsequent branch
joins its predecessor) + `Parameters []Parameter` (deduplicated query-wide in
first-appearance order) + `StatementKind` (the driver's transaction-mode
axis). `Branch` = `Parts []Part` (WITH-bounded scope segments). `Part` =
`Bindings` + `Returns []ReturnItem` + `ReturnsAll bool` + `Distinct bool` +
`Effects []Effect`, guarded by `NewPart`'s at-least-one-of invariant.

**Sealed sums** (variant counts are the frozen arity):

| Sum | Variants |
|---|---|
| `Binding` ×5 | `NodeBinding`, `EdgeBinding`, `PathBinding`, `UnwindBinding`, `CallBinding` |
| `PathMember` ×4 | `NamedNodeMember`, `NamedEdgeMember`, `AnonNodeMember`, `AnonEdgeMember` |
| `Endpoint` ×2 | `VarEndpoint`, `InlineEndpoint` |
| `Projection` ×5 | `RefProjection`, `LiteralProjection`, `FuncProjection`, `AggregateProjection`, `ExprProjection` |
| `Type` ×17 | `bool`, `int`, `float`, `string`, `null`, `map`, `node`, `edge`, `list<T>` (parameterised over an element `Type`), `unknown`, `date`, `time`, `localtime`, `datetime`, `localdatetime`, `duration`, `path` |
| `Use` ×3 | `PropertyUse`, `ExprUse`, `ClauseSlotUse` |
| `Effect` ×8 | `CreateEffect`, `DeleteEffect`, `SetPropertyEffect`, `SetEntityEffect`, `SetLabelsEffect`, `RemovePropertyEffect`, `RemoveLabelsEffect`, `MergeEffect` |
| `SetEffect` ×3 | the three `Set*Effect`s — the sub-sum `MergeEffect`'s `ON MATCH` / `ON CREATE` branches carry |

**Axes and enums:**

- `UnionKind` ×2 — `union` / `unionAll`.
- `StatementKind` ×2 — `read` / `write`; write iff any outer-scope write
  clause in any branch (a write inside `EXISTS { … }` does not flip it).
- `EdgeHops` — the variable-length hop range (`min`/`max`, either absent);
  negative bounds rejected at construction, empty ranges accepted.
- `directed` on `EdgeBinding` — canonical source→target order when set;
  textual order for the resolver's two-orientation trial when not.
- Three independent DISTINCT axes — `Part.Distinct` (projection body),
  `AggregateProjection.Distinct` (aggregate input), `UnionKind`
  (cross-branch); each is a different cardinality decision on a different
  model surface.
- `AggregateFunc` ×8 — `count`, `sum`, `collect`, `min`, `max`, `avg`,
  `stdev`, `percentile`.
- `ClauseSlot` ×2 — `skip`, `limit`.
- `ExprPosition` ×4 — `projection`, `predicate`, `setValue`, `deleteTarget`
  (the producer/consumer axis on `ExprUse`).
- `SetOp` ×2 — `replace` (`=`) / `merge` (`+=`) on `SetEntityEffect`.

**What "frozen" covers.** Both faces of the contract: the exported Go API of
`internal/query` (types, marker methods, constructors, accessors) and the
JSON wire shape the golden suite pins (tagged unions, always-emit
conventions, key names). A consumer may rely on either.

### The resolver API

ADR 0006 deferred the resolver's package path, constructor signature, and
output type to this ADR. Pinned:

- **Package `internal/resolver`** — a sibling of `internal/query` and
  `internal/schema`, importing both plus `internal/procsig`. None of the
  three import it back; the model packages stay consumer-free.
- **`resolver.New(s schema.Schema, opts ...Option) *Resolver`**, with
  **`WithRegistry(r procsig.Registry) Option`** supplying the procedure
  registry. The constructor binds the per-application compile-time inputs —
  the same functional-options shape as `cypher.New`.
- **`(*Resolver).Resolve(q query.Query) (ValidatedQuery, error)`** — a pure
  function of the constructor's inputs and its argument. No I/O, no state
  mutation; resolving the same query twice yields the same result.
- **`ValidatedQuery` lives in `internal/resolver`.** It is the resolver's
  output vocabulary, not the parser's: the parser must stay ignorant of
  schema-resolved types (ADR 0003's sibling rule), and codegen consumes the
  resolver's output, so the type belongs to the package that produces it.

A bare free function `Resolve(q, s)` — ADR 0003's original
`(query.Query, schema.Schema)` phrasing — was considered and rejected: the
procedure registry (ADR 0007) is a second compile-time input of the same
"user-authored, machine-read at generation time" kind as the schema, and
folding both into a constructor keeps the per-query call site one-argument
and gives future compile-time inputs a home that does not break every call
site. Purity is unaffected: `Resolve` remains a function of
`(query.Query, schema.Schema[, procsig.Registry])`, merely spelled as a
method.

The resolver's build approach — staging, test strategy, error posture,
`ValidatedQuery`'s internal vocabulary — is ADR 0009's subject, not this
one's. This ADR pins only the surface consumers see.

### Post-freeze revision protocol

- **Additive-only.** A revision may add a new variant to a sum, a new axis
  (field with a zero-value-compatible wire default), or a new enum value —
  never rename, remove, or re-type what exists. Each addition lands with a
  dated amendment note on this ADR (the ADR 0003 stage-note convention) and
  a golden rebaseline whose diff shows only the new surface.
- **Breaking changes require a superseding ADR** plus a migration plan for
  every consumer — the deliberately expensive path.
- **`apidiff` gate.** A CI check over `internal/query` (a `just` recipe and
  a CI step, per the recipe-parity convention) fails any PR whose exported
  API change is not compatible. Tracked as its own bead; until it lands,
  review carries the guarantee.

**Known deferred additions** — named here so they are recognised as
in-protocol when they arrive, not scope creep:

- **shortestPath selector axis** on `PathBinding` (see posture below).
- **`EXISTS { … }` Use precision** (gqlc-33k.3): parameters inside an
  existential subquery currently record coarse `ExprUse`s.
- **`CreateEffect` created-vs-prebound split** (gqlc-33k.4): deferred until
  a consumer demonstrates the need — no speculative modelling.
- **`ContainsAggregate` axis on `ExprProjection`** — the escape hatch
  recorded on gqlc-gyw. The committed strategy for grouping-key discovery
  over expression residuals (nested aggregates like `count(n) + 1`) is a
  resolver-side re-parse of the projection's original text span; the axis
  is added only if that proves untenable, and is never inferred from `Type`.

### shortestPath is a dialect extension, out of the frozen scope

`shortestPath()` / `allShortestPaths()` are Neo4j dialect extensions, not
openCypher: the vendored grammar (`internal/grammar/cypher/Cypher.g4`) has no
rule for either, and the vendored TCK contains zero occurrences. They are
therefore not a gap in the corpus sweep and not a freeze blocker. If dialect
support is taken up post-freeze, the expected shape is an additive selector
axis on `PathBinding` — the same move `EdgeHops` made on `EdgeBinding` —
landing under the revision protocol above.

## Consequences

- **Consumer work is open.** The resolver (ADR 0009), codegen, and the
  generated driver may now be written against a shape that will not move
  under them — the payoff ADR 0004 deferred them for.
- **ADR 0003's provisional header note is replaced** with a frozen note
  pointing here; its "stable contract" framing is now literally true. The
  stage-note diary on ADR 0003 remains as history. CONTEXT.md's **Resolver**
  and **Validated query** entries carry the pinned API.
- **Model mistakes are now expensive by design.** Anything the corpus did
  not force into the model arrives via the additive protocol or a
  superseding ADR; the freeze converts "revisit the model" from a cheap
  parser-local edit into a consumer-wide migration, which is exactly the
  pressure that keeps the contract stable.
- **The 438 pending scenarios are a stable posture, not debt.** They assert
  runtime semantics the type interface deliberately does not carry
  (ADR 0005); they stay pending permanently unless the boundary itself is
  revisited.
