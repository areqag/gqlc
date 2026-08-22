#!/usr/bin/env bash
# Tests for kingdom/bin/km — the state machinery of the Թագաւորութիւն.
#
# Everything runs against a throwaway KM_STATE_DIR, so the real town's mail
# and seat state are never touched, and nothing here needs tmux, claude, bd,
# or a running dispatcher. What is pinned is the machinery the society stands
# on: the config reader, seat identity, mail delivery (single, broadcast, and
# the read/unread move), wake queueing, and the halt flag — plus the refusals,
# because a mail system that misdelivers silently or a wake that invents a
# seat is how the ledger starts lying.
#
# Run via: just test-hooks
set -u

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
KM="$REPO/kingdom/bin/km"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export KM_STATE_DIR="$TMP/state"
# The suite may itself run inside a seat one day; identity must not leak in.
unset KINGDOM_SEAT

pass=0
fail=0
ok()  { pass=$((pass + 1)); printf 'ok   - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s: %s\n' "$1" "$2"; }

OUT=""
RC=0
run() {
    OUT="$("$KM" "$@" 2>&1)"
    RC=$?
}

# --- the hermetic seam: everything below stands on this one ------------------

run state-dir
if [ "$RC" -ne 0 ] || [ "$OUT" != "$TMP/state" ]; then
    bad "KM_STATE_DIR overrides the derived state dir" "rc=$RC out=$OUT"
else
    ok "KM_STATE_DIR overrides the derived state dir"
fi

# --- config reader -----------------------------------------------------------

run cfg concurrency max_active
if [ "$RC" -ne 0 ] || [ "$OUT" != 5 ]; then
    bad "cfg reads a bare scalar" "rc=$RC out=$OUT"
else
    ok "cfg reads max_active from [concurrency]"
fi

run cfg kingdom tmux_session
if [ "$RC" -ne 0 ] || [ "$OUT" != kingdom ]; then
    bad "cfg strips quotes from a string value" "rc=$RC out=$OUT"
else
    ok "cfg strips the quotes from tmux_session"
fi

# --- seat identity -----------------------------------------------------------

run seat-info sedrak
if [ "$RC" -ne 0 ] || [ "$OUT" != "mayor claude-opus-5 Սեդրակ" ]; then
    bad "seat-info decodes class:model:display" "rc=$RC out=$OUT"
else
    ok "seat-info sedrak is the mayor, claude-opus-5, Սեդրակ"
fi

run seat-info nobody
if [ "$RC" -eq 0 ]; then
    bad "an unknown seat is refused" "exited 0: $OUT"
else
    ok "seat-info refuses a seat the roster does not know"
fi

# --- mail: one letter, delivered and then read -------------------------------

run mail send sedrak -s "Test letter" -m "hello from the test"
letter="$(find "$KM_STATE_DIR/mail/sedrak/inbox" -type f -name '*--andranik--test-letter.md' 2>/dev/null)"
if [ "$RC" -ne 0 ] || [ -z "$letter" ]; then
    bad "a letter lands in the recipient's inbox" "rc=$RC out=$OUT"
elif ! grep -q '^from: andranik$' "$letter" || ! grep -q '^subject: Test letter$' "$letter"; then
    bad "a letter lands in the recipient's inbox" "front-matter is wrong: $(cat "$letter")"
elif [ -z "$(find "$KM_STATE_DIR/mail/andranik/sent" -type f -name '*test-letter*' 2>/dev/null)" ]; then
    bad "a letter lands in the recipient's inbox" "no sent copy for the sender"
else
    ok "a letter is delivered with front-matter, and the sender keeps a sent copy"
fi

run mail read --seat sedrak "$(basename "$letter")"
if [ "$RC" -ne 0 ] || ! printf '%s' "$OUT" | grep -q 'hello from the test'; then
    bad "reading a letter prints it" "rc=$RC out=$OUT"
elif [ -e "$letter" ]; then
    bad "reading a letter marks it read" "still in the inbox"
