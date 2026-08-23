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

# git hooks export GIT_DIR into everything they shell out to, and this suite
# runs under pre-push. A seat pushes from a linked worktree, whose gitdir is
# <shared>/.git/worktrees/<seat> — a name not ending in .git, which git's
# guess_repository_type reads as BARE. A plain `git init <dir>` inherited into
# that environment therefore re-initialises the SHARED repo instead of creating
# <dir>/.git, and writes core.bare=true into it. That disables the shared
# checkout's main worktree while every linked worktree stays green, so no seat
# sees the damage (bd gqlc-tl78; it happened, twice).
unset "${!GIT_@}"

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
SUITE_START_HEAD="$(gitf -C "$REPO" rev-parse HEAD 2>/dev/null || echo none)"

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
gitf init -q --bare -b master "$FIXTURE_ORIGIN"
gitf init -q -b master "$FIXTURE"
gitf -C "$FIXTURE" config user.email fixture@example.invalid
gitf -C "$FIXTURE" config user.name fixture
gitf -C "$FIXTURE" config commit.gpgsign false
# The operator's global config may point core.hooksPath at this very repo; the
# fixture's own commits must not run the hooks under test.
gitf -C "$FIXTURE" config core.hooksPath /dev/null
mkdir -p "$FIXTURE/kingdom/bin"
printf 'fixture\n' >"$FIXTURE/justfile"
printf 'fixture\n' >"$FIXTURE/kingdom/bin/km"
gitf -C "$FIXTURE" add -A
gitf -C "$FIXTURE" commit -qm 'fixture: paths the hold rows ask about'
gitf -C "$FIXTURE" remote add origin "$FIXTURE_ORIGIN"
gitf -C "$FIXTURE" push -q origin master
gitf -C "$FIXTURE" fetch -q origin master
gitf -C "$FIXTURE" update-ref refs/remotes/origin/master FETCH_HEAD


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
# The guard sweep's scratch reap shells out to `just` in the deployed root, and
# declines with a named reason when that root has no justfile. Untracked, so it
# is invisible to kingdom_drift (which diffs the gate paths only) and the deploy
# rows below are unaffected.
printf 'fixture\n' >"$TMP/deployed/justfile"

# --- the hermetic seam: everything below stands on this one ------------------

run state-dir
if [ "$RC" -ne 0 ] || [ "$OUT" != "$TMP/state" ]; then
    bad "KM_STATE_DIR overrides the derived state dir" "rc=$RC out=$OUT"
else
    ok "KM_STATE_DIR overrides the derived state dir"
fi

# --- config reader -----------------------------------------------------------

# Read against a FIXTURE, not against the town. This row used to assert the
# live max_active, so raising the town's concurrency reddened the config
# reader on a one-number edit that touched no code (gqlc-p5s8). The fixture
# value is one no town would run, so the row cannot pass by coincidence if the
# KM_CONFIG override stops taking effect.
cat >"$TMP/reader.toml" <<'TOML'
[concurrency]
max_active = 4242
TOML
OUT="$(KM_CONFIG="$TMP/reader.toml" "$KM" cfg concurrency max_active 2>&1)"
RC=$?
if [ "$RC" -ne 0 ] || [ "$OUT" != 4242 ]; then
    bad "KM_CONFIG overrides the config path and cfg reads a bare scalar" "rc=$RC out=$OUT"
else
    ok "KM_CONFIG overrides the config path, and cfg reads a bare scalar from it"
fi

# That the live file still PARSES is a separate claim from what its number is.
# Asserting a shape rather than a value is what stops this row from becoming
# the pin it just was: any positive integer is a town someone may choose.
run cfg concurrency max_active
LIVE_MAX="$OUT"
if [ "$RC" -ne 0 ] || ! printf '%s' "$OUT" | grep -qE '^[1-9][0-9]*$'; then
    bad "the live kingdom.toml yields a positive max_active" "rc=$RC out=$OUT"
else
    ok "the live kingdom.toml yields a positive max_active, whatever it is tuned to"
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

# --- only Սեդրակ or Անդրանիկ lowers a halt (Article VI.4) --------------------
# VI.4: "Anyone may raise a halt for cause; only Սեդրակ or Անդրանիկ lowers it."
# km recorded WHO lowered it and let anyone do it: a guard lowered one on
# 2026-08-22 and km printed "halt lowered by raffi" cheerfully (bd gqlc-5hex).
# The halt is the town's only emergency brake, so the check fails closed — an
# identity the rule does not name, and no identity at all, are both refused,
# and neither may leave the flag removed on the way out.

run_as() { local s="$1"; shift; OUT="$(KINGDOM_SEAT="$s" "$KM" "$@" 2>&1)"; RC=$?; }

run_as raffi resume
if [ "$RC" -eq 0 ]; then
    bad "a seat outside VI.4 cannot lower the halt" "raffi lowered it: rc=0, out=$OUT"
elif [ ! -f "$haltf" ]; then
    bad "a seat outside VI.4 cannot lower the halt" "refused, then removed the flag anyway: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'VI.4'; then
    bad "a seat outside VI.4 cannot lower the halt" "the refusal names no authority: $OUT"
else
    ok "a seat the constitution does not name is refused, and the halt flag survives the refusal"
fi

# KINGDOM_SEAT unset reads as andranik everywhere else in km. For the one
# command that releases the emergency brake that default is the hole: every
# ephemeral agent session in the shared checkout runs without it, so the crown
# has to say so on purpose.
run resume
if [ "$RC" -eq 0 ]; then
    bad "an unidentified caller cannot lower the halt" "rc=0: $OUT"
elif [ ! -f "$haltf" ]; then
    bad "an unidentified caller cannot lower the halt" "the flag is gone: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'KINGDOM_SEAT'; then
    bad "an unidentified caller cannot lower the halt" "the refusal does not say how to declare an identity: $OUT"
else
    ok "with KINGDOM_SEAT unset the halt is not lowered, and the refusal says how to declare who you are"
fi

run_as sedrak resume
if [ "$RC" -ne 0 ] || [ -f "$haltf" ]; then
    bad "resume lowers the flag" "rc=$RC out=$OUT"
else
    ok "resume lowers the halt flag for the mayor"
fi

# The other half of the clause, and the raise that sets it up carries no
# identity on purpose: VI.4 restricts only the lowering.
run halt second cause
run_as andranik resume
if [ "$RC" -ne 0 ] || [ -f "$haltf" ]; then
    bad "the crown lowers the flag too" "rc=$RC out=$OUT"
else
    ok "the crown lowers the halt flag too, so the rule names two and not one"
fi

run_as sedrak resume
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

# The bd stub's default answer to a query no row has an opinion about. Exported
# because the stub is written from a QUOTED heredoc: $TMP does not exist inside
# it, and an unset path there would read as "no fixture named" and redden every
# row that merely happens to make the query.
export KM_EMPTY_BOARD="$TMP/empty-board.json"
printf '[]' >"$KM_EMPTY_BOARD"

# `just`, so the guard sweep's scratch reap can be observed without a real
# filesystem being reaped — the one thing in this suite whose live version
# deletes another agent's files. It records its argv and answers with the shape
# the real `tmp-reap-cadence` prints on a quiet tick; $KM_FAKE_JUST_RC is how a
# row makes it fail. Written to a path a row names, not to $KM_STATE_DIR, so it
# survives the dispatch_case that re-points that variable.
export KM_FAKE_JUST_CALLS="$TMP/just-calls"
cat >"$BIN/just" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${KM_FAKE_JUST_CALLS:-/dev/null}"
printf 'scratch: bytes 12%% , inodes 9%%\nunder the 75%% reap threshold — nothing was deleted.\n'
exit "${KM_FAKE_JUST_RC:-0}"
STUB
chmod +x "$BIN/just"

cat >"$BIN/tmux" <<'STUB'
#!/usr/bin/env bash
# The town is up. WHICH seats hold a window is data, not a constant: a seat is
# listed iff $KM_STATE_DIR/fake-windows names it. A seat with no window is both
# an ordinary state (a wake queues to a file instead of being typed into a
# pane) and the shape of a dead slot, so a stub that always answers the same
# way cannot express the distinction this suite now has to draw.
#
# Panes carry a synthetic tty per seat, which is what lets the `ps` stub decide
# SEPARATELY whether a claude runs on it. Window-exists and session-is-alive are
# different facts; conflating them is the defect under test (gqlc-s16s).
#
# A THIRD fact joins them here: whether the km-seat loop that IS the window's
# command still exists (gqlc-6ha4). It lives in the pgrep stub, because that is
# where km asks — but the window list is what it hangs off, so new-window and
# kill-window have to move it or a respawn would fix nothing and still pass.
#
# And a FOURTH: what the pane SAYS. `km status` cannot tell a working seat from
# one frozen at a modal or finished at an empty prompt, and the pane is the only
# place that difference is written down (gqlc-5vp7). So this stub keeps a pane
# per seat whose last line is the prompt, and send-keys edits it the way a TUI
# would — which is also what lets a row express the paste-swallowed Enter that
# dropped six nudges while reporting success on every one.
_wins="${KM_STATE_DIR:-}/fake-windows"
_seat_of_target() { # <args after the subcommand> -> the seat named by -t
    # The ARGUMENT AFTER -t, not "the last thing with a colon in it": a nudge's
    # text is an argument too, and a stub that scanned for a colon would start
    # reading the message as the address.
    local a prev="" t=""
    for a in "$@"; do
        [ "$prev" = "-t" ] && { t="$a"; break; }
        prev="$a"
    done
    t="${t##*:}"
    printf '%s' "${t#=}"
}
_pane_of() { printf '%s' "${KM_STATE_DIR:-}/fake-pane-$1"; }
_prompt_set() { # <seat> <text after the prompt, or empty>
    local f; f=$(_pane_of "$1")
    [ -f "$f" ] || return 0
    # The prompt is the last line, which is where a TUI puts it.
    sed -i '$d' "$f"
    if [ -n "$2" ]; then printf '❯ %s\n' "$2" >>"$f"; else printf '❯\n' >>"$f"; fi
}
case "$1" in
    has-session) exit 0 ;;
    new-window)
        _n=""; _prev=""
        for _a in "$@"; do [ "$_prev" = "-n" ] && _n="$_a"; _prev="$_a"; done
        [ -n "$_n" ] || exit 1
        # Real tmux would start the command; here what matters downstream is
        # that the window exists again AND that its runner is no longer dead,
        # since a respawn that left the seat on the dead list would let a row
        # assert recovery that never happened.
        echo "$_n" >>"$_wins"
        if [ -f "${KM_STATE_DIR:-}/fake-dead-runners" ]; then
            grep -vx "$_n" "${KM_STATE_DIR}/fake-dead-runners" >"${KM_STATE_DIR}/fake-dead-runners.tmp" || true
            mv "${KM_STATE_DIR}/fake-dead-runners.tmp" "${KM_STATE_DIR}/fake-dead-runners"
        fi
        [ -z "${KM_NEWWINDOW_LOG:-}" ] || printf '%s\n' "$_n" >>"$KM_NEWWINDOW_LOG"
        exit 0 ;;
    kill-window)
        shift
        _k=$(_seat_of_target "$@")
        [ -n "$_k" ] || exit 0
        if [ -f "$_wins" ]; then
            grep -vx "$_k" "$_wins" >"$_wins.tmp" || true
            mv "$_wins.tmp" "$_wins"
        fi
        [ -z "${KM_KILLWINDOW_LOG:-}" ] || printf '%s\n' "$_k" >>"$KM_KILLWINDOW_LOG"
        exit 0 ;;
    capture-pane)
        shift
        _c=$(_seat_of_target "$@")
        _f=$(_pane_of "$_c")
        [ -f "$_f" ] || exit 1
        cat "$_f"
        exit 0 ;;
    list-windows)
        [ -f "$_wins" ] && cat "$_wins"
        exit 0 ;;
    list-panes)
        _seat=""
        for _a in "$@"; do
            case "$_a" in *:*) _seat="${_a##*:}" ;; esac
        done
        # Real tmux resolves a window target EXACTLY only when the component is
        # prefixed with '='; bare, it falls back to a PREFIX match. Verified on
        # tmux 3.7c: with only a window named artur, `-t "=sess:ar"` returns
        # artur's pane and exits 0. The roster makes that live — `ar` prefixes
        # artur, arpine, aregak and aramazd — so a stub that always matched
        # exactly would encode a belief tmux does not hold, and would hide the
        # one seat whose liveness could be read off another seat's session.
        case "$_seat" in
            =*) _exact=1; _seat="${_seat#=}" ;;
            *)  _exact=0 ;;
        esac
        [ -f "$_wins" ] || exit 1
        if grep -qx "$_seat" "$_wins"; then
            echo "/dev/fake/$_seat"
            exit 0
        fi
        if [ "$_exact" -eq 0 ]; then
            _hit=$(grep -m1 "^$_seat" "$_wins" 2>/dev/null) || true
            if [ -n "${_hit:-}" ]; then
                echo "/dev/fake/$_hit"
                exit 0
            fi
        fi
        exit 1 ;;
    send-keys)
        # Record argv so a row can assert HOW the keys were sent: one
        # invocation per line, arguments tab-separated. Bundling the text and
        # the Enter into a single call is what strands the text (gqlc-s16s).
        if [ -n "${KM_SENDKEYS_LOG:-}" ]; then
            printf '%s\n' "$(printf '%s\t' "${@:2}")" >>"$KM_SENDKEYS_LOG"
        fi
        shift
        _t=$(_seat_of_target "$@")
        # The keystroke, which is the last argument.
        _k="${!#}"
        case "$_k" in
            C-u)   _prompt_set "$_t" "" ;;
            Enter)
                # A seat on the sticky list never submits: this is the TUI that
                # read the burst as a paste and took the CR as literal text, so
                # the line sits typed and unsent while every send reports
                # success (gqlc-5vp7 addendum, measured on six seats).
                if [ -f "${KM_STATE_DIR:-}/fake-sticky-prompt" ] &&
                    grep -qx "$_t" "${KM_STATE_DIR}/fake-sticky-prompt"; then :
                else _prompt_set "$_t" ""; fi ;;
            *)     _prompt_set "$_t" "$_k" ;;
        esac
        exit 0 ;;
esac
exit 0
STUB

cat >"$BIN/pgrep" <<'STUB'
#!/usr/bin/env bash
# `pgrep -t <tty> -x claude` over the synthetic ttys the tmux stub hands out. A
# seat's session is alive iff $KM_STATE_DIR/fake-claude-ttys names its tty.
#
# km passes the tty with its /dev/ prefix STRIPPED, which is what pgrep wants,
# so the names recorded here are stripped too. A tty outside fake/ is not ours:
# defer to the real pgrep, so stubbing this does not quietly create a world in
# which no process anywhere exists.
_tty=""
_prev=""
for _a in "$@"; do
    [ "$_prev" = "-t" ] && _tty="$_a"
    _prev="$_a"
done
case "$_tty" in
    fake/*) ;;
    *) exec /usr/bin/pgrep "$@" ;;
esac

# The second query on the same tty: `-f "km-seat <seat>$"`, asking whether the
# RUNNER is still parked there (gqlc-6ha4). It is a different process from the
# claude above and the two disagree in the ordinary case — a healthy asleep seat
# has a runner and no claude — so this arm reads a fixture of its own.
#
# A window's command IS km-seat, so the DEFAULT is that a window has a runner;
# what a row states is the anomaly, by naming a seat on fake-dead-runners. And
# the pattern is really matched against a cmdline rather than assumed, so a km
# that stopped anchoring it does not pass here for free.
_pat=""
_prev=""
for _a in "$@"; do
    [ "$_prev" = "-f" ] && _pat="$_a"
    _prev="$_a"
done
if [ -n "$_pat" ]; then
    _seat="${_tty#fake/}"
    _dead="${KM_STATE_DIR:-}/fake-dead-runners"
    if [ -f "$_dead" ] && grep -qx "$_seat" "$_dead"; then exit 1; fi
    if printf '%s' "/fake/kingdom/bin/km-seat $_seat" | grep -Eq "$_pat"; then
        echo 4242
        exit 0
    fi
    exit 1
fi

_live="${KM_STATE_DIR:-}/fake-claude-ttys"
if [ -f "$_live" ] && grep -qx "$_tty" "$_live"; then
    echo 4242
    exit 0
fi
exit 1
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
    list)  limit=50
           case "$_all" in
               *"--status in_progress"*) f="${KM_FAKE_INPROG:-}" ;;
               # doctor's identity arm (gqlc-0rv9). Defaulted to an EMPTY board
               # rather than to the "no fixture named" refusal, because every
               # row written before this arm existed would otherwise redden on
               # a query it says nothing about. A row that wants the arm to
               # fire sets KM_FAKE_OPEN itself; the rows that do are below and
               # they assert the exit status, so the arm is pinned by them and
               # not by this default.
               *"--status open"*) f="${KM_FAKE_OPEN:-$KM_EMPTY_BOARD}" ;;
               # The patrol bound's query (ADR 0004 §2). It asks --status ALL
               # on purpose — bd's `--status open` excludes in_progress and
               # blocked, so a claimed patrol bead would read as absent — and
               # the stub answers it from its own fixture so a row can put a
               # patrol bead on the board in any status it likes. An empty
               # board is the default for the same reason as the arm above:
               # every row written before patrol existed says nothing about it.
               *"--status all"*) f="${KM_FAKE_ALL:-$KM_EMPTY_BOARD}" ;;
               *) echo "bd stub: unexpected list query: $_all" >&2; exit 1 ;;
           esac ;;
    # bd WRITES, which no other stubbed query does. The whole invocation is
    # recorded rather than parsed, so a row can assert the priority, the labels
    # and the ABSENCE of an assignee against the bytes km actually passed —
    # a stub that decoded them into a tidy record could not witness a flag km
    # never wrote. A sidecar rc makes the refusal path expressible.
    # Newlines are squashed so the record is ONE LINE PER INVOCATION: the -d
    # body is a multi-line brief, and a row counting beads filed would
    # otherwise count the paragraphs of a single one.
    create) printf '%s\n' "${_all//$'\n'/ }" >>"${KM_STATE_DIR:-/dev/null}/bd-created"
            [ -z "${KM_FAKE_CREATE_RC:-}" ] || exit "$KM_FAKE_CREATE_RC"
            echo "created: gqlc-fake1"
            exit 0 ;;
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
    # The EXPLICIT-ID query, and it serves two call sites: the hold verdict's
    # parent statuses and the resume pass's blocker join. Three properties, all
    # different from ready/list and all load-bearing:
    #
    #   no cap is modelled — an explicit-id query has no renderer window to
    #     fall past, which is the whole reason the blocker join is done this
    #     way rather than against `bd blocked` (which has no -n flag at all, so
    #     its overflow behaviour is unmeasurable and would fail OPEN) or
    #     against list's dependency_count (reads 0 beside an open blocker;
    #     measured 2026-08-21). The asymmetry with ready/list above is
    #     deliberate, not an omission.
    #   the ids FILTER the answer, as real bd does. One fixture can then be the
    #     union both call sites read, and a caller that asked for the wrong ids
    #     gets nothing rather than getting the other caller's payload.
    #   ZERO ids print an error OBJECT on STDOUT. Real bd exits 1 with it
    #     (re-measured 2026-08-23: `{"error": "at least one issue ID is
    #     required (use positional args, --id flag, or --current)",
    #     "schema_version": 1}`); an earlier note here said exit 0 and was
    #     wrong. The stub keeps exit 0 ON PURPOSE, as the strictly HARDER case:
    #     with rc=1 the caller's pipefail catches the mistake for free, so the
    #     `[ -n "$ids" ]` guard could be deleted and no row would notice. At
    #     exit 0 the failure rides in on stdout only, and killing the guard
    #     means killing it against the shape it actually has to survive. km is
    #     safe under BOTH: the guard prevents the call, `.[]` aborts jq on an
    #     object, and pipefail sees the rc.
    show)  f="${KM_FAKE_SHOW:-}"; limit=0; _show=1 ;;
    *) exit 0 ;;
esac
shift
_ids=""
while [ $# -gt 0 ]; do
    case "$1" in
        -n|--limit) limit="${2:-}"; shift ;;
        -*) ;;
        *) _ids="$_ids $1" ;;
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
if [ "${_show:-0}" = 1 ]; then
    if [ -z "${_ids//[[:space:]]/}" ]; then
        echo '{"error":"no issue ids supplied"}'
        exit 0
    fi
    if jq -e . "$f" >/dev/null 2>&1; then
        jq -c --argjson ids \
            "$(printf '%s\n' $_ids | jq -Rsc 'split("\n") | map(select(length > 0))')" \
            'map(select(.id as $i | $ids | index($i)))' "$f"
    else
        # Malformed fixtures flow through untouched, as they do above, so the
        # rows that redden the jq half of the pipeline still can.
        cat "$f"
    fi
    exit 0
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
chmod +x "$BIN/tmux" "$BIN/bd" "$BIN/pgrep"

DCASE=0
dispatch_case() { # $1=ready, $2=in-progress, $3=dep edges, $4=parent statuses, $5=bd show
    DCASE=$((DCASE + 1))
    export KM_STATE_DIR="$TMP/dispatch-$DCASE"
    mkdir -p "$KM_STATE_DIR"
    export KM_FAKE_READY="$KM_STATE_DIR/ready.json"
    export KM_FAKE_INPROG="$KM_STATE_DIR/inprog.json"
    export KM_FAKE_DEPS="$KM_STATE_DIR/deps.json"
    export KM_FAKE_SHOW="$KM_STATE_DIR/show.json"
    printf '%s' "$1" >"$KM_FAKE_READY"
    printf '%s' "$2" >"$KM_FAKE_INPROG"
    # An empty edge set is the shape almost every bead has, so it is the default
    # rather than something each case restates.
    printf '%s' "${3:-[]}" >"$KM_FAKE_DEPS"
    # ONE `bd show` fixture for both explicit-id call sites — the hold verdict's
    # parents ($4) and the resume pass's blocker join — because real bd answers
    # both from one table and the stub filters by the ids each caller asks for.
    #
    # The default derives an ALL-UNBLOCKED payload from the in-progress fixture,
    # which is what lets every row written before the blocker join existed pass
    # unchanged through the new code path. That preservation is the assertion:
    # an assigned in_progress bead with no blocker resumes exactly as it did.
    # A row that wants a blocker passes $5 and states the whole union itself.
    if [ -n "${5:-}" ]; then
        printf '%s' "$5" >"$KM_FAKE_SHOW"
    else
        {
            # A fixture that does not parse is left out rather than repaired:
            # those rows redden the query BEFORE the show call is reached, and
            # inventing JSON for them would change what they mean.
            if jq -e . <<<"$2" >/dev/null 2>&1
            then jq -c 'map({id, assignee, dependencies: []})' <<<"$2"
            else echo '[]'; fi
            if jq -e . <<<"${4:-[]}" >/dev/null 2>&1
            then printf '%s\n' "${4:-[]}"
            else echo '[]'; fi
        } | jq -sc 'add' >"$KM_FAKE_SHOW"
    fi
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

# A seat that spends a slot. `awake` in the status file is only half of it: the
# other half is a session that actually exists, and the two can disagree, which
# is the whole of gqlc-s16s. fill_cap builds the HONEST shape — status, window,
# and a claude on the pane — so a row that wants a slot spent gets one for the
# right reason. A queued `wake` file (as the priority row above uses) is a
# different state again and does NOT count, so the three cannot stand in for
# each other.
seat_window()  { local s; for s in "$@"; do echo "$s" >>"$KM_STATE_DIR/fake-windows"; done; }
seat_claude()  { local s; for s in "$@"; do echo "fake/$s" >>"$KM_STATE_DIR/fake-claude-ttys"; done; }
seat_state()   { mkdir -p "$KM_STATE_DIR/seats/$1"; echo "$2" >"$KM_STATE_DIR/seats/$1/status"; }

fill_cap() { local s; for s in "$@"; do seat_state "$s" awake; seat_window "$s"; seat_claude "$s"; done; }

# The runner — km-seat, the pane's own command and the only reader of a wake
# file. A window implies one unless a row says otherwise, which is what
# seat_runner_dead says (gqlc-6ha4).
seat_runner_dead() { local s; for s in "$@"; do echo "$s" >>"$KM_STATE_DIR/fake-dead-runners"; done; }

# What the pane SAYS. The three shapes the town has actually measured:
#   working  — claude's spinner line, which carries "esc to interrupt" for the
#              whole of a turn
#   idle     — a prompt with nothing typed after it (six seats, eleven hours)
#   modal    — no prompt line at all, because the modal covers the box (two
#              judges, 80 and 152 minutes)
# The prompt is the LAST line, which is where the send-keys arm of the tmux stub
# edits it.
#
# working KEEPS the empty prompt line. That is what the TUI actually renders —
# the input box stays below the spinner all turn, so you can type ahead — and
# it is the whole reason the spinner test has to exist. A first version of this
# fixture dropped the prompt line, and the empty-prompt half of seat_pane_idle
# rejected it on its own; deleting the spinner test outright then left the suite
# green (mutation M7). A fixture that a guard does not need is a guard that is
# not tested.
pane_working() { printf 'Reading kingdom/bin/km\n✳ Thinking… (41s · esc to interrupt)\n\n❯\n' >"$KM_STATE_DIR/fake-pane-$1"; }
pane_idle()    { printf 'I have pushed the branch and written my handoff.\n\n❯\n' >"$KM_STATE_DIR/fake-pane-$1"; }
pane_modal()   { printf 'Background work is running\n  Exit anyway\n> Stay\n' >"$KM_STATE_DIR/fake-pane-$1"; }
pane_of()      { cat "$KM_STATE_DIR/fake-pane-$1" 2>/dev/null; }

# A TUI that swallows the Enter after a burst it read as a paste.
pane_sticky()  { local s; for s in "$@"; do echo "$s" >>"$KM_STATE_DIR/fake-sticky-prompt"; done; }

# The statusline's heartbeat, at a chosen age. `updated` is what km reads —
# never the mtime, so a row can age a heartbeat without touching the clock.
seat_heartbeat() { # <seat> <seconds old> [context_pct]
    mkdir -p "$KM_STATE_DIR/seats/$1"
    jq -cn --arg s "$1" --arg u "$(date -u -d "@$(($(date +%s) - $2))" +%Y-%m-%dT%H:%M:%SZ)" \
        --argjson p "${3:-42}" '{seat: $s, context_pct: $p, updated: $u}' \
        >"$KM_STATE_DIR/seats/$1/heartbeat.json"
}

# The three broken shapes, each a slot the town is spending on nothing.
#   windowless: status says awake, the tmux window is GONE  (witnessed live on
#               `ayg`, 2026-08-22T05:35Z, still counted against max_active)
#   zombie:     window survives, no claude on its tty
#   pending:    the citizen ran `km sleep`, the /exit never arrived, so the
#               session is alive under a status that says it left (gqlc-0gjt)
fill_windowless() { local s; for s in "$@"; do seat_state "$s" awake; done; }
fill_zombie()     { local s; for s in "$@"; do seat_state "$s" awake; seat_window "$s"; done; }
fill_pending()    { local s; for s in "$@"; do seat_state "$s" asleep-pending; seat_window "$s"; seat_claude "$s"; done; }

# The cron leaves two minutes between a suspicion and the pass that confirms it;
# a suite running reconcile twice in one second leaves none, and reconcile
# declines to reap a sighting that young — a seat mid-startup looks the same.
# Rows that want the CONFIRMED outcome age the marker the way the cadence would.
age_suspicion() { local s; for s in "$@"; do touch -d '2 minutes ago' "$KM_STATE_DIR/seats/$s/suspect"; done; }

# The cap rows below run against their OWN config. They used to name five seats
# by hand and assert the literal 5, so raising the town's concurrency reddened
# four of them on a config edit that touched no code (gqlc-p5s8). Constitution
# V.6.1 says these numbers change by a config edit, and that is only true if
# nothing else pins them.
#
# The fixture is the live file with the one line rewritten, so the roster and
# the classes are still the town's own — what is decoupled is the NUMBER. Both
# the capped set and each seat's class are asked of km rather than re-derived
# here, so the suite holds no second copy of either rule to go stale.
CAP=0
CAPPED_ARCH=""
CAPPED_WAR=""
cap_config() { # $1 = max_active the fixture should carry
    sed "s/^max_active = .*/max_active = $1/" "$REPO/kingdom/kingdom.toml" >"$TMP/cap.toml"
    export KM_CONFIG="$TMP/cap.toml"
    CAP="$("$KM" cfg concurrency max_active)"
    [ "$CAP" = "$1" ] || bad "the cap fixture takes effect" "asked for max_active=$1, km read '$CAP'"
    CAPPED_ARCH=""
    CAPPED_WAR=""
    local s
    for s in $("$KM" capped-seats); do
        case "$("$KM" seat-info "$s" | cut -d' ' -f1)" in
            architect) CAPPED_ARCH="$CAPPED_ARCH$s " ;;
            warrior)   CAPPED_WAR="$CAPPED_WAR$s " ;;
        esac
    done
}

# $1 = how many capped slots to leave FREE. Any further arguments are seats to
# wake that the cap does not count (the judge).
#
# Fills through fill_cap, so the seats it leaves behind are the same HONEST
# shape every other row spends a slot with — status, window, and a claude on the
# pane. A status file alone spends no slot since gqlc-s16s, so a filler that
# wrote only the status would leave the cap open and every row below vacuous.
#
# Architect seats are filled first on purpose: every row here routes WARRIOR
# work, so the slots left free must be warrior slots or the row proves nothing.
# FREE_SEAT names the first warrior left asleep — the seat a free slot would
# actually route to. Rows assert against it instead of a hardcoded name, so
# neither a roster change nor a cap change can silently move the target and
# leave the row passing for the wrong reason.
FREE_SEAT=""
fill_cap_leaving() {
    local free="$1" want woke=" " n=0 s
    shift
    want=$((CAP - free))
    FREE_SEAT=""
    for s in $CAPPED_ARCH $CAPPED_WAR; do
        [ "$n" -ge "$want" ] && break
        fill_cap "$s"
        woke="$woke$s "
        n=$((n + 1))
    done
    [ "$n" -eq "$want" ] ||
        bad "fill_cap_leaving fills the cap" "wanted $want capped seats awake, the roster offered only $n"
    for s in $CAPPED_WAR; do
        case "$woke" in *" $s "*) ;; *) FREE_SEAT="$s"; break ;; esac
    done
    for s in "$@"; do fill_cap "$s"; done
}

# The fresh pass: a bead of each class reaches a free seat of that class, and
# the unlabelled one reaches a WARRIOR by inference (gqlc-38ye). This row is also
# the liveness control for every row below it — it is the only one that proves
# the stubs, the tmux seam and the wake-file path all work, so a "nobody was
# woken" assertion elsewhere means the dispatcher declined rather than that the
# harness was inert.
#
# Every bead here is at or above [dispatch] max_priority on purpose. gqlc-j1 was
# a P3 until the floor existed, and under the floor it stopped being routed —
# which would have turned this liveness control into a row asserting that a
# judge bead reaches nobody. The floor gets rows of its own below.
#
# gqlc-unl is P2, not the P0 it was written as: since an unlabelled bead now
# routes as a warrior, a P0 one would sort AHEAD of gqlc-w1 and take the seat
# this row names for it, so the row would redden on seat identity rather than on
# the property it is here for. Behind gqlc-w1 it witnesses the inference without
# disturbing the assertion above it.
dispatch_case '[
  {"id":"gqlc-unl","priority":2,"assignee":null,"labels":null},
  {"id":"gqlc-taken","priority":0,"assignee":"ar","labels":["class:warrior"]},
  {"id":"gqlc-w1","priority":1,"assignee":null,"labels":["class:warrior"]},
  {"id":"gqlc-a1","priority":2,"assignee":null,"labels":["area:parser","class:architect"]},
  {"id":"gqlc-j1","priority":2,"assignee":null,"labels":["class:judge"]}
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
elif ! grep -rq 'gqlc-unl' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "the fresh pass routes a bead of each class" "the unlabelled bead reached nobody instead of being inferred to a warrior (woken: $(woken_seats)) out=$OUT"
elif ! grep -rl 'gqlc-unl' "$KM_STATE_DIR/seats" 2>/dev/null | grep -qE '/(aramazd|vahagn|astghik|ar|nvard|ayg|tsovinar|hayk)/'; then
    bad "the fresh pass routes a bead of each class" "the unlabelled bead went somewhere that is not a warrior seat: $(grep -rl 'gqlc-unl' "$KM_STATE_DIR/seats" 2>/dev/null | tr '\n' ' ')"
elif ! wake_of ar | grep -q 'gqlc-taken'; then
    bad "the fresh pass routes a bead of each class" "the assigned bead did not reach its own assignee: $(woken_seats)"
elif grep -rl 'gqlc-taken' "$KM_STATE_DIR/seats" 2>/dev/null | grep -qv '/ar/'; then
    bad "the fresh pass routes a bead of each class" "a bead ար already holds was handed to somebody else: $(grep -rl 'gqlc-taken' "$KM_STATE_DIR/seats" 2>/dev/null | tr '\n' ' ')"
else
    ok "the fresh pass routes architect, warrior and judge beads to free seats of their class, routes the unlabelled bead to a warrior by inference, and sends the assigned one to its own assignee rather than to a stranger"
fi

