#!/usr/bin/env bash
# Unit tests for the tool-provisioning gate (bd gqlc-l45j): the arms in
# .githooks/pre-commit and .githooks/pre-push that decide what happens when
# golangci-lint cannot be provisioned, and the retry loop in the justfile's
# ensure-golangci that both of them call.
#
# Both hook arms used to warn and continue on a provisioning failure, so an
# HTTP 429 on the download retired the format gate and the lint gate behind one
# line on stderr. These cases pin the blocking behaviour, pin the single escape
# hatch, and pin that a healthy provisioning still runs the gate it guards --
# because a hook that blocks unconditionally would pass the first three of
# those on its own.
#
# The hooks are driven through a stub `just` on PATH: the arms under test
# branch on ensure-golangci's exit status, and the stub is what makes that
# status settable without a network. ensure-golangci itself is exercised for
# real, over a copy of the repo justfile taken fresh on every run, placed in a
# temp directory so the sandbox .bin/ is empty and the version-check early exit
# does not swallow the download cases. Its `curl` is a stub that counts its own
# invocations, which is how "it retried" is asserted as a number rather than
# inferred from a wall-clock delay.
#
# Run via: just test-hooks
set -u

# When run under a git hook (this file runs from pre-push via `just test`),
# GIT_DIR and friends leak in and would point the sandbox at the parent repo.
unset "${!GIT_@}"

HOOKS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO_ROOT="$(cd "$HOOKS_DIR/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

OUT="$TMP/stdout"
ERR="$TMP/stderr"
JUST_LOG="$TMP/just.log"
CURL_COUNT_FILE="$TMP/curl.count"

pass=0
fail=0
last_rc=0
curl_calls=0

ok() {
    pass=$((pass + 1))
    printf 'ok   - %s\n' "$1"
}
bad() {
    fail=$((fail + 1))
    printf 'FAIL - %s\n' "$1"
}

assert_rc_zero() {
    if [ "$last_rc" -eq 0 ]; then ok "$1"; else bad "$1 (expected exit 0, got $last_rc)"; fi
}
assert_rc_nonzero() {
    if [ "$last_rc" -ne 0 ]; then ok "$1"; else bad "$1 (expected non-zero exit, got 0)"; fi
}
assert_err_has() {
    if grep -qF -- "$2" "$ERR"; then ok "$1"; else bad "$1 (stderr lacks '$2')"; fi
}
assert_err_lacks() {
    if grep -qF -- "$2" "$ERR"; then bad "$1 (stderr carries '$2')"; else ok "$1"; fi
}
assert_just_called() {
    if grep -qxF -- "$2" "$JUST_LOG"; then ok "$1"; else bad "$1 (just was never run with '$2')"; fi
}
assert_just_not_called() {
    if grep -qxF -- "$2" "$JUST_LOG"; then bad "$1 (just WAS run with '$2')"; else ok "$1"; fi
}
assert_curl_calls() {
    if [ "$curl_calls" -eq "$2" ]; then ok "$1"; else bad "$1 (expected $2 curl calls, counted $curl_calls)"; fi
}

# --- hook sandbox -----------------------------------------------------------
# The hooks are copied rather than run in place because pre-push resolves its
# siblings through `dirname "$0"`; a copy is what puts a stub there. The copy
# is taken from the tree on every run, so it cannot go stale, and a copy that
# failed shows up as every accept case turning into a reject.
SANDBOX="$TMP/sandbox"
HOOKS="$TMP/hooks"
STUB_JUST_DIR="$TMP/bin-just"
mkdir -p "$SANDBOX" "$HOOKS" "$STUB_JUST_DIR"
cp "$HOOKS_DIR/pre-commit" "$HOOKS/pre-commit"
cp "$HOOKS_DIR/pre-push" "$HOOKS/pre-push"
# The real guard, not a stub: it makes no network call and touches no state, so
# a stub would only be an opportunity for the sandbox to disagree with the
# tree. run_hook feeds it an empty ref list, which is the "nothing to push"
# case it accepts — these tests are about the provisioning arms below it.
# .githooks/tests/worktree-upstream-test.sh is where it is decided on.
cp "$HOOKS_DIR/guard-push-destination" "$HOOKS/guard-push-destination"
chmod +x "$HOOKS/pre-commit" "$HOOKS/pre-push" "$HOOKS/guard-push-destination"

cat >"$HOOKS/bd-gh-sync" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$HOOKS/bd-gh-sync"

