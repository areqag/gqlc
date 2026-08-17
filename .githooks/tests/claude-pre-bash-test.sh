#!/usr/bin/env bash
# Unit tests for .githooks/claude-pre-bash (master-guard PreToolUse hook).
#
# Builds throwaway git repos under mktemp and feeds the hook the same JSON
# shape Claude Code sends on stdin, asserting allow/deny per case. The hook
# must resolve the branch at the command's EFFECTIVE target directory
# (leading `cd` chains, `git -C <path>`), not the hook's own pwd, and must
# not match `git commit` inside quoted literals or heredoc bodies.
#
# Run via: just test-hooks
set -u

# When run under a git hook (pre-push via `just test`), GIT_DIR etc. leak in
# and redirect every git call — repo setup would re-init the parent repo and
# the hook under test would resolve the wrong branch. Isolate completely.
unset "${!GIT_@}"

HOOK="$(cd "$(dirname "$0")/.." && pwd)/claude-pre-bash"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkrepo() { # $1=path $2=branch
  git init -q -b "$2" "$1"
  git -C "$1" -c user.email=t@t.invalid -c user.name=t commit -q --allow-empty -m init
}

MASTER_REPO="$TMP/on-master";   mkrepo "$MASTER_REPO" master
MAIN_REPO="$TMP/on-main";       mkrepo "$MAIN_REPO" main
FEATURE_REPO="$TMP/on-feature"; mkrepo "$FEATURE_REPO" feature-x
git init -q -b master "$TMP/unborn"   # repo with zero commits, HEAD unborn on master
DETACHED_REPO="$TMP/detached"; mkrepo "$DETACHED_REPO" master
git -C "$DETACHED_REPO" checkout -q --detach   # at master's tip, but not ON master

pass=0
fail=0

run_hook() { # $1=cwd-for-hook $2=command-string -> hook stdout
  (
    cd "$1" || exit 1
    python3 -c 'import json,sys; print(json.dumps({"tool_name":"Bash","tool_input":{"command":sys.argv[1]}}))' "$2" | "$HOOK" 2>/dev/null
  )
}

record() { # $1=name $2=expected $3=actual
  if [ "$3" = "$2" ]; then
    pass=$((pass + 1)); printf 'ok   - %s\n' "$1"
  else
    fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s)\n' "$1" "$2" "$3"
  fi
}

run_case() { # $1=name $2=expected(deny|allow) $3=cwd-for-hook $4=command-string
  local out decision
  out="$(run_hook "$3" "$4")"
  decision=allow
  if printf '%s' "$out" | grep -q '"permissionDecision": *"deny"'; then decision=deny; fi
  record "$1" "$2" "$decision"
}

# --- baseline behavior that must be preserved -------------------------------
run_case "bare form, cwd on master"            deny  "$MASTER_REPO"  'git commit -m x'
run_case "bare form, cwd on main"              deny  "$MAIN_REPO"    'git commit -m x'
run_case "bare form, cwd on feature branch"    allow "$FEATURE_REPO" 'git commit -m x'
run_case "commit-tree is not commit"           allow "$MASTER_REPO"  'git commit-tree HEAD^{tree} -m x'
run_case "unrelated git subcommand"            allow "$MASTER_REPO"  'git status'
run_case "second position in && chain"         deny  "$MASTER_REPO"  'go test ./... && git commit -m x'
run_case "env-assignment prefix"               deny  "$MASTER_REPO"  'GIT_AUTHOR_NAME=x git commit -m y'

# --- bug 1: false negative — `git -C <path>` form was never branch-checked --
run_case "-C to master repo from feature cwd"  deny  "$FEATURE_REPO" "git -C $MASTER_REPO commit -m x"
run_case "-C relative path to master repo"     deny  "$TMP"          'git -C on-master commit -m x'
run_case "-C to unborn master repo"            deny  "$TMP"          "git -C $TMP/unborn commit -m x"
run_case "cd to master repo from feature cwd"  deny  "$FEATURE_REPO" "cd $MASTER_REPO && git commit -m x"

