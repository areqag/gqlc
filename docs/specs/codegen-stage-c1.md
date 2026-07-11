# Stage C1 spec — codegen: scalar/property read queries

The implementation brief for Stage C1 of `internal/codegen`, extending the
merged C0 skeleton (`docs/specs/codegen-stage-c0.md`) with the two
capabilities ADR 0010 D7 places at C1: **per-query Params/Row structs
projected from `resolver.ValidatedQuery` for scalar and property-typed
columns**, and **real `:one`/`:many` method emission** flowing through the
`ExecuteRead` arm of the C0 `driverDB.run` indirection. Build this
**test-first**. Scope, sequencing, and error posture inherit from ADR 0010
and the C0 spec unchanged; this document revises only the sections C1
touches.

Stage C1 keeps the C0 file set (`db.go` / `querier.go` / `models.go`)
byte-identical for the parts C1 does not touch, changes `db.go`'s
`driverDB.run` body from stub to real read dispatch, populates
`querier.go`'s `ReadQuerier` with every read method the batch declares,
and introduces the first per-source `<name>.cypher.go` file emitting the
query-text const, the Params struct (when the query has two-plus
parameters), the Row struct (when the query has two-plus columns), the
generated method on `*Queries`, and the per-query row assembly. Every
column and parameter in a C1-admissible query has resolved type
`ResolvedProperty` (the D3 table's schema-side rows: `property STRING /
BOOL`, `property INT8..INT64` / `UINT8..UINT64` native widths, `property
INT / UINT` machine widths, `property FLOAT32/64`). Entity projections,
collections, temporals, scalars, unknowns, and the unrepresentable widths
route to `ErrOutOfC1Scope` (§5.2, category-grained per C0's precedent —
retirement plan mirrors the resolver's `ErrOutOfR0Scope`). Writes
(`:exec`, `WriteQuerier` population, `ExecuteWrite` path,
cardinality×shape rejection) stay out of scope and continue to route to
`ErrOutOfC1Scope`; C4 owns them (ADR 0010 D7).

---

## 1. Deliverables

- `internal/codegen/generate.go` — extended with a per-query emission
  loop over `Input.Queries`, the naming kernel (§4), the property→Go
  type mapping (§5.1), Params / Row struct rendering (§5.2), method
  rendering into `db.go` `Queries` (§5.3), `ReadQuerier` interface
  population (§5.4), the per-source `<name>.cypher.go` file assembly
  (§5.5), and the real `driverDB.run` read-dispatch body (§5.6). The
  C0 file layout stands (`codegen.go` / `input.go` / `errors.go` /
  `generate.go`); no new files are introduced at C1.
- `internal/codegen/errors.go` — extended with five new sentinels
  (§5.7): `ErrOutOfC1Scope`, `ErrParamNameCollision`,
  `ErrRowFieldCollision`, `ErrAliasRequired`, `ErrIdentifierCollision`.
  `ErrNoRows` is a *generated* sentinel (§5.5) — emitted into the
  user's package, not returned from `Generate`; it does not join
  `allSentinels`.
- `test/data/codegen/valid/<name>/` — new C1 valid fixtures (§6.2),
  each holding a schema `.gql`, one or more `.cypher` query files, a
  `manifest.json`, and a `golden/` subdirectory whose file set adds
  one `<name>.cypher.go` per source file plus `db.go` / `querier.go`
  now populated with the read method and `ReadQuerier` entries.
  `models.go` stays empty at C1 (entity structs land at C2).
- `test/data/codegen/invalid/<name>/` — new C1 negative fixtures for
  each of the five new sentinels (§6.4).
- `internal/codegen/codegen_test.go` — no structural change (the C0
  harness scales to C1 fixtures without churn); the `sentinelByName`
  map (§6.4) grows one row per new sentinel.

Nothing downstream of scalar/property reads is built. Entity structs +
entity decode helpers (C2), collections + temporals + unrepresentable-
width sentinels + FLOAT32 schema-width contract (C3), writes + `:exec`
+ cardinality × shape rejection (C4), `edgeUnion` sealed interfaces +
package-level collision-sweep hardening (C5), version-stamp polish
(C6), `:iter` streaming (post-v1, `gqlc-1a5`) stay for their owning
stage per ADR 0010 D7.

---

## 2. Architecture — deltas from C0

C0's architecture (§2 of the C0 spec) stands: the `Generator` seam, the
concrete `*Codegen` return, the empty `Option` surface, the purity /
determinism / short-circuit posture, and the `generate.go` / `generate`
kernel split are unchanged. C1 extends only the kernel's per-query loop;
no new exported types except the five new sentinels.

### 2.1 The C1 kernel structure

The kernel remains one linear pass with early returns. C1 replaces
C0's empty per-query loop (C0's `generate` short-circuits after
`renderModels` — no query is projected) with a two-phase per-query
walk:

- **Phase A — batch admission** (§4). Every `NamedQuery` passes C0's
  `validateQueries` gate first (unchanged: zero cardinality, duplicate
  name, duplicate source-file basename). Then a single sweep over
  `in.Queries` in slice order runs the per-query admission checks
  C1 introduces: every `Column.Type` is `ResolvedProperty` (§5.1
  scope), every `ResolvedParameter.Type` is `ResolvedProperty`, the
  cardinality is `:one` or `:many` (`:exec` is C4), and the method
  name (verbatim from `NamedQuery.Name`) does not collide with any
  package-level identifier already reserved (`Queries`, `New`,
  `WithTx`, `ReadQuerier`, `WriteQuerier`, `Querier`, `driverOrTx`,
  `driverDB`, `txDB`, `ErrNoRows`). Non-property columns / parameters
  route to `ErrOutOfC1Scope` naming the offending column or parameter
  and its resolved kind. `:exec` routes to `ErrOutOfC1Scope` naming
  the cardinality. Method-name collision with a reserved identifier
  is `ErrIdentifierCollision`. Phase A short-circuits: first offender
  wins.
- **Phase B — per-query name derivation** (§4). A second sweep over
  `in.Queries` in slice order runs the naming kernel: derive the
  method name (verbatim, already gate-checked), derive Params-field
  names for each parameter, derive Row-field names for each column,
  and check derived-name collisions within each Params struct
  (`ErrParamNameCollision`) and within each Row struct
  (`ErrRowFieldCollision`). Row field derivation uses text-shape
  analysis on `Column.Name` (§4.3 lays out the shape rules and the
  `AS`-required verdict). Phase B short-circuits: first offender wins.
  A cross-query package-level collision sweep (two Row structs named
  `XRow` sharing `X`, a method colliding with another method) runs
  after Phase B; the C1 rule is any collision → `ErrIdentifierCollision`
  (no renaming). C5 hardens the sweep (ADR 0010 D7); C1's coverage is
  the union of every generated top-level identifier a C1-admissible
  batch can produce.

Phase A runs before Phase B because Phase B's naming reads
`Column.Type`'s D3-table cell (§5.1) to name Row struct fields —
non-property columns must have already failed Phase A. Phase A alone
never fails on a name derivation — every rejection is on a type or a
reserved-identifier match. Phase B rejects only on the derived-name
axes.

The per-source file emission walk (§5.5) runs after Phase B: groups
`Queries` by `SourceFile` basename in first-appearance order, renders
one file per group. `db.go`'s C0 body is regenerated at C1 with the
methods appended (§5.3); `querier.go`'s `ReadQuerier` is regenerated
with the method signatures listed (§5.4).

### 2.2 The naming kernel — helpers on the emission walk

C1 introduces three internal helpers, unexported, in `generate.go`:

```go
// paramFieldName derives the Params-struct field name for a parameter
// whose annotation was $<raw>. Split on '_', capitalize the first rune
// of each non-empty segment, preserve internal case of non-ALL-CAPS
// segments (§4.2). Returns "" on empty input; caller treats "" as an
// invalid derivation (unreachable — the parser rejects empty parameter
// names).
func paramFieldName(raw string) string

// rowFieldName derives the Row-struct field name for a column whose
// text is one of the two clean shapes: bare identifier ("p" -> "P") or
// property access ("p.name" -> "Name"). Anything else (function calls,
// expressions, literals) returns "", ok=false — the caller emits
// ErrAliasRequired naming the column's Column.Name. Alias-and-
// bare-variable projections render identically because they *are*
// identical strings from the resolver's point of view (D2 Resolved
// audit: Column has no "was aliased" bit).
func rowFieldName(colText string) (string, bool)

// goType maps a resolved property type to its native Go emission (§5.1
// table). Returns (typeText, ok): ok=false for the unrepresentable
// widths (INT128/256, UINT128/256, FLOAT16, FLOAT128/256, DECIMAL) —
// caller routes to ErrOutOfC1Scope naming the width (C3 owns them, D3
// table Resolved). Callers append a leading '*' for nullable columns/
// parameters (§5.1).
func goType(pt graph.PropertyType) (string, bool)
```

The helpers are grounded in the C0 kernel's existing `generate` scope:
`paramFieldName` reads from `ResolvedParameter.Name`;
`rowFieldName` reads from `Column.Name` (a plain string per
`internal/resolver/validated.go:29-33`, no shape tag); `goType`
switches on `graph.PropertyType` (the closed enum in
`internal/graph/propertytype.go:9-40`, one case per constant).

### 2.3 Purity, determinism, short-circuit — unchanged

C0 §2.3's three invariants stand:

- **Pure.** No new I/O; the naming helpers are pure text-to-text.
- **Deterministic.** Iteration order is `Input.Queries` slice order
  (Phase A / Phase B / per-source grouping), then per-query
  `Validated.Parameters` in query-wide first-appearance order (§5.2
  Params ordering), then `Validated.Columns` in projection order
  (§5.2 Row ordering). Both underlying slices are already ordered by
  the resolver (`internal/resolver/validated.go:17-18`; docstrings on
  `Column` and `ResolvedParameter` and R2 spec §6.5). No map
  iteration escapes into the output.
- **Short-circuit.** First-error wins across Phase A, Phase B, the
  cross-query collision sweep, and per-source emission. Zero value on
  error: `(nil, err)`.

### 2.4 What the driverDB.run body change means for gqlc's module

C1's `driverDB.run` moves from C0's `_ = session; return nil, nil` stub
to a real `session.ExecuteRead` call whose callback runs the query and
buffers the driver's result stream into `[]*neo4j.Record` **inside the
transaction lifetime**, returning that slice to the caller for
post-close decoding (§5.6). This is a **C0-committed seam signature
revision**: C0 declared `driverOrTx.run` returning
`(neo4j.ResultWithContext, error)`, but the neo4j-go-driver v5
`ResultWithContext` is only valid inside its transaction — consuming
it after `ExecuteRead` returns is not a documented pattern (verified
against `pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5/neo4j`,
2026-07-11). C0's stub body never exercised the lifetime issue, so
the invalid signature shipped. C1 is the first stage that actually
exercises the seam; the revision must land here, not later. The new
signature (§5.6) returns `[]*neo4j.Record` — driver-documented as
self-contained value snapshots safe to consume after the transaction
closes. Both `driverDB.run` and `txDB.run` update symmetrically so the
`driverOrTx` abstraction still holds; decode runs in the generated
method body outside the run indirection.

The change is entirely inside the emitted `db.go` template string
(§5.6 gives the exact body); gqlc's own module is not affected — the
generator emits text, and text-level changes cross no dependency
boundary. The nested-module compile fence (`just test-codegen-fence`,
C0 §7) is what proves the emitted body type-checks against the pinned
driver version (`test/data/codegen/go.mod`, v5.28.4 today).

---

## 3. Emitted API surface — the C1 shape

The user-visible generated surface C1 adds on top of C0. C1's exported
package-level identifiers are: the C0 exported skeleton set (`Queries`,
`New`, `WithTx`, `ReadQuerier`, `WriteQuerier`, `Querier`) plus the
C1 additions below. C0's unexported items (`driverOrTx` /
`driverDB` / `txDB`) stay unexported; C1 adds the unexported per-
query `<methodName>QueryText` const per §5.3 / §5.5 — neither set
participates in §4.4's exported-collision sweep.

### 3.1 Per-query method surface

Sqlc-style ergonomics, exactly as D2 Resolved pins:

- **Zero parameters, single column** — bare arg, bare value:
  ```go
  func (q *Queries) AllPeopleNames(ctx context.Context) ([]string, error)
  ```
- **Zero parameters, two-plus columns** — bare arg, XRow:
  ```go
  type PersonSummaryRow struct { Name string; Age int64 }
  func (q *Queries) PersonSummaries(ctx context.Context) ([]PersonSummaryRow, error)
  ```
- **Exactly one parameter, any column shape** — bare typed arg:
  ```go
  func (q *Queries) PersonById(ctx context.Context, id int64) (PersonRow, error)
  ```
- **Two-plus parameters, any column shape** — XParams struct:
  ```go
  type PeopleOverAgeParams struct { MinAge int64; Locale string }
  func (q *Queries) PeopleOverAge(ctx context.Context, arg PeopleOverAgeParams) ([]string, error)
  ```

- **Cardinality × shape:**
  - `:one` → `(XRow, error)` (or `(T, error)` for a single-column
    query). Empty result → generated `ErrNoRows` sentinel (§5.5).
    Multi-result on `:one` → generated `ErrMultipleResults` sentinel
    (§5.5) — matches sqlc's discipline for the analogous cardinality
    (sqlc's `QueryRow` implicitly discards; gqlc errors, per house
    reject-don't-guess: the discarded rows are a footgun the same way
    sqlc's `:exec`-on-a-SELECT is).
  - `:many` → `([]XRow, error)` (or `([]T, error)` for a single-
    column query). Zero rows returns `(nil, nil)` — the slice can be
    empty, that is not an error.

The C1 method surface is on `*Queries` (the C0 struct). Every C1
method is also a member of the emitted `ReadQuerier` interface (§3.3).

### 3.2 Params struct

`XParams` is emitted iff a query has two-plus parameters. Fields are
in `Validated.Parameters` order (query-wide first-appearance, R2 spec
§6.5). Every field is exported (§4.2 mangle); every field's Go type is
`goType(Type)` (§5.1) or `*goType(Type)` for a nullable parameter (D3
Resolved: symmetric parameter treatment; nil encodes as Cypher `null`).
The zero-parameter and single-parameter queries do not emit a Params
struct: zero-parameter has no arg, single-parameter takes the bare
typed arg.

Two parameters mangling to the same field name → `ErrParamNameCollision`
naming both parameters (deterministic order: first-appearance in
`Validated.Parameters`; the first offender wins so re-runs produce
identical errors).

### 3.3 Row struct

`XRow` is emitted iff a query has two-plus columns. Fields are in
`Validated.Columns` order (projection order, R0 §2.3). Every field is
exported (§4.3 mangle); every field's Go type is `goType(Type.Type)`
for a `ResolvedProperty` column (§5.1), or `*goType(Type.Type)` for a
nullable column. Single-column queries do not emit a Row struct: the
method returns the bare typed value (or slice of it).

Row-field derivation runs on the column's `Name` (§4.3 text-shape
analysis). Two derived Row fields colliding → `ErrRowFieldCollision`
naming both columns and demanding an explicit `AS alias`. A column
whose text-shape analysis returns `ok=false` (anything richer than a
bare identifier or single-dot property access) → `ErrAliasRequired`
naming the column; the fix is a query-side `AS alias` — a nudge toward
self-documenting queries the resolver already accepts.

### 3.4 `ReadQuerier` interface population

C0's empty `ReadQuerier` (`type ReadQuerier interface {}`) becomes the
list of every C1 method signature, one line per method, deterministic
by `Input.Queries` order. `WriteQuerier` stays empty at C1 (C4
populates it — ADR 0010 D7). `Querier` embeds both unconditionally,
per D2 Resolved.

The compile-time assertion `var _ Querier = (*Queries)(nil)` (C0 §5.4)
now fences drift: adding a method to `*Queries` without listing it in
`ReadQuerier` fails to compile at the nested-module fence.

### 3.5 The `ErrNoRows` and `ErrMultipleResults` sentinels

Generated into the user's package, not returned from `Generate`:

```go
// ErrNoRows is returned by a :one method when the query produced zero
// rows. Callers branch with errors.Is.
var ErrNoRows = errors.New("gqlc: no rows in result set")

// ErrMultipleResults is returned by a :one method when the query
// produced more than one row. Callers branch with errors.Is.
var ErrMultipleResults = errors.New("gqlc: multiple rows in :one result set")
```

Emitted at the top of `db.go` (below the imports, above the `Queries`
type) iff the batch contains at least one `:one` query — no-`:one`
batches skip the emission to keep the generated tree gofmt-clean
without unused-var suppression. Adding an unused `var _ = ErrNoRows`
per emission was considered and rejected: the sentinels are
user-facing, so the file's usage pattern is the caller's; an
`errors` import that becomes dead if the caller does not check is a
`go vet` concern the caller reports, not gqlc.

Neither sentinel joins `codegen.allSentinels`. `allSentinels` is the
closed set of errors `Generate` returns; these are emitted values in
the *user's* package, not codegen returns. The sweep discipline (§5.7)
does not apply.

### 3.6 The `driverOrTx.run` seam signature — C0 revision

C0's `driverOrTx.run` returned `(neo4j.ResultWithContext, error)` on
the strength of a stub body that never exercised the driver. C1 is
the first stage to actually call the seam and finds the shape is
invalid: `ResultWithContext` from a `ManagedTransaction.Run` is only
valid inside the callback that owns the transaction (verified against
`pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5/neo4j`, 2026-07-11).
C1 therefore revises the seam:

```go
// C0's declaration (unchanged in queryfile, replaced in codegen):
//   run(...) (neo4j.ResultWithContext, error)
// C1's replacement (both driverDB and txDB):
type driverOrTx interface {
    run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error)
}
```

`[]*neo4j.Record` is the driver's documented safe-post-close shape:
`Record` values are self-contained snapshots. The callback buffers
via `.Collect(ctx)` inside the transaction; the outer function
returns the buffered slice. §5.6 gives the full `driverDB.run` /
`txDB.run` bodies. The user-visible surface (§3.1's methods) is
unchanged — the seam is unexported and internal — but the C0
goldens' `db.go` file regenerates under the new template on the
first C1 run; the byte diff is the signature swap plus the real
read-arm body plus a `fmt` import.

---

## 4. The naming kernel

### 4.1 Method names — annotation verbatim, no mangling

`NamedQuery.Name` becomes the generated method name unchanged. The
front end (`internal/queryfile`, C0 §4) already validated the name as
a Go identifier (`^[A-Z][A-Za-z0-9]*$`), so the codegen-side check is
the reserved-identifier match:

```
Name ∈ { "Queries", "New", "WithTx", "ReadQuerier", "WriteQuerier",
         "Querier", "ErrNoRows", "ErrMultipleResults" }
       → ErrIdentifierCollision
