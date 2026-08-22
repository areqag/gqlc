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
# Nor may the invoking git's environment. `git push` runs this suite from a hook
# with GIT_DIR set to the real repo, and git honours GIT_DIR over BOTH the CWD
# and an explicit `-C <dir>` — so the fixture below is built somewhere else and
# every git call in it, including `config`, lands on the repo under test. Most
# rows would still pass, because the paths they ask about exist there too; only
# the fetch row caught it. Passing for the wrong reason is the exact defect the
# fixture exists to end, so the environment is cleared here rather than worked
# around per row. The glob form, and not a list: it takes GIT_CONFIG_GLOBAL and
# whatever git adds next, which a list written today does not. (Measured on
# PR #1128 — green direct, red under push; postmortem PR #1160, bd gqlc-o13d.)
unset "${!GIT_@}"

# The same scrub per call, because the decoy row re-exports GIT_DIR on purpose.
# Everything here talks to local paths, so it can drop the whole GIT_* namespace
# rather than a list someone has to keep complete; km's git_at cannot, and says
# why there.
gitf() {
    local -a scrub=()
    local v
    for v in "${!GIT_@}"; do scrub+=(-u "$v"); done
    env "${scrub[@]}" git "$@"
}

# Read after the unset, so it is the real repo's HEAD and not a hijacked one.
# Asserted again at the end of the file; see the note there.
SUITE_START_HEAD="$(git -C "$REPO" rev-parse HEAD 2>/dev/null || echo none)"

# A hermetic git fixture, because km resolves the repo from its CWD and the hold
# rows ask real questions of `origin/master`. Run against the checkout the suite
# happens to sit in, those rows answer differently per environment: a GitHub
# Actions checkout has NO origin/master ref, so every path reads absent and each
# "present path" row passes for the wrong reason — green locally, ten red rows in
# CI (measured on PR #1128). The remote here is a real bare repo on disk, so the
# fetch path is exercised for what it is rather than stubbed out, and no row
# touches the network.
FIXTURE_ORIGIN="$TMP/origin.git"
FIXTURE="$TMP/work"
git init -q --bare -b master "$FIXTURE_ORIGIN"
git init -q -b master "$FIXTURE"
git -C "$FIXTURE" config user.email fixture@example.invalid
git -C "$FIXTURE" config user.name fixture
git -C "$FIXTURE" config commit.gpgsign false
# The operator's global config may point core.hooksPath at this very repo; the
# fixture's own commits must not run the hooks under test.
git -C "$FIXTURE" config core.hooksPath /dev/null
mkdir -p "$FIXTURE/kingdom/bin"
printf 'fixture\n' >"$FIXTURE/justfile"
printf 'fixture\n' >"$FIXTURE/kingdom/bin/km"
git -C "$FIXTURE" add -A
git -C "$FIXTURE" commit -qm 'fixture: paths the hold rows ask about'
git -C "$FIXTURE" remote add origin "$FIXTURE_ORIGIN"
git -C "$FIXTURE" push -q origin master
git -C "$FIXTURE" fetch -q origin master
git -C "$FIXTURE" update-ref refs/remotes/origin/master FETCH_HEAD


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

# The unset above only scrubs what THIS shell inherited; a `git` added to the
# file later runs with whatever the caller exported, which is exactly how this
# suite came to commit into the repo under test. So the rule is that every git
# invocation here goes through gitf, and the rule gets a check — one without the
# other is a wish. Comments are stripped first: this file discusses git a lot.
stray=$(sed 's/#.*//' "$0" | grep -cE '(^|[;&|(])[[:space:]]*git[[:space:]]')
if [ "$stray" -ne 0 ]; then
    bad "every git call in this suite is scrubbed" "$stray bare git invocation(s) bypass gitf"
else
    ok "every git call in this suite goes through gitf rather than bare git"
fi

# --- the deploy seam ---------------------------------------------------------
# The town executes kingdom/ out of the main checkout, and km refuses to act on
# the town when that tree differs from origin/master. The real main checkout is
# routinely drifted — that is the bug this seam exists for (bd gqlc-ed2u) — so
# the whole suite stands on a throwaway origin+clone pair, exactly as it stands
# on a throwaway KM_STATE_DIR.

mk_origin() { # <bare> : a bare repo whose master carries a kingdom/ tree
    gitf init -q --bare "$1"
    gitf init -q "$1.seed"
    gitf -C "$1.seed" config user.email km@test
    gitf -C "$1.seed" config user.name km-test
    mkdir -p "$1.seed/kingdom/bin" "$1.seed/.beads"
    printf 'deployed\n' >"$1.seed/kingdom/bin/km"
    printf 'export-v1\n' >"$1.seed/.beads/issues.jsonl"
    printf 'inter-v1\n' >"$1.seed/.beads/interactions.jsonl"
    gitf -C "$1.seed" add -A
    gitf -C "$1.seed" commit -qm "the deployed tree"
    gitf -C "$1.seed" push -q "$1" HEAD:master
}

advance_origin() { # <bare> <path> <content> : one more commit on master
    printf '%s\n' "$3" >"$1.seed/$2"
    gitf -C "$1.seed" add -A
    gitf -C "$1.seed" commit -qm "advance $2"
    gitf -C "$1.seed" push -q "$1" HEAD:master
}

mk_clone() { # <bare> <dir>
    gitf clone -q "$1" "$2"
    gitf -C "$2" config user.email km@test
    gitf -C "$2" config user.name km-test
}

mk_origin "$TMP/town.git"
mk_clone "$TMP/town.git" "$TMP/deployed"
export KM_DEPLOY_ROOT="$TMP/deployed"

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
    # The hold verdict's two inputs (gqlc-pj4r). Neither is capped by bd, so
    # neither models a limit. `dep list` is pinned to the MULTI-id form on
    # purpose: the single-id form returns the parent issue object instead of
    # {issue_id, depends_on_id, type} rows, so a caller that drifted to
    # one-id-per-call would parse a different shape and silently find no edges.
    dep)   [ "${2:-}" = list ] || { echo "bd stub: unexpected dep query: $_all" >&2; exit 1; }
           f="${KM_FAKE_DEPS:-}"; limit=0
           case "$_all" in
               *"gqlc-"*" gqlc-"*|*"gqlc-"*) ;;
               *) echo "bd stub: dep list with no ids: $_all" >&2; exit 1 ;;
           esac ;;
    show)  f="${KM_FAKE_PARENTS:-}"; limit=0 ;;
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
dispatch_case() { # $1=ready, $2=in-progress, $3=dep edges, $4=parent statuses
    DCASE=$((DCASE + 1))
    export KM_STATE_DIR="$TMP/dispatch-$DCASE"
    mkdir -p "$KM_STATE_DIR"
    export KM_FAKE_READY="$KM_STATE_DIR/ready.json"
    export KM_FAKE_INPROG="$KM_STATE_DIR/inprog.json"
    export KM_FAKE_DEPS="$KM_STATE_DIR/deps.json"
    export KM_FAKE_PARENTS="$KM_STATE_DIR/parents.json"
    printf '%s' "$1" >"$KM_FAKE_READY"
    printf '%s' "$2" >"$KM_FAKE_INPROG"
    # An empty edge set is the shape almost every bead has, so it is the default
    # rather than something each case restates.
    printf '%s' "${3:-[]}" >"$KM_FAKE_DEPS"
    printf '%s' "${4:-[]}" >"$KM_FAKE_PARENTS"
}

run_dispatch() {
    OUT="$(cd "$FIXTURE" && PATH="$BIN:$PATH" "$KM" dispatch 2>&1)"
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
# The bench is the town's merge gate. Counting judges against max_active
# schedules them against the eight warriors generating the very PRs they must
# clear, so work enters faster than it can leave and the backlog can only
# grow — 1-in-12 when the roster held one judge. Worse,
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
# already mid-review would interrupt him, so with every judge seat awake the
# ready review waits rather than landing on an occupied bench.
dispatch_case '[
  {"id":"gqlc-jbusy","priority":0,"assignee":null,"labels":["class:judge"]}
]' '[]'
fill_cap aramazd vahagn astghik ar nvard mihr anahit
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an awake judge is not woken again" "rc=$RC out=$OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "an awake judge is not woken again" "it woke: $(woken_seats) out=$OUT"
else
    ok "a judge who is already awake is not woken again for ready judge work"
