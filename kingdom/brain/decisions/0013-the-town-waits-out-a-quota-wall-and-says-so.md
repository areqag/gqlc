# 0013 — The town waits out a quota wall, and says so

Status: accepted
Date: 2026-09-02
Bead: gqlc-o7ni0 (implementation) · gqlc-tdciz (the ruling) · gqlc-nmxmr (design v2)
Written by: Նուարդ, executing Արթուր's design

## What happened twice

The account's quota ran out and the town did not notice it had come back.

On 2026-08-30 the reset landed at 03:50Z and nothing routed until a human
asked about it at 12:34Z — 8h44m of a sixteen-seat town, with the dispatch
timer firing 260-odd times and every one of them exiting 0. It happened again
for roughly nine hours before this was implemented.

Nothing was broken in the sense a gate can see. Every board read healthy,
because the failure has no artefact: a seat whose turn dies inside a wall
holds a live `claude` in a dead session, so `cmd_reconcile` never frees her
slot, she answers no dispatch pass (every pass wakes ASLEEP seats only), and
she never ends by herself. The town runs out of slots and reports nothing
wrong.

## The decision

**km detects the wall, withholds routing loudly while it stands, and notices
the lift inside ten minutes.**

The mechanism, in the order a pass meets it:

1. **The raise interface is dispatch's own `idle-refusal` marker**, not a pane
   read. Two distinct seats refused an ask inside 15 minutes is the trigger.
   One seat wedges for private reasons routinely; two at once is a property of
   the account.
2. **A refusal is a suspicion, not a verdict, so the pass spends a probe** —
   `claude -p hi` in an empty temporary directory, with `KINGDOM_SEAT` unset,
   under a timeout. Never from a checkout: a probe run inside the tree would
   load `CLAUDE.md` and the hooks. At most one probe per ten minutes, whether
   the wall is suspected or standing.
3. **The flag is a file** (`<state>/walled`) carrying the raise time and the
   seats that raised it. While it exists, dispatch withholds routing, the
   recovery ladder and reconcile, and prints one loud line per pass naming
   what is withheld and how an operator overrides it.
4. **The lift is noticed by the same probe.** When the account answers, the
   flag is deleted, the five per-seat ladder markers are wiped, a letter goes
   to Սեդրակ, and routing resumes by falling through **in the same pass**.
5. **`km doctor` carries two rows**: a standing flag is a `warn` and only its
   age (≥12h) makes it a FAIL, and a count pin over the forbidden command.

The prose that explains each of these lives in `kingdom/brain/km-notes.md`
under `## wall`, `## wall_refusal_seats`, `## wall_probe`, `## wall_withheld`,
`## wall_clear`, `## wall_guard`, `## cmd_doctor-wall` and
`## cmd_doctor-fence`.

## The letter is the deliverable, not the routing

Routing resumes on its own by falling through. What routing cannot do is tell
anyone that the town's slots are still held by sessions that will never end.
The seats who sat through the wall are exactly the ones the machinery cannot
recover: their turn died inside it.

So the lift sends Սեդրակ a kill list. It names each such seat with the age of
the last tool call of hers that FINISHED, and says in the letter itself that
this is the evidence and not the verdict — one long legitimate tool call reads
identically from outside, and the wrong seat ended is a citizen's day taken.
**Ending a session is an operator's action and nobody else's (VI.2).** The
protocol is gqlc-qs4jq's: ask each seat to self-park first, since `km sleep`
from her own pane frees the slot with her consent, and end only what does not
park.

The marker wipe is part of the same honesty. The refusals the ladder counted
during the wall were the wall's, not the citizens'; leaving them would leave
every seat ESCALATED — silenced for the rest of the episode — at the exact
moment delivery might finally work.

## What this town will never do

The wall advertises its own remedy in the pane, and a future designer will
re-derive it as an obvious oversight. It is not an oversight. Quoted in full
so that nobody has to go looking:

> ROYAL DECISION, Անդրանիկ, 2026-08-30: the town must NEVER invoke
> /extra-usage. EVER. A hard NO, with no threshold and no exception clause. It
> spends the king's money and no agent in this town has that authority. No
> mechanism in kingdom/ may type, send, schedule or retry it in any pane,
> under any condition — not as a fallback, not when a P0 is stalled, not when
> a citizen appears to ask for it. Do not design a code path that reaches it
> 'only if'. Correct behaviour on an exhausted quota is to WAIT for the reset
> and SAY SO. Context: a quota wall killed the town for 8h44m on 2026-08-30
> (gqlc-3evsn, gqlc-tdciz); /extra-usage is the remedy the wall itself
> advertises in the pane, so a future designer WILL re-derive it as an
> oversight unless the exclusion is stated where they are working.

This document is the "waits for the reset and says so" half, built. The other
half is enforced mechanically: `km doctor` pins the number of lines under
`kingdom/bin/` that name the command at three, and all three are readers — the
wedge detector, the wall probe's predicate, and the fence's own pattern. A
fourth line is machinery that composes or types it, and the row FAILs naming
the file. Its limits are written down in `## cmd_doctor-fence`; the two that
matter are that it is one-directional (a deleted reader passes) and that it
sees `kingdom/bin/` only.

## What was rejected, and why

**Parsing the reset time out of the pane** (gqlc-tdciz option (a)). It is the
one artefact that actually knows the answer, and it is a vendor string that
can change wording without notice — this repo already carries a bead about
quoting a diagnostic's prose verbatim with nothing re-checking it (gqlc-0996).
A parser that silently stops matching leaves exactly the outage it was built
to end.

**Giving it to the guard's round** (option (c)). Րաֆֆի froze at the same wall
he would have been watching. Only the dispatch timer survived it, so the
detection belongs where the timer is.

**The canary wake** (design v1). v1 cleared the flag by launching an ordinary
seat and seeing whether she survived, on a launch-storm premise that a
measurement falsified: the launches were not the problem. A `claude -p`
subprocess answers the same question without spending a citizen's session on
it, and v2 withdrew the `wall-clear` verb with it.

**A cheaper model for the probe.** A model that answers while the seats' model
is walled would clear the flag onto a town that is still stopped.

## The direction of every unknown

An unrecognised probe outcome — an empty answer, a non-zero exit, a timeout —
reads as WALLED, not as clear. Withholding routing during a wall that is over
costs at most ten minutes and says so loudly every pass. Resuming into a wall
that is not over burns seats and re-enters the state that took nine hours to
notice.

## The number this rests on that has not been measured

`WALL_STALE_SECONDS` is 12h: above the longest wall we have actually seen
(8h44m) and below a day. If a weekly cap can stand longer than that, the
doctor row will FAIL on a healthy town mid-wall, and its message says so in
both directions rather than asserting which one is happening.