# --- bug 2: false positive — target dir ignored, hook pwd checked instead ---
run_case "-C to feature repo from master cwd"  allow "$MASTER_REPO"  "git -C $FEATURE_REPO commit -m x"
run_case "cd to feature repo from master cwd"  allow "$MASTER_REPO"  "cd $FEATURE_REPO && git commit -m x"
run_case "cd then -C overrides tracked cwd"    allow "$MASTER_REPO"  "cd $TMP && git -C $FEATURE_REPO commit -m x"

# --- command substitution executes for real: must still be checked ----------
# The three below are the exception SC2016 exists to flag: the substitution is
# the fixture, and it has to reach the hook unevaluated or the case tests
# nothing. Taken one line at a time rather than file-wide, so a *stray*
# unexpanded expansion in a later case is still reported.
# shellcheck disable=SC2016 # the substitution is the hook's input, not this file's code
run_case "commit inside \$() on master cwd"    deny  "$MASTER_REPO"  'X="$(git commit -m x)" && echo "$X"'
# shellcheck disable=SC2016 # ditto
run_case "commit inside \$() on feature cwd"   allow "$FEATURE_REPO" 'X="$(git commit -m x)" && echo "$X"'
# shellcheck disable=SC2016 # ditto
run_case "commit inside backticks on master"   deny  "$MASTER_REPO"  'X=`git commit -m x`'
run_case "-c option consumes its arg"          deny  "$MASTER_REPO"  'git -c user.email=x@x commit -m y'

# --- newline-separated commands (no && between them) must still be seen -----
run_case "newline-separated cd then commit"    deny  "$FEATURE_REPO" "$(printf 'cd %s\ngit commit -m x' "$MASTER_REPO")"
run_case "multi-line quoted commit message"    deny  "$MASTER_REPO"  "$(printf 'git commit -m "l1\nl2"')"

# --- quoted << is prose, not a heredoc: must not swallow following lines ----
run_case "quoted heredoc-lookalike then commit" deny "$MASTER_REPO"  "$(printf 'echo "usage: cmd << EOF"\ngit commit -m x')"

# --- pushd tracks like cd ---------------------------------------------------
run_case "pushd to feature repo from master"   allow "$MASTER_REPO"  "pushd $FEATURE_REPO && git commit -m x"
run_case "pushd to master repo from feature"   deny  "$FEATURE_REPO" "pushd $MASTER_REPO && git commit -m x"

# --- branch resolution edges ------------------------------------------------
run_case "detached HEAD is not master"         allow "$DETACHED_REPO" 'git commit -m x'
run_case "unbalanced quote falls back, master" deny  "$MASTER_REPO"  "echo 'oops && git commit -m x"
run_case "unbalanced quote falls back, feature" allow "$FEATURE_REPO" "echo 'oops && git commit -m x"

# --- bug 3: false positive — quoted literals and heredoc prose matched ------
run_case "quoted literal in echo"              allow "$MASTER_REPO"  'echo "git commit"'
run_case "prose argument (bd-comment shape)"   allow "$MASTER_REPO"  'bd comment gqlc-xyz "next step: git commit the export"'
run_case "heredoc body with prose+apostrophe"  allow "$MASTER_REPO"  "$(printf 'cat <<%s > /dev/null\ndo not ever run git commit here, it won'\''t fly\nEOF\n' "'EOF'")"

