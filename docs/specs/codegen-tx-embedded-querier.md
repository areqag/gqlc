# codegen: the querier embedded on Tx

Bead: gqlc-jwfm (design) / gqlc-f4hf (execution). Author: Արթուր.
Supersedes, in part, `docs/specs/codegen-tx-object.md` — §4's emitted
shape and §9.1's reservation grounds. That spec's measured history (§8's
battery, its CI run ids) is not disturbed: it records what was true of
the shape it shipped.

## 1. The problem in plain words

The shipped Tx object makes a caller write `tx.Queries().GetPerson(ctx,
id)` — a hop through an accessor to reach the repository methods the
transaction exists to run. Անդրանիկ, using the generated packages in
anger, asks for the querier to live on the Tx itself: `tx.GetPerson(ctx,
id)`, with nesting made impossible rather than merely refused.

The hop is not only ergonomics. `tx.Queries()` returns a full
`*Queries`, so today `tx.Queries().Begin(ctx)` *compiles* and is turned
away only at runtime, and so do `tx.Queries().WithTx(...)` and (on AGE)
`tx.Queries().EnsureGraph(ctx)`. The accessor is a door from the
transaction back to the whole root surface. Removing the hop and closing
that door are the same change, done right.

What "impossible" can honestly mean: `tx.Begin` becomes a **compile
error**. It cannot mean the runtime refusals go away — a handle bound to
a caller-owned transaction by `WithTx`, or (AGE) a `pgx.Tx` handed
straight to `New`, is transaction-bound as a *dynamic-type fact* the
compiler cannot see. Those refusals stay exactly as shipped and tested.

## 2. The shape

One new **unexported** type per generated package, and no new exported
names anywhere:

```go
// the core: every generated query method moves here
type queries struct { ... backend-specific seam fields ... }

type Queries struct {  // the root handle, a wrapper
	queries
}

type Tx struct {       // the transaction handle
	queries            // bound to the open transaction
	... backend-specific tx fields ...
}
```

Go's method promotion does the rest:

- Query methods are emitted with receiver `*queries`. They promote into
  the method sets of both `*Queries` and `*Tx`, so `q.GetPerson(...)`
  and `tx.GetPerson(...)` both compile with one emission of each method.
