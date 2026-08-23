#!/usr/bin/env bash
# Unit tests for the two server-side gates added by bd gqlc-gy3q and
# bd gqlc-89vw:
#
#   .github/scripts/check-pr-authors.sh    — implausible author/committer
#                                            identity, and AI-attribution
#                                            trailers, over a PR's commits
#   .github/scripts/check-label-lengths.py — bead labels GitHub cannot mirror
#
# Every row asserts the REASON as well as the verdict where a reason exists:
# both checkers have more than one path to a refusal, so a row that only reads
# the exit status passes when the wrong arm fired.
#
# Run: bash .githooks/tests/ci-identity-gate-test.sh
set -u

# When run under a git hook (pre-push via `just test`), GIT_DIR and friends
# leak in and redirect git calls to the parent repo. Isolate.
unset "${!GIT_@}"

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
AUTHORS="$ROOT/.github/scripts/check-pr-authors.sh"
LABELS="$ROOT/.github/scripts/check-label-lengths.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0

ok() { pass=$((pass + 1)); printf 'ok   - %s\n' "$1"; }
no() {
    fail=$((fail + 1))
    printf 'FAIL - %s\n' "$1"
    [ -n "${2:-}" ] && printf '       %s\n' "$2"
}

# ---------------------------------------------------------------------------
# check-pr-authors.sh
# ---------------------------------------------------------------------------

# commit_json <author-email> <committer-email> [message] [sha]
commit_json() {
    local ae="$1" ce="$2" msg="${3:-a subject line}" sha="${4:-0123456789abcdef0123456789abcdef01234567}"
    python3 - "$ae" "$ce" "$msg" "$sha" <<'PY'
import json, sys
ae, ce, msg, sha = sys.argv[1:5]
print(json.dumps({
    "sha": sha,
    "commit": {
        "author": {"name": "A Person", "email": ae},
        "committer": {"name": "A Person", "email": ce},
        "message": msg,
    },
}))
PY
}

# run_authors_case <name> <expected accept|reject> <ndjson-content> [expected-stderr-substring]
run_authors_case() {
    local name="$1" expected="$2" content="$3" want="${4:-}"
    local file="$TMP/commits.ndjson"
    printf '%s' "$content" >"$file"

    local out decision
    if out="$("$AUTHORS" "$file" 2>&1)"; then decision=accept; else decision=reject; fi

    if [ "$decision" != "$expected" ]; then
        no "$name" "expected $expected, got $decision: $out"
        return
    fi
    if [ -n "$want" ] && [[ "$out" != *"$want"* ]]; then
        no "$name" "expected output to mention '$want', got: $out"
        return
    fi
    ok "$name"
}

clean="$(commit_json "jane@doe.dev" "jane@doe.dev")"
NL=$'\n'

run_authors_case "clean commit accepted" accept "$clean" "checked 1 commit"

run_authors_case "two clean commits accepted" accept \
    "$clean$NL$(commit_json "ops@corp.io" "ops@corp.io" "second" "aaaaaaaabbbbbbbbccccccccddddddddeeeeeeee")" \
    "checked 2 commit"

run_authors_case "fixture author rejected" reject \
    "$(commit_json "fixture@example.invalid" "jane@doe.dev")" \
    "implausible author identity"

run_authors_case "fixture committer rejected" reject \
    "$(commit_json "jane@doe.dev" "fixture@example.invalid")" \
    "implausible committer identity"

# The four normalisation walk-arounds the shared predicate closes. These rows
# are here to witness that this checker uses that predicate rather than a
# second copy of the rule: a hand-rolled `*@example.invalid` glob passes all
# four.
run_authors_case "author at reserved name, FQDN root dot" reject \
    "$(commit_json "dev@example.invalid." "jane@doe.dev")" "reserved"
run_authors_case "author at reserved name, run of root dots" reject \
    "$(commit_json "dev@example.invalid.." "jane@doe.dev")" "reserved"
run_authors_case "author at localhost, FQDN root dot" reject \
    "$(commit_json "dev@localhost." "jane@doe.dev")" "reserved"
run_authors_case "author at a domain of nothing but dots" reject \
    "$(commit_json "dev@..." "jane@doe.dev")" "not of the form"
run_authors_case "uppercased fixture address" reject \
    "$(commit_json "FIXTURE@EXAMPLE.INVALID" "jane@doe.dev")" "reserved"
run_authors_case "subdomain of a reserved name" reject \
    "$(commit_json "a@mail.example.invalid" "jane@doe.dev")" "reserved"
run_authors_case "empty author address" reject \
    "$(commit_json "" "jane@doe.dev")" "not of the form"

# Falsifiers: a real domain that merely CONTAINS a reserved name. A gate that
# over-matches here locks every contributor out of every PR.
run_authors_case "domain containing 'invalid' accepted" accept \
    "$(commit_json "ops@invalid-arguments.io" "ops@invalid-arguments.io")"
run_authors_case "domain containing 'example' accepted" accept \
    "$(commit_json "ops@example-corp.io" "ops@example-corp.io")"
run_authors_case "citizen address accepted" accept \
    "$(commit_json "antranig.yeretzian@proton.me" "antranig.yeretzian@proton.me")"

# AI-attribution trailers, the rule CLAUDE.md states and the one --no-verify
# is most likely to be used to walk past.
run_authors_case "Co-Authored-By Claude rejected" reject \
    "$(commit_json "jane@doe.dev" "jane@doe.dev" \
        "subject

