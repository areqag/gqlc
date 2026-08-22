#!/usr/bin/env bash
# Tests for kingdom/bin/km — the state machinery of the Թագաւորութիւն.
#
# Everything runs against a throwaway KM_STATE_DIR, so the real town's mail
# and seat state are never touched, and nothing here needs tmux, claude, or a
# running dispatcher. One row is the exception and asks the real bd, in a
# throwaway workspace of its own; it skips where bd is absent, and it says so.
# What is pinned is the machinery the society stands
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
#
# The stub MODELS bd's result cap, because the cap is itself a defect under
# test (gqlc-mlca): `bd ready --json` returns at most 100 and `bd list --json`
# at most 50, neither saying so, while `-n 0` means unlimited. A stub that
# always handed back the whole fixture would make a truncated query look
# exactly like a complete one, which is the property that hid this.
_all="$*"
case "$1" in
    ready) f="${KM_FAKE_READY:-}"; limit=100 ;;
    list)  f="${KM_FAKE_INPROG:-}"; limit=50
           case "$_all" in
               *"--status in_progress"*) ;;
               *) echo "bd stub: unexpected list query: $_all" >&2; exit 1 ;;
           esac ;;
    *) exit 0 ;;
esac
shift
while [ $# -gt 0 ]; do
    case "$1" in
        -n|--limit) limit="${2:-}"; shift ;;
    esac
    shift
done
[ -n "$f" ] || { echo "bd stub: no fixture named for: $_all" >&2; exit 1; }
if [ -f "$f.rc" ]; then
    # Defaulted, not inlined: `exit ""` is not fatal in bash, so an empty knob
    # would leave the stub succeeding while looking as though it had failed.
    _rc="$(cat "$f.rc")"
    echo "bd: error: database is locked by another process" >&2
    exit "${_rc:-1}"
fi
# 0 is unlimited. Any other limit truncates SILENTLY — no notice, no marker —
# which is exactly how the real renderer behaves and why km must ask for 0.
# The jq failure arm keeps malformed fixtures flowing through untouched, so
# the rows that redden the pipeline by feeding it non-JSON still can.
if [ "$limit" = 0 ] || ! jq -e . "$f" >/dev/null 2>&1; then
    cat "$f"
else
    jq -c ".[0:$limit]" "$f"
fi
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

# cmd_status cannot reach its seat table or its counter line in a fresh state
# dir without these: unread_count's `find` exits 1 on a missing directory and
# pipefail turns that into an abort mid-table (gqlc-6wqw, filed not fixed — a
# different defect that merely stands in front of the one under test here).
make_inboxes() {
    awk -F' *= *' '
        /^\[seats\]/ { s = 1; next } /^\[/ { s = 0 }
        s && NF >= 2 && $1 !~ /^#/ { print $1 }' "$REPO/kingdom/kingdom.toml" |
        while read -r s; do
            mkdir -p "$KM_STATE_DIR/mail/$s/inbox"
        done
}

wake_of()     { cat "$KM_STATE_DIR/seats/$1/wake" 2>/dev/null; }
woken_seats() { find "$KM_STATE_DIR/seats" -mindepth 2 -maxdepth 2 -name wake 2>/dev/null | sed 's|.*/seats/||; s|/wake$||' | sort | tr '\n' ' '; }

# The cap counts seats whose status file reads `awake`, which is what a running
# session leaves behind. A queued `wake` file (as the priority row above uses)
# is a different state and does NOT count, so the two cannot stand in for each
# other.
fill_cap() { local s; for s in "$@"; do mkdir -p "$KM_STATE_DIR/seats/$s"; echo awake >"$KM_STATE_DIR/seats/$s/status"; done; }

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

# --- the concurrency cap, and the judge's exemption from it (gqlc-dz85) ------
# Միհր is the town's sole merge gate. Counting him against max_active schedules
# him 1-in-12 against the eight warriors generating the very PRs he must clear,
# so work enters faster than it can leave and the backlog can only grow. Worse,
# the cap check is an early return that fires BEFORE the queue is sorted, so at
# a full cap a P0 judge bead is not outranked — it is never considered at all.
#
# The cap itself had no test of any kind before this section, so the rows below
# pin both halves: that a full cap does stop capped work, and that it does not
# stop the gate.

# The warrior bead is P0 and the judge bead P1, so the judge sorts SECOND: the
# row fails if the fix merely reaches the queue at a full cap and takes its
# head. The unrouted warrior bead is also the control proving the cap really is
# full — without it, a woken judge could just mean the cap never engaged.
dispatch_case '[
  {"id":"gqlc-wcap","priority":0,"assignee":null,"labels":["class:warrior"]},
  {"id":"gqlc-jcap","priority":1,"assignee":null,"labels":["class:judge"]}
]' '[]'
fill_cap aramazd vahagn astghik ar nvard
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a full cap does not close the merge gate" "rc=$RC out=$OUT"
elif grep -rq 'gqlc-wcap' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "a full cap does not close the merge gate" "the cap never engaged — the warrior bead routed, so this row proves nothing: $(woken_seats)"
elif ! wake_of mihr | grep -q 'bead:gqlc-jcap'; then
    bad "a full cap does not close the merge gate" "the judge was not woken for ready judge work at a full cap (woken: $(woken_seats)) out=$OUT"