- `New`, `WithTx`, `Begin` (and AGE's `EnsureGraph`/`DropGraph`) keep
  their receivers on `*Queries` (or stay package functions). `Tx` embeds
  `queries`, **not** `Queries`, so none of them is in `*Tx`'s method
  set: `tx.Begin`, `tx.WithTx`, `tx.Queries` are compile errors — there
  is no identifier to resolve.
- The embedded field is unexported, so no caller outside the generated
  package can reach `tx.queries` to smuggle the core out. There is no
  path from a `*Tx` to any Begin-bearing handle.
- `(*Tx).Queries()` is **removed**, not kept for compatibility. It is
  the hop the owner rejects, and keeping it keeps the compilable
  nesting path alive. There is no compatibility to keep: generated code
  is regenerated, and the only in-tree callers are the live-battery
  adapters this plan rewrites.

The name `queries` cannot collide with anything user-derived: query
names must match `^[A-Z][A-Za-z0-9]*$` (`internal/queryfile/parse.go:27`,
enforced at `:69`), entity names likewise
(`internal/codegen/names.go`, `exportedGoIdentRe`), and every derived
Params/Row/decoder name is built from those. Verified, not assumed.

Zero values: `Tx{}` remains "not usable; Begin is the only constructor",
as its doc comment already says. Promotion does not change that — a
zero `queries.db` panics on first use exactly as a zero `Queries` would.

## 3. What each caller line does, before and after

| caller writes | shipped | this design |
|---|---|---|
| `tx.GetPerson(ctx, id)` | compile error | runs in the tx |
| `tx.Queries().GetPerson(ctx, id)` | runs in the tx | compile error (accessor gone) |
| `tx.Begin(ctx)` | compile error | compile error (unchanged) |
| `tx.Queries().Begin(ctx)` | compiles, refused at runtime | compile error |
| `tx.Queries().WithTx(x)` | compiles | compile error |
| `q.WithTx(x).Begin(ctx)` | refused at runtime | refused at runtime (unchanged) |
| AGE `New(pgxTx, g).Begin(ctx)` | refused at runtime | refused at runtime (unchanged) |
| `readQuerierArg(tx)` (interface use) | needs `tx.Queries()` | pass `tx` directly — `*Tx` satisfies ReadQuerier/WriteQuerier/Querier by promotion |

The last row answers the bead's ReadQuerier question: no separate
treatment is needed. `*Tx` satisfies the querier interfaces for free,
and each backend's `render_querier.go` adds one emitted pin beside the
existing one:

```go
var _ Querier = (*Queries)(nil)
var _ Querier = (*Tx)(nil)
```

(`Tx` is emitted unconditionally on both backends, so the pin compiles
whatever the batch holds; on a zero-query batch `Querier` is empty and
the pin is vacuous but harmless.)

## 4. The emitted surface, per backend

### 4.1 AGE (`internal/codegen/age/render_db.go`)

```go
type DBTX interface { ... unchanged ... }

// queries is the core every generated query method hangs off. Queries
// and Tx both embed it, which is what lets one emission of each method
// serve both handles; it is unexported so a Tx cannot hand it out.
type queries struct {
	db    DBTX
	graph string
}

type Queries struct {
	queries
}

func New(db DBTX, graph string) *Queries {
	return &Queries{queries: queries{db: db, graph: graph}}
}

func (q *Queries) WithTx(tx pgx.Tx) *Queries {
	return &Queries{queries: queries{db: tx, graph: q.graph}}
}

func (q *queries) boundGraph() (string, error) { ... body unchanged ... }

func (q *queries) cypherStmt(tag, text, record string) (string, error) { ... body unchanged ... }

type Tx struct {
	queries
	tx   pgx.Tx
	done bool
}

func (q *Queries) Begin(ctx context.Context) (*Tx, error) {
	// both refusals unchanged; final return becomes:
	return &Tx{queries: queries{db: tx, graph: q.graph}, tx: tx}, nil
}

// (tx *Tx) Queries() — DELETED.
// Commit, Rollback — unchanged.
```

`Tx.tx` and `Tx.queries.db` hold the same `pgx.Tx`; the concrete field
stays because Commit/Rollback need `pgx.Tx`, not `DBTX`. Struct
literals spell the embedded field keyed (`queries: queries{...}`) — go
vet's composites check aside, an unkeyed nested literal is the thing
that breaks silently when a field is added.

`boundGraph` and `cypherStmt` move to `*queries` because the query
methods that call them move there; `EnsureGraph`/`DropGraph`
(`render_graph.go:77,:91`) keep calling `q.boundGraph()` through
promotion and **do not change**. The `withQueries` gating of
`cypherStmt` is untouched.

### 4.2 neo4j (`internal/codegen/neo4j/render_db.go`, v5 spelling; v6 identical via driverAgnostic)

```go
type queries struct {
	db driverOrTx
}

type Queries struct {
	queries
}

func New(driver neo4j.DriverWithContext) *Queries {
	return &Queries{queries: queries{db: driverDB{driver: driver}}}
}

func (q *Queries) WithTx(tx neo4j.ManagedTransaction) *Queries {
	return &Queries{queries: queries{db: txDB{tx: tx}}}
}

// driverOrTx, driverDB, txDB — unchanged.

type Tx struct {
	queries
	session neo4j.SessionWithContext
	tx      neo4j.ExplicitTransaction
	done    bool
}

func (q *Queries) Begin(ctx context.Context) (*Tx, error) {
	// refusal and session/tx opening unchanged; final return becomes:
	return &Tx{queries: queries{db: txDB{tx: tx}}, session: session, tx: tx}, nil
}

// (tx *Tx) Queries() — DELETED.
// Commit, Rollback — unchanged.
```

`txDB.tx` is typed `neo4j.ManagedTransaction` and an
`ExplicitTransaction` satisfies it — that assignment is exactly what the
deleted accessor already did, so nothing new is asserted about the
driver.

### 4.3 Query methods (`neo4j/render_queries.go:353`, `age/render_queries.go:243`)

Both sites emit `"func (q *Queries) "` today; both become
`"func (q *queries) "`. That is the entire query-method change — bodies
read only `q.db`, `q.graph`, `q.cypherStmt`, all on the core.

### 4.4 Doc comments are part of the emission

Every emitted comment that names the accessor or the old ground must
move with the shape, or it becomes the "universal the tree falsifies":

- neo4j Begin's comment: "by WithTx or by another Tx's Queries" → "by
  WithTx: with the querier embedded on Tx there is no other
  transaction-bound handle to hold" (adjust to taste; the point is the
  accessor clause dies).
