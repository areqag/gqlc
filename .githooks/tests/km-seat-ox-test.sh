#!/usr/bin/env bash
# Tests for kingdom/bin/km-seat-ox — the opencode seat runner's session
# continuity across wake cycles (bd gqlc-pykf).
#
# The real `opencode session list` spans every project on the host, so each
# scenario serves a TOWN-WIDE pool of JSON rows from a stub: another seat's
# fresher session sits beside this worktree's, and the runner under test must
# resolve ids only within its own directory, by recency. Each scenario copies
# the runner into a sandbox, drives one wake→turn→park cycle end to end
# against a fake km, and asserts on what landed in the seat state. Abnormal
# exits are exercised by having the stub's `run` fail the way the designed
# 3600s timeout does.
#
# Run via: just test-hooks
set -u

# Hooks export GIT_DIR into everything they shell out to; nothing here wants it.
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
RUNNER_SRC="$REPO/kingdom/bin/km-seat-ox"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { pass=$((pass+1)); printf 'ok   - %s\n' "$1"; }
bad() { fail=$((fail+1)); printf 'FAIL - %s\n       %s\n' "$1" "$2"; }
check_eq() { # desc actual expected
    if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "got '$2', want '$3'"; fi
}

# One session record as `opencode session list --format json` emits it.
sess_json() { # id directory updated title -> one json object
    jq -n --arg id "$1" --arg dir "$2" --argjson up "$3" --arg t "$4" \
        '{id: $id, directory: $dir, projectId: "p", title: $t, created: ($up - 1000), updated: $up}'
}

json_pool() { # file [row-json...]  (empty args = an empty pool)
    local f=$1
    shift
    if [ "$#" -eq 0 ]; then printf '[]\n' >"$f"; else printf '%s\n' "$@" | jq -s . >"$f"; fi
}

build_sandbox() { # name -> echoes the sandbox root
    local s="$TMP/$1"
    mkdir -p "$s/kingdom/bin" "$s/wt" "$s/tmp" "$s/state/seats/tester/handoff/archive"
    cp "$RUNNER_SRC" "$s/kingdom/bin/km-seat-ox"
    chmod +x "$s/kingdom/bin/km-seat-ox"

    cat >"$s/kingdom/bin/km" <<EOF
#!/usr/bin/env bash
case "\$1" in
    seat-info) echo "tester warrior Tester" ;;
    state-dir) echo "$s/state" ;;
    seat-worktree) echo "$s/wt" ;;
    cfg) echo "stub/model" ;;
    seat-refresh) exit 0 ;;
    *) echo "fake km: unexpected \$*" >&2; exit 64 ;;
esac
EOF
    chmod +x "$s/kingdom/bin/km"

    cat >"$s/kingdom/bin/opencode" <<'EOF'
#!/usr/bin/env bash
# Serves OXSTUB_DIR/{pre,post}.json: listings before this turn's `run` read
# pre (the resume-membership check), anything after reads post (the record
# step). `run` marks the boundary by touching OXSTUB_DIR/ran, then exits per
# OXSTUB_RUN_RC. A non-json listing is refused: the runner must resolve ids
# from the JSON view alone.
if [ "$1" = run ]; then
    printf '%s\n' "$*" >>"$OXSTUB_DIR/run-args"
    : >"$OXSTUB_DIR/ran"
    exit "${OXSTUB_RUN_RC:-0}"
fi
if [ "${1:-}" = session ] && [ "${2:-}" = list ]; then
    case "$*" in
        *--format\ json*) ;;
        *) echo "opencode stub: non-json listing requested ($*)" >&2; exit 64 ;;
    esac
    if [ -f "$OXSTUB_DIR/ran" ]; then cat "$OXSTUB_DIR/post.json"; else cat "$OXSTUB_DIR/pre.json"; fi
    exit 0
fi
echo "opencode stub: unexpected $*" >&2
exit 64
EOF
    chmod +x "$s/kingdom/bin/opencode"

    mkdir -p "$s/kingdom/seats/tester"
    printf 'SOUL-MARKER tester\n' >"$s/kingdom/seats/tester/soul.md"
    printf '%s\n' "$s"
}

