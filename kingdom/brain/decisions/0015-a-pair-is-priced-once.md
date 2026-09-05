# 0015 — A design/execution pair is priced once

Date: 2026-09-05 (UTC). Designed against bd `gqlc-zbx3n` (Արթուր, closed
2026-09-04T23:52:09Z), under decision 0003. Executed by `gqlc-kj9ha`. Raised by
Սեդրակ, who measured 205 ready beads of which 16 were routable and asked
whether dispatch's P2 floor starves a town that reads healthy.

The short answer is that the floor is not the defect. The floor is the
mechanism of Constitution V.3.1 — machinery is worked on the side of the
product — and it is working. What the measurement exposed is a rule the town
has been keeping by habit and had never written down.

## The shape of the problem, in plain words

`kingdom/brain/playbooks/design-gate.md` files a design and its execution stub
together, and both lines of that recipe carry the same `-p <P>`:

```
bd create "design: <thing>"    --label class:architect  -p <P>
bd create "<thing>"            --label class:warrior    -p <P>
```

One price, written twice. Nothing said that was a rule, so the repeated `<P>`
read as a template being indifferent to the number rather than as an
instruction. Practice diverged twice in one week and in the same direction: the
design filed at the floor, the execution stub filed below it. The design then
closes, the dependency releases, and the stub becomes *ready* — into a tier the
dispatcher never routes from. The pair promised a handoff and had guaranteed it
could not happen.

Nothing goes red when this occurs. The design bead closes cleanly, the
architect's work is done and correct, and the stub sits on the board looking
exactly like the hundred and seventy-eight other P3s.

## The evidence, re-derived

Measured 2026-09-05T00:00Z from `bd history <id>`, whose every entry carries a
`[P<n> - <status>]` snapshot, so the priority a bead was FILED at is
recoverable and not a matter of anyone's recollection.

| bead | design | filed | promoted by hand |
| --- | --- | --- | --- |
| `gqlc-az1rj` | `gqlc-dakzu` (P2) | P3-open 2026-09-02T20:54:52Z | 2026-09-04T19:32:42Z |
| `gqlc-0sxu6` | `gqlc-d8ghh` (P2) | P3-open 2026-09-02T21:02:02Z | 2026-09-04T19:32:44Z |

Both promotions are two seconds apart: one person, one sweep, after the fact.

**The harm landed on one of the two, and that is the finding, not a caveat.**
`gqlc-az1rj` is the clean case — open, unassigned and sub-floor from its
design's close at 2026-09-03T00:56:19Z until the promotion 42h36m later, and
still open today. `gqlc-0sxu6` was already `in_progress` at 2026-09-02T21:06:53Z,
*before* its design closed at 2026-09-03T01:02:59Z, so it was held by a citizen
and no unroutable window ever opened; it was worked and closed at P3, which
Constitution III.3 expressly permits, and the blanket sweep promoted it
afterwards for tidiness rather than in answer to a stall.

That asymmetry is why this ruling is prose in a playbook rather than a stall
detector. The forbidden state occurred twice and produced a visible symptom
once. Anything that waits for someone to notice a stalled bead catches half of
these, and catches that half two days late.

(The bound on that evidence: `bd history` renders title, priority and status,
so the dependency edge cannot be dated from it. Nothing here claims a citizen
claimed a blocked bead — only that the stub was in progress before its design
closed.)

Ready histogram, for the doctrine below: P1=3 P2=15 P3=178 P4=10 measured
2026-09-05T00:00:43Z, against P1=3 P2=13 P3=176 P4=10 measured by the design
one day earlier. The tier moves by twos between sessions and its shape does
not.

## Ruling 1 — what a priority number means

Relative to `[dispatch] max_priority`, which is `"2"`:

- **P0–P2 is a queue.** The dispatcher will hand the bead to somebody.
- **P3–P4 is not a slow queue. It is a no-queue** — a parked, searchable
  record that will never be handed to anybody until a person renumbers it.

So pricing a bead P3 is not an estimate of its urgency. It is the decision that
nobody will ever be *woken* for it, and the filer must mean exactly that. This
is not a complaint about the tier: P3 is where the town's adversarial review of
its own machinery is *supposed* to deposit findings, by Constitution V.3.2 and
by the measured argument in `kingdom.toml`'s own `[dispatch]` comment.

**Read "never routes" precisely, because the imprecise reading is falsified by
this decision's own evidence.** The floor binds the FRESH dispatch pass only —
ready and unassigned. An assigned sub-floor bead still routes on the owned
pass, and an in-progress one on resume; Constitution III.3 is the right to
finish your own work whatever its number. So the tier is a no-queue for a bead
nobody holds, which is precisely the condition of a freshly released execution
stub, and precisely why the two incidents below came out differently: the stub
nobody had claimed sat for two days, and the stub a citizen was already holding
was carried to close at P3 without difficulty.

