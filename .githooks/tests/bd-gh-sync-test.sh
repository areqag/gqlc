#!/usr/bin/env bash
# Tests for .githooks/bd-gh-sync (gqlc-63y, gqlc-w318, gqlc-onji).
#
# These execute the real script with `bd` and `gh` stubbed on PATH, and assert
# on the commands it issues. The previous version of this file re-implemented
# the guard's Python inline and asserted against that copy, so it stayed green
# while the shipped guard could not execute at all — its payload travelled in
# the environment and execve rejected it. A test that does not run the artifact
# cannot see that class of fault, so nothing here transcribes the script's
# logic.
#
# Run via: just test-hooks
set -u

unset "${!GIT_@}"

SYNC="$(cd "$(dirname "$0")/.." && pwd)/bd-gh-sync"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

BIN="$TMP/bin"
mkdir -p "$BIN"

# The stubs read their canned payloads from files named by the environment, so
# the fixtures themselves never travel on argv or in the environment block.
cat >"$BIN/bd" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "bd $*" >>"$CALLS"
if [ "$1" = "list" ]; then
    n=0
    [ -f "$STUBTMP/bd_list_count" ] && n=$(cat "$STUBTMP/bd_list_count")
    printf '%s' "$((n + 1))" >"$STUBTMP/bd_list_count"
    if [ "$n" -ge 1 ] && [ -n "${FAKE_BEADS_AFTER:-}" ]; then
        cat "$FAKE_BEADS_AFTER"
    else
        cat "$FAKE_BEADS"
    fi
fi
exit 0
STUB

cat >"$BIN/gh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "gh $*" >>"$CALLS"
case "$1 ${2:-}" in
    "auth token") echo faketoken ;;
    "issue list")
        case "$*" in
            *"--state all"*) cat "$FAKE_GH" ;;
            *"--state open"*) cat "$FAKE_GH_OPEN" ;;
        esac
        ;;
esac
exit 0
STUB

chmod +x "$BIN/bd" "$BIN/gh"

pass=0
fail=0
ok()  { pass=$((pass + 1)); printf 'ok   - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s: %s\n' "$1" "$2"; }

# $1=action $2=beads $3=gh_all [$4=gh_open] [$5=beads_after]
# Leaves the invocation log in $TMP/calls and the script's stderr in $TMP/err.
run_sync() {
    : >"$TMP/calls"
    rm -f "$TMP/bd_list_count"
    printf '%s' "$2" >"$TMP/beads.json"
    printf '%s' "$3" >"$TMP/gh.json"
    printf '%s' "${4:-[]}" >"$TMP/gh_open.json"
    local after=""
    if [ -n "${5:-}" ]; then
        printf '%s' "$5" >"$TMP/beads_after.json"
        after="$TMP/beads_after.json"
    fi
    PATH="$BIN:$PATH" CALLS="$TMP/calls" STUBTMP="$TMP" \
        FAKE_BEADS="$TMP/beads.json" FAKE_GH="$TMP/gh.json" \
        FAKE_GH_OPEN="$TMP/gh_open.json" FAKE_BEADS_AFTER="$after" \
        "$SYNC" "$1" >"$TMP/out" 2>"$TMP/err"
}

scoped_ids() { grep -o -- '--issues [^ ]*' "$TMP/calls" | cut -d' ' -f2 | tr ',' '\n'; }
pull_ran()   { grep -q -- 'bd github sync' "$TMP/calls"; }

ISSUE=https://github.com/org/r/issues

# --- gqlc-63y: statuses GitHub cannot represent must never be in scope -------
# The measured blast radius of the unscoped pull was exactly the claimed beads.
# GitHub has no in_progress, so the mirror reads OPEN and --prefer-github
# writes that back over the claim.

for st in in_progress blocked deferred; do
    run_sync pull \
        "[{\"id\":\"b-$st\",\"status\":\"$st\",\"external_ref\":\"$ISSUE/1\",\"description\":\"same\"}]" \
        '[{"number":1,"state":"OPEN","body":"same"}]'
    if scoped_ids | grep -qx "b-$st"; then
        bad "$st bead held out of pull scope" "it was passed to --issues"
    elif ! grep -q "holding b-$st" "$TMP/err"; then
        bad "$st bead held out of pull scope" "no notice on stderr"
    else
        ok "$st bead is held out of pull scope and reported"
    fi
done

# --- gqlc-63y: the pull that is still wanted must still happen ---------------

run_sync pull \
    "[{\"id\":\"b-open\",\"status\":\"open\",\"external_ref\":\"$ISSUE/2\",\"description\":\"same\"}]" \
    '[{"number":2,"state":"CLOSED","body":"same"}]'
if scoped_ids | grep -qx b-open; then
    ok "open bead whose GH mirror closed is pulled"
