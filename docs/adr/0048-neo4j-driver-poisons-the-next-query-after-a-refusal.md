# The neo4j driver poisons the next query after a refusal, and gqlc does not work around it

Both neo4j driver majors gqlc targets have a defect (bead `gqlc-xeyks`). The
server can refuse a request at decode with `Neo.ClientError.Request.Invalid`,
for instance a `time.Time` sent with a zone id it does not know. When that
happens, the NEXT query on the same pooled connection fails client-side with
`invalid state 4, expected: [0]` (or `[1 0]` for an auto-commit run). The
query after that one succeeds. gqlc's generated code surfaces the failure on
whichever unrelated query is unlucky enough to borrow that connection.

This ADR records the measurement, names the caller-side mitigation, and
decides not to work around it in emitted code.

## Measured

Measured on 2026-10-04 against the pinned image (`neo4j@sha256:362542416de6…`,
server 5.26.28) with neo4j-go v5.28.4 and v6.2.0. Each row provoked one
refusal and then ran three ordinary queries. Both majors gave the same result
for every row:

| session shape | first query after | second | third |
|---|---|---|---|
| new session per call, managed transaction (what `driverDB.run` emits) | `invalid state 4` | ok | ok |
| `ExecuteQuery` | `invalid state 4` | ok | ok |
| new session, auto-commit `session.Run` | `invalid state 4` | ok | ok |
| new session, explicit transaction (generated `Begin`, then `txDB`) | `invalid state 4` at `BeginTransaction` | ok | ok |

`MaxConnectionPoolSize` 1 and 100 gave the same result: the pool hands the
poisoned connection out next either way. The emitted session handling neither
causes this nor makes it worse.

The failure is only ever met when a connection is borrowed, because that is
when `assertState` first runs on it. A transaction borrows its connection
once, at `Begin`. So on a poisoned pool the generated `q.Begin` itself fails,
and the statements inside a transaction that did begin succeed (re-measured
2026-10-05 on both majors, pool sizes 1 and 100). The failure never surfaces
in the middle of a transaction. Wherever it does surface, it is the first
message on a borrowed connection, before anything reached the server.

## Cause, read off a bolt trace and the v5.28.4 source

1. The client pipelines RUN and PULL, and the server sends three FAILUREs
   back for those two messages: `Neo.DatabaseError.General.UnknownError`, the
   `Request.Invalid`, and `Message of type PullMessage cannot be handled by a
   session in the READY state`. Then it drops the connection; the driver's
   later RESET meets EOF.
2. The driver surfaces the `Request.Invalid` and returns the connection to the
   pool as alive. `isFatalError` (`bolt4.go`) treats only
   `Status.Security.AuthorizationExpired` as fatal.
3. On return, `bolt5.Reset` calls `ForceReset`. That function clears `b.err`,
   then `receiveAll` reads the stray third FAILURE, which sets `b.err` and the
   state `bolt5Failed` (4). `ForceReset` then returns early, without sending
   RESET. `Reset`'s deferred cleanup clears `b.err` again, so the connection
   sits in the pool in state 4 with no error recorded.
4. The next borrower's `assertState` refuses state 4 with an untyped
   `fmt.Errorf`. Only then does the driver send RESET, meet EOF, and discard
   the connection.

In v6.2.0, `bolt5.Reset`, `ForceReset` and `assertState` are byte-identical
to v5.28.4's, and so is `isFatalError` (diffed 2026-10-04). The bolt trace
was taken on v5 only; the v6 row in the table above is the behavioural match.

## Mitigation: a liveness check on every borrow

A caller can avoid the defect by setting
`ConnectionLivenessCheckTimeout = 0` in the driver config (it has the same
name in v5 and v6). The pool then runs its health check, a `ForceReset`, on
every borrow. On the poisoned connection that RESET meets EOF, so the
connection is marked dead and discarded, a fresh one is dialled, and the
query succeeds.

Measured on 2026-10-05 on both majors, pool sizes 1 and 100, through the
generated query and the generated `Begin`:

| `ConnectionLivenessCheckTimeout` | next query after a refusal | the one after |
|---|---|---|
| default | `invalid state 4` | ok |
| 1h | `invalid state 4` | ok |
| 0 | ok | ok |

The cost is one RESET round trip on every borrow. That is the caller's trade
to make, and gqlc cannot make it for them: the generated `New(driver)` takes
the caller's driver, and the driver's configuration is the caller's.

## Decision: no workaround in emitted code

A workaround in emitted code would have to recognise the failure and retry.
A retry would be safe at every point the failure surfaces, since nothing has
reached the server yet (above). Recognising it is the problem: the error is an
untyped `fmt.Errorf("invalid state %d, expected: %+v")`, so the only test is
matching the message text, and a driver release that rewords it would
silently disable the retry. The supported remedy is also already in the
caller's hands, as the driver setting above, which gqlc does not own.

The trigger is also narrow. Since bead `gqlc-m3ax`, gqlc's own TIMESTAMP binds
no longer send a zone id the server refuses, apart from names its census found
refused and unseeded (`posixrules`, `posix/*`, `right/*`), which fail loudly
anyway. Bead `gqlc-nvb4` covers `time.Time` inside ANY-carried parameters. So
the defect reaches a gqlc user only after some other request the server
refuses at decode.

The fix belongs in the driver. An upstream report is drafted in the bead's
notes and has not been filed.

## Gate

The rows in `test/data/codegen/live_neo4j_refusal_poisons_next_query_test.go`
pin the defect on both majors. They provoke a refusal through a raw session,
then require the next generated query to fail with `invalid state 4` and the
one after it to succeed. An allow-pin beside them opens the same arm with
`ConnectionLivenessCheckTimeout = 0` and requires the next generated query to
succeed, so the mitigation this ADR names is held by a test, not only
described.

**What would falsify this ADR:** a driver release that fixes the defect. Those
rows then go red, and this ADR's caveat should be removed. If the allow-pin
goes red, the mitigation has stopped working, and this ADR must stop
recommending it.