fi

# The whole point of a second judge: one busy judge no longer stalls the
# review queue at a full cap. Guards the roster, not just the routing — with
# `anahit` dropped from [seats] this wakes nobody and the bench serialises
# again.
dispatch_case '[
  {"id":"gqlc-jfree","priority":0,"assignee":null,"labels":["class:judge"]}
]' '[]'
fill_cap aramazd vahagn astghik ar nvard mihr
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a busy judge does not stall the review queue" "rc=$RC out=$OUT"
elif ! wake_of anahit | grep -q 'gqlc-jfree'; then
    bad "a busy judge does not stall the review queue" "expected anahit, woke: $(woken_seats) out=$OUT"
elif woken_seats | grep -qw mihr; then
    bad "a busy judge does not stall the review queue" "it also woke the busy judge: $(woken_seats) out=$OUT"
else
    ok "with one judge awake and one free, ready review work routes to the free judge at a full cap"
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

# --- km hold-verdict: the mechanical routing hold (gqlc-pj4r) ----------------
# Review residue — a bead filed against code that exists only on an unmerged PR
# branch — reads `ready`, and a warrior routed to it would branch from master and
# find nothing to fix. The only guard was Սեդրակ declining to class-label such
# beads from a list in his handoff: a person remembering. These rows pin the
# mechanical replacement, whose release condition is the PR merging rather than
# anyone's recall.
#
# Every row below is paired with a falsifier differing in exactly ONE input. A
# row that cannot fail witnesses nothing, and the hold direction is especially
# prone to that: HOLD is the answer this command gives when it knows nothing, so
# a broken build holds everything and every hold assertion passes.
#
# gh is stubbed; git is real but read-only (`cat-file` against the shared repo's
# origin/master) and the fetch is skipped, so nothing here touches the network.

cat >"$BIN/gh" <<'STUB'
#!/usr/bin/env bash
# Answers `gh pr list --state open --limit N --json <fields>` from a fixture
# holding gh's shape: [{"number":N,"changedFiles":M,"files":[{"path":"..."}]}].
case "$*" in
    *"pr list"*"--json"*) ;;
    *) echo "gh stub: unexpected query: $*" >&2; exit 1 ;;
esac
if [ -n "${KM_FAKE_GH_RC:-}" ]; then
    echo "gh: could not connect to github.com" >&2
    exit "$KM_FAKE_GH_RC"
fi
fields="" limit="" prev=""
for a in "$@"; do
    [ "$prev" = "--json" ] && fields="$a"
    [ "$prev" = "--limit" ] && limit="$a"
    prev="$a"
done
[ -n "$fields" ] || { echo "gh stub: --json with no field list: $*" >&2; exit 1; }
raw=$(cat "${KM_FAKE_GH:-/dev/null}")
[ -n "$raw" ] || raw='[]'
# gh returns ONLY the fields asked for. The stub must project the same way, or a
# fixture keeps supplying a field the caller stopped requesting and that field's
# guard goes untested — the truncation tell reads null and fails open silently.
#
# --limit is honoured for the same reason: real gh returns at most N rows and
# says nothing about what it dropped, so a stub that hands back all of them
# makes the list-level cap unreachable from any fixture.
printf '%s' "$raw" | jq -c --arg f "$fields" --argjson n "${limit:-0}" \
    '($f | split(",")) as $keep
     | [ .[] | with_entries(select(.key | IN($keep[]))) ]
     | if $n > 0 then .[0:$n] else . end'
STUB
chmod +x "$BIN/gh"

HVGH="$TMP/hv-gh.json"
GH_RC=""
ERR=""
gh_prs() { printf '%s' "$1" >"$HVGH"; }
hv() { # $1 = candidate docs on stdin
    printf '%s' "$1" | ( cd "$FIXTURE" && PATH="$BIN:$PATH" KM_HOLD_SKIP_FETCH=1 \
        KM_FAKE_GH="$HVGH" KM_FAKE_GH_RC="$GH_RC" "$KM" hold-verdict ) \
        >"$TMP/hv.out" 2>"$TMP/hv.err"
    RC=$?
    OUT="$(cat "$TMP/hv.out")"
    ERR="$(cat "$TMP/hv.err")"
}

# Rows 1 and 2 — the pair that gives condition (2) its meaning. Only the PR map
# varies between them; the candidate is byte-identical. Without row 1, "HOLD"
# could be this command's answer to everything.
gh_prs '[]'
hv '[{"id":"gqlc-r1","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a subject present on master with no open PR routes" "rc=$RC out=$OUT err=$ERR"
elif [ "$OUT" != "ROUTE gqlc-r1" ]; then
    bad "a subject present on master with no open PR routes" "expected 'ROUTE gqlc-r1', got: $OUT"
else
    ok "a bead whose subject is on origin/master and in no open PR is routed"
fi

gh_prs '[{"number":1057,"files":[{"path":"justfile"}]}]'
hv '[{"id":"gqlc-r1","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ]; then
    bad "an open PR touching the subject holds it" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-r1 '; then
    bad "an open PR touching the subject holds it" "not held: $OUT"
elif ! printf '%s' "$OUT" | grep -q '#1057'; then
    bad "an open PR touching the subject holds it" "the hold does not name the PR: $OUT"
else
    ok "a bead whose subject an open PR modifies is held, and the hold names that PR number"
fi

# Row 3 — the slash boundary, as its own pair. A substring match would hold
# every bead under `internal/tools/tmpreaper` for a PR touching
# `internal/tools/tmpreap`, and the two are different subsystems. `kingdom/bin`
# is a TREE on master, which also exercises cat-file's directory case.
gh_prs '[{"number":900,"files":[{"path":"kingdom/bin/km"}]}]'
hv '[{"id":"gqlc-dir","labels":["class:warrior","subject:kingdom/bin"]}]'
if [ "$RC" -ne 0 ] || ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-dir .*#900'; then
    bad "a directory subject is held by a PR touching a file under it" "rc=$RC out=$OUT err=$ERR"
else
    ok "a directory subject is held by an open PR touching a file beneath it"
fi

gh_prs '[{"number":900,"files":[{"path":"kingdom/binary-thing"}]}]'
hv '[{"id":"gqlc-dir","labels":["class:warrior","subject:kingdom/bin"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a shared string prefix without the slash boundary does not hold" "rc=$RC out=$OUT err=$ERR"
elif [ "$OUT" != "ROUTE gqlc-dir" ]; then
    bad "a shared string prefix without the slash boundary does not hold" \
        "kingdom/binary-thing was read as being under kingdom/: $OUT"
else
    ok "a path merely sharing the subject's string prefix, without the slash boundary, does not hold it"
fi

# Row 4 — condition (1), which is the non-redundant half: it still covers a PR
# closed without merging, a branch with no PR yet, and any run where gh is
# unreachable. Its falsifier is row 1, which differs only in the path existing.
gh_prs '[]'
hv '[{"id":"gqlc-gone","labels":["class:warrior","subject:no/such/path/km-hold-test"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a subject absent from origin/master is held" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-gone .*premise absent'; then
    bad "a subject absent from origin/master is held" "expected a premise-absent hold, got: $OUT"
else
    ok "a bead whose subject path does not exist on origin/master is held as premise-absent"
fi

# Rows 5, 6 and 7 — the dep arm, for residue that has no subject: label yet.
# Three rows, one varying input each: the parent's STATUS (5), the presence of a
# dep at all (6), and the dep's TYPE (7). Together they pin that the gate is
# discovered-from-and-open, not "has any edge".
gh_prs '[]'
hv '[{"id":"gqlc-res","labels":["class:warrior"],
      "deps":[{"depends_on_id":"gqlc-par","type":"discovered-from","status":"open"}]}]'
if [ "$RC" -ne 0 ]; then
    bad "residue of an open parent is held" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-res .*gqlc-par'; then
    bad "residue of an open parent is held" "expected a hold naming the parent, got: $OUT"
else
    ok "an unlabelled bead discovered from a still-open parent is held, and the hold names that parent"
fi

hv '[{"id":"gqlc-res","labels":["class:warrior"],
      "deps":[{"depends_on_id":"gqlc-par","type":"discovered-from","status":"closed"}]}]'
if [ "$RC" -ne 0 ] || [ "$OUT" != "ROUTE gqlc-res" ]; then
    bad "residue of a closed parent routes" "rc=$RC out=$OUT err=$ERR"
