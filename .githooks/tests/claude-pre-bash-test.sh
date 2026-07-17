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

pass=0
fail=0

run_case() { # $1=name $2=expected(deny|allow) $3=cwd-for-hook $4=command-string
  local out decision
  out="$(
    cd "$3" || exit 1
    python3 -c 'import json,sys; print(json.dumps({"tool_name":"Bash","tool_input":{"command":sys.argv[1]}}))' "$4" | "$HOOK" 2>/dev/null
  )"
  decision=allow
  if printf '%s' "$out" | grep -q '"permissionDecision": *"deny"'; then decision=deny; fi
  if [ "$decision" = "$2" ]; then
    pass=$((pass + 1)); printf 'ok   - %s\n' "$1"
  else
    fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s)\n' "$1" "$2" "$decision"
  fi
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
run_case "commit inside \$() on master cwd"    deny  "$MASTER_REPO"  'X="$(git commit -m x)" && echo "$X"'
run_case "commit inside \$() on feature cwd"   allow "$FEATURE_REPO" 'X="$(git commit -m x)" && echo "$X"'
run_case "commit inside backticks on master"   deny  "$MASTER_REPO"  'X=`git commit -m x`'
run_case "-c option consumes its arg"          deny  "$MASTER_REPO"  'git -c user.email=x@x commit -m y'

# --- newline-separated commands (no && between them) must still be seen -----
run_case "newline-separated cd then commit"    deny  "$FEATURE_REPO" "$(printf 'cd %s\ngit commit -m x' "$MASTER_REPO")"
run_case "multi-line quoted commit message"    deny  "$MASTER_REPO"  "$(printf 'git commit -m "l1\nl2"')"

# --- bug 3: false positive — quoted literals and heredoc prose matched ------
run_case "quoted literal in echo"              allow "$MASTER_REPO"  'echo "git commit"'
run_case "prose argument (bd-comment shape)"   allow "$MASTER_REPO"  'bd comment gqlc-xyz "next step: git commit the export"'
run_case "heredoc body with prose+apostrophe"  allow "$MASTER_REPO"  "$(printf 'cat <<%s > /dev/null\ndo not ever run git commit here, it won'\''t fly\nEOF\n' "'EOF'")"

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
