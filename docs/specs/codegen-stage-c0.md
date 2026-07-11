# Stage C0 spec — codegen: harness and skeleton

The implementation brief for Stage C0 of `internal/codegen`, the first slice
of the codegen epic. Build this **test-first**. Scope, sequencing, and
error posture are set by ADR 0010; this document is the precise decision
surface for C0 alone: the nested golden module and its compile fence, the
`internal/queryfile` annotation front end, the generator API shape, the
skeleton emission (`db.go` / `querier.go` / `models.go`) that already
compiles with zero queries in the input, the double-run byte-identity
determinism test, the C0 sentinel set, and the golden harness.

Stage C0 emits a **compiling but empty** generated package — package name,
generated-file header, `Queries` handle with `New` / `WithTx` / the
unexported run indirection, the three `Querier` interfaces (empty),
`models.go` empty — plus the front-end plumbing (`internal/queryfile`)
that every later stage will feed. No query is projected in C0; the input
`Queries []NamedQuery` is either empty or the fixture exists only to prove
the front end round-trips. Later stages (ADR 0010 D7) evolve the surface;
do not anticipate them here.

---

## 1. Deliverables

- `internal/queryfile/` — the annotation front end (§4):
  `queryfile.go` (the `Parser` seam, `New`, `Parse`), `annotated.go`
  (`AnnotatedQuery`, `Cardinality`), `errors.go` (the queryfile sentinel
  set), and an internal parse kernel that walks a query file line by line
  and produces `[]AnnotatedQuery`. Own package because it is a small
  language front end (an annotation grammar plus a query-body slicer),
  sibling of `internal/schema/gql` in role, not part of `internal/codegen`.
- `internal/codegen/` — the generator (§3, §5):
  `codegen.go` (the `Generator` seam, `New`, `Generate`, functional
  options), `input.go` (`Input`, `NamedQuery`, `File`, `Cardinality`
  re-exported so `internal/codegen` consumers do not import
  `internal/queryfile`), `errors.go` (the C0 codegen sentinel set),
  and a pure emission kernel called by `Generate`.
- `test/data/codegen/` — the **nested Go module** (§6): its own
  `go.mod` pinning `github.com/neo4j/neo4j-go-driver/v5` and Go 1.26.2,
  a top-level `_include.go` proving the tree compiles, and the fixture
  corpus at `valid/` and `invalid/`.
- `test/data/codegen/valid/<name>/` — one directory per valid fixture,
  each holding a schema `.gql`, one or more `.cypher` query files (the
  annotated form), and a `golden/` subdirectory containing the complete
  generated package (`db.go`, `querier.go`, `models.go`, and per-source
  `<name>.cypher.go` files) exactly as `Generate` returns them.
- `test/data/codegen/invalid/<name>/` — one directory per negative
  fixture: schema + query file(s) + a `manifest.json` naming the
  expected sentinel from either package.
- `internal/codegen/codegen_test.go` — the testify suite (§6): golden
  round-trip, invalid-fixture map totality, double-run determinism
  (§8), sentinel-reachability sweep (§9).
- `internal/queryfile/queryfile_test.go` — the queryfile front-end
  suite (§4, §9): `AnnotatedQuery` round-trip plus queryfile sentinel
  reachability.
- `justfile` — one recipe added (§7): `test-codegen-fence` runs
  `go build ./... && go vet ./...` inside `test/data/codegen`, used
  identically locally and in CI.

Nothing downstream of the emission skeleton is built. Params / Row
structs, entity structs, decode helpers, and per-query methods land in
C1–C5 (ADR 0010 D7).

---

## 2. Architecture

### 2.1 Package layout

Two new packages, both siblings of `internal/resolver`:

- `internal/queryfile/` — the annotation front end. Imports nothing from
  gqlc's internal tree; a raw byte slice in, `[]AnnotatedQuery` out.
  Sibling of `internal/schema/gql` in role: a small language front end
  whose only job is to lower a text file into a curated model. It is
  deliberately *not* part of `internal/codegen` — ADR 0010 D1 calls it
  out as its own small package because a golden fixture is an annotated
  query file, and `internal/codegen` should not carry the annotation
  grammar for a stage that produces empty output.
- `internal/codegen/` — the generator. Imports `internal/queryfile`
  (only for the re-exported `Cardinality` enum; C0's `Generate` receives
  `NamedQuery`s already lowered by the caller — see §3), `internal/resolver`
  (for the `resolver.ValidatedQuery` field on `NamedQuery`, unused in C0
  but present for wire stability), `internal/schema` (for `schema.Schema`
  on `Input`), and the stdlib. **Nothing in gqlc imports the neo4j
  driver** — the generator emits text; the emitted text imports the
  driver in the user's module (ADR 0010 D4).

Nothing imports either package back. The pipeline orchestration
(split → parse → resolve → generate) stays out of both: tests wire it
inline; the future CLI bead owns it in production (ADR 0010 D6).

### 2.2 The `Generator` seam

Same shape as the resolver's `QueryResolver` (ADR 0010 D4 pins the
one-method interface):

```go
// Generator emits a generated package from a schema plus a batch of
// named queries. The concrete producer is *Codegen; consumers accept
// the interface so an alternative target (a future TypeScript emitter,
// a second-driver Go emitter) can substitute without importing this
// package's target-specific types.
type Generator interface {
    Generate(Input) ([]File, error)
}

var _ Generator = (*Codegen)(nil)
```

`Codegen` is the concrete struct. `New(opts ...Option) *Codegen` returns
the concrete type; a consumer that only needs to generate takes a
`Generator`. No compile-time inputs at C0 (schema and queries arrive on
`Input`, not `New`), so `New` takes only options for now; the option
surface is empty at C0 (`Option` is defined so later stages can add
knobs — e.g., a version-stamp override for testing — without churning
the constructor).

### 2.3 Purity, determinism, short-circuit