cat >"$STUB_JUST_DIR/just" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$1" >>"$JUST_LOG"
case "$1" in
    ensure-golangci) exit "${STUB_ENSURE_RC:-0}" ;;
    fmt-check)       exit "${STUB_FMT_RC:-0}" ;;
    lint-new)        exit "${STUB_LINT_RC:-0}" ;;
    test)            exit "${STUB_TEST_RC:-0}" ;;
    *)               exit 0 ;;
esac
STUB
chmod +x "$STUB_JUST_DIR/just"

# $1=hook name, rest=KEY=VALUE overrides for the stub
run_hook() {
    local hook="$1"
    shift
    : >"$JUST_LOG"
    (
        cd "$SANDBOX" \
            && env PATH="$STUB_JUST_DIR:$PATH" JUST_LOG="$JUST_LOG" \
                BRANCH_OVERRIDE=feature/tool-gate-test "$@" "$HOOKS/$hook"
        # git always hands pre-push a ref list on stdin; nothing does when the
        # hook is run by hand, and without this the guard at the top of it
        # reads the terminal and the suite hangs.
    ) </dev/null >"$OUT" 2>"$ERR"
    last_rc=$?
}

# --- pre-commit: provisioning healthy ---------------------------------------
run_hook pre-commit STUB_ENSURE_RC=0 STUB_FMT_RC=0
assert_rc_zero "pre-commit: formatted tree commits"
assert_just_called "pre-commit: healthy provisioning runs fmt-check" fmt-check

run_hook pre-commit STUB_ENSURE_RC=0 STUB_FMT_RC=1
assert_rc_nonzero "pre-commit: unformatted tree is blocked"
assert_err_has "pre-commit: unformatted tree names the fix" "just fmt"

# --- pre-commit: provisioning failed ----------------------------------------
# The regression this file exists for. Before bd gqlc-l45j this case exited 0.
run_hook pre-commit STUB_ENSURE_RC=1
assert_rc_nonzero "pre-commit: provisioning failure BLOCKS the commit"
assert_err_has "pre-commit: block names the cause" "could not provision golangci-lint"
assert_err_has "pre-commit: block names the escape hatch" "GQLC_ALLOW_MISSING_LINTER=1"
assert_just_not_called "pre-commit: a failed provisioning does not reach fmt-check" fmt-check

run_hook pre-commit STUB_ENSURE_RC=1 GQLC_ALLOW_MISSING_LINTER=1
assert_rc_zero "pre-commit: escape hatch lets the commit through"
assert_err_has "pre-commit: escape hatch announces the skip" "SKIPPED by GQLC_ALLOW_MISSING_LINTER=1"
assert_just_not_called "pre-commit: escape hatch does not run fmt-check" fmt-check

# A hatch that opened on any non-empty value would open on a stale `export
# GQLC_ALLOW_MISSING_LINTER=0` in a shell profile.
run_hook pre-commit STUB_ENSURE_RC=1 GQLC_ALLOW_MISSING_LINTER=0
assert_rc_nonzero "pre-commit: GQLC_ALLOW_MISSING_LINTER=0 does not open the hatch"
run_hook pre-commit STUB_ENSURE_RC=1 GQLC_ALLOW_MISSING_LINTER=yes
assert_rc_nonzero "pre-commit: GQLC_ALLOW_MISSING_LINTER=yes does not open the hatch"

# --- pre-push: provisioning healthy -----------------------------------------
run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=0
assert_rc_zero "pre-push: clean tree pushes"
assert_just_called "pre-push: healthy provisioning runs lint-new" lint-new

run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=1
assert_rc_nonzero "pre-push: a lint-new finding blocks the push"

run_hook pre-push STUB_ENSURE_RC=0 STUB_TEST_RC=1
assert_rc_nonzero "pre-push: a failing test suite blocks the push"
assert_just_not_called "pre-push: a failing test suite does not reach lint-new" lint-new

# --- pre-push: provisioning failed ------------------------------------------
run_hook pre-push STUB_ENSURE_RC=1
assert_rc_nonzero "pre-push: provisioning failure BLOCKS the push"
assert_err_has "pre-push: block names the cause" "could not provision golangci-lint"
assert_err_has "pre-push: block names the escape hatch" "GQLC_ALLOW_MISSING_LINTER=1"
assert_just_not_called "pre-push: a failed provisioning does not reach lint-new" lint-new

run_hook pre-push STUB_ENSURE_RC=1 GQLC_ALLOW_MISSING_LINTER=1
assert_rc_zero "pre-push: escape hatch lets the push through"
assert_err_has "pre-push: escape hatch announces the skip" "SKIPPED by GQLC_ALLOW_MISSING_LINTER=1"
assert_just_not_called "pre-push: escape hatch does not run lint-new" lint-new

