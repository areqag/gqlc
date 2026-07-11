# Stage C4 spec — codegen: write queries

The implementation brief for Stage C4 of `internal/codegen`, extending
the merged C3 slice (`docs/specs/codegen-stage-c3.md`) with the four
capability slices ADR 0010 D7 places at C4: **`:exec` cardinality**
(the third `Cardinality` enum member C0/C1/C2/C3 have deferred),
**`WriteQuerier` interface population** (the second half of the
`Querier { ReadQuerier; WriteQuerier }` embedding that C0 declared but
kept empty), **the `ExecuteWrite` arm of `driverDB.run`** (the stub
`return nil, fmt.Errorf(...)` the C1 body left for this stage —
§5.6), and **cardinality × shape rejection** (two new sentinels
distinguishing the two footgun shapes sqlc silently accepts). Build
this **test-first**. Scope, sequencing, and error posture inherit
from ADR 0010 and the C0 / C1 / C2 / C3 specs unchanged; this
document revises only the sections C4 touches.

Stage C4 keeps the C3 file set (`db.go` / `querier.go` / `models.go`
/ `<name>.cypher.go`) byte-identical for the parts C4 does not touch,
extends Phase A's cardinality admission from `{One, Many}` to the
full `{One, Many, Exec}` set with a paired shape check that fires
the two new cardinality×shape sentinels, extends Phase B with the
`:exec` method rendering (no Row struct, no row assembly, no return
value beyond `error`), extends `querier.go` with the populated
`WriteQuerier` (write methods listed in `Input.Queries` order),
extends `db.go`'s `driverDB.run` write arm from stub to real
`neo4j.ExecuteWrite` dispatch, and threads the emitted method's
access-mode argument from `AccessModeRead` (unconditional at C3)
to `AccessModeRead` or `AccessModeWrite` per `Validated.Statement`.
The C3 property → Go type mapping stays byte-identical for reads;
writes reuse the same Params-side mapping unchanged. Every `:exec`
method body is three lines (`_, err := q.db.run(...); return err`,
plus the emitted `nil` params or map-literal build);
the `:one` / `:many` **write-with-projection** path
(`CREATE (p:Person) RETURN p` — legal under ADR 0003's `Statement`
axis and admitted by the resolver, verified below) reuses the C1
row-assembly with the sole delta that `access` is `AccessModeWrite`.

---

## 1. Deliverables

- `internal/codegen/generate.go` — extended with the C4 cardinality
  admission (§4.9), the two new cardinality×shape sentinel routes
  (§4.9), the `:exec` method rendering arm (§5.3, §5.5), the
  method-body access-mode threading rule (§5.5), the populated
  `WriteQuerier` walk (§5.4), and the real `driverDB.run`
  `AccessModeWrite` body (§5.6). The C0 file layout stands
  (`codegen.go` / `input.go` / `errors.go` / `generate.go`); no
  new files at C4.
- `internal/codegen/errors.go` — extended with two new sentinels
  and one rename (§9): `ErrExecOnProjection`,
  `ErrCardinalityShapeMismatch`, plus `ErrOutOfC3Scope` →
  `ErrOutOfC4Scope`. No other sentinel additions or retirements.
- `test/data/codegen/valid/<name>/` — new C4 valid fixtures (§6.2),
  each with a schema `.gql` plus at least one write query, a
  `manifest.json`, and a `golden/` subdirectory with the complete
  generated package.
- `test/data/codegen/invalid/<name>/` — new C4 negative fixtures
  for the two new sentinels plus the renamed `ErrOutOfC4Scope`
  retirements (§6.4). The C3 `out_of_c3_scope_exec` fixture
  retires from the invalid set (writes are now in-scope) and
  reappears (with the same schema and query text) as a valid
  fixture (`write_exec_delete`, §6.2); the C3
  `out_of_c3_scope_edge_union` and
  `out_of_c3_scope_non_property_parameter` fixtures rename to
  `out_of_c4_scope_*` (§6.4).
- `internal/codegen/codegen_test.go` — no structural change; the
  `sentinelByName` map grows two rows and renames one.

Nothing downstream of writes + `:exec` + cardinality × shape
rejection is built. `edgeUnion` sealed interfaces + package-level
collision-sweep hardening (C5), version-stamp polish (C6),
`:iter` streaming (post-v1, `gqlc-1a5`) stay for their owning
stage per ADR 0010 D7. Non-property parameters (whole node, whole
edge, temporal expression, scalar literal, list, unknown) stay
post-v1 and continue to route through `ErrOutOfC4Scope`.

---

## 2. Architecture — deltas from C3

C3's architecture (§2 of the C3 spec) stands: the `Generator`
seam, the concrete `*Codegen` return, the empty `Option` surface,
the purity / determinism / short-circuit posture, the
`generate.go` / `generate` kernel split, the Phase Z / Phase A /
Phase B / cross-query sweep sequence, the `resolvedListGoType`
recursion, the eager width sweep. C4 extends Phase A's cardinality
admission with the two new cardinality × shape sentinels (§2.1),
extends Phase B's per-query name-derivation with the `:exec`
short-circuit (no Row struct — no Row-field derivation — for a
zero-column method) (§2.1), and extends the emission walk with
`:exec`'s three-line body and the `WriteQuerier` population
(§2.1). No new exported types except the two new sentinels; no
API-shape delta (§3 below); the `Input` struct stays `{Schema,
Queries}` (ADR 0010 D6).

### 2.1 The C4 kernel structure

The kernel remains one linear pass with early returns. C4 extends
each of the three existing phases in-place:

- **Phase Z — schema-shape admission and entity naming**
  (unchanged from C3). The Rule 1–6 sequence at §4.5 / §4.8 of the
  C3 spec runs verbatim: entity naming, property-field derivation,
  eager unrepresentable-width sweep. Nothing in Phase Z sees the
  cardinality axis; a schema with only writes projected is
  identical to a schema with only reads projected at Phase Z, so
  models-only adoption (an empty `Queries` slice) and every mixed
  or write-only batch produce the same entity emissions.
- **Phase A — batch admission** (unchanged shape, extended
  cardinality admission). Every `NamedQuery` still passes C0's
  `validateQueries` gate and the C1 / C2 / C3 per-query admission
  checks. C4 widens the admissible cardinality set from `{One,
  Many}` to the full `{One, Many, Exec}` set, and adds two paired
  shape checks:
  - **`:exec` on `len(Columns) > 0`** — a column-producing query
    annotated `:exec` routes to `ErrExecOnProjection` naming the
    query, cardinality, and the number of projected columns. sqlc
    silently accepts this ("`:exec` on a SELECT discards rows",
    per ADR 0010 D1 Resolved's language); we reject it because
    the projected columns are a documented contract the caller
    typed on-purpose, and discarding them is a footgun the same
    way `:one`-on-empty is (`ErrNoRows`).
  - **`:one` / `:many` on `len(Columns) == 0 && Statement ==
    StatementWrite`** — a zero-column write annotated `:one` or
    `:many` routes to `ErrCardinalityShapeMismatch` naming the
    query, cardinality, and the shape (`zero-column write`). The
    caller expects rows the query cannot produce; the fix is
    either annotate `:exec` or add a `RETURN` clause. The
    resolver identifies the shape unambiguously per ADR 0010 D1
    Resolved (`len(Columns) == 0 && Statement == StatementWrite`
    — no effects inspection needed).
  - **`:exec` on `len(Columns) == 0 && Statement == StatementRead`**
    — a zero-column read annotated `:exec` (a `CALL` procedure
    with no `YIELD`, or a rare pure-read query with no `RETURN`).
    Admitted at C4: the emitted body is identical to a zero-column
    write's `:exec` body (three lines, no row assembly). The
    resolver produces `StatementRead` because no write clause is
    at outer scope; `q.db.run` receives `AccessModeRead`, so
    cluster routing to followers is preserved. This is the one
    reason `:exec` is NOT synonymous with "write" — the axis is
    cardinality, not statement kind.
  - **`:exec` on `len(Columns) == 0 && Statement == StatementWrite`**
    — the canonical write path. Admitted at C4; `access` is
    `AccessModeWrite`. Body is the same three lines as the read
    `:exec` above, with the sole delta in the access-mode
    argument.
  - **`:one` / `:many` on a column-producing write** (write-with-
    projection: `CREATE (p:Person) RETURN p`). Admitted at C4.
    Full row assembly per C1's rules, sole delta is the access-
    mode argument. See §3.3.
  - **`ResolvedEdgeUnion` column** on any cardinality (`:exec`,
    `:one`, `:many`) still routes to `ErrOutOfC4Scope` (C5 owns).
    The C3 route stays, renamed sentinel.
  - **Non-property parameter** on any cardinality (whole node,
    whole edge, temporal literal, scalar literal, list, unknown)
    still routes to `ErrOutOfC4Scope`. Widening this axis is
    post-v1 per ADR 0010 D7.
  Phase A short-circuits: first offender wins across the extended
  admission set, in the C3 order (cardinality × shape check
  before column-type check before parameter-type check, so a
  fixture with both `:exec`-on-projection and a non-property
  parameter fires the cardinality sentinel).
- **Phase B — per-query name derivation** (unchanged shape,
  extended derivation set). The C1 helpers `paramFieldName`,
  `rowFieldName`, and `goType` stand unchanged. C4 extends the
  method-render dispatch (§5.3): a `:exec` query short-circuits
  through the row-derivation pass — no Row struct is emitted, no
  Row-field name is derived, no `<Method>Row` identifier joins
  the cross-query collision sweep. The Params-side derivation
  runs unchanged: `:exec` queries carry parameters exactly like
  `:one` / `:many` queries do, so the Params-field mangle and the
  collision check are the same code. A `:exec` with zero
  parameters emits no Params struct (same rule as `:one` / `:many`
  with zero parameters, §3.2 C1); a `:exec` with two-plus
  parameters emits `<Method>Params` (§3.2 C1); a `:exec` with one
  parameter takes the bare typed arg.

Phase A runs before Phase B because Phase B's name derivation reads
Phase A's admission decisions (the `:exec` short-circuit at Phase B
is safe because Phase A has already checked the paired shape). The
cross-query package-level identifier collision sweep runs unchanged
after Phase B. C4 introduces no new exported identifiers — the two
new sentinels are internal to `internal/codegen`, and every
generated identifier is one C0 / C1 / C2 / C3 already sweep-covers
(method name, `<Method>Params`, `<Method>Row`, entity struct
names). The sweep's four identifier sources stay; a `:exec` method
adds one method-name entry and, if it has two-plus parameters, one
`<Method>Params` entry (no `<Method>Row` entry — the `:exec`
short-circuit above).