- AGE Begin's comment: same clause, same fate. Its savepoint-placement
  paragraph is untouched.
- `Tx`'s comment gains one line: query methods are promoted from the
  embedded core and run inside this transaction, reading its own
  uncommitted writes (this sentence replaces the deleted accessor's
  comment, which is currently the only place that fact is stated).
- `queries` gets the comment shown in §4.1.

## 5. Reserved identifiers: grounds rewritten, membership unchanged

`reservedIdentifiers` (`internal/codegen/prepare.go:71`) keeps exactly
its current membership and scopes. What changes is *why* three rows
hold, and the comment block at `prepare.go:55-70` (and spec
codegen-tx-object.md §9.1, bannered per §9 below) must be rewritten to
say so:

- **`Begin`** — shipped ground: real redeclaration on `*Queries`. New
  ground, stronger: a query named Begin would be emitted on `*queries`
  and *promote into `*Tx`'s method set unshadowed* — `tx.Begin(ctx)`
  would compile again and run a user query inside the open transaction.
  The reservation is what keeps "tx.Begin is a compile error" true.
- **`Commit`, `Rollback`** — shipped ground: call-site ambiguity
  (`tx.Commit()` vs `tx.Queries().Commit(...)`, one selector apart).
  That spelling no longer exists. New ground: a query named Commit on
  `*queries` promotes into `*Tx`, where the depth-0 `Commit` shadows it
  *silently* — the user's query is callable on `q` and unreachable on
  `tx`, no diagnostic anywhere. A silently shadowed user query is the
  outcome the bead names as the one that must not ship; Phase A's
  membership-based refusal (`prepare.go:602`) already prevents it and
  does not change.
- **`Queries`** — the "occupies both scopes" paragraph shrinks: with the
  accessor deleted it occupies `scopePackage` alone, which is what the
  row already records. The scope gate's biconditional (§9.1's "scope
  iff some golden declares it package-level") holds without edits;
  regenerated goldens still declare `type Queries` package-level.
- **`WithTx`** — unchanged ground (redeclaration on `*Queries`), plus
  the same promotion-shadow argument now applies through `*Queries`
  itself (a user `WithTx` on the core would be shadowed on the root
  handle). No row change.

No **new** reservation is needed for `queries` itself: §2's grammar
facts make a lowercase user-derived name impossible, and the refusal
would be dead code guarding a state the parser already refuses.

## 6. EnsureGraph / DropGraph stay on the root handle

They keep receiver `*Queries` (`age/render_graph.go:77,:91`), so they do
not promote onto `Tx`. This is a deliberate narrowing: today
`tx.Queries().EnsureGraph(ctx)` compiles, and under this design no
graph-lifecycle call can be made from a transaction handle. Graph
lifecycle is administrative surface with no neo4j counterpart, the live
battery only ever calls it on the root handle at session setup, and a
caller who truly wants DDL inside their own transaction still has
`New(pgxTx, g)` / `WithTx`. If a real need surfaces, moving the two
receivers to `*queries` is a two-line follow-up bead, not a constraint
of this shape.

