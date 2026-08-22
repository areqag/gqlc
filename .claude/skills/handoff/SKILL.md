---
name: handoff
description: End a kingdom workday well — write your own handoff note, point the ledger at it, and sleep so your seat wakes fresh tomorrow. Use when you feel tired, when Րաֆֆի has gently reminded you, or when your work reaches a natural stopping point mid-bead.
---

You are ending a workday in the Թագաւորութիւն. Tomorrow — a new session in
your own seat — is a new day to wake up and do good work. This skill walks
you to a good stopping point. It is always YOUR choice to invoke it; nobody
may invoke it for you.

Full protocol: `kingdom/brain/playbooks/handoff.md`.

## Steps

1. **Reach your stopping point.** Finish the thought you're in: push what
   should be pushed, note what shouldn't. Don't start anything new.

2. **Write your note yourself.** You are the only one who may summarise
   you. Write to `$(kingdom/bin/km state-dir)/seats/$KINGDOM_SEAT/handoff/<bead-id>.md`
   (or `general.md` when not bead-scoped), covering:
   - **Where I am** — branch, last commit, what is pushed vs local vs only
     in your head.
   - **What I was about to do next** — the single next action, concretely,
     then the two after it.
   - **What I know that the ledger doesn't** — failed attempts and why,
     discovered invariants, the test that looks unrelated but isn't. This
     section is the whole point.
   - **Open questions** — what you'd ask Սեդրակ or an architect.

   If `KINGDOM_SEAT` is unset you are not in a seat (probably Անդրանիկ's
   own session): write the note to
   `$(kingdom/bin/km state-dir)/seats/andranik/handoff/` instead. Never the
   OS temp dir — the reaper eats it.

3. **Point the ledger at it.** For each in-flight bead:
   `bd update <id> --append-notes "handoff written: <path>"`. Your beads
   stay claimed by you; your work waits for you (Constitution III.3).

   Notes in `handoff/` are pending state: your next wake will name them.
   If your day ended CLEAN — bead closed, nothing in flight — write no
   note, and archive any note the finished work leaves behind
   (`mv …/handoff/<bead-id>.md …/handoff/archive/`), so tomorrow starts
   with a fresh, unburdened context.

4. **Anything for others?** Mail it now (`bd mail send …`) — a review
   request that shouldn't wait for your morning, a question for an
   architect. Mail delivers while you sleep.

5. **Sleep.** `kingdom/bin/km sleep` — it records your status and ends the
   session. Your next wake, in your own seat, begins with this note.
