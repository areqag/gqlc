#!/usr/bin/env bash
# Tests for .githooks/bd-gh-sync (gqlc-63y, gqlc-w318, gqlc-onji, gqlc-jwuw).
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
if [ "$1 ${2:-}" = "github sync" ] && [ "${FAKE_SYNC_RC:-0}" != 0 ]; then
    echo "bd: error: connection refused" >&2
    exit "$FAKE_SYNC_RC"
fi
if [ "$1" = "list" ]; then
    # Keyed by call ordinal, because the pull path calls `bd list` twice and
    # the two calls fail differently: the first is fail-closed and loud, the
    # second used to be fail-open and mute. A stub that can only answer the
    # same way every time cannot express that difference, which is how three
    # rounds of review found defects the suite was structurally unable to
    # represent. `bd_list_fails` / `bd_list_emits` drive these files.
    n=0
    [ -f "$STUBTMP/bd_list_count" ] && n=$(cat "$STUBTMP/bd_list_count")
    n=$((n + 1))
    printf '%s' "$n" >"$STUBTMP/bd_list_count"
    if [ -f "$STUBTMP/bd_list_rc_$n" ]; then
        echo "bd: error: database is locked by another process" >&2
        exit "$(cat "$STUBTMP/bd_list_rc_$n")"
    fi
    if [ -f "$STUBTMP/bd_list_out_$n" ]; then
        # Verbatim, so empty/truncated/non-JSON output is expressible. The
        # FAKE_BEADS fixtures below can only ever be valid JSON.
        cat "$STUBTMP/bd_list_out_$n"
        exit 0
    fi
    if [ "$n" -ge 2 ] && [ -n "${FAKE_BEADS_AFTER:-}" ]; then
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

# Per-invocation control over the `bd list` stub, keyed by call ordinal (the
# pull path calls it twice: the before snapshot, then the after snapshot the
# postcondition compares against). Both are consumed by the next run_sync and
# cleared by it, so an override cannot leak into a later test.
#
#   bd_list_fails 2      -> the 2nd `bd list` exits 1 with nothing on stdout
#   bd_list_fails 2 3    -> ...exits 3
#   bd_list_emits 2 ''   -> the 2nd `bd list` succeeds with empty output
#   bd_list_emits 2 '[{' -> ...succeeds with truncated JSON
bd_list_fails() { printf '%s' "${2:-1}" >"$TMP/bd_list_rc_$1"; }
bd_list_emits() { printf '%s' "$2" >"$TMP/bd_list_out_$1"; }

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
        FAKE_SYNC_RC="${SYNC_RC:-0}" \
        "$SYNC" "$1" >"$TMP/out" 2>"$TMP/err"
    RC=$?
    rm -f "$TMP"/bd_list_rc_* "$TMP"/bd_list_out_*
}
RC=0

# Exit status the `bd github sync` stub reports; 0 unless a test sets it.
SYNC_RC=0

scoped_ids() { grep -o -- '--issues [^ ]*' "$TMP/calls" | cut -d' ' -f2 | tr ',' '\n'; }
pull_ran()   { grep -q -- 'bd github sync' "$TMP/calls"; }
last_line()  { tail -n 1 "$TMP/err"; }
# How many `bd github sync` batches were actually issued. The summary reports a
# count of pulled beads, and the only evidence for that count is this.
sync_batches() { grep -c -- 'bd github sync' "$TMP/calls"; }

ISSUE=https://github.com/org/r/issues

# --- gqlc-63y: statuses GitHub cannot represent must never be in scope -------
# The measured blast radius of the unscoped pull was exactly the claimed beads.
# GitHub has no in_progress, so the mirror reads OPEN and --prefer-github
# writes that back over the claim.

for st in in_progress blocked deferred; do
    run_sync pull \
        "[{\"id\":\"b-$st\",\"status\":\"$st\",\"external_ref\":\"$ISSUE/1\",\"description\":\"local\"}]" \
        '[{"number":1,"state":"OPEN","body":"local\nadded on GH"}]'
    if scoped_ids | grep -qx "b-$st"; then
        bad "$st bead held out of pull scope" "it was passed to --issues"
    elif ! grep -q "holding b-$st" "$TMP/err"; then
        bad "$st bead held out of pull scope" "no notice on stderr"
    else
        ok "$st bead with GH content to lose is held and reported"
    fi