## 7. The surface gate, redesigned (`internal/codegen/txsurface_test.go`)

The gate stays the only assertion of cross-backend Tx agreement
(conformance still excludes db.go — that exclusion is untouched). Its
changes:

1. **Closed set** drops the accessor:
   ```go
   var txSurfaceNames = []string{
   	"func (*Queries) Begin",
   	"func (*Tx) Commit",
   	"func (*Tx) Rollback",
   	"type Tx",
   	"var ErrTxDone",
   }
   ```
   Because the comparison is equality, an emission still carrying
   `func (*Tx) Queries` **fails** — the gate enforces the removal, not
   merely tolerates it.
2. **Structural assertions**, per backend, on the parsed db.go — each
   one is a promotion precondition, and each is falsifiable by the
   mutation that breaks it (§10):
   - `type Tx`'s field list contains exactly one *embedded* field
     (an `ast.Field` with nil `Names`), whose type is the plain ident
     `queries` — not `Queries` (which would promote Begin onto Tx) and
     not `*queries` (a pointer embed changes the zero value and buys
     nothing).
   - `type Queries`'s field list is exactly one embedded `queries`
     field, same test.
   - No `FuncDecl` with receiver `queries`/`*queries` is named `Begin`,
     `Commit`, `Rollback`, `Queries`, or `WithTx` — a lifecycle name on
     the core is a promotion leak onto `Tx`.
3. **Query-method receiver**: the probe batch already emits
   `txprobe.cypher.go`; the gate additionally parses it and asserts
   every method declaration there has receiver `*queries`. This is the
   direct falsifier for §4.3 — a renderer regressed to `*Queries`
   reddens it by name.
4. **Querier pins**: parse the probe's `querier.go` and assert both
   `var _ Querier = (*Queries)(nil)` and `var _ Querier = (*Tx)(nil)`
   are declared, so a renderer that drops the emitted pin fails here
   rather than only in a test file's private copy of the fact.
5. `txReceiver` learns the third receiver name (`queries`, and its
   pointer form) so the extractor can see the core's methods at all;
   `beginErrors`, `txPrivateBeginErrors`, the shared-refusal
   intersection, and the signature comparison are all unchanged —
   Begin's body does not move.

## 8. The method-set witness (compile-level, over real packages)

AST assertions plus the Go spec's promotion rules imply the method
sets, but the town's standard is a witness at the real boundary. New
file `test/data/codegen/methodset_test.go`, package `codegen_test`,
**no build tag** (it needs no container and no server), over two real
golden packages:

```go
import (
	mixedage "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden/apache-age-pgx-v5"
	mixedv5  "github.com/areqag/gqlc/test/data/codegen/valid/mixed_read_write_batch/golden/neo4j-go-v5"
)

func TestTxMethodSet(t *testing.T) { ... }
```

Per package, via `reflect.TypeOf((*pkg.Tx)(nil))`:

- `MethodByName` **absent** on `*Tx`: `Begin`, `Queries`, `WithTx`,
  and (AGE package only) `EnsureGraph`, `DropGraph`. Absence in the
  reflected method set and `tx.Begin` failing to compile are the same
  language fact.
- `MethodByName` **present** on `*Tx`: `Commit`, `Rollback`, and the
  promoted query methods `RemovePerson`, `GetPersonName`.
- `MethodByName("Begin")` **present** on `*Queries` (the constructor
  did not move).
- Compile-time interface pins in the test file:
  `var _ mixedage.Querier = (*mixedage.Tx)(nil)` and the v5 twin.

Red-before/green-after: on today's shape the `RemovePerson`-present and
`Queries`-absent rows fail, so the test discriminates the designs and
cannot pass vacuously.

**Enrollment — this is load-bearing, not a footnote.** Every existing
test file in the nested module is `codegen_live`-tagged and the
PR-blocking neo4j arm selects by name (`justfile:2376`,
`-run TestLiveSmoke`), so an unenrolled test is a detector nobody runs.
Two executions cover it:

- The full battery (`justfile:2338`, no `-run` filter) picks it up on
  schedule/dispatch with no edit.
- The PR arm's recipe at `justfile:2376` becomes
  `-run 'TestLiveSmoke|TestTxMethodSet'` — the alternation is a name
  list and `-run` is unanchored (the recipe's own comment at
  `justfile:2387` documents this); `TestTxMethodSet` prefixes no other
  test name in the module (grep before relying on it, per the recipe's
  rule). Do **not** enroll it in the AGE recipe at `justfile:2404` —
  that arm never runs on PRs and the full battery already covers it
  there.

The fence (`test-codegen-fence`, `justfile:2112`) compiles the module's
test files with derived tags via `go vet` on every PR, so the rewritten
adapters (§9) are themselves a PR-blocking *compile* witness that
promotion works; `TestTxMethodSet` adds the run-time reflection rows
and the negatives.

## 9. Live battery, adapters, and the nesting scenario

All call sites verified by grep; the accessor appears nowhere outside
`test/data/codegen` and docs.

1. **Adapter hops** — `a.tx.Queries().RemovePerson(ctx, id)` →
   `a.tx.RemovePerson(ctx, id)`, and likewise `GetPersonName`:
   `live_age_test.go:475,:479`; `live_neo4j_test.go:452,:456` (v5),
   `:876,:880` (v6).
2. **`beginNested` leaves `liveTx`** (`live_test.go:171`). Its current
   implementations (`live_age_test.go:488`, `live_neo4j_test.go:466,
   :885`) spell `a.tx.Queries().Begin(ctx)`, which no longer compiles —
   *by design*. The compile-impossibility is witnessed by §8; what
   still needs a live witness is the runtime refusal on the handles
   that remain transaction-bindable. Replace the scenario
   `txBeginIsRefusedInsideATransaction` (`live_test.go:1033`) with
   `txBeginIsRefusedOnATransactionBoundHandle`, driven per arm from the
   backend struct (which owns the driver/pool), asserting the same
   `txNestedRefusal` message both halves:
   - **AGE**: `pgxTx, err := pool.Begin(ctx)` (deferred rollback), then
     `q.WithTx(pgxTx).Begin(ctx)` must refuse. The existing
     `New(pgxTx, graph)` refusal row (`live_age_test.go:340`) and the
     savepoint-placement probe (`txRefusedNestedBeginOpensNoSavepoint`,
     which never used the accessor) stay as they are.
   - **neo4j v5/v6**: open a session, and inside
     `neo4j.ExecuteWrite(ctx, session, func(mt neo4j.ManagedTransaction) (struct{}, error) {...})`
     call `q.WithTx(mt).Begin(ctx)`; the closure returns Begin's error
     for the scenario to assert on. Without this row, deleting the
     accessor would leave neo4j's runtime refusal live-unwitnessed —
     the row is owed, not optional.
3. **Comment truth**: `liveTx`'s doc ("run through the handle
   Tx.Queries hands out", `live_test.go:157`) and the AGE comment at
   `live_age_test.go:332` name the accessor; rewrite both to the
   promoted-methods wording. The scenario's doc keeps its portability
   rationale (neo4j cannot nest; AGE refuses rather than savepoints) —
   that reasoning is unchanged.

## 10. Goldens, gates, and the mutation battery the execution PR owes

- **Regeneration**: `go test -update` in
  `internal/codegen/conformance` rewrites the fixture goldens under
  `test/data/codegen/valid/*/golden/*`; the per-backend corpus tests
  (`internal/codegen/{age,neo4j}/corpus_test.go`) take the same flag
  where they hold goldens. Then `GOTOOLCHAIN=go1.26.6 just gates` —
  the fence recompiles the nested module against the new shape, the
  scope gate re-derives its census from the new emissions (it is
  differential, `conformance/scope_test.go:282`, and needs no edit),
  and the golden-diff machinery holds emitted == blessed.
