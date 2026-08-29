# Writing to the bd ledger

Sibling of [bd-ledger-queries.md](bd-ledger-queries.md), which covers the read
side. `bd` is an external tool (`/usr/bin/bd`, a node shim over a native binary);
its source is not in this repository and none of this can be fixed by patching
it. What follows is the write-side contract as measured against the deployed
binary.

All rows below were taken first-party on 2026-08-24 against **bd 1.0.4
(`ce242a879`)**, in throwaway `bd init` workspaces — never against the live
ledger. Binary unchanged on this host since 2026-05-09, so it is the same binary
every seat has been running. bd `gqlc-o6kp`.

## The confirmation line means "accepted", not "changed"

    $ bd update <id> --status blocked --assignee somebody   # already both
    ✓ Updated issue: <id> — <title>
    rc=0, and not one field moved.

`✓ Updated issue` reports that the command was accepted, not that a row changed.
There is no separate rendering for an update that was a no-op. **A success line
is not evidence of a write**, so a citizen who watches for one and stops there
has checked nothing. Read the field back.

## A refused flag or value discards the whole update — loudly

Every rejection path measured behaves identically: exit 1, **stdout empty**, the
reason on stderr, and **no field written, including the fields that were valid**.

| Command (single id) | rc | stdout | wrote |
| --- | --- | --- | --- |
| `--status open` | 0 | `✓ Updated issue` | status |
| `--assignee ""` | 0 | `✓ Updated issue` | assignee |
| `--assignee "" --status open` | 0 | `✓ Updated issue` | both |
| `--status open --set-labels class:warrior` | 0 | `✓ Updated issue` | both |
| `-l class:warrior` | 1 | *empty* | nothing |
| `--status open -l class:warrior` | 1 | *empty* | nothing |
| `--status open --frobnicate x` | 1 | *empty* | nothing |
| `--status bogus` | 1 | *empty* | nothing |
| `--assignee "" --status bogus` | 1 | *empty* | nothing |
| `--priority 9` | 1 | *empty* | nothing |
| `--status open --priority 9` | 1 | *empty* | nothing |

Two consequences worth stating separately.

**Multi-flag updates are not poisoned as a class.** Rows 3 and 4 apply both
fields. The documented two-field release —

    bd update <id> --assignee "" --status open

— is safe, and `--assignee ""` is parsed as a value, not as a missing one.

**One bad flag costs the whole command, not just itself.** Validation runs before
the write, so a valid `--status` sitting beside an invalid `--priority` is
discarded with it. That is the right behaviour — a half-applied release is worse
than a refused one — but it means a typo silently costs every intent on the line
if nobody reads the exit status.

### `bd update` has no `-l`

Labels on `bd update` are `--add-label`, `--remove-label` and `--set-labels`.
`-l, --labels` is **`bd create`'s** flag. Carrying the habit across from `create`
costs the entire update, with this on stderr and nothing on stdout:

    Error: unknown shorthand flag: 'l' in -l

## Where the silence actually comes from

Put the two halves together. A refused write produces **empty stdout, exit 1, and
a reason on stderr only**. Discard stderr — which this repository's scripted call
sites do, and which agent transcripts routinely do — and ignore the exit status,
and a refused write is indistinguishable from a silent no-op. The tool was loud;
the channel it was loud on was closed.

This is the same failure the query side has with the row cap notice: bd discloses
on stderr, and stderr is where nobody is listening.

## `bd update` is per-id best-effort; `bd close` is all-or-nothing

Both accept several ids. They disagree about what a bad one means, and the
disagreement is in the exit status, which is the thing scripts read:

    $ bd update <good-id> <nonexistent-id> --status open
    ✓ Updated issue: <good-id> — <title>          # stdout
    Error resolving <nonexistent-id>: ...          # stderr
    rc=0                                           # <-- reports success
    good-id was updated; nonexistent-id was not.

    $ bd close <good-id> <nonexistent-id>
    Error: resolving ID <nonexistent-id>: ...      # stderr
    rc=1
    good-id was NOT closed. Nothing was written.

So `bd update` is the one shape measured here where **a partially failed write
exits 0**. Order does not matter — the bad id first or second gives the same
result. Name one bead per `bd update` whose success you intend to check by exit
status, or check each bead's state afterwards rather than the command's.

## Rules for a scripted write

1. **Check the exit status.** It is load-bearing on every rejection path.
2. **Do not discard stderr on a write.** Capture it and print it on the failure
   branch; a bare `2>/dev/null` throws away the only account of what refused.
3. **One id per `bd update`** you intend to verify by exit status.
4. **Read back the field that matters**, and for routability read it back with
   `bd ready` — `bd show` will display a bead no dispatch pass can reach.
5. **Never treat `✓` as a write.**

## What is gated, and what is not

`.github/workflows/bd-behaviour.yml` runs these claims against the **latest
released** bd, in its own job, so the town learns that a future bd has changed
its mind before it upgrades. That workflow is an ALARM, not a merge gate, for
the reasons its own header gives — the claims are about a binary this repository
neither owns nor vendors.

Nothing gates the write call sites themselves, and that is a deliberate
omission rather than an oversight. The query side has a sweep because it has
eleven call sites; the write side has **one**.

## Audit of this repository's write call sites

Taken 2026-08-24 with the same scanner shape the query sweep uses (comments cut,
quoted spans blanked, command position required).

| Site | Call | Verdict |
| --- | --- | --- |
| `kingdom/bin/km` 2370 | `bd create ... >/dev/null 2>&1` inside `if !` | correct — exit status checked, and the failure branch prints its own diagnostic, so the discarded stderr costs the reason rather than the detection |

Every other match, when this was taken, was fixture text in
`.githooks/tests/claude-pre-bash-test.sh` — `bd close` command strings handed to
a hook as data and never executed. PR #1595 deleted that file, so those matches
are gone from the tree rather than reclassified.

Re-measured 2026-08-29 (bd `gqlc-u2nim`): the row above is still the only write
call site discarding stderr, and it has moved to `kingdom/bin/km` 3246 — which is
what the drift warning in the query-side audit is about. That re-measure was a
grep, not the scanner: the scanner shape described above cuts comments and blanks
quoted spans, and no such tool survives in this tree to re-run.

The exposure this document addresses is therefore in the **recipes citizens type
by hand**, not in the scripts. See `kingdom/brain/playbooks/citizen-protocol.md`.

## What could not be reproduced

`gqlc-o6kp` was filed on the observation that

    bd update <id> --assignee "" --status open -l class:warrior

printed `✓ Updated issue:` and changed nothing, and it asked whether an
unhonoured flag discards its siblings while still reporting success. Half of
that reproduces and half does not, and the half that does not matters to anyone
reading the bead later.

The discard is real and total. The success line is not: that argv exits 1 with
empty stdout on the deployed binary, and on the latest release, and there is only
one `bd` on this host with an unchanged mtime, so it is not a version difference.
A `✓` and that argv cannot come from the same invocation.

What the bead recorded faithfully is the **experience** — and the experience is
fully explained without any success line being printed by that command, once you
know that the refusal writes nothing to stdout and speaks only on stderr. The
bead's own step 2 records `-l` alone as having "printed NO output at all", which
is exactly what a stderr-suppressed rc=1 looks like from the inside.

That is why the rules above are about the exit status and stderr rather than
about a flag: the reported defect was not bd being quiet, it was bd being loud in
a place nobody was reading.
