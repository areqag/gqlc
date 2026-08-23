#!/usr/bin/env bash
# Tests for .githooks/pre-push's handling of a CONCURRENT golangci-lint lock —
# bd gqlc-hg61 / GH #1161.
#
# THE DEFECT. golangci-lint holds a machine-wide lock and refuses to start while
# another instance owns it, printing "parallel golangci-lint is running" and
# exiting 3. Observed twice first-party while a DIFFERENT worker was linting —
# seat hayk on 2026-08-22, and a second lane on 2026-08-23 — both times on a
# branch with nothing wrong with it, both times green on a bare retry. The push
# was refused for a reason having nothing to do with the pushed diff. That is
# not an inconvenience here: our rule is that work is not complete until `git
# push` succeeds, sessions are killed mid-flight by quota walls and stall
# watchdogs, and a seat that reads a red push as its own breakage sleeps with
# the branch on one disk.
#
# THE TRAP THIS FILE EXISTS TO HOLD SHUT. The fix is a bounded retry, and the
# obvious wrong version of it — retry on any lint failure, or skip the lint on
# contention — makes the gate tolerant of the PUSHER'S OWN violation, which is
# strictly worse than the bug. So every row here pins a DIRECTION, and the
# retried rows and the refused rows are equally load-bearing. The instrument for
# that is the invocation COUNT, not the exit status: a run that fails after four
# attempts and a run that fails after one are the same verdict and a completely
# different gate.
#
# THE SANDBOX. Rows drive the REAL .githooks/pre-push — copied, not stubbed —
# with a scripted `just` first on PATH and stub siblings beside the copy, which
# is where `$(dirname "$0")` sends it. A stub of the hook itself would pin this
# file's idea of the hook rather than the hook. What IS stubbed is everything
# the hook shells out to, because the row is about the hook's control flow.
#
# Run via: just test-hooks
set -u

# When run under a git hook (this file runs from pre-push via `just test`),
# GIT_DIR and friends leak in. Isolate before anything touches git.
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

HOOKS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
HOOK="$HOOKS_DIR/pre-push"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok() {
    pass=$((pass + 1))
    printf 'ok   - %s\n' "$1"
}
bad() {
    fail=$((fail + 1))
    printf 'FAIL - %s\n' "$1"
}
check() {
    local name="$1" want="$2" got="$3"
    if [ "$want" = "$got" ]; then ok "$name"; else bad "$name (expected $want, got $got)"; fi
}

# The literal golangci-lint emits when it cannot take the lock. Spelled once,
# here, and used by both the fixture and the assertions: if upstream ever
# changes the sentence, this file must be the thing that goes red.
LOCK_LINE='The command is terminated due to an error: parallel golangci-lint is running'

mkdir -p "$TMP/bin"

# A `just` whose lint-new is scripted per invocation. Each line of $STATE/script
# is `<rc>|<stdout>`; the last line repeats once the script runs out, which is
# what lets a row say "the lock never clears" without knowing the retry budget.
cat >"$TMP/bin/just" <<'STUB'
#!/usr/bin/env bash
state="$JUST_STATE"
printf '%s\n' "$1" >>"$state/calls"
case "$1" in
    lint-new)
        n=$(($(cat "$state/lintcount" 2>/dev/null || echo 0) + 1))
        printf '%s\n' "$n" >"$state/lintcount"
        line="$(awk -v n="$n" 'NR==n {print; found=1} END {if (!found) print last} {last=$0}' "$state/script")"
        printf '%s\n' "${line#*|}"
        exit "${line%%|*}"
        ;;
    ensure-golangci) exit "${STUB_ENSURE_RC:-0}" ;;
    *) exit 0 ;;
esac
STUB
chmod +x "$TMP/bin/just"

# $1 = row directory name, remaining args = script lines.
# Echoes "<rc> <lint-new invocation count>"; leaves the hook's combined output
# in $TMP/<row>/out.
run_hook() {
    local row="$1"
    shift
    local dir="$TMP/$row"
    mkdir -p "$dir/hooks" "$dir/state" "$dir/cwd"
    cp "$HOOK" "$dir/hooks/pre-push"
    local sibling
    for sibling in guard-push-destination gate-pushed-commits bd-gh-sync; do
        printf '#!/usr/bin/env bash\ncat >/dev/null\nexit 0\n' >"$dir/hooks/$sibling"
        chmod +x "$dir/hooks/$sibling"
    done
    printf '%s\n' "$@" >"$dir/state/script"
    local rc=0
    (
        cd "$dir/cwd" || exit 99
        PATH="$TMP/bin:$PATH" \
            JUST_STATE="$dir/state" \
            STUB_ENSURE_RC="${STUB_ENSURE_RC:-0}" \
            GQLC_LINT_LOCK_DELAY=0 \
            GQLC_LINT_LOCK_RETRIES=3 \
            bash "$dir/hooks/pre-push" origin git@example.invalid:x/y.git </dev/null \
            >"$dir/out" 2>&1
    ) || rc=$?
    printf '%s %s\n' "$rc" "$(cat "$dir/state/lintcount" 2>/dev/null || echo 0)"
}