else
    ok "with every capped slot held, a ready judge bead still wakes the judge while a higher-priority warrior bead is correctly held back"
fi

# The over-correction this must not become: exempting the judge from the cap is
# not licence to wake somebody on every full-cap run.
dispatch_case '[
  {"id":"gqlc-wonly","priority":0,"assignee":null,"labels":["class:warrior"]}
]' '[]'
fill_cap aramazd vahagn astghik ar nvard
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a full cap with no judge work wakes nobody" "rc=$RC out=$OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "a full cap with no judge work wakes nobody" "it woke: $(woken_seats) out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'no free slot'; then
    bad "a full cap with no judge work wakes nobody" "the run does not say the cap held it: $OUT"
else
    ok "a full cap with no ready judge bead wakes nobody, so the exemption is for judge work and not for every run"
fi

# The second half of the exemption: a judge who IS awake must not consume one
# of the five slots the warriors and architects share. Four warriors plus the
# judge reads as five awake seats to the defect and four to the fix, and the
# difference is a warrior bead that either routes or does not.
dispatch_case '[
  {"id":"gqlc-wfree","priority":0,"assignee":null,"labels":["class:warrior"]}
]' '[]'
fill_cap aramazd vahagn astghik ar mihr
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an awake judge does not spend a capped slot" "rc=$RC out=$OUT"
elif ! wake_of nvard | grep -q 'bead:gqlc-wfree'; then
    bad "an awake judge does not spend a capped slot" "the free warrior slot went unused (woken: $(woken_seats)) out=$OUT"
elif printf '%s' "$OUT" | grep -q 'no free slot'; then
    bad "an awake judge does not spend a capped slot" "the run counted the judge and declared itself full: $OUT"
else
    ok "an awake judge is not counted against max_active, so the fifth slot is still spent on warrior work"
fi

# The same exemption as the operator reads it. Six seats are awake here and the
# line must say five, because the number is what a human sizes the town by —
# and gqlc-z1qw, gqlc-bn5r and gqlc-ed2u were each a healthy-looking indicator
# vouching for machinery that was not doing what the number implied.
dispatch_case '[]' '[]'
fill_cap aramazd vahagn astghik ar nvard mihr
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the printed cap count excludes the judge" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q '5/5'; then
    bad "the printed cap count excludes the judge" "six awake seats, one of them the judge, did not print as 5/5: $OUT"
else
    ok "with the judge awake alongside a full bench, the cap line counts the five capped seats and not the six awake ones"
fi

# The other number the operator reads. Deriving the total from slot arithmetic
# (`max - active - slots`) held only while every wake spent a slot; the judge's
# exemption breaks that, and at a full cap the expression floors to zero — so
# the run that reaches the merge gate is exactly the run that reports having
# done nothing, and the town reads as idle at the moment it is not.
dispatch_case '[
  {"id":"gqlc-jcount","priority":0,"assignee":null,"labels":["class:judge"]}
]' '[]'
fill_cap aramazd vahagn astghik ar nvard
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the wake count counts the judge's wake" "rc=$RC out=$OUT"
elif ! wake_of mihr | grep -q 'bead:gqlc-jcount'; then
    bad "the wake count counts the judge's wake" "the judge was not woken, so the count pins nothing (woken: $(woken_seats)) out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'done (1 wake(s)'; then
    bad "the wake count counts the judge's wake" "one seat was woken and the run did not say so: $OUT"