### 2.2 `WriteQuerier` population — the one-line rule

C3's `WriteQuerier` was declared but empty:

```go
type WriteQuerier interface {
}
```

C4 populates it with every write method's signature, in
`Input.Queries` slice order, filtered by
`prepared.WriteMethod` (a new per-prepared-query bit set at Phase
B — see §5.4). The `ReadQuerier` population rule is unchanged;
C0's declared-unconditionally rule (`Querier { ReadQuerier;
WriteQuerier }` always exists even when one arm is empty) still
holds. The bit derivation is the ADR 0010 D2 Resolved rule
verbatim: a method is a `WriteQuerier` member iff
`Validated.Statement == StatementWrite`; every other method
(`StatementRead`) is a `ReadQuerier` member. Cardinality is not
the axis — a `:exec` on a `StatementRead` (call-with-no-yield)
lands in `ReadQuerier`; a `:one` on a `StatementWrite`
(write-with-projection) lands in `WriteQuerier`. The resolver's
`StatementKind` axis is the one that shapes the interface
partition; cardinality shapes only the method-body's row
assembly.

- **Read+write mixed batch** — `ReadQuerier` and `WriteQuerier`
  populate independently; a method belongs to exactly one. The
  order in each interface is `Input.Queries` order filtered by
  statement kind, so a mixed batch reads deterministically:
  `ReadQuerier` methods first, `WriteQuerier` methods first
  within their filter — the two interfaces list the same methods
  the batch declared, split by statement kind.
- **Read-only batch** — `WriteQuerier` stays empty; the emitted
  block is `type WriteQuerier interface {}` (C3 shape). Same
  outcome as C3 for the fixtures that never introduce writes.
- **Write-only batch** — `ReadQuerier` stays empty; the emitted
  block is `type ReadQuerier interface {}`. Same shape as the
  read-only case with the arm flipped.
- **Every method in both interfaces?** Never. `Statement` is a
  single-valued enum on `ValidatedQuery`, and the partition is
  by construction disjoint. There is no configuration knob to
  register a method twice; a hand-constructed prepared query
  bypassing the derivation is a caller bug the collision sweep
  would catch (method-name duplication).

### 2.3 The `driverDB.run` write arm — the C4 body

C1's `driverDB.run` shipped the read arm live and the write arm
stubbed (`case neo4j.AccessModeWrite: return nil, fmt.Errorf("gqlc:
write path not implemented")`). C4 replaces the stub with the real
`neo4j.ExecuteWrite` dispatch, structurally identical to the read
arm modulo the driver-side function name and the caller-side access
mode. The signature stays the C1 signature; the seam contract
(`[]*neo4j.Record` return, buffered via `.Collect(ctx)` inside the
transaction) still applies. §5.6 gives the full body.

`neo4j.ExecuteWrite` is the top-level function
`func ExecuteWrite[T any](ctx context.Context, session
SessionWithContext, work ManagedTransactionWorkT[T], configurers
...func(*TransactionConfig)) (T, error)` — verified against
`pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5@v5.28.4/neo4j`,
2026-07-11. Same generic-over-`T` shape as `ExecuteRead`; C4
instantiates `T` as `[]*neo4j.Record` for signature parity with the
read arm. Writes route through the driver's leader (via cluster
routing when the driver targets an Aura or clustered deployment);
followers are read-only. The generated code makes no policy
decision on cluster routing beyond passing `AccessModeWrite` — the
driver's session-config axis handles the rest.

- **Write arm callback returns `[]*neo4j.Record`.** For a `:exec`
  method the callback still runs `tx.Run` + `.Collect`, and the
  returned slice is either empty (the canonical shape for a write
  that does not `RETURN`) or one-plus records (the write-with-
  projection shape). The method body ignores the records for
  `:exec` (§5.5), decodes them for `:one` / `:many` (unchanged
  from C1). The uniform return shape is what keeps the seam
  contract minimal — a per-arm signature difference would push
  the arm-choice into the caller.
- **`AccessModeWrite` from the method body.** The emitted method
  passes `neo4j.AccessModeWrite` as the fourth `run` argument iff
  the query's `Validated.Statement == StatementWrite`; otherwise
  it passes `neo4j.AccessModeRead`. The dispatch on the arm is
  entirely inside `driverDB.run`; `txDB.run` still ignores
  `access` (the caller owns the transaction, so the session's
  access mode was already set at construction — the WithTx path
  neither knows nor cares about the argument, per C1 §5.6).

### 2.4 Purity, determinism, short-circuit — unchanged

C3 §2.3's three invariants stand:

- **Pure.** No new I/O; the `:exec` short-circuit is one branch
  in the method-render dispatch, the `WriteQuerier` walk is one
  filter over the prepared-query slice, the `driverDB.run` write
  arm is a template-literal delta with no runtime effect at
  generation time.
- **Deterministic.** Iteration order: Phase A / Phase B / per-
  source grouping are still `Input.Queries` slice order; the
  `WriteQuerier` population walk is `Input.Queries` slice order
  filtered by `Validated.Statement == StatementWrite`. No map
  iteration escapes into the output.
- **Short-circuit.** First-error wins across Phase Z, Phase A
  (extended with the two new cardinality sentinels), Phase B, the
  cross-query collision sweep, and per-source emission. Zero value
  on error: `(nil, err)`.

### 2.5 What the C4 change means for the emitted module

C4 revises the emitted `db.go` and `querier.go` bodies. The
tightest invariants (extending C3 §2.4):

- **`db.go`'s `driverDB.run` write arm.** The `case
  neo4j.AccessModeWrite:` body moves from
  `return nil, fmt.Errorf("gqlc: write path not implemented")` to
  the real `neo4j.ExecuteWrite` call. `fmt` is still imported
  (the `default` arm still returns a wrapped error). No new
  `db.go` import at C4.
- **`querier.go`'s `WriteQuerier` block.** Every write method
  gains one line in the `WriteQuerier` block, deterministic by
  `Input.Queries` filtered order. The `context` import (and
  potentially `dbtype` / `time` for a write-with-projection Row
  type) follows the C2 `querierImports` sweep, unchanged.
- **`<name>.cypher.go`'s method body for `:exec`.** The three-
  line body: `_, err := q.db.run(...); return err`. No `errors`
  import needed (no `ErrNoRows` / `ErrMultipleResults` reference
  from a `:exec` method). No `fmt` import needed *for the exec
  method's body* — but the file may still import `fmt` if any
  other method in the file emits a decode wrapper. §5.5 gives the
  exact template.
- **`<name>.cypher.go`'s method body for write-with-projection.**
  Identical to a C1 `:one` / `:many` read body except the fourth
  `run` argument is `neo4j.AccessModeWrite`. The row-assembly
  templates in the C3 spec (§5.5) reuse verbatim.

The change is entirely inside the emitted templates; gqlc's own
module is not affected — the generator emits text, and text-level
changes cross no dependency boundary. The nested-module compile
fence (`just test-codegen-fence`, C0 §7) is what proves the
emitted write arm type-checks against the pinned driver version.
`neo4j.ExecuteWrite` is stable in v5.28.4 (verified against
`pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5@v5.28.4/neo4j`,
2026-07-11): same top-level function shape as `ExecuteRead`,
same `[T any]` generic signature, same `ManagedTransactionWorkT`
callback type, same `TransactionConfig` variadic tail.

---

## 3. Emitted API surface — the C4 shape

The user-visible generated surface C4 adds on top of C3. C4's
exported package-level identifiers grow by the `:exec` method
names and (for two-plus-parameter `:exec` queries) their Params
struct names; the `WriteQuerier` interface gains members; the
`Querier` embedding stays byte-identical. The C0 / C1 / C2 / C3
exported skeleton set (`Queries`, `New`, `WithTx`, `ReadQuerier`,
`WriteQuerier`, `Querier`; `ErrNoRows` / `ErrMultipleResults` when
a `:one` query is present; per-query methods; entity structs;
`<Method>Params` / `<Method>Row` structs) is unchanged.

### 3.1 `:exec` method returns and signatures

A `:exec` method's return type is `error`; the method has no other
return value. The parameter shape follows the C1 rule verbatim
(zero → `(ctx)`, one → bare typed arg, two-plus →
`<Method>Params`):

```go
// name: RemovePerson :exec
// MATCH (p:Person) WHERE p.id = $id DELETE p
func (q *Queries) RemovePerson(ctx context.Context, id int64) error

// name: RemoveBoth :exec
// MATCH (p:Person)-[r:KNOWS]->(o:Person) WHERE p.id = $pid AND o.id = $oid DELETE r
type RemoveBothParams struct { Pid int64; Oid int64 }
func (q *Queries) RemoveBoth(ctx context.Context, arg RemoveBothParams) error

