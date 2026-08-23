#!/usr/bin/env bash
# Unit tests for .githooks/claude-tool-witness — the town's progress witness
# (bd gqlc-r3ac, gqlc-gw10).
#
# The claim under test is not "a file gets written". It is that the file
# advances ONLY when a tool call really started or really finished. Every other
# liveness instrument the town owns is written by something on a schedule —
# km-statusline writes heartbeat.json every prompt refresh, all the way through
# a wedged turn — so a witness that could be advanced by a timer would be the
# fourth instrument in one night to vouch for dead machinery (gqlc-z1qw,
# gqlc-bn5r, gqlc-ed2u, gqlc-n97e).
#
# So the rows that matter most here are the NEGATIVE ones: an unknown event
# writes nothing, a PreToolUse never sets last_progress, and a session that is
# not a seat leaves no trace at all. A suite of positive rows would pass against
# a hook that touched the file on every invocation, which is precisely the shape
# this file exists to rule out.
#
# Run via: just test-hooks
set -u

HOOKS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO_ROOT="$(cd "$HOOKS_DIR/.." && pwd)"
HOOK="$HOOKS_DIR/claude-tool-witness"
SETTINGS="$REPO_ROOT/.claude/settings.json"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
OUT="$TMP/stdout"
ERR="$TMP/stderr"

pass=0
fail=0
last_rc=0

ok() {
    pass=$((pass + 1))
    printf 'ok   - %s\n' "$1"
}
bad() {
    fail=$((fail + 1))
    printf 'FAIL - %s\n' "$1"
}

PRE='{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}'
POST='{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_response":{}}'

# A fresh state dir per scenario. Rows that share one would read each other's
# counters, and the counters are half of what is asserted here.
fresh_state() {
    STATE="$TMP/state-$1"
    rm -rf "$STATE"
    mkdir -p "$STATE"
    WITNESS="$STATE/seats/mihr/progress.json"
}

run_hook() { # <payload> [env assignments...]
    local payload="$1"
    shift
    printf '%s' "$payload" | env KINGDOM_SEAT=mihr KM_STATE_DIR="$STATE" "$@" \
        "$HOOK" >"$OUT" 2>"$ERR"
    last_rc=$?
}

field() { # <jq path> -> the value, or the empty string
    jq -r "$1 // empty" "$WITNESS" 2>/dev/null
}

assert_rc_zero() {
    if [ "$last_rc" -eq 0 ]; then ok "$1"; else bad "$1 (exit $last_rc)"; fi
}
assert_field() { # <name> <jq path> <expected>
    local got
    got="$(field "$2")"
    if [ "$got" = "$3" ]; then ok "$1"; else bad "$1 (expected '$3', got '$got')"; fi
}
assert_no_witness() {
    if [ -e "$WITNESS" ]; then
        bad "$1 (a witness was written: $(cat "$WITNESS"))"
    else
        ok "$1"
    fi
}

# --- a tool call starting, and a tool call finishing -------------------------
fresh_state basic
run_hook "$PRE"
assert_rc_zero "PreToolUse exits 0"
assert_field "PreToolUse counts a started tool call" .started 1
assert_field "PreToolUse names the tool" .last_tool Bash
assert_field "PreToolUse records the seat" .seat mihr

# THE row. last_progress is the field km reads to decide whether anything has
# happened, and a start is not a happening: a seat wedged inside its first tool
# call has fired PreToolUse and will never fire PostToolUse, and if a start set
# this field the witness would report progress at the exact moment the seat died.
if [ -n "$(field .last_progress)" ]; then
    bad "a started tool call is not progress" \
        "last_progress was set by PreToolUse alone: $(cat "$WITNESS")"
else
    ok "a started tool call is not progress — only PostToolUse sets last_progress"
fi
if [ -z "$(field .last_start)" ]; then
    bad "a started tool call is recorded as an attempt (last_start)"
else
    ok "a started tool call is recorded as an attempt, which is what dates a wedge"
fi

run_hook "$POST"
assert_rc_zero "PostToolUse exits 0"
assert_field "PostToolUse counts a completed tool call" .completed 1
assert_field "PostToolUse leaves the started count alone" .started 1
if [ -z "$(field .last_progress)" ]; then
    bad "PostToolUse stamps last_progress"
else
    ok "PostToolUse stamps last_progress — the one event that means work advanced"
fi

# --- the negative rows: what must NOT move the witness -----------------------
# An event this hook does not understand. Without this row the hook could key
# off the mere presence of tool_name and a future Notification or Stop payload
# would silently increment a counter that means something else.
fresh_state unknown
run_hook '{"hook_event_name":"Notification","tool_name":"Bash"}'
assert_rc_zero "an unrecognised hook event exits 0"
assert_no_witness "an unrecognised hook event writes nothing"

fresh_state noevent
run_hook '{"tool_name":"Bash"}'
assert_rc_zero "a payload with no event name exits 0"
assert_no_witness "a payload with no event name writes nothing"

fresh_state garbage
run_hook 'not json at all {{{'
assert_rc_zero "unparseable stdin exits 0"
assert_no_witness "unparseable stdin writes nothing"

fresh_state nonobject
run_hook '[]'
assert_rc_zero "a non-object payload exits 0"
assert_no_witness "a non-object payload writes nothing"

