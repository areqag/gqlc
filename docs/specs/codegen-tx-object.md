# codegen: the emitted Tx object

Design for bead `gqlc-h0lw`. Written 2026-08-24 by Արթուր; every claim
about driver behaviour below was read out of the pinned driver sources in
the module cache, not out of documentation or memory. The corpus pins are
`neo4j-go-driver/v5 v5.28.4`, `neo4j-go-driver/v6 v6.2.0` and
`pgx/v5 v5.10.0` (`test/data/codegen/go.mod`), and the file:line
references below are into those versions.

## 1. The problem in plain words

gqlc knows the target driver at generation time, yet a user who wants an
explicit transaction must open it themselves, against a driver-specific
API, and then hand it to the generated package through `WithTx`. The two
backends' rituals differ in every particular — `neo4j.ManagedTransaction`
obtained inside a closure on one side, `pgx.Tx` from a pool on the other —
so the one piece of user code that most wants to be target-portable, the
begin/commit/rollback wrapper, is exactly the piece each user writes twice.

The repository should emit that wrapper. The shape is decided by the
owner and is a constraint, not an option: a method on the repository
returns a **`Tx` object** with `Begin` on the repository and `Commit` /
`Rollback` on the object — a value that can be stored and passed around.
A closure API (`WithTransaction(ctx, func(q *Queries) error)`) is
rejected and must not be re-proposed as the primary surface. The neo4j
access-mode split is resolved by decree: generated transactions are
**always write mode**; no read/write inference pass is designed here.

## 2. What exists now, verified

- neo4j (`internal/codegen/neo4j/render_db.go`): `Queries` holds an
  unexported `db driverOrTx`; `New(driver)` wraps `driverDB`, and
  `WithTx(tx neo4j.ManagedTransaction)` wraps `txDB`. The per-call path
  opens a session and runs `neo4j.ExecuteRead` / `ExecuteWrite` — the
  managed, retrying, closure path.
- AGE (`internal/codegen/age/render_db.go`): `Queries` holds the exported
  `DBTX` seam (`Exec` + `Query`), satisfied by `*pgxpool.Pool`,
  `*pgx.Conn` and `pgx.Tx`; `WithTx(tx pgx.Tx)` rebinds it, carrying the
  bound graph name forward.

Neither backend can OPEN a transaction. That is the whole gap.

## 3. Driver facts the design stands on

Each fact below names its witness. An executing Ռազմիկ does not need to
re-verify these, but can.

**F1 — both neo4j majors expose the same unmanaged surface.**
`SessionWithContext.BeginTransaction(ctx, ...configurers) (ExplicitTransaction, error)`
(v5 `session_with_context.go:60`) and
`Session.BeginTransaction(...)` (v6 `session.go:54`) are name-identical
apart from the session interface's own name (`SessionWithContext` v5,
`Session` v6; `driver.NewSession(ctx, SessionConfig)` in both, v6
`driver.go:69`). `ExplicitTransaction` is `Run` / `Commit` / `Rollback` /
`Close` in both (v5 `transaction_with_context.go:35`, v6
`transaction.go:35`).

**F2 — the two majors' explicit-transaction state machines are the same
machine.** Compared v5 `transaction_with_context.go:91-126` against v6
`transaction.go:88-123`: `Commit` after completion returns a `UsageError`
("cannot use this transaction, ...`"); `Rollback` after completion
returns the same `UsageError`; `Close` is rollback-if-pending and a
**no-op returning nil** on a completed transaction; `Rollback` after an
in-transaction `Run` error returns nil (the failed result handler already
tore the transaction down).

**F3 — an `ExplicitTransaction` satisfies the seam `WithTx` already
takes.** v5 `ManagedTransaction` is `{Run, legacy()}` and
`explicitTransaction` carries both (`transaction_with_context.go:27-32,
128`); v6 `ManagedTransaction` is `{Run}` alone (`transaction.go:28-32`).
Interface-to-interface assignment holds in both majors, so the tx-bound
`Queries` needs **no new seam**: `txDB{tx: <ExplicitTransaction>}`
compiles today.