// name: TruncatePeople :exec
// MATCH (p:Person) DELETE p
func (q *Queries) TruncatePeople(ctx context.Context) error
```

- **`error` return, uniform.** A `:exec` never returns rows-
  affected, IDs generated, or any other side-channel value. If
  the caller needs those, they annotate `:one` / `:many` with a
  `RETURN` clause (write-with-projection, §3.3) — the honest
  path. `error` alone matches sqlc's `:exec` precedent and keeps
  the method body's three-line shape (`_, err := q.db.run(...);
  return err`).
- **`ErrNoRows` / `ErrMultipleResults` do not apply.** The `:exec`
  body ignores the returned `[]*neo4j.Record`, so the arity check
  the `:one` body runs (`len(records)`) is skipped. A `:exec` on
  a query that would produce rows (a `MATCH ... RETURN` labelled
  `:exec`) is impossible at generation time — such a query would
  fire `ErrExecOnProjection` at Phase A (§4.9); the resolver's
  `Columns` slice is the discriminator, not the runtime record
  count.
- **Signature reshaping on parameter count** — same C1 rule as
  `:one` / `:many`. A `:exec` gaining its second parameter grows
  a `<Method>Params` struct; the caller adjusts. This is not
  churn to engineer away — same trade-off as C1's `:one` /
  `:many` rule.

### 3.2 `WriteQuerier` interface population

C3's `WriteQuerier` was declared empty. C4 populates it with one
line per write method, deterministic by `Input.Queries` slice
order filtered by statement kind:

```go
type WriteQuerier interface {
    RemovePerson(ctx context.Context, id int64) error
    RemoveBoth(ctx context.Context, arg RemoveBothParams) error
    CreatePerson(ctx context.Context, arg CreatePersonParams) (Person, error)
}
```

- **Order** is `Input.Queries` slice order filtered by
  `Validated.Statement == StatementWrite`. Interleaving reads and
  writes in the input source is transparent: the filter picks up
  writes in their input order, reads in their input order.
- **Signature parity** with the concrete method. The interface
  member is the exact method signature — same return type, same
  parameter list. Test parity (the `var _ Querier = (*Queries)(nil)`
  assertion) covers both interfaces uniformly.
- **Write-with-projection member** — a `:one` / `:many` write
  method still lists in `WriteQuerier`, not `ReadQuerier` (the
  partition is on `Statement`, not on `Cardinality`). The last
  line of the block above is the write-with-projection case: the
  method returns `(Person, error)` — a C1 single-column `:one`
  return whose column resolved to `Person` (§3.3 below).

The `var _ Querier = (*Queries)(nil)` assertion at the bottom of
`querier.go` still holds unchanged: every method emitted on
`*Queries` is a member of exactly one of the two interfaces, and
`Querier` embeds both.

### 3.3 Write-with-projection: `:one` / `:many` with `RETURN`

A query like `CREATE (p:Person) RETURN p` is legal openCypher, and
the resolver produces `Statement = StatementWrite` (a write clause
at outer scope) with `Columns = [{Name: "p", Type: ResolvedNode{...}}]`
— verified against ADR 0003 §3.1's Statement axis and the R0
resolver's `Statement` derivation (source in
`internal/resolver/statement.go`, unchanged from R0). C4 admits
this shape and routes it through both the write path (Phase A's
statement-shape check) and the C1/C2/C3 row assembly (Phase B's
name derivation, §5.5's decode block).

```go
// name: CreatePerson :one
// CREATE (p:Person {id: $id, name: $name}) RETURN p
type CreatePersonParams struct { Id int64; Name string }
func (q *Queries) CreatePerson(ctx context.Context, arg CreatePersonParams) (Person, error)
```

- **`WriteQuerier` membership**, not `ReadQuerier`. The
  partition is on `Statement`; the Row-projection is orthogonal.
- **`AccessModeWrite` in the emitted method body.** The fourth
  `q.db.run` argument is `neo4j.AccessModeWrite`; the row
  assembly runs unchanged over the write arm's returned
  `[]*neo4j.Record`. The `driverDB.run` write arm buffers the
  records the same way the read arm does (`.Collect(ctx)`
  inside the callback); the seam contract is uniform.
- **`:one` empty / multi sentinels apply** — a
  `CREATE ... RETURN` that produces zero rows (an idempotent
  create pattern the caller labelled `:one`) fires `ErrNoRows`;
  a caller who wanted zero-or-one should use `:many`. Symmetric
  with reads. The `:many` write-with-projection accepts any
  arity including zero.
- **Nullability applies.** A `RETURN p, p.deletedAt` on a nullable
  property emits pointer fields the same way C1 read paths do;
  the resolver's nullability derivation is orthogonal to
  `Statement`.
- **`ResolvedEdgeUnion` in a write-with-projection column** still
  routes to `ErrOutOfC4Scope` — the column type axis defers to
  C5 regardless of whether the query is a read or a write. Same
  sentinel value as C3, per the C3→C4 rename.
- **Non-property parameter on a write** still routes to
  `ErrOutOfC4Scope`. Widening this axis is post-v1 per ADR 0010
  D7.

### 3.4 The C3 property → Go type mapping stands

C4 does not revise the C3 property → Go type table. Reads,
writes, and mixed-statement queries all use the same table; the
Statement axis has no consequence for the type of a projected
column or a bound parameter, only for the transaction routing.
The C3 §5.1 table stands byte-identical at C4.

### 3.5 The C1 `driverOrTx.run` seam signature stands

C4 does not revise the C1 seam signature. `[]*neo4j.Record`
accommodates the write arm's return shape unchanged — an empty
slice for a bare `:exec`, a one-plus slice for a write-with-
projection. The `neo4j.AccessMode` parameter is now dispatched
against by `driverDB.run` (the C1 stub arm is replaced by real
code at C4); `txDB.run` still ignores it. Zero template change
to the seam interface declaration.

---

## 4. The naming kernel — C4 additions

C3's naming kernel (§4 of the C3 spec) stands: method names
verbatim (§4.1), Params fields via the one-mangle rule (§4.2),
Row fields via text-shape analysis on `Column.Name` (§4.3),
package-level exported-identifier collision sweep (§4.4 / §4.6),
entity-naming rules (§4.5), property-field mangle with collision
sentinel (§4.5 Rule 5), `ResolvedList<T>` recursion (§4.7), eager
width sweep (§4.8). C4 adds the cardinality × shape gate rule
(§4.9).

### 4.9 Cardinality × shape gate — Phase A's C4 extension

**Rule 7 (added at C4) — every admitted query's `(Cardinality,
Statement, len(Columns))` triple is one of the four legal
combinations, or the paired sentinel.** Phase A's per-query
admission (§4.9 of the C3 spec) adds one gate before the column-
type sweep and one gate before the parameter-type sweep:

**Cardinality × shape truth table (C4 admissibility):**

| Cardinality | Statement | `len(Columns)` | Outcome |
|---|---|---|---|
| `:one` | Read | 0 | `ErrCardinalityShapeMismatch` (a `:one` read with no `RETURN` is a caller bug; the C3-C4 rule is that `:one`/`:many` projections must have at least one column — sqlc precedent) |
| `:one` | Read | 1+ | Admitted (unchanged from C3) |
| `:one` | Write | 0 | `ErrCardinalityShapeMismatch` (canonical zero-column write — annotate `:exec` instead) |
| `:one` | Write | 1+ | Admitted (write-with-projection, §3.3) |
| `:many` | Read | 0 | `ErrCardinalityShapeMismatch` (same reason as `:one` / Read / 0) |
| `:many` | Read | 1+ | Admitted (unchanged from C3) |
| `:many` | Write | 0 | `ErrCardinalityShapeMismatch` (same reason as `:one` / Write / 0) |
| `:many` | Write | 1+ | Admitted (write-with-projection, §3.3) |
| `:exec` | Read | 0 | Admitted (call-with-no-yield, §2.1) |
| `:exec` | Read | 1+ | `ErrExecOnProjection` (a projection query the author labelled `:exec` — the columns are contract) |
| `:exec` | Write | 0 | Admitted (canonical write path, §3.1) |
| `:exec` | Write | 1+ | `ErrExecOnProjection` (a write-with-`RETURN` labelled `:exec` — the RETURN is contract) |

- **Read + 0-column outcome.** The C1/C2/C3 codepath was
  `ErrOutOfC(n)Scope` naming "query has no projected columns"
  (see the C3 code at `generate.go:534-537`). C4 recategorises
  the same shape under `ErrCardinalityShapeMismatch` because the
  rejection axis is cardinality × shape, not scope-deferral —
  the query would work if the author added a `RETURN` clause or
  changed the cardinality to `:exec`, so the fix is on the
  cardinality × shape axis, not on the "wait for a later stage"
  axis. This is a **sentinel move** (not a widening of
  admissibility): the same fixture shape now fires
  `ErrCardinalityShapeMismatch` at C4 that fired
  `ErrOutOfC3Scope` at C3. The C3 spec's precedent (§4.8 moving
  `INT128` from `ErrOutOfC2Scope` to `ErrUnrepresentableWidth`)
  is the exact same recategorisation shape.
- **Sentinel discipline — two sentinels, not one.**
  `ErrExecOnProjection` and `ErrCardinalityShapeMismatch` are
  distinct constants because the two axes address different
  caller edits. `ErrExecOnProjection` says "your query has
  columns; either drop `:exec` or drop the `RETURN`" —
  discriminating between `:one` and `:many` here is the caller's
  choice, but the two possible fixes are on the caller side;
  gqlc's job is to name the axis. `ErrCardinalityShapeMismatch`
  says "your query has no rows to return; either annotate
  `:exec` or add a `RETURN`" — the two fixes are also on the
  caller side. Merging the two into a single sentinel would
  erase the axis; `errors.Is` consumers who want to gate on
  "is this a `:exec` misuse?" branch differently from
  "is this a `:one` / `:many` on a zero-column write?" — the
  fixes are on different clauses of the query. See §9 for the
  three-way naming defence (grill options: distinct pair,
  single sentinel, three sentinels).
- **Fail-message discipline.**
  `ErrExecOnProjection` names the query name, the cardinality
  (`:exec`), the projected column count, and (if the caller
  wants to see it in the message) the first column's name.
  `ErrCardinalityShapeMismatch` names the query name, the
  cardinality, the statement kind, and the shape
  (`zero-column write` or `zero-column read`). The message is
  the discriminator when the caller reads a stderr line; the
  sentinel is the discriminator when the caller runs
  `errors.Is`.
- **Gate ordering — first-error-wins.** The cardinality × shape
  gate runs before the column-type sweep (§4.9 of C3), so a
  fixture that combines `:exec`-on-projection with an
  `unrepresentable-width` column fires
  `ErrExecOnProjection` (not `ErrUnrepresentableWidth`); the
  caller fixes the cardinality first, and re-running exposes the
  width failure. This is the C0 first-error posture applied to
  the extended sweep; the C3 fail-site ordering (widths before
  scope) extends to (cardinality-shape before widths).
- **`:exec` on `len(Columns) == 0 && Statement == StatementRead`
  is admitted.** A `CALL` procedure with no `YIELD` (Cypher's
  legitimate zero-column read shape) is one legal source of this
  triple; a pure-read query with no `RETURN` (rare — mostly the
  same `CALL` case) is another. Admitting this triple at C4 is
  the honest posture: the `:exec` axis is cardinality (zero
  rows to project), not statement kind (read vs write), and the
  resolver's `Statement` is the axis for cluster routing. A
  `:exec` on a read routes `AccessModeRead`; the emitted body
  is byte-identical to the write `:exec` body except the fourth
  `run` argument.

The truth table above lands as one paired-shape check on
`(Cardinality, Statement, len(Columns))`, added to Phase A ahead
of the column-type sweep. The check runs on every `NamedQuery`
before any column or parameter is inspected; a query that fails
the gate is rejected before its columns' resolved types are
walked. This is Phase A's C4 addition; the C3 C0-source at
`generate.go:528-540` is the site (lines to swap in the new gate;
lines 534-537 lose their "no projected columns" branch in favor
of the extended gate; the `:exec` short-circuit at 528-529 is
subsumed by the new gate).

---

## 5. Emission templates and per-query files — C4 additions

### 5.1 Property → Go type mapping — unchanged

C3's property → Go type table stands byte-identical at C4. Writes
and reads share the same table; the Statement axis has no
consequence for the type of a projected column or a bound
parameter. No new rows.

### 5.2 `models.go` — unchanged

C3's `renderModels` stands byte-identical at C4. Entity structs
are schema-shaped only; writes do not perturb the schema-shape
axis. A schema with only write queries and a schema with only
read queries produce the same `models.go`; the file is a function
of `Schema.Nodes` and `Schema.Edges` alone, not of
`Input.Queries` (ADR 0010 D2 Resolved — models-only adoption is
legal). The C3 iteration order (`LabelSetKey`-order,
`EdgeKey`-triple-lex-order) is unchanged; the C3 import gates
(`fmt`, `time`, `neo4j`, `dbtype`) are unchanged; the C3 width
sweep is unchanged.

### 5.3 Method rendering into `db.go`

The method rendering for `:one` / `:many` reads stands unchanged
from C1 (§5.3 of C1). C4 adds the `:exec` shape and threads the
`access` argument:

**`:exec` method shape:**

```go
// <MethodName> executes the <method-name> query.
//
//   <first-3-lines-of-query-text>
//   [... truncated if the query exceeds 3 lines]
func (q *Queries) <MethodName>(ctx context.Context<param-list>) error {
    _, err := q.db.run(ctx, <queryTextConst>, <paramsMap>, <access>)
    return err
}
```

- **`<param-list>`** — the C1 rule: empty (zero parameters),
  `, <bareParam> <T>` (one parameter, lowercase-initial), or
  `, arg <MethodName>Params` (two-plus).
- **`<queryTextConst>`** — the per-query const name (§5.5):
  `<methodName>QueryText`. Unchanged from C1.
- **`<paramsMap>`** — the `map[string]any` literal or `nil`.
  Zero parameters: `nil` (deliberate — the driver accepts nil
  for no-parameter queries; passing an empty `map[string]any{}`
  is equivalent but noisier). One parameter:
  `map[string]any{"<rawName>": <bareParam>}`. Two-plus:
  `map[string]any{"<rawName1>": arg.<Field1>, ...}`. Unchanged
  from C1.
- **`<access>`** — the fourth `run` argument. `:exec` reads pass
  `neo4j.AccessModeRead`; every other cardinality obeys the
  new §5.5 access-mode rule (`AccessModeWrite` iff
  `Validated.Statement == StatementWrite`, else
  `AccessModeRead`). The `:exec` method has no cardinality
  dispatch of its own beyond this; the body is the same three
  lines regardless of read / write dispatch.
- **No return value** beyond `error`. The row assembly is
  skipped entirely; `_, err := q.db.run(...)` discards the
  `[]*neo4j.Record` uniformly.
- **No doc-comment consequence.** The 3-line quote of the query
  text (§5.3 C1) runs unchanged; the `:exec` method's doc
  comment reads the same as a `:one` / `:many` method's doc
  comment.

**`:one` / `:many` method shape (unchanged from C1/C2/C3)**, with
the sole delta being that the fourth `q.db.run` argument is now
`neo4j.AccessModeRead` iff `Validated.Statement == StatementRead`,
else `neo4j.AccessModeWrite`. The C1 template hardcoded
`AccessModeRead` (verified in `generate.go:1388`); C4 replaces the
hardcode with the dispatch rule. §5.5 gives the exact template.

### 5.4 `querier.go` regeneration — `WriteQuerier` population

C3's empty `WriteQuerier` becomes:

```go
type WriteQuerier interface {
    <WriteMethodName1>(ctx context.Context<param-list-1>) <return-1>
    <WriteMethodName2>(ctx context.Context<param-list-2>) <return-2>
    ...
}
```

- **Order** is `Input.Queries` slice order filtered by
  `Validated.Statement == StatementWrite`. The filter runs after
  Phase B (per-method prepared shape is already computed); the
  emitted lines are one per write method.
- **Return** is `error` for `:exec`, `<T>` / `<Method>Row` for
  `:one`, `[]<T>` / `[]<Method>Row` for `:many` — the same rule
  as the concrete method's return type (§3 of C1, extended here
  with `:exec`'s `error`).
- **`ReadQuerier` population** stays the C1 rule: every method
  with `Validated.Statement == StatementRead`, in
  `Input.Queries` filtered order. Interleaving reads and writes
  in the input is transparent — both interfaces pull from the
  same slice through their respective filters.
- **Zero-write batch** — `WriteQuerier` stays empty (`type
  WriteQuerier interface {}`) — same shape as C3. A read-only
  batch's `querier.go` is byte-identical to the C3 emission
  (assuming no interface-set import change from a `dbtype` or
  `time` addition — the C2 `querierImports` sweep is unchanged).
- **Zero-read batch** — `ReadQuerier` stays empty. Symmetric with
  the C3 rule; the file is emitted with the empty read arm and
  the populated write arm.
- **`Querier` embedding** — `type Querier interface { ReadQuerier;
  WriteQuerier }` — byte-identical to C3.
- **`var _ Querier = (*Queries)(nil)`** — the compile-time
  assertion still fences drift; a `:exec` method emitted on
  `*Queries` without being listed in `WriteQuerier` (or a
  `StatementRead :exec` not listed in `ReadQuerier`) fails to
  compile at the nested-module fence.

### 5.5 The per-source `<name>.cypher.go` file — the C4 method-body arms

C3's per-source file shape stands: query-text const, Params
struct (if two-plus params), Row struct (if two-plus columns),
method with row-assembly body. C4 extends the method-body dispatch
with the `:exec` arm and the access-mode threading rule.

**Access-mode threading rule.** Every method body's `q.db.run`
call takes a fourth argument dispatched on
`Validated.Statement`:

```go
records, err := q.db.run(ctx, <method>QueryText, <paramsMap>, neo4j.AccessModeRead)   // Statement == StatementRead
records, err := q.db.run(ctx, <method>QueryText, <paramsMap>, neo4j.AccessModeWrite)  // Statement == StatementWrite
```

- **The dispatch runs once per emitted method** at generation
  time; the emitted body carries the constant, not a runtime
  branch. `Validated.Statement` is a compile-time input to the
  generator, not a runtime input to the emitted code.
- **`:exec` methods** carry the same rule: `AccessModeRead` for
  a call-with-no-yield reads, `AccessModeWrite` for the
  canonical write shape. The C1 hardcode goes away entirely.

**Per-query method body template — `:exec` (any parameter
count, any Statement kind):**

```go
_, err := q.db.run(ctx, <method>QueryText, <paramsMap>, <access>)
return err
```

- **Three lines** (four counting the closing `}`). No `records`
  local, no arity check, no per-column decode block. The `:exec`
  method body is the minimum-shape method a `q.db.run` seam can
  produce.
- **No `fmt` reference in the emitted body.** The C1 read body
  emits `fmt.Errorf` in the decode wrapper; a `:exec` body has
  no decode. But the file's `fmt` import may still be present
  if any other method in the file emits a decode wrapper (the
  C1 rule stays: `fmt` is imported iff any method in the file
  uses it, and a mixed file — one `:one` read + one `:exec`
  write — imports `fmt` for the read's decode wrapper).
- **No `errors` reference.** `:exec` methods do not check for
  `ErrNoRows` / `ErrMultipleResults`. The file's `errors` import
  is C0's rule (only if `ErrNoRows` / `ErrMultipleResults` are
  emitted in `db.go`, and even then only at `db.go`'s import
  site; `<name>.cypher.go` does not import `errors` per §5.5 of
  C1).
- **Zero-parameter `:exec`.** `<paramsMap>` is `nil`; the emitted
  body: `_, err := q.db.run(ctx, truncatePeopleQueryText, nil,
  neo4j.AccessModeWrite); return err`.
- **One-parameter `:exec`.** `<paramsMap>` is
  `map[string]any{"id": id}`; the emitted body:
  `_, err := q.db.run(ctx, removePersonQueryText,
  map[string]any{"id": id}, neo4j.AccessModeWrite); return err`.
- **Two-plus-parameter `:exec`.** `<paramsMap>` is
  `map[string]any{"pid": arg.Pid, "oid": arg.Oid}`; the emitted
  body: `_, err := q.db.run(ctx, removeBothQueryText,
  map[string]any{"pid": arg.Pid, "oid": arg.Oid},
  neo4j.AccessModeWrite); return err`.

**Per-query method body template — `:one` / `:many` write-with-
projection:**

Identical to the C1 / C2 / C3 template (§5.5 of C1 for the
scalar/property case; §5.5 of C2 for the entity case; §5.5 of C3
for the temporal / list / scalar / unknown cases) with the sole
delta that the fourth `q.db.run` argument is
`neo4j.AccessModeWrite`. No structural template change; the row
assembly runs the same code paths, the same helper decodes, the
same nullability arm.

**Example — `:exec`, one parameter, write:**

```go
// RemovePerson executes the remove-person query.
//
//   // name: RemovePerson :exec
//   MATCH (p:Person) WHERE p.id = $id DELETE p
func (q *Queries) RemovePerson(ctx context.Context, id int64) error {
    _, err := q.db.run(ctx, removePersonQueryText, map[string]any{"id": id}, neo4j.AccessModeWrite)
    return err
}
```

**Example — `:one` write-with-projection, one column, non-nullable
entity:**

```go
// CreatePerson executes the create-person query.
//
//   // name: CreatePerson :one
//   CREATE (p:Person {id: $id, name: $name}) RETURN p
func (q *Queries) CreatePerson(ctx context.Context, arg CreatePersonParams) (Person, error) {
    records, err := q.db.run(ctx, createPersonQueryText, map[string]any{"id": arg.Id, "name": arg.Name}, neo4j.AccessModeWrite)
    if err != nil {
        return Person{}, err
    }
    if len(records) == 0 {
        return Person{}, ErrNoRows
    }
    if len(records) > 1 {
        return Person{}, ErrMultipleResults
    }
    value, isNil, err := neo4j.GetRecordValue[dbtype.Node](records[0], "p")
    if err != nil {
        return Person{}, fmt.Errorf("CreatePerson: decode column %q: %w", "p", err)
    }
    if isNil {
        return Person{}, fmt.Errorf("CreatePerson: column %q is non-nullable but arrived null", "p")
    }
    return decodePerson(value)
}
```

- **`AccessModeWrite`** replaces the C3 template's
  `AccessModeRead`. All else is byte-identical to a C2 entity-
  projected `:one` read.
- **`ErrNoRows` / `ErrMultipleResults`** apply. A `CREATE
  ... RETURN p` that somehow produces zero rows (an idempotent
  merge pattern, an `OPTIONAL MATCH` interaction, etc.) fires
  `ErrNoRows`; the caller can either annotate `:many` or
  restructure the query.

**Example — `:many` write-with-projection:**

```go
func (q *Queries) CreateBatch(ctx context.Context, arg CreateBatchParams) ([]Person, error) {
    records, err := q.db.run(ctx, createBatchQueryText, map[string]any{...}, neo4j.AccessModeWrite)
    if err != nil {
        return nil, err
    }
    out := make([]Person, 0, len(records))
    for _, record := range records {
        value, isNil, err := neo4j.GetRecordValue[dbtype.Node](record, "p")
        if err != nil {
            return nil, fmt.Errorf("CreateBatch: decode column %q: %w", "p", err)
        }
        if isNil {
            return nil, fmt.Errorf("CreateBatch: column %q is non-nullable but arrived null", "p")
        }
        entity, err := decodePerson(value)
        if err != nil {
            return nil, err
        }
        out = append(out, entity)
    }
    return out, nil
}
```

- Byte-identical to a C2 `:many` entity read except the fourth
  `q.db.run` argument.

**Owner directive (2026-07-11) — lint-clean parity.** Every
emitted `models.go` and `<name>.cypher.go` must lint-clean under
gqlc's `.golangci.yml` (C1 §6.6, C2 §5.5, C3 §5.5's directive
extends). The C4 emissions are structurally identical to C3 modulo
the `:exec` three-line body and the write-access-mode argument,
and the `.golangci.yml` posture (no `//nolint` directives at file
heads) still holds. `errorlint` wrapping discipline: the `:exec`
body's `return err` is a bare propagation (idiomatic; no wrap),
which passes `errorlint` because there is no format-string call
to consider. `stylecheck` posture: the `:exec` body reads as a
minimum-idiom function; no comment fields to punctuate. `errcheck`
and `ineffassign` pass by construction (the discarded first return
value is intentional; `_` is the idiomatic discard).