done

# ...but the notice is only worth printing when there is something to act on.
# `status == gh_state` is false by construction for a status GitHub cannot
# represent, so these can never fall into the in-sync drop and the notice is
# structurally unconditional: the live corpus emits seven of them on every
# single `git pull`, forever, against two actionable ones. That ratio is how
# `2>/dev/null` gets added back. When the body is byte-identical and GitHub
# holds the only state it can, GitHub is not behind bd in any respect GitHub
# can express — nothing is being withheld and there is nothing to say.
for st in in_progress blocked deferred; do
    run_sync pull \
        "[{\"id\":\"b-$st-quiet\",\"status\":\"$st\",\"external_ref\":\"$ISSUE/1\",\"description\":\"same\"}]" \
        '[{"number":1,"state":"OPEN","body":"same"}]'
    if scoped_ids | grep -qx "b-$st-quiet"; then
        bad "$st bead identical to its mirror stays out of scope" "it was pulled"
    elif grep -q "holding b-$st-quiet" "$TMP/err"; then
        bad "$st bead identical to its mirror is quiet" "notice printed anyway"
    else
        ok "$st bead byte-identical to an open mirror is held silently"
    fi
done

# The exception that keeps the two live signals: GitHub closing an issue whose
# bead is still blocked/in_progress is a real disagreement, identical body or
# not (gqlc-23e is exactly this shape).
run_sync pull \
    "[{\"id\":\"b-blocked-closed\",\"status\":\"blocked\",\"external_ref\":\"$ISSUE/1\",\"description\":\"same\"}]" \
    '[{"number":1,"state":"CLOSED","body":"same"}]'
if scoped_ids | grep -qx b-blocked-closed; then
    bad "blocked bead closed on GH is reported" "it was pulled"
elif ! grep -q 'holding b-blocked-closed' "$TMP/err"; then
    bad "blocked bead closed on GH is reported" "silenced with the routine ones"
else
    ok "blocked bead whose mirror was closed on GH is still reported"
fi

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

# "Never pull where GitHub is behind bd" has to hold for every shape of bd-side
# edit, not only added lines. A set-subset test over stripped lines admits
# three edits that add no content at all, and --prefer-github reverts each.
# Eligibility is therefore append-only: the GH body must extend the bead
# description verbatim, which is exactly the case --prefer-github serves.

# Re-indent: per-line .strip() discards the nesting, so the sets match.
run_sync pull \
    "[{\"id\":\"b-indent\",\"status\":\"open\",\"external_ref\":\"$ISSUE/10\",\"description\":\"- a\n  - b\"}]" \
    '[{"number":10,"state":"OPEN","body":"- a\n- b"}]'
if scoped_ids | grep -qx b-indent; then
    bad "bd-side re-indent held out" "pulled; the nesting would be flattened"
else
    ok "bd-side re-indent is held out of pull scope"
fi

# Reorder: sets are order-blind.
run_sync pull \
    "[{\"id\":\"b-reorder\",\"status\":\"open\",\"external_ref\":\"$ISSUE/11\",\"description\":\"step two\nstep one\"}]" \
    '[{"number":11,"state":"OPEN","body":"step one\nstep two"}]'
if scoped_ids | grep -qx b-reorder; then
    bad "bd-side reorder held out" "pulled; the ordering would be reverted"
else
    ok "bd-side reorder is held out of pull scope"
fi

# De-dup: sets collapse duplicates, so removing one of two identical lines is
# invisible to a subset test.
run_sync pull \
    "[{\"id\":\"b-dedup\",\"status\":\"open\",\"external_ref\":\"$ISSUE/12\",\"description\":\"x\nx\"}]" \
    '[{"number":12,"state":"OPEN","body":"x"}]'
if scoped_ids | grep -qx b-dedup; then
    bad "GH body shorter than the bead held out" "pulled; the de-dup reverted"
else
    ok "GH body that does not extend the bead description is held out"
fi

# A bd-side deletion of a trailing block is byte-identical to a GH-side append
# and is admitted. This is the residual the append-only rule does not close and
# cannot close from the two bodies alone: the fixture below differs from
# b-ahead above only in the wording of the line, and b-ahead must pull. Pinned
# so a future rule change has to confront the collision rather than rediscover
# it. Closing it needs a third input (edit times or a common ancestor).
run_sync pull \
    "[{\"id\":\"b-trailing-cut\",\"status\":\"open\",\"external_ref\":\"$ISSUE/13\",\"description\":\"line one\"}]" \
    '[{"number":13,"state":"OPEN","body":"line one\nline two deleted in bd"}]'
