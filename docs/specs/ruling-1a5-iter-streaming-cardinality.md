# Ruling — `:iter` ships as ADR 0010 D8 wrote it; `:many` does not move

The design answer for **gqlc-1a5** ("Codegen: `:iter` streaming cardinality"),
and the implementation brief for that same bead, which stays open to carry the
build (§7).

Two research sessions (2026-09-08, 2026-09-09/10) closed every feasibility
question this needed and recorded `STATUS: no decision taken`. This document
takes the decision. It re-measures the load-bearing claims rather than citing
them; §9 lists every place the bead's ledger and the tree disagree, including the
two figures the re-measurement moved.

Every file:line below was read, and every probe run, at this branch's base,
master `21720c45`. The probes were first run at `687896a9` and the counts and
line numbers re-derived after the rebase; the one claim that did **not** survive
it is §5's third bullet, which named a defect that has since been fixed on
master, and it is restated rather than deleted.

**The short answer.** ADR 0010 §D8's 2026-07-11 ruling stands unchanged:
`:iter` is an **author-declared, opt-in fourth cardinality** returning
`iter.Seq2[Row, error]`, and `:many` keeps `([]T, error)`. The owner's question
— make the iterator universal on `:many` — is declined, and so is the
`Result[T]` lazy thunk that was raised as the softer version of it. The single
strongest reason is not memory or ceremony: **`:many` is the cardinality gqlc
allows on writes, and both universal options hand a streaming spender to a write
query, which is exactly what D8's `ErrIterOnWrite` was ruled to refuse at
generation time — and which, once the spender is chosen at the call site,
generation can no longer see.** §3.1.

**Why this is not an ADR.** It changes no shipped behaviour, no wire format, no
generated signature. The *execution* owes one — a new annotation token, a new
sentinel and a new emitted method shape are user-visible — and §6 says what goes
in it.

---

## 1. The measurement everything else is weighed against

`errcheck` polices a returned error; over the arms below it does not police a
yielded one. Run against the repo's pinned `.bin/golangci-lint` v2.13.1 and the
real `.golangci.yml` (`errcheck.check-blank: true`,
`check-type-assertions: true`), on a scratch module outside the tree, with doc
comments and `gofmt` applied so `revive`/`gci`/`gofumpt` are silent and the exit
status is errcheck's alone. **The whole enabled linter set runs**, not errcheck
in isolation, so a `0 issues.` row is the claim that none of them fires:

| arm | source | verdict |
|---|---|---|
| materialising | `rows, _ := Slice()` | `Error return value is not checked (errcheck)` — **EXIT=1** |
| streaming | `for row := range Seq()` | `0 issues.` — **EXIT=0** |

The bead's figure holds exactly, and it re-ran unchanged after this branch was
rebased onto `21720c45`. And the failure the linter is silent about is not a
dropped error. D8's sketch yields `(zeroRow, err)`, and a one-variable `range`
over that — legal Go, `go vet` exit 0 — **appends a phantom zero-valued row**.
Measured on go1.27.0 over a sequence yielding two good rows then an error:

```
len=3 rows=[]main.Row{{Name:"ada", Age:36}, {Name:"bob", Age:41}, {Name:"", Age:0}}
```

and reproduced on the rebase with a one-good-row sequence, which lands
`len=2 … {Name:"", Age:0}` — the phantom is the last element either way, one per
error yielded, not an artefact of the row count.

The consumer's slice is longer than the result set and the extra element is
indistinguishable from a real empty row. That is silent-wrong, the class
ADR 0030's stated posture and ADR 0010 §D1's "reject, don't guess" exist to
refuse.

This does **not** say the streaming shape is unsafe, and it is not a claim about
Go tooling in general — only about the linters this repository gates a PR with.
It says the shape has a hazard those linters do not catch, so the decision is
*who has to opt into it, and whether the opt-in is visible where the query is
reviewed*.

## 2. Universal `:many` → `iter.Seq2` is declined

It removes the materialising shape, so it removes the EXIT=1 row of §1 from
100% of read queries in the corpus — 51 `:many` annotations across 34 of 97
valid fixture dirs (`// name: X :many`, counted below). Nothing replaces it: the
error is delivered through a yield parameter that a consumer is free to not
bind, and no linter in `.golangci.yml` reads that.

