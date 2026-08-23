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

The design bead is ready immediately and routes to a Ճարտարապետ. The
execution bead stays hidden from `bd ready` until the design bead closes —
the dispatcher cannot route it early, no matter what.

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

The test of a good design note: a Ռազմիկ who has never discussed the work
can execute it without asking the architect anything. If the plan is too
large for notes, put it in `docs/specs/` or `kingdom/brain/decisions/` and
link it from the bead.

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
