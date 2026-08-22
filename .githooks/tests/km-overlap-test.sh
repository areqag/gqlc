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

# No row here invokes git today, so this scrubs nothing today. It is here
# because the FIRST git call added to this suite months from now would inherit
# git's exported environment when the suite runs from a hook, and follow GIT_DIR
# out of its fixture and into the real repo — green on every direct run, red
# only under `git push` (gqlc-tl78, measured on PR #1128). The glob form takes
# GIT_CONFIG_GLOBAL and whatever git exports next; a list written today does not.
unset "${!GIT_@}"

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
# changedFiles is the PR's true file count and is carried on every record: gh
# truncates the files array at 100 and says nothing, so the two disagreeing is
# the only in-band signal that a record is short.
CORPUS='[
  {"number":1213,"title":"a sha is a sha in any case","changedFiles":2,"files":[
     {"path":".githooks/claude-pre-bash"},
     {"path":".githooks/tests/claude-pre-bash-test.sh"}]},
  {"number":1181,"title":"the drift message named a value","changedFiles":2,"files":[
     {"path":".githooks/claude-pre-bash"},
     {"path":".githooks/tests/claude-pre-bash-test.sh"}]},
  {"number":1177,"title":"the drift detector read a value git would not use","changedFiles":1,"files":[
     {"path":".githooks/claude-pre-bash"}]},
  {"number":1195,"title":"stop a fixture identity reaching the shared repo","changedFiles":2,"files":[
     {"path":"justfile"},
     {"path":"kingdom/justfile.md"}]},
  {"number":1225,"title":"fail a test run that changed its repository","changedFiles":1,"files":[
     {"path":"justfile"}]},
  {"number":1069,"title":"corpus subtest silence","changedFiles":1,"files":[
     {"path":"internal/codegen/corpus.go"}]},
  {"number":1300,"title":"touches only the longer path","changedFiles":1,"files":[
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
#
# Each of these rows names the REASON, not just the refusal. Measured: with the
# usability guard struck, this record still exits 2 — the truncation guard
# catches it and reports "a truncated file list for #1213 (0 of 1)", because an
# absent files key has length 0 and 0 < 1. Two paths to one exit status, so a
# row asserting only rc 2 and the PR number stayed green with the guard it
# exists for deleted, and the surviving message named the wrong cause.
GH_NOFILES="$(make_gh '[{"number":1213,"title":"t","changedFiles":1}]' 0)"
run_overlap "$GH_NOFILES" path justfile
expect "a record with no files key is a refusal" 2 "1213" "no usable file list"

# MEASURED 2026-08-22 against kubernetes/kubernetes#141360: `gh pr list --json
# files` returned 100 paths for a PR whose changed_files is 322, and `gh pr view`
# returned the same 100. gh truncates a record's file list at 100 and gives no
# signal. Unguarded, the 101st file of a large PR is invisible to the census, and
# `path <that file>` answers "no open PR touches this" — the fail-open this whole
# tool refuses. changedFiles is the true count, so the two disagreeing detects it
# exactly, with no threshold to guess at.
GH_TRUNC="$(make_gh '[{"number":1360,"title":"a large PR","changedFiles":322,"files":[
    {"path":"justfile"},{"path":"a"},{"path":"b"}]}]' 0)"
run_overlap "$GH_TRUNC" path justfile
expect "a record whose file list is shorter than its true count is a refusal" 2 "1360" "truncated"

# The overlap is real and reported; the refusal is about the files NOT returned.
run_overlap "$GH_TRUNC" path some/file/not/listed
expect "a truncated record refuses even when the query would have missed anyway" 2 "1360" "truncated"

# The guard must not fire on the ordinary case, or the tool refuses every run.
GH_EXACT="$(make_gh '[{"number":1361,"title":"an ordinary PR","changedFiles":2,"files":[
    {"path":"justfile"},{"path":"README.md"}]}]' 0)"
run_overlap "$GH_EXACT" path justfile
expect "a record whose file list matches its true count is trusted" 1 "#1361"

# Absent, the field cannot witness anything, and a query that did not return it
# is not a PR that changed nothing.
GH_NOCOUNT="$(make_gh '[{"number":1362,"title":"no count","files":[{"path":"justfile"}]}]' 0)"
run_overlap "$GH_NOCOUNT" path justfile
expect "a record with no changedFiles is a refusal" 2 "1362" "no usable file list"

# A key that is PRESENT and null passes has(), and jq sorts null below every
# number, so `(.files|length) < .changedFiles` is false against a null count:
# such a record walked both guards and was reported as trusted. Absence and
# null are different shapes and only a type check refuses both.
GH_NULLCOUNT="$(make_gh '[{"number":1213,"title":"t","changedFiles":null,"files":[
    {"path":"justfile"}]}]' 0)"
run_overlap "$GH_NULLCOUNT" path justfile
expect "a null changedFiles is a refusal, not a trusted count" 2 "1213" "no usable file list"

# The reason matters here too: with the type guard reverted to has(), this
# record's query jq dies on the null and the query-status guard refuses it — the right
# exit status by way of the wrong guard, and a row pinned only to rc 2 would
# have called that a pass.
GH_NULLFILES="$(make_gh '[{"number":1213,"title":"t","changedFiles":null,"files":null}]' 0)"
run_overlap "$GH_NULLFILES" path justfile
expect "a null files list is a refusal, not an empty one" 2 "1213" "no usable file list"

