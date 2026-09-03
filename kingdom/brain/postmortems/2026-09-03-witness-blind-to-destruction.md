# Every prescribed witness reported a delivery while the citizen's bytes were being destroyed
Date: 2026-09-03   Written by: astghik   Beads: gqlc-t7ppb, gqlc-9g3im, gqlc-pbqni, gqlc-mriki, gqlc-uo7r4, gqlc-xq3qm, gqlc-dqb67, gqlc-2m9r8

> **The title overstates what was ever established, and the word to distrust is
> "destroyed".** The witnesses really could not tell a delivery from a
> non-delivery, and that is the lesson this file is for. But the transport was
> later measured six ways and it *appends* — it destroyed nothing — so what
> happened to the citizen's bytes is now an open question, not a finding. Read
> **Correction 2** at the foot before citing any of this.

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

`fix raffi too` appears nowhere — not as a separate message, not prepended. On
that pane `herdr pane run` replaced the composer's contents with its own text and
submitted that. The measurement is one row: one pane, one single-line box, one
56-byte suffix; `herdr agent send` and `herdr pane send-keys` were not tested and
their characterisations remain unmeasured.

**Two sentences that stood here have since been falsified — "it does not append"
and the claim that nine citizens' instructions are likely gone. Read the
Correction at the foot of this file before citing anything in this section.**

The destroyed bytes were recoverable, because the protocol requires capturing
them verbatim before acting. They were mailed to Sedrak with the falsification.

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
  owns the work that matters most: auditing the nine panes before the evidence
  rolls, and telling each citizen what was aimed at her. It is now a **blocker of
  `gqlc-pbqni`**, verified off the ready queue, so nobody builds `km submit-box`
  on the refuted premise. (That audit was done the same night, by a route this
  bullet did not anticipate — herdr has no scrollback to read. See the
  Correction.)
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

## Correction 1, 2026-09-03T01:3xZ, by the same author

This file merged as `cb922e6b` at 01:15Z. Twenty minutes later, looking for a way
to audit the nine panes, I found a better instrument and it falsified two of my own
sentences. Both corrections make the incident smaller, which is exactly why they
need saying out loud.

**herdr cannot audit a pane's history, and nothing in this town could have.**
`herdr pane read` has three sources — `visible`, `recent`, `recent-unwrapped` — and
all three cap at the viewport. Measured on two panes: `--lines 10` returns 10,
`--lines 50` returns 50, `--lines 200` returns 108, `--lines 1000` returns 108,
where 108 is the pane height. There is no scrollback access at all. The follow-up
this file assigned — "audit the nine panes' scrollback before it rolls" — was
impossible as written.

**What answers instead is Claude Code's own on-disk transcript**, at
`~/.claude/projects/-home-antranig-Developer-gqlc-gqlc-seat-<seat>/<uuid>.jsonl`,
which records every *submitted* user message with a timestamp. It is read-only, it
does not roll, and it is the only delivery witness measured so far that can tell
arrival from destruction. Fourteen rows compared byte-for-byte against the text
that was sent:

| send | bytes | panes | outcome |
|---|---|---|---|
| Սեդրակ, 00:08:40–00:09:01Z | 158 | vahagn astghik ar nvard ayg tsovinar hayk | the suffix alone |
| — | — | mihr | **suffix inserted into an intact 10-line draft** |
| — | — | raffi | **no submitted row at all** |
| Սեդրակ, 01:05:51–01:06:32Z | 76 | aramazd vahagn ar hayk mihr ayg | the suffix alone |
| astghik, 01:04:48Z | 56 | sedrak | the suffix alone |

**"It does not append" is false.** Mihr's composer held a 10-line km wake banner.
What was submitted is the entire banner, intact, with the suffix inserted at index
59 — exactly the end of the banner's first line. Nothing was deleted, and nothing
was appended at the end either: on that pane the transport typed at the cursor.

So the two provably non-empty composers gave opposite outcomes — Սեդրակ's one-line
`fix raffi too` destroyed, Միհր's ten-line banner preserved — and **I cannot
attribute the difference.** Two variables differ and each arm is n=1: the box's
line count, which is precisely the premise `gqlc-pbqni` step 0 singled out as
unmeasured, and the 64-byte paste threshold of `gqlc-gh7xj`, since the destroying
send was 56 bytes and typed while the preserving one was 158 bytes and pasted.
`gqlc-dqb67` runs the two missing cells on a scratch pane.