- **Battery** (measured per the citizen-protocol template, rows named
  here so the executor measures rather than invents):
  1. Revert one backend's `render_queries.go` receiver to `*Queries` →
     §7.3 reddens by name; §8's `RemovePerson`-on-Tx row and the fence
     compile of the adapters redden independently.
  2. Move Begin's emitted receiver to `*queries` → §7's closed set
     (missing `func (*Queries) Begin`) and §7.2's no-lifecycle-on-core
     row redden; §8's Begin-absent-on-`*Tx` row reddens.
  3. Re-add `func (tx *Tx) Queries()` to one emission → closed-set
     equality reddens (extra name).
  4. Change Tx's embed to `Queries` → §7.2 embed-ident row reddens;
     §8's Begin-absent row reddens (Begin would promote).
  5. Change Tx's embed to `*queries` → §7.2's not-a-StarExpr row
     reddens.
  6. Drop `var _ Querier = (*Tx)(nil)` from one `render_querier.go` →
     §7.4 reddens.
  7. Delete the new WithTx-refusal scenario arm on neo4j → the live
     row count drops; the battery's own row-count assertion (protocol
     template) is what catches a silently skipped row.

## 11. Documentation changes

- **This design PR** (docs only): this spec; banners into
  `codegen-tx-object.md` at §4 and §9.1 ("superseded by
  codegen-tx-embedded-querier.md once gqlc-f4hf lands; until then this
  section describes the shipped emission") plus a header note. No
  CONTEXT.md edit yet — the vocabulary must describe the shipped tree,
  and until f4hf merges the shipped tree still has the accessor.
- **The execution PR**: rewrite CONTEXT.md's "Transaction handle (Tx)"
  entry (`CONTEXT.md:612`): drop "`Queries()` yielding a repository
  handle bound to it", state that query methods are promoted onto `Tx`
  from an unexported embedded core, that `Begin`/`WithTx` on a `Tx` do
  not compile, and that Begin on an externally transaction-bound handle
  (WithTx, or AGE's New-with-pgx.Tx) is refused at runtime — compile
  impossibility is claimed **only** for `Tx`, never universally (the
  no-universals rule; the runtime rows are the enumerated limit).
  Rewrite the `prepare.go:55-70` comment per §5. Fix
  `codegen-sentinel-taxonomy.md:510`'s `tx.Queries().Commit` example to
  the promotion-shadow wording.

## 12. Declined, with reasons

- **Embed `Queries` (exported) on Tx** — one line, and wrong: `Begin`,
  `WithTx` promote onto `Tx`, so `tx.Begin(ctx)` compiles and the
  nesting guard is demoted back to runtime. The unexported core exists
  precisely to split "query methods" from "root-handle lifecycle".
- **A new exported root type (`DB`/`Repository`) owning Begin, with
  `Queries` demoted** — achieves the same split at the cost of breaking
  `New`'s return type, every caller's first line, and one more exported
  name per package; the unexported core achieves it with zero exported
  surface change at the root.
- **Keep `(*Tx).Queries()` alongside promotion** — reopens
  `tx.Queries().Begin` (compilable, runtime-refused), which is the door
  this design exists to close; also leaves two spellings for every
  call, and the gate would have to defend both forever.
- **Interface-shaped Tx or closure API** — closures were declined by
  the owner on gqlc-h0lw (`codegen-tx-object.md` §10) and are not
  re-proposed.
- **Reserving `queries` in `reservedIdentifiers`** — guards a state the
  query-name and entity-name grammars already refuse (§2); a refusal
  that cannot fire is dead weight in a table people read for truth.

## 13. Execution shape

One bead (gqlc-f4hf), one PR: renderers + gate + methodset test +
justfile enrollment + live rewrite + goldens + comment/doc edits are one
coherent change — the gate cannot go green with only half the emissions
moved. The bead is design-gated, so the executing Ռազմիկ files a
`class:judge` review bead per protocol, and the battery of §10 ships
measured in the PR body.