# A queue of nothing but unlabelled beads MOVES (gqlc-38ye). It used to be
# Սեդրակ's chore and reached nobody until he labelled it; that made one seat a
# mandatory step in front of every bead in the town, and 25 of 208 unassigned
# open beads — one P0, three P1 — were sitting behind it, invisible and silent
# about being invisible. Three shapes of absent label are here on purpose: null,
# the empty array, and a labelled bead whose labels name no class. All three read
# `.cls == null` and all three must route.
#
# It is also the row that pins the corrected account of gqlc-z1qw: the type error
# fired on unassigned beads of ANY labelling, so under the bug this payload
# aborts jq rather than passing quietly.
dispatch_case '[
  {"id":"gqlc-u1","priority":0,"assignee":null,"labels":null},
  {"id":"gqlc-u2","priority":1,"assignee":null,"labels":[]},
  {"id":"gqlc-u3","priority":2,"assignee":null,"labels":["area:parser"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an all-unlabelled queue still moves" "rc=$RC out=$OUT"
elif [ -z "$(woken_seats)" ]; then
    bad "an all-unlabelled queue still moves" "nobody was woken at all: $OUT"
else
    _missed=""
    for _b in gqlc-u1 gqlc-u2 gqlc-u3; do
        grep -rq "$_b" "$KM_STATE_DIR/seats" 2>/dev/null || _missed="$_missed $_b"
    done
    if [ -n "$_missed" ]; then
        bad "an all-unlabelled queue still moves" "these reached nobody:$_missed (woken: $(woken_seats)) out=$OUT"
    elif ! printf '%s' "$OUT" | grep -q 'done'; then
        bad "an all-unlabelled queue still moves" "the run did not report itself done: $OUT"
    else
        ok "a queue of only unlabelled beads — null labels, empty labels, and labels naming no class — routes all three rather than stranding them behind one seat's chore"
    fi
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

# --- the owned pass: a bead that names a seat goes to that seat (gqlc-xq8a) ---
# The two passes above do not cover the board between them. The resume pass
# reads `--status in_progress`; the fresh pass selects `.assignee == null`. A
# bead that is ASSIGNED but still OPEN satisfies neither, so it sits on the
# board looking owned and routes to nobody, at any priority including P0, with
# nothing reporting the omission. That is why it survived: to a human reading
# the board, "assigned" says someone has it.
#
# The rule these rows pin is the resume pass's rule widened to every status: a
# bead that names a seat is that seat's, and labels do not decide it. So the
# owned bead below carries NO class label and must still route.
dispatch_case '[
  {"id":"gqlc-own","priority":1,"assignee":"ar","labels":null},
  {"id":"gqlc-ctl","priority":2,"assignee":null,"labels":["class:warrior"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a ready bead assigned to a seat reaches that seat" "rc=$RC out=$OUT"
elif ! wake_of aramazd | grep -q 'bead:gqlc-ctl'; then
    bad "a ready bead assigned to a seat reaches that seat" "the control bead did not route, so this row proves nothing (woken: $(woken_seats)) out=$OUT"
elif ! wake_of ar | grep -q 'gqlc-own'; then
    bad "a ready bead assigned to a seat reaches that seat" "ար was not woken for the bead assigned to him (woken: $(woken_seats)) out=$OUT"
elif wake_of ar | grep -q 'resume your in-progress work'; then
    bad "a ready bead assigned to a seat reaches that seat" "an open bead was announced as work already in progress: $(wake_of ar)"
elif ! wake_of ar | grep -q 'claim'; then
    bad "a ready bead assigned to a seat reaches that seat" "the wake does not tell him to claim it first: $(wake_of ar)"
elif [ "$(grep -rl 'gqlc-own' "$KM_STATE_DIR/seats" 2>/dev/null | wc -l)" -ne 1 ]; then
    bad "a ready bead assigned to a seat reaches that seat" "the owned bead reached more than its owner: $(grep -rl 'gqlc-own' "$KM_STATE_DIR/seats" 2>/dev/null | tr '\n' ' ')"
else
    ok "a ready bead assigned to a seat wakes that seat and no other, is announced as unclaimed rather than in progress, and needs no class label to route"
fi

# The other half of the same rule, and the reason the fresh pass filtered
# assigned beads out in the first place: an assigned bead must not be handed to
# a free seat of its class as though it were unclaimed.
dispatch_case '[
  {"id":"gqlc-mine","priority":0,"assignee":"hayk","labels":["class:warrior"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an assigned bead is not re-routed to a stranger" "rc=$RC out=$OUT"
elif ! wake_of hayk | grep -q 'gqlc-mine'; then
    bad "an assigned bead is not re-routed to a stranger" "its owner was not woken, so the row cannot see a re-route (woken: $(woken_seats)) out=$OUT"
elif [ "$(woken_seats)" != "hayk " ]; then
    bad "an assigned bead is not re-routed to a stranger" "seats other than the owner were woken: $(woken_seats)"
else
    ok "a ready bead assigned to one warrior wakes only that warrior, and is not offered to the other free warriors as fresh work"
fi

# The seat that is in BOTH lists, which is a state this branch creates: before
# it, one pass flowed through route_owners; now two do, in one run. The guard is
# the wake-file skip, and until this row its behaviour lived in a comment — the
# exact shape this branch exists to end. Delete the skip and the suite stays
# green — 63/63 where Անահիտ found it (#1237 round 1, head 5e6c3f34), 118/0
# where I reproduced it after the rebase (e7872b4b).
#
# Both halves of the guard are asserted, because they fail differently. WITHIN a
# run, dropping the skip wakes Հայկ twice and spends two capped slots for one
# seat. ACROSS runs it compounds: cmd_wake APPENDS, and a seat that has been
# woken but has not yet risen is still `asleep`, so every tick re-wakes it and
# re-charges a slot — silent under-routing, which is the defect class of this
# whole branch. The second run therefore resets nothing on purpose.
dispatch_case '[
  {"id":"gqlc-both-open","priority":0,"assignee":"hayk","labels":null}
]' '[
  {"id":"gqlc-both-ip","assignee":"hayk","labels":null}
]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a seat in both lists is woken once, for the in-progress half" "rc=$RC out=$OUT"
elif [ "$(wake_of hayk | wc -l)" -ne 1 ]; then
    bad "a seat in both lists is woken once, for the in-progress half" \
        "$(wake_of hayk | wc -l) wake line(s), so both passes reached him in one run: $(wake_of hayk)"
elif ! wake_of hayk | grep -q 'resume your in-progress work.*gqlc-both-ip'; then
    bad "a seat in both lists is woken once, for the in-progress half" \
        "the single wake is not the resume one, so the passes ran in the wrong order: $(wake_of hayk)"
elif wake_of hayk | grep -q 'gqlc-both-open'; then
    bad "a seat in both lists is woken once, for the in-progress half" \
        "the owned half was announced too, which is the double wake this guard exists to prevent: $(wake_of hayk)"
else
    # Deliberately NOT reset: a queued wake outlives the run that made it, and
    # the seat is still asleep, so this is what the next timer tick actually
    # meets.
    run_dispatch
    if [ "$(wake_of hayk | wc -l)" -ne 1 ]; then
        bad "a queued wake is not re-queued on the next tick" \
            "the wake file grew to $(wake_of hayk | wc -l) lines across two runs, so each tick re-wakes and re-charges a slot: $(wake_of hayk)"
    else
        ok "a seat holding in-progress work and assigned an unclaimed bead is woken once, for the in-progress half, and a second dispatch over the same unreset state neither appends a second wake nor spends a second slot"
    fi
fi

# A ready bead assigned to a human is nobody's to route — the same rule the
# in-progress row above pins, on the status the fresh pass sees. The one live
# instance of this shape on the board today is exactly this: gqlc-do1, open and
# assigned to antranig-yeretzian.
dispatch_case '[
  {"id":"gqlc-hum","priority":0,"assignee":"antranig-yeretzian","labels":["class:warrior"]},
  {"id":"gqlc-ctl2","priority":1,"assignee":null,"labels":["class:warrior"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a human's ready bead wakes no seat" "rc=$RC out=$OUT"
elif ! wake_of aramazd | grep -q 'bead:gqlc-ctl2'; then
    bad "a human's ready bead wakes no seat" "the control bead did not route, so this row proves nothing (woken: $(woken_seats)) out=$OUT"
elif grep -rq 'gqlc-hum' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "a human's ready bead wakes no seat" "the human's bead was routed to a seat: $(woken_seats)"
else
    ok "a ready bead assigned to a human wakes no seat, while an unassigned bead in the same run still routes"
fi

# The owned pass is capped work like any other. հայկ is the assignee and is
# asleep; the capped seats awake around him are what must hold him back, and the
# printed cap line is the control proving the cap engaged.
#
# These two rows run against the cap fixture for the same reason the section
# further down does: naming a fixed number of seats by hand asserts the town's
# live max_active, so tuning it reddens a row about the owned pass (gqlc-p5s8).
cap_config 7
dispatch_case '[
  {"id":"gqlc-owncap","priority":0,"assignee":"hayk","labels":["class:warrior"]}
]' '[]'
fill_cap_leaving 0
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an owned bead waits for a slot" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'no free slot'; then
    bad "an owned bead waits for a slot" "the cap never engaged, so this row proves nothing: $OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "an owned bead waits for a slot" "it woke: $(woken_seats) out=$OUT"
else
    ok "a ready bead assigned to a sleeping warrior does not wake him while every capped slot is held"
fi

# ...and the judge's exemption reaches it too, for the same reason it reaches
# the other two passes: the town's merge gate must not be held shut by the
# town's own throughput (gqlc-dz85).
dispatch_case '[
  {"id":"gqlc-ownjudge","priority":0,"assignee":"mihr","labels":null}
]' '[]'
fill_cap_leaving 0
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an owned judge bead is exempt from the cap" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'no free slot'; then
    bad "an owned judge bead is exempt from the cap" "the cap never engaged, so this row proves nothing: $OUT"
elif ! wake_of mihr | grep -q 'gqlc-ownjudge'; then
    bad "an owned judge bead is exempt from the cap" "the judge was not woken for his own assigned bead (woken: $(woken_seats)) out=$OUT"
else
    ok "a ready bead assigned to the judge wakes him at a full cap, as his in-progress beads already do"
fi
unset KM_CONFIG

# --- the state no pass can reach, named rather than left to be found ---------
# Closing the assigned-and-open hole leaves one shape that no pass can wake
# anyone for: in_progress with no assignee. The resume pass wants a non-null
# assignee and the fresh pass wants status open, and `bd ready` returns open
# beads only — so nothing returns it to circulation. It is reachable by hand:
# unassigning a bead a seat had already claimed does exactly this, and it has
# happened here (gqlc-mro7), done by someone trying to make the bead MORE
# routable.
#
# This is the third distinct way a bead has been ready, labelled and invisible,
# after pre-assigned review beads and assigned-and-open ones. The point of the
# report is that the fourth is found by the report and not by accident.
dispatch_case '[
  {"id":"gqlc-live","priority":0,"assignee":null,"labels":["class:warrior"]}
]' '[
  {"id":"gqlc-orphan","assignee":null,"labels":["class:warrior"]}
]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the dispatcher names a bead no pass can reach" "rc=$RC out=$OUT"
elif ! wake_of aramazd | grep -q 'bead:gqlc-live'; then
    bad "the dispatcher names a bead no pass can reach" "the control bead did not route, so this row proves nothing (woken: $(woken_seats)) out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'gqlc-orphan'; then
    bad "the dispatcher names a bead no pass can reach" "the orphaned bead was not named in the run's output: $OUT"
elif grep -rq 'gqlc-orphan' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "the dispatcher names a bead no pass can reach" "it was routed to a seat instead of reported: $(woken_seats)"
else
    ok "an in-progress bead with no assignee is named in the dispatch run's output rather than passing silently, and is not routed to a stranger"
fi

# A healthy board must not print the warning, or the line stops meaning
# anything and the next real one is read as noise.
dispatch_case '[
  {"id":"gqlc-live2","priority":0,"assignee":null,"labels":["class:warrior"]}
]' '[
  {"id":"gqlc-held","assignee":"vahagn","labels":["class:warrior"]}
]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a healthy board prints no stranding warning" "rc=$RC out=$OUT"
elif ! wake_of vahagn | grep -q 'gqlc-held'; then
    bad "a healthy board prints no stranding warning" "the resume wake did not happen, so the board was not the healthy one: $OUT"
elif printf '%s' "$OUT" | grep -qi 'stranded'; then
    bad "a healthy board prints no stranding warning" "it warned about a board where every bead is in a pass: $OUT"
else
    ok "a board whose beads are all in some pass draws no stranding warning"
fi

# Priority decides who gets the last seat. Every warrior but հայկ is given a
# queued wake, so there is exactly one free seat and the two beads are in real
# contention — without that there are eight free warriors, both beads route, and
# the ordering is unobservable.
#
# gqlc-lo is P2, not the P3 it was written as: under [dispatch] max_priority the
# loser would be withheld from the queue entirely, so the row would pass because
# there was only ever one candidate and would witness no ordering at all. Both
# beads must be routable for the contention to be real.
dispatch_case '[
  {"id":"gqlc-lo","priority":2,"assignee":null,"labels":["class:warrior"]},
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

# --- the priority floor: [dispatch] max_priority ------------------------------
# The town restarted with 158 of 319 open beads at P3 — mostly defect findings
# its own adversarial review produced faster than anyone could fix them. The
# fresh pass now declines them. What must NOT change is a citizen's right to
# finish their own work (Constitution III.3): the resume and owned passes ignore
# the floor entirely, so a seat already holding a P3 is never stranded by it.
#
# These rows run against the town's REAL kingdom.toml, like every dispatch row
# above, so the floor they test is the shipped one rather than a number invented
# here — and the fixture priorities are derived from it rather than hard-coded,
# so raising the floor retunes the rows instead of falsifying them.
FLOOR=$("$KM" cfg dispatch max_priority 2>/dev/null)
OVER=$((FLOOR + 1))
case "$FLOOR" in
    '' | *[!0-9]*)
        bad "[dispatch] max_priority is declared in the town's config" "read '$FLOOR'" ;;
    *)
        ok "[dispatch] max_priority is declared in the town's config, as a number" ;;
esac

# One case, both halves: the bead at the floor routes and the bead one below it
# does not. Same class, same run, adjacent priorities — so a dispatcher that had
# simply stopped routing warriors fails the first assertion, and one that
# ignored the floor fails the second. The floor bead is also the liveness
# control: without it, "gqlc-under reached nobody" is equally true of a harness
# that did nothing at all.
dispatch_case "[
  {\"id\":\"gqlc-atfloor\",\"priority\":$FLOOR,\"assignee\":null,\"labels\":[\"class:warrior\"]},
  {\"id\":\"gqlc-under\",\"priority\":$OVER,\"assignee\":null,\"labels\":[\"class:warrior\"]}
]" '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the fresh pass declines a bead below the floor" "rc=$RC out=$OUT"
elif ! woken_seats | grep -q .; then
    bad "the fresh pass declines a bead below the floor" "nobody was woken at all, so the harness proves nothing: $OUT"
elif ! grep -rq 'gqlc-atfloor' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "the fresh pass declines a bead below the floor" "the bead AT the floor was not routed either: $(woken_seats) out=$OUT"
elif grep -rq 'gqlc-under' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "the fresh pass declines a bead below the floor" \
        "a P$OVER bead was routed under a floor of $FLOOR: $(grep -rl 'gqlc-under' "$KM_STATE_DIR/seats" | tr '\n' ' ')"
else
    ok "the fresh pass routes a bead at max_priority and declines the one below it"
fi

# Declining SILENTLY is the failure this dispatcher has shipped three times
# (gqlc-z1qw, gqlc-fo48): a pass that routes nothing and prints a summary that
# reads healthy. So the refusal is asserted twice — named per bead, and counted
# in the one line an operator reads. Both halves, because a count with no id is
# unactionable and an id with no count is invisible in a long run.
if ! printf '%s' "$OUT" | grep -q 'gqlc-under'; then
    bad "a bead declined for priority is named out loud" "the run never mentions it: $OUT"
elif ! printf '%s' "$OUT" | grep -q "gqlc-under.*priority $OVER"; then
    bad "a bead declined for priority is named out loud" "it is mentioned without its priority: $OUT"
# The count is pinned exactly, not as "greater than zero": the queue held two
# beads and only one was declined, so a floor that counted everything it looked
# at — or counted the routed bead too — reads 2 here and fails.
elif ! printf '%s' "$OUT" | grep -qF ", 1 below max_priority $FLOOR,"; then
    bad "a bead declined for priority is counted in the summary" "the done line hides it: $(printf '%s' "$OUT" | grep 'done')"
else
    ok "the declined bead is named with its priority and counted in the done line"
fi

# Constitution III.3, the half the floor must not touch. A seat holding an
# in-progress P-below-the-floor bead gets it back, and a ready one that NAMES a
# seat still reaches that seat. Both passes run before the fresh one and neither
# consults max_priority; a floor applied to the queue as a whole rather than to
# the fresh arm alone would strand both, mid-work, with nothing said.
dispatch_case "[
  {\"id\":\"gqlc-ownlow\",\"priority\":$OVER,\"assignee\":\"ayg\",\"labels\":[\"class:warrior\"]}
]" "[
  {\"id\":\"gqlc-reslow\",\"priority\":$OVER,\"assignee\":\"vahagn\",\"labels\":[\"class:warrior\"]}
]"
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the floor does not strand a citizen's own work" "rc=$RC out=$OUT"
elif ! wake_of vahagn | grep -q 'gqlc-reslow'; then
    bad "the floor does not strand a citizen's own work" \
        "a P$OVER bead in progress never came back to the seat holding it (woken: $(woken_seats)) out=$OUT"
elif ! wake_of ayg | grep -q 'gqlc-ownlow'; then
    bad "the floor does not strand a citizen's own work" \
        "a ready P$OVER bead naming a seat never reached it (woken: $(woken_seats)) out=$OUT"
elif printf '%s' "$OUT" | grep -q 'below the floor'; then
    bad "the floor does not strand a citizen's own work" "it was applied to an owned bead: $OUT"
else
    ok "a bead below the floor still resumes to the seat holding it and still reaches the seat it names (III.3)"
fi

# The floor fails OPEN, and that choice is the whole of this row. A number the
# dispatcher cannot read is a typo in a config file; if it fell back to routing
# nothing, one bad character would stop the town while every board still read
# healthy — the exact shape of gqlc-z1qw and gqlc-fo48. Routing everything and
# saying why is loud, recoverable, and visible to an operator.
#
# The fixture is the REAL config with one line rewritten, not a minimal town, so
# the roster the rows above route against is the roster this one routes against.
badfloor="$TMP/badfloor.toml"
sed 's/^max_priority = .*/max_priority = "banana"/' "$REPO/kingdom/kingdom.toml" >"$badfloor"
if ! grep -q '^max_priority = "banana"' "$badfloor"; then
    bad "an unreadable max_priority routes everything" "the fixture rewrite missed, so this row would pass against the real floor"
else
    dispatch_case "[
      {\"id\":\"gqlc-badfloor\",\"priority\":$OVER,\"assignee\":null,\"labels\":[\"class:warrior\"]}
    ]" '[]'
    OUT="$(cd "$FIXTURE" && PATH="$BIN:$PATH" KM_CONFIG="$badfloor" "$KM" dispatch 2>&1)"
    RC=$?
    if [ "$RC" -ne 0 ]; then
        bad "an unreadable max_priority routes everything" "rc=$RC out=$OUT"
    elif ! grep -rq 'gqlc-badfloor' "$KM_STATE_DIR/seats" 2>/dev/null; then
        bad "an unreadable max_priority routes everything" \
            "it failed CLOSED — one typo halted routing (woken: $(woken_seats)) out=$OUT"
    elif ! printf '%s' "$OUT" | grep -q "banana"; then
        bad "an unreadable max_priority names the value it could not read" \
            "it failed open silently, so nobody can find the typo: $OUT"
    else
        ok "a malformed max_priority routes every priority and names the value it could not read"
    fi
fi

# --- the inferred class, and the floor that lets P3 through (gqlc-38ye) -------
# Owner's decision, 2026-08-23. Labelling stops being a gate and becomes an
# optimisation: an unassigned bead nobody labelled routes as class:warrior, and
# the warrior escalates under Constitution IV if the work is out of scope. The
# risk was stated and accepted; a bead nobody can see is the worse failure.
#
# The inference must be VISIBLE. A routed-by-default bead and a labelled one
# reaching the same seat are indistinguishable from the wake alone, so the run
# has to say which it was — the same argument as the lowpri arm, which emits
# rather than drops precisely so the withheld beads can be named. This row
# asserts BOTH halves, because a seat woken with nothing said is the silent
# shape, and a line printed for a bead that reached nobody is theatre.
dispatch_case '[
  {"id":"gqlc-noclass","priority":1,"assignee":null,"labels":["area:parser"]},
  {"id":"gqlc-said","priority":1,"assignee":null,"labels":["class:warrior"]}
]' '[]'
run_dispatch
_infseat="$(grep -rl 'gqlc-noclass' "$KM_STATE_DIR/seats" 2>/dev/null | sed 's|.*/seats/||; s|/wake$||')"
if [ "$RC" -ne 0 ]; then
    bad "an unlabelled bead routes as a warrior and says so" "rc=$RC out=$OUT"
elif ! grep -rq 'gqlc-said' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "an unlabelled bead routes as a warrior and says so" \
        "the labelled control reached nobody, so this row proves nothing: $OUT"
elif [ -z "$_infseat" ]; then
    bad "an unlabelled bead routes as a warrior and says so" \
        "the unlabelled bead reached nobody (woken: $(woken_seats)) out=$OUT"
elif [ "$("$KM" seat-info "$_infseat" | cut -d' ' -f1)" != warrior ]; then
    bad "an unlabelled bead routes as a warrior and says so" \
        "it went to $_infseat, class $("$KM" seat-info "$_infseat" | cut -d' ' -f1)"
elif ! printf '%s' "$OUT" | grep -q 'gqlc-noclass.*inferred\|inferred.*gqlc-noclass'; then
    bad "an unlabelled bead routes as a warrior and says so" \
        "the run routed it without naming the class as inferred, so an operator cannot tell it from a labelled bead: $OUT"
# The naming must be SPECIFIC to the inferred bead. A line printed for every
# fresh bead would satisfy the assertion above and tell an operator nothing.
elif printf '%s' "$OUT" | grep 'inferred' | grep -q 'gqlc-said'; then
    bad "an unlabelled bead routes as a warrior and says so" \
        "the labelled bead was announced as inferred too: $(printf '%s' "$OUT" | grep 'inferred')"
else
    ok "an unassigned bead with no class: label routes to a warrior seat and the run names the class as inferred, distinguishably from the labelled bead beside it"
fi

# The floor at P3, pinned against the LITERAL number rather than against
# whatever cfg returns. Every floor row above derives its fixture from
# `km cfg`, which is what lets them retune instead of falsifying — and is
# exactly why none of them can witness the value. MEASURED at the time the
# floor moved: the board held 162 P3 and 5 P4, so a floor of 2 made 167 ready
# beads unroutable by configuration while the fresh pass named every one of
# them on every two-minute tick.
dispatch_case '[
  {"id":"gqlc-p3","priority":3,"assignee":null,"labels":["class:warrior"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a P3 bead is routable" "rc=$RC out=$OUT"
elif printf '%s' "$OUT" | grep -q 'gqlc-p3.*below the floor\|below the floor.*gqlc-p3'; then
    bad "a P3 bead is routable" "it was withheld by the floor, which is still below 3: $OUT"
elif ! grep -rq 'gqlc-p3' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "a P3 bead is routable" "it reached nobody (woken: $(woken_seats)) out=$OUT"
else
    ok "a ready, unassigned, class-labelled P3 bead is routed rather than reported below the floor"
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
#
# Everything from here to the end of the section runs against the cap fixture,
# NOT the town. The number below is the fixture's own and is deliberately not
# the town's, so if the override ever stops taking effect these rows go red
# rather than quietly re-pinning the live value. It is also deliberately larger
# than the architect bench: at a cap of 3 or 4 `fill_cap 1` fills entirely with
# architects, every warrior is asleep, and "the first free warrior" and "the
# first warrior" name the same seat — so FREE_SEAT's skip would be dead code
# that no row could tell from a bug.
cap_config 7

# The membership rule every row below takes on trust. `cap_config` asks km which
# seats the cap counts, so a km that dropped a whole class from that rule would
# move the fixture and the rows together and none of them would redden. The
# roster in the config is the independent witness: km's rule is checked against
# the classes the town wrote down, not against itself.
ROSTER="$(awk '/^\[seats\]/ { s = 1; next } /^\[/ { s = 0 }
               s && $1 !~ /^#/ && /=/ { print $1 }' "$KM_CONFIG")"
EXPECT_CAPPED=""
UNCAPPED=""
for s in $ROSTER; do
    case "$("$KM" seat-info "$s" | cut -d' ' -f1)" in
        architect | warrior) EXPECT_CAPPED="$EXPECT_CAPPED$s " ;;
        *) UNCAPPED="$UNCAPPED$s " ;;
    esac
done
GOT_CAPPED="$("$KM" capped-seats | tr '\n' ' ')"
if [ -z "$CAPPED_ARCH" ] || [ -z "$CAPPED_WAR" ]; then
    bad "the cap counts every architect and warrior, and nobody else" "the fixture roster offers no architect or no warrior, so this row proves nothing"
elif [ -z "$UNCAPPED" ]; then
    bad "the cap counts every architect and warrior, and nobody else" "the fixture roster has no exempt seat, so an all-seats rule would pass"
elif [ "$GOT_CAPPED" != "$EXPECT_CAPPED" ]; then
    bad "the cap counts every architect and warrior, and nobody else" "km counts '$GOT_CAPPED', the roster's architects and warriors are '$EXPECT_CAPPED'"
else
    ok "km's capped-seats is exactly the roster's architects and warriors, with ${UNCAPPED% } exempt"
fi

# The warrior bead is P0 and the judge bead P1, so the judge sorts SECOND: the
# row fails if the fix merely reaches the queue at a full cap and takes its
# head. The unrouted warrior bead is also the control proving the cap really is
# full — without it, a woken judge could just mean the cap never engaged.
dispatch_case '[
  {"id":"gqlc-wcap","priority":0,"assignee":null,"labels":["class:warrior"]},
  {"id":"gqlc-jcap","priority":1,"assignee":null,"labels":["class:judge"]}
]' '[]'
fill_cap_leaving 0
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
fill_cap_leaving 0
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
fill_cap_leaving 1 mihr
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an awake judge does not spend a capped slot" "rc=$RC out=$OUT"
elif ! wake_of "$FREE_SEAT" | grep -q 'bead:gqlc-wfree'; then
    bad "an awake judge does not spend a capped slot" "the free warrior slot went unused (woken: $(woken_seats)) out=$OUT"
elif printf '%s' "$OUT" | grep -q 'no free slot'; then
    bad "an awake judge does not spend a capped slot" "the run counted the judge and declared itself full: $OUT"
else
    ok "an awake judge is not counted against max_active, so the last slot is still spent on warrior work"
fi

# The same exemption as the operator reads it. One more seat is awake here than
# the cap allows for, and the line must still say CAP/CAP, because the number
# is what a human sizes the town by — and gqlc-z1qw, gqlc-bn5r and gqlc-ed2u
# were each a healthy-looking indicator vouching for machinery that was not
# doing what the number implied.
dispatch_case '[]' '[]'
fill_cap_leaving 0 mihr
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the printed cap count excludes the judge" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q "$CAP/$CAP"; then
    bad "the printed cap count excludes the judge" "$((CAP + 1)) awake seats, one of them the judge, did not print as $CAP/$CAP: $OUT"
else
    ok "with the judge awake alongside a full bench, the cap line counts the capped seats and not the judge"
fi

# The numerator is a count of who is awake, not the cap restated. At exactly the
# cap the two are the same number, so every row above passes whichever one km
# prints — the state that tells them apart is an over-full bench, which is what
# a cap lowered under awake seats or a leaked slot (gqlc-s16s) actually looks
# like, and precisely when an operator needs the real number.
dispatch_case '[]' '[]'
fill_cap_leaving -1
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "the cap line counts the awake seats, not the cap" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'no free slot'; then
    bad "the cap line counts the awake seats, not the cap" "an over-full bench did not report itself full at all: $OUT"
elif ! printf '%s' "$OUT" | grep -q "$((CAP + 1))/$CAP"; then
    bad "the cap line counts the awake seats, not the cap" "$((CAP + 1)) capped seats awake did not print as $((CAP + 1))/$CAP: $OUT"
else
    ok "with one more capped seat awake than the cap allows, the line reports the seats awake over the cap, not the cap over itself"
fi

# The other number the operator reads. Deriving the total from slot arithmetic
# (`max - active - slots`) held only while every wake spent a slot; the judge's
# exemption breaks that, and at a full cap the expression floors to zero — so
# the run that reaches the merge gate is exactly the run that reports having
# done nothing, and the town reads as idle at the moment it is not.
dispatch_case '[
  {"id":"gqlc-jcount","priority":0,"assignee":null,"labels":["class:judge"]}
]' '[]'
fill_cap_leaving 0
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
fill_cap_leaving 1
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a judge wake does not spend the last free slot" "rc=$RC out=$OUT"
elif ! wake_of mihr | grep -q 'bead:gqlc-jslot'; then
    bad "a judge wake does not spend the last free slot" "the judge was not woken at all (woken: $(woken_seats)) out=$OUT"
elif ! wake_of "$FREE_SEAT" | grep -q 'bead:gqlc-wslot'; then
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
fill_cap_leaving 1
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a judge resume does not spend the last free slot" "rc=$RC out=$OUT"
elif ! wake_of mihr | grep -q 'gqlc-jslot2'; then
    bad "a judge resume does not spend the last free slot" "the judge was not resumed at all (woken: $(woken_seats)) out=$OUT"
elif ! wake_of "$FREE_SEAT" | grep -q 'bead:gqlc-wslot2'; then
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
fill_cap_leaving 0
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
fill_cap_leaving 0 mihr anahit tir
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an awake judge is not woken again" "rc=$RC out=$OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "an awake judge is not woken again" "it woke: $(woken_seats) out=$OUT"
else
    ok "a judge who is already awake is not woken again for ready judge work"
fi

# The whole point of a bench rather than a single judge: a busy judge no longer
# stalls the review queue at a full cap. Guards the roster, not just the routing
# — with every judge but `mihr` dropped from [seats] this wakes nobody and the
# bench serialises again.
#
# The row above needs the WHOLE bench named; this one deliberately does not, so
# that seating a further judge cannot silently disarm it. It asserts only that
# SOME free judge took the bead and that the busy one was left alone, which is
# the property, rather than pinning which seat gets it — that is
# free_worker_of_class's roster order and is not what this row is about.
dispatch_case '[
  {"id":"gqlc-jfree","priority":0,"assignee":null,"labels":["class:judge"]}
]' '[]'
fill_cap_leaving 0 mihr
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a busy judge does not stall the review queue" "rc=$RC out=$OUT"
elif ! { wake_of anahit; wake_of tir; } | grep -q 'gqlc-jfree'; then
    bad "a busy judge does not stall the review queue" "no free judge took it, woke: $(woken_seats) out=$OUT"
elif woken_seats | grep -qw mihr; then
    bad "a busy judge does not stall the review queue" "it also woke the busy judge: $(woken_seats) out=$OUT"
else
    ok "with one judge awake and others free, ready review work routes to a free judge at a full cap"
fi

# The property this whole section exists to keep. Four of the rows above once
# died on a one-line config edit that touched no code (gqlc-p5s8) — the point
# at which a suite stops guarding a tuning parameter and starts forbidding it.
# Running the same assertion at two different caps is what makes that visible:
# a row that has quietly re-acquired a hardcoded number passes at one value and
# fails at the other, where a single run passes either way.
for cap_try in 2 7; do
    cap_config "$cap_try"
    dispatch_case '[
      {"id":"gqlc-wtune","priority":0,"assignee":null,"labels":["class:warrior"]},
      {"id":"gqlc-jtune","priority":1,"assignee":null,"labels":["class:judge"]}
    ]' '[]'
    fill_cap_leaving 0
    run_dispatch
    if [ "$RC" -ne 0 ]; then
        bad "the cap rows follow the configured cap (max_active=$cap_try)" "rc=$RC out=$OUT"
    elif grep -rq 'gqlc-wtune' "$KM_STATE_DIR/seats" 2>/dev/null; then
        bad "the cap rows follow the configured cap (max_active=$cap_try)" "the cap never engaged, the warrior bead routed: $(woken_seats)"
    elif ! wake_of mihr | grep -q 'bead:gqlc-jtune'; then
        bad "the cap rows follow the configured cap (max_active=$cap_try)" "the judge was not reachable (woken: $(woken_seats)) out=$OUT"
    elif ! printf '%s' "$OUT" | grep -q "$cap_try/$cap_try"; then
        bad "the cap rows follow the configured cap (max_active=$cap_try)" "the cap line does not read $cap_try/$cap_try: $OUT"
    else
        ok "at max_active=$cap_try the cap fills, holds capped work, and still reaches the judge"
    fi
done

# Back onto the town's own config. A fixture that leaked would re-point every
# row below at a file in /tmp, and they would pass for the wrong reason.
unset KM_CONFIG
run cfg concurrency max_active
if [ "$RC" -ne 0 ] || [ "$OUT" != "$LIVE_MAX" ]; then
    bad "the cap fixture does not leak past its section" "expected the live $LIVE_MAX, got rc=$RC out=$OUT"
else
    ok "with KM_CONFIG unset the reader is back on the town's own kingdom.toml"
fi

# --- no free seat: the fresh pass's own silence (gqlc-2ve4) -------------------
# OBSERVED 2026-08-22T13:12Z: a P0 labelled class:architect sat unrouted while
# all three architect seats were asleep-with-work-queued, and NOTHING said so —
# not the run, not the summary, not the board. `free_worker_of_class` returning
# 1 was an `if` with no `else`, one layer below the `elif .cls == null then
# empty` arm that used to drop unlabelled beads. Same defect, same file: a
# decision taken and not spoken, and the town reading healthy throughout.
#
# Four conditions with four different remedies, so the rows below assert the
# REASON and not merely that something was said. A single "could not route"
# line would pass a row that only checked for noise.
#
# Every row here uses the town's own roster (3 architects, 8 warriors, 3
# judges) unless it says otherwise, and routes ARCHITECT work while leaving the
# warriors free — so a run that reported nothing because it reported nothing at
# all is distinguishable from one that declined the architect bead.

# The bead itself: a P0 whose whole class is occupied is named, loudly, and
# counted. The warrior control routes in the same run, so "the architect bead
# reached nobody" cannot be satisfied by a dispatcher that did nothing.
dispatch_case '[
  {"id":"gqlc-p0stall","priority":0,"assignee":null,"labels":["class:architect"]},
  {"id":"gqlc-livew","priority":1,"assignee":null,"labels":["class:warrior"]}
]' '[]'
fill_cap artur arpine aregak
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "no free seat: a P0 whose class is fully awake is named STALLED" "rc=$RC out=$OUT"
elif ! grep -rq 'gqlc-livew' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "no free seat: a P0 whose class is fully awake is named STALLED" \
        "the warrior control reached nobody, so this row proves nothing: $OUT"
elif grep -rq 'gqlc-p0stall' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "no free seat: a P0 whose class is fully awake is named STALLED" \
        "an architect bead routed with every architect awake: $(woken_seats)"
elif ! printf '%s' "$OUT" | grep -q 'gqlc-p0stall'; then
    bad "no free seat: a P0 whose class is fully awake is named STALLED" \
        "the highest-priority bead in the town went unrouted and the run never mentions it: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'STALLED.*gqlc-p0stall'; then
    bad "no free seat: a P0 whose class is fully awake is named STALLED" \
        "it is mentioned without the P0 marker an operator greps for: $(printf '%s' "$OUT" | grep gqlc-p0stall)"
# The REASON, not just the fact. 'awake' and the seat count are what tell this
# apart from a queued-wake backlog or an unseated class, which have different
# remedies entirely.
elif ! printf '%s' "$OUT" | grep 'gqlc-p0stall' | grep -q '3 architect seat'; then
    bad "no free seat: a P0 whose class is fully awake is named STALLED" \
        "the line does not say how many seats of the class there are: $(printf '%s' "$OUT" | grep gqlc-p0stall)"
elif ! printf '%s' "$OUT" | grep 'gqlc-p0stall' | grep -q '3 awake'; then
    bad "no free seat: a P0 whose class is fully awake is named STALLED" \
        "the line does not say the seats were awake, so it is indistinguishable from a queued-wake or unseated class: $(printf '%s' "$OUT" | grep gqlc-p0stall)"
elif ! printf '%s' "$OUT" | grep -qF ', 1 unroutable (1 at P0)'; then
    bad "no free seat: a P0 whose class is fully awake is counted in the done line" \
        "the summary hides it: $(printf '%s' "$OUT" | grep 'done')"
else
    ok "a P0 bead whose class is entirely awake is named STALLED with the seat count and the reason, and counted in the done line"
fi

# The negative control the row above needs. A line printed on every run tells an
# operator nothing, and a counter that never reads zero is not a counter.
dispatch_case '[
  {"id":"gqlc-quiet1","priority":0,"assignee":null,"labels":["class:architect"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "no free seat: a routable run says nothing about unroutable beads" "rc=$RC out=$OUT"
elif ! grep -rq 'gqlc-quiet1' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "no free seat: a routable run says nothing about unroutable beads" \
        "the bead did not route with every architect free (woken: $(woken_seats)) out=$OUT"
elif printf '%s' "$OUT" | grep -q 'STALLED\|unroutable gqlc-'; then
    bad "no free seat: a routable run says nothing about unroutable beads" \
        "it complained about a bead it had just routed: $OUT"
elif ! printf '%s' "$OUT" | grep -qF ', 0 unroutable (0 at P0)'; then
    bad "no free seat: a routable run says nothing about unroutable beads" \
        "the counter does not read zero on a clean run: $(printf '%s' "$OUT" | grep 'done')"
else
    ok "with the class free the bead routes, no unroutable line is printed, and the counter reads zero"
fi

# Proportionate to priority. A P0 that cannot find a seat is an emergency; a P3
# that cannot find a seat is the ordinary state of a 197-bead queue, and a
# per-bead line for every one of them on a two-minute tick is the volume that
# makes an operator stop reading — the same failure as silence, arrived at from
# the other side. So the low-priority beads are counted, not named, and the
# count is what makes them visible.
dispatch_case '[
  {"id":"gqlc-lo1","priority":2,"assignee":null,"labels":["class:architect"]},
  {"id":"gqlc-lo2","priority":2,"assignee":null,"labels":["class:architect"]},
  {"id":"gqlc-lo3","priority":3,"assignee":null,"labels":["class:architect"]}
]' '[]'
fill_cap artur arpine aregak
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "no free seat: low-priority beads are counted, not named one by one" "rc=$RC out=$OUT"
elif printf '%s' "$OUT" | grep -q 'gqlc-lo1\|gqlc-lo2\|gqlc-lo3'; then
    bad "no free seat: low-priority beads are counted, not named one by one" \
        "three ordinary backlog beads each got their own line: $(printf '%s' "$OUT" | grep 'gqlc-lo')"
elif ! printf '%s' "$OUT" | grep -q 'unroutable.*3 architect bead'; then
    bad "no free seat: low-priority beads are counted, not named one by one" \
        "they were suppressed entirely, which is the silence this bead is about: $OUT"
elif ! printf '%s' "$OUT" | grep -qF ', 3 unroutable (0 at P0)'; then
    bad "no free seat: low-priority beads are counted, not named one by one" \
        "the done line does not carry all three, or wrongly calls one of them P0: $(printf '%s' "$OUT" | grep 'done')"
else
    ok "three unroutable beads above the loud priority are aggregated into one counted line rather than named individually"
fi

# The middle band, and it is the row the two above cannot stand in for. With
# only a P0 case and a P2 case, deleting the loud arm entirely leaves the suite
# green: the P0 falls to the STALLED arm and the P2 to the counted one, and
# nothing witnesses the priority BETWEEN them (measured — that mutation
# SURVIVED until this row existed). It also pins the other half of the
# distinction: STALLED is the P0 marker, so a P1 that wore it too would leave
# an operator no way to grep the emergency out of the merely important.
dispatch_case '[
  {"id":"gqlc-mid1","priority":1,"assignee":null,"labels":["class:architect"]},
  {"id":"gqlc-mid2","priority":2,"assignee":null,"labels":["class:architect"]}
]' '[]'
fill_cap artur arpine aregak
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "no free seat: a P1 is named individually and is not called STALLED" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'unroutable gqlc-mid1'; then
    bad "no free seat: a P1 is named individually and is not called STALLED" \
        "a P1 that reached nobody was counted rather than named: $OUT"
elif printf '%s' "$OUT" | grep -q 'STALLED.*gqlc-mid1'; then
    bad "no free seat: a P1 is named individually and is not called STALLED" \
        "it wears the P0 marker, so an operator cannot grep the emergency apart: $(printf '%s' "$OUT" | grep gqlc-mid1)"
elif ! printf '%s' "$OUT" | grep 'gqlc-mid1' | grep -q 'P1'; then
    bad "no free seat: a P1 is named individually and is not called STALLED" \
        "the line does not carry the priority: $(printf '%s' "$OUT" | grep gqlc-mid1)"
elif printf '%s' "$OUT" | grep -q 'gqlc-mid2'; then
    bad "no free seat: a P1 is named individually and is not called STALLED" \
        "the P2 beside it was named too, so the band has no upper edge: $(printf '%s' "$OUT" | grep gqlc-mid2)"
elif ! printf '%s' "$OUT" | grep -qF ', 2 unroutable (0 at P0)'; then
    bad "no free seat: a P1 is named individually and is not called STALLED" \
        "both beads should be counted and neither is a P0: $(printf '%s' "$OUT" | grep 'done')"
else
    ok "an unroutable P1 is named on its own line without the P0 marker, while the P2 beside it is only counted"
fi

# A queued wake is a DIFFERENT condition from an occupied class, and its remedy
# is to wait one run rather than for a citizen to sleep. It is also the exact
# state gqlc-2ve4 was observed in.
dispatch_case '[
  {"id":"gqlc-qw","priority":0,"assignee":null,"labels":["class:architect"]}
]' '[]'
for s in artur arpine aregak; do
    mkdir -p "$KM_STATE_DIR/seats/$s"
    echo "already queued" >"$KM_STATE_DIR/seats/$s/wake"
done
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "no free seat: a class already handed its wakes says so" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'STALLED.*gqlc-qw'; then
    bad "no free seat: a class already handed its wakes says so" "the P0 was not named at all: $OUT"
elif printf '%s' "$OUT" | grep 'gqlc-qw' | grep -q 'awake'; then
    bad "no free seat: a class already handed its wakes says so" \
        "three ASLEEP seats were reported as awake, which sends the operator looking for work that is not there: $(printf '%s' "$OUT" | grep gqlc-qw)"
elif ! printf '%s' "$OUT" | grep 'gqlc-qw' | grep -q 'wake queued'; then
    bad "no free seat: a class already handed its wakes says so" \
        "the run does not name the queued wakes as the reason: $(printf '%s' "$OUT" | grep gqlc-qw)"
# The REMEDY, which is what separates this condition from occupancy. Every one
# of those seats has a wake waiting, so the next run has somewhere to put this
# bead and the correct response is to do nothing. Told to wait for a citizen to
# sleep instead, an operator acts on a condition that is already resolving.
elif ! printf '%s' "$OUT" | grep 'gqlc-qw' | grep -q 'waits for a later run'; then
    bad "no free seat: a class already handed its wakes says so" \
        "it describes a transient state with the occupancy remedy: $(printf '%s' "$OUT" | grep gqlc-qw)"
else
    ok "a bead whose class is asleep with every wake already queued is named, and the reason distinguishes it from an occupied class"
fi

# The fourth condition, and the only one that never self-heals: a class with no
# seat at all. A retired or never-seated class swallows its whole queue for as
# long as the roster stands, so the remedy is an edit to kingdom.toml and the
# line has to say so rather than describing seats that do not exist.
noarch="$TMP/noarch.toml"
sed '/^artur \|^arpine \|^aregak /d' "$REPO/kingdom/kingdom.toml" >"$noarch"
if KM_CONFIG="$noarch" "$KM" capped-seats | grep -qx 'artur\|arpine\|aregak'; then
    bad "no free seat: an unseated class names the roster" "the fixture still seats an architect, so this row proves nothing"
elif ! grep -q '^aramazd ' "$noarch"; then
    bad "no free seat: an unseated class names the roster" "the fixture rewrite took the warriors too, so the control below proves nothing"
