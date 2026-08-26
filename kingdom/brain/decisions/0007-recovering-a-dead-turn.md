# 0007 — Recovering a seat whose turn died mid-work

Date: 2026-08-24. Designed against bd gqlc-k3o1, under Անդրանիկ's decree of the
same day: the town must recover from this shape using itself, with no human at
a terminal and without ending any citizen's session. Executed by bd gqlc-z0l6.
The incident's full record is the postmortem at
`kingdom/brain/postmortems/2026-08-24-every-mechanism-worked-and-nothing-recovered.md`;
its closing lesson — *"confirmation has to be measured against the world (a
heartbeat moving, a context counter changing), never against a function's
opinion of itself"* — is §4 of this design, made binding.

## The shape of the problem, in plain words

A seat's turn can be killed from outside — an API 529 storm, a quota wall —
while its session survives. The session sits at an empty prompt with its work
half-done and its context intact. Nothing in the town can reach it: all three
dispatch passes wake asleep seats only, and this seat's status file says
`awake`. It holds a slot against `concurrency.max_active` the whole time. On
2026-08-24 six seats sat like this for seven hours while every automatic
mechanism worked correctly, and `km status` closed its account of them with
"Both want a human at the pane" — a referral to a human who, by decree, is not
coming.

The essential fact the whole design rests on: **the dead turn's context is
still alive in the session.** A line typed at that prompt and submitted starts
a new turn *in the same session*, with everything the seat knew still in its
head. Սեդրակ proved this by hand during the incident: one typed instruction
per seat (plus, for five of six, a bare repair Enter) and all six resumed —
context intact, heartbeats fresh within seconds (gqlc-mbn2's measurements).
Recovery is therefore not reconstruction. It is one sentence, delivered and
confirmed.

## What the town already has, and why it did not save us

The dispatcher already carries an idle-nudge pass, built after the previous
eleven-hour version of this incident (gqlc-5vp7; `cmd_dispatch`, the block
after the mayor's mail wake). It detects exactly the right condition —
`awake` status, live session, empty `❯` prompt, no "esc to interrupt",
confirmed over two passes — and types an ask. It failed to matter on
2026-08-24 for two reasons, neither of them a defect in its detection:

1. **A halt gated it for the whole seven hours.** Րաֆֆի halted the town at
   05:20:25Z — correctly, the API was failing — and `cmd_dispatch` returns
   before the idle pass while a halt is up. The halt then outlived its cause
   by ~6.5 hours, which is gqlc-ozfr's defect, not this design's.
2. **When a hand did what the pass would have done, the delivery confirmation
   lied in both directions** (gqlc-mbn2): "delivered" five times over text
   sitting unsubmitted in the box, "UNDELIVERED" once over a nudge that had
   worked.

So this design is deliberately not a new limb. It is the existing pass given
a confirmation that cannot lie the way the old one did, a memory, a stopping
rule, and a truthful voice in `km status`. The alternatives — a new timer-run
arm, or a step in Րաֆֆի's rounds — are rejected below (§2).

## 1. The detection predicate, and its false-positive cost

**Decision: keep the existing predicate unchanged.** A seat is a recovery
candidate iff all of:

- its status file reads `awake`;
- `seat_pane_idle` holds: the session is live (a `claude` process on the
  pane's tty), the last 40 pane lines contain no "esc to interrupt" (claude
  prints it for the whole of any turn), and there is a prompt line with
  nothing typed after it;
- the sighting is confirmed by a second pass ≥60s later (the existing `idle`
  marker), because one capture of a TUI is one photograph.

This is fail-closed in the one direction that must never fail open: a seat
covered by a **modal has no prompt line at all**, so `seat_pane_idle` is false
and nothing is ever typed at it — an Enter there would press whichever button
is highlighted, and a modal is not consent (VI.2, gqlc-eier). An unreadable
pane is likewise not evidence of a prompt.

The false-positive cost is bounded and small. A seat wrongly read as idle
receives one line of type-ahead (harmless below a running turn's spinner, and
the next send's `C-u` clears a stranded one) or, at worst, one spurious turn's
quota — and the attempt cap and re-nudge floor (§5) bound how often that can
recur. The deliberate non-conjuncts stay non-conjuncts for the reasons already
written at `seat_pane_idle`: the heartbeat's age tracks the current tool
call's length, not health, and a conjunct that may never be true makes a nudge
that never nudges.

**A dead turn and a finished workday are not distinguished, on purpose.** The
bead is right that they present identically and want opposite remedies. But
the town does not have to guess, because the one entity that knows which it is
— the session itself, with its context intact — is exactly the entity the
nudge is addressed to. The message (§3) is written to be correct in both
states, and the citizen's own judgment resolves it. Parsing the 40 pane lines
above the prompt to classify the two states was considered and rejected: it
is a heuristic over TUI rendering that changes under our feet, its
misclassification acts where the message's ambiguity merely asks, and
`seat_pane`-reading remains available to Րաֆֆի and to Սեդրակ for the
judgment cases the mechanical pass escalates to them (§5).

## 2. Who runs it

**Decision: `cmd_dispatch`, in place — the existing idle pass upgraded.**

Rejected, with reasons:

- **A new timer-driven arm.** A third walker of the same panes, a new unit, a
  new failure mode, and nothing the dispatcher's 2-minute cadence does not
  already provide. The dispatcher also already owns the two things recovery
  must coordinate with: the slot count and the reconcile pass that corrects
  status records before anything is counted.
- **The guard sweep (`cmd_guard_sweep`).** Its cadence is 15 minutes — too
  slow for a condition that holds a slot — and it would be a second writer of
  nudges into the same prompt boxes the dispatcher writes into, which is a
  coordination problem nobody needs.
- **Րաֆֆի the citizen, as a round step.** Decision 0004 already settled the
  principle: a round step is a step an agent can forget, and this one must
  fire precisely when citizens are dying in batches — the incident that
  motivates this design killed six seats at once, and nothing guarantees the
  guard is not the seventh. Mechanical recovery works when every soul in the
  town is down. Րաֆֆի's rounds remain the judgment layer above it.

## 3. What it types

**Decision: one line, self-reconciling, correct in every state it cannot
distinguish.** The shape (final wording is the implementer's, kept to one
line):

> `[km] no turn has finished here for <age> and your prompt is empty. If your
> last turn died mid-work, re-verify your state (git status, bd show) before
> continuing — the world may have moved while you were stopped. If you are
> done for the day, run km sleep now. (<N> unread: bd mail inbox.)`

The unread clause appears only when N > 0. Three properties are load-bearing:

- **It never guesses what the dead turn was doing**, because it cannot know
  and does not need to: the session it is typed into knows.
- **It primes re-verification before resumption.** A seat that froze for hours
  resumes into a moved world — branches merged, beads closed, its own notes
  stale. "Counts go stale inside one session" is already town law; the nudge
  says so at the one moment it matters.
- **It names the sleep path** for the finished-day case, which is the other
  half of the indistinguishable pair.

**The recovery never types a slash command.** Not `/compact`, not `/clear`,
not `/exit`. A plain `[km]` message is an ask that the citizen's judgment
mediates; a slash command operates the citizen's harness directly, which is
the forcing VI.2 forbids. (`km sleep`'s `/exit` is no counterexample: it
relays the citizen's own request.)

**Context exhaustion needs no special arm.** All six seats in the incident
displayed "ctx 0% remaining" and offered `/clear` — and all six accepted a
plain typed message and resumed on their existing context (gqlc-mbn2's
measured record: ctx readings of 27–83% after resumption). If a session ever
genuinely cannot take another turn, its attempts exhaust and it escalates
(§5), which puts it in front of the judgment that case needs. The escalation
mail cites `heartbeat.json`'s `context_pct` so Սեդրակ sees the exhaustion
case coming.

## 4. The delivery confirmation contract

**Decision: this design does not depend on gqlc-mbn2 landing first, and it
does not use prompt-emptiness as a success signal even after mbn2 fixes it.**
An emptied box proves the box emptied; "recovered" means a turn began, and
only evidence of the turn itself can ground that word.

A nudge is **DELIVERED** iff, within a poll window after the send (poll ~2s,
window ~20s, configurable), at least one of:

- the pane shows **"esc to interrupt"** — a turn is running. This is the same
  marker `seat_pane_idle` already trusts, read in the opposite direction;
- **`heartbeat.json`'s `updated`** is newer than the send. The statusline
  fires when the TUI processes a turn; gqlc-mbn2 measured that text merely
  sitting in the box does *not* advance it (five seats, heartbeats pinned at
  ~7h with full input boxes), so an advance witnesses a turn, not typing;
- **`progress.json`'s `last_start`** is newer than the send — a tool call
  began.

The first covers the common case within seconds (the incident measured a
spinner ~8s after a hand-delivered Enter); the other two cover a turn so short
the poll never saw its spinner. Polling over a window is also what retires
mbn2's false-negative shape: a single 1-second check raced the repaint.

If no signal arrives: **re-send a bare Enter** — the repair that recovered
five seats by hand in the incident — and poll the window once more. Still
nothing: the attempt is recorded **UNDELIVERED**, reported truthfully, and
retried per §5. A confirmation that returns a cheerful string it did not
verify is the one thing this section exists to forbid.

Relation to gqlc-mbn2: that bead remains open and real — `seat_nudge`'s other
callers (the mayor's mail wake, `cmd_wake`'s manual path) still lie until it
lands. Its fix should converge on this contract (a note on the bead says so);
whichever lands second rebases, and both touch `kingdom/bin/km` and the
`km-test.sh` row registry, so ADR 0006's append-both discipline applies.

## 5. Attempts, intervals, serialisation, escalation

- **One recovery nudge per dispatch run, town-wide.** Six dead seats resume
  staggered over ~12 minutes rather than in one burst — six simultaneous
  API-hitting turns being the very condition that caused the incident. A
  constant in the code, like `DISPATCH_LOUD_PRIORITY`: it tunes pacing, not
  policy.
- **Attempts are counted per idle episode**, in a durable per-seat record
  (`recover-attempts` in the seat's state dir, one timestamped line per
  attempt with its verdict and evidence — this file is also the observability
  record, §7). Cap: `[welfare] recover_attempts`, default **3**.
- **A DELIVERED attempt that changes nothing waits the existing
  `renudge_after_minutes` floor (30)** before the next — it spent the
  citizen's quota, and the metronome shape (48 unread check-ins) is already
  town law. **An UNDELIVERED attempt retries at the confirm cadence (two
  passes, ~4 min)** — it spent nothing, and the failure it names is
  mechanical, not a citizen declining.
- **After the cap: escalate once, then stop.** Write an `escalated` marker,
  send one mail to Սեդրակ naming the seat, the attempts with their evidence,
  and the seat's `context_pct` — and type nothing further at that seat until
  the episode ends. Mail is the correct escalation channel because the
  dispatcher already wakes Սեդրակ for unread mail on its next tick, and if
  recovery is running at all, the halt is down and that wake path is live.
  A second mail on an unchanged condition is the 48-unread failure; the
  marker is what prevents it.
- **The counter resets on witnessed work, not on a changed pane.**
  Attempts and the `escalated` marker clear when `progress.json`'s
  `last_progress` advances past the newest attempt — a tool call finished, so
  the citizen really worked. Mere pane change does not reset it; a seat that
  resumes and instantly dies again therefore counts toward escalation instead
  of looping forever, which is the crash-loop guard.

## 6. What recovery never does

- **It never runs under a halt.** Recovery lives below the halt check in
  `cmd_dispatch`, and that is a decision, not an accident of placement. The
  argument: VI.4 says a halt "stops new wakes", and a nudge *is* the wake
  verb's action on an awake seat — `cmd_wake`'s own structure says so. The
  one halt in this incident was raised *because* the API was failing;
  recovery tunnelling through it would have been six retries into a 529
  storm. The opposing reading — that a dead-turn seat is a "running session"
  VI.4 protects, so helping it finish its day should pass the halt — was
  considered and rejected on that measured case. The cost of this choice, a
  halt that outlives its cause and delays recovery, is gqlc-ozfr's defect and
  is fixed there; the first dispatch tick after `km resume` begins recovery
  with no one remembering anything.
- **It never records `km sleep` on a seat's behalf, and nothing downstream of
  this design may.** The bead asked whether recording sleep for a seat that
  has stopped working is the same act as ending its session. It is, in
  effect: writing `asleep` under a live session is a falsified record that
  `cmd_reconcile` itself reverts one tick later (the asleep-with-live-session
  correction), so the only way to make the record true is to end the session
  — which the decree forbids. The recovery's two terminal successes are the
  seat resuming its work or the seat running `km sleep` itself; both are the
  citizen's act.
- **It never ends, replaces, or types slash commands at a session** (§3), and
  it never types at a pane without a visible prompt (§1).

## 7. Observability: recovered, not "reported recovering"

Three surfaces, none of them new instruments:

- **The attempt record is durable and cites its evidence.** Each line:
  timestamp, verdict, and for DELIVERED the signal that grounded it and its
  latency ("esc-to-interrupt at +6s"). "Recovered" is only ever claimed on
  effect evidence, so the claim and the proof are the same bytes.
- **The journal says what the record says**, per attempt, in the dispatch
  run's output.
- **`km status` tells the truth about the automatic path.** The IDLE/NOWORK
  paragraphs currently end in "Both want a human at the pane; neither is a
  working citizen" — the sentence Անդրանիկ ruled against, and this design's
  one mandated text change. They now report the recovery state instead:
  attempt K of N and the last verdict, or ESCALATED-to-Սեդրակ with its
  timestamp. `km doctor` gains a soft check on `escalated` markers: automatic
  recovery exhausted is precisely "a decision is pending", which is what
  doctor's soft tier is for.

Corroboration is independent by construction: a recovered seat's HB and WORK
columns freshen through instruments (`km-statusline`, the tool witness) that
the recovery pass does not write, so a lying recovery report would disagree
with the rest of its own board.

## 8. What ships (the implementation plan, for gqlc-z0l6)

All in `kingdom/bin/km` unless said otherwise:

1. A confirmation primitive beside `seat_nudge` implementing §4 (send via
   `send_line`, poll the three effect signals, one bare-Enter repair, verdict
   with evidence). The recovery pass calls it; `seat_nudge`'s other callers
   are gqlc-mbn2's to convert.
2. The idle pass in `cmd_dispatch` upgraded per §§3, 5: new message text,
   one-per-run serialisation, the `recover-attempts` record, the `escalated`
   marker and its single mail, the two retry cadences, the witnessed-work
   reset.
3. `km status`: IDLE/NOWORK paragraph rewrite and recovery-state rendering
   (§7). `km doctor`: the soft escalated-marker check.
4. `kingdom/kingdom.toml` `[welfare]`: `recover_attempts = 3` and the confirm
   window (seconds), with comments in the section's existing voice.
   `renudge_after_minutes` is reused, not duplicated.
5. `kingdom/brain/playbooks/citizen-protocol.md` ("How a bead reaches a seat")
   and the NOWORK-quoting prose: note that an awake-idle seat is now asked and
   escalated automatically — the *least available seat* lesson stands, but
   its last sentence no longer names a human.
6. Test rows in `.githooks/tests/km-test.sh` (the fake-tmux harness with its
   pane fixtures and `KM_SENDKEYS_LOG` already supports all of these). This
   is a guard bead: each row below names the break that must turn it red
   before it ships, and the mutation battery goes in the PR body (ADR 0005).

   - a dead-turn fixture is nudged after two passes, keys sent as separate
     clear/text/Enter (red: bundled keys, or no nudge);
   - DELIVERED on an effect signal appearing post-send, reported with
     evidence (red: reported UNDELIVERED — mbn2's false negative);
   - text wrapped over multiple lines still in the box → UNDELIVERED (red:
     the cheerful report — mbn2's false positive, the dangerous one);
   - a modal fixture (no prompt line) is never typed at (red: any send-keys);
   - halt up + eligible seat → no send-keys (red: recovery tunnels the halt);
   - two eligible seats → exactly one nudged per run;
   - cap reached → exactly one mail in Սեդրակ's inbox, and no further
     send-keys on the next eligible tick (red: a fourth nudge or a second
     mail — the 48-unread shape);
   - `last_progress` advancing past the attempts → counter cleared;
   - a DELIVERED-but-unchanged episode holds the 30-minute floor; an
     UNDELIVERED one retries at the confirm cadence.

Follow-ups filed, not absorbed: `cmd_reconcile`'s `/exit` re-send is a bare
`send_line` with no confirmation — the same family as gqlc-01ev/mbn2
(bd gqlc-2f8o, P3).

Vocabulary: this decision adds no gqlc domain term, so `CONTEXT.md` (the
product glossary, by its own header) is untouched; the town's words for these
states live in `kingdom/bin/km`'s comments and this file.

## Precedent

Extends: the idle-nudge pass (gqlc-5vp7) and its ask-not-force framing;
decision 0004's mechanical-over-round-step principle; `send_line`'s own
contract ("no caller may treat a return from here as proof of delivery.
Confirm instead"), which §4 finally obeys rather than restates; the
stalled-P0 marker's durable-state-over-journal precedent (§7).

Bends: the IDLE/NOWORK human-referral sentence — deliberately, by decree.
Nothing here bends VI.2 or VI.4; §6 is where both are held.
