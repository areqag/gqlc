# The resolver is built test-first, in staged slices

The resolver — the first consumer ADR 0008 opened — is built with the same
discipline that built the parser (ADR 0004): a golden-file test suite written
before the implementation, one capability per slice, and an output model
that develops while the resolver is its only producer. This ADR fixes what
ADR 0008 deliberately left open: the resolver's internal vocabulary, its
error posture, its test strategy, and the staging order (R0–R7). The API
surface itself — package, constructor, `Resolve` signature — is already
pinned by ADR 0008 and is not revisited here.

## Context

`query.Query` reached its feature-complete surface at Stage 14 (ADR 0008)
and the resolver's externally visible
shape is pinned: `internal/resolver`, `resolver.New(s schema.Schema,
opts ...Option)` with `WithRegistry(procsig.Registry)`, and a pure
`(*Resolver).Resolve(q query.Query) (ValidatedQuery, error)`. What is not yet
decided is everything behind that signature. Two in-repo precedents shape the
answer:

- **`internal/schema/gql`** is the existing resolution stage on the schema
  side: a listener fills a raw intermediate, then a pure `resolve()` turns it
  into `schema.Schema`, short-circuiting on the first of a closed set of
  package-level sentinel errors. Its test harness is fixture-driven golden
  files (`test/data/schema/gql/{valid,invalid}`, `-update` flag), with every
  invalid fixture paired to its sentinel and a bidirectional
  `TestSentinelReachability` sweep over a canonical `allSentinels` list.
- **ADR 0004's build discipline** carried the parser through sixteen slices
  without a consumer-facing rewrite: tests first, capabilities one at a time,
  the model unlocked exactly as long as churn is cheap.

The resolver sits between two vocabularies: the `query.Type` sum
(deliberately coarse — `unknown` is the parser's honest "cannot tell") and
the schema's bit-width-preserving `graph.PropertyType` families (ADR 0002:
`INT`…`INT256`, `UINT`…`UINT256`, `FLOAT`…`FLOAT256`, `DECIMAL`, `STRING`,
`BOOL`, `DATE`, `TIMESTAMP`). Codegen needs the rich end: an `INT32` column
must not decay to a generic int because the parser could only say `unknown`.

## Decision

### Purity and error posture

`Resolve` is a pure function of `(query.Query, schema.Schema[,
procsig.Registry])` — no I/O, no mutation, deterministic. Resolution
**short-circuits on the first error**, mirroring `schema/gql`'s `resolve()`:
one query, one verdict; there is no multi-error accumulation, because the
consumer (generation) aborts on any failure and the original text never runs.

Errors are **package-level sentinels** in `internal/resolver` (`ErrUnknownLabel`,
`ErrUnknownProperty`, `ErrParameterTypeConflict`, … — the concrete set grows
stage by stage), wrapped with detail at the fail-site
(`fmt.Errorf("%w: %s", …)`) so callers match with `errors.Is` and humans get
specifics — the cypher package's convention. The sentinel set is closed per
stage and swept by the same bidirectional reachability test the other two
packages carry: every sentinel reachable by some invalid fixture, every
invalid fixture pinned to its sentinel.

### `ValidatedQuery` and its type vocabulary

`ValidatedQuery` is the resolver's output model in `internal/resolver` (ADR
0008): the schema-checked, fully typed description codegen consumes — resolved
result columns, resolved parameters, and the entity/transaction facts codegen
needs (statement kind, nullability per column).