says() { grep -qF "$2" "$TMP/$1/out" && echo yes || echo no; }

# --- the reported failure: a neighbour's lock that clears ---------------------
# Two locked attempts then a clean one. This is the row the bead was filed for.

got="$(run_hook clears "3|$LOCK_LINE" "3|$LOCK_LINE" "0|0 issues.")"
check "lock clears on retry: the push is ALLOWED" "0" "${got% *}"
check "lock clears on retry: lint-new actually ran three times" "3" "${got#* }"
check "lock clears on retry: the wait is announced, not silent" yes \
    "$(says clears 'this is not your branch')"

# --- the lock that never clears: still REFUSED --------------------------------
# A gate that yields under load is not a gate. Budget is 1 + 3 retries.

got="$(run_hook stuck "3|$LOCK_LINE")"
check "lock never clears: the push is REFUSED" "3" "${got% *}"
check "lock never clears: the retry budget is bounded and spent" "4" "${got#* }"
check "lock never clears: the refusal says it is not the pusher's branch" yes \
    "$(says stuck 'NOT YOUR BRANCH')"
# The GOTOOLCHAIN diagnosis below the retry loop shares this exit code and is
# wrong about a lock. Two diagnoses, one of them wrong, is worse than one.
check "lock never clears: the toolchain misdiagnosis is suppressed" no \
    "$(says stuck 'could not form an opinion')"

# --- THE OPPOSITE DIRECTION: the pusher's own violation -----------------------
# golangci-lint's --issues-exit-code is 1 and nothing here overrides it, so 1 is
# the only code that means "I graded the code and found issues". The count is
# the assertion that matters: a retried finding would still fail in the end, and
# a verdict-only row could not tell the two gates apart.

got="$(run_hook finding "1|internal/x/y.go:3:1: undefined: q (typecheck)")"
check "pusher's own finding: the push is REFUSED" "1" "${got% *}"
check "pusher's own finding: it is NOT retried" "1" "${got#* }"

# The adversarial spelling of the same claim. A finding whose text happens to
# contain the lock sentence — a test fixture, a comment, this very file — must
# not buy a retry. The hook requires the sentence AND a code that is not 1.
got="$(run_hook findingsayslock "1|internal/x/y.go:3:1: string literal \"$LOCK_LINE\" (goconst)")"
check "a finding that QUOTES the lock sentence: REFUSED" "1" "${got% *}"
check "a finding that QUOTES the lock sentence: not retried" "1" "${got#* }"

# --- a non-lock hard failure keeps its own diagnosis --------------------------
# `--config` on a file golangci-lint will not load exits 3, measured on the
# pinned v2.13.1 on 2026-08-22 — same code as the lock, different cause, and no
# amount of waiting fixes it.

got="$(run_hook badconfig '3|can'"'"'t load config: unsupported version of the configuration')"
check "a non-lock exit 3: the push is REFUSED" "3" "${got% *}"
check "a non-lock exit 3: it is NOT retried" "1" "${got#* }"
check "a non-lock exit 3: the pre-existing diagnosis still prints" yes \
    "$(says badconfig 'could not form an opinion')"

# --- the negative control -----------------------------------------------------
# If this ever read REFUSED, every refusal above would be meaningless.

got="$(run_hook clean '0|0 issues.')"
check "a clean lint: the push is ALLOWED" "0" "${got% *}"
check "a clean lint: lint-new ran exactly once" "1" "${got#* }"

# --- the harness is not passing vacuously -------------------------------------
# Every row above reaches the lint arm only if `just ensure-golangci` succeeded.
# If the stub ever stopped being found on PATH, the hook would take the
# provisioning branch and the "REFUSED" rows would stay green for the wrong
# reason. This pins that the arm is reachable and that the copy really is the
# shipped hook.
got="$(STUB_ENSURE_RC=1 run_hook noline '0|0 issues.')"
check "harness: with provisioning failed the hook refuses BEFORE linting" "1" "${got% *}"
check "harness: with provisioning failed lint-new is never reached" "0" "${got#* }"

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