else
    ok "a judge woken at a full cap is counted in the run's wake total, which slot arithmetic alone would have reported as zero"
fi

# Exemption means the judge's wake costs the town nothing, not merely that he
# is reached. With one slot free and the judge bead sorting first, a judge wake
# that decrements `slots` would spend the warriors' last slot on a seat that
# was never counted for one — invisible at a full cap, where the counter is
# already floored, and a stolen slot everywhere else.
dispatch_case '[
  {"id":"gqlc-jslot","priority":0,"assignee":null,"labels":["class:judge"]},
  {"id":"gqlc-wslot","priority":1,"assignee":null,"labels":["class:warrior"]}
]' '[]'
fill_cap aramazd vahagn astghik ar
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a judge wake does not spend the last free slot" "rc=$RC out=$OUT"
elif ! wake_of mihr | grep -q 'bead:gqlc-jslot'; then
    bad "a judge wake does not spend the last free slot" "the judge was not woken at all (woken: $(woken_seats)) out=$OUT"
elif ! wake_of nvard | grep -q 'bead:gqlc-wslot'; then
    bad "a judge wake does not spend the last free slot" "the judge's wake consumed the one free warrior slot (woken: $(woken_seats)) out=$OUT"
else
    ok "with one slot free, routing a judge bead ahead of a warrior bead still leaves that slot for the warrior"
fi

# The same property on the other pass: resuming the judge must not spend a slot
# either.
dispatch_case '[
  {"id":"gqlc-wslot2","priority":1,"assignee":null,"labels":["class:warrior"]}
]' '[
  {"id":"gqlc-jslot2","assignee":"mihr","labels":["class:judge"]}
]'
fill_cap aramazd vahagn astghik ar
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a judge resume does not spend the last free slot" "rc=$RC out=$OUT"
elif ! wake_of mihr | grep -q 'gqlc-jslot2'; then
    bad "a judge resume does not spend the last free slot" "the judge was not resumed at all (woken: $(woken_seats)) out=$OUT"
elif ! wake_of nvard | grep -q 'bead:gqlc-wslot2'; then
    bad "a judge resume does not spend the last free slot" "the judge's resume consumed the one free warrior slot (woken: $(woken_seats)) out=$OUT"
else
    ok "resuming the judge's own in-progress bead leaves the free slot for warrior work"
fi

# A review half-done is the same gate as a review not started: if the resume
# pass is behind the cap check, a judge who claimed a bead and slept never gets
# it back while the town is busy.
dispatch_case '[]' '[
  {"id":"gqlc-jres","assignee":"mihr","labels":["class:judge"]}
]'
fill_cap aramazd vahagn astghik ar nvard
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a full cap does not strand a half-done review" "rc=$RC out=$OUT"
elif ! wake_of mihr | grep -q 'resume your in-progress work: gqlc-jres'; then
    bad "a full cap does not strand a half-done review" "the judge was not handed his own bead back (woken: $(woken_seats)) out=$OUT"
else
    ok "a judge holding an in-progress bead is resumed at a full cap, not stranded behind it"
fi

# Exempt from the cap is not exempt from being busy. Waking a judge who is
# already mid-review would interrupt him and, with more than one judge seat,
# would be the thing that actually uncaps the town.
dispatch_case '[
  {"id":"gqlc-jbusy","priority":0,"assignee":null,"labels":["class:judge"]}
]' '[]'
fill_cap aramazd vahagn astghik ar nvard mihr
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an awake judge is not woken again" "rc=$RC out=$OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "an awake judge is not woken again" "it woke: $(woken_seats) out=$OUT"
else
    ok "a judge who is already awake is not woken again for ready judge work"
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

# The refusal above claims the resume wakes still stand. That claim is only
# worth printing if it is true, so it is pinned: resume succeeds, the fresh
# query then fails, and the seat that was handed its own work keeps it.
dispatch_case 'not json at all' '[
  {"id":"gqlc-r2","assignee":"vahagn","labels":["class:warrior"]}
]'
run_dispatch
if [ "$RC" -eq 0 ]; then
    bad "a resume wake survives a later fresh-query failure" "exited 0: $OUT"
elif ! wake_of vahagn | grep -q 'gqlc-r2'; then
    bad "a resume wake survives a later fresh-query failure" "the resume wake was lost: $(woken_seats)"
