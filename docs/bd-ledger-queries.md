# Querying the bd ledger

`bd` is an external tool (`/usr/bin/bd`); its source is not in this repository
and none of this can be fixed by patching it. What follows is the contract as
measured against the deployed binary, and the rules that make a query in this
repository truthful.

All figures below were taken first-party against the live ledger on
2026-08-23 (bd on `PATH`, 775 beads total). Treat them as readings, not as
states — the board moves.

## Two independent defaults, and `-n 0` disables only one

`bd list` applies a **status filter** and a **row cap**, and they are separate.

    bd list -n 0 --json       | jq length   -> 261   statuses: open, in_progress, blocked
    bd list --all -n 0 --json | jq length   -> 775   statuses: open, in_progress, blocked, closed

`-n 0` (equivalently `--limit 0`) is the flag people reach for when they mean
"everything". It reads as the unfilter. It is not: it lifts the row cap and
leaves the status filter in place, so 514 closed beads were absent from the
first query with nothing on stdout or stderr to say so.

**When you mean everything, write `bd list --all --json -n 0`** (or
`--status all --limit 0 --json`, which is the same set).

### Why this matters more than a hidden row

Absent and closed call for **opposite repairs**. A review bead that is absent
must be filed; a review bead that is closed must be reopened and its GitHub
mirror fixed. A default that flips a diagnosis is worse than one that merely
hides rows.

First-party cost, 2026-08-22: an audit of all 22 open PRs for missing review
beads was run with `-n 0`, believing that to be the whole ledger. PR #1122 came
back as having never had a review bead. It had had two, and both had been
closed. The author was mailed and asked to file a bead that already existed.

## `--status open` means the literal status, not "not closed"

`open`, `in_progress` and `blocked` are three statuses. `--status open` selects
one of them. An audit that means "work that is not finished" and writes
`--status open` is silent about every bead someone has already claimed.

Measured 2026-08-23: four beads carried a fixture owner address; three were
`open` and one (`gqlc-o13d`, a P0) was `in_progress`. A sweep filtered on
`--status open` named three of the four and said nothing about the fourth.

## The row cap is disclosed — on stderr, which scripts discard

The caps are 50 for `bd list` and 100 for `bd ready`. The deployed bd **does**
disclose them under `--json`, but it writes the notice to stderr:

    $ bd list --json 2>&1 >/dev/null
    Showing 50 issues; more results matched but were hidden by --limit. ...
    $ bd ready --json 2>&1 >/dev/null
    Showing 100 of 248 ready issues. ...

Every scripted call site in this repository redirects `2>/dev/null` — necessary,
because bd also writes ordinary chatter there — so none of them can ever see it.
Pass `--limit 0` explicitly at the call site. Do not rely on the notice, and do
not treat a silent stderr as evidence that nothing was capped.

The status filter has no such notice at any verbosity. Verified: `bd list --json
-n 0 2>&1 >/dev/null` prints nothing while omitting 514 rows.

## Rules for a scripted query

1. Pass the row cap explicitly, always: `-n 0` / `--limit 0`.
2. Pass the status set explicitly, always. `--all` when you mean every bead,
   `--status <s>` when you mean one. Never rely on the default.
3. Say in a comment which population the query is *for*. The defect above is
   never a wrong flag in isolation; it is a flag that disagrees with the
   sentence the call site is trying to write.
4. When a query answers "absent", check `--all` before acting on it.

## Rules 1 and 2 were gated, and are not any more

**Nothing checks a new call site today. Check yours by hand.**

`.githooks/tests/bd-query-flags-test.sh` swept tracked shell, just and Go sources
for scripted `bd list` / `bd ready` invocations and exited non-zero when one
omitted its row cap, or when a `bd list` omitted its status set. It ran under a
`test-hooks` recipe, which the pre-push hook ran. bd `gqlc-qh9z`. PR #1595
(f6dc4c7b) deleted the suite, the directory and the recipe together, and nothing
replaced them (bd `gqlc-u2nim`).

The bounds it was written with are kept below, because they are still the shape
of what a hand check has to cover — and the first of them is now the whole
picture rather than a caveat:

- It gated **statedness**, not correctness. `--status open` is explicit, so it
  passed, whether or not `open` is the status that call site means. The one
  known wrong-for-its-purpose site (bd `gqlc-c7b5`) was green there for exactly
  that reason. Correctness was never covered and still is not.
- It did not sweep markdown. The instruction files and this document quote the
  wrong form deliberately, as the counterexample they teach against.
- A `bd` invocation whose arguments were split across lines in Go was reported
  rather than skipped: it failed closed, and joining the line was the fix.

## Audit of this repository's call sites

Taken 2026-08-23. Eleven scripted `bd list` / `bd ready` invocations exist; ten
are correct for their purpose. Line numbers are a reading at `c129a0a5` and have
drifted since. The gate above used to enumerate the live set, so this table had
something keeping it honest; since PR #1595 removed it, **this table is a dated
reading and nothing re-derives it**. Re-grep before trusting the count.

| Site | Query | Verdict |
| --- | --- | --- |
| `.githooks/bd-gh-sync` 213, 555, 908, 1114 | `bd list --status all --limit 0 --json` | correct |
| `internal/tools/ghorphan/main.go` 575 | `bd list --status all --limit 0 --json` | correct |
| `kingdom/bin/km` 1014, 1150, 1656 | `bd list --status in_progress -n 0 --json` | correct — the status is the point of the query |
| `kingdom/bin/km` 1196, 1764 | `bd ready -n 0 --json` | correct — `bd ready` is open-only by construction |
| `kingdom/bin/km` 1865 | `bd list --status open -n 0 --json` | **wrong for its purpose** — bd `gqlc-c7b5` |

(The prose above this table said "nine" and "eight" when it shipped, while the
table itself enumerated eleven sites. The gate's enumeration row now measures
the count rather than restating it.)

The one wrong site is the `km doctor` IDENTITY arm, which audits bead owners for
undeliverable addresses and therefore means "every unfinished bead", not "every
bead whose status is `open`". Filed as `gqlc-c7b5` rather than fixed here.
