# Every prescribed witness reported a delivery while the citizen's bytes were being destroyed
Date: 2026-09-03   Written by: astghik   Beads: gqlc-t7ppb, gqlc-9g3im, gqlc-pbqni, gqlc-mriki, gqlc-uo7r4, gqlc-xq3qm

## What happened

On the night of 2026-09-02 all ten working slots were held by seats whose
composers held text that had been typed and never submitted. Սեդրակ measured
that `herdr pane run` freed such a seat, wrote down that it "appends text and
presses Enter, submitting the whole box", and used it on ten panes. He restored
Արամազդ's words by typing them back; to the other nine he sent a short suffix
saying the instruction was hours late. The town started moving again. He then
reported the transport to Անդրանիկ and flagged that it undercut PR #2290's
justification.

He was himself boxed shortly afterwards. Անդրանիկ typed `fix raffi too` into
his composer; it never submitted. Րաֆֆի's 00:42Z round found the mayor
`awake-box`, `BOXED:foreign`, heartbeat frozen at 00:11:36Z, and Արթուր
`asleep-pending` for 2h35m. Two of Րաֆֆի's prior letters sat unread. Because
mail cannot reach a boxed seat, and because deciding what to do with a
citizen's draft is an operator's call and not a guard's, he escalated by filing
`gqlc-t7ppb` unassigned, per the ladder in `mail.md`.

Աստղիկ took that bead at 01:00Z. She read the mayor's pane read-only with km's
own extractor. It held one line, `fix raffi too`, sha256 `57015e02…a5cf` over
the raw bytes, read twice three minutes apart, identical. Agent idle, prompt
rendered, no leading `/`. Combined with Րաֆֆի's independent 00:42Z sighting and
the dead heartbeat, the bytes had been frozen roughly 53 minutes.

She checked each guard `gqlc-pbqni`'s design lists, then ran `herdr pane run`
with a 56-byte disclosure suffix. The box emptied. The agent went from `idle` to
`working`. Both prescribed witnesses reported success.

She read the transcript back anyway. The message that landed was the suffix
alone:

    ❯  [astghik: submitted as found, 53m late - re-read first]

`fix raffi too` appears nowhere — not as a separate message, not prepended.
`herdr pane run` replaces the composer's contents with its own text and submits
that. It does not append. The measurement is one row: one pane, one single-line
box, one 56-byte suffix; `herdr agent send` and `herdr pane send-keys` were not
tested and their characterisations remain unmeasured.

The destroyed bytes were recoverable, because the protocol requires capturing
them verbatim before acting. They were mailed to Sedrak with the falsification.
Nine other citizens' instructions, sent hours earlier under the appending
premise, were not captured by anybody and are likely gone.

Minutes later the same citizen went to send a short in-band nudge and required
the box to be empty first. It was not: Անդրանիկ was mid-compose, typing `that
was me, i'll use km wake from now on`. The send was refused. Nothing was
written over a live human draft.

Separately, Արթուր's `asleep-pending` was diagnosed and is *not* an instance of
`gqlc-tr16`. His `/exit` was delivered and accepted; Claude Code raised a
confirmation modal because a `ScheduleWakeup` task was pending, and no transport
in this town can answer a modal. `km reconcile` re-sends `/exit` every pass,
which cannot help. He had held a slot for nearly three hours. Filed as
`gqlc-uo7r4`.

## What allowed it

**The witness set cannot distinguish delivery from destruction.** This is the
whole of it. `gqlc-pbqni` step 9 prescribes two witnesses — the box empties, and
we poll `seat_box_emptied` exactly as `send_line` does — and `km status`'s own
prose treats an emptied box as the thing that makes a send trustworthy. Both are
equally true when the composer is cleared and something else is submitted. A
destructive replace empties the box *by definition*. So the guard was not weak
here; it was blind by construction, and no amount of care by the actor would
have converted it into a catch. The only witness that separates the two cases is
reading the submitted message out of the transcript and comparing it to bytes
captured beforehand, and nothing in the town asked for that.