```

The reserved list is the C1 package-level *exported*-identifier set
exactly (unexported items like `driverOrTx` / `driverDB` / `txDB`
cannot be reached because `NamedQuery.Name` must be exported — front
end enforces `^[A-Z][A-Za-z0-9]*$`). Non-C0-emitted C1 additions
(`ErrNoRows`, `ErrMultipleResults`) are included even though they
only emit for `:one`-carrying batches — the check is uniform: a
query named `ErrNoRows` collides with the sentinel name whether or
not the sentinel actually emits, because C5's package-level collision
sweep will not want a discontinuity introduced at C1 (a rename that
works in one batch but not another is exactly the "renaming scheme"
D2 Resolved refused).

A cross-query method collision (two `:one` queries both named `Widget`)
is caught by C0's `validateQueries` `ErrDuplicateQueryName` on
`NamedQuery.Name` — no C1 addition needed.

### 4.2 Params field names — the one mangle site

D2 Resolved's parameter-name mangle:

1. Read `ResolvedParameter.Name` (the `$`-stripped name, per
   `internal/resolver/validated.go:39-42`).
2. Split on `_` into segments. Empty segments (from leading or
   trailing underscores, or consecutive underscores) are dropped —
   they contribute no name.
3. For each segment: uppercase the first rune. If the segment is not
   entirely ALL-CAPS, preserve the case of the remaining runes. If
   the segment is entirely ALL-CAPS (`API`, `URL`, `ID`), keep it
   ALL-CAPS.
4. Concatenate. The result is the field name.

Examples: `min_age` → `MinAge`; `minAge` → `MinAge`; `min_Age` →
`MinAge`; `MIN_AGE` → `MINAGE`; `id` → `Id`; `API_key` → `APIKey`;
`_min_age` → `MinAge` (leading underscore dropped). The empty-string
case is unreachable — the Cypher parser rejects `$` with an empty name
at build time.

Two parameters mangling to the same field name → `ErrParamNameCollision`
naming both parameter positions. The check runs after every parameter
has been mangled (not per-parameter as the derivation runs, so the
error message names both offenders) but reports on the *first*
collision (Phase B short-circuit).

**Resolved (spec-cycle, 2026-07-11): non-ASCII parameter names
accepted.** Cypher's `oC_SymbolicName` admits Unicode letters; Go
identifiers accept Unicode letters as identifier runes; the compile
fence catches malformed derived names before goldens bake. A
non-ASCII parameter's mangle rules follow the same split-on-`_` and
first-rune-uppercase discipline. No sentinel fires for non-ASCII.
This is consistent with C0's `packageIdent` check being an
ASCII-only grammar for the package identifier — a package name is
a directory-name concern, a field name is a source-token concern.

### 4.3 Row field names — text-shape analysis on `Column.Name`

D2 Resolved's row-field naming rules distinguish three text shapes on
`Column.Name`. `Column` has no "was aliased" bit (ADR 0010 D2 Resolved
audit finding); `Name` is one string, alias or verbatim source text,
which is why alias-and-bare-variable projections render identically:
they *are* identical strings.

The three shapes:

1. **Bare identifier** — matches `^[A-Za-z_][A-Za-z0-9_]*$`. Mangle
   as §4.2 (split on `_`, capitalize segments, preserve internal
   case). Example: `p` → `P`; `name` → `Name`; `min_age` → `MinAge`.
2. **Property access** — matches `^[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*$`.
   Take the final segment (after the `.`), mangle as §4.2. Example:
   `p.name` → `Name`; `person.min_age` → `MinAge`; `p.id` → `Id`.
   Alias-and-bare-variable ambiguity: a `RETURN p.name AS name`
   yields `Column.Name == "name"` (bare identifier shape); a `RETURN
   p.name` yields `Column.Name == "p.name"` (property-access shape).
   Both derive to `Name`. The ambiguity is harmless because both
   render to the same field name and (by D3 Resolved's schema-derived
   type mapping) the same Go type — a wart the reader can spot in
   the query text but which changes no output.
3. **Everything else** — `count(*)`, `p.name + 1`, string literals,
   list literals, function calls, `x IS NULL` predicates, any
   expression that survives R2's projection walk. `rowFieldName`
   returns `("", false)`; the caller emits `ErrAliasRequired` naming
   the column's `Column.Name` verbatim and prompting an explicit `AS
   alias` in the query.

The regex definitions live inline in `generate.go` as
`var (rowBareIdent, rowPropAccess *regexp.Regexp)` package-level
constants, compiled once. Both patterns anchor with `^...$` — a match
against a substring would silently accept `count(x.y)` as
`property-access shape` for the `x.y` inner run, which is exactly
wrong. `regexp.MustCompile` at package-init: a bad pattern is a
codegen bug, not a fixture concern.

Two derived Row field names colliding → `ErrRowFieldCollision` naming
both columns and stating "explicit `AS alias` required to disambiguate".
The two-columns case is deterministic on first-appearance in
`Validated.Columns`; the first offender wins.

### 4.4 Package-level identifier collision sweep

After Phase B, sweep every emitted **exported** top-level identifier
for duplicates. Unexported package-internal identifiers (currently
`driverOrTx` / `driverDB` / `txDB` from the C0 skeleton and the
per-query `<methodName>QueryText` const) are not swept here — the
nested-module compile fence catches unexported redeclarations with a
strictly-better diagnostic (§4.4 rationale below). The exported
identifier set per generated package is:

- The C0 exported skeleton set: `Queries`, `New`, `WithTx`,
  `ReadQuerier`, `WriteQuerier`, `Querier`.
- Zero or one sentinel emission: `ErrNoRows`, `ErrMultipleResults`
  (both or neither — always paired when the batch contains at least
  one `:one` query, §3.5).
- One per query: the method name.
- Zero or one per query: `XParams` (query has two-plus parameters).
- Zero or one per query: `XRow` (query has two-plus columns).

Any duplicate → `ErrIdentifierCollision` naming both identifier
sources (e.g. "method `PersonRow` collides with row struct
`PersonRow`", or "row struct `PersonRow` collides with row struct
`PersonRow` from a different query"). The sweep is a single map
insertion pass over the identifier set; the first duplicate wins.

**Why single-column bare-value queries don't emit an XRow.** D2
Resolved: single column → bare value. So a `RETURN p.name` `:many`
emits `([]string, error)` with no `PersonNamesRow` struct — the
identifier is not reserved and cannot collide. A separate query
whose Row *does* have to be named `PersonNames` would be free to do
so.

**Why `<methodName>QueryText` consts do not participate in the
exported-identifier sweep.** The per-query query-text const's name is
`<methodName>QueryText` (lower-camel-case first rune, §5.3, §5.5) —
unexported. The user calls the method, never the const; the const is
a codegen implementation detail that no user code can name or shadow.
Two unexported consts colliding would fail the nested-module compile
fence with a "redeclared in this block" error — the fence is the
enforcement surface for unexported identifiers, and the fence error
names the file and both declaration sites, which is a strictly
better diagnostic than an `ErrIdentifierCollision` fail-message that
only names the query pair. The `<methodName>` prefix is deterministic
in `Input.Queries` order (§4.1 verbatim method name → lowercase
first rune), so a duplicate at this axis would already imply a
duplicate method-name collision the exported sweep catches first. C5
extends the exported sweep with entity-struct and decode-helper
identifiers, which are user-visible; unexported package-internal
identifiers stay on the fence.

C5 hardens this sweep against additional identifier sources
(entity structs, decode helpers) as C2/C3 add them (ADR 0010 D7).
C1's identifier set is a strict subset of C5's.

---

## 5. Emission templates and per-query files

### 5.1 Property → Go type mapping (the D3 table's C1 rows)

The C1 slice of the D3 type-mapping table — schema-side property
types only, in the source order of `internal/graph/propertytype.go`:

| `graph.PropertyType` | Go type | Nullable emission |
|---|---|---|
| `STRING`               | `string`   | `*string`   |
| `BOOL`                 | `bool`     | `*bool`     |
| `DATE`                 | (deferred to C3)              | — |
| `TIMESTAMP`            | (deferred to C3)              | — |
| `INT`                  | `int`      | `*int`      |
| `INT8`                 | `int8`     | `*int8`     |
| `INT16`                | `int16`    | `*int16`    |
| `INT32`                | `int32`    | `*int32`    |
| `INT64`                | `int64`    | `*int64`    |
| `INT128` / `INT256`    | `ErrOutOfC1Scope` (unrepresentable, C3 sentinel) | — |
| `UINT`                 | `uint`     | `*uint`     |
| `UINT8`                | `uint8`    | `*uint8`    |
| `UINT16`               | `uint16`   | `*uint16`   |
| `UINT32`               | `uint32`   | `*uint32`   |
| `UINT64`               | `uint64`   | `*uint64`   |
| `UINT128` / `UINT256`  | `ErrOutOfC1Scope` (unrepresentable, C3 sentinel) | — |
| `FLOAT`                | `float64`  | `*float64`  |
| `FLOAT16`              | `ErrOutOfC1Scope` (unrepresentable, C3 sentinel) | — |
| `FLOAT32`              | `float32`  | `*float32`  |
| `FLOAT64`              | `float64`  | `*float64`  |
| `FLOAT128` / `FLOAT256`| `ErrOutOfC1Scope` (unrepresentable, C3 sentinel) | — |
| `DECIMAL`              | `ErrOutOfC1Scope` (unrepresentable, C3 sentinel) | — |

- **`FLOAT` (unqualified) → `float64`.** The schema language accepts
  `FLOAT` as the machine-width family (analogue of `INT` / `UINT`);
  the neo4j driver stores floats as float64, so unqualified `FLOAT`
  maps to `float64` unconditionally. ADR 0010 D3 audit: unqualified
  FLOAT is extrapolated by analogue with the INT/UINT machine rows —
  D3's table has "`property` INT / UINT (machine) → `int` / `uint`"
  but omits a corresponding FLOAT row; if D3 later disagrees with
  the machine-width analogue (e.g., ruling that unqualified FLOAT is
  invalid schema syntax and must appear only as one of the
  bit-widthed variants), revise the row here. The FLOAT32
  schema-width contract (D3 Resolved) is the *widening/narrowing*
  contract for a schema author who declared FLOAT32 specifically —
  its implementation is C3's business (encode widens to float64;
  decode narrows by plain conversion, no range check). At C1,
  FLOAT32 emits as `float32`; the encode/decode contract is
  documented in the C1 fixture golden as an accepted TODO for C3,
  not implemented.
- **`DATE` / `TIMESTAMP` → deferred to C3.** They are property-side
  temporals; C1's scope explicitly does not cover temporals (§7). A
  query projecting a `DATE` or `TIMESTAMP` property column routes to
  `ErrOutOfC1Scope`, not `ErrIdentifierCollision`. The fixture
  discipline (§6.4) provides one negative fixture for each.
- **Nullable → pointer, uniformly.** D3 Resolved: a property column
  whose resolved `Nullable == true` renders `*T`; a non-nullable
  column renders `T`. Same rule symmetrically on Params fields
  (nullable parameter → `*T`, encoded as Cypher `null` when nil).
  Non-nullable column arriving null from the driver is a decode
  error naming the column (§5.5's per-query row assembly emits the
  named error) — never a silent zero value.

`goType(pt)` returns the Go type text and `ok=true` for every
representable row, `ok=false` for every deferred row. Callers of
`goType` route `ok=false` to `ErrOutOfC1Scope` with the property-type
name in the fail-message so the fixture points at the exact width.

### 5.2 Params and Row struct rendering

Per-query, if the query has two-plus parameters, emit:

```go
type <MethodName>Params struct {
    <Field1> <goType(Type1)>
    <Field2> <goType(Type2)>
    ...
}
```

Fields in `Validated.Parameters` order; each field a bare exported
name (§4.2) with the mapped Go type (§5.1) prefixed with `*` iff
nullable. No JSON tags (Params structs are not a serialisation
format). No doc comment on the struct — the per-method doc comment
(§5.3) references the struct by name; the struct's fields are
self-describing.

Per-query, if the query has two-plus columns, emit:

```go
type <MethodName>Row struct {
    <Field1> <goType(Type1)>
    <Field2> <goType(Type2)>
    ...
}
```

Fields in `Validated.Columns` order (projection order); each field
name derived by text-shape analysis (§4.3); each field's Go type
mapped from `Column.Type.Type` (the `ResolvedProperty.Type`, at C1
scope). Nullable columns get `*T` per §5.1.

Both struct types emit into the per-source `<name>.cypher.go` file
(§5.5). Their name is `<MethodName>Params` / `<MethodName>Row` — the
method name is already a valid exported Go identifier, so the suffix
concatenation is always valid.

### 5.3 Method rendering into `db.go`

The method rendering appends to the C0 `db.go` body (§5.6 quotes
the full C1 body). Every `:one`/`:many` method's shape is:

```go
// <MethodName> executes the <method-name> query.
//
//   <first-3-lines-of-query-text>
//   [... truncated if the query exceeds 3 lines]
func (q *Queries) <MethodName>(ctx context.Context<param-list>) (<return>, error) {
    records, err := q.db.run(ctx, <queryTextConst>, <paramsMap>, neo4j.AccessModeRead)
    if err != nil {
        return <zero>, err
    }
    <decode-body>
}
```

- **`<param-list>`** — empty if zero parameters, `, <bareParam> <T>`
  if one parameter, `, arg <MethodName>Params` if two-plus.
  `<bareParam>` is the single parameter's field-name mangle (§4.2),
  but lowercase-initial for the Go argument-name convention:
  `paramFieldName(name)` → `MinAge`, then lowercase the first rune
  → `minAge`. The lowercase pass is per-Go idiom (locally-scoped
  variables are lower-camel-case); no naming rule is affected.
- **`<return>`** — `T` for `:one` single-column; `<MethodName>Row`
  for `:one` two-plus-columns; `[]T` for `:many` single-column;
  `[]<MethodName>Row` for `:many` two-plus-columns.
- **`<queryTextConst>`** — the per-query const name (§5.5):
  `<methodName>QueryText`. Lower-camel-case first rune (Go
  package-internal const convention); the const itself is
  package-scoped so the identifier is package-visible but not
  exported.
- **`<paramsMap>`** — the `map[string]any` literal binding driver
  parameter names to Go values. Zero parameters: `nil`. One
  parameter: `map[string]any{"<rawName>": <bareParam>}`. Two-plus:
  `map[string]any{"<rawName1>": arg.<Field1>, ...}`. The map's
  keys are `Validated.Parameters[i].Name` verbatim (the `$`-stripped
  raw parameter name — the driver binds by name and matches the
  query-text's `$name` occurrences).
- **`<zero>`** — the zero value of the return type: `""` for
  `string`, `0` for numerics, `false` for `bool`, `nil` for slices
  and pointer types, `<MethodName>Row{}` for a two-plus-column
  Row-returning `:one`.
- **`<decode-body>`** — the per-query row assembly, §5.5. Runs a
  `len(records)` arity test (`:one` empty → `ErrNoRows`, `:one`
  multi → `ErrMultipleResults`, `:many` any length is accepted),
  decodes each `neo4j.Record` into the Row shape via
  `neo4j.GetRecordValue[T]` per column, and materialises the return
  value. The `records` local is the buffered `[]*neo4j.Record` the
  seam returned (§5.6).

The 3-line doc-comment quote of the query text is a readability
affordance for `godoc` browsers — the query is the source of truth,
and having the top of it inline saves a jump to the `<methodName>QueryText`
const. Truncation policy: if the query is more than 3 lines, take
the first 3 and append `//   ...`; the const carries the whole text
regardless. The doc-comment lines are prefixed `//   ` (two spaces
after `//`) so the comment reads as a code block.

### 5.4 `querier.go` regeneration — `ReadQuerier` population

C0's empty `ReadQuerier` becomes:

```go
type ReadQuerier interface {
    <MethodName1>(ctx context.Context<param-list-1>) (<return-1>, error)
    <MethodName2>(ctx context.Context<param-list-2>) (<return-2>, error)
    ...
}
```

Order is `Input.Queries` slice order (deterministic). `WriteQuerier`
stays empty (`type WriteQuerier interface {}`) — the C0 template body
is unchanged for the write side. `Querier` still embeds both. The
`var _ Querier = (*Queries)(nil)` assertion holds because every
method emitted on `*Queries` (§5.3) is listed in `ReadQuerier`.

If the batch has zero read queries (a models-only batch or an
all-`:exec` batch — the latter is C4 territory), `ReadQuerier` stays
empty and `querier.go` is byte-identical to C0. This is a legitimate
outcome; the C0 goldens for empty-query fixtures stay untouched at C1.

### 5.5 The per-source `<name>.cypher.go` file

Emitted per `SourceFile` basename in `Input.Queries` (§2.1 grouping;
first-appearance order). The file's shape:

```go
// Code generated by gqlc <version>. DO NOT EDIT.

package <derived>

import (
    "context"
    "fmt"

    "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const <method1>QueryText = `<query1-source-text>`

const <method2>QueryText = `<query2-source-text>`

type <Method1>Params struct { ... }   // omitted if <2 params
type <Method1>Row    struct { ... }   // omitted if <2 columns

// <Method1> executes ...
func (q *Queries) <Method1>(...) (..., error) { ... }

// <Method2> executes ...
func (q *Queries) <Method2>(...) (..., error) { ... }
```

- **Imports.** `context` is always present (every method takes
  `ctx context.Context`). `fmt` is always present (every column
  emits a `fmt.Errorf` decode wrapper — §5.5's row-assembly
  templates). The `github.com/neo4j/neo4j-go-driver/v5/neo4j`
  import is always present (every method calls `q.db.run` whose
  signature names `neo4j.AccessMode` and returns `[]*neo4j.Record`,
  and the decode body uses `neo4j.GetRecordValue`). `errors` is
  *not* imported by the per-source file: `ErrNoRows` and
  `ErrMultipleResults` are emitted from `db.go`, and the empty /
  multi checks in the row assembly test `len(records)` on the
  buffered slice rather than driver typed errors.
- **Query-text consts.** One `const <method>QueryText = "..."` per
  method, using Go's raw-string backtick delimiter to preserve
  interior newlines and quotes byte-for-byte per ADR 0005. A
  query text containing a backtick is a fixture-time invariant
  violation the generator flags with `ErrOutOfC1Scope` naming the
  query (the query text is a Cypher construct — Cypher does not
  use backticks in a way ADR 0005 cannot preserve — but the
  emission decision is on `const` shape, so the check is codegen's;
  C4 or later may loosen to a fallback escape). This is a rarely-
  reached branch; the fixture is one-line for coverage.
- **Struct emission order.** For each query in this file (in
  `Input.Queries` order): (a) query-text const; (b) Params struct if
  emitted; (c) Row struct if emitted; (d) method. Order per query
  matches sqlc's per-query grouping so a reader scanning a per-source
  file finds all a query's identifiers together, not interleaved
  across the file.
- **Blank-line separation.** One blank line between each block.
  `go/format.Source` (C0 §5.7) normalises to gofmt conventions on
  the way out.

Per-query row assembly reads `[]*neo4j.Record` (the seam's return
shape per §5.6) — empty/multi checks are `len(records)` tests on the
buffered slice, not driver-error-type checks; `ErrNoRows` /
`ErrMultipleResults` fire deterministically on slice length.

**Per-query row assembly template — `:one`, single column:**

```go
records, err := q.db.run(ctx, <method>QueryText, <paramsMap>, neo4j.AccessModeRead)
if err != nil {
    return <zero>, err
}
if len(records) == 0 {
    return <zero>, ErrNoRows
}
if len(records) > 1 {
    return <zero>, ErrMultipleResults
}
value, _, err := neo4j.GetRecordValue[<T>](records[0], "<column-name>")
if err != nil {
    return <zero>, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
}
<nullability-check>
return <value-or-deref>, nil
```

The empty and multi cases are simple length tests — the driver has
already buffered every record inside the callback (§5.6), so the
generated method reads the exact arity `.Collect` produced. No
driver-typed-error branching is needed.

**Per-query row assembly template — `:many`, single column:**

```go
records, err := q.db.run(ctx, <method>QueryText, <paramsMap>, neo4j.AccessModeRead)
if err != nil {
    return nil, err
}
out := make([]<T>, 0, len(records))
for _, record := range records {
    value, _, err := neo4j.GetRecordValue[<T>](record, "<column-name>")
    if err != nil {
        return nil, fmt.Errorf("<method>: decode column %q: %w", "<column-name>", err)
    }
    <nullability-check>
    out = append(out, <value-or-deref>)
}
return out, nil
```

**Per-query row assembly template — `:one`, two-plus columns:**

```go
records, err := q.db.run(ctx, <method>QueryText, <paramsMap>, neo4j.AccessModeRead)
if err != nil {
    return <Method>Row{}, err
}
if len(records) == 0 {
    return <Method>Row{}, ErrNoRows
}
if len(records) > 1 {
    return <Method>Row{}, ErrMultipleResults
}
var row <Method>Row
<per-column decode block, one per column>:
value1, _, err := neo4j.GetRecordValue[<T1>](records[0], "<column-name-1>")
if err != nil {
    return <Method>Row{}, fmt.Errorf("<method>: decode column %q: %w", "<column-name-1>", err)
}
<nullability-check-1>
row.<Field1> = <value-or-deref-1>
... (repeat per column, deterministic in Validated.Columns order)
return row, nil
```

**Per-query row assembly template — `:many`, two-plus columns:**
same as `:many` single column, but `out := make([]<Method>Row, 0,
len(records))` and each iteration decodes every column into a fresh
`<Method>Row` before appending.

**Nullability check.** For a non-nullable column: `GetRecordValue`
returns `value, isNil, err`. If `err != nil`, propagate wrapped. If
`isNil == true`, return the "non-nullable column arrived null" error:

```go
return <zero>, fmt.Errorf("<method>: column %q is non-nullable but arrived null", "<column-name>")
```

Not a sentinel — the message is fixture-worthy prose (the fail-site
is the emitted code, not `Generate`). The precedent is sqlc's decode
error naming the column. For a nullable column: `isNil == true` → set
the pointer field to `nil`; `isNil == false` → take the address of a
local variable holding the value (`v := value; row.X = &v`).

Both `fmt` and `neo4j` are unconditionally imported by every
per-source file (a C1-admissible per-source file has at least one
method, and every method emits both a decode-error `fmt.Errorf` and
a `neo4j.GetRecordValue` call). `errors` is *not* imported: the
generated sentinels `ErrNoRows` / `ErrMultipleResults` live in
`db.go` and are visible in the same package, and the length-based
empty/multi tests use no `errors.Is` branch. Removing `errors`
from the emission's import block avoids `goimports`-level noise on
files that don't need it.

### 5.6 The `driverOrTx.run` seam-signature revision and the C1 body

**Signature revision.** C0 committed
`run(ctx, cypher, params, access) (neo4j.ResultWithContext, error)`
on the unexported `driverOrTx` interface (C0 §5.3). That signature is
invalid on the pinned driver: `neo4j.ResultWithContext` handed back by
`ManagedTransaction.Run` is only valid inside the callback that
received the `ManagedTransaction` — `ExecuteRead` closes the
transaction on callback return, invalidating any result the callback
hands back. C0's stub `return nil, nil` never exercised the lifetime
issue, so the invalid shape shipped merged. C1 is the first stage that
actually calls the seam, so C1 revises it.

**New signature** (both `driverDB.run` and `txDB.run`):

```go
type driverOrTx interface {
    run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error)
}
```

`[]*neo4j.Record` is the driver-documented safe-post-close shape:
`neo4j.Record` values are self-contained snapshots and the slice
survives transaction close (verified against
`pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5/neo4j`, 2026-07-11).
Buffered-consumption discipline: the callback runs the query and
calls `.Collect(ctx)` inside the transaction, returning the buffered
slice out through `ExecuteRead`'s generic return-type parameter. The
generated method decodes over the returned slice outside the callback.

C0 emits (unchanged from merged C0 code — the invalid stub):

```go
type driverOrTx interface {
    run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) (neo4j.ResultWithContext, error)
}

func (d driverDB) run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) (neo4j.ResultWithContext, error) {
    session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: access})
    defer session.Close(ctx)
    // C0: stub
    _ = session
    return nil, nil
}
```

C1 emits the revised seam + real read-arm body:

```go
type driverOrTx interface {
    run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error)
}

func (d driverDB) run(ctx context.Context, cypher string, params map[string]any, access neo4j.AccessMode) ([]*neo4j.Record, error) {
    session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: access})
    defer session.Close(ctx)
    switch access {
    case neo4j.AccessModeRead:
        return session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) ([]*neo4j.Record, error) {
            result, err := tx.Run(ctx, cypher, params)
            if err != nil {
                return nil, err
            }
            return result.Collect(ctx)
        })
    case neo4j.AccessModeWrite:
        // C4 populates the write arm.
        return nil, fmt.Errorf("gqlc: write path not implemented")
    default:
        return nil, fmt.Errorf("gqlc: unknown access mode %v", access)
    }
}

func (t txDB) run(ctx context.Context, cypher string, params map[string]any, _ neo4j.AccessMode) ([]*neo4j.Record, error) {
    result, err := t.tx.Run(ctx, cypher, params)
    if err != nil {
        return nil, err
    }
    return result.Collect(ctx)
}
```

- **`ExecuteRead[T=[]*neo4j.Record]`.** The driver's `ExecuteRead` is
  generic over the callback's return type
  (`func (s SessionWithContext) ExecuteRead[T](ctx, work
  ManagedTransactionWorkT[T], ...) (T, error)`; verified against
  `pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5/neo4j`, 2026-07-11).
  C1 instantiates `T` as `[]*neo4j.Record` — the callback's Collect
  buffers everything inside the transaction, the outer function
  returns the buffered slice.
- **Result lifetime — resolved.** The driver documents `Record` as a
  self-contained snapshot; `[]*Record` is safe to consume after
  `session.ExecuteRead` returns because the transaction close only
  invalidates the streaming `ResultWithContext`, not the buffered
  records that `Collect` materialised. Decoding therefore runs in
  the generated method body over the returned slice, one iteration
  per record for `:many` and a one-shot arity-1 check for `:one`
  (§5.5).
- **`txDB.run` symmetric.** The `ManagedTransaction` handed to
  `txDB` is caller-owned (via `WithTx` — C0 §5.3); the caller opens
  and closes it. `txDB.run` still calls `.Collect(ctx)` immediately
  after `tx.Run` for signature symmetry — the buffered slice shape
  is the seam contract, uniform across both implementations. In
  principle a `txDB` caller could stream (the tx is open), but the
  seam predates that need; C1 does not optimise the tx path
  independently. If a future stage adds streaming (`:iter`,
  `gqlc-1a5`), the seam widens at that point; C1 stays uniform.
- **`neo4j.AccessModeWrite` returns a stub error.** The `WriteQuerier`
  is still empty at C1 (§5.4), so no generated method ever passes
  `AccessModeWrite` to `run` — the arm is unreachable at C1. The
  stubbed error is a defensive `fmt.Errorf` so a *user* who calls
  `q.db.run` directly (impossible: `db` is unexported and
  `driverOrTx` is unexported) or a future test that manually
  constructs an `AccessModeWrite` call gets a named error, not a
  panic. C4 populates the body.
- **`default` arm.** A third `AccessMode` value is a driver-internal
  concern; return a wrapped error so the generated code fails
  gracefully on driver upgrades that add access modes. Unreachable
  in practice; defensive.
- **`fmt` import.** The `db.go` template gains `fmt` in its import
  block; the C0 template did not need it.

**Golden regeneration.** The C0 goldens' `db.go`
(`test/data/codegen/valid/skeleton/golden/db.go` and
`test/data/codegen/valid/queries_ignored/golden/db.go`) carry the
old `driverOrTx.run` signature and the old `driverDB.run` / `txDB.run`
return types. C1 regenerates both under the new template — the byte
diff is the signature swap plus the real read-arm body plus the
`fmt` import. The `-update` flag (C0 §6.4) rewrites the goldens; the
compile fence proves the revised shape type-checks.

### 5.7 Sentinel set — the C1 additions

C0's four sentinels stand (`ErrInvalidPackageName`,
`ErrDuplicateSourceFile`, `ErrDuplicateQueryName`,
`ErrInvalidCardinality`, plus the excluded `ErrFormatFailure`). C1
adds five:

```go
// ErrOutOfC1Scope is returned when a C1-admissible input carries a
// construct C1 does not project: a column or parameter whose resolved
// type is not ResolvedProperty (nodes / edges / edgeUnion / scalars /
// temporals / lists / unknowns), an unrepresentable-width property
// type (INT128+, UINT128+, FLOAT16, FLOAT128+, DECIMAL), a DATE or
// TIMESTAMP property column (C3 owns temporals), a :exec cardinality
// (C4 owns writes), or a query-text const carrying a raw-string-
// hostile character (backtick). Category-grained per C0's precedent
// (mirrors resolver.ErrOutOfR0Scope); C4/C5/C6 retire the sub-cases
// as they land. Introduced at C1.
var ErrOutOfC1Scope = errors.New("out of C1 scope")

// ErrParamNameCollision is returned when two Parameters mangle to
// the same Params-struct field name (§4.2). The fail-message names
// both parameter positions. Introduced at C1.
var ErrParamNameCollision = errors.New("parameter name collision")

// ErrRowFieldCollision is returned when two Columns derive to the
// same Row-struct field name (§4.3). The fail-message names both
// column positions and prompts an explicit AS alias. Introduced at
// C1.
var ErrRowFieldCollision = errors.New("row field name collision")

// ErrAliasRequired is returned when a Column's Name matches neither
// the bare-identifier shape nor the property-access shape (§4.3),
// so the row-field name cannot be derived deterministically. The
// fail-message names the column and prompts an explicit AS alias.
// Introduced at C1.
var ErrAliasRequired = errors.New("alias required")

// ErrIdentifierCollision is returned when two generated top-level
// identifiers in one package collide (§4.4), or a query's method
// name matches a reserved identifier the emission owns (§4.1). The
// fail-message names both identifier sources. C5 hardens the sweep
// as entity structs and decode helpers land (C2/C3). Introduced at
// C1.
var ErrIdentifierCollision = errors.New("identifier collision")
```

