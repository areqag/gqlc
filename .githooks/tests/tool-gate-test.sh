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
# lint-new's own findings stay on stdout, where the recipe puts them today; the
# hook's diagnoses are on stderr. The two are asserted separately so a change
# that routed the findings into the diagnosis stream would be visible here.
assert_out_has() {
    if grep -qF -- "$2" "$OUT"; then ok "$1"; else bad "$1 (stdout lacks '$2')"; fi
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

# git runs a hook from the top of the worktree, so the sandbox needs the one
# file pre-push reads from there: the go directive names the toolchain its
# no-opinion diagnosis tells the developer to re-run under. The version here is
# deliberately NOT the one this repo pins: a hook that spelled its own go.mod's
# version into the message would pass an assertion written against 1.26.6, and
# would fail the one below.
printf 'module sandbox\n\ngo 1.21.9\n' >"$SANDBOX/go.mod"

cat >"$STUB_JUST_DIR/just" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$1" >>"$JUST_LOG"
case "$1" in
    ensure-golangci) exit "${STUB_ENSURE_RC:-0}" ;;
    fmt-check)       exit "${STUB_FMT_RC:-0}" ;;
    lint-new)        printf '%b' "${STUB_LINT_OUT:-}"; exit "${STUB_LINT_RC:-0}" ;;
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

# --- pre-push: the linter ran and could not form an opinion (bd gqlc-m9ca) ---
# The third state the provisioning arms below never considered: the binary
# provisions perfectly and then reports on a standard library it cannot read,
# because the machine's default toolchain is one golangci-lint was not built
# against. Measured 2026-08-22 in this repo: go.mod names go 1.26.6, the machine
# default was go1.27.0-X:nodwarf5 (a custom build), and `golangci-lint run
# --new-from-rev origin/master` under the then-pinned v2.12.2 produced the
# fixture below. That pin has since moved to v2.13.1 (#1071), built against
# go1.27.0, and lint-new is green under the same default toolchain today — so
# this fixture is a RECORDING, not a live reproducer, and the two rows further
# down (a panic, and a config that will not load) are the ones still reachable
# at the current pin.
#
# EXIT CODE 1 -- the same code as a real finding, which is why the classification
# reads the OUTPUT and not the status. Anything keyed on the exit code would have
# to treat every real lint finding the same way.
TOOLCHAIN_LINT_OUT='../../../../../usr/lib/go/src/crypto/internal/randutil/randutil.go:11:2: could not import math/rand/v2 (/usr/lib/go/src/math/rand/v2/rand.go:213:17: method must have no type parameters) (typecheck)\n\t"math/rand/v2"\n\t^\n2 issues:\n* typecheck: 2\n'
# A real finding, for the contrast rows. Repo-relative path, a named linter that
# is not typecheck: everything the classification must NOT fire on.
# shellcheck disable=SC2016 # the backticks are errcheck's own message, quoted
# verbatim; expanding them would make the fixture stop matching real output
REAL_LINT_OUT='internal/parser/walk.go:412:6: Error return value of `w.Close` is not checked (errcheck)\n\tdefer w.Close()\n\t     ^\n1 issues:\n* errcheck: 1\n'
# The fixture that separates the two halves of the tell. This tree does not
# compile, so golangci-lint reports typecheck — the SAME linter name and the
# same `* typecheck:` summary line as the toolchain failure — about a file
# INSIDE the repo. It must read as a finding: the developer's own code is
# broken and no toolchain change will help.
#
# Written because the classification survived a mutation without it. Dropping
# the outside-repo half of the pattern and keeping `(typecheck)` alone passed
# all 50 rows, so the hook's comment claiming the summary line is unusable on
# its own was true and unpinned.
INREPO_TYPECHECK_OUT='internal/parser/walk.go:88:14: undefined: notAFunction (typecheck)\n\treturn notAFunction(x)\n\t       ^\n1 issues:\n* typecheck: 1\n'

run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=1 STUB_LINT_OUT="$TOOLCHAIN_LINT_OUT"
assert_rc_nonzero "pre-push: a linter that cannot form an opinion still BLOCKS"
assert_err_has "pre-push: the no-opinion block says the lint did not report on this repo" \
    "could not form an opinion"
