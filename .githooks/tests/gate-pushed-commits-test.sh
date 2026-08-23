#!/usr/bin/env bash
# Unit tests for .githooks/gate-pushed-commits — the push-time re-screen of
# every commit message a push would publish.
#
# WHY THIS SUITE EXISTS AT ALL. The gate is the FALLBACK for another gate.
# commit-msg is one-shot and per-commit, and core.hooksPath drift disarms it
# silently and repo-wide; this script is what covers the commits made inside a
# drift window that has since closed. An untested gate that is the fallback for
# another gate is the worst kind of untested: both halves can be dead at once and
# every dashboard reads green. It shipped in PR #1318 verified BY HAND ONLY
# (bd gqlc-z8of), because registering a suite was a justfile edit at the time and
# the justfile was another lane's. `just test-hooks` discovers suites by glob now
# (bd gqlc-234l), so this file needs no registration.
#
# HOW THE ROWS ARE HELD HONEST. Each section names the mutation that kills it.
# Those mutations were RUN against a copy of the hooks tree while writing this
# file, not reasoned about — a row whose killing mutation was never applied is a
# row certifying its own author's belief.
#
# Every fixture commit is made under `git -c core.hooksPath=/dev/null`, which is
# the drift shape the gate exists for: the commit really is unscreened, rather
# than screened and then re-screened. That inline `-c` is also the exact bypass
# bd gqlc-0tsh asks about, so the trailer row below is that bead's measurement:
# the bypass wins at commit time and loses at push time. It is the commit-msg
# half only — pre-commit's branch guard and format gate have no push-time
# re-screen, and neither does `--no-verify` on the push itself.
#
# Run via: just test-hooks
set -u

# git exports GIT_DIR and friends to every hook it runs, and this suite runs from
# .githooks/pre-push via `just test`. Without this, the fixtures' git commands
# reach the SHARED repo (see the file's own header).
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

REAL_HOOKS="$(cd "$(dirname "$0")/.." && pwd)"
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

# contains <name> <haystack> <needle>
contains() {
    case "$2" in
        *"$3"*) check "$1" yes yes ;;
        *) check "$1" yes no ;;
    esac
}

TRAILER='Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>'
ZERO=0000000000000000000000000000000000000000

# A copy of the hooks the gate needs, so a row can mutate one without touching
# the tree this suite is running out of. Copies rather than symlinks: the gate
# resolves commit-msg from its OWN dirname, so the copy has to be a real
# directory holding real files.
make_hooks() {
    local dir="$1"
    mkdir -p "$dir"
    local f
    for f in gate-pushed-commits commit-msg implausible-identity.sh guard-push-destination; do
        cp "$REAL_HOOKS/$f" "$dir/$f"
    done
    chmod +x "$dir/gate-pushed-commits" "$dir/commit-msg" "$dir/guard-push-destination"
}

HOOKS="$TMP/hooks"
make_hooks "$HOOKS"

# A throwaway repo whose commits were all made with the hooks DEAD.
make_repo() {
    local repo="$1"
    git init -q -b feature "$repo"
    git -C "$repo" config user.name "Fixture"
    git -C "$repo" config user.email "fixture@example.invalid"
    commit_in "$repo" "init"
}

# commit_in <repo> <subject> [<trailer body>]
commit_in() {
    local repo="$1" subject="$2" body="${3:-}"
    # --allow-empty and no file churn: a shared churn file would conflict when a
    # fixture merges two branches, which is a fixture failure dressed as a gate
    # verdict.
    local msg="$subject"
    [ -n "$body" ] && msg="$subject"$'\n\n'"$body"
    git -C "$repo" -c core.hooksPath=/dev/null commit -q --allow-empty -m "$msg"
}

