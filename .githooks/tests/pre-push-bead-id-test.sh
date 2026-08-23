#!/usr/bin/env bash
# Tests for bd gqlc-uz3c / GH #1391 — a branch name carrying no bead id, so
# CI's tidy job has nothing to hold a `Closes #N` against, and the refusal
# reads as a test failure because tidy gates lint/test/codegen-fence.
#
#   A. .githooks/warn-missing-bead-id decides correctly over crafted stdin,
#      and NEVER refuses.
#   B. .githooks/pre-push actually runs it, on a real push, at the end.
#   C. The branch form documented in CLAUDE.md and AGENTS.md carries an id
#      that check-pr-closes.py's own fallback can read.
#   D. The warner's pattern is byte-identical to the checker's.
#
# B is what stops A from being decoration: A alone passes with the warner
# wired to nothing. D is what stops C and A from drifting apart from the gate
# they exist to predict — a warner with its own private alphabet warns about
# branches CI accepts, or stays quiet on ones it refuses.
#
# Run via: just test-hooks
set -u

# When run under a git hook (this file runs from pre-push via `just test`),
# GIT_DIR etc. leak in and would redirect every throwaway repo's git commands
# to the parent repo. Isolate completely.
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

HOOKS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT="$(cd "$HOOKS_DIR/.." && pwd)"
WARNER="$HOOKS_DIR/warn-missing-bead-id"
CHECKER="$ROOT/.github/scripts/check-pr-closes.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

GIT=(git -c user.email=t@t.invalid -c user.name=t -c commit.gpgsign=false)

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

# The warner's own words. Asserted on rather than "stderr was non-empty", so
# that a bystander's chatter on the same stream cannot stand in for it — every
# other hook in pre-push writes to stderr too.
MARKER='carries no bead id, so CI cannot resolve a bead'
# The remedy is the whole point of the warning. A message that says a branch is
# wrong without saying what clears it sends the reader back to the check table,
# which is where this bead started.
REMEDY='Bead: <bead-id>'

if [ ! -x "$WARNER" ]; then
    bad "warn-missing-bead-id is missing or not executable at $WARNER"
    printf '\n%d passed, %d failed\n' "$pass" "$fail"
    exit 1
fi

# --- a repository the warner can answer rev-list questions against ----------
# The "commits ahead of origin/master" arm is not decidable from stdin alone,
# so the cases below run with cwd inside a real clone whose origin/master is a
# real ref. WORK_SHA is ahead of it; BASE_SHA is origin/master itself.
ORIGIN="$TMP/origin.git"
"${GIT[@]}" init -q --bare -b master "$ORIGIN"
SEED="$TMP/seed"
"${GIT[@]}" init -q -b master "$SEED"
"${GIT[@]}" -C "$SEED" commit -q --allow-empty -m init
"${GIT[@]}" -C "$SEED" remote add origin "$ORIGIN"
"${GIT[@]}" -C "$SEED" push -q origin master

WORK="$TMP/work"
"${GIT[@]}" clone -q "$ORIGIN" "$WORK"
BASE_SHA="$("${GIT[@]}" -C "$WORK" rev-parse origin/master)"
"${GIT[@]}" -C "$WORK" checkout -q -b fix/some-descriptive-name origin/master
"${GIT[@]}" -C "$WORK" commit -q --allow-empty -m "feature work"
WORK_SHA="$("${GIT[@]}" -C "$WORK" rev-parse HEAD)"
Z=0000000000000000000000000000000000000000

# --- A. warn-missing-bead-id over crafted pre-push stdin --------------------
# $1=name $2=expected(warn|quiet) $3=stdin
warn_case() {
    local name="$1" expected="$2" stdin="$3" err rc decision
    err="$TMP/warn.err"
    rc=0
    (cd "$WORK" && printf '%s' "$stdin" |
        "$WARNER" origin git@example.invalid:x/y.git >/dev/null 2>"$err") || rc=$?
    if grep -qF -- "$MARKER" "$err"; then decision=warn; else decision=quiet; fi
    if [ "$decision" = "$expected" ]; then
        ok "warner: $name"
    else
        bad "warner: $name (expected $expected, got $decision)"
    fi
    # ADVISORY IS THE PROPERTY, so it is asserted on every row rather than on
    # one dedicated row: a non-zero exit on any input reaches pre-push, and
    # `|| true` there is the only thing between that and a refused push.
    if [ "$rc" -ne 0 ]; then
        bad "warner: $name exited $rc — it must never refuse"
    fi
}

warn_case "no bead id, ahead of origin/master (the case this bead is a report of)" \
    warn "refs/heads/fix/some-descriptive-name $WORK_SHA refs/heads/fix/some-descriptive-name $Z