else
    ok "the same bead routes once its parent closes — the hold releases itself, with nobody remembering"
fi

hv '[{"id":"gqlc-plain","labels":["class:warrior"]}]'
if [ "$RC" -ne 0 ] || [ "$OUT" != "ROUTE gqlc-plain" ]; then
    bad "an ordinary bead is unaffected" "rc=$RC out=$OUT err=$ERR"
else
    ok "an ordinary bead with no subject label and no deps routes exactly as before"
fi

hv '[{"id":"gqlc-oth","labels":["class:warrior"],
      "deps":[{"depends_on_id":"gqlc-par","type":"until","status":"open"},
              {"depends_on_id":"gqlc-p2","type":"related","status":"in_progress"}]}]'
if [ "$RC" -ne 0 ] || [ "$OUT" != "ROUTE gqlc-oth" ]; then
    bad "only discovered-from gates" "an until/related edge to an open parent held it: rc=$RC out=$OUT"
else
    ok "until and related edges to open parents do not hold — only discovered-from carries the residue meaning"
fi

# Row 8 — fail-closed is SCOPED. A gh outage must not stop the town: it holds
# exactly the guarded class and lets ordinary beads through. Both candidates are
# in ONE invocation, so this cannot pass by holding everything.
GH_RC=1
hv '[{"id":"gqlc-sub","labels":["class:warrior","subject:justfile"]},
     {"id":"gqlc-nosub","labels":["class:warrior"]}]'
GH_RC=""
if [ "$RC" -ne 0 ]; then
    bad "a gh outage holds only the subject-labelled candidates" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-sub .*gh unavailable'; then
    bad "a gh outage holds only the subject-labelled candidates" "the subject-labelled bead was not held: $OUT"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-nosub'; then
    bad "a gh outage holds only the subject-labelled candidates" \
        "the outage stopped an ordinary bead too, so an outage would idle the whole town: $OUT"
else
    ok "when gh is unreachable the subject-labelled bead holds and the ordinary one still routes — the fail-closed direction is scoped, not global"
fi

# Row 9 — the abort is loud, and a malformed candidate costs one line, not the
# run. gqlc-z1qw is the whole reason: a jq abort that reads as a healthy zero.
gh_prs '[]'
hv '[{"id":"gqlc-a","labels":["class:warrior"]},
     {"id":"gqlc-b"},
     {"id":"gqlc-c","labels":["class:warrior"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a malformed candidate holds itself and does not abort the run" "rc=$RC out=$OUT err=$ERR"
elif [ "$(printf '%s\n' "$OUT" | grep -c .)" -ne 3 ]; then
    bad "a malformed candidate holds itself and does not abort the run" "expected 3 lines, got: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-b .*malformed'; then
    bad "a malformed candidate holds itself and does not abort the run" "the candidate with no labels was not held: $OUT"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-a' || ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-c'; then
    bad "a malformed candidate holds itself and does not abort the run" "it swallowed its neighbours: $OUT"
else
    ok "a candidate missing its labels is held as malformed while its neighbours are still answered — one bad row costs one line, not the run"
fi

hv 'not json at all'
if [ "$RC" -eq 0 ]; then
    bad "unparseable stdin refuses" "exited 0: $OUT"
elif [ -n "$OUT" ]; then
    bad "unparseable stdin refuses" "it emitted verdicts anyway: $OUT"
elif ! printf '%s' "$ERR" | grep -q 'hold-verdict'; then
    # Not merely "something on stderr": a bare `jq: error (at <stdin>:0)` is
    # what this looks like when the refusal is deleted and the downstream jq
    # happens to fail too, which is a different program being right by luck.
    bad "unparseable stdin refuses" "the refusal does not identify itself: err=$ERR"
else
    ok "stdin that is not a JSON array exits nonzero and says so in its own name, rather than printing an empty and healthy-looking verdict set"
fi

# A junk element inside a well-formed array. The array parses, so the run must
# continue; the element is not a candidate document, so it cannot be cleared.
hv '["just a string",{"id":"gqlc-ok","labels":["class:warrior"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a non-object candidate is held, not cleared" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -q '^HOLD ? — malformed candidate: not an object'; then
    bad "a non-object candidate is held, not cleared" "expected a malformed hold for the junk element, got: $OUT"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-ok'; then
    bad "a non-object candidate is held, not cleared" "the good candidate beside it was lost: $OUT"
else
    ok "an array element that is not a candidate document at all is held as malformed, and its well-formed neighbour still routes"
fi

# Row 10 — gh's own silent cap, which the design named as an unverified risk.
# MEASURED 2026-08-22 on PR #742: changedFiles 102 against a files array of 100.
# `--json files` caps per PR and says nothing, so a capped list taken at face
# value makes condition (2) fail OPEN on precisely the largest PRs.
#
# changedFiles is the tell, and this pair holds the file COUNT fixed at 3 to say
# so: what decides is the DISAGREEMENT, not the size. A count threshold cannot
# pass this pair at all — both sides are far under any cap.
gh_prs "$(jq -cn '[{number: 742, changedFiles: 5, files: [range(3) | {path: "pad/f\(.).go"}]}]')"
hv '[{"id":"gqlc-cap","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a truncated PR file list is unknown, not empty" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-cap .*#742'; then
    bad "a truncated PR file list is unknown, not empty" \
        "changedFiles 5 over 3 listed says two paths are withheld, and they were read as absent: $OUT"
else
    ok "a PR that reports more changed files than it lists is treated as unknown and holds, instead of silently reading as a PR that does not touch the subject"
fi

gh_prs "$(jq -cn '[{number: 742, changedFiles: 3, files: [range(3) | {path: "pad/f\(.).go"}]}]')"
hv '[{"id":"gqlc-cap","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ] || [ "$OUT" != "ROUTE gqlc-cap" ]; then
    bad "a complete PR file list that misses the subject routes" \
        "changedFiles agrees with the 3 listed and none of them is the subject: rc=$RC out=$OUT"
else
    ok "a PR whose changedFiles agrees with its file list is trusted, so a complete list does not hold everything"
fi

# The measured shape itself, and the reason text. Արփինէ's ruling asks the
# journal line to carry its own evidence, so the indefinite hold prints both
# counts: a reader of the log can see WHY the answer is unknown without going
# back to gh, and by then the PR may have merged and the numbers moved.
gh_prs "$(jq -cn '[{number: 742, changedFiles: 102, files: [range(100) | {path: "pad/f\(.).go"}]}]')"
hv '[{"id":"gqlc-cap","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ]; then
    bad "an indefinite hold prints the counts it rests on" "rc=$RC out=$OUT err=$ERR"
elif [ "$OUT" != "HOLD gqlc-cap — open PR #742 lists 100 of 102 files — cannot rule out justfile" ]; then
    bad "an indefinite hold prints the counts it rests on" \
        "the reason must name both counts, or the log cannot be audited after the PR merges: $OUT"
else
    ok "the indefinite hold names the PR and both counts, so the journal line carries the evidence for its own verdict"
fi

# Ordering: a match in the VISIBLE list of a truncated PR is still definite, and
# must not be downgraded to "cannot rule out". Asserted on the reason TEXT —
# both arms return HOLD, so a verdict-only assertion witnesses nothing here.
# The parentheses around the value are required by jq 1.6, which CI has and which
# rejects a bare `+` in an object-value position ("May need parentheses around
# object key expression"). jq 1.8 here accepts it, so the row was green locally and
# red only in CI.
gh_prs "$(jq -cn '[{number: 742, changedFiles: 102, files: ([range(99) | {path: "pad/f\(.).go"}] + [{path: "justfile"}])}]')"
hv '[{"id":"gqlc-cap","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a visible match in a truncated PR holds for the definite reason" "rc=$RC out=$OUT err=$ERR"
elif [ "$OUT" != "HOLD gqlc-cap — open PR #742 touches justfile" ]; then
    bad "a visible match in a truncated PR holds for the definite reason" \
        "the subject is right there in the visible list; reporting it as unrulable understates the evidence: $OUT"
else
    ok "a subject found in the visible files of a truncated PR is reported as a definite match, not downgraded to the indefinite cannot-rule-out reason"
fi

# The over-holding half. A threshold guard reads any 100-file PR as suspect; the
# real cap is only a cap when something was actually withheld, and a PR that
# changed exactly 100 files withheld nothing.
gh_prs "$(jq -cn '[{number: 742, changedFiles: 100, files: [range(100) | {path: "pad/f\(.).go"}]}]')"
hv '[{"id":"gqlc-cap","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ] || [ "$OUT" != "ROUTE gqlc-cap" ]; then
    bad "a complete file list sitting exactly on gh's cap is still complete" \
        "100 listed and 100 changed withholds nothing, so this must not hold: rc=$RC out=$OUT"
else
    ok "a PR that changed exactly as many files as gh's cap allows is read as complete, instead of held forever by a boundary the data does not support"
fi

# Row 11 — the LIST-level cap, one level up from the per-PR one above. gh has no
# list-level tell at all: asked for 100 it returns 100 whether there are 100 open
# PRs or 400, so the 101st PR is indistinguishable from no PR. The per-PR
# truncation guard cannot see this — every listed PR can be individually complete
# while the PR that touches the subject was never listed. Length == the limit
# asked for is therefore UNKNOWN, and unknown holds.
#
# The fixture holds 101 open PRs and the stub hands back the first 100, so the
# PR that touches the subject is the one gh never mentions — the real shape,
# rather than a hundred PRs that happen to sit on the boundary.
gh_prs "$(jq -cn '[ range(100) | {number: (2000 + .), changedFiles: 1, files: [{path: "pad/f\(.).go"}]} ]
                  + [{number: 2999, changedFiles: 1, files: [{path: "justfile"}]}]')"
hv '[{"id":"gqlc-lcap","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a PR list sitting on the limit is unknown, not complete" "rc=$RC out=$OUT err=$ERR"
elif [ "$OUT" != "HOLD gqlc-lcap — gh unavailable — cannot rule out an open PR touching justfile" ]; then
    # On the REASON, not merely on HOLD: the 101st PR in this fixture touches the
    # subject, so a harness that hands back all 101 also holds — for a definite
    # match. Asserting the verdict alone would pass on the run where the cap was
    # never consulted at all.
    bad "a PR list sitting on the limit is unknown, not complete" \
        "none of the 100 listed PRs touches justfile, but the 101st does and would never appear: $OUT"
elif ! printf '%s' "$ERR" | grep -q 'hold-verdict.*100'; then
    # The hold degrades to the existing cannot-answer reason, which says "gh
    # unavailable" — true of the answer, not of the cause. Without this note the
    # journal blames an outage for a cap, and the operator checks the wrong thing.
    bad "a PR list sitting on the limit is unknown, not complete" \
        "the cap must name itself on stderr, or the hold is attributed to an outage that did not happen: err=$ERR"
else
    ok "an open-PR list whose length equals the limit asked for is treated as unknown and holds, and the cap names itself rather than being logged as a gh outage"
fi

# The falsifier: one PR fewer. Same fixture shape, same subject, and the only
# thing that changed is that the list is now demonstrably not truncated.
gh_prs "$(jq -cn '[ range(99) | {number: (2000 + .), changedFiles: 1, files: [{path: "pad/f\(.).go"}]} ]')"
hv '[{"id":"gqlc-lcap","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ] || [ "$OUT" != "ROUTE gqlc-lcap" ]; then
    bad "a PR list under the limit is complete" \
        "99 returned against a limit of 100 proves gh had nothing more to give: rc=$RC out=$OUT err=$ERR"
else
    ok "a list that comes back shorter than the limit is trusted as complete, so the list-cap guard does not hold the town forever"
fi

# Row 12 — the residue arm clears only on AFFIRMATIVE evidence. A parent whose
# status is anything other than "closed" — including a status this code has never
# heard of, and including the empty subset bd hands back for an id it cannot find —
# is not evidence the parent is done.
gh_prs '[]'
hv '[{"id":"gqlc-unk","labels":["class:warrior"],
      "deps":[{"depends_on_id":"gqlc-par","type":"discovered-from","status":"unknown"}]}]'
