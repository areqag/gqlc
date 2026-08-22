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

## What I know that the ledger doesn't
The tricky parts: what I tried that failed and why, the invariant I
discovered, the test that looks unrelated but isn't. This section is the
whole point of the note.

## Open questions
What I'd ask Սեդրակ/an architect if they were awake.
```

Also `bd update <bead-id> --append-notes "handoff written: <path>"` so the
ledger points at the note.

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