if scoped_ids | grep -qx b-trailing-cut; then
    ok "trailing-block deletion is indistinguishable from a GH append (known)"
else
    bad "trailing-block deletion is treated as a GH append" \
        "held out — b-ahead, the same bytes, must pull"
fi

# A byte-for-byte prefix test has to say which bytes. GitHub's web editor stores
# CRLF; bd stores LF. On the live corpus 188 of 289 bodies are multi-line and
# none carry CR today, so one edit through the GitHub UI is all it takes — and
# the effect is not a one-off: the bead is held forever, the notice prints on
# every merge forever, and the legitimate GH append is never pulled. Fails
# closed, but it is still a permanent hold created by an invisible byte.
run_sync pull \
    "[{\"id\":\"b-crlf\",\"status\":\"open\",\"external_ref\":\"$ISSUE/15\",\"description\":\"line one\nline two\"}]" \
    '[{"number":15,"state":"OPEN","body":"line one\r\nline two\r\nline three added on GH"}]'
if scoped_ids | grep -qx b-crlf; then
    ok "a CRLF GH body still counts as extending an LF bead description"
else
    bad "a CRLF GH body still counts as extending an LF bead description" \
        "held out — the append is unreachable until someone rewrites the body"
fi

# ...and normalising the line endings must not normalise away the rule. The bd
# side is still longer; the GH body still does not extend it.
run_sync pull \
    "[{\"id\":\"b-crlf-amend\",\"status\":\"open\",\"external_ref\":\"$ISSUE/16\",\"description\":\"line one\nline two\nbd only amendment\"}]" \
    '[{"number":16,"state":"OPEN","body":"line one\r\nline two"}]'
if scoped_ids | grep -qx b-crlf-amend; then
    bad "CRLF normalisation does not admit a bd-side amendment" "it was pulled"
else
    ok "a bd-only amendment is still held out when the GH body is CRLF"
fi

# The same class with an invisible character instead of an invisible byte: a
# no-break space arrives by copy-paste out of rendered markdown. Same permanent
# hold, same cost, same fix — compare text, not encodings.
run_sync pull \
    "[{\"id\":\"b-nbsp\",\"status\":\"open\",\"external_ref\":\"$ISSUE/17\",\"description\":\"line\\u00a0one\"}]" \
    '[{"number":17,"state":"OPEN","body":"line one\nline two added on GH"}]'
if scoped_ids | grep -qx b-nbsp; then
    ok "a no-break space in the bead description does not block the pull"
else
    bad "a no-break space in the bead description does not block the pull" "held out"
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

# ...on the line the caller keeps. The python traceback was echoed *below* the
# verdict, so `tail -1` in .claude/settings.json got
# `json.decoder.JSONDecodeError: Expecting value: line 1 column 1 (char 0)` and
# the session started with no idea the pull had been refused. Detail first,
# verdict last: this is the only bail-out that produces no summary line.
case "$(last_line)" in
    *"SKIPPING pull"*) ok "the refusal is the last line a tail -1 caller keeps" ;;
    *) bad "the refusal is the last line a tail -1 caller keeps" "got: $(last_line)" ;;
esac
if ! grep -q 'JSONDecodeError' "$TMP/err"; then
    bad "the underlying error is still on stderr" "the traceback was dropped"
else
    ok "the selection's own error is still reported, above the verdict"
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

# The property under all of it: whatever comes back from `bd` or `gh`, no path
# may reach `bd github sync` with an empty or absent --issues, because that
# widens the pull back to every bead — the failure this whole file exists to
# prevent. Held as a sweep rather than one fixture at a time, so a new door has
# to be argued about rather than discovered later. Exit status is checked with
# it: the script runs from git hooks and a non-zero exit aborts the merge.
hostile_case() { # $1=name $2=beads $3=gh
    run_sync pull "$2" "$3"
    if grep -qE -- '--issues( +--|[[:space:]]*$)' "$TMP/calls"; then
        bad "no unscoped pull from a hostile payload ($1)" \
            "empty --issues: $(grep 'github sync' "$TMP/calls")"
    elif [ "$RC" -ne 0 ]; then
        bad "no unscoped pull from a hostile payload ($1)" "exited $RC"
    else
        ok "hostile payload reaches no unscoped pull ($1)"
    fi
}