if [ "$RC" -ne 0 ]; then
    bad "residue of a parent in an unrecognised state is held" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-unk .*gqlc-par'; then
    bad "residue of a parent in an unrecognised state is held" \
        "an open/in_progress whitelist clears every state it does not recognise, which is the fail-OPEN direction: $OUT"
else
    ok "residue whose parent status is neither open nor closed is held rather than cleared — the gate reads 'not closed', so a state this code has not met yet cannot release work"
fi

# Input order is the contract cmd_dispatch relies on to keep priority order.
gh_prs '[]'
hv '[{"id":"gqlc-o1","labels":["class:warrior"]},
     {"id":"gqlc-o2","labels":["class:warrior","subject:no/such/path/km-hold-test"]},
     {"id":"gqlc-o3","labels":["class:warrior"]}]'
if [ "$RC" -ne 0 ]; then
    bad "verdicts come back in input order" "rc=$RC out=$OUT err=$ERR"
elif [ "$(printf '%s\n' "$OUT" | awk '{print $2}' | tr '\n' ' ')" != "gqlc-o1 gqlc-o2 gqlc-o3 " ]; then
    bad "verdicts come back in input order" "order lost: $OUT"
else
    ok "one line per candidate, in input order, so the caller can keep its priority ordering"
fi

# The fetch, which every row above skips. Without it the whole guard reads a
# frozen origin/master, so a path merged an hour ago stays "absent" and its bead
# is held forever — the failure mode is a permanent hold that looks exactly like
# a correct one. A second clone lands a path in the bare origin behind this
# working clone's back, leaving its tracking ref genuinely stale; the row then
# asks for that path with the fetch ENABLED. Its falsifier is the staleness
# itself: drop the fetch and the answer is HOLD.
FIXTURE_LATE="$TMP/late"
git clone -q "$FIXTURE_ORIGIN" "$FIXTURE_LATE"
git -C "$FIXTURE_LATE" config user.email fixture@example.invalid
git -C "$FIXTURE_LATE" config user.name fixture
git -C "$FIXTURE_LATE" config commit.gpgsign false
git -C "$FIXTURE_LATE" config core.hooksPath /dev/null
printf 'fixture\n' >"$FIXTURE_LATE/late-arrival"
git -C "$FIXTURE_LATE" add late-arrival
git -C "$FIXTURE_LATE" commit -qm 'lands after the working clone last looked'
git -C "$FIXTURE_LATE" push -q origin master

gh_prs '[]'
if [ "$(git -C "$FIXTURE" cat-file -e origin/master:late-arrival 2>&1; echo $?)" = "0" ]; then
    bad "the fetch is what makes a newly-merged path visible" \
        "the tracking ref is not stale, so this row cannot witness the fetch"
else
    printf '%s' '[{"id":"gqlc-late","labels":["class:warrior","subject:late-arrival"]}]' \
        | ( cd "$FIXTURE" && PATH="$BIN:$PATH" KM_FAKE_GH="$HVGH" "$KM" hold-verdict ) \
        >"$TMP/hv.out" 2>"$TMP/hv.err"
    RC=$?; OUT="$(cat "$TMP/hv.out")"; ERR="$(cat "$TMP/hv.err")"
    if [ "$RC" -ne 0 ] || [ "$OUT" != "ROUTE gqlc-late" ]; then
        bad "the fetch is what makes a newly-merged path visible" \
            "origin/master was stale and stayed stale: rc=$RC out=$OUT err=$ERR"
    else
        ok "hold-verdict fetches before it judges, so a path merged since the last run is seen and its bead is released instead of held against a frozen origin/master"
    fi
fi

# --- cmd_dispatch honours the verdict (gqlc-pj4r) ----------------------------
# The unit rows above prove the verdict is computed. These prove it is OBEYED —
# a held bead reaching a seat anyway would leave every row above green.

export KM_HOLD_SKIP_FETCH=1
export KM_FAKE_GH="$HVGH"
gh_prs '[{"number":1057,"files":[{"path":"justfile"}]}]'