It also takes the live harness's `:many` contract with it.
`test/data/codegen/live_test.go:1002` asserts
`require.NotNil(rows, "empty :many result must be an empty slice, not nil")`,
and `iter.Seq2` has no nil/empty distinction, so the row has no analogue and must
be deleted rather than restated. The contract it pins is emitted deliberately and
uniformly: of 129 `out := make(` sites in the valid corpus's `*.cypher.go`
goldens, 129 give length 0 — 80 as `make([]T, 0, len(records))` on neo4j and the
rest as `make([]T, 0)` on AGE. A `nil` result slice is not a shape gqlc emits
today.

Not among the reasons: backward compatibility. The owner has authorised breaking
changes (unreleased, zero users, no version tags), and this ruling weighs none.

## 3. `Result[T]` is declined, and not on cost

The lazy thunk — `:many` returns a value holding ctx+params that opens nothing at
construction, exposing `.All() ([]T, error)` and `.Range() iter.Seq2[T, error]`
— is the strongest of the three options and deserves the argument that beats it
on something other than golden churn. Three things beat it.

### 3.1 It destroys the write gate, which is the decisive one

D8 ruled `:iter` **read-only**, enforced by a new `ErrIterOnWrite` at generation
time: streaming a write yields rows to the caller while the transaction is open,
so a mid-stream rollback leaves the caller holding rows that no longer exist, and
a pre-first-yield retry re-runs the `CREATE` so a stashed `elementId` no longer
matches. `:many` holds "if you see it, it's committed"; a streamed write cannot.

`:many` **is** the write cardinality for projecting writes —
`write_many_projection_entity` and `write_many_projection_property` are both in
the valid corpus, and both land in `WriteQuerier`:

```
test/data/codegen/valid/write_many_projection_entity/queries.cypher:1
  // name: MarkAdults :many
  MATCH (p:Person) WHERE p.age >= $minAge SET p.checked = true RETURN p
```

Under `Result[T]` on `:many`, `MarkAdults(ctx, 18).Range()` is that refused
program, and it is refused by nothing. The cardinality annotation no longer
records whether the caller streams, so the generation-time gate has no input.
The remedies are both bad: emit a second, `Range`-less result type for write
queries — two generic types per generated package, and the user meets the
difference as a compile error with no sentinel and no witness — or permit the
leak. D8's `:iter` opt-in keeps the gate intact, because the author declares the
spender in the file the reviewer reads, one query at a time, and
`ErrIterOnWrite` fires there with the query name and position.

This is not a preference between two safe designs. One of them can enforce a rule
this project has already taken and the other cannot.

### 3.2 A lazy thunk with no error in its signature is a silent no-op

Measured the same way as §1, four arms, same linter and config:

| arm | source | verdict |
|---|---|---|
| A | `rows, _ := Lazy().All()` | errcheck fires — **EXIT=1** |
| B | `for row := range Lazy().Range()` | `0 issues.` — **EXIT=0** |
| C | `Lazy()` as a bare statement | `0 issues.` — **EXIT=0**, `go vet` 0 |
| D | `Today()` as a bare statement | errcheck fires — **EXIT=1** |

Arm C against arm D is the finding. Today, calling a `:many` method and spending
nothing is caught, because the signature returns an error. Under `Result[T]` the
signature returns one value and no error, so the call is clean to every tool this
repository runs — `go vet` and the pinned `golangci-lint` under `.golangci.yml`,
which is the whole of what gates a PR here, not a claim about the Go ecosystem —
and because the thunk is lazy, *the query does not run*. Confirmed by execution:
a `Lazy()` whose `.All()` sets a flag leaves the flag `false`. `unparam` and
`unused` are both enabled in `.golangci.yml` and neither fires on arm C, so this
is not a case of one linter being switched off. What was **not** tested is a
custom `go/analysis` pass; a project willing to write one could catch arm C, and
§7 records that as the falsifier.

Opening nothing at construction is what makes `Result[T]` leak-free, and it is
the same property that makes forgetting to spend it invisible. The two cannot be
separated.

### 3.3 `.Range()` is exactly as unpoliced as a bare `iter.Seq2`

Arm B is EXIT=0, identical to §1's streaming arm. So `Result[T]` does not repair
the phantom-row hazard; it relocates the opt-in from the query file to the call
site. The safety delta against D8's `:iter` is zero — both confine the unpoliced
shape to an explicit opt-in — and what is actually being traded is *where the
opt-in is written*.

