# 0014 — Who merges a PR, and what says the author is finished

Date: 2026-09-02. Designed against bd gqlc-23m3v (Արփինէ), raised by the mayor's
open-PR sweep. Executed by gqlc-l51ns (Ար). Amends Constitution V.2, which is
why this ADR ships with an amendment rather than alone.

## The problem, in plain words

The town had text for **when** a PR may merge and no text for **who** merges it,
and no visible way for an author to say "I am done".

Two holes, and they are not the same hole. Because nobody was named to merge, a
finished PR waited on its author's session surviving: PR #2066 was opened
2026-08-30T14:39:14Z and merged 2026-09-02T15:21:52Z — three days green,
holding a P0 architect bead out of thirteen dispatch passes, for want of a live
hand. Because no signal said "done", an unfinished PR was indistinguishable from
a finished one: that same PR was then merged while its author's 14-mutant screen
was still running, her "not yet" existing only in her pane, where no board, bead
or query could reach it.

The second hole has a quieter shape too. `gqlc-1amz6`, `gqlc-vhvz7` and
`gqlc-rl3c` were all `in_progress` when the design was written — one status
carrying three different readinesses: one waiting on a reopen the mirror would
not take, and two whose PRs had a PASS and had simply never been merged by
anyone. The ledger had no way to tell them apart, so neither did dispatch.

(Two of those three have since closed: their author closed `gqlc-vhvz7` and
`gqlc-rl3c` at 2026-09-02T15:46Z, after the design was written and before this
ADR. The shape they witnessed is unaffected; the ledger simply no longer shows
it, which is why the ids are recorded here.)

**The fix needs no new instrument.** GitHub already owns a merger with no
session to lose — auto-merge, measured available on this repository today. The
reviewed path already has a ledger-visible finished-signal — the round-1 review
bead. The whole decision is to name them.

## The decision

### 1. The finished-signal

- **On a PR that owes no review** (the majority), the author **arms GitHub
  auto-merge**: `gh pr merge <N> --squash --auto`. It is deliberate, because it
  *is* the merge command and so cannot be set by accident. It is cheap: the
  command authors already run, plus one flag. It is visible to any citizen in
  one query — `gh pr view <N> --json autoMergeRequest`, non-null means armed.
  And it is durable, because it is GitHub-side state that outlives the author's
  session, which is the exact failure mode #2066 exhibited.
- **On a PR that owes review**, the signal already exists and is already
  ledger-visible: filing the round-1 `class:judge` bead. An author files it
  exactly when they consider the PR readable. Nothing new to set.
- **A PR carrying neither signal is thereby declared unfinished by its author.**
  That is the third readiness the ledger could not express, now expressed by the
  absence of both.

### 2. The merger

- **Unreviewed: GitHub merges**, when the seven required contexts pass. It has
  no session to die, which is the entire answer to "what happens when the role's
  session is gone".
- **Reviewed: the judge who signs the PASS merges**, and merges **before**
  closing the review bead. ADR 0009 already defines a PASS as the judge being
  willing to sign the merge of the SHA they read, as it stands; this clause has
  them perform that signature. It also closes the PASS-to-merge handoff gap the
  three beads above sat in. **The order is load-bearing**: a judge whose session
  dies between verdict and merge still holds an `in_progress` assigned review
  bead, so the resume pass returns them to finish it. Residue beads are still
  filed before closing, per ADR 0009 — the order is residue, merge, close.
- **Neither signal: nobody merges it.** A green sitter with no signal is a
  question for its author — mail, escalate — never a merge for a passer-by.
  This retires the mayoral open-PR sweep that was standing in for it.

### 3. Vocabulary

**Armed** and **disarmed** are town machinery words, defined here, for GitHub
auto-merge state on a PR: armed means `autoMergeRequest` is non-null. They are
town vocabulary and not product vocabulary — `CONTEXT.md` is the product
glossary and is untouched, following the precedent ADR 0008 set.

**This design deliberately says "armed" and never "finished" for the
author-side signal**, because ADR 0008 already spends "finished" on a different
predicate: a *branch* is finished when every PR for it is closed. Two predicates
under one word would collide precisely where a seat-refresh decision meets a
merge decision, so they are kept apart by name.

## Witnesses

Declared before running, and reported as they landed.