dispatch_case '[
  {"id":"gqlc-held","priority":0,"assignee":null,"labels":["class:warrior","subject:justfile"]},
  {"id":"gqlc-free","priority":1,"assignee":null,"labels":["class:warrior"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "dispatch refuses to route a held bead" "rc=$RC out=$OUT"
elif grep -rq 'gqlc-held' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "dispatch refuses to route a held bead" "the held bead was routed to a seat anyway: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'hold gqlc-held'; then
    bad "dispatch refuses to route a held bead" "the hold is not reported, so it would be invisible: $OUT"
elif ! printf '%s' "$OUT" | grep -q '#1057'; then
    bad "dispatch refuses to route a held bead" "the reported hold does not say WHICH condition fired: $OUT"
elif ! grep -rq 'gqlc-free' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "dispatch refuses to route a held bead" \
        "the lower-priority routable bead did not reach a seat either, so the hold stopped the pass: $OUT"
else
    ok "dispatch holds the bead whose subject an open PR touches, says which condition fired, and still routes the next routable bead behind it"
fi

# The falsifier for the row above: the SAME queue with the PR map empty. Without
# it, "held" could be this dispatcher's answer to every subject-labelled bead.
gh_prs '[]'
dispatch_case '[
  {"id":"gqlc-held","priority":0,"assignee":null,"labels":["class:warrior","subject:justfile"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "dispatch routes a subject-labelled bead once nothing touches it" "rc=$RC out=$OUT"
elif ! grep -rq 'gqlc-held' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "dispatch routes a subject-labelled bead once nothing touches it" \
        "it was held with no open PR and its subject on master (woken: '$(woken_seats)'): $OUT"
else
    ok "with the PR closed, the same subject-labelled bead routes — the hold is released by the merge, not by anyone remembering"
fi

# The dep arm reaches dispatch only through two further bd queries, whose shapes
# the design measured and which this pins: `dep list` multi-id rows carry
# issue_id, and parent status arrives from a separate `show`.
gh_prs '[]'
dispatch_case '[
  {"id":"gqlc-dres","priority":0,"assignee":null,"labels":["class:warrior"]}
]' '[]' '[{"issue_id":"gqlc-dres","depends_on_id":"gqlc-dpar","type":"discovered-from"}]' \
   '[{"id":"gqlc-dpar","status":"in_progress"}]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "dispatch holds residue of an unfinished parent" "rc=$RC out=$OUT"
elif grep -rq 'gqlc-dres' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "dispatch holds residue of an unfinished parent" "it was routed: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'hold gqlc-dres .*gqlc-dpar'; then
    bad "dispatch holds residue of an unfinished parent" "the hold does not name the parent: $OUT"
else
    ok "dispatch assembles the dep edges and parent statuses from bd and holds unlabelled residue of a parent still in progress"
fi

dispatch_case '[
  {"id":"gqlc-dres","priority":0,"assignee":null,"labels":["class:warrior"]}
]' '[]' '[{"issue_id":"gqlc-dres","depends_on_id":"gqlc-dpar","type":"discovered-from"}]' \
   '[{"id":"gqlc-dpar","status":"closed"}]'
run_dispatch
if [ "$RC" -ne 0 ] || ! grep -rq 'gqlc-dres' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "dispatch routes residue once the parent closes" "rc=$RC out=$OUT woken='$(woken_seats)'"
else
    ok "the same residue routes once its parent is closed, so the dispatcher's hold self-releases"
fi

# A verdict that cannot be computed must skip the fresh pass, never fall through
# to routing unchecked. The resume wakes taken earlier in the run still stand.
gh_prs '[]'
dispatch_case '[
  {"id":"gqlc-x1","priority":0,"assignee":null,"labels":["class:warrior"]}
]' '[
  {"id":"gqlc-x2","assignee":"vahagn","labels":["class:warrior"]}
]' 'not json at all'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a failed verdict skips the fresh pass" "rc=$RC out=$OUT"
elif grep -rq 'gqlc-x1' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "a failed verdict skips the fresh pass" "it routed the fresh bead unchecked: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'fresh pass skipped'; then
    bad "a failed verdict skips the fresh pass" "the skip is not announced: $OUT"
elif printf '%s' "$OUT" | grep -q 'dispatch: done'; then
    # The skip must not ALSO sign off as a completed run. A pass that announces
    # its own failure and then prints its ordinary closing line reads, to anyone
    # scanning the journal for the last line, exactly like a healthy run.
    bad "a failed verdict skips the fresh pass" "it announced the skip and then signed off as a normal run: $OUT"
elif ! wake_of vahagn | grep -q 'gqlc-x2'; then
    bad "a failed verdict skips the fresh pass" "it discarded the resume wake taken earlier in the run: $OUT"
else
    ok "when the hold verdict cannot be computed the fresh pass is skipped out loud and nothing is routed unchecked, while the resume wakes already taken stand"
fi

unset KM_HOLD_SKIP_FETCH KM_FAKE_GH
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
        # Two seats of DIFFERENT classes. With a one-warrior roster every argv
        # row launches the same class, so `cfg effort "$class"` hard-wired to
        # `warrior` resolves correctly for the only seat there is and the suite
        # stays green (Միհր, verdict-uhqh-r1 B3, M5).
        printf '[seats]\nhayk = "warrior:claude-opus-5:Հայկ"\nmihr = "judge:claude-opus-5:Միհր"\n'
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
# The seat's ENVIRONMENT is part of what km-seat composes, not just its argv.
printf 'KM_CONFIG=%s\n' "${KM_CONFIG-<unset>}" >"$KM_TEST_ARGV.env"
STUB
chmod +x "$stubdir/claude"

# The argv carries the whole soul, so a raw dump buries the flag it is being
# printed to show. Everything before --append-system-prompt is the part these
# rows are about.
argv_brief() { [ -s "$1" ] && sed '/^--append-system-prompt$/q' "$1" | tr '\n' ' '; }

# km resolves the town from `git rev-parse` against the CALLER's cwd (km:78-82)
# and km-seat then demands a seat worktree beside it. Run from the real repo,
# these rows read the author's own `../<repo>-seat-hayk` — which exists on the
# machine they were written on and on no CI runner, where all four failed with
# "no worktree at /home/runner/work/gqlc/gqlc-seat-hayk". So the rows get a
# throwaway town of their own and depend on nothing outside $TMP.
TOWN="$TMP/town"
mkdir -p "$TOWN"
TOWN=$(cd "$TOWN" && pwd -P) # git reports the physical path; mktemp may hand back a symlink
gitf init -q "$TOWN"
mkdir -p "$TOWN-seat-hayk" "$TOWN-seat-mihr"

ARGV=""
STDERR=""
compose_argv() { # compose_argv <config> [seat] -> ARGV (one arg per line), STDERR
    local cfgfile=$1 seat=${2:-hayk} sdir pid waited=0
    ARGV="$TMP/argv.$RANDOM"
    STDERR="$TMP/stderr.$RANDOM"
    sdir="$TMP/seatstate.$RANDOM"
    mkdir -p "$sdir/seats/$seat"
    echo "a test wake" >"$sdir/seats/$seat/wake"
    (cd "$TOWN" && PATH="$stubdir:$PATH" KM_CONFIG="$cfgfile" KM_STATE_DIR="$sdir" \
        KM_TEST_ARGV="$ARGV" "$KM_SEAT" "$seat") >"$STDERR" 2>&1 &
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
    bad "km-seat passes --effort" "argv: $(argv_brief "$ARGV")"
elif [ "$(grep -A1 -x -- '--effort' "$ARGV" | tail -1)" != high ]; then
    bad "km-seat passes the class's level" "argv: $(argv_brief "$ARGV")"
else
    ok "km-seat passes --effort high for a warrior seat when [effort] says high"
fi

# A DIFFERENT class through the same path, at a level no other argv row uses.
# The row above cannot see a resolver hard-wired to `warrior`, because the seat
# it launches is a warrior; nor a whitelist that has lost `xhigh`, because it
# never asks for one. Both survived a full battery at 42/42 until this row
# existed (Միհր, verdict-uhqh-r1 B3). `xhigh` is also the level #1134 assigns
# the judge, so dropping it silently is a live misconfiguration, not a
# hypothetical.
alt_config "$TMP/judge-xhigh.toml" 'warrior   = "low"' 'judge     = "xhigh"'
compose_argv "$TMP/judge-xhigh.toml" mihr
if [ ! -s "$ARGV" ]; then
    bad "a judge seat launches at all" "no argv; log: $(cat "$STDERR" 2>/dev/null)"
elif [ "$(grep -A1 -x -- '--effort' "$ARGV" | tail -1)" != xhigh ]; then
    bad "a judge seat launches at the judge's level, not the warrior's" "argv: $(argv_brief "$ARGV")"
else
    ok "a judge seat launches at xhigh while the same config puts warriors at low"
fi

# The level must come from the seat's CLASS, not from a fixed string: flip the
# config and the same seat must launch differently.
alt_config "$TMP/warrior-low.toml" 'warrior = "low"'
compose_argv "$TMP/warrior-low.toml"
if [ "$(grep -A1 -x -- '--effort' "$ARGV" 2>/dev/null | tail -1)" != low ]; then
    # The detail reads a file that does not exist when the launch itself failed,
    # so without the guard the row's own failure message is buried under a raw
    # "No such file or directory" from the shell.
    bad "the level tracks the config, not a hard-coded default" "argv: $(argv_brief "$ARGV")"
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
    bad "no [effort] section must omit the flag entirely" "argv: $(argv_brief "$ARGV")"
# Silence is part of the claim. Absence is the ordinary state of a town that
# has not tuned [effort], not a misconfiguration, so it must not warn on every
# launch — and deleting the empty arm from km-seat's case leaves the argv
# identical while doing exactly that (Միհր, verdict-uhqh-r1 N1).
elif [ -s "$STDERR" ]; then
    bad "no [effort] section must launch quietly" "stderr: $(cat "$STDERR")"
else
    ok "no [effort] section omits --effort, says nothing, and the seat launches anyway"
fi

# The seam must not follow the seat in. KM_CONFIG is set for km-seat's benefit,
# and if it reached the launched claude then every `km` a citizen's tools run
# would read the suite's throwaway roster instead of the town's — the shape that
# sent bd into the wrong repo via an inherited GIT_DIR this week. km-seat sets
# the override HERE, on purpose, so a row asserting the child cannot see it is
# the only thing standing between that and production.
compose_argv "$TMP/warrior-high.toml"
if [ ! -s "$ARGV.env" ]; then
    bad "the launched claude's environment is observable at all" "no env recorded"
elif ! grep -qx 'KM_CONFIG=<unset>' "$ARGV.env"; then
    bad "KM_CONFIG must not reach the launched claude" "$(cat "$ARGV.env")"
else
    ok "the seat's claude is launched without KM_CONFIG, whatever km-seat itself read"
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
    bad "a misspelled level must not reach claude" "argv: $(argv_brief "$ARGV")"
elif ! grep -q 'mediun' "$STDERR"; then
    bad "a misspelled level must be named on stderr" "log: $(cat "$STDERR" 2>/dev/null)"
else
    ok "a misspelled level is refused, named on stderr, and the seat still launches"
fi

# --- deploy: the town must run the code that merged --------------------------
# PR #1081 merged the P0 dispatcher fix and the town was told it was fixed. The
# systemd units execute kingdom/bin/km out of the main checkout, which nothing
# advances, so the defect kept running for hours behind a healthy indicator
# (bd gqlc-ed2u). Two halves are pinned here: a detector that FAILS rather than
# warns, and a deploy that moves the tree without touching what bd exported
# into it.

cat >"$BIN/claude" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$BIN/claude"

deploy_case() { # <name> : a fresh origin + clone, and KM_DEPLOY_ROOT pointing at it
    mk_origin "$TMP/$1.git"
    mk_clone "$TMP/$1.git" "$TMP/$1"
    export KM_DEPLOY_ROOT="$TMP/$1"
}
run_stubbed() { OUT="$(PATH="$BIN:$PATH" "$KM" "$@" 2>&1)"; RC=$?; }
doctor_line() { printf '%s' "$OUT" | grep -i 'kingdom.*origin/master\|origin/master.*kingdom' | head -1; }

# The two doctor rows are one differential: same stubs, same PATH, same state
# dir, and the ONLY difference is whether the deploy root matches origin/master.
# That is what makes the non-zero exit attributable to the drift rather than to
# a missing tmux — "does it FAIL?", not "is there a check?" (bd gqlc-z1qw).

deploy_case doctor-clean
run_stubbed doctor
if [ "$RC" -ne 0 ]; then
    bad "doctor passes a deploy root that matches origin/master" "rc=$RC out=$OUT"
elif ! doctor_line | grep -q '^ok:'; then
    bad "doctor passes a deploy root that matches origin/master" "no ok: row names the deployed tree: $OUT"
else
    ok "doctor passes when the deployed kingdom/ matches origin/master"
fi

deploy_case doctor-behind
advance_origin "$TMP/doctor-behind.git" kingdom/bin/km "fixed"
gitf -C "$TMP/doctor-behind" fetch -q origin
run_stubbed doctor
if [ "$RC" -eq 0 ]; then
    bad "doctor FAILS on a stale deploy root" "exited 0 — a warning is not a gate: $OUT"
elif ! doctor_line | grep -q '^FAIL:'; then
    bad "doctor FAILS on a stale deploy root" "the deployed-tree row is not a FAIL: $OUT"
else
    ok "doctor FAILS, not warns, when the deployed kingdom/ is behind origin/master"
fi

# Drift is a property of the tree that executes, not of HEAD. An implementation
# that compared revisions would pass this row while the town ran edited code.
deploy_case doctor-edited
printf 'hand-edited\n' >"$TMP/doctor-edited/kingdom/bin/km"
run_stubbed doctor
if [ "$RC" -eq 0 ] || ! doctor_line | grep -q '^FAIL:'; then
    bad "an edited deployed file is drift even at the right commit" "rc=$RC out=$OUT"
else
    ok "doctor FAILS on a deployed file edited in place, though HEAD is origin/master"
fi

# The refusals. A detector nobody runs every two minutes is still a detector you
# must remember, so the timer-driven commands check their own freshness.

# An exported GIT_DIR is the shape that made this suite destructive, and the
# decoy is a LINKED WORKTREE because that shape is load-bearing:
#   - `git -C <path> commit` writes through GIT_DIR, so the fixture commits landed
#     in the repo under test and force-moved its branch; `switch -c` parked it.
#   - a plain `git init <dir>` re-initialises GIT_DIR, and git guesses bareness
#     from the gitdir's own NAME (guess_repository_type). A linked worktree's
#     gitdir is .git/worktrees/<name>, which does not end in .git, so git guesses
#     BARE and writes core.bare=true into the SHARED config — after which every
#     checkout on that repo has no work tree. The pushes that broke this repo came
#     from a seat worktree, and a decoy whose GIT_DIR ends in .git cannot witness
#     the flip at all: it would pass this row on a technicality (bd gqlc-ed2u).
# `git init --bare` and `git clone` are not vectors — --bare re-points GIT_DIR at
# "." after its chdir, and clone ignores it — but they still go through gitf,
# because a scrubbing rule with a remembered exception is the same failure shape
# as a deploy you must remember.

# The decoy is a deployable-looking repo left BEHIND its own origin — the most
# attractive victim an unscrubbed fetch-and-merge could find, and the shape of
# the checkout that actually got moved.
mk_origin "$TMP/decoy.git"
mk_clone "$TMP/decoy.git" "$TMP/decoy"
gitf -C "$TMP/decoy" worktree add -q "$TMP/decoy-wt" -b decoy-wt
advance_origin "$TMP/decoy.git" kingdom/bin/km "fixed"
gitf -C "$TMP/decoy-wt" fetch -q origin
# A distinct identity, because the fixtures write one and the writes are silent:
# unscrubbed, `config user.email` landed in this repo's shared config and stayed
# there until a commit hook refused the address days later.
gitf -C "$TMP/decoy" config user.email decoy@decoy
decoy_email=$(gitf -C "$TMP/decoy" config --local user.email)
decoy_head=$(gitf -C "$TMP/decoy-wt" rev-parse HEAD)
DECOY_GITDIR=$(gitf -C "$TMP/decoy-wt" rev-parse --absolute-git-dir)
export GIT_DIR="$DECOY_GITDIR"
case "$GIT_DIR" in
    *.git) bad "the decoy exposes a worktree gitdir" "GIT_DIR=$GIT_DIR ends in .git, so the bare-guess cannot fire" ;;
    *)     ok "the decoy exposes a worktree gitdir, where git's bare-guess applies" ;;
