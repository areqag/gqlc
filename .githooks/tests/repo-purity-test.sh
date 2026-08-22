#!/usr/bin/env bash
# Unit tests for .githooks/repo-purity.
#
# The guard's claim is behavioural — "running the tests did not change the repo
# they ran in, and if it did, the run FAILS" — so every row here drives the real
# script against a throwaway town and asserts on its exit status and on the
# fixture's resulting state, not on what it prints. A row that only asserted the
# message would stay green if the script exited 0, which is the exact defect
# this guard exists to refuse (bd gqlc-2zet, gqlc-z1qw).
#
# THE SANDBOX SHAPE IS PART OF THE CLAIM. Every fixture here is a repo PLUS A
# LINKED WORKTREE, with GIT_DIR exported to the worktree's gitdir, because the
# damage under test cannot occur in any other shape: git decides bareness from
# the gitdir's NAME, and only a linked worktree's gitdir
# (`.git/worktrees/<seat>`) fails to end in `.git`. Rebuilt on a plain repo,
# every dangerous row below goes green while testing nothing (bd gqlc-tl78).
#
# Run via: just test-hooks
set -u

# When run under a git hook (pre-push via `just test`), GIT_DIR and friends leak
# in and would redirect the fixture's git commands at the parent repo. This
# suite exports GIT_DIR deliberately, per fixture, inside subshells only.
unset "${!GIT_@}"

GUARD="$(cd "$(dirname "$0")/.." && pwd)/repo-purity"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0

check() {
    local name="$1" want="$2" got="$3"
    if [ "$want" = "$got" ]; then
        pass=$((pass + 1)); printf 'ok   - %s\n' "$name"
    else
        fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s)\n' "$name" "$want" "$got"
    fi
}

# A town shaped like this one: a shared repo whose main worktree is where
# humans read, plus a linked worktree standing in for a seat. Echoes the two
# paths so a row can drive either. $1 names the directory to build in.
make_town() {
    local root="$1"
    git init -q -b master "$root/shared"
    git -C "$root/shared" config user.email t@t.invalid
    git -C "$root/shared" config user.name t
    git -C "$root/shared" commit -q --allow-empty -m base
    git -C "$root/shared" worktree add -q --detach "$root/seat" >/dev/null 2>&1
}

# Runs the guard from inside the seat worktree with GIT_DIR exported exactly as
# git exports it to a hook, which is the condition every observed damage shape
# needed. Echoes the guard's exit status.
run_guard() {
    local root="$1"; shift
    (
        cd "$root/seat" || exit 99
        export GIT_DIR="$root/shared/.git/worktrees/seat"
        bash "$GUARD" "$@" >"$root/out" 2>&1
        echo $? >"$root/rc"
    )
    cat "$root/rc"
}

shared_cfg() { git config -f "$1/shared/.git/config" --get "$2" 2>/dev/null || echo "<unset>"; }

# --- a clean run is let through ----------------------------------------------

T="$TMP/clean"; mkdir -p "$T"; make_town "$T"
rc="$(run_guard "$T" true)"
check "clean run: guard exits 0" 0 "$rc"

# --- the inner command's own failure is propagated ---------------------------
# A guard that swallowed a real test failure would be worse than no guard.

T="$TMP/innerfail"; mkdir -p "$T"; make_town "$T"
rc="$(run_guard "$T" sh -c 'exit 3')"
check "inner failure: guard propagates non-zero" nonzero "$([ "$rc" != 0 ] && echo nonzero || echo zero)"

# --- THE REAL VECTOR: a suite that forgot to scrub bricks the shared config ---
# Not a stub. This runs the actual `git init <dir>` that flipped core.bare on
# 2026-08-22, and asserts the guard turns it into a failing run.

T="$TMP/bare"; mkdir -p "$T"; make_town "$T"
before="$(shared_cfg "$T" core.bare)"
rc="$(run_guard "$T" git init -q -b master "$T/fixture")"
after="$(shared_cfg "$T" core.bare)"
check "vector fires: fixture got no .git of its own" absent \
    "$([ -d "$T/fixture/.git" ] && echo present || echo absent)"
check "vector fires: shared core.bare flipped" "false->true" "$before->$after"
check "core.bare damage: guard FAILS the run" nonzero \
    "$([ "$rc" != 0 ] && echo nonzero || echo zero)"
check "core.bare damage: guard names the key" yes \
    "$(grep -q 'core\.bare' "$T/out" && echo yes || echo no)"

# --- a fixture identity written into the shared config is caught -------------
# The shape that sat in the shared config for days until a commit hook refused
# the address (bd gqlc-ed2u).