launch_runner() { # sandbox-root [stub-run-exit-code]
    # The stub's run exit code rides the launcher's own environment: an
    # unexported assignment in the calling scenario never reaches it.
    local s=$1 rc=${2:-0}
    printf 'test wake\n' >"$s/state/seats/tester/wake"
    (
        cd "$s/wt" || exit 1
        export PATH="$s/kingdom/bin:$PATH"
        export TMPDIR="$s/tmp"
        export OXSTUB_DIR="$s"
        export OXSTUB_RUN_RC="$rc"
        unset KINGDOM_SEAT BEADS_ACTOR KM_STATE_DIR KM_CONFIG
        exec bash "$s/kingdom/bin/km-seat-ox" tester
    ) >"$s/launch.log" 2>&1 &
    printf '%s\n' "$!" >"$s/rpid"
}

stop_runner() {
    # The runner parks in `sleep 5`; bash defers its TERM trap until that
    # returns, so a bare kill costs every scenario a full wake interval.
    local rpid
    rpid=$(cat "$1/rpid" 2>/dev/null) || return 0
    kill "$rpid" 2>/dev/null
    for _ in $(seq 20); do kill -0 "$rpid" 2>/dev/null || break; sleep 0.05; done
    kill -9 "$rpid" 2>/dev/null
    wait "$rpid" 2>/dev/null
}

await_turn() { # sandbox-root
    local s=$1
    local st="$s/state/seats/tester/status"
    # One phase: the runner writes ox-session before it writes asleep, so
    # seeing asleep witnesses both the turn and the record step. The five
    # runners start together, so on a loaded runner the slowest park decides
    # the wall time: this ceiling stays under the git-env-sandbox harness's
    # own 120s per-suite budget, and a runner that cannot make it turns the
    # scenario red honestly instead of dying invisibly mid-run.
    for _ in $(seq 400); do grep -qx asleep "$st" 2>/dev/null && break; sleep 0.25; done
    if ! grep -qx asleep "$st" 2>/dev/null; then
        bad "$CURRENT_SCN (premise)" \
            "the runner never parked again (status: $(cat "$st" 2>/dev/null)); launch.log tail: $(tail -2 "$s/launch.log" 2>/dev/null | tr '\n' ' ')"
        return 1
    fi
    return 0
}

temp_leaks() { find "$1/tmp" -name 'ox-ids-*' 2>/dev/null | wc -l; }

CURRENT_SCN=""
sbx() { printf '%s/%s' "$TMP" "$1"; }

# The five sandboxes are independent, so their runners launch together and
# total wall time is one cycle, not five — on a slow runner the sequential
# form summed past the git-env-sandbox harness's own timeout (bd gqlc-pykf).
start_a() { # a foreign session in the host-wide pool must not become ours
    local s
    s=$(build_sandbox a)
    json_pool "$s/pre.json"
    json_pool "$s/post.json" \
        "$(sess_json sesAAAA111111 "$s/wt" 2000 'tester wake')" \
        "$(sess_json sesZZZZ999999 /other/seat-wt 9000 'other seat wake')"
    launch_runner "$s"
}

check_a() {
    CURRENT_SCN="a foreign session in the town pool does not become this seat's continuity id"
    local s
    s=$(sbx a)
    await_turn "$s" || { stop_runner "$s"; return 0; }
    check_eq "$CURRENT_SCN" "$(cat "$s/state/seats/tester/ox-session" 2>/dev/null)" sesAAAA111111
    if grep -q SOUL-MARKER "$s/run-args" 2>/dev/null; then
        ok "an empty scope starts a fresh episode: the soul opens the session"
    else
        bad "an empty scope starts a fresh episode: the soul opens the session" "run argv carries no soul text"
    fi
    stop_runner "$s"
}

start_b() { # within our own project, recency decides, not sort order
    local s
    s=$(build_sandbox b)
    json_pool "$s/pre.json" "$(sess_json sesOLD000001 "$s/wt" 1000 'earlier episode')"
    json_pool "$s/post.json" \
        "$(sess_json sesOLD000001 "$s/wt" 1000 'earlier episode')" \
        "$(sess_json sesAAA222222 "$s/wt" 3000 'newest')" \
        "$(sess_json sesZZZ333333 "$s/wt" 2000 'older than newest')"
    launch_runner "$s"
}

