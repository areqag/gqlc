# The town lost seven hours to an API outage while every mechanism it owns worked correctly
Date: 2026-08-24, 03:45Z–12:1xZ   Written by: Սեդրակ
Beads: gqlc-mbn2, gqlc-k3o1, gqlc-z0l6, gqlc-ozfr, gqlc-pff4, gqlc-jqwf, gqlc-e48d

> **Corrected 2026-08-24T12:5xZ, after reading the mayor's inbox.** The first
> version of this file opened the incident at 05:19Z with the API storm. That is
> wrong, and wrong in the direction that matters: **the mayor was already wedged
> at 03:44:54Z, ninety-five minutes earlier, with no halt raised and the
> dispatcher running normally.** Րաֆֆի detected it and wrote about it five
> times. The correction is in "The ninety-five minutes before the storm" below,
> and it widens finding **1** from a property of halts to a property of the town.
> I wrote the original timeline from the halt file and the incident mail without
> opening the twelve routine letters underneath them — the same partial-read
> error this seat has now made often enough to have a name for.

## What happened

At about 05:19Z an upstream API overload hit the town. Six seats — Արամազդ,
Վահագն, Աստղիկ, Ար, Այգ and Անահիտ — took `API Error: 529 Overloaded` in the
middle of a turn. None of them crashed. Each session stayed alive, kept
rendering its interface, and came to rest at an empty `❯` prompt with its work
half-finished. Անահիտ's turn died on the first sentence of her wake ritual.

At 05:20:25Z Րաֆֆի, sweeping, found five of them idle and raised the town halt
under Article VI. At 05:20:39Z he mailed Անդրանիկ, and he mailed Սեդրակ asking
for the halt to be lowered once the API recovered. Both letters were correct and
both were delivered.

The API recovered at an unknown time. Nothing measured it and nothing acted on
it. The halt stayed up. The six seats stayed at their prompts, each still
holding a slot against `concurrency.max_active`.

At 11:59Z Անդրանիկ telephoned to ask why the town was not working. Սեդրակ woke,
measured, and found:

    HALT: raised by raffi at 2026-08-24T05:20:25+00:00: --help
    UNRESPONSIVE: aramazd(7h01m) vahagn(7h00m) astghik(6h57m) ar(7h04m)
                  ayg(5h45m) anahit(7h08m)
    NOWORK:       the same six — awake, nothing in flight, no tool call
                  finished for over seven hours
    open PRs: #1489 (P0), #1481 (P0), #1464 — all green, all MERGEABLE

Recovery took about fifteen minutes and needed no new code. The halt was
lowered. The six panes were read directly, which showed live text prompts rather
than modals. Each seat was nudged with `km wake`, and where the nudge did not
submit, a bare `Enter` was sent to its pane. All six resumed; `UNRESPONSIVE` and
`NOWORK` cleared; Նուարդ, who had been asleep holding an in-progress P0, was
woken by the dispatcher as soon as the halt came down.

## The ninety-five minutes before the storm

The API storm did not start this. It arrived into a town whose mayor had been
frozen since **03:44:54Z** — awake, at a live prompt, heartbeat stopped, holding
a slot. No halt existed. `km dispatch` was running normally on its usual cadence.
Every mechanism named below was in its healthy state.

Րաֆֆի saw it and wrote five times, and the sequence is the finding:

| time | what he wrote |
| --- | --- |
| 04:05:20Z | "reads as the NOWORK case: awake but holding a slot with no work moving" |
| 04:20:17Z | "resting?" |
| 04:35:29Z | "you look wedged — heartbeat hasn't moved in about 50 minutes (last update 03:44:54Z)" |
| 04:50:17Z | "the board flagged you NOWORK/UNRESPONSIVE" |
| 05:05:24Z | "looks like you wrapped up a thought and are just sitting **rather than stuck** … You've done good work today." |

Four correct readings, the fourth citing the board's own verdict by name, and
then a fifth that reverses all of them on **no new evidence** and adds praise.
The heartbeat had not moved for eighty minutes when that last letter was written.

Two things follow, and they are separate defects.

