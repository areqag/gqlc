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
#
# How many assertions this file makes is a number worth quoting in a review, so
# here is the command that produces it rather than a figure that goes stale the
# next time someone adds a case:
#
#   .githooks/tests/bd-gh-sync-test.sh | grep -cE '^(ok|FAIL) '
#
# That number is not a gate and cannot be one — see the assertion census at the
# foot of this file, which is. Adding an assertion means adding its name there.
#
# And how many of them a given older bd-gh-sync fails — the measure of what a
# change to that script is actually worth — by running this file against it:
#
#   d=$(mktemp -d) && mkdir -p "$d/tests" &&
#     git show <rev>:.githooks/bd-gh-sync >"$d/bd-gh-sync" &&
#     cp .githooks/tests/bd-gh-sync-test.sh "$d/tests/" &&
#     chmod +x "$d/bd-gh-sync" "$d/tests/bd-gh-sync-test.sh" &&
#     "$d/tests/bd-gh-sync-test.sh" | grep -cE '^FAIL '
#
# The script under test is resolved relative to this file, which is what makes
# that second one work at all.
set -u

unset "${!GIT_@}"

SYNC="$(cd "$(dirname "$0")/.." && pwd)/bd-gh-sync"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

BIN="$TMP/bin"
mkdir -p "$BIN"

# The stubs read their canned payloads from files named by the environment, so
# the fixtures themselves never travel on argv or in the environment block.
#
# Each call logs a flattened `bd $*` line and then one `  ARG=[...]` line per
# argument. The flattened line cannot show where one argument ends and the next
# begins, so `bd github push b-1 b-2` and `bd github push "b-1 b-2"` — two ids
# and one nonexistent one — log byte-identically. This branch is a quoting fix;
# a harness that cannot see argument boundaries cannot fence one.
cat >"$BIN/bd" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "bd $*" >>"$CALLS"
if [ "$#" -gt 0 ]; then printf '  ARG=[%s]\n' "$@" >>"$CALLS"; fi
if [ "$1 ${2:-}" = "github sync" ] && [ "${FAKE_SYNC_RC:-0}" != 0 ]; then
    echo "bd: error: connection refused" >&2
    exit "$FAKE_SYNC_RC"
fi
if [ "$1 ${2:-}" = "github push" ] && [ "${FAKE_PUSH_RC:-0}" != 0 ]; then
    echo "bd: error: connection refused" >&2
    exit "$FAKE_PUSH_RC"
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
        # Defaulted, not inlined: `exit ""` is not fatal in bash — it reports
        # "numeric argument required" and carries on to the next line — so an
        # empty knob file would leave this stub succeeding while looking as
        # though it failed.
        _rc="$(cat "$STUBTMP/bd_list_rc_$n")"
        echo "bd: error: database is locked by another process" >&2
        exit "${_rc:-1}"
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
if [ "$#" -gt 0 ]; then printf '  ARG=[%s]\n' "$@" >>"$CALLS"; fi
case "$1 ${2:-}" in
    "auth token") echo faketoken ;;
    "issue list")
        # Every branch in the script that turns on this command's exit status
        # was unreachable from here until this knob existed: a stub that can
        # only succeed makes the whole error path dead code as far as the suite
        # is concerned, however carefully the script handles it.
        if [ "${FAKE_GH_LIST_RC:-0}" != 0 ]; then
            echo "gh: HTTP 502 Bad Gateway (https://api.github.com/graphql)" >&2
            exit "$FAKE_GH_LIST_RC"
        fi
        case "$*" in
            *"--state all"*) cat "$FAKE_GH" ;;
            *"--state open"*) cat "$FAKE_GH_OPEN" ;;
        esac
        ;;
    "issue close")
        if [ "${FAKE_CLOSE_RC:-0}" != 0 ]; then
            echo "gh: HTTP 403: Resource not accessible by integration" >&2
            exit "$FAKE_CLOSE_RC"
        fi
        ;;
esac
exit 0
STUB

# Every payload this script hands python travels through the directory `mktemp
# -d` makes, so the run cannot proceed without one — and it is the one early
# exit that is a fault rather than a configuration. Resolved before $BIN joins
# PATH, or the stub execs itself.
REAL_MKTEMP="$(command -v mktemp)"
cat >"$BIN/mktemp" <<'STUB'
#!/usr/bin/env bash
if [ "${FAKE_MKTEMP_RC:-0}" != 0 ]; then
    echo "mktemp: failed to create directory via template: Read-only file system" >&2
    exit "$FAKE_MKTEMP_RC"
fi
_d="$("$REAL_MKTEMP" "$@")" || exit
# FAKE_MKTEMP_BLOCK plants a directory where the script expects one named file,
# so that one payload is unwritable and unreadable while every other file in the
# working directory behaves. Which payload matters: blocking allow.txt instead
# of pulled.txt fails the pull outright, and the assertion that uses this knob
# has an arm that says so rather than passing on a run that proved nothing.
[ -n "${FAKE_MKTEMP_BLOCK:-}" ] && mkdir "$_d/$FAKE_MKTEMP_BLOCK"
printf '%s\n' "$_d"
STUB

# "the check ran and could not see" and "the check never ran" are different
# findings the script reports differently, and the second is reachable only if
# the interpreter itself can fail. Keyed by call ordinal like `bd list`, because
# each action runs two of these and they fail to different effect: a selection
# that dies refuses the whole run, a postcondition that dies leaves it blind.
# Resolved before $BIN joins PATH, or the stub execs itself.
REAL_PYTHON3="$(command -v python3)"
cat >"$BIN/python3" <<'STUB'
#!/usr/bin/env bash
n=0
[ -f "$STUBTMP/py_count" ] && n=$(cat "$STUBTMP/py_count")
n=$((n + 1))
printf '%s' "$n" >"$STUBTMP/py_count"
if [ -f "$STUBTMP/py_rc_$n" ]; then
    # Defaulted for the same reason as `bd list` above: `exit ""` falls through
    # to the exec below, which would hand the script a working interpreter out
    # of a stub that had announced its own death.
    _rc="$(cat "$STUBTMP/py_rc_$n")"
    echo "python3: can't open file '<stdin>': [Errno 13] Permission denied" >&2
    exit "${_rc:-1}"
fi
exec "$REAL_PYTHON3" "$@"
STUB

chmod +x "$BIN/bd" "$BIN/gh" "$BIN/mktemp" "$BIN/python3"

pass=0
fail=0
# Both arms log the name, because the census at the foot of this file is about
# whether an assertion ran at all and a failing assertion ran. `ok` and `bad`
# are the only two ways out of an assertion block, so this file cannot make an
# assertion that escapes the roll-call.
ok()  { pass=$((pass + 1)); printf '%s\n' "$1" >>"$TMP/ran.txt"
        printf 'ok   - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf '%s\n' "$1" >>"$TMP/ran.txt"
        printf 'FAIL - %s: %s\n' "$1" "$2"; }
: >"$TMP/ran.txt"

# Per-invocation control over the `bd list` stub, keyed by call ordinal (the
# pull path calls it twice: the before snapshot, then the after snapshot the
# postcondition compares against). Both are consumed by the next run_sync and
# cleared by it, so an override cannot leak into a later test.
#
#   bd_list_fails 2      -> the 2nd `bd list` exits 1 with nothing on stdout
#   bd_list_fails 2 3    -> ...exits 3
#   bd_list_emits 2 ''   -> the 2nd `bd list` succeeds with empty output
#   bd_list_emits 2 '[{' -> ...succeeds with truncated JSON
#   py_fails 2         -> the 2nd python3 invocation exits 1 having run nothing
#   py_fails 2 9       -> ...exits 9
bd_list_fails() { printf '%s' "${2:-1}" >"$TMP/bd_list_rc_$1"; }
bd_list_emits() { printf '%s' "$2" >"$TMP/bd_list_out_$1"; }
py_fails()      { printf '%s' "${2:-1}" >"$TMP/py_rc_$1"; }

# $1=action $2=beads $3=gh_all [$4=gh_open] [$5=beads_after]
# Leaves the invocation log in $TMP/calls and the script's stderr in $TMP/err.
run_sync() {
    : >"$TMP/calls"
    rm -f "$TMP/bd_list_count" "$TMP/py_count"
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
        FAKE_SYNC_RC="${SYNC_RC:-0}" FAKE_PUSH_RC="${PUSH_RC:-0}" \
        FAKE_CLOSE_RC="${CLOSE_RC:-0}" FAKE_MKTEMP_RC="${MKTEMP_RC:-0}" \
        FAKE_MKTEMP_BLOCK="${MKTEMP_BLOCK:-}" \
        FAKE_GH_LIST_RC="${GH_LIST_RC:-0}" \
        REAL_MKTEMP="$REAL_MKTEMP" REAL_PYTHON3="$REAL_PYTHON3" \
        "$SYNC" "$1" >"$TMP/out" 2>"$TMP/err"
    RC=$?
    rm -f "$TMP"/bd_list_rc_* "$TMP"/bd_list_out_* "$TMP"/py_rc_*
}
RC=0

# Exit statuses the mutating stubs report; 0 unless a test sets one.
SYNC_RC=0
PUSH_RC=0
CLOSE_RC=0
MKTEMP_RC=0
MKTEMP_BLOCK=
GH_LIST_RC=0

scoped_ids() { grep -o -- '--issues [^ ]*' "$TMP/calls" | cut -d' ' -f2 | tr ',' '\n'; }
pull_ran()   { grep -q -- 'bd github sync' "$TMP/calls"; }
last_line()  { tail -n 1 "$TMP/err"; }
# How many `bd github sync` batches were actually issued. The summary reports a
# count of pulled beads, and the only evidence for that count is this.
sync_batches() { grep -c -- 'bd github sync' "$TMP/calls"; }

# Reads the per-argument records the stubs log, so an assertion sees argument
# boundaries rather than a space-joined line. $1 is the flattened prefix that
# selects the call; $2 is how many leading argv words that prefix already names
# and therefore drops, per call, so several batches concatenate cleanly.
argv_after() {
    awk -v want="$1" -v skip="$2" '
        substr($0, 1, 7) == "  ARG=[" {
            if (on && ++n > skip) print substr($0, 8, length($0) - 8)
            next
        }
        { on = (substr($0, 1, length(want)) == want); n = 0 }
    ' "$TMP/calls"
}
argv_of() { argv_after "$1" 0; }

# The push-side equivalents. `bd github push` takes its ids as separate argv
# words, so one call is one batch and its arguments are the ids it was handed.
# Read from argv rather than by splitting the flattened line on spaces: that
# split reconstructs the boundaries it is meant to be checking, so an id list
# passed as one argument comes back looking exactly like ids passed as several.
pushed_ids()   { argv_after 'bd github push' 2; }
push_batches() { grep -c -- '^bd github push' "$TMP/calls"; }

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