else
    bad "open bead whose GH mirror closed is pulled" "absent from --issues"
fi

# GH ahead of bd is the case --prefer-github exists to serve; blocking it was
# the over-broad half of the original guard.
run_sync pull \
    "[{\"id\":\"b-ahead\",\"status\":\"open\",\"external_ref\":\"$ISSUE/3\",\"description\":\"line one\"}]" \
    '[{"number":3,"state":"OPEN","body":"line one\nline two added on GH"}]'
if scoped_ids | grep -qx b-ahead; then
    ok "GH body ahead of bd does not block the pull"
else
    bad "GH body ahead of bd does not block the pull" "held out"
fi

run_sync pull \
    "[{\"id\":\"b-amended\",\"status\":\"open\",\"external_ref\":\"$ISSUE/4\",\"description\":\"line one\nbd-only amendment\"}]" \
    '[{"number":4,"state":"OPEN","body":"line one"}]'
if scoped_ids | grep -qx b-amended; then
    bad "bd-only amendment held out" "it was pulled and would be reverted"
else
    ok "bd-only amendment is held out of pull scope"
fi

run_sync pull \
    "[{\"id\":\"b-closed\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/5\",\"description\":\"same\"}]" \
    '[{"number":5,"state":"OPEN","body":"same"}]'
if scoped_ids | grep -qx b-closed; then
    bad "locally-closed bead not reopened by a pull" "it was pulled"
else
    ok "locally-closed bead still open on GH is held out"
fi

# Naming a bead in --issues syncs it whether or not GitHub moved, so an
# in-sync bead has to be dropped from the batch or every merge rewrites the
# whole mirrored corpus. Measured against the live corpus: 129 beads would be
# updated where an unscoped pull proposed 4.
run_sync pull \
    "[{\"id\":\"b-same\",\"status\":\"open\",\"external_ref\":\"$ISSUE/6\",\"description\":\"identical\"}]" \
    '[{"number":6,"state":"OPEN","body":"identical"}]'
if pull_ran; then
    bad "bead already in sync is not re-pulled" "it was passed to --issues"
else
    ok "bead already in sync with GH is not re-pulled"
fi

# --- gqlc-63y: the guard must fail closed ------------------------------------
# The old guard turned an unrunnable selection into an empty result and fell
# through to an unscoped --prefer-github pull. The DONE sentinel is what makes
# "selection did not run" distinguishable from "selection chose nothing".

run_sync pull 'not json at all' '[{"number":1,"state":"OPEN","body":"x"}]'
if pull_ran; then
    bad "unreadable bead payload blocks the pull" "the pull ran anyway"
elif ! grep -q 'SKIPPING pull' "$TMP/err"; then
    bad "unreadable bead payload blocks the pull" "no diagnostic on stderr"
else
    ok "unreadable bead payload blocks the pull and says so"
fi

run_sync pull "[{\"id\":\"b-1\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\",\"description\":\"x\"}]" 'not json either'
if pull_ran; then
    bad "unreadable GH payload blocks the pull" "the pull ran anyway"
else
    ok "unreadable GH payload blocks the pull"
fi

# An empty --issues would widen the pull back to every bead, so "nothing is
# eligible" has to mean no invocation rather than an invocation with no list.
run_sync pull '[]' '[]'
if pull_ran; then
    bad "no eligible bead means no sync call" "bd github sync was invoked"
else
    ok "no eligible bead means bd github sync is not invoked"
fi

# --- gqlc-63y: the payload must survive execve -------------------------------
# The shipped guard died with "Argument list too long" on ~1MB of beads because
# it passed them in the environment. execve caps any single argument or
# environment string at 128KB regardless of ARG_MAX, so this fixture is sized
# past that cap and a decision still has to come out.

run_sync pull \
    "$(python3 -c '
import json
big = "\n".join("padding line %d" % i for i in range(20000))
print(json.dumps([{"id": "b-big", "status": "open",
                   "external_ref": "https://github.com/org/r/issues/9",
                   "description": big}]))')" \
    "$(python3 -c '
import json
big = "\n".join("padding line %d" % i for i in range(20000))
print(json.dumps([{"number": 9, "state": "OPEN", "body": big + "\nadded on GH"}]))')"
if grep -q 'Argument list too long' "$TMP/err"; then
    bad "guard runs on a payload past MAX_ARG_STRLEN" "execve rejected it"
elif scoped_ids | grep -qx b-big; then
    ok "guard runs on a payload past MAX_ARG_STRLEN"
else
    bad "guard runs on a payload past MAX_ARG_STRLEN" "no decision reached"
fi

# --- gqlc-onji: an unmirrored GH issue must not be minted into a bead --------