The case for the call site is that knowledge of data volume lives there. That is
true and it is already served: D8 says a caller who wants both shapes writes two
annotations and gets two methods, which is the same expressive power for one
extra line in a `.cypher` file. What the call site additionally buys is not
having to pre-declare — and it pays for that with §3.1 and §3.2.

### 3.4 The cost, stated so it is not the reason

The bead records "158 goldens" from the spike. That number does not reproduce,
and the re-measurement is larger, so the command is given here rather than the
figure alone — the count is only as good as what it counts, and the spike's is
not reconstructible:

```
grep -rhE '^func \(q \*queries\) [A-Za-z0-9_]+\(ctx context\.Context.*\) \(\[\].*, error\) \{$' \
  --include='*.go' test/data/codegen/valid | wc -l          # 148
grep -rlE '^func \(q \*queries\) [A-Za-z0-9_]+\(ctx context\.Context.*\) \(\[\].*, error\) \{$' \
  --include='*.go' test/data/codegen/valid | wc -l          #  92
```

with the matching `querier.go` interface entry counted the same way. That gives
**148 signatures on each side, in 92 files each: 184 signature-bearing golden
files and 296 signature lines.** The implementation files split 37
apache-age-pgx-v5 / 36 neo4j-go-v5 / 19 neo4j-go-v6, and the `querier.go` files
split identically, which cross-checks against the 51 `:many` queries: each is
emitted once per backend it has a golden for, and the v6 corpus is the thinnest.
By basename the 92 are 90 `queries.cypher.go` and 2 `directory.cypher.go`.
`Tx` adds nothing — it carries no `([]T, error)` method of its own in any
golden, so the forwarders are not a third site. §9 records the discrepancy.

The two figures bound different things and the execution needs both: **184** is
how many files a reviewer opens, **296** is how many lines change. Neither is a
count of *edits* — a regenerated golden is rewritten wholesale, so the work is
one emitter change per backend plus one `just test-codegen` regeneration, and
these numbers measure the diff a reviewer has to read, not the labour.

Worse than the count: on neo4j the `:many` body decodes a
**materialised** `[]*neo4j.Record` handed back across `driverOrTx.run`
(`internal/codegen/neo4j/render_db.go:58-88`), whose own doc comment calls those
"self-contained value snapshots safe to consume after the transaction closes",
and whose `driverDB.run` has already run `defer session.Close(ctx)` by the time
the body sees a row. `.All()` keeping today's managed retry and `.Range()`
streaming therefore means **two emitted bodies per `:many` query**, not one
rewritten body. AGE is closer — its emitted body already loops `rows.Next()`
with `defer rows.Close()` — which is itself worth recording: the two backends
are at different distances from streaming, and the neo4j one is not a local
edit.

None of that decides the ruling. §3.1 does.

## 4. `:iter` is built on `ExecuteRead` with D8's corrected sentinel, not on `BeginTransaction`

D8's reconciliation with `codegen-tx-object.md` §3 F1/F3 left gqlc-1a5 one
further option: build `:iter` on an explicit `BeginTransaction`, deleting the
retry envelope and the `gqlc-nx54` re-entry hazard structurally rather than by
sentinel.

**Declined**, on that spec's own accounting of the cost. F4 (`:83-91`) states it:
retry lives in `runRetriable` and is entered only from
`ExecuteRead`/`ExecuteWrite`, so `BeginTransaction` "never reaches it" and a
transient cluster error surfaces "where the managed path would have retried".
That cost is accepted there for a `Tx` object, whose whole point is that the user
drives the transaction. It is not acceptable here: `:iter` would be strictly
flakier than `:many` on the same query against the same cluster, in the
connection-setup, routing-refresh and leader-election class that fires *before*
any row is delivered. A cardinality whose advertised difference from `:many` is
delivery shape must not also silently drop `:many`'s resilience.

