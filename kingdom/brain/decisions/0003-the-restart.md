# 0003 — The restart

Date: 2026-08-22. Decided by Անդրանիկ, directly, with the town halted.

The Թագաւորութիւն ground to a halt on its second day. Անդրանիկ stopped it,
merged the open work by hand, and changed five things before turning it back
on. This records what he changed and why, so that whoever re-tunes any of it
later argues with the reasoning rather than rediscovering it.

## What the halt actually looked like

25 open PRs. A backlog opening two to four times more beads per day than it
closed (gqlc-jagv). 319 open beads, 264 of them P2/P3 — overwhelmingly
low-priority defect findings the town's own adversarial review had produced.
Every P1 on the ready queue was town machinery; not one advanced gqlc itself
(gqlc-ag4g). And underneath all of it, three P0s that meant the town could not
route work at all: dead `km-seat` loops leaving 7 of 15 seats permanently
unreachable (gqlc-6ha4), finished seats holding cap slots at empty prompts for
eleven hours (gqlc-5vp7), and both judges frozen on modals while every board in
the town read healthy (gqlc-eier).

**The order matters.** The review gate was the visible constraint and the
liveness defects were the real one. A town whose seats cannot be woken is not
a town with a slow review queue, and it was diagnosed as the latter for most
of a day.

## The five changes

1. **Review follows the design gate** (Constitution V.2, rewritten). A PR is
   reviewed if and only if its bead was blocked by a design bead — plus
   constitution amendments, plus any PR a citizen asks to have reviewed.
   Everything else merges on green gates.

   Rejected: the LIGHT/FULL tier system adopted hours earlier the same day. It
   was sound but it still cost a judge wake per PR, and the queue was the
   problem. Rejected also: reviewing everything faster, by more judges at less
   depth — that buys throughput by making the gate worse at the one thing it
   is for.

   The reasoning is that the town already makes this judgment once, at intake,
   when it decides whether work needs a plan. Asking it a second time, per PR,
   bought a queue and nothing else. The cost accepted: the design-gate call is
   now a quality decision as well as a planning one, and a bead split wrongly
   at intake loses its adversarial read. Two things hold that open — a Ռազմիկ
   who finds the work larger than filed stops and has it resized, and any
   citizen may demand review of any PR owing nobody a reason.

2. **Three equal judges, and calibration repealed.** Տիր joins Միհր and
   Անահիտ. The clause making a new judge's first three PASSes wait on a
   countersignature is gone: Անդրանիկ wants no ladder, Article II.4 does not
   admit one, and it was incoherent the moment two judges were new at once,
   since they would have countersigned each other (gqlc-8wwa). What keeps a
   verdict honest is V.2.1 — the standard binds the signer, not the seat.

   With most PRs now unreviewed, the judges' **patrol** duty carries more
   weight than it did: it is the compensating control on change 1.

3. **`high` is the ceiling.** Warriors medium → high. xhigh and max exist for
   one case — a Ճարտարապետ on a design complex enough to need one — and are
   reached per-bead through an `effort:<level>` label, never by raising a
   default. The town had spent its entire life at xhigh, inherited from an
   operator settings file nobody chose.

   The label also delivers Constitution V.6.2's right, which had been
   unreachable in practice: `/effort` is parsed from user input and a citizen
   cannot type it (gqlc-jmwh). Scoping the escalation to a bead makes V.6.3
   true by construction rather than by anyone remembering.

4. **The dispatcher routes P0–P2 only.** P3 and P4 stay filed and searchable
   and wake nobody.

   This is the honest version of a trade the town had already noticed:
   lowering effort would have shrunk the backlog by discovering fewer defects,
   and a falling number would then have read as health (gqlc-jagv). Declining
   to ROUTE low-priority findings does not pretend they were never found. The
   number to watch is open P0–P2, not raw open count. Raise the threshold when
   that queue drains; do not raise it because the lake is large.

5. **Personalities, not specialisations.** Every soul had carried a standing
   preference for a kind of work — Վահագն the hardest bugs, Այգ greenfield,
   Հայկ precision fixes — and the architects and judges were differentiated by
   gift. The dispatcher never read any of it: routing is by `class:` label
   alone. So a preference could not attract the right bead, only make a
   citizen reluctant on a bead correctly given them, in a town that was
   already stalling.

   Every seat of a class now carries a byte-identical duties section — the
   union of what the class had collectively known, so nothing was lost — and
   the personality lives entirely in who the citizen *is*. Article II.4
   already had the words: citizens of a class differ in personality and in
   what they are drawn to look at, never in what their word is worth.

## What was NOT changed, deliberately

- **`max_active` stays at 5.** Throughput was not bounded by the cap; it was
  bounded by seats that could not be woken and by seats holding slots while
  idle. Raising the cap before fixing those would have bought more stuck
  seats.
- **The standard of a review** (V.2.1) is untouched. Fewer PRs are read; the
  ones that are read are read the same way.
- **The root cause of the dead `km-seat` loops** is still unknown, and
  gqlc-6ha4 says so plainly rather than guessing. Detection with the right
  polarity is the fix; the cause is to be found, not assumed.

## How to tell whether this worked

Change 1 is a throughput measure and V.2.0.4 binds the town to judge it as
one: a defect reaching master through an unreviewed merge repeals or narrows
it before the next merge. That requires counting — a defect found on merged
code records whether its PR was reviewed. Without that record there will be no
evidence to re-tune any of this, and it will be re-tuned on feeling instead,
which is how it was tuned the first time.