hostile_case "both empty"       '[]' '[]'
hostile_case "beads malformed"  '{'  '[]'
hostile_case "gh malformed"     '[]' '{'
hostile_case "both null"        'null' 'null'
hostile_case "empty strings"    '' ''
hostile_case "bead id empty" \
    "[{\"id\":\"\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\",\"description\":\"a\"}]" \
    '[{"number":1,"state":"OPEN","body":"a\nmore"}]'
hostile_case "bead id flag-shaped" \
    "[{\"id\":\"--issues\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\",\"description\":\"a\"}]" \
    '[{"number":1,"state":"OPEN","body":"a\nmore"}]'
hostile_case "gh fields null" \
    "[{\"id\":\"b-n\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\",\"description\":\"a\"}]" \
    '[{"number":1,"state":null,"body":null}]'

# Batching exists because --issues travels on argv and execve caps a single
# argument at 128KB, so it has to survive an allowlist bigger than one batch:
# every id passed exactly once, no id invented, and the count reported equal to
# the count sent. Hand-rolled since xargs parses quotes, which means nothing
# else checks the arithmetic.
run_sync pull \
    "$(python3 -c '
import json
print(json.dumps([{"id": "b-%03d" % i, "status": "open",
                   "external_ref": "https://github.com/org/r/issues/%d" % i,
                   "description": "d"} for i in range(250)]))')" \
    "$(python3 -c '
import json
print(json.dumps([{"number": i, "state": "OPEN", "body": "d\nadded on GH"}
                  for i in range(250)]))')"
if [ "$(sync_batches)" -ne 3 ]; then
    bad "an allowlist past one batch is split, not truncated" \
        "$(sync_batches) batches for 250 ids at 100 per batch"
elif [ "$(scoped_ids | sort -u | wc -l)" -ne 250 ]; then
    bad "an allowlist past one batch is split, not truncated" \
        "$(scoped_ids | sort -u | wc -l) distinct ids reached --issues"
elif [ "$(scoped_ids | wc -l)" -ne 250 ]; then
    bad "an allowlist past one batch is split, not truncated" \
        "$(scoped_ids | wc -l) ids sent — some were sent twice"
elif [ "$(last_line)" != "bd-gh-sync: pulled 250 bead(s), held 0, left 0 unmirrored GH issue(s) alone." ]; then
    bad "an allowlist past one batch is split, not truncated" "got: $(last_line)"
else
    ok "250 eligible beads are split into 3 batches, each id sent once"
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

# The detector cannot be armed by another bead happening to be pullable. On the
# live corpus the allowlist is empty in the steady state — 277 beads, 289
# issues, nine held and nothing eligible — so a postcondition that runs only
# after a pull is a postcondition that never runs. This is the shipped shape:
# one claimed bead, nothing to pull, reverted anyway.
run_sync pull "[$CLAIMED]" '[{"number":7,"state":"OPEN","body":"same"}]' '[]' \
    "[{\"id\":\"b-claim\",\"status\":\"open\",\"external_ref\":\"$ISSUE/7\",\"description\":\"same\"}]"
if pull_ran; then
    bad "postcondition runs with an empty allowlist" "a pull was issued"
elif ! grep -q 'WARNING b-claim was held out of the pull but changed in_progress -> open' "$TMP/err"; then
    bad "postcondition runs with an empty allowlist" "no warning; last line: $(last_line)"
else
    ok "a reverted claim is reported even when no bead was eligible to pull"
fi

# ...and the tail -1 caller has to see it. A WARNING printed above the summary
# is dropped by `tail -1` in .claude/settings.json, which is the same
# discarded-output failure the postcondition exists to end.
case "$(last_line)" in
    *WARNING*b-claim*) ok "the summary line carries the postcondition warning" ;;
    *) bad "the summary line carries the postcondition warning" "got: $(last_line)" ;;
esac