## Ruling 2 — a pair is priced once

**A design/execution pair is priced once, at intake, at one number.** The
execution stub inherits the design's priority at filing — that is what the
repeated `<P>` in the recipe has always meant.

At design close the architect **re-derives** that price as part of writing the
plan into the stub. The scope is known by then and the number may move in
either direction; what it may not do is go unconsidered.

**The forbidden state is a design at or above the floor whose execution stub is
below it.** Such a pair asserts that the work is worth waking a citizen for and
simultaneously guarantees that no citizen will be woken for the half that does
it.

Enforcement is prose — this decision, plus the design-gate playbook, which is
what a citizen actually reads at filing time. A `km doctor` row is deliberately
not built today; see the reopen condition below.

## Ruling 3 — the three hypotheses, adjudicated

Սեդրակ offered three explanations for 16 routable beads out of 205.

**(a) "The floor is right and the priorities are wrong."** True for a minority,
and that minority is real. Of the 186 sub-floor ready beads measured
2026-09-04: 123 warrior, 13 architect, 50 unlabelled; by subject, 71 machinery,
15 product, 100 carrying no subject prefix — and a 50-title sample of that last
population reads about 24% product. So on the order of 40 product beads sit
sub-floor, among them a real codegen defect (a list element's nullability is
discarded, so a NULL element fails to decode). Renumbering them is Constitution
V.3.3 territory and belongs to Սեդրակ, bead by bead. **This decision renumbers
nothing, and the warrior executing it was told not to** — the ledger is not the
tree, and a PR that renumbers beads has no reviewable diff.

**(b) "The floor is right and the supply is wrong — idle seats hold slots
instead of sleeping."** Real, and a different bug; Սեդրակ said so himself. It
had no bead until this design filed one as its own pair. Out of scope here.

**(c) "The floor is wrong for one class — execution halves arrive P3 and
die."** The incident is real and the proposed remedy is not. A dispatch
carve-out that routed any sub-floor bead reading `Execution of …` would also
route the ones that are correctly parked, and it would site the fix at dispatch
time when the defect happens at filing time. Ruling 2 is the fix.

Current orphan stock, re-derived 2026-09-05T00:00Z over the ready queue:
**exactly one** sub-floor execution stub of a closed design — `gqlc-rm5cs`, P3,
unassigned, the herdr composer-clear lever. Named to Սեդրակ for hand-promotion;
no machinery chases it. (The query returns 3 execution stubs before the
priority filter, so the answer of one is not an artefact of a query that finds
nothing.)

## Ruling 4 — `max_priority` stays `"2"`

No configuration value changes. The `[dispatch]` comment's own measured
argument holds and is not repealed by this decision: the P3 tier refills faster
than it drains because the town keeps writing to it, so raising the floor to
reach it fills every citizen slot with machinery, which V.3.1 forbids. The
2026-08-23 raise to `"3"` was reversed by Անդրանիկ directly for that reason.

## Rejected alternatives

1. **Raise the floor to 3.** Repeats the raise that was reversed within the
   day; fills slots with machinery.
2. **A class-scoped floor exemption for execution stubs.** Routes correctly
   parked stubs too, and fixes at dispatch time a defect that happens at filing
   time.
3. **A `km doctor` row now.** Mechanism before the stated rule has failed even
   once as prose. Decision 0003's economy argues prose first, and until today the
   rule was never stated, so it has not yet been given the chance to fail.
4. **Auto-inherit priority in bd tooling.** bd is upstream. We do not patch it
   for a rule two sentences state.

## Falsifiers and reopen conditions

- **If the routable queue empties while seats sit idle**, because product work
  was never filed or was filed sub-floor, then the doctrine of ruling 1
  narrows: the floor would be starving the town after all, and V.3.4's own
  falsifier applies.
- **If a third design/execution pair diverges in price after this decision
  merges**, prose enforcement has failed and the `km doctor` row rejected above
  is owed. File it then, against this decision, not now. Two divergences before a
  rule existed is not evidence about a rule that exists.

## What this does not fix

**The 186 sub-floor beads are still sub-floor**, and about 40 of them are
product work. This decision explains what their number means; it does not
re-price a single one. That sweep is Սեդրակ's under V.3.3 and it is the
substantial residue of Սեդրակ's original question.

**Nothing detects the forbidden state.** By construction — see rejected
alternative 3 and the reopen condition. Between now and a third divergence, the
only thing standing between a mispriced pair and a stub nobody is woken for is
a citizen reading design-gate.md at filing time.

**The constitution is not amended.** V.3.1 already makes the floor the
mechanism it is, and III.3 already protects finishing your own sub-floor work.
This ruling states a filing practice that those clauses assume; it adds no
force they do not already carry.