# Not a seat: an ordinary session in this repo has no board to appear on, and
# writing one would invent a seat directory for a person.
fresh_state notseat
printf '%s' "$PRE" | env -u KINGDOM_SEAT KM_STATE_DIR="$STATE" "$HOOK" >"$OUT" 2>"$ERR"
last_rc=$?
assert_rc_zero "a session with no KINGDOM_SEAT exits 0"
assert_no_witness "a session with no KINGDOM_SEAT writes nothing"

fresh_state nostate
# -u before the assignments: env stops reading options at the first operand, so
# `env KEY=v -u OTHER cmd` runs a command literally named "-u" and exits 127 —
# which this row's rc assertion caught, and its no-witness assertion did not.
printf '%s' "$PRE" | env -u KM_STATE_DIR KINGDOM_SEAT=mihr "$HOOK" >"$OUT" 2>"$ERR"
last_rc=$?
assert_rc_zero "a seat with no KM_STATE_DIR exits 0"
assert_no_witness "a seat with no KM_STATE_DIR writes nothing"

# --- it must never block, and never speak ------------------------------------
# A PreToolUse hook that exits 2 DENIES the tool, and one that prints JSON can
# be read as a permission decision. Either would let this instrument wedge the
# session it exists to watch — the exact failure it reports on.
fresh_state silence
run_hook "$PRE"
if [ -s "$OUT" ]; then
    bad "the witness says nothing on stdout" "stdout carried: $(cat "$OUT")"
else
    ok "the witness says nothing on stdout, so it cannot be read as a decision"
fi

# An unwritable state dir is the ordinary shape of this: a seat whose state dir
# was removed under it. The hook must still not block the tool.
fresh_state unwritable
: >"$STATE/seats"   # a FILE where the seats directory must be
run_hook "$PRE"
assert_rc_zero "a state dir it cannot write into still exits 0"

# --- corrupt input, and the counters -----------------------------------------
# A hand-edited or truncated witness must not take the instrument down, and must
# not be read as a fresh file with real counts either.
fresh_state corrupt
mkdir -p "$STATE/seats/mihr"
printf 'not json\n' >"$WITNESS"
run_hook "$POST"
assert_rc_zero "a corrupt witness exits 0"
assert_field "a corrupt witness restarts its counters rather than crashing" .completed 1
if jq -e . "$WITNESS" >/dev/null 2>&1; then
    ok "a corrupt witness is replaced by valid JSON"
else
    bad "a corrupt witness is replaced by valid JSON (still: $(cat "$WITNESS"))"
fi

fresh_state negcount
mkdir -p "$STATE/seats/mihr"
printf '{"seat":"mihr","started":"lots","completed":-4}\n' >"$WITNESS"
run_hook "$POST"
assert_field "a non-numeric count restarts at zero" .started 0
assert_field "a negative count restarts at zero" .completed 1

# --- the parallel-tool-call row ----------------------------------------------
# Tool calls in one assistant block run at the same time, so several copies of
# this hook read-modify-write one file concurrently. A lost PostToolUse is not a
# cosmetic miscount: it is a seat whose completed count trails forever. This row
# is the only one that fails if the flock is removed.
fresh_state parallel
mkdir -p "$STATE/seats/mihr"
N=25
for _ in $(seq "$N"); do
    printf '%s' "$POST" | env KINGDOM_SEAT=mihr KM_STATE_DIR="$STATE" "$HOOK" \
        >/dev/null 2>&1 &
done
wait
assert_field "$N concurrent PostToolUse hooks lose no increment" .completed "$N"

# --- the wiring ---------------------------------------------------------------
# The hook records whatever it is handed, so on its own it proves nothing about
# whether any tool call reaches it. This reads the file the harness reads, and
# pins the neighbouring entries too: the edit that adds a matcher is exactly the
# edit that can replace an array instead of appending to it, and the Bash gate
# would die without a sound.
check_settings() {
    python3 - "$SETTINGS" <<'PY'
import json
import sys

with open(sys.argv[1]) as fh:
    settings = json.load(fh)
hooks = settings.get("hooks", {})
wanted = {
    # An empty matcher is "every tool". A witness that watched a NAMED set of
    # tools would be blind to exactly the case gqlc-r3ac names: a renamed or
    # unknown interactive tool nobody wrote down.
    ("PreToolUse", ""): ".githooks/claude-tool-witness",
    ("PostToolUse", ""): ".githooks/claude-tool-witness",
    ("PreToolUse", "Bash"): ".githooks/claude-pre-bash",
    ("PreToolUse", "AskUserQuestion|EnterPlanMode"): ".githooks/claude-pre-ask",
}
for (event, matcher), command in wanted.items():
    entries = hooks.get(event) or []
    hit = [e for e in entries if e.get("matcher") == matcher]
    if not hit:
        print("no %s entry with matcher %r (found: %r)"
              % (event, matcher, [e.get("matcher") for e in entries]))
        sys.exit(1)
    got = [(h.get("type"), h.get("command")) for h in (hit[0].get("hooks") or [])]
    if ("command", command) not in got:
        print("%s matcher %r does not run %r (runs: %r)"
              % (event, matcher, command, got))
        sys.exit(1)
print("ok")
PY
}
if wiring="$(check_settings)" && [ "$wiring" = ok ]; then
    ok "settings.json wires the witness on both PreToolUse and PostToolUse"
else
    bad "settings.json wiring: $wiring"
fi

if [ -x "$HOOK" ]; then
    ok "the hook is executable"
else
    bad "the hook is executable (test -x $HOOK failed)"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
