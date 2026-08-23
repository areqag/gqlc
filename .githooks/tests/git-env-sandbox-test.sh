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

# The operator's dotfiles, stood in for. Nothing in .githooks/ reads HOME today
# — measured, `grep -rn HOME .githooks/` finds no reference outside prose — so
# this starts empty and every path that appears in it was put there by a suite.
#
# Pointing HOME at a throwaway is the half that stops mattering the moment
# someone adds the first `$HOME` row: a suite that wrote the real ~/.gitconfig
# would be green on the author's machine and red on a runner, which is the shape
# in the memory `seat worktree hides non-hermetic tests`. Part D below refuses
# the SPELLING; this refuses the damage, and neither implies the other.
#
# What it does not witness, in the same terms part B already uses for the
# canary: a READ. A suite that reads ~/.gitconfig and asserts on it creates
# nothing here and passes this row. bd gqlc-1mha.
FAKE_HOME="$TMP/fake-home"
mkdir -p "$FAKE_HOME"
home_snapshot() { find "$FAKE_HOME" | sort; }

# A throwaway home is not only $HOME. Measured on CI and not locally:
# km-overlap-test.sh runs `gh`, and gh writes a device id to
# $XDG_STATE_HOME/gh, defaulting to ~/.local/state/gh — so the row fired on a
# write no suite chose to make and could not avoid short of never invoking the
# tool. Redirecting the XDG roots is part of standing the home up, not an
# exemption from the row: a tool that ignores XDG and writes to ~ directly
# still trips it, and so does the suite's own dotfile.
XDG_ROOTS=(
    "XDG_STATE_HOME=$TMP/xdg/state"
    "XDG_CONFIG_HOME=$TMP/xdg/config"
    "XDG_CACHE_HOME=$TMP/xdg/cache"
    "XDG_DATA_HOME=$TMP/xdg/data"
)
mkdir -p "$TMP/xdg"

# RED control for the HOME row in the loop below. Same reason part B has one for
# the shim: a tripwire watching the wrong directory reports every suite clean.
# The fixture is composed with printf rather than written as a literal, so this
# file does not itself contain the spelling part D refuses.
DOTFILE_LEAK="$TMP/home-leak-suite.sh"
printf '#!/usr/bin/env bash\n: > "$%s/leaked-dotfile"\n' HOME >"$DOTFILE_LEAK"

home_control_before="$(home_snapshot)"
( cd "$ROOT" && env "HOME=$FAKE_HOME" timeout 120 bash "$DOTFILE_LEAK" ) >/dev/null 2>&1 || true
if [ "$home_control_before" != "$(home_snapshot)" ]; then
    ok "RED control: the HOME tripwire sees a suite writing a dotfile"
else
    bad "RED control: the HOME tripwire sees a suite writing a dotfile" \
        "a script whose only statement creates a file under HOME left the tripwire unchanged, so every HOME row in the loop below witnesses nothing"
fi
rm -rf "${FAKE_HOME:?}"
mkdir -p "$FAKE_HOME"

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
    home_before="$(home_snapshot)"
    ( cd "$ROOT" \
      && env "${poison[@]}" \
             "GQLC_GIT_ENV_LOG=$log" \
             "HOME=$FAKE_HOME" \
             "${XDG_ROOTS[@]}" \
             "PATH=$SHIM_DIR:$PATH" \
             timeout 120 bash "$suite" ${self_arg[@]+"${self_arg[@]}"} ) >/dev/null 2>&1 || rc=$?
    canary_after="$(snapshot)"
    home_after="$(home_snapshot)"

    if [ "$home_before" = "$home_after" ]; then
        ok "$name writes nothing into HOME"
    else
        bad "$name writes nothing into HOME" \
            "it created $(diff <(printf '%s\n' "$home_before") <(printf '%s\n' "$home_after") | grep '^>' | tr '\n' ' ') under HOME. On a developer machine that is the operator's own dotfiles. Keep the suite inside its own mktemp -d."
        rm -rf "${FAKE_HOME:?}"
        mkdir -p "$FAKE_HOME"
    fi

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


