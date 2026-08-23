#!/usr/bin/env bash
# Tests for `just check-shared-config` — bd gqlc-qhno / GH #1189.
#
# The defect: `core.bare = true` appeared in the shared .git/config of the main
# worktree, and every git command in the repository CLAUDE.md designates for
# read-only research died with "fatal: this operation must be run in a work
# tree". core.worktree has the same blast radius and no tripwire either.
#
# Three things this file has to establish, because any one alone is decoration:
#
#   A. The recipe is WIRED — `just test` depends on it. A check nobody invokes
#      is not a check, and a detector that exits 0 is not a gate.
#   B. The recipe REFUSES each key in the named set, and ACCEPTS the values
#      that are legitimate. The accept rows are the ones that would lock a
#      developer out of `just test` if the guard over-matched.
#   C. The refusal names WHICH key drifted. Two keys with one shared message
#      are indistinguishable, and deleting either from the set leaves a
#      verdict-only row green — the same trap that let a mutation survive on
#      the commit-msg guard (bd gqlc-n8n0).
#
# And the row that motivates the whole design: the drift is invisible from a
# LINKED worktree behaviourally, so a probe built on `git rev-parse
# --is-bare-repository` reports healthy from every seat while the shared cwd is
# bricked. `linked worktree still works` pins that asymmetry so the reason for
# reading the config cannot be refactored away by someone who has not seen it.
#
# Run via: just test-hooks
set -u

# When run under a git hook (this file runs from pre-push via `just test`),
# GIT_DIR etc. leak in and would redirect every throwaway repo's git commands
# to the parent repo. Isolate completely.
# Through the SHARED line rather than a private copy of it (bd gqlc-o9wz).
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
# Read out of just's parsed recipe graph rather than grepped out of the
# justfile, so a dependency sitting in a comment cannot answer for a wired one.
for target in test doctor; do
    if just --dump --dump-format json 2>/dev/null | python3 -c '
import json, sys
d = json.load(sys.stdin)
deps = [x["recipe"] for x in d["recipes"][sys.argv[1]]["dependencies"]]
sys.exit(0 if "check-shared-config" in deps else 1)
' "$target"; then
        ok "wiring: 'just $target' depends on check-shared-config"
    else
        bad "wiring: 'just $target' does not depend on check-shared-config — nothing runs it"
    fi
done

# --- B/C. the recipe over throwaway repositories ----------------------------
# A fresh main worktree, restored to health between rows.
REPO="$TMP/repo"
"${GIT[@]}" init -q -b master "$REPO"
printf 'a\n' >"$REPO/f"
"${GIT[@]}" -C "$REPO" add f
"${GIT[@]}" -C "$REPO" commit -q -m init

# $1=name $2=expected(reject|accept) [$3=substring the refusal must contain]
#
# $3 is not decoration: every refusal carries the same "shared git config"
# sentence, so a verdict-only row cannot tell a core.bare refusal from a
# core.worktree one, and dropping either key from the set would leave such a
# row green. It must also be the KEY-NAMING part of the message — a bare
# "core.bare" is satisfied by the repair line at the bottom of the refusal,
# which let a mutation that stopped naming the key survive.
recipe_case() {
    local name="$1" expected="$2" reason="${3:-}" decision out
    if out=$(just -f "$ROOT/justfile" check-shared-config "$REPO" 2>&1); then
        decision=accept
    else
        decision=reject
    fi
    if [ "$decision" != "$expected" ]; then
        bad "$name (expected $expected, got $decision)"
    elif [ -n "$reason" ] && ! printf '%s' "$out" | grep -qF "$reason"; then
        bad "$name (rejected, but not naming $reason: $out)"
    else
        ok "$name"
    fi
}

# The healthy baseline. If this ever reads reject, every reject row below is
# meaningless — they would be refusing a repo that was already refused.
recipe_case "a healthy repo is accepted" accept

# core.bare — the key actually observed on 2026-08-22.
"${GIT[@]}" -C "$REPO" config core.bare true
recipe_case "core.bare=true is refused" reject "error: core.bare is"
"${GIT[@]}" -C "$REPO" config --unset-all core.bare
recipe_case "the documented repair clears core.bare" accept

# false is what `git init` writes, so refusing it would refuse every repo.
"${GIT[@]}" -C "$REPO" config core.bare false
recipe_case "core.bare=false, which git init itself writes, is accepted" accept
"${GIT[@]}" -C "$REPO" config --unset-all core.bare

# core.worktree — same blast radius, and it has no legitimate value here, so
# unlike core.bare there is no accepted spelling other than absent.
"${GIT[@]}" -C "$REPO" config core.worktree /tmp/somewhere-else
recipe_case "core.worktree pointing elsewhere is refused" reject "error: core.worktree is"
"${GIT[@]}" -C "$REPO" config --unset-all core.worktree
recipe_case "the documented repair clears core.worktree" accept

# A key set TWICE, drifted value FIRST. Measured: `git config --get` prints
# only the LAST value and exits 0, so this ordering reads perfectly clean
# through --get and is the only ordering that holds --get-all in place. With
# the values the other way round, --get returns the drifted one anyway and the
# row cannot tell the two flags apart (it scored an equivalent mutant when it
# was written that way).
"${GIT[@]}" -C "$REPO" config --add core.bare true
"${GIT[@]}" -C "$REPO" config --add core.bare false
recipe_case "core.bare set twice, drifted value masked by a good one, is refused" \
    reject "error: core.bare is"
"${GIT[@]}" -C "$REPO" config --unset-all core.bare
recipe_case "clearing both values restores health" accept

# --- the asymmetry that dictates the probe ----------------------------------
# Why the recipe reads the config instead of asking git whether it is bare:
# core.bare disables the MAIN worktree only. A linked worktree — which is what
# every seat has, and where `just test` actually runs — keeps working and
# reports --is-bare-repository FALSE while the shared cwd is bricked.
LINKED="$TMP/linked"
"${GIT[@]}" -C "$REPO" worktree add -q --no-track -b topic "$LINKED" master
"${GIT[@]}" -C "$REPO" config core.bare true

if "${GIT[@]}" -C "$REPO" status --short >/dev/null 2>&1; then
    bad "asymmetry: core.bare=true left the MAIN worktree working (premise gone)"
else
    ok "asymmetry: core.bare=true bricks the main worktree"
fi

if "${GIT[@]}" -C "$LINKED" status --short >/dev/null 2>&1; then
    ok "asymmetry: the linked worktree keeps working, so no seat notices"
else
    bad "asymmetry: the linked worktree broke too (premise gone)"
fi

if [ "$("${GIT[@]}" -C "$LINKED" rev-parse --is-bare-repository)" = "false" ]; then
    ok "asymmetry: --is-bare-repository reads false from the linked worktree"
else
    bad "asymmetry: --is-bare-repository reported the drift, contradicting the design note"
fi

if [ "$("${GIT[@]}" -C "$LINKED" config --get core.bare)" = "true" ]; then
    ok "asymmetry: the config read DOES see it from the linked worktree"
else
    bad "asymmetry: the config read missed it from the linked worktree — the recipe cannot work"
fi

# And the recipe itself, run from the linked worktree, must refuse: that is the
# arm that reaches the seats.
if just -f "$ROOT/justfile" check-shared-config "$LINKED" >/dev/null 2>&1; then
    bad "the recipe run from a linked worktree missed the drift"
else
    ok "the recipe run from a linked worktree refuses"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