# --- core.hooksPath drift (bd gqlc-nzwa) ------------------------------------
# Four config states, per bd gqlc-5fm. The fourth — a path that exists and is
# full of executable *.sample files — is the one that actually occurred twice,
# and the one an "is it set?" or "does the directory exist?" test passes over.
#
# Every fixture is a throwaway repo under mktemp and every `git config` here
# is pinned with `git -C`: an unpinned `git config` run from the wrong cwd is
# the documented root cause of drift occurrence #1 (bd gqlc-r41), so a test
# for this defect that could write the real repo's config would be the defect.
#
# Drift fixtures sit on a feature branch so a deny can only come from the
# hooks guard, never from the master guard — which is why run_drift_case
# separates deny-hooks from deny-master rather than reporting a bare "deny".
classify() { # $1=hook stdout -> deny-hooks|deny-master|warn|silent|unrecognized(...)
  if printf '%s' "$1" | grep -q '"permissionDecision": *"deny"'; then
    if printf '%s' "$1" | grep -q 'core\.hooksPath'; then printf 'deny-hooks'; else printf 'deny-master'; fi
  elif printf '%s' "$1" | grep -q '"systemMessage"'; then
    printf 'warn'
  elif [ -z "$1" ]; then
    printf 'silent'
  else
    printf 'unrecognized(%s)' "$1"
  fi
}

run_drift_case() { # $1=name $2=expected(deny-hooks|deny-master|warn|silent) $3=cwd $4=command
  record "$1" "$2" "$(classify "$(run_hook "$3" "$4")")"
}

mkhookrepo() { # $1=path — repo on a feature branch shipping one live .githooks hook
  mkrepo "$1" drift-branch
  mkdir -p "$1/.githooks"
  printf '#!/bin/sh\nexit 0\n' > "$1/.githooks/commit-msg"
  chmod +x "$1/.githooks/commit-msg"
}

# state 1: the correct value, hooks present and executable
OK_REPO="$TMP/hooks-ok"; mkhookrepo "$OK_REPO"
git -C "$OK_REPO" config core.hooksPath .githooks

# state 2: unset — git silently falls back to $GIT_DIR/hooks
UNSET_REPO="$TMP/hooks-unset"; mkhookrepo "$UNSET_REPO"

# state 3: a path that is not this repo's hook tree
WRONG_REPO="$TMP/hooks-wrong"; mkhookrepo "$WRONG_REPO"
git -C "$WRONG_REPO" config core.hooksPath "$TMP/nowhere"

# state 4: THE RECORDED DRIFT — absolute path at <gitdir>/hooks, which exists
# and holds executable files, every one of them a *.sample git will not run.
SAMPLE_REPO="$TMP/hooks-samples"; mkhookrepo "$SAMPLE_REPO"
find "$SAMPLE_REPO/.git/hooks" -type f ! -name '*.sample' -delete
printf '#!/bin/sh\nexit 0\n' > "$SAMPLE_REPO/.git/hooks/pre-commit.sample"
chmod +x "$SAMPLE_REPO/.git/hooks/pre-commit.sample"
git -C "$SAMPLE_REPO" config core.hooksPath "$SAMPLE_REPO/.git/hooks"

# state 5: value correct, but .githooks/ holds nothing git will execute. The
# value check cannot see this one; it is what separates "points somewhere"
# from "points at hooks that exist".
DEAD_REPO="$TMP/hooks-dead"; mkrepo "$DEAD_REPO" drift-branch
mkdir -p "$DEAD_REPO/.githooks"
printf '#!/bin/sh\nexit 0\n' > "$DEAD_REPO/.githooks/pre-commit.sample"
chmod +x "$DEAD_REPO/.githooks/pre-commit.sample"
git -C "$DEAD_REPO" config core.hooksPath .githooks

# state 5b: the other half of "hooks that exist" — right name, right path, but
# the file is not executable, which git skips as silently as a wrong path does.
NOEXEC_REPO="$TMP/hooks-noexec"; mkrepo "$NOEXEC_REPO" drift-branch
mkdir -p "$NOEXEC_REPO/.githooks"
printf '#!/bin/sh\nexit 0\n' > "$NOEXEC_REPO/.githooks/commit-msg"
chmod -x "$NOEXEC_REPO/.githooks/commit-msg"
git -C "$NOEXEC_REPO" config core.hooksPath .githooks