**W1 — arm, disarm, and visibility. Witnessed.** Scratch PR #2217, a throwaway
branch into master carrying an inert file, opened for this row and never merged.
With its required checks still pending (`mergeStateStatus: BLOCKED`),
`gh pr merge 2217 --squash --auto` exited 0 and printed nothing;
`autoMergeRequest` went from `null` to an object carrying
`enabledAt: 2026-09-02T15:54:27Z` and `mergeMethod: SQUASH`. Then
`gh pr merge 2217 --disable-auto` exited 0, also silently, and
`autoMergeRequest` read `null` again. So both transitions work, both are silent
on stdout — **the JSON field, not the command's output, is what a citizen reads
to know the state** — and the disarm in V.2.4.2 is a real remedy rather than an
assumed one.

**W2 — arming a PR that is already in clean status. Witnessed on an unprotected
base, and it came back the other way round.** The design expected a refusal
naming clean status, which is what the two-command recipe was for. Scratch PR
#2219, `mergeStateStatus: CLEAN` with all eight non-skipped checks passed:
`gh pr merge 2219 --squash --auto` exited 0 and **merged it**, at
2026-09-02T15:59:52Z, `state: MERGED`, `autoMergeRequest: null`. No refusal.

So the single command suffices and the fallback sentence is dropped from the
clause and from the protocol, per the design's own instruction for this row.
**What my measurement cannot distinguish** is whether `gh` merged directly or
GitHub armed the PR and fired it in the same breath: the merge consumes the
auto-merge request either way, so the post-state is identical. Nothing in the
shipped text depends on the difference, and it is not claimed here.

The row also produced something the design did not anticipate, and it is the
reason V.2.4.1 now carries a sentence about it: **arming is not a reservation.**
It is a merge that fires as soon as it can, and on an already-green PR that is
immediately, with no interval in which anyone could intervene. That makes
V.2.4.2's prohibition sharper rather than weaker — it is why the clause says a
review bead's filer disarms *first*.

**W3 — that an armed merge actually fires when the checks go green. NOT
witnessed by this battery**, and this ADR does not claim it. Witnessing it means
landing a junk merge on master. It is GitHub's documented core behaviour and its
first live witness will be the first real armed PR.

**W4 — that the judge who signs the PASS merges. First witnessed by this PR's
own review**, which is owed twice over: the bead is design-blocked (V.2.0) and
the change amends the constitution (VII.2). One judge round satisfies both.

### Why W2 was not run against master

W2's question is whether `gh pr merge <N> --squash --auto` refuses a PR already
in `CLEAN` status, which is what justifies the two-command recipe in V.2.4.1.
Answering it on master requires arming a green scratch PR, and if the answer is
"it merges", the answer arrives as an unwanted commit on master. The design bead
ruled on that trade in advance: an honest unwitnessed row beats a junk merge.

So the row was taken on a PR into a throwaway base branch instead
(`scratch/gqlc-l51ns-base`, deleted after), where the same `gh` and the same
GitHub mutation see a PR in the same `CLEAN` state and no outcome can reach
master. **The limit of that substitution, stated rather than glossed:** the two
bases differ in protection, and the two PRs were observed to differ in the state
they hold while checks run — #2217 into protected master read `BLOCKED`, #2219
into the unprotected base read `UNSTABLE`. Only the terminal `CLEAN` state, the
one the row is about, is common to both.

**The choice of base was load-bearing, not cautious book-keeping.** The row
merged. Had it been taken on #2217 against master, as the battery's first draft
had it, `kingdom/.witness-gqlc-l51ns` would be on master now and this ADR would
be describing the revert PR instead.

**The residual risk, and its direction.** The behaviour was measured against an
unprotected base, so what is certain is that `--auto` merges a `CLEAN` PR rather
than refusing it; that master's protection changes this is possible and
unmeasured. If it does, an author following V.2.4.1 sees `gh` print an error and
their PR stays open — a visible, self-announcing failure, not a silent one — and
the clause gains its fallback sentence back. The failure that could not be
recovered from is the opposite one: text promising a refusal that does not
happen, read by an author who believes arming a green PR is reversible. That is
the one this row closes.

## Measurements

All 2026-09-02, each one command, all re-run by the executor rather than
inherited from the design.

- `gh api repos/areqag/gqlc --jq '{allow_auto_merge, delete_branch_on_merge,
  owner_type:.owner.type}'` → `allow_auto_merge: true`,
  `delete_branch_on_merge: true`, owner type `User`. **Auto-merge needs no
  settings change.** This is the design's premise: if `allow_auto_merge` is ever
  false, V.2.4.1 has no mechanism behind it.
