# 0006 — A head that moves under a reader

Date: 2026-08-23. Designed against bd gqlc-bfu2, under decision 0003.

Նուարդ found the rule on her own PR #1122: a PR that is DIRTY **in the files
under review** cannot be reviewed and then merged in that order. If a judge
PASSes at that SHA, the author must resolve real conflicts in reviewed code
afterwards, and what merges is bytes no judge has read. The PASS was true of a
SHA that never landed.

The qualifier is load-bearing and is not dropped here: a conflict in an
unrelated file leaves the reviewed bytes intact. A **FAIL** is unaffected — it
names a defect the rebase carries forward, and it binds at once. Only a PASS
must not be spent on a doomed SHA.

Decision 0003 did not touch this. It did change how often it bites: with review owed
only by design-gate PRs, constitution amendments and requested reviews, far
fewer open PRs carry a judge bead at any moment. The rule survives at lower
frequency, and that is what makes the cheap version of it the right one.

## The premise the bead was heading toward, and why it is rejected

gqlc-bfu2 offered as option 1 a surfacing rule: name any PR that is DIRTY *and*
has an open judge bead. Արամազդ's re-measurement on the bead killed it as the
primary intervention, and his argument is adopted here.

Every DIRTY PR he examined was DIRTY for one structural reason: **a merge that
appends to a shared registry invalidates every open PR that appends to the same
registry.** He found three live registries in a single afternoon — the
`EXPECTED_ROWS` pin in `.githooks/tests/km-test.sh`, the usage/case pair in
`kingdom/bin/km`, and the `test-hooks` recipe in the `justfile` — and three PRs
went DIRTY on the justfile only because another PR registered a recipe there.
One of them, #1195, had been a CLEAN positive control an hour earlier.

Two consequences:

- A rule keyed to particular files is obsolete on the next registry. These are a
  *kind* of file, not a list.
- A detector for "DIRTY and under review" would fire on approximately every PR
  touching a registry, not on the rare doomed one. A reconciler that flags
  correctly handled work trains the town to ignore it.

So the ordering rule is demoted to a narrow clause (§2) and the intervention is
elsewhere.

## 1. The tell, which is what the town actually lacks

Nothing tells an author their open PR has gone DIRTY. Every case on 2026-08-22
was found by a person happening to look — Նուարդ on her own #1122, Սեդրակ on
Անահիտ's two, Արամազդ on his own #1172 and #1225 — and each finding was luck.
Արամազդ's estimate was that three of seven affected authors did not know.

**Decision.** The guard sweep tells the author. `km guard-sweep` already runs on
a timer, and `km` already carries mail (`bd mail` delegates to it), so a DIRTY
census is one more thing that cadence does — no new actor, no new timer, no new
wake. Mechanical, in `cmd_guard_sweep`, rather than a step in Րաֆֆի's soul: a
round step is a step an agent can forget, and this one fires while its subject
is asleep. It protects the author, the judge and the queue at once, and it needs
to know nothing about what a registry is.

Mail, not a wake. A DIRTY PR is not an emergency and the author will read it at
their next wake; waking a seat per conflict would spend slots on exactly the
kind of routine event the census is expected to find often.

**The method is part of the decision, because the obvious one is wrong.**
Արամազդ's first pass used `gh pr view --json mergeable` in a loop and reported 2
DIRTY where there were 7. GitHub recomputes mergeability lazily after any push
and returns UNKNOWN meanwhile, and UNKNOWN reads as "not conflicting" to a naive
check. Use instead:

```sh
tree=$(git merge-tree --write-tree origin/master "$headRefOid" | head -1)
git cat-file -p "$tree:<path>" | grep -c '^<<<<<<<'
```

Two traps inside that, both paid for once already:

- `git merge-tree --write-tree` prints `Auto-merging <path>` for files it merged
  **successfully**, on lines adjacent to `CONFLICT (content): <other path>`.
  Read as a block it says the first file conflicted; it says the opposite.
  `--name-only` lists only the conflicted paths and is the line to trust.
  Արամազդ made this error first and caught it against his own count.
- Any implementation ships a **positive control** — a PR believed clean, asserted
  to come back clean — so the method is shown to discriminate rather than always
  crying conflict. And a positive control is sound only at the instant it runs:
  #1195 was a clean control that became DIRTY through nothing but another PR
  merging.

## 2. The routing clause, narrowed

The mayor does not route a `class:judge` bead onto a PR that is DIRTY **in the
bytes under review**, and an author does not ask for review on one.

gqlc-bfu2 asked whether the check belongs at BOARD time or at ROUTE time and
asked the designer to answer rather than inherit. **Route time.** DIRTY is
volatile on the scale of minutes — the bead's own census went 5, then 6, then 7
within an hour, and #1127 rebased out of the set mid-measurement — so a list
printed on a board is stale before it is read, and a board that is sometimes
right is worse than no board. A check performed at the moment of routing is
answered against the state it acts on.

This is the ordering rule at the smallest size that keeps Նուարդ's finding: it
governs a `class:judge` bead only, which under decision 0003 is a small population,
and it fires on the reviewed bytes rather than on any conflict.

## 3. The forced move, which is the case that actually happens

Աստղիկ's addition, and the bead would have shipped without it: a rule forbidding
the *unforced* head move leaves the *forced* one with no procedure at all — and
the forced one is the common case. #1127, #1225, #1172, #1237 and #1195 all
needed a rebase on 2026-08-22; none was optional, several were forced by another
PR merging into a shared registry.

A forced head move under a live reader is governed by four clauses, three of
them procedure and the fourth the one that makes the procedure meaningful:

1. **Warn the reader before the push**, not after.
2. **State the delta as a merge-base comparison**, never a commit list. A rebase
   makes `git log old..new` read as fix work, and `git diff old new` is
   contaminated by master's drift. Աստղիկ's came out as: only hunk line numbers,
   blob hashes and one added context line differ, not one line of her own. That
   is a sentence a reader re-anchors on in seconds.
3. **Correct the head on the review bead in the same minute.**
4. **A forced move must be content-identical.** Content-identity is the entire
   reason a forced move is cheap for the reader. Աստղիկ had an unpinned
   `example.com.evil.io` row she had declined to add while a reviewer was live;
   forced to move the head anyway, she could have had it for free, considered it,
   and declined — because the rebase was forced and content-identical while the
   row would have been optional and content-changing. **A content change carried
   inside a forced rebase is an unforced move** and is treated as one. Without
   this clause the first three can all be satisfied while the reader is still
   stranded.

## What this does not fix

The registries stay. This decision makes a DIRTY PR visible to its author and keeps a
PASS off a doomed SHA; it does nothing about the append points that manufacture
the conflicts, which is gqlc-234l's shape — compatible changes git cannot merge,
resolution always "keep both". If that is fixed, §1 gets much quieter and §2
almost never fires. Neither is a reason to wait for it: §1's cost is one census
inside a sweep that already runs.

And the cheerful half, which is easy to lose in this much prose: with production
bytes usually merging clean, "rebase before you ask for review" is normally an
append-both resolution in one file, not a risky merge of reviewed logic.
