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
#   B. Every suite in this directory — THIS FILE INCLUDED — run under the same
#      poison with a `git` that records the environment it was handed and
#      refuses to run. What is asserted is that no git invocation the suite
#      reaches inherits a poisoned value.
#
#      B emits two rows per suite, deliberately not folded into one. The first
#      reads the recording git's log. The second asks a question no log can
#      answer — did the poisoned repo itself move while the suite ran — and it
#      is what covers everything the shim cannot see. They detect different
#      things, so folded together either could rot while the row stayed green.
#
#      Including itself is not symmetry for its own sake. While this file was
#      exempt, deleting its own `source` line left it green at 13/13: the gate
#      could not gate its author. Its self-row is shallower than the others by
#      construction — the child stops after one git command rather than
#      recursing into part B — and that one command is the whole of what part B
#      reads from any suite anyway.
#
# What B's LOG row does not witness, stated rather than left to be found: the
# recording git refuses to run, so a suite aborts at its first git command and
# the calls it would have made afterwards are never seen. Sanitisation is a
# top-of-file property — the first call is where it holds or fails — but a
# suite that cleared the environment, ran a while, and then re-exported a
# poisoned value passes that row. The log row also sees only git invocations
# resolved through PATH: a suite calling /usr/bin/git, or shelling out to a
# tool that does, never touches the shim.
#
# Both of those are why the CANARY row exists. It is blind to intent and to
# spelling alike: it does not ask what the suite invoked or how, only whether
# the repository GIT_DIR names was written. Measured before it was added — a
# suite spelled `/usr/bin/git config --local user.name mutant`, dropped into
# this directory, was reported "runs no git command" and left this gate green
# at 23/23 (bd gqlc-10co, gqlc-kl2d).
#
# What the canary row does NOT witness, in turn: a read. A suite that runs
# `git log` against the shared repo and asserts on the answer corrupts nothing
# and moves nothing, so it passes both rows. Neither row is a claim that a
# suite is hermetic; together they are a claim that it does no damage.
#
# Run via: just test-hooks
set -u

HOOKS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
# This suite runs git itself, and is subject to the rule it enforces.
# `just lint-hooks` runs shellcheck without -x, so the path is not followed.
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$HOOKS_DIR/git-env-sandbox.sh"

# Part B runs every suite in this directory under a poisoned environment, and
# that includes this one. A child launched that way is told so by ARGUMENT: it
# makes exactly one git command — which is all part B reads — and stops before
# it would recurse into part B itself.
#
# An argument rather than an environment variable on purpose. A marker read
# from the environment could arrive from outside, and its effect here is to
# skip the entire suite; that failure would be silent and in the fail-open
# direction. Nothing invokes this file with `--self-test` except the loop below.
#
# This exists because the gate did not gate itself. With this file exempt from
# its own part B, deleting the `source` line above left the suite green at 13/13
# (Միհր's MG4 on PR #1195). Sanitisation is a top-of-file property, so ONE git
# command after the source is exactly the evidence that matters — the same
# reason part B's header already says a suite's later calls are not witnessed.
if [ "${1:-}" = "--self-test" ]; then
    git --version >/dev/null 2>&1 || true
    exit 0
fi

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

# RED control for part B. Every row below is read out of that log, so a shim
# that quietly stopped recording GIT_ variables would turn the whole of part B
# into passes — measured: blinding the `env | grep '^GIT_'` line above leaves
# the suite green (Միհր's MG2 on PR #1195). Part A has had a red control since
# it was written; this is the same idea for the half that had none.
#
# The control is a suite-shaped script that deliberately does NOT sandbox
# itself, run through the identical pipeline. If the leak is not seen here, the
# instrument is blind and nothing below means anything.
CONTROL="$TMP/leaky-suite.sh"
cat >"$CONTROL" <<'LEAKY'
#!/usr/bin/env bash
# Deliberately omits `source .githooks/git-env-sandbox.sh` — that omission is
# the whole point of this file.
git --version >/dev/null 2>&1 || true
LEAKY

control_log="$TMP/env.control.log"
: >"$control_log"
( cd "$ROOT" \
  && env "${poison[@]}" \
         "GQLC_GIT_ENV_LOG=$control_log" \
         "PATH=$SHIM_DIR:$PATH" \
         timeout 120 bash "$CONTROL" ) >/dev/null 2>&1 || true

if grep -qF "$MARK" "$control_log"; then
    ok "RED control: part B sees a suite that inherits the environment"
else
    bad "RED control: part B sees a suite that inherits the environment" \
        "the recording git logged no poisoned value for a script that never sandboxed itself, so every row below would pass whatever the suites do"
fi

# RED control: compliance that is only a comment is not compliance.
#
# This gate is behavioural, and this row is what says so rather than leaving it
# to be assumed. A gate that read the suite's source bytes for a `source
# .../git-env-sandbox.sh` line would accept the line below, because the line
# below is there — it is just commented out, so it clears nothing. The
# repository has been bitten by exactly that (a sweep over a function's raw
# source bytes accepting commented-out evidence, bd gqlc-tfh1's family), so the
# case is pinned here rather than argued.
COMMENTED="$TMP/commented-suite.sh"
cat >"$COMMENTED" <<COMMENTED_SCRIPT
#!/usr/bin/env bash
# The next line looks like the house idiom and does nothing at all.
# source "$HOOKS_DIR/git-env-sandbox.sh"
#     unset "\${!GIT_@}"
git --version >/dev/null 2>&1 || true
COMMENTED_SCRIPT