esac
mk_origin "$TMP/leak.git"
mk_clone "$TMP/leak.git" "$TMP/leak"
gitf -C "$TMP/leak" switch -qc parked
unset GIT_DIR
if [ "$(gitf -C "$TMP/decoy" config --local core.bare)" != false ]; then
    bad "an exported GIT_DIR cannot redirect the fixtures" "a fixture init flipped core.bare on the decoy"
elif [ "$(gitf -C "$TMP/decoy-wt" rev-parse HEAD)" != "$decoy_head" ]; then
    bad "an exported GIT_DIR cannot redirect the fixtures" "a fixture commit moved the decoy worktree's HEAD"
elif [ "$(gitf -C "$TMP/decoy-wt" symbolic-ref --short HEAD)" != decoy-wt ]; then
    bad "an exported GIT_DIR cannot redirect the fixtures" "a fixture switch parked the decoy worktree"
elif [ "$(gitf -C "$TMP/decoy" config --local user.email)" != "$decoy_email" ]; then
    bad "an exported GIT_DIR cannot redirect the fixtures" "a fixture config write took over the decoy's identity"
elif [ "$(gitf -C "$TMP/leak.git.seed" rev-list --count HEAD 2>/dev/null)" != 1 ]; then
    bad "an exported GIT_DIR cannot redirect the fixtures" "the fixture did not land in its own repo"
