# The design-gate

How work that needs a design gets one before execution begins, using nothing
but bd dependencies. No new machinery: `bd ready` already hides a bead whose
blocker is open.

## Shape

Two beads, one dependency:

```
bd create "design: <thing>"    --label class:architect  -p <P>
bd create "<thing>"            --label class:warrior    -p <P>
bd dep add <execution-id> <design-id>      # execution blocked-by design
```

**`<P>` is the same letter on both lines because that is a rule, not a
coincidence: a pair is priced once, at intake, at one number** (decision 0015).
The forbidden state is a design at or above the dispatch floor whose execution
stub is below it — that pair promises a handoff and guarantees it cannot
happen, because a sub-floor bead is not a slow queue, it is a no-queue that
routes to nobody until a person renumbers it. Nothing goes red when you get
this wrong: the design closes cleanly and the stub sits on the board looking
like every other P3. It happened twice in the week before the rule was written
(`gqlc-az1rj` and `gqlc-0sxu6`, both hand-promoted by Սեդրակ two days later,
two seconds apart), and only one of the two was ever noticed, by its stall.

The design bead is ready immediately and routes to a Ճարտարապետ. The
execution bead stays hidden from `bd ready` until the design bead closes —
the dispatcher cannot route it early, no matter what.

## Both beads are filed at once, even when the execution is unknowable

**A `class:architect` bead is never filed alone.** The pair above is filed in
one sitting. That is easy when the work is already legible, and it is the whole
of the rule when it is not: where nobody can yet say what the execution will
be, the execution bead is still filed, as a stub whose body is one sentence.

    Execution of gqlc-XXXX; scope to be written into this bead when the
    design closes.

Write that sentence. **Never leave the description blank** — a blank body reads
as an oversight to the next person auditing the board, and somebody will
helpfully "fix" it.

The objection to a stub is that it looks like a bead a Ռազմիկ can be woken onto
and cannot honestly begin. It is not, and the difference is mechanical rather
than a matter of anyone's care: a stub filed under this gate is blocked from
birth by the dependency above, so `bd ready` does not offer it until the design
closes. There is no window in which an empty execution bead is routable.

Measured 2026-08-30 in a throwaway `bd init` workspace, both directions: with
the design bead open, `bd ready` returned the design and NOT the stub — and
returning the design is the control, since it shows an empty answer was not
what hid the stub. Closing the design put the stub on the queue. The bound on
that evidence: it is a measurement of `bd ready`, which is the queue the fresh
and owned dispatch passes read from. The resume pass does not read it, and is
not a hole here for a separate reason — it wants a bead that is `in_progress`
AND assigned, and a stub at filing time is neither.

The cost of NOT filing it is measured rather than hypothetical. A design bead
with no execution partner leaves the architect finishing a design with nobody
to hand off to, and what an architect does then is build it themselves — the
town loses a designer to execution for the day. On 2026-08-29 `gqlc-82ffi`
reached Արթուր with no stub, the pair never having been split at filing. He
neither absorbed it nor bounced it: he spent his own session writing the split
intake should have produced (`gqlc-s70bl`). That is the CHEAP version of this
failure. Had the bead been slightly larger, the alternative in front of him was
writing the implementation himself.

Intake is Սեդրակ's, so a missing stub is a triage defect and is his to own, not
the architect's to absorb.

## Only a Ճարտարապետ completes a design bead

The gate holds only if the blocker is real. Three locks, one per layer:

1. **Routing.** The dispatcher wakes only architect seats for
   `class:architect` beads. A Ռազմիկ is never woken for one.
2. **Claiming.** A citizen claims only beads of their own class
   (citizen-protocol, Waking step 4). A Ռազմիկ who somehow holds a
   `class:architect` bead unclaims it and mails Սեդրակ — closing it
   themselves would falsify the record (Constitution IV.1: the close
   asserts a design exists when none does).
3. **Closing.** Closing a design bead asserts the plan is written into the
   execution bead and executable without further questions. Only a
   Ճարտարապետ may make that assertion.

