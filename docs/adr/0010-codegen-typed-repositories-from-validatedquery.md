# Codegen emits typed Go repositories from ValidatedQuery

Codegen is the stage the whole pipeline exists for: it consumes a resolved
query and emits type-safe Go repository code, the sqlc analogue for the graph
side. This ADR fixes the generated code's shape — the one boundary where
stability actually matters — plus the inputs codegen reads, the type-mapping
table, the runtime seam, and the staged build plan (C0–Cn). It is the
"ADR 0008 analogue" the `ValidatedQuery` docstring anticipated, minus the
retired freeze framing: it pins what codegen consumes by writing the first
consumer, not by protocol.

## Context

- Parser Stages 1–15 and resolver R0–R7 are complete. `resolver.Resolve`
  yields `ValidatedQuery{Columns, Parameters, Statement, Distinct}` with the
  sealed `ResolvedType` sum: `node`, `edge`, `edgeUnion`, `property`
  (bit-width families, ADR 0002), `scalar` (6 kinds), `temporal` (6 kinds),
  `list<T>`, `unknown`.
- ADR 0005: generated code executes the **original query text verbatim**,
  `$param` placeholders intact, values bound by name. The model shapes
  signatures only; it never reconstructs the query. Original text must be
  carried to codegen alongside the validated query — an orchestration
  concern the parser deliberately does not own.
- CONTEXT.md (**Query**): splitting a source file into individual, named
  queries is outside the parser. Codegen's front end owns it.
- Generated code lives in the **user's module**. It cannot import
  `internal/*` — every symbol it references must come from the stdlib, an
  exported gqlc runtime package, or the generated files themselves. This is
  a hard Go-level forcing function on the runtime seam.

## Decision

### D1 — Inputs and the generation unit

Generation is **batch** — one pure call over the whole package:

```go
type NamedQuery struct {
    Name        string
    Cardinality Cardinality // :one / :many / :exec, author-declared (below)
    SourceFile  string      // groups generated output per query file (D4)
    SourceText  string      // executed verbatim, ADR 0005
    Validated   resolver.ValidatedQuery
}

Generate(Input{Schema schema.Schema, Queries []NamedQuery}) ([]File, error)
```

Batch, not per-query, because several outputs are cross-query by nature:
the single `Queries` struct carries every method; name-collision detection
(method vs method, Row struct vs schema struct) needs the global view;
per-file import blocks depend on which queries share a file. A per-query
API would push exactly that assembly onto every caller. Internally the
generator loops query-by-query; the loop is not the caller's problem.
First-error short-circuit, package-level sentinels (house posture).

Resolved (grill, 2026-07-11): the annotation front end is its own small
package (`internal/queryfile`; name cosmetic): raw query-file text →
`[]AnnotatedQuery{Name, Cardinality, Text}`, with its own sentinel set
(missing annotation, unknown cardinality, duplicate name, …), built at C0
because a golden fixture *is* an annotated query file. Pipeline
orchestration (split → parse → resolve → generate) stays out of
`internal/codegen`: tests wire it inline, the CLI bead owns it in
production.

The schema side needs no new front end: `internal/schema/gql` already is
one (`gql.New() schema.Parser`, source in, `schema.Schema` out — the
scratch harness demonstrates the wiring). No annotations, no splitting:
`schema.Schema` models exactly one graph type. v1 posture: one graph type
→ one generated package; multiple graph types are a CLI-level loop over
the whole pipeline, never visible to `Generate`.

- Codegen never reaches back into `query.Query`.
- Zero-column write methods are detectable as `len(Columns) == 0 &&
  Statement == StatementWrite` — no effects inspection needed.
- Schema-struct generation with an empty `Queries` slice is legal
  (models-only adoption path) and costs nothing under the batch shape.
- Resolved (grill, 2026-07-11) — `ValidatedQuery` (plus the `NamedQuery`
  envelope and `schema.Schema`) is **sufficient, confirmed by audit**:
  every artifact of the resolved D2/D3 surface maps to an available
  field, and the hunted counterexamples dissolve — effects are not
  needed (zero-column write = `len(Columns)==0 && Statement==Write`),
  `Use` positions did their job in R2's unification, per-part bindings
  never reach the projection surface. One finding worth recording:
  `Column` carries no "was aliased" bit — `Name` is one string, alias
  or verbatim source text. The Q5 row-field naming rules therefore run
  on **text-shape analysis**: bare identifier → mangle (alias and
  bare-variable projection are indistinguishable but render
  identically, so the ambiguity is harmless); `ident.ident` → final
  segment; anything else → the "alias required" sentinel. This works
  precisely because an alias is always an identifier — no alias can
  look like an expression. No resolver change needed.

