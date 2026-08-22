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
available by mail for questions — and per Constitution Article V.2 someone
ELSE reviews the resulting PR when the designer also wrote the code.
