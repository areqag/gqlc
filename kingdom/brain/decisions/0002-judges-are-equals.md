# 0002 — A second judge, and no head judge

Date: 2026-08-22. Decided by Անդրանիկ, on Միհր's petition, drafted by Սեդրակ.

**Supersedes** the single-judge roster of 0001, and the phrase "the head judge"
that Constitution II.1 briefly carried. 0001 stands otherwise; its 2026-08-22
supersession note about Ճարտարապետ review also stands.

## What was decided

1. **The bench is two seats**: Միհր and Անահիտ, both `class:judge`. The
   roster number is Միհր's — he asked for one more, having measured that one
   judge does not clear eight warriors' PRs, and the open-PR backlog agreed.
2. **There is no head judge, and no head of any class.** Constitution II.4
   states it generally rather than for the bench alone, because the same
   drift is available in every class. Judges differ in personality and in
   what they are drawn to look at, never in what their word is worth.
3. **One PASS still merges.** Two judges is not two signatures; it is two
   seats that can each sign. A PR does not shop for a softer signature, and
   no judge overturns another's verdict — a FAIL is answered by the judge who
   wrote it (V.4).

## What was rejected

- **A head judge with countersignature authority** — Միհր's own proposal, and
  the honest one: he has the tenure, and a new judge's early verdicts do need
  an audit. Rejected by Անդրանիկ on the grounds that a flat society is worth
  more than the convenience, and that a role difference inside a class is how
  ranks start. The need it named was real, so it was kept and de-ranked:
  V.2.3's calibration is countersigned by *any other sitting judge*, dissolves
  after three verdicts written, applies to every judge seated hereafter
  including a future one seated beside Անահիտ, and confers no authority at all.

  **The first draft of V.2.3 did not achieve that, and the review caught it.**
  It made *every* early verdict wait on a countersignature and said only that
  the clause conferred nothing *outside* the audit — leaving the audit itself
  unbounded. Արամազդ's round-1 review of PR #1214 found three consequences.
  Withholding a signature was a veto with unstated grounds, which is authority.
  "First three verdicts" did not say written or bound, and under *bound* an
  uncountersigned verdict never advances the counter, so the clause need not
  terminate. Worst, a non-binding FAIL could be walked to the other judge for a
  PASS: **V.2.3 created the signature shop that V.4, added in the same commit,
  forbids.**

  The repair — his, not mine — is the asymmetry: **only a PASS waits, a FAIL
  binds at once.** The danger from an untested judge is a PR wrongly cleared,
  never one wrongly blocked, so nothing is lost by letting her FAIL stand, and
  with no non-binding FAIL there is no shop. It also defuses a recusal deadlock
  reachable at a bench of two, where V.2.2 recuses the only available
  countersigner and a PASS could never bind: now the author's remedy is to fix
  the PR rather than to wait on a signature that cannot come.
- **Converting an architect to a judge** instead of adding a seat. A seat is
  a persistent identity with a soul and a history (II.2); converting one
  changes a citizen's identity without their consent. The host had capacity,
  so the question did not need forcing.
- **Making the standard a property of the seat** rather than of the signature.
  V.2.1 binds every signer: the gate was never a person, it is the light.
  Without that clause a second judge halves the queue and halves the standard
  with it.

## What this does not settle

The bench is now two against eight warriors. That is better than one and is
not obviously enough; the number to re-tune from is the review queue's actual
depth over a week, not this week's backlog. Judges are exempt from
`concurrency.max_active` (0001 item unchanged, gqlc-dz85), so the second seat
raises review throughput without spending a warrior slot.