assert_err_has "pre-push: the no-opinion block names the remedy that makes the gate RUN" \
    "GOTOOLCHAIN="
# The remedy has to carry the version, not just the variable name: `GOTOOLCHAIN=`
# alone is not something a seat can paste, and the whole complaint on gqlc-m9ca
# is that the seat is left without a next move. Asserted against the SANDBOX's
# go directive (1.21.9), which this repo does not use, so a version written into
# the hook by hand reddens this row instead of passing it.
assert_err_has "pre-push: the remedy names the version go.mod asks for" "GOTOOLCHAIN=go1.21.9"
# The escape hatch is for a MISSING linter and this linter is present. Widening
# it here would open it on exit code 1, which is every real finding in the repo.
assert_err_lacks "pre-push: the no-opinion block does not offer the missing-linter hatch" \
    "GQLC_ALLOW_MISSING_LINTER=1 git push"
# Both presentations share the "could not form an opinion" headline, so the rows
# above pass on either account being printed. These two pin WHICH account this
# shape gets. The exit code here IS 1, so the exit-code sentence would be
# self-contradictory prose — it would tell the seat that 1 is not a findings
# verdict, when 1 is the only code that is one.
assert_err_has "pre-push: the typecheck shape is diagnosed as an unreadable stdlib" \
    "typecheck error against a file OUTSIDE"
assert_err_lacks "pre-push: the typecheck shape does not borrow the exit-code account" \
    "the linter failed rather than graded"

# THE ROW THAT PINS THE DECISION rather than the diagnosis: the hatch does not
# apply to a linter that is present. Without this, widening it later would pass
# every other row in this file.
run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=1 STUB_LINT_OUT="$TOOLCHAIN_LINT_OUT" \
    GQLC_ALLOW_MISSING_LINTER=1
assert_rc_nonzero "pre-push: GQLC_ALLOW_MISSING_LINTER does not open on a present linter"

# CONTRAST, and it is the half that discriminates: the row above asserts a block,
# and a hook that blocked on every lint failure would pass it without owning a
# classifier at all. These two say the classifier can also stay QUIET.
run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=1 STUB_LINT_OUT="$REAL_LINT_OUT"
assert_rc_nonzero "pre-push: a real finding blocks the push"
assert_err_lacks "pre-push: a real finding is not diagnosed as a toolchain failure" \
    "could not form an opinion"
assert_err_lacks "pre-push: a real finding is not answered with GOTOOLCHAIN" "GOTOOLCHAIN="

run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=1 STUB_LINT_OUT="$INREPO_TYPECHECK_OUT"
assert_rc_nonzero "pre-push: a tree that does not compile blocks the push"
assert_err_lacks "pre-push: an IN-REPO typecheck error is a finding, not a toolchain failure" \
    "could not form an opinion"

# --- the SECOND presentation of the same cause (bd gqlc-m9ca) ----------------
# The same command that produced TOOLCHAIN_LINT_OUT produced this instead,
# minutes later in the same worktree, differing only in build-cache state:
# golangci-lint panicked outright, exit code 2, and emitted no typecheck
# diagnostic at all. Measured, both first-party. A classification that knew only
# the typecheck shape went silent on this one — which is the original defect,
# unfixed, for half the occurrences.
#
# `--issues-exit-code` defaults to 1 (`golangci-lint run --help`, v2.12.2) and
# nothing in this repo overrides it, so exit 2 is on its own proof that the run
# is not a findings verdict.
PANIC_LINT_OUT='panic: load: 1 errors occurred:\n\tcould not load export data\n\ngoroutine 1358 [running]:\ngithub.com/golangci/golangci-lint/v2/pkg/goanalysis.(*loadingPackage).loadWithFacts(0x0)\n\tpkg/goanalysis/runner_loadingpackage.go:315 +0x128\n'

run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=2 STUB_LINT_OUT="$PANIC_LINT_OUT"
assert_rc_nonzero "pre-push: a linter that panicked still BLOCKS"
assert_err_has "pre-push: a panicked linter is named as forming no opinion" \
    "could not form an opinion"
