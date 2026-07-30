# ADR 0023 — required_status_checks.strict: leave false

**Status:** Accepted
**Date:** 2026-07-29
**Bead:** gqlc-fer

## Context

GitHub branch protection's `required_status_checks.strict` flag, when true,
requires every PR's head branch to be up to date with the base before its
status checks count as passing. In practice this means each PR must be rebased
on master — and CI re-run — after every other PR merges.

The repo runs one or two autonomous merge loops concurrently (observed
2026-07-27), each capable of merging several PRs per hour. With `strict=true`
every merge blocks all open PRs until they rebase and re-pass CI. At two loops
× multiple merges/hour, this serializes the pipeline completely: PR N cannot
merge until PR N-1 has merged *and* its successor has rebased and re-passed.
Loop throughput collapses to 1 merge per CI wall-time, which is currently
several minutes per run.

The risk of `strict=false` materialized once, on PR #421: two PRs that both
touched `internal/schema/gql` merged without either being rebased on the other.
The combined tree was verified manually and was clean.

## Decision

Leave `required_status_checks.strict = false`.

The throughput cost of strict serialization is prohibitive given the loop
velocity. The manual combined-diff check is an adequate mitigation for the
observed overlap class (two PRs touching the same package): the reviewer
inspects the combined diff before approving the second merge. The risk accepted
is:

> CI computed against a stale base is accepted. If two PRs touch overlapping
> paths, the human or agent reviewer must verify the combined diff before
> merging the second PR.

## Considered alternatives

**Turn strict=true.** Correct by construction: CI always runs against the exact
tree that will land. The cost is throughput: every merge forces a rebase + CI
cycle on all remaining open PRs. At current loop velocity (multiple merges/hour,
two loops) this would reduce effective throughput to one merge per CI wall-time.
Not adopted.

**Selective rebase on path overlap.** Detect overlapping changed paths at merge
time and require a rebase only when detected. GitHub's branch protection does
not support conditional strictness; this would require bespoke tooling in the
merge loop. Deferred — the manual check covers the same ground at lower
implementation cost.

## Consequences

- `required_status_checks.strict` remains false on master.
- The merge loop's reviewer step (human or agent) must check for path overlap
  before merging any PR where another PR has merged since this PR's last CI run.
- If throughput drops (e.g., loop slows to one merge per hour), the case for
  turning strict on strengthens and this ADR should be revisited.