# ...and the two flags that decide what "the pull" means, which nothing here
# read: both appeared in this file only in comments, and deleting
# `--prefer-github` from the script left it at 136 passed, 0 failed while
# changing what every eligible pull does. The whole eligibility argument above
# — hold unless the GH body extends the bead description — exists to find the
# beads where GitHub is ahead, and without that flag the bd copy wins every one
# of them, so each ALLOW is a no-op and the pull the run just decided on never
# happens. `--pull-only` is the other direction: without it `bd github sync`
# pushes as well, which is the ~30s whole-corpus scan with unreliable close
# propagation that the push path exists to avoid, over a scope chosen for a
# pull. Asserted separately, because they fail to opposite effect.
#
# Read as argv words rather than off the flattened call line, for the reason
# the id assertions read argv: `--pull-only --prefer-github` handed over as one
# argument is a single word bd would reject, and is byte-identical on that line.
run_sync pull \
    "[{\"id\":\"b-flags\",\"status\":\"open\",\"external_ref\":\"$ISSUE/3\",\"description\":\"line one\"}]" \
    '[{"number":3,"state":"OPEN","body":"line one\nline two added on GH"}]'
_argv="$(argv_after 'bd github sync' 2 | tr '\n' ' ')"
if argv_after 'bd github sync' 2 | grep -qx -- '--prefer-github'; then
    ok "an eligible pull is issued with --prefer-github, or GitHub's added lines lose"
else
    bad "an eligible pull is issued with --prefer-github, or GitHub's added lines lose" \
        "argv after 'bd github sync' was: ${_argv:-(no batch ran at all)}"
fi
if argv_after 'bd github sync' 2 | grep -qx -- '--pull-only'; then
    ok "an eligible pull is issued with --pull-only, or the pull pushes as well"
else
    bad "an eligible pull is issued with --pull-only, or the pull pushes as well" \
        "argv after 'bd github sync' was: ${_argv:-(no batch ran at all)}"
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

# The shape no fixture above has: GitHub *prepending*. Every case so far loses
# some of the bead text, so all of them are held by a substring test just as
# they are by a prefix one — which means relaxing `gh_body.startswith(desc)` to
# `desc in gh_body` passes this whole file, and the rule the header documents
# byte for byte silently becomes a weaker rule with nothing objecting. A
# prepend is the witness that separates them: the bead text survives verbatim,
# so it is not data loss and nothing here would call it one, but it is not an
# append either — the bead's first line stops being first, and a pull rewrites
# the description to GitHub's ordering.
run_sync pull \
    "[{\"id\":\"b-prepend\",\"status\":\"open\",\"external_ref\":\"$ISSUE/14\",\"description\":\"step one\"}]" \
    '[{"number":14,"state":"OPEN","body":"PREFACE\nstep one"}]'
if scoped_ids | grep -qx b-prepend; then
    bad "a GH body that prepends to the bead description is held out" \
        "pulled; a prefix rule that admits a prepend is a substring rule"
elif ! grep -q 'holding b-prepend (GH #14) out of the pull — bd-description-not-a-prefix-of-gh-body' "$TMP/err"; then
    bad "a GH body that prepends to the bead description is held out" \
        "held, but not by the prefix rule: $(grep 'b-prepend' "$TMP/err" | tr '\n' '|')"
else
    ok "a GH body that prepends to the bead description is held out"
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

# --- gqlc-w4q9: the push path has to report what it did to GitHub ------------
# push is the arm that *mutates* GitHub, and it used to say nothing at all:
# `bd github push $_ids || true` under xargs, plus a close pass whose entire
# output went to /dev/null. So every way of pushing nothing — an id xargs
# re-parsed, a batch that exited non-zero, a bead list that would not load —
# came out byte-identical to a healthy steady-state run and there was nothing an
# assertion could hold on to.
#
# The contract these tests pin: the last stderr line summarises the run, and the
# exit status is 0 iff every new bead reached GitHub and every stale mirror was
# closed. pull cannot use its exit status (post-merge and session start ride on
# it), but pre-push already invokes push behind `|| true`, so here a non-zero is
# free to carry the outcome.

run_sync push \
    "[{\"id\":\"b-mirrored\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\"}]" '[]' '[]'
if [ "$(last_line)" = "bd-gh-sync: pushed 0 new bead(s), closed 0 stale GH mirror(s)." ]; then
    ok "a push with nothing to do still says so on the line a caller keeps"
else
    bad "a push with nothing to do still says so on the line a caller keeps" \
        "got: $(last_line)"
fi
if [ "$RC" -eq 0 ]; then
    ok "a push that did everything it set out to do exits 0"
else
    bad "a push that did everything it set out to do exits 0" "exited $RC"
fi

# Both arms mutate GitHub, so both are in the count. A summary that reported only
# the beads it mirrored would leave the close pass exactly as unobservable as the
# whole path used to be.
run_sync push \
    "[{\"id\":\"b-fresh\",\"status\":\"open\",\"external_ref\":\"\"},
      {\"id\":\"b-done\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
if [ "$(last_line)" = "bd-gh-sync: pushed 1 new bead(s), closed 1 stale GH mirror(s)." ]; then
    ok "the summary counts both arms: beads mirrored and mirrors closed"
else
    bad "the summary counts both arms: beads mirrored and mirrors closed" \
        "got: $(last_line)"
fi

# The pull path stopped batching through xargs because xargs parses quotes. push
# kept it, on the arm that creates issues: one apostrophe aborts the whole
# construction, `|| true` eats the non-zero, and the run reports success having
# mirrored nothing. A space splits one id into two, and a leading dash arrives as
# a flag. Same gate as pull, and the bead beside the bad one still has to go.
push_hostile_id() { # $1=label $2=the bead id, as it appears inside a JSON string
    run_sync push \
        "[{\"id\":\"$2\",\"status\":\"open\",\"external_ref\":\"\"},
          {\"id\":\"b-ok\",\"status\":\"open\",\"external_ref\":\"\"}]" '[]' '[]'
    if ! pushed_ids | grep -qx 'b-ok'; then
        bad "an unusable bead id is refused, the bead beside it pushed ($1)" \
            "b-ok never reached 'bd github push' either"
    elif [ "$(pushed_ids | wc -l)" -ne 1 ]; then
        bad "an unusable bead id is refused, the bead beside it pushed ($1)" \
            "argv also carried: $(pushed_ids | grep -vx b-ok | tr '\n' ' ')"
    elif ! grep -q 'is unusable' "$TMP/err"; then
        bad "an unusable bead id is refused, the bead beside it pushed ($1)" \
            "it was dropped without a word"
    else
        ok "an unusable bead id is refused and named, the bead beside it pushed ($1)"
    fi
    # The count alone is not the finding: "1 of 2" with no reason leaves the
    # operator to guess whether GitHub refused the bead or the script did.
    case "$RC:$(last_line)" in
        0:*) bad "a refused bead id makes the push fail loudly ($1)" \
            "exited 0: $(last_line)" ;;
        *"PUSH FAILED"*"1 of 2 new bead(s) mirrored"*"1 bead id(s) unusable"*)
            ok "a refused bead id makes the push fail loudly ($1)" ;;
        *) bad "a refused bead id makes the push fail loudly ($1)" \
            "rc=$RC, last line: $(last_line)" ;;
    esac
}

push_hostile_id "apostrophe"   "b'q"
push_hostile_id "space"        "b two"
push_hostile_id "leading dash" "-b"
push_hostile_id "newline"      "b\ntrash"
push_hostile_id "empty"        ""

# "pushed nothing because every batch aborted" is the shape the summary has to
# separate from "nothing needed pushing", and it is exactly what one re-parsed
# id used to produce: xargs dies, the loop body never runs, and `|| true` makes
# it a clean exit. `_failed` cannot see it — nothing incremented it — so the
# line has to be built from what reached a batch, plus why the rest did not.
run_sync push "[{\"id\":\"b'q\",\"status\":\"open\",\"external_ref\":\"\"}]" '[]' '[]'
if [ "$(push_batches)" -ne 0 ]; then
    bad "a push where every id was refused says no batch ran" \
        "$(grep 'github push' "$TMP/calls")"
elif [ "$(last_line)" != "bd-gh-sync: PUSH FAILED — 0 of 1 new bead(s) mirrored, 0 of 0 stale GH mirror(s) closed; 1 bead id(s) unusable; no batch ran at all." ]; then
    bad "a push where every id was refused says no batch ran" "got: $(last_line)"
else
    ok "a push that mirrored nothing because every id was refused says exactly that"
fi

# ...and a batch that ran and was rejected by GitHub is a different line, because
# it calls for a different response: one is a corrupt id to go and look at, the
# other is a retry. `|| true` collapsed both into the steady-state line.
PUSH_RC=1
run_sync push '[{"id":"b-n1","status":"open","external_ref":""}]' '[]' '[]'
PUSH_RC=0
case "$(last_line)" in
    *"pushed 1 new bead(s)"*)
        bad "a batch that exited non-zero is not counted as mirrored" \
            "reported success: $(last_line)" ;;
    "bd-gh-sync: PUSH FAILED — 0 of 1 new bead(s) mirrored, 0 of 0 stale GH mirror(s) closed; 1 of 1 'bd github push' batch(es) exited non-zero.")
        ok "a batch that exited non-zero is named as a failed batch, not a bad id" ;;
    *) bad "a batch that exited non-zero is named as a failed batch, not a bad id" \
        "got: $(last_line)" ;;
esac
if [ "$RC" -ne 0 ]; then
    ok "a failed batch makes the push exit non-zero"
else
    bad "a failed batch makes the push exit non-zero" "exited 0"
fi

# Ids travel on argv here too, so the same 128KB execve cap applies and the
# batching has to be arithmetic anyone can check: every id sent exactly once,
# none invented, and the reported count equal to the count sent. The open
# listing carries the 250 issues such a push would have minted: `[]` after a
# push of 250 is the lost listing gqlc-mbe0 refuses to count over, and would
# make this a test of that refusal instead of of the batching.
run_sync push \
    "$(python3 -c '
import json
print(json.dumps([{"id": "b-%03d" % i, "status": "open", "external_ref": ""}
                  for i in range(250)]))')" '[]' \
    "$(python3 -c '
import json
print(json.dumps([{"number": n} for n in range(1, 251)]))')"
if [ "$(push_batches)" -ne 3 ]; then
    bad "an unmirrored set past one batch is split, not truncated" \
        "$(push_batches) batches for 250 ids at 100 per batch"
elif [ "$(pushed_ids | sort -u | wc -l)" -ne 250 ]; then
    bad "an unmirrored set past one batch is split, not truncated" \
        "$(pushed_ids | sort -u | wc -l) distinct ids reached argv"
elif [ "$(pushed_ids | wc -l)" -ne 250 ]; then
    bad "an unmirrored set past one batch is split, not truncated" \
        "$(pushed_ids | wc -l) ids sent — some were sent twice"
elif [ "$(last_line)" != "bd-gh-sync: pushed 250 new bead(s), closed 0 stale GH mirror(s)." ]; then
    bad "an unmirrored set past one batch is split, not truncated" "got: $(last_line)"
else
    ok "250 unmirrored beads are split into 3 batches, each id sent once"
fi

