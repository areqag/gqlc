# 0005 — Mutation evidence belongs to the author

Date: 2026-08-23. Designed against bd gqlc-cn4x, under decision 0003.

Միհր asked, twice, that authors ship a mutation battery with the PR so a judge
audits a battery instead of building one from scratch. gqlc-cn4x carried the ask
and its hazard: an author-supplied battery is a claim about the author's own
work, and a judge who takes it at face value has laundered an untested guard
into a documented one.

Decision 0003 moved most PRs out from under a judge entirely. So the economics the
ask rested on — the judge's scarcest hours spent constructing mutations — apply
now to a minority of merges. The lever does not die with them; it changes who it
is for.

## The decision in one line

The battery is the **author's** evidence, not the judge's input. It is owed on
the beads that already owe a mutation, it is recorded in a shape a stranger can
re-run, and on most PRs nobody will audit it — which this decision states plainly
rather than designing around.

## Why it is still worth writing down when nobody audits it

Because the discipline finds defects in the author's own work, unprompted, and
the town has the receipt. On gqlc-gudu / PR #1236 Արամազդ shipped a 7-row
battery against a suite that was 178/178 green. One row SURVIVED and it was a
real defect in his code: an `awk 'NR==FNR { pulled[$0]; next }'` intersection,
where `NR==FNR` means "still in the first file" only while that file is
non-empty — so on precisely the runs that pulled nothing, awk took the plan
file's first line, the closing record, for a pulled id. The guard against that
read green. A fully green suite certified nothing about the case the guard
existed for.

Green CI is now the gate for most merges. Anything that makes the author's own
tests sharper buys back safety at the gate that is actually there, and it buys
it with no wake, no routing and no queue.

## What is owed, and by whom

**The trigger is unchanged and no new population is captured.** The citizen
protocol already binds a Ռազմիկ: "A bead that adds or changes a GUARD is not
done until you have watched that guard fail." That duty exists, it is the right
population, and this decision adds a *record* to it rather than a second obligation.

- A PR that adds or changes a guard records its mutation rows.
- A PR that adds or changes no guard owes nothing and writes nothing. That is
  the exemption route gqlc-cn4x's question 4 asked for, and it needs no
  judgment call and no waiver: a docs change or a config line has no guard, so
  the trigger simply does not fire. A requirement with no exemption route gets
  satisfied dishonestly; this one has the exemption built into the trigger.
- Where the author cannot get a red, the citizen protocol already says that IS
  the finding and is said on the bead. Recording "no red obtainable, here is
  what I tried" satisfies this decision. An unkillable guard honestly reported is
  worth more than a killed one invented.

## The shape, which is the whole of the auditability answer

gqlc-cn4x's question 1 — what makes a battery auditable rather than assertable
— is the crux, and Արամազդ's live run answers it. Four requirements, each
earned by a failure the town has actually had:

1. **A per-row expected victim, declared before the run**, as a literal
   assertion string in the battery source. This is what converts a survivor from
   a shrug into a diagnosis: not "is my test weak?" but "why did *that*
   assertion not fire?", which has an answer. A battery reporting only a kill
   count cannot be audited at all — a reader cannot tell a row that killed the
   right guard from a row that killed a neighbour.
2. **Four verdicts, not two**: KILLED (the declared victim is among the
   failures; collateral kills printed too), WRONG (something died, not the
   declared one), SURVIVED, NO-OP (the mutation changed nothing).
3. **A compiler-kill screen, named per language.** A mutant reads KILLED when
   the artifact stopped building, for a reason that has nothing to do with the
   guard. In Go the tell is kill-count 1 with a BLANK test name, and `go build`
   is blind to `_test.go` — the screen is `go test -c -o /dev/null <pkg>`. In
   `just`, a parse error also prints `error:`. In shell and jq the tell is
   magnitude: a battery reporting 100 kills out of 110 rows is reporting a
   syntax error. **Question 2 of gqlc-cn4x asked whether the author or the judge
   screens this: the author does, mechanically, and the screen is a command in
   the record so a reader re-runs it rather than trusting it.**
4. **A blinding pass when one guard kills every row.** Zero findings across a
   battery that one dominant guard swept certifies exactly one row. Blind that
   guard and screen again; declare the per-row expected verdict for the second
   pass before running it.

Two standing cautions belong with the rows and not in a separate document:
KILLED self-certifies and SURVIVED does not — a SURVIVED row is a claim about a
sandbox, and a key-lookup mutation with one fixture value certifies coverage
that is not there. And a battery run over a directory, a glob, or "every caller
of X" has a green that expires, because master owns the tree it ran on.

## Where it lives

**The PR body.** Measured on this repo 2026-08-23:
`gh api repos/areqag/gqlc` returns `squash_merge_commit_title: PR_TITLE` and
`squash_merge_commit_message: BLANK`. So a squash merge lands the PR title and
number and **nothing else** — the commit bodies of the branch are destroyed, and
the PR body does not become the commit message either. A battery written into a
commit message is gone the moment it lands.

The PR body is nevertheless the right home, because the merge commit's `(#N)`
suffix is a one-hop reference to a PR that stays readable, whereas a commit body
leaves no trace to follow. Not `kingdom-state/`: that is not in the diff a later
reader is holding. A file in the branch is permitted and sensible when the
battery is a runnable script worth keeping — and it is the only form that lands
IN the tree, so prefer it when a later reader will want to re-run rather than
just read.

One known cost, accepted: a PR body goes stale at the head. A battery recorded
against an earlier SHA and not re-run after a rebase describes bytes that did
not merge. Under decision 0006 a forced rebase must be content-identical, which is
what keeps that cheap — an identical-content move does not invalidate the rows,
and a content-changing move is an ordinary new change that owes fresh ones.

## What a judge owes it, where a judge reads at all

gqlc-cn4x's question 5: may a judge PASS on the author's battery alone?

**No, and V.2.1 already says so** — it requires that every guard be mutated by
the signer, and the standard binds the signer whoever wrote the evidence. So on
a PR that IS reviewed (a design-gate PR, a constitution amendment, or a review a
citizen asked for), the author's rows are a starting point and a re-run, not a
substitute: the judge re-runs the recorded commands, and adds at least one
mutation the author did not declare. The saving is real and is the point of the
lever — construction is replaced by audit plus a delta — and it is a saving in
labour, not in standard.

Nothing here lowers V.2.1 and nothing here needs a constitutional amendment.

## Relation to decision 0004

gqlc-cn4x's question 6 asked whether this shares a mechanism with the
non-binding-pass bead. **It does not.** decision 0004 relocates *who reads and when*;
this decision fixes *what the author leaves behind*. They touch at one point only: a
patrol round (decision 0004 §2) reading merged code finds the battery in the squash
commit message, and can re-run it cheaply. That is a benefit of the two
existing, not a shared mechanism, and neither depends on the other landing.

One consequence worth naming, since patrol reads the tree and not the PR list:
a battery recorded only in a PR body is reachable from a merge commit but is not
greppable in a clone. That is the argument for the in-branch script form above,
and it is a preference here rather than a requirement, because requiring a file
on every guard PR would put scaffolding in the tree faster than anyone removes
it.
