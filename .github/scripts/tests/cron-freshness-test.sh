#!/usr/bin/env bash
# Unit tests for .github/scripts/check-cron-freshness.sh (bd gqlc-zqa3).
#
# The whole point of that checker is that SILENCE must not read as health, so
# every row here asserts the REASON as well as the verdict: the checker has five
# paths to a refusal, and a row reading only the exit status passes when the
# wrong arm fired.
#
# `gh` is stubbed on PATH. The stub is what lets a row put the checker in states
# the live API will not produce on demand — a workflow that has never run on a
# schedule, a run three weeks old, a 403 from a token without actions: read.
#
# Run: bash .github/scripts/tests/cron-freshness-test.sh
set -u

# Under a git hook (pre-push via `just test`) GIT_DIR and friends leak in.
unset "${!GIT_@}"

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
CHECK="$ROOT/.github/scripts/check-cron-freshness.sh"
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

mkdir -p "$TMP/bin"
cat >"$TMP/bin/gh" <<'STUB'
#!/usr/bin/env bash
# Answers the one endpoint the checker calls, from two variables.
if [ "${STUB_GH_STATUS:-0}" != "0" ]; then
    echo "${STUB_GH_BODY:-gh: HTTP 403}" >&2
    exit "${STUB_GH_STATUS}"
fi
printf '%s' "${STUB_GH_BODY:-}"
STUB
chmod +x "$TMP/bin/gh"
PATH="$TMP/bin:$PATH"
export PATH