- `gh api repos/areqag/gqlc/branches/master/protection` → required contexts
  `lint, test, tidy, actionlint, govulncheck, live-smoke, codegen-fence`;
  `strict: false`; `enforce_admins: true`. These seven are what an armed merge
  waits for.
- `gh api repos/areqag/gqlc/rulesets/18407856` → the same seven contexts,
  `enforcement: active`, `updated_at 2026-08-29T20:53:49-04:00`. The two
  sources agree; there is no union to take. **This line is a correction.** Round
  1 of this PR's review (bd `gqlc-nj9ub`) found the sentence that stood here —
  that the ruleset required five of the seven, ADR 0010's known drift — and
  falsified it by running the command above. It was the one premise in this
  block I inherited rather than re-ran, under a heading that says all of them
  were re-run, and the drift it described had been closed three days before the
  design was written. ADR 0010 is not wrong: it measured on 2026-08-29 and says
  so, and consolidating the ruleset was its own prerequisite. It is simply no
  longer current, and this ADR restated it as if it were.
  Nothing downstream moves — seven bound before and seven bind now — which is
  why the finding is about the provenance of a measurement rather than about
  the decision it supports.
- Article V.2 read directly before the edit: both merge sentences state when,
  neither names an agent. Hole 1 confirmed against the text itself.
- PR #2066: `createdAt 2026-08-30T14:39:14Z`, `mergedAt 2026-09-02T15:21:52Z`.
- `gqlc-1amz6` `in_progress`; `gqlc-vhvz7` and `gqlc-rl3c` closed
  2026-09-02T15:46Z, as noted above.

## Rejected

- **Amend V.2 to name only the merger** (the mayor's recorded fallback). Closes
  hole 1 only, at the same prose-only cost as the design that closes both.
  Dominated, so this was not a coin-flip and did not go to Սեդրակ.
- **A PR label, a body line, or draft status as the signal.** A label still
  needs a separate answer for who merges. A body edit cancels the PR's own CI on
  this repository and is paste-prone. Draft status defaults the *wrong way* —
  non-draft is what `gh pr create` produces, so "finished" would be the accident
  default, which is the failure this design exists to prevent. Arming is signal
  and merger in one deliberate act.
- **km merges, or km files merge-beads.** A new instrument with a new
  silent-failure surface, and unnecessary once GitHub is the merger. Dispatch
  needs no change at all: this design asks nothing of the already-computed PR
  set, which is cheaper than asking a question of it.
- **Unconditional auto-merge on every PR** (the queue shape). The signal-gate is
  precisely what keeps hole 2 closed; removing it reopens the merged-mid-screen
  failure for every PR at once.

## Risks and limits, named

- **V.2.4.2 — never arm a PR that owes review — is unguarded prose**, the same
  enforcement class as the standing rule against `--admin`. A machine guard
  would need CI to read the ledger, which is not worth an instrument. The
  exposure it leaves, merging over a demanded review, existed identically before
  this design; it was simply unnamed.
- **Stale-green merges** (the `gqlc-hpa1` class; ADR 0010 is not in force) are
  exactly as exposed under an armed merge as under a manual one. Unchanged, not
  worsened. `gqlc-9vzmw` stands.
- **GitHub disarms auto-merge itself on some transitions** — draft conversion, a
  base change — and that is unwitnessed here. The worst case is a sitter the
  author's resume wake catches, which is a degradation to today's status quo and
  never below it.
- **ADR 0008 composes for free**: a seat asleep on an armed branch is moved off
  it by seat-refresh only after the PR record says finished, so the two designs
  need no edits to agree.
- **If the ADR 0010 merge queue ever activates** (it needs an org move,
  `gqlc-9vzmw`), `--auto` becomes enqueue-when-ready. The signal semantics carry
  over unchanged: arming still means the author is done, and the queue rather
  than the branch becomes what the merge waits on.

## Precedent this supersedes

ADR 0009's line about "an author acting on merge-on-PASS" describes the
pre-0014 flow. It is correct as a dated record of how the town then worked, and
is left standing as one. This ADR supersedes that phrase alone: under V.2.4.3
the judge merges, not the author. Everything else ADR 0009 says about what a
PASS is — unconditional, signed on the SHA read, residue filed before closing —
is untouched and is what V.2.4.3 rests on.