T="$TMP/ident"; mkdir -p "$T"; make_town "$T"
rc="$(run_guard "$T" git config user.email km@test)"
check "identity damage: guard FAILS the run" nonzero \
    "$([ "$rc" != 0 ] && echo nonzero || echo zero)"
check "identity damage: guard names the key" yes \
    "$(grep -q 'user\.email' "$T/out" && echo yes || echo no)"

# --- a moved HEAD is caught --------------------------------------------------

T="$TMP/head"; mkdir -p "$T"; make_town "$T"
rc="$(run_guard "$T" git commit -q --allow-empty -m fixture)"
check "moved HEAD: guard FAILS the run" nonzero \
    "$([ "$rc" != 0 ] && echo nonzero || echo zero)"
check "moved HEAD: guard says HEAD" yes \
    "$(grep -qi 'HEAD' "$T/out" && echo yes || echo no)"

# --- a repointed checkout is caught even though the sha never moves ----------
# The other half of what happened on 2026-08-22: the seat worktree was left
# parked on a fixture branch. Switching branches at the same commit changes
# HEAD's symbolic-ref while `rev-parse HEAD` is byte-identical, so the sha
# check above cannot see it. Added because the symbolic-ref check SURVIVED the
# mutation screen without it — the incident justified the check, but nothing
# witnessed it.

T="$TMP/ref"; mkdir -p "$T"; make_town "$T"
sha_before="$(git -C "$T/seat" rev-parse HEAD)"
rc="$(run_guard "$T" git checkout -q -b fixturebranch)"
check "repointed checkout: the sha did NOT move" same \
    "$([ "$sha_before" = "$(git -C "$T/seat" rev-parse HEAD)" ] && echo same || echo moved)"
check "repointed checkout: guard FAILS the run" nonzero \
    "$([ "$rc" != 0 ] && echo nonzero || echo zero)"
check "repointed checkout: guard names the branch" yes \
    "$(grep -q 'fixturebranch' "$T/out" && echo yes || echo no)"

# --- damage is reported even when the inner command ALSO fails ---------------
# The trap case. A guard that only verified on the success path would let the
# worst run — one that broke the repo AND failed — report only the failure, and
# the damage would be found days later by hand.

T="$TMP/both"; mkdir -p "$T"; make_town "$T"
rc="$(run_guard "$T" sh -c "git config core.bare true; exit 3")"
check "inner failed AND damaged: guard FAILS" nonzero \
    "$([ "$rc" != 0 ] && echo nonzero || echo zero)"
check "inner failed AND damaged: damage still reported" yes \
    "$(grep -q 'core\.bare' "$T/out" && echo yes || echo no)"

# --- a key ADDED to the shared config is caught, not just a changed one ------
# Enumeration is the habit this guard is meant to break: it must notice a key
# it was never taught the name of (bd gqlc-n8n0).

T="$TMP/newkey"; mkdir -p "$T"; make_town "$T"
rc="$(run_guard "$T" git config some.inventedkey whatever)"
check "unknown key added: guard FAILS the run" nonzero \
    "$([ "$rc" != 0 ] && echo nonzero || echo zero)"
check "unknown key added: guard names it" yes \
    "$(grep -q 'some\.inventedkey' "$T/out" && echo yes || echo no)"

# --- the guard does not invent damage ----------------------------------------
# A gate that cries wolf gets switched off, so the negative control is a suite
# that DID scrub: same `git init` as the vector row above, wrapped in the unset
# that every correctly-written fixture builder uses. Measured 2026-08-22 in a
# repo+linked-worktree sandbox, the two forms diverge completely — unscrubbed,
# core.bare goes false->true and the target directory is never created;
# scrubbed, core.bare stays unset and the target is a real repo. Both exit 0.
# The row therefore also pins WHICH of the two shapes the guard is reacting to:
# if it fired here it would be reacting to `git init` at all, not to the damage.

T="$TMP/quiet"; mkdir -p "$T"; make_town "$T"
rc="$(run_guard "$T" sh -c "mkdir -p '$T/scratch' && ( unset \"\${!GIT_@}\"; git init -q '$T/scratch/r' ) 2>/dev/null; true")"
# On failure the guard's own report is the useful diagnostic, so it is carried
# into the comparison rather than a bare exit status.
if [ "$rc" = 0 ]; then quiet_got=0; else quiet_got="rc=$rc: $(cat "$T/out")"; fi
check "writes confined to a fixture path: guard does not fail on its own account" \
    0 "$quiet_got"

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