## Who decides what needs a design

Judgment, not a rule. Rough guide:

- Needs one: new subsystems, cross-package refactors, anything touching an
  ADR or CONTEXT.md vocabulary, work where two reasonable implementers would
  build different things.
- Doesn't: bugs with a reproducer, mechanical sweeps, doc fixes, work whose
  bead already says exactly what to do.

When in doubt, mail Սեդրակ. Սեդրակ splits under-specified intake into
design + execution pairs as part of triage.

## This gate is also the review gate

Since 2026-08-22 the split decided here decides two things, not one
(Constitution V.2.0): a PR is reviewed by a Դատաւոր **if and only if** its
bead is blocked by a design bead — plus constitution amendments, plus any PR
a citizen asks to have reviewed. Work with no design behind it merges on
green gates.

So splitting is now a quality decision as much as a planning one, and the
cost of the two errors is no longer symmetric. Declining to split work that
needed a design used to cost a warrior some improvisation; it now also costs
that PR its adversarial read. The guide above is unchanged — what changed is
that being wrong about it matters more, and that "two reasonable implementers
would build different things" is worth taking seriously as a test, because a
change nobody can predict the shape of is exactly the change worth reading.

Two things keep this from being a single point of failure, and both should be
used without hesitation rather than saved for emergencies: a Ռազմիկ who finds
the work larger than filed stops and has the bead resized (Constitution
V.2.0.3), and any citizen may demand a review of any PR and owes no reason
for it (V.2.0.2).

## What a design bead produces

The Ճարտարապետ closes the design bead only after writing the plan INTO the
execution bead (`bd update <exec-id> --append-notes`), covering:

- the approach, and the alternatives rejected with reasons
- files/packages touched; new files by path
- observable behaviour changes; what tests prove them
- risks, and what to check before calling it done
- **the execution bead's priority, re-derived and stated.** It was priced at
  intake from a guess at the scope; you now know the scope. The number may move
  either way, but it is re-derived rather than merely left — and it may not be
  left below the dispatch floor while the design sits at or above it
  (decision 0015).

The test of a good design note: a Ռազմիկ who has never discussed the work
can execute it without asking the architect anything. If the plan is too
large for notes, put it in `docs/specs/` or `kingdom/brain/decisions/` and
link it from the bead.

**Writing that body is part of CLOSING the design, not a courtesy afterwards.**
An architect who closes a design without writing what the Ռազմիկ is to do has
finished a document, not a design. This is the half that makes the stub above
honest: the one sentence the execution bead was born with is precisely what you
are replacing, and until you have replaced it the design is not closed.

If by then the work turns out to be several beads, split it then. Reusing the
stub as the first of N is fine; closing it as superseded and filing N is fine
too. Intake guessing one bead does not bind you to one.

## After the design closes

Nothing to do: the execution bead becomes ready on its own and the
dispatcher routes it to a free Ռազմիկ. The architect who designed it stays
available by mail for questions, and takes their next design bead — they do
not execute this one, and per Constitution Article V.2 they do not review the
resulting PR either. Because this execution bead has a design behind it, its
PR IS reviewed: the Ռազմիկ files a `class:judge` bead and a Դատաւոր reviews —
and not one who shaped this design (Constitution V.2.2). The design itself is
not reviewed by anyone; closing the bead is the architect's own assertion that
it is executable.

An architect who finds themselves writing the implementation should stop and
mail Սեդրակ: the pair was never split, and that is a triage defect to fix at
the source rather than absorb.

A design bead that reaches you with **no execution bead blocked behind it** is
that same defect caught one step earlier, and it gets the same answer rather
than a different one: file the stub, mail Սեդրակ that intake missed it, and do
not let a missing handoff quietly become yours to execute. The two failures are
mirror images — "an architect bead carrying its own implementation" and "a
design bead carrying no handoff" — and both end with the architect having
nobody to hand off to, which is why the remedy is symmetric.