Its type vocabulary is **the schema's, not the parser's**: resolved column
and parameter types carry `graph.PropertyType` — the bit-width families of
ADR 0002 — plus the structural kinds the schema does not describe (entities,
lists, paths, temporal values from expressions). Resolution is exactly the
upgrade of the parser's honest `unknown`/coarse types into this richer
vocabulary; a resolver that flattened `INT32` to "int" would forfeit the
bit-width preservation ADR 0002 exists for. The concrete mapping table —
each of the seventeen `query.Type` variants → its resolved-type
counterpart, including which are already final (a literal's `int`), which
upgrade from the schema (`unknown` on a property ref), and which unify across
uses (parameters) — is the **R0 spec's first design item**, decided on paper
before the skeleton lands.

**`ValidatedQuery` develops through R0–R7**: it evolves slice by slice
while the resolver is the only producer. Codegen and driver code follow
once the resolver expresses what they target — the ADR 0004 anti-cascade
argument applies verbatim one layer down: model churn is cheapest while
downstream code does not yet exist.

### Test strategy

Golden **pairs** under `test/data/resolver/`: a hand-authored GQL schema
fixture plus a Cypher query fixture resolve to a `*.validated.golden.json`
snapshot, `-update` regenerates, `valid/` and `invalid/` split — the
`internal/schema/gql` harness shape, applied to a two-input stage. Invalid
fixtures pair with their sentinel in a map the test requires to be total.
Fixtures are hand-authored by necessity: the TCK supplies no GQL schemas, so
there is no corpus to vendor — schemas are written per capability, and one
schema fixture is shared by many query fixtures where possible. Layer 2
stays: targeted Go tests for constructor invariants and any behaviour a
golden diff states poorly (e.g. two-orientation trial outcomes).

The parser's TCK suite is untouched: it pins parsing, not resolution. The
resolver suite is a new, independent progress meter.

### Stages R0–R7, one capability per slice

Each stage is a TDD slice with a spec in `docs/specs/` written before the
code (the parser-stage convention), built via the same adversarial
implement→review loop, and tracked by its bead (`gqlc-0mx.2`–`.9`; the epic
is `gqlc-0mx`). Ordering is by dependency; a stage may be resequenced if a
spec round shows the dependency was misjudged.

- **R0 — skeleton** (`gqlc-0mx.2`): the package, the pinned API, minimal
  `ValidatedQuery`, the golden-pair harness, and the type-mapping table
  decided. Labelled single-node `MATCH`/`RETURN` (whole-entity and property
  refs) resolves end to end.
- **R1 — edges** (`gqlc-0mx.3`): directed `schema.EdgeKey` formation from
  named and inline endpoints; unlabelled-binding inference by walking the
  edges that touch a binding (the ADR 0003 consequence).
- **R2 — typing** (`gqlc-0mx.4`): property-type upgrade of `unknown`;
  parameter unification across `Uses` (property, clause-slot, expression)
  with a conflict sentinel.
- **R3 — edge semantics** (`gqlc-0mx.5`): undirected two-orientation trial
  against the schema (and the double-match question recorded on `gqlc-int`);
  multi-type edges as one candidate key per type; var-length hop-range
  lookups with `list<edge>` results.
- **R4 — nullability** (`gqlc-0mx.6`): ADR 0006's flow-typing regimes a and
  b (`gqlc-lqm`), relaxing the conservative everything-OPTIONAL-is-nullable
  default — a non-breaking refinement by construction.
- **R5 — multi-part and branches** (`gqlc-0mx.7`): `WITH` carry-forward
  scope checks, `UNION` column compatibility, `RETURN *` expansion (the
  resolver owns expansion), and implicit grouping keys — including the
  recorded `gqlc-gyw` contract: grouping keys over `ExprProjection`
  residuals come from a resolver-side re-parse of the projection's original
  text span; the `ContainsAggregate` axis is only the ADR 0008 escape hatch.
- **R6 — writes** (`gqlc-0mx.8`): effect validation against the schema —
  `SET`/`REMOVE` property and label existence, `CREATE`/`MERGE` shape,
  `DELETE` targets; projection-less writes resolve to a zero-column
  `ValidatedQuery`.
- **R7 — CALL** (`gqlc-0mx.9`): `YIELD` column typing from the
  `procsig.Registry`; argument assignability including `NUMBER`
  assignable-from `INTEGER`-or-`FLOAT` (ADR 0007's Stage-14 note); unknown
  procedure is a generation-time error by design.

## Considered options

**Resolve everything in one build against the query model, then test.**
Rejected: the query model reached feature-complete at Stage 14, but
`ValidatedQuery` itself is new and its shape is exactly what the staged,
test-first approach is for. The parser's sixteen slices are the evidence the
discipline works; there is no reason to believe the resolver is different.

**Reuse `query.Type` as the resolved vocabulary and bolt bit-widths on
later.** Rejected: that repeats the mistake ADR 0002 prevents on the schema
side. The parser's sum is coarse by design (it is schema-free); the resolver
exists to produce the schema-accurate types. Starting coarse would make every
later width-refinement a change that has to propagate through codegen.

**Multi-error accumulation instead of short-circuit.** Rejected for now: the
generation pipeline aborts on the first invalid query anyway, and
`schema/gql` set the short-circuit precedent. Accumulation is a UX
refinement that can be added behind the same API later (the error value,
not the signature, would change).

## Consequences

- **No resolver code before the owner grills this ADR.** The staging, the
  vocabulary decision, and the harness shape are the reviewable surface; R0
  (`gqlc-0mx.2`) is blocked on this ADR's bead (`gqlc-0mx.1`) closing.
- **`ValidatedQuery` develops slice by slice through R0–R7.** Codegen is
  built once the resolver expresses what it needs; the R-stage specs revise
  `ValidatedQuery` freely, exactly as the parser stages revised `query.Query`.
- **A second fixture corpus begins.** Hand-authored fixtures lack the TCK's
  breadth; coverage grows with each stage's spec, and the sentinel
  reachability sweep keeps the rejection surface honest. If a public GQL
  schema corpus emerges, it can be vendored later without changing the
  harness shape.
- **The type-mapping table becomes load-bearing.** It is decided in R0's
  spec, revised per stage as new `query.Type` variants become resolvable,
  and it is the load-bearing R-stage spec item.
