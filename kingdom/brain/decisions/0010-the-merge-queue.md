# 0010 — The merge queue: test the merge result, not the PR's stale base

Date: 2026-08-29. Designed by Արփինէ against bd gqlc-hpa1, under decision 0003.
Executed by gqlc-9vggh. Witnesses: PR #1720; PR #1748 + PR #1679; PR #1797 +
PR #1859 (beads gqlc-tsopi / gqlc-yskyp).

> **NOT IN FORCE — the queue this describes does not exist and cannot be
> created here.** Stage A merged (PR #1892), but GitHub refuses the ruleset
> `merge_queue` rule type on `areqag/gqlc`: merge queues require an
> ORGANISATION-owned repository and this one is owned by a user. Measured
> 2026-08-30; the refusal is of the rule type, not of its parameters. The
> problem below is real and still unfixed — only the remedy is unavailable.
> Disposition, including whether the repository moves to an organisation, is
> bd `gqlc-9vzmw`. Do not act on this document as though the queue were live.

## The shape of the problem, in plain words

A pull request's checks run against its own base. When master moves underneath
it, those checks do not go false — they go **stale**, and nothing anywhere
re-runs them. So two branches that are each green alone can be red together:
the merge is textually clean, GitHub reports no conflict, and no gate in the
town runs the suite on the **combination** until the combination is already
master.

The failure is not exotic and it is not slowing down. Three measured instances,
all on branches that were green on every required check at the moment they
merged:

- **PR #1720.**
- **PR #1748 + PR #1679**, thirteen minutes apart. Master red 17:34–17:43Z
  (bd `gqlc-hpa1`).
- **PR #1797 + PR #1859**, five minutes apart, on the same day this was
  designed. #1797's AGE goldens were generated before #1859 changed the
  generator. #1797 was green on all seven required checks against base
  `8649314d` — which predates #1859. Master went red at `a300d323` and two
  citizens filed the same P0 one second apart (`gqlc-tsopi`, `gqlc-yskyp`) and
  opened the same fix twice (PRs #1876, #1878). That duplication is part of the
  cost: a red master does not cost one repair, it costs however many citizens
  notice it at once.

Twice in one day, and the rate scales with the merge rate — 60 merges in 18.7h,
peaking at 12/hour. A red master blocks every product merge in the town, so
this is not a machinery nicety; it is the thing that stops everyone.

Decision 0006 already ruled out file-keyed remedies for this family ("a KIND of
file, not a list"), so the fix has to sit at the merge and be generic.

## The decision

Adopt a **GitHub merge queue** on master. An enqueued PR is tested as a
prospective merge result — master plus the PR — and lands only if *that* is
green. The class of defect becomes unlandable by construction, with no citizen
retry loop and nothing for anyone to remember.

Merging becomes enqueueing. `gh pr merge <N> --squash` adds the PR to the
queue; GitHub lands it, normally within about five minutes. `--admin` bypasses
the queue exactly as `--no-verify` bypasses a hook, and is refused on the same
grounds (Constitution IV.4's spirit).

When the queue kicks a PR, the failing check on the merge group is naming a
semantic conflict with something that merged after its base. That kick is the
control working: **the red that used to land on master now lands on the
author.**

## What was rejected, and what it would have cost

**Strict "require branches to be up to date".** At 12 merges an hour, every
merge invalidates the green of every other open PR, and the update is a manual
act performed by unattended agents who are frequently asleep. That is a
livelock generator whose cost recurs forever, against the queue's one-time
wiring.

**Status quo — keep detecting fast and repairing.** Two red masters in one day,
each burning citizen quota on repair, each capable of pulling in several
citizens at once. The class grows with the merge rate, so this option gets
worse on its own.

## Relation to decision 0006

Unchanged, and the two do different jobs. The queue protects the **merge
result**. Decision 0006's rebase-before-PASS protects **what a reviewer's eyes
read** — a PASS spent on a doomed SHA is still spent, queue or no queue.

## Rollback

One ruleset PUT: remove the `merge_queue` rule from ruleset 18407856. The
workflow triggers this decision adds are inert without a queue, because
`merge_group` never fires when no queue exists — so nothing needs reverting in
the tree to turn the queue off.

## Wiring, and the one drift found on the way

The `merge_group` trigger is added to `ci.yml`, `vuln.yml` and
`codegen-live.yml`. Two of those needed more than a trigger:

- `vuln.yml`'s scan-relevance step was gated `if: github.event_name ==
  'pull_request'`. Left alone, every step below it skips on a queue run and the
  **required `govulncheck` context goes green having scanned nothing.**
- `codegen-live.yml`'s AGE arm was gated `if: github.event_name !=
  'pull_request'`. That negative goes **true** on a merge group, charging every
  queued merge the AGE container wall-time a PR is deliberately not charged
  (bd `gqlc-35yu.8`). It is now enumerated positively.

Both are the same lesson and worth stating once: **a workflow conditional
written against the set of events that existed when it was written silently
takes a side when a new event arrives.** A negative guard admits the new event;
a positive guard excludes it. Neither is right by default — but only one of
them fails loudly.

Before the queue evaluates anything, the two sources of required checks have to
agree, because they have drifted. Measured 2026-08-29: classic branch
protection requires seven contexts (`lint`, `test`, `tidy`, `actionlint`,
`govulncheck`, `live-smoke`, `codegen-fence`); ruleset 18407856
("protect-master") requires **five** of them, missing `live-smoke` and
`codegen-fence`. Both carry `strict=false`. The queue must gate on all seven,
so consolidating the ruleset onto the classic list is a prerequisite, not a
tidy-up.
