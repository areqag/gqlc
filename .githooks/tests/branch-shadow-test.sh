#!/usr/bin/env bash
# The detector for bd gqlc-qcen: a LOCAL branch whose name matches an open PR's
# head ref, but whose SHA does not.
#
# THE HAZARD, measured by Սեդրակ 2026-08-22 while ruling on gqlc-q9z2/gqlc-einq:
#
#     worktree ../gqlc-hookspath
#       local  fix/hookspath-inert-detector -> f376e93a   + 1 uncommitted file
#       origin fix/hookspath-inert-detector -> 92e78a89   (PR #1044 head, OPEN)
#
# The seat doing the real work was on a differently-named branch, pushing to the
# PR's remote ref, and did nothing wrong. The consequence is still a local ref
# carrying the exact name of a live PR's branch, two rebases behind its head.
#
# Why that is worse than an ordinary stale branch: every affordance agrees with
# you. `git checkout <name>` hands you the wrong tree, `git log --oneline` looks
# like the branch, `git status` looks like work in progress, and the branch name
# in your prompt is RIGHT — so the usual tell for a stale ref, a wrong name, is
# absent. Only the SHA disagrees, and the SHA is the one thing nobody compares.
# Two live harms: a read (reviewing, grepping or diffing "the #1044 branch"
# locally answers about the wrong commit) and a write (a push, or a lease
# computed against the local ref, force-pushes a stale tree over an open PR).
#
# WHY THIS ANNOUNCES RATHER THAN REFUSES. The acceptance clause on gqlc-qcen is
# two-branched: either no local ref shadows a PR head, "or the divergence is
# announced somewhere a citizen will read". This file takes the second branch,
# deliberately, and the reasons are worth writing down because the first branch
# looks tidier:
#
#   - Local branches and worktrees are PER-MACHINE untracked state. A push gate
#     over them means nothing on a CI runner, which has none, so a refusal here
#     would fail on developer machines and pass in CI — the shape that teaches
#     people the gate is noise.
#   - Several of the ~19 legacy worktrees holding these refs contain UNCOMMITTED
#     work belonging to citizens. A refusal is a coercion to delete, and the
#     disposal decision is the owner's; one unique unpushed commit has already
#     had to be rescued from that population.
#
# So: the FIXTURE arm below is a gate — it fails, hard, if the detector stops
# detecting. The LIVE arm is a report. That split is the point. A detector whose
# only exercise is the live tree certifies nothing on the day the tree is clean,
# which is today: measured 2026-08-23 against 75 registered worktrees and 269
# local refs, both open PRs had a local ref and both agreed with their head.
#
# Run via: just test-hooks
set -u

# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok() {
    pass=$((pass + 1))
    printf 'ok   - %s\n' "$1"
}
bad() {
    fail=$((fail + 1))
    printf 'FAIL - %s\n' "$1"
}

# --- the detector -------------------------------------------------------------
# shadows <repo> < <heads>
#
# <heads> is one `<number> <headRefName> <headRefOid>` per line — the open PRs.
# It is read from stdin rather than fetched here so the fixture arm can drive the
# comparison without a network, and so the live arm can decide separately whether
# it trusts what `gh` said.
#
# Prints one line per shadowing pair:
#     <headRefName> local=<sha> pr=#<n> head=<sha> [worktree=<path>]
# and exits 1 if there was at least one. Exit 0 and silence means every local ref
# that names an open PR's branch agrees with it.
#
# A PR whose branch has NO local ref is not a finding: there is nothing to
# mistake. That is the false-positive direction, and it is the common case — 267
# of the 269 local refs here name no open PR at all.
shadows() {
    local repo="$1" n ref oid loc wt found=0
    while read -r n ref oid; do
        [ -n "${ref:-}" ] || continue
        loc="$(git -C "$repo" rev-parse --verify --quiet "refs/heads/$ref" 2>/dev/null)" || loc=""
        [ -n "$loc" ] || continue
        [ "$loc" = "$oid" ] && continue
        # Which worktree, if any, has it checked out — that is where a citizen
        # would be standing when the affordances lied to them.
        wt="$(git -C "$repo" worktree list --porcelain 2>/dev/null |
            awk -v want="refs/heads/$ref" '
                /^worktree /  { p = substr($0, 10) }
                $0 == "branch " want { print p; exit }')"
        found=1
        if [ -n "$wt" ]; then
            printf '%s local=%s pr=#%s head=%s worktree=%s\n' "$ref" "$loc" "$n" "$oid" "$wt"
        else
            printf '%s local=%s pr=#%s head=%s\n' "$ref" "$loc" "$n" "$oid"
        fi
    done
    [ "$found" -eq 0 ]
}

