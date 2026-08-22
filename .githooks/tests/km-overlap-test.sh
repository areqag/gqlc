#!/usr/bin/env bash
# Tests for kingdom/bin/km-overlap (bd gqlc-zgka).
#
# km-overlap answers one question from two sides: "who else has an open PR
# touching this file?". The mayor asks it of a PATH before any branch exists,
# to decide whether a bead is routable; an author asks it of their own PR once
# one does. Both read the same index, which is why they are one tool.
#
# The property that matters is not "does it find the overlap" — a wrong answer
# there costs a conflict. It is that the tool NEVER reports "no overlap" when it
# has not actually looked. Every way the query can fail (gh absent, gh
# non-zero, unparseable JSON, a result that may be truncated, a record with no
# files key) is a row below, and each asserts exit 2 rather than exit 0,
# because a silent 0 is indistinguishable from a real all-clear and is the
# answer a caller acts on. The exit codes are a tri-state on purpose:
#
#     0  looked, found nothing        1  looked, found overlap        2  could not look
#
# gh is injected through KM_GH so these rows are hermetic. A stub encodes what
# I believe gh's contract is, so one row at the bottom runs the REAL gh and
# holds the field names the stub is written against — it needs no network, no
# auth and no repo, because gh validates --json field names locally.
#
# There is deliberately NO suite-size pin here. The bead this suite belongs to
# measured that a row-count constant turns "two citizens extended the same
# suite" from a merge git can do into a hand resolution; adding one to a brand
# new file would mint the next collision.
#
# Run via: just test-hooks
set -u

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT="$REPO/kingdom/bin/km-overlap"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { pass=$((pass + 1)); printf 'ok   - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s: %s\n' "$1" "$2"; }

# The corpus every hermetic row reads. It mirrors the real shape measured on
# 2026-08-22: a file touched by three PRs, one touched by two, one touched by
# exactly one, and a path that is a prefix of another path so that a substring
# match cannot pass for a path match.
CORPUS='[
  {"number":1213,"title":"a sha is a sha in any case","files":[
     {"path":".githooks/claude-pre-bash"},
     {"path":".githooks/tests/claude-pre-bash-test.sh"}]},
  {"number":1181,"title":"the drift message named a value","files":[
     {"path":".githooks/claude-pre-bash"},
     {"path":".githooks/tests/claude-pre-bash-test.sh"}]},
  {"number":1177,"title":"the drift detector read a value git would not use","files":[
     {"path":".githooks/claude-pre-bash"}]},
  {"number":1195,"title":"stop a fixture identity reaching the shared repo","files":[
     {"path":"justfile"},
     {"path":"kingdom/justfile.md"}]},
  {"number":1225,"title":"fail a test run that changed its repository","files":[
     {"path":"justfile"}]},
  {"number":1069,"title":"corpus subtest silence","files":[
     {"path":"internal/codegen/corpus.go"}]},
  {"number":1300,"title":"touches only the longer path","files":[
     {"path":"kingdom/justfile.md"}]}
]'

# Writes a fake gh onto PATH that prints $1 and exits $2, recording its argv in
# <dir>/argv so a row can assert what the tool actually asked for.
# Echoes the path of the fake.
make_gh() {
    local dir payload rc
    dir="$TMP/gh.$RANDOM.$RANDOM"
    mkdir -p "$dir"
    payload="$dir/payload.json"
    printf '%s' "$1" >"$payload"
    rc="$2"
    cat >"$dir/gh" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"$dir/argv"
cat "$payload"
exit $rc
EOF
    chmod +x "$dir/gh"
    printf '%s' "$dir/gh"
}

OUT=""
RC=0
# $1 = path to the gh to inject (or the string "none" to inject nothing), rest
# = arguments to km-overlap.
run_overlap() {
    local gh="$1"
    shift
    if [ "$gh" = "none" ]; then
        OUT="$(KM_GH="$TMP/definitely-not-here" bash "$SCRIPT" "$@" 2>&1)"
    else
        OUT="$(KM_GH="$gh" bash "$SCRIPT" "$@" 2>&1)"
    fi
    RC=$?
}