# iso_days_ago <n> -- an ISO-8601 Z timestamp n days before now.
iso_days_ago() {
    python3 -c '
import sys
from datetime import datetime, timedelta, timezone
n = int(sys.argv[1])
print((datetime.now(timezone.utc) - timedelta(days=n, hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ"))
' "$1"
}

# runs_json <iso-timestamp>  |  runs_json  -- an empty run list
runs_json() {
    if [ "$#" -eq 0 ]; then
        printf '{"total_count":0,"workflow_runs":[]}'
        return
    fi
    printf '{"total_count":1,"workflow_runs":[{"created_at":"%s"}]}' "$1"
}

# run_case <name> <accept|reject> <want-substring> <workflow> <days> [env assignments...]
run_case() {
    local name="$1" expected="$2" want="$3" workflow="$4" days="$5"
    shift 5

    local out decision
    if out="$(env GITHUB_REPOSITORY=areqag/gqlc "$@" "$CHECK" "$workflow" "$days" 2>&1)"; then
        decision=accept
    else
        decision=reject
    fi

    if [ "$decision" != "$expected" ]; then
        no "$name" "expected $expected, got $decision: $out"
        return
    fi
    case "$out" in
        *"$want"*) ok "$name" ;;
        *) no "$name" "verdict $decision was right, but the reason was not '$want': $out" ;;
    esac
}

# ---------------------------------------------------------------------------
# The accepting side. Without it every row below passes against a checker that
# refuses unconditionally.
# ---------------------------------------------------------------------------

run_case "a nightly that ran last night passes" accept "last ran on a schedule" \
    codegen-live.yml 14 "STUB_GH_BODY=$(runs_json "$(iso_days_ago 0)")"

run_case "an age just inside the threshold passes" accept "13 day(s) ago" \
    codegen-live.yml 14 "STUB_GH_BODY=$(runs_json "$(iso_days_ago 13)")"

# ---------------------------------------------------------------------------
# The refusal this gate exists for.
# ---------------------------------------------------------------------------

run_case "an age past the threshold is refused, naming the remedy" reject \
    "gh workflow enable codegen-live.yml" \
    codegen-live.yml 14 "STUB_GH_BODY=$(runs_json "$(iso_days_ago 30)")"

run_case "the refusal says how old the run is" reject "is 30 days old" \
    codegen-live.yml 14 "STUB_GH_BODY=$(runs_json "$(iso_days_ago 30)")"

# One day past is refused: the boundary is `> threshold`, and a row at exactly
# the threshold passing (above) is what makes this row measure the comparison
# rather than the arithmetic.
run_case "one day past the threshold is refused" reject "is 15 days old" \
    codegen-live.yml 14 "STUB_GH_BODY=$(runs_json "$(iso_days_ago 15)")"

# ---------------------------------------------------------------------------
# The checker's OWN failure modes. bd gqlc-zqa3 named these: none of them may
# read as "fresh".
# ---------------------------------------------------------------------------

run_case "an API error does not read as a fresh cron" reject "fails closed" \
    codegen-live.yml 14 "STUB_GH_STATUS=1" "STUB_GH_BODY=gh: Resource not accessible (403)"

run_case "an API error names the permission it probably wants" reject "actions: read" \
    codegen-live.yml 14 "STUB_GH_STATUS=1" "STUB_GH_BODY=gh: Resource not accessible (403)"

run_case "an empty run list does not read as a fresh cron" reject "NEVER run on a schedule" \
    codegen-live.yml 14 "STUB_GH_BODY=$(runs_json)"

run_case "unparseable JSON does not read as a fresh cron" reject "does not know whether" \
    codegen-live.yml 14 "STUB_GH_BODY=not json at all"

run_case "a run with no created_at does not read as a fresh cron" reject \
    "does not know whether" \
    codegen-live.yml 14 'STUB_GH_BODY={"total_count":1,"workflow_runs":[{}]}'

run_case "an unparseable created_at does not read as a fresh cron" reject \
    "does not know whether" \
    codegen-live.yml 14 'STUB_GH_BODY={"total_count":1,"workflow_runs":[{"created_at":"soon"}]}'

# ---------------------------------------------------------------------------
# Arguments and subject.
# ---------------------------------------------------------------------------

run_case "a workflow that is not in the tree is refused" reject "is not in this tree" \
    no-such-workflow.yml 14 "STUB_GH_BODY=$(runs_json "$(iso_days_ago 0)")"

# ci.yml has no cron. Watching it would refuse on every PR for a reason that is
# not a disabled cron, so the checker says which mistake was made.
run_case "a workflow with no schedule: trigger is refused as a wiring error" reject \
    "declares no 'schedule:' trigger" \
    ci.yml 14 "STUB_GH_BODY=$(runs_json "$(iso_days_ago 0)")"

run_case "a non-numeric threshold is a usage error" reject "whole number of days" \
    codegen-live.yml fourteen "STUB_GH_BODY=$(runs_json "$(iso_days_ago 0)")"

# GITHUB_REPOSITORY unset and no third argument: nothing to query, and an
# unqueried check must not pass.
if out="$(env -u GITHUB_REPOSITORY "STUB_GH_BODY=$(runs_json "$(iso_days_ago 0)")" \
    "$CHECK" codegen-live.yml 14 2>&1)"; then
    no "no repository is refused" "it passed: $out"
else
    case "$out" in
        *"GITHUB_REPOSITORY is unset"*) ok "no repository is refused" ;;
        *) no "no repository is refused" "wrong reason: $out" ;;
    esac
fi

# ---------------------------------------------------------------------------
# The step in ci.yml has to exist, in the required `tidy` job, or none of the
# above is reached by anything.
# ---------------------------------------------------------------------------

if python3 - "$ROOT/.github/workflows/ci.yml" <<'PY'
import sys
import re

src = open(sys.argv[1], encoding="utf-8").read()
# The `tidy:` job's block: from its key to the next job key at the same indent.
m = re.search(r"^  tidy:\n(.*?)(?=^  \S)", src, re.MULTILINE | re.DOTALL)
if not m:
    print("ci.yml has no tidy: job", file=sys.stderr)
    sys.exit(1)
body = m.group(1)
if "check-cron-freshness.sh" not in body:
    print("the tidy job does not run check-cron-freshness.sh", file=sys.stderr)
    sys.exit(1)
if not re.search(r"^\s*actions:\s*read\s*$", body, re.MULTILINE):
    print("the tidy job does not grant actions: read, so the query has no scope",
          file=sys.stderr)
    sys.exit(1)
PY
then
    ok "ci.yml's tidy job runs the checker and can read Actions"
else
    no "ci.yml's tidy job runs the checker and can read Actions" \
        "a checker nothing calls is not a gate (bd gqlc-zqa3)"
fi

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