# out of scope: a repo that ships no .githooks/ at all is indistinguishable
# from any unrelated repo on the machine, so the check stays silent there.
BARE_REPO="$TMP/hooks-none"; mkrepo "$BARE_REPO" drift-branch

run_drift_case "correct value, live hooks: commit"   silent     "$OK_REPO"     'git commit -m x'
run_drift_case "correct value, live hooks: push"     silent     "$OK_REPO"     'git push'
run_drift_case "correct value, live hooks: merge"    silent     "$OK_REPO"     'git merge --no-ff side'
run_drift_case "correct value, live hooks: pull"     silent     "$OK_REPO"     'git pull'
run_drift_case "correct value, live hooks: ls"       silent     "$OK_REPO"     'ls -la'
run_drift_case "unset: commit refused"               deny-hooks "$UNSET_REPO"  'git commit -m x'
run_drift_case "unset: push refused"                 deny-hooks "$UNSET_REPO"  'git push'
# A merge or pull that writes a merge commit fires commit-msg — the
# AI-attribution gate — so a merge written while drifted is an ungated commit,
# not just a stale bd mirror. Refused for the same reason commit and push are.
# Other shapes fire less: `pull --rebase` on the row below fired post-merge
# while the branch was still fast-forwardable and none of the four once it had
# diverged. It is refused anyway because HOOK_GATED keys on the subcommand,
# which is all this hook can see before the fact — the shape is settled by
# remote state at run time. revert, cherry-pick (with and without -e), rebase
# and am fired none of the four in the same measurement, so they stay outside
# HOOK_GATED; the four warn rows below pin that membership boundary.
run_drift_case "unset: merge refused"                deny-hooks "$UNSET_REPO"  'git merge --no-ff side'
run_drift_case "unset: pull refused"                 deny-hooks "$UNSET_REPO"  'git pull --rebase'
run_drift_case "unset: revert only warns"            warn       "$UNSET_REPO"  'git revert --no-edit HEAD'
run_drift_case "unset: cherry-pick only warns"       warn       "$UNSET_REPO"  'git cherry-pick HEAD'
run_drift_case "unset: rebase only warns"            warn       "$UNSET_REPO"  'git rebase origin/master'
run_drift_case "unset: am only warns"                warn       "$UNSET_REPO"  'git am /tmp/x.patch'
run_drift_case "unset: innocuous command warns"      warn       "$UNSET_REPO"  'ls -la'
run_drift_case "wrong path: commit refused"          deny-hooks "$WRONG_REPO"  'git commit -m x'
run_drift_case "wrong path: push refused"            deny-hooks "$WRONG_REPO"  'git push'
run_drift_case "wrong path: innocuous command warns" warn       "$WRONG_REPO"  'ls -la'
run_drift_case "sample-only dir: commit refused"     deny-hooks "$SAMPLE_REPO" 'git commit -m x'
run_drift_case "sample-only dir: push refused"       deny-hooks "$SAMPLE_REPO" 'git push'
run_drift_case "sample-only dir: innocuous warns"    warn       "$SAMPLE_REPO" 'ls -la'
run_drift_case "value ok but no runnable hook"       deny-hooks "$DEAD_REPO"   'git commit -m x'
run_drift_case "value ok but hook not executable"    deny-hooks "$NOEXEC_REPO" 'git commit -m x'
run_drift_case "no .githooks/ in repo: silent"       silent     "$BARE_REPO"   'git commit -m x'
run_drift_case "non-repo cwd never fires"            silent     "$TMP"         'git commit -m x'

# the repair has to stay runnable, or the guard wedges the session that has to
# fix it. `just init` and a direct git config write are the two documented forms.
run_drift_case "just init still runs while drifted"  warn       "$UNSET_REPO"  'just init'
run_drift_case "config repair still runs"            warn       "$UNSET_REPO"  'git config core.hooksPath .githooks'