run_sync pull '[]' '[{"number":597,"state":"OPEN","body":"orphan"}]'
if pull_ran; then
    bad "orphan GH issue not adopted" "a pull ran that could mint a bead"
elif ! grep -q 'GH #597 has no bead' "$TMP/err"; then
    bad "orphan GH issue not adopted" "it was not reported"
else
    ok "orphan GH issue is reported, not minted into a bead"
fi

# A closed orphan is the resurrected-ghost shape (#597): touching it bumps it
# into the pull's window. It must not be adopted, and must not be reported
# either, or every cleanup regenerates its own noise.
run_sync pull '[]' '[{"number":597,"state":"CLOSED","body":"ghost"}]'
if grep -q 'GH #597' "$TMP/err"; then
    bad "closed orphan stays quiet" "it was reported"
else
    ok "closed orphan is neither adopted nor reported"
fi

# --- gqlc-w318: a bead closed before its first push must not strand a mirror -
# The bead carries no external_ref when the run starts; `bd github push` mints
# one. Reading the pre-push snapshot in the close pass skips exactly this bead,
# and the issue it just created stays open forever.

run_sync push \
    '[{"id":"b-new","status":"closed","close_reason":"done","external_ref":""}]' \
    '[]' \
    '[{"number":616}]' \
    '[{"id":"b-new","status":"closed","close_reason":"done","external_ref":"https://github.com/org/r/issues/616"}]'
if ! grep -q 'bd github push b-new' "$TMP/calls"; then
    bad "closed unmirrored bead pushed then closed" "it was never pushed"
elif ! grep -q 'gh issue close 616' "$TMP/calls"; then
    bad "closed unmirrored bead pushed then closed" "mirror #616 left open"
else
    ok "bead closed before its first push has its new mirror closed"
fi

# The case that passed even with the stale snapshot. Keeping it is what stops
# the suite from mistaking "a closed bead with a mirror gets closed" for the
# fix; on its own it never saw the defect.
run_sync push \
    '[{"id":"b-old","status":"closed","external_ref":"https://github.com/org/r/issues/500"}]' \
    '[]' \
    '[{"number":500}]'
if grep -q 'gh issue close 500' "$TMP/calls"; then
    ok "already-mirrored closed bead still has its mirror closed"
else
    bad "already-mirrored closed bead still has its mirror closed" "not closed"
fi

run_sync push \
    '[{"id":"b-live","status":"open","external_ref":"https://github.com/org/r/issues/501"}]' \
    '[]' \
    '[{"number":501}]'
if grep -q 'gh issue close 501' "$TMP/calls"; then
    bad "open bead's mirror left alone" "it was closed"
else
    ok "open bead's mirror is left alone"
fi

# --- gqlc-jwuw: a held bead that moves anyway must be reported ---------------
# Scoping keeps this script's own pull off a claimed bead, but it cannot stop
# `bd hooks run post-merge` replaying the exported JSONL over newer DB state,
# which reverts in_progress to open and keeps started_at. The postcondition
# names the bead whoever moved it.

CLAIMED="{\"id\":\"b-claim\",\"status\":\"in_progress\",\"external_ref\":\"$ISSUE/7\",\"description\":\"same\"}"
ELIGIBLE="{\"id\":\"b-ok\",\"status\":\"open\",\"external_ref\":\"$ISSUE/8\",\"description\":\"same\"}"
GH_BOTH='[{"number":7,"state":"OPEN","body":"same"},{"number":8,"state":"OPEN","body":"same\nadded on GH"}]'

run_sync pull "[$CLAIMED,$ELIGIBLE]" "$GH_BOTH" '[]' \
    "[{\"id\":\"b-claim\",\"status\":\"open\",\"external_ref\":\"$ISSUE/7\",\"description\":\"same\"},$ELIGIBLE]"
if grep -q 'WARNING b-claim was held out of the pull but changed in_progress -> open' "$TMP/err"; then
    ok "held bead reverted behind the script's back is reported"
else
    bad "held bead reverted behind the script's back is reported" "no warning"
fi

run_sync pull "[$CLAIMED,$ELIGIBLE]" "$GH_BOTH" '[]' "[$CLAIMED,$ELIGIBLE]"
if grep -q 'WARNING' "$TMP/err"; then
    bad "unchanged held bead is quiet" "spurious warning"
else
    ok "held bead that did not move produces no warning"
fi

# .claude/settings.json keeps only the last stderr line, so it has to carry the
# shape of the run rather than whatever notice happened to print last.
if [ "$(tail -n 1 "$TMP/err")" = "bd-gh-sync: pulled 1 bead(s), held 1, left 0 unmirrored GH issue(s) alone." ]; then
    ok "final stderr line summarises the run for a tail -1 caller"
else
    bad "final stderr line summarises the run" "got: $(tail -n 1 "$TMP/err")"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