elif ! printf '%s' "$OUT" | grep -q 'resume wakes above stand'; then
    bad "a resume wake survives a later fresh-query failure" "the refusal does not say the resume wakes stand: $OUT"
else
    ok "when the fresh query fails after the resume pass, the resume wakes stand and the refusal says so"
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

# --- the silent result cap (gqlc-mlca) ---------------------------------------
# bd's JSON renderers truncate and say nothing: `ready` at 100, `list` at 50.
# The plain renderers do disclose it ("Showing 100 of 234 ready issues"), so the
# divergence is invisible to exactly the caller that cannot read prose. A bead
# past the window is not a bead nobody claimed — it is a bead nobody was shown,
# and the two are indistinguishable from the board.

# Padding is unlabelled and unassigned, so it routes to nobody and occupies no
# seat: the only bead that CAN route is the far one, which makes a silent
# "0 wake(s)" the whole signal.
dispatch_case "$(jq -cn '[range(100) | {id: "gqlc-pad\(.)", priority: 1, assignee: null, labels: ["area:pad"]}]
                       + [{id: "gqlc-far", priority: 2, assignee: null, labels: ["class:warrior"]}]')" '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the fresh pass sees a routable bead past the default window" "rc=$RC out=$OUT"
elif ! grep -rq 'gqlc-far' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "the fresh pass sees a routable bead past the default window" \
        "the bead at position 101 reached no seat (woken: '$(woken_seats)') out=$OUT"
else
    ok "a routable bead sorting past position 100 is still routed, instead of vanishing behind bd ready's silent cap"
fi

# The same cap on the dispatcher's other query, one window earlier. A seat whose
# own in-progress bead sorts past 50 is handed nothing and reads as idle — and
# the fresh pass may then route it work while it already holds some.
dispatch_case '[]' "$(jq -cn '[range(50) | {id: "gqlc-ip\(.)", assignee: "nobody\(.)", labels: []}]
                            + [{id: "gqlc-rfar", assignee: "vahagn", labels: ["class:warrior"]}]')"
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the resume pass sees an in-progress bead past the default window" "rc=$RC out=$OUT"
elif ! wake_of vahagn | grep -q 'gqlc-rfar'; then
    bad "the resume pass sees an in-progress bead past the default window" \
        "vahagn was not handed his own bead back (woken: '$(woken_seats)') out=$OUT"
else
    ok "a seat's in-progress bead sorting past position 50 is still resumed, instead of vanishing behind bd list's silent cap"
fi

# The board reads the same truncated queue, so every class counter is silently a
# floor. Սեդրակ sizes his standing labelling chore off this line, which is what
# makes a precise-looking wrong number worse here than no number at all.
dispatch_case "$(jq -cn '[range(100) | {id: "gqlc-spad\(.)", priority: 1, assignee: null, labels: ["area:pad"]}]
                       + [range(5) | {id: "gqlc-sw\(.)", priority: 2, assignee: null, labels: ["class:warrior"]}]')" '[]'
make_inboxes
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
RC=$?
if [ "$RC" -ne 0 ]; then
    bad "the board counts the whole ready queue" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q '5 warrior'; then
    bad "the board counts the whole ready queue" \
        "the counter missed the 5 warrior beads past the window: $(printf '%s' "$OUT" | grep 'ready queue:')"
else
    ok "km status counts the whole ready queue, so its class counters are not silent floors"
fi

# The board's fourth query, and the one with no other row over it: the BEADS
# column reads the same capped `bd list`. A seat shown holding nothing while it
# holds a bead is how a stalled seat gets read as an idle one.
dispatch_case '[]' "$(jq -cn '[range(50) | {id: "gqlc-sip\(.)", assignee: "nobody\(.)", labels: []}]
                            + [{id: "gqlc-sfar", assignee: "vahagn", labels: ["class:warrior"]}]')"
make_inboxes
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
RC=$?
if [ "$RC" -ne 0 ]; then
    bad "the board shows a seat's in-progress bead past the default window" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -E '^vahagn' | grep -q 'gqlc-sfar'; then
    bad "the board shows a seat's in-progress bead past the default window" \
        "vahagn's row does not name the bead he holds: $(printf '%s' "$OUT" | grep -E '^vahagn')"
else
    ok "km status shows a seat's in-progress bead even when it sorts past bd list's silent cap"
