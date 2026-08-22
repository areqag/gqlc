#!/usr/bin/env bash
# The gate on .githooks/git-env-sandbox.sh: no suite in this directory may
# carry git's ambient environment into its own git commands.
#
# git exports GIT_DIR to every hook it runs, and this repo runs these suites
# from pre-push. GIT_DIR beats `git -C <dir>`, so a suite that inherits it
# writes the SHARED repo — config and objects both — while believing it is
# writing a throwaway under /tmp. That is gqlc-7iea: a fixture identity in the
# config every linked worktree reads, and two citizens' commits authored
# `fixture <fixture@example.invalid>` as a result.
#
# Two halves, because either alone passes over the defect:
#
#   A. The MECHANISM, with its own RED control. A fixture-shaped script is run
#      under a poisoned environment twice — once without the sandbox, where the
#      canary repo MUST be written, and once with it, where the canary MUST be
#      untouched. The first row is what stops this suite from certifying a
#      detector that cannot see anything.
#
#   B. Every suite in this directory, run under the same poison with a `git`
#      that records the environment it was handed and refuses to run. What is
#      asserted is that no git invocation the suite reaches inherits a poisoned
#      value.
#
# What B does NOT witness, stated rather than left to be found: the recording
# git refuses to run, so a suite aborts at its first git command and the calls
# it would have made afterwards are never seen. Sanitisation is a top-of-file
# property — the first call is where it holds or fails — but a suite that
# cleared the environment, ran a while, and then re-exported a poisoned value
# would pass here. B also sees only git invocations resolved through PATH: a
# suite calling /usr/bin/git directly is outside its reach.
#
# Run via: just test-hooks
set -u

HOOKS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
# This suite runs git itself, and is subject to the rule it enforces.
# `just lint-hooks` runs shellcheck without -x, so the path is not followed.
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$HOOKS_DIR/git-env-sandbox.sh"

ROOT="$(cd "$HOOKS_DIR/.." && pwd)"
TESTS_DIR="$HOOKS_DIR/tests"
SELF="$(basename "$0")"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { pass=$((pass + 1)); printf 'ok   - %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL - %s: %s\n' "$1" "$2"; }

# Every poisoned value carries this string, so a leak is identified by what it
# carries rather than by which variable carried it.
MARK="gqlc-env-canary"

# The stand-in for the shared repo: what a leaked git command writes instead of
# the fixture it was aimed at.
CANARY="$TMP/$MARK"
init_canary() {
    rm -rf "$CANARY"
    git init -q -b master "$CANARY"
    git -C "$CANARY" config user.name "canary"
    git -C "$CANARY" config user.email "canary@canary.example"
    git -C "$CANARY" commit -q --allow-empty -m "canary init"
}
init_canary

# The variables git itself exports to a hook, plus the identity pair a fixture
# is most likely to set. GIT_DIR is the one that did the damage; the rest are
# here because a leak through any of them is the same defect.
poison=(
    "GIT_DIR=$CANARY/.git"
    "GIT_WORK_TREE=$CANARY"
    "GIT_INDEX_FILE=$CANARY/.git/index"
    "GIT_AUTHOR_NAME=$MARK"
    "GIT_AUTHOR_EMAIL=$MARK@canary.example"
    "GIT_COMMITTER_NAME=$MARK"
    "GIT_COMMITTER_EMAIL=$MARK@canary.example"
    "GIT_REFLOG_ACTION=$MARK"
)

# Config and history together: a leaked `git config` shows up in the first, a
# leaked commit in the second, and the incident produced both.
snapshot() {
    git -C "$CANARY" config --list --local
    git -C "$CANARY" log --all --format='%H %an <%ae> %s'
}

# --- A. the mechanism, and the control that keeps it honest -----------------
# A script shaped like the fixture that caused the incident: a throwaway repo,
# an identity written into it, a commit made in it. $1 is a scratch directory.
write_fixture_script() { # $1=path $2=sandboxed|bare
    local path="$1" sandbox=""
    [ "$2" = "sandboxed" ] && sandbox="source \"$HOOKS_DIR/git-env-sandbox.sh\""
    cat >"$path" <<SCRIPT
#!/usr/bin/env bash
set -u
$sandbox
d="\$1"
git init -q -b master "\$d/fixture"
git -C "\$d/fixture" config user.name fixture
git -C "\$d/fixture" config user.email fixture@example.invalid
git -C "\$d/fixture" commit -q --allow-empty -m "fixture commit"
SCRIPT
    chmod +x "$path"
}