run_hook pre-push STUB_ENSURE_RC=1 GQLC_ALLOW_MISSING_LINTER=0
assert_rc_nonzero "pre-push: GQLC_ALLOW_MISSING_LINTER=0 does not open the hatch"

# --- ensure-golangci retry --------------------------------------------------
# The real recipe, over a fresh copy of the real justfile. `just -f` makes
# justfile_directory() the sandbox, so the recipe looks for .bin/golangci-lint
# there and finds nothing -- without that the version check exits 0 on the
# developer's own binary and every case below passes without a download.
STUB_CURL_DIR="$TMP/bin-curl"
mkdir -p "$STUB_CURL_DIR"
cat >"$STUB_CURL_DIR/curl" <<'STUB'
#!/usr/bin/env bash
n=$(($(cat "$CURL_COUNT") + 1))
printf '%s' "$n" >"$CURL_COUNT"
if [ "$n" -le "$CURL_FAIL_FIRST" ]; then
    printf 'curl: (22) The requested URL returned error: 429\n' >&2
    exit 22
fi
# What install.sh would be: a script `sh -s` runs and that succeeds.
printf 'exit 0\n'
STUB
chmod +x "$STUB_CURL_DIR/curl"

new_sandbox() {
    local dir="$TMP/$1"
    mkdir -p "$dir"
    cp "$REPO_ROOT/justfile" "$dir/justfile"
    printf '%s' "$dir"
}

# $1=sandbox dir, $2=how many leading curl calls fail, $3=attempts, $4=delay
run_ensure() {
    printf '0' >"$CURL_COUNT_FILE"
    (
        cd "$1" \
            && env PATH="$STUB_CURL_DIR:$PATH" \
                CURL_COUNT="$CURL_COUNT_FILE" CURL_FAIL_FIRST="$2" \
                GQLC_PROVISION_ATTEMPTS="$3" GQLC_PROVISION_DELAY="$4" \
                just -f "$1/justfile" ensure-golangci
    ) >"$OUT" 2>"$ERR"
    last_rc=$?
    curl_calls="$(cat "$CURL_COUNT_FILE")"
}

SB_FAIL="$(new_sandbox ensure-fail)"
run_ensure "$SB_FAIL" 99 3 0
assert_rc_nonzero "ensure-golangci: exhausted retries still FAIL"
assert_curl_calls "ensure-golangci: a permanent 429 is retried, not tried once" 3
assert_err_has "ensure-golangci: the first attempt is reported" "attempt 1 of 3"
assert_err_has "ensure-golangci: the last attempt is reported" "attempt 3 of 3"
assert_err_has "ensure-golangci: the failure counts the attempts" "after 3 attempt(s)"
assert_err_has "ensure-golangci: the failure says it is not a lint finding" "not a lint finding"

SB_FLAKE="$(new_sandbox ensure-flake)"
run_ensure "$SB_FLAKE" 2 4 0
assert_rc_zero "ensure-golangci: two 429s then a success is a success"
assert_curl_calls "ensure-golangci: it stops retrying once one attempt works" 3
assert_err_lacks "ensure-golangci: a recovered download reports no error" "could not provision"

# A budget below 1 must not read as "nothing to do".
SB_ZERO="$(new_sandbox ensure-zero)"
run_ensure "$SB_ZERO" 99 0 0
assert_rc_nonzero "ensure-golangci: an attempts budget of 0 fails rather than passes"
assert_curl_calls "ensure-golangci: an attempts budget of 0 downloads nothing" 0

# The happy path has to stay download-free, or every commit would pay a network
# round trip and the 429 window would block work outright.
SB_WARM="$(new_sandbox ensure-warm)"
PINNED="$(just -f "$SB_WARM/justfile" --evaluate golangci_version)"
mkdir -p "$SB_WARM/.bin"
# Version read out of the justfile, not spelled again here: a literal would let
# this case go green against a stale pin after a version bump.
cat >"$SB_WARM/.bin/golangci-lint" <<EOF
#!/usr/bin/env bash
printf '%s\n' "${PINNED#v}"
EOF
chmod +x "$SB_WARM/.bin/golangci-lint"
run_ensure "$SB_WARM" 99 3 0
assert_rc_zero "ensure-golangci: a warm .bin/ succeeds"
assert_curl_calls "ensure-golangci: a warm .bin/ makes no network call" 0

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