fi

export KM_STATE_DIR="$TMP/state"

# --- the contract with the real bd (gqlc-mlca) -------------------------------
# Every row above pins km against a MODEL of bd, and the model is where `-n 0`
# gets its meaning. So if bd stopped honouring it — a flag rename, a cap
# reintroduced at another number, `0` reread as "none" — those rows would all
# stay green while the dispatcher went blind again in precisely the shape of the
# defect they exist to close. This row is the one place the real binary is asked.
# CI installs no bd, so it skips there, out loud.
#
# Both halves are deliberate. `-n 0` returning all 105 is the contract; the
# default stopping at 100 is what proves the fixture straddles the cap, and
# without it "bd honoured -n 0" and "bd never capped" are the same green — this
# bead's own defect one level up. Should bd drop the cap, this goes red to
# report that the premise changed, rather than passing on a technicality.
bd_contract="the real bd returns the whole ready queue for -n 0, past the cap it applies by default"
if ! command -v bd >/dev/null 2>&1; then
    printf 'skip - %s: no bd on PATH\n' "$bd_contract"
else
    ws="$TMP/bd-contract"
    mkdir -p "$ws"
    # Ours, so it is bound once; the 100 below is bd's and stays a literal.
    fixture_n=105
    jq -nc --argjson n "$fixture_n" 'range($n) | {
        _type: "issue", id: "cap-\(.)", title: "cap fixture \(.)",
        status: "open", priority: 3, issue_type: "task"
    }' >"$ws/fixture.jsonl"
    # bd resolves its workspace through GIT_DIR/GIT_WORK_TREE, which git exports
    # to every hook, so under `git push` this row reached the SHARED repo's
    # workspace and aborted — it failed from a hook and only from a hook. The
    # unset is what makes it run there. It is not what keeps the fixture out of
    # the real ledger: measured, bd refuses when it finds an existing workspace
    # and initialises in the cwd when it finds none. The .beads check below
    # guards a case today's bd does not produce, and is kept only because what
    # it would cost is 105 fixture issues in the town's tracker.
    bd_in_ws() { (unset "${!GIT_@}"; cd "$ws" && bd "$@"); }
    if ! bd_in_ws init >"$ws/setup.log" 2>&1; then
        bad "$bd_contract" "no throwaway bd workspace: $(tail -1 "$ws/setup.log")"
    elif [ ! -d "$ws/.beads" ]; then
        bad "$bd_contract" "bd init resolved outside $ws; refusing to import a fixture into a ledger that may be real"
    elif ! bd_in_ws import fixture.jsonl >>"$ws/setup.log" 2>&1; then
        bad "$bd_contract" "the fixture would not import: $(tail -1 "$ws/setup.log")"
    else
        capped="$(bd_in_ws ready --json 2>/dev/null | jq -r 'length')"
        whole="$(bd_in_ws ready -n 0 --json 2>/dev/null | jq -r 'length')"
        if [ "$capped" != 100 ]; then
            bad "$bd_contract" \
                "the default query returned $capped, so $fixture_n issues no longer straddle bd's cap — re-argue whether -n 0 is still load-bearing"
        elif [ "$whole" != "$fixture_n" ]; then
            bad "$bd_contract" "-n 0 returned $whole of the $fixture_n ready issues"
        else
            ok "$bd_contract"
        fi
    fi
fi

# --- sleep outside a seat is a no-op, not an error ---------------------------
# The /handoff skill ends with `km sleep`; Անդրանիկ's own sessions run it too.

run sleep
if [ "$RC" -ne 0 ] || ! printf '%s' "$OUT" | grep -q 'not a seat session'; then
    bad "sleep outside a seat degrades gracefully" "rc=$RC out=$OUT"
else
    ok "sleep outside a seat session explains itself and exits 0"
fi

# --- per-seat effort level ---------------------------------------------------
# gqlc-w6tl. Every seat used to inherit "effortLevel": "xhigh" from the
# operator's personal ~/.claude/settings.json, because km-seat passed no
# --effort at all. The level is now per class in [effort].
#
# Two hermetic seams carry this block. KM_STATE_DIR is the suite's own, above.
# KM_CONFIG is the second: the rows below must run against a kingdom.toml with
# the [effort] section REMOVED, and the only alternative to an override is
# mutating the real town's config while the dispatcher reads it.

