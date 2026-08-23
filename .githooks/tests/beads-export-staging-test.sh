#!/usr/bin/env bash
# Tests for `just check-beads-export` — bd gqlc-c2ch / GH #1170.
#
# The defect: from a seat worktree every `bd update` ended with
#
#     ✓ Updated issue: gqlc-cn8e — ...
#     Warning: auto-export: git add failed: exit status 128: fatal: this
#     operation must be run in a work tree
#
# The DB write landed, bd printed ✓ and exited 0, and `.beads/issues.jsonl`
# stopped tracking the ledger with nothing failing. Reproduced 2026-08-23 on a
# throwaway repo driving the real bd: the cause is `core.bare = true` in the
# SHARED config, and it is not specific to worktrees — bd stages into the MAIN
# checkout from wherever it is run, so a seat inherits the main checkout's
# brickedness while its own `git status` stays clean.
#
# What this file has to establish:
#
#   A. The recipe is WIRED. A detector that exits 0 is not a gate, and one no
#      recipe depends on is not even that.
#   B. It refuses each way the staging operation can be broken, and ACCEPTS a
#      healthy repo — the accept rows are what stop it locking out `just test`
#      for every seat in the town.
#   C. The refusals are DISTINGUISHABLE, so deleting either arm of the recipe
#      leaves a row red rather than a verdict-only row green.
#   D. THE SEAT ARM: run from a LINKED worktree over a bricked main checkout, it
#      refuses. That is the row that did not exist on 2026-08-22, and its
#      absence is the whole bead — every probe anyone ran from the seat
#      (`git rev-parse --is-inside-work-tree`, `git status`, `env | grep ^GIT`)
#      answered healthy.
#
# Run via: just test-hooks
set -u

# When run under a git hook (this file runs from pre-push via `just test`),
# GIT_DIR etc. leak in and would redirect every throwaway repo's git commands to
# the parent repo.
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

HOOKS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT="$(cd "$HOOKS_DIR/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

GIT=(git -c user.email=t@t.invalid -c user.name=t -c commit.gpgsign=false)

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

# --- A. wiring --------------------------------------------------------------
# Out of just's parsed recipe graph, not grepped out of the justfile, so a
# dependency sitting in a comment cannot answer for a wired one. `-f` because a
# bare `just --dump` resolves whatever justfile the cwd finds.
for target in test doctor; do
    if just -f "$ROOT/justfile" --dump --dump-format json 2>/dev/null | python3 -c '
import json, sys
d = json.load(sys.stdin)
deps = [x["recipe"] for x in d["recipes"][sys.argv[1]]["dependencies"]]
sys.exit(0 if "check-beads-export" in deps else 1)
' "$target"; then
        ok "wiring: 'just $target' depends on check-beads-export"
    else
        bad "wiring: 'just $target' does not depend on check-beads-export — nothing runs it"
    fi
done

# --- B/C. the recipe over throwaway repositories ----------------------------
REPO="$TMP/repo"
"${GIT[@]}" init -q -b master "$REPO"
mkdir -p "$REPO/.beads"
printf '{"id":"scr-1"}\n' >"$REPO/.beads/issues.jsonl"
printf 'a\n' >"$REPO/f"
"${GIT[@]}" -C "$REPO" add f .beads/issues.jsonl
"${GIT[@]}" -C "$REPO" commit -q -m init

# $1=name $2=expected(reject|accept) $3=dir [$4=substring the refusal must carry]
#
# $4 is not decoration. The two arms of the recipe fail for different reasons —
# staging refused outright, versus staging succeeding against the wrong tree —
# and both refusals cite the same bead id. A verdict-only row cannot tell them
# apart, so deleting one arm would leave every reject row below green.
recipe_case() {
    local name="$1" expected="$2" dir="$3" reason="${4:-}" decision out
    if out=$(just -f "$ROOT/justfile" check-beads-export "$dir" 2>&1); then
        decision=accept
    else
        decision=reject
    fi
    if [ "$decision" != "$expected" ]; then
        bad "$name (expected $expected, got $decision: $out)"
    elif [ -n "$reason" ] && ! printf '%s' "$out" | grep -qF "$reason"; then
        bad "$name (rejected, but not naming '$reason': $out)"
    else
        ok "$name"
    fi
}

# The healthy baseline. If this reads reject, every reject row below is
# meaningless — they would be refusing a repo that was already refused.
recipe_case "a healthy repo is accepted" accept "$REPO"

# core.bare — the drift actually observed, and the one that produced the bd
# warning. Arm A (the git add probe) is what sees it.
"${GIT[@]}" -C "$REPO" config core.bare true
recipe_case "core.bare=true, which is what bd's git add hit, is refused" \
    reject "$REPO" "cannot stage .beads/issues.jsonl"
"${GIT[@]}" -C "$REPO" config --unset-all core.bare
recipe_case "clearing core.bare restores health" accept "$REPO"

# core.worktree — and the measurement that says arm A cannot cover it. Under a
# core.worktree redirect `git add` exits 0 while staging a path under the OTHER
# tree, so the staging probe passes on a repo where bd's export goes to the
# wrong file. This premise row is why arm B exists; if git ever starts failing
# here, the row says so instead of the arm silently becoming redundant.
mkdir -p "$TMP/elsewhere/.beads"
printf '{"id":"scr-1"}\n' >"$TMP/elsewhere/.beads/issues.jsonl"
"${GIT[@]}" -C "$REPO" config core.worktree "$TMP/elsewhere"
if "${GIT[@]}" -C "$REPO" add --dry-run -- .beads/issues.jsonl >/dev/null 2>&1; then
    ok "premise: under a core.worktree redirect 'git add' exits 0, so arm A alone is blind"
