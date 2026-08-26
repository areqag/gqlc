#!/usr/bin/env bash
# Tests for kingdom/bin/km-seat-ox's session continuity (bd gqlc-hv6g).
#
# `opencode session list` answers for the whole TOWN: one store, every seat,
# and cwd scopes nothing — tir measured five seats' ox-session files against
# it and four pointed at another citizen's conversation. The runner's
# continuity diff ran over that global pool with `sort -u | tail -1`, so any
# session ANY seat created during my turn could win my next wake's `-s`,
# chosen lexicographically rather than by recency.
#
# Each row drives ONE REAL runner turn in a sandbox town against a fake
# opencode whose session store mixes this worktree's sessions with
# foreigners'. Three properties are pinned:
#
#   A. the pool is scoped to the seat worktree — a foreign id is invisible
#      to both the resume decision and the recording diff;
#   B. among our own, recency wins (created desc), not lexicographic order;
#   C. the turn's own fresh session is what gets recorded, because `run`
#      creates it (the stub appends one whenever it is NOT asked to resume).
#
# Run via: just test-hooks
set -u

# This suite runs under pre-push like the rest; the shared scrub clears git's
# exported environment (GIT_DIR and friends) rather than trusting each row.
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
RUNNER="$REPO/kingdom/bin/km-seat-ox"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export KM_STATE_DIR="$TMP/state"
unset KINGDOM_SEAT KM_CONFIG