check_b() {
    CURRENT_SCN="two concurrent own-project sessions resolve to the newest, not the lexicographically last"
    local s
    s=$(sbx b)
    await_turn "$s" || { stop_runner "$s"; return 0; }
    check_eq "$CURRENT_SCN" "$(cat "$s/state/seats/tester/ox-session" 2>/dev/null)" sesAAA222222
    stop_runner "$s"
}

start_c() { # the designed timeout kill must not orphan recovery or leak temps
    local s
    s=$(build_sandbox c)
    json_pool "$s/pre.json"
    json_pool "$s/post.json" "$(sess_json sesKILL777777 "$s/wt" 5000 'killed mid-turn')"
    launch_runner "$s" 124
}

check_c() {
    CURRENT_SCN="opencode run killed abnormally still records its session and cleans up"
    local s
    s=$(sbx c)
    await_turn "$s" || { stop_runner "$s"; return 0; }
    check_eq "$CURRENT_SCN" "$(cat "$s/state/seats/tester/ox-session" 2>/dev/null)" sesKILL777777
    check_eq "no resolution temp file is left behind" "$(temp_leaks "$s")" 0
    stop_runner "$s"
}

start_d() { # a resumed turn whose pool yields nothing new must survive the empty diff
    local s
    s=$(build_sandbox d)
    printf 'sesSAVED55555\n' >"$s/state/seats/tester/ox-session"
    json_pool "$s/pre.json" "$(sess_json sesSAVED55555 "$s/wt" 1000 'carried episode')"
    json_pool "$s/post.json" "$(sess_json sesSAVED55555 "$s/wt" 6000 'carried episode')"
    launch_runner "$s"
}

check_d() {
    CURRENT_SCN="a resumed turn with no newer sessions keeps its id"
    local s
    s=$(sbx d)
    await_turn "$s" || { stop_runner "$s"; return 0; }
    check_eq "$CURRENT_SCN" "$(cat "$s/state/seats/tester/ox-session" 2>/dev/null)" sesSAVED55555
    if grep -q sesSAVED55555 "$s/run-args" 2>/dev/null; then
        ok "a recorded id is resumed, not re-opened"
    else
        bad "a recorded id is resumed, not re-opened" "run argv never names the saved session"
    fi
    if grep -q SOUL-MARKER "$s/run-args" 2>/dev/null; then
        bad "a resumed episode does not re-prepend the soul" "run argv carries the soul again"
    else
        ok "a resumed episode does not re-prepend the soul"
    fi
    stop_runner "$s"
}

start_e() { # tonight's live contamination: a foreign id in the record must not be resumed
    local s
    s=$(build_sandbox e)
    printf 'sesFOREIGN00000\n' >"$s/state/seats/tester/ox-session"
    json_pool "$s/pre.json" \
        "$(sess_json sesFOREIGN00000 /other/seat-wt 9000 'another citizens conversation')"
    json_pool "$s/post.json" \
        "$(sess_json sesFOREIGN00000 /other/seat-wt 9000 'another citizens conversation')" \
        "$(sess_json sesOWNNEW11111 "$s/wt" 10000 'tester wake')"
    launch_runner "$s"
}

check_e() {
    CURRENT_SCN="a poisoned continuity record opens a fresh episode, not the stranger's thread"
    local s
    s=$(sbx e)
    await_turn "$s" || { stop_runner "$s"; return 0; }
    if grep -q sesFOREIGN00000 "$s/run-args" 2>/dev/null; then
        bad "$CURRENT_SCN" "run argv names the foreign session"
    else
        ok "$CURRENT_SCN"
    fi
    if grep -q SOUL-MARKER "$s/run-args" 2>/dev/null; then
        ok "a rejected record re-prepends the soul"
    else
        bad "a rejected record re-prepends the soul" "run argv carries no soul text"
    fi
    check_eq "the turn ran here, so its own id is what gets recorded" \
        "$(cat "$s/state/seats/tester/ox-session" 2>/dev/null)" sesOWNNEW11111
    stop_runner "$s"
}

start_a
start_b
start_c
start_d
start_e

check_a
check_b
check_c
check_d
check_e

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