else
    bad "premise: 'git add' failed under core.worktree (premise gone — arm B may be redundant)"
fi
recipe_case "a core.worktree redirect, which staging does NOT fail on, is refused" \
    reject "$REPO" "reports its work tree as"
"${GIT[@]}" -C "$REPO" config --unset-all core.worktree
recipe_case "clearing core.worktree restores health" accept "$REPO"

# The export target absent entirely. bd creates it, so its absence means the
# export never ran here — the same silence by a different route.
mv "$REPO/.beads/issues.jsonl" "$TMP/held.jsonl"
recipe_case "an absent export target is refused" \
    reject "$REPO" "does not exist"
mv "$TMP/held.jsonl" "$REPO/.beads/issues.jsonl"
recipe_case "restoring the export target restores health" accept "$REPO"

# Not a repository at all. Fail closed: a probe that cannot resolve bd's target
# has not established that staging works.
mkdir -p "$TMP/notarepo"
recipe_case "a directory that is not a git repository is refused" \
    reject "$TMP/notarepo" "is not a git repository"

# --- D. the seat arm --------------------------------------------------------
# Every seat has a linked worktree, and that is where the defect was reported
# from. bd stages into the MAIN checkout regardless, so the recipe has to reach
# across; the premise rows below pin why the obvious local probes cannot.
LINKED="$TMP/linked"
"${GIT[@]}" -C "$REPO" worktree add -q --no-track -b topic "$LINKED" master
recipe_case "from a healthy linked worktree the recipe accepts" accept "$LINKED"

"${GIT[@]}" -C "$REPO" config core.bare true

if "${GIT[@]}" -C "$LINKED" status --short >/dev/null 2>&1; then
    ok "premise: the linked worktree's own git status stays clean while main is bricked"
else
    bad "premise: the linked worktree broke too (premise gone — a local probe would have caught it)"
fi

if [ "$("${GIT[@]}" -C "$LINKED" rev-parse --is-inside-work-tree 2>/dev/null)" = "true" ]; then
    ok "premise: --is-inside-work-tree answers true from the seat, as it did on 2026-08-22"
else
    bad "premise: --is-inside-work-tree did not answer true from the seat (premise gone)"
fi

# THE ROW. Run from the linked worktree over the bricked main checkout, the
# recipe must refuse — and must refuse for the staging reason, which is the one
# bd reported.
recipe_case "THE ROW: from a linked worktree, a bricked main checkout is refused" \
    reject "$LINKED" "cannot stage .beads/issues.jsonl"

"${GIT[@]}" -C "$REPO" config --unset-all core.bare
recipe_case "from a linked worktree, a repaired main checkout is accepted" accept "$LINKED"

# --- the git environment ----------------------------------------------------
# git exports GIT_DIR to hooks and `just test` runs from pre-push. Those beat
# `git -C`, so a recipe that did not unset them would judge whatever repository
# the hook was running in. Both directions, because only one of them is a
# false PASS and only the other is a false FAIL.
HEALTHY="$TMP/healthy"
"${GIT[@]}" init -q -b master "$HEALTHY"
mkdir -p "$HEALTHY/.beads"
printf '{"id":"scr-1"}\n' >"$HEALTHY/.beads/issues.jsonl"
"${GIT[@]}" -C "$HEALTHY" add .beads/issues.jsonl
"${GIT[@]}" -C "$HEALTHY" commit -q -m init

"${GIT[@]}" -C "$REPO" config core.bare true
if GIT_DIR="$HEALTHY/.git" GIT_WORK_TREE="$HEALTHY" \
    just -f "$ROOT/justfile" check-beads-export "$REPO" >/dev/null 2>&1; then
    bad "env: a GIT_DIR pointing at a healthy repo hid the bricked one"
else
    ok "env: a GIT_DIR pointing at a healthy repo does not hide the bricked one"
fi
if GIT_DIR="$REPO/.git" GIT_WORK_TREE="$REPO" \
    just -f "$ROOT/justfile" check-beads-export "$HEALTHY" >/dev/null 2>&1; then
    ok "env: a GIT_DIR pointing at the bricked repo does not condemn a healthy one"
else
    bad "env: a GIT_DIR pointing at the bricked repo condemned a healthy one"
fi
"${GIT[@]}" -C "$REPO" config --unset-all core.bare

# And the scrub is fail-closed on its own absence. A `source` of a missing file
# under `set -u` without `-e` prints to stderr and carries on, leaving GIT_*
# standing — the recipe would then run with exactly the redirect the two rows
# above rule out, and pass. Arranged by copying the justfile somewhere with no
# .githooks beside it, which is what justfile_directory() resolves against.
mkdir -p "$TMP/nosandbox"
cp "$ROOT/justfile" "$TMP/nosandbox/justfile"
if just -f "$TMP/nosandbox/justfile" check-beads-export "$HEALTHY" >/dev/null 2>&1; then
    bad "env: a missing git-env-sandbox.sh was ignored rather than refused"
else
    ok "env: a missing git-env-sandbox.sh is refused rather than silently skipped"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