The corrected sketch already in D8 pays for that and satisfies
`codegen-tx-object.md` §3 F4's principle ("an object with `Commit` cannot retry,
because retrying means re-running user code that already observed results")
without an explicit transaction: its **exit rule** — once a row has reached the
consumer, every exit from the unit of work returns `errIterStreamStarted`, so the
driver never reaches `TxCommit` and the re-entry window does not exist. Verified
this session at the pinned versions that the primitive the rule is built over is
real and is the same code on both majors: `Records(ctx) func(yield func(*Record,
error) bool)` at v5.28.4 `neo4j/result_with_context.go:50,154` and v6.2.0
`neo4j/result.go:50,160`, with `Collect` implemented **on top of** it
(v5 `:178-200`, calling `Records` at `:187`; v6 at `:193`). Streaming is the
driver primitive; today's materialisation is the wrapper.

**This section is about neo4j only.** pgx has no managed retry, so on AGE there
is no envelope to re-enter and no sentinel to emit: the `:iter` body is the
`rows.Next()` loop the `:many` body already writes, with the append replaced by a
yield and `defer rows.Close()` kept. The execution must not carry
`errIterStreamStarted` across to the AGE emitter for symmetry's sake.

The falsifier, named because it has never been run: gqlc-nx54's live row — force
a `TxCommit` failure after at least one streamed row and assert an error return
with no panic. If the sentinel does not in fact suppress re-entry against a real
server, `BeginTransaction` is the fallback and the swap is contained to one
emitted body on the neo4j side.

## 5. The runway, and the three places it does not reach

D8 calls its grill markers "the paved runway". They hold, and the enum half is
real: `internal/queryfile/annotated.go:14-20` documents `:iter` as reserved by
name, and the members start at `iota + 1` so a fourth constant churns no wire
format. Three gaps the execution meets that the bead does not record — the third
of which has been paved since this ruling was drafted, and is kept here because
§6 orders the work around it.

- **`exhaustive` covers a quarter of the sites.** Adding `CardinalityIter` reds
  the four `switch` statements over the enum (`internal/codegen/prepare.go:721`,
  `neo4j/render_queries.go:374`, `age/render_queries.go:276`,
  `queryfile/annotated.go:38`). None of the four uses a `default`, which is the
  whole reason all four stay visible under `default-signifies-exhaustive: true`
  — `annotated.go:46-49` says so in a comment, citing bd gqlc-51l6m. The two
  stringers put a fallback below the switch; the two emitter switches have
  **no** fallback, so an unhandled cardinality there emits a method with an
  empty body rather than failing. That is caught by `exhaustive` and by nothing
  else, so the execution adds the `:iter` arm at both rather than leaning on a
  runtime error that does not exist. It cannot see the `if`-comparison sites: over
  `internal/`, excluding `_test.go`, `Cardinality ==` / `!=` matches **thirteen**
  lines holding sixteen comparisons against a named member, plus a fourteenth
  (`prepare.go:419`) testing `== 0`. Four are the dangerous ones —
  `neo4j/render_queries.go:279,294` and `age/render_queries.go:172,203` — where
  an `:iter` query failing `== CardinalityMany` falls silently into the `:one`
  shape. The execution converts those to switches or adds an explicit `:iter`
  arm at each; it does not rely on the linter to find them. The two
  `== CardinalityOne` sites in `neo4j/generate.go:60` and `age/generate.go:125`
  read the other way and need no edit: they set `hasOne` to gate `renderDB`'s
  single-row helper, and an `:iter` query is correctly not `:one`. That is worth
  stating because the grep does not distinguish them, and an execution lane
  sweeping every comparison would otherwise "fix" a site that is already right.
- **`querierImports` is a string scan with no named guard.** `neo4j/render_querier.go:77-101`
  tests `strings.Contains(ty, "dbtype.")` / `"time.Time"` over `GoType` text;
  age's equivalent is `slices.ContainsFunc(..., namesInstant)`
  (`age/render_querier.go:22`, `age/render_queries.go:19-22`). Neither has an
  arm for a new import, and `grep -rln 'querierImports\|namesInstant'` over
  `internal/codegen/` returns three non-test files — both `render_querier.go`
  and `age/render_queries.go`, which declares `namesInstant` and uses it a second
  time at `:107` — and **no `_test.go` at all**, so the miss surfaces as a
  compile failure under `codegen-fence` and nothing names it. A guard is owed;
  §6 says which.