Query naming and cardinality: sqlc-style annotations in the query file,
using Cypher line comments:

```cypher
// name: PeopleOverAge :many
MATCH (p:Person) WHERE p.age > $minAge RETURN p.name AS name
```

- Cardinality (`:one` / `:many` / `:exec`) is author-declared, as in
  sqlc — it is not inferable (`LIMIT 1` inference is a trap).
- Resolved (grill, 2026-07-11): grammar mirrors sqlc byte-for-byte,
  adapted to Cypher's line comment — `// name: PeopleOverAge :many`. No
  namespaced prefix: annotations appear only in gqlc-owned query files,
  so `// gqlc:` would buy nothing.
- Resolved (grill, 2026-07-11): maximally strict, house reject-don't-guess
  posture. Every query in a query file must carry an annotation (no
  anonymous queries); duplicate names across the input set and unknown
  cardinality tokens are front-end sentinels; cardinality×shape mismatch
  is a generation-time error — `:exec` on a column-producing query
  rejects, `:one`/`:many` on a zero-column write rejects. (sqlc silently
  allows `:exec` on a SELECT, discarding rows — a footgun we refuse.)
- Resolved (grill, 2026-07-11): the axis's canonical term is
  **cardinality**, unqualified — claimed in CONTEXT.md with a
  Flagged-ambiguities entry separating it from the edge **hop range**
  axis and the runtime "result cardinality" sense.

### D2 — Generated API shape

The complete inventory of what generation emits (owner brain dump
2026-07-10 + decomposition):

1. **Schema structs** — the `models.go` analogue: one struct per schema
   node/edge type, emitted unconditionally.
2. **Repository handle** — the `Queries` struct + `New(...)` constructor.
3. **Per-query `Params` / `Row` structs.**
4. **Decode path** — `dbtype.Node` / record → typed structs; the bulk of
   generated LOC and the main correctness surface (sqlc's scan-code
   analogue).
5. **Querier interfaces** (below) — the mockability surface. Resolved
   (grill, 2026-07-11): the executor-seam question splits into two
   rents. Mocking rent: none — the Querier interfaces cover it, and no
   exported executor interface is generated. Transaction-composition
   rent: **real, and paid in v1** — `WithTx` (below) requires every
   method body to route through one *unexported* run indirection. The
   default path opens a per-call session and executes under
   `ExecuteRead`/`ExecuteWrite` managed-retry semantics; the WithTx
   path runs `tx.Run` on the caller's transaction. Users never see the
   indirection.

Deliberately not generated: schema DDL / migrations (the schema is an
input, not an output), any runtime package (D4).

Method surface, following sqlc's proven shape:

```go
q := gen.New(db)                                   // db: runtime seam, D4
rows, err := q.PeopleOverAge(ctx, PeopleOverAgeParams{MinAge: 21})
// :one  -> (PersonRow, error)   :many -> ([]PersonRow, error)
// :exec -> error
```

- One `Queries` struct per generated package; one method per named query.

Constructor and interfaces (owner direction, 2026-07-10):

```go
func New(db neo4j.DriverWithContext) *Queries // concrete struct out

type ReadQuerier interface  { /* methods with Statement == read  */ }
type WriteQuerier interface { /* methods with Statement == write */ }
type Querier interface { ReadQuerier; WriteQuerier }

var _ Querier = (*Queries)(nil) // compile-time assertion, house style
```

- `New` takes the pool-owning driver object and returns the **concrete**
  struct (Go idiom: return structs, accept interfaces); the interfaces
  exist for consumers to declare dependencies against and for
  mockery-style tooling.
- The read/write partition is **derived from the resolver's
  statement-kind axis** — zero annotation cost, cannot drift. It is the
  reader/writer-pool DevX story: an application with separate reader and
  writer pools constructs two `*Queries` instances and types them
  `ReadQuerier` / `Querier` in its own code; a write method on the reader
  instance is a compile error. gqlc neither knows nor cares which pool is
  which. A single master instance typed `Querier` does both.
- `Statement` therefore shapes the generated surface twice: interface
  membership, and per-method session access mode (read methods request
  `AccessModeRead`, so cluster routing to followers is free).