elif [ ! -e "$KM_STATE_DIR/mail/sedrak/read/$(basename "$letter")" ]; then
    bad "reading a letter marks it read" "not in read/ either — the letter is gone"
else
    ok "mail read --seat prints the letter and moves it from inbox to read"
fi

# --- mail: the town broadcast ------------------------------------------------
# Everyone but the sender, and the king too. A sender who receives their own
# broadcast reads it as unread mail and the dispatcher wakes them for it.

run mail send town --from sedrak -s "Town notice" -m "the bell rings"
if [ "$RC" -ne 0 ]; then
    bad "a town broadcast is delivered" "rc=$RC out=$OUT"
elif [ -z "$(find "$KM_STATE_DIR/mail/andranik/inbox" -name '*town-notice*' 2>/dev/null)" ]; then
    bad "a town broadcast is delivered" "the king did not get it"
elif [ -z "$(find "$KM_STATE_DIR/mail/vahagn/inbox" -name '*town-notice*' 2>/dev/null)" ]; then
    bad "a town broadcast is delivered" "a warrior did not get it"
elif [ -n "$(find "$KM_STATE_DIR/mail/sedrak/inbox" -name '*town-notice*' 2>/dev/null)" ]; then
    bad "a town broadcast is delivered" "the sender got their own broadcast"
else
    ok "a town broadcast reaches every other citizen and the king, not the sender"
fi

# --- mail: the refusals ------------------------------------------------------

run mail send zork -s "x" -m "y"
if [ "$RC" -eq 0 ] || [ -d "$KM_STATE_DIR/mail/zork" ]; then
    bad "an unknown recipient is refused" "rc=$RC, box created: $OUT"
else
    ok "mail to a recipient the roster does not know is refused, nothing delivered"
fi

run mail send sedrak artur -s "x" -m "y"
if [ "$RC" -eq 0 ]; then
    bad "two recipients on one letter are refused" "exited 0: $OUT"
else
    ok "a second positional recipient is refused (town is the broadcast)"
fi

# --- wake: queued for a sleeping seat, refused for a ghost -------------------

run wake vahagn --bead gqlc-test --reason "ready warrior work"
wakef="$KM_STATE_DIR/seats/vahagn/wake"
if [ "$RC" -ne 0 ] || [ ! -f "$wakef" ]; then
    bad "a wake is queued for a sleeping seat" "rc=$RC out=$OUT"
elif ! grep -q 'bead:gqlc-test' "$wakef"; then
    bad "a wake is queued for a sleeping seat" "the bead id is not in the wake file"
else
    ok "a wake for a sleeping seat queues the reason, bead id included"
fi
rm -f "$wakef"

run wake nobody
if [ "$RC" -eq 0 ]; then
    bad "waking an unknown seat is refused" "exited 0: $OUT"
else
    ok "a wake for a seat the roster does not know is refused"
fi

# --- the halt flag stops the dispatcher --------------------------------------
# Sedrak is given unread mail first: without the halt that is exactly what
# would wake him, so a quiet dispatcher here is the flag working, not a
# dispatcher with nothing to do. (With no tmux session the dispatcher parks
# even earlier; both prints are accepted, and the wake assertion holds either
# way.)

run mail send sedrak -s "Unanswered" -m "waiting"
run halt test cause
haltf="$KM_STATE_DIR/halt"
if [ "$RC" -ne 0 ] || [ ! -f "$haltf" ]; then
    bad "halt raises the flag" "rc=$RC out=$OUT"
elif ! grep -q 'raised by andranik' "$haltf" || ! grep -q 'test cause' "$haltf"; then
    bad "halt raises the flag" "the flag does not say who or why: $(cat "$haltf")"
else
    ok "halt raises a flag naming who raised it and why"
fi

run dispatch
if [ "$RC" -ne 0 ]; then
    bad "a halted dispatcher wakes nobody" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -qE 'halted|town is down'; then
    bad "a halted dispatcher wakes nobody" "it neither halted nor parked: $OUT"
elif [ -f "$KM_STATE_DIR/seats/sedrak/wake" ]; then
    bad "a halted dispatcher wakes nobody" "sedrak was woken despite the halt"