"
warn_case "the branch carries a bead id, so the CI fallback will resolve it" \
    quiet "refs/heads/fix/gqlc-uz3c-prepush-warning $WORK_SHA refs/heads/fix/gqlc-uz3c-prepush-warning $Z
"
warn_case "a sub-bead id, which the checker's alphabet also admits" \
    quiet "refs/heads/fix/gqlc-h9n.22-thing $WORK_SHA refs/heads/fix/gqlc-h9n.22-thing $Z
"
warn_case "an uppercased id — the checker matches case-insensitively" \
    quiet "refs/heads/fix/GQLC-UZ3C-thing $WORK_SHA refs/heads/fix/GQLC-UZ3C-thing $Z
"
# The shape several branches merged on 2026-08-23 actually had. It reads to a
# human as carrying bead ids and is invisible to a reader anchored on the
# prefix, which is exactly why it has a row.
warn_case "bare bead suffixes with no gqlc- prefix (fix/sync-drift-x98l-mdhr)" \
    warn "refs/heads/fix/sync-drift-x98l-mdhr $WORK_SHA refs/heads/fix/sync-drift-x98l-mdhr $Z
"
warn_case "no commits ahead of origin/master — no PR to open" \
    quiet "refs/heads/fix/some-descriptive-name $BASE_SHA refs/heads/fix/some-descriptive-name $Z
"
warn_case "a delete, which publishes no commit" \
    quiet "(delete) $Z refs/heads/fix/some-descriptive-name $WORK_SHA
"
# The all-zero sha under a branch ref, which is the input the zero test uniquely
# guards: the `(delete)` row above is dropped by the ref spelling before it gets
# there. Without this row the zero test is unkillable, and `git rev-list
# origin/master..0000...` fails, which this hook reads as "cannot tell" and
# warns on.
warn_case "an all-zero sha under a branch ref reaches no rev-list guess" \
    quiet "refs/heads/fix/some-descriptive-name $Z refs/heads/fix/some-descriptive-name $WORK_SHA
"
warn_case "master itself" \
    quiet "refs/heads/master $WORK_SHA refs/heads/master $Z
"
warn_case "a tag" \
    quiet "refs/tags/v0 $WORK_SHA refs/tags/v0 $Z
"
warn_case "nothing to push (git runs the hook with empty stdin)" quiet ""
warn_case "HEAD on the left, as 'git push -u origin HEAD' presents it" \
    warn "HEAD $WORK_SHA refs/heads/fix/some-descriptive-name $Z
"

# One branch, two lines: one warning, not two. A hook that repeated itself per
# ref would bury the message it is trying to make visible.
dup_err="$TMP/dup.err"
(cd "$WORK" && printf '%s\n%s\n' \
    "refs/heads/fix/some-descriptive-name $WORK_SHA refs/heads/fix/some-descriptive-name $Z" \
    "refs/heads/fix/some-descriptive-name $WORK_SHA refs/heads/other $Z" |
    "$WARNER" origin git@example.invalid:x/y.git >/dev/null 2>"$dup_err") || true
dup_n="$(grep -cF -- "$MARKER" "$dup_err" || true)"
if [ "$dup_n" = 1 ]; then
    ok "warner: one branch on two ref lines warns once, not twice"
else
    bad "warner: one branch on two ref lines warned $dup_n times"
fi

# The remedy, and the tell that sends readers to the wrong place. Both are the
# content that distinguishes this warning from a message saying only that
# something is wrong.
sole_err="$TMP/sole.err"
(cd "$WORK" && printf '%s\n' \
    "refs/heads/fix/some-descriptive-name $WORK_SHA refs/heads/fix/some-descriptive-name $Z" |
    "$WARNER" origin git@example.invalid:x/y.git >/dev/null 2>"$sole_err") || true
if grep -qF -- "$REMEDY" "$sole_err"; then
    ok "warner: the message names the 'Bead:' line that clears the gate"
else
    bad "warner: the message never names the remedy"
fi
if grep -qF -- 'skipping' "$sole_err"; then
    ok "warner: the message names the 'skipping' shape the failure presents as"
else
    bad "warner: the message does not warn about the misleading check table"
fi
# Advisory, said out loud. A reader who takes this for a refusal goes looking
# for what blocked a push that in fact succeeded.
if grep -qF -- 'ADVISORY' "$sole_err"; then
    ok "warner: the message says it is advisory and is not refusing"
else
    bad "warner: the message does not say it is advisory"
fi