# Runs the gate in repo $1 over the ref-line text $2, leaving the combined output
# in GATE_OUT and passed|refused in GATE_VERDICT.
#
# Both are variables rather than an echoed verdict because a message assertion
# has to read the SAME run the status came from. An `echo` consumed by `$(...)`
# runs the whole function in a subshell, so every GATE_OUT it set is discarded
# and the message rows read a stale value — measured on the first cut of this
# file, where four of them went green against the previous section's output.
GATE_OUT=""
GATE_VERDICT=""
# feed_refs restores the trailing newline `$(...)` strips. Without it the last
# ref line is an incomplete line, `while IFS= read -r` returns non-zero on it and
# the loop body never runs — which is a gate screening NOTHING while exiting 0.
# Measured on this file's second cut: five reject rows went green.
feed_refs() {
    if [ -n "$1" ]; then printf '%s\n' "$1"; fi
}

run_gate() {
    local repo="$1" refs="$2" hooks="${3:-$HOOKS}"
    local rc=0
    GATE_OUT="$( (cd "$repo" && feed_refs "$refs" | "$hooks/gate-pushed-commits" origin "$TMP/remote.git") 2>&1 )" || rc=$?
    if [ "$rc" -eq 0 ]; then GATE_VERDICT=passed; else GATE_VERDICT=refused; fi
}

# The pre-push ref line for a first push of $2 in repo $1 (remote sha all zeros).
new_ref_line() {
    printf 'refs/heads/%s %s refs/heads/%s %s\n' \
        "$2" "$(git -C "$1" rev-parse HEAD)" "$2" "$ZERO"
}

# --- 1. RED: an unscreened trailer in the pushed range is REFUSED -------------
# The headline row. Killed by: deleting the `offenders+=(...)` append; by
# `exit 1` -> `exit 0` at the foot; by dropping the `|| rc=$?` so a refusal is
# invisible under `set -e`. All three run, all three turn this row green->red.

REPO="$TMP/tainted"
make_repo "$REPO"
commit_in "$REPO" "carries the trailer" "$TRAILER"
TAINTED_SHA="$(git -C "$REPO" log -1 --format=%h)"

run_gate "$REPO" "$(new_ref_line "$REPO" feature)"
check "a commit carrying the AI-attribution trailer is REFUSED" refused "$GATE_VERDICT"
# ...and it says WHICH commit. A refusal that does not name the offender leaves
# the pusher to bisect their own branch by hand.
contains "the refusal names the offending sha" "$GATE_OUT" "$TAINTED_SHA"
contains "the refusal names the offending subject" "$GATE_OUT" "carries the trailer"

# ...and the remedy it prints ALLOCATES its scratch file rather than spelling
# one. bd gqlc-7ysc: this hook used to advise writing to a chosen name under
# /tmp, which is the exact shape CLAUDE.md's "## Scratch space" section forbids,
# and printing it is worse than doing it — it teaches the collision to every
# citizen the gate refuses. Two seats screening a commit at the same moment both
# write the chosen name and the loser reads the winner's bytes (bd gqlc-b8gd).
#
# Asserted on the RENDERED refusal rather than by grepping the hook's source:
# the source is already swept by scratch-literals-test.sh, and a second copy of
# that sweep would only re-measure the same bytes. What this row adds is that
# the advice a citizen actually SEES is runnable and self-cleaning.
contains "the remedy allocates its scratch file with mktemp" "$GATE_OUT" 'mktemp'
contains "the remedy removes the scratch file afterwards" "$GATE_OUT" 'rm -f'
# The negative half. Without it, an advice line that both calls mktemp AND keeps
# the old fixed path would pass the two rows above.
FIXED_TMP="/tmp"/
case "$GATE_OUT" in
    *"$FIXED_TMP"*) check "the remedy spells no fixed scratch name" no yes ;;
    *) check "the remedy spells no fixed scratch name" no no ;;
esac

# --- 2. GREEN: the same history without that commit passes --------------------
# The falsifier for a gate that refuses everything. Without this row, `exit 1`
# planted at the top of the script passes every reject row above.

REPO="$TMP/clean"
make_repo "$REPO"
commit_in "$REPO" "an ordinary commit"
run_gate "$REPO" "$(new_ref_line "$REPO" feature)"
check "a history with no trailer PASSES" passed "$GATE_VERDICT"

# --- 3. GREEN: empty stdin ----------------------------------------------------
# git's "nothing to push". The loop iterates zero times; the gate must not
# invent a refusal out of an empty ref list.