**Naming defence — `ErrOutOfC1Scope`, not `ErrUnsupportedType`.**
The C0/resolver precedent (`ErrOutOfR0Scope`) is a category-grained
"out of *this* stage's scope" sentinel that shrinks as later stages
retire sub-cases. `ErrUnsupportedType` reads as terminal —
suggesting no stage will ever handle the case — but nodes/edges/
lists/temporals *are* handled, just later. The stage-scoped naming
signals the deferral. Sub-cases carried in the fail-message (the
resolved type name, the property width name, the cardinality name,
the column position) let the reader disambiguate without a sentinel
per sub-case.

**Naming defence — `ErrParamNameCollision` / `ErrRowFieldCollision`
/ `ErrIdentifierCollision`, three sentinels not one.** Different
fix surfaces: `ErrParamNameCollision` fires on parameter names, fix
is at the query (rename a parameter or a schema property);
`ErrRowFieldCollision` fires on projected columns, fix is at the
query (add an `AS alias`); `ErrIdentifierCollision` fires on
top-level Go identifiers, fix is at the query (rename a query) or
possibly at the schema (a schema-derived struct name). Three
distinct rewrite-sites; three distinct sentinels — errors.Is-able
by consumers who want to trigger a specific IDE quick-fix or wrap
with different retry logic. Rejected: one `ErrNameCollision` — the
fail-message would have to name the category anyway, so the sentinel
identity should carry it.

