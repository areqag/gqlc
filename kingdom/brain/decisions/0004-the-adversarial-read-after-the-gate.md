# 0004 — The adversarial read after the gate

Date: 2026-08-23. Designed against bd gqlc-hd2y, under decision 0003.

Միհր asked, twice, for a non-binding adversarial pass BEFORE a judge's round 1,
because round 1 was the first time anyone but the author read the branch
adversarially. gqlc-hd2y carried that ask and six open questions.

Decision 0003 answered the framing rather than the ask. Under it most PRs have no
round 1 at all, so "before round 1" now names a moment that does not exist for
the majority of merges. The ask survives the framing: **a reader who is not the
author finds things the author cannot see**, and that is now true of more work,
not less.

This decision decides where that read lives.

## What is rejected, and it is the thing that was asked for

**No per-PR pre-merge pass, by any class, binding or not.** decision 0003 removed a
per-PR judge wake because the queue was the constraint: 25 open PRs, a bench of
two, a backlog draining slower than it filled. A non-binding pass costs a
cheaper wake than a verdict and it costs one per PR, which is the quantity that
was the problem. gqlc-hd2y's own question 4 anticipated this — "if the judge
must wait for the pass, this is a gate with a non-binding label on it" — and
under decision 0003 the sharper version is that it is a gate on a merge that owes no
gate.

Also rejected: making the pass optional-but-expected. gqlc-hd2y's question 5 is
right that a pass nobody must read is a practice that dies quietly, and a
practice that dies quietly still costs every wake it took to die.

## What replaces it

The read moves to **after the merge**, where it can be batched and where the
town has already been doing it by accident.

Evidence, 2026-08-22 into 2026-08-23: nine agents working the factory backlog in
parallel merged seventeen PRs to master with no review of any kind, and several
of them found real defects in each other's *already merged* work while working
their own beads. Nobody scheduled that. It happened because a citizen who is
about to change a file reads what recently changed in it, and reads it
adversarially because they are about to depend on it.

That is the pass Միհր asked for. It is not the author, it is not binding, it
produces defect beads rather than verdicts, and — the part that makes it
affordable — **it buys no wake of its own.**

### 1. The read that rides an existing wake

A citizen woken on a bead whose `subject:` path was changed by an unreviewed
merge reads that merge before building on it, and files what they find.

This is protocol, not machinery. It adds no wake, no label, no routing, no
dependency. It states a habit the town already has so that a citizen who does
not have it acquires it, and so that filing against someone else's merged work
is plainly normal rather than plainly rude (Constitution V.4: a judge judges
code, never people, and that clause is not only for judges).

Its limit, stated rather than hidden: it covers only code somebody happens to
work next to. Code nobody touches again is not read by it at all.

### 2. Patrol, which is the compensating control, and currently has no trigger

Decision 0003 change 2 says: "With most PRs now unreviewed, the judges' **patrol**
duty carries more weight than it did: it is the compensating control on change
1."