run_gate "$TMP/tainted" ""
check "empty stdin PASSES" passed "$GATE_VERDICT"

# --- 4. GREEN: a deletion line is skipped -------------------------------------
# `git push origin :branch` reports a LOCAL sha of all zeros — the shape below,
# with the remote's current sha in the fourth field — and publishes no commit.
#
# THE VERDICT ROW ALONE DOES NOT HOLD THE GUARD, and that is measured, not
# suspected. With the `zero_re` test on local_sha deleted, the whole suite stayed
# 26/0: `git rev-list <remote>..0000000` fatals, the gate reads rev-list's status
# through `mapfile` rather than through `set -e`, so the range comes back empty
# and a skipped line and an errored one are indistinguishable by verdict.
#
# What distinguishes them is the OUTPUT. A deletion is an ordinary push; the gate
# must be silent about it rather than printing git's fatal at every developer who
# deletes a merged branch. So the row asserts silence, and THAT is what the
# mutation kills. Run: deleting the guard reddens the silence row and nothing
# else.

REPO="$TMP/tainted"
run_gate "$REPO" "$(printf '(delete) %s refs/heads/feature %s\n' "$ZERO" "$(git -C "$REPO" rev-parse HEAD)")"
check "a deletion line (local sha all zeros) is skipped" passed "$GATE_VERDICT"
check "the deletion line produces no output at all" "" "$GATE_OUT"

# --- 5. RED: a MERGE commit carrying the trailer is REFUSED -------------------
# This inverts what the gate shipped with. It carried `--no-merges`, justified by
# commit-msg's blanket MERGE_HEAD `exit 0` — and that blanket exit was the hole
# in bd gqlc-7y7e, now split so the trailer scan runs on a merge message and only
# the identity arms stand down. A `--no-merges` here would have been the
# surviving half of the same hole.
#
# Killed by: restoring `--no-merges` on either rev-list. Run.
#
# The merge commit is the ONLY offender in the range — its two parents are clean
# — so this row cannot go green on a non-merge commit being caught instead. That
# is asserted below rather than assumed.

REPO="$TMP/merge"
make_repo "$REPO"
git -C "$REPO" checkout -q -b side
commit_in "$REPO" "side work"
git -C "$REPO" checkout -q feature
commit_in "$REPO" "trunk work"
printf 'Merge branch side\n\n%s\n' "$TRAILER" >"$TMP/merge-msg"
git -C "$REPO" -c core.hooksPath=/dev/null merge --no-ff -F "$TMP/merge-msg" side -q
check "the merge fixture's HEAD really is a merge commit" 2 \
    "$(git -C "$REPO" rev-list --no-walk --parents -1 HEAD | awk '{print NF-1}')"
check "the merge fixture has no OTHER offending commit" 0 \
    "$(git -C "$REPO" log --no-merges --format=%B | grep -ci claude || true)"
run_gate "$REPO" "$(new_ref_line "$REPO" feature)"
check "a merge commit carrying the trailer is REFUSED" refused "$GATE_VERDICT"
contains "the merge refusal names the merge subject" "$GATE_OUT" "Merge branch side"

# A clean merge still passes: the change above must not refuse merges as such.
REPO="$TMP/merge-clean"
make_repo "$REPO"
git -C "$REPO" checkout -q -b side
commit_in "$REPO" "side work"
git -C "$REPO" checkout -q feature
commit_in "$REPO" "trunk work"
git -C "$REPO" -c core.hooksPath=/dev/null merge --no-ff -m "Merge branch side" side -q
run_gate "$REPO" "$(new_ref_line "$REPO" feature)"
check "a clean merge commit PASSES" passed "$GATE_VERDICT"