**`ErrAliasRequired` alongside `ErrRowFieldCollision` — one, not
merged.** `ErrAliasRequired` fires on a single column whose text is
richer than the two clean shapes; `ErrRowFieldCollision` fires on
two columns whose names both derive but collide. Different arities
(one column vs two) and different fixes (add an alias vs disambiguate
between two aliases). The rejected merge would produce a sentinel
whose fail-message conditionally names one or two columns, which
grep-across-source auditability finds worse than two clean names.

**Closed set for the C1 sweep.** `allSentinels` at C1:

```go
var allSentinels = []error{
    ErrInvalidPackageName,   // C0
    ErrDuplicateSourceFile,  // C0
    ErrDuplicateQueryName,   // C0
    ErrInvalidCardinality,   // C0
    ErrOutOfC1Scope,         // C1
    ErrParamNameCollision,   // C1
    ErrRowFieldCollision,    // C1
    ErrAliasRequired,        // C1
    ErrIdentifierCollision,  // C1
}
```

Nine sentinels. `ErrFormatFailure` stays excluded (C0 §9.2 rationale
unchanged). Every C1 member has at least one negative fixture (§6.4);
the reachability sweep is C0's `TestSentinelReachability` unchanged.

---

## 6. The golden harness — C1 revision

C0 §6's harness stands: the `test/data/codegen/{valid,invalid}` layout,
the nested Go module, the `manifest.json` shape, the `-update` flag,
the testify suites, the compile fence. C1 revises the fixture set
only, not the harness code.