# ...and "sent once" is a claim about argv, so it has to be read off argv. Every
# assertion above this one would hold just as well if the script had passed the
# whole batch as a single argument — `bd github push "b-a1 b-a2"`, one bead id
# nobody minted — because the flattened log line is identical either way and
# splitting it on spaces puts the boundaries back. The gate this branch adds is
# a quoting gate; nothing fences it unless the harness can see where one
# argument stops.
run_sync push \
    '[{"id":"b-a1","status":"open","external_ref":""},
      {"id":"b-a2","status":"open","external_ref":""}]' '[]' '[]'
if [ "$(argv_of 'bd github push' | tr '\n' '|')" != "github|push|b-a1|b-a2|" ]; then
    bad "each bead id is its own argv word to 'bd github push'" \
        "argv was: $(argv_of 'bd github push' | tr '\n' '|')"
else
    ok "each bead id reaches 'bd github push' as an argv word of its own"
fi

# Same blindness on the other mutating command. `gh issue close` takes the
# comment body as one argument; a body that split into several would still log a
# line beginning `gh issue close 500`, which is all any assertion here checked.
run_sync push \
    "[{\"id\":\"b-c\",\"status\":\"closed\",\"close_reason\":\"superseded by gqlc-x\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
if [ "$(argv_of 'gh issue close' | wc -l)" -ne 5 ]; then
    bad "the close comment reaches 'gh' as a single argument" \
        "$(argv_of 'gh issue close' | wc -l) argv words: $(argv_of 'gh issue close' | tr '\n' '|')"
elif [ "$(argv_of 'gh issue close' | sed -n 4p)" != "--comment" ]; then
    bad "the close comment reaches 'gh' as a single argument" \
        "argv[4] was $(argv_of 'gh issue close' | sed -n 4p), not --comment"
elif [ "$(argv_of 'gh issue close' | sed -n 5p)" != "Auto-close: bead b-c closed locally. superseded by gqlc-x" ]; then
    bad "the close comment reaches 'gh' as a single argument" \
        "body was: $(argv_of 'gh issue close' | sed -n 5p)"
else
    ok "'gh issue close' gets the issue number and the whole comment as one argument each"
fi

# A bead list that will not load means push cannot know which beads have no
# mirror yet. Swallowing that produced the steady-state line exactly — the same
# fail-open-into-silence the pull path's DONE sentinel exists to end.
push_selection_case() { # $1=name; the stub knob is set by the caller
    run_sync push '[{"id":"b-n","status":"open","external_ref":""}]' '[]' '[]'
    case "$(last_line)" in
        *"SKIPPING push"*)
            if [ "$RC" -eq 0 ]; then
                bad "an unusable bead list refuses the push and says so ($1)" \
                    "refused, then exited 0"
            else
                ok "an unusable bead list refuses the push and says so ($1)"
            fi ;;
        *) bad "an unusable bead list refuses the push and says so ($1)" \
            "indistinguishable from a healthy run: $(last_line)" ;;
    esac
}

bd_list_fails 1;                              push_selection_case "'bd list' exits non-zero"
bd_list_emits 1 '';                           push_selection_case "no output"
bd_list_emits 1 ' ';                          push_selection_case "whitespace only"
bd_list_emits 1 '[{"id":"b-n","exter';        push_selection_case "truncated JSON"
bd_list_emits 1 'bd: fatal: not a workspace'; push_selection_case "not JSON at all"

# The close pass keys on the *second* snapshot, because `bd github push` mints
# the external_ref it matches on. That snapshot failing leaves every stale mirror
# open, and `|| : >beads_after.json` turned it into an empty list the pass walked
# without complaint.
bd_list_fails 2
run_sync push "[{\"id\":\"b-c\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
case "$RC:$(last_line)" in
    0:*) bad "an unreadable post-push snapshot is reported" "exited 0: $(last_line)" ;;
    *"stale-mirror pass could not run"*)
        ok "an unreadable post-push snapshot is reported, not walked as empty" ;;
    *) bad "an unreadable post-push snapshot is reported" "got: $(last_line)" ;;
esac

# Same hole on the other input: `gh issue list` failing produced an empty set of
# open issues, which reads as "nothing is stale" rather than "nobody looked".
run_sync push "[{\"id\":\"b-c\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' 'not json'
case "$RC:$(last_line)" in
    0:*) bad "an unreadable open-issue listing is reported" "exited 0: $(last_line)" ;;
    *"stale-mirror pass could not run"*)
        ok "an unreadable open-issue listing is reported, not read as nothing stale" ;;
    *) bad "an unreadable open-issue listing is reported" "got: $(last_line)" ;;
esac

# ...and the case above both: the close pass dying before it reaches a verdict
# at all. A list of bare integers loads as JSON and then breaks on `.get`, so no
# status is written and the only evidence left is the file's absence. This is
# what says the status file needs no pre-truncation to be readable as "no
# verdict" — an empty file and a missing one are the same string to the shell,
# so the `: >` that used to precede the pass could not have been the difference.
bd_list_emits 2 '[1,2,3]'
run_sync push "[{\"id\":\"b-c\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
case "$RC:$(last_line)" in
    0:*) bad "a close pass that never reached a verdict is reported" \
        "exited 0: $(last_line)" ;;
    *"stale-mirror pass could not run (the stale-mirror pass did not run at all)"*)
        ok "a close pass that wrote no verdict reads as blind, not as nothing stale" ;;
    *) bad "a close pass that never reached a verdict is reported" "got: $(last_line)" ;;
esac

# --- gqlc-w4q9 review: never print a count that was not measured -------------
# Every guard above catches a snapshot that would not *load*. A snapshot that
# loads and comes back `[]` takes the same fail-open one step further in: the
# close pass enumerates an empty list, finds nothing stale, and reports "0 stale
# mirrors" — a count of a set nobody looked at, byte-identical to a healthy
# steady state. It is also the reachable shape, because `bd list` answering `[]`
# with exit 0 is what the wrong workspace looks like; `bd list` failing outright
# already had a guard.
#
# The rule these three pin is one rule: a count reaches the summary only if the
# script enumerated the set behind it. Where it did not, the summary says so
# instead of printing a number.

# The post-push snapshot is the close pass's entire evidence, and this run
# pushed beads rather than deleting any — so an empty list here after a
# non-empty one there is a snapshot that was lost, not a tracker that emptied.
bd_list_emits 2 '[]'
run_sync push "[{\"id\":\"b-c\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
if [ "$RC" -eq 0 ]; then
    bad "an empty post-push snapshot is blind, not zero-and-fine" \
        "exited 0: $(last_line)"
else
    case "$(last_line)" in
        *"of 0 stale GH mirror(s) closed"*)
            bad "an empty post-push snapshot is blind, not zero-and-fine" \
                "printed a stale count off an empty list: $(last_line)" ;;
        *"stale-mirror pass could not run"*"post-push bead list came back empty"*)
            ok "an empty post-push snapshot reads as blind, not as nothing stale" ;;
        *) bad "an empty post-push snapshot is blind, not zero-and-fine" \
            "got: $(last_line)" ;;
    esac
fi

# Both empty is the same hole with the witness gone too. There is no way to tell
# a repository with no beads from a bd database nobody could read, so the honest
# answer is that neither arm was measured — not two zeroes and exit 0.
bd_list_emits 1 '[]'
bd_list_emits 2 '[]'
run_sync push '[{"id":"b-n","status":"open","external_ref":""}]' '[]' '[{"number":500}]'
if [ "$RC" -eq 0 ]; then
    bad "two empty snapshots are blind, not an empty repository" \
        "exited 0: $(last_line)"
else
    case "$(last_line)" in
        *"of 0 stale GH mirror(s) closed"* | *"0 of 0 new bead(s)"*)
            bad "two empty snapshots are blind, not an empty repository" \
                "printed counts off two empty lists: $(last_line)" ;;
        *"both bead lists came back empty"*)
            ok "two empty snapshots read as blind, not as an empty repository" ;;
        *) bad "two empty snapshots are blind, not an empty repository" \
            "got: $(last_line)" ;;
    esac
fi

# And the first snapshot alone. `_new` — the denominator of "N of M new bead(s)
# mirrored" — is counted off it, and the post-push snapshot is the witness that
# contradicts it: this run pushed nothing, so beads that exist after the push
# existed before it and the selection never saw them.
bd_list_emits 1 '[]'
run_sync push '[{"id":"b-n","status":"open","external_ref":""}]' '[]' '[]'
if [ "$RC" -eq 0 ]; then
    bad "an empty pre-push snapshot is blind, not nothing to push" \
        "exited 0: $(last_line)"
else
    case "$(last_line)" in
        *"of 0 new bead(s) mirrored"*)
            bad "an empty pre-push snapshot is blind, not nothing to push" \
                "printed a new-bead count off an empty list: $(last_line)" ;;
        *"pre-push bead list came back empty"*)
            ok "an empty pre-push snapshot reads as blind, not as nothing to push" ;;
        *) bad "an empty pre-push snapshot is blind, not nothing to push" \
            "got: $(last_line)" ;;
    esac
fi

# ...and the control the three above would otherwise be free to pass by refusing
# everything: a run where both snapshots hold beads still reports its two counts
# as numbers and exits 0.
run_sync push \
    "[{\"id\":\"b-mir\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\"}]" '[]' '[]'
if [ "$RC" -eq 0 ] &&
   [ "$(last_line)" = "bd-gh-sync: pushed 0 new bead(s), closed 0 stale GH mirror(s)." ]; then
    ok "a run with both snapshots readable still prints both counts and exits 0"
else
    bad "a run with both snapshots readable still prints both counts and exits 0" \
        "rc=$RC: $(last_line)"
fi

# `subprocess.run(..., check=False)` with the return code discarded: a mirror
# GitHub refused to close is the one outcome the operator has to act on, and it
# was the one the close pass could not express.
CLOSE_RC=1
run_sync push "[{\"id\":\"b-c\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
CLOSE_RC=0
if ! grep -q 'could not close GH #500' "$TMP/err"; then
    bad "a mirror that would not close is named and counted" "silently swallowed"
elif [ "$RC" -eq 0 ]; then
    bad "a mirror that would not close is named and counted" "exited 0"
else
    case "$(last_line)" in
        *"0 of 1 stale GH mirror(s) closed"*)
            ok "a mirror that would not close is named and counted" ;;
        *) bad "a mirror that would not close is named and counted" "got: $(last_line)" ;;
    esac
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

# --- gqlc-b7lt: a held bead deleted between the snapshots is not silent ------
# The comparison skipped every id absent from the second snapshot, so the one
# transition it could not see was the most destructive one: a held bead deleted
# behind the script's back vanished, and the run came out byte-identical to a
# healthy one.
#
# The skip cannot simply be dropped, because "absent from the second snapshot"
# covers two different things. A snapshot that came back empty makes *every*
# held bead absent, and that is already a blind run reported as one by the cases
# above — the interaction this bead was filed alongside. What is left after that
# is a snapshot that loaded, still holds other beads, and has lost this one:
# that is a deletion, and it is decidable from the set the run actually held
# rather than from the intersection of the two snapshots.

SURVIVOR="{\"id\":\"b-live\",\"status\":\"open\",\"external_ref\":\"$ISSUE/9\",\"description\":\"same\"}"
GH_CLAIM_LIVE='[{"number":7,"state":"OPEN","body":"same"},
                {"number":9,"state":"OPEN","body":"same"}]'

