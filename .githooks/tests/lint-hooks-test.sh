#!/usr/bin/env bash
# Tests for the `lint-hooks` recipe (gqlc-jhi2).
#
# The recipe is the only thing standing between `.githooks/` and the SC-class
# defects this repo has already shipped three of. A linter wired in wrong is
# worse than none, because the `# shellcheck disable=` directives in the tree
# then read as enforced when they are comments — so what is pinned here is the
# recipe's behaviour rather than the linter's: which files it selects, that it
# refuses to report success over an empty selection, and that a sanctioned
# disable stays green while an unsanctioned violation goes red naming its line.
#
# Run via: just test-hooks
set -u

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { pass=$((pass + 1)); printf 'ok   - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s: %s\n' "$1" "$2"; }

# Leaves the recipe's combined output in $OUT and its status in $RC. Runs from
# the repo root because that is where the justfile lives, and hands the recipe
# an absolute directory so the tree under test is never the real one by accident.
OUT=""
RC=0
run_lint() {
    OUT="$(cd "$REPO" && just lint-hooks "$1" 2>&1)"
    RC=$?
}

# $1=tree name; echoes the absolute path of a fresh directory under $TMP.
tree() { mkdir -p "$TMP/$1"; printf '%s' "$TMP/$1"; }

CLEAN='#!/usr/bin/env bash
echo "nothing to see"
'
# An unquoted expansion of a value the linter cannot prove is word-split-safe.
# A literal assignment is not enough: its value is tracked and the rule stays
# quiet. The two fixtures are source text for the tree under test, so their
# expansions must survive this file unevaluated — SC2016 taken per fixture.
# shellcheck disable=SC2016 # fixture source, not this file's code
DIRTY='#!/usr/bin/env bash
v="${SOME_ENV:-}"
: $v
'
# shellcheck disable=SC2016 # ditto
SANCTIONED='#!/usr/bin/env bash
v="${SOME_ENV:-}"
# shellcheck disable=SC2086 # deliberate split, exactly as .githooks/bd-gh-sync:510
: $v
'

# --- the gate must go red on a real violation, naming file and line ----------

d="$(tree dirty)"
printf '%s' "$DIRTY" >"$d/hook"
run_lint "$d"
if [ "$RC" -eq 0 ]; then
    bad "an SC2086 violation reddens the gate" "exited 0: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'SC2086'; then
    bad "an SC2086 violation reddens the gate" "did not name the rule: $OUT"
elif ! printf '%s' "$OUT" | grep -q "$d/hook line 3:"; then
    bad "an SC2086 violation reddens the gate" \
        "did not name the file and line: $OUT"
else
    ok "an SC2086 violation reddens the gate, naming the file and the line"
fi

# --- ...and stay green on the sanctioned one ---------------------------------
# The whole point of a `# shellcheck disable=` directive. A linter that fails on
# the exception it documents is one nobody keeps, and .githooks/bd-gh-sync:510
# carries exactly this directive over a split that is load-bearing.

d="$(tree sanctioned)"
printf '%s' "$SANCTIONED" >"$d/hook"
run_lint "$d"
if [ "$RC" -eq 0 ]; then
    ok "a sanctioned 'shellcheck disable' does not redden the gate"
else
    bad "a sanctioned 'shellcheck disable' does not redden the gate" \
        "exited $RC: $OUT"
fi

# --- a clean tree is green, so the two above are not both just "always red" ---

d="$(tree clean)"
printf '%s' "$CLEAN" >"$d/hook"
run_lint "$d"
if [ "$RC" -eq 0 ]; then
    ok "a clean shell script leaves the gate green"
else
    bad "a clean shell script leaves the gate green" "exited $RC: $OUT"
fi

# --- the selection is recursive ----------------------------------------------
# `.githooks/tests/` is a subdirectory, and a gate that only reads the top level
# would go green over every test script in this repo without saying so.