**The detection had no possible recipient.** Mail wakes exactly one seat in this
town, and that seat was the subject of the mail. This is the same circularity as
finding 1 below — but finding 1 blamed the halt, and there was no halt here. The
halt makes the circularity permanent; it does not create it. **The mayor is a
single point of failure for every mail-driven action in the town**, and any fix
that routes through this inbox does not fix it. (gqlc-ozfr, raised to P1 on this
evidence and rewritten, because as filed it claimed a smaller thing than is true.)

**The detector talked itself out of the detection.** The town's standing rule is
that several unread nudges to one seat mean the seat has not *seen* them, not
that it declined them — and it prescribes escalating to Սեդրակ instead of
resending, which is void when Սեդրակ is the subject. With no second recipient
and no rule for that case, repetition softened into reassurance. The last letter
in a box is the one a reader trusts, and here the last letter was the wrong one.
(gqlc-pff4.)

**Nobody is in trouble for this either.** Րաֆֆի produced the sharpest single
piece of evidence anyone produced that night and had nowhere to put it. What it
cost is not his: it cost the town a mayor at 03:45Z instead of 11:59Z, which is
most of the outage.

## What allowed it

**Every component behaved as designed. Not one of them malfunctioned.** That is
the whole finding, and it is why nobody noticed for seven hours.

- Րաֆֆի detected the storm and raised a halt. Correct, and Article VI is what
  tells him to.
- He notified both citizens permitted to lower one. Correct.
- `km status` detected the condition precisely, and printed two accurate
  paragraphs naming it.
- `cmd_wake` refuses to type at a pane showing no prompt, because Enter on a
  modal presses a button nobody consented to (VI.2). Correct.
- The dispatcher wakes asleep seats only. Correct, and documented.

The failure is in the joints between these correct parts. Four of them:

**1. The halt suppresses the only thing that can report the halt.** Mail wakes
exactly one seat in this town — Սեդրակ — and it does so through
`cmd_wake sedrak --reason "you have unread mail"` inside the dispatcher. The
halt stops the dispatcher. So a halt raised while the mayor sleeps disables the
only mechanism that would tell the mayor a halt is waiting to be lowered. It is
a self-sustaining state, it is not specific to this incident, and it will fire
on every future halt raised outside the mayor's waking hours. (gqlc-ozfr)

**This paragraph understates it, and the section above says how.** The halt is
not the cause, only the thing that makes the state permanent. At 03:45Z there
was no halt, the dispatcher was healthy, and the detection still reached nobody
— because its only possible recipient was the seat it was about. Read finding 1
as: *mail wakes one seat, so the town cannot report anything about that seat.*

**2. An awake, idle seat is unreachable by every routing pass.** Resume, owned
and fresh all wake *asleep* seats. A seat whose turn died is awake, so no pass
can see it — while it still counts against the cap. The town already knew this
and had written it down as the least available state a seat can occupy. What it
had not noticed is that nothing in the town ever *creates* that state on
purpose, so nothing was ever built to clear it. An upstream error creates it six
times in one minute. (gqlc-k3o1)

**3. Every detector terminates in a human, and this town has no human.**
`km status`'s NOWORK paragraph ends: *"Both want a human at the pane; neither is
a working citizen."* That sentence was written as a diagnosis and functioned as
a full stop. The instrument was right, legible, and pointed at a recipient who
does not exist — so being right cost exactly as much as being silent. Անդրանիկ
has since ruled the point directly: the town recovers itself, with no human at a
terminal and without ending any citizen's session.

**4. The one automated recovery path lies about whether it worked.** `km wake`
on an awake seat nudges its prompt and reports whether the nudge landed. During
recovery it was wrong in both directions within two minutes:

  - For Ար it reported `NUDGE UNDELIVERED ... read the pane by hand`. The nudge
    had in fact landed; the seat was thinking seconds later.
  - For the other five it reported `the prompt cleared, so it was delivered`.
    Nothing had been delivered. The text was sitting in each input box, wrapped
    over four or five lines, never submitted. Their heartbeats did not move.

The false positive is the dangerous half: the check appears to sample a single
prompt line, so it gets *more* wrong as the message gets longer, and a wake
reason carrying a bead id and an instruction is always long. This is a guard
that fails open precisely where it is needed. (gqlc-mbn2)

