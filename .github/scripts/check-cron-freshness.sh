#!/usr/bin/env bash
# A scheduled workflow that STOPPED FIRING looks exactly like one that passes
# (bd gqlc-zqa3).
#
# GitHub disables cron-triggered workflows after 60 days with no repository
# activity, and it does so REPO-WIDE. So no other cron in this tree can witness
# it: codegen-live.yml's nightly and vuln.yml's weekly sweep go dark together.
# The failure mode is silence, not a red check. codegen-live.yml's nightly-alert
# job opens a tracking issue when the scheduled run FAILS; it cannot see a run
# that never happened.
#
# Only a pull_request-triggered check can observe this, because a
# pull_request trigger is the one a disablement leaves alive. Hence: a step in
# ci.yml's `tidy` job, which is a required status context.
#
# Usage:  check-cron-freshness.sh <workflow-file> <max-age-days> [owner/repo]
#
#   check-cron-freshness.sh codegen-live.yml 14
#
# Needs `gh` authenticated, and the `actions: read` scope on the token — the
# workflow-runs endpoint is under Actions, not Contents.
#
# ------------------------------------------------------------------------
# THIS GATE BLOCKS MERGES, AND THAT WAS A DECISION.
#
# bd gqlc-zqa3 left it open and required whoever took it to record why. The
# alternative was `::warning::` and exit 0, and this repository has repeatedly
# measured that a detector which exits 0 is not a gate — most recently in the
# form of a check-hooks that warned while `just doctor` printed ok.
#
# The hazard with failing closed is real and it is named in bd gqlc-flko: an
# infrastructure state unrelated to the change blocks every pull request, at
# 4am, with nobody awake. What makes that acceptable HERE and not for a 429 is
# the threshold. A 429 is a transient with a duty cycle of seconds; a disabled
# cron is a latched state that only a human running `gh workflow enable` clears.
# A threshold of 14 days against a DAILY cron means thirteen consecutive missed
# nights pass in silence, so nothing transient can reach the refusal — no
# outage, no queue backlog, no runner shortage this repository has seen lasts
# that long. What reaches it is the latched state, and the remedy is one
# command, named in the message.
#
# The reverse cost is bounded too: the detection lag is at most the threshold,
# against a 60-day disablement clock.
#
# ITS OWN FAILURE MODES, which bd gqlc-zqa3 also required be handled:
#   - an API error must NOT read as "fresh". It exits 1 here.
#   - an empty run list must NOT read as "fresh". It exits 1 here, and says
#     so differently from a stale one, because a workflow that has never fired
#     on a schedule is a different defect from one that stopped.
#   - a workflow with no `schedule:` trigger at all must not be silently
#     watched, since it would refuse forever for a reason that is not this one.
set -euo pipefail

usage() {
    echo "usage: $0 <workflow-file> <max-age-days> [owner/repo]" >&2
    exit 2
}

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
    usage
fi
WORKFLOW="$1"
MAX_AGE_DAYS="$2"
REPO="${3:-${GITHUB_REPOSITORY:-}}"

case "$MAX_AGE_DAYS" in
    '' | *[!0-9]*)
        echo "error: max-age-days must be a whole number of days, got '$MAX_AGE_DAYS'." >&2
        exit 2
        ;;
esac

if [ -z "$REPO" ]; then
    echo "error: no repository given and GITHUB_REPOSITORY is unset, so this check has" >&2
    echo "       nothing to query. Failing rather than passing (bd gqlc-zqa3)." >&2
    exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORKFLOW_PATH="$REPO_ROOT/.github/workflows/$WORKFLOW"

if [ ! -f "$WORKFLOW_PATH" ]; then
    echo "error: .github/workflows/$WORKFLOW is not in this tree, so this check is" >&2
    echo "       watching a workflow that no longer exists. Either it was renamed and" >&2
    echo "       the ci.yml step naming it was not, or it is gone and the step should" >&2
    echo "       be too (bd gqlc-zqa3)." >&2
    exit 1
fi