# --- C. the shared line is the only spelling a new file may use --------------
# Parts A and B are behavioural, so they pass either spelling: a file with its
# own `unset "${!GIT_@}"` is as sandboxed as one that sources the shared file,
# and neither row above can tell them apart. That is exactly why the private
# copies spread to eleven files before anyone noticed (bd gqlc-07bf, gqlc-o9wz).
#
# A copy is not a duplication nuisance. The shared file is where the REASON
# lives — the incident, the measurement, the fact that a private copy passes
# every direct run and fails only under a real push. A copy carries the line
# without the reason, so the next author to touch it has nothing telling them
# what it is for, and deleting it looks free.
#
# This row is textual on purpose and that is its whole limitation: it refuses a
# SPELLING, and says nothing about whether any file is actually sandboxed. It
# is the half B cannot do, in the same way B is the half this cannot do.
#
# LEGACY, and why the gate lands with exceptions rather than not at all. Four
# files still carry the copy. All four are outside the file set this change
# owns, and editing them tonight would collide with the lanes that do own them.
# Naming them is what lets the gate refuse a FIFTH, which is the value: a
# convention with no gate drifts back. Each entry is a debt, and the stale-entry
# row below is what stops an entry outliving the copy it excuses.
PATTERN='^[[:space:]]*unset[[:space:]]+"\$\{!GIT_@\}"'

# The definition itself, which necessarily carries the line.
DEFINITION='.githooks/git-env-sandbox.sh'

# And this file, because C1 below has to CONSTRUCT the shape to prove the
# pattern still matches it. That exemption is the one thing that could hide a
# regression here, so it is paid for immediately: the row after C1 asserts this
# file sources the shared line, which is the only thing the scan would have
# checked about it anyway.
SELF_PATH='.githooks/tests/git-env-sandbox-test.sh'

legacy=(
    '.githooks/tests/bd-gh-sync-test.sh'
    '.githooks/tests/km-overlap-test.sh'
    '.githooks/tests/km-test.sh'
    '.github/scripts/tests/ci-identity-gate-test.sh'
)

# C1, the RED control, and it is the row that keeps C2 from being decoration: a
# PATTERN that matched nothing would report a clean tree and read as a pass.
# Different invocation from C2's `git grep`, same PATTERN variable — what is
# being controlled for is the pattern rotting, not the search command.
LIVENESS="$TMP/liveness-fixture.sh"
cat >"$LIVENESS" <<'LIVENESS_FIXTURE'
#!/usr/bin/env bash
set -u
        unset "${!GIT_@}"
LIVENESS_FIXTURE
if grep -qE "$PATTERN" "$LIVENESS"; then
    ok "RED control: the inline-copy pattern matches an inline copy"
else
    bad "RED control: the inline-copy pattern matches an inline copy" \
        "the pattern found nothing in a file whose third line IS the copy, so the tree scan below would report clean over any number of them"
fi

# The pattern reads a live line, not a mention of one. Every suite that
# converted kept prose naming the construct, and a gate that could not tell a
# comment from code would have made those edits impossible to describe.
MENTION="$TMP/mention-fixture.sh"
cat >"$MENTION" <<'MENTION_FIXTURE'
#!/usr/bin/env bash
# This file talks about `unset "${!GIT_@}"` and does not run it.
#     unset "${!GIT_@}"
set -u
MENTION_FIXTURE
if grep -qE "$PATTERN" "$MENTION"; then
    bad "the inline-copy pattern ignores a commented mention" \
        "a file that only NAMES the construct in prose was reported as carrying a copy, so the gate forbids describing the rule it enforces"
else
    ok "the inline-copy pattern ignores a commented mention"
fi

# What the self-exemption above costs, paid back. The scan skips this file, so
# this is the row that would notice if it stopped sourcing the shared line.
if grep -qE '^source ".*/git-env-sandbox\.sh"$' "$ROOT/$SELF_PATH"; then
    ok "this file sources the shared line, which the scan below skips it for"
else
    bad "this file sources the shared line, which the scan below skips it for" \
        "$SELF_PATH is exempt from C2 because its fixtures construct the copy, and it no longer sources the shared file either, so nothing checks it at all"
fi

# C2, the gate. Tracked files only: git ls-files is the boundary, so a scratch
# file under /tmp or an untracked local edit is not the subject.
offenders=()
while IFS= read -r hit; do
    [ -n "$hit" ] || continue
    [ "$hit" = "$DEFINITION" ] && continue
    [ "$hit" = "$SELF_PATH" ] && continue
    skip=""
    for allowed in "${legacy[@]}"; do
        [ "$hit" = "$allowed" ] && skip=1 && break
    done
    [ -n "$skip" ] || offenders+=("$hit")
done < <(git -C "$ROOT" grep -lE "$PATTERN" -- . || true)

if [ "${#offenders[@]}" -eq 0 ]; then
    ok "no tracked file outside the named legacy set spells the scrub inline"
else
    bad "no tracked file outside the named legacy set spells the scrub inline" \
        "${offenders[*]} carries its own \`unset \"\${!GIT_@}\"\`. Replace it with: source \"\$(cd \"\$(dirname \"\$0\")/..\" && pwd)/git-env-sandbox.sh\" — the shared file carries the reason, a copy does not. Do not add the file to the legacy list above; that list is four debts, not a door."
