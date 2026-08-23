#!/usr/bin/env bash
# Tests for the ssh-keepalive fix — bd gqlc-ehgg / GH #1414.
#
# The defect: git opens the transport BEFORE running pre-push, this repository's
# pre-push then holds it idle for twelve to fifteen minutes of gates, and
# GitHub's idle timeout closes it. git exits 141 or 143 with every gate PASSED.
# Four lanes hit it on 2026-08-23; two abandoned work that had actually landed,
# one pushed with --no-verify.
#
# Three artifacts, and each is worthless without the others:
#
#   A. `just check-push-keepalive` — puts the keepalive in core.sshCommand, so
#      an ordinary `git push` is protected without anyone remembering an env
#      var. Wired into init, doctor and test, because the worktrees that predate
#      it will never run `just init` again.
#   B. .githooks/push-transport-notice — says the gates PASSED before the
#      transport can fail, so the failure is not read as a refusal.
#   C. `just push-landed` — answers "did it land anyway", which is the state two
#      lanes guessed wrong about.
#
# The rows that carry the design rather than the behaviour, so that a later
# cleanup cannot quietly undo the reasoning:
#   - an EXISTING core.sshCommand is never overwritten (someone's -i keyfile)
#   - core.hooksPath is untouched (agent spawns have rewritten it repo-wide)
#   - a keepalive MASKED by a later plain value is reported, which is why the
#     recipe reads --get and not --get-all
#   - GIT_SSH_COMMAND overrides the config, so the notice reads the env first
#   - an https remote gets no ssh prescription
#
# Run via: just test-hooks
set -u

# When run under a git hook (this file runs from pre-push via `just test`),
# GIT_DIR etc. leak in and would redirect every throwaway repo's git commands to
# the parent repo. Isolate completely.
# shellcheck source=../git-env-sandbox.sh disable=SC1091
source "$(cd "$(dirname "$0")/.." && pwd)/git-env-sandbox.sh"

HOOKS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT="$(cd "$HOOKS_DIR/.." && pwd)"
NOTICE="$HOOKS_DIR/push-transport-notice"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

GIT=(git -c user.email=t@t.invalid -c user.name=t -c commit.gpgsign=false)

pass=0
fail=0
ok() {
    pass=$((pass + 1))
    printf 'ok   - %s\n' "$1"
}
bad() {
    fail=$((fail + 1))
    printf 'FAIL - %s\n' "$1"
}

# --- A. wiring --------------------------------------------------------------
# Read out of just's parsed recipe graph rather than grepped out of the
# justfile, so a dependency sitting in a comment cannot answer for a wired one.
# `-f "$ROOT/justfile"` so this judges the repository's justfile whatever the
# caller's cwd is.
for target in init doctor test; do
    if just -f "$ROOT/justfile" --dump --dump-format json 2>/dev/null | python3 -c '
import json, sys
d = json.load(sys.stdin)
deps = [x["recipe"] for x in d["recipes"][sys.argv[1]]["dependencies"]]
sys.exit(0 if "check-push-keepalive" in deps else 1)
' "$target"; then
        ok "wiring: 'just $target' depends on check-push-keepalive"
    else
        bad "wiring: 'just $target' does not depend on check-push-keepalive — nothing runs it"
    fi
done

if [ -x "$NOTICE" ]; then
    ok "wiring: push-transport-notice is executable"
else
    bad "wiring: push-transport-notice is not executable, so pre-push cannot run it"
fi

# Judged against a copy with COMMENT LINES BLANKED, line numbers preserved. The
# invocation sits under a paragraph of comment that names the script four times,
# so a plain grep over the file is satisfied by the prose alone: deleting the
# invocation left both rows below green when it was written that way (measured —
# a comment can be a witness, bd gqlc-o9wz's neighbour).
PREPUSH_CODE="$TMP/pre-push.code"
awk '{ if ($0 ~ /^[[:space:]]*#/) print ""; else print }' "$HOOKS_DIR/pre-push" >"$PREPUSH_CODE"

if grep -qF 'push-transport-notice' "$PREPUSH_CODE"; then
    ok "wiring: pre-push invokes push-transport-notice"
else
    bad "wiring: pre-push never invokes push-transport-notice — the notice is inert"
fi

# It has to be the LAST thing pre-push runs: it claims the gates are done, and
# a claim made before the last gate is a false one.
if [ "$(grep -n 'push-transport-notice' "$PREPUSH_CODE" | tail -1 | cut -d: -f1)" \
    -gt "$(grep -n 'just test$' "$PREPUSH_CODE" | tail -1 | cut -d: -f1)" ]; then
    ok "wiring: the notice is invoked after 'just test', so 'gates passed' is true when printed"
