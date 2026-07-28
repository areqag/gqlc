#!/usr/bin/env bash
# Tests for the bd-gh-sync pull description-guard (gqlc-so6).
#
# The guard compares local bd descriptions against GH issue bodies and blocks
# the sync when they diverge, to prevent --prefer-github from silently
# reverting bd-only amendments. Tests exercise the Python divergence-detector
# inline, without requiring live bd or gh.
#
# Run via: just test-hooks
set -u

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0

# Run the Python divergence-detector from bd-gh-sync with synthetic inputs.
# $1=name  $2=expected(diverged|clean)  $3=bd_beads JSON  $4=gh_bodies JSON
run_case() {
    local name="$1" expected="$2" bd_beads="$3" gh_bodies="$4"

    local output
    output=$(BD_BEADS="$bd_beads" GH_BODIES="$gh_bodies" python3 - <<'PYEOF'
import json, re, os, sys

try:
    bd_beads = json.loads(os.environ["BD_BEADS"])
except Exception:
    bd_beads = []
try:
    gh_issues = json.loads(os.environ["GH_BODIES"])
except Exception:
    gh_issues = []

gh_body_by_num = {i["number"]: (i.get("body") or "").strip() for i in gh_issues}
pat = re.compile(r"/issues/(\d+)$")

for b in bd_beads:
    if b.get("status") == "closed":
        continue
    ext = b.get("external_ref") or ""
    m = pat.search(ext)
    if not m:
        continue
    n = int(m.group(1))
    local_desc = (b.get("description") or "").strip()
    if not local_desc:
        continue
    remote_body = gh_body_by_num.get(n)
    if remote_body is None:
        continue
    if local_desc != remote_body:
        print(f"{b['id']}:{n}:{len(local_desc)}:{len(remote_body)}")
PYEOF
    )
    local rc=$?

    local got
    if [ $rc -ne 0 ]; then
        got="error"
    elif [ -n "$output" ]; then
        got="diverged"
    else
        got="clean"
    fi

    if [ "$got" = "$expected" ]; then
        pass=$((pass + 1)); printf 'ok   - %s\n' "$name"
    else
        fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s; output=%q)\n' \
            "$name" "$expected" "$got" "$output"
    fi
}

# --- diverged: amendment must be detected -----------------------------------

run_case "bd-only amendment detected" diverged \
    '[{"id":"gqlc-x1","status":"open","external_ref":"https://github.com/org/r/issues/42","description":"amended longer text"}]' \
    '[{"number":42,"body":"original shorter text"}]'

run_case "bd longer than GH (typical amendment shape)" diverged \
    '[{"id":"gqlc-x2","status":"in_progress","external_ref":"https://github.com/org/r/issues/7","description":"extended description with new context added"}]' \
    '[{"number":7,"body":"short"}]'

run_case "GH body longer than bd (GH edited directly)" diverged \
    '[{"id":"gqlc-x3","status":"open","external_ref":"https://github.com/org/r/issues/9","description":"short"}]' \
    '[{"number":9,"body":"longer GH body that differs from bd"}]'

# --- clean: sync must proceed without warning --------------------------------

run_case "descriptions match exactly — clean" clean \
    '[{"id":"gqlc-y1","status":"open","external_ref":"https://github.com/org/r/issues/5","description":"identical text"}]' \
    '[{"number":5,"body":"identical text"}]'

run_case "no external_ref — skip" clean \
    '[{"id":"gqlc-y2","status":"open","description":"something"}]' \
    '[]'

run_case "closed bead — skip even if diverged" clean \
    '[{"id":"gqlc-y3","status":"closed","external_ref":"https://github.com/org/r/issues/11","description":"different text"}]' \
    '[{"number":11,"body":"other text"}]'

run_case "blocked bead — divergence detected (not skipped)" diverged \
    '[{"id":"gqlc-y3b","status":"blocked","external_ref":"https://github.com/org/r/issues/12","description":"amended text blocked bead"}]' \
    '[{"number":12,"body":"original shorter"}]'

run_case "deferred bead — divergence detected (not skipped)" diverged \
    '[{"id":"gqlc-y3c","status":"deferred","external_ref":"https://github.com/org/r/issues/13","description":"deferred bead amendment"}]' \
    '[{"number":13,"body":"original"}]'

run_case "empty local description — skip" clean \
    '[{"id":"gqlc-y4","status":"open","external_ref":"https://github.com/org/r/issues/3","description":""}]' \
    '[{"number":3,"body":"something"}]'

run_case "GH issue not in bulk list — skip safely" clean \
    '[{"id":"gqlc-y5","status":"open","external_ref":"https://github.com/org/r/issues/99","description":"something"}]' \
    '[]'

# --- mutation: guard must be the cause of detection -------------------------
# Same as first case but GH body matches → clean.  Proves the guard fires on
# mismatch, not unconditionally.
run_case "mutation: matching bodies no longer diverge" clean \
    '[{"id":"gqlc-z1","status":"open","external_ref":"https://github.com/org/r/issues/42","description":"amended longer text"}]' \
    '[{"number":42,"body":"amended longer text"}]'

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
