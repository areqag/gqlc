# `:iter` is an opt-in, read-only streaming cardinality

A query annotated `:iter` generates a method returning `iter.Seq2[Row, error]`
instead of `([]Row, error)`. The rows reach the caller as the server produces
them, so the whole result set is never in the process at once:

```go
// name: StreamPersonNames :iter
// MATCH (p:Person) RETURN p.name
func (q *queries) StreamPersonNames(ctx context.Context) iter.Seq2[string, error]
```

Everything else about the query is unchanged — the same text, the same
parameters, the same decoding — so `:many` and `:iter` over one query differ in
the annotation and in nothing else. All three targets emit the same signature;
`neo4j-go-v5`, `neo4j-go-v6` and `apache-age-pgx-v5` are byte-identical on it,
doc comment included.

**The named type is emitted, not the structural `func(yield func(Row, error)
bool)` the neo4j driver spells.** The driver writes it structurally to avoid
imposing a Go floor on its own consumers; gqlc has no such constraint, because
`range` over a function value needs Go 1.23 either way. A named type reads in
godoc and a structure does not.

Written 2026-09-11, executing spec
[`ruling-1a5-iter-streaming-cardinality.md`](../specs/ruling-1a5-iter-streaming-cardinality.md)
on bead `gqlc-1a5`. The goldens are under `test/data/codegen/valid/iter_*`.

## The opt-in is the decision, and it is not a migration step

`:many` does not move, and no query becomes a sequence because it looks
unbounded. Making every `:many` an `iter.Seq2` was evaluated and declined, on
one fact about Go's tooling rather than on cost:

| the caller writes | `errcheck` |
|---|---|
| `rows, _ := q.PeopleByAgeAndLocale(ctx, arg)` | reports it — EXIT=1 |
| `for row := range q.StreamPeopleByAge(ctx, arg)` | silent — EXIT=0 |

A one-variable `range` over a `Seq2` whose second type parameter is `error` is
not a discarded return value to any linter in this project's config, so the
error is not dropped visibly — it is not spelled at all. A slice method that
fails hands back a nil slice AND an error the tooling insists on; a sequence
that fails mid-stream hands back the rows it managed and an error nothing makes
the caller name. Turning every existing `:many` into that would convert a
compile-time-ish obligation into a silent one across every caller in the fleet,
without any of them having asked.

So the cost is paid by the query author who wrote `:iter`, who by writing it
said they will read the second variable. That is also why the rows-then-error
ordering below is a contract worth stating rather than an implementation
detail: it is all the caller gets.

This would be worth re-arguing if `errcheck`, `staticcheck` or a `revive` rule
gained a check for a one-variable range over a `Seq2[_, error]`. As of
2026-09-11 none has one.

## `:iter` on a write is refused

```
ErrIterOnWrite = errors.New("iter cardinality on write query")
```

and the failure the caller sees names the query and its position:

```
iter cardinality on write query: query "StreamCreatedPeople" at position 0 has
cardinality :iter but the query writes — a streamed write yields rows the
transaction may still roll back; annotate :many
```

The refusal is at generation time, in `internal/codegen/prepare.go`, so a
`:iter` write never reaches an emitter. It is in the sentinel taxonomy
(`docs/specs/codegen-sentinel-taxonomy.md` §1) and reachable from user input by
`test/data/codegen/invalid/iter_on_write`.

Why refuse rather than serve it: `:many` holds *if you see it, it's committed*.
A streamed write cannot. The rows go to the caller while the transaction is
still open, so a mid-stream rollback leaves the caller holding rows that no
longer exist, and a pre-first-yield retry re-runs the write, so an elementId the
caller stashed from the first pass no longer matches anything. Neither is a
failure the caller can be told about after the fact — they already have the
rows. The remedy is one character: `:many` materialises and keeps the managed
retry envelope, or the write comes out of the query.

This is a read-only cardinality by ruling, not by present limitation. It is not
waiting on an implementation.

## A `:iter` transaction that delivered a row is never committed

On neo4j the streaming seam runs inside `ExecuteRead`, which is the driver's
managed-retry envelope: when the unit of work fails retriably — **including on a
commit failure, where the retry state reports "not done"** — the driver calls
the work function again. A second pass would call `yield` on a range loop the
consumer may already have broken out of, and the Go runtime answers that with a
panic:

```
range function continued iteration after function for loop body returned false
```

The answer is an exit rule, and it is absolute: **once a row has reached the
consumer, every exit out of the unit of work returns `errIterStreamStarted`.**
Not an error the caller sees — the sentinel is filtered at the bottom of the
seam — and not a retriable driver error type, so the managed envelope ends
rather than restarting. The consequence is in this section's title: the driver
never reaches TxCommit for a stream that delivered anything.

