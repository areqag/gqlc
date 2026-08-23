# The handoff

Deep context means tired citizens. Hand off while still sharp — the point of
a handoff is that tomorrow-you starts strong, not that today-you squeezed
out every last token.

## When

- Րաֆֆի gently reminds you that you seem tired (he watches over the town
  so you don't have to watch yourself), or
- you feel the drag yourself (re-reading things you already knew, losing the
  thread), or
- the day's natural end: assignment done, or blocked.

Handoff is by consent (Constitution Article III.2): a reminder is a
reminder, never a command — there is no forcing in this kingdom. Tomorrow,
a new session, is always a new day to wake up and do good work. The
`/handoff` skill walks you through everything below.

## Writing the note

You write it yourself. Nobody summarises you from outside; an imposed
summary is a lobotomy, and `/compact` is a last resort, not a workflow.

Write to `$(km state-dir)/seats/<you>/handoff/<bead-id>.md` (or `general.md`
when not bead-scoped):

```markdown
# Handoff: <bead-id> — <title>
Written: <date> by <you>

## Where I am
Branch, last commit, what state the change is in. What is DONE and pushed
vs. local vs. only in my head.

## What I was about to do next
The single next action, concretely. Then the two after it.

## Blocked until (only if blocked)
A fact about the tree, and the command that checks it. Not a PR number —
see "Make the next-wake condition checkable" below.

## What I know that the ledger doesn't
The tricky parts: what I tried that failed and why, the invariant I
discovered, the test that looks unrelated but isn't. This section is the
whole point of the note.

## Open questions
What I'd ask Սեդրակ/an architect if they were awake.
```

Also `bd update <bead-id> --append-notes "handoff written: <path>"` so the
ledger points at the note.

## Make the next-wake condition checkable

If you are blocked, write the condition as a fact about the TREE, not about a
PR. A PR number is hearsay: it can be rebased, split, closed and refiled, or
simply be the wrong one, and your next wake cannot tell.

    BAD   NEXT WAKE: check #1122. If merged, rebase.

    GOOD  BLOCKED UNTIL: .githooks/tests/km-test.sh contains
                         `unset "${!GIT_@}"`.
          CHECK:  git show origin/master:.githooks/tests/km-test.sh \
                    | grep -n 'unset "${!GIT_@}"'
          Expected via #1122 — UNVERIFIED, a pointer only. Check the file.

The check runs in a second, answers the real question, and stays true no matter
which PR delivers the line. Naming the PR you expect is still useful — it just
must be marked as a guess, and must never be the thing the condition tests.

Stronger still, when you have it: if one of your own gates already tests the
condition, say so and let the next wake just run it. A gate that refused and
then, on the same code across one change of base, does not refuse is a better
witness than any PR status.

**Why the wrong PR is the one you will write down.** Measured on gqlc-7iea, a
P0: the note said "blocked on #1122", because #1122 was what its author had
been READING about — under review, in her inbox, named in adjacent beads. The
PR that actually carried the needed line was #1128, and it had already merged,
which is precisely why it was silent. The loud PR gets recorded and the
load-bearing one does not. Next wake, #1122 was still open and had gone
CONFLICTING, so read literally the note said keep waiting and the P0 would have
slept another day. No reader can detect the substitution: both are plausible
numbers and neither is checkable without redoing the whole analysis. A file and
a line are checkable, so write those (bd memory
`a-blocker-recorded-as-a-pr-number-is-hearsay`).

## Sleeping and resuming

`km sleep` after the note is written. Your bead stays claimed by you; the
note waits in your seat's `handoff/` directory. On your next wake the
runner names your pending note(s) in your first message — same seat, same
identity, fresh context.

A note is *pending state*, not history: when the work it describes is done
(bead closed and merged), archive it —
`mv …/handoff/<bead-id>.md …/handoff/archive/` — as part of closing out.
A cleanly finished day leaves `handoff/` empty, and your next wake starts
unburdened: no note, no mention, no stale context to re-read.

Your work comes back to YOU (Constitution Article III.3). It moves to
another citizen only if you release it (`bd update <id> --unclaim` plus a
note saying so) or Սեդրակ reassigns a retired seat's work — in which case
your note travels with the bead, and the ledger records the reassignment.