### 5.6 The `driverDB.run` write-arm body — the C4 revision

C1 committed the read arm live and the write arm stubbed. The
`db.go` template's `case neo4j.AccessModeWrite:` branch moves from
`return nil, fmt.Errorf("gqlc: write path not implemented")` to the
real `neo4j.ExecuteWrite` call — structurally symmetric with the
read arm:

**New body** (`driverDB.run`, C4 revision — read arm unchanged):

```go
func (d driverDB) run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error) {
    session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: access})
    defer session.Close(ctx)
    switch access {
    case neo4j.AccessModeRead:
        return neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) ([]*neo4j.Record, error) {
            result, err := tx.Run(ctx, cypher, params)
            if err != nil {
                return nil, err
            }
            return result.Collect(ctx)
        })
    case neo4j.AccessModeWrite:
        return neo4j.ExecuteWrite(ctx, session, func(tx neo4j.ManagedTransaction) ([]*neo4j.Record, error) {
            result, err := tx.Run(ctx, cypher, params)
            if err != nil {
                return nil, err
            }
            return result.Collect(ctx)
        })
    default:
        return nil, fmt.Errorf("gqlc: unknown access mode %v", access)
    }
}
```

- **`neo4j.ExecuteWrite[T=[]*neo4j.Record]`.** The driver's
  `ExecuteWrite` is the top-level helper `func ExecuteWrite[T
  any](ctx, session SessionWithContext, work
  ManagedTransactionWorkT[T], configurers ...func(*TransactionConfig))
  (T, error)` — the same shape as `ExecuteRead`. Verified against
  `pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5@v5.28.4/neo4j`,
  2026-07-11. C4 instantiates `T` as `[]*neo4j.Record` for signature
  parity with the read arm.