commented_log="$TMP/env.commented.log"
: >"$commented_log"
( cd "$ROOT" \
  && env "${poison[@]}" \
         "GQLC_GIT_ENV_LOG=$commented_log" \
         "PATH=$SHIM_DIR:$PATH" \
         timeout 120 bash "$COMMENTED" ) >/dev/null 2>&1 || true

if grep -qF "$MARK" "$commented_log"; then
    ok "RED control: a sandbox line inside a comment is still a leak"
else
    bad "RED control: a sandbox line inside a comment is still a leak" \
        "a script whose only sandbox line is commented out was not seen to leak, which means this gate is reading text rather than behaviour"
fi

# RED control: git reached by a path, which the shim above cannot see.
#
# Part B replaces `git` on PATH, so it witnesses only invocations resolved
# through PATH. A suite calling $REAL_GIT — or /usr/bin/git, or any tool that
# does — is outside the shim's reach entirely, and the log-half of every row
# below is blind to it. That blind spot was stated in this file's header and
# witnessed by nothing; it is witnessed here, and the canary check added to the
# loop below is what covers it.
#
# So this control asserts BOTH halves: the log does not see it (the blind spot
# is real, and if that ever stops being true this row says so) and the canary
# does (the cover works).
REAL_GIT="$(command -v git)"

PATHED="$TMP/pathed-suite.sh"
cat >"$PATHED" <<PATHED_SCRIPT
#!/usr/bin/env bash
# No sandbox, and git by absolute path: the shim on PATH never runs.
"$REAL_GIT" config --local user.email "$MARK@canary.example" >/dev/null 2>&1 || true
PATHED_SCRIPT

pathed_log="$TMP/env.pathed.log"
: >"$pathed_log"
pathed_before="$(snapshot)"
( cd "$ROOT" \
  && env "${poison[@]}" \
         "GQLC_GIT_ENV_LOG=$pathed_log" \
         "PATH=$SHIM_DIR:$PATH" \
         timeout 120 bash "$PATHED" ) >/dev/null 2>&1 || true
pathed_after="$(snapshot)"

if grep -qF "$MARK" "$pathed_log"; then
    bad "RED control: the shim is blind to git reached by a path" \
        "the shim logged a poisoned value for a script that never invoked git through PATH, so this control no longer describes the blind spot it was written for"
else
    ok "RED control: the shim is blind to git reached by a path"
fi

if [ "$pathed_before" != "$pathed_after" ]; then
    ok "RED control: the canary sees git reached by a path"
else
    bad "RED control: the canary sees git reached by a path" \
        "a script that wrote the poisoned repo by absolute path left the canary unchanged, so the canary rows in the loop below witness nothing"
fi

init_canary

# The directory is the classifier, as it is for ciguard's hookSuites: a glob
# for `*-test.sh` would silently skip a suite spelled some other way, and a
# skipped suite is the case this whole file exists to catch.
suites=()
while IFS= read -r entry; do
    suites+=("$entry")
done < <(find "$TESTS_DIR" -mindepth 1 -maxdepth 1 -type f | sort)

# This file is always one of them, so an empty list is impossible and a list of
# ONE means there is nothing here but us.
if [ "${#suites[@]}" -le 1 ]; then
    bad "the suite list holds more than this file" \
        "$TESTS_DIR holds only $SELF, so part B witnesses nothing about any other suite"
fi

for suite in "${suites[@]}"; do
    name="$(basename "$suite")"
    log="$TMP/env.$name.log"
    : >"$log"

    # Only ourselves, and only via argv — see the guard at the top of this file.
    self_arg=()
    [ "$name" = "$SELF" ] && self_arg=(--self-test)

    rc=0
    canary_before="$(snapshot)"
    ( cd "$ROOT" \
      && env "${poison[@]}" \
             "GQLC_GIT_ENV_LOG=$log" \
             "PATH=$SHIM_DIR:$PATH" \
             timeout 120 bash "$suite" ${self_arg[@]+"${self_arg[@]}"} ) >/dev/null 2>&1 || rc=$?
    canary_after="$(snapshot)"

    # The second half, and it is a row of its own rather than a branch of the
    # one below: the two halves detect different things, and folded together
    # either could rot while the row stayed green.
    #
    # The shim only replaces `git` on PATH. Anything reaching git another way —
    # an absolute path, a tool the suite shells out to that carries its own —
    # is invisible in the log and yet does the exact damage this file exists to
    # stop. This asks the other question: not what the suite invoked, but
    # whether the poisoned repo moved while it ran. Measured before it existed:
    # a suite spelled `/usr/bin/git config --local user.name mutant`, dropped
    # into this directory, was reported "runs no git command" and left the gate
    # green at 23/23.
    if [ "$canary_before" = "$canary_after" ]; then
        ok "$name leaves the poisoned repo untouched"
    else
        bad "$name leaves the poisoned repo untouched" \
            "it wrote the repo GIT_DIR pointed at: $(diff <(printf '%s\n' "$canary_before") <(printf '%s\n' "$canary_after") | tr '\n' ' '). Under a hook that repo is the shared checkout. Source .githooks/git-env-sandbox.sh at the top of the file — and note the shim did not see this, so the call did not go through PATH."
        # Independent rows: the next suite is measured against a clean canary,
        # not against this one's damage.
        init_canary
    fi

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