d="$(tree nested)"
mkdir -p "$d/sub"
printf '%s' "$CLEAN" >"$d/hook"
printf '%s' "$DIRTY" >"$d/sub/deep"
run_lint "$d"
if [ "$RC" -eq 0 ]; then
    bad "a violation in a subdirectory is found" "exited 0: $OUT"
elif ! printf '%s' "$OUT" | grep -q "$d/sub/deep"; then
    bad "a violation in a subdirectory is found" "the nested file was not named: $OUT"
else
    ok "a violation in a subdirectory of the hooks tree is found"
fi

# --- a selection that matched nothing is a failure, not a pass ---------------
# The defect this whole bead is about: a gate reporting success over a set it
# never looked at. An empty selection is byte-for-byte a clean tree unless the
# recipe refuses it.

d="$(tree empty)"
run_lint "$d"
if [ "$RC" -eq 0 ]; then
    bad "an empty selection is refused, not reported as clean" "exited 0: $OUT"
elif ! printf '%s' "$OUT" | grep -q 'watching nothing'; then
    bad "an empty selection is refused, not reported as clean" \
        "refused without saying the gate saw no files: $OUT"
else
    ok "a hooks tree with no shell script in it fails rather than passing vacuously"
fi

# --- a file the recipe cannot classify is a failure, not a silent skip -------
# The other way the selection shrinks: a new hook shaped so the shebang test
# does not recognise it drops out of the linted set and nothing says so.

d="$(tree unclassified)"
printf '%s' "$CLEAN" >"$d/hook"
printf 'plain text, no shebang at all\n' >"$d/NOTES"
run_lint "$d"
if [ "$RC" -eq 0 ]; then
    bad "an unclassifiable file is refused, not skipped" "exited 0: $OUT"
elif ! printf '%s' "$OUT" | grep -q "$d/NOTES"; then
    bad "an unclassifiable file is refused, not skipped" "did not name it: $OUT"
else
    ok "a file with no recognised shebang fails the gate by name"
fi

# --- ...but a python hook is classified, not refused -------------------------
# .githooks/claude-pre-bash is python3, and shellcheck answers SC1071 on it. It
# has to be skipped by what it is rather than by silencing the rule, or SC1071
# would be off for every shell file too.

d="$(tree python)"
printf '%s' "$CLEAN" >"$d/hook"
printf '#!/usr/bin/env python3\nprint("hi")\n' >"$d/py-hook"
run_lint "$d"
if [ "$RC" -ne 0 ]; then
    bad "a python hook is skipped rather than refused" "exited $RC: $OUT"
elif printf '%s' "$OUT" | grep -q 'py-hook'; then
    bad "a python hook is skipped rather than refused" \
        "it was handed to shellcheck: $OUT"
else
    ok "a python hook is skipped by its shebang, not by disabling SC1071"
fi

# --- and the gate is pointed at the real tree --------------------------------
# Everything above runs over throwaway directories, so all of it would hold just
# as well if the default argument named a directory that does not exist. This is
# the assertion that the linted set is the repo's own hooks.

OUT="$(cd "$REPO" && just lint-hooks 2>&1)"
RC=$?
if [ "$RC" -ne 0 ]; then
    bad "the default selection is the repo's own hooks tree" "exited $RC: $OUT"
elif ! printf '%s' "$OUT" | grep -q '\.githooks/bd-gh-sync$'; then
    bad "the default selection is the repo's own hooks tree" \
        ".githooks/bd-gh-sync is not in the linted set: $OUT"
elif ! printf '%s' "$OUT" | grep -q '\.githooks/tests/bd-gh-sync-test\.sh$'; then
    bad "the default selection is the repo's own hooks tree" \
        ".githooks/tests/bd-gh-sync-test.sh is not in the linted set: $OUT"
else
    ok "the default run lints the repo's own hooks, bd-gh-sync among them"
fi