### 6.1 Fixture strategy

C0's two valid fixtures (`skeleton`, `queries_ignored`) stay unchanged
— they cover the models-only and front-end-only paths, both still
relevant. C1 adds valid fixtures for each capability slice below,
each with its own schema (a fixture directory holds one schema per
D6 posture). Fixture-per-capability sizing keeps the golden diffs
per fixture small; a single-fixture-covers-everything approach would
put the diffs in one 500-line diff every future stage has to read.

### 6.2 C1 valid fixtures

Under `test/data/codegen/valid/`, each new directory holds a `schema.gql`,
one or more `.cypher` files, a `manifest.json`, and a `golden/`
subdirectory with the complete generated package:

| Fixture | Coverage |
|---|---|
| `one_col_one_param_one` | `:one`, single column bare-value return, single parameter bare arg (`func (q *Queries) PersonName(ctx, id int64) (string, error)`). Exercises the smallest read-method surface: single-column bare-value, single-param bare-arg, `:one` empty→`ErrNoRows` fixture prose. |
| `one_col_many` | `:many`, single column bare-value slice return, zero params (`func (q *Queries) AllPersonNames(ctx) ([]string, error)`). Exercises the ergonomics minimum for `:many`. |
| `many_col_one_row` | `:one`, two-plus columns yielding an XRow, single parameter bare arg. Exercises Row emission, decode assembly, `ErrNoRows`. |
| `many_col_many` | `:many`, two-plus columns yielding `[]XRow`, two-plus params yielding an XParams. Exercises the full Params+Row surface. |
| `nullable_columns` | Mixes nullable and non-nullable property columns, some nullable-arriving-nil test cases folded into the driver-side response (no assertion at codegen; fixture prose documents intent). |
| `nullable_parameter` | A nullable parameter (`*int` field on Params); exercises D3 Resolved's symmetric parameter treatment. Encode direction verified at the code cycle. |
| `multi_source_files` | Two `.cypher` files in one fixture, each declaring one query; goldens include two `<name>.cypher.go` files. Exercises per-source grouping (D4 Resolved). |
| `alias_bare_variable_ambiguity` | Two queries — one `RETURN p.name, p.age`, one `RETURN p.name AS name, p.age` — that both derive a two-plus-column XRow whose first field is `Name` (property-access shape in query 1; bare-identifier shape from alias in query 2). Not a collision (they are in different queries, so `<Method>Row` names are distinct); each query's Row is separately emitted with identical field-1 name `Name`. Documents the intentional alias-and-bare ambiguity per §4.3 shape 2. The `p.age` second column ensures a Row struct is emitted (single-column queries render bare-value and never exercise Row-field derivation, so a `RETURN p.name` / `RETURN p.name AS name` pair proves nothing). |
| `all_widths` | One query projecting one column per representable width in §5.1 (STRING, BOOL, INT/INT8/INT16/INT32/INT64, UINT/UINT8/UINT16/UINT32/UINT64, FLOAT/FLOAT32/FLOAT64). Exercises the full type-mapping table. |