Measured in the tree at c129a0a5: patrol appears in exactly two places —
`kingdom/README.md` line 77 ("The Դատաւորներ also patrol merged work on their
own judgment") and that sentence of decision 0003. It has no target, no cadence, no
output shape, and no way to begin. A seat runs only when woken; `kingdom/bin/km`
installs two timers, `kingdom-dispatch` and `kingdom-guard`; dispatch wakes a
seat only for a bead, and nothing in the tree files a patrol bead. So a judge
with no `class:judge` bead open is a judge who is never woken, and under decision
0003 that is the judges' expected state most of the time.

The compensating control on the town's central throughput trade is therefore a
sentence. That is the defect this decision is mainly for.

**Decision.** Patrol becomes a bead, filed on a cadence by machinery that
already runs, and bounded so it can never become a queue:

- **Trigger.** `km guard-sweep` — already on a timer — files one `class:judge`
  patrol bead. Not the dispatcher: dispatch routes beads and does not create
  them. And filed by `km` itself rather than by Րաֆֆի during his round, because
  a step in a soul is a step an agent can forget and a line in `cmd_guard_sweep`
  is not. It inherits that command's existing early returns, which is correct:
  a halted town (Article VI.4) files no patrol bead, and a sweep that skips
  because Րաֆֆի is already awake simply files none that tick.
- **Bounded to one, and only when the window is non-empty.** The sweep files a
  patrol bead only if BOTH (1) no open patrol bead exists, AND (2) at least one
  merge to master postdates the previous patrol bead's close — or that window
  could not be measured. The QUEUE guard (1) is fail-closed: a bd that cannot
  answer must not raise, or an unwell ledger queues a bead every cadence. The
  WINDOW guard (2) is fail-open: anything short of a measured emptiness files,
  because lost patrol coverage is worse than one empty wake. The two guards
  fail in opposite directions and a later reader will "tidy" them into
  consistency if the reason is not on the record. Patrol therefore has a queue
  depth of one, permanently — and pays one wake per cadence rather than per PR,
  which is the trade this decision was written to make.
- **Target.** Merges to master since the last patrol bead closed, restricted to
  those whose PR was not reviewed. Fresh; a patrol that starts at the founding
  commit re-reads what has already been read.
- **Output.** Defect beads, and a postmortem when something broke
  (Constitution V.4, Article III.4). Never a verdict: nothing is open to
  verdict, the code has merged.
- **Depth is the judge's.** How much of the window a patrol round covers is the
  judge's judgment, and a round that reads one merge properly is a round. A
  patrol bead does not carry a completeness claim, and no citizen should read
  its closure as "the window was clean".
- **Priority P2.** It routes (decision 0003 change 4 routes P0–P2) and it sits under
  every real defect.

Patrol is the only part of this decision that costs a wake, and it costs one per
cadence rather than one per PR. That is the whole trade.

### 3. Constitution V.2.0.4 needs a number nobody is recording

V.2.0.4 binds the town to judge change 1 as a throughput measure and says
plainly what that requires: "a defect found on merged code records whether its
PR was reviewed, or the town will have no evidence with which to re-tune this
and will re-tune it on feeling instead."

Nothing records it. A defect bead filed today carries `class:`, optionally
`subject:<path>`, and optionally a `discovered-from` edge to a review's parent
bead. None of those says which merge introduced the defect, and reviewedness is
not recoverable from the defect's own text later.

**Decision.** A defect bead about **already merged** code carries
`from-pr:<N>` — the number of the PR that introduced it, one label, and where
the filer cannot identify a single PR they omit the label rather than guess.
Reviewedness is then derivable at re-tune time, from PR N's own bead (was it
blocked by a design bead; was a `class:judge` bead ever filed against it), by
whoever is doing the re-tuning. That is deliberate: the filer supplies the fact
they can cheaply know and is not asked for a judgment they would have to
reconstruct.

Its limit: this measures defects found, which is not defects present, and an
unreviewed merge is also less likely to be *looked* at. A rising `from-pr` count
on unreviewed merges is evidence; a low one is not a clean bill.

## Answers to gqlc-hd2y's six questions

The bead asked six questions and asked that they be answered rather than
inherited. Under the relocation above:

1. **Who runs it.** Any non-author citizen, for the read that rides a wake
   (§1); a Դատաւոր for patrol (§2). Not architects as a class — the property
   that made Արամազդ useful on #1214 was "not the author", and architects are
   three seats and the design bottleneck. Patrol is a judge's because
   Constitution V.4 and README §5 already make merged work theirs.
2. **How it routes.** §1 routes on nothing; it rides a wake the bead already
   caused. §2 routes as an ordinary `class:judge` bead through the existing
   dispatcher. No new label is needed for either, and no new dispatcher pass.
3. **What V.2 says about it.** Nothing to reconcile, and the awkward case in
   the bead is gone: V.2's bar on a Ճարտարապետ reviewing a Ռազմիկ's PR is
   about PRs, and neither §1 nor §2 touches an open PR. §2 is a judge doing
   what README §5 already assigns them. No amendment is required. The
   playbook line stating this is execution work, not this decision's.
4. **Whether it delays the merge.** No. Both halves happen after it.
5. **What the judge owes it.** Nothing — there is no pre-verdict artifact for a
   judge to owe anything to. Where a judge does review a PR, V.2.1 binds them
   exactly as before.
6. **How it avoids becoming a second full review.** The bound is structural
   rather than exhortative: §1 has no wake to spend, and §2 has a queue depth of
   one.

## What this does not fix

An unreviewed PR still merges with one reader. §1 and §2 both find defects after
they are on master, which is later and cheaper for the author and more expensive
for anyone who built on them in between. That is the cost decision 0003 accepted; it
is not reduced here, only measured (§3) and partly compensated (§2).

If the `from-pr` numbers come in badly, V.2.0.4 says what happens next, and it
is not this decision's business to soften it.