- **Callback shape is byte-identical to the read arm.** `tx.Run`
  + `.Collect(ctx)` inside the transaction; the returned buffered
  slice survives close (`Record` is self-contained per the C1
  §5.6 defence). The write arm buffers records the same way the
  read arm does, so a write-with-projection lands in the caller
  the same way a read does — one uniform seam.
- **`txDB.run` unchanged.** The caller-owned managed transaction
  path already ignores `access`; C4 makes no change (`t.tx.Run`
  + `.Collect`). The caller opened the transaction with their own
  access mode; the seam parameter is redundant on that path.
- **`default` arm unchanged.** A third `AccessMode` value stays a
  driver-internal concern; the wrapped error stands. Same
  defensive posture as C1 (unreachable in practice; robust on
  driver upgrades).
- **`fmt` import unchanged.** `db.go` still imports `fmt` for the
  `default` arm's `fmt.Errorf`. C4 does not delete the "write
  path not implemented" line — it replaces the line's body — so
  the `fmt` import is still needed.

**Golden regeneration.** Every C0 / C1 / C2 / C3 golden `db.go`
carries the C1 stub write-arm body. C4 regenerates every `db.go`
under the new template — the byte diff is the swap of the
`return nil, fmt.Errorf(...)` line for the seven-line
`neo4j.ExecuteWrite` block, no other change to `db.go`. The
`-update` flag (C0 §6.4) rewrites the goldens; the compile fence
proves the revised shape type-checks. The read arm is
byte-identical across the C1 → C4 diff; the write arm is the
only change.

### 5.7 Cross-query package-level identifier collision sweep — unchanged

C1's sweep stands unchanged at C4: any two generated top-level
identifiers colliding → `ErrIdentifierCollision`. The four
identifier sources (entity struct names, method names,
`<Method>Params`, `<Method>Row`) are unchanged. A `:exec` method
adds one method-name entry and, if it has two-plus parameters,
one `<Method>Params` entry (no `<Method>Row` entry — the `:exec`
short-circuit). A write-with-projection method adds all three
(or two, for a single-column) — same as a read method. C5 hardens
the sweep further as decode-helper names enter the exported
surface (ADR 0010 D7); C4's coverage is the union of every
generated top-level identifier a C4-admissible batch can
produce, including the new `:exec` and write-with-projection
methods.

---

## 6. The golden harness — C4 revision

C0 §6's harness stands: the `test/data/codegen/{valid,invalid}`
layout, the nested Go module, the `manifest.json` shape, the
`-update` flag, the testify suites, the compile fence. C3 §6.6's
lint parity applies transitively. C4 revises the fixture set only,
not the harness code.

### 6.1 Fixture strategy

C3's discipline stands (fixture-per-capability, one schema per
fixture). C4 adds valid fixtures for each capability slice of §5
plus a small set of negative fixtures for each new sentinel /
retirement / rename. The nested module's `go.mod` (pin
`neo4j-go-driver/v5 v5.28.4`) does not change — the C4 emissions
add no new imports. Every existing C0 / C1 / C2 / C3 valid
fixture's `db.go` regenerates because the write arm changes;
their `<name>.cypher.go` and `models.go` files stay byte-
identical (no read query is perturbed by the C4 changes).

**Existing C0 / C1 / C2 / C3 valid fixtures whose `db.go`
regenerates** — every one. The C1 stub write-arm becomes the C4
real write-arm; the `-update` run rewrites every `db.go` golden.
Read arm bytes and every other file (`<name>.cypher.go`,
`models.go`, `querier.go` if the batch is read-only) stay
byte-identical.