# --- A. FIXTURE ARM — this is the gate ----------------------------------------
# A miniature repository with one ref of each shape, so every branch of the
# comparison is exercised whether or not the live tree happens to be dirty today.

FIX="$TMP/fix"
git init -q -b master "$FIX"
git -C "$FIX" -c user.email=t@t.invalid -c user.name=t -c commit.gpgsign=false \
    commit -q --allow-empty -m base
BASE="$(git -C "$FIX" rev-parse HEAD)"
git -C "$FIX" -c user.email=t@t.invalid -c user.name=t -c commit.gpgsign=false \
    commit -q --allow-empty -m second
AHEAD="$(git -C "$FIX" rev-parse HEAD)"

# SHADOWING: local ref sits at the older commit, the PR head is the newer one.
git -C "$FIX" branch -q fix/shadowed "$BASE"
# AGREEING: local ref is exactly the PR head.
git -C "$FIX" branch -q fix/agrees "$AHEAD"
# ABSENT: fix/no-local has no local ref at all.

HEADS="1044 fix/shadowed $AHEAD
1379 fix/agrees $AHEAD
1387 fix/no-local $AHEAD"

OUT="$(printf '%s\n' "$HEADS" | shadows "$FIX")" && RC=0 || RC=1

case "$OUT" in
    *"fix/shadowed local=${BASE:0:40}"*) ok "a local ref BEHIND its PR head is reported" ;;
    *) bad "a local ref BEHIND its PR head was NOT reported — got: ${OUT:-<nothing>}" ;;
esac
case "$OUT" in
    *"pr=#1044"*) ok "the report names the PR the local ref is shadowing" ;;
    *) bad "the report does not name the PR number, so a reader cannot check the head" ;;
esac
case "$OUT" in
    *"head=$AHEAD"*) ok "the report names the head SHA the local ref disagrees with" ;;
    *) bad "the report does not name the real head SHA — the one thing nobody compares" ;;
esac
# The falsifier for a detector that reports everything. Without these two rows,
# a `shadows` that printed every input line would pass every row above.
case "$OUT" in
    *"fix/agrees"*) bad "a local ref that AGREES with its PR head was reported — the detector would redden correct state" ;;
    *) ok "a local ref that AGREES with its PR head is not reported" ;;
esac
case "$OUT" in
    *"fix/no-local"*) bad "a PR whose branch has NO local ref was reported — there is nothing there to mistake" ;;
    *) ok "a PR with no local ref of that name is not reported" ;;
esac

# A detector that exits 0 is not a detector (bd gqlc-z1qw). The exit status is
# the half a caller in a pipeline reads, and it is separate from the text above.
if [ "$RC" -eq 1 ]; then
    ok "the detector EXITS NON-ZERO when it found a shadowing pair"
else
    bad "the detector found a pair and still exited 0 — nothing scripted around it could tell"
fi

printf '%s\n' "1379 fix/agrees $AHEAD" | shadows "$FIX" >/dev/null && CLEANRC=0 || CLEANRC=1
if [ "$CLEANRC" -eq 0 ]; then
    ok "the detector exits 0 when every local ref agrees"
else
    bad "the detector exits non-zero on clean state — it would cry wolf forever"
fi