# --- ...and CI reaches the recipe at all -------------------------------------
# Everything above names `lint-hooks` itself. CI never does: it runs `just lint`,
# and the sole edge from there to shellcheck is the `lint-hooks` dependency
# written on that recipe's line. Strike the word and every assertion in this
# file still passes — over a hooks tree no CI job lints — and a
# `# shellcheck disable=` directive in a tree with no linter is a comment, which
# is exactly the state bd gqlc-jhi2 was filed against. A recipe nothing calls is
# the same nothing as a recipe that does not exist, so the edge is asserted here
# rather than assumed by the rest of the file.
#
# The entry points are read out of the workflows rather than written down here:
# reachability from a recipe CI does not run proves nothing, and which recipe CI
# runs is not this file's to decide. Whole-line YAML comments are dropped first,
# or `just lint` written in prose would hold this green after the step itself
# was gone. The walk is transitive, so moving `lint-hooks` behind an
# intermediate recipe stays a refactor rather than becoming a failure.

WORKFLOWS="$REPO/.github/workflows"
ENTRY="$(grep -hvE '^[[:space:]]*#' "$WORKFLOWS"/*.yml |
    grep -oE 'just +[a-z0-9][a-z0-9-]*' | awk '{print $2}' | sort -u)"
DUMP="$(cd "$REPO" && just --dump --dump-format json 2>&1)"
DUMP_RC=$?
printf '%s' "$DUMP" >"$TMP/just-dump.json"

# REACHED <path> | UNREACHED | MISSING, and anything else means the walk itself
# did not run — caught below, because a walk that crashed prints nothing and an
# unreachable target prints nothing either.
REACH=""
if [ "$DUMP_RC" -eq 0 ] && [ -n "$ENTRY" ]; then
    REACH="$(python3 - "$TMP/just-dump.json" "$ENTRY" 2>&1 <<'PYEOF'
import json, sys

with open(sys.argv[1], encoding="utf-8") as fh:
    recipes = json.load(fh)["recipes"]
target = "lint-hooks"
if target not in recipes:
    print("MISSING")
    raise SystemExit(0)

# Breadth-first from each entry point, carrying the path so the answer can name
# the edge it found rather than merely assert one exists.
found = None
for entry in sys.argv[2].split():
    queue, seen = [[entry]], set()
    while queue and not found:
        path = queue.pop(0)
        node = path[-1]
        if node in seen or node not in recipes:
            continue
        seen.add(node)
        if node == target:
            found = path
            break
        for dep in recipes[node]["dependencies"]:
            queue.append(path + [dep["recipe"]])
    if found:
        break
print("REACHED " + " -> ".join(found) if found else "UNREACHED")
PYEOF
)"
fi

if [ "$DUMP_RC" -ne 0 ]; then
    bad "a CI recipe reaches the hooks linter" "just --dump exited $DUMP_RC: $DUMP"
elif [ -z "$ENTRY" ]; then
    bad "a CI recipe reaches the hooks linter" \
        "no 'just <recipe>' invocation found under $WORKFLOWS, so the walk had nowhere to start"
elif [ "$REACH" = "MISSING" ]; then
    bad "a CI recipe reaches the hooks linter" \
        "the justfile has no lint-hooks recipe at all"
elif [ "$REACH" = "UNREACHED" ]; then
    bad "a CI recipe reaches the hooks linter" \
        "none of the recipes CI runs depends on lint-hooks, so shellcheck never runs in CI: $(printf '%s' "$ENTRY" | tr '\n' ' ')"
elif [ -n "${REACH##REACHED *}" ]; then
    bad "a CI recipe reaches the hooks linter" \
        "the reachability walk did not run: $REACH"
else
    # Printed, like the linted set above: the edge is one word on one line of
    # the justfile, and this is the standing evidence that it is still there.
    ok "CI reaches shellcheck over the hooks tree by ${REACH#REACHED }"
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
