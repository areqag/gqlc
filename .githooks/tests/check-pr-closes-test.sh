#!/usr/bin/env bash
# Tests for .github/scripts/check-pr-closes.py (bd gqlc-nyo, bd gqlc-w4al).
#
# The checker decides whether a PR may merge without closing the GitHub issue
# its bead mirrors. Every one of its exits is a pass except two, so the
# interesting property is not "does it fail on a bad body" but "is there any
# input it cannot read that it passes anyway" — a gate whose inputs go missing
# quietly is a gate that has stopped running while still reporting green. Its
# body used to arrive as an environment variable filled by a grep pipeline
# whose failure was indistinguishable from "no bead on this PR"; it now
# arrives as a file, and the cases below hold both that file and the bd export
# to fail-closed.
#
# The workflow half — that anything re-runs when a PR body changes, and that
# what is read is the live body rather than the frozen event payload — is
# pinned in internal/tools/ciguard, against the parsed ci.yml.
#
# Run via: just test-hooks
set -u

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$REPO/.github/scripts/check-pr-closes.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { pass=$((pass + 1)); printf 'ok   - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s: %s\n' "$1" "$2"; }

# A bd export with one bead of each shape the checker branches on.
EXPORT="$TMP/issues.jsonl"
cat >"$EXPORT" <<'JSONL'
{"id":"gqlc-mirrored","issue_type":"bug","external_ref":"https://github.com/areqag/gqlc/issues/617"}
{"id":"gqlc-local","issue_type":"task","external_ref":""}
{"id":"gqlc-umbrella","issue_type":"epic","external_ref":"https://github.com/areqag/gqlc/issues/500"}
JSONL

# $1=file contents; echoes the path of a body file holding them.
body() {
    local f
    f="$TMP/body.$RANDOM.txt"
    printf '%s' "$1" >"$f"
    printf '%s' "$f"
}

# Leaves the checker's combined output in $OUT and its status in $RC.
# $1=export path, $2=body file path, $3=branch name.
OUT=""
RC=0
run_check() {
    OUT="$(python3 "$SCRIPT" "$1" "$2" "$3" 2>&1)"
    RC=$?
}

# $1=case name, $2=export, $3=body file, $4=branch, $5=expected substring.
# The substring is required as well as the status: a gate that refuses without
# saying which issue number it wanted sends the author back to the workflow
# source to find out, which is how the old failure text ("Add 'Closes #598' to
# the PR body") stranded a PR whose body already said exactly that.
expect_red() {
    run_check "$2" "$3" "$4"
    if [ "$RC" -eq 0 ]; then
        bad "$1" "exited 0: $OUT"
    elif ! printf '%s' "$OUT" | grep -qF "$5"; then
        bad "$1" "refused, but did not say '$5': $OUT"
    else
        ok "$1"
    fi
}

# $1=case name, $2=export, $3=body file, $4=branch.
expect_green() {
    run_check "$2" "$3" "$4"
    if [ "$RC" -eq 0 ]; then
        ok "$1"
    else
        bad "$1" "exited $RC: $OUT"
    fi
}

# --- the gate's reason for existing -----------------------------------------

expect_green "a body closing the bead's own issue passes" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

Closes #617')" "some/branch"

expect_red "a body with no Closes line is refused, naming the issue" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored')" "some/branch" \
    "missing 'Closes #617'"

expect_red "a body closing the wrong issue is refused, naming both numbers" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

Closes #123')" "some/branch" \
    "closes #123 but bead gqlc-mirrored maps to #617"

# The escape from the state this bead was filed over: the failure has to say
# that editing the body is enough. Pushing a commit and reopening the PR were
# the only two ways out, and the message named neither.
expect_red "the refusal says an edit alone re-runs the check" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored')" "some/branch" \
    "Editing the body re-runs this check on its own"

# --- inputs the checker cannot read are refusals, not passes -----------------
# Both of these used to be, or would naturally be, a silent pass. An absent
# body reads as "no Closes line present" only if you already know a body was
# fetched; an absent export reads as "bead not found", which the checker
# passes on purpose so a stale export cannot block a PR.

expect_red "a body file that is not there is refused" \
    "$EXPORT" "$TMP/no-such-body.txt" "some/branch" \
    "cannot read the PR body"

expect_red "an export that is not there is refused" \
    "$TMP/no-such-export.jsonl" "$(body 'Bead: gqlc-mirrored

Closes #617')" "some/branch" \
    "cannot read the bd export"

# An empty body is a body: it was fetched and it says nothing. The branch
# still names the bead, so silence must be refused rather than skipped —
# which is the shape of the fetch failure the step's `bash -e` catches, held
# here from the other side in case it ever does not.
expect_red "an empty body over a bead-bearing branch is refused" \
    "$EXPORT" "$(body '')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# --- which bead the PR is about ---------------------------------------------

expect_red "the bead id is taken from the branch when the body has no Bead line" \
    "$EXPORT" "$(body 'no marker here')" "fix/gqlc-mirrored-thing" \
    "missing 'Closes #617'"

# The body wins, so a branch named after one bead can carry a PR about
# another. If precedence inverted, a body that named its bead explicitly
# would be overruled by whatever the branch name happened to contain.
expect_green "a Bead line in the body overrides the branch name" \
    "$EXPORT" "$(body 'Bead: gqlc-local')" "fix/gqlc-mirrored-thing"

expect_green "a PR naming no bead at all passes" \
    "$EXPORT" "$(body 'no marker here')" "some/branch"

# --- the sanctioned passes --------------------------------------------------
# Each is a way through the gate, so each is a way past it: they are pinned
# so that a fourth cannot be added without a test admitting it.

expect_green "a bead with no GitHub mirror needs no Closes" \
    "$EXPORT" "$(body 'Bead: gqlc-local')" "some/branch"

expect_green "an epic is never closed by a child PR" \
    "$EXPORT" "$(body 'Bead: gqlc-umbrella')" "some/branch"

expect_green "a bead the export does not carry does not block the PR" \
    "$EXPORT" "$(body 'Bead: gqlc-notinexport')" "some/branch"

# --- shapes the body is allowed to take -------------------------------------

expect_green "GitHub's other closing keywords count" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

fixes #617')" "some/branch"

expect_green "a second Closes line does not hide the right one" \
    "$EXPORT" "$(body 'Bead: gqlc-mirrored

Closes #617
Closes #900')" "some/branch"

# One unparseable line ahead of the bead used to abort the whole scan, and an
# aborted scan is a bead "not in the export", which passes. A malformed
# export would therefore have waved every PR through.
BROKEN="$TMP/broken.jsonl"
{ printf 'not json at all\n'; cat "$EXPORT"; } >"$BROKEN"
expect_red "a malformed line ahead of the bead does not hide it" \
    "$BROKEN" "$(body 'Bead: gqlc-mirrored')" "some/branch" \
    "missing 'Closes #617'"

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