else
    bad "wiring: the notice is invoked BEFORE the suite — it would claim gates that had not run"
fi

# --- B. check-push-keepalive over throwaway repositories --------------------
REPO="$TMP/repo"
"${GIT[@]}" init -q -b master "$REPO"
printf 'a\n' >"$REPO/f"
"${GIT[@]}" -C "$REPO" add f
"${GIT[@]}" -C "$REPO" commit -q -m init

# CI is unset for every behavioural row: the recipe skips under CI by design,
# and `just test-hooks` runs this file under CI=true on every runner. Without
# the unset every row below would pass vacuously on the machine that matters.
keepalive() {
    env -u CI just -f "$ROOT/justfile" check-push-keepalive "$REPO" 2>&1
}
cfg() {
    "${GIT[@]}" -C "$REPO" config --get core.sshCommand 2>/dev/null || true
}

out="$(keepalive)"
rc=$?
if [ "$rc" -ne 0 ]; then
    bad "unset: the recipe exited $rc — it must never refuse a push over this"
else
    ok "unset: the recipe exits 0"
fi
case "$(cfg)" in
    *"ServerAliveInterval=15"*"ServerAliveCountMax=120"*)
        ok "unset: core.sshCommand is set to a keepalive-bearing ssh command" ;;
    *)
        bad "unset: core.sshCommand is '$(cfg)', carrying no keepalive" ;;
esac
if printf '%s' "$out" | grep -qF 'core.sshCommand set to'; then
    ok "unset: it says what it wrote"
else
    bad "unset: it wrote core.sshCommand without saying so: $out"
fi

# Second run: idempotent AND silent. A note repeated on every `just test` is a
# note people stop reading.
out="$(keepalive)"
if [ -z "$out" ]; then
    ok "already keepalived: the recipe is silent"
else
    bad "already keepalived: the recipe spoke again on a healthy repo: $out"
fi

# THE VALUELESS SPELLING. `git config core.sshCommand ""` writes `sshCommand =`,
# which reads back as a present EMPTY string and which git then falls through to
# plain ssh over — so it carries no keepalive and is nobody's configuration.
# Treated as absent, not as a value to preserve.
"${GIT[@]}" -C "$REPO" config core.sshCommand ""
if [ -z "$(cfg)" ]; then
    ok "empty core.sshCommand reads back as an empty value (premise holds)"
else
    bad "empty core.sshCommand did not read back empty (premise gone): $(cfg)"
fi
keepalive >/dev/null
case "$(cfg)" in
    *ServerAliveInterval=*) ok "an empty core.sshCommand is treated as absent and gets the keepalive" ;;
    *) bad "an empty core.sshCommand was left empty: '$(cfg)'" ;;
esac

# A FOREIGN VALUE IS NEVER OVERWRITTEN. `ssh -i ~/.ssh/id_foo` is legitimate and
# belongs to whoever wrote it; clobbering it locks its author out of the remote,
# which is a larger harm than a lost push. Warn, do not touch, exit 0.
foreign='ssh -i /tmp/nonexistent-key-for-test'
"${GIT[@]}" -C "$REPO" config core.sshCommand "$foreign"
out="$(keepalive)"
rc=$?
if [ "$rc" -ne 0 ]; then
    bad "foreign value: the recipe exited $rc — a custom ssh command must not refuse a push"
else
    ok "foreign value: the recipe exits 0"
fi
if [ "$(cfg)" = "$foreign" ]; then
    ok "foreign value: core.sshCommand is left exactly as its author wrote it"
else
    bad "foreign value: core.sshCommand was overwritten with '$(cfg)'"
fi
if printf '%s' "$out" | grep -qF "core.sshCommand is '$foreign'"; then
    ok "foreign value: the warning quotes the value it refused to touch"
else
    bad "foreign value: the warning does not name the value: $out"
fi
if printf '%s' "$out" | grep -qF 'ServerAliveInterval=15'; then
    ok "foreign value: the warning shows how to add the keepalive by hand"
else
    bad "foreign value: the warning names no remedy: $out"
fi