### 6.2 C4 valid fixtures

Under `test/data/codegen/valid/`, each new directory holds a
`schema.gql`, one or more `.cypher` files, a `manifest.json`, and
a `golden/` subdirectory with the complete generated package:

| Fixture | Coverage |
|---|---|
| `write_exec_delete` | `MATCH (p:Person) WHERE p.id = $id DELETE p :exec` — canonical single-parameter `:exec` write. Reincarnates the C3 `out_of_c3_scope_exec` fixture as the honest positive case. Exercises `WriteQuerier` population, `AccessModeWrite`, three-line `:exec` body. |
| `write_exec_zero_param` | `MATCH (p:Person) DELETE p :exec` — zero-parameter write. Exercises the `<paramsMap> == nil` arm and the truncate-style body. |
| `write_exec_two_params` | Two-parameter write. Exercises `<Method>Params` struct emission on the write side. |
| `write_exec_read` | `CALL db.labels() :exec` (or a legal `CALL` with no `YIELD`). Exercises the `:exec` on `StatementRead` arm — `AccessModeRead` on a `:exec` body. Legitimate Cypher shape. |
| `write_one_projection_entity` | `CREATE (p:Person {id: $id, name: $name}) RETURN p :one` — write-with-projection returning an entity. Exercises the C2 entity decode helper on a write path, `AccessModeWrite`, `ErrNoRows` / `ErrMultipleResults` sentinels remain in `db.go`. |
| `write_one_projection_property` | `MATCH (p:Person {id: $id}) SET p.name = $name RETURN p.name AS name :one` — write-with-projection returning a scalar property. Exercises the C1 single-column body on a write path. |
| `write_many_projection_entity` | `UNWIND $names AS n CREATE (p:Person {name: n}) RETURN p :many` — write-with-projection returning many entities. Exercises the C2 `:many` entity path on a write path. NOTE: `$names :: list<STRING>` is a non-property parameter — this fixture would land under `ErrOutOfC4Scope` at C4; the alternative body is `MATCH (p:Person) WHERE p.age > $minAge SET p.checked = true RETURN p :many` (a write that touches many rows). Fixture author picks the property-only variant. |
| `write_many_projection_property` | A `:many` write-with-projection returning a scalar column, mirroring `write_one_projection_property`. |
| `mixed_read_write_batch` | One `.cypher` file with two queries: `:many` read + `:exec` write. Exercises `ReadQuerier` + `WriteQuerier` co-population, `querier.go`'s dual-block emission, per-source file with both cardinality shapes. |
| `write_exec_nullable_param` | `MATCH (p:Person {id: $id}) SET p.optionalTag = $tag :exec` with `$tag :: STRING` (nullable). Exercises pointer-typed parameter on the write path — the C1 nullable rule stands unchanged. |
| `write_exec_float32_param` | `MATCH (p:Person) WHERE p.height >= $minHeight SET p.tall = true :exec` with `$minHeight :: FLOAT32`. Exercises the C3 FLOAT32 encode-widen on the write path. |
| `write_exec_temporal_param` | `MATCH (p:Person) WHERE p.updatedAt < $since SET p.stale = true :exec` with `$since :: TIMESTAMP` (`time.Time` encoding). Exercises the C3 temporal parameter binding on a write path. |
| `write_only_batch` | A `.cypher` file with only write queries; `ReadQuerier` stays empty, `WriteQuerier` is populated. Exercises the empty-read-arm shape mirroring the read-only-batch fixtures. |

Thirteen new valid fixtures. Each is one `schema.gql`, at least
one `.cypher` file, a `manifest.json`, and a `golden/` tree. The
`golden/` trees compile under the C0 compile fence against
`neo4j-go-driver/v5 v5.28.4` — every `neo4j.ExecuteWrite`,
`AccessModeWrite`, and `:exec` three-line body type-checks against
the pinned driver.

### 6.3 Schema fixture text — illustrative

`test/data/codegen/valid/write_exec_delete/schema.gql`:

```gql
CREATE PROPERTY GRAPH TYPE WriteExecDelete AS {
    (:Person {
        id :: INT64 NOT NULL
    })
}
```

Paired query file
`test/data/codegen/valid/write_exec_delete/queries.cypher`:

```cypher
// name: RemovePerson :exec
MATCH (p:Person) WHERE p.id = $id DELETE p
```