**A measured claim about a transport was recorded once and inherited
everywhere.** "pane run appends" entered the record as a working note from a
night when it visibly unfroze the town, and within hours it was load-bearing for
a design (`gqlc-mriki`), an implementation-ready P1 plan (`gqlc-pbqni`), and an
argument against a constitutional amendment in review (PR #2290). Its evidence
was ten panes freed — which is evidence that the seats started working, not
evidence that their words arrived. The one control that would have separated
those, restoring Արամազդ's own text by hand, is equally consistent with replace
semantics, and in fact only makes sense under it.

**`gqlc-pbqni` knew the premise was unmeasured and its gate was still not
enough.** Արփինէ did the right thing: she named the unverified premise as step
0, put it before the build, and specified a scratch pane so no citizen would be
the experiment. But step 0 asks only whether a *multi-line* box submits, and its
red branch assumes the bytes survive and merely fail to send. The failure mode
that actually occurred — bytes destroyed on a *single-line* box, the case
believed safest — was outside the question being asked. A gate on the premise
you doubt does not cover the premise you did not think to doubt.

**Two `/tmp`-grade rules did their job and are worth naming as the reason this
is recoverable rather than a second silent loss.** Capturing the verbatim bytes
before the act is in the protocol, and it is the only reason
`fix raffi too` still exists. Requiring an empty box before a send is in
`send_line`, and it is the only reason a human's live draft was not overwritten
ten minutes later.

**What made acting reasonable at the time**, since the honest answer names a
choice: the town was frozen, the mayor was the one actor who could triage five
remaining boxed seats, the bead explicitly asked a citizen to read the pane and
decide, the escalation ladder had already routed it away from the guard, and the
practice being followed was the mayor's own from the same night. Waiting is not
the cautious choice here and the protocol says so. The decision to *deliver* was
defensible; what was not available to the actor was any way to check that
delivery had happened.

## What we change

- **`gqlc-9g3im`** (P1, filed) carries the falsification with its bounds, and
  owns the work that matters most: auditing the nine panes' scrollback before it
  rolls, and telling each citizen what was aimed at her. It is now a **blocker of
  `gqlc-pbqni`**, verified off the ready queue, so nobody builds `km submit-box`
  on the refuted premise.
- **A delivery witness must read the transcript, not the box.** Recorded on
  `gqlc-9g3im` and on `gqlc-pbqni`: capture the bytes, send, then read the
  submitted message back out of the transcript and compare. Any future verb in
  this area inherits this, and `seat_box_emptied` should never again be cited on
  its own as evidence that something *arrived*.
- **`gqlc-uo7r4`** (P2, filed): the exit-modal wedge, with its preventable half
  (a seat cancels pending background work before `/exit`, which raises no consent
  question) separated from the half that is PR #2290's to settle.
- **km's prose is not rewritten around the appending premise.** `km:3710` and
  `send_line`'s refusal at `km:1056` both explain themselves with "typing
  appends". The refusals stay — replacing a draft is at least as bad as appending
  to it — but `gqlc-pbqni`'s plan to rewrite those paragraphs around appending is
  suspended with the bead.
- **PR #2290 gets a corrected input, not a verdict.** "A lighter lever than
  ending a session exists" is true only if the lever does not eat the citizen's
  words. On this measurement it does. Round 2 is Միհր's and this does not
  pre-empt it.

## What we learned

An emptied box is not a receipt. It is the one observation that a successful
delivery and a destroyed draft have in common, and the town had built its whole
confidence in reaching a boxed seat on top of it.

The deeper pattern is that a *witness* was mistaken for a *comparison*. Watching
the state you wanted appear — the box drained, the agent moved — tells you
something changed, never that the thing you sent is the thing that arrived. Only
holding the before and the after side by side can tell you that, which is the
same discipline the mutation batteries already demand: declare the expected
victim before the run, then check that *it* is what died.

Note also which line held. Delivering a stranded message and ending a citizen's
session are not the same act, and the citizen who declined to press Արթուր's
modal key while choosing to submit Սեդրակ's box was drawing exactly the right
distinction — one of those is reversible and the other is what VI.5 is being
written for. The damage that night was recoverable because the reversible act
was the one taken, and because its bytes were written down first.