- **Pure.** `Generate` is a pure function of its `Input`: no I/O, no
  goroutines, no time source (the header's version stamp is a build-time
  constant embedded via `-ldflags` or defaulted, §5.2 — not read from the
  environment), no mutation of inputs. Generating the same `Input`
  twice yields byte-identical `[]File` (enforced by the §8 double-run
  test).
- **Deterministic.** Ordered iteration everywhere. `Schema.Nodes` and
  `Schema.Edges` are maps; the emission walk sorts them by their
  canonical keys (`graph.LabelSetKey` for nodes, `schema.EdgeKey`
  triple-lex for edges — the same convention `schema.Schema.MarshalJSON`
  already uses). `Input.Queries` iterates in slice order (the caller's
  first-appearance discipline). The output `[]File` is sorted by
  `File.Path` before return.
- **Short-circuit.** First-error wins — codegen sentinels + queryfile
  sentinels alike. Zero value on error: `Generate` returns `(nil, err)`,
  not a partial `[]File`.
- **Batch, not per-query.** ADR 0010 D1: cross-query outputs (the single
  `Queries` struct, name-collision detection, per-file import blocks)
  need the global view. C0's skeleton is trivially batch (no queries
  are projected), but the API shape is fixed here so C1+ inherits it
  without churn.

### 2.4 Nothing named `emit` at the seam

The `internal/codegen` kernel file is `generate.go`; the exported entry is
`Generate`. Per CONTEXT.md's Generation-language vocabulary:
**generation** is the stage, **generated code** is what it produces.
No sibling terms (`emitter.go`, `Emission` type) are introduced. The
internal kernel function is named `generate` (not `emit` or `render`).
Consistent with the resolver's `resolve.go`/`resolve()` and
`schema/gql`'s `resolve.go`/`rawSchema.resolve()` posture.

---

## 3. The generator API shape

The public surface `Generate` takes and returns, pinned at C0 because
later stages extend it (fields added to `NamedQuery`, cells filled in the
type-mapping table) but never restructure it.

### 3.1 Input

```go
// Input is the batch a Generate call runs over: exactly one schema
// (v1 posture: one graph type per generated package) plus the
// annotated, parsed, resolved queries to project. Fields are added
// as later stages need them; the shape stays batch-shaped and
// caller-lowered.
type Input struct {
    Schema  schema.Schema
    Queries []NamedQuery
}
```

- **`Schema`** is the parsed schema type from `internal/schema`. The
  emission walk reads `Schema.Name` (for the derived package name, §5.1)
  and iterates `Schema.Nodes` / `Schema.Edges` (at C2+ — C0 emits no
  entity structs). ADR 0010 D1: schema needs no annotation front end;
  `internal/schema/gql` already produces `schema.Schema` from source.
- **`Queries`** is the slice of `NamedQuery` in caller order. Empty is
  legal (the models-only adoption path, ADR 0010 D1 last paragraph) —
  and at C0 it is the *only* interesting input, because C0 does not
  project queries. Fixtures with a non-empty `Queries` slice exist to
  prove the front end lowers annotations correctly; the skeleton
  emission ignores the slice's contents.

### 3.2 `NamedQuery`

```go
// NamedQuery is one annotated query lowered by the front end and
// resolved by internal/resolver, in the form Generate consumes.
type NamedQuery struct {
    Name        string
    Cardinality Cardinality
    SourceFile  string
    SourceText  string
    Validated   resolver.ValidatedQuery
}
```

- **`Name`** is the annotation-declared identifier (`// name: PeopleOverAge
  :many` → `"PeopleOverAge"`). Must already be a valid exported Go
  identifier — `^[A-Z][A-Za-z0-9]*$` — enforced by the front end (§4);
  Generate does not re-validate.
- **`Cardinality`** is the author-declared row axis (§3.4).
- **`SourceFile`** is the query file's basename (`people.cypher`),
  carried forward as the grouping key for the per-source generated
  file (`people.cypher.go`, ADR 0010 D4 §Resolved). C0 emits no per-source
  files (no methods to emit), so the field is present for wire stability
  and used by C1+.
- **`SourceText`** is the exact query text between this annotation and
  the next one (or EOF), preserved byte-for-byte per ADR 0005 —
  generated code executes the verbatim text; codegen never
  reconstructs a query.
- **`Validated`** is the resolver's output. C0 does not read any field
  of it; C1+ derives Params, Row, and method surfaces from it.

Codegen never reaches back into `query.Query`. ADR 0010 D1 audit:
`ValidatedQuery` + `Schema` + this envelope is sufficient — codegen
never asks the parser anything.

### 3.3 `File`

```go
// File is one emitted file: its path relative to the caller's out
// directory, and its complete, gofmt-clean contents. Path is the
// canonical form the caller writes to disk. Generate never touches
// disk (ADR 0010 D4); the caller owns I/O.
type File struct {
    Path     string
    Contents []byte
}
```

`Generate` returns `[]File` sorted by `Path` (§2.3). Two files with
identical paths in the returned slice is a bug — the caller can rely on
the slice being a set keyed by `Path`.

### 3.4 `Cardinality`

```go
// Cardinality is the author-declared consumer-side row axis of a
// NamedQuery (CONTEXT.md: one row, a list of rows, or no rows). Open
// enum: :iter is reserved for post-v1 (ADR 0010 D8, gqlc-1a5).
type Cardinality int

const (
    CardinalityOne Cardinality = iota + 1
    CardinalityMany
    CardinalityExec
)
```

- Enum values start at 1, not 0 — the zero value `Cardinality(0)` means
  "not set" and is a bug the front end never produces; if it ever leaks
  into `Generate`, C0 rejects with `ErrInvalidCardinality` (§9). This
  keeps `NamedQuery`'s zero value obviously wrong (it fails), consistent
  with the reject-don't-guess posture.