# A workflow with no cron cannot go stale by disablement, and watching one would
# refuse forever for a reason that has nothing to do with this gate.
if ! grep -qE '^[[:space:]]*schedule:[[:space:]]*$' "$WORKFLOW_PATH"; then
    echo "error: .github/workflows/$WORKFLOW declares no 'schedule:' trigger, so it has" >&2
    echo "       no scheduled runs for this check to age and it would refuse on every" >&2
    echo "       pull request forever. If the cron was removed deliberately, remove the" >&2
    echo "       step in ci.yml that names this workflow (bd gqlc-zqa3)." >&2
    exit 1
fi

# Not piped and not `|| true`. Under `set -e` a failed query aborts here; an
# empty response reaching the parser below would read as "no runs", which is
# refused too, but for the wrong reason and with the wrong remedy.
if ! runs_json="$(gh api "repos/$REPO/actions/workflows/$WORKFLOW/runs?event=schedule&per_page=1" 2>&1)"; then
    echo "error: could not read the scheduled runs of $WORKFLOW from $REPO." >&2
    echo "       This check fails closed: an API error must not read as a fresh cron," >&2
    echo "       because 'fresh' is what a disabled workflow would also look like if" >&2
    echo "       silence passed (bd gqlc-zqa3). What gh said:" >&2
    printf '%s\n' "$runs_json" | sed 's/^/         /' >&2
    echo "       If this is a permissions failure, the job needs 'actions: read'." >&2
    exit 1
fi

# python3 rather than jq: this directory already depends on it, and the age
# arithmetic wants a real date parser rather than string surgery.
age_days="$(printf '%s' "$runs_json" | python3 -c '
import json, sys
from datetime import datetime, timezone

try:
    doc = json.load(sys.stdin)
except (json.JSONDecodeError, UnicodeDecodeError) as e:
    print(f"unparseable: {e}", file=sys.stderr)
    sys.exit(3)

runs = doc.get("workflow_runs") or []
if not runs:
    sys.exit(4)

created = runs[0].get("created_at")
if not created:
    print("newest run carries no created_at", file=sys.stderr)
    sys.exit(3)
try:
    when = datetime.fromisoformat(created.replace("Z", "+00:00"))
except ValueError as e:
    print(f"unparseable created_at {created!r}: {e}", file=sys.stderr)
    sys.exit(3)

print(int((datetime.now(timezone.utc) - when).total_seconds() // 86400))
')" && parse_status=0 || parse_status=$?

if [ "$parse_status" -eq 4 ]; then
    echo "error: $WORKFLOW has NEVER run on a schedule in $REPO." >&2
    echo "       That is not the same defect as a cron that stopped: either the" >&2
    echo "       'schedule:' trigger was added and has not fired yet, or the workflow" >&2
    echo "       was disabled before its first scheduled run. Check" >&2
    echo "         gh workflow view $WORKFLOW" >&2
    echo "       and enable it with" >&2
    echo "         gh workflow enable $WORKFLOW" >&2
    echo "       if it is disabled (bd gqlc-zqa3)." >&2
    exit 1
fi
if [ "$parse_status" -ne 0 ]; then
    echo "error: could not read a run date out of the API response for $WORKFLOW, so this" >&2
    echo "       check does not know whether the cron is alive. Failing closed" >&2
    echo "       (bd gqlc-zqa3)." >&2
    exit 1
fi

if [ "$age_days" -gt "$MAX_AGE_DAYS" ]; then
    echo "error: the newest SCHEDULED run of $WORKFLOW is $age_days days old, and the" >&2
    echo "       threshold is $MAX_AGE_DAYS. GitHub disables cron-triggered workflows on a" >&2
    echo "       repository with no activity, repo-wide, so every other cron in this tree" >&2
    echo "       has gone dark with it — silently, because a workflow that never runs" >&2
    echo "       reports no red check." >&2
    echo "       Remedy:" >&2
    echo "         gh workflow enable $WORKFLOW" >&2
    echo "       This check blocks merges on purpose; the threshold is long enough that" >&2
    echo "       no transient outage can reach it (bd gqlc-zqa3)." >&2
    exit 1
fi

echo "[check-cron-freshness] $WORKFLOW last ran on a schedule $age_days day(s) ago (threshold $MAX_AGE_DAYS)"