# The claim is that a held bead comes back *unchanged*, and description
# clobbering is the entire threat model this scoping exists to address — a
# check that watches only `status` watches the wrong column. b-amend2 below is
# exactly the shape the append-only rule holds back: a bd-only amendment that a
# --prefer-github pull would revert.
AMENDED="{\"id\":\"b-amend3\",\"status\":\"in_progress\",\"external_ref\":\"$ISSUE/7\",\"description\":\"line one\nbd only amendment\"}"
run_sync pull "[$AMENDED]" '[{"number":7,"state":"OPEN","body":"line one"}]' '[]' \
    "[{\"id\":\"b-amend3\",\"status\":\"in_progress\",\"external_ref\":\"$ISSUE/7\",\"description\":\"line one\"}]"
if ! grep -q 'WARNING b-amend3' "$TMP/err"; then
    bad "a held bead whose description was clobbered is reported" \
        "silent; last line: $(last_line)"
elif ! grep -q 'description' "$TMP/err"; then
    bad "a held bead whose description was clobbered is reported" \
        "warned without naming the field: $(grep WARNING "$TMP/err" | head -n 1)"
else
    ok "a held bead whose description was clobbered is reported"
fi
case "$(last_line)" in
    *WARNING*b-amend3*) ok "a clobbered description reaches the tail -1 caller" ;;
    *) bad "a clobbered description reaches the tail -1 caller" "got: $(last_line)" ;;
esac

# Both fields at once must read as one warning about one bead, not two beads.
run_sync pull "[$AMENDED]" '[{"number":7,"state":"OPEN","body":"line one"}]' '[]' \
    "[{\"id\":\"b-amend3\",\"status\":\"open\",\"external_ref\":\"$ISSUE/7\",\"description\":\"clobbered\"}]"
if [ "$(grep -c 'WARNING b-amend3' "$TMP/err")" -ne 1 ]; then
    bad "status and description moving together is one warning" \
        "$(grep -c 'WARNING b-amend3' "$TMP/err") warnings"
elif ! grep -q 'WARNING b-amend3 was held out of the pull but changed in_progress -> open, description' "$TMP/err"; then
    bad "status and description moving together is one warning" \
        "got: $(grep 'WARNING b-amend3' "$TMP/err")"
else
    ok "a bead whose status and description both moved warns once, naming both"
fi

# Trailing-whitespace churn is not a clobber, and a check that cries wolf gets
# `2>/dev/null` bolted onto it.
run_sync pull "[$AMENDED]" '[{"number":7,"state":"OPEN","body":"line one"}]' '[]' \
    "[{\"id\":\"b-amend3\",\"status\":\"in_progress\",\"external_ref\":\"$ISSUE/7\",\"description\":\"line one\nbd only amendment\n\"}]"
if grep -q 'WARNING' "$TMP/err"; then
    bad "trailing whitespace is not a description change" \
        "$(grep WARNING "$TMP/err" | head -n 1)"
else
    ok "a held bead whose description only gained trailing whitespace is quiet"
fi

# --- ...and a detector that could not run must say so ------------------------
# The first `bd list` failing is fail-closed and loud (SKIPPING pull). The
# second was fail-open and mute: `|| : >beads_post.json` fed the detector an
# empty file, `if not after: sys.exit(0)` swallowed it, and `|| true` swallowed
# the rest, so the run came out byte-identical to a healthy one.
#
# That is worse than it sounds, because the second call fails exactly when
# something else holds the Dolt lock — which is the scenario (`bd hooks run
# post-merge` replaying JSONL over newer DB state) the detector exists to
# catch. The detector was disarmed by the very event it detects. A blind run
# must never be mistakable for an armed one on the line a `tail -1` caller
# keeps.

GH7='[{"number":7,"state":"OPEN","body":"same"}]'

# The detector needs two snapshots, and a `bd list` deduplicated away for
# performance would disarm it without changing a line of the comparison. Pin
# the call count.
run_sync pull "[$CLAIMED]" "$GH7" '[]' "[$CLAIMED]"
if [ "$(grep -c 'bd list' "$TMP/calls")" -eq 2 ]; then
    ok "the pull takes a before and an after snapshot of the bead list"
else
    bad "the pull takes a before and an after snapshot of the bead list" \
        "$(grep -c 'bd list' "$TMP/calls") 'bd list' call(s)"
fi