alt_config() { # alt_config <path> [effort-section-lines...] -> a throwaway toml
    local dest=$1
    shift
    {
        printf '[kingdom]\ntmux_session = "kingdom-test"\nstate_dirname = "kingdom-state"\n\n'
        printf '[claude]\npermission_mode = "acceptEdits"\n\n'
        printf '[seats]\nhayk = "warrior:claude-opus-5:Հայկ"\n'
        # One key per line: km's reader is line-oriented, so "$*" would fold a
        # whole section into a single unparseable line.
        [ "$#" -eq 0 ] || { printf '\n[effort]\n'; printf '%s\n' "$@"; }
    } >"$dest"
}

alt_config "$TMP/noeffort.toml"
OUT=$(KM_CONFIG="$TMP/noeffort.toml" "$KM" cfg kingdom tmux_session 2>&1)
RC=$?
# The real config says "kingdom"; reading "kingdom-test" back proves km took
# the override rather than its own default, so the rows below mean something.
if [ "$RC" -ne 0 ] || [ "$OUT" != kingdom-test ]; then
    bad "KM_CONFIG overrides the config path" "rc=$RC out=$OUT"
else
    ok "KM_CONFIG overrides which config km reads"
fi

# Per class, never an aggregate: a row that asserted "some class resolved"
# stays green while four of five silently return empty.
#
# Deliberately NOT the town's own numbers, which belong to whoever tunes
# kingdom.toml, not to this suite. Every level here differs from the one that
# class really carries, so a resolver that ignored KM_CONFIG would fail rather
# than pass on the live file; and all five differ from each other, so a
# resolver that returned some other class's level fails too.
alt_config "$TMP/fiveclass.toml" \
    'mayor     = "low"' \
    'architect = "xhigh"' \
    'warrior   = "max"' \
    'judge     = "medium"' \
    'guard     = "high"'
while read -r class want; do
    OUT=$(KM_CONFIG="$TMP/fiveclass.toml" "$KM" cfg effort "$class" 2>&1)
    RC=$?
    if [ "$RC" -ne 0 ] || [ "$OUT" != "$want" ]; then
        bad "cfg effort $class" "rc=$RC out=$OUT want=$want"
    else
        ok "cfg effort $class is $want"
    fi
done <<'CLASSES'
mayor low
architect xhigh
warrior max
judge medium
guard high
CLASSES

OUT=$(KM_CONFIG="$TMP/noeffort.toml" "$KM" cfg effort warrior 2>&1)
RC=$?
if [ "$RC" -ne 0 ] || [ -n "$OUT" ]; then
    bad "an absent [effort] section resolves to nothing, quietly" "rc=$RC out=$OUT"
else
    ok "an absent [effort] section resolves to empty and exits 0"
fi

# Against the POPULATED section, so this proves the lookup missed rather than
# that the section was empty all along.
OUT=$(KM_CONFIG="$TMP/fiveclass.toml" "$KM" cfg effort dragonslayer 2>&1)
RC=$?
if [ "$RC" -ne 0 ] || [ -n "$OUT" ]; then
    bad "an unknown class resolves to nothing, quietly" "rc=$RC out=$OUT"
else
    ok "an unknown class resolves to empty and exits 0"
fi

# --- the composed command line -----------------------------------------------
# The bead's own warning: the only witness is a seat that actually launches,
# and this town has shipped "correct" changes that routed nothing. So these
# rows read the argv km-seat REALLY composed, via a claude stub on PATH, and
# assert on the presence and absence of the flag rather than on a seat that
# merely starts.

KM_SEAT="$REPO/kingdom/bin/km-seat"
stubdir="$TMP/stub"
mkdir -p "$stubdir"
cat >"$stubdir/claude" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$@" >"$KM_TEST_ARGV"
STUB
chmod +x "$stubdir/claude"

# km resolves the town from `git rev-parse` against the CALLER's cwd (km:78-82)
# and km-seat then demands a seat worktree beside it. Run from the real repo,
# these rows read the author's own `../<repo>-seat-hayk` — which exists on the
# machine they were written on and on no CI runner, where all four failed with
# "no worktree at /home/runner/work/gqlc/gqlc-seat-hayk". So the rows get a
# throwaway town of their own and depend on nothing outside $TMP.
TOWN="$TMP/town"
mkdir -p "$TOWN"
TOWN=$(cd "$TOWN" && pwd -P) # git reports the physical path; mktemp may hand back a symlink
git init -q "$TOWN"
mkdir -p "$TOWN-seat-hayk"

