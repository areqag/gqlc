#!/usr/bin/env bash
# Unit tests for .githooks/claude-pre-ask — the PreToolUse gate that refuses
# interactive-input tools inside an unattended seat (bd gqlc-n97e, gqlc-07e3).
#
# A seat is a claude session with no human at the terminal, so a tool whose
# whole function is to ask one blocks until the session is killed. Nothing
# detects it: the statusline heartbeat keeps refreshing DURING the wedge,
# because the turn is still live. 13+ minutes were measured that way, with a
# worker slot held and every indicator green.
#
# Two halves are asserted here, and neither alone is a gate:
#   - the hook refuses, and refuses even when its own input is unreadable
#     (a wrongly denied prompt costs a mail; a wrongly allowed one costs a
#     wedged slot, so the error direction is deliberate);
#   - .claude/settings.json actually invokes it, on both tool names. The hook
#     denies whatever it is handed; the matcher decides what it is handed.
#
# The unset-KINGDOM_SEAT case is the liveness control: without it every deny
# row above would pass against a hook that refused unconditionally, which
# would strip an attended session's menus in this repo.
#
# Run via: just test-hooks
set -u

HOOKS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO_ROOT="$(cd "$HOOKS_DIR/.." && pwd)"
HOOK="$HOOKS_DIR/claude-pre-ask"
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

ASK_PAYLOAD='{"tool_name":"AskUserQuestion","tool_input":{"questions":[]}}'
PLAN_PAYLOAD='{"tool_name":"EnterPlanMode","tool_input":{}}'

# $1 = stdin payload, rest = env arguments (KEY=VALUE, or -u KEY)
run_hook() {
    local payload="$1"
    shift
    printf '%s' "$payload" | env "$@" "$HOOK" >"$OUT" 2>"$ERR"
    last_rc=$?
}

# The harness reads a decision out of two nested fields, and a payload naming
# the wrong event is ignored — which would read as "allowed" at the tool, but
# as a deny to any grep for permissionDecision. Both fields, or the verdict
# does not correspond to what the session would experience.
verdict() {
    python3 - "$OUT" <<'PY'
import json
import sys

raw = open(sys.argv[1]).read()
if not raw.strip():
    print("silent")
    sys.exit(0)
try:
    payload = json.loads(raw)
except Exception:
    print("unparseable")
    sys.exit(0)
hook_out = payload.get("hookSpecificOutput") or {}
print("%s/%s" % (hook_out.get("hookEventName"), hook_out.get("permissionDecision")))
PY
}

reason_text() {
    python3 - "$OUT" <<'PY'
import json
import sys

raw = open(sys.argv[1]).read()
try:
    payload = json.loads(raw)
except Exception:
    sys.exit(0)
hook_out = payload.get("hookSpecificOutput") or {}
sys.stdout.write(str(hook_out.get("permissionDecisionReason") or ""))
PY
}

assert_rc_zero() {
    if [ "$last_rc" -eq 0 ]; then
        ok "$1"
    else
        bad "$1 (expected exit 0, got $last_rc)"
    fi
}
assert_verdict() {
    local got
    got="$(verdict)"
    if [ "$got" = "$2" ]; then ok "$1"; else bad "$1 (expected $2, got $got)"; fi
}
assert_stdout_empty() {
    if [ -s "$OUT" ]; then
        bad "$1 (stdout carried: $(cat "$OUT"))"
    else
        ok "$1"
    fi
}
assert_reason_has() {
    if reason_text | grep -qF -- "$2"; then ok "$1"; else bad "$1 (reason lacks '$2')"; fi
}

# --- a seat is refused ------------------------------------------------------
run_hook "$ASK_PAYLOAD" KINGDOM_SEAT=test
assert_rc_zero "seat: AskUserQuestion exits 0"
assert_verdict "seat: AskUserQuestion is denied" "PreToolUse/deny"
assert_reason_has "seat: the denial names the tool it refused" "AskUserQuestion"