# --- 6. SCOPE: which commits the range actually covers ------------------------
# Two arms, and each needs its own fixture because they take different branches
# of the same `if`.
#
# 6a. remote_sha non-zero -> `remote_sha..local_sha`. A commit already on the
#     remote is NOT re-screened, so a trailer that landed before this gate
#     existed does not block every future push from that clone.
#     Killed by: `remote_sha..local_sha` -> `local_sha`. Run.
# 6b. remote_sha all zeros -> `local_sha --not --remotes`. A first push of a
#     branch cut from an already-published base does not re-litigate the base.
#     Killed by: deleting `--not --remotes`. Run — 6b goes red, 6a stays green,
#     which is what makes the two arms distinguishable.

REPO="$TMP/scope"
make_repo "$REPO"
commit_in "$REPO" "published and tainted" "$TRAILER"
git init -q --bare "$TMP/remote.git"
git -C "$REPO" remote add origin "$TMP/remote.git"
git -C "$REPO" -c core.hooksPath=/dev/null push -q --no-verify origin feature
PUBLISHED="$(git -C "$REPO" rev-parse HEAD)"
commit_in "$REPO" "new and clean"

check "scope fixture: the tainted commit really is on the remote" yes \
    "$(git -C "$REPO" branch -r --contains "$PUBLISHED" | grep -q origin/feature && echo yes || echo no)"
run_gate "$REPO" "$(printf 'refs/heads/feature %s refs/heads/feature %s\n' \
    "$(git -C "$REPO" rev-parse HEAD)" "$PUBLISHED")"
check "6a: with remote_sha set, a commit already on the remote is not re-screened" passed "$GATE_VERDICT"
run_gate "$REPO" "$(new_ref_line "$REPO" feature)"
check "6b: with remote_sha zeros, --not --remotes excludes it too" passed "$GATE_VERDICT"
# The control for both: the same tainted commit IS caught when it is genuinely
# unpublished. Without this the two rows above are green on a gate that screens
# nothing at all.
REPO="$TMP/scope-control"
make_repo "$REPO"
commit_in "$REPO" "unpublished and tainted" "$TRAILER"
run_gate "$REPO" "$(new_ref_line "$REPO" feature)"
check "6-control: the same commit unpublished is REFUSED" refused "$GATE_VERDICT"

# --- 7. IDENTITY IS OUT OF SCOPE ----------------------------------------------
# At push time `git var` reports the PUSHER, not the author, so re-grading
# identity here would grade every historical commit against whoever is pushing.
# The gate switches those arms off through commit-msg's own documented escape —
# it blanks GIT_AUTHOR_NAME/EMAIL and GIT_COMMITTER_NAME/EMAIL, which makes
# `git var` fail, which is the condition commit-msg's identity arms return 0 on.
#
# Killed by: deleting the four blanking assignments in the gate. Run, and the
# asymmetry is the point: six PASS rows went red (every fixture commits at
# fixture@example.invalid) and row 1 stayed RED. A suite where the same mutation
# moved both rows would not be telling the two arms apart.
#
# Every fixture in this file commits as fixture@example.invalid, which is an
# address commit-msg refuses — asserted here rather than assumed, because if it
# did NOT refuse it, row 7 would be green on a premise that is not true.

REPO="$TMP/clean"
printf 'subject line\n' >"$TMP/ident-msg"
ident_rc=0
(cd "$REPO" && GIT_AUTHOR_NAME=A GIT_AUTHOR_EMAIL=fixture@example.invalid \
    GIT_COMMITTER_NAME=A GIT_COMMITTER_EMAIL=fixture@example.invalid \
    "$HOOKS/commit-msg" "$TMP/ident-msg") >/dev/null 2>&1 || ident_rc=$?
check "premise: commit-msg DOES refuse the fixture identity when asked" 1 "$ident_rc"
run_gate "$REPO" "$(new_ref_line "$REPO" feature)"
check "7: a commit authored at a refused identity still PASSES the gate" passed "$GATE_VERDICT"

# --- 8. FAIL-CLOSED when commit-msg is absent or not executable ---------------
# The gate exists because commit-msg can be missing at the moment it matters. It
# must not read its own absence as nothing to do.
#
# THE VERDICT ROWS DO NOT HOLD THIS GUARD EITHER, measured: with the
# `[ ! -x "$commit_msg" ]` block deleted, both verdict rows stayed green. With
# commit-msg gone the gate execs a missing file, gets rc=127, and files EVERY
# commit in the range as an offender — a refusal for the wrong reason, whose
# message tells the pusher to rewrite innocent commit messages. Only the message
# rows see it, so each of the two has one.