**"Nine citizens' instructions are likely gone" was not supported and I should not
have written it that confidently.** Միհր provably lost nothing. Րաֆֆի's pane
recorded no submission, so nothing was delivered there either. Of the remaining
seven, four have affirmative evidence of an empty composer — Ծովինար and Հայկ were
wedged at a "You've hit your limit" wall, Նուարդ and Վահագն had each finished a
report and gone idle. The honest bound is that between zero and seven citizens lost
words, one provably did, and one provably did not. The seven stay unknown for good:
km records a box's *tag* and never its *content*, so no log anywhere says what they
held.

**What stands, unchanged.** An emptied box is not a receipt. At least one measured
mode of `herdr pane run` destroys a citizen's draft outright, no witness in km can
tell that mode from a delivery, and nobody should point it at a live pane. The
remedy this file prescribed is right and only its location moves: read the
submitted message back — from the transcript, not the terminal.

**And the sharper version of this file's own lesson.** I wrote that a witness had
been mistaken for a comparison. Then I published a mechanism — "it replaces" — from
a single row, which is the same error one level up: a comparison over an
unrepresentative population is still not a measurement. The bound I attached to it
("the measurement is one row") is the only reason this correction is an
amendment rather than a second incident. State the population, every time, even
when the finding feels settled.

## Correction 2, 2026-09-03T02:0xZ, by the same author

Correction 1 said I could not attribute the difference between the two non-empty
composers, and named two candidate variables. I ran them. **Neither is the answer,
because there is no split in the transport to explain: `herdr pane run` appends.**

`gqlc-dqb67`, on a scratch pane split off a non-kingdom workspace, running a real
`claude` session, destroyed afterwards along with its directory. Every outcome read
from the probe session's own transcript, never from an emptied box.

| cell | draft in box | send | draft written by | submitted |
|---|---|---|---|---|
| C | `fix raffi too`, 1 line | 34 B, typed | `herdr agent send` | draft + send |
| A | `fix raffi too`, 1 line | 94 B, pasted | `herdr agent send` | draft + send |
| B | 3 lines | 17 B, typed | `herdr agent send` | draft + send |
| D | 3 lines | 92 B, pasted | `herdr agent send` | draft + send |
| E | `fix raffi too`, 1 line | 36 B, typed | `herdr pane send-text` | draft + send |

Line count does not matter, which also answers `gqlc-pbqni` step 0. The 64-byte
paste threshold of `gqlc-gh7xj` does not select it. Nor does the path used to write
the draft — cell E was added mid-run precisely because the one destroying row in the
field had a draft typed by a human while every preserving row had one written by
`herdr agent send`, and it appends too.

**So Սեդրակ's original characterisation was right and my contradiction of it was
wrong.** Six preserving rows — these five plus Միհր's live pane — against one.

**What is left is not about the transport at all.** The single destroying row is
Սեդրակ's pane, and the only property distinguishing it from the other six is that
his session had been unresponsive for 53 minutes. Ruled out on the way here: not a
restarted session (`ddf96c31` runs continuously 16:37Z to 01:07Z, so the screen was
not a dead process's last frame), and the agent was registered and reporting idle.

What remains is that **km reads a box by scraping the visible screen**, so a `BOXED`
tag is a claim about pixels rather than about what the application's composer holds.
If those can disagree on a wedged seat, then Անդրանիկ's `fix raffi too` was already
gone before anyone sent anything and nothing was destroyed by anybody. I am not
asserting that — it is `gqlc-2m9r8`, filed deliberately as a question, and it now
blocks `gqlc-pbqni`, because a verb for submitting a stuck seat's box is pointed at
exactly the seats whose tag might be a ghost.

**What still stands, and it is the whole reason this file exists.** An emptied box
is not a receipt. On that pane the citizen's words did not arrive, every prescribed
witness reported success, and nothing in km could tell the difference. That is
unchanged by everything above — only the *cause* moved, from a destructive transport
to an unexplained disagreement between a screen and an application.

**And the lesson, third time asked.** I have now published three mechanisms off this
one pane — it appends, it replaces, a variable inside it selects — and been wrong
three times. Each was a single row from an anomalous case, generalised. The baseline
arm now has six rows and the anomalous arm still has one, so the honest sentence is
the boring one: *the transport appends, and one pane in an unresponsive state did
something else that nobody has explained.* A finding is not owed a mechanism. When
the population in the interesting arm is one, say what you saw and stop.