# --- B. pre-push actually runs it, on a real push ---------------------------
# A stub `just` bounds the blast radius: without it a real `just test` runs
# inside a throwaway repo that has none of this one's files. A stub `gh` that
# produces no token is what stops bd-gh-sync — which pre-push reaches just
# before the warner — from talking to GitHub from a test.
STUB_BIN="$TMP/stub-bin"
mkdir -p "$STUB_BIN"
cat >"$STUB_BIN/just" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
cat >"$STUB_BIN/gh" <<'STUB'
#!/usr/bin/env bash
exit 1
STUB
chmod +x "$STUB_BIN/just" "$STUB_BIN/gh"

e2e_err="$TMP/e2e.err"
if env PATH="$STUB_BIN:$PATH" \
    "${GIT[@]}" -C "$WORK" -c core.hooksPath="$HOOKS_DIR" \
    push -q -u origin HEAD >/dev/null 2>"$e2e_err"; then
    ok "pre-push: the warning does not stop a push from a bead-idless branch"
else
    bad "pre-push: the push was REFUSED ($(tail -3 "$e2e_err" | tr '\n' ' '))"
fi
if grep -qF -- "$MARKER" "$e2e_err"; then
    ok "pre-push: the warner is wired in and its words reach the terminal"
else
    bad "pre-push: pushed a bead-idless branch and said nothing (warner not wired in?)"
fi

# The other direction. Without it, a warner hardwired to warn passes every row
# above.
"${GIT[@]}" -C "$WORK" checkout -q -b fix/gqlc-uz3c-e2e
"${GIT[@]}" -C "$WORK" commit -q --allow-empty -m "work on a named branch"
e2e_named_err="$TMP/e2e-named.err"
env PATH="$STUB_BIN:$PATH" \
    "${GIT[@]}" -C "$WORK" -c core.hooksPath="$HOOKS_DIR" \
    push -q -u origin HEAD >/dev/null 2>"$e2e_named_err" || true
if grep -qF -- "$MARKER" "$e2e_named_err"; then
    bad "pre-push: warned about a branch that DOES carry a bead id"
else
    ok "pre-push: silent on a branch whose name carries a bead id"
fi

# --- C. the branch form the documents prescribe -----------------------------
# Extracted and run against the checker's own pattern rather than restated: a
# regex written out here would keep passing after someone reverted the
# document, which is the failure this bead is a report of.
#
# The checker's pattern is lifted from its source so that C fails if the gate's
# alphabet moves under the documents, not only if the documents move.
checker_ere="$(sed -n 's/^BEAD_IN_BRANCH = re\.compile(r"(?i)(\(.*\))")$/\1/p' "$CHECKER")"
if [ -z "$checker_ere" ]; then
    bad "check-pr-closes.py: could not extract BEAD_IN_BRANCH to test against"
    checker_ere='(this-cannot-match)'
fi

# The documents state the form with `<bead-id>` as a placeholder; filling it is
# what turns prose into something the checker can be asked about.
doc_case() {
    local doc="$1" path="$ROOT/$1" form filled
    form="$(grep -m1 -oE '<type>/<bead-id>-<slug>' "$path" || true)"
    if [ -z "$form" ]; then
        bad "$doc: prescribes no branch form containing <bead-id>"
        return
    fi
    filled="${form//<type>/fix}"
    filled="${filled//<bead-id>/gqlc-uz3c}"
    filled="${filled//<slug>/some-work}"
    if printf '%s' "$filled" | grep -Eqi "$checker_ere"; then
        ok "$doc: the documented branch form ('$form' -> '$filled') is one CI can resolve"
    else
        bad "$doc: the documented branch form '$filled' carries no id CI can read"
    fi
}
doc_case CLAUDE.md
doc_case AGENTS.md

# The document has to name the escape for a branch already cut, or the only
# move it leaves is re-cutting the branch and force-pushing.
if grep -qF 'Bead: <bead-id>' "$ROOT/CLAUDE.md"; then
    ok "CLAUDE.md: names the 'Bead:' line that rescues a branch already cut"
else
    bad "CLAUDE.md: no 'Bead: <bead-id>' remedy for a branch already cut"
fi

# --- D. one alphabet, two files ---------------------------------------------
warner_ere="$(sed -n "s/^BEAD_IN_BRANCH_ERE='\(.*\)'\$/\1/p" "$WARNER")"
if [ -z "$warner_ere" ]; then
    bad "warn-missing-bead-id: no BEAD_IN_BRANCH_ERE to compare against the checker"
elif [ "$warner_ere" = "$checker_ere" ]; then
    ok "warner and check-pr-closes.py read the same alphabet ('$warner_ere')"
else
    bad "pattern drift: warner has '$warner_ere', check-pr-closes.py has '$checker_ere'"
fi

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