Nine new valid fixtures. Each is one or two `.cypher` files, one
`schema.gql`, one `manifest.json`, and a `golden/` tree. The
`golden/` trees compile under the C0 compile fence
(`just test-codegen-fence`) — this is the whole point of the nested
module.

### 6.3 Schema fixture text — illustrative

Every C1 valid fixture's `schema.gql` is small and hand-written; the
corpus grows one schema per fixture rather than one shared schema.
Illustrative:

`test/data/codegen/valid/one_col_one_param_one/schema.gql`:

```gql
CREATE PROPERTY GRAPH TYPE OneColOneParamOne AS {
    (:Person {
        id   :: INT64 NOT NULL,
        name :: STRING NOT NULL
    })
}
```

Paired query file
`test/data/codegen/valid/one_col_one_param_one/queries.cypher`:

```cypher
// name: PersonName :one
MATCH (p:Person) WHERE p.id = $id RETURN p.name
```

Resolved: `Columns = [{Name: "p.name", Type: ResolvedProperty{STRING,
false}}]`, `Parameters = [{Name: "id", Type:
ResolvedProperty{INT64, false}}]`, `Statement = read`. C1 admission:
one column, `Column.Type` is `ResolvedProperty` (§4 Phase A); one
param, `Parameter.Type` is `ResolvedProperty` (§4 Phase A). Row-field
derivation: `p.name` matches property-access shape → `Name`. Params-
field derivation: `id` → `Id`. Single column → bare value (§3.1);
single parameter → bare arg. Emitted method:

```go
func (q *Queries) PersonName(ctx context.Context, id int64) (string, error) {
    records, err := q.db.run(ctx, personNameQueryText, map[string]any{"id": id}, neo4j.AccessModeRead)
    if err != nil {
        return "", err
    }
    if len(records) == 0 {
        return "", ErrNoRows
    }
    if len(records) > 1 {
        return "", ErrMultipleResults
    }
    value, _, err := neo4j.GetRecordValue[string](records[0], "p.name")
    if err != nil {
        return "", fmt.Errorf("PersonName: decode column %q: %w", "p.name", err)
    }
    return value, nil
}
```

Notice `GetRecordValue`'s key is `"p.name"` — the resolver
projection's `Column.Name` verbatim (which is the driver's column
name for a `RETURN p.name` projection). An alias-carrying variant
would resolve to `Column.Name == "<alias>"`; the code emission
tracks whichever text the resolver settled on. This is the sole
role of `Column.Name` at the decode surface: it names the record
column, orthogonal to the Row-struct field name derivation.

### 6.4 C1 invalid fixtures — one per new sentinel

Added under `test/data/codegen/invalid/`:

| Fixture | Sentinel | Coverage |
|---|---|---|
| `out_of_c1_scope_node_column`  | `ErrOutOfC1Scope`      | `MATCH (p:Person) RETURN p` — a whole-entity `ResolvedNode` column, out of C1 scope. |
| `out_of_c1_scope_exec`         | `ErrOutOfC1Scope`      | `// name: RemovePerson :exec ...` — `:exec` cardinality, C4's business. |
| `out_of_c1_scope_int128`       | `ErrOutOfC1Scope`      | A schema property typed `INT128` projected as a column — unrepresentable width. |
| `param_name_collision`         | `ErrParamNameCollision`| Two parameters `$min_age` and `$minAge` both mangling to `MinAge`. |
| `row_field_collision`          | `ErrRowFieldCollision` | `RETURN p.name AS x, p.age AS x` — two columns aliased to the same name. (The resolver admits identical column names; the collision is at codegen's field-name derivation.) |
| `alias_required_function_call` | `ErrAliasRequired`     | `RETURN count(*)` — expression column with no alias. |
| `alias_required_expression`    | `ErrAliasRequired`     | `RETURN p.age + 1` — arithmetic expression column with no alias. |
| `identifier_collision_reserved`| `ErrIdentifierCollision`| `// name: New :one ...` — method name collides with C0's `New` constructor. |

Eight invalid fixtures — one per new sentinel. Each maps to its
sentinel in the C0 `sentinelByName` map. The reachability sweep
asserts every C1 sentinel has at least one fixture; the map
assertion asserts every declared fixture maps to a known sentinel.
`ErrIdentifierCollision` is exercised solely by
`identifier_collision_reserved` at C1; the additional exported-vs-
exported collision axis (two user-defined identifiers, e.g. a method
and a Row struct colliding across queries) has no clean single-file
fixture at C1's scope — the collision only arises when two queries
produce identifiers along different codegen paths, and the
constructions currently reachable at C1 (method name from
annotation, `<Method>Params`, `<Method>Row`) all derive from
`NamedQuery.Name`, so a cross-query exported-identifier collision
already fails as `ErrDuplicateQueryName` at C0's front-end gate. C5
hardens the sweep against entity-struct and decode-helper
identifiers (ADR 0010 D7), at which point a genuine
exported-vs-exported collision becomes reachable and a fixture
lands there.

C0's four invalid fixtures (`duplicate_query_name`,
`duplicate_source_file`, `invalid_cardinality`, `invalid_package_name`)
stay unchanged — the C0 sentinels are still in `allSentinels`, so the
sweep needs their fixtures.

### 6.5 Determinism — C1 additions

C0's `TestDoubleRun` (§8 of C0 spec) runs unchanged. C1's kernel
adds three new ordered surfaces, none of which iterate a map:

- Per-source grouping: `Input.Queries` slice order (existing).
- Per-query Params fields: `Validated.Parameters` order
  (query-wide first-appearance).
- Per-query Row fields: `Validated.Columns` order (projection
  order).
- Cross-query identifier collision sweep: single-pass map insertion
  over identifiers in emission order; the map is not iterated for
  output, only queried; the first collision in insertion order
  wins.

Every ordered surface is either the resolver's guaranteed order or
insertion-order. The doubled-run test remains a strong contract:
byte-identical output twice on the same input.

### 6.6 Non-obvious harness invariants — C1 additions

C0's §6.7 invariants stand. C1 adds:

- **Every valid fixture's `golden/` must compile with the pinned
  driver version.** `test/data/codegen/go.mod` pins
  `neo4j-go-driver/v5 v5.28.4` at C0; the emitted `db.go` and
  `<name>.cypher.go` files use `session.ExecuteRead[T]`
  (generic-instantiated per §5.6) and `neo4j.GetRecordValue[T]`
  (generic-instantiated per §5.5) — both APIs are stable in v5.28.4
  (verified against
  `pkg.go.dev/github.com/neo4j/neo4j-go-driver/v5/neo4j`; D7
  standing instruction directs re-verification at the C1 code
  cycle).
- **Owner directive (2026-07-11): generated code must uphold
  gqlc's own linting and formatting standards.** The compile fence
  is the primary gate: `just test-codegen-fence` runs `go build
  ./... && go vet ./...` inside the nested module. C1's new
  emissions (methods, structs, doc comments, per-query row assembly
  bodies) must additionally lint-clean under gqlc's `.golangci.yml`
  — mirror the linter invocation across the fence recipe. If lint
  rules constrain a template (e.g., `stylecheck`'s `ST1000`
  requiring package-level doc comments), the template accommodates:
  every emitted file receives a package-level `//` comment even
  when the file body is trivially small. Rejected: relaxing lint
  rules for generated files (`//nolint:all` at the file head) — the
  fix is to make templates produce lint-clean code, not to hide
  from it. If the linter runs against the nested module in CI, this
  is enforced automatically; if not, a `just test-codegen-lint`
  recipe joins §7 unifying build + vet + lint under one invocation.
- **Fixture files named for shape.** `one_col_many` names the
  shape (single column, many rows); `many_col_one_row` names the
  shape. Sentinel-fixtures name the sentinel: `param_name_collision`
  is the sentinel, not a query shape. Same convention as R1's
  fixture-naming style (R1 spec §6.6).

---

## 7. C1 capability scope — what emits

**In scope:** an `Input.Queries` slice whose every element is a
`NamedQuery` whose `Validated`:

- `Statement == StatementRead`.
- `Columns` is a non-empty slice.
- Every `Columns[i].Type` is `ResolvedProperty` (schema-side
  property width per §5.1's representable rows).
- `Parameters` is any slice (empty is legal).
- Every `Parameters[i].Type` is `ResolvedProperty` (schema-side
  property width per §5.1's representable rows).
- Cardinality is `CardinalityOne` or `CardinalityMany`.

**Out of scope, routed to the appropriate sentinel:**

| Construct                                              | Sentinel                | Stage owner |
|--------------------------------------------------------|-------------------------|-------------|
| `ResolvedNode` column                                  | `ErrOutOfC1Scope`       | C2          |
| `ResolvedEdge` column                                  | `ErrOutOfC1Scope`       | C2          |
| `ResolvedEdgeUnion` column                             | `ErrOutOfC1Scope`       | C5          |
| `ResolvedScalar` column                                | `ErrOutOfC1Scope`       | C3          |
| `ResolvedTemporal` column                              | `ErrOutOfC1Scope`       | C3          |
| `ResolvedList` column                                  | `ErrOutOfC1Scope`       | C3          |
| `ResolvedUnknown` column                               | `ErrOutOfC1Scope`       | C3          |
| `ResolvedProperty` column with `DATE` / `TIMESTAMP`    | `ErrOutOfC1Scope`       | C3          |
| `ResolvedProperty` column with `INT128` / `INT256`     | `ErrOutOfC1Scope`       | C3          |
| `ResolvedProperty` column with `UINT128` / `UINT256`   | `ErrOutOfC1Scope`       | C3          |
| `ResolvedProperty` column with `FLOAT16`               | `ErrOutOfC1Scope`       | C3          |
| `ResolvedProperty` column with `FLOAT128` / `FLOAT256` | `ErrOutOfC1Scope`       | C3          |
| `ResolvedProperty` column with `DECIMAL`               | `ErrOutOfC1Scope`       | C3          |
| Non-`ResolvedProperty` parameter (whole node/edge, etc.) | `ErrOutOfC1Scope`     | C3          |
| Unrepresentable-width parameter (INT128+, DECIMAL, …)  | `ErrOutOfC1Scope`       | C3          |
| `CardinalityExec`                                      | `ErrOutOfC1Scope`       | C4          |
| Query text containing a Go raw-string-hostile backtick | `ErrOutOfC1Scope`       | C4-or-later |
| Method name matches reserved identifier                | `ErrIdentifierCollision`| —           |
| Two params mangling to one field                       | `ErrParamNameCollision` | —           |
| Two columns deriving to one Row field                  | `ErrRowFieldCollision`  | —           |
| Column text neither bare-ident nor prop-access         | `ErrAliasRequired`      | —           |
| Two emitted top-level identifiers colliding            | `ErrIdentifierCollision`| C5 hardens  |

**Silently accepted (not routed anywhere):**

- `Validated.Distinct == true`. `DISTINCT` is enforced by the
  database executing the verbatim query text (D2 Resolved: it
  changes which rows arrive, not their types); the emitted method
  is byte-identical to a non-DISTINCT version. Same posture as R5's.
- `Validated.Columns[i].GroupingKey`. Same as `Distinct` — no
  signature consequence (D2 Resolved).
- Comments in the query text. ADR 0005: text runs verbatim; the
  `<name>QueryText` const carries them.

**The C0 shape stands unchanged** for anything C1 does not touch:
package-name derivation (C0 §5.1), generated-file header (C0 §5.2),
`Queries` handle constructors (`New`, `WithTx`, both unchanged),
`driverOrTx` interface shape (unchanged), `txDB` behaviour
(unchanged), `models.go` empty (C2 fills), `querier.go`'s
`WriteQuerier` and `Querier` embedding shape (unchanged), the
compile-time `Querier` assertion (unchanged), the sentinel-set
discipline (unchanged), the double-run determinism test (§6.5
unchanged), the compile fence (§7 unchanged).

---

## 8. Compile fence (unchanged)

The C0 `just test-codegen-fence` recipe (`cd test/data/codegen && go
build ./... && go vet ./...`) covers C1's emissions without change:
the nested module builds every fixture's `golden/` tree, so every
new method, Params/Row struct, and per-source file type-checks
against the pinned driver. Failure modes:

- **A template regression producing a type-check error.** The fence
  fails with the standard Go compiler error naming the file
  (`test/data/codegen/valid/<fixture>/golden/<name>.cypher.go:12: ...`),
  pointing at the exact fixture and line — one-hop diagnostic from
  fence failure to fix site.
- **A driver-version drift.** Bumping `neo4j-go-driver/v5` (a
  `test/data/codegen/go.mod` change) may break emitted `ExecuteRead`
  or `GetRecordValue` shapes. The fence catches this at the version
  bump — a version-bump PR flags emission changes upstream of user
  code.
- **A `go vet` warning.** Unused imports (an emission that includes
  `errors` without emitting an `errors.Is` call, an emission that
  includes `fmt` without emitting a `fmt.Errorf`) fail here.

C1 does not add a second fence recipe. The linter integration
(§6.6's owner directive) may add a `just test-codegen-lint` recipe
running `golangci-lint run` against the nested module; the addition
is a §6 harness concern, not §8 template surface.

---

## 9. Determinism (unchanged)

C0 §2.3 and §8's determinism invariants stand at C1. C1 adds no map
iteration to the emission walk (§6.5 enumerates every ordered
surface). The double-run test (C0 §8) fires on every C1 valid
fixture unchanged.

---

## 10. Sentinel set delta — the C1 view

Recap of §5.7's set with the delta callout:

- **C0 carries:** `ErrInvalidPackageName`, `ErrDuplicateSourceFile`,
  `ErrDuplicateQueryName`, `ErrInvalidCardinality`,
  `ErrFormatFailure` (excluded from sweep).
- **C1 adds:** `ErrOutOfC1Scope`, `ErrParamNameCollision`,
  `ErrRowFieldCollision`, `ErrAliasRequired`,
  `ErrIdentifierCollision`.
- **Total in `allSentinels`:** nine (four C0 + five C1).
- **Generated into user's package (not swept):** `ErrNoRows`,
  `ErrMultipleResults`. `Generate` never returns these; they are
  values the emitted `:one` methods construct at runtime.

Reachability discipline unchanged from C0: each `allSentinels`
member has ≥1 invalid fixture; each `sentinelByName` value is in
`allSentinels`. The sweep is `TestSentinelReachability` — C0's test,
scale-invariant.

---

## 11. Out-of-scope table

Every downstream capability C1 does not deliver, with the stage that
owns it. Read as ADR 0010 D7 unpacked to the C1-vs-later boundary
(C0's version tightens as C1 slice retires from it):

| Capability                                         | Stage owner |
|----------------------------------------------------|-------------|
| Entity structs in `models.go` (schema-shaped only) | C2          |
| Entity type-name resolution (`Name` first, mangle fallback, eager multi-label check) | C2 |
| Entity decode helpers (`dbtype.Node` → struct)     | C2          |
| Entity column projection (`RETURN p` for a node)   | C2          |
| Collections (`list<T>`)                            | C3          |
| Six temporals via `dbtype`                         | C3          |
| Property columns of type `DATE` / `TIMESTAMP`      | C3          |
| Unrepresentable-width sentinels (INT128+, FLOAT16, DECIMAL) | C3   |
| FLOAT32 schema-width contract (encode widen / decode narrow) | C3 |
| `unknown` / `scalar null` / `scalar map` → `any`   | C3          |
| Writes (`:exec`, zero-column methods, `WriteQuerier` population) | C4 |
| `ExecuteWrite` path in `driverDB.run`              | C4          |
| Cardinality × shape rejection (`:exec` on a projection query, `:one`/`:many` on a zero-column write) | C4 |
| Raw-string-hostile query text (backtick escape / fallback) | C4-or-later |
| `edgeUnion` sealed interfaces + `//sumtype:decl`   | C5          |
| Package-level collision sweep hardening (entity structs / decode helpers as identifier sources) | C5 |
| Version-stamp polish (`-ldflags -X` wiring)        | C6          |
| Session-config polish                              | C6          |
| `gqlc-0aa` re-scope against D4's no-runtime-package decision | C6 |
| `:iter` streaming cardinality (fourth enum value)  | `gqlc-1a5` (post-v1) |
| Configuration file (`gqlc.yaml` analogue), CLI    | future config effort |
| Disk writes, out-dir sync (stale deletion)         | future CLI effort |

Rows above the `gqlc-1a5` line are staged by ADR 0010 D7; the last
two are ADR 0010 D6 futures.

---

## 12. Definition of done for C1 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is
out of scope of this document. The spec is done when:

1. This file exists at `docs/specs/codegen-stage-c1.md`, committed
   on branch `codegen-c1-spec`.
2. §3 pins the C1 method surface (Params / Row / cardinality ×
   method-shape ergonomics), the `ReadQuerier` population rule,
   and the generated `ErrNoRows` / `ErrMultipleResults` sentinels.
3. §4 gives the naming kernel: method verbatim + reserved-
   identifier check (§4.1), the Params-field one-mangle rule
   (§4.2), the Row-field text-shape analysis and the
   `AS-required`/`collision` verdicts (§4.3), the package-level
   collision sweep (§4.4).
4. §5 gives the emission templates: the property → Go type table
   (§5.1), Params/Row struct rendering (§5.2), method rendering
   (§5.3), `querier.go`'s `ReadQuerier` population (§5.4), the
   per-source file layout and per-query row assembly (§5.5), the
   real `driverDB.run` read-arm body (§5.6).
5. §5.7 names and defends the five new sentinels
   (`ErrOutOfC1Scope`, `ErrParamNameCollision`,
   `ErrRowFieldCollision`, `ErrAliasRequired`,
   `ErrIdentifierCollision`) and confirms the closed set.
6. §6 designs the fixture set: the nine valid fixtures (§6.2), the
   eight invalid fixtures (§6.4), and the fixture-per-capability
   discipline.
7. §7 states the C1 capability scope in shape terms and lists its
   out-of-scope complement with the sentinel each construct routes
   to and the stage that owns the next widening.
8. §8 confirms the C0 compile fence covers C1 emissions without
   change; §6.6 flags the linting-parity owner directive.
9. §9 confirms the C0 determinism invariants stand.
10. §10 recaps the sentinel set delta against C0.
11. §11 enumerates every downstream capability with its stage
    owner.
12. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer);
every blocker he raises is fixed on this same branch before the
branch merges. Cycle 2 (the C1 code cycle,
`codegen-c1-implementation` stacked on this branch) begins only
when the spec cycle merges.