else
    ok "an exported GIT_DIR redirects neither the fixture commits, its branch, its identity, nor init"
fi

# km reads paths from the environment too, so the same leak must not move its
# drift check off KM_DEPLOY_ROOT and onto whichever repo invoked the hook.
deploy_case doctor-git-dir
advance_origin "$TMP/doctor-git-dir.git" kingdom/bin/km "fixed"
gitf -C "$TMP/doctor-git-dir" fetch -q origin
OUT="$(PATH="$BIN:$PATH" GIT_DIR="$DECOY_GITDIR" "$KM" doctor 2>&1)"
RC=$?
if [ "$RC" -eq 0 ] || ! doctor_line | grep -q '^FAIL:'; then
    bad "km reads its deploy root, not an exported GIT_DIR" "rc=$RC out=$OUT"
else
    ok "km still measures KM_DEPLOY_ROOT when a hook has exported GIT_DIR"
fi

# The same leak with the sign reversed, and the pair is the point: the decoy is
# behind its own origin, so a drift check that reads through the inherited env
# reports drift on a root that has none. A single row only catches the direction
# it happens to share with the leak, and then passes for the wrong reason.
deploy_case doctor-git-dir-clean
OUT="$(PATH="$BIN:$PATH" GIT_DIR="$DECOY_GITDIR" "$KM" doctor 2>&1)"
RC=$?
if [ "$RC" -ne 0 ] || ! doctor_line | grep -q '^ok:'; then
    bad "a clean root stays clean when the hook's repo is drifted" "rc=$RC out=$OUT"
else
    ok "doctor passes a clean deploy root though GIT_DIR names a repo that is behind"
fi

# The row above pins KM_DEPLOY_ROOT, so it never exercises the derivation. With
# the override absent the root comes from git, and that must be read from the
# working directory rather than from a hook's exported GIT_DIR — deploy is the
# first thing in km that WRITES to the root it resolves.
deploy_case main-root-git-dir
advance_origin "$TMP/main-root-git-dir.git" kingdom/bin/km "fixed"
gitf -C "$TMP/main-root-git-dir" fetch -q origin
OUT="$(cd "$TMP/main-root-git-dir" \
    && PATH="$BIN:$PATH" GIT_DIR="$DECOY_GITDIR" env -u KM_DEPLOY_ROOT "$KM" status 2>&1)"
if ! printf '%s' "$OUT" | grep -q "DRIFT: $TMP/main-root-git-dir is not origin/master"; then
    bad "the derived deploy root comes from the cwd, not an exported GIT_DIR" "no DRIFT for the cwd's repo: $OUT"
elif printf '%s' "$OUT" | grep -q "$TMP/decoy"; then
    bad "the derived deploy root comes from the cwd, not an exported GIT_DIR" "km resolved the decoy: $OUT"
else
    ok "with no KM_DEPLOY_ROOT the deploy root is derived from the cwd, not a hook's GIT_DIR"
fi
export KM_DEPLOY_ROOT="$TMP/main-root-git-dir"

# Unknowable is not clean. If the ref the check reads is missing the answer must
# be drift, or the gate opens exactly where it has the least information.
deploy_case doctor-no-ref
gitf -C "$TMP/doctor-no-ref" update-ref -d refs/remotes/origin/master
run_stubbed doctor
if [ "$RC" -eq 0 ] || ! doctor_line | grep -q '^FAIL:'; then
    bad "no origin/master ref is drift, not clean" "rc=$RC out=$OUT"
else
    ok "a deploy root with no origin/master ref FAILS rather than reading as deployed"
fi

# ...and it must stay unknowable when a hook has exported a repo that DOES know
# origin/master. Read the ref through the inherited env and the check clears its
# own guard, then diffs against a ref its own root has never heard of: empty
# output, which this file spells "no drift". The row above cannot see that — its
# ambient environment has no repo to borrow an answer from.
deploy_case no-ref-git-dir
gitf -C "$TMP/no-ref-git-dir" update-ref -d refs/remotes/origin/master
OUT="$(PATH="$BIN:$PATH" GIT_DIR="$DECOY_GITDIR" "$KM" doctor 2>&1)"
RC=$?
if [ "$RC" -eq 0 ] || ! doctor_line | grep -q '^FAIL:'; then
    bad "a missing ref stays drift when the hook's repo has one" "rc=$RC out=$OUT"
else
    ok "a deploy root with no origin/master is drift even when GIT_DIR names a repo that has one"
fi

deploy_case dispatch-stale
advance_origin "$TMP/dispatch-stale.git" kingdom/bin/km "fixed"
gitf -C "$TMP/dispatch-stale" fetch -q origin
export KM_STATE_DIR="$TMP/dispatch-stale-state"
mkdir -p "$KM_STATE_DIR"
export KM_FAKE_READY="$KM_STATE_DIR/ready.json"
export KM_FAKE_INPROG="$KM_STATE_DIR/inprog.json"
printf '[{"id":"gqlc-w9","priority":0,"assignee":null,"labels":["class:warrior"]}]' >"$KM_FAKE_READY"
printf '[]' >"$KM_FAKE_INPROG"
run_stubbed dispatch
if [ "$RC" -eq 0 ]; then
    bad "a stale dispatcher refuses" "exited 0: $OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "a stale dispatcher refuses" "it routed work from stale code: $(woken_seats)"
elif ! printf '%s' "$OUT" | grep -q 'km deploy'; then
    bad "a stale dispatcher refuses" "the refusal does not name the remedy: $OUT"
else
    ok "a dispatcher whose own tree is behind origin/master refuses and names 'km deploy'"
fi

run_stubbed guard-sweep
if [ "$RC" -eq 0 ]; then
    bad "a stale guard sweep refuses" "exited 0: $OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "a stale guard sweep refuses" "it woke: $(woken_seats)"
else
    ok "the guard sweep refuses from a stale tree too, so Րաֆֆի is not run by dead code"
fi

# A refusal visible only in the journal is gqlc-vzpn again, so the glance says it.
run_stubbed status
if ! printf '%s' "$OUT" | grep -q 'DRIFT'; then
    bad "status shows the drift" "the town's glance is silent about stale machinery: $OUT"
else
    ok "km status announces DRIFT, so a refusing dispatcher is not journal-only"
fi

deploy_case status-clean
run_stubbed status
if printf '%s' "$OUT" | grep -q 'DRIFT'; then
    bad "status is quiet when deployed" "it cried DRIFT on a matching tree: $OUT"
else
    ok "km status says nothing about drift when the deployed tree matches"
fi

export KM_STATE_DIR="$TMP/state"

# Deploy proper. The live main checkout carries a STAGED .beads/issues.jsonl and
# an UNSTAGED .beads/interactions.jsonl — bd's export — and clobbering those
# reverts ledger state. These rows fix which of the two shapes moves the tree.

deploy_case deploy-ff
advance_origin "$TMP/deploy-ff.git" kingdom/bin/km "fixed"
gitf -C "$TMP/deploy-ff" fetch -q origin
printf 'local-export\n' >"$TMP/deploy-ff/.beads/issues.jsonl"
gitf -C "$TMP/deploy-ff" add .beads/issues.jsonl
printf 'local-inter\n' >"$TMP/deploy-ff/.beads/interactions.jsonl"
run_stubbed deploy
if [ "$RC" -ne 0 ]; then
    bad "deploy fast-forwards past unrelated local dirt" "rc=$RC out=$OUT"