Resolved: `Columns = []`, `Parameters = [{Name: "id", Type:
ResolvedProperty{INT64, Nullable: false}}]`, `Statement =
StatementWrite`, `Cardinality = CardinalityExec`. Phase Z:
`Person` node type with `Id int64` property emits fine. Phase A
(cardinality × shape gate): `(Exec, Write, 0)` — admitted, no
sentinel. Phase B: method name `RemovePerson` derives verbatim,
one-parameter form, no Row struct, no Row-field derivation.
Emitted method: three-line `:exec` body with `AccessModeWrite`;
`WriteQuerier` gains one line; `db.go` emits the full C4 write
arm; `models.go` carries `Person` with `Id int64` and its decode
helper (unused by this fixture, but emitted unconditionally per
C2's models-only invariant).

`test/data/codegen/valid/write_one_projection_entity/schema.gql`:

```gql
CREATE PROPERTY GRAPH TYPE WriteOneProjectionEntity AS {
    (:Person {
        id   :: INT64 NOT NULL,
        name :: STRING NOT NULL
    })
}
```

`queries.cypher`:

```cypher
// name: CreatePerson :one
CREATE (p:Person {id: $id, name: $name}) RETURN p
```

Resolved: `Columns = [{Name: "p", Type: ResolvedNode{Labels:
"Person", Nullable: false}}]`, `Parameters = [{Name: "id",
Type: ResolvedProperty{INT64, Nullable: false}}, {Name: "name",
Type: ResolvedProperty{STRING, Nullable: false}}]`, `Statement =
StatementWrite`, `Cardinality = CardinalityOne`. Phase A
(cardinality × shape gate): `(One, Write, 1)` — admitted. Phase B:
method name `CreatePerson`, two-parameter Params struct, single-
column `:one` bare-value return type `Person`. Emitted method:
C2 `:one` entity body with `AccessModeWrite`; `WriteQuerier`
gains one line (`CreatePerson(ctx context.Context, arg
CreatePersonParams) (Person, error)`); `db.go` emits the full C4
write arm; `models.go` emits `Person` + `decodePerson`.

`test/data/codegen/valid/mixed_read_write_batch/queries.cypher`:

```cypher
// name: GetPerson :one
MATCH (p:Person) WHERE p.id = $id RETURN p.name AS name

// name: RemovePerson :exec
MATCH (p:Person) WHERE p.id = $id DELETE p
```

Resolved: two queries, one `StatementRead :one`, one
`StatementWrite :exec`. `ReadQuerier` lists `GetPerson`;
`WriteQuerier` lists `RemovePerson`; `Querier` embeds both;
`querier.go`'s two interface blocks both populate. `db.go` emits
one C4 write arm; the per-source file emits both methods with
appropriate access modes.

### 6.4 C4 invalid fixtures — the new + renamed set

Added under `test/data/codegen/invalid/`:

| Fixture | Sentinel | Coverage |
|---|---|---|
| `exec_on_projection_read` | `ErrExecOnProjection` | `RETURN 1 AS n :exec` — `:exec` on a column-producing read. Exercises the `(Exec, Read, 1)` cell of the cardinality × shape table. |
| `exec_on_projection_write` | `ErrExecOnProjection` | `CREATE (p:Person) RETURN p :exec` — `:exec` on a write-with-`RETURN`. Exercises the `(Exec, Write, 1)` cell. |
| `cardinality_shape_zero_column_read_one` | `ErrCardinalityShapeMismatch` | `MATCH (p:Person) :one` (no `RETURN`). Reincarnates the C3 "no projected columns" case with the C4 sentinel. Exercises `(One, Read, 0)` cell. |
| `cardinality_shape_zero_column_write_one` | `ErrCardinalityShapeMismatch` | `MATCH (p:Person) WHERE p.id = $id DELETE p :one` — a write labelled `:one`. Exercises `(One, Write, 0)` cell. Fix: `:exec`. |
| `cardinality_shape_zero_column_write_many` | `ErrCardinalityShapeMismatch` | Same schema and query as above, annotated `:many`. Exercises `(Many, Write, 0)` cell. |
| `out_of_c4_scope_edge_union` | `ErrOutOfC4Scope` | Query projecting a `ResolvedEdgeUnion` column — deferred to C5. Renamed from `out_of_c3_scope_edge_union`. |
| `out_of_c4_scope_non_property_parameter` | `ErrOutOfC4Scope` | `$p :: ResolvedNode` — non-property parameter, still post-v1. Renamed from `out_of_c3_scope_non_property_parameter`. |

Seven invalid fixtures — five new (two for `ErrExecOnProjection`,
three for `ErrCardinalityShapeMismatch`) plus two renamed from
`out_of_c3_scope_*`. The retired `out_of_c3_scope_exec` fixture
(the C3 sentinel for the C0-C3 catchment) reappears in `valid/`
as `write_exec_delete` (the honest positive case). The C4
retirement is a clean cut: C3's exec-catchment retires because
writes are now in-scope.

The C0 / C1 / C2 / C3 invalid fixtures whose sentinel is unchanged
(`invalid_package_name`, `duplicate_query_name`,
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
`unrepresentable_width_float128_list_column`) stay unchanged.

### 6.5 Determinism — C4 additions

C0's `TestDoubleRun` runs unchanged. C4's kernel adds no new
ordered surfaces: the cardinality × shape gate runs per-query in
`Input.Queries` slice order; the `WriteQuerier` population walk
filters the same slice by `Validated.Statement`. The `:exec`
method rendering is a template short-circuit — one branch on
`Cardinality`, no iteration. The `driverDB.run` write arm is a
constant template. Every ordered surface is either the C3
`Input.Queries` order or a deterministic filter of it.

### 6.6 Non-obvious harness invariants — C4 additions

C3's §6.6 invariants stand. C4 adds:

- **Every valid fixture's `golden/db.go` compiles under the C4
  write-arm body.** `test/data/codegen/go.mod` still pins
  `neo4j-go-driver/v5 v5.28.4`; the emitted `neo4j.ExecuteWrite`
  call type-checks against the pinned driver. `SessionWithContext`
  is the driver's session type; the `ExecuteWrite[T]` generic
  instantiates with `[]*neo4j.Record` symmetrically with
  `ExecuteRead[T]`. Any driver-side signature drift at a version
  bump would fail the compile fence at the version bump.
- **Every valid fixture's `golden/db.go` regenerates on the
  C3→C4 diff.** The `-update` flag rewrites the write-arm body;
  the byte diff is the seven-line `neo4j.ExecuteWrite` block
  replacing the C3 stub. The compile fence catches any
  template regression at the version bump.
- **`:exec` method bodies pass `errcheck` / `errorlint` /
  `stylecheck`** — the three-line body is idiomatic. The
  `_, err := q.db.run(...); return err` shape has no
  format-string wrap for `errorlint` to inspect and no naked
  return for `stylecheck` to flag. The C2 §5.5 lint parity
  directive extends transitively.
- **Owner directive (C1 §6.6, C2 §5.5, C3 §5.5, 2026-07-11)
  extends transitively.** The `errorlint` + `stylecheck` posture
  holds: every `fmt.Errorf` in §5.5 (`AccessModeWrite` mode,
  entity decode wrapper for write-with-projection) uses `%w` for
  wrap, lowercase prefix, no ending punctuation. The `:exec`
  three-line body is emitted at the standard method-body indent;
  `gofmt` normalises whitespace on the way out of
  `format.Source`, so the golden bytes are reproducible.

---

## 7. C4 capability scope — what emits

**In scope:** an `Input` whose:

- `Schema.Nodes` and `Schema.Edges` produce entity struct names via
  Rules 1–4 without failure (C2 invariant), and every property on
  every entity has a representable width (Rule 6 at C3 §4.8).
- Every `NamedQuery` still satisfies C3's admission with the
  following widening: `Cardinality` may now be `CardinalityExec`
  in addition to `CardinalityOne` / `CardinalityMany`, and the
  `(Cardinality, Statement, len(Columns))` triple satisfies the
  §4.9 truth table.
- `Validated.Statement` may now be `StatementWrite`; the emitted
  method routes through `AccessModeWrite`. `StatementRead` still
  routes through `AccessModeRead`. Both statement kinds coexist
  in the same package deterministically.

**Out of scope, routed to the appropriate sentinel:**

| Construct                                                    | Sentinel                          | Stage owner |
|--------------------------------------------------------------|-----------------------------------|-------------|
| `ResolvedEdgeUnion` column (any cardinality, any statement)  | `ErrOutOfC4Scope`                 | C5          |
| Non-property parameter (whole node/edge, temporal literal, etc.) | `ErrOutOfC4Scope`             | Post-v1     |
| Query text containing a Go raw-string-hostile backtick       | `ErrOutOfC4Scope`                 | C4-or-later |
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
| Two emitted top-level identifiers colliding (incl. entity)   | `ErrIdentifierCollision`          | C5 hardens  |

**Silently accepted (not routed anywhere):**

- Empty `Schema.Nodes` and `Schema.Edges` (unchanged from C3).
- Schema node type or edge type with zero properties (unchanged).
- `Validated.Distinct == true` — unchanged from C1.
- `Validated.Columns[i].GroupingKey` — unchanged from C1.
- Comments in the query text — unchanged from ADR 0005.
- `list<list<...<unknown>>>` — the `any` fallback propagates
  through the recursion (C3 §7).
- `RETURN null AS n :one` — the `any` return is legal-but-
  pointless (C3 §7).
- A `:exec` on a `StatementRead` query with zero columns (a
  `CALL` with no `YIELD` — legitimate Cypher shape).
- A write-with-projection with a nullable column (the resolver
  types nullability from R4 flow-typing; the emitted body decodes
  through the C1 pointer-field rule).
- A batch with only writes (`ReadQuerier` stays empty; the
  `querier.go` shape mirrors the C3 read-only case with the arm
  flipped).

**The C1 / C2 / C3 shape stands unchanged** for anything C4 does
not touch: package-name derivation (C0 §5.1), generated-file
header (C0 §5.2), `Queries` handle constructors, `driverOrTx`
interface shape, `txDB` behaviour, the sentinel-set discipline
(with the C4 additions), the double-run determinism test, the
compile fence, the entity-naming rules, the property-field mangle
rule, the package-level exported-identifier sweep, the property →
Go type table, the `resolvedListGoType` recursion, the eager
width sweep.

---

## 8. Compile fence (unchanged)

C0 `just test-codegen-fence` (`cd test/data/codegen && go build
./... && go vet ./...`) covers C4's emissions without change: the
nested module builds every fixture's `golden/` tree, so every new
`neo4j.ExecuteWrite` call, every `AccessModeWrite` argument, and
every `:exec` three-line body type-checks against the pinned
driver. Failure modes:

- **A template regression in `db.go` write-arm body.** The fence
  fails with the standard Go compiler error naming the file and
  line — same diagnostic quality as C1/C2/C3.
- **A `ExecuteWrite` / `SessionWithContext` drift.** Bumping
  `neo4j-go-driver/v5` may reshape the signature (e.g., a v6
  variadic-configurer change); the fence catches at the version
  bump. The D7 standing instruction directs re-verification at
  each stage spec cycle, honored above (2026-07-11).
- **Unused imports.** The `db.go` `fmt` import is still needed
  (the `default` arm). The per-source file's `fmt` import
  survives only when at least one method emits a decode wrapper
  — a write-only file with only `:exec` bodies does not import
  `fmt` (no decode wrapper is emitted). The C0 emission walk's
  `fmt`-import gate widens to check "any method in the file
  emits a decode wrapper", not "any method exists". `go vet`
  catches drift at the version bump.

C4 does not add a second fence recipe. C2 §5.5's lint parity
directive extends transitively to the C4 emissions; if CI runs
`golangci-lint` against the nested module, this is enforced
automatically.

---

## 9. Sentinel set delta — the C4 view

C3's thirteen sentinels stand at C4 with one rename and two
additions. C4 renames `ErrOutOfC3Scope` → `ErrOutOfC4Scope` for
the same reason C3 renamed from C2 (per-stage rename discipline,
§9 of C3 spec defence extends). C4 adds `ErrExecOnProjection` and
`ErrCardinalityShapeMismatch` for the two axes of cardinality ×
shape rejection — **hard errors, not scope errors**: no future
stage will "fix" `:exec` on a projection by silently discarding
rows; the sentinels signal permanent shape violations, distinct
from `ErrOutOfC4Scope`'s "wait for stage X" semantics.

**New sentinels at C4:**

```go
// ErrOutOfC4Scope is returned when a C4-admissible input carries
// a construct C4 does not project: a column whose resolved type is
// ResolvedEdgeUnion (C5), a non-property parameter (post-v1), or a
// query text carrying a raw-string-hostile backtick. Category-
// grained per C0's precedent; C5 retires the ResolvedEdgeUnion sub-
// case as it lands. Renamed from ErrOutOfC3Scope at C4 — :exec
// cardinality retires from the catchment (writes are now in-scope,
// with the cardinality × shape rejection axis carved out to the two
// new sentinels below).
var ErrOutOfC4Scope = errors.New("out of C4 scope")

// ErrExecOnProjection is returned when a query annotated :exec has
// at least one projected column (len(Validated.Columns) > 0). The
// caller either drops the :exec annotation (annotate :one or :many
// per the desired arity) or drops the RETURN clause (annotate :exec
// on the pure write). sqlc silently allows :exec on a SELECT,
// discarding rows; we refuse (ADR 0010 D1 Resolved: reject-don't-
// guess). The fail-message names the query, the cardinality
// (:exec), the projected column count, and the first column's
// name. Introduced at C4.
var ErrExecOnProjection = errors.New("exec cardinality on projection query")

// ErrCardinalityShapeMismatch is returned when a query annotated
// :one or :many has zero projected columns
// (len(Validated.Columns) == 0). Zero-column reads and zero-column
// writes both flag: the caller either annotates :exec (if no rows
// are wanted) or adds a RETURN clause (if rows are wanted). The
// fail-message names the query, the cardinality (:one or :many),
// the statement kind (read or write), and the shape ("zero-column
// read" or "zero-column write"). Distinct from ErrExecOnProjection:
// the two sentinels address different query edits (annotation vs
// clause). Introduced at C4.
var ErrCardinalityShapeMismatch = errors.New("cardinality-shape mismatch")
```

**Retired at C4:** `ErrOutOfC3Scope` — the constant is dropped
from the package, the fixtures rename to `out_of_c4_scope_*`, and
the `sentinelByName` map's entry renames. The retirement is a
clean cut: no `//nolint:staticcheck` for a lingering alias, no
deprecation window.

**Naming defence — `ErrExecOnProjection`, distinct from
`ErrCardinalityShapeMismatch`.** The two sentinels encode
different semantics: `errors.Is(err, ErrExecOnProjection)` says
"your query has rows-to-return the annotation says are
discarded", `errors.Is(err, ErrCardinalityShapeMismatch)` says
"your annotation demands rows the query has no way to produce".
A consumer library gating on "is my query annotated wrong for
the shape it has?" branches on the first when the shape is a
column-producing query and the annotation is `:exec`, on the
second when the shape is a zero-column query and the annotation
is `:one` or `:many`. Merging the two into one sentinel would
erase the axis; the fix in each case is on a different clause of
the query (the annotation on the `:exec`-on-projection case, the
`RETURN` clause on the zero-column-`:one`/`:many` case).
Rejected: name both `ErrCardinalityShapeMismatch` and use the
message to discriminate — the naming loses the grep-across-source
audit affordance the C3 spec's §9 defends. Rejected: name one
`ErrCardinalityAnnotationConflict` — too verbose, and the "shape"
axis is the one the message discriminates on, not the "annotation"
axis.

**Naming defence — `ErrOutOfC4Scope`, per-stage rename on its
own merits.** C3's §9 defence extends verbatim: the failing
surface is textually different at every stage boundary,
`errors.Is` consumers who branched on `ErrOutOfC3Scope` break at
C4 — this is desirable, they were claiming knowledge of C3's
scope, and C4 has revised what "out of scope" means (writes are
now in). Grill options (a) freeze, (b) neutral, (c) per-stage:
still picked (c). The `staged rename → staging observable at the
error site` axis holds.

**Rejected — collapsing `ErrExecOnProjection` and
`ErrCardinalityShapeMismatch` into one sentinel.** The C3
precedent for two distinct sentinels
(`ErrUnrepresentableWidth` vs `ErrOutOfC3Scope`) argues by
analogy: two distinct axes get two distinct sentinels. The
cardinality × shape axis has two orthogonal failure modes
(annotation-implies-more-than-shape-produces vs
annotation-implies-less-than-shape-produces); collapsing them
would erase the discriminator that tells the caller which clause
to edit. `errors.Is` consumers get one sentinel per axis.

**Rejected — three sentinels: `ErrExecOnProjection`,
`ErrOneOnZeroColumnWrite`, `ErrManyOnZeroColumnWrite`.** The two
`:one` / `:many` cases have the same fix (either annotate `:exec`
or add a `RETURN`); one sentinel with a discriminating fail-
message names both. A third sentinel for `ErrOneOrManyOnZero`
adds nothing over `ErrCardinalityShapeMismatch`. Rejected also:
one sentinel that pairs `:one` / `:many` / `:exec` all as
"cardinality mismatch" — the axes are genuinely orthogonal, and
merging them erases the caller-visible discriminator. The
symmetric zero-column read case (`:one` / `:many` on a read with
no `RETURN`) shares the sentinel with the write case because the
fix is identical: add a `RETURN` clause or change the
cardinality to `:exec`. Grill option "distinct read vs write
sentinels for zero-column" rejected: the axis is on
`len(Columns)`, not on `Statement`.

**Rejected — `ErrExecOnColumns` (shorter name).** The word
"projection" is what the ValidatedQuery documentation uses (§3.3
of ADR 0010 D3), and the error-message discipline is to reflect
the resolver's vocabulary at the fail-site. "Projection" also
disambiguates from "columns" in the Row-struct-name sense.

**Closed set for the C4 sweep.** `allSentinels` at C4:

```go
var allSentinels = []error{
    ErrInvalidPackageName,        // C0
    ErrDuplicateSourceFile,       // C0
    ErrDuplicateQueryName,        // C0
    ErrInvalidCardinality,        // C0
    ErrOutOfC4Scope,              // C4 (renamed from ErrOutOfC3Scope)
    ErrParamNameCollision,        // C1
    ErrRowFieldCollision,         // C1
    ErrAliasRequired,             // C1
    ErrIdentifierCollision,       // C1
    ErrInvalidEntityName,         // C2
    ErrUnnamedMultiLabelType,     // C2
    ErrPropertyFieldCollision,    // C2
    ErrUnrepresentableWidth,      // C3
    ErrExecOnProjection,          // C4
    ErrCardinalityShapeMismatch,  // C4
}
```

Fifteen sentinels. `ErrFormatFailure` stays excluded (C0 §9.2
rationale unchanged). Every C4 member has at least one negative
fixture (§6.4); the reachability sweep is C0's
`TestSentinelReachability` unchanged.

---

## 10. Out-of-scope table

Every downstream capability C4 does not deliver, with the stage
that owns it. Read as ADR 0010 D7 unpacked to the C4-vs-later
boundary (C3's version tightens as C4's slice retires the writes
+ `:exec` + cardinality × shape axis):

| Capability                                          | Stage owner |
|-----------------------------------------------------|-------------|
| Raw-string-hostile query text (backtick escape / fallback) | C4-or-later (deliberately deferred; not on any bead) |
| `edgeUnion` sealed interfaces + `//sumtype:decl`    | C5          |
| List-of-edgeUnion column recursion                   | C5          |
| Package-level collision sweep hardening (decode-helper names as identifier sources, if C5 promotes them exported) | C5 |
| Non-property parameters (whole node, whole edge, temporal literal, scalar literal, list, unknown) | Post-v1 |
| Version-stamp polish (`-ldflags -X` wiring)         | C6          |
| Session-config polish (transaction timeouts, metadata) | C6       |
| `gqlc-0aa` re-scope against D4's no-runtime-package decision | C6 |
| `:iter` streaming cardinality (fourth enum value)   | `gqlc-1a5` (post-v1) |
| Configuration file (`gqlc.yaml` analogue), CLI     | future config effort |
| Disk writes, out-dir sync (stale deletion)          | future CLI effort |

Rows above the `gqlc-1a5` line are staged by ADR 0010 D7; the
last two are ADR 0010 D6 futures.

---

## 11. Definition of done for C4 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2)
is out of scope of this document. The spec is done when:

1. This file exists at `docs/specs/codegen-stage-c4.md`, committed
   on branch `codegen-c4-spec`.
2. §3 pins the C4 emitted surface additions (`:exec` method
   signatures, populated `WriteQuerier`, write-with-projection
   `:one` / `:many` on writes) and confirms the unchanged property
   → Go type table and the unchanged C1 seam signature.
3. §4 gives the new naming-kernel rule — the cardinality × shape
   gate (§4.9) — and defends the two new sentinels' distinct
   axes on ADR 0010 D1 Resolved's language.
4. §5 gives the emission templates: the unchanged property → Go
   type table (§5.1), the unchanged `models.go` (§5.2), the
   `:exec` and access-mode-threading additions in `db.go` (§5.3),
   the populated `WriteQuerier` in `querier.go` (§5.4), the C4
   method-body arms including the `:exec` three-line body and
   write-with-projection (§5.5), the real `driverDB.run` write
   arm (§5.6), the unchanged cross-query collision sweep (§5.7).
5. §9 names and defends the two new sentinels
   (`ErrExecOnProjection`, `ErrCardinalityShapeMismatch`) and the
   rename (`ErrOutOfC3Scope` → `ErrOutOfC4Scope`); confirms the
   closed set of fifteen.
6. §6 designs the fixture set: the thirteen valid fixtures (§6.2),
   the seven invalid fixtures (§6.4), the retirement of
   `out_of_c3_scope_exec` into `write_exec_delete`, the two
   renames (`out_of_c3_scope_edge_union` →
   `out_of_c4_scope_edge_union`, `out_of_c3_scope_non_property_parameter`
   → `out_of_c4_scope_non_property_parameter`), the fixture-per-
   capability discipline.
7. §7 states the C4 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel each construct
   routes to and the stage that owns the next widening.
8. §8 confirms the C0 compile fence covers C4 emissions without
   change; §6.6 flags the linting-parity owner directive
   extending transitively.
9. §12 gives the fixture-count summary.
10. §10 enumerates every downstream capability with its stage
    owner.
11. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer);
every blocker he raises is fixed on this same branch before the
branch merges. Cycle 2 (the C4 code cycle,
`codegen-c4-implementation` stacked on this branch) begins only
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
- **C3 valid fixtures kept (20):** `temporal_column_date`,
  `temporal_column_datetime`, `temporal_column_localtime`,
  `temporal_column_localdatetime`, `temporal_column_duration`,
  `temporal_column_time`, `property_date`, `property_timestamp`,
  `float32_column`, `float32_parameter`, `list_int`, `list_string`,
  `list_list_int`, `list_entity`, `list_nullable`, `list_unknown`,
  `scalar_null`, `scalar_map`, `unknown_column`,
  `many_columns_mixed_temporal_list`.