else
    dispatch_case '[
      {"id":"gqlc-noseat","priority":0,"assignee":null,"labels":["class:architect"]},
      {"id":"gqlc-noseatw","priority":1,"assignee":null,"labels":["class:warrior"]}
    ]' '[]'
    OUT="$(cd "$FIXTURE" && PATH="$BIN:$PATH" KM_CONFIG="$noarch" "$KM" dispatch 2>&1)"
    RC=$?
    if [ "$RC" -ne 0 ]; then
        bad "no free seat: an unseated class names the roster" "rc=$RC out=$OUT"
    elif ! grep -rq 'gqlc-noseatw' "$KM_STATE_DIR/seats" 2>/dev/null; then
        bad "no free seat: an unseated class names the roster" \
            "the warrior control reached nobody, so this row proves nothing: $OUT"
    elif ! printf '%s' "$OUT" | grep -q 'STALLED.*gqlc-noseat '; then
        bad "no free seat: an unseated class names the roster" "the P0 was not named at all: $OUT"
    elif ! printf '%s' "$OUT" | grep 'gqlc-noseat ' | grep -q 'seats no architect'; then
        bad "no free seat: an unseated class names the roster" \
            "the reason does not say the class is unseated, so the operator is sent to wait for a seat that will never sleep: $(printf '%s' "$OUT" | grep 'gqlc-noseat ')"
    else
        ok "a bead of a class the roster does not seat is named STALLED and the reason points at kingdom.toml rather than at absent seats"
    fi
fi

# The cap is its own reason with its own remedy, and it is NOT the roster's.
# Before this it shared the roster's silence: the `[ "$slots" -gt 0 ] ||
# continue` arm skipped past a capped bead saying nothing per bead, and the one
# aggregate cap line above it does not name what it held.
cap_config 7
dispatch_case '[
  {"id":"gqlc-capped","priority":0,"assignee":null,"labels":["class:warrior"]},
  {"id":"gqlc-jexempt","priority":1,"assignee":null,"labels":["class:judge"]}
]' '[]'
fill_cap_leaving 0
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "no free seat: a bead held by the cap is named, and the judge is not" "rc=$RC out=$OUT"
elif ! wake_of mihr | grep -q 'bead:gqlc-jexempt'; then
    bad "no free seat: a bead held by the cap is named, and the judge is not" \
        "the judge's exemption from the cap broke — ready review work did not reach the bench (woken: $(woken_seats)) out=$OUT"
elif printf '%s' "$OUT" | grep -q 'STALLED.*gqlc-jexempt\|unroutable.*gqlc-jexempt'; then
    bad "no free seat: a bead held by the cap is named, and the judge is not" \
        "the judge bead was reported unroutable although it was routed: $(printf '%s' "$OUT" | grep gqlc-jexempt)"
elif ! printf '%s' "$OUT" | grep -q 'STALLED.*gqlc-capped'; then
    bad "no free seat: a bead held by the cap is named, and the judge is not" \
        "a P0 held back by a full cap is still not named: $OUT"
elif ! printf '%s' "$OUT" | grep 'gqlc-capped' | grep -q 'cap'; then
    bad "no free seat: a bead held by the cap is named, and the judge is not" \
        "the reason does not name the cap, so it reads as a roster problem: $(printf '%s' "$OUT" | grep gqlc-capped)"
elif ! printf '%s' "$OUT" | grep -qF ', 1 unroutable (1 at P0)'; then
    bad "no free seat: a bead held by the cap is named, and the judge is not" \
        "the done line does not count the capped bead, or counts the routed judge: $(printf '%s' "$OUT" | grep 'done')"
else
    ok "at a full cap the held P0 is named STALLED with the cap as the reason, while the cap-exempt judge bead routes and is reported unroutable nowhere"
fi
unset KM_CONFIG

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

# --- the resume pass wakes only for ACTIONABLE work (gqlc-s1lm / gqlc-3jmx) --
# Post-#1116 law: a warrior's implementation bead stays in_progress while its
# class:judge review bead awaits a verdict. The resume pass woke every
# in-progress holder, so every two-minute cycle re-woke review-blocked warriors
# who could only read mail and sleep again — each one burning a slot from
# max_active, with the judge who would unblock them competing for the slots
# they had just eaten. The town spent eight hours in that shape.
#
# The predicate is the bead's own dependency graph: actionable means no OPEN
# blocks-type dep. The warrior files the review bead with
# `--deps blocks:<impl-bead>`, which makes the review bead block the impl bead;
# the judge's close is the single toggle that frees it.
#
# These rows pass the show fixture EXPLICITLY (the fifth dispatch_case
# argument), because the default derives an all-unblocked payload and that
# default is what every row above is asserting.
blocked_show() { # <impl id> <assignee> <blocker id> <blocker status>
    jq -cn --arg i "$1" --arg a "$2" --arg b "$3" --arg s "$4" \
        '[{id: $i, assignee: $a,
           dependencies: [{id: $b, status: $s, dependency_type: "blocks"}]}]'
}

# R1. The headline, and all three halves belong in one row. The cap is ONE, so
# the slot the blocked warrior used to eat is the only slot there is: under the
# old pass he took it to read mail and sleep again, and the capped work behind
# him reached nobody. Asserting only "he was not woken" would pass a change that
# simply stopped resuming; the handoff is what makes it a fix.
cap_config 1
dispatch_case '[
  {"id":"gqlc-jrev","priority":0,"assignee":null,"labels":["class:judge"]},
  {"id":"gqlc-wfresh","priority":0,"assignee":null,"labels":["class:warrior"]}
]' '[
  {"id":"gqlc-impl","assignee":"aramazd","labels":["class:warrior"]}
]' '[]' '[]' "$(blocked_show gqlc-impl aramazd gqlc-jrev open)"
run_dispatch
r1="a review-blocked warrior sleeps through dispatch and the slot he was eating is spent on work"
if [ "$RC" -ne 0 ]; then
    bad "$r1" "rc=$RC out=$OUT"
elif wake_of aramazd | grep -q 'gqlc-impl'; then
    bad "$r1" "the blocked warrior was resume-woken anyway: $(wake_of aramazd)"
elif ! wake_of mihr | grep -q 'gqlc-jrev'; then
    bad "$r1" "the review bead reached no judge (woken: $(woken_seats)) out=$OUT"
elif ! grep -rq 'gqlc-wfresh' "$KM_STATE_DIR/seats" 2>/dev/null; then
    bad "$r1" "the freed slot bought nothing — the capped warrior bead reached nobody (woken: $(woken_seats)) out=$OUT"
else
    ok "$r1 — at max_active=1 the wake that used to eat the only slot no longer does"
fi
unset KM_CONFIG

# R2. The release. Nothing else changes state back: `bd ready` is open-only, so
# an in_progress bead never re-enters it and this pass is the ONLY way back.
dispatch_case '[]' '[
  {"id":"gqlc-impl","assignee":"aramazd","labels":["class:warrior"]}
]' '[]' '[]' "$(blocked_show gqlc-impl aramazd gqlc-jrev closed)"
run_dispatch
r2="a closed blocker is what wakes the warrior again"
if [ "$RC" -ne 0 ]; then
    bad "$r2" "rc=$RC out=$OUT"
elif ! wake_of aramazd | grep -q 'gqlc-impl'; then
    bad "$r2" "the judge's close freed nothing (woken: $(woken_seats)) out=$OUT"
elif ! wake_of aramazd | grep -q 'resume your in-progress work'; then
    bad "$r2" "the wake does not read as a resume: $(wake_of aramazd)"
else
    ok "$r2 — a blocker whose status is closed no longer withholds the resume wake"
fi

# R3. Only blocks-type deps gate readiness; `related` and `discovered-from`
# state a fact about provenance and withhold nothing. A predicate that dropped
# the type conjunct would put every bead with any open neighbour to sleep.
dispatch_case '[]' '[
  {"id":"gqlc-impl","assignee":"aramazd","labels":["class:warrior"]}
]' '[]' '[]' "$(jq -cn '[{id: "gqlc-impl", assignee: "aramazd",
        dependencies: [{id: "gqlc-note", status: "open", dependency_type: "related"}]}]')"
run_dispatch
r3="an open dep that is not a blocker withholds nothing"
if [ "$RC" -ne 0 ]; then
    bad "$r3" "rc=$RC out=$OUT"
elif ! wake_of aramazd | grep -q 'gqlc-impl'; then
    bad "$r3" "a related-type dep was treated as a blocker (woken: $(woken_seats)) out=$OUT"
else
    ok "$r3 — only blocks-type dependencies gate the resume wake, as they gate bd's own readiness"
fi

# R4. Mixed holdings, and this is where a plausible wrong fix shows: filter the
# WAKE but build the reason from the pre-filter id set, and the seat is handed
# back a bead it cannot act on beside one it can.
dispatch_case '[]' '[
  {"id":"gqlc-blk","assignee":"aramazd","labels":["class:warrior"]},
  {"id":"gqlc-act","assignee":"aramazd","labels":["class:warrior"]}
]' '[]' '[]' "$(jq -cn '[{id: "gqlc-blk", assignee: "aramazd",
        dependencies: [{id: "gqlc-jrev", status: "open", dependency_type: "blocks"}]},
       {id: "gqlc-act", assignee: "aramazd", dependencies: []}]')"
run_dispatch
r4="a seat holding one blocked and one actionable bead is woken for the actionable one only"
if [ "$RC" -ne 0 ]; then
    bad "$r4" "rc=$RC out=$OUT"
elif ! wake_of aramazd | grep -q 'gqlc-act'; then
    bad "$r4" "the actionable bead was withheld too: $(wake_of aramazd)"
elif wake_of aramazd | grep -q 'gqlc-blk'; then
    bad "$r4" "the wake reason names the blocked bead, so he is handed work he cannot act on: $(wake_of aramazd)"
else
    ok "$r4, and the reason names it alone"
fi

# R5 / R6. Fail-closed, both halves, the same four assertions the older failed-
# query rows make. A blocker query that fails is not a graph with no blockers
# in it, and the shape it must not take is a clean "done (0 wake(s) this run)".
for blocker_fail in rc parse; do
    dispatch_case '[]' '[
      {"id":"gqlc-impl","assignee":"aramazd","labels":["class:warrior"]}
    ]' '[]' '[]' "$(blocked_show gqlc-impl aramazd gqlc-jrev open)"
    case "$blocker_fail" in
        rc)    printf '1' >"$KM_FAKE_SHOW.rc" ;;
        parse) printf 'not json at all' >"$KM_FAKE_SHOW" ;;
    esac
    run_dispatch
    r5="a failed blocker query refuses instead of reading as an unblocked graph ($blocker_fail)"
    if [ "$RC" -eq 0 ]; then
        bad "$r5" "exited 0: $OUT"
    elif [ -n "$(woken_seats)" ]; then
        bad "$r5" "it woke: $(woken_seats)"
    elif ! printf '%s' "$OUT" | grep -q 'blocker query'; then
        bad "$r5" "the refusal does not name the blocker query: $OUT"
    elif printf '%s' "$OUT" | grep -q '0 wake(s)'; then
        bad "$r5" "it still reported a normal idle run: $OUT"
    else
        ok "$r5"
    fi
done

# The zero-id guard, stated as a row rather than left to the '[]' '[]' cases
# above to catch by accident. Real `bd show --json` with no ids prints an error
# OBJECT on stdout (and exits 1; see the stub, which models exit 0 as the
# harder case), so an unguarded call would abort jq on every idle run and the
# dispatcher would refuse where it should report a quiet one.
dispatch_case '[]' '[]'
run_dispatch
r7="an empty in-progress set never reaches the blocker query at all"
if [ "$RC" -ne 0 ]; then
    bad "$r7" "an idle run refused: rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q '0 wake(s)'; then
    bad "$r7" "an idle run does not report itself as one: $OUT"
else
    ok "$r7, so a run with nothing in progress reports a quiet run rather than a refusal"
fi

# --- the halt stops the guard sweep too (gqlc-6dzi) --------------------------
# `km halt` promises the town will wake nobody until it is lowered, and the
# dispatcher honours that. The guard sweep is a SECOND timer-driven wake path
# and read no flag at all, so a halted town went on launching a real claude
# session once per guard cadence — spending tokens under a halt, invisibly to
# whoever raised it. Article VI.4 reserves lowering a halt to Սեդրակ or
# Անդրանիկ, so a halt that does not actually stop the town is a halt in name
# only and the person who raised it has no way to find that out.
#
# These rows live down here rather than beside the halt rows above because they
# need the tmux stub: with the town DOWN, guard-sweep parks on its first line
# and never reaches a halt check, so a row run up there would pass without
# witnessing anything. That vacuity is the whole reason this shipped.
run_guard() { OUT="$(PATH="$BIN:$PATH" "$KM" guard-sweep 2>&1)"; RC=$?; }

# The control, and it has to come first: a sweep that wakes nobody is the
# result under test below, so this row is what distinguishes the halt working
# from the sweep being broken outright.
dispatch_case '[]' '[]'
run_guard
if [ "$RC" -ne 0 ]; then
    bad "an unhalted guard sweep wakes Րաֆֆի" "rc=$RC out=$OUT"
elif ! wake_of raffi | grep -q 'round'; then
    bad "an unhalted guard sweep wakes Րաֆֆի" "the sweep woke nobody with no halt raised (woken: $(woken_seats)) out=$OUT"
else
    ok "with no halt raised the guard sweep wakes Րաֆֆի for his round, so a silent sweep below means the flag and not a broken sweep"
fi

dispatch_case '[]' '[]'
run halt guard cadence must stop too
run_guard
if [ "$RC" -ne 0 ]; then
    bad "a halted guard sweep wakes nobody" "rc=$RC out=$OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "a halted guard sweep wakes nobody" "it woke: $(woken_seats) out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'halted'; then
    bad "a halted guard sweep wakes nobody" "the sweep was silent but does not say the halt held it: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'guard cadence must stop too'; then
    bad "a halted guard sweep wakes nobody" "it does not quote the reason the halt was raised for: $OUT"
else
    ok "a halted guard sweep wakes nobody and says the halt held it, quoting the reason, as the dispatcher already did"
fi

# The over-correction this must not become. The halt is aimed at the TIMERS —
# the two paths that wake seats with nobody watching. A human at a terminal is
# the one who lowers the halt, and taking his manual wake away with it would
# leave a halted town with no way to be worked on at all.
dispatch_case '[]' '[]'
run halt manual paths stay open
OUT="$(PATH="$BIN:$PATH" "$KM" wake raffi --reason "by hand during a halt" 2>&1)"
RC=$?
if [ "$RC" -ne 0 ]; then
    bad "a halt does not disarm a wake typed by hand" "rc=$RC out=$OUT"
elif ! wake_of raffi | grep -q 'by hand during a halt'; then
    bad "a halt does not disarm a wake typed by hand" "the manual wake was swallowed by the halt (woken: $(woken_seats)) out=$OUT"
else
    ok "a halt stops the timers and not the operator: km wake still reaches a seat while the flag is up"
fi

# The message is part of the defect, not just the code. `km halt` named ONE of
# the two timers it stops, so an operator reading the line had no reason to
# suspect the other was still running — and the line was the only report he got.
dispatch_case '[]' '[]'
run halt what does it claim
if [ "$RC" -ne 0 ]; then
    bad "the halt message names every wake path it stops" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -qi 'guard'; then
    bad "the halt message names every wake path it stops" "it promises a quiet town without mentioning the guard sweep: $OUT"
else
    ok "raising a halt names the guard sweep alongside the dispatcher, so the promise covers both timers it makes"
fi

# --- the scratch reap runs on EVERY tick, not only the ones that wake (u078) --
# internal/tools/tmpreap has existed since PR #1057 and was wired to no cadence,
# so every reclamation of the shared /tmp was a person noticing. The one time
# nobody did, the tmpfs hit 99% of its 1048576-inode cap and began refusing
# writes town-wide while `df -h` still showed 5.9G free (bd gqlc-vze6). The guard
# timer is the cadence it now hangs off.
#
# The rows below are all about PLACEMENT, because the reap is one line and the
# only way to get it wrong is to put it in the wrong place. Scratch is written by
# AGENTS, not by the town's loop: the halt, the town being down, and Րաֆֆի being
# already awake each return before the wake, and each of them is a state in which
# sixteen worktrees are still allocating scratch. The incident happened while the
# town was halted.
just_calls() { cat "$KM_FAKE_JUST_CALLS" 2>/dev/null; }
reset_just() { : >"$KM_FAKE_JUST_CALLS"; }

# The control, first, for the same reason the halt section has one: the rows
# below assert that a reap happens in states where the sweep does nothing else,
# and they are worthless if the reap never happens at all.
dispatch_case '[]' '[]'
reset_just
run_guard
if [ "$RC" -ne 0 ]; then
    bad "an ordinary guard tick reaps scratch" "rc=$RC out=$OUT"
elif ! just_calls | grep -q 'tmp-reap-cadence'; then
    bad "an ordinary guard tick reaps scratch" "the sweep invoked no reap at all (calls: $(just_calls)) out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'scratch'; then
    bad "an ordinary guard tick reaps scratch" "it reaped without saying so, and a timer's only reader is the journal: $OUT"
else
    ok "a guard tick invokes the scratch reap and reports what it got, on stdout, where the journal keeps it"
fi

# The placement row. A reap under the halt check is a reap disarmed in exactly
# the state that produced the incident — and a halt promises a quiet town, not a
# filling disk. This is what the reap being ABOVE the early returns buys.
dispatch_case '[]' '[]'
reset_just
run halt scratch keeps accumulating under a halt
run_guard
if [ "$RC" -ne 0 ]; then
    bad "a halted guard tick still reaps scratch" "rc=$RC out=$OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "a halted guard tick still reaps scratch" "the halt stopped binding the wake: it woke $(woken_seats)"
elif ! just_calls | grep -q 'tmp-reap-cadence'; then
    bad "a halted guard tick still reaps scratch" "the halt disarmed the reap too, so /tmp fills while the town is quiet (calls: $(just_calls)) out=$OUT"
else
    ok "a halt stops the wake and not the reap: the filesystem keeps filling while the town is quiet, so the reaper has to keep running"
fi

# The second placement row. `km down` does not stop the agents: a factory wave
# and every seat worktree go on writing into /tmp with no tmux session anywhere.
#
# Its own bin directory, holding a tmux that answers "no session" rather than
# no tmux at all. The real tmux on this host would be asked about the REAL
# town's session, and an answer of yes would carry this row past the down check
# and into file_patrol_bead, which calls the real bd. A stub that refuses is the
# only shape of this fixture that cannot reach the live town.
DOWNBIN="$TMP/bin-town-down"
mkdir -p "$DOWNBIN"
cp "$BIN/just" "$DOWNBIN/just"
printf '#!/usr/bin/env bash\nexit 1\n' >"$DOWNBIN/tmux"
chmod +x "$DOWNBIN/tmux"
reset_just
OUT="$(PATH="$DOWNBIN:$PATH" "$KM" guard-sweep 2>&1)"
RC=$?
if [ "$RC" -ne 0 ]; then
    bad "a guard tick over a town that is DOWN still reaps scratch" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'the town is down'; then
    bad "a guard tick over a town that is DOWN still reaps scratch" "the fixture never reached the down path, so this row witnesses nothing: $OUT"
elif ! just_calls | grep -q 'tmp-reap-cadence'; then
    bad "a guard tick over a town that is DOWN still reaps scratch" "no reap ran (calls: $(just_calls)) out=$OUT"
else
    ok "a town that is down still has agents writing scratch, and the tick reaps before it notices the town is down"
fi

# ADVISORY in both directions. The sweep's job is the Պահակ's round; a
# filesystem tool that could stop it would be a second way for the town to go
# quiet, and this town has had enough of those.
dispatch_case '[]' '[]'
reset_just
export KM_FAKE_JUST_RC=3
run_guard
unset KM_FAKE_JUST_RC
if [ "$RC" -ne 0 ]; then
    bad "a failing reap does not stop the sweep" "rc=$RC out=$OUT"
elif ! wake_of raffi | grep -q 'round'; then
    bad "a failing reap does not stop the sweep" "the sweep woke nobody after the reap failed (woken: $(woken_seats)) out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'exited 3'; then
    bad "a failing reap does not stop the sweep" "the failure was swallowed, so a reaper that has stopped working looks identical to one that had nothing to do: $OUT"
else
    ok "a reap that fails is named with its exit status and the round goes ahead: advisory, and never silent"
fi

# The reap needs a justfile in the DEPLOYED root, which is a thing that can be
# false — a deploy root pointed somewhere else, a checkout mid-rewrite. It
# declines rather than shelling out into whatever justfile `just` finds by
# searching upward from there, which on this host is the repository itself.
dispatch_case '[]' '[]'
reset_just
mk_clone "$TMP/town.git" "$TMP/deployed-no-justfile"
OUT="$(PATH="$BIN:$PATH" KM_DEPLOY_ROOT="$TMP/deployed-no-justfile" "$KM" guard-sweep 2>&1)"
RC=$?
if [ "$RC" -ne 0 ]; then
    bad "a deployed root with no justfile declines the reap by name" "rc=$RC out=$OUT"
elif [ -n "$(just_calls)" ]; then
    bad "a deployed root with no justfile declines the reap by name" "it ran just anyway, from a root with no justfile: $(just_calls)"
elif ! printf '%s' "$OUT" | grep -q 'no justfile'; then
    bad "a deployed root with no justfile declines the reap by name" "it skipped the reap silently: $OUT"
else
    ok "a deployed root with no justfile declines the reap and says which root, rather than skipping silently"
fi

# --- the silent result cap (gqlc-mlca) ---------------------------------------
# bd's JSON renderers truncate and say nothing: `ready` at 100, `list` at 50.
# The plain renderers do disclose it ("Showing 100 of 234 ready issues"), so the
# divergence is invisible to exactly the caller that cannot read prose. A bead
# past the window is not a bead nobody claimed — it is a bead nobody was shown,
# and the two are indistinguishable from the board.

# Padding is assigned to names that are not seats, so it takes the `owned` arm,
# matches no seat there and occupies none: the only bead that CAN route is the
# far one, which makes a silent "0 wake(s)" the whole signal.
#
# It used to be unassigned-and-unlabelled, which reached nobody for a different
# reason — the fresh pass dropped it. Since gqlc-38ye an unlabelled bead is
# inferred to class:warrior, so that padding filled every warrior seat and
# gqlc-far reached nobody: the row went red on seat exhaustion while claiming to
# report the cap. A non-seat assignee is inert under BOTH rules.
dispatch_case "$(jq -cn '[range(100) | {id: "gqlc-pad\(.)", priority: 1, assignee: "nobody\(.)", labels: ["area:pad"]}]
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
#
# The malformed candidate is `labels: "class:warrior"` — a SCALAR where an array
# belongs. It used to be `{"id":"gqlc-b"}` with no labels key at all, and that
# was the defect gqlc-bcir names: absent and null are what bd emits for an
# ordinary unlabelled bead, so the row was pinning the wrong population as
# corrupt. The rows below hold the new boundary; this one still holds the
# containment property, which is unchanged.
gh_prs '[]'
hv '[{"id":"gqlc-a","labels":["class:warrior"]},
     {"id":"gqlc-b","labels":"class:warrior"},
     {"id":"gqlc-c","labels":["class:warrior"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a malformed candidate holds itself and does not abort the run" "rc=$RC out=$OUT err=$ERR"
elif [ "$(printf '%s\n' "$OUT" | grep -c .)" -ne 3 ]; then
    bad "a malformed candidate holds itself and does not abort the run" "expected 3 lines, got: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-b .*malformed'; then
    bad "a malformed candidate holds itself and does not abort the run" "the candidate whose labels are a scalar was not held: $OUT"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-a' || ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-c'; then
    bad "a malformed candidate holds itself and does not abort the run" "it swallowed its neighbours: $OUT"
else
    ok "a candidate whose labels are a scalar is held as malformed while its neighbours are still answered — one bad row costs one line, not the run"
fi

# --- gqlc-bcir: `labels: null` is the commonest shape in the town, not corrupt -
# MEASURED 2026-08-22 over the whole ready queue: 293 candidates carried a
# labels ARRAY and 14 carried null. bd serialises a bead with no labels that
# way; it is what an unlabelled bead IS. All 14 were reported to the operator
# as "malformed candidate: labels is not an array" — 14 of the 27 holds the
# town was issuing, so a majority of the hold census was an artefact spelled in
# the vocabulary of data corruption, and a citizen auditing it went looking for
# corrupt records that did not exist. The same file already disagreed with
# itself: km:601, 609, 650 and the two ready-queue counters in cmd_status all
# write `(.labels // [])`, and one of those counters PRINTS the unlabelled
# population to the operator as a standing chore.
#
# The guard itself is right and stays: `.labels[]` aborts jq on null, and one
# aborted run reading as a healthy zero is gqlc-z1qw. Only the predicate was
# wider than the hazard.
#
# The five spellings are held in ONE invocation on purpose. null and absent must
# agree with [], and a real scalar must still be refused — a fix that widens the
# guard by deleting it passes any one of the routing rows alone.
bcir_row="labels null, absent and [] are the same unlabelled bead, and a scalar is still malformed"
hv '[{"id":"gqlc-null","labels":null},
     {"id":"gqlc-absent"},
     {"id":"gqlc-empty","labels":[]},
     {"id":"gqlc-scalar","labels":"class:warrior"},
     {"id":"gqlc-num","labels":42}]'
if [ "$RC" -ne 0 ]; then
    bad "$bcir_row" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-null'; then
    bad "$bcir_row" "labels:null was not routed — this is the 14-of-27 defect: $OUT"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-absent'; then
    bad "$bcir_row" "an absent labels key diverged from null: $OUT"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-empty'; then
    bad "$bcir_row" "the two spellings of no-labels diverged — [] did not route: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-scalar — malformed candidate: labels is string, not an array'; then
    # NAMES the type it found. "labels is not an array" tells a reader what was
    # wanted and not what is there, which is the difference between a message
    # they can act on and one that sends them looking for a corrupt record.
    bad "$bcir_row" "a string labels value was not held, or the reason does not name the type: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-num — malformed candidate: labels is number, not an array'; then
    bad "$bcir_row" "a numeric labels value was not held, or the reason does not name the type: $OUT"
else
    ok "$bcir_row"
fi

# The consequence, and the reason this is not a wording nit. Under the old
# predicate a null-labelled bead never REACHED the residue arm, so the logic
# written for exactly that bead — unlabelled, filed off an open parent — was
# unreachable for the entire population it was written for. gqlc-0rv9 is a live
# instance: labels null, a discovered-from parent, and described on its own bead
# as held by the subject-path mechanism when it was in fact held by this guard.
bcir_res="a null-labelled bead reaches the residue arm instead of dying at the malformed guard"
hv '[{"id":"gqlc-nres","labels":null,
      "deps":[{"depends_on_id":"gqlc-npar","type":"discovered-from","status":"open"}]}]'
if [ "$RC" -ne 0 ]; then
    bad "$bcir_res" "rc=$RC out=$OUT err=$ERR"
elif printf '%s' "$OUT" | grep -q 'malformed'; then
    bad "$bcir_res" "it still died at the malformed guard, so the residue arm never ran: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-nres — unlabelled residue of open gqlc-npar'; then
    bad "$bcir_res" "the residue arm did not judge it: $OUT"
else
    ok "$bcir_res"
fi

# ...and the arm must still CLEAR it when the parent is closed, or the fix
# trades a wrong hold for a permanent one.
bcir_clr="a null-labelled bead whose discovered-from parent is closed routes"
hv '[{"id":"gqlc-cres","labels":null,
      "deps":[{"depends_on_id":"gqlc-cpar","type":"discovered-from","status":"closed"}]}]'
if [ "$RC" -ne 0 ]; then
    bad "$bcir_clr" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-cres'; then
    bad "$bcir_clr" "routing null-labelled beads into the residue arm made them hold forever: $OUT"
else
    ok "$bcir_clr"
fi

# is_judge reads .labels too, at a SECOND site. Under the old code a
# null-labelled candidate never got that far, so widening subjects_of alone
# leaves a `.labels[]` that aborts the WHOLE run — the gqlc-z1qw shape again,
# and invisible in the rows above because none of them reaches the judge
# exemption. Two candidates in one invocation, so a partial fix cannot pass by
# answering the easy one.
bcir_jdg="the judge exemption survives a null-labelled candidate in the same batch"
hv '[{"id":"gqlc-jnull","labels":null},
     {"id":"gqlc-jrev","labels":["class:judge","subject:kingdom/bin/km"]}]'
if [ "$RC" -ne 0 ]; then
    bad "$bcir_jdg" "rc=$RC out=$OUT err=$ERR"
elif [ "$(printf '%s\n' "$OUT" | grep -c .)" -ne 2 ]; then
    bad "$bcir_jdg" "the run aborted part way — expected 2 verdicts, got: $OUT"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-jnull'; then
    bad "$bcir_jdg" "the null candidate did not route: $OUT"
else
    ok "$bcir_jdg"
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

# --- class:judge is exempt from the PR arms (gqlc-n4oe) ----------------------
# A review bead's premise IS an open PR touching that path, so the three arms
# that ask "might an open PR be touching this?" are inverted for it: they hold
# the review for exactly as long as anyone wants it, and release it the moment
# the PR merges. Measured before the fix: of 21 open class:judge beads, exactly
# 2 carried a subject: label and exactly those 2 were held — one for nine hours,
# looking from the board like ordinary queue depth.
#
# Every row pairs a judge candidate with a warrior candidate differing ONLY in
# the class label, in ONE invocation. That is what stops the exemption passing
# by routing everything: the warrior beside it must still hold, and on the same
# reason it held on before.

# Arm (3) — a definite match in an open PR's file list.
gh_prs '[{"number":1057,"files":[{"path":"justfile"}]}]'
hv '[{"id":"gqlc-jrev","labels":["class:judge","subject:justfile"]},
     {"id":"gqlc-wrev","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a judge bead routes though an open PR touches its subject" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-jrev'; then
    bad "a judge bead routes though an open PR touches its subject" \
        "the review of PR #1057 was held by the very PR it reviews: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-wrev — open PR #1057 touches justfile$'; then
    bad "a judge bead routes though an open PR touches its subject" \
        "the exemption leaked to the warrior beside it, on the same input: $OUT"
else
    ok "a class:judge bead whose subject an open PR modifies is routed — that PR is its premise — while a class:warrior bead on the identical subject still holds on the identical reason"
fi

# Arm (2) — gh unreachable. "Cannot rule out an open PR" is not a reason to hold
# a bead whose whole premise is an open PR, so an outage must not idle the
# review queue on top of everything else it idles.
GH_RC=1
hv '[{"id":"gqlc-jrev","labels":["class:judge","subject:justfile"]},
     {"id":"gqlc-wrev","labels":["class:warrior","subject:justfile"]}]'
GH_RC=""
if [ "$RC" -ne 0 ]; then
    bad "a gh outage does not hold a judge bead" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-jrev'; then
    bad "a gh outage does not hold a judge bead" \
        "an outage cannot rule out the PR the review is OF, which is not a reason to hold it: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-wrev .*gh unavailable'; then
    bad "a gh outage does not hold a judge bead" \
        "the warrior beside it stopped holding, so this row proves nothing about the outage arm: $OUT"
else
    ok "a gh outage routes the class:judge bead and still holds the class:warrior bead beside it — the outage arm is scoped away from reviews, not deleted"
fi

# Arm (4) — the truncation hold, which is the same "cannot rule out" shape one
# level down and would otherwise survive the exemption of arm (3) untouched.
gh_prs "$(jq -cn '[{number: 742, changedFiles: 5, files: [range(3) | {path: "pad/f\(.).go"}]}]')"
hv '[{"id":"gqlc-jrev","labels":["class:judge","subject:justfile"]},
     {"id":"gqlc-wrev","labels":["class:warrior","subject:justfile"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a truncated PR list does not hold a judge bead" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-jrev'; then
    bad "a truncated PR list does not hold a judge bead" \
        "an unreadable file list cannot rule out the PR under review, which is not a reason to hold the review: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-wrev .*cannot rule out justfile'; then
    bad "a truncated PR list does not hold a judge bead" \
        "the warrior beside it stopped holding, so the truncation arm is untested here: $OUT"
else
    ok "a PR list truncated per-PR routes the class:judge bead and still holds the class:warrior bead — the exemption reaches arm (4), not only the definite-match arm above it"
fi

# Arm (1) STAYS. A review naming a path that exists on no branch and in no PR is
# a typo, and holding it is right. Asserted on the REASON, not on the verdict:
# with two arms able to answer HOLD for one candidate, a verdict-only assertion
# stays green when the wrong one fires (see the assert-the-reason finding).
gh_prs '[{"number":1057,"files":[{"path":"justfile"}]}]'
hv '[{"id":"gqlc-jgone","labels":["class:judge","subject:no/such/path/km-hold-test"]}]'
if [ "$RC" -ne 0 ]; then
    bad "a judge bead with an absent subject is still held" "rc=$RC out=$OUT err=$ERR"
elif [ "$OUT" != "HOLD gqlc-jgone — premise absent from origin/master: no/such/path/km-hold-test" ]; then
    bad "a judge bead with an absent subject is still held" \
        "the class exemption must not blanket the premise-absent arm, and the hold must be ON that arm: $OUT"
else
    ok "a class:judge bead whose subject path is on no branch and in no PR is still held as premise-absent — the exemption is per-arm, not a blanket pass for the class"
fi

# The residue arm. This is the sharp one: the machinery's printed remedy for the
# arm above — "needs a subject: label" — is the thing that causes THIS hold,
# because a review bead's parent cannot close until the PR merges, which needs
# the review. Two doors, one deadlock; closing the first opens the second.
gh_prs '[]'
hv '[{"id":"gqlc-jres","labels":["class:judge"],
      "deps":[{"depends_on_id":"gqlc-par","type":"discovered-from","status":"open"}]},
     {"id":"gqlc-wres","labels":["class:warrior"],
      "deps":[{"depends_on_id":"gqlc-par","type":"discovered-from","status":"open"}]}]'
if [ "$RC" -ne 0 ]; then
    bad "unlabelled judge residue of an open parent routes" "rc=$RC out=$OUT err=$ERR"
elif ! printf '%s' "$OUT" | grep -qx 'ROUTE gqlc-jres'; then
    bad "unlabelled judge residue of an open parent routes" \
        "the parent stays open until the PR merges, which needs this review, so this hold never releases: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^HOLD gqlc-wres .*gqlc-par'; then
    bad "unlabelled judge residue of an open parent routes" \
        "the residue arm stopped holding warriors, which is the case it was written for: $OUT"
else
    ok "an unlabelled class:judge bead discovered from a still-open parent is routed, while the identical class:warrior bead is still held — the label the first hold demands is what triggers the second"
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
gitf clone -q "$FIXTURE_ORIGIN" "$FIXTURE_LATE"
gitf -C "$FIXTURE_LATE" config user.email fixture@example.invalid
gitf -C "$FIXTURE_LATE" config user.name fixture
gitf -C "$FIXTURE_LATE" config commit.gpgsign false
gitf -C "$FIXTURE_LATE" config core.hooksPath /dev/null
printf 'fixture\n' >"$FIXTURE_LATE/late-arrival"
gitf -C "$FIXTURE_LATE" add late-arrival
gitf -C "$FIXTURE_LATE" commit -qm 'lands after the working clone last looked'
gitf -C "$FIXTURE_LATE" push -q origin master

gh_prs '[]'
if [ "$(gitf -C "$FIXTURE" cat-file -e origin/master:late-arrival 2>&1; echo $?)" = "0" ]; then
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

# The owned pass is upstream of the verdict, so no hold can reach it. This is
# the pair for the FIRST row of this section, not the falsifier above: same open
# PR, same subject, same held condition — the only difference is that this bead
# names a seat. Both answers are defensible and the split this branch adds is
# what forces the choice, so the choice gets a row rather than being left as an
# artifact of a diff. It goes this way because the hold's premise is that a
# fresh routing sends a stranger to branch from origin/master and find nothing;
# an assignee already holds the context the hold exists to protect, and
# withholding here would make a bead unroutable for as long as any PR touched
# its file, which is most of the time on kingdom/ paths.
gh_prs '[{"number":1057,"files":[{"path":"justfile"}]}]'
dispatch_case '[
  {"id":"gqlc-owned","priority":0,"assignee":"vahagn","labels":["class:warrior","subject:justfile"]}
]' '[]'
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a hold does not withhold a bead that names a seat" "rc=$RC out=$OUT"
elif ! wake_of vahagn | grep -q 'gqlc-owned'; then
    bad "a hold does not withhold a bead that names a seat" \
        "its assignee was not woken under the same condition that holds an unassigned bead (woken: '$(woken_seats)') out=$OUT"
elif printf '%s' "$OUT" | grep -q 'hold gqlc-owned'; then
    bad "a hold does not withhold a bead that names a seat" \
        "it was reported held as well as routed, so the journal contradicts the wake: $OUT"
elif ! printf '%s' "$OUT" | grep -qF 'this run, 0 held'; then
    bad "a hold does not withhold a bead that names a seat" \
        "the run counts a hold it did not act on: $OUT"
else
    ok "a ready bead assigned to a seat reaches that seat even when an open PR touches its subject — the condition that holds the identical unassigned bead — and the run reports no hold"
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

# --- the per-argument ceiling (gqlc-fo48) ------------------------------------
# On 2026-08-22 the fresh pass died 63 consecutive runs over two hours with
#
#     km: line 652: /usr/bin/jq: Argument list too long
#     dispatch: the candidate documents could not be assembled
#
# because `--argjson ready "$ready"` puts the whole `bd ready` document into ONE
# command-line argument, and Linux caps a single argument at MAX_ARG_STRLEN —
# 131072 bytes on a 4 KiB-page machine. That is a hard ceiling in the binfmt
# loader: not ARG_MAX, unmoved by `ulimit`, and unrelated to free memory.
#
# The reason these rows exist rather than a comment on the call site: THE
# PAYLOAD IS THE BACKLOG. The failure is monotonic in the queue, so it cannot
# self-heal, and the dispatcher that drains the queue switches off precisely as
# the queue grows. Nothing about a full queue looks like a broken router; the
# line above it in the journal read `5/5 capped seats awake; no free slot`, a
# complete and plausible account of an idle town, printed by an arm that was
# working correctly.
#
# THE FALSIFIER IS MEASURED, NOT ASSUMED. A fixture that fits in argv would pass
# these rows on the pre-fix km too, and a queue-length fixture is exactly the
# kind that drifts under a threshold as someone trims it. So each row first
# offers ITS OWN payload to jq the old way and requires that to fail, while the
# same bytes on a descriptor parse — two-sided, so an argv failure for any other
# reason cannot be read as the ceiling. If the payload turns out to fit, the row
# reports a skip and witnesses nothing, rather than banking a green.

over_arg_ceiling() { # payload -> true when argv rejects it AND a descriptor takes it
    printf '%s' "$1" | jq -e . >/dev/null 2>&1 || return 1
    jq -n --argjson probe "$1" '1' >/dev/null 2>&1 && return 1
    return 0
}

