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

`bd history` prints the **host-local** clock with no marker; every time below is
converted to UTC, and every ordering below is taken from that one renderer rather
than compared across two (`bd show --json` prints true UTC with a `Z`). Mixing the
two inverted an earlier draft of this section by four hours; the renderer defect
is `gqlc-3e3ww`.

| bead | design | filed | promoted by hand |
| --- | --- | --- | --- |
| `gqlc-az1rj` | `gqlc-dakzu` (P2) | P3-open 2026-09-03T00:54:52Z | 2026-09-04T23:32:42Z |
| `gqlc-0sxu6` | `gqlc-d8ghh` (P2) | P3-open 2026-09-03T01:02:02Z | 2026-09-04T23:32:44Z |

Both promotions are two seconds apart: one person, one sweep, after the fact.

**Both stubs entered the no-queue the moment their designs closed. What
separates them is four minutes of one citizen's initiative.** `gqlc-az1rj` is the
plain case — open, unassigned and sub-floor from its design's close at
2026-09-03T00:56:19Z until the promotion 46h36m later, and still open today.
`gqlc-0sxu6` entered the same state at its design's close, 2026-09-03T01:02:59Z,
and left it 3m54s later when a citizen claimed it at 01:06:53Z: an assigned bead
routes on the owned pass and an in-progress one on resume, so the claim — not the
priority — is what carried it. It then ran at P3 for about 46 of its roughly 47
held hours, which Constitution III.3 expressly permits, and closed at P2 at
2026-09-04T23:48:40Z, sixteen minutes after the sweep promoted it.

That is why this ruling is prose in a playbook rather than a stall detector. The
forbidden state occurred twice and produced a visible symptom once — and the
instance that stayed harmless was rescued by an unprompted manual claim inside
four minutes, not by anything the town built. A mechanism that waits for someone
to notice a stalled bead is betting on that reflex firing every time; here it
fired once out of two, and the miss cost 46h36m.

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
stub, and precisely why the two incidents above came out differently. Both
entered the no-queue at their designs' close. One was claimed 3m54s later and
carried to close at P3 without difficulty, because a claim moves a bead onto the
owned and resume passes where the floor does not reach. The other was claimed by
nobody and sat 46h36m.

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

Current orphan stock, re-derived 2026-09-05T00:51:12Z over the ready queue:
**seven** unassigned sub-floor stubs whose closed design sat at or above the
floor — `gqlc-6grh5`, `gqlc-zptd0`, `gqlc-h9ig1`, `gqlc-mrf89`, `gqlc-ng7qv`,
`gqlc-p6wd`, `gqlc-f8ny`. All seven are kingdom machinery; none is product work.
No machinery chases any of them.

The query, so the falsifier travels with the number — `-n 0` is load-bearing and
is the whole reason this figure is seven:

```sh
A=$(bd list --all --json -n 0 | jq -c 'map({key:.id,value:{status,priority,labels:(.labels//[])}})|from_entries')
bd ready --json -n 0 | jq -r --argjson all "$A" '
  [ .[] | select(.assignee==null and .priority > 2) | . as $s
        | select([ $s.dependencies[]? | select(.type=="blocks")
                   | $all[.depends_on_id] // empty
                   | select(.status=="closed" and .priority <= 2
                            and (.labels|index("class:architect"))) ] | length > 0) ]
  | length'
```

An earlier draft of this section reported **one**, and named `gqlc-rm5cs`. That
was an artefact of omitting `-n 0`: `bd ready --json` caps at 100 rows and the
queue holds 208, and the same query over the truncated window returns exactly 1.
The draft even carried a guard against the mistake — it checked that the query
found something before the priority filter — and that guard is blind to this
failure, because a truncated query is not an empty one. Recorded rather than
quietly fixed, because the number moved by 7× and the wrong number was the
reassuring one. `bd`'s row caps are documented in `CLAUDE.md` and in
`docs/bd-ledger-queries.md`; both say to pass `-n 0` at every scripted call site.