# A denial that only says no teaches nothing, and the citizen it lands on has
# no other way to be told. Each substring is one step of the path that works.
assert_reason_has "seat: the denial names the bead-notes step" "--append-notes"
assert_reason_has "seat: the denial names the mail step" "bd mail send"
assert_reason_has "seat: the denial names the sleep step" "km sleep"
assert_reason_has "seat: the denial cites the measured incident" "gqlc-n97e"

run_hook "$PLAN_PAYLOAD" KINGDOM_SEAT=test
assert_rc_zero "seat: EnterPlanMode exits 0"
assert_verdict "seat: EnterPlanMode is denied" "PreToolUse/deny"
# Reading the name from stdin rather than hardcoding one: with a literal, the
# AskUserQuestion row above passes and this one does not.
assert_reason_has "seat: the denial names EnterPlanMode too" "EnterPlanMode"

# --- fail closed on unreadable input ----------------------------------------
# Two distinct shapes: bytes that are not JSON at all, and JSON that parses
# into something with no fields to read. Either way the hook cannot know what
# it was asked about, and the seat is still unattended.
run_hook 'not json at all {{{' KINGDOM_SEAT=test
assert_rc_zero "seat: unparseable stdin exits 0"
assert_verdict "seat: unparseable stdin still denies" "PreToolUse/deny"

run_hook '[]' KINGDOM_SEAT=test
assert_rc_zero "seat: a non-object payload exits 0"
assert_verdict "seat: a non-object payload still denies" "PreToolUse/deny"

# --- the liveness control ---------------------------------------------------
# Without these two rows, every row above passes against `print(DENY)`.
run_hook "$ASK_PAYLOAD" -u KINGDOM_SEAT
assert_rc_zero "attended: exits 0"
assert_stdout_empty "attended: AskUserQuestion is allowed in silence"

# km's own seat predicate is `${KINGDOM_SEAT:-}` (km sleep refuses on empty as
# "not a seat session"), so empty reads as attended here too rather than as a
# third state the two files would disagree about.
run_hook "$ASK_PAYLOAD" KINGDOM_SEAT=
assert_rc_zero "empty KINGDOM_SEAT: exits 0"
assert_stdout_empty "empty KINGDOM_SEAT: reads as attended, like km's own predicate"

# --- the wiring -------------------------------------------------------------
# The hook refuses whatever it is invoked on, so on its own it proves nothing
# about which tools reach it. This row reads the settings file the harness
# reads. It also pins the neighbouring Bash entry, because the edit that adds
# this gate is exactly the edit that could replace that array instead of
# appending to it — and the master-commit guard would die without a sound.
check_settings() {
    python3 - "$SETTINGS" <<'PY'
import json
import sys

with open(sys.argv[1]) as fh:
    settings = json.load(fh)
entries = settings.get("hooks", {}).get("PreToolUse", [])
wanted = {
    "AskUserQuestion|EnterPlanMode": ".githooks/claude-pre-ask",
    "Bash": ".githooks/claude-pre-bash",
}
for matcher, command in wanted.items():
    hit = [e for e in entries if e.get("matcher") == matcher]
    if not hit:
        print("no PreToolUse entry with matcher %r (found: %r)"
              % (matcher, [e.get("matcher") for e in entries]))
        sys.exit(1)
    hooks = hit[0].get("hooks") or []
    got = [(h.get("type"), h.get("command")) for h in hooks]
    if ("command", command) not in got:
        print("matcher %r does not run %r (runs: %r)" % (matcher, command, got))
        sys.exit(1)
print("ok")
PY
}
if wiring="$(check_settings)" && [ "$wiring" = ok ]; then
    ok "settings.json wires both PreToolUse gates"
else
    bad "settings.json wiring: $wiring"
fi

# git runs nothing without this bit, and neither does the harness.
if [ -x "$HOOK" ]; then
    ok "the hook is executable"
else
    bad "the hook is executable (test -x $HOOK failed)"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