# 4 KiB of padding per bead, so the document clears the ceiling in tens of beads
# rather than thousands and the row stays fast.
ceil_ready="$(jq -c -n '
    [ range(0; 64)
      | { id: "gqlc-ceil\(.)", priority: 3, assignee: null,
          labels: ["class:warrior"],
          design: ("x" * 4096) } ]
    + [ { id: "gqlc-ceilwin", priority: 0, assignee: null,
          labels: ["class:warrior"] } ]')"

ceiling_row="the fresh pass survives a ready queue larger than one argv slot"
if ! over_arg_ceiling "$ceil_ready"; then
    printf 'skip - %s: the fixture (%d bytes) still fits in one argument here\n' \
        "$ceiling_row" "${#ceil_ready}"
else
    gh_prs '[]'
    dispatch_case "$ceil_ready" '[]'
    run_dispatch
    if printf '%s' "$OUT" | grep -q 'could not be assembled'; then
        bad "$ceiling_row" "the assembly died on the payload size: $OUT"
    elif [ "$RC" -ne 0 ]; then
        bad "$ceiling_row" "rc=$RC out=$OUT"
    elif ! grep -rq 'gqlc-ceilwin' "$KM_STATE_DIR/seats" 2>/dev/null; then
        bad "$ceiling_row" \
            "nothing routed off a queue of $((${#ceil_ready} / 1024)) KiB (woken: '$(woken_seats)'): $OUT"
    else
        ok "a ready queue past the per-argument ceiling still routes its top bead, so the dispatcher does not switch itself off as the backlog it drains grows"
    fi
fi

# The same defect at the hold verdict's own input, pinned BEFORE it fires rather
# than after. $prs carries every open PR's full file list, so it grows with the
# PR queue exactly as the ready payload grows with the bead queue — measured
# 2026-08-22 at 26 open PRs it was already within ~30% of the ceiling. The
# candidate here is subject-labelled, which is what makes km consult the PR map
# at all; the verdict asserted is HOLD, and the row below it is the falsifier
# that stops HOLD from being the answer to everything.
ceil_prs="$(jq -c -n '
    [ range(0; 64)
      | { number: (2000 + .),
          changedFiles: 1,
          files: [ { path: "pad/\("y" * 4080)/\(.)" } ] } ]
    + [ { number: 1057, changedFiles: 1, files: [ { path: "justfile" } ] } ]')"

prs_row="the hold verdict survives a PR map larger than one argv slot"
if ! over_arg_ceiling "$ceil_prs"; then
    printf 'skip - %s: the fixture (%d bytes) still fits in one argument here\n' \
        "$prs_row" "${#ceil_prs}"
else
    gh_prs "$ceil_prs"
    hv '[{"id":"gqlc-ceilhold","labels":["class:warrior","subject:justfile"]}]'
    if [ "$RC" -ne 0 ]; then
        bad "$prs_row" "rc=$RC out=$OUT err=$ERR"
    elif [ "$OUT" != "HOLD gqlc-ceilhold — open PR #1057 touches justfile" ]; then
        bad "$prs_row" "the verdict changed under a large PR map: '$OUT' err=$ERR"
    else
        ok "a PR map past the per-argument ceiling still yields its verdict, and still names the PR that caused the hold"
    fi

    # Falsifier: the same oversized map with #1057 dropped. Without it, a HOLD
    # above could mean "km fell over and held everything", which is the shape
    # this whole section is about.
    gh_prs "$(printf '%s' "$ceil_prs" | jq -c '[ .[] | select(.number != 1057) ]')"
    hv '[{"id":"gqlc-ceilhold","labels":["class:warrior","subject:justfile"]}]'
    if [ "$RC" -ne 0 ] || [ "$OUT" != "ROUTE gqlc-ceilhold" ]; then
        bad "$prs_row is not a blanket hold" \
            "the same oversized map without #1057 did not release the bead: rc=$RC out='$OUT' err=$ERR"
    else
        ok "the oversized PR map releases the bead when nothing in it touches the subject, so the row above measured the verdict and not a collapse"
    fi
fi

unset KM_HOLD_SKIP_FETCH KM_FAKE_GH
# --- the cap counts sessions, not claims (gqlc-s16s) -------------------------
# `awake` in a status file is a CLAIM, written by km-seat before it starts a
# session and by `km sleep` before it asks for one to end. Nothing reconciled
# the claim against whether a session existed, so a slot could be spent on
# nothing at all, permanently, while every indicator read healthy. Measured on
# the live town 2026-08-22T02:40Z: two of five slots held by finished sessions.
#
# The rows below fix the accounting at its source — the status file is repaired
# against ground truth before anything reads it — so the cap, the board, and
# free_worker_of_class all become right together rather than one at a time.
#
# One warrior bead, and who gets woken, is the observable throughout: it is
# behaviour rather than a message, so a row cannot pass on a reworded string.
one_warrior_bead='[{"id":"gqlc-slot","priority":1,"assignee":null,"labels":["class:warrior"]}]'

# The arithmetic below is written around five capped slots — four live seats
# plus one broken one is a FULL cap, and that is the whole observable. Read off
# the town's own max_active that stops being true the moment anyone tunes it, so
# the section takes its five from a fixture instead (gqlc-p5s8). Five here is the
# suite's own number and is deliberately not read from the live file.
cap_config 5

# The control, and it must come first: a seat with a real session still spends
# its slot. Without this row, "reap everything unconditionally" is green.
dispatch_case "$one_warrior_bead" '[]'
fill_cap artur arpine aregak vahagn astghik
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "five live sessions still fill the cap" "rc=$RC out=$OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "five live sessions still fill the cap" "woke [$(woken_seats)] with no slot free"
else
    ok "a seat whose session is genuinely alive still spends its slot"
fi

# The witnessed shape: status=awake, tmux window GONE. `ayg` was in exactly this
# state at 05:35Z and was still counted. km:550 already renders it `awake?`, so
# the town detected the dead slot and spent it anyway.
dispatch_case "$one_warrior_bead" '[]'
fill_cap artur arpine aregak vahagn
fill_windowless ayg
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an awake seat with no window does not spend a slot" "rc=$RC out=$OUT"
elif ! wake_of aramazd | grep -q gqlc-slot; then
    bad "an awake seat with no window does not spend a slot" \
        "the freed slot routed nothing; woke [$(woken_seats)]"
else
    ok "a seat marked awake whose tmux window is gone does not spend a slot"
fi

# The slot came back on that first pass, but the LEDGER must not be rewritten
# on one sighting: km-seat writes `awake` before it execs claude, so a seat
# mid-startup looks exactly like a dead one for that instant, and correcting it
# there would hand a running seat's name back to the dispatcher. Freeing the
# slot is reversible arithmetic; renaming the seat's state is not.
if [ "$(cat "$KM_STATE_DIR/seats/ayg/status" 2>/dev/null)" != awake ]; then
    bad "one sighting of a dead seat does not rewrite the ledger" \
        "status changed on the first pass: [$(cat "$KM_STATE_DIR/seats/ayg/status" 2>/dev/null)]"
else
    ok "a seat seen dead once is not yet written off — the startup gap looks the same"
fi

# Two sightings are not two cycles. Nothing puts time between reconcile passes:
# `km reconcile` is a public subcommand, this suite runs it back to back, and
# one manual `km dispatch` seconds before a cron tick pairs with it around a
# seat that is mid-startup. So the pair has to be separated by AGE, or the
# startup gap the two-pass rule exists to survive is still open.
young_row="a second sighting with no time between it and the first does not reap"
if [ ! -f "$KM_STATE_DIR/seats/ayg/suspect" ]; then
    bad "$young_row" "the first pass recorded no suspicion, so this row ages nothing"
else
    run_dispatch
    if [ "$(cat "$KM_STATE_DIR/seats/ayg/status" 2>/dev/null)" != awake ]; then
        bad "$young_row" \
            "reaped on a sighting seconds old: [$(cat "$KM_STATE_DIR/seats/ayg/status" 2>/dev/null)]"
    elif ! printf '%s' "$OUT" | grep -q 'too recent'; then
        bad "$young_row" "held, but not for the sighting's age: $OUT"
    else
        ok "$young_row"
    fi
fi

# Now give the suspicion the cycle it is supposed to have had — which is what
# keeps the row above and this one from being the same measurement.
age_suspicion ayg
run_dispatch
if [ "$(cat "$KM_STATE_DIR/seats/ayg/status" 2>/dev/null)" != asleep ]; then
    bad "a dead seat confirmed twice is repaired in the ledger" \
        "still reads [$(cat "$KM_STATE_DIR/seats/ayg/status" 2>/dev/null)]"
else
    ok "a dead seat still dead a cycle later is corrected to asleep, not merely discounted"
fi

# The confirmation has to be able to say no, or it is a delay dressed as a
# check. A seat that was suspect and is alive again keeps its awake record.
dispatch_case "$one_warrior_bead" '[]'
fill_cap artur arpine aregak vahagn
fill_windowless ayg
run_dispatch
seat_window ayg
seat_claude ayg
run_dispatch
if [ "$(cat "$KM_STATE_DIR/seats/ayg/status" 2>/dev/null)" != awake ]; then
    bad "a suspected seat that is alive again is not reaped" \
        "reaped anyway: [$(cat "$KM_STATE_DIR/seats/ayg/status" 2>/dev/null)]"
elif [ -f "$KM_STATE_DIR/seats/ayg/suspect" ]; then
    bad "a suspected seat that is alive again is not reaped" \
        "the suspicion outlived the evidence; a later pass would reap a live seat"
else
    ok "a seat suspected once and alive at the next pass is cleared, not reaped"
fi

# The window can outlive the session: km-seat's claude exits, the trap writes
# asleep — but if that write is what was lost, the window remains with no
# claude on its tty.
dispatch_case "$one_warrior_bead" '[]'
fill_cap artur arpine aregak vahagn
fill_zombie ayg
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "an awake seat with a window but no claude does not spend a slot" "rc=$RC out=$OUT"
elif ! wake_of aramazd | grep -q gqlc-slot; then
    bad "an awake seat with a window but no claude does not spend a slot" \
        "the freed slot routed nothing; woke [$(woken_seats)]"
else
    ok "a seat whose window survives its claude does not spend a slot"
fi

# The other direction, and the one the old accounting got backwards (gqlc-0gjt):
# `km sleep` writes asleep-pending BEFORE it tries to deliver /exit, and the cap
# tested for the literal `awake`, so a session whose departure never arrived was
# alive and UNCOUNTED. The cap could then overcommit. A live claude spends a
# slot whatever the status file says it intends.
dispatch_case "$one_warrior_bead" '[]'
fill_cap artur arpine aregak vahagn
fill_pending ayg
run_dispatch
if [ "$RC" -ne 0 ]; then
    bad "a live session spends a slot even while marked asleep-pending" "rc=$RC out=$OUT"
elif [ -n "$(woken_seats)" ]; then
    bad "a live session spends a slot even while marked asleep-pending" \
        "woke [$(woken_seats)] though five sessions were alive"
else
    ok "an asleep-pending seat whose claude is still alive spends its slot"
fi

# --- Enter must travel alone, and even that is not enough (gqlc-s16s) --------
# MEASURED with a raw-tty reader behind a real tmux 3.7c pane: `send-keys
# "text" Enter` delivers both in ONE read() burst, and a TUI that coalesces a
# burst into a paste takes the trailing CR as literal text instead of a submit.
# Three separate invocations arrive as three bursts. C-u is not the cause —
# `send-keys "/exit" Enter` bundles just the same without it.
#
# Separation is NECESSARY. It is NOT SUFFICIENT, and no row here can pretend
# otherwise: with the reader not sitting in read(), the two writes re-coalesce
# in the tty buffer and the CR is swallowed again, at every inter-call delay I
# tried. That is why the reconciler below exists — delivery is confirmed after
# the fact rather than trusted at the point of sending.
# Asserts the whole shape of a delivery, because each third of it fails
# differently and silently:
#   C-u first — the box may already hold a stranded attempt, and this is a
#     RE-send path, so without the clear a retried /exit lands as /exit/exit and
#     the retry is what breaks the pane. Dropping this line passes every other
#     assertion here, which is how it was found.
#   text alone — bundled with Enter it arrives in one read() burst and a
#     paste-coalescing TUI takes the CR as literal text (measured).
#   Enter sent — checking only that nothing is bundled would pass a km that
#     never submits at all: the same stranded pane by a shorter route.
# Given no payload line carries Enter, any surviving Enter line is a lone one.
assert_delivery_shape() { # <label> <log>
    local label=$1 log=$2 bundled
    bundled=$(grep -E '(/exit|\[km\])' "$log" 2>/dev/null | grep -c 'Enter' || true)
    if [ ! -s "$log" ]; then
        bad "$label" "no send-keys reached the pane at all"
    elif ! head -1 "$log" | grep -q 'C-u'; then
        bad "$label" "the first invocation does not clear the line, so a re-send appends to a stranded one: $(head -1 "$log")"
    elif [ "$bundled" -ne 0 ]; then
        bad "$label" "$bundled invocation(s) carried the text AND Enter together: $(grep 'Enter' "$log" | head -1)"
    elif ! grep -q 'Enter' "$log"; then
        bad "$label" "the text was sent but Enter never was, so nothing submits: $(cat "$log")"
    else
        ok "$label"
    fi
}

dispatch_case '[]' '[]'
seat_window ayg
seat_claude ayg
export KM_SENDKEYS_LOG="$KM_STATE_DIR/sendkeys.log"
: >"$KM_SENDKEYS_LOG"
PATH="$BIN:$PATH" "$KM" sleep --seat ayg >/dev/null 2>&1
assert_delivery_shape "km sleep clears the line, then sends /exit, then Enter — three invocations" "$KM_SENDKEYS_LOG"

# The nudge path has the same shape and the same failure — a wake reason typed
# at an already-awake seat, stranded in its input box, wakes nobody.
#
# The pane is set here rather than left absent: a real awake seat has one, and
# the rows below turn on what it SAYS, so a fixture with no pane at all would be
# a seat shape the town has never had.
dispatch_case '[]' '[]'
fill_cap ayg
pane_idle ayg
export KM_SENDKEYS_LOG="$KM_STATE_DIR/sendkeys.log"
: >"$KM_SENDKEYS_LOG"
PATH="$BIN:$PATH" "$KM" wake ayg --reason "a nudge" >/dev/null 2>&1
assert_delivery_shape "km wake nudges an awake seat with the clear, the text and Enter each on their own" "$KM_SENDKEYS_LOG"

# --- km wake CONFIRMS its nudge, exactly as the dispatcher's does (gqlc-01ev) -
# Half of this bead was already fixed when it was read: the single bundled
# `send-keys ... "text" Enter` it was filed against had since been split into
# the clear/text/Enter of send_line, and the row above pins that. The half that
# was NOT fixed is the one that matters, because send_line's own contract says
# the split is NECESSARY AND NOT SUFFICIENT — the tty re-coalesces at any delay
# if the reader is not in read() between the writes. seat_nudge exists to obey
# that contract by confirming after the fact, and on 2026-08-22 it was wired
# into the dispatcher's mail nudge (km:990) and NOT into cmd_wake's
# already-awake nudge, which is the path this bead was filed about and the path
# `km wake` and route_owners both take.
#
# So: the same four assertions the dispatcher's sticky row makes, on cmd_wake.
dispatch_case '[]' '[]'
fill_cap ayg
pane_idle ayg
pane_sticky ayg
export KM_SENDKEYS_LOG="$KM_STATE_DIR/sendkeys.log"
: >"$KM_SENDKEYS_LOG"
OUT="$(PATH="$BIN:$PATH" "$KM" wake ayg --reason "a nudge" 2>&1)"
RC=$?
wake_sticky_row="km wake reports a nudge that never left the prompt box"
if [ "$RC" -ne 0 ]; then
    bad "$wake_sticky_row" "rc=$RC — an undelivered nudge must not abort the dispatcher that called it: $OUT"
elif printf '%s' "$OUT" | grep -q 'is already awake; nudged instead'; then
    bad "$wake_sticky_row" "success was reported for a line still sitting unsent: $OUT / pane: $(pane_of ayg)"
elif ! printf '%s' "$OUT" | grep -q 'NUDGE UNDELIVERED to ayg'; then
    bad "$wake_sticky_row" "nothing said the message never landed: $OUT"
elif [ "$(grep -c 'Enter' "$KM_SENDKEYS_LOG")" -lt 2 ]; then
    bad "$wake_sticky_row" "the bare Enter that repaired this by hand was never re-sent: $(cat "$KM_SENDKEYS_LOG")"
elif ! wake_of ayg | grep -q 'a nudge'; then
    bad "$wake_sticky_row" "the reason was dropped instead of queued, so it dies with the send: $(wake_of ayg)"
else
    ok "$wake_sticky_row, re-sends the bare Enter, and queues the reason so it survives the failed send"
fi
unset KM_SENDKEYS_LOG

# The other side of the same guard. A nudge that DID land must say it was
# confirmed, and must not queue a duplicate onto the wake file — a report that
# cannot distinguish the two is the report `km wake` printed for the whole of
# 2026-08-22 while delivering nothing.
dispatch_case '[]' '[]'
fill_cap ayg
pane_idle ayg
export KM_SENDKEYS_LOG="$KM_STATE_DIR/sendkeys.log"
: >"$KM_SENDKEYS_LOG"
OUT="$(PATH="$BIN:$PATH" "$KM" wake ayg --reason "a nudge" 2>&1)"
wake_ok_row="km wake claims delivery only after the prompt is seen clear"
if ! printf '%s' "$OUT" | grep -q 'the prompt cleared, so it was delivered'; then
    bad "$wake_ok_row" "delivery was claimed without being confirmed: $OUT"
elif printf '%s' "$OUT" | grep -q 'UNDELIVERED'; then
    bad "$wake_ok_row" "a delivered nudge was reported undelivered: $OUT"
elif [ -n "$(wake_of ayg)" ]; then
    bad "$wake_ok_row" "a delivered nudge also queued itself, so the seat hears it twice: $(wake_of ayg)"
else
    ok "$wake_ok_row"
fi
unset KM_SENDKEYS_LOG

# VI.2, on the operator's path this time. cmd_wake typed at any awake seat with
# a window, and a nudge ends in Enter; a seat frozen on a usage-limit or
# shutdown modal has no prompt line at all because the modal covers the box, so
# that Enter CHOOSES whichever option is highlighted. The dispatcher's mail
# nudge has been guarded against this since gqlc-eier; `km wake` was not, and
# route_owners reaches it on every dispatch run.
dispatch_case '[]' '[]'
fill_cap ayg
pane_modal ayg
export KM_SENDKEYS_LOG="$KM_STATE_DIR/sendkeys.log"
: >"$KM_SENDKEYS_LOG"
OUT="$(PATH="$BIN:$PATH" "$KM" wake ayg --reason "a nudge" 2>&1)"
wake_modal_row="km wake does not type at a seat frozen on a modal"
if [ -s "$KM_SENDKEYS_LOG" ]; then
    bad "$wake_modal_row" "an Enter was sent at a modal, which presses whatever is highlighted: $(cat "$KM_SENDKEYS_LOG")"
elif ! printf '%s' "$OUT" | grep -q 'no prompt to type at'; then
    bad "$wake_modal_row" "the refusal is silent, so the reason looks delivered: $OUT"
elif ! wake_of ayg | grep -q 'a nudge'; then
    bad "$wake_modal_row" "the reason was dropped rather than queued for when the modal clears: $(wake_of ayg)"
else
    ok "$wake_modal_row — it queues the reason instead, because Enter on a modal is not consent (VI.2)"
fi
unset KM_SENDKEYS_LOG

# --- a requested departure that never arrived is re-delivered ----------------
# The citizen ran `km sleep`; consent is not in question. What failed is the
# delivery, and nothing ever noticed, so the session sat alive under a status
# that said it had left. Re-delivering is the confirmation step that arm (4)
# showed the send itself can never provide.
dispatch_case '[]' '[]'
fill_pending ayg
export KM_SENDKEYS_LOG="$KM_STATE_DIR/sendkeys.log"
: >"$KM_SENDKEYS_LOG"
OUT="$(PATH="$BIN:$PATH" "$KM" reconcile 2>&1)"
RC=$?
if [ "$RC" -ne 0 ]; then
    bad "an undelivered departure is re-delivered" "rc=$RC out=$OUT"
elif ! grep -q '/exit' "$KM_SENDKEYS_LOG" 2>/dev/null; then
    bad "an undelivered departure is re-delivered" "no /exit was re-sent: $(cat "$KM_SENDKEYS_LOG" 2>/dev/null)"
else
    ok "a seat still alive under asleep-pending has its /exit re-delivered"
    assert_delivery_shape "the re-delivered /exit keeps the same three-invocation shape" "$KM_SENDKEYS_LOG"
fi

# The opposite case must stay quiet. A seat with no session is not owed a
# keystroke, and typing into a dead or absent pane is how a reconciler starts
# inventing state instead of repairing it.
dispatch_case '[]' '[]'
fill_windowless ayg
export KM_SENDKEYS_LOG="$KM_STATE_DIR/sendkeys.log"
: >"$KM_SENDKEYS_LOG"
PATH="$BIN:$PATH" "$KM" reconcile >/dev/null 2>&1
age_suspicion ayg
PATH="$BIN:$PATH" "$KM" reconcile >/dev/null 2>&1
if [ -s "$KM_SENDKEYS_LOG" ]; then
    bad "a seat with no session is sent nothing" "keys went to a pane that does not exist: $(cat "$KM_SENDKEYS_LOG")"
elif [ "$(cat "$KM_STATE_DIR/seats/ayg/status" 2>/dev/null)" != asleep ]; then
    bad "a seat with no session is sent nothing" "status not repaired: [$(cat "$KM_STATE_DIR/seats/ayg/status" 2>/dev/null)]"
else
    ok "a seat with no session is repaired silently, with no keys sent to a pane that is gone"
fi
unset KM_SENDKEYS_LOG

# A seat's liveness must not be readable off ANOTHER seat's session. tmux
# resolves a bare window target by PREFIX when no window matches exactly, and
# this roster makes that live: `ar` is a prefix of artur, arpine, aregak and
# aramazd. So `list-panes -t "=kingdom:ar"` happily returns artur's pane, and a
# predicate that trusted it would report `ar` alive on artur's claude — and
# `send_line` would type artur's /exit into artur's pane. window_up is what
# stops it, by asking for an EXACT window name first. Found by mutation: with
# that one line removed every other row here stayed green.
dispatch_case '[]' '[]'
fill_cap artur
seat_state ar awake
export KM_SENDKEYS_LOG="$KM_STATE_DIR/sendkeys.log"
: >"$KM_SENDKEYS_LOG"
prefix_row="a seat is not read as live through a longer seat's window"
if ! PATH="$BIN:$PATH" "$KM" seat-live artur; then
    bad "$prefix_row" "artur holds a window and a claude and was not seen as live; the fixture is wrong"
elif PATH="$BIN:$PATH" "$KM" seat-live ar; then
    bad "$prefix_row" "ar has no window of its own, yet borrowed artur's session by prefix match"
else
    PATH="$BIN:$PATH" "$KM" reconcile >/dev/null 2>&1
    age_suspicion ar
    PATH="$BIN:$PATH" "$KM" reconcile >/dev/null 2>&1
    if [ "$(cat "$KM_STATE_DIR/seats/ar/status" 2>/dev/null)" != asleep ]; then
        bad "$prefix_row" "ar was not freed: [$(cat "$KM_STATE_DIR/seats/ar/status" 2>/dev/null)]"
    elif [ "$(cat "$KM_STATE_DIR/seats/artur/status" 2>/dev/null)" != awake ]; then
        bad "$prefix_row" "artur was reaped alongside its own prefix: [$(cat "$KM_STATE_DIR/seats/artur/status" 2>/dev/null)]"
    elif [ -s "$KM_SENDKEYS_LOG" ]; then
        bad "$prefix_row" "keys were typed while repairing a windowless seat: $(cat "$KM_SENDKEYS_LOG")"
    else
        ok "$prefix_row, and repairing it does not touch the seat whose name it prefixes"
    fi
fi
unset KM_SENDKEYS_LOG

# Back onto the town's own config, so a leaked fixture cannot re-point the rows
# below at a file in /tmp and have them pass for the wrong reason.
unset KM_CONFIG

# --- the runner: the loop that reads a wake file (gqlc-6ha4) -----------------
# km-seat is an endless loop parked on the seat's wake file, and it is the ONLY
# consumer of one anywhere in the system. When it exits, `km wake` still appends
# a reason and still reports "queued" — true about the queue, false about the
# world — and BOTH dispatch passes then skip the seat because it has a wake file
# pending, so the undelivered wake is itself what removes the seat from routing.
# Measured 2026-08-22T18:3xZ: 7 of 15 seats, all three architects and one of two
# judges; a judge's wake had been unread for four hours while she held three
# in-progress P0 review beads.
#
# The probe cannot be seat_session_live. A HEALTHY asleep seat has no claude at
# all — that is what asleep IS — so the session probe calls every resting
# citizen dead. These first two rows are the pair that states that difference,
# because a fix that reused the session probe would pass every row below.
dispatch_case '[]' '[]'
seat_window ayg
runner_row="the runner probe is not the session probe"
if ! PATH="$BIN:$PATH" "$KM" seat-runner ayg; then
    bad "$runner_row" "a parked km-seat with no claude on the pane was read as no runner"
elif PATH="$BIN:$PATH" "$KM" seat-live ayg; then
    bad "$runner_row" "the fixture is wrong: no claude was placed and the session read live"
else
    ok "$runner_row — a healthy asleep seat has a runner and no session, and the two probes disagree about it"
fi

dispatch_case '[]' '[]'
seat_window ayg
seat_runner_dead ayg
if PATH="$BIN:$PATH" "$KM" seat-runner ayg; then
    bad "a dead runner is seen" "a window whose km-seat has exited still read as having a runner"
else
    ok "a window whose km-seat loop has exited is not read as having a runner"
fi

# The false report itself. Nothing else in the town creates a window, so a wake
# queued at a seat with no runner is a message to nobody, and Article IV.1 is
# the reason a record saying otherwise matters.
dispatch_case '[]' '[]'
export KM_NEWWINDOW_LOG="$KM_STATE_DIR/newwindows.log"
: >"$KM_NEWWINDOW_LOG"
OUT="$(PATH="$BIN:$PATH" "$KM" wake ayg --reason "a P0 review" 2>&1)"
if ! grep -qx ayg "$KM_NEWWINDOW_LOG"; then
    bad "a wake at a seat with no runner puts one back" "no km-seat window was created: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'respawned'; then
    bad "a wake at a seat with no runner puts one back" \
        "the report does not say the queue had no reader: $OUT"
elif [ ! -s "$KM_STATE_DIR/seats/ayg/wake" ]; then
    bad "a wake at a seat with no runner puts one back" "the reason was not queued at all: $OUT"
else
    ok "a wake at a seat with no runner respawns km-seat and says so, instead of reporting 'queued' to a queue with no reader"
fi
unset KM_NEWWINDOW_LOG

# THE POLARITY. reconcile's status arms look at seats recorded awake or
# asleep-pending; a dead runner lives in the `asleep` seats they skip, and an
# asleep seat with no session is exactly what a resting citizen looks like
# (III.5). So this row states the case the old shape could not reach: status
# asleep, window present, runner gone. The kill is what makes it specific — a
# seat with no window at all is respawned too, but only a window that survived
# its runner is replaced.
dispatch_case '[]' '[]'
seat_state ayg asleep
seat_window ayg
seat_runner_dead ayg
export KM_NEWWINDOW_LOG="$KM_STATE_DIR/newwindows.log"
export KM_KILLWINDOW_LOG="$KM_STATE_DIR/killwindows.log"
: >"$KM_NEWWINDOW_LOG"; : >"$KM_KILLWINDOW_LOG"
OUT="$(PATH="$BIN:$PATH" "$KM" reconcile 2>&1)"
RC=$?
polarity_row="reconcile sees a dead runner under an ASLEEP status"
if [ "$RC" -ne 0 ]; then
    bad "$polarity_row" "rc=$RC out=$OUT"
elif ! grep -qx ayg "$KM_KILLWINDOW_LOG"; then
    bad "$polarity_row" "the empty window was left standing, so the respawn happened nowhere: $OUT"
elif ! grep -qx ayg "$KM_NEWWINDOW_LOG"; then
    bad "$polarity_row" "no km-seat was put back: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'ayg had no km-seat parked on its wake file'; then
    bad "$polarity_row" "the repair is not named in the output: $OUT"
else
    ok "$polarity_row and replaces it, instead of skipping the one status the defect lives in"
fi
unset KM_NEWWINDOW_LOG KM_KILLWINDOW_LOG

# The refusal, and it is the constitutional half. A window holding a LIVE claude
# and no runner is a citizen at work behind a loop that has died: replacing the
# window would end that session (VI.2). It is reported and left alone, and the
# next pass repairs it for free once the session ends on its own.
dispatch_case '[]' '[]'
fill_cap ayg
seat_runner_dead ayg
export KM_NEWWINDOW_LOG="$KM_STATE_DIR/newwindows.log"
export KM_KILLWINDOW_LOG="$KM_STATE_DIR/killwindows.log"
: >"$KM_NEWWINDOW_LOG"; : >"$KM_KILLWINDOW_LOG"
OUT="$(PATH="$BIN:$PATH" "$KM" reconcile 2>&1)"
noforce_row="a live session is never replaced to repair its runner"
if grep -qx ayg "$KM_KILLWINDOW_LOG"; then
    bad "$noforce_row" "the window of a live claude was killed: $OUT"
elif grep -qx ayg "$KM_NEWWINDOW_LOG"; then
    bad "$noforce_row" "a second window was opened over a live session: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'ayg has a live session but no km-seat runner'; then
    bad "$noforce_row" "the state was neither repaired nor reported, so it is invisible: $OUT"
else
    ok "$noforce_row — it is reported and left standing, because ending a session nobody asked to end is VI.2"
fi
unset KM_NEWWINDOW_LOG KM_KILLWINDOW_LOG

# --- the idle seat: awake, finished, and unreachable (gqlc-5vp7) -------------
# Six seats finished their work, wrote handoffs, never ran `km sleep`, and sat
# at empty prompts for ELEVEN hours holding all five slots. The dispatcher wakes
# only ASLEEP seats, an awake seat still counts against the cap, and the only
# mail-driven wake in the whole file was Սեդրակ's — so every one of them was
# unroutable by every automatic mechanism the town had, while the board called
# them awake and the merge gate stayed shut.
#
# The idle test is the crux, and each of the three panes below is a shape that
# was actually observed. Two consecutive sightings are required, so each row
# ages its own marker the way the cadence would.
age_idle_sighting() { local s; for s in "$@"; do touch -d '2 minutes ago' "$KM_STATE_DIR/seats/$s/idle"; done; }
nudge_setup() { # <seat> — awake, live, one unread letter, inboxes made
    make_inboxes
    fill_cap "$1"
    PATH="$BIN:$PATH" "$KM" mail send "$1" -s "a bead for you" -m "please pick this up" >/dev/null 2>&1
    export KM_SENDKEYS_LOG="$KM_STATE_DIR/sendkeys.log"
    : >"$KM_SENDKEYS_LOG"
}

dispatch_case '[]' '[]'
nudge_setup ayg
pane_idle ayg
run_dispatch
first_row="an idle seat is confirmed before it is nudged"
if ! printf '%s' "$OUT" | grep -q 'confirming next pass before nudging'; then
    bad "$first_row" "the first sighting did not announce itself: $OUT"
elif [ -s "$KM_SENDKEYS_LOG" ]; then
    bad "$first_row" "keys were sent on a single sighting of a TUI: $(cat "$KM_SENDKEYS_LOG")"
else
    age_idle_sighting ayg
    run_dispatch
    if ! printf '%s' "$OUT" | grep -q 'nudged ayg'; then
        bad "an idle awake seat with unread mail is nudged" "the second pass sent nothing: $OUT"
    elif ! printf '%s' "$OUT" | grep -q 'the prompt cleared, so it was delivered'; then
        bad "an idle awake seat with unread mail is nudged" "delivery was claimed without being confirmed: $OUT"
    else
        ok "$first_row, and the second pass nudges it — the mail-driven wake is no longer Սեդրակ's alone"
        assert_delivery_shape "the nudge keeps the clear/text/Enter shape that stops the paste from swallowing it" "$KM_SENDKEYS_LOG"
    fi
fi
unset KM_SENDKEYS_LOG

# The falsifier that matters most, because getting it wrong means typing into a
# citizen's session mid-thought. A seat inside a turn shows the same empty
# prompt LINE; what separates them is the body above it.
dispatch_case '[]' '[]'
nudge_setup ayg
pane_working ayg
run_dispatch
age_idle_sighting ayg 2>/dev/null || true
run_dispatch
if [ -s "$KM_SENDKEYS_LOG" ]; then
    bad "a working seat is not nudged" "keys were typed at a seat mid-turn: $(cat "$KM_SENDKEYS_LOG")"
elif printf '%s' "$OUT" | grep -q 'nudged ayg'; then
    bad "a working seat is not nudged" "a seat with a live spinner was called idle: $OUT"
else
    ok "a seat mid-turn is not nudged, however empty its prompt line looks"
fi
unset KM_SENDKEYS_LOG

# The other falsifier, and it is why the idle test asks for the PROMPT rather
# than merely for the absence of a spinner. A frozen seat's modal covers the
# prompt box; the nudge ends in Enter, and Enter on a usage-limit modal chooses
# whichever option is highlighted. "Stop and wait / Switch to extra usage /
# Upgrade" is not a menu the dispatcher may press (VI.2, gqlc-eier).
dispatch_case '[]' '[]'
nudge_setup ayg
pane_modal ayg
run_dispatch
age_idle_sighting ayg 2>/dev/null || true
run_dispatch
if [ -s "$KM_SENDKEYS_LOG" ]; then
    bad "a seat frozen at a modal is not nudged" "an Enter was sent at a modal, which presses whatever is highlighted: $(cat "$KM_SENDKEYS_LOG")"
else
    ok "a seat frozen at a modal is not nudged, because the nudge would press a button nobody consented to"
fi
unset KM_SENDKEYS_LOG

# Confirm after the fact, always. The town's only channel to an awake seat
# dropped six messages in a row while reporting success on every one, and an
# operator repairing the stall by hand believed the town unblocked while nothing
# moved. A nudge path that reported success here would ship that same lie.
dispatch_case '[]' '[]'
nudge_setup ayg
pane_idle ayg
pane_sticky ayg
run_dispatch
age_idle_sighting ayg
run_dispatch
sticky_row="an undelivered nudge is reported as undelivered"
if printf '%s' "$OUT" | grep -q 'the prompt cleared, so it was delivered'; then
    bad "$sticky_row" "a nudge still sitting in the prompt box was reported delivered: $(pane_of ayg)"
elif ! printf '%s' "$OUT" | grep -q 'NUDGE UNDELIVERED to ayg'; then
    bad "$sticky_row" "nothing said the message never landed: $OUT"
elif ! grep -c 'Enter' "$KM_SENDKEYS_LOG" >/dev/null || [ "$(grep -c 'Enter' "$KM_SENDKEYS_LOG")" -lt 2 ]; then
    bad "$sticky_row" "the bare Enter that repaired this by hand was never re-sent: $(cat "$KM_SENDKEYS_LOG")"
else
    ok "$sticky_row, after a bare Enter is re-sent — km's own success message is not evidence of delivery"
fi
unset KM_SENDKEYS_LOG

# --- the idle seat with NOTHING in its inbox (gqlc-971s) ---------------------
# The residual hole in the pass above, and it is the measured shape rather than
# a corner: the six seats of gqlc-5vp7 FINISHED — pushed, wrote their handoffs —
# and then forgot `km sleep`. A citizen who has finished has typically just
# drained her inbox, so `pending -gt 0` is exactly the condition that is FALSE
# for her, and the mail nudge skipped her without a word. Nothing else in the
# town looks at an awake seat, so she sat there for eleven hours.
#
# The ask is the same ask: it types a line at a citizen who is demonstrably
# between turns, and it ends nobody's session. What changes is that having no
# mail is no longer a reason to say nothing to a seat that is holding a slot and
# is unroutable while it does.
nudge_setup_nomail() { # <seat> — awake, live, inbox EMPTY
    make_inboxes
    fill_cap "$1"
    export KM_SENDKEYS_LOG="$KM_STATE_DIR/sendkeys.log"
    : >"$KM_SENDKEYS_LOG"
}

dispatch_case '[]' '[]'
nudge_setup_nomail ayg
pane_idle ayg
run_dispatch
nomail_row="an idle seat with an empty inbox is asked to sleep"
if ! printf '%s' "$OUT" | grep -q 'confirming next pass before nudging'; then
    bad "$nomail_row" "a seat with no unread mail was passed over in silence, which is the eleven-hour shape: $OUT"
elif [ -s "$KM_SENDKEYS_LOG" ]; then
    bad "$nomail_row" "keys were sent on a single sighting of a TUI: $(cat "$KM_SENDKEYS_LOG")"
else
    age_idle_sighting ayg
    run_dispatch
    if ! printf '%s' "$OUT" | grep -q 'nudged ayg'; then
        bad "$nomail_row" "the second pass sent nothing, so an empty inbox still buys eleven hours of silence: $OUT"
    elif ! grep -q 'km sleep' "$KM_SENDKEYS_LOG"; then
        bad "$nomail_row" "the line typed at her does not name the one command that frees the slot: $(cat "$KM_SENDKEYS_LOG")"
    else
        ok "$nomail_row, and it is confirmed over two passes exactly as the mail nudge is"
    fi
fi
unset KM_SENDKEYS_LOG

# And it must not become a metronome. A nudge is not free: it STARTS A TURN in
# the citizen's session and spends her quota, so a seat that does not act on one
# would be typed at every two passes — 165 times over the eleven hours this pass
# exists to end. The floor is per idle episode: any sign of life clears it, so
# it delays nobody who is actually working.
dispatch_case '[]' '[]'
nudge_setup_nomail ayg
pane_idle ayg
run_dispatch
age_idle_sighting ayg
run_dispatch
: >"$KM_SENDKEYS_LOG"
run_dispatch
age_idle_sighting ayg
run_dispatch
floor_row="a seat that ignores its nudge is not nudged again immediately"
if [ -s "$KM_SENDKEYS_LOG" ]; then
    bad "$floor_row" "a second nudge was typed minutes after the first, which spends the citizen's quota on a repeat: $(cat "$KM_SENDKEYS_LOG")"
elif ! printf '%s' "$OUT" | grep -q 'still awake and idle'; then
    bad "$floor_row" "the pass went quiet instead of saying it was holding off, so the held slot is invisible again: $OUT"
else
    ok "$floor_row, and the pass says so rather than going silent about a slot still held"
fi
unset KM_SENDKEYS_LOG