run_sync pull "[$CLAIMED,$SURVIVOR]" "$GH_CLAIM_LIVE" '[]' "[$SURVIVOR]"
if ! grep -q 'WARNING b-claim' "$TMP/err"; then
    bad "a held bead deleted between the snapshots is reported" \
        "it vanished without a word; last line: $(last_line)"
elif ! grep -q 'WARNING b-claim was held out of the pull but is gone from the bead list' \
        "$TMP/err"; then
    bad "a held bead deleted between the snapshots is reported" \
        "warned without naming the deletion: $(grep 'WARNING b-claim' "$TMP/err")"
else
    ok "a held bead deleted between the snapshots is reported as gone"
fi

# ...on the line a tail -1 caller keeps, and saying which of the two it was. A
# deletion and a reverted status call for different responses — `bd update`
# re-asserts a status, and nothing re-asserts a bead that is no longer there —
# so the summary that carries "1 bead(s) ... changed anyway" has to distinguish
# them rather than fold deletion into the generic count.
_ll="$(last_line)"
if [ -z "$_ll" ] || [ -n "${_ll##*WARNING*}" ]; then
    bad "a deleted held bead reaches the tail -1 caller" "no warning on it: $_ll"
elif [ -n "${_ll##*b-claim*}" ]; then
    bad "a deleted held bead reaches the tail -1 caller" "did not name it: $_ll"
elif [ -n "${_ll##*deleted outright*}" ]; then
    bad "a deleted held bead reaches the tail -1 caller" \
        "counted it as an ordinary change: $_ll"
else
    ok "the summary line names the deleted held bead and says it was deleted"
fi

# The control the case above would otherwise pass by warning about anything
# missing from the second snapshot: a bead that was *pulled* was never held, so
# it is outside this detector's claim entirely and its absence is not this
# check's finding. Absent-because-never-in-scope, not absent-because-deleted.
PULLABLE="{\"id\":\"b-pulled\",\"status\":\"open\",\"external_ref\":\"$ISSUE/8\",\"description\":\"same\"}"
run_sync pull "[$PULLABLE,$SURVIVOR]" \
    '[{"number":8,"state":"OPEN","body":"same\nadded on GH"},
      {"number":9,"state":"OPEN","body":"same"}]' '[]' "[$SURVIVOR]"
if ! scoped_ids | grep -qx b-pulled; then
    bad "a bead that was pulled is outside the held-bead detector" \
        "the fixture never pulled it, so it proves nothing: $(last_line)"
elif grep -q 'WARNING' "$TMP/err"; then
    bad "a bead that was pulled is outside the held-bead detector" \
        "warned about a bead it never held: $(grep WARNING "$TMP/err" | head -n 1)"
else
    ok "a pulled bead missing from the second snapshot is not a held-bead finding"
fi

# ...and the other half of that distinction, which the control above cannot see
# because its pull succeeded. The exemption is read from allow.txt, so it is the
# set this run *intended* to pull, and `_pulled` — the number on the summary
# line — counts only ids a batch that exited 0 was handed. Two different sets,
# and the detector took the wrong one: a bead the run announces on stderr as not
# pulled was exempted anyway, so the detector went blind on precisely the beads
# whose handling had already gone wrong. This file's own summary comment says
# intent is not outcome; the gap between them is the finding.
#
# First shape: an id the charset gate refuses. It reaches no batch at all, so
# there is no hypothesis under which a pull touched it.
UNUSABLE="{\"id\":\"b bad\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\",\"description\":\"x\"}"
NOMIRROR='{"id":"b-keep","status":"open","external_ref":"","description":"y"}'
run_sync pull "[$UNUSABLE,$NOMIRROR]" \
    '[{"number":1,"state":"OPEN","body":"x\nadded on GH"}]' '[]' "[$NOMIRROR]"
if ! grep -q "bead id 'b bad' is unusable" "$TMP/err"; then
    bad "an id the gate refused is watched, not exempt" \
        "the fixture never refused the id, so it proves nothing: $(last_line)"
elif [ "$(sync_batches)" -ne 0 ]; then
    bad "an id the gate refused is watched, not exempt" \
        "it reached a batch after all: $(grep 'github sync' "$TMP/calls")"
elif ! grep -q 'WARNING b bad was held out of the pull but is gone from the bead list' \
        "$TMP/err"; then
    bad "an id the gate refused is watched, not exempt" \
        "the run said it was not pulled and then let it be deleted in silence: $(last_line)"
else
    ok "an id the gate refused is watched by the held-bead detector"
fi

# Second shape: the batch itself exited non-zero. `_pulled` does not count these
# ids and the summary says so, so neither may the exemption — the summary and
# the detector have to be describing one run. A partially applied batch is the
# price: a bead that failed batch did move can draw a warning naming a change
# the pull made, and that only ever happens under a summary that already reads
# PULL FAILED and names the batch. An unwitnessed exemption is the worse half of
# that trade, because the transition it hides is the one no re-assert undoes.
SYNC_RC=1
run_sync pull "[$PULLABLE,$NOMIRROR]" \
    '[{"number":8,"state":"OPEN","body":"same\nadded on GH"}]' '[]' "[$NOMIRROR]"
SYNC_RC=0
if [ "$(sync_batches)" -ne 1 ]; then
    bad "a bead in a batch that failed is watched, not exempt" \
        "the fixture issued $(sync_batches) batch(es), so it proves nothing"
elif ! grep -q 'WARNING b-pulled was held out of the pull but is gone from the bead list' \
        "$TMP/err"; then
    bad "a bead in a batch that failed is watched, not exempt" \
        "counted as unpulled by the summary and as pulled by the detector: $(last_line)"
else
    ok "a bead in a batch that exited non-zero is watched by the held-bead detector"
fi

# ...and the case that decides what happens when the exemption set itself is
# unavailable, rather than merely wrong. The postcondition reads pulled.txt with
# no fallback, so this run has to end in the script's "the check did not run".
# The alternative — defaulting to an empty set — is the reading under which
# every bead this run pulled becomes a bead it held, and the pull that rewrote
# them becomes the clobber the detector reports. b-pulled is pulled here, so
# that reading produces a WARNING naming it, under a summary saying it was
# pulled: the same run stating both.
MKTEMP_BLOCK=pulled.txt
run_sync pull "[$PULLABLE,$NOMIRROR]" \
    '[{"number":8,"state":"OPEN","body":"same\nadded on GH"}]' '[]' \
    "[{\"id\":\"b-pulled\",\"status\":\"open\",\"external_ref\":\"$ISSUE/8\",\"description\":\"same\nadded on GH\"},$NOMIRROR]"
MKTEMP_BLOCK=
_ll="$(last_line)"
if [ "$(sync_batches)" -ne 1 ] || [ -n "${_ll##*pulled 1 bead(s)*}" ]; then
    bad "an exemption set that could not be written stops the held-bead check" \
        "the sabotage took the pull down with it, so it proves nothing: $_ll"
elif [ -n "${_ll##*the held-bead check did not run (the check itself did not run)*}" ]; then
    bad "an exemption set that could not be written stops the held-bead check" \
        "the check ran without it: $_ll"
else
    ok "an exemption set that could not be written stops the held-bead check"
fi

# ...and the interaction the bead was filed with. A second snapshot that came
# back empty makes every held bead absent at once, which under a naive fix reads
# as the whole tracker being deleted. That run is blind, not a mass deletion,
# and it has to keep saying so.
bd_list_emits 2 '[]'
run_sync pull "[$CLAIMED,$SURVIVOR]" "$GH_CLAIM_LIVE" '[]' "[$SURVIVOR]"
_ll="$(last_line)"
if grep -q 'is gone from the bead list' "$TMP/err"; then
    bad "an empty second snapshot is blind, not a mass deletion" \
        "reported deletions off a snapshot nobody could read: $(grep 'is gone' "$TMP/err" | tr '\n' '|')"
elif [ -z "$_ll" ] || [ -n "${_ll##*held-bead check did not run*}" ]; then
    bad "an empty second snapshot is blind, not a mass deletion" "got: $_ll"
else
    ok "an empty second snapshot stays blind rather than reading as deletions"
fi

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

# --- gqlc-icsd: a newline in a bead id must not split the plan record --------
# "Taking it whole lets the gate below see it and refuse it" is true for a space
# and false for a newline: the plan file is line-delimited, so an `ALLOW <id>`
# record carrying one becomes two records before `sed 's/^ALLOW //'` runs. The
# gate is then handed two halves it finds unobjectionable and never examines the
# id it was meant to refuse.
#
# The fixture is the round-4 reviewer's: `b-held` must be held out of the pull,
# and `b-held\ntrash` is eligible. Splitting the second record reconstitutes the
# first id inside the allowlist, so the run announces a hold, pulls the held
# bead, and reports success — a single run contradicting itself.

NL_BEADS="[{\"id\":\"b-held\",\"status\":\"in_progress\",\"external_ref\":\"$ISSUE/1\",\"description\":\"local\"},
           {\"id\":\"b-held\ntrash\",\"status\":\"open\",\"external_ref\":\"$ISSUE/2\",\"description\":\"a\"}]"
NL_GH='[{"number":1,"state":"OPEN","body":"local\nadded on GH"},
        {"number":2,"state":"OPEN","body":"a\nadded on GH"}]'

run_sync pull "$NL_BEADS" "$NL_GH"
if ! grep -q 'holding b-held' "$TMP/err"; then
    bad "a hold and a pull cannot name the same bead in one run" \
        "b-held was not held at all, so the fixture proves nothing"
elif scoped_ids | grep -qx 'b-held'; then
    bad "a hold and a pull cannot name the same bead in one run" \
        "announced the hold then pulled it: $(grep 'github sync' "$TMP/calls")"
else
    ok "a hold and a pull cannot name the same bead in one run"
fi

# Refused by name, like every other hostile shape — and named in a form that
# does not itself split the diagnostic across two stderr lines.
if grep -qF 'b-held\ntrash' "$TMP/err"; then
    ok "a bead id carrying a newline is refused by name, escaped"
else
    bad "a bead id carrying a newline is refused by name, escaped" \
        "no diagnostic names it: $(grep unusable "$TMP/err" | head -n 1)"
fi

case "$(last_line)" in
    *FAILED*) ok "a bead id that cannot be passed is a failed pull, not a success" ;;
    *) bad "a bead id that cannot be passed is a failed pull, not a success" \
        "got: $(last_line)" ;;
esac