- Resolved (grill, 2026-07-11) — **`WithTx` ships in v1** (owner
  requirement: chaining repository methods within a single
  transaction):

  ```go
  func (q *Queries) WithTx(tx neo4j.ManagedTransaction) *Queries

  session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
      qtx := q.WithTx(tx)
      if err := qtx.CreatePerson(ctx, ...); err != nil { return nil, err }
      return nil, qtx.CreateFriendship(ctx, ...)
  })
  ```

  sqlc's `WithTx` precedent, adapted to the driver's retry-safe idiom.
  `ManagedTransaction`-only (not `ExplicitTransaction`) keeps callers
  inside managed retry; widening later breaks nothing, since the
  parameter type is the driver's, not ours. Deferring WithTx (the D8
  treatment) was rejected: streaming is deferrable because nothing in
  v1's shape forecloses it, but transaction composition *is* foreclosed
  by method bodies that own their sessions — the run indirection must
  exist from C0.
- Resolved (grill, 2026-07-11) — names are `ReadQuerier` /
  `WriteQuerier` / `Querier`: `Querier` is sqlc's exact emitted name
  (instant recognition for the arriving-from-sqlc audience);
  `Reader`/`Writer` carry immovable `io.Reader`/`io.Writer` priors that
  misread at the dependency-declaration call site; the embedding
  `Querier { ReadQuerier; WriteQuerier }` states the partition in one
  line.
- Resolved (grill, 2026-07-11) — all three interfaces are emitted
  **unconditionally**, empty or not. An empty `WriteQuerier` adds no
  safety (any type satisfies it) but omission would make `querier.go`'s
  structural shape depend on the query mix — the first write query
  added to a read-only package would restructure the interface set
  underneath existing user code. Constant shape beats saving two inert
  lines.
- Resolved (grill, 2026-07-11) — mirror sqlc's signature ergonomics
  exactly. Params: zero → `(ctx)` only; exactly one → bare typed arg;
  two-plus → `XParams` struct, fields in first-appearance order. Rows:
  single column → bare value (`AllPeople(ctx) ([]Person, error)` with
  zero ceremony); two-plus columns → `XRow` struct, fields in projection
  order. Signature reshaping when a query gains its second param/column
  is the compile-time-safety product working, not churn to engineer away;
  struct-always would tax the most common queries forever.
- Resolved (grill, 2026-07-11) — naming rules, reject-don't-guess with
  exactly one deterministic mangle:
  1. Method names: the annotation name verbatim; it must already be a
     valid exported Go identifier (`^[A-Z][A-Za-z0-9]*$`), sentinel
     otherwise.
  2. Params fields: the one mangle site — `$min_age`/`$minAge` →
     `MinAge` (split on `_`, capitalize segments, preserve internal case
     of non-ALL-CAPS segments). Two params mangling to one field →
     sentinel.
  3. Row fields: derived only from the two clean shapes — bare variable
     (`p` → `P`) and property access (`p.name` → final segment →
     `Name`). Everything else (`count(*)`, expressions, literals)
     requires an explicit `AS alias`, sentinel otherwise ("alias
     required" — nudges self-documenting queries). Derived-name
     collisions within one Row → sentinel demanding aliases.
  4. Package-level: any two generated top-level identifiers colliding
     (XRow vs entity struct, method vs `New`) → sentinel; no renaming
     scheme.
- Resolved (grill, 2026-07-11) — `Distinct` and `Column.GroupingKey`
  surface **nothing**: `DISTINCT` is enforced by the database executing
  the verbatim text (it changes which rows arrive, not their types) and
  the emitted query-text const already displays it; `GroupingKey` did
  its job in resolver validation (R5) and has no signature consequence.
  Both stay in `ValidatedQuery` untouched, free for future targets.
  Rejected: paraphrasing them into doc comments — the adjacent query
  text is strictly more informative, and paraphrases drift.

### D3 — Type mapping (the load-bearing table)

The table is **per-target** (D4): each generator owns its mapping. The
table below is the neo4j-go-driver v5 target. The driver's `dbtype`
package supplies faithful representations we would otherwise have to
invent — notably `dbtype.Duration` (calendar-aware months/days/seconds,
no lossy `time.Duration`) and the four date/time kinds — closing most of
the temporal cells. Conversely, neo4j stores integers as int64 and floats
as float64: the schema language's wide families (INT128/256, UINT128/256,
FLOAT128/256, DECIMAL — and FLOAT16, which has no Go representation) are
**unrepresentable on this target**: a generation-time sentinel error, not
silent narrowing (resolved in grilling).

The mapping from the `ResolvedType` sum:

| Resolved | Go |
|---|---|
| `property` STRING / BOOL | `string` / `bool` |
| `property` INT8..INT64, UINT8..UINT64 | native `int8..int64`, `uint8..uint64` |
| `property` INT / UINT (machine) | `int` / `uint` |
| `property` INT128/256, UINT128/256 | generation-time sentinel error (unrepresentable: neo4j stores int64) |
| `property` FLOAT16 | generation-time sentinel error (no Go representation) |
| `property` FLOAT32/64 | `float32` / `float64` (FLOAT32: schema-width contract, resolution below) |
| `property` FLOAT128/256, DECIMAL | generation-time sentinel error (unrepresentable: neo4j stores float64) |
| `property` DATE / TIMESTAMP | `dbtype.Date` / `time.Time` |
| `scalar` bool/int/float/string | `bool` / `int64` / `float64` / `string` |
| `scalar` null | `any` (always nil; legal-but-pointless column) |
| `scalar` map | `map[string]any` (model carries no per-key structure, ADR 0003) |
| `temporal` date | `dbtype.Date` |
| `temporal` datetime | `time.Time` (v5's zoned-datetime mapping) |
| `temporal` localdatetime | `dbtype.LocalDateTime` |
| `temporal` time / localtime | `dbtype.Time` / `dbtype.LocalTime` |
| `temporal` duration | `dbtype.Duration` (calendar-aware; never `time.Duration`) |
| `node` / `edge` | generated entity struct (below), `Nullable` → pointer |
| `edgeUnion` | sealed interface per query column (resolution below) |
| `list<T>` | `[]T` recursive |
| `unknown` | `any` (honest type for honestly-untypeable; resolution below) |

Entity structs: generated once per schema node/edge type, fields from
schema properties. Naming resolved (grill, 2026-07-11), extending the
Q5 reject-don't-guess rules:

1. The explicit **type name** (`NodeType.Name` / `EdgeType.Name`) always
   wins when present — validated as an exported Go identifier, sentinel
   if invalid. This gives the schema's until-now-unconsumed `Name` field
   its consumer.
2. Single-label node type → the label through the standard mangle
   (identity for conventional `Person`-style labels).
3. Single-label edge type with an unambiguous label (one endpoint pair
   in the schema) → same mangle; `ACTED_IN` → `ActedIn`.
4. Multi-label types and edge labels shared across endpoint pairs are
   **fully supported — via an explicit type name**, which is *required*
   there: no invented `ActorPerson`/`KnowsPersonCompany` guesses,
   sentinel when omitted. Checked **eagerly**: an unnamed multi-label
   type fails generation even if no query projects it — lazily erroring
   would make output depend on the query set in a surprising way.
5. Property fields: `Property.Name` through the standard mangle,
   collision → sentinel. Package-level collisions land in the Q5 rule.
- Entity property nullability: the vocabulary already exists —
  `schema.Property` carries `Nullable` → pointer field (resolution
  below).
- Resolved (grill, 2026-07-11) — entity structs are **schema-shaped
  only**: fields come from schema properties and nothing else. No
  `ElementId`, no start/end ids, no deprecated int64 `Id` — driver
  identity is metadata of a *particular fetched record*, not of the
  schema type, and keeping it off the structs keeps the conceptual
  models target-independent (a future TypeScript generator reuses the
  same vocabulary). Driver identity is instead **a projection, not a
  property**: `RETURN p, elementId(p) AS id` puts identity on the Row
  struct beside the entity, opt-in per query, visible in the query
  text, executable today under ADR 0005's verbatim-text posture. Wart,
  accepted: the resolver types function invocations as `unknown`, so
  the alias renders `any` for now; a resolver-side builtin
  scalar-function signature table (`elementId(node|edge)` → string) is
  filed as `gqlc-v5t` — when it lands, the same query yields
  `Id string` with zero codegen changes (the Q9 "resolver upgrades
  silently tighten generated code" gradient, working as designed).
  Rejected: `ElementId` on every entity struct (driver leakage into the
  schema vocabulary), config opt-in (two users' `models.go` diverge
  structurally for the same schema).
- Resolved (grill, 2026-07-11) — `edgeUnion` renders as a **sealed
  interface**, the house closed-sum pattern (`ResolvedType` itself is
  one): `type <QueryName><Field> interface{ is<QueryName><Field>() }`,
  candidate entity structs implementing via generated marker methods,
  annotated `//sumtype:decl` so gochecksumtype users get static
  exhaustiveness on the consuming type switch. Naming follows the
  invented-but-derived precedent (query name + row-field name), so the
  wrapper is per-query-column: two queries projecting the same
  multi-type edge get two interfaces — deduplicating would reintroduce
  banned set-derived names. Nullability carve-out: a nullable union
  column is a nil interface (pointer-to-interface is an anti-pattern);
  decode still enforces non-nullable-never-nil. Accepted cost:
  `json.Unmarshal` cannot target an interface field — Row structs are
  not a serialization format. Rejected: one-of struct with
  per-candidate pointers (no exhaustiveness story, nil-chain
  consumption), raw `dbtype.Relationship` (discards R3's typed
  candidates), v1 refusal (walls off a shipped resolver capability).
- Resolved (grill, 2026-07-11) — nullability is **pointers, uniformly**:
  nullable properties, nullable bindings (R4 flow-typed), nullable
  scalar/temporal columns all render `*T`. Why not a `Null[T]` wrapper:
  neither shape is compile-time safe in Go, so the choice is between
  failure modes — a forgotten nil-check panics loudly at the fault site,
  a forgotten `.Valid` check yields a **silent zero value** that
  corrupts data downstream; loud-and-immediate wins. A third-party
  wrapper (gonull et al.) additionally bakes a supply-chain dependency
  into every user module — disqualifying on its own; a generated
  per-package `Null[T]` makes two generated packages mutually
  incompatible. The enforced safety lives in decode: a non-nullable
  column arriving null from the driver is a decode error naming the
  column, never a zero value — so the nil-checkable surface is exactly
  the columns the resolver proved nullable. The rule applies
  **symmetrically to parameters**: a `ResolvedParameter` whose type
  carries `Nullable` (unified against a nullable property) renders a
  pointer field in the Params struct, and nil encodes as Cypher
  `null` — same table, both directions. Orthogonal axis: `:one` with
  zero rows returns a generated `ErrNoRows`-style sentinel (sqlc
  precedent), never a nil result — row absence is not nullability.
- Resolved (grill, 2026-07-11) — the residual cells. `unknown` → `any`:
  reject-don't-guess targets wrong answers, not honest ones — `any` is
  the truthful type for a column the resolver cannot yet type; refusing
  would wall off legitimate queries that merely outrun the resolver's
  coverage, and every resolver upgrade silently tightens generated code.
  `scalar` null → `any` (always nil at runtime; refusing a harmless
  column is gratuitous). `scalar` map → `map[string]any` (ADR 0003's
  model carries no per-key structure — nothing richer exists to
  generate). FLOAT16 → generation-time sentinel, same family as
  INT128+/DECIMAL: no Go representation exists. FLOAT32 → `float32`
  with the **schema's declared width as the API contract** (owner
  direction): the generated field is `float32`, encode widens
  losslessly to the driver's float64, decode narrows by plain
  conversion — no range check, because the schema author declared the
  width and the store validated writes. The engine's transport width is
  an implementation detail the generated surface deliberately does not
  leak.

### D4 — Targets: the driver is a generation-time choice

Decided direction (owner, 2026-07-10): driver choice is a **generation-time
target**, not a runtime abstraction — sqlc's posture (`sql_package: pgx` vs
`database/sql`). The pluggable seam is the generator itself:

```go
type File struct {
    Path     string // relative to the configured out dir
    Contents []byte // gofmt-clean, header-stamped
}

type Generator interface {
    Generate(Input) ([]File, error)
}
```

- First and only initial implementation: **Go + neo4j-go-driver v5
  (latest)**. Generated code imports the driver directly; no exported gqlc
  runtime package, preserving the repo's internal-only posture.
- `[]File` is the language-neutral boundary: a future TypeScript or
  second-driver generator returns files too — the interface survives new
  targets without redesign.
- `Generate` **never touches disk**: it returns the complete, authoritative
  file set for the out dir, in deterministic order. The caller owns I/O —
  including stale-file cleanup, implementable as "sync dir to returned set".
- Self-contained emission: the executor seam (D2) and all decode code are
  generated into the user's package; generated code references only the
  stdlib and the neo4j driver.
- Consequence: `gqlc-0aa` (driver wiring) shrinks — there is no runtime
  layer to build, at most session/tx helpers. Re-scope that bead once this
  ADR lands.
Resolved (grill, 2026-07-11) — the emitted file set, sqlc's proven layout:

```
db.go            — Queries struct + New(neo4j.DriverWithContext)
models.go        — one struct per schema node/edge type, plus their
                   unexported decode helpers (dbtype.Node → struct)
querier.go       — ReadQuerier / WriteQuerier / Querier + assertion
<name>.cypher.go — per input query file: query-text consts, Params/Row
                   structs, methods, per-query row assembly
```

