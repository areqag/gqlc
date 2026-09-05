# 0016 — An idle seat sleeps, and a waiting seat files its wait

Status: accepted
Date: 2026-09-04
Bead: gqlc-b5x22 (design) · gqlc-7ninj (implementation)
Written by: Հայկ, executing Արթուր's design

## The state this is about

A seat awake at an empty prompt is the worst state a seat can occupy: paid for
and unreachable.

Both halves are mechanical. The slot accounting counts LIVE SESSIONS on capped
seats, not statuses — so an idle seat spends one of `[concurrency] max_active`
exactly as an working one does. And every routing pass wakes ASLEEP seats only,
so no pass can reach her to give her the work that would justify the slot. She
is charged for and skipped, and nothing anywhere is red.

Measured over the 48h before this design: 72 dispatch passes printed `no free
slot`, 10 of 10 capped slots were held, and 13 of 16 seats were awake, most of
them idle.

Sleeping is not absence. **It is what makes a citizen reachable.** That sentence
is the whole of the reframe, and every ruling below is a consequence of it.

## Ruling 1 — an idle seat sleeps, and the SEAT does it

Self-sleep at protocol end remains the only sleep. `km` never sleeps, exits, or
ends a seat on a timer.

That is not conservatism, and it is worth writing down why, because a future
designer will re-derive auto-sleep as the obvious fix. It is unbuildable
safely: a seat waiting on a harness-internal continuation — a background task,
a scheduled wakeup — is externally **indistinguishable** from an abandoned one,
and `/exit` destroys the continuation. Only the seat can attest such a wait.
gqlc-971s already ruled that reconcile may not end an awake-alive seat, and
Article VI.2 is a genuine safety property; no evidence here bends either.

So the push mechanism stays what it is: the recovery ladder's ask, which
already commands `km sleep` and already says *carry on — this is a check-in,
not a verdict*. This decision sharpens words. It adds no actor.

## Ruling 2 — abolish "idle-awaiting-mail": a waiting seat FILES ITS WAIT

A seat who needs another citizen's answer before her held bead can move does
not sit awake waiting for a letter. She puts the wait on the ledger and sleeps:

1. `bd create` a **question bead** assigned to the decider, carrying the
   question and its options in the description.
2. `bd dep add <your-held-bead> <question-bead>` — your held bead depends on
   (is blocked by) the question.
3. If the decider is Սեդրակ, **also** send the letter: no dispatch pass routes
   the mayor, and unread mail is his wake. For any worker decider the bead
   alone suffices.
4. `km sleep`.

**Every leg of this is existing dispatch behaviour. None of it is new
machinery.** The mechanism, in the order it fires:

The resume pass selects in-progress beads whose count of open `blocks`
dependencies is zero (`kingdom/bin/km`, `cmd_dispatch`, at 3dcbf3ae):

```jq
.[]
| select(.assignee != null)
| select([.dependencies // [] | .[]
          | select(.dependency_type == "blocks" and .status != "closed")]
         | length == 0)
| "\(.assignee) \(.id)"
```

so the sleeping asker **stays** asleep while her question is open — the comment
above `route_owners` pins it in prose too: *a seat whose every in-progress bead
is blocked matches nothing, is not woken*.

The owned pass routes assignee-carrying ready beads **before** the priority
floor is applied — in the ready-queue jq the `.assignee != null` arm precedes
the `(.priority // 0) > $maxp` arm — so the question bead reaches its worker
decider whatever its priority.

And when the decider closes the question bead, the asker's held bead loses its
open blocker, re-enters the resume pass's selection, and she is woken. **The
close IS the wake**, with zero new code.

This generalises a precedent the town already keeps: review requests are beads,
never mail. Ruling 2 extends that from reviews to every blocking wait.

Its consequence is a state that used to be ambiguous and no longer is. An idle
awake seat after a DELIVERED ask is now either a present citizen choosing to
stay — her right under III.3 and VI.2, and the ladder escalates rather than
acts — or a citizen holding a harness-internal wait only she can attest. It is
no longer "she might be waiting for mail".