# the master guard keeps precedence, so its message is not replaced by drift's
MASTER_DRIFT="$TMP/hooks-master"; mkrepo "$MASTER_DRIFT" master
mkdir -p "$MASTER_DRIFT/.githooks"
printf '#!/bin/sh\nexit 0\n' > "$MASTER_DRIFT/.githooks/commit-msg"
chmod +x "$MASTER_DRIFT/.githooks/commit-msg"
run_drift_case "master guard wins over drift"        deny-master "$MASTER_DRIFT" 'git commit -m x'

# the drift check must follow the command's effective target, like the master
# guard does: a healthy cwd must not excuse a push into a drifted repo, and a
# drifted cwd must not condemn a commit aimed at a healthy one.
run_drift_case "-C into drifted repo from healthy"   deny-hooks "$OK_REPO"     "git -C $UNSET_REPO push"
run_drift_case "-C into healthy repo from drifted"   warn       "$UNSET_REPO"  "git -C $OK_REPO commit -m x"

# hooks_drift() strips GIT_* before shelling out, because repo-discovery env
# redirects `git -C <root> config --get` at whichever repo exported it — a
# drifted repo would then read a healthy repo's config and fall silent. The
# `unset "${!GIT_@}"` at the top of this file means no row above can reach that
# guard, so this one puts GIT_DIR back for a single call, pointed at the healthy
# repo while the cwd is the drifted one. Measured without the strip: the answer
# is `silent`, not merely a downgraded `warn` — GIT_DIR masks the drift outright.
run_gitdir_case() { # $1=name $2=expected $3=cwd $4=command $5=GIT_DIR
  local out
  out="$(
    cd "$3" || exit 1
    export GIT_DIR="$5"
    python3 -c 'import json,sys; print(json.dumps({"tool_name":"Bash","tool_input":{"command":sys.argv[1]}}))' "$4" | "$HOOK" 2>/dev/null
  )"
  record "$1" "$2" "$(classify "$out")"
}
run_gitdir_case "GIT_DIR cannot mask drift" deny-hooks "$UNSET_REPO" 'git commit -m x' "$OK_REPO/.git"

# An internal error must not read as a silent allow: this hook exists to refuse
# when the git hooks are dead, so exiting 0 with no output on its own bug is the
# defect it guards against. Malformed stdin is the reachable trigger — json.load
# raises before any check runs. The fixture is the HEALTHY repo on purpose: a
# drift warn is impossible there (the row above it is `silent`), so a warn here
# can only have come from the top-level handler.
run_raw_case() { # $1=name $2=expected $3=cwd $4=raw stdin
  local out
  out="$(cd "$3" && printf '%s' "$4" | "$HOOK" 2>/dev/null)"
  record "$1" "$2" "$(classify "$out")"
}
run_raw_case "healthy repo, valid stdin: silent"  silent "$OK_REPO" '{"tool_name":"Bash","tool_input":{"command":"ls"}}'
run_raw_case "internal error warns, not silent"   warn   "$OK_REPO" 'not json at all'

# A total pins suite SIZE, not membership — swapping any row for a different one
# leaves it green, which is why membership is pinned separately by the mutation
# battery (dropping or adding a HOOK_GATED entry has to turn a NAMED row red).
# What this catches is the one thing that battery cannot see: rows silently
# disappearing. Deleting all 27 run_drift_case invocations reported "29 passed,
# 0 failed" and exited 0; deleting the two escape-hatch rows reported "54
# passed, 0 failed". Both are now failures.
EXPECTED_ROWS=61
if [ "$((pass + fail))" -ne "$EXPECTED_ROWS" ]; then
  printf 'FAIL - suite size drifted: expected %d rows, ran %d\n' "$EXPECTED_ROWS" "$((pass + fail))"
  fail=$((fail + 1))
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