# The split blinds the postcondition as well: the reconstituted `b-held` lands in
# allow.txt, and the moved-bead check skips everything in allow.txt. So the one
# detector that would have caught the contradiction is disarmed by it.
run_sync pull "$NL_BEADS" "$NL_GH" '[]' \
    "[{\"id\":\"b-held\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\",\"description\":\"local\"},
      {\"id\":\"b-held\ntrash\",\"status\":\"open\",\"external_ref\":\"$ISSUE/2\",\"description\":\"a\"}]"
if grep -q 'WARNING b-held was held out of the pull but changed in_progress -> open' "$TMP/err"; then
    ok "a split id does not blind the held-bead check for the id it collides with"
else
    bad "a split id does not blind the held-bead check for the id it collides with" \
        "no warning; last line: $(last_line)"
fi

# Without a collision the same split is quieter and no less wrong: `grep '^ALLOW '`
# keeps the head of the record and discards the tail, so --issues names `b-nl` —
# a bead id nobody minted — while the bead that was actually eligible is dropped
# without a word and still counted as pulled.
run_sync pull \
    "[{\"id\":\"b-nl\ntrash\",\"status\":\"open\",\"external_ref\":\"$ISSUE/1\",\"description\":\"a\"}]" \
    '[{"number":1,"state":"OPEN","body":"a\nadded on GH"}]'
if [ "$(sync_batches)" -ne 0 ]; then
    bad "the head of a split bead id is not pulled as a bead of its own" \
        "$(grep 'github sync' "$TMP/calls")"
elif [ "$(last_line)" != "bd-gh-sync: PULL FAILED — 0 of 1 eligible bead(s) pulled; 1 bead id(s) unusable; no batch ran at all, held 0, left 0 unmirrored GH issue(s) alone." ]; then
    bad "the head of a split bead id is not pulled as a bead of its own" \
        "got: $(last_line)"
else
    ok "a bead id that splits its record reaches no batch and is counted as unpulled"
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
# ${_l##*X*} is empty both when _l contains X and when _l is empty, so an absent
# summary line would satisfy all four checks below and report ok.
if [ -z "$_l" ]; then
    bad "the summary reports every condition at once" "no summary line at all"
elif [ -n "${_l##*FAILED*}" ]; then
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

# --- gqlc-oaxm: the diagnostic lines have to be reachable, and reached --------
# Six lines existed only to tell a human *why* a run went blind, and none of
# them was under an assertion: each was deletable or invertible with the suite
# still green. A blind run that names the wrong reason is worse than one that
# names none, because it sends the reader to the wrong place.
#
# The worst of the six was not merely untested but untestable: every path that
# turns on `gh` exiting non-zero was unreachable by construction, because the
# stub could only succeed. A stub that cannot fail cannot test a failure path.

# `gh issue list` failing is the more precise diagnosis than the python's view
# that the file was merely unreadable, and it is the one the operator acts on.
GH_LIST_RC=4
run_sync push "[{\"id\":\"b-c\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
GH_LIST_RC=0
case "$RC:$(last_line)" in
    0:*) bad "the push blind notice names the 'gh issue list' status it saw" \
        "exited 0: $(last_line)" ;;
    *"'gh issue list' exited 4"*)
        ok "the push blind notice names the 'gh issue list' exit status it saw" ;;
    *) bad "the push blind notice names the 'gh issue list' status it saw" \
        "got: $(last_line)" ;;
esac

# ...and the control the case above would otherwise pass against a stub that
# always failed: with the knob unset the listing has to come back and be used.
run_sync push "[{\"id\":\"b-c\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
if [ "$RC" -eq 0 ] &&
   [ "$(last_line)" = "bd-gh-sync: pushed 0 new bead(s), closed 1 stale GH mirror(s)." ]; then
    ok "the 'gh issue list' stub is transparent when its knob is unset"
else
    bad "the 'gh issue list' stub is transparent when its knob is unset" \
        "rc=$RC: $(last_line)"
fi

# The `bd list` twin on the push side. The pull side has had this assertion
# since gqlc-jwuw; push had the line and nothing watching it.
bd_list_fails 2 3
run_sync push "[{\"id\":\"b-c\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
case "$RC:$(last_line)" in
    0:*) bad "the push blind notice names the 'bd list' status it saw" \
        "exited 0: $(last_line)" ;;
    *"'bd list' exited 3"*)
        ok "the push blind notice names the 'bd list' exit status it saw" ;;
    *) bad "the push blind notice names the 'bd list' status it saw" \
        "got: $(last_line)" ;;
esac

# The refusal path's own diagnosis. A selection that would not run is already
# reported, but "the bead list would not load" and "bd would not answer" call
# for different responses — wait for the database, or go and look at what came
# back — and only the exit status separates them.
bd_list_fails 1 5
run_sync push '[{"id":"b-n","status":"open","external_ref":""}]' '[]' '[]'
_ll="$(last_line)"
if ! grep -q "'bd list' exited 5" "$TMP/err"; then
    bad "a refused push names the 'bd list' exit status behind it" \
        "no line names it: $(grep 'bd list' "$TMP/err" | head -n 1)"
elif [ -z "$_ll" ]; then
    bad "a refused push names the 'bd list' exit status behind it" "no verdict line at all"
elif [ -n "${_ll##*SKIPPING push*}" ]; then
    bad "a refused push names the 'bd list' exit status behind it" \
        "the detail displaced the verdict from the last line: $_ll"
else
    ok "a refused push names the 'bd list' exit status, verdict still last"
fi

# --- ...and the pull side's third blind reason -------------------------------
# `bd list` answering and the check *running* are different failures. The two
# reasons above both mean the check ran and could not see; this one means the
# interpreter never got as far as a verdict, which is why the status file's
# absence — not its contents — is what says so.

py_fails 2
run_sync pull "[$CLAIMED]" "$GH7" '[]' "[$CLAIMED]"
case "$(last_line)" in
    *"the check itself did not run"*)
        ok "a postcondition whose interpreter died says the check did not run" ;;
    *) bad "a postcondition whose interpreter died says the check did not run" \
        "got: $(last_line)" ;;
esac

# ...distinct from the reason beside it, or the notice sends the reader to the
# wrong place: this one means nobody looked, that one means the list would not
# parse.
bd_list_emits 2 '[]'
run_sync pull "[$CLAIMED]" "$GH7" '[]' "[$CLAIMED]"
case "$(last_line)" in
    *"the post-pull bead list was unreadable"*)
        ok "an unreadable post-pull list is named as unreadable, not as not-run" ;;
    *) bad "an unreadable post-pull list is named as unreadable, not as not-run" \
        "got: $(last_line)" ;;
esac

# ...and the control: the python3 stub must be transparent with its knob unset,
# or every case above it passes against an interpreter that never ran at all.
# Pinned to the whole healthy summary rather than to the absence of the blind
# notice: an interpreter dead from its first call refuses the run long before
# the postcondition, and "never said it was blind" is true of that refusal too.
run_sync pull "[$CLAIMED]" "$GH7" '[]' "[$CLAIMED]"
_ll="$(last_line)"
if [ "$_ll" = "bd-gh-sync: pulled 0 bead(s), held 0, left 0 unmirrored GH issue(s) alone." ]; then
    ok "the python3 stub is transparent when its knob is unset"
else
    bad "the python3 stub is transparent when its knob is unset" \
        "a healthy run did not reach its summary: $_ll"
fi

# --- ...and the close comment the operator reads on GitHub -------------------
# The comment is this script's only trace on an issue it closed. Its bead id
# goes through the same escaping the plan records use, for the same reason: the
# comment travels as one argv word, and a raw newline in an id splits it.

run_sync push \
    "[{\"id\":\"b-c\ntrash\",\"status\":\"closed\",\"close_reason\":\"done\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
if [ "$(argv_of 'gh issue close' | wc -l)" -ne 5 ]; then
    bad "a newline in a bead id does not split the close comment" \
        "$(argv_of 'gh issue close' | wc -l) argv words: $(argv_of 'gh issue close' | tr '\n' '|')"
elif [ "$(argv_of 'gh issue close' | sed -n 5p)" != 'Auto-close: bead b-c\ntrash closed locally. done' ]; then
    bad "a newline in a bead id does not split the close comment" \
        "body was: $(argv_of 'gh issue close' | sed -n 5p)"
else
    ok "a bead id carrying a newline reaches the close comment escaped, one argv word"
fi

# The other half of the same line. A bead with no id at all must not have the
# comment name a bead called `None`, which is the string python's own repr
# supplies when the fallback is dropped.
run_sync push \
    "[{\"id\":null,\"status\":\"closed\",\"close_reason\":\"done\",\"external_ref\":\"$ISSUE/501\"}]" \
    '[]' '[{"number":501}]'
if [ "$(argv_of 'gh issue close' | sed -n 5p)" != "Auto-close: bead  closed locally. done" ]; then
    bad "a bead with no id does not become a bead named None" \
        "body was: $(argv_of 'gh issue close' | sed -n 5p)"
else
    ok "a bead with no id leaves the close comment's bead name empty, not 'None'"
fi

# `close_reason` is free text a human wrote, and it lands in a GitHub comment.
# Only its first line goes, because a multi-paragraph reason turns one comment
# into a wall, and the comment is meant to be the pointer rather than the record.
run_sync push \
    "[{\"id\":\"b-c\",\"status\":\"closed\",\"close_reason\":\"superseded by gqlc-x\nplus three\nmore paragraphs\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
if [ "$(argv_of 'gh issue close' | wc -l)" -ne 5 ]; then
    bad "only the first line of close_reason reaches the comment" \
        "$(argv_of 'gh issue close' | wc -l) argv words: $(argv_of 'gh issue close' | tr '\n' '|')"
elif [ "$(argv_of 'gh issue close' | sed -n 5p)" != "Auto-close: bead b-c closed locally. superseded by gqlc-x" ]; then
    bad "only the first line of close_reason reaches the comment" \
        "body was: $(argv_of 'gh issue close' | sed -n 5p)"
else
    ok "a multi-line close_reason contributes only its first line to the comment"
fi

# ...and that first line is clamped, because nothing bounds what a human typed
# on it and GitHub's comment body is not a place to discover a limit.
run_sync push \
    "[{\"id\":\"b-c\",\"status\":\"closed\",\"close_reason\":\"$(printf 'y%.0s' $(seq 1 400))\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[{"number":500}]'
_body="$(argv_of 'gh issue close' | sed -n 5p)"
_tag="${_body#Auto-close: bead b-c closed locally. }"
if [ "${#_tag}" -ne 300 ]; then
    bad "an overlong close_reason is clamped" "the tag was ${#_tag} characters, not 300"
elif [ -n "${_tag%...}" ] && [ "${_tag%...}" = "$_tag" ]; then
    bad "an overlong close_reason is clamped" "clamped without an ellipsis: ${_tag}"
else
    ok "a close_reason past 300 characters is clamped to 300 with an ellipsis"
fi

# --- gqlc-mbe0: the GH listing is a set that has to be witnessed too ----------
# `gqlc-w4q9` closed this class over the two bead snapshots and left the GitHub
# listing open. `gh issue list` answering `[]` yields no stale mirrors, which
# walks to `0 of 0 stale GH mirror(s) closed` and exit 0 — a count over a set
# nobody enumerated. An empty listing is a legitimate answer in general, so
# emptiness alone is not the finding; a run that just minted N issues and then
# cannot see one of them is, and that is a comparison the script already has
# both halves of.
#
# `_ll` is taken outright rather than through `${_ll:=...}`, and the count arm
# asserts a phrase is present rather than one absent. Both halves of the older
# form were dead: `_ll` still held what an earlier case left in it, so `:=`
# never ran `last_line` and the arm read a verdict from a different run; and
# `closed 0 of 0` is a wording this script has never printed — its count arms
# are `closed N stale GH mirror(s)` and `N of M stale GH mirror(s) closed` — so
# the absence being tested was free. What has to hold is that the count was
# withdrawn, and that is a phrase the script does print, on this run.
run_sync push '[{"id":"b-n","status":"open","external_ref":""}]' '[]' '[]'
_ll="$(last_line)"
if [ "$RC" -eq 0 ]; then
    bad "a push of N>0 over an empty open listing is not counted as clean" \
        "exited 0: $_ll"