# The falsifier for the floor: a seat that came back to life and went idle again
# is a NEW episode, and must be reachable. A floor keyed to the seat rather than
# to the episode would silence the town's only channel to her for its whole
# window, which is the fail-silent shape all of these beads are in.
dispatch_case '[]' '[]'
nudge_setup_nomail ayg
pane_idle ayg
run_dispatch
age_idle_sighting ayg
run_dispatch
pane_working ayg
run_dispatch
pane_idle ayg
: >"$KM_SENDKEYS_LOG"
run_dispatch
age_idle_sighting ayg
run_dispatch
episode_row="a seat that worked and went idle again is nudged again"
if [ ! -s "$KM_SENDKEYS_LOG" ]; then
    bad "$episode_row" "the floor outlived the episode it was measuring, so a citizen who came back is unreachable: $OUT"
else
    ok "$episode_row — the floor is cleared by any sign of life, so it holds off a repeat and not a citizen"
fi
unset KM_SENDKEYS_LOG

# The direction nobody reported until Նուարդ was seen mid-generation on PR #1122
# under a board that read her ASLEEP. It strands nobody; it interrupts — both
# passes wake asleep seats, so she is handed a second bead on top of the one she
# holds. A fix whose predicate is "detect an idle awake seat" passes its own
# tests and leaves this half untouched, which is why it has a row of its own.
dispatch_case '[]' '[]'
seat_state nvard asleep
seat_window nvard
seat_claude nvard
OUT="$(PATH="$BIN:$PATH" "$KM" reconcile 2>&1)"
if [ "$(cat "$KM_STATE_DIR/seats/nvard/status" 2>/dev/null)" != awake ]; then
    bad "a working seat recorded asleep is corrected" \
        "status stayed [$(cat "$KM_STATE_DIR/seats/nvard/status" 2>/dev/null)], so both passes will still route work at her: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'nvard is recorded asleep with a live session'; then
    bad "a working seat recorded asleep is corrected" "the correction is silent: $OUT"
else
    ok "a seat recorded asleep with a live session is recorded awake, so no pass hands her a second bead"
fi

# --- the frozen seat: a heartbeat the board threw away (gqlc-eier) -----------
# Both judges froze on modals at once — 80 and 152 minutes — with the merge gate
# shut and every instrument in the town reading healthy. The evidence was
# already on disk: heartbeat.json carries an `updated` timestamp, and the status
# table opened that very file, rendered context_pct, and discarded the one field
# that would have shown it.
dispatch_case '[]' '[]'
make_inboxes
fill_cap mihr
seat_heartbeat mihr 300 42
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
if ! printf '%s' "$OUT" | grep -E '^mihr' | grep -q '5m'; then
    bad "the board renders heartbeat age" "the age is still discarded: $(printf '%s' "$OUT" | grep -E '^mihr')"
elif printf '%s' "$OUT" | grep -q 'UNRESPONSIVE'; then
    bad "the board renders heartbeat age" "a five-minute-old heartbeat was called unresponsive: $OUT"
else
    ok "km status renders the heartbeat's age, the field it used to open the file and throw away"
fi

# Միհր's shape: awake, on a usage-limit modal, 80 minutes without a heartbeat.
# The STATE column still says awake and that is not a bug — it is the report
# that is true about its own input and false about the world, and the point is
# that a second column now disagrees with it out loud.
dispatch_case '[]' '[]'
make_inboxes
fill_cap mihr
seat_heartbeat mihr 4800 42
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
frozen_row="a frozen seat is named rather than counted as a working citizen"
if ! printf '%s' "$OUT" | grep -E '^mihr' | grep -q '1h20m'; then
    bad "$frozen_row" "the 80-minute age is not rendered: $(printf '%s' "$OUT" | grep -E '^mihr')"
elif ! printf '%s' "$OUT" | grep '^UNRESPONSIVE' | grep -q mihr; then
    bad "$frozen_row" "no line names the seat, so the operator has nothing to act on: $OUT"
elif ! printf '%s' "$OUT" | grep -E '^mihr' | grep -q awake; then
    bad "$frozen_row" "the STATE column was rewritten; the two accounts must be able to disagree in the open: $OUT"
else
    ok "$frozen_row, while the STATE column keeps saying what the status FILE says"
fi

# Անահիտ's shape, and the reason the age is not read for `awake` alone: she had
# written her handoff and run `km sleep`, so she froze under asleep-pending on
# the shutdown modal. A column that looked at awake seats only would have shown
# a dash for the worse of the two freezes.
dispatch_case '[]' '[]'
make_inboxes
fill_pending anahit
seat_heartbeat anahit 9120 61
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
if ! printf '%s' "$OUT" | grep '^UNRESPONSIVE' | grep -q anahit; then
    bad "a seat frozen under asleep-pending is named too" \
        "152 minutes without a heartbeat under asleep-pending passed unremarked: $OUT"
else
    ok "a seat frozen under asleep-pending is named too, which is the half a column reading only 'awake' would have missed"
fi

# Unknown is not zero. A heartbeat that cannot be parsed says NOTHING about the
# seat, and rendering it as an age would say the most reassuring possible thing.
dispatch_case '[]' '[]'
make_inboxes
fill_cap ayg
mkdir -p "$KM_STATE_DIR/seats/ayg"
printf 'not json at all\n' >"$KM_STATE_DIR/seats/ayg/heartbeat.json"
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
if ! printf '%s' "$OUT" | grep -E '^ayg' | grep -q '?'; then
    bad "an unreadable heartbeat renders as unknown" "row: $(printf '%s' "$OUT" | grep -E '^ayg')"
elif printf '%s' "$OUT" | grep -E '^ayg' | grep -qE '(^| )0s'; then
    bad "an unreadable heartbeat renders as unknown" "it was rendered as a fresh heartbeat: $(printf '%s' "$OUT" | grep -E '^ayg')"
else
    ok "a heartbeat that cannot be parsed renders as unknown rather than as a zero-second age"
fi

# --- a seat whose CHECKOUT cannot report is named, not read as idle (h17n) ---
# heartbeat.json is written by no heartbeat script. It is a side effect of
# kingdom/bin/km-statusline, which runs only because .claude/settings.json wires
# it as the statusLine command — and both of those are TRACKED. So a seat's
# ability to report its own liveness is a property of the COMMIT its worktree is
# parked on. Վահագն's seat sat on a branch forked three days before the kingdom
# landed and went silent for a whole night's work, while the board's blank HB
# column read exactly like the eight genuinely idle seats beside him.
#
# The rows run from the hermetic $FIXTURE, because km derives seat_worktree from
# its CWD. Run from the real checkout they would read whatever seat worktrees
# happen to exist on the operator's disk — green on his box and answering a
# different question on every other.
status_at_fixture() { OUT="$(cd "$FIXTURE" && PATH="$BIN:$PATH" "$KM" status 2>&1)"; RC=$?; }
seat_checkout() { # <seat> <wired|unwired>
    local wt="$FIXTURE-seat-$1"
    mkdir -p "$wt/.claude" "$wt/kingdom/bin"
    rm -f "$wt/.claude/settings.local.json"
    if [ "$2" = wired ]; then
        printf '#!/usr/bin/env bash\n' >"$wt/kingdom/bin/km-statusline"
        printf '{"statusLine":{"type":"command","command":"kingdom/bin/km-statusline"}}\n' \
            >"$wt/.claude/settings.json"
    elif [ "$2" = halfwired ]; then
        # Script present, wiring absent — the half the file-existence check
        # cannot see. Reachable two ways: a branch that carries kingdom/bin but
        # forked before the statusLine key was added, and a checkout whose
        # settings.json was replaced by hand. Nothing runs km-statusline in
        # either, so the seat is exactly as silent as the fully-unwired one.
        printf '#!/usr/bin/env bash\n' >"$wt/kingdom/bin/km-statusline"
        printf '{}\n' >"$wt/.claude/settings.json"
    else
        # The measured shape, and BOTH halves of it: on that branch the key was
        # absent AND km-statusline did not exist, so even an untracked local
        # override could not have pointed at anything.
        printf '{}\n' >"$wt/.claude/settings.json"
        rm -f "$wt/kingdom/bin/km-statusline"
    fi
}

dispatch_case '[]' '[]'
make_inboxes
fill_cap vahagn
seat_checkout vahagn unwired
status_at_fixture
blind_row="a live seat whose checkout has no statusline wiring is named blind"
if [ "$RC" -ne 0 ]; then
    bad "$blind_row" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -E '^vahagn' | grep -q 'blind'; then
    bad "$blind_row" "the HB column still reads as an ordinary silence: $(printf '%s' "$OUT" | grep -E '^vahagn')"
elif ! printf '%s' "$OUT" | grep '^BLIND' | grep -q vahagn; then
    bad "$blind_row" "no line names the seat, so the operator has nothing to act on: $OUT"
elif ! printf '%s' "$OUT" | grep '^BLIND' | grep -q 'seat-refresh'; then
    bad "$blind_row" "the line names the seat but not the remedy: $(printf '%s' "$OUT" | grep '^BLIND')"
else
    ok "$blind_row, so the instrument's own silence is no longer indistinguishable from an idle seat"
fi

# The same claim through the OTHER half of the guard. seat_can_report asks two
# questions — is km-statusline there, and does settings.json wire it — and the
# row above answers only the first, because its fixture deletes the script and
# the file check short-circuits before the grep ever runs. Measured, not
# supposed: blinding the grep (to a pattern that matches nothing, then `|| true`)
# left the suite at 214/0 on 2026-08-23. So this row pins the wiring half on a
# fixture where the script IS present, and it is the only row that can fail when
# the settings.json check stops discriminating.
dispatch_case '[]' '[]'
make_inboxes
fill_cap vahagn
seat_checkout vahagn halfwired
status_at_fixture
half_row="a checkout that has km-statusline but does not wire it is blind too"
if [ "$RC" -ne 0 ]; then
    bad "$half_row" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -E '^vahagn' | grep -q 'blind'; then
    bad "$half_row" "the present script was taken for a working instrument: $(printf '%s' "$OUT" | grep -E '^vahagn')"
elif ! printf '%s' "$OUT" | grep '^BLIND' | grep -q vahagn; then
    bad "$half_row" "no line names the seat: $OUT"
else
    ok "$half_row — the guard reads the wiring, not merely the presence of the file"
fi

# The falsifier that keeps the new arm from swallowing the old signal. A seat
# that CAN report and simply is not running is the ordinary case — nine of
# fifteen seats on the day this was measured — and calling those blind would
# turn one over-broad reading of a blank into another.
dispatch_case '[]' '[]'
make_inboxes
fill_cap vahagn
seat_checkout vahagn wired
status_at_fixture
wired_row="a wired checkout with no heartbeat yet is not called blind"
if printf '%s' "$OUT" | grep -q '^BLIND'; then
    bad "$wired_row" "a seat that can report was reported as unable to: $(printf '%s' "$OUT" | grep '^BLIND')"
elif printf '%s' "$OUT" | grep -E '^vahagn' | grep -q 'blind'; then
    bad "$wired_row" "the HB column claims the instrument is out: $(printf '%s' "$OUT" | grep -E '^vahagn')"
else
    ok "$wired_row — the misconfiguration arm reads the checkout, not the absence of the file"
fi

# And a seat that is not live at all says nothing either way: an asleep seat has
# no session to hear from, so its blank is the reading that was always correct.
dispatch_case '[]' '[]'
make_inboxes
seat_state vahagn asleep
seat_checkout vahagn unwired
status_at_fixture
asleep_row="an asleep seat is not called blind, however its checkout is wired"
if printf '%s' "$OUT" | grep -q '^BLIND'; then
    bad "$asleep_row" "a seat with no session was reported as a broken instrument: $(printf '%s' "$OUT" | grep '^BLIND')"
else
    ok "$asleep_row"
fi

# The heartbeat still wins where there is one. A seat that HAS reported has
# demonstrated the wiring works, whatever a later read of its checkout says, and
# a blind marker there would overwrite a real age with a guess.
dispatch_case '[]' '[]'
make_inboxes
fill_cap vahagn
seat_heartbeat vahagn 300 42
seat_checkout vahagn unwired
status_at_fixture
beats_row="a seat that has actually reported is rendered by its heartbeat, not by its checkout"
if ! printf '%s' "$OUT" | grep -E '^vahagn' | grep -q '5m'; then
    bad "$beats_row" "the age was replaced: $(printf '%s' "$OUT" | grep -E '^vahagn')"
elif printf '%s' "$OUT" | grep -q '^BLIND'; then
    bad "$beats_row" "a seat with a five-minute-old heartbeat was called blind: $OUT"
else
    ok "$beats_row"
fi

# --- the awake-and-idle seat is NAMED on the board (gqlc-971s) ----------------
# The state the town can enter by ordinary SUCCESS: a citizen finishes, writes
# her handoff, and does not run `km sleep`. She is then awake with a live
# session at an empty prompt, which is the LEAST available state a seat can be
# in — every dispatch pass wakes ASLEEP seats only, so she is unroutable, while
# the live session still spends one of max_active's slots. Six of these held all
# five slots for eleven hours and the board called every one of them healthy
# (gqlc-5vp7), because `awake` and `awake and working` are the same cell.
#
# The board is where this belongs. `km reconcile` cannot end such a session —
# that is VI.2, and the bead this row is filed under is the argument — but an
# operator who can SEE the seat fixes it in one command, and marking costs
# nobody's uncommitted work.
dispatch_case '[]' '[]'
make_inboxes
fill_cap ayg
pane_idle ayg
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
idle_row="an awake seat idle at its prompt is named on the board"
if ! printf '%s' "$OUT" | grep '^IDLE' | grep -q ayg; then
    bad "$idle_row" "no line names the seat, so the slot it holds is invisible exactly as it was for eleven hours: $OUT"
elif ! printf '%s' "$OUT" | grep '^IDLE' | grep -q 'km sleep'; then
    bad "$idle_row" "the line names the seat but not the remedy: $(printf '%s' "$OUT" | grep '^IDLE')"
elif ! printf '%s' "$OUT" | grep -E '^ayg' | grep -q 'awake-idle'; then
    bad "$idle_row" "the STATE cell still reads like a working citizen: $(printf '%s' "$OUT" | grep -E '^ayg')"
else
    ok "$idle_row, with the remedy, and its STATE cell no longer reads like a working citizen"
fi

# The falsifier that matters most, and it is the same one the nudge has: a seat
# INSIDE a turn renders the same empty prompt line below its spinner. Naming her
# idle would send an operator to run `km sleep --seat` at a citizen mid-work,
# which is ending a session against her will by way of a report (VI.2).
dispatch_case '[]' '[]'
make_inboxes
fill_cap ayg
pane_working ayg
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
working_row="a seat mid-turn is not named idle, however empty its prompt line looks"
if printf '%s' "$OUT" | grep -q '^IDLE'; then
    bad "$working_row" "a seat with a live spinner was named idle: $(printf '%s' "$OUT" | grep '^IDLE')"
elif printf '%s' "$OUT" | grep -E '^ayg' | grep -q 'awake-idle'; then
    bad "$working_row" "the STATE cell called a working citizen idle: $(printf '%s' "$OUT" | grep -E '^ayg')"
else
    ok "$working_row"
fi

# And a FROZEN seat is not idle either, which is a separate claim from the one
# above and needs its own row. A modal covers the input box, so there is no
# prompt line at all — and the remedy this line prints is `km sleep --seat`,
# which sends /exit and an Enter. An Enter on a usage-limit modal CHOOSES
# whichever option is highlighted (gqlc-eier, two judges, 80 and 152 minutes).
# The frozen seat already has a line of its own, UNRESPONSIVE, whose remedy is
# to read the pane and press nothing.
dispatch_case '[]' '[]'
make_inboxes
fill_cap ayg
pane_modal ayg
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
modal_row="a seat frozen at a modal is not named idle"
if printf '%s' "$OUT" | grep -q '^IDLE'; then
    bad "$modal_row" "a frozen seat was named idle, and the remedy that line prints presses a button nobody consented to: $(printf '%s' "$OUT" | grep '^IDLE')"
else
    ok "$modal_row, because the remedy an IDLE line prints would press whatever the modal has highlighted"
fi

# A seat recorded awake with NO session is a different fault with a different
# remedy — a stale record, which cmd_reconcile frees on its own without asking
# anybody. Reading it as idle would send an operator to sleep a seat that is
# already gone, and would hide the one case the town repairs automatically.
#
# fill_zombie and not fill_windowless, and the difference is the whole row.
# A windowless seat is rejected by seat_pane's OWN window_up guard, so that
# fixture passes with the session check deleted outright — measured, mutation
# M7: removing `seat_session_live` from seat_pane_idle left the suite at 284/0.
# The zombie has a window, and a pane that reads exactly like an idle citizen's;
# what it does not have is a claude on the tty, and the session probe is the
# only thing in the file that can tell.
dispatch_case '[]' '[]'
make_inboxes
fill_zombie ayg
pane_idle ayg
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
stale_rec_row="a seat recorded awake with no session is not named idle"
if printf '%s' "$OUT" | grep -q '^IDLE'; then
    bad "$stale_rec_row" "a stale record was reported as a live citizen holding a slot: $(printf '%s' "$OUT" | grep '^IDLE')"
else
    ok "$stale_rec_row — that is a stale record, and reconcile frees it needing nobody's consent"
fi

# The status guard is `awake` and not merely "a live session at an empty
# prompt", and this is the row that says why. A seat RECORDED ASLEEP with a live
# session is Նուարդ's shape (gqlc-5vp7) — and it is not this waste at all: both
# dispatch passes wake asleep seats, so that record leaves her REACHABLE, which
# is the one thing the awake record takes away. Her repair is cmd_reconcile's
# own arm, which corrects the status without asking anybody. Naming her here
# would point an operator at `km sleep --seat` for a seat the town can already
# route work to, and the fixture is deliberately LIVE so this row fails if the
# status guard stops discriminating rather than passing on an absent session.
dispatch_case '[]' '[]'
make_inboxes
seat_state ayg asleep
seat_window ayg
seat_claude ayg
pane_idle ayg
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
asleep_idle_row="a live seat RECORDED asleep is not named idle"
if printf '%s' "$OUT" | grep -q '^IDLE'; then
    bad "$asleep_idle_row" "an asleep record was named: $(printf '%s' "$OUT" | grep '^IDLE')"
else
    ok "$asleep_idle_row — an asleep record is the one the dispatcher can still route to, and reconcile corrects it needing nobody's consent"
fi

# --- the contract with real tmux ---------------------------------------------
# Every row above stands on a stubbed tmux and a stubbed ps, and a stub encodes
# what I BELIEVE those tools do. The belief is load-bearing here in a way it was
# not before: `#{pane_tty}` and `ps -t <tty> -o comm=` are now the town's
# definition of whether a seat is alive, so if either stopped meaning what it
# means, every row above would stay green while the cap went back to guessing.
# This row is the one place the real tools are asked. It runs in a throwaway
# session of its own — KM_TMUX_SESSION exists so it cannot read the live town.
real_tmux="seat_session_live agrees with real tmux about whether a session exists"
if ! command -v tmux >/dev/null 2>&1; then
    printf 'skip - %s: no tmux on PATH\n' "$real_tmux"
else
    export KM_STATE_DIR="$TMP/real-tmux"
    mkdir -p "$KM_STATE_DIR"
    realbin="$TMP/realbin"
    mkdir -p "$realbin"
    # comm is the basename of the executed binary, so a COPY named claude is
    # what makes `ps -o comm=` say claude. A shell script would report its
    # interpreter and prove nothing.
    cp "$(command -v sleep)" "$realbin/claude"
    export KM_TMUX_SESSION="km-test-$$"
    tmux new-session -d -s "$KM_TMUX_SESSION" -n ayg -x 80 -y 24 "$realbin/claude 120" 2>/dev/null
    # A pane exists before its child has exec'd, so polling here is not slack in
    # the test — it is the same startup gap that the two-pass confirmation above
    # exists to survive, showing up as a real property of real tmux.
    for _ in $(seq 1 50); do
        "$KM" seat-live ayg && break
        sleep 0.1
    done
    # The window exists either way; what changes underneath is only the process,
    # so a predicate that merely looked for a window could not tell these apart.
    if ! "$KM" seat-live ayg; then
        bad "$real_tmux" "a real pane running a real claude was not seen as live"
    elif "$KM" seat-live ay; then
        # The prefix row above states this as a fact about tmux and then checks
        # it against a stub that I wrote to agree. Here it is asked of tmux
        # itself: `ay` names no window, and the only reason it could come back
        # live is real tmux resolving the target onto ayg.
        bad "$real_tmux" "real tmux resolved a windowless seat onto a longer one's pane and km believed it"
    else
        tmux kill-session -t "=$KM_TMUX_SESSION" 2>/dev/null
        if "$KM" seat-live ayg; then
            bad "$real_tmux" "a seat was still live after its real session was killed"
        else
            ok "$real_tmux"
        fi
    fi
    tmux kill-session -t "=$KM_TMUX_SESSION" 2>/dev/null
    unset KM_TMUX_SESSION
fi

# The same contract for the RUNNER probe, and it needs its own row because it
# asks pgrep a different question: `-f` against the whole command line rather
# than `-x` against comm. km-seat has an `env bash` shebang, so its comm is the
# interpreter's — a probe written the way the session probe is written would
# find nothing, and every stubbed row above would stay green while no dead
# runner was ever detected again.
real_runner="seat_runner_live agrees with real tmux and real pgrep about a parked km-seat"
if ! command -v tmux >/dev/null 2>&1; then
    printf 'skip - %s: no tmux on PATH\n' "$real_runner"
else
    export KM_STATE_DIR="$TMP/real-runner"
    mkdir -p "$KM_STATE_DIR"
    runnerbin="$TMP/runnerbin"
    mkdir -p "$runnerbin"
    # A shell script, deliberately: that is what km-seat is, and the point of
    # the row is that its comm is not its name.
    printf '#!/usr/bin/env bash\nsleep 120\n' >"$runnerbin/km-seat"
    chmod +x "$runnerbin/km-seat"
    export KM_TMUX_SESSION="km-test-runner-$$"
    tmux new-session -d -s "$KM_TMUX_SESSION" -n artur -x 80 -y 24 "$runnerbin/km-seat artur" 2>/dev/null
    for _ in $(seq 1 50); do
        "$KM" seat-runner artur && break
        sleep 0.1
    done
    if ! "$KM" seat-runner artur; then
        bad "$real_runner" "a real pane running a real km-seat script was not seen as having a runner"
    elif "$KM" seat-live artur; then
        bad "$real_runner" "a parked runner with no claude was read as a live SESSION, which would call every asleep seat busy"
    elif "$KM" seat-runner ar; then
        # `ar` is a seat, and a prefix of artur, so this asks both halves at
        # once: tmux's prefix resolution and pgrep's unanchored -f pattern.
        bad "$real_runner" "a windowless seat borrowed a longer seat's runner by prefix"
    else
        tmux kill-session -t "=$KM_TMUX_SESSION" 2>/dev/null
        if "$KM" seat-runner artur; then
            bad "$real_runner" "a runner was still seen after its real window was killed"
        else
            ok "$real_runner, and a parked runner is not a live session"
        fi
    fi
    tmux kill-session -t "=$KM_TMUX_SESSION" 2>/dev/null
    unset KM_TMUX_SESSION
fi

# --- the services' last result is READ, not just journalled (gqlc-vzpn) ------
# gqlc-z1qw made the dispatcher fail-closed, so a failed run now dies and is
# recorded on the unit. Nothing read that record: `km doctor` asked only whether
# the TIMER was enabled, and the board did not mention the services at all. The
# loudness landed in `journalctl --user -u kingdom-dispatch`, where nobody
# looks — the same invisibility the fail-closed refusal was meant to end.
#
# The four states below are not a taxonomy invented here; each was measured
# against real systemd on 2026-08-22 (`systemctl --user show`), and the reason
# there are four is the third row:
#
#   loaded, ran, Result=success                     -> ok
#   loaded, ran, Result=exit-code ExecMainStatus=7  -> failed
#   LoadState=not-found                             -> Result=success ANYWAY
#   loaded, never fired                             -> InactiveEnterTimestamp=
#
# A unit that DOES NOT EXIST reports Result=success. So a report keyed on Result
# alone calls an uninstalled dispatcher healthy, which is precisely the shape of
# defect this bead exists to end.
cat >"$BIN/systemctl" <<'STUB'
#!/usr/bin/env bash
# Answers `show` from a fixture file per unit, named by KM_FAKE_SYSTEMD.
#
# Properties are printed in FIXTURE order, which is deliberately not the order
# they were asked for: real systemd does not answer in request order (measured
# — LoadState, ActiveState, ActiveEnterTimestamp, InactiveEnterTimestamp,
# Result, NRestarts, ExecMainStatus came back for a request that named them in
# another order). A stub that echoed the request order would let a positional
# parser pass here and misread the real thing.
#
# KM_FAKE_SYSTEMCTL_RC models "systemctl cannot answer" — an absent binary, or
# the absent user bus that CI has. It is one state to km either way.
if [ -n "${KM_FAKE_SYSTEMCTL_RC:-}" ]; then
    echo "Failed to connect to bus: No medium found" >&2
    exit "$KM_FAKE_SYSTEMCTL_RC"
fi
case "${2:-}" in
    show) ;;
    *) exit 0 ;;   # is-enabled and friends: not what these rows are about
esac
unit="${3:-}"
want=""
shift 3
while [ $# -gt 0 ]; do
    case "$1" in
        -p) want="$want ${2:-}"; shift ;;
    esac
    shift
done
f="${KM_FAKE_SYSTEMD:-}/$unit.props"
# No fixture = the unit is not on this machine, which is what systemd says
# about one: not-found, and a Result of success regardless.
[ -f "$f" ] || printf 'LoadState=not-found\nInactiveEnterTimestamp=\nResult=success\nExecMainStatus=0\n' >"/dev/stdout"
[ -f "$f" ] || exit 0
while IFS= read -r line; do
    for w in $want; do
        case "$line" in "$w="*) printf '%s\n' "$line" ;; esac
    done
done <"$f"
STUB
chmod +x "$BIN/systemctl"

# $1=unit, rest=KEY=VALUE property lines
fake_unit() {
    local unit=$1
    shift
    mkdir -p "$KM_FAKE_SYSTEMD"
    printf '%s\n' "$@" >"$KM_FAKE_SYSTEMD/$unit.props"
}

svc_case() { # fresh state + fixture dir for one board rendering
    dispatch_case '[]' '[]'
    make_inboxes
    export KM_FAKE_SYSTEMD="$KM_STATE_DIR/systemd"
    unset KM_FAKE_SYSTEMCTL_RC
    mkdir -p "$KM_FAKE_SYSTEMD"
}

run_status() {
    OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
    RC=$?
}

# The liveness control, and it comes first: every loud row below would pass
# against a board that shouted unconditionally, and a board that cries failure
# at a working town teaches everyone to stop reading it.
svc_case
fake_unit kingdom-dispatch.service 'LoadState=loaded' 'ActiveState=inactive' \
    'InactiveEnterTimestamp=Fri 2026-08-21 22:17:53 EDT' 'Result=success' 'ExecMainStatus=0'
fake_unit kingdom-guard.service 'LoadState=loaded' 'ActiveState=inactive' \
    'InactiveEnterTimestamp=Fri 2026-08-21 22:19:00 EDT' 'Result=success' 'ExecMainStatus=0'
run_status
if [ "$RC" -ne 0 ]; then
    bad "the board reports a healthy dispatcher quietly" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -qi 'dispatch.*ok'; then
    bad "the board reports a healthy dispatcher quietly" "no ok line for dispatch: $OUT"
elif printf '%s' "$OUT" | grep -q 'FAILED\|NEVER RUN\|NOT INSTALLED'; then
    bad "the board reports a healthy dispatcher quietly" \
        "a successful run was rendered as an alarm: $(printf '%s' "$OUT" | grep -E 'dispatch|guard')"
else
    ok "km status renders a successful last run as ok, so the loud states below mean something"
fi

# The row the bead is about.
svc_case
fake_unit kingdom-dispatch.service 'LoadState=loaded' 'ActiveState=failed' \
    'InactiveEnterTimestamp=Fri 2026-08-21 22:18:46 EDT' 'Result=exit-code' 'ExecMainStatus=7'
fake_unit kingdom-guard.service 'LoadState=loaded' 'ActiveState=inactive' \
    'InactiveEnterTimestamp=Fri 2026-08-21 22:19:00 EDT' 'Result=success' 'ExecMainStatus=0'
run_status
if [ "$RC" -ne 0 ]; then
    bad "the board reports a FAILED dispatch run" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'FAILED'; then
    bad "the board reports a FAILED dispatch run" "the board is silent about it: $OUT"
elif ! printf '%s' "$OUT" | grep 'FAILED' | grep -q 'journalctl'; then
    bad "the board reports a FAILED dispatch run" \
        "the failure line does not say where to read the run: $(printf '%s' "$OUT" | grep FAILED)"
else
    ok "km status renders a failed dispatch run loudly, and names the journal that holds it"
fi

# The measured trap: systemd reports Result=success for a unit that does not
# exist, so this state is indistinguishable from health on Result alone.
svc_case
fake_unit kingdom-dispatch.service 'LoadState=not-found' 'InactiveEnterTimestamp=' \
    'Result=success' 'ExecMainStatus=0'
run_status
if [ "$RC" -ne 0 ]; then
    bad "an uninstalled dispatcher is not reported as healthy" "rc=$RC out=$OUT"
elif printf '%s' "$OUT" | grep -E '^dispatch' | grep -qi 'ok'; then
    bad "an uninstalled dispatcher is not reported as healthy" \
        "Result=success on a not-found unit read as ok: $(printf '%s' "$OUT" | grep -E '^dispatch')"
elif ! printf '%s' "$OUT" | grep -q 'NOT INSTALLED'; then
    bad "an uninstalled dispatcher is not reported as healthy" \
        "no not-installed line: $(printf '%s' "$OUT" | grep -E '^dispatch')"
else
    ok "km status distinguishes an uninstalled unit from a healthy one, which systemd's Result does not"
fi

# Enabled but never fired: the ed2u/z1qw shape — machinery that has never done
# its job, with nothing in its own record to say so.
svc_case
fake_unit kingdom-dispatch.service 'LoadState=loaded' 'ActiveState=inactive' \
    'InactiveEnterTimestamp=' 'Result=success' 'ExecMainStatus=0'
run_status
if [ "$RC" -ne 0 ]; then
    bad "a dispatcher that has never run is not reported as healthy" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'NEVER RUN'; then
    bad "a dispatcher that has never run is not reported as healthy" \
        "no never-run line: $(printf '%s' "$OUT" | grep -E '^dispatch')"
else
    ok "km status separates 'never fired' from 'last run succeeded', which share a Result value"
fi

# The guard is the other half of the ask, and nothing else here would notice if
# only the dispatcher were wired up.
svc_case
fake_unit kingdom-dispatch.service 'LoadState=loaded' 'ActiveState=inactive' \
    'InactiveEnterTimestamp=Fri 2026-08-21 22:17:53 EDT' 'Result=success' 'ExecMainStatus=0'
fake_unit kingdom-guard.service 'LoadState=loaded' 'ActiveState=failed' \
    'InactiveEnterTimestamp=Fri 2026-08-21 22:19:00 EDT' 'Result=exit-code' 'ExecMainStatus=2'
run_status
if [ "$RC" -ne 0 ]; then
    bad "the board reports a FAILED guard run too" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -E '^guard' | grep -q 'FAILED'; then
    bad "the board reports a FAILED guard run too" \
        "guard's failure is not on the board: $(printf '%s' "$OUT" | grep -E '^guard')"
else
    ok "km status reports the guard's last run as well as the dispatcher's"
fi

# km's own header promises every tmux-touching path degrades rather than
# crashing, because CI has no tmux. CI has no user bus either, so the same
# promise has to hold for systemctl: a board that aborted here would take the
# seat table and the queue counters down with it.
svc_case
export KM_FAKE_SYSTEMCTL_RC=1
run_status
if [ "$RC" -ne 0 ]; then
    bad "an unanswerable systemctl does not abort the board" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'ready queue:'; then
    bad "an unanswerable systemctl does not abort the board" \
        "the board stopped before its counters: $OUT"
elif printf '%s' "$OUT" | grep -E '^dispatch' | grep -qi 'ok'; then
    bad "an unanswerable systemctl does not abort the board" \
        "an unanswered query read as ok: $(printf '%s' "$OUT" | grep -E '^dispatch')"
else
    ok "km status survives a systemctl that cannot answer, and does not call the silence ok"
fi
unset KM_FAKE_SYSTEMCTL_RC

# doctor asked whether the TIMER was enabled and never whether the last RUN
# worked, which is how it kept reporting a healthy town over a dead dispatcher.
svc_case
fake_unit kingdom-dispatch.service 'LoadState=loaded' 'ActiveState=failed' \
    'InactiveEnterTimestamp=Fri 2026-08-21 22:18:46 EDT' 'Result=exit-code' 'ExecMainStatus=7'
fake_unit kingdom-guard.service 'LoadState=loaded' 'ActiveState=inactive' \
    'InactiveEnterTimestamp=Fri 2026-08-21 22:19:00 EDT' 'Result=success' 'ExecMainStatus=0'
DOUT="$(PATH="$BIN:$PATH" "$KM" doctor 2>&1)"
if ! printf '%s' "$DOUT" | grep -qi 'dispatch last run'; then
    bad "km doctor checks the last run, not just the timer" "no last-run check: $DOUT"
elif printf '%s' "$DOUT" | grep -i 'dispatch last run' | grep -q '^ok:'; then
    bad "km doctor checks the last run, not just the timer" \
        "a failed run passed the check: $(printf '%s' "$DOUT" | grep -i 'dispatch last run')"
else
    ok "km doctor reports a failed last dispatch run instead of only its timer"
fi

svc_case
fake_unit kingdom-dispatch.service 'LoadState=loaded' 'ActiveState=inactive' \
    'InactiveEnterTimestamp=Fri 2026-08-21 22:17:53 EDT' 'Result=success' 'ExecMainStatus=0'
fake_unit kingdom-guard.service 'LoadState=loaded' 'ActiveState=inactive' \
    'InactiveEnterTimestamp=Fri 2026-08-21 22:19:00 EDT' 'Result=success' 'ExecMainStatus=0'
DOUT="$(PATH="$BIN:$PATH" "$KM" doctor 2>&1)"
if ! printf '%s' "$DOUT" | grep -i 'dispatch last run' | grep -q '^ok:'; then
    bad "km doctor passes a healthy dispatcher" \
        "a successful run did not pass: $(printf '%s' "$DOUT" | grep -i 'dispatch last run')"
else
    ok "km doctor passes a dispatcher whose last run succeeded"
fi

# --- does the town survive logout? (gqlc-yxnf) -------------------------------
# Systemd USER timers live in the login session's user manager, which is torn
# down at logout unless `loginctl enable-linger` holds it open. So the same
# installed, enabled, healthy pair of timers means two different things on two
# boxes: on one the town stops when the operator logs out, on the other it
# keeps dispatching with nobody attached to any pane and comes back after a
# reboot. Nothing anywhere reported which of those this box was.
#
# Neither state is an error, so this is not a pass/fail arm — it is a NAMING
# arm, and what the rows below pin is that the two states are DISTINGUISHABLE
# and each names its own consequence. A row that printed one sentence for both
# would satisfy "doctor mentions lingering" and answer nothing.
cat >"$BIN/loginctl" <<'STUB'
#!/usr/bin/env bash
# KM_FAKE_LINGER: yes | no | unanswerable (no user bus, which is CI's state).
#
# The stub is also an assertion: it refuses any call that is not a Linger
# query for a named user. If km stops asking that question, the refusal drives
# doctor down the unanswerable branch and the two definite rows below redden,
# rather than a wrong-shaped call being answered anyway.
[ "${1:-}" = show-user ] || { echo "loginctl: unexpected verb ${1:-<none>}" >&2; exit 64; }
[ -n "${2:-}" ] || { echo "loginctl: no user named" >&2; exit 64; }
want=no
shift 2
while [ $# -gt 0 ]; do
    case "$1" in -p) [ "${2:-}" = Linger ] && want=yes; shift ;; esac
    shift
done
[ "$want" = yes ] || { echo "loginctl: Linger was not asked for" >&2; exit 64; }
[ "${KM_FAKE_LINGER:-no}" != unanswerable ] || {
    echo "Failed to connect to bus: No medium found" >&2
    exit 1
}
printf 'Linger=%s\n' "${KM_FAKE_LINGER:-no}"
STUB
chmod +x "$BIN/loginctl"

linger_row() { printf '%s\n' "$DOUT" | grep -i 'linger' | head -1; }
run_linger() { # $1 = KM_FAKE_LINGER
    svc_case
    fake_unit kingdom-dispatch.service 'LoadState=loaded' 'ActiveState=inactive' \
        'InactiveEnterTimestamp=Fri 2026-08-21 22:17:53 EDT' 'Result=success' 'ExecMainStatus=0'
    fake_unit kingdom-guard.service 'LoadState=loaded' 'ActiveState=inactive' \
        'InactiveEnterTimestamp=Fri 2026-08-21 22:19:00 EDT' 'Result=success' 'ExecMainStatus=0'
    DOUT="$(KM_FAKE_LINGER="$1" PATH="$BIN:$PATH" "$KM" doctor 2>&1)"
}

# The state the bead is about: enabled, and it outlives the operator.
lrow="km doctor says out loud when the town survives logout"
run_linger yes
LON="$(linger_row)"
if [ -z "$LON" ]; then
    bad "$lrow" "doctor says nothing about lingering at all: $DOUT"
elif ! printf '%s' "$LON" | grep -q 'lingering is ON'; then
    bad "$lrow" "lingering was on and the row does not say so: $LON"
elif ! printf '%s' "$LON" | grep -q 'logout'; then
    bad "$lrow" "the row does not name the consequence it exists to report: $LON"
elif ! printf '%s' "$LON" | grep -q 'disable-linger'; then
    bad "$lrow" "the row reports a state the operator is given no way to undo: $LON"
else
    ok "$lrow, and names what to run to end it"
fi

# The other state, and it is not a failure: it is the default, and it is what
# an attended box wants. What it must not do is read like the row above.
lrow="km doctor reports a town that stops at logout as such, not as lingering"
run_linger no
LOFF="$(linger_row)"
if [ -z "$LOFF" ]; then
    bad "$lrow" "doctor says nothing about lingering at all: $DOUT"
elif [ "$LOFF" = "$LON" ]; then
    bad "$lrow" "both linger states render the same row, so the row answers nothing: $LOFF"
elif printf '%s' "$LOFF" | grep -q 'lingering is ON'; then
    bad "$lrow" "lingering was off and the row claims it is on: $LOFF"
elif ! printf '%s' "$LOFF" | grep -q 'enable-linger'; then
    bad "$lrow" "the row does not say what a headless town would need: $LOFF"
elif printf '%s' "$LOFF" | grep -q '^FAIL:'; then
    bad "$lrow" "a legitimate attended configuration was failed: $LOFF"
else
    ok "$lrow, and points at enable-linger for the headless case"
fi

# CI has no user bus — the same reason every systemctl row above is soft. An
# unanswered query must not render as either definite state, and must not take
# the rest of doctor down with it (gqlc-z1qw: the silence that reads as clean).
lrow="km doctor does not call an unanswerable loginctl a definite state"
run_linger unanswerable
LUNK="$(linger_row)"
if [ -z "$LUNK" ]; then
    bad "$lrow" "doctor went silent about lingering instead of saying it could not ask: $DOUT"