Note also that `dependencies[]` has two different shapes: `bd show --json` returns
full issue records (`.id`, `.status`, `.labels`), while `bd ready --json` returns
edge rows (`.issue_id`, `.depends_on_id`, `.type`) with none of that. A query
written against the first shape and run against the second matches nothing and
reports zero — silently, which is how the query above was wrong on its first run.

Both corrections are recorded rather than quietly repaired, and the reason
generalises past this document: **being re-derived is exactly what made the
number credible.** A figure nobody can see the working of is worth less than a
figure whose working shows where it moved and why. That applies hardest to
whoever has the most authority in the room, because a false action item from the
mayor costs more than a false one from anyone else.

## Ruling 4 — `max_priority` stays `"2"`

No configuration value changes. The `[dispatch]` comment's own measured
argument holds and is not repealed by this decision: the P3 tier refills faster
than it drains because the town keeps writing to it, so raising the floor to
reach it fills every citizen slot with machinery, which V.3.1 forbids. The
2026-08-23 raise to `"3"` was reversed by Անդրանիկ directly for that reason.

## Ruling 5 — the seven stay at P3, and what to do with the next one

The seven are in the state ruling 2 calls forbidden. Ruling 2 does not tell you
to promote them, and this section exists because a reader could reasonably think
it did — and promoting seven machinery beads to P2 would fill seven of ten
citizen slots with the town's own plumbing, which is precisely what the floor
exists to prevent and what Սեդրակ declined for `gqlc-mwwfn` on the same grounds.

**A forbidden-state pair whose design has ALREADY CLOSED and whose stub is
machinery is a historical intake artefact, not a live defect. Leave it. Do not
promote the stub to match the design.**

Ruling 2 and Constitution V.3.2 appear to collide here and do not, because
ruling 2 identifies the wrong half as the defect in this shape. When both halves
are machinery, the sub-floor stub is correctly priced under V.3.2; it is the
DESIGN that was priced too high at intake. That mispricing is now costless — a
closed bead routes nothing, wakes nobody and holds no slot — so there is nothing
to repair. The forbidden state is still worth preventing **going forward**, and
there it binds intake, which is where a pair is priced once.

Ruled by Սեդրակ 2026-09-05, who re-ran the query above verbatim rather than
taking the count on trust: the same seven ids, in the same order, nothing more
and nothing less.

**Seven is a floor, and nineteen is a second floor rather than a ceiling.**
Dropping the `class:architect` requirement from the blocker — keeping only
"closed and at or above the floor" — returns 19, so twelve further stubs are held
by a blocker carrying no design label. That widened query over-catches rather
than correcting the seven: at least three of the twelve are genuine product holds
whose own titles say "blocked on a named consumer" or "post-v1" (`gqlc-8pe`,
`gqlc-5md`, `gqlc-1a5`). A blocker being closed and P2 does not make it a design
gate; it makes it a bead that finished.

Neither number bounds the population from above, and the reason is not the label
but the **edge**. Both queries require a `blocks` dependency, and the type of
that dependency is a choice made at intake rather than a fact about the work: bd
permits one type per pair, and this town has already measured a design gate
living purely in bead prose with an empty `blocked_by`. Relax only the edge type
— delete `| select(.type=="blocks")` from the query above and change nothing else
— and the seven become **eight**, the addition being `gqlc-6jp65`, held to the
closed design `gqlc-tdb9a` by `discovered-from` (re-derived 2026-09-05). Սեդրակ's own sweep is the wider form of the same measurement:
over all 92 closed at-or-above-floor `class:architect` beads, 24 of 105 dependent
edges are not `blocks`. So the honest statement is Անահիտ's on `gqlc-ctgp7`: **at
least N, unbounded above by any label- or edge-shaped query.**

Two limits on the eighth, so it is not read as a settled reclassification.
`gqlc-tdb9a`'s own close reason calls `gqlc-6jp65` residue rather than an
execution stub, which is precisely the reading job the twelve need and not its
answer; and `bd dep list --json` returns `type: null` on every row, so any sweep
of this shape must come off the plain renderer. Settling N needs somebody to read
the delta blockers and say which were designs — a bounded job, and one that will
still not produce a ceiling.

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