# The control: same fixture, healthy second snapshot, nothing moved. The blind
# notice must not be a thing that just always prints.
run_sync pull "[$CLAIMED]" "$GH7" '[]' "[$CLAIMED]"
case "$(last_line)" in
    *"held-bead check did not run"*)
        bad "an armed postcondition is quiet" "cried blind on a healthy run: $(last_line)" ;;
    *) ok "a postcondition that ran does not claim it was skipped" ;;
esac

blind_case() { # $1=name; the stub knob is set by the caller
    run_sync pull "[$CLAIMED]" "$GH7" '[]' "[$CLAIMED]"
    case "$(last_line)" in
        *"held-bead check did not run"*)
            ok "an unusable post-pull snapshot is reported ($1)" ;;
        *)
            bad "an unusable post-pull snapshot is reported ($1)" \
                "indistinguishable from a healthy run: $(last_line)" ;;
    esac
}

bd_list_fails 2;                              blind_case "'bd list' exits non-zero"
bd_list_emits 2 '';                           blind_case "no output"
bd_list_emits 2 ' ';                          blind_case "whitespace only"
bd_list_emits 2 '[]';                         blind_case "empty bead list"
bd_list_emits 2 '[{"id":"b-claim","stat';     blind_case "truncated JSON"
bd_list_emits 2 'bd: fatal: not a workspace'; blind_case "not JSON at all"

# ...naming which of the two it was, because they call for different actions:
# a non-zero `bd list` is contention on the database (wait and re-run), output
# that parsed to nothing is corruption (go look).
bd_list_fails 2 3
run_sync pull "[$CLAIMED]" "$GH7" '[]' "[$CLAIMED]"
case "$(last_line)" in
    *"'bd list' exited 3"*) ok "the blind notice names the exit status it saw" ;;
    *) bad "the blind notice names the exit status it saw" "got: $(last_line)" ;;
esac

# --- the summary line must report the outcome, not the intent ----------------
# .claude/settings.json keeps only the last stderr line, so it has to carry the
# shape of the run rather than whatever notice happened to print last.

HELDBACK="{\"id\":\"b-amend2\",\"status\":\"open\",\"external_ref\":\"$ISSUE/14\",\"description\":\"keep\nbd only\"}"
GH_SUMMARY='[{"number":8,"state":"OPEN","body":"same\nadded on GH"},
             {"number":14,"state":"OPEN","body":"keep"},
             {"number":99,"state":"OPEN","body":"orphan"}]'

run_sync pull "[$ELIGIBLE,$HELDBACK]" "$GH_SUMMARY"
if [ "$(last_line)" = "bd-gh-sync: pulled 1 bead(s), held 1, left 1 unmirrored GH issue(s) alone." ]; then
    ok "final stderr line summarises the run for a tail -1 caller"
else
    bad "final stderr line summarises the run" "got: $(last_line)"
fi

# `bd github sync` failing is the shape the summary must not paper over: its
# own error goes to stderr and is dropped by `tail -1`, so a summary that
# reports the allowlist length instead of what happened leaves the operator
# reading a success line for a pull that never landed.
SYNC_RC=1
run_sync pull "[$ELIGIBLE,$HELDBACK]" "$GH_SUMMARY"
SYNC_RC=0
case "$(last_line)" in
    *"pulled 1 bead(s)"*)
        bad "a failed sync is visible to a tail -1 caller" \
            "reported success: $(last_line)" ;;
    *FAILED*)
        ok "a failed sync is reported on the line a tail -1 caller keeps" ;;
    *)
        bad "a failed sync is visible to a tail -1 caller" "got: $(last_line)" ;;
esac

# The count on that line is a claim about what `bd github sync` was handed, and
# the allowlist length is not evidence for it. A bead id that cannot survive the
# trip to argv — empty, or carrying a quote that splits it — used to be counted
# as pulled while zero batches ran: `pulled 1 bead(s)` with no sync at all, and
# the xargs error printed *above* the summary where `tail -1` drops it.

run_sync pull \
    "[{\"id\":\"\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\",\"description\":\"a\"}]" \
    '[{"number":1,"state":"OPEN","body":"a\nadded on GH"}]'
if [ "$(sync_batches)" -eq 0 ]; then
    ok "an empty bead id reaches no 'bd github sync' batch"
else
    bad "an empty bead id reaches no 'bd github sync' batch" \
        "$(grep 'github sync' "$TMP/calls")"