**F4 — the unmanaged path loses the retry loop and nothing else relative
to today's emission.** Retry lives in `runRetriable`
(`session_with_context.go:428-`) and is entered only from
`ExecuteRead`/`ExecuteWrite`; `BeginTransaction` never reaches it. So a
generated `Tx` that dies on a transient cluster error (leader switch,
connection loss) surfaces the error where the managed path would have
retried within `MaxTransactionRetryTime`. That is the cost, it is real,
and it is accepted: an object with `Commit` cannot retry, because
retrying means re-running user code that already observed results.
Bookmarks are NOT additionally lost: `BeginTransaction` sends the
session's bookmarks and `onClose` retrieves them
(`session_with_context.go:352, 389`), and today's emitted per-call path
already opens each session with a bare
`SessionConfig{AccessMode: ...}` — no bookmark propagation across
generated calls exists now, and none is removed.

**F5 — one explicit transaction per session is driver-enforced**
(`session_with_context.go:322`, "Session already has a pending
transaction"), and a session is a pooled connection while a transaction
is open. So the emitted `Tx` must own a private session, and both must be
closed on every path the generated code controls.

**F6 — pgx's `Tx` is begin-capable everywhere we need it.**
`(*pgxpool.Pool).Begin(ctx) (pgx.Tx, error)` (`pgxpool/pool.go:798`),
`(*pgx.Conn).Begin(ctx) (pgx.Tx, error)` (`tx.go:94`), and
`pgx.Tx.Begin` itself (savepoint nesting, `tx.go:165`). After close,
`Commit` and `Rollback` both return `pgx.ErrTxClosed` (`tx.go:85,
180-182, 209-212`); a query on a closed tx fails with the same sentinel.

## 4. The emitted surface

Identical exported surface in both generated packages; driver names
appear only in unexported struct fields and bodies. Doc comments on the
exported declarations are part of the emission and must state the
semantics table of §5 (write mode, ErrTxDone, nil-on-late-Rollback, the
caller's duty to finish the transaction).

### 4.1 neo4j (`render_db.go`, appended to db.go; v5 spelling)

```go
// ErrTxDone is returned by Commit when the transaction has already
// been committed or rolled back. Rollback on a finished transaction
// returns nil instead, so `defer tx.Rollback(ctx)` is always safe.
var ErrTxDone = errors.New("gqlc: transaction has already been committed or rolled back")

// Tx is an open write transaction and the session that owns it. It is
// finished by exactly one Commit or Rollback; the zero value is not
// usable — Begin is the only constructor.
type Tx struct {
	session neo4j.SessionWithContext
	tx      neo4j.ExplicitTransaction
	done    bool
}

func (q *Queries) Begin(ctx context.Context) (*Tx, error) {
	d, ok := q.db.(driverDB)
	if !ok {
		return nil, errors.New("gqlc: Begin on a transaction-bound Queries")
	}
	session := d.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	tx, err := session.BeginTransaction(ctx)
	if err != nil {
		return nil, errors.Join(err, session.Close(ctx))
	}
	return &Tx{session: session, tx: tx}, nil
}

func (tx *Tx) Queries() *Queries {
	return &Queries{db: txDB{tx: tx.tx}}
}

func (tx *Tx) Commit(ctx context.Context) error {
	if tx.done {
		return ErrTxDone
	}
	tx.done = true
	return errors.Join(tx.tx.Commit(ctx), tx.session.Close(ctx))
}

func (tx *Tx) Rollback(ctx context.Context) error {
	if tx.done {
		return nil
	}
	tx.done = true
	return errors.Join(tx.tx.Close(ctx), tx.session.Close(ctx))
}
```

Notes pinned to lines above:

- `q.db.(driverDB)` is the type switch that refuses `Begin` on a handle
  `WithTx` or `tx.Queries()` produced. neo4j cannot nest transactions
  (F5), so the refusal is the portable behaviour — see §5.
- `tx.tx.Close(ctx)` in `Rollback`, not `tx.tx.Rollback(ctx)`: `Close`
  is rollback-if-pending and nil on an already-torn-down transaction
  (F2), which makes Rollback-after-failed-Run clean without a second
  state flag.
- `txDB{tx: tx.tx}` type-checks by F3. `Queries()` deliberately returns
  the same shape `WithTx` returns; `WithTx` itself is unchanged and
  remains the entry point for an externally-owned transaction.
- `errors.Join` (go ≥1.20; the corpus module is at go 1.26.6) carries a
  session-close failure without hiding a commit failure; it is nil when
  both are nil.

### 4.2 v6 differences

The session field's type is `neo4j.Session` instead of
`neo4j.SessionWithContext`. Everything else is byte-identical.
`driverTarget` (`internal/codegen/neo4j/driver.go`) grows a
`sessionIface` field (`"neo4j.SessionWithContext"` / `"neo4j.Session"`),
and `driverAgnostic` (`internal/codegen/neo4j/corpus_test.go:255`) grows
one `strings.ReplaceAll(body, "neo4j.SessionWithContext",
"neo4j.Session")` so the existing v5≡v6 emission gate keeps holding.
Order the replacements so the driver-handle replacement cannot corrupt
the session name (replace the session iface first; the two literals do
not overlap, but say so in a comment rather than relying on the reader
noticing).

### 4.3 AGE (`render_db.go`, appended to db.go)

```go
// ErrTxDone — same text and comment as §4.1, byte-identical.
var ErrTxDone = errors.New("gqlc: transaction has already been committed or rolled back")

// Tx — same comment obligations as §4.1.
type Tx struct {
	tx    pgx.Tx
	graph string
	done  bool
}

func (q *Queries) Begin(ctx context.Context) (*Tx, error) {
	if _, ok := q.db.(pgx.Tx); ok {
		return nil, errors.New("gqlc: Begin on a transaction-bound Queries")
	}
	b, ok := q.db.(interface {
		Begin(ctx context.Context) (pgx.Tx, error)
	})
	if !ok {
		return nil, errors.New("gqlc: the DBTX bound by New cannot begin a transaction")
	}
	tx, err := b.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, graph: q.graph}, nil
}

func (tx *Tx) Queries() *Queries {
	return &Queries{db: tx.tx, graph: tx.graph}
}

func (tx *Tx) Commit(ctx context.Context) error {
	if tx.done {
		return ErrTxDone
	}
	tx.done = true
	return tx.tx.Commit(ctx)
}

func (tx *Tx) Rollback(ctx context.Context) error {
	if tx.done {
		return nil
	}
	tx.done = true
	return tx.tx.Rollback(ctx)
}
```

Notes:

- The `pgx.Tx` check MUST precede the **call** to `b.Begin`: `pgx.Tx`
  itself has `Begin` (savepoints, F6), so it satisfies the capability
  assertion and the call under it alike, and this check is the only
  thing that refuses it. Placed after the call, the refusal would first
  open a savepoint it then has to abandon.
  Its order against the **assertion alone** is not load-bearing. An
  earlier draft of this spec claimed it was — that swapping the two made
  "the refusal never fire" — and that was measured false on 2026-08-24
  (bd `gqlc-3d0l`, dispatch run `32693045598`): swapped, `pgx.Tx` passes
  the assertion, falls through to the check, and the refusal still
  fires. The AGE live row stayed green. §8 carries the row.
- The refusal covers both a `WithTx`-derived handle and a `pgx.Tx`
  passed directly to `New` as the `DBTX` — the check is on the dynamic
  type, not on how the handle was made.
- The second error ("cannot begin") is AGE-only: the `DBTX` seam admits
  user fakes that cannot begin. neo4j's `driverDB` always can, so no
  counterpart exists there. This is a body difference, not a surface
  difference; the §7 gate compares surfaces.
- `DBTX` itself is unchanged. Widening it would break every existing
  fake implementing two methods.

## 5. Semantics, identical on both backends by construction

| case | behaviour | underlying witness |
|---|---|---|
| `Begin` on the handle `New` returned | `(*Tx, nil)`, write mode | F1, F6 |
| `Begin` on a tx-bound handle | error `gqlc: Begin on a transaction-bound Queries` | neo4j cannot nest (F5); AGE refusal is emitted to match |
| first `Commit` | commits; neo4j also closes the owned session | F2 / F6 |
| `Commit` after `Commit` or `Rollback` | `ErrTxDone`, driver never reached | done flag |
| first `Rollback` | rolls back; nil after an in-tx statement error | F2 (`Close`), F6 |
| `Rollback` after `Commit` or `Rollback` | `nil` — `defer tx.Rollback(ctx)` is unconditionally safe | done flag |
| query via `tx.Queries()` after finish | driver's own already-completed error (neo4j `UsageError`, AGE `pgx.ErrTxClosed`) — deliberately not normalised; generated methods surface driver errors today and this changes nothing | F2, F6 |
| `Tx` abandoned unfinished | holds a pooled connection until the pool's own policies reclaim it, on both backends; the generated code cannot fix this and says so in the doc comment. Every path the generated code controls closes the neo4j session exactly once. | F5, F6 |

Nested transactions are refused rather than half-supported: AGE could
nest via savepoints, neo4j cannot, and a surface that nests on one
backend and errors on the other is the portability failure this bead
exists to remove. If savepoint support is ever wanted it is a new bead.

## 6. Placement: generated per target, no shared runtime module

`Tx` is emitted into each generated package, structurally identical, no
shared import. Rejected: a gqlc runtime module carrying a `Tx`
interface.

- Precedent: every piece of emitted API surface today is per-package —
  the `ErrNoRows` / `ErrMultipleResults` sentinels are duplicated
  per package by design, `DBTX` is declared in-package, and ADR 0012
  makes the output directory self-contained. There is no gqlc runtime
  module, and inventing one for three one-line methods buys nothing:
  target portability here means *source* portability (swap the import,
  recompile), which structural identity already delivers and the §7
  gate enforces.
- A shared interface would buy cross-package type identity — code that
  handles both backends' transactions in one process. Nobody has asked
  for that, and it can be added later without breaking anything: a
  later interface `{Commit(ctx) error; Rollback(ctx) error}` in a
  runtime module is satisfied by these emitted `*Tx` types as they
  stand.
- The same placement question is open on gqlc-49hu (neutral temporal
  types, Արփինէ). The answers are aligned, not coupled: 49hu's types
  appear in exported *signatures* and so may genuinely need type
  identity across packages; `Tx` never crosses a package boundary.
  Nothing here forecloses a runtime module if 49hu needs one; this
  design simply does not require it. Divergence-of-record is handled by
  mail between the two design beads, and any residue goes to Սեդրակ.

## 7. The surface-agreement gate

A new test, `internal/codegen/txsurface_test.go` (external test package,
importing both backend packages — legal, only backends import
`internal/codegen`), which FAILS when the two backends' emitted `Tx`
surfaces differ:

1. Drive one minimal schema + query batch both backends accept (single
   node type, `STRING`/`INTEGER` properties only, one `:one` and one
   `:exec` query) through both `Generate`s.
2. Parse each emitted `db.go` with `go/parser` and collect, **from the
   AST, not the source bytes** (a commented-out declaration must not
   count): the `Tx` type declaration's presence; the rendered signature
   (via `go/printer`, receiver dropped) of every exported method on
   `*Tx` and of `(*Queries).Begin`; the string literal inside
   `ErrTxDone`'s `errors.New`; and the `Begin`-refusal string literal.
3. Assert **per backend, by name**, that exactly
   `{Tx, Begin, Commit, Rollback, Queries, ErrTxDone}` are present —
   a positive declaration on each side, so that the gate cannot pass
   vacuously when both backends omit the block (two empty maps compare
   equal; that shape of silent pass is a measured failure mode in this
   repo).
4. Assert the collected signatures and both string literals are equal
   across backends. Every signature in the set names zero driver types
   (`Begin(ctx context.Context) (*Tx, error)`,
   `Commit(ctx context.Context) error`,
   `Rollback(ctx context.Context) error`, `Queries() *Queries`), so
   equality needs no normalisation.

The v5/v6 half of the agreement is already held by the existing
`driverAgnostic` gate (§4.2 extends it); this test compares v5 against
AGE and lets transitivity cover v6.

This dedicated gate is load-bearing, not belt-and-braces. The corpus's
cross-target comparison, `TestBackendInvariantSurface`, deliberately
excludes `db.go` and `graph.go` via the `connectionSurface` map
(`internal/codegen/conformance/conformance_test.go:651`, applied at
:751), because those files hold the backend-specific handle and differ
by construction. Tx lands in `db.go` (§4), so that gate is blind to it
by design: without this test, the claim "the Tx surface is identical
across backends" would be asserted nowhere. Do not delete this test as
redundant with the conformance gate, and do not "fix" the overlap by
un-excluding `db.go` there — the rest of db.go differs on purpose.
(Exclusion confirmed independently by the gqlc-49hu design; its
temporal.go carriers are NOT excluded, which is why that design needs
no dedicated gate and this one does.)

## 8. Witnesses and the mutation battery the execution PR owes

The change adds guards, so the PR records rows per ADR 0005 /
citizen-protocol step 3, at minimum:

| guard | mutation | expected victim |
|---|---|---|
| surface gate presence arm | delete the whole Tx block from the AGE renderer | txsurface row "AGE: missing Begin" (per-name assertion, step 3 above) |
| surface gate equality arm | change AGE's `ErrTxDone` literal by one character | txsurface literal-equality row |
| surface gate name arm | emit `rollback` (unexported) instead of `Rollback` in neo4j | txsurface row "neo4j: missing Rollback" |
| done flag (both backends) | drop `if tx.done { return ErrTxDone }` from Commit | live row: double-Commit expects `ErrTxDone`. **MEASURED KILLED** on both neo4j arms, 2026-08-24, run `32692156620`. The AGE arm was left untouched by that mutation and stayed green, which is the run's own blinding control. Every offline gate is blind to this one — it is behaviour, not surface. |
| Rollback idempotence | make late Rollback return `ErrTxDone` instead of nil | live row: Rollback-after-Commit expects nil. **NOT MEASURED** as written. A weaker variant was: see the row below. |
| Rollback done-guard | drop `if tx.done { return nil }` from Rollback, both renderers | **MEASURED SPLIT**, 2026-08-24, run `32693045598`. **KILLED** on AGE (`Rollback after Commit is nil` fails, `live_test.go:661`; the other five rows pass, so the kill is scoped). **SURVIVED** on both neo4j arms — mutation confirmed live in the compiled fixture, and the arm printed `ok … 42.375s`, not `(cached)`. It survives because the neo4j driver's `Close` is already idempotent, so the early return changes nothing observable there. The guard is kept as defence-in-depth that makes the surface contract independent of the driver; no black-box row can kill it while the driver keeps that behaviour, and it is the AGE arm that witnesses the guard is needed at all. Recorded as a SURVIVED rather than deleted or claimed as a kill. |
| Begin refusal (neo4j) | drop the `driverDB` type switch | live row: Begin on `WithTx`-handle expects error. **NOT MEASURED, and not measurable as written**: the refusal doubles as the nil-guard for the two lines under it. `driverDB.driver` is an interface (`target.driverIface`), so with the `if !ok` return gone, `d` is the zero value and `d.driver.NewSession(...)` is a method call on a nil interface — a panic by language rule, not an assertion firing. The row would go red via a crash that also takes the rest of the battery with it, which is a fake RED and would have to be dispatched alone to mean anything. What witnesses this refusal instead is the **blinding** row below: it proves the scenario executes on the neo4j arms. |
| Begin refusal order (AGE) | swap the `pgx.Tx` check after the capability assertion | **MEASURED NO-OP**, declared as such in advance and against this table's own earlier prediction of a kill. 2026-08-24, run `32693045598`: `tx: Begin inside a transaction is refused` PASSED on the AGE arm with the two swapped. `pgx.Tx` satisfies the capability assertion and falls through to the check, so the refusal still fires. The prediction this row used to carry — "gets a savepoint tx" — was false, and it had been copied into the emitted comment in every generated AGE `db.go`; both are corrected. The order that *is* load-bearing is check-before-`b.Begin`, and that one is **unwitnessed**: placed after the call, the function still returns the error, so the live row stays green and only a savepoint leaks. Bead `gqlc-g8rc` filed. |
| Begin refusal presence (AGE) | delete the `pgx.Tx` check and its `errors.New` outright | **MEASURED KILLED OFFLINE**, 2026-08-24 — and it never reached CI. `TestTxSurfaceAgreesAcrossBackends` failed at pre-push (`txsurface_test.go:97`): "the backends share no single Begin refusal message". The cheaper kill, in seconds and without Docker. But note what it does *not* witness: the gate matches the refusal **message**, so it fired without the live row ever being consulted. |
| Begin refusal condition (AGE) | keep the message, assert on `*pgx.Conn` — a dynamic type the bound handle never has | **MEASURED KILLED**, 2026-08-24, run `32695403649`. `TestTxSurfaceAgreesAcrossBackends` was verified PASSING under this mutation before dispatch, so every offline gate is blind to it and the live arm is the only objector. AGE arm: `tx: Begin inside a transaction is refused` FAILS at `live_test.go:679`, "An error is expected but got nil" — the declared failure mode. The other eleven AGE rows pass, so the kill is scoped. neo4j green, as the mutation is AGE-only. Together with the row above, the refusal is guarded in both halves: its message offline, its condition live. Neither gate covers both. |
| session ownership (neo4j) | drop `session.Close` from Commit | expected **NO-OP**, declared as such: the driver's own `Commit` returns the pooled connection via its `onClosed` hook (`session_with_context.go:389`), and every `BeginTransaction` failure path returns it too (`session_with_context.go:343, 352, 369` — verified, all three arms). So no path the generated code owns holds a connection that `session.Close` would release, and no behavioural row can go red on it. The closes are kept because the session contract demands them, not because a resource is observably freed; a row that reads KILLED here is the surprise, and would mean the driver's lifecycle changed under us. |
| v5≡v6 gate extension | emit a v6-only comment line in the Tx block | existing `driverAgnostic` equality row |
| blinding: does the battery run at all | change scenario 1's expected name from "Bob" to "Carol" | **MEASURED KILLED on 3/3 arms**, 2026-08-24, run `32691410054`, victims named per arm. Run **first**, before any real mutation: until `-v` was added to `test-codegen-live-neo4j` that arm printed one package-level `ok`, so a green live-smoke looked identical whether the six scenarios executed or never ran. No verdict below is worth anything without this row. |
| blinding: the three rows no mutation reached | invert the expectation in scenarios 2, 3 and 6 simultaneously | rows 2, 3, 6 RED on all three arms, rows 1, 4, 5 GREEN — 9 reds and 9 greens declared per row before the run. **MEASURED exactly that**, 2026-08-24, run `32696442170`, no row out of place. Run because M1 blinds only scenario 1, and the real mutations between them reach only 1, 4, 5 and 6-on-AGE; without it, scenarios 2 and 3 and 6-on-neo4j were compiled, listed, green and unproven to execute. This was also the first dispatch carrying the `-v` added to `test-codegen-live-neo4j`, and that arm named all twelve of its rows — the instrumentation fix is itself witnessed. |

Behavioural rows live in the live battery (`test/data/codegen/live_test.go`
scenario list), which runs every scenario against every arm: the neo4j
arms are PR-blocking (`codegen-live.yml` live-smoke), the AGE arm is
nightly/manual.

**That asymmetry costs more than this spec first claimed.** The original
justification — "acceptable because pgx itself carries most of the AGE
behaviour (F6) and the PR-blocking fence still compiles the AGE emission" —
survives only in part. The battery above measured two guards that are killed
on the AGE arm **alone**: the Rollback done-guard (neo4j's driver `Close` is
already idempotent, so it survives there) and the AGE Begin refusal's
condition as distinct from its message. `live-smoke-age` carries
`if: github.event_name != 'pull_request'`, and `codegen-live.yml` records at
that line that GitHub counts a *skipped* check as satisfying a required
context — which is why the id is deliberately kept off master's required
list. So a change breaking either guard merges green and is caught by the
nightly, which files an issue rather than blocking. Not silent; not
PR-blocking either. Stated here so the check table is not read as promising
more than it does.

Driver-stub unit tests are NOT available for the neo4j rows:
`ExplicitTransaction` carries the unexported `legacy()` method in v5, so
it cannot be faked outside the driver package — that is why the rows sit
in the live battery and not beside the renderer.

New live scenarios (each runs on v5, v6 and — nightly — AGE):

1. begin → write → commit → row visible to a fresh handle.
2. begin → write → rollback → row absent.
3. begin → write → read *through the same Tx* sees the uncommitted row.
4. double-Commit → second returns `ErrTxDone`.
5. Rollback-after-Commit → nil.
6. Begin on `tx.Queries()` handle → refusal error.

## 9. Renderer changes, in full

| file | change |
|---|---|
| `internal/codegen/neo4j/driver.go` | add `sessionIface` to `driverTarget`, both values |
| `internal/codegen/neo4j/render_db.go` | emit §4.1 block; `errors` import becomes unconditional (today it is gated on `emitOneSentinels`) |
| `internal/codegen/age/render_db.go` | emit §4.3 block; `errors` import becomes unconditional (today gated on `withOneSentinels`) |
| `internal/codegen/neo4j/corpus_test.go` | extend `driverAgnostic` per §4.2 |
| `internal/codegen/txsurface_test.go` | new, §7 |
| `test/data/codegen/live_test.go` (+ arm files if adapters need a Begin hook) | scenarios §8 |
| `test/data/codegen/valid/*/golden/*/db.go` | regenerate — every fixture's db.go grows the block; large but mechanical churn, `just test` regolds |
| `CONTEXT.md` | done in the design PR itself (Transaction handle entry) |
| `internal/codegen/prepare.go` | reserve the five new exported names — §9.1 |
| `internal/codegen/prepare_test.go` | the reserved rows, and the scope gate's biconditional — §9.1 |
| `docs/specs/codegen-sentinel-taxonomy.md` | §6's table and the counts it states in prose; that section is gate-held cell-by-cell, so it cannot be skipped — §9.1 |
| `internal/codegen/conformance/conformance_test.go`, `internal/codegen/age/age_test.go`, `internal/codegen/neo4j/testdata/driverstub_neo4j.go.txt` | existing assertions over the emitted surface, widened for the block |
| `.golangci.yml` | the live battery's new interfaces join the per-type `ireturn` allow-list |

### 9.1 Reserved identifiers

This subsection was absent from the design and is written from the
execution (`gqlc-3d0l`, PR #1489), ruled in by Արթուր rather than
improvised: the omission was the design's.

The block adds five exported names to `db.go` on all three targets, so
they must be reserved. They are reserved **at their true scope, with no
new scope value**: `Tx` and `ErrTxDone` at `scopePackage`; `Begin`,
`Commit` and `Rollback` at `scopeMethod`. The scope column means
"occupies the package block or not" and is receiver-agnostic by charter,
so a `*Tx` method answers it exactly as a `*Queries` method does and a
third value would encode nothing.

The five do not all stand on the same ground, and §6 must say which is
which:

- **`Begin` stands on a real collision.** It is declared on `*Queries`,
  so a query of that name would redeclare it.
- **`Commit` and `Rollback` stand on call-site ambiguity, not
  redeclaration.** They are declared on `*Tx`, so `func (q *Queries)
  Commit` would compile. They are reserved anyway, package-wide and
  receiver-blind, because `tx.Commit()` and `tx.Queries().Commit(ctx,
  ...)` sit one selector apart — one ends the transaction, the other
  runs a user query — and that is the misreading that loses data. The
  remedy costs the author one rename under a clear diagnostic; the
  ambiguity costs every later reader.

`Queries` is consequently declared at **both** scopes (the handle type,
and the accessor on `*Tx`). The scope gate therefore became a
biconditional — `scopePackage` iff some golden declares the name
package-level. Only the permissive direction was relaxed; the strict
direction is preserved whole, and it is the load-bearing one, because it
is what stops sources 1–6 taking a name the package block already holds.

The Tx block is emitted unconditionally (not gated on the batch having
queries): it is repository surface, all of it exported, so the emitted-
but-unused lint concern that gates the AGE composer does not apply.

## 10. Declined, with reasons

- **Closure helper on top of the object** — owner rejected closures as
  the primary API; adding the secondary form now is surface nobody asked
  for. New bead if wanted.
- **Read-mode transactions** — decreed out; a follow-up bead may add a
  read mode later (the decree in gqlc-h0lw's own text).
- **`BeginTx` with driver options (timeouts, metadata, pgx.TxOptions)** —
  both drivers support it (`BeginTransaction` configurers;
  `pgxpool.BeginTx`); no portable subset was asked for. The escape hatch
  for a caller who needs options is the one that exists today: open the
  transaction themselves and use `WithTx`.
- **A `Close` method on Tx** — `Rollback`'s nil-after-done covers the
  defer idiom; a third finisher is redundant surface.
- **Normalising the after-finish query error across backends** —
  generated query methods surface driver errors today; wrapping every
  error path to rewrite one case is out of proportion.

## 11. Execution

One execution bead, one PR: the renderer edits, the gate, the golden
regeneration and the live rows are one coherent change, and the gate
cannot land before both emissions exist. The PR is review-owed (its bead
is blocked by gqlc-h0lw, a design bead), so the executing Ռազմիկ files a
`class:judge` bead on it per protocol.