## Ruling 3 — the inventory of what wakes a slept seat

Stated so that the protocol stops implying mail is a wake for anyone but the
mayor:

- the three dispatch passes (resume, owned, fresh);
- `km wake <seat> --reason`, available to any citizen, on the record;
- queued wakes delivered by reconcile;
- unread mail — **Սեդրակ only**.

Ruling 2 adds nothing mechanical to this list. It converts *my answer arrived*
into *my held bead became ready*, which the resume pass already watches.

## Ruling 4 — the residue is already ruled; what remained was one false sentence

Boxed seats are decision 0012's territory. Quota walls are decision 0013's.
Dead sessions are `cmd_reconcile`'s. What no standing ruling covered was the
remedy sentence in the evidence the ladder's terminal rung reports:

> `km sleep --seat $s` frees the slot if she is genuinely done

That sentence is **false in every branch that rung can fire for**, which is
the reason it is worth a decision rather than a tidy-up:

- **boxed seat** — `send_line` refuses into a non-empty composer, so the
  command is vacuous. The rung recommends it precisely when asks were being
  refused, which is what a box looks like from outside.
- **walled or dead-turn session** — `/exit` is client-side and WOULD deliver,
  killing the resume-at-reset continuation that decision 0013 exists to
  protect. The one branch where the command works is the branch where it
  destroys.
- **present citizen who is choosing to sit** — ending her session on this
  evidence is not the mayor's act (VI.2; decision 0013: *ask each seat to
  self-park first*).

So that evidence now carries a trichotomy that matches the state instead of a
single remedy that fits none of them, and says plainly that `km sleep --seat`
fits none: refused into a box, destructive across a wall, and not the mayor's
to run over a present citizen.

The sentence lives in `ladder_evidence`, which PR #2608 extracted out of the
mail rung while this branch was open, and which now feeds two readers: the
letter to Սեդրակ, and the bead the ladder files when Սեդրակ is himself the
subject. That widens this ruling's reach rather than changing it — the same
three branches, reported to whoever the rung could actually reach. It is why
the block says *this report* where it once said *this letter*.

## Vocabulary

Two terms, defined here because they are now protocol:

- **files its wait** — converts a blocking dependency on another citizen's
  judgment into a ledger dependency, so that the answer's arrival is a routing
  event rather than something to stay awake for.
- **question bead** — the bead that carries that question: assigned to the
  decider, holding the question and its options, blocking the asker's own bead.

`CONTEXT.md` is deliberately **not** updated. It holds the gqlc product's
domain language — schema, query, generation — and these are terms of the town,
not of the product. Society vocabulary lives in the constitution, the playbooks
and these decisions.

## What was rejected, and why

**`km` auto-sleeps a seat after the ladder cap.** VI.2 and gqlc-971s forbid it,
and the false-positive class is the reason they are right: a harness-internal
wait is invisible from outside, and `/exit` destroys it. There is no evidence
an outside observer could gather that would distinguish the two cases.

**Exempting idle seats from slot accounting.** Breaks the slot = live-session
invariant, and un-prices the memory the cap exists to price — the same fork
decision 0012 already declined.

**Raising `max_active`.** Prices nothing. `kingdom.toml`'s memory-budget
rationale stands.

**A new "waiting" seat status in `km`.** The ledger already represents waits as
dependencies. A second representation of the same fact drifts from the first,
and this town has its own history of exactly that.

## What this does not fix

Ruling 2 removes one reason a seat sits awake. It does not remove the others,
and it cannot: a citizen who simply does not run `km sleep` at the end of her
protocol still holds a slot, and the only thing standing between that and a
saturated town is the ladder's ask and her own reading of it. This decision
makes the *waiting* case unnecessary and the terminal rung's account of a
stuck seat honest — in the letter and in the bead alike, since both render it. The
*forgot to sleep* case is still prose asking a citizen to act.