elif [ -z "$_ll" ] || [ -n "${_ll##*an unknown number of stale GH mirror(s) left open*}" ]; then
    bad "a push of N>0 over an empty open listing is not counted as clean" \
        "printed a count over the empty set: $_ll"
elif ! grep -q 'pushed 1 new bead(s) but the open-issue listing came back empty' \
        "$TMP/err"; then
    bad "a push of N>0 over an empty open listing is not counted as clean" \
        "did not say why the listing cannot be believed: $_ll"
else
    ok "a push that minted issues and then saw none of them refuses to count"
fi

# ...and the other side of the same comparison, or the guard above is just
# "an empty listing always fails" and every push into a repository with no open
# issues left is a false alarm. Nothing new to push here, so nothing was minted
# and `[]` is a listing that agrees with the run rather than one that lost it.
run_sync push "[{\"id\":\"b-o\",\"status\":\"open\",\"external_ref\":\"$ISSUE/500\"}]" \
    '[]' '[]'
if [ "$(last_line)" = "bd-gh-sync: pushed 0 new bead(s), closed 0 stale GH mirror(s)." ]; then
    ok "an empty open listing after a push of nothing is a legitimate empty"
else
    bad "an empty open listing after a push of nothing is a legitimate empty" \
        "got: $(last_line)"
fi

# --- ...and a listing cut off at its --limit is a page, not the set ----------
# `--limit 500` returns at most 500, and a listing of exactly 500 is what both
# "there are 500" and "there are 5000" look like from here. The count of stale
# mirrors over it is a count over one page, and it gets more wrong as the repo
# grows — which is the direction repos go.

# Spelled out rather than read off the script, because a fixture derived from
# the value under test agrees with it by construction: the third arm below is
# what notices the flag and this number parting company, and it can only notice
# if this number is written down independently.
OPEN_LIMIT=500
GH_OPEN_FULL="$(python3 -c '
import json, sys
print(json.dumps([{"number": n} for n in range(1, int(sys.argv[1]) + 1)]))' "$OPEN_LIMIT")"

run_sync push "[{\"id\":\"b-c\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/1\"}]" \
    '[]' "$GH_OPEN_FULL"
# Taken outright, for the reason the empty-listing case above takes it outright:
# `${_ll:="$(last_line)"}` assigns only when _ll is unset or empty, so the arm
# below read whatever an earlier case left in the variable unless an `unset _ll`
# happened to sit above it — and nothing asserted that line was there.
_ll="$(last_line)"
if [ "$(grep -c -- '--limit' <<<"$(argv_of 'gh issue list')")" -eq 0 ]; then
    bad "an open listing at its --limit is refused rather than counted" \
        "the listing carried no --limit at all: $(argv_of 'gh issue list' | tr '\n' '|')"
elif [ "$(argv_after 'gh issue list' 0 | grep -A1 -- '--limit' | tail -n 1)" != "$OPEN_LIMIT" ]; then
    bad "an open listing at its --limit is refused rather than counted" \
        "the script asks for --limit $(argv_after 'gh issue list' 0 | grep -A1 -- '--limit' | tail -n 1), so this fixture of $OPEN_LIMIT does not reach it"
elif [ "$RC" -eq 0 ]; then
    bad "an open listing at its --limit is refused rather than counted" \
        "exited 0: $(last_line)"
elif ! grep -q "the open-issue listing came back at its --limit of $OPEN_LIMIT" "$TMP/err"; then
    bad "an open listing at its --limit is refused rather than counted" \
        "did not name the cap it hit: $(last_line)"
elif ! grep -q 'is set in \.githooks/bd-gh-sync' "$TMP/err"; then
    # Naming the wrong fix under the right reason sends the reader somewhere
    # useless just as surely as naming the wrong reason does. The cap lives in
    # one file and the notice has to say which, so what is pinned is the
    # sentence that says so and not merely that the path appears somewhere on
    # stderr — the path is printed by the true remedy and by a false one sitting
    # next to it alike.
    bad "an open listing at its --limit is refused rather than counted" \
        "named the cap but not where it is set: $(grep 'stale' "$TMP/err" | tr '\n' '|')"
elif grep -qiE 're-run|the database is free' "$TMP/err"; then
    # The other half of the same claim, and the half this branch exists for. The
    # remedy the operator is handed is one paragraph covering every reason the
    # stale-mirror pass can go blind, so it has to be true of all of them, and
    # the notice this replaced was not: a listing cut off at its cap is not
    # waiting on a bd database, and an operator told to re-run once the database
    # is free re-runs forever. Checking only that the true remedy is present
    # cannot see the false one returning beside it — the two read as one
    # paragraph and the arm above passes on either — so the false one's absence
    # is pinned in its own right.
    #
    # Over the whole of this run's stderr rather than a window of N lines below
    # the reason: a paragraph that grew by a line would walk out of the window
    # and take the check with it. Nothing this run legitimately prints says
    # either of these — its five lines are the reason, three of remedy, and the
    # summary — and the one other place in the script that does say "re-run once
    # the database is free" is the empty-pre-push-list notice, which is true
    # there and cannot fire here: this fixture's bead list holds one bead.
    bad "an open listing at its --limit is refused rather than counted" \
        "the remedy sends the reader to wait on a database that is not the reason: $(grep -iE 're-run|the database is free' "$TMP/err" | tr '\n' '|')"
elif [ -z "$_ll" ] || [ -n "${_ll##*an unknown number of stale GH mirror(s) left open*}" ]; then
    bad "an open listing at its --limit is refused rather than counted" \
        "counted over the truncated page anyway: $_ll"
else
    ok "an open listing returning exactly its --limit is treated as truncated"
fi

# ...and one issue short of the cap is a whole set, or the guard above is
# "any large listing fails" and the stale-mirror pass stops working at 499.
GH_OPEN_SHORT="$(python3 -c '
import json, sys
print(json.dumps([{"number": n} for n in range(1, int(sys.argv[1]))]))' "$OPEN_LIMIT")"
run_sync push "[{\"id\":\"b-c\",\"status\":\"closed\",\"external_ref\":\"$ISSUE/1\"}]" \
    '[]' "$GH_OPEN_SHORT"
if [ "$(last_line)" = "bd-gh-sync: pushed 0 new bead(s), closed 1 stale GH mirror(s)." ]; then
    ok "an open listing one short of its --limit is counted as the whole set"
else
    bad "an open listing one short of its --limit is counted as the whole set" \
        "got: $(last_line)"
fi

# --- ...and the pull side reads a capped listing of its own ------------------
# `--limit 1000` on `--state all`. Pull's decisions are fail-closed under a
# short listing — an issue it cannot see holds its bead out rather than pulling
# it — so the damage is not a bad pull but a bad number: `left N unmirrored GH
# issue(s) alone` is counted off the page, and reads as the whole repository.

# Written down here rather than read off the script, for the reason the open
# listing's cap is: a fixture sized from the value under test cannot notice that
# value moving away from the flag that carries it.
ALL_LIMIT=1000
GH_ALL_FULL="$(python3 -c '
import json, sys
print(json.dumps([{"number": n, "state": "OPEN", "body": "x"}
                  for n in range(1, int(sys.argv[1]) + 1)]))' "$ALL_LIMIT")"

# One bead, carrying no external_ref: enough that the bead list is a set this
# run enumerated (gqlc-nvjz below withdraws the counts when it is not), and
# invisible to the orphan tally these two cases are about, because a bead with
# no mirror claims no issue.
UNMIRRORED='{"id":"b-nomirror","status":"open","external_ref":"","description":"x"}'

run_sync pull "[$UNMIRRORED]" "$GH_ALL_FULL" '[]' "[$UNMIRRORED]"
# Outright, for the reason the push side takes it outright.
_ll="$(last_line)"
if [ "$(argv_after 'gh issue list' 0 | grep -A1 -- '--limit' | tail -n 1)" != "$ALL_LIMIT" ]; then
    bad "an all-state listing at its --limit is not counted over" \
        "the script asks for --limit $(argv_after 'gh issue list' 0 | grep -A1 -- '--limit' | tail -n 1), so this fixture of $ALL_LIMIT does not reach it"
elif ! grep -q "the GitHub listing came back at its --limit of $ALL_LIMIT" "$TMP/err"; then
    bad "an all-state listing at its --limit is not counted over" \
        "said nothing about the cap: $(last_line)"
elif ! grep -q 'the cap in \.githooks/bd-gh-sync' "$TMP/err"; then
    bad "an all-state listing at its --limit is not counted over" \
        "named the cap but not where it is set: $(grep 'limit' "$TMP/err" | tr '\n' '|')"
elif [ -z "$_ll" ] || [ -n "${_ll##*left an unknown number of unmirrored GH issue(s) alone*}" ]; then
    bad "an all-state listing at its --limit is not counted over" \
        "counted the orphans off one page: $_ll"
else
    ok "a pull over a capped GitHub listing reports the count as unknown"
fi

# ...and its control, for the same reason as the push side's.
GH_ALL_SHORT="$(python3 -c '
import json, sys
print(json.dumps([{"number": n, "state": "OPEN", "body": "x"}
                  for n in range(1, int(sys.argv[1]))]))' "$ALL_LIMIT")"
run_sync pull "[$UNMIRRORED]" "$GH_ALL_SHORT" '[]' "[$UNMIRRORED]"
if [ "$(last_line)" = "bd-gh-sync: pulled 0 bead(s), held 0, left $((ALL_LIMIT - 1)) unmirrored GH issue(s) alone." ]; then
    ok "an all-state listing one short of its --limit is counted as the whole set"
else
    bad "an all-state listing one short of its --limit is counted as the whole set" \
        "got: $(last_line)"
fi

# --- gqlc-nvjz: the pull path has to witness its own bead listing ------------
# `gqlc-w4q9` and `gqlc-mbe0` established one rule over three sets on the push
# side: a count reaches the summary only if this run enumerated the set behind
# it, and a listing it could not read is not an empty listing. The pull path
# reported `held 0, left N unmirrored GH issue(s) alone` off a `bd list` that
# answered `[]` with exit 0 — a held count over a bead set nobody witnessed, and
# an orphan count whose whole meaning is "no bead names this issue", asserted
# against no beads at all.

# The status, first, and separately from the cardinality. Both inputs to the
# selection were `|| : >file`, which threw the exit status away and left the
# python's JSONDecodeError as the only account of what happened: the run
# refused, correctly, and named neither `bd list` nor `gh issue list` as the
# reason. Detail first, verdict last, exactly as the push twin of this case.
bd_list_fails 1 7
run_sync pull "[$CLAIMED]" "$GH7" '[]' "[$CLAIMED]"
_ll="$(last_line)"
if ! grep -q "'bd list' exited 7" "$TMP/err"; then
    bad "a refused pull names the 'bd list' exit status behind it" \
        "no line names it: $(grep -c . "$TMP/err") line(s) on stderr"