That is sound precisely because of the section above. A read transaction that is
abandoned rather than committed has nothing to lose, and `:iter` cannot carry a
write.

Retry is therefore **pre-first-yield only**. An error arriving before any record
has been delivered is returned to the envelope, which may retry the whole unit
of work; an error arriving after one has been delivered is yielded to the
consumer, because the consumer has already seen rows a retry would re-produce.
Either way the consumer sees the error exactly once.

`WithTx(...)` is the other path and it has no envelope, so it has no re-entry to
guard: the `txDB` seam streams the caller's transaction directly. Whether a
`:iter` method on a transaction-bound handle should be admitted at all is a
question the ruling leaves open; today it compiles and runs, and nothing here
decides it.

**The managed seam has a second fence beside the exit rule, and it is a
different claim.** The exit rule is about re-entry; this one is about the seam
having two places it can deliver an error from — inside the record loop, and
below the envelope once `ExecuteRead` has returned. Those are independent, and
the driver reports a cancellation in its own words as well as failing the read,
so a cancelled stream reached both. The consequence is not cosmetic: a consumer
that does the obvious thing on an error —

```go
for row, err := range q.StreamPeopleByAge(ctx, arg) {
    if err != nil { return err }
    ...
}
```

— has returned `false` from `yield`, and the second delivery is a yield after a
false, which the Go runtime answers with a panic in the caller's own goroutine.
So the seam carries a `stopped` flag, set the moment the consumer can take no
further item, and the yield below the envelope is fenced on it.

This was found by the live arm and not by review, on the first run of
`iterFailureAfterDelivery` against a real server: rows=1, errs=2.

## The consumer's contract

Stated as three clauses because each is separately falsifiable, and all three
are witnessed live rather than in a golden
(`test/data/codegen/live_test.go`, `iterScenarios`):

- **Every row that decoded before a failure reaches the consumer, and no row
  after it.** A decode failure ends the sequence; it does not skip the row and
  carry on. The alternative would hand a caller a short result set that looks
  complete — the silent wrong answer `:many` structurally cannot produce,
  because it returns an error instead of a slice.
- **An error item is always the sequence's last**, and arrives exactly once. So
  `if err != nil { return err }` inside the range is a correct consumer, and
  the section above is what makes it one.
- **The consumer may stop early.** `break` produces a `false` from `yield`, and
  the emitted body releases what it holds at that point.

One thing the contract does not promise: that cancelling the context stops the
sequence. It stops it on neo4j, where each record is pulled off the wire as the
consumer ranges. On Apache AGE a small result is already in the pgx
connection's receive buffer before the first row is handed out, so `rows.Next()`
walks memory and never consults the context — measured 2026-09-11 on a
three-row read, which ran to its natural end after a cancel that neo4j reported
at row two. A caller who needs a stream to stop should `break`.

## `:iter` on Apache AGE holds a pooled connection for the whole consumer loop

`pgxpool.Pool.Query` acquires a connection and holds it until `Rows.Close()`,
and the emitted `:iter` body defers that Close to the end of the sequence. So
the connection is out of the pool for as long as the consumer keeps ranging —
not for the duration of the query, which is what every `:many` method on this
backend costs today.

The failure mode is a deadlock rather than a slowdown, and it is reachable from
ordinary-looking code: a consumer that does per-row work which itself needs a
connection, under a bounded pool, can consume the pool with suspended sequences
and then block forever waiting for the connection that would let one of them
finish. neo4j is not exempt in kind — it holds a session and its transaction for
exactly as long — but the pooled-connection form is the one with a small hard
limit in front of it.

**This is disclosed in the emitted method's own doc comment**, byte-identical
across all three targets, because the cost is paid by a caller reading the
method through editor hover and not by a reader of this file:

```
// The returned sequence holds a database connection for as long as the
// consumer keeps ranging, and releases it when the range ends — by
// exhaustion, by break, or by an error item, which is always the sequence's
// last. Ranging over it while doing per-row work that itself needs a
// connection can deadlock against a bounded pool.
```

The wording names no driver mechanism on purpose. Saying "pooled connection"
would be true of AGE and false of neo4j, and the surface fence
(`TestBackendInvariantSurface`) reads doc comments as caller-visible surface, so
a per-backend paragraph is not available even if it were wanted.

The release on early exit is what the live battery measures, and measures by
exhaustion rather than by inspection: `iterAbandonedRange` walks away from 24
sequences — more than any pool the battery opens — and then asks for one more.
A single abandoned range proves nothing, because a leaked connection is still
one of several the pool holds.

**What is still unmeasured** is the ceiling: how a slow consumer loop interacts
with pool exhaustion under real concurrency. The live arm establishes that an
abandoned range releases; it does not establish a safe consumer shape under
load. That is stated here rather than in a bead because a caller choosing
`:iter` on AGE needs it.