- Per-source-file grouping keeps generated diffs local to the query file
  the author edited; `NamedQuery.SourceFile` carries the grouping key.
  Two input files sharing a base name collide on output — front-end
  sentinel, same strictness family as duplicate query names.
- Decode placement follows what's shared: entity decode
  (`dbtype.Node` → `Person`) is identical for every query projecting that
  entity, so it lives in `models.go` beside the struct; per-query row
  assembly (record positions → `XRow{...}`) is query-specific and inline
  in that query's file. Fully-inline duplicates entity decode N times;
  fully-shared invents a reflection-ish layer — this split avoids both.
- Resolved (grill, 2026-07-11) — with one implementation the `Generator`
  interface is speculative breadth, accepted because it fixes the
  `[]File` contract now. The leakage check passes: `Input` ended the
  grill as exactly `{Schema, Queries}` (D6) — model types, no
  driver-specific fields — and `File` is path + bytes; nothing in the
  signature names a target.

### D5 — Determinism and output stability

The stability that matters lives here (freeze retirement rationale).
Resolved (grill, 2026-07-11):

- Byte-identical output for identical inputs; ordered iteration everywhere.
  Enforced twice: golden comparison, plus a cheap dedicated test that
  generates twice and requires byte-identical `[]File` — golden diffs
  catch within-run nondeterminism (map iteration) only flakily; the
  double-run test catches it deterministically.
- Golden-file harness mirroring `schema/gql` and `resolver`:
  `test/data/codegen/{valid,invalid}`, `-update` flag. A valid fixture is
  a schema `.gql` + annotated query file(s); its golden is the complete
  generated package. Invalid fixtures pair to sentinels in a total map
  with the bidirectional reachability sweep.
- **The goldens are a nested Go module** (`test/data/codegen/go.mod`).
  Codegen goldens are `.go` files, and `test/data` is not the magic
  `testdata` name Go tooling skips — a nested module is excluded from
  the parent's `./...` walks automatically. Three consequences, all
  wanted: gqlc's own `go.mod` stays **driver-free** (the generator emits
  text and never imports the driver); the nested module's `go.mod` is
  the single place the target driver version is pinned; and the compile
  fence is simply `go build ./... && go vet ./...` inside that module —
  one small cached build, seconds of CI time, expressed as a `just`
  recipe used identically locally and in CI. Rejected: `testdata`
  renaming (hides goldens but forces a copy-to-temp-module fence),
  per-run temp-module assembly (module resolution per test run; CI
  wall-time is a hard constraint).
- Error posture: closed package-level sentinel set + bidirectional
  reachability sweep, same as the other two stages.
- Generated file header: tool + version stamp, "do not edit" marker.

### D6 — No configuration; CLI deferred

Resolved (grill, 2026-07-11): **v1 has no configuration at all** (owner
direction: config is designed later, properly, as its own effort — not
smuggled in field by field). `Input` is exactly `{Schema, Queries}`.

- The one value the generator cannot invent — the emitted package
  name — is **derived from the schema name**: `Schema.Name` through a
  lowercase mangle (`Movies` → `package movies`), sentinel if the
  result is not a valid Go package identifier. A deterministic
  derivation rule, same family as label → struct name, not a guess —
  and better DevX than a placeholder (`movies.New(db)` reads right).
  When config exists, an override knob joins properly; the internal
  `Input` shape is freely revisable (no internal-API freeze).
- The annotation front end (D1) forces no early config decisions: it
  consumes raw text handed to it. File discovery (globs), schema paths,
  out dir, target selection, the procsig registry file (`gqlc-6rz`) are
  caller concerns — the golden harness wires them inline; the future
  CLI bead owns them in production.
- Deferred to that future effort: the CLI itself (`main.go`'s promotion
  from scratch harness), disk writes, out-dir sync (stale-file deletion
  against the returned complete set), and the config file (`gqlc.yaml`
  analogue) — after the generator exists under golden tests.

### D7 — Staged build plan (test-first, spec-first per stage)

Same discipline as ADR 0004/0009: golden tests first, one capability per
slice, each stage its own branch/PR with a spec doc
(`docs/specs/codegen-stage-c*.md`) written before the code, built via the
adversarial implement→review loop. Resolved (grill, 2026-07-11); beads
mirror the resolver precedent — children of `gqlc-8i0`, each stage
blocked by its predecessor:

- **C0 — harness + skeleton** (`gqlc-8i0.2`): the nested golden module +
  compile fence (D5), `internal/queryfile` annotation front end (D1),
  double-run determinism test, and skeleton emission that already
  compiles: derived package name + header (D6), `db.go` (`Queries`,
  `New`, `WithTx`, the unexported run indirection), `querier.go` (three
  interfaces + assertion, empty), empty `models.go`. WithTx's presence
  here is why D2 had to decide it now.
- **C1 — scalar/property reads** (`gqlc-8i0.3`): Params/Row structs, the
  naming rules + text-shape analysis (D1 audit), native-width property
  mapping, nullable → pointers (both directions), `:one`/`:many`,
  `ErrNoRows`, read path via `ExecuteRead`.
- **C2 — entity projections** (`gqlc-8i0.4`): `models.go` structs from
  the schema (naming rules incl. the eager multi-label check),
  schema-shaped-only posture, entity decode helpers in `models.go`,
  per-query row assembly inline.
- **C3 — collections + temporals + widths** (`gqlc-8i0.5`): `list<T>`,
  six temporals via `dbtype`, unrepresentable-width sentinels, the
  FLOAT32 schema-width contract, `unknown`/`null`/`map` → `any`.
- **C4 — writes** (`gqlc-8i0.6`): `:exec`, zero-column methods,
  `WriteQuerier` population, `ExecuteWrite` path, cardinality×shape
  rejection.
- **C5 — hard residue** (`gqlc-8i0.7`): `edgeUnion` sealed interfaces,
  package-level collision sweep hardening.
- **C6 — polish** (`gqlc-8i0.8`): version-stamp hardening,
  session-config polish, re-scope `gqlc-0aa` against D4's
  no-runtime-package decision.
- **C7 — `:iter` streaming (post-v1)**: `gqlc-1a5`, blocked on the epic;
  D8's design opens are transferred to that bead.

Standing instruction for every C-stage spec: the D3 dbtype cells were
written from memory of the driver — verify each mapping against
neo4j-go-driver v5 docs when the stage that renders it is specced.

### D8 — Streaming and result iteration (documented now, built later)

sqlc's known weakness, deliberately not repeated: every many-row query
materialises `[]Row`, so accidental full-result loads are easy, pagination
is painful, and streaming can never be retrofitted — the session is closed
by the time the caller sees data. The neo4j driver streams results
natively and Go ≥ 1.23 range-over-func gives the consumer shape, so the
design space is reserved now even though v1 does not build it:

```go
// candidate: a fourth cardinality, :iter
func (q *Queries) HeaviestPeople(ctx context.Context) iter.Seq2[PersonRow, error]
```

- Session lifetime is the hazard streaming must own: the session survives
  while the consumer ranges; `iter.Seq2` scopes cleanup to loop exit,
  early `break` included.
- v1 commitments are exactly two: (a) cardinality is an open enum, so
  `:iter` joins `:one/:many/:exec` without an annotation-grammar break;
  (b) nothing in D2's method shape assumes materialisation.
- Requires Go ≥ 1.23 in the *user's* module (this repo is on 1.26).
- Resolved (grill, 2026-07-11): `:iter` is **author-declared, opt-in** —
  it takes its own annotation, joining `:one/:many/:exec` as the fourth
  cardinality. Auto-generating an `XIter` sibling for every `:many`
  rejected: consistency with §D1's reject-don't-guess posture, session-
  lifetime discipline is a hazard callers must opt into, doubling the
  Querier surface for users who never asked for streaming taxes the
  mocking + docs path. Callers who want both shapes write two
  annotations (`AllPeople :many`, `AllPeopleIter :iter`). Universal
  `:iter` (kill `:many`) considered as a safety-by-default framing and
  deferred to a later revisit — it fixes the memory footgun but breaks
  managed retry (pre-first-yield-only, below) and creates a read-
  uncommitted visibility hazard on writes.
- Resolved (grill, 2026-07-11): `:iter` is **read-only**. New sentinel
  `ErrIterOnWrite` (introduced when the bead lands) rejects `:iter` on
  `StatementWrite` at generation time. Writes-with-return under `:iter`
  leak uncommitted data — the callback yields rows to the caller while
  the tx is still open, so a mid-stream error rolls the tx back and the
  caller keeps references to rows that no longer exist, or pre-first-
  yield retry re-runs the CREATE and the caller's stashed `elementId`
  no longer matches. `:many` holds the invariant "if you see it, it's
  committed"; `:iter` cannot. Callers who need streaming plus write must
  use `:many` and accept materialisation.
