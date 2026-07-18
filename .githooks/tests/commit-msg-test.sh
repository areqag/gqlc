#!/usr/bin/env bash
# Unit tests for .githooks/commit-msg (AI-attribution guard).
#
# Feeds crafted commit messages via a temp file (git's commit-msg contract)
# and asserts accept/reject. Verifies both the trailer patterns we want to
# block (Claude in value, @anthropic.com in email, any case for the key) and
# the escape hatches (no trailer, human co-author, merge in progress).
#
# Run via: just test-hooks
set -u

# When run under a git hook (pre-push via `just test`), GIT_DIR etc. leak in
# and would redirect the throwaway repo's git commands to the parent repo.
# Isolate completely.
unset "${!GIT_@}"

HOOK="$(cd "$(dirname "$0")/.." && pwd)/commit-msg"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# The hook shells out to `git rev-parse --verify MERGE_HEAD`; that call needs
# a working tree with an OID database. Give every test the same throwaway
# repo — the hook only cares about MERGE_HEAD existence, not history shape.
REPO="$TMP/repo"
git init -q -b master "$REPO"
git -C "$REPO" -c user.email=t@t.invalid -c user.name=t commit -q --allow-empty -m init

pass=0
fail=0

# $1=name $2=expected(reject|accept) $3=msg-body [$4=merge]
run_case() {
    local name="$1" expected="$2" msg="$3" merge="${4:-}"
    local msg_file="$TMP/msg.$$"
    printf '%s' "$msg" >"$msg_file"

    if [ "$merge" = "merge" ]; then
        # Fabricate MERGE_HEAD so the hook's early-exit branch fires.
        printf '%s\n' "$(git -C "$REPO" rev-parse HEAD)" >"$REPO/.git/MERGE_HEAD"
    else
        rm -f "$REPO/.git/MERGE_HEAD"
    fi

    local decision
    if (cd "$REPO" && "$HOOK" "$msg_file") >/dev/null 2>&1; then
        decision=accept
    else
        decision=reject
    fi

    if [ "$decision" = "$expected" ]; then
        pass=$((pass + 1)); printf 'ok   - %s\n' "$name"
    else
        fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s)\n' "$name" "$expected" "$decision"
    fi

    rm -f "$msg_file" "$REPO/.git/MERGE_HEAD"
}

# --- must reject ------------------------------------------------------------
run_case "canonical claude trailer (mixed-case key)" reject "\
subject line

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
"

run_case "canonical claude trailer (git's own case)" reject "\
subject line

Co-authored-by: Claude Opus 4.7 <noreply@anthropic.com>
"

run_case "non-claude name but anthropic.com email" reject "\
subject line

Co-Authored-By: Some Human <claude-bot@anthropic.com>
"

run_case "fully lowercase trailer" reject "\
subject line

co-authored-by: claude opus 4.7 <noreply@anthropic.com>
"

# --- must accept ------------------------------------------------------------
run_case "no trailer at all" accept "\
subject line

body paragraph, nothing else.
"

run_case "human co-author trailer" accept "\
subject line

Co-Authored-By: Jane Doe <jane@example.com>
"

# --- merge escape hatch -----------------------------------------------------
# Even if the merged message would otherwise trip the guard, the hook must
# bail early when MERGE_HEAD exists — commits on the source branch are the
# right place to catch this, not on the merger's machine.
run_case "merge commit with claude trailer" accept "\
Merge branch 'foo'

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
" merge

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
