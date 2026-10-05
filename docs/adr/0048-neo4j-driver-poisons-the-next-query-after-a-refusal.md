# The neo4j driver poisons the next query after a refusal, and gqlc does not work around it

Both neo4j driver majors gqlc targets have a defect (bead `gqlc-xeyks`). The
server can refuse a request at decode with `Neo.ClientError.Request.Invalid`,
for instance a `time.Time` sent with a zone id it does not know. When that
happens, the NEXT query on the same pooled connection fails client-side with
`invalid state 4, expected: [0]` (or `[1 0]` for an auto-commit run). The
query after that one succeeds. gqlc's generated code surfaces the failure on
whichever unrelated query is unlucky enough to borrow that connection.

This ADR records the measurement and decides not to work around it in emitted
code.

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
| new session, explicit transaction (what `txDB` runs inside) | `invalid state 4` | ok | ok |

`MaxConnectionPoolSize` 1 and 100 gave the same result: the pool hands the
poisoned connection out next either way. The emitted session handling neither
causes this nor makes it worse.

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

## Decision: no workaround in emitted code

A workaround would have to recognise the failure and retry. Both halves are
unsafe or brittle here:

- The error is an untyped `fmt.Errorf("invalid state %d, expected: %+v")`, so
  the only way to recognise it is matching the message text. A driver release
  that rewords it would silently disable the retry.
- A retry is safe only where nothing reached the server. That holds for the
  managed transaction `driverDB.run` opens. It does not hold inside a
  caller's explicit transaction (`txDB`): there the connection is the
  transaction's, and retrying one statement on a fresh connection would run it
  outside the transaction the caller opened.

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
one after it to succeed.

**What would falsify this ADR:** a driver release that fixes the defect. Those
rows then go red, and this ADR's caveat should be removed.