elif ! printf '%s' "$LUNK" | grep -q 'UNKNOWN'; then
    bad "$lrow" "an unanswered query rendered as an answer: $LUNK"
elif [ "$LUNK" = "$LOFF" ] || [ "$LUNK" = "$LON" ]; then
    bad "$lrow" "'could not ask' is indistinguishable from a measured state: $LUNK"
elif ! printf '%s' "$DOUT" | grep -q 'town is up'; then
    bad "$lrow" "doctor stopped at the linger arm instead of finishing: $DOUT"
else
    ok "$lrow, and finishes the remaining checks anyway"
fi

# --- the off-switch has a recipe (gqlc-yxnf) ---------------------------------
# Turning the town on was `just kingdom-install`; turning it off — removing the
# units, so nothing fires after a reboot — was an undocumented `km` subcommand
# nobody reading the README would find. `just --show` parses the real justfile,
# so this cannot pass against a recipe that does not parse; it is deliberately
# not a grep for the name.
lrow="the full off-switch is a just recipe beside the on-switch"
if ! command -v just >/dev/null 2>&1; then
    printf 'skip - %s: no just on PATH\n' "$lrow"
elif ! JUST_SHOW="$(just --justfile "$REPO/justfile" --show kingdom-uninstall 2>&1)"; then
    bad "$lrow" "no kingdom-uninstall recipe: $JUST_SHOW"
elif ! printf '%s' "$JUST_SHOW" | grep -q 'km uninstall-units'; then
    bad "$lrow" "kingdom-uninstall does not remove the units: $JUST_SHOW"
else
    ok "$lrow, and it calls km uninstall-units"
fi

rm -f "$BIN/loginctl"
unset KM_FAKE_SYSTEMD
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
    bd_in_ws() { (cd "$ws" && bd "$@"); }
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

# --- the contract with the real systemd (gqlc-vzpn) --------------------------
# The unit rows above pin km against a MODEL of systemd, and the model carries
# the whole reason km reads LoadState at all: a unit that DOES NOT EXIST is
# reported with Result=success. Were that to change — not-found answered with a
# failure, or refused outright — the stub rows would stay green while a real
# board called something healthy on evidence that no longer means health. This
# row is the one place the real binary is asked. CI has no user bus, and asking
# there proves nothing, so it skips out loud.
sd_contract="the real systemd calls a unit that does not exist Result=success, which is why km reads LoadState"
absent="km-no-such-unit-$$.service"
if ! command -v systemctl >/dev/null 2>&1; then
    printf 'skip - %s: no systemctl on PATH\n' "$sd_contract"
elif ! sd_out=$(systemctl --user show "$absent" -p LoadState -p Result 2>&1); then
    printf 'skip - %s: no user bus here (%s)\n' "$sd_contract" "$(printf '%s' "$sd_out" | head -1)"
elif ! printf '%s' "$sd_out" | grep -q '^LoadState=not-found$'; then
    bad "$sd_contract" "an absent unit no longer reports LoadState=not-found: $sd_out"
elif ! printf '%s' "$sd_out" | grep -q '^Result=success$'; then
    bad "$sd_contract" \
        "an absent unit no longer reports Result=success — the trap the parser is built around has changed, so re-argue which property discriminates: $sd_out"
else
    ok "$sd_contract"
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
        # The town's REAL mode, not an arbitrary one: km-seat now says something
        # on stderr about every other mode (bd gqlc-twmh), and rows below assert
        # that a launch it has no complaint about is SILENT. ALT_CONFIG_PM is
        # how the permission-mode rows vary it.
        printf '[claude]\npermission_mode = "%s"\n\n' "${ALT_CONFIG_PM:-bypassPermissions}"
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

# The bead ledger km-seat consults for an `effort:` label, in the same shape the
# dispatcher's bd stub uses: a fixture file named by the environment. It answers
# the MULTI-id form, because that is the one call km-seat makes, and it echoes
# the whole fixture rather than filtering — km-seat picks its own bead out of the
# answer, and a stub that filtered would hide a km-seat that took the wrong row.
# With no fixture named it answers nothing, so every argv row written before the
# label existed keeps launching exactly as it did.
cat >"$stubdir/bd" <<'STUB'
#!/usr/bin/env bash
case "${1:-}" in
    show) [ -n "${KM_TEST_BEADS:-}" ] || exit 1; cat "$KM_TEST_BEADS" ;;
    *) exit 0 ;;
esac
STUB
chmod +x "$stubdir/claude" "$stubdir/bd"

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
gitf init -q -b master "$TOWN"
gitf -C "$TOWN" config user.email t@t.invalid
gitf -C "$TOWN" config user.name t
# A WELL-FORMED town, not merely a git directory. km-seat refreshes a seat's
# worktree before launching it (bd gqlc-xtre), and refresh judges the seat
# against origin/master — so a town with no origin makes km-seat report a
# degraded town on stderr, and the argv rows below assert on stderr being
# EMPTY. Giving the fixture the origin and the real worktrees a town always has
# keeps that silence assertion at full strength instead of teaching it to
# ignore a line (Միհր, verdict-uhqh-r1 N1).
gitf init -q --bare "$TMP/town-origin.git"
gitf -C "$TOWN" commit -q --allow-empty -m base
gitf -C "$TOWN" remote add origin "$TMP/town-origin.git"
gitf -C "$TOWN" push -q origin master
gitf -C "$TOWN" worktree add -q --detach "$TOWN-seat-hayk" master
gitf -C "$TOWN" worktree add -q --detach "$TOWN-seat-mihr" master

ARGV=""
STDERR=""
compose_argv() { # compose_argv <config> [seat] [wake-lines] -> ARGV (one arg per line), STDERR
    # The wake reasons are an INPUT to the launch now, not just to the message:
    # `km wake --bead ID` writes "bead:ID <reason>", and the bead's labels can
    # move --effort. Default unchanged, so every row written before that keeps
    # asking the same question.
    local cfgfile=$1 seat=${2:-hayk} wake=${3:-a test wake} sdir pid waited=0
    ARGV="$TMP/argv.$RANDOM"
    STDERR="$TMP/stderr.$RANDOM"
    sdir="$TMP/seatstate.$RANDOM"
    SEATSTATE="$sdir" # what the launch did to the seat's state, for rows about a launch that never happened
    mkdir -p "$sdir/seats/$seat"
    printf '%s\n' "$wake" >"$sdir/seats/$seat/wake"
    (cd "$TOWN" && PATH="$stubdir:$PATH" KM_CONFIG="$cfgfile" KM_STATE_DIR="$sdir" \
        KM_TEST_ARGV="$ARGV" KM_TEST_BEADS="${BEADS_FIXTURE:-}" "$KM_SEAT" "$seat") >"$STDERR" 2>&1 &
    pid=$!
    # km-seat parks again after the stub exits, so it never runs away; we stop
    # waiting as soon as the argv lands, or give up and report what we have.
    while [ ! -s "$ARGV" ] && [ "$waited" -lt 100 ] && kill -0 "$pid" 2>/dev/null; do
        sleep 0.1
        waited=$((waited + 1))
    done
    kill "$pid" 2>/dev/null
    # SEAT_RC only means anything when km-seat exited on its own: a launch that
    # reached the stub is still parked on its wake file and is killed here, so
    # that case reports the signal. Refusal rows read it; argv rows do not.
    wait "$pid" 2>/dev/null
    SEAT_RC=$?
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

# --- the permission mode (bd gqlc-twmh) --------------------------------------
# One line of [claude] decides how every seat in the town starts, and one of the
# six values claude takes cannot work here at all: `plan` starts the session
# inside the state .githooks/claude-pre-ask exists to keep seats out of, where
# the only way forward is a plan approval and nobody is at the pane to give one.
# EnterPlanMode never fires, because there is nothing to enter.
#
# So these rows ask three different questions, and the third is the one that
# makes the first two worth having: that the mode REACHES claude (a guard on a
# value nobody passes on is decoration), that a launch km has no complaint about
# is silent, and that a refusal names the seat and the value rather than dying
# with the wake still queued and no account of why.
pm_config() { # pm_config <dest> <mode> -> a throwaway toml at that permission mode
    ALT_CONFIG_PM=$2
    alt_config "$1" 'warrior = "high"'
    ALT_CONFIG_PM=""
}

pm_config "$TMP/pm-bypass.toml" bypassPermissions
compose_argv "$TMP/pm-bypass.toml"
if [ ! -s "$ARGV" ]; then
    bad "the town's own permission mode launches a seat" "no argv; log: $(cat "$STDERR" 2>/dev/null)"
elif [ "$(grep -A1 -x -- '--permission-mode' "$ARGV" | tail -1)" != bypassPermissions ]; then
    bad "the configured permission mode reaches claude" "argv: $(argv_brief "$ARGV")"
elif [ -s "$STDERR" ]; then
    bad "bypassPermissions must launch quietly" "stderr: $(cat "$STDERR")"
else
    ok "bypassPermissions reaches claude's argv and the seat launches without a word"
fi

# THE ROW THIS BEAD IS ABOUT. Refusal, not coercion: a seat launched into plan
# mode wedges silently and open-endedly with its heartbeat still green (bd
# gqlc-n97e measured 13 minutes of exactly that), while a refusal costs one line
# of stderr and one line of kingdom.toml. Asserted at the argv, because a guard
# that warned and launched anyway would leave the wedge in place.
pm_config "$TMP/pm-plan.toml" plan
compose_argv "$TMP/pm-plan.toml"
if [ -s "$ARGV" ]; then
    bad "permission_mode = plan must not launch a seat" "argv: $(argv_brief "$ARGV")"
elif [ "$SEAT_RC" -eq 0 ]; then
    # A refusal that exits 0 reads to `km up` and to a tmux window as a seat
    # that finished its day.
    bad "a refused permission mode must exit non-zero" "rc=$SEAT_RC log: $(cat "$STDERR" 2>/dev/null)"
elif ! grep -q "'plan'" "$STDERR"; then
    bad "a refused permission mode must be named on stderr" "log: $(cat "$STDERR" 2>/dev/null)"
elif ! grep -q "approval" "$STDERR"; then
    # WHY, not merely that it was refused. Deleting the plan arm leaves `plan`
    # refused by the unknown-value arm — same verdict, and an operator told
    # only that claude does not take `plan`, which is false and sends them
    # looking for a typo.
    bad "the plan refusal must say what plan mode is waiting for" "log: $(cat "$STDERR" 2>/dev/null)"
elif ! grep -q "hayk" "$STDERR"; then
    # Which seat, not merely which value: `km up` starts sixteen of these, and a
    # refusal that does not say whose window just died is a scroll to read.
    bad "a refused permission mode must name the seat" "log: $(cat "$STDERR" 2>/dev/null)"
else
    ok "permission_mode = plan refuses to launch hayk and names both the seat and the mode"
fi

# The wake must SURVIVE the refusal, which is a claim about WHERE the guard
# sits. km-seat deletes the wake file the moment it reads it, so a guard inside
# the loop would refuse and eat the reason the seat was woken for: the town
# would lose the work quietly while looking merely misconfigured. Placed with
# the soul and worktree preflights instead, the refusal happens before any wake
# is touched, and whoever fixes the config gets the wake back.
if [ ! -s "$SEATSTATE/seats/hayk/wake" ]; then
    bad "a refused launch leaves the wake queued" "the wake file was consumed by a launch that never happened"
else
    ok "a refused permission mode leaves the seat's wake reason queued for the next attempt"
fi

# Case is not a formality here: `Plan` is not one of claude's six choices, so it
# is refused by the same guard for the other reason — claude would refuse it
# too, and dropping the flag falls back to `default`, which prompts for
# everything. Neither arm may let it through.
pm_config "$TMP/pm-case.toml" Plan
compose_argv "$TMP/pm-case.toml"
if [ -s "$ARGV" ]; then
    bad "a permission mode claude does not take must not launch a seat" "argv: $(argv_brief "$ARGV")"
elif ! grep -q "Plan" "$STDERR"; then
    bad "an unknown permission mode must be named on stderr" "log: $(cat "$STDERR" 2>/dev/null)"
else
    ok "'Plan' is not 'plan': an unknown permission mode is refused and named"
fi

pm_config "$TMP/pm-typo.toml" bypassPermission
compose_argv "$TMP/pm-typo.toml"
if [ -s "$ARGV" ]; then
    bad "a misspelled permission mode must not launch a seat" "argv: $(argv_brief "$ARGV")"
elif [ "$SEAT_RC" -eq 0 ]; then
    bad "a misspelled permission mode must exit non-zero" "rc=$SEAT_RC log: $(cat "$STDERR" 2>/dev/null)"
elif ! grep -q "bypassPermission'" "$STDERR"; then
    bad "a misspelled permission mode must be named on stderr" "log: $(cat "$STDERR" 2>/dev/null)"
else
    ok "a misspelled permission mode is refused and named, instead of falling back to prompts"
fi

# The middle tier, and the reason this is a case and not an if. acceptEdits is
# what kingdom.toml itself offers for a supervised run, so refusing it would
# take away a documented affordance; but a seat at that mode is only workable
# while someone is attached to the pane, and nothing else says so. Launch, and
# say it.
pm_config "$TMP/pm-accept.toml" acceptEdits
compose_argv "$TMP/pm-accept.toml"
if [ ! -s "$ARGV" ]; then
    bad "acceptEdits still launches the seat" "no argv; log: $(cat "$STDERR" 2>/dev/null)"
elif [ "$(grep -A1 -x -- '--permission-mode' "$ARGV" | tail -1)" != acceptEdits ]; then
    bad "acceptEdits reaches claude unchanged" "argv: $(argv_brief "$ARGV")"
elif ! grep -q "acceptEdits" "$STDERR"; then
    bad "a supervised permission mode must say so on stderr" "log: $(cat "$STDERR" 2>/dev/null)"
else
    ok "acceptEdits launches the seat unchanged and warns that the pane needs someone in it"
fi

# --- the per-bead effort escalation (Constitution V.6.2, bd gqlc-jmwh) --------
# `/effort` is a client-side TUI command parsed from USER input, so a citizen —
# which emits assistant turns — cannot invoke it, and the level was otherwise
# fixed at launch. V.6.2's right was therefore unreachable in practice. A bead
# carrying `effort:<level>` now moves the launch instead.
#
# These rows go through compose_argv like every row above: a real km-seat, a
# real wake file with a real `bead:ID` reason in it, and the argv the stub
# claude actually received. What is stubbed is bd (the labels) and claude (the
# argv sink); what EXECUTES is km-seat's whole wake path — parsing the bead id
# out of the reasons, the bd call, the label extraction, the validation, and the
# argv assembly.

BEADS_FIXTURE="$TMP/beads.json"

# Escalation. The config says `low` for warriors in the same breath, so a row
# that merely saw `xhigh` cannot be satisfied by the class default leaking
# through — the two values are different and the bead's must win.
printf '[{"id":"gqlc-deep","labels":["class:warrior","effort:xhigh"]}]' >"$BEADS_FIXTURE"
alt_config "$TMP/labelcfg.toml" 'warrior = "low"'
compose_argv "$TMP/labelcfg.toml" hayk "bead:gqlc-deep ready warrior work"
if [ ! -s "$ARGV" ]; then
    bad "an effort: label reaches the launch" "no argv; log: $(cat "$STDERR" 2>/dev/null)"
elif [ "$(grep -A1 -x -- '--effort' "$ARGV" | tail -1)" != xhigh ]; then
    bad "an effort: label reaches the launch" "argv: $(argv_brief "$ARGV")"
elif ! grep -q 'gqlc-deep' "$STDERR"; then
    bad "an escalation is said out loud" "the seat launched deeper without naming why: $(cat "$STDERR" 2>/dev/null)"
else
    ok "a bead labelled effort:xhigh launches its seat at xhigh though the class default is low"
fi

# The control the row above needs to mean anything: the SAME wake, the same bead
# id, the same config — and no effort: label. Without it, a km-seat that ignored
# the config and hard-wired xhigh whenever a bead was named would pass.
printf '[{"id":"gqlc-deep","labels":["class:warrior"]}]' >"$BEADS_FIXTURE"
compose_argv "$TMP/labelcfg.toml" hayk "bead:gqlc-deep ready warrior work"
if [ "$(grep -A1 -x -- '--effort' "$ARGV" 2>/dev/null | tail -1)" != low ]; then
    bad "an unlabelled bead leaves the class default alone" "argv: $(argv_brief "$ARGV")"
elif [ -s "$STDERR" ]; then
    bad "an unlabelled bead launches quietly" "stderr: $(cat "$STDERR")"
else
    ok "a bead with no effort: label launches at the class default, quietly"
fi

# The same gate the config gets, and for a worse threat: any citizen may write a
# label and nobody reviews one, while `claude --effort dragon` refuses to start.
# So the level is dropped, named on stderr, and the seat launches at its class
# default rather than not at all — a bad label must cost nobody their day.
printf '[{"id":"gqlc-bad","labels":["effort:dragon"]}]' >"$BEADS_FIXTURE"
compose_argv "$TMP/labelcfg.toml" hayk "bead:gqlc-bad ready warrior work"
if [ ! -s "$ARGV" ]; then
    bad "a bad effort: label still launches the seat" "no argv; log: $(cat "$STDERR" 2>/dev/null)"
elif [ "$(grep -A1 -x -- '--effort' "$ARGV" | tail -1)" != low ]; then
    bad "a bad effort: label must not reach claude" "argv: $(argv_brief "$ARGV")"
elif ! grep -q 'dragon' "$STDERR"; then
    bad "a bad effort: label must be named on stderr" "log: $(cat "$STDERR" 2>/dev/null)"
else
    ok "an unrecognised effort: label is dropped, named on stderr, and the seat launches at its class default"
fi

# The wake file APPENDS, so one wake can name several beads and they can
# disagree. The documented rule is that the FIRST bead named wins; the row pins
# both halves, because "first wins" is unobservable without the loser being
# heard from. Second bead first in the bd answer, so a km-seat that took bd's
# order rather than the wake file's fails here.
printf '[{"id":"gqlc-second","labels":["effort:max"]},{"id":"gqlc-first","labels":["effort:high"]}]' >"$BEADS_FIXTURE"
compose_argv "$TMP/labelcfg.toml" hayk "bead:gqlc-first one
bead:gqlc-second two"
if [ "$(grep -A1 -x -- '--effort' "$ARGV" 2>/dev/null | tail -1)" != high ]; then
    bad "the first bead named in a wake decides the level" "argv: $(argv_brief "$ARGV")"
elif ! grep -q 'gqlc-second also asks' "$STDERR"; then
    bad "the overruled bead is said out loud" "the second ask vanished silently: $(cat "$STDERR" 2>/dev/null)"
else
    ok "when a wake names two beads asking for different levels, the first wins and the second is named on stderr"
fi

BEADS_FIXTURE=""

# --- seat-refresh: a merged hook reaches a parked seat (gqlc-xtre) -----------
# core.hooksPath is RELATIVE, so every seat runs the hooks in its own checkout
# and acquires a merged one only when it next moves. `km up` parks a seat with
# `worktree add --detach`, and a parked seat never moves, so merging a gate
# deploys it to nobody. Measured: 7 of 14 seats had no push guard hours after
# it merged, and `git config --get core.hooksPath` read '.githooks' in every
# one of them — the indicator is clean exactly where the gate is absent.
#
# So no row here may assert that a file EXISTS. The fixture builds a real
# upstream whose second commit adds a pre-push that REFUSES, parks a seat at
# the first commit, and pushes for real: before the refresh the push is
# allowed, after it the push is refused. What is pinned is the hook RUNNING.
#
# Every fixture git call unsets GIT_*. `just test` can itself run from a hook,
# and git exports GIT_DIR to hooks, which would resolve these commands into the
# town's own repo (the same trap documented on the bd row above).

hk="$TMP/hk"
mkdir -p "$hk"

# The gate refuses by DESTINATION, the way guard-push-destination does, rather
# than refusing everything: the fixture has to push master itself, and a hook
# that refused that would only be testable by disabling it during setup.
allow_hook='#!/bin/sh
cat >/dev/null
exit 0
'
# shellcheck disable=SC2016  # the hook body is data here; it must reach the
# file unexpanded and be expanded by the shell that later runs it as a hook.
refuse_hook='#!/bin/sh
while read -r _ _ remote_ref _; do
    case "$remote_ref" in
        *probe*) echo "MERGED-GATE-SPEAKING" >&2; exit 1 ;;
    esac
done
exit 0
'

fixture_ok=1
{
    gitf init --quiet --bare "$hk/upstream.git"
    gitf clone --quiet "$hk/upstream.git" "$hk/town"
    gitf -C "$hk/town" config user.email seat@example.invalid
    gitf -C "$hk/town" config user.name "seat fixture"
    gitf -C "$hk/town" config core.hooksPath .githooks
    mkdir -p "$hk/town/.githooks"

    # C1: a pre-push that allows. This is the hook a parked seat keeps running.
    printf '%s' "$allow_hook" >"$hk/town/.githooks/pre-push"
    chmod +x "$hk/town/.githooks/pre-push"
    gitf -C "$hk/town" add -A
    gitf -C "$hk/town" commit --quiet -m "c1: a pre-push that allows"
    gitf -C "$hk/town" branch -M master
    gitf -C "$hk/town" push --quiet -u origin master
} >"$hk/setup.log" 2>&1 || fixture_ok=0

c1=$(gitf -C "$hk/town" rev-parse HEAD 2>/dev/null || true)

# Park the seats at C1, exactly as `km up` does. The last three carry the
# gqlc-d38n / gqlc-p1ek rows at the foot of this section: aramazd stays parked
# and stale, nvard is moved to master, tsovinar goes onto a branch cut from the
# stale commit. Nothing above touches them.
for s in raffi mihr vahagn artur hayk aramazd nvard tsovinar; do
    gitf -C "$hk/town" worktree add --detach --quiet "$hk/town-seat-$s" master \
        >>"$hk/setup.log" 2>&1 || fixture_ok=0
done

{
    # C2: the merged gate. Everything above is now stale.
    printf '%s' "$refuse_hook" >"$hk/town/.githooks/pre-push"
    gitf -C "$hk/town" add -A
    gitf -C "$hk/town" commit --quiet -m "c2: a pre-push that refuses"
    gitf -C "$hk/town" push --quiet origin master
} >>"$hk/setup.log" 2>&1 || fixture_ok=0

c2=$(gitf -C "$hk/town" rev-parse HEAD 2>/dev/null || true)

# Does a push from this seat worktree run a hook that refuses? Answered by
# pushing, not by reading a file. Echoes "refused" / "allowed".
seat_push_verdict() { # <seat> <branch>
    local out
    if out=$( (cd "$hk/town-seat-$1" && gitf push origin "HEAD:refs/heads/$2" 2>&1) ); then
        echo allowed
    elif printf '%s' "$out" | grep -q MERGED-GATE-SPEAKING; then
        echo refused
    else
        echo "broken: $out"
    fi
}

refresh() { # <seat> -> OUT/RC, run from inside the fixture town
    OUT="$( (cd "$hk/town" && "$KM" seat-refresh "$1") 2>&1 )"
    RC=$?
}

seat_head() { gitf -C "$hk/town-seat-$1" rev-parse HEAD 2>/dev/null || echo none; }

if [ "$fixture_ok" -ne 1 ] || [ -z "$c1" ] || [ -z "$c2" ] || [ "$c1" = "$c2" ]; then
    bad "the seat-refresh fixture builds" "$(tail -3 "$hk/setup.log" 2>&1)"
else
    ok "the seat-refresh fixture builds an upstream whose second commit adds a refusing pre-push"

    # THE DEFECT, witnessed. Without this row the refusal row below would pass
    # against a hook that never refused anything, and the suite would be green
    # whether or not the refresh did a thing.
    v=$(seat_push_verdict raffi probe-before)
    if [ "$v" != allowed ]; then
        bad "a parked seat does NOT run the merged gate" "expected the stale hook to allow, got: $v"
    else
        ok "a seat parked at c1 pushes freely — the gate merged into master does not run there"
    fi

    # THE FIX, witnessed the same way.
    refresh raffi
    if [ "$RC" -ne 0 ]; then
        bad "seat-refresh moves a parked seat" "rc=$RC out=$OUT"
    elif [ "$(seat_head raffi)" != "$c2" ]; then
        bad "seat-refresh moves a parked seat" "head is $(seat_head raffi), wanted $c2"
    else
        v=$(seat_push_verdict raffi probe-after)
        if [ "$v" != refused ]; then
            bad "a refreshed seat RUNS the merged gate" "expected a refusal, got: $v"
        else
            ok "after seat-refresh the same push is refused by the merged gate — the hook runs"
        fi
    fi

    # Idempotent: a seat already at master is left alone and says so.
    refresh raffi
    if [ "$RC" -ne 0 ] || [ "$(seat_head raffi)" != "$c2" ]; then
        bad "seat-refresh is idempotent" "rc=$RC head=$(seat_head raffi) out=$OUT"
    elif ! printf '%s' "$OUT" | grep -q current; then
        bad "seat-refresh says a current seat is current" "out=$OUT"
    else
        ok "seat-refresh on an already-current seat reports current and moves nothing"
    fi

    # WORK IN FLIGHT, half one: uncommitted changes are never discarded.
    # A refresh that clobbers a citizen's tree is worse than the hole it closes.
    echo "half-finished work" >"$hk/town-seat-mihr/scratch.txt"
    refresh mihr
    if [ "$(seat_head mihr)" != "$c1" ]; then
        bad "seat-refresh holds off a dirty seat" "it moved a dirty worktree to $(seat_head mihr)"
    elif [ ! -f "$hk/town-seat-mihr/scratch.txt" ]; then
        bad "seat-refresh holds off a dirty seat" "it destroyed uncommitted work"
    elif [ "$RC" -ne 3 ]; then
        bad "a held stale seat exits 3" "rc=$RC out=$OUT"
    else
        ok "seat-refresh refuses to move a dirty seat, keeps its uncommitted work, and exits 3"
    fi

    # WORK IN FLIGHT, half two: a seat on a branch. This is the vahagn shape —
    # on a branch, cut from a STALE master, so it carries the hole through its
    # whole bead. It must be reported, and it must not be moved off its branch.
    (cd "$hk/town-seat-vahagn" \
        && gitf checkout --quiet -b fix/in-flight \
        && gitf -c user.email=s@example.invalid -c user.name=s commit --quiet --allow-empty -m "own work") \
        >>"$hk/setup.log" 2>&1
    vahagn_head=$(seat_head vahagn)
    refresh vahagn
    if [ "$(seat_head vahagn)" != "$vahagn_head" ]; then
        bad "seat-refresh holds off a seat on a branch" "it moved to $(seat_head vahagn)"
    elif [ "$(gitf -C "$hk/town-seat-vahagn" rev-parse --abbrev-ref HEAD)" != fix/in-flight ]; then
        bad "seat-refresh holds off a seat on a branch" "it dropped the branch checkout"
    elif [ "$RC" -ne 3 ]; then
        bad "a seat on a stale branch is reported" "rc=$RC out=$OUT"
    elif ! printf '%s' "$OUT" | grep -qi 'behind'; then
        bad "a seat on a stale branch is reported" "the line does not say it is behind: $OUT"
    else
        ok "seat-refresh holds a seat that is on a branch, and says how far behind its hooks are"
    fi

    # …but it must not cry wolf, and this is the row that decides it. artur
    # cuts a branch carrying every merged hook, and THEN master moves ahead on
    # an ordinary non-hook commit. So artur is behind master and current on
    # gates at the same time, and only a count restricted to the gate paths can
    # tell those apart. Count every commit instead and the banner fires at every
    # working seat on every wake, which is how a banner stops being read.
    (cd "$hk/town-seat-artur" \
        && gitf checkout --quiet --detach master \
        && gitf checkout --quiet -b fix/fresh-cut \
        && gitf -c user.email=s@example.invalid -c user.name=s commit --quiet --allow-empty -m "own work") \
        >>"$hk/setup.log" 2>&1
    {
        echo "ordinary work, no gate in it" >"$hk/town/README.md"
        gitf -C "$hk/town" add -A
        gitf -C "$hk/town" commit --quiet -m "c3: a commit that touches no hook"
        gitf -C "$hk/town" push --quiet origin master
    } >>"$hk/setup.log" 2>&1 || bad "the fixture can move master past a seat without touching hooks" "see $hk/setup.log"
    refresh artur
    if [ "$RC" -ne 0 ]; then
        bad "a seat in flight on a CURRENT master does not warn" "rc=$RC out=$OUT"
    else
        ok "seat-refresh does not warn about a seat whose in-flight branch already carries every merged hook"
    fi

    # WORK IN FLIGHT, half three — the shape neither half above covers. A seat
    # can be DETACHED with a CLEAN tree and still be carrying work: a citizen
    # who committed without first cutting a branch. Nothing is dirty and there
    # is no branch, so "parked" reads TRUE, and the move drops that commit off
    # every ref while reporting success — measured on gqlc-gx2y as rc=0 with
    # "0 hook/settings change(s) picked up", the seat's own commit findable
    # afterwards only through the worktree reflog.
    ahead_row="seat-refresh holds a detached seat that has committed, instead of orphaning the commit"
    (cd "$hk/town-seat-hayk" \
        && gitf -c user.email=s@example.invalid -c user.name=s \
            commit --quiet --allow-empty -m "committed without cutting a branch") \
        >>"$hk/setup.log" 2>&1
    hayk_head=$(seat_head hayk)
    refresh hayk
    if [ "$hayk_head" = "$c1" ]; then
        bad "$ahead_row" "the fixture did not commit in hayk's worktree; see $hk/setup.log"
    elif [ "$(seat_head hayk)" != "$hayk_head" ]; then
        bad "$ahead_row" "it moved to $(seat_head hayk); $hayk_head is now reachable from no ref"
    elif [ "$RC" -ne 3 ]; then
        bad "$ahead_row" "rc=$RC out=$OUT"
    elif ! printf '%s' "$OUT" | grep -q 'commit(s) of its own'; then
        # The REASON, not merely the hold. Widening the dirty or branch test to
        # catch everything holds this seat too, for a cause that is not true of
        # it, and a row reading only rc=3 calls that a pass.
        bad "$ahead_row" "held, but not for the commits it carries: $OUT"
    else
        ok "$ahead_row"
    fi

    # --- gqlc-p1ek: --check reports and moves NOTHING --------------------
    # status and doctor have to be able to ask this question without acting on
    # the answer, and the reason it is a MODE rather than a second derivation
    # is written on the bead: the obvious second way to ask is to read
    # `git config --get core.hooksPath`, which returned '.githooks' in all 14
    # seats while 7 of them held no push guard. Two derivations of one fact
    # drift, and this one drifts toward reporting healthy.
    #
    # The row asserts BOTH halves. rc=3 alone passes against a --check that
    # moved the seat and then reported the staleness it had just repaired.
    chk_row="seat-refresh --check reports a stale parked seat and does not move it"
    aramazd_before=$(seat_head aramazd)
    OUT="$( (cd "$hk/town" && "$KM" seat-refresh aramazd --check) 2>&1 )"
    RC=$?
    if [ "$aramazd_before" != "$c1" ]; then
        bad "$chk_row" "the fixture did not park aramazd at c1; see $hk/setup.log"
    elif [ "$(seat_head aramazd)" != "$aramazd_before" ]; then
        bad "$chk_row" "--check MOVED the worktree to $(seat_head aramazd)"
    elif [ "$RC" -ne 3 ]; then
        bad "$chk_row" "rc=$RC — a seat still missing the merged gate must report exposed: $OUT"
    elif ! printf '%s' "$OUT" | grep -q 'STALE'; then
        bad "$chk_row" "it exited 3 without saying what it found: $OUT"
    else
        v=$(seat_push_verdict aramazd probe-check)
        if [ "$v" != allowed ]; then
            # The whole point: --check leaves the seat exactly as exposed as it
            # found it. If the push is refused, something moved the worktree and
            # the rows above measured the wrong thing.
            bad "$chk_row" "after --check the merged gate is running, so it moved something: $v"
        else
            ok "$chk_row"
        fi
    fi

    # --- gqlc-d38n / gqlc-p1ek: the board carries the tell ----------------
    # A seat 14 commits behind renders IDENTICALLY to one at master, because
    # every other cell in the table is read out of the shared state dir. It was
    # measured on the guard himself: detached at the founding commit, fifteen
    # hours old, reading a roster with one judge in it and correctly concluding
    # the roster was broken. His `git status` was clean and there was no other
    # tell anywhere in the town.
    #
    # Three seats in ONE render, because the distinction the bead insists on is
    # between two kinds of behind:
    #   aramazd  parked and behind         -> STALE. This is the defect.
    #   tsovinar on a branch, behind       -> NOT stale. A seat mid-PR is
    #                                         legitimately behind, and a tell
    #                                         that fires on it fires on every
    #                                         working seat and stops being read.
    #   nvard    at master                 -> silent.
    # A row that only asserts aramazd passes against a check that flags all
    # three.
    gitf -C "$hk/town-seat-nvard" checkout --quiet --detach master >>"$hk/setup.log" 2>&1
    (cd "$hk/town-seat-tsovinar" && gitf checkout --quiet -b fix/mid-pr) >>"$hk/setup.log" 2>&1

    dispatch_case '[]' '[]'
    make_inboxes
    st_row="km status marks a PARKED stale worktree, and does not mark a working seat that is merely behind"
    OUT="$( (cd "$hk/town" && PATH="$BIN:$PATH" "$KM" status) 2>&1 )"
    RC=$?
    stale_line=$(printf '%s\n' "$OUT" | grep '^STALE:' || true)
    exposed_line=$(printf '%s\n' "$OUT" | grep '^EXPOSED:' || true)
    if [ "$RC" -ne 0 ]; then
        bad "$st_row" "status exited $RC: $OUT"
    elif [ -z "$stale_line" ]; then
        bad "$st_row" "no STALE line at all — a seat parked two commits back rendered as healthy: $OUT"
    elif ! printf '%s' "$stale_line" | grep -q 'aramazd'; then
        bad "$st_row" "the STALE line does not name the parked seat: $stale_line"
    elif printf '%s' "$stale_line" | grep -q 'tsovinar'; then
        bad "$st_row" "it called a seat on an in-flight branch stale: $stale_line"
    elif printf '%s' "$stale_line" | grep -q 'nvard'; then
        bad "$st_row" "it called a seat sitting on origin/master stale: $stale_line"
    else
        ok "$st_row"
    fi

    # The gate-coverage half (gqlc-p1ek), and it is a DIFFERENT population from
    # the one above: tsovinar is not stale but IS missing the merged pre-push,
    # because the refresh at wake deliberately does not move a seat holding
    # work — and tells only that seat, in its own banner. Nobody sweeping the
    # town could see it, and the population it covers is exactly the seats that
    # are about to push.
    ex_row="km status names every seat whose worktree is missing a merged .githooks commit, working or parked"
    if [ -z "$exposed_line" ]; then
        bad "$ex_row" "no EXPOSED line: $OUT"
    elif ! printf '%s' "$exposed_line" | grep -q 'aramazd'; then
        bad "$ex_row" "the parked seat missing the gate is not named: $exposed_line"
    elif ! printf '%s' "$exposed_line" | grep -q 'tsovinar'; then
        bad "$ex_row" "a seat on an in-flight branch cut from stale master is missing the gate and was not named: $exposed_line"
    elif printf '%s' "$exposed_line" | grep -q 'nvard'; then
        bad "$ex_row" "a seat at origin/master was reported exposed: $exposed_line"
    else
        ok "$ex_row"
    fi

    # An unanswered question is not a clean answer. Most of the roster has no
    # worktree under this fixture at all, and the cell for those seats must not
    # read the same as nvard's.
    unj_row="a seat km cannot measure gets '?' and a named UNJUDGED line, never 'ok'"
    if ! printf '%s' "$OUT" | grep -q '^UNJUDGED:.*astghik'; then
        bad "$unj_row" "a seat with no worktree was not reported unmeasurable: $OUT"
    elif ! printf '%s\n' "$OUT" | grep -E '^nvard ' | grep -q ' ok '; then
        bad "$unj_row" "the control seat sitting on origin/master does not read ok, so the cell above proves nothing: $(printf '%s\n' "$OUT" | grep -E '^nvard ')"
    elif printf '%s\n' "$OUT" | grep -E '^astghik ' | grep -q ' ok '; then
        bad "$unj_row" "an unmeasurable seat rendered as ok: $(printf '%s\n' "$OUT" | grep -E '^astghik ')"
    else
        ok "$unj_row"
    fi

    # doctor's coverage arm reads the SAME derivation. It is soft on purpose —
    # a worktree legitimately mid-PR is not an operator error, and VI.2 forbids
    # anything here from coercing the citizen standing in it — so the row reads
    # the LINE, not the exit status.
    dc_row="km doctor reports per-seat gate coverage and names the seats that lack it"
    OUT="$( (cd "$hk/town" && PATH="$BIN:$PATH" env -u KM_DEPLOY_ROOT "$KM" doctor) 2>&1 )"
    # `\.githooks`, not `gate`: doctor also prints "bd mail.delegate points at
    # km", which contains the substring, and a looser match picked that line up
    # and then reported the coverage arm missing for the wrong reason.
    cov_line=$(printf '%s\n' "$OUT" | grep '^warn:\|^ok:' | grep '\.githooks' || true)
    if [ -z "$cov_line" ]; then
        bad "$dc_row" "doctor says nothing about seat gate coverage: $OUT"
    elif ! printf '%s' "$cov_line" | grep -q '^warn:'; then
        bad "$dc_row" "two seats are missing the merged gate and doctor reported coverage clean: $cov_line"
    elif ! printf '%s' "$cov_line" | grep -q 'aramazd'; then
        bad "$dc_row" "the coverage line does not name the exposed seat: $cov_line"
    else
        ok "$dc_row"
    fi
fi

# A refresh judges a seat against origin/master, so with no origin/master there
# is no question to answer. Found by mutation: strike the refusal and every row
# above stays green while seat-refresh compares HEAD to an EMPTY ref — which
# reports a seat current, the one answer it has no evidence for.
lone_row="seat-refresh refuses to judge a seat with no origin/master, rather than calling it current"
{
    gitf init --quiet "$hk/lone"
    gitf -C "$hk/lone" -c user.email=s@example.invalid -c user.name=s \
        commit --quiet --allow-empty -m "lone"
    gitf -C "$hk/lone" worktree add --detach --quiet "$hk/lone-seat-astghik" HEAD
} >>"$hk/setup.log" 2>&1
OUT="$( (cd "$hk/lone" && "$KM" seat-refresh astghik) 2>&1 )"
RC=$?
if [ "$RC" -eq 0 ]; then
    bad "$lone_row" "it returned 0 with no origin/master to judge against: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'no origin/master'; then
    # Deliberately the exact refusal, not just "origin/master" anywhere in the
    # text: with the guard struck, seat-refresh walks on and dies later trying
    # to check out the empty ref, and THAT message names origin/master too. A
    # looser match reads that accident as the refusal working.
    bad "$lone_row" "it failed for some other reason than the missing ref: $OUT"