# $1 = case name, $2 = expected rc, $3... = substrings required in the output.
expect() {
    local name="$1" want="$2"
    shift 2
    if [ "$RC" -ne "$want" ]; then
        bad "$name" "expected rc $want, got $RC; output: $OUT"
        return
    fi
    local needle
    for needle in "$@"; do
        case "$OUT" in
            *"$needle"*) ;;
            *) bad "$name" "output did not mention '$needle'; output: $OUT"; return ;;
        esac
    done
    ok "$name"
}

# $1 = case name, $2 = expected rc, $3 = substring that must NOT appear.
# The rc is required as well: an absence assertion is satisfied by any output
# that does not contain the needle, including a crash. Written without it,
# every row below passed against a km-overlap that did not exist yet.
expect_absent() {
    if [ "$RC" -ne "$2" ]; then
        bad "$1" "expected rc $2, got $RC; output: $OUT"
        return
    fi
    case "$OUT" in
        *"$3"*) bad "$1" "output mentioned '$3' and should not have; output: $OUT" ;;
        *) ok "$1" ;;
    esac
}

GH_OK="$(make_gh "$CORPUS" 0)"

# ---------------------------------------------------------------- path queries
# The routing question. One other PR is as much a hold as eight, so the
# single-toucher case is its own row: a threshold of "more than one" belongs to
# the census and would silently make this entry point useless.
run_overlap "$GH_OK" path .githooks/claude-pre-bash
expect "path: names every open PR touching the file" 1 "#1213" "#1181" "#1177"

run_overlap "$GH_OK" path internal/codegen/corpus.go
expect "path: a single toucher is still an overlap" 1 "#1069"

run_overlap "$GH_OK" path internal/schema/gql/listener.go
expect "path: an untouched file is a clean answer" 0 "no open PR"

# justfile and kingdom/justfile.md are both in the corpus and one contains the
# other. A substring match would report #1195 twice and invent an overlap on a
# file only one PR touches.
run_overlap "$GH_OK" path kingdom/justfile.md
expect "path: matching is by whole path, not substring" 1 "#1195"
expect_absent "path: a prefix match does not drag in the longer path" 1 "#1225"

run_overlap "$GH_OK" path justfile
expect "path: the shorter path is not matched by the longer one" 1 "#1195" "#1225"
# #1300 touches kingdom/justfile.md and nothing else. Only a substring match
# drags it into an answer about `justfile`, and only a corpus where some PR
# touches the longer path ALONE can tell the two matchers apart: with #1195
# touching both, a substring match returned the right set by luck.
expect_absent "path: a PR touching only the longer path is not an answer about the shorter" 1 "#1300"

# ------------------------------------------------------------------ pr queries
# The author's question. Answered from the same index, so it must not report
# the PR as sharing a file with itself.
run_overlap "$GH_OK" pr 1213
expect "pr: names the siblings on each shared file" 1 \
    ".githooks/claude-pre-bash" "#1181" "#1177"
expect_absent "pr: a PR does not share a file with itself" 1 "#1213 #1213"

run_overlap "$GH_OK" pr 1069
expect "pr: a PR touching only its own file is clean" 0 "no file"

# A number that is not in the open set cannot be answered. Reporting it as
# clean would tell an author their branch is safe on the strength of a typo.
run_overlap "$GH_OK" pr 9999
expect "pr: an unknown number is refused, not called clean" 2 "9999"

# ------------------------------------------------------------------- census
run_overlap "$GH_OK" census
expect "census: reports files touched by more than one PR" 1 \
    ".githooks/claude-pre-bash" "justfile"
expect_absent "census: a file with one toucher is not a collision" 1 "internal/codegen/corpus.go"