# The worktree annotation, which is what turns a report into an action. Asserted
# separately because it is the part that needs `git worktree` to still speak the
# porcelain this parses.
WT="$TMP/fix-wt"
git -C "$FIX" worktree add -q --detach "$WT" "$BASE" 2>/dev/null
git -C "$FIX" -C "$WT" checkout -q fix/shadowed 2>/dev/null ||
    git -C "$WT" checkout -q fix/shadowed 2>/dev/null
WOUT="$(printf '%s\n' "1044 fix/shadowed $AHEAD" | shadows "$FIX")" || true
case "$WOUT" in
    *"worktree=$WT"*) ok "the report names the worktree holding the shadowing ref" ;;
    *) bad "the report does not name the holding worktree, so the owner cannot find it: $WOUT" ;;
esac

# --- B. LIVE ARM — a report, not a gate ---------------------------------------
# Skipped, loudly and by name, when `gh` cannot answer. A skip that is not
# counted and not printed is indistinguishable from a pass, and that is the shape
# this repo has been bitten by more than once.

# The arm is a function so that its REPORTING branch can be exercised against
# the fixture repo below. Left inline, that branch would be dead code on every
# day the tree is clean — which is most days, and is today — so the first time it
# ever ran would be the day someone needed it.
live_arm() {
    local repo="$1" heads out n
    if ! heads="$(cat)"; then
        ok "live arm: SKIPPED — no open-PR head list could be read."
        return
    fi
    n="$(printf '%s' "$heads" | grep -c . || true)"
    out="$(printf '%s\n' "$heads" | shadows "$repo")" || true
    if [ -z "$out" ]; then
        ok "live arm: no local ref shadows any of the $n open PR head(s)"
        return
    fi
    # NOT a bad(). See the header: local refs are untracked per-machine state and
    # several of the trees holding them carry other citizens' uncommitted work,
    # so this announces and leaves disposal to the owner (gqlc-qcen).
    ok "live arm: REPORT — $(printf '%s\n' "$out" | grep -c .) local ref(s) shadow an open PR head, listed below"
    printf 'WARN - a local ref names an open PR branch but points somewhere else:\n'
    printf '       %s\n' "$out"
    printf '%s\n' \
        "       Do NOT push from these, and do not compute a --force-with-lease" \
        "       against them: the remote is ahead. Match the SHA, never the name." \
        "       Disposal is the worktree owner's call — some of these trees hold" \
        "       uncommitted work. bd gqlc-qcen."
}

# B1. The reporting branch, driven over the fixture. This is the row that keeps
# the WARN block from rotting between the days it is needed.
REPORT="$(printf '%s\n' "1044 fix/shadowed $AHEAD" | live_arm "$FIX" 2>&1)"
case "$REPORT" in
    *"WARN - a local ref names an open PR branch"*) ok "live arm: the reporting branch renders a WARN block naming the pair" ;;
    *) bad "live arm: the reporting branch printed no WARN block: $REPORT" ;;
esac
case "$REPORT" in
    *"force-with-lease"*) ok "live arm: the WARN block names the write harm (a lease computed against the stale ref)" ;;
    *) bad "live arm: the WARN block does not say what not to do with the finding" ;;
esac
case "$REPORT" in
    *"FAIL"*) bad "live arm: the reporting branch FAILS the suite — it is meant to announce, not to coerce a deletion (see the header)" ;;
    *) ok "live arm: the reporting branch announces without failing the suite" ;;
esac

# B2. The real tree.
if ! command -v gh >/dev/null 2>&1; then
    ok "live arm: SKIPPED on the real tree — gh is not on PATH. B1 above still exercised both branches."
elif ! LIVE_HEADS="$(gh pr list --state open --limit 300 \
    --json number,headRefName,headRefOid \
    --jq '.[] | "\(.number) \(.headRefName) \(.headRefOid)"' 2>/dev/null)"; then
    ok "live arm: SKIPPED on the real tree — gh errored (no auth, no network, or rate limit). B1 above still exercised both branches."
else
    printf '%s\n' "$LIVE_HEADS" | live_arm "$ROOT"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