else
    ok "a halted dispatcher wakes nobody, not even the mayor with unread mail"
fi

run resume
if [ "$RC" -ne 0 ] || [ -f "$haltf" ]; then
    bad "resume lowers the flag" "rc=$RC out=$OUT"
else
    ok "resume lowers the halt flag"
fi

run resume
if [ "$RC" -ne 0 ] || ! printf '%s' "$OUT" | grep -q 'no halt'; then
    bad "resume without a halt says so" "rc=$RC out=$OUT"
else
    ok "a second resume reports there was no halt, instead of pretending"
fi

# --- the dispatcher's two routing passes -------------------------------------
# `km dispatch` IS the autonomous factory: if it routes nothing, no seat ever
# wakes on its own. It shipped with no test at all and a jq type error that
# aborted the fresh pass on every unassigned bead it ever saw, labelled or not
# (bd gqlc-z1qw) — so the town has never routed a single bead, and said
# "done (0 wake(s) this run)" while doing it.
#
# bd and tmux are stubbed on PATH for these cases only; the halt case above
# deliberately runs against the real (absent) tmux. jq is NOT stubbed: the jq
# program is the thing under test.

BIN="$TMP/bin"
mkdir -p "$BIN"

cat >"$BIN/tmux" <<'STUB'
#!/usr/bin/env bash
# The town is up, but no seat holds a window — so a wake queues to a file
# rather than being typed into a pane, which is the state these cases read.
# list-windows prints nothing, which is what makes window_up false.
case "$1" in
    has-session)  exit 0 ;;
    list-windows) exit 0 ;;
esac
exit 0
STUB

cat >"$BIN/bd" <<'STUB'
#!/usr/bin/env bash
# Answers the dispatcher's two queries from fixture files named by the
# environment. A fixture may hold non-JSON, so a jq abort is expressible; a
# sidecar <fixture>.rc makes bd itself fail, so either half of the pipeline
# can be reddened on its own.
case "$*" in
    "ready --json")                     f="${KM_FAKE_READY:-}" ;;
    "list --status in_progress --json") f="${KM_FAKE_INPROG:-}" ;;
    *) exit 0 ;;
esac
[ -n "$f" ] || { echo "bd stub: no fixture named for: $*" >&2; exit 1; }
if [ -f "$f.rc" ]; then
    # Defaulted, not inlined: `exit ""` is not fatal in bash, so an empty knob
    # would leave the stub succeeding while looking as though it had failed.
    _rc="$(cat "$f.rc")"
    echo "bd: error: database is locked by another process" >&2
    exit "${_rc:-1}"
fi
cat "$f"
STUB
chmod +x "$BIN/tmux" "$BIN/bd"

DCASE=0
dispatch_case() { # $1=ready payload, $2=in-progress payload; fresh state each time
    DCASE=$((DCASE + 1))
    export KM_STATE_DIR="$TMP/dispatch-$DCASE"
    mkdir -p "$KM_STATE_DIR"
    export KM_FAKE_READY="$KM_STATE_DIR/ready.json"
    export KM_FAKE_INPROG="$KM_STATE_DIR/inprog.json"
    printf '%s' "$1" >"$KM_FAKE_READY"
    printf '%s' "$2" >"$KM_FAKE_INPROG"
}

run_dispatch() {
    OUT="$(PATH="$BIN:$PATH" "$KM" dispatch 2>&1)"
    RC=$?
}

wake_of()     { cat "$KM_STATE_DIR/seats/$1/wake" 2>/dev/null; }
woken_seats() { find "$KM_STATE_DIR/seats" -mindepth 2 -maxdepth 2 -name wake 2>/dev/null | sed 's|.*/seats/||; s|/wake$||' | sort | tr '\n' ' '; }