# ------------------------------------------------ fail-closed: it never guesses
# Each of these is a way to learn nothing. All of them must be distinguishable
# from "I looked and the file is free".
run_overlap none path justfile
expect "no gh on PATH is a refusal, not an all-clear" 2 "gh"

GH_RC1="$(make_gh "$CORPUS" 1)"
run_overlap "$GH_RC1" path justfile
expect "a non-zero gh is a refusal even when it printed a usable body" 2 "gh"

GH_JUNK="$(make_gh 'not json at all' 0)"
run_overlap "$GH_JUNK" path justfile
expect "unparseable output is a refusal" 2

# The array check is the only guard that sees this: it parses, so jq succeeds,
# and it is non-empty, so the emptiness check passes. gh prints an object like
# this when it has something to say other than the list that was asked for.
GH_OBJ="$(make_gh '{"message":"Not Found"}' 0)"
run_overlap "$GH_OBJ" path justfile
expect "valid JSON that is not the requested array is a refusal" 2

GH_EMPTY="$(make_gh '' 0)"
run_overlap "$GH_EMPTY" path justfile
expect "an empty body is a refusal" 2

# A record with no files key is not a PR that touches nothing — it is a query
# that did not return what was asked for. Treating it as touching nothing is
# how a real overlap goes unreported.
GH_NOFILES="$(make_gh '[{"number":1213,"title":"t"}]' 0)"
run_overlap "$GH_NOFILES" path justfile
expect "a record with no files key is a refusal" 2 "1213"

# gh pr list caps at 30 by default and says nothing when it truncates. The tool
# asks for an explicit limit; if the answer comes back AT that limit it may
# have been cut, and a cut census under-reports overlap silently.
run_overlap "$GH_OK" --limit 7 census
expect "a result at the requested limit may be truncated, so it is refused" 2 "limit"

run_overlap "$GH_OK" --limit 8 census
expect "a result below the limit is trusted" 1

# An explicit limit must actually reach gh; without it the default 30 applies
# and the tool would be silently blind past the 30th open PR.
if grep -q -- '-L' "$(dirname "$GH_OK")/argv" 2>/dev/null; then
    ok "the limit is passed to gh, not left to its default"
else
    bad "the limit is passed to gh, not left to its default" \
        "no -L in argv: $(cat "$(dirname "$GH_OK")/argv" 2>/dev/null)"
fi

# --------------------------------------------------------------------- usage
run_overlap "$GH_OK"
expect "no subcommand is a usage error, not a census" 2 "usage"

run_overlap "$GH_OK" wat
expect "an unknown subcommand is refused" 2 "usage"

run_overlap "$GH_OK" path
expect "path with no argument is refused" 2 "usage"

# Two paths reach exit 2 here: the argument guard, and jq's --argjson failing on
# a non-number further down. Asserting only the status let the guard be deleted
# with the row still green, so the row names the reason it expects.
run_overlap "$GH_OK" pr notanumber
expect "pr with a non-numeric argument is refused by the argument guard" 2 "is not a PR number"

# ------------------------------------------------------- the real gh boundary
# Everything above believes the stub. This row asks the actual gh binary what
# --json accepts, so that a rename of `files` or `number` upstream reddens the
# suite instead of leaving the stub agreeing with a tool that has moved.
# `gh pr list --json <bogus>` prints the valid field list and fails locally: no
# network, no auth, no repo.
if command -v gh >/dev/null 2>&1; then
    # Run outside the repo: gh validates --json field names before it needs a
    # repo, a remote or a token, so this row costs no network and cannot flake.
    FIELDS="$(cd "$TMP" || exit 1; gh pr list --json __km_overlap_bogus__ 2>&1)"
    for want in number files state; do
        case "$FIELDS" in
            *"$want"*) ok "real gh still offers the '$want' field the stub is written against" ;;
            *) bad "real gh still offers the '$want' field the stub is written against" \
                   "not in gh's field list: $FIELDS" ;;
        esac
    done
else
    bad "real gh is available to check the stub against" \
        "gh not on PATH; the stub in this suite is unwitnessed"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