else
    ok "$lone_row"
fi

# Pinned on the MESSAGE, not merely on a non-zero rc: `unknown command` exits
# non-zero too, so an rc-only row stays green against a km that never grew the
# subcommand at all.
run seat-refresh no-such-seat
if [ "$RC" -eq 0 ] || ! printf '%s' "$OUT" | grep -q 'unknown seat'; then
    bad "seat-refresh refuses an unknown seat" "rc=$RC out=$OUT"
else
    ok "seat-refresh refuses a seat that is not in the roster"
fi

# --- seat-refresh --all: the sweep the condition actually needs (gqlc-k9r2) --
# The single-seat command above is correct and was unreachable at the scale of
# the defect. MEASURED 2026-08-23 across the live town: 15 of 16 seat worktrees
# were missing merged .githooks/.claude commits, 7 of them PARKED as far as 53
# commits back — all three architects, both seated judges and the guard among
# them — and a 16th roster seat had no worktree at all. Fixing that meant
# knowing which seats were parked, typing sixteen commands, and reading the
# ones that refused. An operator who has to compose the sweep himself, at the
# moment he is switching the town on, will run some of it.
#
# So `--all` exists to make the safe act the easy one. Three properties, and
# the last two are what stop it being a footgun:
#   it moves ONLY the parked seats, exactly as the single-seat path does;
#   a seat with no worktree is a reported ROW, not an abort — otherwise one
#     unseated seat (tir, live) truncates the sweep and the seats after it in
#     roster order are silently never visited;
#   it exits non-zero while any seat is still exposed or unjudged, so a script
#     or a human cannot read "it ran" as "the town is covered".
#
# A SECOND fixture, built the same way as hk above but never touched by those
# rows. The rows above deliberately dirty, branch and advance seats in hk, so a
# sweep run over it would be asserting against whatever they happened to leave.

hka="$TMP/hka"
mkdir -p "$hka"
allfix_ok=1
{
    gitf init --quiet --bare "$hka/upstream.git"
    gitf clone --quiet "$hka/upstream.git" "$hka/town"
    gitf -C "$hka/town" config user.email seat@example.invalid
    gitf -C "$hka/town" config user.name "seat fixture"
    gitf -C "$hka/town" config core.hooksPath .githooks
    mkdir -p "$hka/town/.githooks"
    printf '%s' "$allow_hook" >"$hka/town/.githooks/pre-push"
    chmod +x "$hka/town/.githooks/pre-push"
    gitf -C "$hka/town" add -A
    gitf -C "$hka/town" commit --quiet -m "a1: a pre-push that allows"
    gitf -C "$hka/town" branch -M master
    gitf -C "$hka/town" push --quiet -u origin master
} >"$hka/setup.log" 2>&1 || allfix_ok=0

a1=$(gitf -C "$hka/town" rev-parse HEAD 2>/dev/null || true)

# Four states, one per hazard the sweep has to respect. tir gets NO worktree,
# which is the live shape and the one that aborts a naive loop.
for s in artur arpine aregak astghik mihr anahit raffi vahagn aramazd nvard; do
    gitf -C "$hka/town" worktree add --detach --quiet "$hka/town-seat-$s" master \
        >>"$hka/setup.log" 2>&1 || allfix_ok=0
done
{
    # vahagn: a branch in flight. aramazd: uncommitted work. nvard: committed
    # on no branch, the shape where a move orphans the commit off every ref.
    gitf -C "$hka/town-seat-vahagn" switch -qc fix/in-flight
    printf 'half a fix\n' >"$hka/town-seat-aramazd/WIP.txt"
    printf 'a finished fix\n' >"$hka/town-seat-nvard/DONE.txt"
    gitf -C "$hka/town-seat-nvard" add -A
    gitf -C "$hka/town-seat-nvard" commit --quiet -m "nvard's own commit, on no branch"
} >>"$hka/setup.log" 2>&1 || allfix_ok=0
nvard_own=$(gitf -C "$hka/town-seat-nvard" rev-parse HEAD 2>/dev/null || true)

{
    printf '%s' "$refuse_hook" >"$hka/town/.githooks/pre-push"
    gitf -C "$hka/town" add -A
    gitf -C "$hka/town" commit --quiet -m "a2: a pre-push that refuses"
    gitf -C "$hka/town" push --quiet origin master
} >>"$hka/setup.log" 2>&1 || allfix_ok=0
a2=$(gitf -C "$hka/town" rev-parse HEAD 2>/dev/null || true)

hka_head() { gitf -C "$hka/town-seat-$1" rev-parse HEAD 2>/dev/null || echo none; }
# The push verdict, pointed at this fixture. Same argument as seat_push_verdict
# above: the question is whether the merged hook RUNS, and only a real push
# can answer it.
seat_push_verdict_a() { # <seat> <branch>
    local out
    if out=$( (cd "$hka/town-seat-$1" && gitf push origin "HEAD:refs/heads/$2" 2>&1) ); then
        echo allowed
    elif printf '%s' "$out" | grep -q MERGED-GATE-SPEAKING; then
        echo refused
    else
        echo "broken: $out"
    fi
}
refresh_all() { OUT="$( (cd "$hka/town" && "$KM" seat-refresh --all "$@") 2>&1 )"; RC=$?; }

if [ "$allfix_ok" -ne 1 ] || [ -z "$a1" ] || [ -z "$a2" ] || [ "$a1" = "$a2" ]; then
    bad "the --all fixture builds" "$(tail -3 "$hka/setup.log" 2>&1)"
elif [ "$nvard_own" = "$a1" ]; then
    bad "the --all fixture builds" "nvard's own commit did not land, so the orphaning hazard is not represented"
else
    ok "the --all fixture builds a town with parked, branched, dirty, committed and unseated seats"

    # --check FIRST, because it is what an operator runs before he trusts the
    # acting run, and because a --check that moved something would be found
    # here and nowhere else.
    chk_all="seat-refresh --all --check reports every seat and moves nothing"
    refresh_all --check
    if [ "$(hka_head artur)" != "$a1" ] || [ "$(hka_head aramazd)" != "$a1" ]; then
        bad "$chk_all" "--check moved a worktree: artur=$(hka_head artur) aramazd=$(hka_head aramazd)"
    elif [ ! -f "$hka/town-seat-aramazd/WIP.txt" ]; then
        bad "$chk_all" "--check destroyed uncommitted work"
    elif [ "$RC" -eq 0 ]; then
        bad "$chk_all" "exited 0 with seven seats still exposed: $OUT"
    elif [ "$(printf '%s\n' "$OUT" | grep -cE '^ *(artur|arpine|aregak|astghik|mihr|anahit|raffi|vahagn|aramazd|nvard|tir)\b')" -lt 11 ]; then
        # Every roster seat gets a line. A sweep that reports only the ones it
        # would act on leaves the operator unable to tell "held" from "skipped".
        bad "$chk_all" "not every seat is reported: $OUT"
    else
        ok "$chk_all"
    fi

    # The abort hazard, on its own row: tir is LAST in roster order in the live
    # kingdom.toml, so a loop that dies on him would still have printed most of
    # the table. This asserts he is reported AND that the run reached its
    # summary, which a `die` cannot do.
    tir_row="an unseated seat is a reported row, not an abort that truncates the sweep"
    if [ -d "$hka/town-seat-tir" ]; then
        bad "$tir_row" "the fixture gave tir a worktree, so the hazard is not represented"
    elif ! printf '%s\n' "$OUT" | grep -E '^ *tir\b' | grep -qi 'no worktree'; then
        bad "$tir_row" "tir's line does not say he has no worktree: $(printf '%s\n' "$OUT" | grep -E '^ *tir\b')"
    elif ! printf '%s' "$OUT" | grep -qi 'seat-refresh --all:'; then
        bad "$tir_row" "the sweep printed no summary, so it did not run to the end: $OUT"
    else
        ok "$tir_row"
    fi

    # THE ACT.
    act_all="seat-refresh --all moves every parked seat to origin/master"
    refresh_all
    moved_ok=1
    for s in artur arpine aregak astghik mihr anahit raffi; do
        [ "$(hka_head "$s")" = "$a2" ] || { moved_ok=0; moved_bad="$s at $(hka_head "$s")"; }
    done
    if [ "$moved_ok" -ne 1 ]; then
        bad "$act_all" "a parked seat was left behind: $moved_bad"
    else
        ok "$act_all"
    fi

    # Not "a file appeared": the hook RUNNING. Same argument as the single-seat
    # section — 7 of 14 seats read core.hooksPath='.githooks' while holding no
    # push guard at all, so existence proves nothing.
    push_all="after --all a refreshed seat's push is refused by the merged gate"
    if [ "$(seat_push_verdict_a raffi probe-all-1)" != refused ]; then
        bad "$push_all" "the merged hook does not run in raffi's refreshed worktree"
    else
        ok "$push_all"
    fi

    # THE THREE REFUSALS, after the acting run rather than before it: this is
    # the state in which work is actually lost, and the single-seat rows above
    # cannot see a sweep that treats "held" as "skip the guard and move on".
    held_all="--all leaves in-flight work exactly where it found it"
    if [ "$(hka_head vahagn)" = "$a2" ]; then
        bad "$held_all" "it moved a seat that was on a branch"
    elif [ "$(gitf -C "$hka/town-seat-vahagn" symbolic-ref --quiet --short HEAD)" != fix/in-flight ]; then
        bad "$held_all" "it dropped vahagn's branch checkout"
    elif [ ! -f "$hka/town-seat-aramazd/WIP.txt" ]; then
        bad "$held_all" "it destroyed aramazd's uncommitted work"
    elif [ "$(hka_head aramazd)" = "$a2" ]; then
        bad "$held_all" "it moved a dirty worktree"
    elif [ "$(hka_head nvard)" != "$nvard_own" ]; then
        bad "$held_all" "it orphaned nvard's commit off every ref: head is now $(hka_head nvard)"
    else
        ok "$held_all"
    fi

    # The exit status is the only thing a morning script reads. Seven seats
    # moved and three are still exposed, so "it ran" must not read as "done".
    exposed_all="--all still exits non-zero while any seat remains exposed, and names them"
    if [ "$RC" -eq 0 ]; then
        bad "$exposed_all" "exited 0 with vahagn, aramazd and nvard still uncovered: $OUT"
    elif ! printf '%s' "$OUT" | grep -qi 'vahagn'; then
        bad "$exposed_all" "the summary does not name a held seat: $OUT"
    else
        ok "$exposed_all"
    fi

    # Idempotence, and it is not a formality: the seven moved seats must now
    # report current rather than being checked out again on every sweep.
    idem_all="a second --all moves nothing and says the refreshed seats are current"
    refresh_all
    if [ "$(hka_head artur)" != "$a2" ]; then
        bad "$idem_all" "artur moved off master on the second pass: $(hka_head artur)"
    elif ! printf '%s\n' "$OUT" | grep -E '^ *artur\b' | grep -qi 'current'; then
        bad "$idem_all" "artur is not reported current: $(printf '%s\n' "$OUT" | grep -E '^ *artur\b')"
    else
        ok "$idem_all"
    fi

    # --all and a seat name together is ambiguous — does the name narrow the
    # sweep or is it a typo for a single-seat run? Refuse rather than guess.
    both_all="seat-refresh refuses --all together with a seat name"
    OUT="$( (cd "$hka/town" && "$KM" seat-refresh --all raffi) 2>&1 )"
    RC=$?
    # Pinned on the ambiguity being NAMED, and explicitly NOT on `unknown flag`:
    # before --all existed this row passed on that message, which is non-zero
    # and carries the word "all", against a km holding no sweep at all.
    if [ "$RC" -eq 0 ]; then
        bad "$both_all" "it accepted both and guessed which was meant: $OUT"
    elif printf '%s' "$OUT" | grep -q 'unknown flag'; then
        bad "$both_all" "--all is not a flag this km knows, so this row measures nothing: $OUT"
    elif ! printf '%s' "$OUT" | grep -q 'a seat name'; then
        bad "$both_all" "the refusal does not say what is ambiguous: $OUT"
    else
        ok "$both_all"
    fi
fi

# THE WIRING ROW. The mechanism above is worth nothing if nobody calls it: a
# refresh that exists and never runs deploys exactly as many hooks as no
# refresh at all. km-seat must call it, and must call it BEFORE the session
# starts — refreshing a seat that is already working is both too late and a
# checkout under a live claude.
kseat="$REPO/kingdom/bin/km-seat"
refresh_line=$(grep -n 'seat-refresh' "$kseat" | head -1 | cut -d: -f1)
claude_line=$(grep -n '^ *claude ' "$kseat" | head -1 | cut -d: -f1)
if [ -z "$refresh_line" ]; then
    bad "km-seat refreshes the worktree at wake" "km-seat never calls seat-refresh"
elif [ -z "$claude_line" ]; then
    bad "km-seat refreshes the worktree at wake" "cannot find the claude invocation to order against"
elif [ "$refresh_line" -ge "$claude_line" ]; then
    bad "km-seat refreshes the worktree at wake" \
        "seat-refresh is called at line $refresh_line, at or after claude at line $claude_line"
else
    ok "km-seat calls seat-refresh before it launches the session"
fi

# …and the warning has to REACH the citizen. The row above pins only that
# seat-refresh is called; drop the `3)` arm that captures its output and every
# row so far stays green while a held seat is told nothing — the exact
# fail-silent shape this bead exists to close. So run the real km-seat against
# the fixture with a stand-in `claude` that records the message it was handed,
# and read the banner out of that message. vahagn is the held-and-behind seat.
banner_row="a held seat is told, in the first message of its day, that its gates are stale"
if ! command -v timeout >/dev/null 2>&1; then
    printf 'skip - %s: no timeout(1) to bound km-seat'"'"'s park loop\n' "$banner_row"
elif [ "$fixture_ok" -ne 1 ]; then
    printf 'skip - %s: the fixture did not build\n' "$banner_row"
else
    shim="$hk/bin"
    mkdir -p "$shim"
    cat >"$shim/claude" <<EOF
#!/bin/sh
# Stands in for the session: keeps the composed first message, runs nothing.
printf '%s' "\${@: -1}" >"$hk/first-message.txt"
EOF
    # ${@: -1} is bash; make sure the shim is read by one.
    sed -i '1s|.*|#!/usr/bin/env bash|' "$shim/claude"
    chmod +x "$shim/claude"

    mkdir -p "$hk/seatstate/seats/vahagn"
    echo "resume your in-progress work: gqlc-fixture" >"$hk/seatstate/seats/vahagn/wake"
    rm -f "$hk/first-message.txt"

    (
        unset "${!GIT_@}"
        cd "$hk/town" || exit 1
        PATH="$shim:$PATH" KM_STATE_DIR="$hk/seatstate" \
            timeout 20 "$REPO/kingdom/bin/km-seat" vahagn
    ) >"$hk/kmseat.log" 2>&1 || true

    if [ ! -f "$hk/first-message.txt" ]; then
        bad "$banner_row" "km-seat never composed a message: $(tail -2 "$hk/kmseat.log")"
    elif ! grep -q 'BEFORE YOU PUSH' "$hk/first-message.txt"; then
        bad "$banner_row" "no stale-gates banner in the message km-seat handed the session"
    elif ! grep -q 'gqlc-xtre' "$hk/first-message.txt"; then
        bad "$banner_row" "the banner does not cite the bead that explains it"
    elif ! grep -q 'gqlc-fixture' "$hk/first-message.txt"; then
        bad "$banner_row" "the banner displaced the wake reason instead of joining it"
    else
        ok "$banner_row"
    fi

    # The mirror image, and the one that decides whether the banner is worth
    # reading: a seat with nothing stale must be handed a message with no
    # warning in it. A banner on every wake is a banner nobody reads.
    quiet_row="a seat whose gates are current gets no warning"
    mkdir -p "$hk/seatstate/seats/artur"
    echo "resume your in-progress work: gqlc-fixture" >"$hk/seatstate/seats/artur/wake"
    rm -f "$hk/first-message.txt"
    (
        unset "${!GIT_@}"
        cd "$hk/town" || exit 1
        PATH="$shim:$PATH" KM_STATE_DIR="$hk/seatstate" \
            timeout 20 "$REPO/kingdom/bin/km-seat" artur
    ) >>"$hk/kmseat.log" 2>&1 || true

    if [ ! -f "$hk/first-message.txt" ]; then
        bad "$quiet_row" "km-seat never composed a message: $(tail -2 "$hk/kmseat.log")"
    elif grep -q 'BEFORE YOU PUSH' "$hk/first-message.txt"; then
        bad "$quiet_row" "a seat carrying every merged hook was warned anyway"
    else
        ok "$quiet_row"
    fi

    # THE DEGRADATION ARM. seat-refresh fails for causes that are not the
    # seat's doing — the lone town has no origin/master to judge against, and
    # a dead network fetch is the same shape — and a wake that died there
    # would cost a citizen the whole day over a warning. km-seat's `*)` arm
    # exists to keep the session and name the failure; it was unwitnessed.
    # Found by mutation on gqlc-gx2y: replacing the arm with an exit survived
    # 65/65, because the argv fixture used to walk through it by accident and
    # the (correct) fixture fix took that away.
    degrade_row="a refresh that FAILS still wakes the seat, and names the failure on stderr"
    mkdir -p "$hk/seatstate/seats/astghik"
    echo "resume your in-progress work: gqlc-fixture" >"$hk/seatstate/seats/astghik/wake"
    rm -f "$hk/first-message.txt"
    (
        unset "${!GIT_@}"
        cd "$hk/lone" || exit 1
        PATH="$shim:$PATH" KM_STATE_DIR="$hk/seatstate" \
            timeout 20 "$REPO/kingdom/bin/km-seat" astghik
    ) >"$hk/kmseat-lone.log" 2>&1 || true

    if [ ! -f "$hk/first-message.txt" ]; then
        bad "$degrade_row" "the failed refresh stopped the wake: $(tail -3 "$hk/kmseat-lone.log")"
    elif ! grep -q 'gqlc-fixture' "$hk/first-message.txt"; then
        bad "$degrade_row" "the seat woke, but its wake reason did not survive the failure"
    elif ! grep -q 'could not refresh' "$hk/kmseat-lone.log"; then
        bad "$degrade_row" "the failure was swallowed — nothing on stderr reports it"
    elif ! grep -q 'no origin/master' "$hk/kmseat-lone.log"; then
        # The cause, not just the fact. An arm that printed a fixed sentence
        # and discarded seat-refresh's own words passes a fact-only row while
        # telling the reader nothing they can act on.
        bad "$degrade_row" "stderr reports a failure but not seat-refresh's reason for it"
    else
        ok "$degrade_row"
    fi
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

# --- the deployed tree includes the GATES, not just kingdom/ (gqlc-d20c) -----
# Claude Code's PreToolUse hook executes .githooks/claude-pre-bash out of the
# main checkout, and so do the git hooks under core.hooksPath. Nothing advanced
# that checkout and the drift check above looked only at `-- kingdom/`, so a
# merged hook fix reached nobody working there and no instrument said a word.
#
# MEASURED 2026-08-23, first party: the shared checkout sat at 2ca5465d while
# origin/master was b323fed1, 34 commits back. PR #1333 had merged the fix for
# gqlc-gtxl; running `bd close` from a sibling worktree was still DENIED by the
# pre-fix hook, and feeding the same JSON to the merged copy was silent. Same
# input, two copies of one file, opposite verdicts. `git status` in that
# checkout was clean apart from two .beads exports, so there was nothing to
# see. The condition was found by a human comparing shas by hand.
#
# The gate paths here are the same two seat_freshness counts commits against —
# one definition of "what is a gate", so the deployed root and a seat worktree
# cannot come to disagree about it.

hooks_behind="doctor FAILS when the deployed .githooks is behind origin/master"
deploy_case doctor-hooks-behind
mkdir -p "$TMP/doctor-hooks-behind.git.seed/.githooks"
advance_origin "$TMP/doctor-hooks-behind.git" .githooks/claude-pre-bash "the merged hook"
gitf -C "$TMP/doctor-hooks-behind" fetch -q origin
run_stubbed doctor
if [ "$RC" -eq 0 ]; then
    bad "$hooks_behind" "exited 0 over a stale deployed hook — the shape that made #1333 inert: $OUT"
elif ! doctor_line | grep -q '^FAIL:'; then
    bad "$hooks_behind" "the deployed-tree row is not a FAIL: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'claude-pre-bash'; then
    # NAMES the file. An operator who is told "something drifted" cannot tell
    # whether the thing gating him is the thing that moved.
    bad "$hooks_behind" "the failure does not name the stale hook, so it cannot be acted on: $OUT"
else
    ok "$hooks_behind"
fi

# .claude/ is the other half of the same gate set — settings.json carries the
# PreToolUse wiring, so a checkout that has the hook file but not the hook
# REGISTRATION runs neither.
claude_behind="doctor FAILS when the deployed .claude is behind origin/master"
deploy_case doctor-claude-behind
mkdir -p "$TMP/doctor-claude-behind.git.seed/.claude"
advance_origin "$TMP/doctor-claude-behind.git" .claude/settings.json '{"hooks":{}}'
gitf -C "$TMP/doctor-claude-behind" fetch -q origin
run_stubbed doctor
if [ "$RC" -eq 0 ] || ! printf '%s' "$OUT" | grep -q 'settings.json'; then
    bad "$claude_behind" "rc=$RC, and the drift row does not name it: $OUT"
else
    ok "$claude_behind"
fi

# Content drift, at the right commit: the same argument the kingdom/ row above
# makes, restated for the gate paths, because these are the files most likely
# to be poked at by hand while debugging a gate.
hooks_edited="a hand-edited deployed hook is drift even when HEAD is origin/master"
deploy_case doctor-hooks-edited
mkdir -p "$TMP/doctor-hooks-edited.git.seed/.githooks"
advance_origin "$TMP/doctor-hooks-edited.git" .githooks/pre-push "the merged gate"
gitf -C "$TMP/doctor-hooks-edited" pull -q --ff-only origin master
printf 'exit 0\n' >"$TMP/doctor-hooks-edited/.githooks/pre-push"
run_stubbed doctor
if [ "$RC" -eq 0 ] || ! printf '%s' "$OUT" | grep -q 'pre-push'; then
    bad "$hooks_edited" "a neutered deployed hook at the right commit read as clean: rc=$RC out=$OUT"
else
    ok "$hooks_edited"
fi

# THE REMEDY, exercised rather than asserted. A detector whose fix is a command
# nobody has run against this condition is half a fix; `km deploy` predates the
# widened path set and had no row proving it picks these files up.
hooks_deploy="km deploy brings the stale gate files current and doctor then passes"
deploy_case doctor-hooks-deploy
mkdir -p "$TMP/doctor-hooks-deploy.git.seed/.githooks"
advance_origin "$TMP/doctor-hooks-deploy.git" .githooks/claude-pre-bash "the merged hook"
gitf -C "$TMP/doctor-hooks-deploy" fetch -q origin
run_stubbed doctor
if [ "$RC" -eq 0 ]; then
    bad "$hooks_deploy" "the precondition is not met — doctor already passed before the deploy: $OUT"
else
    run_stubbed deploy
    if [ "$RC" -ne 0 ]; then
        bad "$hooks_deploy" "km deploy refused: $OUT"
    elif [ ! -f "$TMP/doctor-hooks-deploy/.githooks/claude-pre-bash" ]; then
        bad "$hooks_deploy" "deploy reported success without the hook arriving on disk"
    else
        run_stubbed doctor
        if [ "$RC" -ne 0 ] || ! doctor_line | grep -q '^ok:'; then
            bad "$hooks_deploy" "still failing after a successful deploy: $OUT"
        else
            ok "$hooks_deploy"
        fi
    fi
fi

# The status board is where a human would actually meet this, and the DRIFT
# line there is fed by the same predicate — but through deployed_ok, a second
# call site, so a fix applied to doctor alone would leave the glance clean.
hooks_board="the status board's DRIFT line fires on a stale deployed hook, not only doctor"
deploy_case status-hooks-behind
mkdir -p "$TMP/status-hooks-behind.git.seed/.githooks"
advance_origin "$TMP/status-hooks-behind.git" .githooks/claude-pre-bash "the merged hook"
gitf -C "$TMP/status-hooks-behind" fetch -q origin
run_stubbed status
if ! printf '%s' "$OUT" | grep -q '^DRIFT:'; then
    bad "$hooks_board" "the board is silent about a stale deployed hook: $(printf '%s\n' "$OUT" | head -5)"
else
    ok "$hooks_board"
fi
export KM_DEPLOY_ROOT="$TMP/deployed"

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

# --- the timers deploy themselves rather than refusing (gqlc-nm7w) -----------
# Nothing in the town ever ran `km deploy`: no systemd unit, no km-seat, no
# justfile recipe, no line of citizen-protocol.md calls it. origin/master DOES
# move under the timers, because hold_fetch_master and cmd_seat_refresh both
# fetch. So the sequence was: a citizen merges any PR touching kingdom/ -> the
# next fetch moves the ref -> every dispatch tick and every guard tick exits
# non-zero to the journal -> the town routes nobody until a human types
# `just kingdom-deploy`. Measured: kingdom-guard.service exited 1 at 03:25:43 on
# 2026-08-23 for exactly that, naming twenty-odd kingdom/ paths.
#
# These rows are the reversal of the two that stood here, which pinned the
# refusal. The refusal is still the right answer for `doctor` and for the status
# board — they are read by a human who can act — and it is still wrong for a
# timer, whose only reader is the journal.
selfheal="a dispatcher behind origin/master deploys itself and then routes"
deploy_case dispatch-selfheal
advance_origin "$TMP/dispatch-selfheal.git" kingdom/bin/km "fixed"
gitf -C "$TMP/dispatch-selfheal" fetch -q origin
export KM_STATE_DIR="$TMP/dispatch-selfheal-state"
mkdir -p "$KM_STATE_DIR"
export KM_FAKE_READY="$KM_STATE_DIR/ready.json"
export KM_FAKE_INPROG="$KM_STATE_DIR/inprog.json"
printf '[{"id":"gqlc-w9","priority":0,"assignee":null,"labels":["class:warrior"]}]' >"$KM_FAKE_READY"
printf '[]' >"$KM_FAKE_INPROG"
run_stubbed dispatch
if [ "$RC" -ne 0 ]; then
    bad "$selfheal" "rc=$RC — the tick still fails, which is the halt: $OUT"
elif [ "$(cat "$TMP/dispatch-selfheal/kingdom/bin/km")" != fixed ]; then
    bad "$selfheal" "the deploy root did not advance: $(cat "$TMP/dispatch-selfheal/kingdom/bin/km")"
elif [ -z "$(woken_seats)" ]; then
    bad "$selfheal" "it deployed and then routed nobody, so the town is still stopped"
else
    ok "$selfheal"
fi

# The deploy is only half of it. After a successful fast-forward the km still
# executing is the copy that was just REPLACED, so finishing the tick in this
# process runs the stale code the deploy existed to retire — gqlc-ed2u exactly.
# The command is handed to the deployed copy instead, once, and the bound is
# what stops a tree that re-drifts from exec'ing forever.
selfexec="after a self-deploy the tick is handed to the deployed km, exactly once"
deploy_case dispatch-selfexec
# shellcheck disable=SC2016 # $1 is the stub's own argument, written not expanded
printf '#!/usr/bin/env bash\nprintf "deployed-km ran: %%s\\n" "$1"\n' \
    >"$TMP/dispatch-selfexec.git.seed/kingdom/bin/km"
chmod +x "$TMP/dispatch-selfexec.git.seed/kingdom/bin/km"
gitf -C "$TMP/dispatch-selfexec.git.seed" add -A
gitf -C "$TMP/dispatch-selfexec.git.seed" commit -qm "an executable deployed km"
gitf -C "$TMP/dispatch-selfexec.git.seed" push -q "$TMP/dispatch-selfexec.git" HEAD:master
gitf -C "$TMP/dispatch-selfexec" fetch -q origin
run_stubbed dispatch
if [ "$(printf '%s\n' "$OUT" | grep -c 'deployed-km ran: dispatch')" -ne 1 ]; then
    bad "$selfexec" "the deployed copy did not take over exactly once: $OUT"
else
    ok "$selfexec"
fi

guardheal="the guard sweep deploys itself too, so Րաֆֆի is not stopped by a merge"
deploy_case guard-selfheal
advance_origin "$TMP/guard-selfheal.git" kingdom/bin/km "fixed"
gitf -C "$TMP/guard-selfheal" fetch -q origin
run_stubbed guard-sweep
if [ "$RC" -ne 0 ]; then
    bad "$guardheal" "rc=$RC: $OUT"
elif [ "$(cat "$TMP/guard-selfheal/kingdom/bin/km")" != fixed ]; then
    bad "$guardheal" "the guard tick did not advance its own tree"
else
    ok "$guardheal"
fi

export KM_STATE_DIR="$TMP/dispatch-selfheal-state"

# A drift visible only in the journal is gqlc-vzpn again, and it matters MORE
# now than when the timers refused: the timers no longer stop, so the board is
# the place a human meets an un-deployable tree. status does not self-heal —
# it is a read, and a glance that silently rewrote the operator's checkout
# would be worse than the condition it reports.
deploy_case status-stale
advance_origin "$TMP/status-stale.git" kingdom/bin/km "fixed"
gitf -C "$TMP/status-stale" fetch -q origin
run_stubbed status
if ! printf '%s' "$OUT" | grep -q 'DRIFT'; then
    bad "status shows the drift" "the town's glance is silent about stale machinery: $OUT"
elif [ "$(cat "$TMP/status-stale/kingdom/bin/km")" = fixed ]; then
    bad "status shows the drift" "the glance deployed the operator's checkout under him"
else
    ok "km status announces DRIFT without deploying anything, so the condition is still visible"
fi

# Unmeasurable is not clean either, and this is the arm the no-ref rows cannot
# reach: origin/master IS present, so the guard above clears, and the failure
# happens in the diff — which exits non-zero with empty stdout, the same output
# a matching tree produces. core.bare=true is the shape that put it here: a
# stray `git init` under an exported GIT_DIR writes it into the shared config
# (see the decoy note above), after which every checkout on that repo has no
# work tree and every diff against it fails.
deploy_case doctor-unmeasurable
gitf -C "$TMP/doctor-unmeasurable" config core.bare true
run_stubbed doctor
if [ "$RC" -eq 0 ]; then
    bad "a deploy root git cannot read is drift, not clean" "doctor exited 0: $OUT"
elif ! doctor_line | grep -q '^FAIL:'; then
    bad "a deploy root git cannot read is drift, not clean" "the deployed-tree row is not a FAIL: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'cannot measure drift'; then
    bad "the refusal says it could not measure" "it reports drift without saying the measurement failed: $OUT"
else
    ok "doctor FAILS on a deploy root whose diff cannot run, and says it could not measure"
fi

# The fail-OPEN arm, and the one that decides whether gqlc-nm7w is really
# fixed. A self-heal that still exits non-zero when the heal fails has moved
# the halt rather than removed it, and core.bare=true is a deploy no
# fast-forward can repair. Two halves are asserted together: the tick keeps
# routing, AND it says loudly why it is routing from a tree it could not
# certify. Either half alone is a defect — a silent continue is a town running
# stale code behind a healthy indicator, which is gqlc-ed2u.
unmeasurable="an undeployable dispatcher warns and keeps routing rather than halting the town"
export KM_STATE_DIR="$TMP/unmeasurable-state"
mkdir -p "$KM_STATE_DIR"
export KM_FAKE_READY="$KM_STATE_DIR/ready.json"
export KM_FAKE_INPROG="$KM_STATE_DIR/inprog.json"
printf '[{"id":"gqlc-w8","priority":0,"assignee":null,"labels":["class:warrior"]}]' >"$KM_FAKE_READY"
printf '[]' >"$KM_FAKE_INPROG"
run_stubbed dispatch
if [ "$RC" -ne 0 ]; then
    bad "$unmeasurable" "rc=$RC — the tick still fails, so the town is still stopped: $OUT"
elif [ -z "$(woken_seats)" ]; then
    bad "$unmeasurable" "it routed nobody, which is the halt this bead is about"
elif ! printf '%s' "$OUT" | grep -q 'cannot measure drift'; then
    bad "$unmeasurable" "it continued without naming the root it could not read: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'WARNING'; then
    bad "$unmeasurable" "it continued SILENTLY, which is stale code behind a healthy indicator: $OUT"
else
    ok "$unmeasurable"
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

# --- deploy gets past bd's own export (gqlc-n8h3) ----------------------------
# The row that stood here pinned the opposite: deploy REFUSED when an incoming
# commit touched a locally-modified file, and .beads/issues.jsonl was cited as
# the file worth protecting. Measured 2026-08-23 in the shared checkout, that
# refusal is not a safety property, it is a permanent stop. bd re-exports that
# file continuously, so staged-modified is its STEADY STATE (125 insertions
# against 92 deletions at the time of measurement), and nearly every commit in
# this repo touches it because every bd write is exported and committed. So
# `merge --ff-only` refuses at the first attempt where HEAD is genuinely
# behind, and deploy is the remedy dispatch and guard both name — a loop with
# no exit. It read as working only because HEAD happened to equal origin/master
# and the merge was a no-op, which is why the case below advances origin TWICE:
# once under kingdom/ so the deploy has real work to do, and once under the
# export so the local dirt is genuinely in the way.
#
# Discarding the local export is lossless and it was MEASURED, not assumed. The
# Dolt DB under .beads/embeddeddolt is bd's source of truth and the jsonl is a
# passive export: a fresh `bd export` of the live ledger came back with the
# same 790 lines as the dirty working-tree copy and an empty sorted diff, and
# every id in the COMMITTED copy was present in the export too. The direction
# is what matters — RESTORING the local copy over the merged one would revert
# whatever bead rows other citizens pushed while this tree stood still, which
# is the failure the "a diffstat is not a diff" memory records.
beads_ff="deploy fast-forwards past bd's permanently-dirty export instead of refusing"
deploy_case deploy-beads-blocked
advance_origin "$TMP/deploy-beads-blocked.git" kingdom/bin/km "fixed"
advance_origin "$TMP/deploy-beads-blocked.git" .beads/issues.jsonl "export-v2"
gitf -C "$TMP/deploy-beads-blocked" fetch -q origin
printf 'local-export\n' >"$TMP/deploy-beads-blocked/.beads/issues.jsonl"
gitf -C "$TMP/deploy-beads-blocked" add .beads/issues.jsonl
printf 'local-inter\n' >"$TMP/deploy-beads-blocked/.beads/interactions.jsonl"
run_stubbed deploy
if [ "$RC" -ne 0 ]; then
    bad "$beads_ff" "rc=$RC — this is the stop the town could not get past: $OUT"
elif [ "$(cat "$TMP/deploy-beads-blocked/kingdom/bin/km")" != fixed ]; then
    bad "$beads_ff" "deploy reported success without the merged machinery arriving"
elif [ "$(cat "$TMP/deploy-beads-blocked/.beads/issues.jsonl")" != export-v2 ]; then
    bad "$beads_ff" "the tree did not take the merged export: $(cat "$TMP/deploy-beads-blocked/.beads/issues.jsonl")"
else
    ok "$beads_ff"
fi

# Lossless in the only sense available to a shell script: the bytes it dropped
# are somewhere a human can read them. The DB re-emits the export by itself;
# the copy is for the case where it cannot.
aside="the local export deploy dropped is copied aside, not merely destroyed"
copy=$(find "$KM_STATE_DIR" -path '*deploy-set-aside*' -name 'issues.jsonl' 2>/dev/null | head -1)
if [ -z "$copy" ]; then
    bad "$aside" "no copy of the discarded export exists anywhere under the state dir"
elif [ "$(cat "$copy")" != local-export ]; then
    bad "$aside" "the copy does not hold the bytes that were on disk: $(cat "$copy")"
elif ! printf '%s' "$OUT" | grep -q 'set aside'; then
    bad "$aside" "deploy dropped the operator's bytes without saying so: $OUT"
else
    ok "$aside"
fi

# The scope of the discard is the whole safety argument, so it gets its own
# row. .beads/ is bd's, regenerable, and nobody edits it by hand. Everything
# else in that tree is a human's uncommitted work, and a deploy that resolved
# it would be the blanket `reset --hard` this function exists to avoid. When
# the two are mixed, NOTHING is set aside — a partial one would leave the
# operator resolving a tree km had already edited under him.
mixed="deploy still refuses over non-bd dirt, and sets nothing aside when the dirt is mixed"
deploy_case deploy-mixed-dirt
advance_origin "$TMP/deploy-mixed-dirt.git" kingdom/bin/km "fixed"
advance_origin "$TMP/deploy-mixed-dirt.git" .beads/issues.jsonl "export-v2"
gitf -C "$TMP/deploy-mixed-dirt" fetch -q origin
printf 'local-export\n' >"$TMP/deploy-mixed-dirt/.beads/issues.jsonl"
printf 'my-uncommitted-work\n' >"$TMP/deploy-mixed-dirt/kingdom/bin/km"
before="$(gitf -C "$TMP/deploy-mixed-dirt" rev-parse HEAD)"
run_stubbed deploy
if [ "$RC" -eq 0 ]; then
    bad "$mixed" "exited 0 over a human's uncommitted work: $OUT"
elif [ "$(cat "$TMP/deploy-mixed-dirt/kingdom/bin/km")" != my-uncommitted-work ]; then
    bad "$mixed" "it clobbered work that was not bd's"
elif [ "$(cat "$TMP/deploy-mixed-dirt/.beads/issues.jsonl")" != local-export ]; then
    bad "$mixed" "it set the export aside anyway, editing a tree it then refused to move"
elif [ "$(gitf -C "$TMP/deploy-mixed-dirt" rev-parse HEAD)" != "$before" ]; then
    bad "$mixed" "HEAD moved under a refusal"
elif ! printf '%s' "$OUT" | grep -q 'kingdom/bin/km'; then
    bad "$mixed" "the refusal does not name the file in the way: $OUT"
else
    ok "$mixed"
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

# deploy now runs from inside the dispatch and guard ticks (gqlc-nm7w), and
# OnUnitActiveSec means the next tick cannot start until this one ends. So a
# fetch that hangs is a new way to stop the town — the exact failure mode the
# self-heal was added to remove. The fetch is bounded, and the bound is
# exercised rather than asserted: a transport that sleeps far longer than the
# timeout must not hold the command for anything like that long.
fetch_bound="deploy's fetch is bounded, so a hung transport cannot stall the tick that called it"
deploy_case deploy-fetch-timeout
gitf -C "$TMP/deploy-fetch-timeout" remote set-url origin ssh://git@km-test.invalid/town.git
cat >"$TMP/slow-ssh" <<'SSH'
#!/usr/bin/env bash
sleep 25
SSH
chmod +x "$TMP/slow-ssh"
before_s=$SECONDS
GIT_SSH_COMMAND="$TMP/slow-ssh" KM_FETCH_TIMEOUT=2 "$KM" deploy >/dev/null 2>&1 || true
elapsed=$((SECONDS - before_s))
if [ "$elapsed" -ge 12 ]; then
    bad "$fetch_bound" "deploy held the caller for ${elapsed}s against a 2s bound"