fi
case "$(last_line)" in
    *"pulled 1 bead(s)"*) bad "zero batches cannot report a pulled bead" "got: $(last_line)" ;;
    *FAILED*)             ok "zero batches run is reported as a failed pull" ;;
    *)                    bad "zero batches cannot report a pulled bead" "got: $(last_line)" ;;
esac

# A quote is the other door: `xargs` parses quotes, so one bad id takes the
# whole batch down with it (`xargs: unmatched single quote`) and the good bead
# beside it is never pulled either.
run_sync pull \
    "[{\"id\":\"b'q\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\",\"description\":\"a\"},
      {\"id\":\"b-good\",\"status\":\"open\",\"external_ref\":\"$ISSUE/2\",\"description\":\"a\"}]" \
    '[{"number":1,"state":"OPEN","body":"a\nadded on GH"},
      {"number":2,"state":"OPEN","body":"a\nadded on GH"}]'
if scoped_ids | grep -qx b-good; then
    ok "one unusable bead id does not take the batch down with it"
else
    bad "one unusable bead id does not take the batch down with it" \
        "b-good was never passed to --issues"
fi
if scoped_ids | grep -q "'"; then
    bad "an unusable bead id never reaches argv" "it was passed to --issues"
else
    ok "a bead id carrying a quote is refused rather than passed to argv"
fi
case "$(last_line)" in
    *"pulled 2 bead(s)"*)
        bad "the summary counts what was pulled, not what was eligible" \
            "counted the refused id: $(last_line)" ;;
    *"pulled 1 of 2"* | *FAILED*)
        ok "the summary counts what was pulled, not what was eligible" ;;
    *)
        bad "the summary counts what was pulled, not what was eligible" "got: $(last_line)" ;;
esac
if grep -q 'unusable' "$TMP/err"; then
    ok "the refused bead id is named on stderr"
else
    bad "the refused bead id is named on stderr" "silently dropped"
fi

# A space is the quiet one: the plan file is space-delimited, so an id carrying
# one is truncated to its first token and a *different*, possibly real, bead is
# named in --issues. Refusing beats guessing.
run_sync pull \
    "[{\"id\":\"b two\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\",\"description\":\"a\"}]" \
    '[{"number":1,"state":"OPEN","body":"a\nadded on GH"}]'
if [ "$(sync_batches)" -eq 0 ]; then
    ok "a bead id carrying a space is refused, not truncated to its first token"
else
    bad "a bead id carrying a space is refused, not truncated to its first token" \
        "synced $(scoped_ids | tr '\n' ' ')"
fi

# Whatever the shape of the failure, the invariant that has held through three
# rounds of review must hold: no path reaches an empty or absent --issues,
# because that widens the pull back to every bead.
if grep -qE -- '--issues( +--|[[:space:]]*$)' "$TMP/calls"; then
    bad "no batch is issued with an empty --issues" "$(grep 'github sync' "$TMP/calls")"
else
    ok "no batch is issued with an empty --issues"
fi

# Several conditions at once, on one line: a failed batch, a held bead, an
# orphan issue and a bead that moved behind the script's back. Nothing may win
# silently — the tail -1 caller gets one line and it has to carry all four.
SYNC_RC=1
run_sync pull "[$ELIGIBLE,$HELDBACK,$CLAIMED]" \
    '[{"number":7,"state":"OPEN","body":"same"},
      {"number":8,"state":"OPEN","body":"same\nadded on GH"},
      {"number":14,"state":"OPEN","body":"keep"},
      {"number":99,"state":"OPEN","body":"orphan"}]' '[]' \
    "[$ELIGIBLE,$HELDBACK,{\"id\":\"b-claim\",\"status\":\"open\",\"external_ref\":\"$ISSUE/7\",\"description\":\"same\"}]"
SYNC_RC=0
_l="$(last_line)"
if [ -n "${_l##*FAILED*}" ]; then
    bad "the summary reports every condition at once" "no failure: $_l"
elif [ -n "${_l##*held 1*}" ]; then
    bad "the summary reports every condition at once" "hold count lost: $_l"
elif [ -n "${_l##*left 1*}" ]; then
    bad "the summary reports every condition at once" "orphan count lost: $_l"
elif [ -n "${_l##*WARNING*b-claim*}" ]; then
    bad "the summary reports every condition at once" "moved bead lost: $_l"
else
    ok "a failed batch, a hold, an orphan and a moved bead share one summary line"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