elif [ "$(cat "$TMP/deploy-ff/kingdom/bin/km")" != fixed ]; then
    bad "deploy fast-forwards past unrelated local dirt" "the tree did not move: $(cat "$TMP/deploy-ff/kingdom/bin/km")"
elif [ "$(cat "$TMP/deploy-ff/.beads/issues.jsonl")" != local-export ] ||
    [ "$(cat "$TMP/deploy-ff/.beads/interactions.jsonl")" != local-inter ]; then
    bad "deploy fast-forwards past unrelated local dirt" "it clobbered bd's export"
elif [ -z "$(gitf -C "$TMP/deploy-ff" diff --cached --name-only)" ]; then
    bad "deploy fast-forwards past unrelated local dirt" "the staged export was unstaged under it"
else
    ok "deploy advances the tree while a staged and an unstaged bd export survive byte-intact"
fi

deploy_case deploy-conflict
advance_origin "$TMP/deploy-conflict.git" .beads/issues.jsonl "export-v2"
gitf -C "$TMP/deploy-conflict" fetch -q origin
printf 'local-export\n' >"$TMP/deploy-conflict/.beads/issues.jsonl"
gitf -C "$TMP/deploy-conflict" add .beads/issues.jsonl
before="$(gitf -C "$TMP/deploy-conflict" rev-parse HEAD)"
run_stubbed deploy
if [ "$RC" -eq 0 ]; then
    bad "deploy refuses when the incoming commit is under local dirt" "exited 0: $OUT"
elif [ "$(cat "$TMP/deploy-conflict/.beads/issues.jsonl")" != local-export ]; then
    bad "deploy refuses when the incoming commit is under local dirt" "the local export was overwritten anyway"
elif [ "$(gitf -C "$TMP/deploy-conflict" rev-parse HEAD)" != "$before" ]; then
    bad "deploy refuses when the incoming commit is under local dirt" "HEAD moved under a refusal"
elif ! printf '%s' "$OUT" | grep -q '\.beads/issues\.jsonl'; then
    bad "deploy refuses when the incoming commit is under local dirt" "the refusal does not name the file in the way: $OUT"
else
    ok "deploy refuses rather than clobber a dirty file the incoming commit touches"
fi

deploy_case deploy-off-master
gitf -C "$TMP/deploy-off-master" switch -qc parked
advance_origin "$TMP/deploy-off-master.git" kingdom/bin/km "fixed"
gitf -C "$TMP/deploy-off-master" fetch -q origin
run_stubbed deploy
if [ "$RC" -eq 0 ]; then
    bad "deploy refuses a deploy root parked off master" "exited 0: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'parked'; then
    bad "deploy refuses a deploy root parked off master" "the refusal does not name the branch: $OUT"
else
    ok "deploy refuses when the deploy root is parked on a branch that is not master"
fi

deploy_case deploy-current
run_stubbed deploy
if [ "$RC" -ne 0 ]; then
    bad "deploy on a current root is a no-op that says so" "rc=$RC out=$OUT"
else
    ok "deploy on an already-current root exits 0 instead of inventing work"
fi

# A merge cannot undo a hand edit, so deploy must not report success after one.
deploy_case deploy-edited
printf 'hand-edited\n' >"$TMP/deploy-edited/kingdom/bin/km"
run_stubbed deploy
if [ "$RC" -eq 0 ]; then
    bad "deploy does not vouch for a hand-edited tree" "exited 0 with kingdom/ still differing: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'kingdom/bin/km'; then
    bad "deploy does not vouch for a hand-edited tree" "the refusal does not name what still differs: $OUT"
else
    ok "deploy refuses to certify a deploy root whose kingdom/ was edited in place"
fi

# deploy is the only command in km that WRITES to a repo, and km is reachable
# from a hook, so this is where an inherited GIT_DIR costs the most. The decoy is
# behind its origin and would fast-forward cleanly if asked — so the row can tell
# "deploy moved the right repo" apart from "deploy moved nothing".
# No pre-fetch here, unlike the rows above: deploy has to reach origin itself,
# and the check is against the bare repo's master rather than the root's own
# tracking ref — a fetch that lands in the wrong repo leaves that ref stale, and
# a stale ref agrees with everything.
deploy_case deploy-git-dir
advance_origin "$TMP/deploy-git-dir.git" kingdom/bin/km "fixed"
victim_head=$(gitf -C "$TMP/decoy-wt" rev-parse HEAD)
victim_branch=$(gitf -C "$TMP/decoy-wt" symbolic-ref --short HEAD)
OUT="$(GIT_DIR="$DECOY_GITDIR" "$KM" deploy 2>&1)"
RC=$?
if [ "$RC" -ne 0 ]; then
    bad "deploy under an exported GIT_DIR moves its own root" "rc=$RC out=$OUT"
elif [ "$(gitf -C "$TMP/deploy-git-dir" rev-parse HEAD)" != "$(gitf -C "$TMP/deploy-git-dir.git" rev-parse master)" ]; then
    bad "deploy under an exported GIT_DIR moves its own root" "the root did not reach origin's master: $OUT"
elif [ "$(gitf -C "$TMP/decoy-wt" rev-parse HEAD)" != "$victim_head" ]; then
    bad "deploy under an exported GIT_DIR moves its own root" "it fast-forwarded the repo named by GIT_DIR"
elif [ "$(gitf -C "$TMP/decoy-wt" symbolic-ref --short HEAD)" != "$victim_branch" ]; then
    bad "deploy under an exported GIT_DIR moves its own root" "it moved the branch of the repo named by GIT_DIR"
else
    ok "deploy advances its own root under an exported GIT_DIR and leaves that repo where it stood"
fi

# The scrub is an allowlist, so it needs a witness on the KEEP side too. Every
# row above proves something was dropped; drop everything and they all still
# pass, while deploy quietly loses the transport it fetches over. Origin is
# rewritten to ssh so the fetch must invoke GIT_SSH_COMMAND. The fetch is
# EXPECTED to fail — there is no server — and what is asserted is that the
# transport was reached at all, which is the only part the scrub decides.
deploy_case deploy-ssh-env
gitf -C "$TMP/deploy-ssh-env" remote set-url origin ssh://git@km-test.invalid/town.git
cat >"$TMP/fake-ssh" <<SSH
#!/usr/bin/env bash
printf 'reached\n' >>"$TMP/ssh-marker"
exit 255
SSH
chmod +x "$TMP/fake-ssh"
rm -f "$TMP/ssh-marker"
GIT_DIR="$DECOY_GITDIR" GIT_SSH_COMMAND="$TMP/fake-ssh" "$KM" deploy >/dev/null 2>&1 || true
if [ ! -s "$TMP/ssh-marker" ]; then
    bad "the scrub keeps the ssh transport deploy fetches over" \
        "GIT_SSH_COMMAND did not survive git_at, so the fetch never reached it"
else
    ok "the scrub drops GIT_DIR and keeps GIT_SSH_COMMAND, so deploy can still reach an ssh origin"
fi

export KM_DEPLOY_ROOT="$TMP/deployed"

# This suite builds git repositories, so it must be able to prove it built them
# somewhere else. On PR #1128 it could not: a leaked GIT_DIR sent the fixture's
# `git init` and `git commit` into the repo under test, grafting six fixture
# commits onto the branch and rewriting what the PR contained — silently, three
# times, and the third one pushed. The unset at the top of this file is the fix.
# This is the alarm, kept because that failure is invisible in a green run and
# its blast radius is shipped history.
suite_end_head="$(git -C "$REPO" rev-parse HEAD 2>/dev/null || echo none)"
if [ "$SUITE_START_HEAD" = none ]; then
    # Both sides would read "none" and the comparison would pass having compared
    # nothing. Say so instead of banking a green row.
    printf 'skip - the suite leaves the repo it runs in untouched: %s has no HEAD to watch\n' "$REPO"
elif [ "$suite_end_head" != "$SUITE_START_HEAD" ]; then
    bad "the suite leaves the repo it runs in untouched" \
        "HEAD of $REPO moved $SUITE_START_HEAD -> $suite_end_head during this run"
else
    ok "the suite's own git fixtures land outside the repo under test, leaving its HEAD where it found it"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