# A foreign value that ALREADY keepalives is accepted in silence — the recipe
# wants the property, not its own spelling of it.
"${GIT[@]}" -C "$REPO" config core.sshCommand 'ssh -o ServerAliveInterval=60'
out="$(keepalive)"
if [ -z "$out" ] && [ "$(cfg)" = 'ssh -o ServerAliveInterval=60' ]; then
    ok "a different keepalive spelling is accepted silently and left alone"
else
    bad "a different keepalive spelling was not accepted: out='$out' cfg='$(cfg)'"
fi

# MASKED. Two values, keepalive FIRST. git resolves the LAST, so what is in
# effect carries no keepalive — and `--get-all` would find the good one and
# report health. This ordering is the only one that holds --get in place.
"${GIT[@]}" -C "$REPO" config --unset-all core.sshCommand
"${GIT[@]}" -C "$REPO" config --add core.sshCommand 'ssh -o ServerAliveInterval=15'
"${GIT[@]}" -C "$REPO" config --add core.sshCommand 'ssh'
out="$(keepalive)"
if printf '%s' "$out" | grep -qF 'no ServerAliveInterval'; then
    ok "a keepalive masked by a later plain value is reported, not read as health"
else
    bad "a masked keepalive read as health, so the recipe is judging the wrong value: $out"
fi
"${GIT[@]}" -C "$REPO" config --unset-all core.sshCommand

# CORE.HOOKSPATH IS NOT TOUCHED. Agent spawns have rewritten it in the SHARED
# config and disabled every hook repo-wide; this recipe writes into that same
# file, so the row is not paranoia.
"${GIT[@]}" -C "$REPO" config core.hooksPath .githooks
keepalive >/dev/null
if [ "$("${GIT[@]}" -C "$REPO" config --get core.hooksPath)" = ".githooks" ]; then
    ok "core.hooksPath survives the write untouched"
else
    bad "core.hooksPath is now '$("${GIT[@]}" -C "$REPO" config --get core.hooksPath)' — the recipe disturbed it"
fi

# THE CI SKIP, pinned as behaviour. Every row above runs with CI unset, so
# nothing else here can tell a working recipe from one that skips always.
"${GIT[@]}" -C "$REPO" config --unset-all core.sshCommand
out="$(CI=true just -f "$ROOT/justfile" check-push-keepalive "$REPO" 2>&1)"
if [ -z "$out" ] && [ -z "$(cfg)" ]; then
    ok "under CI the recipe writes nothing and says nothing"
else
    bad "under CI the recipe acted: out='$out' cfg='$(cfg)'"
fi

# --- C. push-transport-notice ------------------------------------------------
# Run with an empty HOME-independent config: the notice reads core.sshCommand
# from whatever repository it runs in, so each row states the env explicitly and
# runs from the throwaway repo.
notice() {
    # $1 = ssh command in the env ("" for none), $2 = remote url, rest = env pairs
    local env_ssh="$1" url="$2"
    shift 2
    if [ -n "$env_ssh" ]; then
        (cd "$REPO" && env GIT_SSH_COMMAND="$env_ssh" "$@" "$NOTICE" origin "$url" 2>&1)
    else
        (cd "$REPO" && env -u GIT_SSH_COMMAND "$@" "$NOTICE" origin "$url" 2>&1)
    fi
}

ssh_url='git@github.com:areqag/gqlc.git'

"${GIT[@]}" -C "$REPO" config core.sshCommand 'ssh -o ServerAliveInterval=15'
out="$(notice "" "$ssh_url" env)"
rc=$?
if [ "$rc" -eq 0 ]; then
    ok "notice: exits 0 (it can never refuse a push)"
else
    bad "notice: exited $rc — it must never refuse a push"
fi
if printf '%s' "$out" | grep -qF 'every gate PASSED'; then
    ok "notice: states that the gates passed, so a later rc=141 is not read as a refusal"
else
    bad "notice: never says the gates passed: $out"
fi
if printf '%s' "$out" | grep -qF 'just push-landed'; then
    ok "notice: names the command that answers 'did it land anyway'"
else
    bad "notice: leaves 'did it land' to a hand investigation: $out"
fi
if printf '%s' "$out" | grep -qF 'NO SSH KEEPALIVE'; then
    bad "notice: warned about a missing keepalive while core.sshCommand carries one: $out"
else
    ok "notice: with a keepalive configured, it does not warn about one"
fi

out="$(notice "" "$ssh_url" env GQLC_PREPUSH_ELAPSED=931)"
if printf '%s' "$out" | grep -qF '(931s)'; then
    ok "notice: reports how long the connection was held idle when told"