BROKEN="$TMP/hooks-nomsg"
make_hooks "$BROKEN"
rm -f "$BROKEN/commit-msg"
run_gate "$TMP/clean" "$(new_ref_line "$TMP/clean" feature)" "$BROKEN"
check "8a: commit-msg ABSENT and a non-empty ref list is REFUSED" refused "$GATE_VERDICT"
contains "8a: the refusal says the hook is missing or not executable" "$GATE_OUT" \
    "missing or not executable"

BROKEN="$TMP/hooks-644"
make_hooks "$BROKEN"
chmod 644 "$BROKEN/commit-msg"
run_gate "$TMP/clean" "$(new_ref_line "$TMP/clean" feature)" "$BROKEN"
check "8b: commit-msg NOT EXECUTABLE and a non-empty ref list is REFUSED" refused "$GATE_VERDICT"
contains "8b: the refusal says the hook is missing or not executable" "$GATE_OUT" \
    "missing or not executable"

# --- 9. WIRING: pre-push replays its stdin to BOTH consumers ------------------
# The shape that would make this gate inert with no other row noticing: git hands
# a pre-push hook its stdin exactly once, and .githooks/pre-push has two
# consumers of it. Feed the second an empty list and every row above still
# passes, because every row above invokes the gate directly.
#
# So this section runs the REAL pre-push, truncated at its first `just test`
# line — everything before that is the two guards and the replay, and everything
# after is the test and lint run, which is not what is under test here and takes
# minutes. The truncation point is asserted to exist, so a pre-push that stopped
# spelling `just test` reddens rather than silently testing the whole file or
# nothing.
#
# Killed by: replacing `_replay_push_refs |` on the gate's line with `printf '' |`
# — row 9a goes green (i.e. red here) while 9b stays refused, which is exactly
# the asymmetry that says the two consumers are fed separately. Run.

PREPUSH_SRC="$REAL_HOOKS/pre-push"
check "pre-push spells a 'just test' line to truncate at" 1 \
    "$(grep -c '^just test$' "$PREPUSH_SRC")"
awk '/^just test$/ { exit } { print }' "$PREPUSH_SRC" >"$HOOKS/pre-push"
chmod +x "$HOOKS/pre-push"

run_prepush() {
    local repo="$1" refs="$2" rc=0
    GATE_OUT="$( (cd "$repo" && feed_refs "$refs" | "$HOOKS/pre-push" origin "$TMP/remote.git") 2>&1 )" || rc=$?
    if [ "$rc" -eq 0 ]; then GATE_VERDICT=passed; else GATE_VERDICT=refused; fi
}

run_prepush "$TMP/tainted" "$(new_ref_line "$TMP/tainted" feature)"
check "9a: pre-push feeds the ref list to gate-pushed-commits" refused "$GATE_VERDICT"
contains "9a: and the refusal is the gate's, not some other failure" "$GATE_OUT" \
    "refused by .githooks/commit-msg"

# The other consumer, so a replay that fed the FIRST one nothing would also be
# caught. A push routing HEAD onto master is guard-push-destination's refusal.
run_prepush "$TMP/clean" "$(printf 'HEAD %s refs/heads/master %s\n' \
    "$(git -C "$TMP/clean" rev-parse HEAD)" "$ZERO")"
check "9b: pre-push feeds the same ref list to guard-push-destination" refused "$GATE_VERDICT"
contains "9b: and that refusal is the destination guard's" "$GATE_OUT" \
    "would land on the shared master branch"

# A clean push reaches the end of the truncated prefix with both guards silent.
# Without this, a prefix that failed for any reason at all would satisfy 9a and
# 9b together.
run_prepush "$TMP/clean" "$(new_ref_line "$TMP/clean" feature)"
check "9c: a clean push passes both consumers" passed "$GATE_VERDICT"

# --- summary ------------------------------------------------------------------

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