# The fresh pass: a bead of each class reaches a free seat of that class, and
# the unlabelled one reaches nobody. This row is also the liveness control for
# every row below it — it is the only one that proves the stubs, the tmux seam
# and the wake-file path all work, so a "nobody was woken" assertion elsewhere
# means the dispatcher declined rather than that the harness was inert.
dispatch_case '[
  {"id":"gqlc-unl","priority":0,"assignee":null,"labels":null},
  {"id":"gqlc-taken","priority":0,"assignee":"ar","labels":["class:warrior"]},
  {"id":"gqlc-w1","priority":1,"assignee":null,"labels":["class:warrior"]},
  {"id":"gqlc-a1","priority":2,"assignee":null,"labels":["area:parser","class:architect"]},
  {"id":"gqlc-j1","priority":3,"assignee":null,"labels":["class:judge"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the fresh pass routes a bead of each class" "rc=$RC out=$OUT"
elif ! wake_of aramazd | grep -q 'bead:gqlc-w1'; then
    bad "the fresh pass routes a bead of each class" "the warrior bead reached no warrior seat (woken: $(woken_seats)) out=$OUT"
elif ! wake_of artur | grep -q 'bead:gqlc-a1'; then
    bad "the fresh pass routes a bead of each class" "the architect bead reached no architect seat (woken: $(woken_seats)) out=$OUT"
elif ! wake_of mihr | grep -q 'bead:gqlc-j1'; then
    bad "the fresh pass routes a bead of each class" "the judge bead reached no judge seat (woken: $(woken_seats)) out=$OUT"
elif ! wake_of aramazd | grep -q 'ready warrior work'; then
    bad "the fresh pass routes a bead of each class" "the wake reason does not name the class: $(wake_of aramazd)"
elif grep -rq 'gqlc-unl' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "the fresh pass routes a bead of each class" "the unlabelled bead was routed to a seat"
elif grep -rq 'gqlc-taken' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "the fresh pass routes a bead of each class" "a bead another seat already holds was routed again"
else
    ok "the fresh pass routes architect, warrior and judge beads to free seats of their class, and routes neither the unlabelled bead nor one already assigned"
fi

# A queue of nothing but unlabelled beads is Սեդրակ's chore, not a failure. It
# is also the row that pins the corrected account of gqlc-z1qw: the type error
# fired on unassigned beads of ANY labelling, so under the bug this payload
# aborts jq rather than passing quietly.
dispatch_case '[
  {"id":"gqlc-u1","priority":0,"assignee":null,"labels":null},
  {"id":"gqlc-u2","priority":1,"assignee":null,"labels":[]},
  {"id":"gqlc-u3","priority":2,"assignee":null,"labels":["area:parser"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an all-unlabelled queue is quiet, not broken" "rc=$RC out=$OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "an all-unlabelled queue is quiet, not broken" "woke: $(woken_seats)"
elif ! printf '%s' "$OUT" | grep -q 'done'; then
    bad "an all-unlabelled queue is quiet, not broken" "the run did not report itself done: $OUT"
else
    ok "a queue of only unlabelled beads routes nobody and still completes, instead of aborting the query"
fi

# The resume pass: a seat's own in-progress bead comes back to it before any
# fresh bead is routed anywhere (Constitution III.3).
dispatch_case '[
  {"id":"gqlc-w2","priority":1,"assignee":null,"labels":["class:warrior"]}
]' '[
  {"id":"gqlc-r1","assignee":"vahagn","labels":["class:warrior"]}
]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the resume pass returns a seat its own work" "rc=$RC out=$OUT"
elif ! wake_of vahagn | grep -q 'resume your in-progress work: gqlc-r1'; then
    bad "the resume pass returns a seat its own work" "vahagn was not resumed: $(wake_of vahagn) (woken: $(woken_seats))"
elif ! wake_of aramazd | grep -q 'bead:gqlc-w2'; then
    bad "the resume pass returns a seat its own work" "the fresh warrior bead did not also route: $(woken_seats)"
elif wake_of vahagn | grep -q 'gqlc-w2'; then
    bad "the resume pass returns a seat its own work" "the resumed seat was also given fresh work"
else
    ok "the resume pass hands a seat back its own in-progress bead, and fresh work goes to a different free seat"
fi

# A bead assigned to a human is nobody's to route: the resume pass matches
# assignees against SEAT names, and the fresh pass excludes assigned beads. The
# labelled bead in the same payload is what proves the run was live.
dispatch_case '[
  {"id":"gqlc-w3","priority":1,"assignee":null,"labels":["class:warrior"]}
]' '[
  {"id":"gqlc-h1","assignee":"areqag","labels":["class:warrior"]}
]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a human's bead wakes no seat" "rc=$RC out=$OUT"
elif ! wake_of aramazd | grep -q 'bead:gqlc-w3'; then
    bad "a human's bead wakes no seat" "the control bead did not route, so this row proves nothing: $(woken_seats)"
elif grep -rq 'gqlc-h1' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "a human's bead wakes no seat" "the human's bead was routed to a seat"
else
    ok "an in-progress bead assigned to a human wakes no seat, while a seat-classed bead in the same run still routes"
fi

# Priority decides who gets the last seat. Every warrior but հայկ is given a
# queued wake, so there is exactly one free seat and the two beads are in real
# contention — without that there are eight free warriors, both beads route, and
# the ordering is unobservable.
dispatch_case '[
  {"id":"gqlc-lo","priority":3,"assignee":null,"labels":["class:warrior"]},
  {"id":"gqlc-hi","priority":0,"assignee":null,"labels":["class:warrior"]}
]' '[]'
for s in aramazd vahagn astghik ar nvard ayg tsovinar; do
    mkdir -p "$KM_STATE_DIR/seats/$s"
    echo "busy" >"$KM_STATE_DIR/seats/$s/wake"
done
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the last free seat goes to the higher-priority bead" "rc=$RC out=$OUT"
elif ! wake_of hayk | grep -q 'bead:'; then
    bad "the last free seat goes to the higher-priority bead" "the one free warrior was given nothing: $(woken_seats) out=$OUT"
elif ! wake_of hayk | grep -q 'bead:gqlc-hi'; then
    bad "the last free seat goes to the higher-priority bead" "it took the lower-priority bead: $(wake_of hayk)"
else
    ok "with one free seat and two ready beads, the higher-priority bead is the one routed"
fi

# Fail-closed, the half that made this invisible for the kingdom's whole life:
# a query that FAILS must not read as a queue with nothing in it.
dispatch_case 'not json at all' '[]'
run_dispatch
if [ "$RC" -eq 0 ]; then
    bad "a failed ready query refuses instead of reading as an empty queue" "exited 0: $OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "a failed ready query refuses instead of reading as an empty queue" "it woke: $(woken_seats)"
elif ! printf '%s' "$OUT" | grep -q 'ready'; then
    bad "a failed ready query refuses instead of reading as an empty queue" "the refusal does not name the ready queue: $OUT"
elif printf '%s' "$OUT" | grep -q '0 wake(s)'; then
    bad "a failed ready query refuses instead of reading as an empty queue" "it still reported a normal idle run: $OUT"
else
    ok "a ready query that fails makes the dispatcher refuse loudly and wake nobody, not report an idle run"
fi

dispatch_case '[]' '[]'
printf '1' >"$KM_FAKE_INPROG.rc"
run_dispatch
if [ "$RC" -eq 0 ]; then
    bad "a failed in-progress query refuses too" "exited 0: $OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "a failed in-progress query refuses too" "it woke: $(woken_seats)"
elif ! printf '%s' "$OUT" | grep -q 'in-progress'; then
    bad "a failed in-progress query refuses too" "the refusal does not name the in-progress query: $OUT"
else
    ok "an in-progress query that fails refuses before the fresh pass, so no seat is routed work another seat is holding"
fi

export KM_STATE_DIR="$TMP/state"

# --- sleep outside a seat is a no-op, not an error ---------------------------
# The /handoff skill ends with `km sleep`; Անդրանիկ's own sessions run it too.

run sleep
if [ "$RC" -ne 0 ] || ! printf '%s' "$OUT" | grep -q 'not a seat session'; then
    bad "sleep outside a seat degrades gracefully" "rc=$RC out=$OUT"
else
    ok "sleep outside a seat session explains itself and exits 0"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