- Resolved (grill, 2026-07-11): cardinality×shape validation extends
  symmetrically — `:iter` requires `len(Columns) > 0`; zero-column
  `:iter` reuses `ErrCardinalityShapeMismatch`. Truth table gains one
  reject cell (`:iter` + Write + columns → `ErrIterOnWrite`) and mirrors
  the existing zero-column rejects.
- Resolved (grill, 2026-07-11): method naming inherits §D2's rule
  verbatim — the annotation name is the method name. No `Iter` suffix
  auto-appended; author owns the name. Collisions between `:many` and
  `:iter` on the same query name caught by the existing package-level
  identifier collision sentinel (§D2 point 4).
- Resolved (grill, 2026-07-11): retry envelope is **pre-first-yield
  only**. `tx.Run(ctx, ...)` failure inside the `ExecuteRead` callback
  triggers the driver's managed retry — the connection-setup /
  leader-election flake class is covered. Once the first row is yielded
  to the caller, transient errors from `result.Err()` or per-row decode
  are yielded as `(zeroRow, err)` and the callback returns nil to
  prevent retry (retry would double-yield already-delivered rows):

  ```go
  func (q *Queries) HeaviestPeople(ctx context.Context) iter.Seq2[Row, error] {
      return func(yield func(Row, error) bool) {
          err := q.session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
              result, err := tx.Run(ctx, cypher, params)
              if err != nil { return nil, err }         // retry covers this
              for result.Next(ctx) {
                  if !yield(decode(result.Record())) {
                      return nil, nil                    // consumer break
                  }
              }
              if err := result.Err(); err != nil {
                  yield(zeroRow, err)                    // mid-stream err
                  return nil, nil                        // nil = no retry
              }
              return nil, nil
          })
          if err != nil { yield(zeroRow, err) }          // retries exhausted
      }
  }
  ```

- Resolved (grill, 2026-07-11): `SKIP` / `LIMIT` are **orthogonal** to
  cardinality — they flow through as `$skip` / `$limit` Params like any
  other typed argument (int64 per D3). Three legit patterns: `:iter`
  alone (stream, break to stop early), `:many` with `SKIP`/`LIMIT`
  (retry-covered pagination), `:iter` with `SKIP`/`LIMIT` (streamed
  window). No special codegen handling.
- Resolved (grill, 2026-07-11): testing mirrors the existing codegen
  fence. Goldens under `test/data/codegen/valid/iter_read_*` for shape
  (scalar, entity, multi-column) and under `invalid/iter_on_write` +
  `invalid/iter_zero_column_read` for sentinels. The `gqlc-73h`
  testcontainer harness (once merged) is extended with `:iter` cases:
  seed rows → range via `:iter` → assert values; verify early `break`
  cleanup; verify pre-first-yield retry. Nested golden module `go.mod`
  for `:iter` fixtures specifies `go 1.23` minimum.
- Implementation trigger: gqlc-1a5 remains open at P3. Picking up the
  build is gated on real user demand (someone files an issue for
  streaming) or benchmarked need (`[]Row` materialisation shown to
  bottleneck a workload). The grill markers above are the paved runway
  when the trigger fires.

## Consequences

- **The generated surface is the product.** `test/data/codegen` goldens
  become the regression fence for the only boundary users feel;
  byte-diffs in goldens are the review surface for every future model
  change.
- **gqlc's own module stays driver-free.** The generator emits text and
  never links the driver; neo4j-go-driver v5 is pinned in exactly one
  place, the nested golden module's `go.mod` (D5).
- **No runtime package, no public API surface to version.** Everything
  generated is self-contained in the user's module (D4). `gqlc-0aa`
  shrinks accordingly — re-scoped at C6 (`gqlc-8i0.8`).
- **No configuration in v1** (D6). Config is a future, deliberate
  effort; the derived package name is the only value that would have
  needed a knob, and its derivation rule leaves nothing blocked.
- **Resolver upgrades silently tighten generated code.** `unknown`
  renders `any` today; each builtin the resolver learns (`gqlc-v5t`:
  `elementId` → `string`) turns `any` fields into typed ones with zero
  codegen changes. The recommended identity pattern (`elementId(p) AS
  id`, D3) is the first beneficiary.
- **A third fixture corpus begins** (schema + annotated query files →
  complete generated packages), hand-authored per stage like the
  resolver's, swept by the same sentinel-reachability discipline.
- **No codegen code before this ADR's bead closes.** C0 (`gqlc-8i0.2`)
  is blocked on `gqlc-8i0.1`; the grill trail in this document's
  "Resolved" markers is the reviewable surface, mirroring ADR 0009's
  gate.