fi

# C3, against the list rotting. An entry that no longer describes anything is a
# standing permission nobody is watching, and the next author to convert one of
# the four has no reason to remember to delete its line here.
for allowed in "${legacy[@]}"; do
    if [ ! -f "$ROOT/$allowed" ]; then
        bad "legacy entry $allowed still names a file" \
            "the path does not exist, so this entry excuses nothing and should be deleted"
    elif grep -qE "$PATTERN" "$ROOT/$allowed"; then
        ok "legacy entry $allowed still carries the copy it excuses"
    else
        bad "legacy entry $allowed still carries the copy it excuses" \
            "the file no longer spells the scrub inline, so the exception outlived the debt. Delete the line from the legacy list in this file — and if that empties the list, the gate is unconditional and this loop can go with it."
    fi
done


# --- D. no suite reads the machine it runs on through HOME ------------------
# The audit for bd gqlc-3qpx read all sixteen suites and found no reference to
# HOME at all, and three deliberate reads of the host, each the point of its own
# row and each documented in its own suite header: init-config-test.sh reads the
# real checkout's core.hooksPath as a tripwire on itself, claude-pre-ask-test.sh
# reads the shipped .claude/settings.json because a fixture copy would assert
# nothing, and km-test.sh asks real questions of origin/master. None of those is
# a HOME read and none is affected by this row.
#
# So this refuses a row that does not exist yet, which is the whole point: a
# suite reading ~/.gitconfig or ~/.claude/settings.json would be green on the
# author's machine, where those files say what the author expects, and red on a
# runner where they are absent. That asymmetry is the one this repo keeps paying
# for (memory: seat worktree hides non-hermetic tests). bd gqlc-1mha.
#
# Textual, and its limits are the limits of that: `${H}OME`, `getent passwd`,
# `eval` and `/home/$USER` all pass. The loop above is the behavioural half and
# catches a WRITE however it is spelled; neither half implies the other, and
# neither is a claim that a suite is hermetic.
#
# Full-line comments are exempt, so a suite can still explain the rule — this
# file's own header does. A line with code on it is not, wherever the mention
# sits on the line.
# Composed rather than written out, so that this file carries no literal its
# own pattern matches and the scan below can include it. The alternative was a
# self-exemption, and the exempt file is the one the gate cannot gate.
DOTFILE_PATTERN="$(printf '^[[:space:]]*[^#[:space:]].*([$]%s|[$][{]%s[}]|~[/])' HOME HOME)"

# D1, the RED control. Composed with printf so this file carries no literal the
# pattern would match, which is what lets the scan below include this file.
DOTFILE_LIVENESS="$TMP/home-liveness-fixture.sh"
printf '#!/usr/bin/env bash\ncfg="$%s/.gitconfig"\n' HOME >"$DOTFILE_LIVENESS"
if grep -qE "$DOTFILE_PATTERN" "$DOTFILE_LIVENESS"; then
    ok "RED control: the HOME pattern matches a HOME read"
else
    bad "RED control: the HOME pattern matches a HOME read" \
        "the pattern found nothing in a file whose second line reads the operator's .gitconfig, so the scan below would report clean over any number of them"
fi

# D2. The exemption that D1 alone would let rot into a blanket one.
DOTFILE_MENTION="$TMP/home-mention-fixture.sh"
printf '#!/usr/bin/env bash\n# A suite must not read $%s, nor anything under ~%s.\nset -u\n' HOME / >"$DOTFILE_MENTION"
if grep -qE "$DOTFILE_PATTERN" "$DOTFILE_MENTION"; then
    bad "the HOME pattern ignores a full-line comment" \
        "a file that only NAMES HOME in prose was reported as reading it, so the gate forbids describing the rule it enforces"
else
    ok "the HOME pattern ignores a full-line comment"
fi

# D3, the gate. Same file list part B uses, for the same reason: the directory
# is the classifier, not a glob.
home_readers=()
for suite in "${suites[@]}"; do
    if grep -qE "$DOTFILE_PATTERN" "$suite"; then
        home_readers+=("$(basename "$suite")")
    fi
done

if [ "${#home_readers[@]}" -eq 0 ]; then
    ok "no suite reads the operator's HOME"
else
    bad "no suite reads the operator's HOME" \
        "${home_readers[*]} names HOME, or a path under the home directory, on a line that is not a comment. A suite that reads the operator's dotfiles is green on the machine that has them and red on a runner. Put what the row needs in its own mktemp -d, or read it from the tree."
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