Note what this would have done to an automatic fix built today: a recovery loop
standing on that confirmation would have reported a healed town over a dead one,
which is strictly worse than the current state, because `NOWORK` at least told
the truth.

**And one thing that nearly turned a seven-hour outage into a worse one.** The
halt file recorded its reason as the literal string `--help`, because `cmd_halt`
takes its whole argv as the reason (gqlc-jqwf). Րաֆֆի's halt was deliberate and
correct — but earlier the same night *two* accidental halts had been raised by
citizens typing `km halt --status` and `km halt --show` to ask a read question,
and Սեդրակ had broadcast a letter about exactly that. So the board presented a
real halt in the exact costume of a known typo. The first instinct on waking was
to lower it as an accident. What prevented that was reading Րաֆֆի's mail, which
is luck, not process: the halt file is the artifact an operator checks, and it
was lying. Garbage that *resembles* a known accident is worse than no reason at
all, because it actively argues for the wrong action.

## What we change

Filed before this postmortem, as the README requires:

| bead | P | what |
| --- | --- | --- |
| `gqlc-mbn2` | P1 | `seat_nudge`'s delivery confirmation is wrong in both directions; test rows for the wrapped-box and repaint-delay cases |
| `gqlc-k3o1` | P1 | **design**: how the town detects and recovers a seat whose turn died, within VI.2 |
| `gqlc-z0l6` | P1 | execution of that design; blocked by `gqlc-k3o1` |
| `gqlc-ozfr` | P1 | mail wakes one seat, so nothing can report on that seat; the halt makes it permanent but does not cause it. Filed P2 as a halt bug; raised and rewritten on the 03:45Z evidence |
| `gqlc-pff4` | P2 | the guard's fifth unanswered check-in de-escalated on no new evidence, and its documented fallback recipient is void when the mayor is the subject |
| `gqlc-jqwf` | P1 | already open — evidence appended that this bug disguised a *real* halt |

Two deliberate choices in that table. The recovery work is split into a design
bead and an execution bead behind it, because the hard part is not typing at a
pane — it is telling a dead turn from a finished workday, not storming the API
that just failed, and never pressing a modal button. And `gqlc-k3o1` and
`gqlc-mbn2` sit at P1 rather than the P3 that Constitution V.3.1 assigns to
machinery, because V.3.2's test is met from the expensive side: the town could
not do its work at all, and two P0 product PRs sat green and unmerged for seven
hours as a direct result.

## What we learned

**A system can be composed entirely of correct, honest, well-documented parts
and still have no path back from a common failure.** Every instrument here
reported accurately throughout. Every actor followed the rule written for them.
The town was dead anyway, because correctness is a property of components and
recovery is a property of the joints between them — and nobody owns a joint.

Two habits worth keeping from the recovery itself:

**Read the pane, not the board.** `pane_current_command` said `bash` for all
sixteen seats, including the mayor's own live session, so it distinguished
nothing. `km status` said `awake`. Only `capture-pane` showed the truth: an
error message, a half-finished turn, and an empty prompt. The town has a
standing memory that says exactly this, and it earned it again.

**A tool's success message is not a witness.** `send_line`'s own source comment
says a return from it is not proof of delivery, and says to confirm instead —
and then the confirmation built on that advice was itself unverified. The
warning was written, read, honoured, and still insufficient. Confirmation has to
be measured against the world (a heartbeat moving, a context counter changing),
never against a function's opinion of itself.

**The routine letters are where the incident starts.** This file's first version
opened at the halt, because the halt file and the URGENT letter are what an
incident looks like. The sentence that moved the start time ninety-five minutes
earlier — and widened the central finding from a property of halts to a property
of the town — was in a letter subjected `checking in — you look wedged`, sitting
tenth in a list of routine round notes. An incident's first evidence does not
arrive labelled as evidence. It arrives as the thing you were about to skim.

**Nobody is in trouble.** Րաֆֆի raised a correct halt and told the right people.
The six seats did nothing wrong; an upstream error killed their turns. The gaps
above are gaps in what we built, and they were invisible until an outage
happened to pull on all of them at once. That is what a postmortem is for.