ARGV=""
STDERR=""
compose_argv() { # compose_argv <config> -> ARGV (one arg per line), STDERR
    local cfgfile=$1 sdir pid waited=0
    ARGV="$TMP/argv.$RANDOM"
    STDERR="$TMP/stderr.$RANDOM"
    sdir="$TMP/seatstate.$RANDOM"
    mkdir -p "$sdir/seats/hayk"
    echo "a test wake" >"$sdir/seats/hayk/wake"
    (cd "$TOWN" && PATH="$stubdir:$PATH" KM_CONFIG="$cfgfile" KM_STATE_DIR="$sdir" \
        KM_TEST_ARGV="$ARGV" "$KM_SEAT" hayk) >"$STDERR" 2>&1 &
    pid=$!
    # km-seat parks again after the stub exits, so it never runs away; we stop
    # waiting as soon as the argv lands, or give up and report what we have.
    while [ ! -s "$ARGV" ] && [ "$waited" -lt 100 ] && kill -0 "$pid" 2>/dev/null; do
        sleep 0.1
        waited=$((waited + 1))
    done
    kill "$pid" 2>/dev/null
    wait "$pid" 2>/dev/null
}

alt_config "$TMP/warrior-high.toml" 'warrior = "high"'
compose_argv "$TMP/warrior-high.toml"
if [ ! -s "$ARGV" ]; then
    bad "km-seat launches claude at all" "no argv recorded; log: $(cat "$STDERR" 2>/dev/null)"
elif ! grep -qx -- '--effort' "$ARGV"; then
    bad "km-seat passes --effort" "argv: $(tr '\n' ' ' <"$ARGV")"
elif [ "$(grep -A1 -x -- '--effort' "$ARGV" | tail -1)" != high ]; then
    bad "km-seat passes the class's level" "argv: $(tr '\n' ' ' <"$ARGV")"
else
    ok "km-seat passes --effort high for a warrior seat when [effort] says high"
fi

# The level must come from the seat's CLASS, not from a fixed string: flip the
# config and the same seat must launch differently.
alt_config "$TMP/warrior-low.toml" 'warrior = "low"'
compose_argv "$TMP/warrior-low.toml"
if [ "$(grep -A1 -x -- '--effort' "$ARGV" 2>/dev/null | tail -1)" != low ]; then
    bad "the level tracks the config, not a hard-coded default" "argv: $(tr '\n' ' ' <"$ARGV" 2>/dev/null)"
else
    ok "the level tracks the config, not a hard-coded default"
fi

# Absence, asserted as absence. Today's behaviour is to inherit the global
# default, and a config that has lost its [effort] section must not change how
# a seat launches -- still less fail to launch it.
compose_argv "$TMP/noeffort.toml"
if [ ! -s "$ARGV" ]; then
    bad "a seat with no [effort] section still launches" "no argv; log: $(cat "$STDERR" 2>/dev/null)"
elif grep -qx -- '--effort' "$ARGV"; then
    bad "no [effort] section must omit the flag entirely" "argv: $(tr '\n' ' ' <"$ARGV")"
else
    ok "no [effort] section omits --effort and the seat launches anyway"
fi

# A typo in the config must not take the town down. This is the shape the bead
# warns about, aimed where the typo actually happens: a misspelled LEVEL takes
# out every seat of that class, and `claude --effort mediun` would refuse to
# start. Omit the flag, say so on stderr, and let the seat work.
alt_config "$TMP/badlevel.toml" 'warrior = "mediun"'
compose_argv "$TMP/badlevel.toml"
if [ ! -s "$ARGV" ]; then
    bad "a misspelled level still launches the seat" "no argv; log: $(cat "$STDERR" 2>/dev/null)"
elif grep -qx -- '--effort' "$ARGV"; then
    bad "a misspelled level must not reach claude" "argv: $(tr '\n' ' ' <"$ARGV")"
elif ! grep -q 'mediun' "$STDERR"; then
    bad "a misspelled level must be named on stderr" "log: $(cat "$STDERR" 2>/dev/null)"
else
    ok "a misspelled level is refused, named on stderr, and the seat still launches"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