- Open axis. ADR 0010 D8: `:iter` is reserved as a fourth value the
  enum absorbs without an annotation-grammar break; C0 does not add the
  constant (unrepresentable values are the point of an open enum), so
  no future churn is required when `gqlc-1a5` lands.
- `Cardinality.String()` returns the wire tag (`"one"` / `"many"` /
  `"exec"`), matching sqlc's tokens minus the colon. Used by both
  packages for error messages and by tests for golden encoding.

The type is defined in `internal/queryfile/annotated.go` and re-exported
as a `type Cardinality = queryfile.Cardinality` alias from
`internal/codegen/input.go` — one enum, two package-level identifiers,
so `internal/codegen` consumers do not import `internal/queryfile`
just to name a cardinality.

### 3.5 The `Generate` signature and the concrete constructor

```go
// New returns a Codegen with the given options applied. C0's option
// surface is empty; later stages add knobs (e.g., version-stamp
// override for goldens, target-driver selection).
func New(opts ...Option) *Codegen

// Generate emits the generated-package file set for a batch. Pure,
// deterministic, short-circuits on the first error (§2.3).
func (c *Codegen) Generate(in Input) ([]File, error)
```

The concrete `*Codegen` is what `New` returns (idiom: return structs,
accept interfaces); `Generator` (§2.2) is the seam consumers accept.

---

## 4. The annotation front end (`internal/queryfile`)

The grammar and the model for the raw-query-file → `[]AnnotatedQuery`
step. ADR 0010 D1 calls this out as its own package; C0 is where it
lands because a valid golden fixture *is* an annotated query file.

### 4.1 The grammar

sqlc's annotation grammar, byte-for-byte, adapted to Cypher's line
comment token (`//` instead of `--`). ADR 0010 D1 Resolved: no
namespaced prefix (`// gqlc: name: ...`) — annotations appear only in
gqlc-owned query files, so a prefix buys nothing.

Line syntax:

```
// name: <ident> :<cardinality>
```

- Leading `//` is Cypher's line-comment token. Whitespace between `//`
  and `name:` is permitted and discarded; the queryfile parser matches
  `^//\s*name:\s*(\S+)\s+:(\S+)\s*$` (single space between the name and
  the `:cardinality` is standard, but the parser tolerates any
  whitespace run between them).
- `<ident>` is the query name, must match `^[A-Z][A-Za-z0-9]*$` (a valid
  exported Go identifier). Enforced here, not deferred to codegen —
  ADR 0010 D2 Q5.1: "the annotation name verbatim; it must already be
  a valid exported Go identifier".
- `<cardinality>` is one of `one` / `many` / `exec`. Unknown tokens
  produce `ErrUnknownCardinality`. `:iter` is not currently accepted
  (ADR 0010 D8; `gqlc-1a5` adds it).
- Anything after the annotation up to the next annotation line (or EOF)
  is the query body. Leading and trailing whitespace on the body are
  trimmed; interior text (including blank lines) is preserved
  byte-for-byte per ADR 0005.