pass=0
fail=0
ok() { pass=$((pass + 1)); printf 'ok   - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s\n' "$1"; [ $# -ge 2 ] && printf '     %s\n' "$2"; }

BIN="$TMP/bin"
mkdir -p "$BIN"

SOUL_MARK='Վահագն — Ռազմիկ'

# One sandbox per row: main repo whose sibling `<main>-seat-vahagn` is the
# worktree km-seat-worktree derives, a state dir for the seat's home, and a
# fake opencode whose store answers both the legacy table grep and --format
# json with the same body.
new_town() { # <rowtag>
    local tag=$1
    T="$TMP/$tag"
    # Full per-row isolation: a state dir apiece, so rows cannot see each
    # other's ox-session, wake file, or runner.log — and an orphaned runner
    # from a previous row cannot consume this row's wake (the same
    # shared-continuity disease this bead fixes, met in miniature).
    export KM_STATE_DIR="$T/state"
    MAIN="$T/main"
    mkdir -p "$MAIN" "$BIN"
    git init -q -b main "$MAIN"
    git -C "$MAIN" config user.email fixture@example.invalid
    git -C "$MAIN" config user.name fixture
    git -C "$MAIN" config commit.gpgsign false
    git -C "$MAIN" config core.hooksPath /dev/null
    WT="$T/main-seat-vahagn"
    mkdir -p "$WT" "$KM_STATE_DIR/seats/vahagn"
    rm -f "$KM_STATE_DIR/seats/vahagn/ox-session"
    SESSION_JSON="$T/sessions.json"
    SESSION_LOG="$T/run-argv.log"
    FIXTURE_JSON="$SESSION_JSON"
    export FIXTURE_JSON SESSION_LOG ROW_TAG=$tag WT
}

# The store: two of ours (the older sorts LEXICOGRAPHICALLY LAST), a foreign
# session from another seat's directory sorting after everything of ours but
# one, and room for the turn's own creation at the top.
load_store() {
    cat >"$SESSION_JSON" <<JSON
[
  {"id":"ses_zzold","title":"Vahagn older","updated":1100,"created":1000,"projectId":"p","directory":"$WT"},
  {"id":"ses_anew","title":"Vahagn newer","updated":2200,"created":2000,"projectId":"p","directory":"$WT"},
  {"id":"ses_zzforeign","title":"tir wake","updated":8900,"created":8800,"projectId":"q","directory":"$T/elsewhere-seat-tir"}
]
JSON
}

cat >"$BIN/opencode" <<'STUB'
#!/usr/bin/env bash
# session list [--format json] -> the canned store; run -> log argv, and
# unless resumed, CREATE two sessions the way a live turn does: this seat's
# own (highest created) and a foreign seat's mid-turn wake (created higher
# than everything pre-turn, id sorting last). The recording diff therefore
# holds TWO new ids, and only scope+recency picks the right one.
if [ "${1:-}" = session ]; then
    exec cat "${FIXTURE_JSON:?}"
fi
if [ "${1:-}" = run ]; then
    {
        printf '%s\n' "--- ARGV ---"
        printf '%s\n' "$@"
    } >>"$SESSION_LOG"
    resume=no
    prev=""
    for a in "$@"; do
        [ "$prev" = -s ] && resume=yes
        prev=$a
    done
    if [ "$resume" = no ]; then
        tmp=$(mktemp)
        jq --arg id "ses_afresh$ROW_TAG" --arg dir "$WT" \
           --arg fid "ses_zzmidthief$ROW_TAG" --arg fdir "$WT-thief" \
           '. + [
             {"id":$fid,"title":"foreign mid-turn","updated":9995,"created":9990,
              "projectId":"q","directory":$fdir},
             {"id":$id,"title":"fresh","updated":9900,"created":9900,
              "projectId":"p","directory":$dir}
           ]' "$FIXTURE_JSON" >"$tmp"
        # Same file the next `session list` reads, atomically enough for tests.
        mv "$tmp" "$FIXTURE_JSON"
    fi
    exit 0
fi
exit 0
STUB
chmod +x "$BIN/opencode"

run_turn() {
    : >"$SESSION_LOG"
    export MAIN BIN RUNNER
    # Single quotes deliberate: MAIN/BIN/RUNNER reach the child as exported
    # environment and are expanded by the new session itself.
    # shellcheck disable=SC2016
    setsid bash -c '
        cd "$MAIN" &&
            PATH="$BIN:$PATH" timeout 300 "$RUNNER" vahagn
    ' >"$T/out.log" 2>&1 &
    RPID=$!
    PGID=$(ps -o pgid= -p "$RPID" | tr -d ' ')
    for _ in $(seq 60); do
        [ -s "$SESSION_LOG" ] && break
        sleep 0.2
    done
    # Completion signal is the runner's OWN status returning to asleep,
    # which happens only after the recorder has run — never the presence of
    # ox-session, which a stale pre-written value makes instant and which
    # since #1567 must anyway outlive one full 60s stall-watcher cycle.
    for _ in $(seq 150); do
        [ "$(cat "$KM_STATE_DIR/seats/vahagn/status" 2>/dev/null)" = asleep ]             && [ -s "$SESSION_LOG" ] && break
        sleep 1
    done
    # Kill the whole group: the runner's turn subshell must not survive to
    # consume a later row's wake file.
    kill -TERM -- -"$PGID" 2>/dev/null
    sleep 0.5
    kill -KILL -- -"$PGID" 2>/dev/null
    wait "$RPID" 2>/dev/null
}

recorded() { cat "$KM_STATE_DIR/seats/vahagn/ox-session" 2>/dev/null; }

# --- Row 1: a fresh episode records THIS turn's own session -----------------
# The recording diff holds TWO new ids after the turn: our fresh one and a
# foreign mid-turn creation that is YOUNGER than ours (a wake that landed a
# beat later) and sorts last. Pre-fix code answered ses_zzmidthiefr1 twice
# over — global pool, then sort -u | tail -1 — and dropping either half of
# the fix (scope, recency) reddens this row again.
new_town r1
load_store
printf 'bead:gqlc-hv6g row one\n' >"$KM_STATE_DIR/seats/vahagn/wake"
run_turn
if [ "$(recorded)" = "ses_afreshr1" ]; then
    ok "a fresh episode records the turn's own new session, not a foreign or lexicographic pick"
else
    bad "a fresh episode records the turn's own new session" "got '$(recorded)', want ses_afreshr1"
fi

# --- Row 2: a recorded OWN id still listed is resumed -----------------------
new_town r2
load_store
printf 'ses_anew\n' >"$KM_STATE_DIR/seats/vahagn/ox-session"
printf 'bead:gqlc-hv6g row two\n' >"$KM_STATE_DIR/seats/vahagn/wake"
run_turn
if grep -qxF -- '-s' "$SESSION_LOG" && grep -qxF 'ses_anew' "$SESSION_LOG"; then
    ok "an own recorded id still listed is resumed with -s"
else
    bad "an own recorded id still listed is resumed with -s" "argv log: $(cat "$SESSION_LOG")"
fi

# --- Row 3: a recorded FOREIGN id starts a fresh episode --------------------
# ses_zzforeign IS in the global pool, which is exactly the trap: only the
# directory scope keeps it out, so the pre-fix runner resumed tir's morning.
new_town r3
load_store
printf 'ses_zzforeign\n' >"$KM_STATE_DIR/seats/vahagn/ox-session"
printf 'bead:gqlc-hv6g row three\n' >"$KM_STATE_DIR/seats/vahagn/wake"
run_turn
if ! grep -qxF -- '-s' "$SESSION_LOG" && grep -qF -- "$SOUL_MARK" "$SESSION_LOG" \
    && [ "$(recorded)" = "ses_afreshr3" ]; then
    ok "a recorded foreign id is not resumed; the fresh episode's own id is recorded instead"
else
    bad "a recorded foreign id is not resumed" \
        "resumed=$(grep -cxF -- '-s' "$SESSION_LOG") recorded='$(recorded)'"
fi

# --- Row 4: a vanished own id starts fresh and records the new episode ------
new_town r4
load_store
printf 'ses_gone\n' >"$KM_STATE_DIR/seats/vahagn/ox-session"
printf 'bead:gqlc-hv6g row four\n' >"$KM_STATE_DIR/seats/vahagn/wake"
run_turn
if ! grep -qxF -- '-s' "$SESSION_LOG" && [ "$(recorded)" = "ses_afreshr4" ]; then
    ok "a vanished own id is not resumed; the fresh episode's id replaces it"
else
    bad "a vanished own id is not resumed" \
        "resumed=$(grep -cxF -- '-s' "$SESSION_LOG") recorded='$(recorded)'"
fi

printf '%s passed, %s failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