- **`emitscan`'s closure gap was real, is now fixed, and what replaced it still
  bounds the execution.** As this ruling was drafted, `DeclaredIdents` switched
  on `*ast.AssignStmt` (DEFINE), `*ast.ValueSpec` and `*ast.RangeStmt` (DEFINE)
  only, so a generated `func(yield func(Row, error) bool)` bound `yield`
  invisibly and `FreeIdents` reported it **free** — the direction its own doc
  comment denied. That was filed as **gqlc-9hrh** and has since landed on master
  (PR #2839, `21720c45`): `DeclaredIdents`
  (`internal/codegen/emitscan/emitscan.go:467-470`) now has a `*ast.FuncLit` arm
  reading the literal's parameters and results, `BodyLocals` (`:506`) inherits it
  by delegation, and `TestAClosureParameterIsNotFree` /
  `TestAClosureParameterIsABodyLocal` (`emitscan_test.go:469,488`) pin both. So
  step 1 of §6 is **already done**; the execution does not re-do it, and the
  `capture_test.go` sweeps in both emitters measure a closure-bodied emission
  correctly on arrival.
  What survives is narrower and is **not** a blocker for `:iter`. `FreeIdents`'
  doc comment now enumerates five constructs it still reports free — a local
  type declaration and its field names, a statement label, a parameter name in a
  func *type*, and a type parameter — filed as **gqlc-db0e**. None is a shape the
  `:iter` emission as specified here produces: the body binds `yield` (a closure
  parameter, now bound) and ordinary short declarations, and the `func(Row,
  error) bool` inside `iter.Seq2` has unnamed parameters. **Don't name them.**
  Writing `func(yield func(row Row, err error) bool)` for readability would put
  those names into gqlc-db0e's fourth construct — a parameter name in a func
  *type* — where `FreeIdents` reports them free. The consequence is a spurious
  capture report rather than a missed one, since `Scope` then intersects the free
  set with package-level declarations: harmless for `row`, a red sweep the day
  someone writes `func(person Person, err error) bool` over a model type the
  emitter also declares. It costs nothing to leave them unnamed. The execution
  should read that doc comment before step 4, and must not read the gqlc-9hrh fix
  as having made the analysis universally conservative.
  What was never at risk, contrary to the bead's framing: `Candidates`
  (`:270-285`) is `ast.Inspect` over every `*ast.Ident`, so a new `yield` enters
  the swept candidate set automatically, and `Scope` (`:296-323`) intersects free
  idents with package-level declarations, which `yield` is not.

## 6. What the execution owes — files, order, guards

In this order, because each step's guard is the next step's screen.

1. ~~**`internal/codegen/emitscan`** — add a `*ast.FuncLit` arm to
   `DeclaredIdents` so a closure parameter is bound.~~ **Done: gqlc-9hrh landed
   as PR #2839**, which is why this step is kept struck through rather than
   deleted — an execution lane reading §5 must not re-derive it. The capture
   sweeps in `neo4j/capture_test.go` and `age/capture_test.go` will measure a
   closure-bodied emission correctly on arrival. Read `FreeIdents`' doc comment
   before step 4: it enumerates five constructs still reported free (gqlc-db0e),
   none of which the `:iter` body emits.
2. **`internal/queryfile`** — `CardinalityIter`, a `parseCardinality` arm
   (`parse.go:169-177`), a `String()` arm (`annotated.go:37-49`, below the
   switch as the existing comment requires), and a `MarshalJSON` wire tag
   (`cardinalityjson_test.go:18`).
3. **`internal/codegen/prepare.go`** — `ErrIterOnWrite` in `errors.go` beside
   `ErrCardinalityShapeMismatch` and in the sentinel list at `:300`; the
   validation arm at `:778-786` gains `:iter` + Write → `ErrIterOnWrite` and
   `:iter` + zero columns → `ErrCardinalityShapeMismatch`. Then sweep the
   `== Cardinality*` comparisons of §5 — thirteen lines, of which four need an
   `:iter` arm and two are correct as they stand.
4. **Emitters, neo4j first** (`render_queries.go` `writeMethodSignature:236`,
   `returnTypeText:263`, the body writer, and `render_db.go`'s `driverOrTx` seam
   (`:58`), which must grow a streaming method because today's returns
   `[]*neo4j.Record`), then AGE (`render_queries.go:147,170`), whose body
   already loops `rows.Next()`. v5 and v6 share the neo4j emitter — two backends
   to change, not three.
5. **`querierImports`** — an `iter.Seq2` arm in both backends, plus the guard
   §5 says is missing: a table test asserting that for each `(signature text,
   expected import set)` pair the querier import block matches. That is the
   guard, and it is the one artefact here whose absence is invisible until a
   golden fails to compile.
6. **Fixtures**, per D8's test plan: `valid/iter_read_scalar`,
   `iter_read_entity`, `iter_read_multicolumn`, `iter_with_skip_limit`;
   `invalid/iter_on_write`, `invalid/iter_zero_column_read`. The nested golden
   module is already `go 1.26.6`, above `iter`'s 1.23 floor.
   [2026-09-11 (gqlc-c7uf): five of those six landed with gqlc-1a5 (PR #2874).
   `invalid/iter_zero_column_read` did not, and no fixture of that name exists.
   Four spellings were measured against that day's conformance harness and none
   reached the zero-column branch: a read reaches the column list through
   `RETURN`, which projects at least one column, and the one other zero-column
   read shape — a `CALL` with no `YIELD` — dies at `ErrUnknownProcedure`,
   because the harness builds `procsig.NewRegistry(nil)`. Four candidates on one
   dated harness is not a demonstration that no text reaches the branch; read it
   as no fixture found yet. The witness is assembled instead, and lives at
   `TestAssembledInput/cardinality-iter-zero-column-read` in
   `internal/codegen/conformance/assembled_input_test.go` — moved there from the
   `codegen_test` package by gqlc-e3ra (PR #2890), because a witness in that
   package is one the coverage reachability fence cannot read.
   `codegen-sentinel-taxonomy.md` §2's `ErrCardinalityShapeMismatch` row carries
   the live account — the four spellings by name, and the corpus change that
   would make a fixture the better witness — and this line is not maintained
   against it. The name stays here rather than being deleted because a plan that
   records what it could not build is worth more than one that reads as though
   it never tried.]
7. **Live arms** (`test/data/codegen/live_test.go`), the two behaviours with
   zero coverage today: a mid-stream error on row *k*, and abandoning the range
   without draining. Plus gqlc-nx54's owed row — a forced `TxCommit` failure
   after a streamed row, asserting an error and no panic (§4). The existing
   `:1002` empty-not-nil row is untouched by this ruling, because `:many` does
   not move.

**Emit `iter.Seq2[Row, error]`, not the driver's structural
`func(yield func(Row, error) bool)`.** The driver spells it structurally to
avoid imposing a Go floor on its own consumers; gqlc has no such constraint,
since `range` over a function value needs 1.23 either way, and ADR 0010's
Consequences say the generated surface is the product — a named type reads in
godoc and a structure does not. If a sub-1.23 consumer ever appears, the swap is
one string in `returnTypeText` and it deletes the §5 import arm.

**Mutation rows**, victim declared before each run, per decision 0005:

| # | mutation | expected victim |
|---|---|---|
| 1 | drop the `:iter` + Write arm | `invalid/iter_on_write` stops refusing |
| 2 | drop the `:iter` + zero-column arm | `invalid/iter_zero_column_read` [2026-09-11 (gqlc-c7uf): no fixture of that name was built — step 6's note has the measurement. This row as written names a victim that cannot have run, so read it as not yet run rather than as run and KILLED. The victim available today is `TestAssembledInput/cardinality-iter-zero-column-read`.] |
| 3 | drop the `iter` arm from `querierImports` | the new import guard test, **and** `codegen-fence` — if only the fence reds, the guard is not a guard |
| 4 | delete the `stopped` test inside `emit` | the live re-entry row of §4; if it is not runnable, the row is vacuous and must be reported as such rather than dropped |
| 5 | flip one `== CardinalityMany` to `!= CardinalityExec` | an `:iter` golden takes the `:many` body |

Five rows, not six. A sixth — drop the `*ast.FuncLit` arm from `DeclaredIdents` —
was owed while step 1 was outstanding; gqlc-9hrh ran it and three others before
merging, so re-running it here would measure that bead's guard, not this one's.

Screen every row with `go test -c -o /dev/null ./internal/codegen/...
./internal/queryfile/` before trusting a RED, and regenerate goldens before each
row: a golden that absorbs the mutant goes green, and that SURVIVED is the
finding.

**The ADR the execution owes.** A new annotation token, a new sentinel, and a
generated method shape that is not `(T, error)` are all user-visible. It must
state: the `:iter` opt-in and why it is not automatic; `ErrIterOnWrite` with its
message text; that `:iter` on AGE holds a **pooled connection for the whole
consumer loop** (`pgxpool.Pool.Query` acquires and holds until `Rows.Close()`),
which no `:many` call does today and which belongs in the emitted method's own
doc comment; and that a `:iter` transaction which delivered a row is never
committed (§4, D8's exit rule). Next free ordinal via `just adr-next` **at push
time**, not when the branch is cut.

## 7. What this ruling leaves unsolved, and what would falsify it

- **It would be falsified by a linter that polices a yielded error.** §1 and
  §3.3 both rest on EXIT=0. If `errcheck`, `staticcheck` or a `revive` rule
  gains a check for a one-variable range over a `Seq2` whose second parameter is
  `error` — or if this project writes one — then the asymmetry disappears, the
  phantom-row hazard becomes catchable, and the universal options deserve
  re-argument on cost alone. §3.1 would still stand, and is the half that does
  not depend on any linter.
- **It would be weakened by a real `:many`-on-write deprecation.** §3.1 is an
  argument about `MarkAdults`-shaped queries. If projecting writes were moved to
  their own cardinality, `:many` would become read-only and `Result[T]` would
  lose its decisive objection. Nothing here proposes that, and two corpus
  fixtures depend on the current shape.
- **The AGE pool-hold has no measured ceiling.** The bead refutes the fatal
  version of the AGE risk (server-side materialisation) with a container probe,
  and this ruling accepts that refutation without re-running it — docker is
  disabled on this host. What nobody has measured is how a slow consumer loop
  interacts with pool exhaustion under concurrency. It is an obligation on the
  live arm, not a blocker, because `:iter` is opt-in.
- **`:iter` interaction with the `Tx` object** is not opened here.
  `codegen-tx-object.md` §3 F4 is cited for its principle; whether
  `WithTx(...).SomeIter(ctx)` is admitted, refused, or admitted with the
  retry-free semantics of §4's rejected option is a question this ruling does not
  answer and the execution must not answer by accident.
- **Pagination stays where D8 put it.** `SKIP`/`LIMIT` are orthogonal
  parameters. A `:page` cardinality was evaluated in gqlc-1a5's notes and is dead
  on ADR 0005 (generated code runs the author's text verbatim; query
  reconstruction is the named rejected option) — not re-litigated here.
- **A generation-time warning on an unbounded `:many` stays dead.** Per ADR 0015
  a key label set identifies the type, not the instance, so gqlc cannot prove
  `MATCH (p:Person {id: $id}) RETURN p` is bounded; the warning would fire on 51
  of 51. ADR 0032's own criterion — a detector that fires on ordinary correct
  queries buries the signal it exists for — refuses it.

**Bead disposition.** `gqlc-1a5` **stays open** and carries the build. Its title
and description already describe exactly the implementation §6 scopes, so
closing it and minting a same-titled execution twin would leave GH #218 closed
and unbuilt. So this document's own PR carries `Refs: gqlc-1a5` and no `Closes`.

What was spun out is §5's third bullet — the `emitscan` `*ast.FuncLit` gap,
**gqlc-9hrh**, because that defect was in the tree independent of `:iter`
shipping. It is now **closed and merged** (PR #2839), and its own investigation
spun out **gqlc-db0e** for five further constructs `FreeIdents` still reports
free. gqlc-db0e is **not** a prerequisite for the `:iter` build — none of the
five is a construct the `:iter` emission produces (§5) — and this ruling does not
schedule it.

## 8. Rejected alternatives

- **Universal `:many` → `iter.Seq2`.** §2 — it deletes the EXIT=1 arm of §1
  from every read query and replaces it with nothing, and §3.1's write objection
  applies to it unchanged.
- **`Result[T]` lazy thunk.** §3 — three reasons, of which §3.1 is sufficient
  alone. The golden churn of §3.4 is real and is deliberately not the reason.
- **`Result[T]` restricted to read `:many`,** which would dodge §3.1. Rejected:
  it makes the return type of a `:many` method depend on whether the statement
  writes, so adding a `SET` to a working query changes its Go signature with no
  annotation edit — a silent break of the kind ADR 0010 §D1 exists to make loud.
  §3.2 and §3.3 also survive it.
- **Auto-generated `XIter` sibling for every `:many`.** Already rejected by D8's
  grill (doubles the Querier surface, taxes mocking and docs). Nothing measured
  since reopens it, and §3.1 now applies to it as well: an auto-generated
  `MarkAdultsIter` is the refused write stream, minted without an author asking.
- **Build `:iter` on `BeginTransaction`.** §4 — it buys structural deletion of
  the re-entry hazard and pays with every retry `:many` has, including the
  pre-first-yield window that covers the flake class managed retry exists for.
  Named as the fallback if §4's falsifier fires.
- **A `pgx.ForEachRow`-style `func(ctx, yield func(Row) error) error`,** which
  *would* be errcheck-policed and so would answer §1 directly. Removed from the
  option set by the no-closures ruling (`codegen-tx-object.md` §1): it hands the
  caller a callback, where `iter.Seq2` does not — the compiler synthesises
  `yield` at the consumer's `for ... range`. Recorded because the shape is
  shipped prior art, verified this session at the deployed version:
  `github.com/jackc/pgx/v5@v5.10.0/rows.go:401-421`.
- **Do nothing and defer again.** Declined. Two sessions banked the research and
  both recorded that no option was chosen; a third deferral would add no
  measurement. The owner has supplied both inputs that were missing — the demand
  signal and permission to break the API — and what remained was a choice.

## 9. Where gqlc-1a5's notes disagree with this tree

Called out rather than silently corrected, because the bead is the ledger a later
reader will reach for.

- **`live_test.go:961` is now `:1002`.** Same assertion, same message; the file
  has grown. The line number in the notes is stale, not wrong about the contract.
- **"7 docs quote the `:many` shape verbatim" reads as 6.** `codegen-stage-c1`,
  `c3`, `c4`, `c5`, `docs/adr/0010` (`:226`) and `resolver-stage-r3` (`:780`)
  quote a generated `:many` method. `codegen-stage-c0` is a false positive — its
  `([]File, error)` matches are gqlc's own internal `Generate` API, not an
  emitted method. Moot under this ruling, which moves none of them.
- **The corpus counts hold, with one grep trap worth recording.** A raw
  `grep -c ':many'` over `test/data/codegen/valid/**/*.cypher` returns **53**,
  and 2 of those are prose inside comments
  (`nested_list_property/queries.cypher:16`, `record_property/queries.cypher:30`).
  Counting `// name: X :many` gives **51**, across **34** of **97** valid dirs —
  the bead's corrected figures exactly — but only when the glob reaches every
  `*.cypher`, not `valid/*/queries.cypher`. `multi_source_files` holds
  `directory.cypher` and `people.cypher`, and the shallow glob reads 50 across
  33 dirs. Both figures are of the valid corpus alone; a repo-wide file count
  also lands on 51 (51 files under `test/data/codegen` contain a `:many`,
  17 of them in `invalid/`), and the coincidence is worth naming because it
  makes a wrong count look confirmed.
- **The "158 goldens" figure does not reproduce; it is 184.** The re-measured
  count, with its definition, is in §3.4: 148 emitted `:many` signatures on the
  implementation side and 148 matching `querier.go` interface entries, in 92
  files each. The spike's per-backend split (64/60/34) is below the measured one
  (74/72/38 counting both sides) by a consistent margin, so the spike was
  probably counting a narrower file set; I could not reconstruct which, and did
  not guess. The direction matters for the execution — the churn is **larger**
  than the bead promises, not smaller — and it is the reason §3.4 states the
  command rather than the number. It changes no part of the ruling, because
  §3.4's cost was already declared not to be the deciding argument.
- **The `emitscan` risk was narrower than "unsettled", and is now closed.**
  `Candidates` was safe by construction and `DeclaredIdents` was the one gap;
  it was fixed on master as gqlc-9hrh (PR #2839) while this ruling was in
  review, so §5's third bullet is the only claim in this document that the tree
  has overtaken. The replacement limit is gqlc-db0e, and it does not touch
  `:iter`.
- **The `querierImports` claim holds and is worse than stated.** Not only is
  there no named guard, there is no test file referencing either backend's import
  helper at all — the three files that name `querierImports` or `namesInstant`
  are all production renderers.