elif [ -z "$_ll" ] || [ -n "${_ll##*SKIPPING pull*}" ]; then
    bad "a refused pull names the 'bd list' exit status behind it" \
        "the detail displaced the verdict from the last line: $_ll"
else
    ok "a refused pull names the 'bd list' exit status, verdict still last"
fi

# The same for the other input. `gh issue list` failing is the one the operator
# acts on differently — GitHub is down or the token expired, and no amount of
# waiting for the bd database helps.
GH_LIST_RC=6
run_sync pull "[$CLAIMED]" "$GH7" '[]' "[$CLAIMED]"
GH_LIST_RC=0
_ll="$(last_line)"
if ! grep -q "'gh issue list' exited 6" "$TMP/err"; then
    bad "a refused pull names the 'gh issue list' exit status behind it" \
        "no line names it: $(grep -c . "$TMP/err") line(s) on stderr"
elif [ -z "$_ll" ] || [ -n "${_ll##*SKIPPING pull*}" ]; then
    bad "a refused pull names the 'gh issue list' exit status behind it" \
        "the detail displaced the verdict from the last line: $_ll"
else
    ok "a refused pull names the 'gh issue list' exit status, verdict still last"
fi

# ...and the cardinality, which the statuses above cannot speak for: `bd list`
# answering `[]` and exiting 0 is what the wrong workspace looks like, and it is
# the shape this bead was filed on. Both counts on the summary line are derived
# from that list — "held" directly, and "unmirrored" because an issue is
# unmirrored only relative to a bead set — so both have to be withdrawn.
run_sync pull '[]' '[{"number":11,"state":"OPEN","body":"x"}]' '[]' '[]'
_ll="$(last_line)"
if [ "$RC" -ne 0 ]; then
    bad "an empty pull-side bead list is blind, not held-zero" \
        "exited $RC, and pull rides on 'git pull': $_ll"
elif [ -z "${_ll##*held 0*}" ]; then
    bad "an empty pull-side bead list is blind, not held-zero" \
        "printed a held count over a list nobody enumerated: $_ll"
elif [ -z "$_ll" ] || [ -n "${_ll##*held an unknown number*}" ]; then
    bad "an empty pull-side bead list is blind, not held-zero" "got: $_ll"
elif [ -n "${_ll##*left an unknown number of unmirrored GH issue(s) alone*}" ]; then
    bad "an empty pull-side bead list is blind, not held-zero" \
        "called an issue unmirrored against no beads at all: $_ll"
elif ! grep -q 'the bead list came back empty' "$TMP/err"; then
    bad "an empty pull-side bead list is blind, not held-zero" \
        "withdrew the counts without saying why: $_ll"
else
    ok "an empty pull-side bead list withdraws both counts derived from it"
fi

# ...and on that same shape the held-bead check must not report itself skipped.
# An empty `before` snapshot is VACUOUS, which is the check running and finding
# no baseline. Folding VACUOUS in with the statuses that mean nobody looked —
# dropping it from the arm that accepts OK — appends `WARNING: the held-bead
# check did not run (the check itself did not run)` to a summary where it is
# simply false, and the suite stayed green through it. Nothing can hide behind
# that warning, since an empty `before` is exactly "no bead existed to protect",
# so this is noise rather than a hole. Pinned anyway for the reason the script's
# own comments give twice: a check that cries wolf is a check that gets
# `2>/dev/null` bolted onto it.
run_sync pull '[]' '[{"number":11,"state":"OPEN","body":"x"}]' '[]' '[]'
_ll="$(last_line)"
if ! grep -q 'the bead list came back empty' "$TMP/err"; then
    bad "a vacuous held-bead check does not report itself as not-run" \
        "the fixture never emptied the bead list, so the check was never vacuous"
elif [ -z "${_ll##*held-bead check did not run*}" ]; then
    bad "a vacuous held-bead check does not report itself as not-run" \
        "the check ran and found no baseline, and the summary calls it skipped: $_ll"
else
    ok "a vacuous held-bead check does not report itself as not-run"
fi

# ...and the two withdrawals have to be ordered, because they overlap and only
# one of them speaks for the held arm. Every fixture above exercises exactly one
# at a time — the --limit cases carry a bead, the empty-list case carries a
# single uncapped issue — so which of the two arms wins when both conditions
# hold in the same run was never asserted anywhere. Swapping the `if` and the
# `elif` that decide it leaves this whole file green while the summary reads
# `held 0` over a bead list nobody enumerated, naming the cap as the only thing
# withdrawn: the gqlc-nvjz defect verbatim, restored under a second condition.
# The bead list is the more fundamental of the two — the orphan tally is a claim
# about the GH listing *measured against* the bead set, so a bead set nobody
# enumerated withdraws it whatever the GH listing did — and it therefore has to
# be read first.
run_sync pull '[]' "$GH_ALL_FULL" '[]' '[]'
_ll="$(last_line)"
if [ "$RC" -ne 0 ]; then
    bad "an unenumerated bead list outranks a capped GitHub listing" \
        "exited $RC, and pull rides on 'git pull': $_ll"
elif ! grep -q "the GitHub listing came back at its --limit of $ALL_LIMIT" "$TMP/err"; then
    bad "an unenumerated bead list outranks a capped GitHub listing" \
        "the fixture never reached the cap, so the two conditions are not both live"
elif ! grep -q 'the bead list came back empty' "$TMP/err"; then
    bad "an unenumerated bead list outranks a capped GitHub listing" \
        "the fixture never emptied the bead list, so the two conditions are not both live"
elif [ -z "${_ll##*held 0*}" ]; then
    bad "an unenumerated bead list outranks a capped GitHub listing" \
        "the cap displaced the empty bead list and printed a held count over a list nobody enumerated: $_ll"
elif [ -z "$_ll" ] || [ -n "${_ll##*held an unknown number of bead(s) (the bead list came back empty)*}" ]; then
    bad "an unenumerated bead list outranks a capped GitHub listing" \
        "the held arm does not name the bead list: $_ll"
elif [ -n "${_ll##*left an unknown number of unmirrored GH issue(s) alone (the bead list came back empty)*}" ]; then
    bad "an unenumerated bead list outranks a capped GitHub listing" \
        "the orphan arm blamed the cap over a bead set nobody enumerated: $_ll"
else
    ok "both withdrawals at once name the bead list, not the cap"
fi

# ...and the guard must not be the push side's, transplanted. There, a run that
# minted N issues and then saw an empty open listing is contradictory on its
# face. Here the corresponding witness would be "beads carry external_refs, so
# the all-state listing cannot be empty" — and that is false. `external_ref` is
# matched by /issues/(\d+)$ over any URL, so a ref legitimately names another
# repository; mirrored beads over an empty listing for *this* one is a reachable
# configuration, and refusing it would fire on every `git pull`. The bead stays
# held, by the same not-in-listing rule that holds any bead whose issue is out
# of view, and the run reports real numbers and exits 0.
FOREIGN="{\"id\":\"b-elsewhere\",\"status\":\"open\",\"external_ref\":\"https://github.com/other/repo/issues/42\",\"description\":\"x\"}"
run_sync pull "[$FOREIGN]" '[]' '[]' "[$FOREIGN]"
if [ "$RC" -ne 0 ]; then
    bad "a mirrored bead over an empty all-state listing is not refused" \
        "exited $RC: $(last_line)"
elif grep -q 'SKIPPING pull' "$TMP/err"; then
    bad "a mirrored bead over an empty all-state listing is not refused" \
        "refused a legitimately empty listing: $(last_line)"
elif ! grep -q 'holding b-elsewhere (GH #42) out of the pull' "$TMP/err"; then
    bad "a mirrored bead over an empty all-state listing is not refused" \
        "the bead was not held out of the pull: $(grep 'b-elsewhere' "$TMP/err" | tr '\n' '|')"
elif [ "$(last_line)" != "bd-gh-sync: pulled 0 bead(s), held 1, left 0 unmirrored GH issue(s) alone." ]; then
    bad "a mirrored bead over an empty all-state listing is not refused" \
        "got: $(last_line)"
else
    ok "a bead whose external_ref names another repository is held, not refused"
fi

# --- gqlc-w4q9 review: the header's claim has to hold on every exit ----------
# "Both actions end by writing their verdict to the last stderr line" is what
# .claude/settings.json and pre-push are built on, and `mktemp -d` failing broke
# it: a bare `|| exit 0` with nothing said. Both callers keep only that line, so
# a run that produced none is indistinguishable from one that had nothing to do
# — the same silence the rest of this file exists to remove, on the one early
# exit that is a fault rather than a repository where sync is not configured.
for act in pull push; do
    MKTEMP_RC=1
    run_sync "$act" '[{"id":"b-n","status":"open","external_ref":""}]' '[]' '[]'
    MKTEMP_RC=0
    if [ -z "$(last_line)" ]; then
        bad "a temp directory that cannot be made is reported ($act)" \
            "exited $RC without a word"
    else
        case "$(last_line)" in
            *"SKIPPING $act"*)
                ok "a temp directory that cannot be made is reported ($act)" ;;
            *) bad "a temp directory that cannot be made is reported ($act)" \
                "got: $(last_line)" ;;
        esac
    fi
done

# ...and the control: the stub is only ever a stub when the knob is set, so a
# run with it unset has to reach GitHub exactly as before. Without this the two
# cases above would pass just as well against a stub that always failed. The
# open listing holds the issue this push mints, for the gqlc-mbe0 reason above.
run_sync push '[{"id":"b-n","status":"open","external_ref":""}]' '[]' '[{"number":1}]'
if [ "$(last_line)" = "bd-gh-sync: pushed 1 new bead(s), closed 0 stale GH mirror(s)." ]; then
    ok "the mktemp stub is transparent when its knob is unset"
else
    bad "the mktemp stub is transparent when its knob is unset" "got: $(last_line)"
fi