Every query in a query file MUST carry an annotation (ADR 0010 D1
Resolved: "maximally strict, house reject-don't-guess posture; no
anonymous queries"). Content before the first annotation is a
scan-error unless it is entirely comment-and-blank — file-header
comments are permitted.

### 4.2 `AnnotatedQuery`

```go
// AnnotatedQuery is one annotated query in the caller's file: its
// author-declared name and cardinality, plus the verbatim query text
// executed by the driver (ADR 0005).
type AnnotatedQuery struct {
    Name        string
    Cardinality Cardinality
    Text        string
}
```

- **`Name`** verbatim from the annotation, already validated as a Go
  identifier.
- **`Cardinality`** parsed from the annotation token.
- **`Text`** is the query body between this annotation and the next,
  trimmed of leading/trailing whitespace only. Interior structure
  (indentation, blank lines, inline `//` comments) is preserved so the
  driver sees the author's exact query.

### 4.3 The `Parser` seam

```go
// Parser lowers a query file's raw bytes into the annotated queries
// it declares.
type Parser interface {
    Parse(r io.Reader) ([]AnnotatedQuery, error)
}

func New() Parser
```

Same shape as `schema.Parser` (`internal/schema/parser.go`). `New`
returns the concrete parser; consumers accept the interface. No
compile-time inputs — the parser is a pure text-to-model step, no
schema needed.

### 4.4 Kernel structure

A hand-rolled scanner, not ANTLR — the grammar is one line, not a
tree. The parse walks the input line-by-line (using `bufio.Scanner`
with the default `ScanLines` split), keeps a running `body` byte
buffer, and flushes on each annotation-line hit:

1. On file entry, the current query is unset. Any non-comment,
   non-blank line before the first annotation → `ErrTextBeforeAnnotation`.
2. On matching an annotation line: if there is a current query, emit
   its `AnnotatedQuery` with the buffered body; then start a new
   current query with the parsed name and cardinality; buffer resets.
3. Otherwise: append the line to the body buffer verbatim.
4. On EOF: emit the current query with the buffered body if one
   exists; otherwise `ErrNoQueries` (an empty query file is a bug per
   ADR 0010 D1's reject-don't-guess).

Duplicate name detection runs after emission: the final slice is
scanned; two entries with the same `Name` → `ErrDuplicateQueryName`
naming both occurrences. Detection at emission time would require
carrying a set through the walk for no code-size or clarity benefit.

### 4.5 Sentinel set

Package-level sentinels in `internal/queryfile/errors.go`, wrapped at
the fail-site with detail (`fmt.Errorf("%w: line %d: %s",
ErrUnknownCardinality, lineno, tok)`) — the schema/gql / resolver
convention.

```go
// ErrMissingAnnotation is returned when a query file contains a query
// body with no preceding annotation, or (equivalently) an annotation
// line at end-of-file with no body.
var ErrMissingAnnotation = errors.New("missing query annotation")

// ErrUnknownCardinality is returned when an annotation's cardinality
// token is not one of :one, :many, :exec.
var ErrUnknownCardinality = errors.New("unknown cardinality")

// ErrInvalidQueryName is returned when an annotation's name token is
// not a valid exported Go identifier (^[A-Z][A-Za-z0-9]*$).
var ErrInvalidQueryName = errors.New("invalid query name")

// ErrDuplicateQueryName is returned when two annotations in one query
// file (or across the batch, checked by codegen) share a name.
var ErrDuplicateQueryName = errors.New("duplicate query name")

// ErrMalformedAnnotation is returned when a line begins with the
// annotation prefix ("// name:") but does not conform to the grammar
// — a common typo like a missing cardinality token, or a name/token
// pair that fails to lex.
var ErrMalformedAnnotation = errors.New("malformed query annotation")

// ErrTextBeforeAnnotation is returned when a query file has query text
// before its first annotation (file-leading comment-and-blank content
// is permitted).
var ErrTextBeforeAnnotation = errors.New("query text before first annotation")

// ErrNoQueries is returned when a query file's parse yielded zero
// AnnotatedQueries.
var ErrNoQueries = errors.New("no queries in query file")
```

`allSentinels` in `errors.go` is the canonical closed set for the sweep
(§9). ADR 0010 D1's list — "missing annotation, unknown cardinality,
duplicate name, invalid Go identifier as name, etc." — is these seven,
enumerated.

### 4.6 What lives in `internal/codegen`, not here

The queryfile front end validates one query file at a time. Batch-level
concerns cross the API boundary:

- Cross-file duplicate names: two files carrying `PeopleOverAge :many`
  in one `Input.Queries` — codegen's job, `ErrDuplicateQueryName`
  reused from `internal/queryfile` at the batch level.
- Two `NamedQuery.SourceFile` values sharing the basename (`a/people.cypher`
  and `b/people.cypher`) — codegen's job, ADR 0010 D4 Resolved calls
  this out. C0 doesn't emit per-source files, but the check runs and
  fires `ErrDuplicateSourceFile` uniformly regardless of stage (a
  fixture that fires this at C0 stays firing it at C5).
- Cardinality × shape mismatch — a later-stage concern (C1 for reads,
  C4 for writes); ADR 0010 D1 Resolved. C0 does not check.

---

## 5. Skeleton emission — the compiling-empty package

The point of C0 is a generated package that **compiles** on its own,
even before any query is projected. This lets every later stage's
golden diff read as an incremental delta on top of the C0 baseline
instead of a full package churn.

### 5.1 Package name derivation

ADR 0010 D6 Resolved: no configuration in v1; the one value the
generator cannot invent is derived from the schema name.

```
Schema.Name → package <lowercase(Schema.Name)>
```

The mangle:

1. Take `Schema.Name` verbatim.
2. Convert to lowercase via `strings.ToLower` (ASCII-only for §5's Go
   identifier rules; a non-ASCII schema name is legal in GQL but not a
   valid Go package identifier — the check in step 4 catches it).
3. If the result is empty (unnamed graph type — the parser rejects
   empty names, but the guard is cheap) → `ErrInvalidPackageName`.
4. If the result does not match `^[a-z][a-z0-9_]*$` (the Go package-
   identifier grammar; underscores are legal, digits inside are legal,
   digit-leading is not) → `ErrInvalidPackageName` naming the derived
   token.

Examples: `Movies` → `movies`; `Social_Graph` → `social_graph`;
`3Movies` → sentinel (digit-leading); `Movies.v2` → sentinel (dot);
`Ταινίες` (Greek) → sentinel (non-ASCII). Package renames are the
user's problem at the schema layer — the derivation is deterministic,
not a guess, and the sentinel names the mangled token so the fix is
obvious.

### 5.2 Generated-file header

Every emitted `.go` file begins with the same header, byte-identical
across files in a batch:

```
// Code generated by gqlc <version>. DO NOT EDIT.

package <derived>
```

- The `Code generated by ... DO NOT EDIT.` line matches the Go
  toolchain regex (`^// Code generated .* DO NOT EDIT\.$`) so `gofmt`
  / `goimports` / linters treat the files as generated.
- `<version>` is a package-level constant in `internal/codegen`,
  defaulting to `"dev"` (the C0 build-time default; a `-ldflags -X`
  override lands with C6 per ADR 0010 D7). Determinism per §2.3: the
  version is a build-time constant, not a runtime lookup, so the double-
  run test passes across arbitrary invocations of the same binary.
- One blank line, then the `package` clause, then the file body. No
  build tag lines. No import block on files that need no imports
  (`querier.go` in C0 has none; `db.go` does).

### 5.3 `db.go`

```
package <derived>

import (
    "context"

    "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Queries struct {
    db driverOrTx
}

func New(driver neo4j.DriverWithContext) *Queries {
    return &Queries{db: driverDB{driver: driver}}
}

func (q *Queries) WithTx(tx neo4j.ManagedTransaction) *Queries {
    return &Queries{db: txDB{tx: tx}}
}

// driverOrTx is the unexported run indirection: every generated query
// body routes through it, dispatching between the per-call-session
// path (New) and the caller-owned managed-transaction path (WithTx).
type driverOrTx interface {
    run(ctx context.Context, statement string, params map[string]any, access neo4j.AccessMode) (neo4j.ResultWithContext, error)
}

type driverDB struct {
    driver neo4j.DriverWithContext
}

func (d driverDB) run(ctx context.Context, statement string, params map[string]any, access neo4j.AccessMode) (neo4j.ResultWithContext, error) {
    session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: access})
    defer session.Close(ctx)
    // C0: the driverDB.run body dispatches ExecuteRead / ExecuteWrite
    // by AccessMode. C1 populates the read arm; C4 populates the
    // write arm.
    _ = session
    return nil, nil
}

type txDB struct {
    tx neo4j.ManagedTransaction
}

func (t txDB) run(ctx context.Context, statement string, params map[string]any, _ neo4j.AccessMode) (neo4j.ResultWithContext, error) {
    return t.tx.Run(ctx, statement, params)
}
```

- **`driverOrTx`** is unexported. Users never see it; it is not a
  mockability surface (that is what `Querier` is for, §5.4). ADR 0010
  D2 Resolved: "every method body to route through one *unexported* run
  indirection". C0 emits both implementations because `WithTx` ships
  in v1 — deferring it would foreclose transaction composition
  (ADR 0010 D2 Resolved: the D8 defer-treatment was rejected for
  WithTx precisely because method bodies own their sessions
  otherwise).
- **`driverDB.run`** opens a per-call session and (at C1+) dispatches
  `ExecuteRead` / `ExecuteWrite` on `AccessMode`. C0's body is a stub
  that returns `(nil, nil)` — a place-holder that `go vet` accepts
  because the `session` local is used via `_ = session`. The stub
  never fires because C0 emits no method body that calls `run`; the
  compile fence is only proving the tree parses and type-checks.
- **`txDB.run`** ignores `AccessMode`: managed transactions were
  already opened in a specific mode by the caller's
  `ExecuteRead` / `ExecuteWrite`, and re-dispatching would be wrong
  (ADR 0010 D2 Resolved: `ManagedTransaction`-only, not
  `ExplicitTransaction`, so the caller stays inside managed retry).
- The `context` import is unconditional because C0 must produce a file
  that compiles without knowing whether a query exists — `New` /
  `WithTx` do not use `context.Context`, but the `driverOrTx.run`
  signature does. C1+ will not add or remove this import.
- **`_ = session`** is the one C0-specific line the code carries. It
  keeps the file honest to `go vet` while a real body is absent; C1's
  first read-query body replaces it, and the golden diff at C1 shows
  the replacement in one place.

### 5.4 `querier.go`

```
package <derived>

type ReadQuerier interface {
}

type WriteQuerier interface {
}

type Querier interface {
    ReadQuerier
    WriteQuerier
}

var _ Querier = (*Queries)(nil)
```

- All three interfaces emitted unconditionally, empty at C0. ADR 0010
  D2 Resolved: "constant shape beats saving inert lines". An empty
  interface accepts every type, so the file adds no C0-user safety —
  but omitting it would make later stages' first read (or write)
  method a structural churn in `querier.go` under existing user code.
- The compile-time assertion `var _ Querier = (*Queries)(nil)` is the
  one line that would fail to compile if `Queries` were ever missing
  the interfaces' methods — a build-time fence against method-name
  drift when C1+ adds methods to `Queries` but forgets the
  corresponding entry in an interface.
- No imports. Empty interfaces need none, and gofmt tolerates an
  import-less generated file.

### 5.5 `models.go`

```
package <derived>
```

Empty. C0 has no schema-shaped structs to emit (they land at C2;
ADR 0010 D7). The file exists so `models.go` is present in the golden
tree from day one — later stages fill it in place, and the caller's
directory sync (whenever it lands) always sees the same file set for
this schema.

### 5.6 Per-source files (not emitted at C0)

`<name>.cypher.go` (ADR 0010 D4) is a per-input-query-file emission
carrying the query-text consts, Params/Row structs, methods, and per-
query row assembly for the queries declared in that file. C0 emits no
methods, so no per-source file is emitted. C1 introduces the first one.
A C0 fixture with a non-empty `Queries` slice thus has three files in
its golden (`db.go`, `querier.go`, `models.go`), regardless of how
many queries are in the input — the front end lowered them, but no
projection happens.

### 5.7 Formatting

All emitted contents pass through `go/format.Source` before entering
`File.Contents`. Two consequences:

1. The template's whitespace does not need to be pixel-perfect — the
   formatter normalises. Templates aim for readable, not gofmt-clean.
2. A formatter error is a codegen bug that ships as `ErrFormatFailure`
   wrapping the underlying `format.Source` error. The sentinel is not
   expected to fire under any legitimate C0 fixture; it exists so a
   template regression is caught at generation time instead of at the
   compile fence with a confusing diff.

---

## 6. The golden harness

Under `test/data/codegen/`, mirroring the resolver's golden discipline
(`test/data/resolver/`) but with two changes forced by the emitted
artifact being Go source, not JSON:

1. Fixtures compare against a **directory** of files (the generated
   package), not one `.json` file.
2. The whole tree lives in its own Go module (§6.1) so gqlc's own
   `go.mod` stays driver-free and the compile fence is a cheap
   `go build ./...` inside the nested module (ADR 0010 D5).

### 6.1 Nested module

`test/data/codegen/go.mod`:

```
module github.com/areqag/gqlc/test/data/codegen

go 1.26.2

require github.com/neo4j/neo4j-go-driver/v5 v5.<latest>
```

The exact patch version of `neo4j-go-driver/v5` is pinned at C0 to the
latest stable release at the time of the C0 spec-cycle merge (verify
against upstream tags in the C0 code cycle; the standing instruction in
ADR 0010 D7 applies). Bumping the driver is a deliberate change to this
one `go.mod`.

The rationale for a nested module (ADR 0010 D5 Resolved, quoted in
spirit):

- **`testdata` was considered and rejected.** Go tooling skips the
  magic-named `testdata/` directory in `./...` walks; a nested module
  achieves the same skip *and* lets the tree have its own dependency
  set. `testdata` alone forces a copy-to-temp-module fence for the
  compile check — one small cached build in a real module beats two
  passes on a temp-module scaffolding per test run.
- **`test/data/` is deliberately not the magic name.** The directory
  is `test/data/`, not `testdata/`, because the parent's `./...`
  should not silently skip it — the nested `go.mod` makes the skip
  explicit, not implicit, and a reader browsing the tree can tell the
  fixtures are a module by looking.
- **The nested `go.mod` is the single place the driver version is
  pinned.** ADR 0010 D5 §Consequences: gqlc's own module stays
  driver-free. The generator emits text; it never links the driver.
- **The compile fence is a `just` recipe (§7), used identically
  locally and in CI.** One small cached build, seconds of CI time
  once the module's `go.sum` is populated.

### 6.2 Corpus layout

```
test/data/codegen/
  go.mod
  go.sum
  _include.go               // package fixtures — an anchor go build finds
  valid/
    empty_input/
      social.gql            // schema fixture
      queries.cypher        // annotated queries (or absent if no queries)
      manifest.json         // pins expected package name; §6.3
      golden/
        db.go
        querier.go
        models.go
    single_query_front_end/
      social.gql
      people.cypher
      manifest.json
      golden/
        db.go
        querier.go
        models.go
  invalid/
    invalid_package_name/
      3movies.gql
      queries.cypher
      manifest.json         // pins expected sentinel; §6.3
    duplicate_query_name/
      social.gql
      queries.cypher
      manifest.json
    ...
```

- **One directory per fixture.** The directory name is the fixture
  identity used in the invalid-sentinel map and the golden-comparison
  loop. It must be a valid Go identifier so the Go tooling walking
  the nested module treats each directory cleanly, and it must sort
  deterministically.
- **`_include.go`** at the module root is `package fixtures` and
  contains no exports. It exists so `go build ./...` inside the
  nested module has a top-level anchor (Go modules need at least one
  package file at the module root or the walk short-circuits). The
  leading underscore keeps the file out of gqlc's own `./...` walks
  even without the nested-module skip, defense in depth.
- **Per-fixture files.** Schema `.gql`(s), one or more `.cypher` query
  files (annotated form), and `manifest.json`. Valid fixtures also
  hold a `golden/` subdirectory containing the exact file set
  `Generate` returned. Empty-input fixtures still hold a `queries.cypher`
  when the queryfile front end is under test — the file may declare
  queries the emission ignores.
- **`golden/`** is the byte-comparison target. Sub-package of the
  fixture directory but *not* imported by anything in the nested
  module (nothing imports goldens); it exists to be built as part of
  the module's `./...` walk, which is the compile fence (§7).
- **No shared `schemas/` subdirectory.** The resolver harness shared
  one schema across many query files by pairing via `schema.mapping.json`;
  the codegen harness has multiple *files* per fixture (schema + query
  file(s) + golden dir), so per-fixture directories read more cleanly
  than a mapping file. Fixtures may hand-copy a schema — the corpus is
  small at C0, and later stages can factor if churn appears.

### 6.3 `manifest.json`

Per-fixture manifest, structured to serve both valid and invalid cases:

```json
{
  "package": "movies",
  "queryFiles": ["people.cypher", "movies.cypher"],
  "expectedError": "queryfile.ErrUnknownCardinality"
}
```

- **`package`** is the expected derived package name (redundant with
  the golden files' `package` clauses on the valid path — the test
  asserts both agree; on the invalid path it is the expected result
  if generation had succeeded, useful for readers scanning the fixture).
- **`queryFiles`** is the ordered list of `.cypher` files in this
  fixture directory to load, in the order they enter `Input.Queries`.
  The order matters because it is the caller's first-appearance
  ordering for deterministic output.
- **`expectedError`** is present on invalid fixtures only. The string
  names the fully-qualified sentinel identifier
  (`queryfile.ErrUnknownCardinality` or `codegen.ErrInvalidPackageName`);
  the test maps it to the actual `error` value at load time via a
  small lookup table (built from the two `allSentinels` slices). Same
  discipline as the resolver's `invalidFixtures` map, one level of
  indirection so fixtures stay pure data files.

### 6.4 The `-update` flag

A test-local `var update = flag.Bool("update", false, "regenerate
codegen goldens")` in `codegen_test.go`. When set, the valid-fixture
sub-test *deletes and rewrites* the fixture's `golden/` directory
from `Generate`'s output. Unlike the JSON-single-file goldens
elsewhere, the golden here is a tree, and stale files must not linger
— a query removed from the input must not leave its old `.cypher.go`
in the golden dir. `-update` runs the same directory-sync the future
CLI will run: delete not-in-output, write all-in-output.

### 6.5 The suite

`codegen_test.go` — testify `suite.Suite`, one test per concern:

- **`TestValid`** — walks `valid/*/`, loads each fixture (schema via
  `schema/gql`, each query file via `queryfile`, resolves each query via
  `resolver`), calls `Generate`, and either writes the golden
  (`-update`) or asserts byte-equality against every file in the
  fixture's `golden/`. Byte-equality, not `JSONEq` — the output is Go
  source, and every whitespace character matters (gofmt normalises,
  but the tree it produces is stable).
- **`TestInvalid`** — walks `invalid/*/`, resolves the manifest's
  `expectedError` string to a sentinel, calls the full pipeline, and
  asserts (a) the returned `[]File` is nil and (b) `errors.Is(err,
  wantErr)`. Map totality asserted at the top of the test.
- **`TestDoubleRun`** — the determinism test (§8).
- **`TestSentinelReachability`** — the bidirectional sweep (§9).

### 6.6 The queryfile suite

`internal/queryfile/queryfile_test.go` — testify `suite.Suite`,
table-driven cases per convention:

- **`TestValidAnnotations`** — each row is `{name string, input string,
  want []AnnotatedQuery}`; the parser round-trips each and `require.Equal`s.
- **`TestInvalidAnnotations`** — each row is `{name string, input string,
  wantErr error}`; the parser fails each with `errors.Is(err, wantErr)`.
- **`TestSentinelReachability`** — the queryfile side of the sweep
  (the codegen side sweeps codegen sentinels; the queryfile side
  sweeps queryfile sentinels — two disjoint sets, two disjoint
  sweeps, one per package).

### 6.7 Non-obvious harness invariants

- **Golden byte-equality, not JSON.** The output is Go source. Two
  goldens differing only in whitespace are two *different* generated
  files — gofmt would fix them consistently, but a golden that goes
  out of gofmt is a bug. The comparison is `bytes.Equal`; on
  mismatch the assertion reports the file path and a `diff -u`-shaped
  message for reviewer convenience.
- **The compile fence is separate from `TestValid`.** ADR 0010 D5's
  compile fence is a `just` recipe (§7) that runs `go build ./... &&
  go vet ./...` inside the nested module. It is *not* invoked from
  the Go test suite (the test suite is inside gqlc's own module, and
  crossing into the nested module from a Go test would defeat the
  driver-free posture of gqlc's `go.mod`). The recipe is the
  invocation; CI wires the recipe alongside `just test`.
- **Every valid fixture's `golden/` must compile.** The compile fence
  covers the whole nested module's `./...`, so a golden that
  type-checks in isolation but fails in composition (two fixtures
  colliding on a package path) fails the fence. Fixture directory
  names are unique (§6.2), so the collision is unreachable in
  practice.

---

## 7. The compile fence

One `just` recipe:

```
# runs the codegen goldens' compile fence: go build && go vet inside the
# nested module. Used identically locally (post-generate) and in CI.
test-codegen-fence:
    cd test/data/codegen && go build ./... && go vet ./...
```

- **`go build ./...`** parses, type-checks, and compiles every package
  in the nested module. A golden that doesn't compile fails here.
- **`go vet ./...`** catches the class of issues the compiler
  accepts (unused imports the emission missed, format-string
  mismatches, etc.). Cheap, catches emitter bugs early.
- **Cached.** After the first run, `go build` is a stat walk unless
  fixtures change — seconds of CI time (ADR 0010 D5 §Consequences).
- **Identical local and CI.** The recipe is the single invocation; CI
  runs `just test-codegen-fence` alongside `just test`. Bumping the
  driver version is a `go.mod` change; the recipe does not.
- **`just test` unaffected.** The existing `test` recipe stays
  focused on gqlc's own module (`go test -shuffle=on ./...`); the
  codegen fence is its own recipe because it is a different tree with
  different semantics. A future `just ci` aggregator can run both.

---

## 8. Double-run determinism test

Codegen goldens catch nondeterminism only *flakily* — a map-iteration
regression manifests as a golden diff sometimes-passing-sometimes-failing
across CI runs. A dedicated deterministic check catches it every time
(ADR 0010 D5 Resolved):

```
// TestDoubleRun asserts Generate is byte-deterministic: same Input in,
// byte-identical []File out, twice. Independent of the golden
// comparison — a golden diff catches within-run nondeterminism (map
// iteration) only flakily; this test catches it in a single run.
func (s *CodegenSuite) TestDoubleRun() {
    for _, fixture := range validFixtures {
        s.Run(fixture.name, func() {
            in := s.loadInput(fixture)
            first, err := New().Generate(in)
            s.Require().NoError(err)
            second, err := New().Generate(in)
            s.Require().NoError(err)
            s.Require().Equal(len(first), len(second))
            for i := range first {
                s.Require().Equal(first[i].Path, second[i].Path, "file %d path drift", i)
                s.Require().True(bytes.Equal(first[i].Contents, second[i].Contents),
                    "file %s contents drift: %d vs %d bytes",
                    first[i].Path, len(first[i].Contents), len(second[i].Contents))
            }
        })
    }
}
```

- **Runs on every valid fixture.** One `TestDoubleRun` case per
  `valid/*/` — invalid fixtures fail generation, so double-running
  them tests nothing new.
- **Byte-comparison per file.** Slice lengths, then per-file `Path`
  and `Contents`. The path check catches an emitter that returns files
  in different orders across runs (an ordered-iteration bug); the
  contents check catches map iteration inside a file body.
- **Both `Generate` calls on fresh `New()` receivers.** A stateful
  `Codegen` reused across calls could accidentally hide
  nondeterminism; two fresh instances are a stronger contract.
- **Not gated by `-update`.** Determinism is a property of the
  emitter, not the golden; even without goldens on disk this test
  should pass.

---

## 9. Sentinel sets

Two disjoint sets, two disjoint sweeps. Both wrapped at the fail-site
with detail (`fmt.Errorf("%w: derived package %q", ErrInvalidPackageName,
name)`).

### 9.1 The queryfile set (§4.5)

```go
var allSentinels = []error{
    ErrMissingAnnotation,
    ErrUnknownCardinality,
    ErrInvalidQueryName,
    ErrDuplicateQueryName,
    ErrMalformedAnnotation,
    ErrTextBeforeAnnotation,
    ErrNoQueries,
}
```

Each carries at least one negative row in the queryfile suite's
`TestInvalidAnnotations` table; `TestSentinelReachability` (queryfile
side) sweeps.

### 9.2 The codegen set

```go
// ErrInvalidPackageName is returned when Schema.Name's lowercase
// mangle does not produce a valid Go package identifier (empty,
// non-ASCII, digit-leading, contains punctuation other than
// underscore).
var ErrInvalidPackageName = errors.New("invalid package name")

// ErrDuplicateSourceFile is returned when two NamedQuery entries in
// one Input carry SourceFile values whose basenames collide. C0
// emits no per-source file, but the check runs uniformly regardless
// of stage.
var ErrDuplicateSourceFile = errors.New("duplicate query file basename")

// ErrDuplicateQueryName is returned when two NamedQuery entries in
// one Input share a Name (a cross-file collision the queryfile front
// end cannot see because it works one file at a time). Same sentinel
// value as queryfile.ErrDuplicateQueryName is deliberately NOT reused
// — errors.Is walks separately per package, and the batch-level check
// is a codegen-owned concern with its own reachability sweep.
var ErrDuplicateQueryName = errors.New("duplicate query name in batch")

// ErrInvalidCardinality is returned when a NamedQuery's Cardinality
// field is the zero value (unset — a caller bug the front end never
// produces). Present so a hand-constructed NamedQuery slipping past
// the front end fails at generation, not silently.
var ErrInvalidCardinality = errors.New("invalid cardinality")

// ErrFormatFailure is returned when go/format.Source rejects an
// emitted file's raw contents. A template bug; not expected to fire
// under any legitimate fixture, but wrapped-and-named beats a
// generic error.
var ErrFormatFailure = errors.New("format failure")

var allSentinels = []error{
    ErrInvalidPackageName,
    ErrDuplicateSourceFile,
    ErrDuplicateQueryName,
    ErrInvalidCardinality,
    ErrFormatFailure,
}
```

- **`ErrFormatFailure` is reachable via a template-corruption fixture.**
  C0's invalid-fixture set includes one fixture designed to fire it —
  the specific construction is deferred to the code cycle, but the
  reachability sweep demands coverage.
- **Category-grained where sensible, specific where needed.** The
  parser and resolver retired constructs from a category-grained
  sentinel (`ErrOutOfR0Scope`) as stages narrowed them; codegen's set
  is finer-grained at C0 because each sentinel maps to a specific
  emission-time check, not a scope gate. Later stages *add*
  sentinels (cardinality × shape mismatch, alias-required, entity-
  collision at C5); C0's set is not a category-grained placeholder.

### 9.3 The two sweeps

Two `TestSentinelReachability` tests, one per package (§6.5, §6.6).
Each is the same bidirectional sweep: `allSentinels` × the negative-
fixture-set-image are equal as sets. A queryfile sentinel added → add
to queryfile `allSentinels` + queryfile invalid table; a codegen
sentinel added → add to codegen `allSentinels` + one negative fixture
directory. Consistent posture with parser/resolver.

---

## 10. Out-of-scope table

Every downstream capability with the stage that will deliver it. Read
as ADR 0010 D7 unpacked to the C0-vs-later boundary:

| Capability                                         | Stage owner |
|----------------------------------------------------|-------------|
| Params / Row structs                               | C1          |
| Method emission (`:one` / `:many`)                 | C1          |
| Native-width property mapping (`INT32`, `BOOL`, …) | C1          |
| Nullable → pointer field (params and rows)         | C1          |
| `ErrNoRows` sentinel for `:one`                    | C1          |
| Method-signature naming rules (Params vs bare arg; Row vs bare value) | C1 |
| Method / param / row field naming rules            | C1 / C2     |
| Text-shape analysis for row fields (bare / `ident.ident` / `AS`) | C1 |
| Entity structs in `models.go` (schema-shaped only) | C2          |
| Entity type-name resolution (`Name` first, mangle fallback, eager multi-label check) | C2 |
| Entity decode helpers (`dbtype.Node` → struct)     | C2          |
| Per-source `<name>.cypher.go` file emission        | C1 / C2     |
| Collections (`list<T>`)                            | C3          |
| Six temporals via `dbtype`                         | C3          |
| Unrepresentable-width sentinels (INT128+, FLOAT16, DECIMAL) | C3   |
| FLOAT32 schema-width contract                      | C3          |
| `unknown` / `scalar null` / `scalar map` → `any`   | C3          |
| Writes (`:exec`, zero-column methods, `WriteQuerier` population) | C4 |
| `ExecuteWrite` path in `driverDB.run`              | C4          |
| Cardinality × shape rejection                      | C4          |
| `edgeUnion` sealed interfaces + `//sumtype:decl`   | C5          |
| Package-level collision sweep hardening            | C5          |
| Version-stamp polish (`-ldflags -X` wiring)        | C6          |
| Session-config polish                              | C6          |
| `gqlc-0aa` re-scope against D4's no-runtime-package decision | C6 |
| `:iter` streaming cardinality (fourth enum value)  | `gqlc-1a5` (post-v1) |
| Configuration file (`gqlc.yaml` analogue), CLI    | future config effort |
| Disk writes, out-dir sync (stale deletion)         | future CLI effort |

Rows above the `gqlc-1a5` line are staged by ADR 0010 D7; the last two
are ADR 0010 D6 futures.

---

## 11. Definition of done for C0 (spec cycle)

This is the spec cycle only (Cycle 1); the code cycle (Cycle 2) is out
of scope of this document. The spec is done when:

1. This file exists at `docs/specs/codegen-stage-c0.md`, committed on
   branch `codegen-c0-spec`.
2. The generator API shape (§3) is decided: `Input`, `NamedQuery`,
   `File`, `Cardinality`, `Generate` signature, `Generator` seam,
   concrete `Codegen` struct.
3. The queryfile front end (§4) is designed: grammar, `AnnotatedQuery`,
   `Parser`, sentinel set.
4. The skeleton emission (§5) is pinned: package name derivation,
   header, `db.go` (`Queries`, `New`, `WithTx`, `driverOrTx` +
   `driverDB` / `txDB`), `querier.go` (three interfaces + assertion),
   `models.go` empty.
5. The nested-module golden harness (§6) is designed: layout,
   `manifest.json`, `-update`, testify suites, non-obvious invariants
   called out.
6. The compile fence (§7) is a single `just` recipe.
7. The double-run determinism test (§8) is specified.
8. Both sentinel sets (§9) are closed, named, and reachability-swept.
9. The out-of-scope table (§10) enumerates every downstream capability
   with its stage owner.
10. `just test` is untouched-green — this cycle is docs-only.

The spec is a review artefact for Linus (adversarial reviewer); every
blocker he raises is fixed on this same branch before the branch
merges. Cycle 2 (the C0 code cycle, `codegen-c0-skeleton` stacked on
this branch) begins only when the spec cycle merges.