run_fixture() { # $1=sandboxed|bare -> echoes the scratch dir it used
    local mode="$1"
    local scratch="$TMP/fixture-$mode"
    mkdir -p "$scratch"
    write_fixture_script "$TMP/fixture-$mode.sh" "$mode"
    env "${poison[@]}" bash "$TMP/fixture-$mode.sh" "$scratch" >/dev/null 2>&1 || true
    printf '%s' "$scratch"
}

before="$(snapshot)"
run_fixture bare >/dev/null
after_bare="$(snapshot)"

if [ "$before" = "$after_bare" ]; then
    bad "RED control: an unsandboxed fixture writes the canary" \
        "the canary is unchanged, so the poison below reaches nothing and every row in part B would pass over a real leak"
else
    ok "RED control: an unsandboxed fixture writes the canary"
fi

# Named, not merely different: the incident was an identity, and a snapshot
# diff would also be satisfied by a stray reflog entry.
if git -C "$CANARY" config --local --get user.name | grep -qx 'fixture'; then
    ok "RED control: the canary's user.name is the fixture's"
else
    bad "RED control: the canary's user.name is the fixture's" \
        "got '$(git -C "$CANARY" config --local --get user.name)'"
fi

# Reset the canary and run the same script with the sandbox sourced.
init_canary

before="$(snapshot)"
scratch="$(run_fixture sandboxed)"
after_sandboxed="$(snapshot)"

if [ "$before" = "$after_sandboxed" ]; then
    ok "the sandbox keeps the fixture's writes out of the canary"
else
    bad "the sandbox keeps the fixture's writes out of the canary" \
        "the canary changed: $(diff <(printf '%s\n' "$before") <(printf '%s\n' "$after_sandboxed") | tr '\n' ' ')"
fi

# The other half of that row: the script has to have done its work somewhere.
# Without this, a sandbox that made the script die on line one would pass.
if [ "$(git -C "$scratch/fixture" config --local --get user.name 2>/dev/null)" = "fixture" ]; then
    ok "the sandboxed fixture's writes land in the fixture"
else
    bad "the sandboxed fixture's writes land in the fixture" \
        "the fixture repo has no user.name, so the row above passed because nothing was written at all"
fi

# --- B. every suite in this directory ---------------------------------------
# A `git` that records what it was handed and refuses to run. The suite under
# test aborts at its first git command, which is what makes running all of them
# cost a fraction of a run.
SHIM_DIR="$TMP/shim"
mkdir -p "$SHIM_DIR"
cat >"$SHIM_DIR/git" <<'SHIM'
#!/usr/bin/env bash
# One record per invocation, written whether or not any GIT_ variable is
# present: an empty log has to mean "no git command ran", and a clean
# environment writes nothing of its own.
{ printf 'git-invocation\n'; env | grep '^GIT_' || true; } >>"$GQLC_GIT_ENV_LOG"
exit 111
SHIM
chmod +x "$SHIM_DIR/git"

# The directory is the classifier, as it is for ciguard's hookSuites: a glob
# for `*-test.sh` would silently skip a suite spelled some other way, and a
# skipped suite is the case this whole file exists to catch.
suites=()
while IFS= read -r entry; do
    [ "$(basename "$entry")" = "$SELF" ] && continue
    suites+=("$entry")
done < <(find "$TESTS_DIR" -mindepth 1 -maxdepth 1 -type f | sort)

if [ "${#suites[@]}" -eq 0 ]; then
    bad "the suite list is not empty" \
        "no file in $TESTS_DIR besides this one, so part B asserts nothing"
fi

for suite in "${suites[@]}"; do
    name="$(basename "$suite")"
    log="$TMP/env.$name.log"
    : >"$log"

    rc=0
    ( cd "$ROOT" \
      && env "${poison[@]}" \
             "GQLC_GIT_ENV_LOG=$log" \
             "PATH=$SHIM_DIR:$PATH" \
             timeout 120 bash "$suite" ) >/dev/null 2>&1 || rc=$?

    leaked="$(grep -F "$MARK" "$log" | sort -u | tr '\n' ' ')"
    if [ -n "$leaked" ]; then
        bad "$name isolates git's environment" \
            "its git commands were handed $leaked — under a hook those reach the shared repo, not its fixture. Source .githooks/git-env-sandbox.sh at the top of the file."
    elif [ -s "$log" ]; then
        # Non-poisoned GIT_ variables are the suite's own: several set GIT_DIR
        # deliberately, at their own fixtures.
        ok "$name isolates git's environment ($(grep -c '^git-invocation$' "$log") git invocation(s))"
    elif [ "$rc" -eq 0 ]; then
        ok "$name runs no git command"
    else
        bad "$name isolates git's environment" \
            "it ran no git command and exited $rc, so this row witnessed nothing either way"
    fi
done

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