# --- the assertion census ----------------------------------------------------
# The one property this file cannot get from `set -u`, from shellcheck or from
# its own exit status: that it still makes every assertion it made yesterday.
# `133 passed, 0 failed` is true of a file that quietly lost an assertion and
# true of one that did not, and nothing in the justfile, in .github/workflows or
# in lint-hooks-test.sh reads that number at all. Both of this tree's ways of
# losing an assertion are silent: a dropped closing quote on this branch
# swallowed twenty lines including a whole block and the run still said `0
# failed`, one assertion lighter, and deleting a block outright says `132
# passed, 0 failed` and looks like a clean run. A guard structurally unable to
# fail is this repository's most common defect; a suite that cannot notice
# losing a guard is that same defect one level up.
#
# Deliberately not a count. A count written beside the thing it counts goes
# stale in silence and fails as `132 != 133`, which names nothing to go and
# look at — this tree has already deleted one such header rather than correct
# it. A census of names fails as a set difference with a name in it, so the
# reader is told which assertion stopped running rather than that one did.
#
# Deliberately not derived from this file's own `ok` lines either. That set
# shrinks along with the block it describes, which is precisely the edit it
# exists to survive: a census a deletion also deletes agrees with the deletion.
# The names are written down here, away from the blocks that raise them, so
# every way of losing one — deleted, commented out, swallowed by a quote,
# stranded behind a branch that stopped running — arrives as the same missing
# name. Two assertions sharing a name would reopen the hole, since either could
# then be deleted under cover of the other, so that is refused here too.
#
# Adding an assertion costs one line here, and the failure prints the line to
# add. Written in execution order for a reader; compared as a set.
_census="the assertion census matches the assertions that ran"
LC_ALL=C sort -u >"$TMP/census.txt" <<'CENSUS'
in_progress bead with GH content to lose is held and reported
blocked bead with GH content to lose is held and reported
deferred bead with GH content to lose is held and reported
in_progress bead byte-identical to an open mirror is held silently
blocked bead byte-identical to an open mirror is held silently
deferred bead byte-identical to an open mirror is held silently
blocked bead whose mirror was closed on GH is still reported
open bead whose GH mirror closed is pulled
GH body ahead of bd does not block the pull
an eligible pull is issued with --prefer-github, or GitHub's added lines lose
an eligible pull is issued with --pull-only, or the pull pushes as well
bd-only amendment is held out of pull scope
bd-side re-indent is held out of pull scope
bd-side reorder is held out of pull scope
GH body that does not extend the bead description is held out
a GH body that prepends to the bead description is held out
trailing-block deletion is indistinguishable from a GH append (known)
a CRLF GH body still counts as extending an LF bead description
a bd-only amendment is still held out when the GH body is CRLF
a no-break space in the bead description does not block the pull
locally-closed bead still open on GH is held out
bead already in sync with GH is not re-pulled
unreadable bead payload blocks the pull and says so
the refusal is the last line a tail -1 caller keeps
the selection's own error is still reported, above the verdict
unreadable GH payload blocks the pull
no eligible bead means bd github sync is not invoked
hostile payload reaches no unscoped pull (both empty)
hostile payload reaches no unscoped pull (beads malformed)
hostile payload reaches no unscoped pull (gh malformed)
hostile payload reaches no unscoped pull (both null)
hostile payload reaches no unscoped pull (empty strings)
hostile payload reaches no unscoped pull (bead id empty)
hostile payload reaches no unscoped pull (bead id flag-shaped)
hostile payload reaches no unscoped pull (gh fields null)
250 eligible beads are split into 3 batches, each id sent once
guard runs on a payload past MAX_ARG_STRLEN
orphan GH issue is reported, not minted into a bead
closed orphan is neither adopted nor reported
bead closed before its first push has its new mirror closed
already-mirrored closed bead still has its mirror closed
open bead's mirror is left alone
a push with nothing to do still says so on the line a caller keeps
a push that did everything it set out to do exits 0
the summary counts both arms: beads mirrored and mirrors closed
an unusable bead id is refused and named, the bead beside it pushed (apostrophe)
a refused bead id makes the push fail loudly (apostrophe)
an unusable bead id is refused and named, the bead beside it pushed (space)
a refused bead id makes the push fail loudly (space)
an unusable bead id is refused and named, the bead beside it pushed (leading dash)
a refused bead id makes the push fail loudly (leading dash)
an unusable bead id is refused and named, the bead beside it pushed (newline)
a refused bead id makes the push fail loudly (newline)
an unusable bead id is refused and named, the bead beside it pushed (empty)
a refused bead id makes the push fail loudly (empty)
a push that mirrored nothing because every id was refused says exactly that
a batch that exited non-zero is named as a failed batch, not a bad id
a failed batch makes the push exit non-zero
250 unmirrored beads are split into 3 batches, each id sent once
each bead id reaches 'bd github push' as an argv word of its own
'gh issue close' gets the issue number and the whole comment as one argument each
an unusable bead list refuses the push and says so ('bd list' exits non-zero)
an unusable bead list refuses the push and says so (no output)
an unusable bead list refuses the push and says so (whitespace only)
an unusable bead list refuses the push and says so (truncated JSON)
an unusable bead list refuses the push and says so (not JSON at all)
an unreadable post-push snapshot is reported, not walked as empty
an unreadable open-issue listing is reported, not read as nothing stale
a close pass that wrote no verdict reads as blind, not as nothing stale
an empty post-push snapshot reads as blind, not as nothing stale
two empty snapshots read as blind, not as an empty repository
an empty pre-push snapshot reads as blind, not as nothing to push
a run with both snapshots readable still prints both counts and exits 0
a mirror that would not close is named and counted
held bead reverted behind the script's back is reported
held bead that did not move produces no warning
a reverted claim is reported even when no bead was eligible to pull
the summary line carries the postcondition warning
a held bead whose description was clobbered is reported
a clobbered description reaches the tail -1 caller
a bead whose status and description both moved warns once, naming both
a held bead whose description only gained trailing whitespace is quiet
the pull takes a before and an after snapshot of the bead list
a postcondition that ran does not claim it was skipped
an unusable post-pull snapshot is reported ('bd list' exits non-zero)
an unusable post-pull snapshot is reported (no output)
an unusable post-pull snapshot is reported (whitespace only)
an unusable post-pull snapshot is reported (empty bead list)
an unusable post-pull snapshot is reported (truncated JSON)
an unusable post-pull snapshot is reported (not JSON at all)
the blind notice names the exit status it saw
a held bead deleted between the snapshots is reported as gone
the summary line names the deleted held bead and says it was deleted
a pulled bead missing from the second snapshot is not a held-bead finding
an id the gate refused is watched by the held-bead detector
a bead in a batch that exited non-zero is watched by the held-bead detector
an exemption set that could not be written stops the held-bead check
an empty second snapshot stays blind rather than reading as deletions
final stderr line summarises the run for a tail -1 caller
a failed sync is reported on the line a tail -1 caller keeps
an empty bead id reaches no 'bd github sync' batch
zero batches run is reported as a failed pull
one unusable bead id does not take the batch down with it
a bead id carrying a quote is refused rather than passed to argv
the summary counts what was pulled, not what was eligible
the refused bead id is named on stderr
a bead id carrying a space is refused, not truncated to its first token
no batch is issued with an empty --issues
a hold and a pull cannot name the same bead in one run
a bead id carrying a newline is refused by name, escaped
a bead id that cannot be passed is a failed pull, not a success
a split id does not blind the held-bead check for the id it collides with
a bead id that splits its record reaches no batch and is counted as unpulled
a failed batch, a hold, an orphan and a moved bead share one summary line
the push blind notice names the 'gh issue list' exit status it saw
the 'gh issue list' stub is transparent when its knob is unset
the push blind notice names the 'bd list' exit status it saw
a refused push names the 'bd list' exit status, verdict still last
a postcondition whose interpreter died says the check did not run
an unreadable post-pull list is named as unreadable, not as not-run
the python3 stub is transparent when its knob is unset
a bead id carrying a newline reaches the close comment escaped, one argv word
a bead with no id leaves the close comment's bead name empty, not 'None'
a multi-line close_reason contributes only its first line to the comment
a close_reason past 300 characters is clamped to 300 with an ellipsis
a push that minted issues and then saw none of them refuses to count
an empty open listing after a push of nothing is a legitimate empty
an open listing returning exactly its --limit is treated as truncated
an open listing one short of its --limit is counted as the whole set
a pull over a capped GitHub listing reports the count as unknown
an all-state listing one short of its --limit is counted as the whole set
a refused pull names the 'bd list' exit status, verdict still last
a refused pull names the 'gh issue list' exit status, verdict still last
an empty pull-side bead list withdraws both counts derived from it
a vacuous held-bead check does not report itself as not-run
both withdrawals at once name the bead list, not the cap
a bead whose external_ref names another repository is held, not refused
a temp directory that cannot be made is reported (pull)
a temp directory that cannot be made is reported (push)
the mktemp stub is transparent when its knob is unset
the assertion census matches the assertions that ran
CENSUS

# The census names itself: this assertion is running, so it is executed by
# construction, and a reader of the list above sees the whole inventory rather
# than all of it but one.
{ cat "$TMP/ran.txt"; printf '%s\n' "$_census"; } | LC_ALL=C sort >"$TMP/ran_sorted.txt"
uniq "$TMP/ran_sorted.txt" >"$TMP/ran_uniq.txt"
_dup="$(uniq -d "$TMP/ran_sorted.txt" | tr '\n' '|' | sed 's/|$//')"
_lost="$(LC_ALL=C comm -23 "$TMP/census.txt" "$TMP/ran_uniq.txt" | tr '\n' '|' | sed 's/|$//')"
_uncensused="$(LC_ALL=C comm -13 "$TMP/census.txt" "$TMP/ran_uniq.txt" | tr '\n' '|' | sed 's/|$//')"
if [ "$fail" -ne 0 ]; then
    # Not read on a run that is already red, and said out loud rather than
    # passed quietly. 64 of this file's 111 assertion blocks word `bad`
    # differently from `ok` — a failing block logs "…is reported" where a
    # passing one logs "…is reported as gone" — so a genuine failure comes back
    # through here as a missing name *and* an unregistered one, and a census
    # that cries wolf on every red run is a census nobody reads. Counted, not
    # estimated: an earlier draft said "several", meaning about twenty, and was
    # out by a factor of three in the direction that makes the exemption more
    # necessary rather than less. Here it is as a command rather than a figure
    # that goes stale — the 65 names only a `bad` arm can ever log:
    #
    #   f=.githooks/tests/bd-gh-sync-test.sh
    #   comm -23 <(grep -oE '(^|[ ;)])bad +"[^"]*"' "$f" | sed 's/^[^"]*//' | sort -u) \
    #            <(grep -oE '(^|[ ;)])ok +"[^"]*"' "$f" | sed 's/^[^"]*//' | sort -u)
    #
    # The exemption is a hole, and it is bounded rather than closed. An edit
    # that both breaks something and loses an assertion reports only the break:
    # replace the `ok` of `the mktemp stub is transparent when its knob is
    # unset` with `:`, turn `if [ "${_beads_n:-0}" -eq 0 ]` in bd-gh-sync into
    # `if false`, and the run prints two FAILs, this note, and never names the
    # assertion it lost. What bounds it is that such a run cannot land. The last
    # line of this file exits non-zero on any FAIL; justfile:183 runs this file
    # inside `test-hooks`; `test` depends on `test-hooks`; `just` aborts a
    # recipe on its first non-zero line and exits with that status; so `just
    # test` at .github/workflows/ci.yml:64 fails the `test` job, which is a
    # required status check on master with enforce_admins on. The suite is
    # failing already; fix that and the roll-call speaks on the next run, which
    # is the only run whose silence this exists to distrust. The loss of a name
    # is deferred to that run, not lost.
    printf -- 'note - the assertion census is not read on a red run (%d failed above)\n' "$fail"
elif [ -n "$_dup" ]; then
    bad "$_census" \
        "two assertions answer to one name, so either could be deleted under cover of the other: $_dup"
elif [ -n "$_lost" ]; then
    bad "$_census" \
        "declared in the census and did not run — deleted, unreachable or renamed: $_lost"
elif [ -n "$_uncensused" ]; then
    bad "$_census" \
        "ran and is not in the census; add it there so its loss would be noticed: $_uncensused"
else
    ok "$_census"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