# The three query pipelines are the second half of the same hole. This record
# passes every guard fetch can make — files IS an array, changedFiles IS a
# number that agrees with its length — and still kills the query jq, which used
# to return empty output with nobody reading its status, so `[ -z "$hits" ]`
# converted the death into "no open PR touches justfile" at rc 0. That is the
# exact answer this tool exists to never give.
#
# One row per command on purpose: each entry point has its own pipeline and its
# own status check, so a shared row would leave two of the three deletable with
# the suite still green.
GH_BADFILES="$(make_gh '[{"number":1213,"title":"t","changedFiles":1,"files":["justfile"]}]' 0)"
run_overlap "$GH_BADFILES" path justfile
expect "a path query that dies is a refusal, not an all-clear" 2
expect_absent "a dead path query never prints the all-clear" 2 "no open PR touches"

run_overlap "$GH_BADFILES" pr 1213
expect "a sibling query that dies is a refusal, not an all-clear" 2
expect_absent "a dead sibling query never prints the all-clear" 2 "shares no file"

run_overlap "$GH_BADFILES" census
expect "a census query that dies is a refusal, not an all-clear" 2
expect_absent "a dead census query never prints the all-clear" 2 "no file is touched"

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

# Presence of the flag is not the value of the flag. Hardcoding `-L 200` left
# every row above green: the grep is satisfied by any constant containing -L,
# and the --limit rows exercise the GUARD (n >= LIMIT), never the transmission,
# because the stub ignores its argv. The fail-open is reachable by following
# this tool's own printed remedy — past the hardcoded number, "raise --limit"
# raises the threshold while gh stays pinned, so the census under-reports with
# the at-limit refusal disarmed. The `--limit 7` run above is what put this in
# argv; only argv witnesses the ask.
if grep -q -- '-L 7' "$(dirname "$GH_OK")/argv" 2>/dev/null; then
    ok "the limit's VALUE reaches gh, not just the flag"
else
    bad "the limit's VALUE reaches gh, not just the flag" \
        "no '-L 7' in argv: $(cat "$(dirname "$GH_OK")/argv" 2>/dev/null)"
fi

# The truncation guard compares files against changedFiles, so it is inert
# unless changedFiles was asked for. Every stub above prints changedFiles no
# matter what argv it got, which means the whole truncation half stays green
# with the field dropped from the request — against real gh the record would
# simply arrive without a count and the guard would compare against null. Only
# argv can witness the ask.
if grep -q -- 'changedFiles' "$(dirname "$GH_OK")/argv" 2>/dev/null; then
    ok "changedFiles is requested from gh, so the truncation guard has a count"
else
    bad "changedFiles is requested from gh, so the truncation guard has a count" \
        "no changedFiles in argv: $(cat "$(dirname "$GH_OK")/argv" 2>/dev/null)"
fi

# Same blindness, milder consequence: every stub prints a title whatever it was
# asked for, so dropping `title` from the --json ask left the whole suite green
# while real gh would return records without one and every answer would name the
# PRs as "null". Cosmetic rather than fail-open — and one grep away from being
# witnessed, so it is witnessed.
if grep -q -- 'title' "$(dirname "$GH_OK")/argv" 2>/dev/null; then
    ok "title is requested from gh, so answers can name the PRs"
else
    bad "title is requested from gh, so answers can name the PRs" \
        "no title in argv: $(cat "$(dirname "$GH_OK")/argv" 2>/dev/null)"
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
#
# gh's check order is: credential present -> parse --json field names -> resolve
# a repo -> network. Only the first two are reached here. That the network is
# never reached is measured, not reasoned: this probe returns the field list
# unchanged under `unshare -rn`, with no network interface to use, while
# `gh api rate_limit` under the same conditions fails to connect.
#
# The credential check comes FIRST, and it is satisfied by any non-empty value —
# gh rejects the bogus field name long before it would try to authenticate with
# it. An earlier version of this row omitted the token and passed on my machine
# for the wrong reason: my own `gh auth` credential satisfied step one, so I
# recorded "no auth needed" as measured. On a GitHub Actions runner, which has
# no credential unless the job is given one, gh stopped at step one and all four
# assertions failed. So the token is supplied here rather than inherited, and
# GH_CONFIG_DIR is redirected at a scratch path, which makes the row read the
# same on a runner as on a seat: it can neither use a developer's real
# credential nor reach the network with one.
if command -v gh >/dev/null 2>&1; then
    FIELDS="$(cd "$TMP" || exit 1
        GH_TOKEN=x GITHUB_TOKEN=x GH_CONFIG_DIR="$TMP/gh-config" \
            gh pr list --json __km_overlap_bogus__ 2>&1)"
    # Exactly the four fields the tool's --json asks for, and no others: `state`
    # was checked here and is never requested (--state is a flag, not a field),
    # while `title` is requested and was not checked — a rename of it upstream
    # would have degraded every row's output to "null" unwitnessed.
    for want in number title files changedFiles; do
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