else
    bad "notice: dropped the elapsed time it was handed: $out"
fi

# No keepalive anywhere, ssh remote: the timeout is live, and it says so.
"${GIT[@]}" -C "$REPO" config --unset-all core.sshCommand
out="$(notice "" "$ssh_url" env)"
if printf '%s' "$out" | grep -qF 'NO SSH KEEPALIVE'; then
    ok "notice: an unprotected ssh push is told the idle timeout is live"
else
    bad "notice: stayed quiet on an unprotected ssh push: $out"
fi

# An https remote reaches GitHub through another transport; prescribing an ssh
# option there is a confident wrong diagnosis.
out="$(notice "" 'https://github.com/areqag/gqlc.git' env)"
if printf '%s' "$out" | grep -qF 'NO SSH KEEPALIVE'; then
    bad "notice: prescribed an ssh keepalive for an https remote: $out"
else
    ok "notice: says nothing about ssh options on an https remote"
fi
if printf '%s' "$out" | grep -qF 'every gate PASSED'; then
    ok "notice: an https push still gets the gates-passed statement"
else
    bad "notice: an https push got no notice at all: $out"
fi

# GIT_SSH_COMMAND OVERRIDES core.sshCommand. Config keepalived, env not: the
# push is unprotected, and a notice reading only the config would report health.
"${GIT[@]}" -C "$REPO" config core.sshCommand 'ssh -o ServerAliveInterval=15'
out="$(notice 'ssh' "$ssh_url" env)"
if printf '%s' "$out" | grep -qF 'NO SSH KEEPALIVE'; then
    ok "notice: a plain GIT_SSH_COMMAND in the env displaces the configured keepalive, and it says so"
else
    bad "notice: read the config while GIT_SSH_COMMAND overrode it: $out"
fi
# And the converse: env keepalived, config not.
"${GIT[@]}" -C "$REPO" config --unset-all core.sshCommand
out="$(notice 'ssh -o ServerAliveInterval=15' "$ssh_url" env)"
if printf '%s' "$out" | grep -qF 'NO SSH KEEPALIVE'; then
    bad "notice: warned while GIT_SSH_COMMAND carried a keepalive: $out"
else
    ok "notice: a keepalive supplied through GIT_SSH_COMMAND counts"
fi

# --- D. push-landed ----------------------------------------------------------
# A real remote, so the recipe's `git ls-remote origin` is exercised rather than
# stubbed.
ORIGIN="$TMP/origin.git"
"${GIT[@]}" init -q --bare -b master "$ORIGIN"
WORK="$TMP/work"
"${GIT[@]}" clone -q "$ORIGIN" "$WORK"
printf 'a\n' >"$WORK/f"
"${GIT[@]}" -C "$WORK" add f
"${GIT[@]}" -C "$WORK" commit -q -m init
"${GIT[@]}" -C "$WORK" push -q --no-verify origin master

landed() {
    env -u CI just -f "$ROOT/justfile" -d "$WORK" push-landed "$1" 2>&1
}

out="$(landed master)"
rc=$?
if [ "$rc" -eq 0 ] && printf '%s' "$out" | grep -qF 'LANDED:'; then
    ok "push-landed: a branch present on origin at the local SHA reports LANDED at rc=0"
else
    bad "push-landed: rc=$rc on a landed branch: $out"
fi

"${GIT[@]}" -C "$WORK" checkout -q -b topic
printf 'b\n' >>"$WORK/f"
"${GIT[@]}" -C "$WORK" commit -q -am topic
out="$(landed topic)"
rc=$?
if [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -qF 'NOT LANDED:'; then
    ok "push-landed: a branch absent from origin reports NOT LANDED at rc!=0"
else
    bad "push-landed: rc=$rc on an unpushed branch: $out"
fi

"${GIT[@]}" -C "$WORK" push -q --no-verify origin topic
printf 'c\n' >>"$WORK/f"
"${GIT[@]}" -C "$WORK" commit -q -am later
out="$(landed topic)"
rc=$?
if [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -qF 'DIVERGED:'; then
    ok "push-landed: origin holding a DIFFERENT commit is reported as DIVERGED, not as landed"
else
    bad "push-landed: rc=$rc on a diverged branch: $out"
fi

out="$(landed no-such-branch)"
rc=$?
if [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -qF 'is not a branch'; then
    ok "push-landed: a branch name that does not exist locally is refused, not reported unpushed"
else
    bad "push-landed: rc=$rc on a nonexistent branch: $out"
fi

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