Co-Authored-By: Claude <noreply@anthropic.com>")" \
    "attributes this commit to Claude"
run_authors_case "co-authored-by lowercase key rejected" reject \
    "$(commit_json "jane@doe.dev" "jane@doe.dev" \
        "subject

co-authored-by: Claude Opus <x@example-corp.io>")" \
    "attributes this commit to Claude"
run_authors_case "anthropic.com address in trailer rejected" reject \
    "$(commit_json "jane@doe.dev" "jane@doe.dev" \
        "subject

Co-Authored-By: Someone <someone@anthropic.com>")" \
    "attributes this commit to Claude"
run_authors_case "human co-author accepted" accept \
    "$(commit_json "jane@doe.dev" "jane@doe.dev" \
        "subject

Co-Authored-By: Sam Human <sam@corp.io>")"
run_authors_case "the word claude in the body, not a trailer" accept \
    "$(commit_json "jane@doe.dev" "jane@doe.dev" \
        "subject

This paragraph mentions claude and anthropic.com in prose.")"

# Fail-closed arms. A gate that examined nothing must not report a pass; this
# repo has shipped the opposite shape (a detector exiting 0 on its own
# condition, and check-hooks warning while `just doctor` printed ok).
run_authors_case "empty commit list refused" reject "" "examined no commits"
run_authors_case "unparseable line refused" reject \
    "not json at all
" "not a JSON object"

if out="$("$AUTHORS" "$TMP/does-not-exist.ndjson" 2>&1)"; then
    no "missing commit list refused" "accepted: $out"
elif [[ "$out" != *"no commit list at"* ]]; then
    no "missing commit list refused" "wrong reason: $out"
else
    ok "missing commit list refused"
fi

if "$AUTHORS" >/dev/null 2>&1; then
    no "no argument refused"
else
    ok "no argument refused"
fi

# ---------------------------------------------------------------------------
# check-label-lengths.py
# ---------------------------------------------------------------------------

# run_labels_case <name> <expected accept|reject> <jsonl-content> [substring]
run_labels_case() {
    local name="$1" expected="$2" content="$3" want="${4:-}"
    local file="$TMP/issues.jsonl"
    printf '%s' "$content" >"$file"

    local out decision
    if out="$(python3 "$LABELS" "$file" 2>&1)"; then decision=accept; else decision=reject; fi

    if [ "$decision" != "$expected" ]; then
        no "$name" "expected $expected, got $decision: $out"
        return
    fi
    if [ -n "$want" ] && [[ "$out" != *"$want"* ]]; then
        no "$name" "expected output to mention '$want', got: $out"
        return
    fi
    ok "$name"
}

bead_json() {
    python3 - "$@" <<'PY'
import json, sys
print(json.dumps({"id": sys.argv[1], "labels": sys.argv[2:]}))
PY
}

# 42 characters, the longest label on the board today.
fits="subject:.githooks/tests/commit-msg-test.sh"
# The exact string GitHub refused, 51 characters.
overlong="subject:kingdom/brain/playbooks/citizen-protocol.md"

run_labels_case "labels within the cap accepted" accept \
    "$(bead_json gqlc-aaaa "class:warrior" "$fits")" "checked 2 label"

run_labels_case "the label GitHub actually refused is refused here" reject \
    "$(bead_json gqlc-xiri "$overlong")" "exceed GitHub's 50-character cap"

run_labels_case "the offending bead and label are named" reject \
    "$(bead_json gqlc-xiri "$overlong")" "gqlc-xiri"

run_labels_case "the remedy names the deepest ancestor that fits" reject \
    "$(bead_json gqlc-xiri "$overlong")" "subject:kingdom/brain/playbooks"

# Boundary. 50 passes, 51 fails; an off-by-one here is the whole defect.
fifty="$(printf 'subject:%s' "$(head -c 42 /dev/zero | tr '\0' 'a')")"
run_labels_case "exactly 50 characters accepted" accept "$(bead_json gqlc-aaaa "$fifty")"
run_labels_case "exactly 51 characters refused" reject "$(bead_json gqlc-aaaa "${fifty}a")" \
    "51 chars"

run_labels_case "a non-subject label is held to the same cap" reject \
    "$(bead_json gqlc-aaaa "$(printf 'class:%s' "$(head -c 45 /dev/zero | tr '\0' 'w')")")" \
    "exceed GitHub's 50-character cap"

run_labels_case "a bead with no labels is fine" accept \
    '{"id":"gqlc-aaaa"}
' "checked 0 label"

run_labels_case "empty export refused" reject "" "examined no labels"
run_labels_case "unparseable export refused" reject "{not json
" "is not JSON"

if out="$(python3 "$LABELS" "$TMP/nope.jsonl" 2>&1)"; then
    no "missing export refused" "accepted: $out"
elif [[ "$out" != *"cannot read"* ]]; then
    no "missing export refused" "wrong reason: $out"
else
    ok "missing export refused"
fi

# The real board. This is the row that makes the gate's verdict on master a
# measured fact rather than an expectation: if any bead on the board already
# carries an unmirrorable label, the gate is red the moment it lands and this
# suite says so here rather than in CI.
if out="$(python3 "$LABELS" "$ROOT/.beads/issues.jsonl" 2>&1)"; then
    ok "the committed board passes the cap ($out)"
else
    no "the committed board passes the cap" "$out"
fi

echo "---"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