- **C4 valid fixtures added (13):** `write_exec_delete`,
  `write_exec_zero_param`, `write_exec_two_params`,
  `write_exec_read`, `write_one_projection_entity`,
  `write_one_projection_property`, `write_many_projection_entity`,
  `write_many_projection_property`, `mixed_read_write_batch`,
  `write_exec_nullable_param`, `write_exec_float32_param`,
  `write_exec_temporal_param`, `write_only_batch`.
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
- **C3 invalid fixtures renamed:**
  `out_of_c3_scope_edge_union` → `out_of_c4_scope_edge_union`;
  `out_of_c3_scope_non_property_parameter` →
  `out_of_c4_scope_non_property_parameter`. `out_of_c3_scope_exec`
  retires from the invalid set and reappears in `valid/` as
  `write_exec_delete`.
- **C4 invalid fixtures added (7):** `exec_on_projection_read`,
  `exec_on_projection_write`, `cardinality_shape_zero_column_read_one`,
  `cardinality_shape_zero_column_write_one`,
  `cardinality_shape_zero_column_write_many`,
  `out_of_c4_scope_edge_union` (rename of
  `out_of_c3_scope_edge_union`),
  `out_of_c4_scope_non_property_parameter` (rename of
  `out_of_c3_scope_non_property_parameter`).

**Totals at C4:**
- Valid fixtures: 2 (C0) + 9 (C1) + 10 (C2) + 20 (C3) + 13 (C4) = 54.
- Invalid fixtures: 4 (C0) + 16 (C1/C2/C3 kept) + 7 (C4 new
  including two renames) = 27. (`out_of_c3_scope_exec` retires
  from the invalid set into `valid/`.)
- Sentinels in `allSentinels`: 15.
- Every sentinel has ≥1 invalid fixture; the reachability sweep
  is C0's `TestSentinelReachability` unchanged.
