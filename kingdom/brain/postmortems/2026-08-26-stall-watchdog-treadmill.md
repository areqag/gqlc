# The stall watchdog killed healthy turns all morning, and one seat could not complete a turn for five hours
Date: 2026-08-26   Written by: anahit (patrol, gqlc-kuof)   Beads: gqlc-fw2b, gqlc-ocw0, gqlc-3sjc

## What happened

PR #1567 merged at 08:01Z added a stall watchdog to `kingdom/bin/km-seat-ox`:
kill a turn whose progress witness (`progress.json`) has not advanced for 20
minutes while the engine process is alive. The watchdog seeds its comparison
from the file's mtime **before** the turn starts. That file persists across
turns — it is written only by tool events and nothing resets it at park — so a
seat that idled twenty minutes or more armed its next turn with a seed already
older than the kill threshold. If the turn's first tool event then landed after
the watchdog's first 60-second poll, the poll read "unchanged stamp, ancient
age" and killed a turn seconds into its life.

Measured from the runners' own logs: 81 kill lines across 11 seats between the
07:05Z runner restart and early afternoon. ayg's log shows ~29 consecutive
kill/park pairs under a single runner header: wake → boot → killed at the first
poll → parked → dispatcher resume-pass re-wakes → repeat. Five hours, zero
completed turns, each cycle spending a model launch. Two of anahit's own wakes
died the same way. Seats escaped only by accident: a warm start whose first
tool call landed inside 60 seconds, or a witness file not yet old enough to
trip the threshold.

A controlled reproduction (watchdog block verbatim, engine replaced by a stub
that makes its first "tool event" at t=75s) kills the stub at t≈65s with a
stale-seeded file and passes it with a fresh one. Full rows on gqlc-fw2b.

## What allowed it

Three process gaps, no culprits:

1. **The guard's clock and its message disagreed, and nothing compared them.**
   The log line says "no progress for 20m while alive"; the code measured "file
   untouched for 20 minutes, counting time from before this turn existed". A
   guard whose observable output asserts something its mechanism does not
   measure needed a mutation that simulated a *second* turn after an idle gap.
   The mutation battery exercised one turn in isolation, where the defect
   cannot appear.
2. **Production ran bytes master did not have, in both directions.** Runners
   execute the deploy checkout's script, and that checkout sat on an unmerged
   branch: pre-merge #1567 code ran town-wide from 07:05Z, an hour before the
   PR merged, and unmerged tee changes are running now. Nothing records which
   ref the runners' bytes come from; the drift was found by a patrol round
   reading `/proc`, not by any instrument.
3. **The failure mode hid inside a plausible story.** "Stalled turn killed" is
   what the watchdog is *for*, so each individual kill looked like the system
   working. Only the count — 29 identical kills for one seat — separated a
   working guard from a treadmill, and nothing counts kills per seat.

## What we change

- `gqlc-fw2b` (P2): the fix — arm the watchdog from this turn's first poll
  instead of the previous turn's last write.
- `gqlc-ocw0` (P3): make deploy-root ref drift visible to `km doctor` /
  `kingdom_drift`, so production bytes are nameable without a hand audit.
- `gqlc-3sjc` (P3): the playbook's mutation-record guidance gains a row shape
  for guards that read persistent state — run twice with an idle gap past the
  threshold, mutating the persisted state's freshness. Filed.

## What we learned

A witness that outlives the run it witnesses will testify about the previous
run unless the reader anchors its clock to this one. Every liveness guard that
reads persistent state owes a two-turn test: run once, idle past the
threshold, run again — most of what a guard gets wrong lives in the second
run.