else
    ok "$fetch_bound"
fi

export KM_DEPLOY_ROOT="$TMP/deployed"

# --- doctor: the stranding report is a gate, not a remark (gqlc-xq8a) --------
# A check that warns and exits 0 is not a gate — the question to ask of it is
# "does it FAIL?", not "is there a check?" (gqlc-z1qw, and `just doctor`
# printing ok over a warning at gqlc-bn5r). So these rows read the exit status,
# and the clean row is what makes the dirty one mean something: without it a
# doctor that fails for an unrelated reason would look like a working gate.
#
# Two confounds are held off rather than one, because doctor now has two rows
# that can redden it. claude is stubbed above, for the deploy rows, and these
# rows need it for the same reason — it is a hard check, so its absence would
# put the exit status on a binary this suite does not care about. And
# KM_DEPLOY_ROOT is left pointing at the clean tree the line above set, so the
# drift gate stays quiet and the status is attributable to the stranding.
run_doctor() {
    OUT="$(PATH="$BIN:$PATH" "$KM" doctor 2>&1)"
    RC=$?
}

dispatch_case '[]' '[
  {"id":"gqlc-doc1","assignee":"vahagn","labels":["class:warrior"]}
]'
run_doctor
if [ "$RC" -ne 0 ]; then
    bad "doctor passes a board with nothing stranded" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q '^ok: .*stranded\|^ok: .*dispatch pass'; then
    bad "doctor passes a board with nothing stranded" "doctor does not report on stranding at all: $OUT"
else
    ok "doctor exits 0 and says so when every bead is in some dispatch pass"
fi

dispatch_case '[]' '[
  {"id":"gqlc-doc2","assignee":null,"labels":["class:warrior"]}
]'
run_doctor
if [ "$RC" -eq 0 ]; then
    bad "doctor FAILS on a bead no pass can reach" "it exited 0 over a stranded bead: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^FAIL:'; then
    bad "doctor FAILS on a bead no pass can reach" "nonzero, but no check reported FAIL — the status came from somewhere else: $OUT"
elif ! printf '%s' "$OUT" | grep '^FAIL:' | grep -q 'gqlc-doc2'; then
    bad "doctor FAILS on a bead no pass can reach" "the failing line does not name the bead, so the operator cannot act on it: $OUT"
else
    ok "doctor fails, names the stranded bead, and does not merely warn"
fi

# Fail-closed, the property the whole dispatcher was rebuilt around: a query
# that cannot be answered is not an answer of "nothing wrong".
dispatch_case '[]' '[]'
printf '1' >"$KM_FAKE_INPROG.rc"
run_doctor
if [ "$RC" -eq 0 ]; then
    bad "doctor refuses to certify what it could not read" "a failed query certified the town healthy: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^FAIL:'; then
    bad "doctor refuses to certify what it could not read" "nonzero without a FAIL line: $OUT"
else
    ok "a stranding query that fails makes doctor fail, rather than reading as a board with nothing stranded"
fi

# --- doctor: a bead's owner must be an address that can reach a person -------
# gqlc-0rv9. `.githooks/commit-msg` refuses an implausible author and the CI arm
# refuses one on a PR — both gate GIT. Nothing gated BD, and both stores consume
# the same ambient `git config user.email`. When a fixture identity leaked into
# the shared repo config, git refused the commit and bd silently wrote the false
# address onto every bead created in the window: MEASURED 2026-08-22, 15 beads
# across 5 citizens, two windows 50 minutes apart (km@test, then
# fixture@example.invalid). The human noticed at 03:20Z; the second window had
# closed at 03:01Z. The noticing arrives after the window, structurally.
#
# The clean row comes FIRST and is what makes the dirty ones mean anything —
# doctor has five other arms that can redden it, so a row reading only the exit
# status would pass against a gate that never fired. dispatch_case leaves the
# board unstranded and KM_DEPLOY_ROOT points at the clean tree.
dispatch_case '[]' '[]'
export KM_FAKE_OPEN="$KM_STATE_DIR/open.json"
printf '[{"id":"gqlc-i1","owner":"antranig.yeretzian@proton.me"},
         {"id":"gqlc-i2","owner":"ops@example-corp.io"},
         {"id":"gqlc-i3","owner":null}]' >"$KM_FAKE_OPEN"
run_doctor
id_clean="doctor passes a board whose owners are all deliverable, and says so"
if [ "$RC" -ne 0 ]; then
    bad "$id_clean" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q '^ok: .*owner'; then
    bad "$id_clean" "doctor does not report on bead ownership at all: $OUT"
else
    # example-corp.io merely CONTAINS a reserved name and is a real domain, and
    # a null owner is an unassigned bead, not a bad address. Both are here so
    # that a gate written as a substring match reddens this row rather than
    # passing the ones below for the wrong reason.
    ok "$id_clean"
fi

# RESERVED TLD. `test` and `invalid` are RFC 2606 / RFC 6761 names: nothing sent
# there reaches anyone, so an address under one belongs to a fixture by
# construction. Both live offenders are here.
id_bad="doctor FAILS and names the bead owner that cannot belong to a person"
printf '[{"id":"gqlc-i4","owner":"km@test"},
         {"id":"gqlc-i5","owner":"fixture@example.invalid"},
         {"id":"gqlc-i6","owner":"antranig.yeretzian@proton.me"}]' >"$KM_FAKE_OPEN"
run_doctor
if [ "$RC" -eq 0 ]; then
    # "does it FAIL?", not "is there a check?" — gqlc-z1qw, and `just doctor`
    # printing ok over a warning at gqlc-bn5r. A check that complains and exits
    # 0 is not a gate, and the bead asks for a gate in as many words.
    bad "$id_bad" "it exited 0 over two undeliverable owners: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^FAIL:.*owner'; then
    bad "$id_bad" "nonzero, but no FAIL line about ownership — the status came from another arm: $OUT"
elif ! printf '%s' "$OUT" | grep '^FAIL:.*owner' | grep -q 'km@test'; then
    bad "$id_bad" "the failing line does not name the address, so the operator cannot act on it: $OUT"
elif ! printf '%s' "$OUT" | grep '^FAIL:.*owner' | grep -q 'fixture@example.invalid'; then
    bad "$id_bad" "it named the first offender and stopped: $OUT"
else
    ok "$id_bad"
fi

# The SHAPE half of the same predicate, and it is a separate arm inside it: an
# address with no domain has no reserved name to match, so a fix that only
# walked the TLD list would clear this one.
id_shape="doctor FAILS on an owner that is not an address at all"
printf '[{"id":"gqlc-i7","owner":"km"}]' >"$KM_FAKE_OPEN"
run_doctor
if [ "$RC" -eq 0 ] || ! printf '%s' "$OUT" | grep -q '^FAIL:.*owner'; then
    bad "$id_shape" "rc=$RC out=$OUT"
else
    ok "$id_shape"
fi

# SOURCED, NOT COPIED. This is the predicate's third consumer, and gqlc-gy3q is
# the record of what a second copy costs: while that bead was open, two PRs
# carried independent copies that had ALREADY disagreed on four inputs. A copy
# diverges silently in the direction where bd accepts what commit-msg refuses.
# The row proves the dependency by MUTATING the one definition and watching km
# follow it — a grep for `implausible_email_reason` in km would pass against a
# call whose result was thrown away.
id_src="doctor reads the identity rule from .githooks/implausible-identity.sh rather than a second copy"
idsrc="$REPO/.githooks/implausible-identity.sh"
if [ ! -r "$idsrc" ]; then
    bad "$id_src" "$idsrc is missing, so the rule has nowhere single to live"
else
    shadow="$TMP/shadow-hooks"
    mkdir -p "$shadow/.githooks" "$shadow/kingdom"
    cp -r "$REPO/kingdom/bin" "$shadow/kingdom/bin"
    cp "$REPO/kingdom/kingdom.toml" "$shadow/kingdom/kingdom.toml"
    # ONE substitution in the shared file: a domain that is not reserved
    # anywhere else in the system. If km carries its own copy of the rule, it
    # cannot possibly know about this one.
    sed 's/^IMPLAUSIBLE_RESERVED_DOMAINS=(/IMPLAUSIBLE_RESERVED_DOMAINS=(mutated-for-the-suite /' \
        "$idsrc" >"$shadow/.githooks/implausible-identity.sh"
    printf '[{"id":"gqlc-i8","owner":"someone@mutated-for-the-suite"}]' >"$KM_FAKE_OPEN"
    OUT="$(PATH="$BIN:$PATH" "$shadow/kingdom/bin/km" doctor 2>&1)"
    RC=$?
    if [ "$RC" -eq 0 ] || ! printf '%s' "$OUT" | grep -q '^FAIL:.*owner'; then
        bad "$id_src" "a domain added to the shared list did not reach km, so km holds its own copy: rc=$RC out=$OUT"
    else
        ok "$id_src"
    fi
fi

# Fail-closed, the property this whole file is organised around: a question that
# could not be asked is not an answer of "nothing wrong". Found by mutation —
# delete the rc test on the query and every row above stays green while doctor
# certifies an empty answer from a database it could not open.
id_rc="a bead query that fails makes doctor refuse to certify the owners it never saw"
printf '[]' >"$KM_FAKE_OPEN"
printf '1' >"$KM_FAKE_OPEN.rc"
run_doctor
if [ "$RC" -eq 0 ]; then
    bad "$id_rc" "a failed owner query certified the board: $OUT"
elif ! printf '%s' "$OUT" | grep -q '^FAIL:.*owner'; then
    bad "$id_rc" "nonzero, but not because the owner query failed: $OUT"
else
    ok "$id_rc"
fi
rm -f "$KM_FAKE_OPEN.rc"
unset KM_FAKE_OPEN

# --- status: the king's inbox count measures delivery, not reading -----------
# gqlc-2abx. A letter leaves inbox/ in exactly one way: mail_read's final `mv`,
# which runs only when someone invokes `km mail read`. Every SEAT is a process
# that runs it. Անդրանիկ is a human who opens the file in an editor, so the
# letter stays in his inbox forever. MEASURED 2026-08-22: inbox 30, read 0, zero
# reads since the town was founded — a number that reads identically whether he
# has read every word or none of them.
#
# It is not a weak signal, it is no signal, and it was rendered in the same
# column as counts that ARE signals. It had already changed the mayor's conduct
# twice in one day, both toward silence: two digests were withheld with "he has
# 30 unread" written down as the reason, and the thing withheld was a bench
# decision only the king could make.
dispatch_case '[]' '[]'
make_inboxes
mkdir -p "$KM_STATE_DIR/mail/andranik/inbox"
printf 'x' >"$KM_STATE_DIR/mail/andranik/inbox/20260822T001840Z--sedrak--digest.md"
printf 'x' >"$KM_STATE_DIR/mail/andranik/inbox/20260822T101840Z--nvard--ask.md"
king_row="km status refuses to call the king's never-drained inbox 'unread'"
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
kline=$(printf '%s\n' "$OUT" | grep "king's inbox" || true)
if [ -z "$kline" ]; then
    bad "$king_row" "no line about the king's inbox at all: $OUT"
elif ! printf '%s' "$kline" | grep -q '2'; then
    bad "$king_row" "the count is wrong or absent, so the row below proves nothing: $kline"
elif printf '%s' "$kline" | grep -q '2 unread'; then
    bad "$king_row" "it still claims two letters are unread on evidence that cannot distinguish read from unread: $kline"
elif ! printf '%s' "$kline" | grep -q 'UNKNOWN'; then
    bad "$king_row" "it dropped the word 'unread' without saying what the number DOES mean: $kline"
else
    ok "$king_row"
fi

# The other half, and the one that stops the fix from being a blanket hedge: a
# box that HAS been drained carries real information, and the line must report
# it rather than printing UNKNOWN over every box forever. A remedy that always
# says "cannot tell" is the same absent signal wearing an apology.
king_drained="a drained box is reported as a count, not as UNKNOWN"
mkdir -p "$KM_STATE_DIR/mail/andranik/read"
printf 'x' >"$KM_STATE_DIR/mail/andranik/read/20260821T101840Z--sedrak--older.md"
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
kline=$(printf '%s\n' "$OUT" | grep "king's inbox" || true)
if printf '%s' "$kline" | grep -q 'UNKNOWN'; then
    bad "$king_drained" "a box with a read/ history still reads UNKNOWN: $kline"
elif ! printf '%s' "$kline" | grep -q '1'; then
    bad "$king_drained" "the read count is not reported: $kline"
else
    ok "$king_drained"
fi

# unread_count's `find` exits 1 on a directory that does not exist, and under
# `set -o pipefail` that killed km mid-table (gqlc-6wqw). read/ is missing far
# more often than inbox/ is, so the counter above would meet it constantly.
king_missing="status survives a mail box whose read/ directory has never been created"
rm -rf "$KM_STATE_DIR/mail/andranik/read"
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
RC=$?
if [ "$RC" -ne 0 ]; then
    bad "$king_missing" "status died on a missing read/ dir: rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q "king's inbox"; then
    bad "$king_missing" "the table stopped before the king's line: $OUT"
else
    ok "$king_missing"
fi

# gqlc-6wqw, the same defect on inbox/ and the one that was actually filed: a
# seat in kingdom.toml that no letter has ever reached has no inbox dir either,
# and status died mid-table on it. Note that this row deliberately does NOT call
# make_inboxes — that helper exists in this file precisely to hold the defect
# off, and every status row above stands on it, so none of them can see this.
inbox_row="status renders a full table when a configured seat has no inbox directory"
dispatch_case '[]' '[]'
rm -rf "$KM_STATE_DIR/mail"
OUT="$(PATH="$BIN:$PATH" "$KM" status 2>&1)"
RC=$?
if [ "$RC" -ne 0 ]; then
    bad "$inbox_row" "status exited $RC part way through: $OUT"
elif [ "$(printf '%s\n' "$OUT" | grep -cE '^(sedrak|raffi|tir) ')" -ne 3 ]; then
    # The LAST seats in roster order, not the first: an abort mid-table still
    # prints a header and some rows, so a row asserting rc alone, or asserting
    # the table started, passes against a table that stopped.
    bad "$inbox_row" "the seat table is short — it aborted part way: $OUT"
elif ! printf '%s' "$OUT" | grep -q "king's inbox"; then
    bad "$inbox_row" "it never reached the counters below the table: $OUT"
else
    ok "$inbox_row"
fi

# --- status is honest about the queries it could not answer (gqlc-bn5r) ------
# `km dispatch` was made fail-closed on both its queries (gqlc-z1qw) and this
# board was left fail-open on both of its own. The two are not symmetrical and
# must not be fixed the same way: dispatch is a timer job whose refusal belongs
# in the journal, so `die` is right there; status is the one glance a citizen
# and the king take at the town, and a status command that dies is a status
# command nobody runs. So the fix here is a MARKER on the parts it could not
# read, not an exit.
#
# What makes this worth a gate rather than a tolerated rough edge: this exact
# divergence is what hid gqlc-z1qw for the whole life of the kingdom. Routing
# was dead while the board printed a healthy idle town, and the board was the
# only witness anybody consulted. An empty answer and an unanswerable question
# rendered identically — "0 architect, 0 warrior, 0 judge" over a query that
# never returned, and an em dash in every seat's BEADS cell.

# The control, and it comes first: every loud row below would pass against a
# board that printed UNAVAILABLE unconditionally, and a board that cries
# failure at a working town is one people learn to skip.
dispatch_case '[{"id":"gqlc-bn1","labels":["class:warrior"]}]' \
    '[{"id":"gqlc-bn2","assignee":"vahagn","labels":["class:warrior"]}]'
make_inboxes
run_status
bn_clean="the board renders real counts quietly when both its queries answer"
if [ "$RC" -ne 0 ]; then
    bad "$bn_clean" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q '1 warrior'; then
    bad "$bn_clean" "the ready queue line does not carry the one warrior in the fixture: $OUT"
elif ! printf '%s\n' "$OUT" | grep -E '^vahagn ' | grep -q 'gqlc-bn2'; then
    bad "$bn_clean" "the seat's BEADS cell does not carry her in-progress bead: $(printf '%s\n' "$OUT" | grep -E '^vahagn ')"
elif printf '%s' "$OUT" | grep -q 'UNAVAILABLE'; then
    bad "$bn_clean" "a board with two working queries still cried UNAVAILABLE: $OUT"
else
    ok "$bn_clean"
fi

# THE DEFECT, first half. bd itself fails; the pipeline's exit status is the
# only signal, and `|| inprog=""` swallowed it.
status_beads_row() { printf '%s\n' "$OUT" | grep -E '^vahagn ' | head -1; }
bn_inprog="the BEADS column says UNAVAILABLE when the in-progress query fails"
dispatch_case '[]' '[{"id":"gqlc-bn3","assignee":"vahagn"}]'
make_inboxes
printf '1' >"$KM_FAKE_INPROG.rc"
run_status
if [ "$RC" -ne 0 ]; then
    bad "$bn_inprog" "status died on a failed query instead of marking it: rc=$RC out=$OUT"
elif ! printf '%s\n' "$OUT" | grep -qE '^sedrak '; then
    bad "$bn_inprog" "the seat table did not finish, so the marker cost the board: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'BEADS: UNAVAILABLE'; then
    bad "$bn_inprog" "no marker line — a failed query rendered as a town with no work in flight: $OUT"
elif printf '%s' "$(status_beads_row)" | grep -q '—'; then
    bad "$bn_inprog" "the cell still reads as an answer: $(status_beads_row)"
else
    ok "$bn_inprog"
fi

# THE DEFECT, second half, and it is a DIFFERENT failure: bd succeeds and jq
# aborts on what it handed back. `|| inprog=""` caught this one too and threw
# away the same distinction, so a fix that only checked bd's own rc would leave
# half the bead open. The fixture holds text that is not JSON.
bn_jq="the BEADS column says UNAVAILABLE when jq aborts on what bd returned"
dispatch_case '[]' '[]'
make_inboxes
printf 'bd: database is locked\n' >"$KM_FAKE_INPROG"
run_status
if [ "$RC" -ne 0 ]; then
    bad "$bn_jq" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'BEADS: UNAVAILABLE'; then
    bad "$bn_jq" "a jq abort rendered as an empty board: $OUT"
else
    ok "$bn_jq"
fi

# The ready queue is the other query, and it is the one the mayor sizes his
# standing chore off. Its fail-open shape prints four precise-looking zeroes.
bn_ready="the ready queue says UNAVAILABLE instead of counting four zeroes it never read"
dispatch_case '[]' '[]'
make_inboxes
printf '1' >"$KM_FAKE_READY.rc"
run_status
if [ "$RC" -ne 0 ]; then
    bad "$bn_ready" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q '^ready queue: UNAVAILABLE'; then
    bad "$bn_ready" "no marker: $(printf '%s\n' "$OUT" | grep '^ready queue')"
elif printf '%s\n' "$OUT" | grep '^ready queue' | grep -q '0 architect'; then
    bad "$bn_ready" "it still printed counts derived from a query that failed: $(printf '%s\n' "$OUT" | grep '^ready queue')"
else
    ok "$bn_ready"
fi

bn_ready_jq="the ready queue says UNAVAILABLE when bd answers with something jq cannot parse"
dispatch_case '[]' '[]'
make_inboxes
printf 'not json at all\n' >"$KM_FAKE_READY"
run_status
if [ "$RC" -ne 0 ]; then
    bad "$bn_ready_jq" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q '^ready queue: UNAVAILABLE'; then
    bad "$bn_ready_jq" "an unparseable answer counted as zero of everything: $(printf '%s\n' "$OUT" | grep '^ready queue')"
else
    ok "$bn_ready_jq"
fi

# Neither marker may cost the rest of the board. The whole argument for marking
# rather than dying is that the board keeps printing what it CAN.
bn_both="a board whose every bd query fails still prints the seat table and the king's inbox"
dispatch_case '[]' '[]'
make_inboxes
printf '1' >"$KM_FAKE_READY.rc"
printf '1' >"$KM_FAKE_INPROG.rc"
run_status
if [ "$RC" -ne 0 ]; then
    bad "$bn_both" "rc=$RC out=$OUT"
elif [ "$(printf '%s\n' "$OUT" | grep -cE '^(sedrak|raffi|tir) ')" -ne 3 ]; then
    bad "$bn_both" "the seat table is short — it aborted part way: $OUT"
elif ! printf '%s' "$OUT" | grep -q "king's inbox"; then
    bad "$bn_both" "it never reached the counters below the table: $OUT"
else
    ok "$bn_both"
fi

# --- the patrol bead (ADR 0004 §2, bead gqlc-ferp) ---------------------------
# The judges' patrol is the compensating control on ADR 0003's whole regime:
# nearly every PR now merges on green gates with its author as the only reader,
# and patrol is the thing that reads them afterwards. Measured at c129a0a5 it
# was two sentences of prose with no trigger — a seat runs only when woken,
# dispatch wakes a seat only for a bead, and nothing filed one — so the control
# could not begin. km guard-sweep files it now.
#
# The BOUND is one open at a time, permanently. That clause is what keeps ADR
# 0004 inside ADR 0003's constraint, so most of these rows are about the bound
# rather than about the filing.
patrol_board=""
patrol_case() { # $1 = the whole board bd answers --status all with
    dispatch_case '[]' '[]'
    export KM_FAKE_ALL="$KM_STATE_DIR/board-all.json"
    printf '%s' "$1" >"$KM_FAKE_ALL"
    patrol_board="$KM_STATE_DIR/bd-created"
}
patrol_created() { cat "$patrol_board" 2>/dev/null; }

bn="a sweep with no patrol bead open files one"
patrol_case '[{"id":"gqlc-other","status":"open","labels":["class:judge"]}]'
run_guard
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif [ ! -s "$patrol_board" ]; then
    bad "$bn" "km filed nothing (out=$OUT)"
elif [ "$(wc -l <"$patrol_board")" -ne 1 ]; then
    bad "$bn" "it filed more than one bead in a single sweep: $(patrol_created)"
elif ! patrol_created | grep -q -- '-p 2'; then
    bad "$bn" "the filed bead is not P2 — ADR 0004 §2 sets patrol's priority: $(patrol_created)"
elif ! patrol_created | grep -q 'class:judge'; then
    bad "$bn" "the filed bead carries no class:judge label, so dispatch would never route it to a judge: $(patrol_created)"
elif ! patrol_created | grep -q 'patrol'; then
    bad "$bn" "the filed bead carries no patrol label, so the one-open bound below cannot find it: $(patrol_created)"
elif patrol_created | grep -qE -- '(--assignee|(^| )-a )'; then
    bad "$bn" "km assigned the bead to a seat; patrol goes on the board unassigned so the dispatcher picks the judge: $(patrol_created)"
elif ! printf '%s' "$OUT" | grep -q 'filed a patrol bead'; then
    bad "$bn" "it filed one silently — the sweep's output has to say so or nobody can tell it happened: $OUT"
else
    ok "$bn, unassigned and P2 with both labels"
fi

# The bound. Three rows, one per status bd calls not-closed, because the whole
# defect this guards against is a status-name reading: bd's `--status open` is
# the LITERAL status and excludes in_progress and blocked, so a patrol bead a
# judge had already CLAIMED would read as absent and the next cadence would
# file a second one — the bound gone, silently, in exactly the state it exists
# for. The same reading made km doctor's identity arm miss a P0 (gqlc-18br,
# gqlc-c7b5). The in_progress row is the one that fails if km ever asks bd the
# convenient question.
for st in open in_progress blocked; do
    bn="a patrol bead already $st suppresses a second one"
    patrol_case "[{\"id\":\"gqlc-pat1\",\"status\":\"$st\",\"labels\":[\"class:judge\",\"patrol\"]}]"
    run_guard
    if [ "$RC" -ne 0 ]; then
        bad "$bn" "rc=$RC out=$OUT"
    elif [ -s "$patrol_board" ]; then
        bad "$bn" "km filed a second patrol bead beside the $st one: $(patrol_created)"
    elif ! printf '%s' "$OUT" | grep -q 'patrol already open (gqlc-pat1)'; then
        bad "$bn" "it filed nothing but does not name the bead that held it, so a silent sweep is indistinguishable from a broken one: $OUT"
    else
        ok "$bn, and the sweep names it"
    fi
done

# A status nobody here has heard of counts as open. For a BOUND that is the
# safe side: the failure of guessing wrong is one round of patrol deferred by a
# cadence, against an unbounded queue if a future bd status defaulted to closed.
bn="an unrecognised patrol status counts as open, not as closed"
patrol_case '[{"id":"gqlc-pat9","status":"deferred","labels":["patrol"]}]'
run_guard
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif [ -s "$patrol_board" ]; then
    bad "$bn" "km treated the unknown status 'deferred' as closed and filed another: $(patrol_created)"
else
    ok "$bn"
fi

bn="a closed patrol bead does not suppress the next round"
patrol_case '[{"id":"gqlc-pat0","status":"closed","closed_at":"2026-08-20T11:00:00Z","labels":["class:judge","patrol"]}]'
run_guard
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif [ ! -s "$patrol_board" ]; then
    bad "$bn" "patrol stopped forever after its first round closed (out=$OUT)"
elif ! patrol_created | grep -q '2026-08-20T11:00:00Z'; then
    bad "$bn" "the new bead does not carry the previous one's close time, so the judge re-reads merges already read: $(patrol_created)"
else
    ok "$bn, and the new one's window starts where the closed one ended"
fi

# The halt binds this too. Article VI.4 reserves lowering a halt to Սեդրակ or
# Անդրանիկ; a halted town that goes on filing beads for a seat nobody may wake
# is accumulating exactly the queue the halt was raised to stop.
bn="a halted sweep files no patrol bead"
patrol_case '[]'
run halt patrol must stop under a halt
run_guard
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif [ -s "$patrol_board" ]; then
    bad "$bn" "the halt was raised and km filed anyway: $(patrol_created)"
elif ! printf '%s' "$OUT" | grep -q 'halted'; then
    bad "$bn" "the sweep was silent but does not say the halt held it: $OUT"
else
    ok "$bn"
fi

# A bound that cannot be MEASURED is not raised. Filing blind on an unwell bd
# would add a patrol bead every cadence for as long as it stayed unwell, which
# is the queue this function exists to prevent — so the refusal has to be the
# fail-CLOSED direction here, and it has to be audible.
bn="an unreadable board files nothing and says why"
patrol_case '[]'
printf '1' >"$KM_FAKE_ALL.rc"
run_guard
if [ "$RC" -ne 0 ]; then
    bad "$bn" "a failed board query took the whole sweep down: rc=$RC out=$OUT"
elif [ -s "$patrol_board" ]; then
    bad "$bn" "km filed blind against a board it could not read: $(patrol_created)"
elif ! printf '%s' "$OUT" | grep -q 'the one-open bound could not be measured'; then
    bad "$bn" "it filed nothing silently, so an unwell bd looks exactly like a bounded board: $OUT"
elif ! wake_of raffi | grep -q 'round'; then
    bad "$bn" "it also stopped the round; patrol is an addition to the sweep, not a precondition for it"
else
    ok "$bn, and the round still happens"
fi

bn="a refused bd create does not cost Րաֆֆի his round"
patrol_case '[]'
export KM_FAKE_CREATE_RC=1
run_guard
unset KM_FAKE_CREATE_RC
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif ! wake_of raffi | grep -q 'round'; then
    bad "$bn" "bd refusing the filing stopped the sweep waking him (woken: $(woken_seats)) out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'could not be filed'; then
    bad "$bn" "the refusal is swallowed — patrol would silently never start: $OUT"
else
    ok "$bn, and the refusal is audible"
fi
unset KM_FAKE_ALL

# --- the DIRTY census (ADR 0006 §1, bead gqlc-wz47) --------------------------
# Nothing told an author their open PR had gone DIRTY. Every case on 2026-08-22
# was found by a person happening to look, and Արամազդ's estimate was that
# three of seven affected authors did not know.
#
# The fixture is built to the SHAPE that manufactures these conflicts rather
# than to an arbitrary one: a branch edits a file, master then edits the same
# file under it, and the branch is DIRTY through nothing its author did. Both
# heads live in a real bare repo on disk, so `git merge-tree` is asked a real
# question and no row touches the network.
DIRTYO="$TMP/dirty-origin.git"
DIRTYW="$TMP/dirty-work"
gitf init -q --bare -b master "$DIRTYO"
gitf init -q -b master "$DIRTYW"
gitf -C "$DIRTYW" config user.email fixture@example.invalid
gitf -C "$DIRTYW" config user.name fixture
gitf -C "$DIRTYW" config commit.gpgsign false
gitf -C "$DIRTYW" config core.hooksPath /dev/null
mkdir -p "$DIRTYW/kingdom/bin" "$DIRTYW/.githooks" "$DIRTYW/.claude"
printf 'a registry\n' >"$DIRTYW/justfile"
printf 'fixture\n' >"$DIRTYW/kingdom/bin/km"
printf 'fixture\n' >"$DIRTYW/.githooks/keep"
printf 'fixture\n' >"$DIRTYW/.claude/keep"
gitf -C "$DIRTYW" add -A
gitf -C "$DIRTYW" commit -qm 'fixture: a shared registry two branches will append to'
gitf -C "$DIRTYW" remote add origin "$DIRTYO"
gitf -C "$DIRTYW" push -q origin master
gitf -C "$DIRTYW" checkout -q -b feat/thing-gqlc-dd11
printf 'a registry\nthe branch line\n' >"$DIRTYW/justfile"
gitf -C "$DIRTYW" commit -qam 'the branch appends to the registry'
gitf -C "$DIRTYW" push -q origin feat/thing-gqlc-dd11
gitf -C "$DIRTYW" checkout -q master
printf 'a registry\nthe master line\n' >"$DIRTYW/justfile"
gitf -C "$DIRTYW" commit -qam 'somebody else appends to the same registry, and merges first'
gitf -C "$DIRTYW" push -q origin master
# The POSITIVE CONTROL, and it is cut from the NEW master on purpose: a control
# is sound only at the instant it runs, and #1195 was a clean control that went
# DIRTY through nothing but another PR merging.
gitf -C "$DIRTYW" checkout -q -b feat/other-gqlc-cc22
printf 'untouched by anyone else\n' >"$DIRTYW/unrelated.md"
gitf -C "$DIRTYW" add -A
gitf -C "$DIRTYW" commit -qm 'a branch that touches no registry'
gitf -C "$DIRTYW" push -q origin feat/other-gqlc-cc22
gitf -C "$DIRTYW" checkout -q master
DIRTY_OID=$(gitf -C "$DIRTYW" rev-parse feat/thing-gqlc-dd11)
CLEAN_OID=$(gitf -C "$DIRTYW" rev-parse feat/other-gqlc-cc22)

DIRTYGH="$TMP/dirty-gh.json"
run_guard_dirty() { # $1 = the PR list gh answers with
    printf '%s' "$1" >"$DIRTYGH"
    OUT="$(cd "$DIRTYW" && PATH="$BIN:$PATH" KM_FAKE_GH="$DIRTYGH" "$KM" guard-sweep 2>&1)"
    RC=$?
}
dirty_mail_of() { find "$KM_STATE_DIR/mail/$1/inbox" -type f 2>/dev/null; }
dirty_pr_json() { # <number> <oid> <branch>
    jq -cn --arg n "$1" --arg o "$2" --arg b "$3" \
        '[{number: ($n | tonumber), headRefOid: $o, headRefName: $b}]'
}

# The POSITIVE CONTROL comes first, and it is not a courtesy row: a census that
# always cries conflict passes every DIRTY assertion below. Without this one
# they witness nothing.
bn="a clean PR is not reported as DIRTY"
dispatch_case '[]' '[]' '[]' '' '[{"id":"gqlc-cc22","assignee":"tir","dependencies":[]}]'
run_guard_dirty "$(dirty_pr_json 4242 "$CLEAN_OID" feat/other-gqlc-cc22)"
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif [ -n "$(dirty_mail_of tir)" ]; then
    bad "$bn" "the census mailed the author about a branch that merges cleanly: $(cat "$(dirty_mail_of tir)")"
elif printf '%s' "$OUT" | grep -q 'DIRTY'; then
    bad "$bn" "it named a clean PR as DIRTY: $OUT"
else
    ok "$bn, so the rows below discriminate rather than always crying conflict"
fi

bn="a DIRTY PR is mailed to the seat its branch names"
dispatch_case '[]' '[]' '[]' '' '[{"id":"gqlc-dd11","assignee":"tir","dependencies":[]}]'
run_guard_dirty "$(dirty_pr_json 4243 "$DIRTY_OID" feat/thing-gqlc-dd11)"
letter="$(dirty_mail_of tir)"
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif [ -z "$letter" ]; then
    bad "$bn" "nothing reached Տիր's inbox, which is the whole defect: $OUT"
elif ! grep -q '4243' "$letter"; then
    bad "$bn" "the letter does not name the PR: $(cat "$letter")"
elif ! grep -qxF '  justfile' "$letter"; then
    # An EXACT line, not a substring. Without --name-only, `git merge-tree
    # --write-tree` lists conflicts as `<mode> <oid> <stage>\tf` — three lines
    # per path, each of which CONTAINS the path — so a substring match passes
    # against the exact output ADR 0006 says not to read. Measured 2026-08-23:
    # this row survived dropping --name-only until it was pinned this way.
    bad "$bn" "the conflicted path is not on a line of its own; the letter is carrying merge-tree's raw stage lines rather than the path: $(cat "$letter")"
elif grep -qE '^  [0-7]{6} ' "$letter"; then
    bad "$bn" "the letter carries merge-tree's mode/oid/stage rows — --name-only was not used: $(cat "$letter")"
elif grep -q 'unrelated.md' "$letter"; then
    bad "$bn" "it named a path that merged cleanly — merge-tree's Auto-merging lines were read as conflicts: $(cat "$letter")"
elif [ -n "$(find "$KM_STATE_DIR/seats/tir" -name wake 2>/dev/null)" ]; then
    bad "$bn" "the census WOKE him; a DIRTY PR is routine and a wake per conflict spends a slot on it"
else
    ok "$bn, naming the conflicted path and only it, by mail rather than by a wake"
fi

# The suppression key. The cadence is minutes, so without this the author gets
# the same letter every tick and stops reading any of them.
bn="the same head is not reported twice"
before=$(dirty_mail_of tir | wc -l)
run_guard_dirty "$(dirty_pr_json 4243 "$DIRTY_OID" feat/thing-gqlc-dd11)"
after=$(dirty_mail_of tir | wc -l)
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif [ "$after" != "$before" ]; then
    bad "$bn" "a second sweep at the same head mailed again ($before then $after letters)"
else
    ok "$bn"
fi

# ...but a NEW head is. The key is the head and not the PR, because an author
# who pushes again into a conflict that is still there is the case that must
# not be swallowed by the suppression protecting them from the tick.
bn="a head that has moved earns a fresh letter"
gitf -C "$DIRTYW" checkout -q feat/thing-gqlc-dd11
printf 'a registry\nthe branch line\nand more of it\n' >"$DIRTYW/justfile"
gitf -C "$DIRTYW" commit -qam 'the author pushes again, still conflicting'
gitf -C "$DIRTYW" push -q origin feat/thing-gqlc-dd11
DIRTY_OID2=$(gitf -C "$DIRTYW" rev-parse feat/thing-gqlc-dd11)
gitf -C "$DIRTYW" checkout -q master
before=$(dirty_mail_of tir | wc -l)
run_guard_dirty "$(dirty_pr_json 4243 "$DIRTY_OID2" feat/thing-gqlc-dd11)"
after=$(dirty_mail_of tir | wc -l)
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif [ "$after" = "$before" ]; then
    bad "$bn" "the suppression key outlived the head it was about, so a still-conflicting push went untold: $OUT"
else
    ok "$bn, so the key is the head and not the PR"
fi

# An oid git cannot resolve — most often a branch someone deleted — is UNKNOWN,
# and unknown is neither clean nor dirty. Reading it as either is a silent
# answer to a question that was not answered.
bn="a head git cannot resolve is named, and reported to nobody"
before=$(dirty_mail_of tir | wc -l)
run_guard_dirty "$(dirty_pr_json 4243 0000000000000000000000000000000000000000 feat/thing-gqlc-dd11)"
after=$(dirty_mail_of tir | wc -l)
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif ! printf '%s' "$OUT" | grep -q 'merge-test'; then
    bad "$bn" "an oid git cannot resolve was passed over silently: $OUT"
elif [ "$after" != "$before" ]; then
    bad "$bn" "it mailed about a head it could not measure: $OUT"
else
    ok "$bn"
fi

bn="an unattributable DIRTY PR goes to Սեդրակ rather than nowhere"
dispatch_case '[]' '[]' '[]' '' '[]'
run_guard_dirty "$(dirty_pr_json 4244 "$DIRTY_OID" feat/no-bead-in-this-name)"
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif [ -z "$(dirty_mail_of sedrak)" ]; then
    bad "$bn" "a PR whose branch names no bead was dropped, which is exactly the case a human has to look at: $OUT"
else
    ok "$bn"
fi

bn="a census gh cannot answer costs neither the sweep nor a false letter"
dispatch_case '[]' '[]'
KM_FAKE_GH_RC=1 run_guard_dirty '[]'
if [ "$RC" -ne 0 ]; then
    bad "$bn" "a gh outage took the whole sweep down: rc=$RC out=$OUT"
elif [ -n "$(dirty_mail_of tir)" ]; then
    bad "$bn" "it mailed on an answer it never got: $(dirty_mail_of tir)"
elif ! printf '%s' "$OUT" | grep -q 'census skipped'; then
    bad "$bn" "the outage is silent, so a broken census looks like a clean board: $OUT"
elif ! wake_of raffi | grep -q 'round'; then
    bad "$bn" "it also stopped the round; the census is an addition to the sweep, not a precondition for it"
else
    ok "$bn, and the round still happens"
fi

bn="a halted sweep runs no DIRTY census"
dispatch_case '[]' '[]' '[]' '' '[{"id":"gqlc-dd11","assignee":"tir","dependencies":[]}]'
run halt no census under a halt
run_guard_dirty "$(dirty_pr_json 4243 "$DIRTY_OID" feat/thing-gqlc-dd11)"
if [ "$RC" -ne 0 ]; then
    bad "$bn" "rc=$RC out=$OUT"
elif [ -n "$(dirty_mail_of tir)" ]; then
    bad "$bn" "the halt was raised and the census mailed anyway: $(dirty_mail_of tir)"
else
    ok "$bn"
fi

# This suite builds git repositories, so it must be able to prove it built them
# somewhere else. On PR #1128 it could not: a leaked GIT_DIR sent the fixture's
# `git init` and `git commit` into the repo under test, grafting six fixture
# commits onto the branch and rewriting what the PR contained — silently, three
# times, and the third one pushed. The unset at the top of this file is the fix.
# This is the alarm, kept because that failure is invisible in a green run and
# its blast radius is shipped history.
suite_end_head="$(gitf -C "$REPO" rev-parse HEAD 2>/dev/null || echo none)"
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