assert_err_has "pre-push: the panic branch cites the exit-code contract it read" \
    "--issues-exit-code is 1"
assert_err_has "pre-push: the panic branch says the linter failed rather than graded" \
    "the linter failed rather than graded"
assert_err_has "pre-push: the panic branch reports the code it actually saw" "It exited 2"
assert_err_has "pre-push: the panic branch still names the remedy" "GOTOOLCHAIN=go1.21.9"
# The two presentations must not borrow each other's account. This output holds
# no typecheck diagnostic, so claiming one would be a false explanation of a
# real failure — the exact defect class PR #1029 spent eight rounds on.
assert_err_lacks "pre-push: the panic branch does not claim a typecheck diagnostic" \
    "typecheck error against a file OUTSIDE"
assert_out_has "pre-push: the panic's own stack survives the classification" \
    "goroutine 1358 [running]"
# The exit-code branch may not PRESCRIBE the toolchain: it has read no evidence
# for it, and the row below is a cause with the same branch and a different fix.
assert_err_lacks "pre-push: the exit-code branch does not prescribe the toolchain fix" \
    "Re-run the push under the toolchain go.mod names"

# A THIRD cause reaching the same branch, and the reason the branch may not
# assert the toolchain. `golangci-lint run --config /dev/null` exits 3 on the
# pinned v2.13.1 — measured first-party 2026-08-22 — because the config will not
# load. It is a present, healthy linter failing for a reason no GOTOOLCHAIN
# touches. This row exists so a later edit that hardens the conditional framing
# back into a diagnosis reddens here.
BADCONFIG_LINT_OUT='Error: can.t load config: unsupported version of the configuration: ""\nThe command is terminated due to an error.\n'

run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=3 STUB_LINT_OUT="$BADCONFIG_LINT_OUT"
assert_rc_nonzero "pre-push: a linter that would not load its config still BLOCKS"
assert_err_has "pre-push: a config failure is named as forming no opinion" \
    "could not form an opinion"
assert_err_has "pre-push: a config failure reports the code it actually saw" "It exited 3"
assert_err_has "pre-push: a config failure is told the code does not say why" \
    "the exit code does not say"
assert_err_has "pre-push: a config failure is told GOTOOLCHAIN may not be its fix" \
    "GOTOOLCHAIN will not move it"
assert_err_lacks "pre-push: a config failure is not diagnosed as an unreadable stdlib" \
    "typecheck error against a file OUTSIDE"

# The remedy's fallback when there is no go directive to read. Without this row,
# a broken extraction prints the literal `GOTOOLCHAIN=go ` — a pasteable command
# that sets the variable to nothing — and every other row still passes.
mv "$SANDBOX/go.mod" "$SANDBOX/go.mod.hidden"
run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=1 STUB_LINT_OUT="$TOOLCHAIN_LINT_OUT"
mv "$SANDBOX/go.mod.hidden" "$SANDBOX/go.mod"
assert_err_has "pre-push: with no go.mod the remedy still names GOTOOLCHAIN" "GOTOOLCHAIN="
assert_err_lacks "pre-push: with no go.mod the remedy is not an empty version" "GOTOOLCHAIN=go "

# Whichever class it is, what the linter said reaches the developer. A hook that
# captured the output to classify it and then dropped it would satisfy every
# assertion above while hiding the finding the push is being blocked for.
#
# Each of these two re-runs the hook immediately above its own assertion rather
# than leaning on the last run in the file. That is not ceremony: an earlier
# draft asserted against a run three rows up, and inserting a case between them
# made the assertion read the wrong stdout and redden for a reason that had
# nothing to do with what it tests.
run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=1 STUB_LINT_OUT="$REAL_LINT_OUT"
assert_out_has "pre-push: a real finding's own text survives the classification" \
    "Error return value of \`w.Close\` is not checked"
run_hook pre-push STUB_ENSURE_RC=0 STUB_LINT_RC=1 STUB_LINT_OUT="$TOOLCHAIN_LINT_OUT"
assert_out_has "pre-push: the no-opinion output survives the classification too" \
    "could not import math/rand/v2"

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
